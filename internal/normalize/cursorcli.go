package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"
)

// CursorCLI projects one Cursor CLI cursor_cli_store_json export. The
// bytes are the filtered document the adapter uploaded. This type does
// not open a file, does not open SQLite, does not call Cursor.Normalize,
// and does not read cursor_state_json.
//
// One export is one session. session_id is "cursor-cli:" plus NativeID,
// the chat directory chats/<workspace>/<session>. That id is not an IDE
// composer id and it is not a "cursor:" session. HarnessVersion is the
// caller's pinned reader version, the same pin the adapter wrote on the
// document.
//
// v1 emits one envelope meta event, then meta rows in export order, then
// blob rows in conversation order (cursorCLIOrder), which reads the blob
// ids that the protobuf root blobs list. Meta key "0" is the session
// record. Fields on that record, including agentId, latestRootBlobId,
// mode, and lastUsedModel, stay on the meta event. session_id stays
// NativeID; it is not agentId. Other meta objects are meta only when
// every field is a session-shell name or timestamp. A blob is a message
// only when its data is a JSON object, cleartext is a string from
// content, then text, then rawText, and the role is user or assistant or
// the numeric type is 1 or 2. A content value that is not a string,
// including a parts array of text, reasoning, or tool blocks, stays one
// unknown event with the object on extra. The AI SDK tool-call and
// tool-result parts of such an array leave that object and become
// tool_call and tool_result events after it. Other parts are not
// promoted. Usage, compaction, summaries, base64 wrappers, and protobuf
// roots stay unknown; a root is decoded only to read its blob ids.
// encrypted, cipher, and sealed fields are copied into extra and are not
// written into content_text. They are not decrypted. Credential keys the
// adapter already drops stay absent.
type CursorCLI struct {
	Now            time.Time
	NativeID       string
	ParentNativeID string
	HarnessVersion string
	ProjectID      string
	CWD            string
	GitCommit      string
	GitBranch      string
	GitDirty       *bool
	Digest         string
}

var _ Normalizer = CursorCLI{}

var (
	errCursorCLINotObject   = errors.New("normalize: cursor-cli export is not a JSON object")
	errCursorCLINotDocument = errors.New("normalize: cursor-cli export is not a cursor_cli_store_json document")
)

// Normalize projects raw. raw is not modified. Malformed JSON fails the
// whole blob. The error text does not include the body. A valid
// document with no conversation turns still succeeds: the envelope meta
// event is enough.
func (c CursorCLI) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()

	doc, err := parseCursorCLIExport(raw)
	if err != nil {
		return nil, err
	}
	if c.HarnessVersion == "" {
		c.HarnessVersion = doc.HarnessVersion
	}

	env, err := c.envelope(doc)
	if err != nil {
		return nil, err
	}
	events := []Event{env}
	at := newOffsets(raw, len(doc.Meta), len(doc.Blobs))
	for i, row := range doc.Meta {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cliAuthKey(row.Key) {
			continue
		}
		ev, err := c.projectMeta(at.metaAt(i, row.Key), row)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	var calls callNames
	for _, i := range cursorCLIOrder(doc) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := doc.Blobs[i]
		if cliAuthKey(row.ID) {
			continue
		}
		evs, err := c.projectBlob(at.blobAt(i, row.ID), row, &calls)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return events, nil
}

// offsets is where each meta row's "key" value and each blob's "id"
// value sits in the export, by index in its array, read in one pass of
// a json.Decoder. Only the row's own top-level field counts, so a
// nested field or another row that names the same id never supplies
// the offset. Searching the whole document from the start for every
// row read it once per row, which on an export of tens of thousands of
// blobs and hundreds of megabytes did not finish.
type offsets struct {
	raw   []byte
	meta  []int
	blobs []int
	ok    bool
}

// newOffsets walks the meta and blobs arrays. A row with no top-level
// "key" or "id" gets the offset of its first byte. A document the
// decoder cannot walk, or whose arrays are not the length the parse
// found, has no offsets, and every row is looked for the way
// cursorOffset does.
func newOffsets(raw []byte, metaRows, blobRows int) offsets {
	o := offsets{raw: raw}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return o
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return o
		}
		var at *[]int
		field := ""
		switch tok {
		case "meta":
			at, field = &o.meta, "key"
		case "blobs":
			at, field = &o.blobs, "id"
		}
		if at == nil {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return o
			}
			continue
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
			return o
		}
		for dec.More() {
			off, err := rowField(dec, raw, field)
			if err != nil {
				return o
			}
			*at = append(*at, off)
		}
		if _, err := dec.Token(); err != nil {
			return o
		}
	}
	o.ok = len(o.meta) == metaRows && len(o.blobs) == blobRows
	return o
}

