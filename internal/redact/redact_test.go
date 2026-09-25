package redact

import (
	"os"
	"strings"
	"testing"
)

func TestRulesetV2Fixtures(t *testing.T) {
	aws := "AKIA" + "Z2X5QW7RT3LK9PMN"
	github := "ghp_" + strings.Repeat("a", 36)
	gitlab := "glpat-" + strings.Repeat("b", 20)
	slack := "xoxb-1234567890-abcdefghij"
	openai := "sk-" + strings.Repeat("c", 20)
	google := "AIza" + strings.Repeat("d", 35)
	stripe := "sk_live_" + strings.Repeat("e", 16)
	npm := "npm_" + strings.Repeat("f", 36)
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	awsSecret := "aws_secret_access_key = " + strings.Repeat("A", 40)

	cases := []struct {
		name string
		body string
		rule string
	}{
		{"aws access key", "key " + aws + " leaked", "aws-access-key-id"},
		{"github pat", github, "github-pat"},
		{"gitlab pat", gitlab, "gitlab-pat"},
		{"slack token", slack, "slack-token"},
		{"openai key", openai, "openai-key"},
		{"google api key", google, "google-api-key"},
		{"stripe key", stripe, "stripe-key"},
		{"npm token", npm, "npm-token"},
		{"private key", pem, "private-key"},
		{"aws secret", awsSecret, "aws-secret-access-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (Ruleset{}).Scan([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got.Ruleset != RulesetV2 || got.Status != "scanned" || got.Hits < 1 {
				t.Fatalf("result: %+v", got)
			}
			if !contains(got.Rules, tc.rule) {
				t.Fatalf("rules %v, want %s", got.Rules, tc.rule)
			}
			joined := strings.Join(got.Rules, " ")
			if strings.Contains(tc.body, joined) && strings.Contains(tc.body, aws) && strings.Contains(joined, aws) {
				t.Fatalf("rules leaked a secret: %v", got.Rules)
			}
			if strings.Contains(joined, github) {
				t.Fatalf("rules leaked a secret: %v", got.Rules)
			}
		})
	}
}

func TestRulesetV2CleanAndNearMiss(t *testing.T) {
	clean := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n{\"type\":\"message\",\"text\":\"hello\"}\n")
	got, err := (Ruleset{}).Scan(clean)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 0 || got.Ruleset != RulesetV2 || len(got.Rules) != 0 {
		t.Fatalf("clean: %+v", got)
	}
	for _, miss := range []string{
		"AKIA_SHORT",
		"ghp_tooshort",
		"sk-short",
		"-----BEGIN PUBLIC KEY-----",
		"-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----",
		"-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
		"-----END RSA PRIVATE KEY-----",
		"password",
		// Left out of the ruleset on purpose: ordinary transcript text.
		"eyJhbGciOiJub25lIn0.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmN",
		`api_key = "supersecretvalue"`,
		`{"text":"set password=hunter2hunter2 in the docs"}`,
	} {
		got, err := (Ruleset{}).Scan([]byte(miss))
		if err != nil {
			t.Fatal(err)
		}
		if got.Hits != 0 {
			t.Fatalf("%q matched: %+v", miss, got)
		}
	}
}

func TestQuarantineOmitsSecret(t *testing.T) {
	secret := "AKIA" + "Z2X5QW7RT3LK9PMN"
	dir := t.TempDir()
	if err := AppendQuarantine(dir, Record{
		RelPath: "sessions/abcd/s.jsonl",
		CWD:     "/work/app",
		SHA256:  strings.Repeat("ab", 32),
		Ruleset: RulesetV2,
		Hits:    1,
		Rules:   []string{"aws-access-key-id"},
	}); err != nil {
		t.Fatal(err)
	}
	path := QuarantineFile(dir)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("quarantine log contains the secret:\n%s", b)
	}
	if !strings.Contains(string(b), "aws-access-key-id") || !strings.Contains(string(b), RulesetV2) {
		t.Fatalf("log: %s", b)
	}
}

