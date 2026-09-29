package archive

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

func identities(t *testing.T, n int) []*age.X25519Identity {
	t.Helper()
	var out []*age.X25519Identity
	for i := 0; i < n; i++ {
		id, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// sample is an archive of two files, encrypted to every identity given.
func sample(t *testing.T, ids ...*age.X25519Identity) []byte {
	t.Helper()
	var rs []age.Recipient
	for _, id := range ids {
		rs = append(rs, id.Recipient())
	}
	var buf bytes.Buffer
	w, err := NewWriter(&buf, rs, Manifest{LakeID: "lake-1", Created: "2026-09-29T00:00:00Z", LampiVersion: "test", CatalogSchema: 7})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"catalog.db": "catalog bytes", "cas/sha256/ab/cdef.zst": strings.Repeat("object ", 1000)} {
		if err := w.Add(name, int64(len(body)), strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Every recipient's identity restores the archive, into owner-only
// files and directories, and the manifest comes back.
func TestEachRecipientRestores(t *testing.T) {
	ids := identities(t, 2)
	b := sample(t, ids...)
	if bytes.Contains(b, []byte("catalog bytes")) || bytes.Contains(b, []byte("lake-1")) {
		t.Fatal("plaintext in the archive")
	}
	for i, id := range ids {
		dest := t.TempDir()
		m, n, err := Extract(bytes.NewReader(b), []age.Identity{id}, dest)
		if err != nil {
			t.Fatalf("identity %d: %v", i, err)
		}
		if n != 3 || m.LakeID != "lake-1" || m.CatalogSchema != 7 || m.Format != Format {
			t.Fatalf("identity %d: %d files, manifest %+v", i, n, m)
		}
		got, err := os.ReadFile(filepath.Join(dest, "cas", "sha256", "ab", "cdef.zst"))
		if err != nil || string(got) != strings.Repeat("object ", 1000) {
			t.Fatalf("object: %v", err)
		}
		for _, p := range []string{"catalog.db", "cas", "cas/sha256/ab", "cas/sha256/ab/cdef.zst", ManifestName} {
			st, err := os.Stat(filepath.Join(dest, p))
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if st.IsDir() {
				want = 0o700
			}
			if st.Mode().Perm() != want {
				t.Fatalf("%s is %o, want %o", p, st.Mode().Perm(), want)
			}
		}
	}
}

// A wrong identity, a flipped bit and a cut-short archive are errors.
func TestDamagedArchivesAndWrongKeysAreRefused(t *testing.T) {
	ids := identities(t, 2)
	b := sample(t, ids[0])
	if _, _, err := Extract(bytes.NewReader(b), []age.Identity{ids[1]}, t.TempDir()); err == nil {
		t.Fatal("a wrong identity decrypted the archive")
	} else {
		var nomatch *age.NoIdentityMatchError
		if !errors.As(err, &nomatch) {
			t.Fatalf("wrong identity: %v", err)
		}
	}
	flipped := append([]byte(nil), b...)
	flipped[len(flipped)-40] ^= 1
	if _, _, err := Extract(bytes.NewReader(flipped), []age.Identity{ids[0]}, t.TempDir()); err == nil {
		t.Fatal("a flipped bit was accepted")
	}
	for _, cut := range []int{len(b) - 1, len(b) / 2, 200} {
		if _, _, err := Extract(bytes.NewReader(b[:cut]), []age.Identity{ids[0]}, t.TempDir()); err == nil {
			t.Fatalf("an archive cut to %d of %d bytes was accepted", cut, len(b))
		}
	}
}

// raw builds an archive by hand, entry by entry, for shapes NewWriter
// would not write.
func raw(t *testing.T, id *age.X25519Identity, entries ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	zw, _ := zstd.NewWriter(enc)
	tw := tar.NewWriter(zw)
	for _, h := range entries {
		body := strings.Repeat("x", int(h.Size))
		if h.Name == ManifestName {
			body = string(mustJSON(t, Manifest{Format: Format}))
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	enc.Close()
	return buf.Bytes()
}

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An entry that would leave the destination, a link, a duplicate, an
// archive without its manifest first and a format this version does
// not know are all refused.
func TestUnsafeEntriesAndUnknownFormatsAreRefused(t *testing.T) {
	id := identities(t, 1)[0]
	manifest := &tar.Header{Typeflag: tar.TypeReg, Name: ManifestName, Mode: 0o600}
	cases := map[string][]*tar.Header{
		"dot-dot":     {manifest, {Typeflag: tar.TypeReg, Name: "../escape", Size: 1, Mode: 0o600}},
		"absolute":    {manifest, {Typeflag: tar.TypeReg, Name: "/etc/escape", Size: 1, Mode: 0o600}},
		"inner dots":  {manifest, {Typeflag: tar.TypeReg, Name: "cas/../../escape", Size: 1, Mode: 0o600}},
		"symlink":     {manifest, {Typeflag: tar.TypeSymlink, Name: "cas", Linkname: "/tmp", Mode: 0o777}},
		"duplicate":   {manifest, {Typeflag: tar.TypeReg, Name: "a", Size: 1, Mode: 0o600}, {Typeflag: tar.TypeReg, Name: "a", Size: 1, Mode: 0o600}},
		"no manifest": {{Typeflag: tar.TypeReg, Name: "catalog.db", Size: 1, Mode: 0o600}},
	}
	for name, entries := range cases {
		dest := t.TempDir()
		if _, _, err := Extract(bytes.NewReader(raw(t, id, entries...)), []age.Identity{id}, dest); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape")); err == nil {
			t.Fatalf("%s: wrote outside the destination", name)
		}
	}

	var buf bytes.Buffer
	enc, _ := age.Encrypt(&buf, id.Recipient())
	zw, _ := zstd.NewWriter(enc)
	tw := tar.NewWriter(zw)
	body := mustJSON(t, Manifest{Format: Format + 1})
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ManifestName, Size: int64(len(body)), Mode: 0o600})
	tw.Write(body)
	tw.Close()
	zw.Close()
	enc.Close()
	if _, _, err := Extract(&buf, []age.Identity{id}, t.TempDir()); !errors.Is(err, ErrFormat) {
		t.Fatalf("a newer format: %v", err)
	}
}
