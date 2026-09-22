// Package terva is the reference harness adapter.
//
// It walks $TERVA_HOME/sessions, reads the first JSONL line when it is a
// meta record, and builds capture-protocol manifests. Digests here are of
// the whole file. internal/upload turns a strict append into a tail put.
// When the session cwd still has a .git, the manifest records origin's
// URL and HEAD.
// Cross-machine project identity is a later step. Redaction is not stamped
// here; upload scans the bytes before they leave the machine.
package terva

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/protocol"
)

// Adapter implements adapter.Harness for terva's on-disk sessions.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessTerva }

// Discover lists session files. root is a terva home, not the sessions
// directory itself.
func (Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := discover.Sessions(root)
	if err != nil {
		return nil, err
	}
	out := make([]adapter.Ref, 0, len(files))
	for _, f := range files {
		out = append(out, adapter.Ref{
			Kind:    f.Kind,
			AbsPath: f.AbsPath,
			RelPath: f.RelPath,
			Size:    f.Size,
			ModTime: f.ModTime,
		})
	}
	return out, nil
}

// ReadSlice opens absPath at offset. Offset 0 reads the whole file.
// Closing the reader is the caller's job.
func (Adapter) ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

// Bundle is a set of manifests plus the local path for each digest the
// client still has to PUT. Two files with the same bytes share one path;
// either file's bytes satisfy the put.
type Bundle struct {
	Manifests []protocol.Manifest
	Paths     map[string]string
}

type meta struct {
	ok        bool
	id        string
	cwd       string
	parent    string
	forkPoint json.RawMessage
}

type item struct {
	file discover.File
	stem string
	meta meta
	sum  string
}

// Manifests groups transcripts with their error sidecars and fills
// protocol 1 manifests. machineID is stamped on every manifest.
// Redaction is left empty. The upload path scans the file and stamps
// ruleset v1 before anything is sent.
func Manifests(tervaHome, machineID string) (Bundle, error) {
	files, err := discover.Sessions(tervaHome)
	if err != nil {
		return Bundle{}, err
	}
	items := make([]item, 0, len(files))
	for _, f := range files {
		it := item{file: f, stem: stemOf(f.AbsPath)}
		if f.Kind == discover.KindTranscript {
			m, err := readMeta(f.AbsPath)
			if err != nil {
				return Bundle{}, fmt.Errorf("terva: %s: %w", f.RelPath, err)
			}
			it.meta = m
		}
		sum, err := hashFile(f.AbsPath)
		if err != nil {
			return Bundle{}, err
		}
		it.sum = sum
		items = append(items, it)
	}

	// Transcript group keys, so an error sidecar can find its session
	// either by the meta id or by the transcript filename stem.
	transcriptKey := map[string]string{}
	for _, it := range items {
		if it.file.Kind != discover.KindTranscript {
			continue
		}
		key := groupKey(it)
		transcriptKey[dirKey(it.file.AbsPath, it.stem)] = key
		if it.meta.id != "" {
			transcriptKey[dirKey(it.file.AbsPath, it.meta.id)] = key
		}
	}

	order := make([]string, 0)
	groups := map[string][]item{}
	for _, it := range items {
		key := groupKey(it)
		if it.file.Kind == discover.KindErrors {
			if k, ok := transcriptKey[dirKey(it.file.AbsPath, it.stem)]; ok {
				key = k
			}
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], it)
	}

	b := Bundle{Paths: map[string]string{}}
	for _, key := range order {
		group := groups[key]
		native := nativeID(group)
		var project protocol.Project
		var lineage protocol.Lineage
		arts := make([]protocol.Artifact, 0, len(group))
		for _, it := range group {
			if it.meta.ok {
				remote, commit := projectGit(it.meta.cwd)
				project = protocol.Project{
					CWD:       it.meta.cwd,
					CWDHash:   CWDHash(it.meta.cwd),
					GitRemote: remote,
					GitCommit: commit,
				}
				if it.meta.parent != "" {
					parent := it.meta.parent
					lineage.ParentNativeID = &parent
				}
				if len(it.meta.forkPoint) > 0 && string(it.meta.forkPoint) != "null" {
					lineage.ForkPoint = it.meta.forkPoint
				}
			}
			b.Paths[it.sum] = it.file.AbsPath
			arts = append(arts, protocol.Artifact{
				Kind:              it.file.Kind,
				RelPath:           it.file.RelPath,
				Size:              it.file.Size,
				MTime:             it.file.ModTime.UTC(),
				SHA256:            it.sum,
				ByteWatermarkPrev: 0,
				// The whole file is the tail until the upload path applies
				// a watermark. Redaction stays empty until that scan.
				TailSHA256: it.sum,
			})
		}
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessTerva,
			NativeSessionID: native,
			Project:         project,
			Artifacts:       arts,
			Lineage:         lineage,
		})
	}
	return b, nil
}

func nativeID(group []item) string {
	for _, it := range group {
		if it.meta.id != "" {
			return it.meta.id
		}
	}
	return group[0].stem
}

func groupKey(it item) string {
	id := it.stem
	if it.meta.id != "" {
		id = it.meta.id
	}
	return dirKey(it.file.AbsPath, id)
}

func dirKey(path, id string) string {
	return filepath.Dir(path) + "\x00" + id
}

func stemOf(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".jsonl")
	name = strings.TrimSuffix(name, ".errors")
	return name
}

func readMeta(path string) (meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return meta{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return meta{}, err
		}
		return meta{}, nil
	}
	var env struct {
		Type string `json:"type"`
		Meta struct {
			ID        string          `json:"id"`
			CWD       string          `json:"cwd"`
			Parent    string          `json:"parent"`
			ForkPoint json.RawMessage `json:"fork_point"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
		// A transcript whose first line is not JSON is still a blob.
		// Leave meta unset rather than failing the walk.
		return meta{}, nil
	}
	if env.Type != "meta" {
		return meta{}, nil
	}
	return meta{
		ok:        true,
		id:        env.Meta.ID,
		cwd:       env.Meta.CWD,
		parent:    env.Meta.Parent,
		forkPoint: env.Meta.ForkPoint,
	}, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CWDHash is terva's project bucket: hex(sha256(cwd)[:8]). The absolute
// path string is the input, so the same repo in two directories does not
// share a hash. That is terva's behavior, preserved on purpose.
func CWDHash(cwd string) string {
	if cwd == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(sum[:8])
}
