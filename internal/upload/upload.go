// Package upload is the one-shot push shared by terva-lampi sync and the
// long-running agent.
//
// The order is fixed. Allowlist, then ruleset v1, then watermark.Plan,
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
package upload

import (
	"bytes"
	"context"
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

	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
	"terva.sh/lampi/internal/watermark"
)

// Options selects the lake, the local terva home, and the off-box gate.
// Projects zero value denies every project. UploadHits is the explicit
// override that sends bytes ruleset v1 flagged.
type Options struct {
	ServerURL  string
	Token      string
	TervaHome  string
	MachineID  string
	StateDir   string
	Client     *http.Client
	Projects   config.Projects
	UploadHits bool
	// Now is the client clock for the hello skew check. Nil uses time.Now.
	Now func() time.Time
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

// Sync pushes allowlisted terva session files under opt.TervaHome.
func Sync(ctx context.Context, opt Options) (Result, error) {
	if opt.ServerURL == "" {
		return Result{}, fmt.Errorf("upload: server URL is empty")
	}
	if opt.MachineID == "" {
		return Result{}, fmt.Errorf("upload: machine_id is empty")
	}
	bundle, err := terva.Manifests(opt.TervaHome, opt.MachineID)
	if err != nil {
		return Result{}, err
	}
	if len(bundle.Manifests) == 0 {
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

	work, res, err := prepare(ctx, opt, wm, q, bundle)
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

	bodies := map[string][]byte{}
	var arts []protocol.Artifact
	for _, w := range work {
		for d, b := range w.bodies {
			bodies[d] = b
		}
		arts = append(arts, w.manifest.Artifacts...)
	}
	if err := uploadDigests(ctx, client, opt, hello.MaxBlobBytes, &res, arts, bodies); err != nil {
		return res, err
	}
	for i := range work {
		w := &work[i]
		if err := requireScanned(w.manifest); err != nil {
			return res, err
		}
		ack, err := postManifest(ctx, client, opt, w.manifest)
		if prefixMismatch(err) && widenToFullFile(w) {
			if err := uploadDigests(ctx, client, opt, hello.MaxBlobBytes, &res, w.manifest.Artifacts, w.bodies); err != nil {
				return res, err
			}
			ack, err = postManifest(ctx, client, opt, w.manifest)
		}
		if err != nil {
			return res, err
		}
		res.Manifests++
		res.Sessions = append(res.Sessions, ack.SessionUID)
		if err := commitAck(ctx, opt, wm, q, w.manifest, ack); err != nil {
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

func uploadDigests(ctx context.Context, client *http.Client, opt Options, max int64, res *Result, arts []protocol.Artifact, bodies map[string][]byte) error {
	if max <= 0 {
		max = protocol.MaxBlobBytes
	}
	seen := map[string]bool{}
	var digests []string
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
			return fmt.Errorf("upload: %s is %d bytes; chunked upload is not implemented (max %d)", a.RelPath, len(b), max)
		}
		digests = append(digests, d)
	}
	if len(digests) == 0 {
		return nil
	}
	res.Checked += len(digests)
	missing, err := postCheck(ctx, client, opt, digests)
	if err != nil {
		return err
	}
	res.Missing += len(missing)
	for _, d := range missing {
		put, err := putBlob(ctx, client, opt, d, bodies[d])
		if err != nil {
			return err
		}
		if !put.Exists {
			res.Uploaded++
		}
	}
	return nil
}

func putDigest(a protocol.Artifact) string {
	if a.ByteWatermarkPrev > 0 {
		return a.TailSHA256
	}
	return a.SHA256
}

func requireScanned(m protocol.Manifest) error {
	for _, a := range m.Artifacts {
		if a.Redaction.Ruleset != redact.RulesetV1 {
			return fmt.Errorf("upload: %s: redaction ruleset v1 did not run", a.RelPath)
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
			return fmt.Errorf("upload: %s: redaction ruleset v1 did not run", a.RelPath)
		}
	}
	return nil
}

func commitAck(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, m protocol.Manifest, ack protocol.ManifestAck) error {
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
			Harness:   protocol.HarnessTerva,
			Root:      opt.TervaHome,
			RelPath:   a.RelPath,
			Size:      size,
			ModTime:   a.MTime,
			SHA256:    sum,
			Offset:    offset,
		}
		if err := wm.Commit(ctx, mark, ack); err != nil {
			return err
		}
		if err := q.Ack(ctx, outbox.Item{Identity: blobIdentity(opt.MachineID, opt.TervaHome, a.RelPath)}); err != nil {
			return err
		}
	}
	return q.Ack(ctx, outbox.Item{Identity: manifestIdentity(opt.MachineID, m.NativeSessionID)})
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
	u, err := endpoint(opt.ServerURL, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else {
		req.Header.Set("Content-Type", "application/json")
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
