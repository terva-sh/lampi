// Package protocol is capture protocol 1, the push wire shared by
// `terva-lampi serve` and the local agent.
//
// Raw bytes are content-addressed. A manifest names those bytes and the
// harness session they belong to. The client advances a watermark only
// after the server ACKs the manifest. A strict append is a tail put;
// the lake assembles it. Bytes that are not a prefix either way are a
// divergent copy and are not merged. A blob under the size cap can be
// resumed with Content-Range or with chunk_sha256s; the lake installs
// that object when the pieces assemble. A file over the cap is a list
// of chunks, each under the cap, and is not installed as one object.
package protocol

import (
	"bytes"
	"encoding/json"
	"time"
)

// Version is the capture_protocol value this tree speaks.
const Version = 1

// MaxBlobBytes is the largest single object the lake will install.
// A PUT body, a Content-Range total, and one chunk stay under this cap.
// A chunk list that concatenates to more than this is a logical file:
// the chunks are stored, and the assembled bytes are not.
const MaxBlobBytes int64 = 32 << 20

// ClockSkewWarn is how far a client clock may sit from hello's server_time
// before the client warns. Manifest mtimes stay hints. ingested_at is the
// server clock.
const ClockSkewWarn = 5 * time.Minute

const (
	// RelationHead is the current artifact for a path, including the first one.
	RelationHead = "head"
	// RelationGrownFrom is a strict byte extension of the previous head.
	RelationGrownFrom = "grown_from"
	// RelationDivergentCopy is the same logical session with bytes that are
	// not a prefix either way. The previous head stays the head.
	RelationDivergentCopy = "divergent_copy"
	// RelationUnchanged means the full digest already is the head.
	RelationUnchanged = "unchanged"
	// RelationStale means the stored head starts with the client bytes.
	// The head stays. The client keeps the local snapshot and records
	// the longer head as a cursor past that file, so the prefix is not
	// posted again.
	RelationStale = "stale"
)

// RelationOf classifies client against the stored head. Equal bytes are
// unchanged. A strict extension is grown_from. A stored file that starts
// with the client bytes is stale. Anything else is a divergent copy.
func RelationOf(prev, client []byte) string {
	if bytes.Equal(prev, client) {
		return RelationUnchanged
	}
	if len(client) > len(prev) && bytes.HasPrefix(client, prev) {
		return RelationGrownFrom
	}
	if len(prev) > len(client) && bytes.HasPrefix(prev, client) {
		return RelationStale
	}
	return RelationDivergentCopy
}

const (
	// KindTranscriptJSONL is an append-only harness transcript.
	KindTranscriptJSONL = "transcript_jsonl"
	// KindErrorsJSONL is a terva error sidecar sitting next to a transcript.
	KindErrorsJSONL = "errors_jsonl"
	// KindRaatiJSON is a terva deliberation record under raati/.
	// It is a snapshot, not an append-only transcript.
	KindRaatiJSON = "raati_json"
	// KindTasksJSON is a terva task board under tasks/, including the
	// archived generations stored in that file. It is a snapshot.
	KindTasksJSON = "tasks_json"
	// KindCursorStateJSON is a filtered export of one Cursor IDE
	// state.vscdb snapshot. It is a rewrite, not an append-only
	// transcript. A later export replaces the current artifact and
	// moves the session head. The raw database is not this kind.
	KindCursorStateJSON = "cursor_state_json"
	// KindCursorCLIStoreJSON is a filtered export of one Cursor CLI
	// store.db snapshot. It is a rewrite, and it moves the session
	// head. It is not a cursor_state_json artifact. The raw database
	// is not this kind.
	KindCursorCLIStoreJSON = "cursor_cli_store_json"

	// HarnessTerva is the reference producer. Its JSONL has a versioned
	// meta line. Normalize workers project it. Codex, OpenCode, and
	// Cursor manifests are stored; those projectors are not implemented.
	HarnessTerva = "terva"
	// HarnessClaude is Claude Code. The on-disk record shape is internal
	// to the adapter; harness_version is that adapter's pinned reader.
	// Workers project transcript_jsonl onto schema_version 1.
	HarnessClaude = "claude"
	// HarnessCodex is the Codex CLI. Rollouts are session JSONL. The
	// prompt history file is not a session.
	HarnessCodex = "codex"
	// HarnessOpenCode is OpenCode. The ingest path is a scheduled
	// `opencode export` document, or the database file when that
	// directory is empty. The WAL sidecar is not a session. The
	// projector is not implemented.
	HarnessOpenCode = "opencode"
	// HarnessCursor is the Cursor IDE. The ingest path is a filtered
	// JSON export of a state.vscdb snapshot. The live database is not
	// opened. The Cursor CLI store is a different corpus and is not
	// this harness. The projector is not implemented.
	HarnessCursor = "cursor"
	// HarnessCursorCLI is the Cursor CLI. The ingest path is a filtered
	// JSON export of a store.db snapshot. It does not share sessions
	// or watermarks with HarnessCursor, and it does not assume the
	// CLI store matches IDE state. The projector is not implemented.
	HarnessCursorCLI = "cursor-cli"

	// RedactionUnscanned means no ruleset looked at the bytes.
	// Do not report "scanned" until a redactor actually runs.
	RedactionUnscanned = "unscanned"
	// RedactionScanned means ruleset v1 ran and the bytes were eligible
	// to leave the machine. Hits is zero in that case.
	RedactionScanned = "scanned"
	// RedactionOverride means ruleset v1 found hits and the operator
	// set the explicit upload override. The hit count stays on the artifact.
	RedactionOverride = "override"
)

