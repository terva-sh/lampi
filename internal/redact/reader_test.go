package redact

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"testing"
)

func compareReaderScan(t *testing.T, dir string, body []byte, clean int) {
	t.Helper()
	want, err := (Ruleset{}).ScanAppended(body, clean)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Ruleset{}).ScanReaderAt(context.Background(), bytes.NewReader(body), int64(len(body)), int64(clean), dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("size=%d clean=%d: reader=%+v slice=%+v err=%v", len(body), clean, got, want, err)
	}
}

func TestReaderScanEveryRuleAndContext(t *testing.T) {
	dir := t.TempDir()
	for _, fx := range ruleFixtures() {
		for _, wrapper := range ruleContexts {
			t.Run(fx.rule+"/"+wrapper.name, func(t *testing.T) {
				body := []byte(wrapper.wrap(fx.body))
				compareReaderScan(t, dir, body, 0)
				compareReaderScan(t, dir, body, len(body)/2)
			})
		}
		// Include both sides of word boundaries and fixed-length exclusions.
		for _, body := range []string{"word" + fx.body + "word", fx.body + "-", "_" + fx.body, strings.Repeat(".", readerBuffer-2) + fx.body} {
			compareReaderScan(t, dir, []byte(body), 0)
		}
	}
	for _, r := range rules {
		for _, example := range r.examples {
			body := string(example)
			switch r.name {
			case "aws-secret-access-key":
				body = "aws_secret_access_key=" + body
			case "slack-webhook":
				body = "hooks.slack.com/services/" + body
			}
			for _, wrapper := range ruleContexts {
				compareReaderScan(t, dir, []byte(wrapper.wrap(body)), 0)
			}
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("scan left private view files: %v %v", files, err)
	}
}

func TestReaderScanBoundariesAndLongSpans(t *testing.T) {
	dir := t.TempDir()
	key := "sk-ant-" + strings.Repeat("c", 30)
	for shift := -len(key) - 6; shift < 6; shift++ {
		body := []byte(strings.Repeat(".", readerBuffer+shift) + `\n` + key + `\u0020end`)
		for _, clean := range []int{0, readerBuffer - 4, readerBuffer + 4, len(body)} {
			compareReaderScan(t, dir, body, clean)
		}
	}
	for _, body := range []string{
		"aws_secret_access_key" + strings.Repeat(" ", 3*Overlap) + "=" + strings.Repeat("z", 45),
		"SECRET_ACCESS_KEY" + strings.Repeat(`\n`, 2*Overlap) + ":" + strings.Repeat("a", 45),
		"hooks.slack.com" + strings.Repeat("/", 2*Overlap) + "services" + strings.Repeat("/", 2*Overlap) + "T0ABC/B0ABC/" + strings.Repeat("w", 30),
		"-----BEGIN " + strings.Repeat("RSA ", Overlap) + "PRIVATE KEY-----\n" + strings.Repeat("A\n", 2*Overlap) + "-----END RSA PRIVATE KEY-----",
		strings.Repeat(`\`, 2*readerBuffer) + "n " + key,
		strings.Repeat(`\`, 2*readerBuffer) + "u0020" + key,
		"aws_secret_access_key=wJalrXUtnFEMI" + strings.Repeat("/", 2*Overlap) + "K7MDENG/bPxRfiCYEXAMPLEKEY",
		strings.Repeat(key+" ", 4000),
		"\xff\xfe" + key + "\xc0\x00end",
	} {
		for _, clean := range []int{0, len(body) / 2, len(body) - 3} {
			compareReaderScan(t, dir, []byte(body), clean)
		}
	}
}

func TestStreamingJSONViewMatchesSlice(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 2))
	parts := []string{`\`, `\\`, `\u0020`, `\u000a`, `\uFFFF`, `\uZZZZ`, `\u00`, `\n`, `\r`, `\t`, `\b`, `\f`, `\/`, `\"`, `\x`, "abc", "\xff", "é"}
	for range 300 {
		var raw strings.Builder
		for range 100 {
			raw.WriteString(parts[rng.IntN(len(parts))])
		}
		var got bytes.Buffer
		w := bufio.NewWriter(&got)
		if err := writeJSONView(w, strings.NewReader(raw.String())); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), jsonView([]byte(raw.String()))) {
			t.Fatal("streaming JSON view differs from slice view")
		}
	}
}

type failedReaderAt struct{}

func (failedReaderAt) ReadAt([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }

func TestReaderScanFailsClosedAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	if _, err := (Ruleset{}).ScanReaderAt(context.Background(), failedReaderAt{}, 100, 0, dir); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read error was accepted as a clean scan: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Ruleset{}).ScanReaderAt(ctx, bytes.NewReader([]byte("x")), 1, 0, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was accepted as a clean scan: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed scan left private view files: %v %v", files, err)
	}
}

func FuzzReaderScanMatchesSlice(f *testing.F) {
	f.Add([]byte(`\nAKIAIOSFODNN7EXAMPLE`), uint16(3))
	f.Add([]byte("aws_secret_access_key="+strings.Repeat("A", 45)), uint16(1))
	f.Add([]byte("-----BEGIN RSA PRIVATE KEY-----\nMIIA\n-----END RSA PRIVATE KEY-----"), uint16(10))
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, body []byte, cut uint16) {
		if len(body) > 1<<20 {
			t.Skip()
		}
		clean := 0
		if len(body) > 0 {
			clean = int(cut) % (len(body) + 1)
		}
		t.Run(fmt.Sprint(clean), func(t *testing.T) { compareReaderScan(t, dir, body, clean) })
	})
}