// rowField reads one array element and returns where the value of its
// top-level field starts, or where the element starts when it is not an
// object or has no such field.
func rowField(dec *json.Decoder, raw []byte, field string) (int, error) {
	start := skipSpace(raw, int(dec.InputOffset()), true)
	tok, err := dec.Token()
	if err != nil {
		return 0, err
	}
	if tok == json.Delim('[') {
		return start, skipOpen(dec)
	}
	if tok != json.Delim('{') {
		return start, nil
	}
	// The field matches as json.Unmarshal matches it for the parse:
	// case ignored, and a repeated field takes the last one that is not
	// null, since null leaves a string alone.
	at := start
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return 0, err
		}
		valueAt := skipSpace(raw, int(dec.InputOffset()), false)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return 0, err
		}
		if name, ok := key.(string); ok && strings.EqualFold(name, field) && !isNull(value) {
			at = valueAt
		}
	}
	if _, err := dec.Token(); err != nil {
		return 0, err
	}
	return at, nil
}

// skipOpen reads to the close of an array or object whose opening
// delimiter the decoder has just returned.
func skipOpen(dec *json.Decoder) error {
	for depth := 1; depth > 0; {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('['), json.Delim('{'):
			depth++
		case json.Delim(']'), json.Delim('}'):
			depth--
		}
	}
	return nil
}

// skipSpace moves at past whitespace and, between elements, a comma,
// or, after an object key, a colon: to the first byte of the next value.
func skipSpace(raw []byte, at int, comma bool) int {
	for at < len(raw) {
		switch c := raw[at]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		case comma && c == ',', !comma && c == ':':
		default:
			return at
		}
		at++
	}
	return at
}

func (o offsets) metaAt(i int, key string) int { return o.at(o.meta, i, key) }
func (o offsets) blobAt(i int, id string) int  { return o.at(o.blobs, i, id) }

func (o offsets) at(rows []int, i int, key string) int {
	if !o.ok || i >= len(rows) {
		return cursorOffset(o.raw, key)
	}
	return rows[i]
}

type cursorCLIExport struct {
	HarnessVersion string
	Confidence     string
	Source         string
	Scope          string
	Meta           []cursorCLIMeta
	Blobs          []cursorCLIBlob
	topExtra       map[string]any
}

type cursorCLIMeta struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type cursorCLIBlob struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

func parseCursorCLIExport(raw []byte) (cursorCLIExport, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("SQLite format 3")) {
		return cursorCLIExport{}, errCursorCLINotObject
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return cursorCLIExport{}, errCursorCLINotObject
	}
	metaRaw, metaOK := top["meta"]
	blobsRaw, blobsOK := top["blobs"]
	if !metaOK || !blobsOK || isNull(metaRaw) || isNull(blobsRaw) {
		return cursorCLIExport{}, errCursorCLINotDocument
	}
	var meta []cursorCLIMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return cursorCLIExport{}, errCursorCLINotDocument
	}
	var blobs []cursorCLIBlob
	if err := json.Unmarshal(blobsRaw, &blobs); err != nil {
		return cursorCLIExport{}, errCursorCLINotDocument
	}
	doc := cursorCLIExport{
		Meta:     meta,
		Blobs:    blobs,
		topExtra: map[string]any{},
	}
	if s, ok := jsonString(top["harness_version"]); ok {
		doc.HarnessVersion = s
	}
	if s, ok := jsonString(top["confidence"]); ok {
		doc.Confidence = s
	}
	if s, ok := jsonString(top["source"]); ok {
		doc.Source = s
	}
	if s, ok := jsonString(top["scope"]); ok {
		doc.Scope = s
	}
	for k, raw := range top {
		switch k {
		case "harness_version", "confidence", "source", "scope", "meta", "blobs":
			continue
		}
		if cliAuthKey(k) {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return cursorCLIExport{}, errCursorCLINotDocument
		}
		doc.topExtra[k] = stripCLIAuthValue(v)
	}
	return doc, nil
}

