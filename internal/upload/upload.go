// Package upload is the one-shot push shared by terva-lampi sync and the
// long-running agent.
//
// The order is fixed. Allowlist, then ruleset v2, then watermark.Plan,
// then the outbox, then the network. A manifest ACK is what commits the
// watermark and acks the outbox. A file whose bytes match the stored
// watermark is checked and not PUT again. Plan KindTail PUTs only the
// suffix: byte_watermark_prev is the stored offset, tail_sha256 is the
// suffix, and sha256 is the full file. The lake assembles that tail onto
// the stored prefix. A tail the lake rejects is sent again as the whole
// file. Hello's server_time is compared to the local clock; a skew past
// protocol.ClockSkewWarn is a warning on Result, and the push still runs.
//
// A finished run rewrites last_sync.json in the state directory. That
// includes a pass that refused or quarantined every session. A lake
// error leaves the previous stamp, so status does not report the failed
// attempt as the last sync.
//
// PieceBytes and ChunkBytes opt a put into a resumable form. Zero keeps
// the single body. The server installs the digest when the pieces assemble
// and the result fits under the blob cap. A body already over that cap is
// split into chunks of at most the cap (or ChunkBytes, when that is
// smaller). Those chunks are stored. The concatenation is not.
package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/claude"
	"terva.sh/lampi/internal/adapter/codex"
	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
	"terva.sh/lampi/internal/watermark"
)

// Options selects the lake, the local terva home, and the off-box gate.
// Projects zero value denies every project. UploadHits is the explicit
// override that sends bytes ruleset v2 flagged.
type Options struct {
	ServerURL string
	Token     string
	TervaHome string
	// ClaudeHome is the Claude Code config directory. Empty skips it.
	ClaudeHome string
	// CodexHome is the Codex CLI state directory. Empty skips it.
	CodexHome string
	// OpenCodeHome is the OpenCode data directory. Empty skips it.
	OpenCodeHome string
	// CursorHome is the Cursor IDE user-data directory. Empty skips it.
	CursorHome string
	// CursorCLIHome is the Cursor CLI config directory. Empty skips it.
	// It is a different corpus from CursorHome. The two are not assumed
	// to match, and they do not share watermarks.
	CursorCLIHome string
	MachineID     string
	StateDir      string
	Client        *http.Client
	Projects      config.Projects
	UploadHits    bool
	// Now is the client clock for the hello skew check. Nil uses time.Now.
	Now func() time.Time
	// PieceBytes sends the body as Content-Range slices of this size.
	// Zero sends one body. A slice larger than the body is one body.
	PieceBytes int64
	// ChunkBytes splits the body into CAS objects of this size and PUTs
	// the chunk digests. Zero sends one body. The manifest lists the
	// chunks when the artifact is the whole file.
	ChunkBytes int64
}

// Result counts what this run did. Sessions are the uids the server ACKed,
// in manifest order. Refused and Quarantined count sessions that did not
// leave the machine.
type Result struct {
	Checked     int
	Missing     int
	Uploaded    int
	Manifests   int
	Refused     int
	Quarantined int
	Sessions    []string
	// Warning is set when hello's server_time disagrees with the client
	// clock by more than protocol.ClockSkewWarn. The push still runs.
	Warning string
}

// Rejected is one or more sessions that stayed on the machine.
// Approved sessions in the same run are still uploaded.
type Rejected struct {
	Reasons []string
}

func (e *Rejected) Error() string {
	var b strings.Builder
	b.WriteString("upload: refused off-box raw:")
	for _, r := range e.Reasons {
		b.WriteByte('\n')
		b.WriteString(r)
	}
	return b.String()
}

