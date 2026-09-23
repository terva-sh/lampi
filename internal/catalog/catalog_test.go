package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestSessionUIDAliasAndProvenance(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	m := sampleManifest()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	ctx := context.Background()
	first, err := c.Ingest(ctx, m, now, []Decision{{
		Relation: protocol.RelationHead,
		Record:   true,
		Head:     true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionUID == "" || len(first.ArtifactIDs) != 1 || first.HeadSHA256 != m.Artifacts[0].SHA256 {
		t.Fatalf("ack: %+v", first)
	}
	if first.Relation != protocol.RelationHead || first.HeadSize != m.Artifacts[0].Size {
		t.Fatalf("ack head: %+v", first)
	}
	alias, ok, err := c.Alias(ctx, m.Harness, m.NativeSessionID, m.MachineID)
	if err != nil || !ok || alias != first.SessionUID {
		t.Fatalf("alias %q ok=%v err=%v", alias, ok, err)
	}

	second, err := c.Ingest(ctx, m, now.Add(time.Minute), []Decision{{
		Relation: protocol.RelationUnchanged,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionUID != first.SessionUID || second.ArtifactIDs[0] != first.ArtifactIDs[0] {
		t.Fatalf("repeat changed ids: %+v then %+v", first, second)
	}
	if second.HeadSHA256 != first.HeadSHA256 || second.Relation != protocol.RelationUnchanged {
		t.Fatalf("repeat ack: %+v", second)
	}

	other := sampleManifest()
	other.MachineID = "machine-b"
	fourth, err := c.Ingest(ctx, other, now.Add(2*time.Minute), []Decision{{
		Relation: protocol.RelationUnchanged,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.SessionUID != first.SessionUID {
		t.Fatal("second machine did not join the logical session")
	}
	if fourth.ArtifactIDs[0] != first.ArtifactIDs[0] || fourth.HeadSHA256 != first.HeadSHA256 {
		t.Fatalf("same bytes minted a blob row: %+v", fourth)
	}
	bAlias, ok, err := c.Alias(ctx, other.Harness, other.NativeSessionID, other.MachineID)
	if err != nil || !ok || bAlias != first.SessionUID {
		t.Fatalf("machine-b alias %q ok=%v err=%v", bAlias, ok, err)
	}
	prov, err := c.Provenance(ctx, first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prov) != 2 || prov[0].MachineID != "machine-a" || prov[1].MachineID != "machine-b" {
		t.Fatalf("provenance: %+v", prov)
	}
	if prov[0].SHA256 != m.Artifacts[0].SHA256 || prov[1].SHA256 != m.Artifacts[0].SHA256 {
		t.Fatalf("provenance digests: %+v", prov)
	}
	arts, err := c.Artifacts(ctx, first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("artifacts: %+v", arts)
	}
}

func TestGrownFromMovesHeadDivergentDoesNot(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	m := sampleManifest()
	first, err := c.Ingest(ctx, m, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	grown := sampleManifest()
	grown.Artifacts[0].SHA256 = strings.Repeat("ab", 32)
	grown.Artifacts[0].Size = 4
	third, err := c.Ingest(ctx, grown, now.Add(time.Minute), []Decision{{
		Relation:  protocol.RelationGrownFrom,
		GrownFrom: m.Artifacts[0].SHA256,
		Record:    true,
		Head:      true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.SessionUID != first.SessionUID {
		t.Fatal("grown session minted a new uid")
	}
	if third.ArtifactIDs[0] == first.ArtifactIDs[0] {
		t.Fatal("new digest reused the old artifact id")
	}
	if third.HeadSHA256 != grown.Artifacts[0].SHA256 || third.Relation != protocol.RelationGrownFrom {
		t.Fatalf("head: %+v", third)
	}
	arts, err := c.Artifacts(ctx, first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 || arts[1].Relation != protocol.RelationGrownFrom || arts[1].GrownFrom != m.Artifacts[0].SHA256 || !arts[1].Current || arts[0].Current {
		t.Fatalf("grown artifacts: %+v", arts)
	}

	fork := sampleManifest()
	fork.Artifacts[0].SHA256 = strings.Repeat("cd", 32)
	fork.Artifacts[0].Size = 9
	div, err := c.Ingest(ctx, fork, now.Add(2*time.Minute), []Decision{{
		Relation: protocol.RelationDivergentCopy,
		Record:   true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if div.SessionUID != first.SessionUID || div.HeadSHA256 != grown.Artifacts[0].SHA256 {
		t.Fatalf("divergent moved the head: %+v", div)
	}
	if div.Relation != protocol.RelationDivergentCopy || div.ArtifactIDs[0] == third.ArtifactIDs[0] {
		t.Fatalf("divergent ack: %+v", div)
	}
	arts, err = c.Artifacts(ctx, first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 3 || arts[2].Relation != protocol.RelationDivergentCopy || arts[2].Current || !arts[1].Current {
		t.Fatalf("divergent artifacts: %+v", arts)
	}
}

func TestCounts(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	n, err := c.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != (Counts{}) {
		t.Fatalf("empty %+v", n)
	}
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	if _, err := c.Ingest(ctx, sampleManifest(), now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	n, err = c.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n.Sessions != 1 || n.Artifacts != 1 || n.Machines != 1 {
		t.Fatalf("after one %+v", n)
	}
	other := sampleManifest()
	other.MachineID = "machine-b"
	if _, err := c.Ingest(ctx, other, now, []Decision{{
		Relation: protocol.RelationUnchanged,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	grown := sampleManifest()
	grown.Artifacts[0].SHA256 = strings.Repeat("ab", 32)
	if _, err := c.Ingest(ctx, grown, now, []Decision{{
		Relation:  protocol.RelationGrownFrom,
		GrownFrom: sampleManifest().Artifacts[0].SHA256,
		Record:    true,
		Head:      true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	n, err = c.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n.Sessions != 1 || n.Artifacts != 2 || n.Machines != 2 {
		t.Fatalf("after join and growth %+v", n)
	}
}

func TestDivergentCopies(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	empty, err := c.DivergentCopies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty %+v", empty)
	}

	base := sampleManifest()
	first, err := c.Ingest(ctx, base, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	grownSHA := strings.Repeat("ab", 32)
	grown := sampleManifest()
	grown.Artifacts[0].SHA256 = grownSHA
	grown.Artifacts[0].Size = 4
	if _, err := c.Ingest(ctx, grown, now.Add(time.Minute), []Decision{{
		Relation:  protocol.RelationGrownFrom,
		GrownFrom: base.Artifacts[0].SHA256,
		Record:    true,
		Head:      true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	again := grown
	again.MachineID = "machine-d"
	if _, err := c.Ingest(ctx, again, now.Add(2*time.Minute), []Decision{{
		Relation: protocol.RelationUnchanged,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	other := sampleManifest()
	other.NativeSessionID = "other-session"
	other.Artifacts[0].RelPath = "sessions/x/other-session.jsonl"
	other.Artifacts[0].SHA256 = strings.Repeat("ee", 32)
	if _, err := c.Ingest(ctx, other, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	forkSHA := strings.Repeat("cd", 32)
	fork := sampleManifest()
	fork.MachineID = "machine-b"
	fork.Artifacts[0].SHA256 = forkSHA
	fork.Artifacts[0].Size = 9
	div, err := c.Ingest(ctx, fork, now.Add(3*time.Minute), []Decision{{
		Relation: protocol.RelationDivergentCopy, Record: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fork2SHA := strings.Repeat("ef", 32)
	fork2 := sampleManifest()
	fork2.MachineID = "machine-c"
	fork2.Artifacts[0].SHA256 = fork2SHA
	fork2.Artifacts[0].Size = 11
	div2, err := c.Ingest(ctx, fork2, now.Add(4*time.Minute), []Decision{{
		Relation: protocol.RelationDivergentCopy, Record: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.DivergentCopies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %d: %+v", len(got), got)
	}
	want := []DivergentCopy{
		{
			SessionUID: first.SessionUID, ArtifactID: div.ArtifactIDs[0],
			Harness: protocol.HarnessTerva, NativeID: base.NativeSessionID,
			Kind: protocol.KindTranscriptJSONL, RelPath: base.Artifacts[0].RelPath,
			SHA256: forkSHA, Size: 9, HeadSHA256: grownSHA, HeadSize: 4,
			Machines: []string{"machine-b"}, HeadMachines: []string{"machine-a", "machine-d"},
		},
		{
			SessionUID: first.SessionUID, ArtifactID: div2.ArtifactIDs[0],
			Harness: protocol.HarnessTerva, NativeID: base.NativeSessionID,
			Kind: protocol.KindTranscriptJSONL, RelPath: base.Artifacts[0].RelPath,
			SHA256: fork2SHA, Size: 11, HeadSHA256: grownSHA, HeadSize: 4,
			Machines: []string{"machine-c"}, HeadMachines: []string{"machine-a", "machine-d"},
		},
	}
	for i := range want {
		if got[i].SessionUID != want[i].SessionUID || got[i].ArtifactID != want[i].ArtifactID ||
			got[i].Harness != want[i].Harness || got[i].NativeID != want[i].NativeID ||
			got[i].Kind != want[i].Kind || got[i].RelPath != want[i].RelPath ||
			got[i].SHA256 != want[i].SHA256 || got[i].Size != want[i].Size ||
			got[i].HeadSHA256 != want[i].HeadSHA256 || got[i].HeadSize != want[i].HeadSize ||
			strings.Join(got[i].Machines, ",") != strings.Join(want[i].Machines, ",") ||
			strings.Join(got[i].HeadMachines, ",") != strings.Join(want[i].HeadMachines, ",") {
			t.Fatalf("row %d\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestIngestIgnoresStaleGrownFrom(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	base := []byte("base\n")
	left := append(append([]byte{}, base...), []byte("left\n")...)
	right := append(append([]byte{}, base...), []byte("right\n")...)
	blobs := memBlobs{
		digestHex(base):  base,
		digestHex(left):  left,
		digestHex(right): right,
	}
	m := sampleManifest()
	m.Artifacts[0].SHA256 = digestHex(base)
	m.Artifacts[0].Size = int64(len(base))
	if _, err := c.Ingest(ctx, m, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, blobs); err != nil {
		t.Fatal(err)
	}
	grown := m
	grown.Artifacts = []protocol.Artifact{m.Artifacts[0]}
	grown.Artifacts[0].SHA256 = digestHex(left)
	grown.Artifacts[0].Size = int64(len(left))
	ack, err := c.Ingest(ctx, grown, now.Add(time.Minute), []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Relation != protocol.RelationGrownFrom || ack.HeadSHA256 != digestHex(left) {
		t.Fatalf("grown: %+v", ack)
	}
	fork := m
	fork.MachineID = "machine-b"
	fork.Artifacts = []protocol.Artifact{m.Artifacts[0]}
	fork.Artifacts[0].SHA256 = digestHex(right)
	fork.Artifacts[0].Size = int64(len(right))
	// Claims an extension of the original head. That head has already moved.
	div, err := c.Ingest(ctx, fork, now.Add(2*time.Minute), []Decision{{
		Relation:  protocol.RelationGrownFrom,
		GrownFrom: digestHex(base),
		Record:    true,
		Head:      true,
	}}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if div.Relation != protocol.RelationDivergentCopy || div.HeadSHA256 != digestHex(left) {
		t.Fatalf("stale grown_from moved the head: %+v", div)
	}
	arts, err := c.Artifacts(ctx, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 3 || arts[2].Relation != protocol.RelationDivergentCopy || arts[2].Current || !arts[1].Current {
		t.Fatalf("artifacts: %+v", arts)
	}
}

type memBlobs map[string][]byte

func (m memBlobs) Read(digest string) ([]byte, error) {
	b, ok := m[digest]
	if !ok {
		return nil, fmt.Errorf("missing %s", digest)
	}
	return append([]byte(nil), b...), nil
}

func digestHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestNormalizeErrorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A catalog from before normalize_error, and from before the
	// provenance and artifact columns Layer B added. Open has to add
	// normalize_error without skipping those reshapes.
	if _, err := db.Exec(`
		CREATE TABLE sessions (
			session_uid TEXT PRIMARY KEY,
			harness TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			head_sha256 TEXT NOT NULL,
			manifest_json TEXT NOT NULL,
			ingested_at TEXT NOT NULL,
			UNIQUE (harness, native_session_id)
		);
		CREATE TABLE provenance (
			session_uid TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			PRIMARY KEY (session_uid, machine_id)
		);
		CREATE TABLE artifacts (
			artifact_id TEXT PRIMARY KEY,
			session_uid TEXT NOT NULL,
			kind TEXT NOT NULL,
			relpath TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			size INTEGER NOT NULL,
			UNIQUE (session_uid, relpath, sha256)
		);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	ack, err := c.Ingest(ctx, sampleManifest(), time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC), []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok, err := c.NormalizeError(ctx, ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("fresh error %q ok=%v err=%v", msg, ok, err)
	}
	if err := c.SetNormalizeError(ctx, ack.SessionUID, "normalize: line 1 is not a JSON object"); err != nil {
		t.Fatal(err)
	}
	msg, _, err = c.NormalizeError(ctx, ack.SessionUID)
	if err != nil || msg == "" {
		t.Fatalf("stored %q %v", msg, err)
	}
	if err := c.SetNormalizeError(ctx, ack.SessionUID, ""); err != nil {
		t.Fatal(err)
	}
	msg, _, err = c.NormalizeError(ctx, ack.SessionUID)
	if err != nil || msg != "" {
		t.Fatalf("cleared %q %v", msg, err)
	}
	list, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].UID != ack.SessionUID || list[0].NormalizeError != "" {
		t.Fatalf("list: %+v", list)
	}
}

func TestProjectLinkAcrossCWDs(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	root := strings.Repeat("a", 40)
	want := protocol.ProjectLinkID("git@github.com:Org/App.git", root)

	left := sampleManifest()
	left.NativeSessionID = "left"
	left.MachineID = "machine-a"
	left.Project = protocol.Project{
		CWD:       "/home/a/src/app",
		CWDHash:   "1111111111111111",
		GitRemote: "git@github.com:Org/App.git",
		GitCommit: strings.Repeat("b", 40),
		GitRoot:   root,
		ProjectID: "1111111111111111",
	}
	if _, err := c.Ingest(ctx, left, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	right := sampleManifest()
	right.NativeSessionID = "right"
	right.MachineID = "machine-b"
	right.Artifacts[0].RelPath = "sessions/y/right.jsonl"
	right.Project = protocol.Project{
		CWD:       "/Users/b/code/app",
		CWDHash:   "2222222222222222",
		GitRemote: "https://github.com/org/app",
		GitCommit: root,
		GitRoot:   strings.ToUpper(root),
	}
	if _, err := c.Ingest(ctx, right, now.Add(time.Minute), []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	linked, err := c.SessionsByProject(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 2 || linked[0].NativeID != "left" || linked[1].NativeID != "right" {
		t.Fatalf("linked: %+v", linked)
	}
	for _, row := range linked {
		if row.ProjectID != want || row.Manifest.Project.ProjectID != want {
			t.Fatalf("stored id %+v", row)
		}
		if row.ProjectID == row.Manifest.Project.CWDHash || row.ProjectID == left.Project.CWDHash || row.ProjectID == right.Project.CWDHash {
			t.Fatalf("project id is a cwd hash: %s", row.ProjectID)
		}
		if row.Manifest.Project.CWDHash == "" || linked[0].Manifest.Project.CWDHash == linked[1].Manifest.Project.CWDHash {
			t.Fatalf("cwd hashes: %+v %+v", linked[0].Manifest.Project, linked[1].Manifest.Project)
		}
	}

	none, err := c.SessionsByProject(ctx, "")
	if err != nil || none != nil {
		t.Fatalf("empty id returned %+v %v", none, err)
	}
	byHash, err := c.SessionsByProject(ctx, left.Project.CWDHash)
	if err != nil || len(byHash) != 0 {
		t.Fatalf("cwd hash query: %+v %v", byHash, err)
	}

	orphan := sampleManifest()
	orphan.NativeSessionID = "orphan"
	orphan.MachineID = "machine-c"
	orphan.Project = protocol.Project{CWD: "/tmp/loose", CWDHash: "3333333333333333", ProjectID: "3333333333333333"}
	if _, err := c.Ingest(ctx, orphan, now.Add(2*time.Minute), []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	again, err := c.SessionsByProject(ctx, want)
	if err != nil || len(again) != 2 {
		t.Fatalf("orphan joined the project: %+v %v", again, err)
	}
	list, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, row := range list {
		if row.NativeID != "orphan" {
			continue
		}
		saw = true
		if row.ProjectID != "" {
			t.Fatalf("path-only session linked as %s", row.ProjectID)
		}
	}
	if !saw {
		t.Fatal("orphan missing")
	}
}

func TestProjectLinkSameHeadRefreshesManifest(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	root := strings.Repeat("e", 40)
	want := protocol.ProjectLinkID("https://github.com/terva-sh/lampi.git", root)

	first := sampleManifest()
	first.Project = protocol.Project{CWD: "/home/a/src/lampi", CWDHash: "aaaaaaaaaaaaaaaa"}
	ack, err := c.Ingest(ctx, first, now, []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].ProjectID != "" || before[0].Manifest.Project.ProjectID != "" {
		t.Fatalf("first post: %+v", before)
	}

	again := first
	again.Project.GitRemote = "git@github.com:terva-sh/lampi.git"
	again.Project.GitCommit = strings.Repeat("f", 40)
	again.Project.GitRoot = root
	again.Project.ProjectID = again.Project.CWDHash
	second, err := c.Ingest(ctx, again, now.Add(time.Minute), []Decision{{
		Relation: protocol.RelationUnchanged,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionUID != ack.SessionUID || second.HeadSHA256 != ack.HeadSHA256 || second.Relation != protocol.RelationUnchanged {
		t.Fatalf("same-head ack: %+v then %+v", ack, second)
	}

	linked, err := c.SessionsByProject(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 {
		t.Fatalf("linked: %+v", linked)
	}
	row := linked[0]
	if row.ProjectID != want || row.Manifest.Project.ProjectID != row.ProjectID {
		t.Fatalf("column %s manifest %s", row.ProjectID, row.Manifest.Project.ProjectID)
	}
	if row.Manifest.Project.GitRoot != root || row.Manifest.Project.CWDHash != first.Project.CWDHash {
		t.Fatalf("stored project: %+v", row.Manifest.Project)
	}
	if row.ProjectID == row.Manifest.Project.CWDHash {
		t.Fatalf("project id is the cwd hash: %s", row.ProjectID)
	}
}

func TestTasksRewriteReplacesPathHead(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	transcript := []byte("{\"type\":\"meta\"}\n")
	board := []byte(`{"tasks":[{"title":"open"}]}`)
	archived := []byte(`{"tasks":[],"generations":[{"seq":1,"tasks":[{"title":"open"}]}]}`)
	td, bd, ad := digestHex(transcript), digestHex(board), digestHex(archived)
	blobs := memBlobs{td: transcript, bd: board, ad: archived}
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sess-1",
		Artifacts: []protocol.Artifact{
			{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/s.jsonl", Size: int64(len(transcript)), SHA256: td},
			{Kind: protocol.KindTasksJSON, RelPath: "tasks/tasks-sess-1.json", Size: int64(len(board)), SHA256: bd},
		},
	}
	first, err := c.Ingest(ctx, m, now, []Decision{
		{Relation: protocol.RelationHead, Record: true, Head: true},
		{Relation: protocol.RelationHead, Record: true, Head: false},
	}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	m.Artifacts[1].SHA256 = ad
	m.Artifacts[1].Size = int64(len(archived))
	second, err := c.Ingest(ctx, m, now.Add(time.Minute), []Decision{
		{Relation: protocol.RelationUnchanged},
		{Relation: protocol.RelationDivergentCopy, Record: true},
	}, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionUID != first.SessionUID || second.HeadSHA256 != td {
		t.Fatalf("session head moved: %+v", second)
	}
	_, arts, ok, err := c.Current(ctx, protocol.HarnessTerva, "sess-1")
	if err != nil || !ok {
		t.Fatalf("current %v %v", ok, err)
	}
	var tasksSHA string
	for _, a := range arts {
		if a.Kind == protocol.KindTasksJSON {
			tasksSHA = a.SHA256
		}
		if a.Kind == protocol.KindTranscriptJSONL && a.SHA256 != td {
			t.Fatalf("transcript head %s", a.SHA256)
		}
	}
	if tasksSHA != ad {
		t.Fatalf("tasks current %s, want %s", tasksSHA, ad)
	}
}

func sampleManifest() protocol.Manifest {
	sha := strings.Repeat("a", 64)
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "20260922-161000-abcd1234",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/20260922-161000-abcd1234.jsonl",
			Size:    3,
			SHA256:  sha,
		}},
	}
}
