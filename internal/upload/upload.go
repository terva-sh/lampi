// Package upload is the one-shot push shared by terva-lampi sync and the
// long-running agent.
//
// The order is fixed. Hello, then allowlist, ruleset v2, watermark.Plan,
// and the outbox, then the blobs and manifests. Hello comes before the
// files are read and scanned, so a lake that is down costs only
// discovery. A manifest ACK is what commits the
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
// attempt as the last sync. last_attempt.json records every run and
// the most recent error.
//
// A bearer token goes over https, or over http only to a loopback host.
// No timeout covers a whole request. A request that moves no bytes for
// StallTimeout is cancelled. blobs/check carries at most 1000 digests.
//
// PieceBytes and ChunkBytes opt a put into a resumable form. Zero keeps
// the single body. A piece the lake acknowledged is not sent again by a
// later attempt in the same process. The server installs the digest
// when the pieces assemble and the result fits under the blob cap. A body already over that cap is
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
	"slices"
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
	// StallTimeout cancels a request that moves no bytes for this long.
	// Zero is DefaultStallTimeout. No limit covers a whole request.
	StallTimeout time.Duration
	// PieceBytes sends the body as Content-Range slices of this size.
	// Zero sends one body. A slice larger than the body is one body.
	// sync and the agent use DefaultPieceBytes.
	PieceBytes int64
	// ChunkBytes splits the body into CAS objects of this size and PUTs
	// the chunk digests. Zero sends one body. The manifest lists the
	// chunks when the artifact is the whole file.
	ChunkBytes int64

	// allowed is the digests quarantine allow acknowledged, read from
	// StateDir when the run starts. A file with exactly those bytes
	// uploads with an override stamp, the way UploadHits does.
	allowed map[string]bool
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
	// Skipped names each file or harness left out of this run because
	// it could not be read. The rest of the run went ahead.
	Skipped []string
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
// Every run, finished or not, rewrites last_attempt.json. A run cut
// short by ctx is a shutdown, not a failure, and is not recorded.
func Sync(ctx context.Context, opt Options) (Result, error) {
	res, err := syncOnce(ctx, opt)
	if err == nil || ctx.Err() == nil {
		recordAttempt(opt.StateDir, opt.now(), err)
	}
	return res, err
}

