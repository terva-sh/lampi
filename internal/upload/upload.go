// Package upload is the one-shot push: hello, blob check, put of missing
// digests, then a manifest per session.
//
// It keeps no outbox. A failed run is retried by running it again. Puts
// of a digest the server already has do not transfer the body twice from
// the server's point of view; this client still skips the PUT when check
// says the digest is present.
package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/protocol"
)

// Options selects the lake and the local terva home.
type Options struct {
	ServerURL string
	Token     string
	TervaHome string
	MachineID string
	Client    *http.Client
}

// Result counts what this run did. Sessions are the uids the server ACKed,
// in manifest order.
type Result struct {
	Checked   int
	Missing   int
	Uploaded  int
	Manifests int
	Sessions  []string
}

// Sync pushes every terva session file under opt.TervaHome.
func Sync(ctx context.Context, opt Options) (Result, error) {
	if opt.ServerURL == "" {
		return Result{}, fmt.Errorf("upload: server URL is empty")
	}
	if opt.MachineID == "" {
		return Result{}, fmt.Errorf("upload: machine_id is empty")
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	hello, err := postHello(ctx, client, opt)
	if err != nil {
		return Result{}, err
	}
	supported := false
	for _, v := range hello.ProtocolVersions {
		if v == protocol.Version {
			supported = true
		}
	}
	if !supported {
		return Result{}, fmt.Errorf("upload: server does not speak capture protocol %d", protocol.Version)
	}

	bundle, err := terva.Manifests(opt.TervaHome, opt.MachineID)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if len(bundle.Manifests) == 0 {
		return res, nil
	}

	digests := make([]string, 0, len(bundle.Paths))
	for d, path := range bundle.Paths {
		st, err := os.Stat(path)
		if err != nil {
			return Result{}, err
		}
		if st.Size() > hello.MaxBlobBytes {
			return Result{}, fmt.Errorf("upload: %s is %d bytes; chunked upload is not implemented (max %d)", path, st.Size(), hello.MaxBlobBytes)
		}
		digests = append(digests, d)
	}
	res.Checked = len(digests)
	missing, err := postCheck(ctx, client, opt, digests)
	if err != nil {
		return Result{}, err
	}
	res.Missing = len(missing)
	for _, d := range missing {
		path := bundle.Paths[d]
		body, err := os.ReadFile(path)
		if err != nil {
			return Result{}, err
		}
		put, err := putBlob(ctx, client, opt, d, body)
		if err != nil {
			return Result{}, err
		}
		if !put.Exists {
			res.Uploaded++
		}
	}
	for _, m := range bundle.Manifests {
		ack, err := postManifest(ctx, client, opt, m)
		if err != nil {
			return Result{}, err
		}
		res.Manifests++
		res.Sessions = append(res.Sessions, ack.SessionUID)
	}
	return res, nil
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
