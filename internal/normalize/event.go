package normalize

import "encoding/json"

// SchemaVersion is the normalized event shape from the capture research
// brief, section 5.1. Bump it when a field changes meaning.
const SchemaVersion = 1

const (
	ActorUser      = "user"
	ActorAssistant = "assistant"
	ActorSystem    = "system"
	ActorTool      = "tool"
	ActorHarness   = "harness"

	EventMessage    = "message"
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventUsage      = "usage"
	EventCompaction = "compaction"
	EventMeta       = "meta"
	EventError      = "error"
	EventUnknown    = "unknown"
)

// Event is one normalized row. Nullable fields are pointers so the JSON
// carries null rather than an empty string the schema does not use.
type Event struct {
	SchemaVersion   int       `json:"schema_version"`
	EventID         string    `json:"event_id"`
	SessionID       string    `json:"session_id"`
	ParentSessionID *string   `json:"parent_session_id"`
	Harness         string    `json:"harness"`
	HarnessVersion  *string   `json:"harness_version"`
	RecordedAt      string    `json:"recorded_at"`
	IngestedAt      string    `json:"ingested_at"`
	CWDHash         string    `json:"cwd_hash"`
	ProjectID       *string   `json:"project_id"`
	Git             Git       `json:"git"`
	Actor           string    `json:"actor"`
	EventType       string    `json:"event_type"`
	Role            *string   `json:"role"`
	Model           Model     `json:"model"`
	ContentText     *string   `json:"content_text"`
	ContentRef      *string   `json:"content_ref"`
	Tool            Tool      `json:"tool"`
	Usage           Usage     `json:"usage"`
	RawType         string    `json:"raw_type"`
	Redaction       Redaction `json:"redaction"`
	Extra           extraMap  `json:"extra"`
}

// Git is the checkout the session was recorded in, when the manifest
// knew it. Branch and dirty are null until a producer supplies them.
type Git struct {
	Branch *string `json:"branch"`
	Commit *string `json:"commit"`
	Dirty  *bool   `json:"dirty"`
}

// Model is the provider and model id in effect for this event.
type Model struct {
	Provider *string `json:"provider"`
	ID       *string `json:"id"`
}

// Tool identifies a call or a result. IsError is null when the row is
// not a tool result.
type Tool struct {
	Name    *string `json:"name"`
	CallID  *string `json:"call_id"`
	IsError *bool   `json:"is_error"`
}

// Usage is token counts for one row. A message leaves every field null.
// Zero is kept when the harness reported zero; a missing count stays null.
type Usage struct {
	Input      *int     `json:"input"`
	Output     *int     `json:"output"`
	CacheRead  *int     `json:"cache_read"`
	CacheWrite *int     `json:"cache_write"`
	CostUSD    *float64 `json:"cost_usd"`
}

// Redaction records whether this projection altered text. MVP copies
// text through unchanged, so status is none.
type Redaction struct {
	Status  string `json:"status"`
	Ruleset string `json:"ruleset"`
}

// extraMap marshals nil as {} so every event has an object there.
type extraMap map[string]any

func (e extraMap) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(e))
}