func TestStripReplacesMatchesAndKeepsRuleName(t *testing.T) {
	aws := "AKIA" + "Z2X5QW7RT3LK9PMN"
	github := "ghp_" + strings.Repeat("a", 36)
	gitlab := "glpat-" + strings.Repeat("b", 20)
	slack := "xoxb-1234567890-abcdefghij"
	openai := "sk-" + strings.Repeat("c", 20)
	google := "AIza" + strings.Repeat("d", 35)
	stripe := "sk_live_" + strings.Repeat("e", 16)
	npm := "npm_" + strings.Repeat("f", 36)
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	awsSecret := "aws_secret_access_key = " + strings.Repeat("A", 40)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"aws access key", "key " + aws + " leaked", "key [redacted:aws-access-key-id] leaked"},
		{"github pat", github, "[redacted:github-pat]"},
		{"gitlab pat", gitlab, "[redacted:gitlab-pat]"},
		{"slack token", slack, "[redacted:slack-token]"},
		{"openai key", openai, "[redacted:openai-key]"},
		{"google api key", google, "[redacted:google-api-key]"},
		{"stripe key", stripe, "[redacted:stripe-key]"},
		{"npm token", npm, "[redacted:npm-token]"},
		{"private key block", pem, "[redacted:private-key]"},
		{"aws secret", awsSecret, "[redacted:aws-secret-access-key]"},
		{"two of one rule", aws + " and " + aws, "[redacted:aws-access-key-id] and [redacted:aws-access-key-id]"},
		{"two rules", aws + " " + github, "[redacted:aws-access-key-id] [redacted:github-pat]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (Ruleset{}).Strip(tc.body)
			if got != tc.want {
				t.Fatalf("strip:\n got %q\nwant %q", got, tc.want)
			}
			if strings.Contains(got, aws) || strings.Contains(got, github) || strings.Contains(got, gitlab) || strings.Contains(got, slack) {
				t.Fatalf("placeholder leaked a secret: %q", got)
			}
			again := (Ruleset{}).Strip(got)
			if again != got {
				t.Fatalf("strip is not stable: %q", again)
			}
		})
	}
}

