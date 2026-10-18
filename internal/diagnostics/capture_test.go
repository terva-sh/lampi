package diagnostics

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDisabledInvalidAndExisting(t *testing.T) {
	c, err := Start(Options{Duration: DefaultDuration})
	if err != nil || c != nil {
		t.Fatal("default diagnostics enabled", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []time.Duration{0, -1, time.Millisecond, MaxDuration + 1} {
		if _, err := Start(Options{Duration: d}); err == nil {
			t.Fatal("accepted invalid duration", d)
		}
	}
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "link")} {
		if path != dir {
			if err := os.Symlink(dir, path); err != nil {
				continue
			}
		}
		if _, err := Start(Options{Dir: path, Duration: DefaultDuration}); err == nil {
			t.Fatal("accepted existing path", path)
		}
	}
}
func TestCaptureCompletionAndEarlyShutdown(t *testing.T) {
	for _, early := range []bool{true, false} {
		t.Run(map[bool]string{true: "shutdown", false: "timed"}[early], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "capture")
			c, err := Start(Options{Dir: dir, Duration: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if !early {
				select {
				case <-c.done:
				case <-time.After(5 * time.Second):
					t.Fatal("unbounded capture")
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"cpu.pprof", "heap.pprof", "runtime.json"} {
				path := filepath.Join(dir, name)
				st, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
					t.Fatal("public profile", st.Mode())
				}
				if st.Size() == 0 {
					t.Fatal("empty capture", name)
				}
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if name == "runtime.json" {
					var v map[string]any
					err = json.NewDecoder(f).Decode(&v)
				} else {
					var z *gzip.Reader
					z, err = gzip.NewReader(f)
					if err == nil {
						_, err = io.Copy(io.Discard, z)
						z.Close()
					}
				}
				f.Close()
				if err != nil {
					t.Fatal(name, err)
				}
			}
		})
	}
}
