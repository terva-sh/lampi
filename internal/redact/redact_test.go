package redact

import (
	"os"
	"strings"
	"testing"
)

func TestRulesetV1Fixtures(t *testing.T) {
	aws := "AKIAIOSFODNN7EXAMPLE"
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
			if got.Ruleset != RulesetV1 || got.Status != "scanned" || got.Hits < 1 {
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

func TestRulesetV1CleanAndNearMiss(t *testing.T) {
	clean := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n{\"type\":\"message\",\"text\":\"hello\"}\n")
	got, err := (Ruleset{}).Scan(clean)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 0 || got.Ruleset != RulesetV1 || len(got.Rules) != 0 {
		t.Fatalf("clean: %+v", got)
	}
	for _, miss := range []string{
		"AKIA_SHORT",
		"ghp_tooshort",
		"sk-short",
		"-----BEGIN PUBLIC KEY-----",
		"password",
		// Left out of v1 on purpose: ordinary transcript text.
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
	secret := "AKIAIOSFODNN7EXAMPLE"
	dir := t.TempDir()
	if err := AppendQuarantine(dir, Record{
		RelPath: "sessions/abcd/s.jsonl",
		CWD:     "/work/app",
		SHA256:  strings.Repeat("ab", 32),
		Ruleset: RulesetV1,
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
	if !strings.Contains(string(b), "aws-access-key-id") || !strings.Contains(string(b), RulesetV1) {
		t.Fatalf("log: %s", b)
	}
}

func TestStripReplacesMatchesAndKeepsRuleName(t *testing.T) {
	aws := "AKIAIOSFODNN7EXAMPLE"
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
		{"private key header", pem, "[redacted:private-key]\nMIIB\n-----END RSA PRIVATE KEY-----"},
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
	secret := "AKIAIOSFODNN7EXAMPLE"
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
		"password",
	} {
		if got := (Ruleset{}).Strip(miss); got != miss {
			t.Fatalf("%q became %q", miss, got)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