// Sync pushes allowlisted session files. An empty home skips that
// harness, including terva. The agent leaves a home empty when
// config says that harness is disabled, so this run does not read
// it, does not move its watermark, and does not put its bytes.
func Sync(ctx context.Context, opt Options) (Result, error) {
	if opt.ServerURL == "" {
		return Result{}, fmt.Errorf("upload: server URL is empty")
	}
	if opt.MachineID == "" {
		return Result{}, fmt.Errorf("upload: machine_id is empty")
	}
	bundles, err := bundlesFor(opt)
	if err != nil {
		return Result{}, err
	}
	defer cleanupBundles(bundles)
	n := 0
	for _, b := range bundles {
		n += len(b.Manifests)
	}
	if n == 0 {
		return finish(opt, Result{}, nil)
	}
	if opt.StateDir == "" {
		return Result{}, fmt.Errorf("upload: state dir is empty")
	}

	q, err := outbox.Open(outbox.File(opt.StateDir))
	if err != nil {
		return Result{}, err
	}
	defer q.Close()
	wm, err := watermark.Open(watermark.File(opt.StateDir))
	if err != nil {
		return Result{}, err
	}
	defer wm.Close()

	work, res, err := prepare(ctx, opt, wm, q, bundles)
	var rejected error
	if r, ok := err.(*Rejected); ok {
		rejected = r
		err = nil
	}
	if err != nil {
		return res, err
	}
	if len(work) == 0 {
		return finish(opt, res, rejected)
	}

	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	hello, err := postHello(ctx, client, opt)
	if err != nil {
		return res, err
	}
	res.Warning = clockWarning(opt.now(), hello.ServerTime)
	supported := false
	for _, v := range hello.ProtocolVersions {
		if v == protocol.Version {
			supported = true
		}
	}
	if !supported {
		return res, fmt.Errorf("upload: server does not speak capture protocol %d", protocol.Version)
	}

	// A file past the lake's cap cannot travel as a tail. The tail form
	// would ask the lake to install the assembled object. Widen first,
	// then split the whole file.
	maxBlob := hello.MaxBlobBytes
	if maxBlob <= 0 {
		maxBlob = protocol.MaxBlobBytes
	}
	for i := range work {
		if sessionOverCap(&work[i], maxBlob) {
			widenToFullFile(&work[i])
		}
	}
	bodies := map[string][]byte{}
	var arts []protocol.Artifact
	for _, w := range work {
		for d, b := range w.bodies {
			bodies[d] = b
		}
		arts = append(arts, w.manifest.Artifacts...)
	}
	lists := map[string]chunkPlan{}
	if err := uploadDigests(ctx, client, opt, maxBlob, &res, arts, bodies, lists); err != nil {
		return res, err
	}
	for i := range work {
		w := &work[i]
		if err := requireScanned(w.manifest); err != nil {
			return res, err
		}
		applyChunkLists(&w.manifest, lists)
		ack, err := postManifest(ctx, client, opt, w.manifest)
		if prefixMismatch(err) && widenToFullFile(w) {
			if err := uploadDigests(ctx, client, opt, maxBlob, &res, w.manifest.Artifacts, w.bodies, lists); err != nil {
				return res, err
			}
			applyChunkLists(&w.manifest, lists)
			ack, err = postManifest(ctx, client, opt, w.manifest)
		}
		if err != nil {
			return res, err
		}
		res.Manifests++
		res.Sessions = append(res.Sessions, ack.SessionUID)
		if err := commitAck(ctx, opt, wm, q, w.root, w.manifest, ack); err != nil {
			return res, err
		}
	}
	return finish(opt, res, rejected)
}

// LastSync is the stamp Sync rewrites when a run finishes.
type LastSync struct {
	At          time.Time `json:"at"`
	Server      string    `json:"server"`
	Checked     int       `json:"checked"`
	Missing     int       `json:"missing"`
	Uploaded    int       `json:"uploaded"`
	Manifests   int       `json:"manifests"`
	Refused     int       `json:"refused"`
	Quarantined int       `json:"quarantined"`
}

// LastSyncFile is the stamp path inside a lampi state directory.
func LastSyncFile(stateDir string) string {
	return filepath.Join(stateDir, "last_sync.json")
}

