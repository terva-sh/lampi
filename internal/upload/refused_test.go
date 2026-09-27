package upload

import (
	"os"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/config"
)

// checkout makes a directory that reads as a git checkout whose origin
// is remote.
func checkout(t *testing.T, dir, remote string) {
	t.Helper()
	mustFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustFile(t, filepath.Join(dir, ".git", "config"), "[remote \"origin\"]\n\turl = "+remote+"\n")
}

func tervaSession(t *testing.T, home, bucket, id, cwd string) {
	t.Helper()
	writeSession(t, home, bucket, id+".jsonl", []byte(`{"type":"meta","meta":{"id":"`+id+`","cwd":"`+cwd+`"}}`+"\n"))
}

// TKT-01M3HKYFF: one line per refused project. Checkouts of one
// repository are one project; an allowed or denied session is reported
// with its reason, and nothing is written.
func TestRefusalsGroupsByRepository(t *testing.T) {
	work := t.TempDir()
	a, b := filepath.Join(work, "a"), filepath.Join(work, "b")
	checkout(t, a, "git@git.example:team/app.git")
	checkout(t, b, "https://git.example/team/app")
	secret := filepath.Join(work, "secret")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(work, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", a)
	tervaSession(t, home, "bbbb", "s2", b)
	tervaSession(t, home, "cccc", "s3", b)
	tervaSession(t, home, "dddd", "s4", secret)
	tervaSession(t, home, "eeee", "s5", allowed)
	state := t.TempDir()

	got, skipped := Refusals(Options{
		TervaHome: home,
		MachineID: "m",
		StateDir:  state,
		Projects: config.Projects{
			Allow: []config.ProjectMatch{{CWDPrefix: allowed}, {CWDPrefix: secret}},
			Deny:  []config.ProjectMatch{{CWDPrefix: secret}},
		},
	})
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want the repository and the denied cwd: %+v", len(got), got)
	}
	repo, denied := got[0], got[1]
	if repo.Sessions != 3 || repo.CWDs != 2 || repo.CWD != a || repo.Reason != config.RefusedNoMatch ||
		len(repo.Harnesses) != 1 || repo.Harnesses[0] != "terva" {
		t.Errorf("repository line %+v", repo)
	}
	if denied.Sessions != 1 || denied.CWD != secret || denied.GitRemote != "" || denied.Reason != config.RefusedByDeny {
		t.Errorf("denied line %+v", denied)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Errorf("the report wrote to the state directory: %v %v", entries, err)
	}
}

func TestRefusalReasons(t *testing.T) {
	cases := []struct {
		name string
		p    config.Projects
		id   config.ProjectID
		want string
	}{
		{"allowed", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{CWD: "/w/a"}, ""},
		{"empty allow", config.Projects{}, config.ProjectID{CWD: "/w/a"}, config.RefusedEmptyAllow},
		{"no cwd", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{}, config.RefusedNoCWD},
		{"no match", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{CWD: "/x"}, config.RefusedNoMatch},
		{"deny wins", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}, Deny: []config.ProjectMatch{{CWDPrefix: "/w/a"}}}, config.ProjectID{CWD: "/w/a"}, config.RefusedByDeny},
	}
	for _, c := range cases {
		if got := c.p.Refusal(c.id); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if permitted := c.p.Permitted(c.id); permitted != (c.want == "") {
			t.Errorf("%s: Permitted %v disagrees with Refusal %q", c.name, permitted, c.want)
		}
	}
}

// Checkouts of one repository refused for different reasons are
// separate lines, each with its own true reason.
func TestRefusalsSplitsARepositoryByReason(t *testing.T) {
	work := t.TempDir()
	open, closed := filepath.Join(work, "open"), filepath.Join(work, "closed")
	checkout(t, open, "git@git.example:team/app.git")
	checkout(t, closed, "git@git.example:team/app.git")
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", open)
	tervaSession(t, home, "bbbb", "s2", closed)
	got, _ := Refusals(Options{
		TervaHome: home,
		MachineID: "m",
		Projects: config.Projects{
			Allow: []config.ProjectMatch{{CWDPrefix: closed}},
			Deny:  []config.ProjectMatch{{CWDPrefix: closed}},
		},
	})
	if len(got) != 2 {
		t.Fatalf("got %+v, want one line per reason", got)
	}
	reasons := map[string]string{got[0].CWD: got[0].Reason, got[1].CWD: got[1].Reason}
	if reasons[open] != config.RefusedNoMatch || reasons[closed] != config.RefusedByDeny {
		t.Fatalf("reasons %v", reasons)
	}
}
