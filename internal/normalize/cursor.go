package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"
)

// Cursor projects one Cursor IDE cursor_state_json export. The bytes are
// the filtered document the adapter uploaded. This type does not open a
// file, does not open SQLite, and does not read cursor_cli_store_json.
//
// One export is one session. session_id is "cursor:" plus NativeID
// (global, or workspace/<id>). Composers in that export stay on the
// same session; composer_id is extra. HarnessVersion is the caller's
// pinned reader version, the same pin the adapter wrote on the
// document. A version string inside a row is not that pin.
//
// v1 projects cleartext bubble messages (type 1 user, type 2
// assistant, text from rawText then text), meta for composerData and
// the ItemTable composer and UI keys, and unknown for everything else.
// A bubble toolFormerData with a string name and a string call id
// (toolCallId, otherwise id) is also a sibling tool_call. content_text
// is rawArgs or params when that value is a non-empty JSON string,
// otherwise the JSON of args. A tool_result uses toolFormerData.result
// when that value is a string; otherwise each toolResults element
// that has a call id and a result string. A missing name or call id
// stays on extra. capabilityType and a numeric tool field do not
// promote on their own. usageData, tokenCount, and
// latestConversationSummary stay on the parent event. They are not
// promoted to usage or compaction. Bubble order follows
// fullConversationHeadersOnly, and a header move carries the whole
// bubble group. createdAt is not an order key.
//
// encrypted, cipher, and sealed fields are copied into extra and are
// not written into content_text. They are not decrypted. A cursorAuth
// key is left absent. Unknown keys are kept.
type Cursor struct {
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

var _ Normalizer = Cursor{}

var (
	errCursorNotObject   = errors.New("normalize: cursor export is not a JSON object")
	errCursorNotDocument = errors.New("normalize: cursor export is not a cursor_state_json document")

	cursorOpaqueFieldRE = regexp.MustCompile(`(?i)encrypted|cipher|sealed`)
)

// Normalize projects raw. raw is not modified. Malformed JSON fails the
// whole blob. The error text does not include the body. A valid
// document with no conversation turns still succeeds: the envelope meta
// event is enough.
func (c Cursor) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()

	doc, err := parseCursorExport(raw)
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
	orders := map[string][]string{}
	rows := make([]cursorKV, 0, len(doc.ItemTable)+len(doc.Disk))
	rows = append(rows, doc.ItemTable...)
	rows = append(rows, doc.Disk...)
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cursorAuthRow(row.Key) {
			continue
		}
		evs, err := c.row(raw, doc.Scope, orders, row)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return orderCursorBubbles(events, orders), nil
}

type cursorExport struct {
	HarnessVersion string
	Confidence     string
	Source         string
	Scope          string
	ItemTable      []cursorKV
	Disk           []cursorKV
	topExtra       map[string]any
}

type cursorKV struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func parseCursorExport(raw []byte) (cursorExport, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("SQLite format 3")) {
		return cursorExport{}, errCursorNotObject
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return cursorExport{}, errCursorNotObject
	}
	itemsRaw, ok := top["item_table"]
	if !ok || isNull(itemsRaw) {
		return cursorExport{}, errCursorNotDocument
	}
	var items []cursorKV
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return cursorExport{}, errCursorNotDocument
	}
	var disk []cursorKV
	if diskRaw, ok := top["cursor_disk_kv"]; ok && !isNull(diskRaw) {
		if err := json.Unmarshal(diskRaw, &disk); err != nil {
			return cursorExport{}, errCursorNotDocument
		}
	}
	doc := cursorExport{
		ItemTable: items,
		Disk:      disk,
		topExtra:  map[string]any{},
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
		case "harness_version", "confidence", "source", "scope", "item_table", "cursor_disk_kv":
			continue
		}
		if cursorAuthRow(k) {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return cursorExport{}, errCursorNotDocument
		}
		doc.topExtra[k] = v
	}
	return doc, nil
}

func (c Cursor) envelope(doc cursorExport) (Event, error) {
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
	return c.emit("cursor_state_json", EventMeta, "", time.Time{}, 0, "", extra)
}

func (c Cursor) row(raw []byte, scope string, orders map[string][]string, row cursorKV) ([]Event, error) {
	switch {
	case strings.HasPrefix(row.Key, "bubbleId:"):
		composer, bubble, ok := bubbleIDs(row.Key)
		if !ok {
			return c.one(c.projectUnknown(raw, scope, row))
		}
		return c.projectBubble(raw, scope, row, composer, bubble)
	case strings.HasPrefix(row.Key, "composerData:"):
		return c.one(c.projectComposerData(raw, scope, orders, row))
	case strings.HasPrefix(row.Key, "composer.content."):
		return c.one(c.projectUnknown(raw, scope, row))
	case strings.HasPrefix(row.Key, "composer."), cursorUIKey(row.Key):
		return c.one(c.projectMeta(raw, scope, row))
	default:
		return c.one(c.projectUnknown(raw, scope, row))
	}
}