func (c CursorCLI) envelope(doc cursorCLIExport) (Event, error) {
	extra := map[string]any{
		"harness_version": doc.HarnessVersion,
		"confidence":      doc.Confidence,
		"source":          doc.Source,
		"scope":           doc.Scope,
	}
	for k, v := range doc.topExtra {
		if _, exists := extra[k]; exists {
			continue
		}
		extra[k] = v
	}
	return c.emit("cursor_cli_store_json", EventMeta, "", time.Time{}, 0, "", extra)
}

func (c CursorCLI) projectMeta(offset int, row cursorCLIMeta) (Event, error) {
	if isBase64Wrapper(row.Value) || !jsonIsObject(row.Value) {
		return c.projectMetaUnknown(offset, row)
	}
	obj, ok := jsonObject(row.Value)
	if !ok {
		return c.projectMetaUnknown(offset, row)
	}
	// Key "0" is the session record. It is never a message turn, even
	// when the object also carries text. Other objects are meta only
	// when every field is a session-shell name or timestamp.
	if row.Key == "0" || cliSessionShell(obj) {
		extra, err := cliObjectExtra(obj)
		if err != nil {
			return Event{}, err
		}
		extra["meta_key"] = row.Key
		return c.emit(row.Key, EventMeta, "", cursorWhen(obj), offset, "", extra)
	}
	return c.projectMetaUnknown(offset, row)
}

func (c CursorCLI) projectMetaUnknown(offset int, row cursorCLIMeta) (Event, error) {
	extra := map[string]any{"meta_key": row.Key}
	if !isNull(row.Value) {
		extra["value"] = cliOpaque(row.Value)
	}
	return c.emit(row.Key, EventUnknown, "", time.Time{}, offset, "", extra)
}