// HelloResponse is the body of POST /v1/hello.
type HelloResponse struct {
	ServerTime       time.Time `json:"server_time"`
	ProtocolVersions []int     `json:"protocol_versions"`
	MaxBlobBytes     int64     `json:"max_blob_bytes"`
}

// BlobCheckResponse is the body of POST /v1/blobs/check.
// The request body is a JSON array of lowercase sha256 hex digests,
// not an object.
type BlobCheckResponse struct {
	Missing []string `json:"missing"`
}

// PutResponse is the body of PUT /v1/blobs/{sha256}.
// Exists is true when that digest was already stored. The put is a no-op
// in that case: identical bytes are not written twice.
// Complete is false while a Content-Range upload still has a gap.
// A finished object, including one that already existed, is complete.
type PutResponse struct {
	Exists   bool   `json:"exists"`
	SHA256   string `json:"sha256"`
	Complete bool   `json:"complete"`
}

// Manifest is the body of POST /v1/manifests.
type Manifest struct {
	CaptureProtocol int        `json:"capture_protocol"`
	MachineID       string     `json:"machine_id"`
	Harness         string     `json:"harness"`
	HarnessVersion  string     `json:"harness_version"`
	NativeSessionID string     `json:"native_session_id"`
	Project         Project    `json:"project"`
	Artifacts       []Artifact `json:"artifacts"`
	Lineage         Lineage    `json:"lineage"`
}

// Project is where the session was recorded. CWDHash is a property of the
// absolute path string on that machine. It is not a project id across hosts.
// GitCommit is HEAD. GitRoot is the first parentless commit on the
// first-parent chain from HEAD. ProjectID is ProjectLinkID of GitRemote
// and GitRoot. The lake recomputes it on ingest.
type Project struct {
	CWD       string `json:"cwd"`
	CWDHash   string `json:"cwd_hash"`
	GitRemote string `json:"git_remote"`
	GitCommit string `json:"git_commit"`
	GitRoot   string `json:"git_root,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// Artifact is one object in the CAS plus the client's watermark hint.
// ChunkSHA256s is null when the file is a single blob. ChunkLengths is
// parallel to that list and sums to Size. Lengths are required when the
// concatenation is longer than MaxBlobBytes, because that file is not
// installed as one object.
type Artifact struct {
	Kind              string    `json:"kind"`
	RelPath           string    `json:"relpath"`
	Size              int64     `json:"size"`
	MTime             time.Time `json:"mtime"`
	SHA256            string    `json:"sha256"`
	ChunkSHA256s      []string  `json:"chunk_sha256s"`
	ChunkLengths      []int64   `json:"chunk_lengths,omitempty"`
	ByteWatermarkPrev int64     `json:"byte_watermark_prev"`
	TailSHA256        string    `json:"tail_sha256"`
	Redaction         Redaction `json:"redaction"`
}

// Redaction records which scan ran before the bytes were eligible to leave
// the machine. Hits is the number of findings, not the findings themselves.
type Redaction struct {
	Status  string `json:"status"`
	Ruleset string `json:"ruleset"`
	Hits    int    `json:"hits"`
}

// Lineage is the harness-native fork edge. ForkPoint stays raw JSON because
// producers disagree on the type: the protocol sketch uses null, and terva
// stores an index.
type Lineage struct {
	ParentNativeID *string         `json:"parent_native_id"`
	ForkPoint      json.RawMessage `json:"fork_point"`
}

// ManifestAck is the 200 body of POST /v1/manifests.
// A client may advance its watermark only after it sees this.
// internal/watermark.Commit refuses to store a mark without a session
// uid from this ACK. terva-lampi sync commits that mark only after this
// ACK, and leaves the cursor where it was when the POST fails.
//
// Relation is how this manifest met the stored head. HeadSHA256 and
// HeadSize are the lake head after the call, which on RelationStale is
// the longer stored object, not the shorter client file.
type ManifestAck struct {
	SessionUID  string   `json:"session_uid"`
	ArtifactIDs []string `json:"artifact_ids"`
	HeadSHA256  string   `json:"head_sha256"`
	HeadSize    int64    `json:"head_size"`
	Relation    string   `json:"relation"`
}

// StatsResponse is the body of GET /v1/stats.
// These are catalog row counts. healthz stays free of them so a process
// probe does not need the device token and does not learn what is stored.
type StatsResponse struct {
	Sessions  int `json:"sessions"`
	Artifacts int `json:"artifacts"`
	Machines  int `json:"machines"`
}

// DivergentCopy is one catalog artifact stored as divergent_copy.
// HeadSHA256 is the session head that stayed. Machines posted this
// digest. HeadMachines posted the head digest for the same path.
type DivergentCopy struct {
	SessionUID      string   `json:"session_uid"`
	ArtifactID      string   `json:"artifact_id"`
	Harness         string   `json:"harness"`
	NativeSessionID string   `json:"native_session_id"`
	Kind            string   `json:"kind"`
	RelPath         string   `json:"relpath"`
	SHA256          string   `json:"sha256"`
	Size            int64    `json:"size"`
	HeadSHA256      string   `json:"head_sha256"`
	HeadSize        int64    `json:"head_size"`
	Machines        []string `json:"machines"`
	HeadMachines    []string `json:"head_machines"`
}

// ConflictsResponse is the body of GET /v1/conflicts.
// Conflicts is empty when the catalog has no divergent_copy rows.
// The route uses the same bearer check as the other /v1 routes.
// Listing does not merge the copies or move the head.
type ConflictsResponse struct {
	Conflicts []DivergentCopy `json:"conflicts"`
}

// ErrorBody is the JSON error shape every non-2xx response uses.
type ErrorBody struct {
	Error   string   `json:"error"`
	Missing []string `json:"missing,omitempty"`
}

// ValidDigest reports whether s is a lowercase sha256 hex digest.
func ValidDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