// ReadLastSync loads the stamp. A missing file is (zero, false, nil).
func ReadLastSync(stateDir string) (LastSync, bool, error) {
	b, err := os.ReadFile(LastSyncFile(stateDir))
	if os.IsNotExist(err) {
		return LastSync{}, false, nil
	}
	if err != nil {
		return LastSync{}, false, err
	}
	var st LastSync
	if err := json.Unmarshal(b, &st); err != nil {
		return LastSync{}, false, fmt.Errorf("upload: last sync: %w", err)
	}
	if st.At.IsZero() {
		return LastSync{}, false, fmt.Errorf("upload: last sync has no timestamp")
	}
	st.At = st.At.UTC()
	return st, true, nil
}

// finish records a completed pass. A *Rejected is complete: the allowlist
// and the scan ran, and any approved session was pushed. A lake error is
// not complete, and the previous stamp stays.
func finish(opt Options, res Result, err error) (Result, error) {
	if !runFinished(err) {
		return res, err
	}
	if stampErr := saveLastSync(opt, res); stampErr != nil {
		if err != nil {
			return res, errors.Join(err, stampErr)
		}
		return res, stampErr
	}
	return res, err
}

func runFinished(err error) bool {
	if err == nil {
		return true
	}
	var rejected *Rejected
	return errors.As(err, &rejected)
}

func saveLastSync(opt Options, res Result) error {
	if opt.StateDir == "" {
		return nil
	}
	if err := os.MkdirAll(opt.StateDir, 0o700); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	raw, err := json.MarshalIndent(LastSync{
		At:          time.Now().UTC(),
		Server:      opt.ServerURL,
		Checked:     res.Checked,
		Missing:     res.Missing,
		Uploaded:    res.Uploaded,
		Manifests:   res.Manifests,
		Refused:     res.Refused,
		Quarantined: res.Quarantined,
	}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	// Rename a complete file over the stamp. A crash mid-write leaves the
	// previous document in place; readers never see a truncated one.
	tmp, err := os.CreateTemp(opt.StateDir, ".last-sync-*")
	if err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	if err := os.Rename(tmpName, LastSyncFile(opt.StateDir)); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	tmpName = ""
	return nil
}

func (opt Options) now() time.Time {
	if opt.Now != nil {
		return opt.Now()
	}
	return time.Now()
}

func clockWarning(client, server time.Time) string {
	if server.IsZero() {
		return ""
	}
	skew := client.Sub(server)
	if skew < 0 {
		skew = -skew
	}
	if skew <= protocol.ClockSkewWarn {
		return ""
	}
	return fmt.Sprintf("upload: clock skew %s from server_time %s", skew.Truncate(time.Second), server.UTC().Format(time.RFC3339))
}

func prefixMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "prefix mismatch")
}

// chunkPlan is one logical file's ordered chunks.
type chunkPlan struct {
	Digests []string
	Lengths []int64
}

func sessionOverCap(w *prepared, max int64) bool {
	for _, a := range w.manifest.Artifacts {
		if a.Size > max {
			return true
		}
	}
	return false
}

func uploadDigests(ctx context.Context, client *http.Client, opt Options, max int64, res *Result, arts []protocol.Artifact, bodies map[string][]byte, lists map[string]chunkPlan) error {
	if max <= 0 {
		max = protocol.MaxBlobBytes
	}
	seen := map[string]bool{}
	var digests []string
	type splitJob struct {
		rel    string
		digest string
		body   []byte
	}
	var splits []splitJob
	for _, a := range arts {
		d := putDigest(a)
		if seen[d] {
			continue
		}
		seen[d] = true
		b, ok := bodies[d]
		if !ok {
			return fmt.Errorf("upload: %s: no bytes for %s", a.RelPath, d)
		}
		if int64(len(b)) > max {
			if a.ByteWatermarkPrev != 0 {
				return fmt.Errorf("upload: %s: a file over %d bytes is sent whole, not as a tail", a.RelPath, max)
			}
			splits = append(splits, splitJob{rel: a.RelPath, digest: d, body: b})
			continue
		}
		digests = append(digests, d)
	}
	if len(digests) > 0 {
		res.Checked += len(digests)
		missing, err := postCheck(ctx, client, opt, digests)
		if err != nil {
			return err
		}
		res.Missing += len(missing)
		for _, d := range missing {
			put, err := putBlobResume(ctx, client, opt, d, bodies[d], lists)
			if err != nil {
				return err
			}
			if !put.Exists {
				res.Uploaded++
			}
		}
	}
	for _, job := range splits {
		if err := uploadSplit(ctx, client, opt, max, res, job.rel, job.digest, job.body, lists); err != nil {
			return err
		}
	}
	return nil
}