func syncOnce(ctx context.Context, opt Options) (Result, error) {
	if opt.ServerURL == "" {
		return Result{}, fmt.Errorf("upload: server URL is empty")
	}
	if opt.MachineID == "" {
		return Result{}, fmt.Errorf("upload: machine_id is empty")
	}
	if err := CheckToken(opt.ServerURL, opt.Token); err != nil {
		return Result{}, err
	}
	if opt.StateDir != "" {
		allowed, err := redact.AllowedDigests(opt.StateDir)
		if err != nil {
			return Result{}, err
		}
		opt.allowed = allowed
	}
	bundles, skipped := bundlesFor(opt)
	defer cleanupBundles(bundles)
	n := 0
	for _, b := range bundles {
		n += len(b.Manifests)
	}
	if n == 0 {
		return finish(opt, Result{Skipped: skipped}, nil)
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

	// Hello runs before the scan. A lake that is down or refuses the
	// token fails the pass before every file is read and scanned again.
	// A pass the allowlist refuses whole never needs the lake, and it
	// still reports the refusal when the lake is down.
	client := opt.Client
	if client == nil {
		client = defaultClient
	}
	var hello protocol.HelloResponse
	warning := ""
	if anyPermitted(opt, bundles) {
		hello, err = postHello(ctx, client, opt)
		if err != nil {
			return Result{}, err
		}
		warning = clockWarning(opt.now(), hello.ServerTime)
		if !slices.Contains(hello.ProtocolVersions, protocol.Version) {
			return Result{Warning: warning}, fmt.Errorf("upload: server does not speak capture protocol %d", protocol.Version)
		}
	}

	work, res, err := prepare(ctx, opt, wm, q, bundles)
	res.Warning = warning
	res.Skipped = append(skipped, res.Skipped...)
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

// anyPermitted reports whether the allowlist lets any session through.
// prepare applies the same rule to each one.
func anyPermitted(opt Options, bundles []adapter.Bundle) bool {
	for _, b := range bundles {
		for _, m := range b.Manifests {
			if opt.Projects.Permitted(projectID(m)) {
				return true
			}
		}
	}
	return false
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
	if err := writeStateFile(opt.StateDir, LastSyncFile(opt.StateDir), raw); err != nil {
		return fmt.Errorf("upload: last sync: %w", err)
	}
	return nil
}

func (opt Options) now() time.Time {
	if opt.Now != nil {
		return opt.Now()
	}
	return time.Now()
}

func (opt Options) stallTimeout() time.Duration {
	if opt.StallTimeout > 0 {
		return opt.StallTimeout
	}
	return DefaultStallTimeout
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
		put, err := putBody(ctx, client, opt, d, chunkBody[d])
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
	return putBody(ctx, client, opt, digest, body)
}

// putBody is one object: Content-Range pieces when the body is over
// PieceBytes, otherwise a single PUT.
func putBody(ctx context.Context, client *http.Client, opt Options, digest string, body []byte) (protocol.PutResponse, error) {
	if opt.PieceBytes > 0 && int64(len(body)) > opt.PieceBytes {
		return putRanged(ctx, client, opt, digest, body)
	}
	return putBlob(ctx, client, opt, digest, body)
}

// rangeResume is, per lake and digest, the offset after the last piece
// the lake acknowledged. The lake keeps those spans under partial/ and
// has no call that lists them, so the next attempt in this process
// starts here instead of at byte 0.
var rangeResume = struct {
	mu   sync.Mutex
	next map[string]int64
}{next: map[string]int64{}}

func resumeKey(opt Options, digest string) string {
	return opt.ServerURL + " " + digest
}

func resumeAt(key string) int64 {
	rangeResume.mu.Lock()
	defer rangeResume.mu.Unlock()
	return rangeResume.next[key]
}

func setResume(key string, next int64) {
	rangeResume.mu.Lock()
	defer rangeResume.mu.Unlock()
	if next <= 0 {
		delete(rangeResume.next, key)
		return
	}
	rangeResume.next[key] = next
}

// putRanged sends body as Content-Range pieces, starting after the last
// piece an earlier attempt landed. When every piece has gone and the
// lake still says incomplete, it no longer holds the earlier spans, and
// the body is sent once more from byte 0.
func putRanged(ctx context.Context, client *http.Client, opt Options, digest string, body []byte) (protocol.PutResponse, error) {
	var last protocol.PutResponse
	size := int64(len(body))
	key := resumeKey(opt, digest)
	start := resumeAt(key)
	if start >= size {
		start = 0
	}
	fromZero := start == 0
	for {
		for start < size {
			end := start + opt.PieceBytes - 1
			if end >= size {
				end = size - 1
			}
			header := fmt.Sprintf("bytes %d-%d/%d", start, end, size)
			last = protocol.PutResponse{}
			err := doRequest(ctx, client, opt, http.MethodPut, "/v1/blobs/"+digest, body[start:end+1], "application/octet-stream", map[string]string{
				"Content-Range": header,
			}, &last)
			if err != nil {
				// A 400 is a bad range or an assembled hash the lake
				// refused and deleted. Nothing it holds is worth resuming.
				var se *StatusError
				if errors.As(err, &se) && se.Code == http.StatusBadRequest {
					setResume(key, 0)
				}
				return last, err
			}
			if last.Complete {
				setResume(key, 0)
				return last, nil
			}
			start = end + 1
			setResume(key, start)
		}
		setResume(key, 0)
		if fromZero {
			return last, fmt.Errorf("upload: %s: range put did not assemble", digest)
		}
		fromZero = true
		start = 0
	}
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
		if _, err := putBody(ctx, client, opt, d, chunkBody[d]); err != nil {
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

// bundlesFor builds each harness's bundle. A harness that fails as a
// whole, such as a home that cannot be read, is one skip line and the
// other harnesses still upload. skipped also carries each file a
// harness left out.
func bundlesFor(opt Options) (out []adapter.Bundle, skipped []string) {
	// The Cursor readers build an export per session. The allowlist
	// check prepare makes runs first, so a refused session is not
	// snapshotted; its manifest still reaches prepare to be reported.
	permit := func(m protocol.Manifest) bool { return opt.Projects.Permitted(projectID(m)) }
	cursorManifests := func(root, machineID string) (adapter.Bundle, error) {
		return cursor.ManifestsPermit(root, machineID, permit)
	}
	cursorCLIManifests := func(root, machineID string) (adapter.Bundle, error) {
		return cursorcli.ManifestsPermit(root, machineID, permit)
	}
	homes := []struct {
		name      string
		home      string
		manifests func(root, machineID string) (adapter.Bundle, error)
	}{
		{protocol.HarnessTerva, opt.TervaHome, terva.Manifests},
		{protocol.HarnessClaude, opt.ClaudeHome, claude.Manifests},
		{protocol.HarnessCodex, opt.CodexHome, codex.Manifests},
		{protocol.HarnessOpenCode, opt.OpenCodeHome, opencode.Manifests},
		{protocol.HarnessCursor, opt.CursorHome, cursorManifests},
		{protocol.HarnessCursorCLI, opt.CursorCLIHome, cursorCLIManifests},
	}
	for _, h := range homes {
		if h.home == "" {
			continue
		}
		b, err := h.manifests(h.home, opt.MachineID)
		if err != nil {
			if b.Cleanup != nil {
				b.Cleanup()
			}
			skipped = append(skipped, fmt.Sprintf("%s: %v", h.name, err))
			continue
		}
		for _, e := range b.Skipped {
			skipped = append(skipped, e.Error())
		}
		out = append(out, b)
	}
	return out, skipped
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

// postCheck asks in batches of at most checkBatch digests. A 413 halves
// the batch and asks again, down to one digest.
func postCheck(ctx context.Context, client *http.Client, opt Options, digests []string) ([]string, error) {
	missing := []string{}
	batch := checkBatch
	for len(digests) > 0 {
		n := min(batch, len(digests))
		part, err := postCheckBatch(ctx, client, opt, digests[:n])
		var se *StatusError
		if n > 1 && errors.As(err, &se) && se.Code == http.StatusRequestEntityTooLarge {
			batch = n / 2
			continue
		}
		if err != nil {
			return nil, err
		}
		missing = append(missing, part...)
		digests = digests[n:]
	}
	return missing, nil
}

func postCheckBatch(ctx context.Context, client *http.Client, opt Options, digests []string) ([]string, error) {
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
	// The watchdog cancels only this request, with its own cause, so a
	// stall is not reported as the caller's shutdown.
	rctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := opt.stallTimeout()
	watch := newStallWatch(stall, cancel)
	defer watch.pause()
	req, err := http.NewRequestWithContext(rctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Body = &progressBody{r: bytes.NewReader(body), w: watch}
		req.GetBody = func() (io.ReadCloser, error) {
			return &progressBody{r: bytes.NewReader(body), w: watch}, nil
		}
	} else {
		watch.pause()
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
	wrap := func(err error) error {
		if errors.Is(context.Cause(rctx), errStalled) {
			return fmt.Errorf("upload: %s %s: no bytes moved for %s", method, p, stall)
		}
		return fmt.Errorf("upload: %s %s: %w", method, p, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return wrap(err)
	}
	defer resp.Body.Close()
	watch.touch()
	respBody, err := io.ReadAll(io.LimitReader(progressReader{r: resp.Body, w: watch}, 1<<20))
	if err != nil {
		return wrap(err)
	}
	watch.pause()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Method: method, Path: p, Code: resp.StatusCode, Status: resp.Status, Body: strings.TrimSpace(string(respBody))}
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
