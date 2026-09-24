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
// blob rows in export order. Meta key "0" is the session record. Fields
// on that record, including agentId, latestRootBlobId, mode, and
// lastUsedModel, stay on the meta event. The projector does not follow
// latestRootBlobId or any other blob id. session_id stays NativeID; it
// is not agentId. Other meta objects are meta only when every field is a
// session-shell name or timestamp. A blob is a message only when its
// data is a JSON object, cleartext is a string from content, then text,
// then rawText, and the role is user or assistant or the numeric type is
// 1 or 2. A content value that is not a string, including a parts array
// of text, reasoning, or tool blocks, stays one unknown event with the
// object on extra. Those parts are not walked into tool_call or
// tool_result. Tool calls, tool results, usage, compaction, summaries,
// base64 wrappers, and protobuf roots stay unknown and are not decoded.
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
	for _, row := range doc.Meta {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cliAuthKey(row.Key) {
			continue
		}
		ev, err := c.projectMeta(raw, row)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	for _, row := range doc.Blobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cliAuthKey(row.ID) {
			continue
		}
		ev, err := c.projectBlob(raw, row)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
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

func (c CursorCLI) projectMeta(raw []byte, row cursorCLIMeta) (Event, error) {
	if isBase64Wrapper(row.Value) || !jsonIsObject(row.Value) {
		return c.projectMetaUnknown(raw, row)
	}
	obj, ok := jsonObject(row.Value)
	if !ok {
		return c.projectMetaUnknown(raw, row)
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
		return c.emit(row.Key, EventMeta, "", cursorWhen(obj), cursorOffset(raw, row.Key), "", extra)
	}
	return c.projectMetaUnknown(raw, row)
}

func (c CursorCLI) projectMetaUnknown(raw []byte, row cursorCLIMeta) (Event, error) {
	extra := map[string]any{"meta_key": row.Key}
	if !isNull(row.Value) {
		extra["value"] = cliOpaque(row.Value)
	}
	return c.emit(row.Key, EventUnknown, "", time.Time{}, cursorOffset(raw, row.Key), "", extra)
}

func (c CursorCLI) projectBlob(raw []byte, row cursorCLIBlob) (Event, error) {
	if isBase64Wrapper(row.Data) || !jsonIsObject(row.Data) {
		return c.projectBlobUnknown(raw, row)
	}
	obj, ok := jsonObject(row.Data)
	if !ok {
		return c.projectBlobUnknown(raw, row)
	}
	// A non-string content value is a parts array or another structure.
	// v1 does not read strings out of it and does not fall through to
	// text or rawText.
	if cliNonStringContent(obj) {
		return c.projectBlobObjectUnknown(raw, row, obj)
	}
	role, roleOK := cliMessageRole(obj)
	text, textOK := cliCleartext(obj)
	if !roleOK || !textOK {
		return c.projectBlobUnknown(raw, row)
	}
	extra, err := cliObjectExtra(obj)
	if err != nil {
		return Event{}, err
	}
	extra["blob_id"] = row.ID
	return c.emit(row.ID, EventMessage, role, cursorWhen(obj), cursorOffset(raw, row.ID), text, extra)
}

// projectBlobObjectUnknown keeps the blob as one unknown event. The
// object fields, including a content parts array, sit on extra. Parts
// are not promoted.
func (c CursorCLI) projectBlobObjectUnknown(raw []byte, row cursorCLIBlob, obj map[string]json.RawMessage) (Event, error) {
	extra, err := cliObjectExtra(obj)
	if err != nil {
		return Event{}, err
	}
	extra["blob_id"] = row.ID
	return c.emit(row.ID, EventUnknown, "", time.Time{}, cursorOffset(raw, row.ID), "", extra)
}

func (c CursorCLI) projectBlobUnknown(raw []byte, row cursorCLIBlob) (Event, error) {
	extra := map[string]any{"blob_id": row.ID}
	if !isNull(row.Data) {
		extra["value"] = cliOpaque(row.Data)
	}
	return c.emit(row.ID, EventUnknown, "", time.Time{}, cursorOffset(raw, row.ID), "", extra)
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
		"workoscursorsessiontoken":
		return true
	default:
		return false
	}
}
