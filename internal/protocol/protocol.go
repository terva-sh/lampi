// Package protocol is capture protocol 1, the push wire shared by
// `terva-lampi serve` and the local agent.
//
// Raw bytes are content-addressed. A manifest names those bytes and the
// harness session they belong to. The client advances a watermark only
// after the server ACKs the manifest. Chunk assembly, append-only tail
// merge, and divergent-copy handling are specified in docs/protocol.md
// and are not implemented in this scaffold.
package protocol

import (
	"encoding/json"
	"time"
)

// Version is the capture_protocol value this tree speaks.
const Version = 1

// MaxBlobBytes is the largest single object a client may PUT.
// Transcripts above this need the chunked upload path, which is not built.
const MaxBlobBytes int64 = 32 << 20

const (
	// KindTranscriptJSONL is an append-only harness transcript.
	KindTranscriptJSONL = "transcript_jsonl"
	// KindErrorsJSONL is a terva error sidecar sitting next to a transcript.
	KindErrorsJSONL = "errors_jsonl"

	// HarnessTerva is the only producer this scaffold knows how to read.
	HarnessTerva = "terva"

	// RedactionUnscanned means no ruleset looked at the bytes.
	// Do not report "scanned" until a redactor actually runs.
	RedactionUnscanned = "unscanned"
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
type PutResponse struct {
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256"`
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
type Project struct {
	CWD       string `json:"cwd"`
	CWDHash   string `json:"cwd_hash"`
	GitRemote string `json:"git_remote"`
	GitCommit string `json:"git_commit"`
}

// Artifact is one object in the CAS plus the client's watermark hint.
// ChunkSHA256s is null when the file is a single blob.
type Artifact struct {
	Kind              string    `json:"kind"`
	RelPath           string    `json:"relpath"`
	Size              int64     `json:"size"`
	MTime             time.Time `json:"mtime"`
	SHA256            string    `json:"sha256"`
	ChunkSHA256s      []string  `json:"chunk_sha256s"`
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
// A client may advance its watermark only after it sees this. This scaffold
// does not store watermarks yet; the ACK is still the contract.
type ManifestAck struct {
	SessionUID  string   `json:"session_uid"`
	ArtifactIDs []string `json:"artifact_ids"`
	HeadSHA256  string   `json:"head_sha256"`
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