func TestStripLeavesCleanTextAndDoesNotRewriteScan(t *testing.T) {
	clean := "hello from the pond"
	if got := (Ruleset{}).Strip(clean); got != clean {
		t.Fatalf("clean text changed: %q", got)
	}
	if got := (Ruleset{}).Strip(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	secret := "AKIA" + "Z2X5QW7RT3LK9PMN"
	buf := []byte("prefix " + secret + " suffix")
	before := string(buf)
	scanned, err := (Ruleset{}).Scan(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf) != before {
		t.Fatal("scan rewrote the buffer")
	}
	if scanned.Hits != 1 || !contains(scanned.Rules, "aws-access-key-id") {
		t.Fatalf("scan: %+v", scanned)
	}
	for _, miss := range []string{
		"AKIA_SHORT",
		"ghp_tooshort",
		"-----BEGIN PUBLIC KEY-----",
		"-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----",
		"-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
		"-----END RSA PRIVATE KEY-----",
		"password",
	} {
		if got := (Ruleset{}).Strip(miss); got != miss {
			t.Fatalf("%q became %q", miss, got)
		}
	}
}

func TestPrivateKeyBlockScanAndStripSpans(t *testing.T) {
	rsa := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	pkcs8 := "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----"
	ec := "-----BEGIN EC PRIVATE KEY-----\nMHQC\n-----END EC PRIVATE KEY-----"
	openssh := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNza\n-----END OPENSSH PRIVATE KEY-----"
	encrypted := "-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: DES-EDE3-CBC,0123456789ABCDEF\n\nMIIB\n-----END RSA PRIVATE KEY-----"
	crlf := "-----BEGIN RSA PRIVATE KEY-----\r\nMIIB\r\n-----END RSA PRIVATE KEY-----"
	twoline := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\nabcdefghijklmnop\n-----END RSA PRIVATE KEY-----"
	mismatched := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END EC PRIVATE KEY-----"
	dangling := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n"
	prose := "-----BEGIN RSA PRIVATE KEY-----\nthis is not a key\n-----END RSA PRIVATE KEY-----"

	cases := []struct {
		name  string
		body  string
		want  string
		hits  int
		rules []string
	}{
		{"rsa", rsa, "[redacted:private-key]", 1, []string{"private-key"}},
		{"pkcs8", pkcs8, "[redacted:private-key]", 1, []string{"private-key"}},
		{"ec", ec, "[redacted:private-key]", 1, []string{"private-key"}},
		{"openssh", openssh, "[redacted:private-key]", 1, []string{"private-key"}},
		{"encrypted headers", encrypted, "[redacted:private-key]", 1, []string{"private-key"}},
		{"crlf", crlf, "[redacted:private-key]", 1, []string{"private-key"}},
		{"two base64 lines", twoline, "[redacted:private-key]", 1, []string{"private-key"}},
		{"mismatched end label", mismatched, "[redacted:private-key]", 1, []string{"private-key"}},
		{"surrounding text", "pre\n" + rsa + "\npost", "pre\n[redacted:private-key]\npost", 1, []string{"private-key"}},
		{"two blocks", rsa + "\n" + ec, "[redacted:private-key]\n[redacted:private-key]", 2, []string{"private-key"}},
		{"begin without end", dangling, "[redacted:private-key]\nMIIB\n", 1, []string{"private-key"}},
		{"prose between armor", prose, "[redacted:private-key]\nthis is not a key\n-----END RSA PRIVATE KEY-----", 1, []string{"private-key"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPrivateKeySpan(t, tc.body, tc.want, tc.hits, tc.rules)
		})
	}

	aws := "AKIA" + "Z2X5QW7RT3LK9PMN"
	mixed := rsa + " " + aws
	wantMixed := "[redacted:private-key] [redacted:aws-access-key-id]"
	got := (Ruleset{}).Strip(mixed)
	if got != wantMixed {
		t.Fatalf("strip:\n got %q\nwant %q", got, wantMixed)
	}
	spans := ruleSpans(t, "private-key", mixed)
	if len(spans) != 1 || mixed[spans[0][0]:spans[0][1]] != rsa {
		t.Fatalf("private-key span %q", mixed[spans[0][0]:spans[0][1]])
	}
	scanned, err := (Ruleset{}).Scan([]byte(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if scanned.Hits != 2 || !contains(scanned.Rules, "private-key") || !contains(scanned.Rules, "aws-access-key-id") {
		t.Fatalf("scan: %+v", scanned)
	}
	if strings.Contains(strings.Join(scanned.Rules, " "), "MIIB") || strings.Contains(strings.Join(scanned.Rules, " "), aws) {
		t.Fatalf("rules leaked a secret: %v", scanned.Rules)
	}
}

func TestQuarantinePrivateKeyOmitsBody(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	got, err := (Ruleset{}).Scan([]byte(pem))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 1 || len(got.Rules) != 1 || got.Rules[0] != "private-key" {
		t.Fatalf("scan: %+v", got)
	}
	if strings.Contains(strings.Join(got.Rules, " "), "MIIB") || strings.Contains(strings.Join(got.Rules, " "), "BEGIN") {
		t.Fatalf("rules contain secret text: %v", got.Rules)
	}
	dir := t.TempDir()
	if err := AppendQuarantine(dir, Record{
		RelPath: "sessions/abcd/s.jsonl",
		SHA256:  strings.Repeat("cd", 32),
		Ruleset: got.Ruleset,
		Hits:    got.Hits,
		Rules:   got.Rules,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(QuarantineFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"MIIB", "-----BEGIN", "-----END", "PRIVATE KEY"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("quarantine log contains %q:\n%s", secret, b)
		}
	}
	if !strings.Contains(string(b), "private-key") {
		t.Fatalf("log: %s", b)
	}
}

func assertPrivateKeySpan(t *testing.T, body, want string, hits int, rules []string) {
	t.Helper()
	got := (Ruleset{}).Strip(body)
	if got != want {
		t.Fatalf("strip:\n got %q\nwant %q", got, want)
	}
	again := (Ruleset{}).Strip(got)
	if again != got {
		t.Fatalf("strip is not stable: %q", again)
	}
	spans := ruleSpans(t, "private-key", body)
	if applyRule(body, spans, "private-key") != got {
		t.Fatalf("scan spans and strip disagree\n spans %v\n strip %q", spans, got)
	}
	scanned, err := (Ruleset{}).Scan([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if scanned.Ruleset != RulesetV2 || scanned.Hits != hits {
		t.Fatalf("scan: %+v", scanned)
	}
	if len(scanned.Rules) != len(rules) {
		t.Fatalf("rules %v, want %v", scanned.Rules, rules)
	}
	for i, name := range rules {
		if scanned.Rules[i] != name {
			t.Fatalf("rules %v, want %v", scanned.Rules, rules)
		}
	}
	joined := strings.Join(scanned.Rules, " ")
	for _, secret := range []string{"MIIB", "MHQC", "b3BlbnNza", "MIIEowIBAAKCAQEA", "-----BEGIN", "-----END"} {
		if strings.Contains(body, secret) && strings.Contains(joined, secret) {
			t.Fatalf("rules leaked %q: %v", secret, scanned.Rules)
		}
	}
}

func ruleSpans(t *testing.T, name, body string) [][]int {
	t.Helper()
	// Scan and Strip both call find on the view; these are the spans
	// that one rule contributes before overlaps are resolved.
	var out [][]int
	known := false
	for _, sp := range find(jsonView([]byte(body))) {
		if rules[sp.rule].name != name {
			continue
		}
		known = true
		out = append(out, []int{sp.start, sp.end})
	}
	if !known {
		for _, r := range rules {
			known = known || r.name == name
		}
	}
	if !known {
		t.Fatalf("no rule %s", name)
	}
	return out
}

func applyRule(body string, spans [][]int, name string) string {
	var buf strings.Builder
	prev := 0
	placeholder := "[redacted:" + name + "]"
	for _, sp := range spans {
		buf.WriteString(body[prev:sp[0]])
		buf.WriteString(placeholder)
		prev = sp[1]
	}
	buf.WriteString(body[prev:])
	return buf.String()
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
