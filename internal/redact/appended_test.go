package redact

import (
	"strings"
	"testing"
)

// assertAppended checks ScanAppended at every clean boundary the
// caller could hold: each cut whose prefix Scan finds nothing in. The
// appended scan has to count what Scan counts for the whole buffer.
func assertAppended(t *testing.T, name string, body []byte, cuts []int) {
	t.Helper()
	full, err := (Ruleset{}).Scan(body)
	if err != nil {
		t.Fatal(err)
	}
	if full.Hits == 0 {
		t.Fatalf("%s: fixture has no hit", name)
	}
	tried := 0
	for _, cut := range cuts {
		pre, err := (Ruleset{}).Scan(body[:cut])
		if err != nil {
			t.Fatal(err)
		}
		if pre.Hits != 0 {
			continue
		}
		tried++
		got, err := (Ruleset{}).ScanAppended(body, cut)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hits != full.Hits || strings.Join(got.Rules, ",") != strings.Join(full.Rules, ",") {
			t.Fatalf("%s cut %d: appended %+v, full %+v", name, cut, got, full)
		}
	}
	if tried == 0 {
		t.Fatalf("%s: no clean cut", name)
	}
}

func every(from, to int) []int {
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// A secret that straddles the old end of the file is found by the
// scan of the new bytes, in plain text and inside a JSON string, for
// every rule and every cut through it.
func TestScanAppendedFindsKeyAcrossBoundary(t *testing.T) {
	pad := strings.Repeat("clean transcript text. ", 8000)
	for _, f := range ruleFixtures() {
		for _, body := range []string{
			pad + f.body + " and after",
			`{"text":` + jsonString(pad+f.body+" and after") + "}\n",
		} {
			at := strings.Index(body, f.body[:4])
			assertAppended(t, f.rule, []byte(body), every(at-2, at+len(f.body)+30))
		}
	}
}

// A private-key block that starts before the boundary and ends after
// it. The BEGIN line matches on its own, so the only clean prefix that
// reaches into the block is one that cuts the BEGIN line. The block is
// the size of a 16384-bit RSA key, with its line breaks escaped.
func TestScanAppendedFindsPrivateKeyAcrossBoundary(t *testing.T) {
	var body strings.Builder
	for range 200 {
		body.WriteString(strings.Repeat("A", 64) + "\n")
	}
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + body.String() + "-----END RSA PRIVATE KEY-----"
	pad := strings.Repeat("clean transcript text. ", 8000)
	raw := `{"text":` + jsonString(pad+pem+"\ndone") + "}\n"
	at := strings.Index(raw, "-----BEGIN")
	assertAppended(t, "pem", []byte(raw), every(at, at+len("-----BEGIN RSA PRIVATE KEY-----")+2))
	// Every cut after the BEGIN line is a prefix that already hit.
	pre, _ := (Ruleset{}).Scan([]byte(raw[:at+len("-----BEGIN RSA PRIVATE KEY-----")+1]))
	if pre.Hits == 0 {
		t.Fatal("a BEGIN line alone should hit")
	}
}

// A span longer than Overlap is a run a rule repeats. The window walks
// back over it, so the literal before the run is in the scan.
func TestScanAppendedWalksBackOverLongRuns(t *testing.T) {
	key := "Zx9" + strings.Repeat("k/7Q+", 7) + "aB2c"
	pad := strings.Repeat("clean transcript text. ", 100)
	cases := []struct {
		name string
		body string
		cut  string
	}{
		{"aws spaces", pad + "aws_secret_access_key" + strings.Repeat(" ", 3*Overlap) + "= " + key + " after", "= "},
		{"aws escaped breaks", `{"t":` + jsonString(pad+"SECRET_ACCESS_KEY"+strings.Repeat("\n", 2*Overlap)+":"+key) + "}", ":"},
		{"slack slashes", pad + "hooks.slack.com" + strings.Repeat("/", 2*Overlap) + "services" + strings.Repeat("/", 2*Overlap) + "T0ABC1234/B0ABC5678/" + strings.Repeat("w", 24), "/wwww"},
		{"pem header words", pad + "-----BEGIN " + strings.Repeat("RSA ", Overlap) + "PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----", "PRIVATE KEY-----\nMIIB"},
	}
	for _, tc := range cases {
		at := strings.LastIndex(tc.body, tc.cut)
		assertAppended(t, tc.name, []byte(tc.body), []int{at, at + 1})
	}
}

// A span that ends inside the clean prefix is not counted again, and
// the window over ordinary text is Overlap and a little more.
func TestScanAppendedCountsOnlyNewSpans(t *testing.T) {
	pad := []byte(strings.Repeat("clean transcript text. ", 20000))
	tail := []byte(" ghp_" + strings.Repeat("a", 36) + " end")
	body := append(append([]byte(nil), pad...), tail...)
	got, err := (Ruleset{}).ScanAppended(body, len(pad))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 1 || got.Rules[0] != "github-pat" {
		t.Fatalf("appended: %+v", got)
	}
	if from := appendedFrom(body, len(pad)); from < len(pad)-Overlap-4*literalSlack {
		t.Fatalf("window starts %d bytes before the boundary", len(pad)-from)
	}
	clean, err := (Ruleset{}).ScanAppended(pad, len(pad))
	if err != nil || clean.Hits != 0 {
		t.Fatalf("nothing appended: %+v %v", clean, err)
	}
}