// uploadSplit PUTs chunks of at most max and records them on lists.
// The logical digest is not PUT. The lake will not install it.
func uploadSplit(ctx context.Context, client *http.Client, opt Options, max int64, res *Result, rel, digest string, body []byte, lists map[string]chunkPlan) error {
	chunk := max
	if opt.ChunkBytes > 0 && opt.ChunkBytes < chunk {
		chunk = opt.ChunkBytes
	}
	if chunk <= 0 || chunk > int64(^uint(0)>>1) {
		return fmt.Errorf("upload: %s: chunk size %d is not usable", rel, chunk)
	}
	parts, lengths, chunkBody := splitBytes(body, int(chunk))
	if lists != nil {
		lists[digest] = chunkPlan{Digests: parts, Lengths: lengths}
	}
	res.Checked += len(parts)
	missing, err := postCheck(ctx, client, opt, parts)
	if err != nil {
		return err
	}
	res.Missing += len(missing)
	for _, d := range missing {
		put, err := putBlob(ctx, client, opt, d, chunkBody[d])
		if err != nil {
			return fmt.Errorf("upload: %s: %w", rel, err)
		}
		if !put.Exists {
			res.Uploaded++
		}
	}
	return nil
}

func splitBytes(body []byte, n int) (parts []string, lengths []int64, blobs map[string][]byte) {
	blobs = map[string][]byte{}
	if n <= 0 {
		n = len(body)
	}
	for start := 0; start < len(body); start += n {
		end := start + n
		if end > len(body) {
			end = len(body)
		}
		piece := body[start:end]
		sum := sha256.Sum256(piece)
		d := hex.EncodeToString(sum[:])
		parts = append(parts, d)
		lengths = append(lengths, int64(len(piece)))
		if _, ok := blobs[d]; !ok {
			blobs[d] = append([]byte(nil), piece...)
		}
	}
	return parts, lengths, blobs
}

func applyChunkLists(m *protocol.Manifest, lists map[string]chunkPlan) {
	if len(lists) == 0 {
		return
	}
	for i, a := range m.Artifacts {
		if a.ByteWatermarkPrev != 0 {
			continue
		}
		plan, ok := lists[a.SHA256]
		if !ok {
			continue
		}
		m.Artifacts[i].ChunkSHA256s = plan.Digests
		m.Artifacts[i].ChunkLengths = plan.Lengths
	}
}

func putBlobResume(ctx context.Context, client *http.Client, opt Options, digest string, body []byte, lists map[string]chunkPlan) (protocol.PutResponse, error) {
	if opt.ChunkBytes > 0 && int64(len(body)) > opt.ChunkBytes {
		return putChunked(ctx, client, opt, digest, body, lists)
	}
	if opt.PieceBytes > 0 && int64(len(body)) > opt.PieceBytes {
		return putRanged(ctx, client, opt, digest, body)
	}
	return putBlob(ctx, client, opt, digest, body)
}

