package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func assertNoSnapshots(t *testing.T, state string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(state, ".upload-*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("private snapshot directories remain: %v %v", paths, err)
	}
	if runtime.GOOS == "linux" {
		fds, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		for _, fd := range fds {
			name, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
			if strings.HasPrefix(name, filepath.Join(state, ".upload-")) {
				t.Fatalf("snapshot file descriptor remains open: %s", fd.Name())
			}
		}
	}
}

func TestPartialSnapshotCleanupOnArtifactError(t *testing.T) {
	for _, failure := range []string{"removed", "rewritten"} {
		t.Run(failure, func(t *testing.T) {
			home, state := t.TempDir(), t.TempDir()
			writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"))
			sidecar := writeSession(t, home, "abcd", "s.errors.jsonl", []byte("sidecar before rewrite\n"))
			bundle, err := terva.Manifests(home, "machine-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Manifests) != 1 || len(bundle.Manifests[0].Artifacts) != 2 {
				t.Fatal("fixture must have one transcript and one sidecar")
			}
			// Ensure one verified snapshot exists before the second artifact fails.
			sort.Slice(bundle.Manifests[0].Artifacts, func(i, j int) bool {
				return bundle.Manifests[0].Artifacts[i].Kind == protocol.KindTranscriptJSONL && bundle.Manifests[0].Artifacts[j].Kind != protocol.KindTranscriptJSONL
			})
			if failure == "removed" {
				if err := os.Remove(sidecar); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(sidecar, []byte("SIDECAR BEFORE REWRITE\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wm, q := openQueues(t, state)
			opt := Options{MachineID: "machine-1", StateDir: state, Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}}
			_, err = prepareEach(context.Background(), opt, wm, q, []adapter.Bundle{bundle}, func(*prepared, *Result) error {
				t.Fatal("session with an unverified artifact reached the consumer")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			assertNoSnapshots(t, state)
		})
	}
}

func TestSyncSnapshotSurvivesSourceRewrite(t *testing.T) {
	for _, form := range []string{"whole", "chunks", "ranges", "tail-fallback"} {
		t.Run(form, func(t *testing.T) {
			lake, _ := openLake(t)
			home, state := t.TempDir(), t.TempDir()
			original := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n")
			path := writeSession(t, home, "abcd", "s.jsonl", original)
			var armed, changed atomic.Bool
			handler := lake.Handler()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mutate := armed.Load() && r.URL.Path == "/v1/blobs/check" && form != "tail-fallback"
				if armed.Load() && r.URL.Path == "/v1/manifests" && form == "tail-fallback" {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					var m protocol.Manifest
					_ = json.Unmarshal(raw, &m)
					r.Body = io.NopCloser(bytes.NewReader(raw))
					mutate = len(m.Artifacts) > 0 && m.Artifacts[0].ByteWatermarkPrev > 0
				}
				if mutate && changed.CompareAndSwap(false, true) {
					// Replace the path, not just its contents. Upload must never
					// reopen this unverified newer version, including on fallback.
					if err := os.Remove(path); err != nil {
						t.Error(err)
					}
					if err := os.WriteFile(path, bytes.Repeat([]byte("Z"), len(original)), 0o600); err != nil {
						t.Error(err)
					}
					if form == "tail-fallback" {
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"error":"prefix mismatch"}`)
						return
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			opt := allowAll(srv, home, state, "/work/app")
			if form == "chunks" {
				opt.ChunkBytes = 32
			} else if form == "ranges" {
				opt.PieceBytes = 32
			}
			if form == "tail-fallback" {
				if _, err := Sync(context.Background(), opt); err != nil {
					t.Fatal(err)
				}
				extra := []byte("{\"type\":\"message\",\"text\":\"snapshot append\"}\n")
				appendFile(t, path, string(extra))
				original = append(original, extra...)
			}
			armed.Store(true)
			res, err := Sync(context.Background(), opt)
			if err != nil || res.Manifests != 1 || !changed.Load() {
				t.Fatalf("sync=%+v changed=%v err=%v", res, changed.Load(), err)
			}
			sum := sha256.Sum256(original)
			stored, err := lake.CAS.Read(hex.EncodeToString(sum[:]))
			if err != nil || !bytes.Equal(stored, original) {
				t.Fatalf("lake did not receive the verified snapshot: %v", err)
			}
			assertNoSnapshots(t, state)
			assertOutboxEmpty(t, state)
		})
	}
}

func TestSyncSnapshotCleanupOnEveryOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "no-bay", "network-error", "quarantine", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			lake, _ := openLake(t)
			handler := lake.Handler()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/manifests" {
					switch outcome {
					case "no-bay":
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"code":"no_bay"}`)
						return
					case "network-error":
						http.Error(w, "synthetic failure", http.StatusServiceUnavailable)
						return
					case "cancel":
						cancel()
						return
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			home, state := t.TempDir(), t.TempDir()
			body := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"
			if outcome == "quarantine" {
				body += "AKIA" + "Z2X5QW7RT3LK9PMN"
			}
			writeSession(t, home, "abcd", "s.jsonl", []byte(body))
			scanHook = func(string, int64) {
				dirs, _ := filepath.Glob(filepath.Join(state, ".upload-*"))
				if len(dirs) != 1 {
					t.Errorf("expected one active session snapshot directory: %v", dirs)
					return
				}
				st, err := os.Stat(dirs[0])
				if err != nil || st.Mode().Perm() != 0o700 {
					t.Errorf("snapshot directory is not private: %v", err)
				}
				files, _ := os.ReadDir(dirs[0])
				for _, f := range files {
					st, err := f.Info()
					if err != nil || st.Mode().Perm() != 0o600 {
						t.Errorf("snapshot file is not private: %v", err)
					}
				}
			}
			t.Cleanup(func() { scanHook = nil })
			res, err := Sync(ctx, allowAll(srv, home, state, "/work/app"))
			if (outcome == "success" || outcome == "no-bay") != (err == nil) {
				t.Fatalf("unexpected outcome: %+v %v", res, err)
			}
			assertNoSnapshots(t, state)
		})
	}
}

func TestFileSnapshotRangeResumeAndReplay(t *testing.T) {
	lake, _ := openLake(t)
	gate := &rangeGate{lake: lake.Handler(), failAt: 20}
	srv := httptest.NewServer(gate)
	t.Cleanup(srv.Close)
	original := []byte(strings.Repeat("0123456789", 5))
	path := filepath.Join(t.TempDir(), "original")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	digest := hex.EncodeToString(sum[:])
	body, err := snapshotBody(context.Background(), path, t.TempDir(), int64(len(original)), digest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { body.file.Close() })
	opt := Options{ServerURL: srv.URL, Token: "tok", PieceBytes: 10}
	if _, err := putRanged(context.Background(), srv.Client(), opt, digest, body); err == nil {
		t.Fatal("first attempt did not fail")
	}
	gate.takeStarts()
	if err := os.WriteFile(path, bytes.Repeat([]byte("Z"), len(original)), 0o600); err != nil {
		t.Fatal(err)
	}
	put, err := putRanged(context.Background(), srv.Client(), opt, digest, body)
	if err != nil || !put.Complete || fmt.Sprint(gate.takeStarts()) != "[20 30 40]" {
		t.Fatalf("snapshot range resume failed: %+v %v", put, err)
	}
	stored, err := lake.CAS.Read(digest)
	if err != nil || !bytes.Equal(stored, original) {
		t.Fatalf("range retry did not preserve verified bytes: %v", err)
	}
}

func TestFileSnapshotHTTPRedirectReplaysVerifiedRange(t *testing.T) {
	original := []byte("a verified file range to replay through GetBody")
	path := filepath.Join(t.TempDir(), "original")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	body, err := snapshotBody(context.Background(), path, t.TempDir(), int64(len(original)), hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { body.file.Close() })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(raw, original[2:len(original)-3]) || r.ContentLength != int64(len(original)-5) {
			t.Errorf("redirect request changed verified bytes/length: length=%d err=%v", r.ContentLength, err)
		}
		if r.URL.Path == "/first" {
			if err := os.WriteFile(path, bytes.Repeat([]byte("Z"), len(original)), 0o600); err != nil {
				t.Error(err)
			}
			http.Redirect(w, r, "/replay", http.StatusTemporaryRedirect)
		}
	}))
	t.Cleanup(srv.Close)
	err = doSourceRequest(context.Background(), srv.Client(), Options{ServerURL: srv.URL}, http.MethodPost, "/first", body.slice(2, body.size-3), "", nil, nil)
	if err != nil || calls.Load() != 2 {
		t.Fatalf("snapshot HTTP replay: calls=%d err=%v", calls.Load(), err)
	}
}
