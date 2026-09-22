// Package upload is the one-shot push shared by terva-lampi sync and, later,
// the long-running agent.
//
// The order is fixed. Allowlist, then ruleset v1, then watermark.Plan,
// then the outbox, then the network. A manifest ACK is what commits the
// watermark and acks the outbox. A file whose bytes match the stored
// watermark is checked and not PUT again. A grown file is still one whole
// blob: the lake does not assemble tails yet. The manifest matches that
// body, with byte_watermark_prev 0 and tail_sha256 equal to sha256.
// A distinct tail hash waits until the PUT body is the suffix.
package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		return Result{}, nil
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
		return res, rejected
	}

	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	hello, err := postHello(ctx, client, opt)
	if err != nil {
		return res, err
	}
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
	var digests []string
	for _, w := range work {
		for _, a := range w.manifest.Artifacts {
			if int64(len(w.bodies[a.SHA256])) > hello.MaxBlobBytes {
				return res, fmt.Errorf("upload: %s is %d bytes; chunked upload is not implemented (max %d)", a.RelPath, len(w.bodies[a.SHA256]), hello.MaxBlobBytes)
			}
			if _, ok := bodies[a.SHA256]; ok {
				continue
			}
			bodies[a.SHA256] = w.bodies[a.SHA256]
			digests = append(digests, a.SHA256)
		}
	}
	res.Checked = len(digests)
	missing, err := postCheck(ctx, client, opt, digests)
	if err != nil {
		return res, err
	}
	res.Missing = len(missing)
	for _, d := range missing {
		put, err := putBlob(ctx, client, opt, d, bodies[d])
		if err != nil {
			return res, err
		}
		if !put.Exists {
			res.Uploaded++
		}
	}
	for _, w := range work {
		if err := requireScanned(w.manifest); err != nil {
			return res, err
		}
		ack, err := postManifest(ctx, client, opt, w.manifest)
		if err != nil {
			return res, err
		}
		res.Manifests++
		res.Sessions = append(res.Sessions, ack.SessionUID)
		if err := commitAck(ctx, opt, wm, q, w.manifest, ack); err != nil {
			return res, err
		}
	}
	return res, rejected
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
		mark := watermark.Mark{
			MachineID: opt.MachineID,
			Harness:   protocol.HarnessTerva,
			Root:      opt.TervaHome,
			RelPath:   a.RelPath,
			Size:      a.Size,
			ModTime:   a.MTime,
			SHA256:    a.SHA256,
			Offset:    a.Size,
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
