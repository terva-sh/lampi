package auth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTokenFileKeepsCommentsAndRejectsNonTokens(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	path := filepath.Join(t.TempDir(), "tokens")
	body := "# laptop\n" + a + "\n\n# desktop, added 2026-09\n" + b + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDevices(path)
	if err != nil {
		t.Fatal(err)
	}
	// The label is a comment, not a third token.
	if d.Len() != 2 || d.Match("Bearer # laptop") || d.Match("Bearer #") {
		t.Fatalf("devices %d", d.Len())
	}
	got, _ := os.ReadFile(path)
	want := "# laptop\nsha256:" + HashToken(a) + "\n\n# desktop, added 2026-09\nsha256:" + HashToken(b) + "\n"
	if string(got) != want {
		t.Fatalf("rewrite:\n%s\nwant:\n%s", got, want)
	}

	for name, line := range map[string]string{
		"label":     "laptop",
		"uppercase": strings.ToUpper(a),
		"short":     a[:63],
		"sentence":  "my token is " + a,
	} {
		bad := filepath.Join(t.TempDir(), "tokens")
		if err := os.WriteFile(bad, []byte("# ok\n"+line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadDevices(bad)
		if err == nil || !strings.Contains(err.Error(), "line 2 is not a device token") {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(err.Error(), line) {
			t.Fatalf("%s: error repeats the line: %v", name, err)
		}
		if left, _ := os.ReadFile(bad); string(left) != "# ok\n"+line+"\n" {
			t.Fatalf("%s: a refused file was rewritten", name)
		}
	}
}

func TestTokenDirectoryLoadsOnlyTokenSuffix(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"laptop.token":   a,
		"laptop~":        b,
		"laptop.revoked": b,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := LoadDevices(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Len() != 1 || !d.Match("Bearer "+a) || d.Match("Bearer "+b) {
		t.Fatal("a file without the .token suffix was enrolled")
	}
	if strings.Join(d.Ignored, ",") != "laptop.revoked,laptop~" {
		t.Fatalf("ignored %v", d.Ignored)
	}

	if err := os.Remove(filepath.Join(dir, "laptop.token")); err != nil {
		t.Fatal(err)
	}
	_, err = LoadDevices(dir)
	if err == nil || !strings.Contains(err.Error(), "<name>.token") || !strings.Contains(err.Error(), "laptop~") {
		t.Fatalf("directory with no .token file: %v", err)
	}
}

func TestReplaceSwapsUnderConcurrentMatch(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	d := &Devices{}
	d.Allow(a)
	next := &Devices{}
	next.Allow(b)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					d.Match("Bearer " + a)
				}
			}
		}()
	}
	d.Replace(next)
	close(stop)
	wg.Wait()
	if d.Match("Bearer "+a) || !d.Match("Bearer "+b) {
		t.Fatal("replace did not swap the set")
	}
}