func (c CursorCLI) projectBlob(offset int, row cursorCLIBlob, calls *callNames) ([]Event, error) {
	one := func(ev Event, err error) ([]Event, error) {
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	if isBase64Wrapper(row.Data) || !jsonIsObject(row.Data) {
		return one(c.projectBlobUnknown(offset, row))
	}
	obj, ok := jsonObject(row.Data)
	if !ok {
		return one(c.projectBlobUnknown(offset, row))
	}
	// A non-string content value is a parts array or another structure.
	// Its tool parts become tool events. Its other parts are not read
	// for text, and text or rawText is not a fallback.
	if cliNonStringContent(obj) {
		return c.projectBlobParts(offset, row, obj, calls)
	}
	role, roleOK := cliMessageRole(obj)
	text, textOK := cliCleartext(obj)
	if !roleOK || !textOK {
		return one(c.projectBlobUnknown(offset, row))
	}
	extra, err := cliObjectExtra(obj)
	if err != nil {
		return nil, err
	}
	extra["blob_id"] = row.ID
	return one(c.emit(row.ID, EventMessage, role, cursorWhen(obj), offset, text, extra))
}

// projectBlobParts keeps the blob as one unknown event, with the object
// fields on extra, and then gives each tool-call part a tool_call event
// and each tool-result part a tool_result event, in part order. The
// tool parts leave the unknown event's content; text, reasoning and
// other parts stay there and are not promoted.
func (c CursorCLI) projectBlobParts(offset int, row cursorCLIBlob, obj map[string]json.RawMessage, calls *callNames) ([]Event, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(obj["content"], &parts); err != nil {
		parts = nil
	}
	var tools []map[string]json.RawMessage
	rest := []json.RawMessage{}
	for _, raw := range parts {
		part := rawObject(raw)
		kind, _ := jsonString(part["type"])
		if kind == "tool-call" || kind == "tool-result" {
			tools = append(tools, part)
			continue
		}
		rest = append(rest, raw)
	}
	if len(tools) > 0 {
		kept, err := json.Marshal(rest)
		if err != nil {
			return nil, err
		}
		trimmed := make(map[string]json.RawMessage, len(obj))
		for k, v := range obj {
			trimmed[k] = v
		}
		trimmed["content"] = kept
		obj = trimmed
	}
	extra, err := cliObjectExtra(obj)
	if err != nil {
		return nil, err
	}
	extra["blob_id"] = row.ID
	blob, err := c.emit(row.ID, EventUnknown, "", time.Time{}, offset, "", extra)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, 1+len(tools))
	out = append(out, blob)
	for _, part := range tools {
		ev, err := c.toolPart(offset, row.ID, part, calls)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// toolPart projects one AI SDK tool-call or tool-result part. raw_type
// is the part type and extra.blob_id names the blob. A part carries no
// time and no error flag, so recorded_at is the ingest time and
// tool.is_error stays null.
func (c CursorCLI) toolPart(offset int, blobID string, part map[string]json.RawMessage, calls *callNames) (Event, error) {
	kind, _ := jsonString(part["type"])
	callID, _ := jsonString(part["toolCallId"])
	toolName, _ := jsonString(part["toolName"])
	if kind == "tool-call" {
		name, server, builtin := cursorCLIToolName(toolName, part["args"])
		extra, err := extraFrom(part, "type", "toolCallId", "toolName", "args")
		if err != nil {
			return Event{}, err
		}
		extra = cursorCLIToolExtra(stripCLIAuthMap(extra), blobID, toolName, name, server, builtin)
		calls.remember(callID, name)
		ev, err := c.emit("tool-call", EventToolCall, ActorAssistant, time.Time{}, offset, argumentsText(part["args"]), extra)
		ev.Tool = Tool{Name: strPtr(name), CallID: strPtr(callID)}
		return ev, err
	}
	name := calls.name(callID)
	if name == "" {
		name = toolName
	}
	extra, err := extraFrom(part, "type", "toolCallId", "toolName", "result")
	if err != nil {
		return Event{}, err
	}
	extra = stripCLIAuthMap(extra)
	extra["blob_id"] = blobID
	if toolName != "" && toolName != name {
		extra["cursor_tool_name"] = toolName
	}
	ev, err := c.emit("tool-result", EventToolResult, ActorTool, time.Time{}, offset, argumentsText(part["result"]), extra)
	ev.Tool = Tool{Name: strPtr(name), CallID: strPtr(callID)}
	return ev, err
}

// cursorCLIToolName names a Cursor CLI tool call. CallDynamicTool
// carries the real tool in args, an object or a JSON-encoded string:
// args.namespace "cursor" is one of Cursor's own tools and keeps the
// bare args.toolName, and any other namespace is an MCP server, named
// mcp__<server>__<tool>. Every other call keeps its toolName.
func cursorCLIToolName(toolName string, args json.RawMessage) (name, server string, builtin bool) {
	if toolName != "CallDynamicTool" {
		return toolName, "", true
	}
	obj, ok := jsonObject(args)
	if !ok {
		if s, isString := jsonString(args); isString {
			obj, ok = jsonObject(json.RawMessage(s))
		}
	}
	if !ok {
		return toolName, "", false
	}
	namespace, _ := jsonString(obj["namespace"])
	inner, _ := jsonString(obj["toolName"])
	if inner == "" {
		return toolName, "", false
	}
	if namespace == "cursor" {
		return inner, "", true
	}
	if n := mcpToolName(namespace, inner); n != "" {
		return n, namespace, false
	}
	return toolName, "", false
}

func cursorCLIToolExtra(extra map[string]any, blobID, toolName, name, server string, builtin bool) map[string]any {
	extra["blob_id"] = blobID
	if toolName != name {
		extra["cursor_tool_name"] = toolName
	}
	if server != "" {
		extra["mcp_server"] = server
	}
	if builtin {
		extra["builtin"] = true
	}
	return extra
}

func (c CursorCLI) projectBlobUnknown(offset int, row cursorCLIBlob) (Event, error) {
	extra := map[string]any{"blob_id": row.ID}
	if !isNull(row.Data) {
		extra["value"] = cliOpaque(row.Data)
	}
	return c.emit(row.ID, EventUnknown, "", time.Time{}, offset, "", extra)
}

func (c CursorCLI) emit(rawType, eventType, role string, recorded time.Time, offset int, text string, extra map[string]any) (Event, error) {
	eid, err := id.New(c.Now)
	if err != nil {
		return Event{}, err
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if role != "" && knownRole(role) == nil {
		extra["role"] = role
	}
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       cursorCLISessionID(c.NativeID),
		ParentSessionID: cursorCLIParentID(c.ParentNativeID),
		Harness:         protocol.HarnessCursorCLI,
		HarnessVersion:  strPtr(c.HarnessVersion),
		RecordedAt:      recordedAt(recorded, c.Now),
		IngestedAt:      c.Now.Format(time.RFC3339Nano),
		CWDHash:         adapter.CWDHash(c.CWD),
		ProjectID:       strPtr(c.ProjectID),
		Git: Git{
			Branch: strPtr(c.GitBranch),
			Commit: strPtr(c.GitCommit),
			Dirty:  c.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		ContentText: strPtr(text),
		ContentRef:  contentRef(c.Digest, offset),
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func cursorCLISessionID(native string) string {
	if native == "" {
		native = "unknown"
	}
	return protocol.HarnessCursorCLI + ":" + native
}

func cursorCLIParentID(native string) *string {
	if native == "" {
		return nil
	}
	s := protocol.HarnessCursorCLI + ":" + native
	return &s
}

// cliNonStringContent reports a content field that is present and is
// not a JSON string. A parts array is the case v1 refuses to walk.
func cliNonStringContent(obj map[string]json.RawMessage) bool {
	raw, ok := obj["content"]
	if !ok || isNull(raw) {
		return false
	}
	_, isString := jsonString(raw)
	return !isString
}

// cliCleartext is the visible text of a blob. content wins, then text,
// then rawText, and only when that field is a string. A blank string is
// not cleartext and the next field is tried. A non-string content value
// yields no text: text and rawText are not a fallback for a parts array.
// A field whose name looks sealed is never the text.
func cliCleartext(obj map[string]json.RawMessage) (string, bool) {
	if cliNonStringContent(obj) {
		return "", false
	}
	for _, key := range []string{"content", "text", "rawText"} {
		if cursorOpaqueField(key) {
			continue
		}
		s, ok := jsonString(obj[key])
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		return s, true
	}
	return "", false
}

// cliMessageRole maps a user or assistant role, or numeric type 1 or 2.
// Any other role or type is not a message.
func cliMessageRole(obj map[string]json.RawMessage) (string, bool) {
	if s, ok := jsonString(obj["role"]); ok {
		switch s {
		case ActorUser, ActorAssistant:
			return s, true
		}
	}
	switch cursorBubbleType(obj["type"]) {
	case 1:
		return ActorUser, true
	case 2:
		return ActorAssistant, true
	}
	return "", false
}

// cliSessionShell reports an object whose fields are only a name, a
// title, or a timestamp. An empty object is not a session shell. Key
// "0" does not use this check.
func cliSessionShell(obj map[string]json.RawMessage) bool {
	if len(obj) == 0 {
		return false
	}
	for k := range obj {
		if !cliShellKey(k) {
			return false
		}
	}
	return true
}

func cliShellKey(key string) bool {
	switch key {
	case "name", "title", "createdAt", "created_at", "timestamp", "timestamps", "updatedAt", "updated_at":
		return true
	default:
		return false
	}
}

func cliObjectExtra(obj map[string]json.RawMessage) (map[string]any, error) {
	extra, err := extraFrom(obj)
	if err != nil {
		return nil, err
	}
	return stripCLIAuthMap(extra), nil
}

func cliOpaque(raw json.RawMessage) any {
	return stripCLIAuthValue(opaque(raw))
}

func stripCLIAuthMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if cliAuthKey(k) {
			continue
		}
		out[k] = stripCLIAuthValue(v)
	}
	return out
}

func stripCLIAuthValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return stripCLIAuthMap(t)
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = stripCLIAuthValue(child)
		}
		return out
	default:
		return v
	}
}

// cliAuthKey reports a credential key the adapter already drops. A
// cursorAuth key matches the whole name or the first slash- or
// colon-separated segment. The other names are exact credential
// fields. The compare ignores case.
func cliAuthKey(key string) bool {
	if strings.EqualFold(key, "cursorAuth") {
		return true
	}
	if head, _, ok := strings.Cut(key, "/"); ok && strings.EqualFold(head, "cursorAuth") {
		return true
	}
	if head, _, ok := strings.Cut(key, ":"); ok && strings.EqualFold(head, "cursorAuth") {
		return true
	}
	switch strings.ToLower(key) {
	case "accesstoken", "refreshtoken", "idtoken", "sessiontoken",
		"access_token", "refresh_token", "id_token", "session_token",
		"workoscursorsessiontoken", "blobencryptionkey":
		return true
	default:
		return false
	}
}