func (c Cursor) one(ev Event, err error) ([]Event, error) {
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Cursor) projectBubble(raw []byte, scope string, row cursorKV, composer, bubble string) ([]Event, error) {
	if isBase64Wrapper(row.Value) || !jsonIsObject(row.Value) {
		ev, err := c.projectUnknown(raw, scope, row)
		if err != nil {
			return nil, err
		}
		ev.Extra["composer_id"] = composer
		ev.Extra["bubble_id"] = bubble
		return []Event{ev}, nil
	}
	obj, ok := jsonObject(row.Value)
	if !ok {
		return c.one(c.projectUnknown(raw, scope, row))
	}
	extra, err := cursorObjectExtra(obj)
	if err != nil {
		return nil, err
	}
	extra["composer_id"] = composer
	extra["bubble_id"] = bubble
	extra["scope"] = scope

	kind := cursorBubbleType(obj["type"])
	role := ""
	text := ""
	switch kind {
	case 1:
		role = ActorUser
		text = cursorVisibleText(obj)
	case 2:
		role = ActorAssistant
		text = cursorVisibleText(obj)
	}
	call, results := cursorBubbleTools(obj)
	when := cursorWhen(obj)
	offset := cursorOffset(raw, row.Key)

	var out []Event
	switch kind {
	case 1, 2:
		// Empty text with a promoted tool is the tool-only path. A
		// type 1 or 2 bubble with nothing promoted still keeps its
		// message, including one whose text is empty.
		if text != "" || (call == nil && len(results) == 0) {
			ev, err := c.emit(row.Key, EventMessage, role, when, offset, text, extra)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
	default:
		ev, err := c.emit(row.Key, EventUnknown, "", when, offset, "", extra)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if call != nil {
		ev, err := c.emit(row.Key, EventToolCall, ActorAssistant, when, offset, call.args, extra)
		if err != nil {
			return nil, err
		}
		ev.Tool = Tool{Name: strPtr(call.name), CallID: strPtr(call.callID)}
		out = append(out, ev)
	}
	for _, res := range results {
		ev, err := c.emit(row.Key, EventToolResult, ActorTool, when, offset, res.text, extra)
		if err != nil {
			return nil, err
		}
		ev.Tool = Tool{Name: strPtr(res.name), CallID: strPtr(res.callID)}
		out = append(out, ev)
	}
	return out, nil
}

func (c Cursor) projectComposerData(raw []byte, scope string, orders map[string][]string, row cursorKV) (Event, error) {
	if isBase64Wrapper(row.Value) || !jsonIsObject(row.Value) {
		return c.projectUnknown(raw, scope, row)
	}
	obj, ok := jsonObject(row.Value)
	if !ok {
		return c.projectUnknown(raw, scope, row)
	}
	noteBubbleHeaders(orders, strings.TrimPrefix(row.Key, "composerData:"), obj)
	extra, err := cursorObjectExtra(obj)
	if err != nil {
		return Event{}, err
	}
	if id := strings.TrimPrefix(row.Key, "composerData:"); id != "" {
		extra["composer_id"] = id
	}
	extra["scope"] = scope
	// name, summary, and headers stay in extra. They are not turns.
	return c.emit(row.Key, EventMeta, "", cursorWhen(obj), cursorOffset(raw, row.Key), "", extra)
}

func (c Cursor) projectMeta(raw []byte, scope string, row cursorKV) (Event, error) {
	if isBase64Wrapper(row.Value) || !jsonIsObject(row.Value) {
		return c.projectUnknown(raw, scope, row)
	}
	obj, ok := jsonObject(row.Value)
	if !ok {
		return c.projectUnknown(raw, scope, row)
	}
	extra, err := cursorObjectExtra(obj)
	if err != nil {
		return Event{}, err
	}
	extra["scope"] = scope
	return c.emit(row.Key, EventMeta, "", cursorWhen(obj), cursorOffset(raw, row.Key), "", extra)
}

func (c Cursor) projectUnknown(raw []byte, scope string, row cursorKV) (Event, error) {
	extra := map[string]any{"key": row.Key, "scope": scope}
	if !isNull(row.Value) {
		extra["value"] = opaque(row.Value)
	}
	return c.emit(row.Key, EventUnknown, "", time.Time{}, cursorOffset(raw, row.Key), "", extra)
}

func (c Cursor) emit(rawType, eventType, role string, recorded time.Time, offset int, text string, extra map[string]any) (Event, error) {
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
		SessionID:       cursorSessionID(c.NativeID),
		ParentSessionID: cursorParentID(c.ParentNativeID),
		Harness:         protocol.HarnessCursor,
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

func cursorSessionID(native string) string {
	if native == "" {
		native = "unknown"
	}
	return protocol.HarnessCursor + ":" + native
}

func cursorParentID(native string) *string {
	if native == "" {
		return nil
	}
	s := protocol.HarnessCursor + ":" + native
	return &s
}

// cursorVisibleText is the cleartext of a type 1 or 2 bubble. richText
// and thinking stay in extra. A field whose name looks sealed is never
// the text.
func cursorVisibleText(obj map[string]json.RawMessage) string {
	for _, key := range []string{"rawText", "text"} {
		if cursorOpaqueField(key) {
			continue
		}
		s, ok := jsonString(obj[key])
		if ok && s != "" {
			return s
		}
	}
	return ""
}

func cursorOpaqueField(key string) bool {
	return cursorOpaqueFieldRE.MatchString(key)
}

// cursorCall is one promoted tool_call. args is already the content_text
// string: a rawArgs or params JSON string, or the JSON of args.
type cursorCall struct {
	name   string
	callID string
	args   string
}

// cursorResult is one promoted tool_result. text is the result string.
type cursorResult struct {
	name   string
	callID string
	text   string
}

// cursorBubbleTools reads toolFormerData and toolResults. A call needs
// a non-empty string name and a non-empty string call id. The call id
// is toolCallId, then id. A numeric tool field and capabilityType are
// not a name or a call id. result on toolFormerData, when it is a
// string, is the only tool_result. Otherwise each toolResults element
// with a call id and a result string is one. An empty toolResults
// array adds nothing. A malformed call or element is left for extra.
func cursorBubbleTools(obj map[string]json.RawMessage) (*cursorCall, []cursorResult) {
	var tf map[string]json.RawMessage
	if raw, ok := obj["toolFormerData"]; ok {
		tf, _ = jsonObject(raw)
	}
	var call *cursorCall
	if tf != nil {
		name, nameOK := jsonString(tf["name"])
		callID, idOK := cursorCallID(tf)
		if nameOK && name != "" && idOK {
			call = &cursorCall{name: name, callID: callID, args: cursorToolArgs(tf)}
		}
	}
	if call != nil {
		if text, ok := jsonString(tf["result"]); ok {
			return call, []cursorResult{{name: call.name, callID: call.callID, text: text}}
		}
	}
	return call, cursorToolResults(obj["toolResults"])
}

// cursorCallID is toolCallId when that value is a non-empty string,
// otherwise id on the same rule.
func cursorCallID(obj map[string]json.RawMessage) (string, bool) {
	for _, key := range []string{"toolCallId", "id"} {
		s, ok := jsonString(obj[key])
		if ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// cursorToolArgs prefers rawArgs, then params, when the value is a
// non-empty JSON string. Otherwise it is the JSON text of args.
func cursorToolArgs(tf map[string]json.RawMessage) string {
	for _, key := range []string{"rawArgs", "params"} {
		s, ok := jsonString(tf[key])
		if ok && s != "" {
			return s
		}
	}
	raw := tf["args"]
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	return string(bytes.TrimSpace(raw))
}

func cursorToolResults(raw json.RawMessage) []cursorResult {
	if isNull(raw) {
		return nil
	}
	var elems []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil
	}
	out := make([]cursorResult, 0, len(elems))
	for _, el := range elems {
		if el == nil {
			continue
		}
		callID, ok := cursorCallID(el)
		if !ok {
			continue
		}
		text, ok := jsonString(el["result"])
		if !ok {
			continue
		}
		name, _ := jsonString(el["name"])
		out = append(out, cursorResult{name: name, callID: callID, text: text})
	}
	return out
}

func cursorBubbleType(raw json.RawMessage) int {
	n, ok := jsonUnix(raw)
	if !ok {
		return 0
	}
	return int(n)
}

// cursorWhen is the row's own timestamp, used as recorded_at. It is not
// the conversation order. fullConversationHeadersOnly is that order.
func cursorWhen(obj map[string]json.RawMessage) time.Time {
	for _, key := range []string{"createdAt", "timestamp", "created_at"} {
		if tm, ok := jsonTime(obj[key]); ok {
			return tm
		}
		n, ok := jsonUnix(obj[key])
		if !ok || n <= 0 {
			continue
		}
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n).UTC()
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Time{}
}

func jsonUnix(raw json.RawMessage) (int64, bool) {
	if isNull(raw) {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}
	return 0, false
}

func cursorObjectExtra(obj map[string]json.RawMessage) (map[string]any, error) {
	extra, err := extraFrom(obj)
	if err != nil {
		return nil, err
	}
	for k := range extra {
		if cursorAuthRow(k) {
			delete(extra, k)
		}
	}
	return extra, nil
}

func cursorAuthRow(key string) bool {
	if strings.EqualFold(key, "cursorAuth") {
		return true
	}
	if head, _, ok := strings.Cut(key, "/"); ok && strings.EqualFold(head, "cursorAuth") {
		return true
	}
	head, _, ok := strings.Cut(key, ":")
	return ok && strings.EqualFold(head, "cursorAuth")
}

func cursorUIKey(key string) bool {
	k := strings.ToLower(key)
	return strings.HasPrefix(k, "workbench.panel.aichat") || strings.HasPrefix(k, "aichat.")
}

func bubbleIDs(key string) (composer, bubble string, ok bool) {
	rest, found := strings.CutPrefix(key, "bubbleId:")
	if !found {
		return "", "", false
	}
	composer, bubble, ok = strings.Cut(rest, ":")
	if !ok || composer == "" || bubble == "" {
		return "", "", false
	}
	return composer, bubble, true
}

func noteBubbleHeaders(orders map[string][]string, composer string, obj map[string]json.RawMessage) {
	if composer == "" {
		return
	}
	ids := bubbleHeaderIDs(obj["fullConversationHeadersOnly"])
	if len(ids) == 0 {
		return
	}
	orders[composer] = ids
}

func bubbleHeaderIDs(raw json.RawMessage) []string {
	if isNull(raw) {
		return nil
	}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		out := make([]string, 0, len(asStrings))
		for _, s := range asStrings {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil
	}
	out := make([]string, 0, len(objs))
	for _, obj := range objs {
		if obj == nil {
			continue
		}
		if id, ok := jsonString(obj["bubbleId"]); ok && id != "" {
			out = append(out, id)
			continue
		}
		if id, ok := jsonString(obj["bubble_id"]); ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// orderCursorBubbles writes each composer's bubble events back into the
// slots they already occupy, in header order. Composers with no header
// list keep scan order. Bubbles the list does not name stay after the
// ones it names, in the order the scan saw them.
func orderCursorBubbles(events []Event, orders map[string][]string) []Event {
	if len(orders) == 0 {
		return events
	}
	grouped := map[string][]int{}
	for i, ev := range events {
		composer, _ := ev.Extra["composer_id"].(string)
		bubble, _ := ev.Extra["bubble_id"].(string)
		if composer == "" || bubble == "" {
			continue
		}
		if _, ok := orders[composer]; !ok {
			continue
		}
		grouped[composer] = append(grouped[composer], i)
	}
	for composer, idxs := range grouped {
		index := map[string]int{}
		for n, id := range orders[composer] {
			if _, seen := index[id]; !seen {
				index[id] = n
			}
		}
		fallback := len(orders[composer])
		ordered := make([]Event, len(idxs))
		for i, idx := range idxs {
			ordered[i] = events[idx]
		}
		sort.SliceStable(ordered, func(a, b int) bool {
			return bubbleRank(index, fallback, ordered[a]) < bubbleRank(index, fallback, ordered[b])
		})
		for i, idx := range idxs {
			events[idx] = ordered[i]
		}
	}
	return events
}

func bubbleRank(index map[string]int, fallback int, ev Event) int {
	id, _ := ev.Extra["bubble_id"].(string)
	if n, ok := index[id]; ok {
		return n
	}
	return fallback
}

func jsonIsObject(raw json.RawMessage) bool {
	_, ok := jsonObject(raw)
	return ok
}

func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if isNull(raw) || isBase64Wrapper(raw) {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

func isBase64Wrapper(raw json.RawMessage) bool {
	if isNull(raw) {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) != 1 {
		return false
	}
	_, ok := obj["base64"]
	return ok
}

func cursorOffset(raw []byte, key string) int {
	if key == "" || len(raw) == 0 {
		return 0
	}
	i := bytes.Index(raw, []byte(strconvQuote(key)))
	if i < 0 {
		return 0
	}
	return i
}