func putRanged(ctx context.Context, client *http.Client, opt Options, digest string, body []byte) (protocol.PutResponse, error) {
	var last protocol.PutResponse
	size := int64(len(body))
	for start := int64(0); start < size; {
		end := start + opt.PieceBytes - 1
		if end >= size {
			end = size - 1
		}
		header := fmt.Sprintf("bytes %d-%d/%d", start, end, size)
		err := doRequest(ctx, client, opt, http.MethodPut, "/v1/blobs/"+digest, body[start:end+1], "application/octet-stream", map[string]string{
			"Content-Range": header,
		}, &last)
		if err != nil {
			return last, err
		}
		if last.Complete {
			return last, nil
		}
		start = end + 1
	}
	if !last.Complete {
		return last, fmt.Errorf("upload: %s: range put did not assemble", digest)
	}
	return last, nil
}

func putChunked(ctx context.Context, client *http.Client, opt Options, digest string, body []byte, lists map[string]chunkPlan) (protocol.PutResponse, error) {
	n := int(opt.ChunkBytes)
	if n <= 0 || int64(n) > protocol.MaxBlobBytes {
		n = int(protocol.MaxBlobBytes)
	}
	parts, lengths, chunkBody := splitBytes(body, n)
	if lists != nil {
		lists[digest] = chunkPlan{Digests: parts, Lengths: lengths}
	}
	missing, err := postCheck(ctx, client, opt, parts)
	if err != nil {
		return protocol.PutResponse{}, err
	}
	for _, d := range missing {
		if _, err := putBlob(ctx, client, opt, d, chunkBody[d]); err != nil {
			return protocol.PutResponse{}, err
		}
	}
	raw, err := json.Marshal(struct {
		ChunkSHA256s []string `json:"chunk_sha256s"`
	}{ChunkSHA256s: parts})
	if err != nil {
		return protocol.PutResponse{}, err
	}
	var out protocol.PutResponse
	err = doRequest(ctx, client, opt, http.MethodPut, "/v1/blobs/"+digest, raw, "application/json", nil, &out)
	return out, err
}

func putDigest(a protocol.Artifact) string {
	if a.ByteWatermarkPrev > 0 {
		return a.TailSHA256
	}
	return a.SHA256
}

func requireScanned(m protocol.Manifest) error {
	for _, a := range m.Artifacts {
		if a.Redaction.Ruleset != redact.RulesetV2 {
			return fmt.Errorf("upload: %s: redaction ruleset %s did not run", a.RelPath, redact.RulesetV2)
		}
		switch a.Redaction.Status {
		case protocol.RedactionScanned:
			if a.Redaction.Hits != 0 {
				return fmt.Errorf("upload: %s: redaction hit would have been uploaded", a.RelPath)
			}
		case protocol.RedactionOverride:
			if a.Redaction.Hits < 1 {
				return fmt.Errorf("upload: %s: override without a redaction hit", a.RelPath)
			}
		default:
			return fmt.Errorf("upload: %s: redaction ruleset %s did not run", a.RelPath, redact.RulesetV2)
		}
	}
	return nil
}

