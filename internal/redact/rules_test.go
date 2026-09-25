package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// ruleFixture is one secret per rule. The values are built from pieces
// so the source does not carry a whole token. secret is the part that
// must not survive Strip.
type ruleFixture struct {
	rule   string
	body   string
	secret string
}

func ruleFixtures() []ruleFixture {
	awsSecret := "Zx9" + strings.Repeat("k/7Q+", 7) + "aB2c"
	pemBody := "MIIEowIBAAKCAQEA" + strings.Repeat("q", 16) + "\nabcdefghijklmnop+/=="
	pgpBody := "lQOYBF" + strings.Repeat("x", 20) + "\n=ab12"
	google := "AIza" + strings.Repeat("d", 34) + "-"
	return []ruleFixture{
		{"private-key", "-----BEGIN RSA PRIVATE KEY-----\n" + pemBody + "\n-----END RSA PRIVATE KEY-----", pemBody},
		{"private-key", "-----BEGIN OPENSSH PRIVATE KEY-----\r\n" + pemBody + "\r\n-----END OPENSSH PRIVATE KEY-----", pemBody},
		{"private-key", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nVersion: GnuPG v1\n\n" + pgpBody + "\n-----END PGP PRIVATE KEY BLOCK-----", pgpBody},
		{"aws-access-key-id", "AKIA" + "Z2X5QW7RT3LK9PMN", "Z2X5QW7RT3LK9PMN"},
		{"aws-access-key-id", "ASIA" + "Z2X5QW7RT3LK9PMN", "Z2X5QW7RT3LK9PMN"},
		{"aws-secret-access-key", "AWS_SECRET_ACCESS_KEY=\"" + awsSecret + "\"", awsSecret},
		{"aws-secret-access-key", "aws_secret_access_key = " + awsSecret, awsSecret},
		{"aws-secret-access-key", `"SecretAccessKey": "` + awsSecret + `"`, awsSecret},
		{"github-pat", "ghp_" + strings.Repeat("a", 36), strings.Repeat("a", 36)},
		{"github-pat", "gho_" + strings.Repeat("a", 36), strings.Repeat("a", 36)},
		{"github-pat", "ghs_" + strings.Repeat("a", 36), strings.Repeat("a", 36)},
		{"github-pat", "github_pat_" + strings.Repeat("A1_", 27), strings.Repeat("A1_", 27)},
		{"gitlab-pat", "glpat-" + strings.Repeat("b", 20), strings.Repeat("b", 20)},
		{"slack-token", "xoxb-1234567890-" + strings.Repeat("j", 10), strings.Repeat("j", 10)},
		{"slack-token", "xapp-1-A0123456789-" + strings.Repeat("7", 13), strings.Repeat("7", 13)},
		{"slack-webhook", "https://hooks.slack.com/services/T0ABC1234/B0ABC5678/" + strings.Repeat("w", 24), strings.Repeat("w", 24)},
		{"anthropic-key", "sk-ant-api03-" + strings.Repeat("c", 40), strings.Repeat("c", 40)},
		{"openai-key", "sk-proj-" + strings.Repeat("c", 40), strings.Repeat("c", 40)},
		{"openai-key", "sk-svcacct-" + strings.Repeat("c", 40), strings.Repeat("c", 40)},
		{"openai-key", "sk-" + strings.Repeat("c", 20), strings.Repeat("c", 20)},
		{"google-api-key", google, google[4:]},
		{"stripe-key", "sk_live_" + strings.Repeat("e", 24), strings.Repeat("e", 24)},
		{"stripe-key", "rk_live_" + strings.Repeat("e", 24), strings.Repeat("e", 24)},
		{"npm-token", "npm_" + strings.Repeat("f", 36), strings.Repeat("f", 36)},
		{"pypi-token", "pypi-AgEIcHlwaS5vcmc" + strings.Repeat("g", 60), strings.Repeat("g", 60)},
		{"huggingface-token", "hf_" + strings.Repeat("h", 34), strings.Repeat("h", 34)},
		{"sendgrid-key", "SG." + strings.Repeat("i", 22) + "." + strings.Repeat("i", 42) + "-", strings.Repeat("i", 42) + "-"},
		{"digitalocean-token", "dop_v1_" + strings.Repeat("0a", 32), strings.Repeat("0a", 32)},
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ruleContexts wraps a secret the way a transcript holds it. The JSON
// cases are the raw bytes of a JSONL line, escapes and all.
var ruleContexts = []struct {
	name string
	wrap func(string) string
}{
	{"plain", func(s string) string { return "key " + s + " leaked" }},
	{"line start", func(s string) string { return "text\n" + s + "\nmore" }},
	{"json after newline", func(s string) string { return `{"text":` + jsonString("line\n"+s+"\nnext") + `}` }},
	{"json after tab", func(s string) string { return `{"text":` + jsonString("col\t"+s+"\tcol") + `}` }},
	{"json after cr", func(s string) string { return `{"text":` + jsonString("row\r\n"+s+"\r\n") + `}` }},
	{"json quoted", func(s string) string { return `{"value":` + jsonString(s) + `}` }},
	{"json escaped quote", func(s string) string { return `{"cmd":` + jsonString(`export X="`+s+`"`) + `}` }},
	{"json unicode escape", func(s string) string {
		return `{"text":"a\u003e` + strings.TrimPrefix(strings.TrimSuffix(jsonString(s), `"`), `"`) + `\u003c"}`
	}},
	{"json twice", func(s string) string {
		return `{"arguments":` + jsonString(`{"input":`+jsonString("out:\n"+s+"\n")+`}`) + `}`
	}},
}

func TestEveryRuleInEveryContext(t *testing.T) {
	seen := map[string]bool{}
	for _, fx := range ruleFixtures() {
		seen[fx.rule] = true
		for _, ctx := range ruleContexts {
			body := ctx.wrap(fx.body)
			t.Run(fx.rule+"/"+ctx.name, func(t *testing.T) {
				got, err := (Ruleset{}).Scan([]byte(body))
				if err != nil {
					t.Fatal(err)
				}
				if got.Ruleset != RulesetV2 || got.Hits != 1 || len(got.Rules) != 1 || got.Rules[0] != fx.rule {
					t.Fatalf("scan %q: %+v", body, got)
				}
				stripped := (Ruleset{}).Strip(body)
				if !strings.Contains(stripped, "[redacted:"+fx.rule+"]") {
					t.Fatalf("strip %q: %q", body, stripped)
				}
				for _, part := range strings.Split(fx.secret, "\n") {
					if strings.Contains(stripped, part) || strings.Contains(stripped, strings.Trim(jsonString(part), `"`)) {
						t.Fatalf("strip left %q in %q", part, stripped)
					}
				}
				if again := (Ruleset{}).Strip(stripped); again != stripped {
					t.Fatalf("strip is not stable: %q", again)
				}
				clean, err := (Ruleset{}).Scan([]byte(stripped))
				if err != nil {
					t.Fatal(err)
				}
				if clean.Hits != 0 {
					t.Fatalf("stripped text still scans: %+v %q", clean, stripped)
				}
			})
		}
	}
	for _, r := range rules {
		if !seen[r.name] {
			t.Errorf("rule %s has no fixture", r.name)
		}
	}
}

func TestStripRemovesJSONEscapedPEM(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\nabcdefghijklmnop\n-----END RSA PRIVATE KEY-----\n"
	line := `{"type":"tool_result","content":` + jsonString("cat id_rsa\n"+pem+"done") + `}`
	want := `{"type":"tool_result","content":"cat id_rsa\n[redacted:private-key]\ndone"}`
	if got := (Ruleset{}).Strip(line); got != want {
		t.Fatalf("strip:\n got %q\nwant %q", got, want)
	}
	got, err := (Ruleset{}).Scan([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 1 || len(got.Rules) != 1 || got.Rules[0] != "private-key" {
		t.Fatalf("scan: %+v", got)
	}
}

func TestPublishedExamplesAreNotHits(t *testing.T) {
	for _, body := range []string{
		"AKIAIOSFODNN7EXAMPLE",
		`{"text":"aws_access_key_id = AKIAIOSFODNN7EXAMPLE\naws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"}`,
		`AWS_SECRET_ACCESS_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
		"AKIAI44QH8DHBEXAMPLE je7MtGbClwBF/2Zp9Utk/h3yCo8nvbEXAMPLEKEY",
		// Split so push protection does not read the placeholder as a
		// webhook.
		"https://hooks.slack.com/services/" + "T00000000/B00000000/" + strings.Repeat("X", 24),
		// JSON escapes the slashes; the view doubles them.
		`{"url":"https:\/\/hooks.slack.com\/services\/` + `T00000000\/B00000000\/` + strings.Repeat("X", 24) + `"}`,
		`{"k":"aws_secret_access_key=wJalrXUtnFEMI\/K7MDENG\/bPxRfiCYEXAMPLEKEY"}`,
	} {
		got, err := (Ruleset{}).Scan([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if got.Hits != 0 {
			t.Fatalf("%q: %+v", body, got)
		}
		if s := (Ruleset{}).Strip(body); s != body {
			t.Fatalf("strip changed %q to %q", body, s)
		}
	}
	// An example value does not hide a real key beside it.
	real := "AKIA" + "Z2X5QW7RT3LK9PMN"
	got, err := (Ruleset{}).Scan([]byte("AKIAIOSFODNN7EXAMPLE " + real))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hits != 1 {
		t.Fatalf("scan: %+v", got)
	}
}

// The example check compares the key material exactly. A real key
// that merely contains a published example is still a hit.
func TestKeyContainingAnExampleIsAHit(t *testing.T) {
	for _, tc := range []struct{ body, rule string }{
		{"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" + "Zq9", "aws-secret-access-key"},
		{"aws_secret_access_key = Zq9" + "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "aws-secret-access-key"},
		{"https://hooks.slack.com/services/" + "T00000000/B00000000/" + strings.Repeat("X", 24) + "Zq9", "slack-webhook"},
		{"https://hooks.slack.com/services/" + "T00000000/B00000000/" + strings.Repeat("X", 23), "slack-webhook"},
	} {
		got, err := (Ruleset{}).Scan([]byte(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if got.Hits != 1 || got.Rules[0] != tc.rule {
			t.Fatalf("%q: %+v", tc.body, got)
		}
	}
}

func TestRuleNearMissesV2(t *testing.T) {
	for _, miss := range []string{
		"-----BEGIN PGP PUBLIC KEY BLOCK-----\nmQENBF\n-----END PGP PUBLIC KEY BLOCK-----",
		`{"text":"-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----"}`,
		"xAKIA" + "Z2X5QW7RT3LK9PMN",
		"AKIA" + "Z2X5QW7RT3LK9PMNX",
		"AIza" + strings.Repeat("d", 35) + "x",
		"hf_" + strings.Repeat("h", 35),
		"risk-" + strings.Repeat("c", 30),
		"xapp-short",
		`{"k":"pypi-AgEshort"}`,
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

// The case-insensitive rule matches its key name in any case, at any
// offset in a large buffer, including in every case of the two bytes
// the literal search keys on.
func TestFoldAtEveryOffset(t *testing.T) {
	for _, name := range []string{"AWS_Secret_Access_Key", "aws_SECRET_access_key", "sEcReTaccesskey"} {
		secret := name + "=" + strings.Repeat("Q", 40)
		for shift := 0; shift <= len(name); shift++ {
			body := strings.Repeat(".", 64<<10-shift) + secret + " " + strings.Repeat(".", 64<<10)
			got, err := (Ruleset{}).Scan([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			if got.Hits != 1 || got.Rules[0] != "aws-secret-access-key" {
				t.Fatalf("%s shift %d: %+v", name, shift, got)
			}
		}
	}
}

func TestRuleShapes(t *testing.T) {
	for _, r := range rules {
		if len(r.prefixes) == 0 {
			t.Errorf("%s has no prefix", r.name)
		}
		for _, p := range r.prefixes {
			if len(p) < 2 {
				t.Errorf("%s: prefix %q is shorter than the two bytes the search keys on", r.name, p)
			}
			if r.fold && strings.ToLower(string(p)) != string(p) {
				t.Errorf("%s: fold prefix %q must be lowercase", r.name, p)
			}
		}
	}
}

func TestResultAdd(t *testing.T) {
	a := Result{Status: "scanned", Ruleset: RulesetV2, Hits: 1, Rules: []string{"npm-token"}}
	b := Result{Status: "scanned", Ruleset: RulesetV2, Hits: 2, Rules: []string{"github-pat", "npm-token"}}
	got := a.Add(b)
	if got.Hits != 3 || strings.Join(got.Rules, ",") != "npm-token,github-pat" || got.Ruleset != RulesetV2 {
		t.Fatalf("add: %+v", got)
	}
	if strings.Join(a.Rules, ",") != "npm-token" {
		t.Fatalf("add rewrote its receiver: %+v", a)
	}
	if got := (Result{}).Add(Result{}); got.Hits != 0 || len(got.Rules) != 0 {
		t.Fatalf("empty: %+v", got)
	}
}
