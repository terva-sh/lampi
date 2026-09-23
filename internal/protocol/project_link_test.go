package protocol

import (
	"strings"
	"testing"
)

func TestProjectLinkID(t *testing.T) {
	root := "0123456789abcdef0123456789abcdef01234567"
	want := "github.com/terva-sh/lampi@" + root
	for _, remote := range []string{
		"git@github.com:terva-sh/lampi.git",
		"https://github.com/terva-sh/lampi.git",
		"https://github.com/terva-sh/lampi",
		"ssh://git@github.com/terva-sh/lampi.git",
		"https://github.com/Terva-sh/Lampi/",
	} {
		if got := ProjectLinkID(remote, root); got != want {
			t.Fatalf("%s: %s", remote, got)
		}
		if got := ProjectLinkID(remote, "ABCDEF0123456789ABCDEF0123456789ABCDEF01"); got == "" || got == ProjectLinkID(remote, root) {
			t.Fatalf("uppercase other root: %s", got)
		}
	}
	upper := ProjectLinkID("git@github.com:terva-sh/lampi.git", "0123456789ABCDEF0123456789ABCDEF01234567")
	if upper != want {
		t.Fatalf("root case: %s", upper)
	}
	hash := "a1b2c3d4e5f60708"
	id := ProjectLinkID("git@github.com:terva-sh/lampi.git", root)
	if id == hash || strings.Contains(id, "/home/") || strings.Contains(id, hash) {
		t.Fatalf("project id %s borrowed a path", id)
	}
	if ProjectLinkID("git@github.com:terva-sh/lampi.git", hash) != "" {
		t.Fatal("cwd hash was accepted as a root commit")
	}
	if ProjectLinkID("", root) != "" || ProjectLinkID("git@github.com:terva-sh/lampi.git", "") != "" {
		t.Fatal("missing remote or root produced an id")
	}
	if ProjectLinkID("git@github.com:terva-sh/lampi.git", "abc123") != "" {
		t.Fatal("short commit produced an id")
	}
	other := ProjectLinkID("https://github.com/terva-sh/other", root)
	if other == want || other == "" {
		t.Fatalf("other remote: %s", other)
	}
}