func bundlesFor(opt Options) ([]adapter.Bundle, error) {
	var out []adapter.Bundle
	add := func(b adapter.Bundle, err error) error {
		if err != nil {
			cleanupBundles(out)
			if b.Cleanup != nil {
				b.Cleanup()
			}
			return err
		}
		out = append(out, b)
		return nil
	}
	if opt.TervaHome != "" {
		b, err := terva.Manifests(opt.TervaHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	if opt.ClaudeHome != "" {
		b, err := claude.Manifests(opt.ClaudeHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	if opt.CodexHome != "" {
		b, err := codex.Manifests(opt.CodexHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	if opt.OpenCodeHome != "" {
		b, err := opencode.Manifests(opt.OpenCodeHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	if opt.CursorHome != "" {
		b, err := cursor.Manifests(opt.CursorHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	if opt.CursorCLIHome != "" {
		b, err := cursorcli.Manifests(opt.CursorCLIHome, opt.MachineID)
		if err := add(b, err); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func cleanupBundles(bundles []adapter.Bundle) {
	for _, b := range bundles {
		if b.Cleanup != nil {
			b.Cleanup()
		}
	}
}

func commitAck(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, root string, m protocol.Manifest, ack protocol.ManifestAck) error {
	for _, a := range m.Artifacts {
		size := a.Size
		sum := a.SHA256
		offset := a.Size
		// The lake head is a strict extension of this file. Keep the
		// local size and hash, and put the cursor at the longer head.
		// Plan then matches this file and does not choose a replace.
		// Copying the head hash onto the shorter file would: the next
		// plan sees a truncate and posts the prefix forever.
		if ack.Relation == protocol.RelationStale &&
			a.Kind == protocol.KindTranscriptJSONL &&
			ack.HeadSize > a.Size &&
			protocol.ValidDigest(ack.HeadSHA256) {
			offset = ack.HeadSize
		}
		mark := watermark.Mark{
			MachineID: opt.MachineID,
			Harness:   m.Harness,
			Root:      root,
			RelPath:   a.RelPath,
			Size:      size,
			ModTime:   a.MTime,
			SHA256:    sum,
			Offset:    offset,
		}
		if err := wm.Commit(ctx, mark, ack); err != nil {
			return err
		}
		if err := q.Ack(ctx, outbox.Item{Identity: blobIdentity(opt.MachineID, root, a.RelPath)}); err != nil {
			return err
		}
	}
	return q.Ack(ctx, outbox.Item{Identity: manifestIdentity(opt.MachineID, m.Harness, m.NativeSessionID)})
}

var (
	verMu   sync.Mutex
	lastVer int64
)

func nextVersion() int64 {
	verMu.Lock()
	defer verMu.Unlock()
	v := time.Now().UnixNano()
	if v <= lastVer {
		v = lastVer + 1
	}
	lastVer = v
	return v
}

func postHello(ctx context.Context, client *http.Client, opt Options) (protocol.HelloResponse, error) {
	var out protocol.HelloResponse
	err := doJSON(ctx, client, opt, http.MethodPost, "/v1/hello", []byte("{}"), &out)
	return out, err
}

func postCheck(ctx context.Context, client *http.Client, opt Options, digests []string) ([]string, error) {
	raw, err := json.Marshal(digests)
	if err != nil {
		return nil, err
	}
	var out protocol.BlobCheckResponse
	if err := doJSON(ctx, client, opt, http.MethodPost, "/v1/blobs/check", raw, &out); err != nil {
		return nil, err
	}
	if out.Missing == nil {
		out.Missing = []string{}
	}
	return out.Missing, nil
}

func putBlob(ctx context.Context, client *http.Client, opt Options, digest string, body []byte) (protocol.PutResponse, error) {
	var out protocol.PutResponse
	err := doJSON(ctx, client, opt, http.MethodPut, "/v1/blobs/"+digest, body, &out)
	return out, err
}

func postManifest(ctx context.Context, client *http.Client, opt Options, m protocol.Manifest) (protocol.ManifestAck, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return protocol.ManifestAck{}, err
	}
	var out protocol.ManifestAck
	err = doJSON(ctx, client, opt, http.MethodPost, "/v1/manifests", raw, &out)
	return out, err
}

func doJSON(ctx context.Context, client *http.Client, opt Options, method, p string, body []byte, dest any) error {
	return doRequest(ctx, client, opt, method, p, body, "", nil, dest)
}

func doRequest(ctx context.Context, client *http.Client, opt Options, method, p string, body []byte, contentType string, extra map[string]string, dest any) error {
	u, err := endpoint(opt.ServerURL, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	if opt.Token != "" {
		req.Header.Set("Authorization", "Bearer "+opt.Token)
	}
	req.Header.Set("User-Agent", "terva-lampi")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %s %s: %w", method, p, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload: %s %s: %s: %s", method, p, resp.Status, strings.TrimSpace(string(respBody)))
	}
	if dest == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("upload: %s %s: %w", method, p, err)
	}
	return nil
}

func endpoint(base, p string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("upload: server URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("upload: server URL must be http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + p
	return u.String(), nil
}
