// Package redact is the scan that must run before bytes leave the machine.
//
// Ruleset v2 is a set of regular expressions for common tokens and private
// keys. Scan counts hits and names the rules that matched. It does not
// keep the matched text, and it does not rewrite the buffer: raw bytes
// stay intact. A hit is quarantined by the upload path unless the operator
// set the explicit override.
//
// Strip is the training projection's copy of that same ruleset. It
// replaces each match with a placeholder that names the rule. The upload
// path does not call it, and it does not rewrite the caller's string.
//
// Both look at a view of the bytes in which JSON string escapes are
// padded to their own length (see jsonView). A key after an escaped
// line break, or a PEM block whose line breaks are escapes, matches as
// it does in plain text, and the span is the same span in the raw bytes.
package redact

import (
	"bytes"
	"regexp"
	"slices"
	"sort"
	"strings"

	"terva.sh/lampi/internal/protocol"
)

// RulesetV2 is the ruleset string stamped on artifact metadata. v1
// missed keys inside JSON escapes. Manifests already on the lake may
// still carry v1.
const RulesetV2 = "v2"

// Result is what a manifest's redaction object records, plus the rule
// names a quarantine line needs. Rules never contains the secret.
type Result struct {
	Status  string
	Ruleset string
	Hits    int
	Rules   []string
}

// Add returns r with o's hits added and the rules o names that r does
// not, appended in o's order. Neither argument is modified.
func (r Result) Add(o Result) Result {
	out := r
	out.Rules = slices.Clone(r.Rules)
	if out.Status == "" {
		out.Status = o.Status
	}
	if out.Ruleset == "" {
		out.Ruleset = o.Ruleset
	}
	out.Hits += o.Hits
	for _, name := range o.Rules {
		if !slices.Contains(out.Rules, name) {
			out.Rules = append(out.Rules, name)
		}
	}
	return out
}

// Redactor inspects raw bytes. Hits are a count, never the secret itself.
type Redactor interface {
	Scan(b []byte) (Result, error)
}

// Ruleset is ruleset v2.
type Ruleset struct{}

var _ Redactor = Ruleset{}

// rule is one secret shape. A match starts with one of prefixes, which
// the scan finds with bytes.Index before it runs re anchored there. A
// leading \b in re would stop Go's regexp from using the literal, and
// every rule would walk every byte.
type rule struct {
	name     string
	prefixes [][]byte
	// fold matches the lowercase prefixes ignoring ASCII case. re is
	// case-insensitive itself.
	fold bool
	// lead, when it sits right before a match (ignoring case), joins
	// the span. It keeps the prefix search to one literal.
	lead []byte
	// word is the leading \b: the byte before the match is not a
	// letter, digit, or underscore.
	word bool
	re   *regexp.Regexp
	// notAfter is the trailing boundary of a fixed-length key whose
	// alphabet includes '-', where \b would miss a key ending in '-'.
	// A match followed by one of these bytes is part of a longer token.
	notAfter string
	// examples are values published in vendor documentation. A match
	// that contains one is not a hit, so a session that read SDK docs
	// is not quarantined.
	examples [][]byte
}

const keyBytes = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

func newRule(name, re string, prefixes ...string) rule {
	r := rule{name: name, word: true, re: regexp.MustCompile(`\A(?:` + re + `)`)}
	for _, p := range prefixes {
		r.prefixes = append(r.prefixes, []byte(p))
	}
	return r
}

func (r rule) withExamples(examples ...string) rule {
	for _, e := range examples {
		r.examples = append(r.examples, []byte(e))
	}
	return r
}

// privateKeyRE matches a PEM or PGP private-key block.
//
// When an END line is present the match is one span from BEGIN through
// END: the header, any Proc-Type / DEK-Info / Version lines, and the
// base64 body. The body has to be base64 (plus those header lines), so
// prose that mentions both armor lines is not one span. The END label
// uses the same shape as BEGIN. RE2 has no backreference, so the words
// on END are not required to repeat the words on BEGIN. A BEGIN line
// with no END still matches that line. PUBLIC KEY and CERTIFICATE blocks
// do not match. A line break may carry spaces or a CR before it, which
// is how an escaped line break looks in the scan's view.
const privateKeyRE = `-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----` +
	`(?:[ \t\r]*\n` +
	`(?:[A-Za-z0-9][A-Za-z0-9 -]*:[^\r\n]*[ \t\r]*\n)*` +
	`(?:[ \t\r]*\n)*` +
	`(?:[A-Za-z0-9+/=]+[ \t\r]*\n)*` +
	`(?:[A-Za-z0-9+/=]+[ \t\r]*)?` +
	`-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----)?`

// rules is the high-signal set: cloud keys, vendor tokens with a fixed
// prefix, and private-key blocks. JWTs and generic password= / api_key=
// assignments are not in it. Both show up in ordinary JSONL (tool
// output, docs, examples) and a hit quarantines the raw upload. Twilio
// is left out for the same reason: its secret has no prefix, and the
// SK id that does is not the secret.
//
// Where two rules match the same span, the earlier rule names it.
// anthropic-key comes before openai-key, whose sk- shape it shares.
var rules = func() []rule {
	pem := newRule("private-key", privateKeyRE, "-----BEGIN ")
	pem.word = false

	// The key name may sit in quotes (JSON, YAML), may be the CLI's
	// SecretAccessKey, and may follow a prefix such as TF_VAR_.
	awsSecret := newRule("aws-secret-access-key",
		`(?i)secret_?access_?key\b[\s'"]*[=:][\s'"]*[A-Za-z0-9/+=]{40,}`, "secret").
		withExamples("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "je7MtGbClwBF/2Zp9Utk/h3yCo8nvbEXAMPLEKEY")
	awsSecret.fold = true
	awsSecret.word = false
	awsSecret.lead = []byte("aws_")

	google := newRule("google-api-key", `AIza[0-9A-Za-z\-_]{35}`, "AIza")
	google.notAfter = keyBytes
	sendgrid := newRule("sendgrid-key", `SG\.[A-Za-z0-9_\-]{22}\.[A-Za-z0-9_\-]{43}`, "SG.")
	sendgrid.notAfter = keyBytes

	return []rule{
		pem,
		newRule("aws-access-key-id", `(?:AKIA|ASIA)[0-9A-Z]{16}\b`, "AKIA", "ASIA").
			withExamples("AKIAIOSFODNN7EXAMPLE", "AKIAI44QH8DHBEXAMPLE"),
		awsSecret,
		newRule("github-pat", `gh[pousr]_[A-Za-z0-9]{36}\b|github_pat_[A-Za-z0-9_]{20,}\b`,
			"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"),
		newRule("gitlab-pat", `glpat-[A-Za-z0-9\-_]{20,}\b`, "glpat-"),
		newRule("slack-token", `(?:xox[a-z]|xapp)-[A-Za-z0-9-]{10,}`, "xox", "xapp-"),
		// The slashes may be \/ escapes, which the view doubles.
		newRule("slack-webhook", `hooks\.slack\.com/+services/+[A-Z0-9]+/+[A-Z0-9]+/+[A-Za-z0-9]+`, "hooks.slack.com").
			withExamples("T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"),
		newRule("anthropic-key", `sk-ant-[A-Za-z0-9_\-]{20,}\b`, "sk-ant-"),
		newRule("openai-key", `sk-[A-Za-z0-9_\-]{20,}\b`, "sk-"),
		google,
		newRule("stripe-key", `[sr]k_live_[0-9A-Za-z]{16,}\b`, "sk_live_", "rk_live_"),
		newRule("npm-token", `npm_[A-Za-z0-9]{36}\b`, "npm_"),
		newRule("pypi-token", `pypi-AgE[A-Za-z0-9_\-]{50,}`, "pypi-AgE"),
		newRule("huggingface-token", `hf_[A-Za-z]{34}\b`, "hf_"),
		sendgrid,
		newRule("digitalocean-token", `do[por]_v1_[a-f0-9]{64}\b`, "dop_v1_", "doo_v1_", "dor_v1_"),
	}
}()

// Scan runs ruleset v2 over b. A nil or empty buffer is a clean scan.
// Status is "scanned" either way; the caller decides whether Hits blocks
// the upload. Hits counts the spans Strip would replace, so one secret
// two rules match is one hit. The matched bytes are not copied onto
// Result. Scan does not modify b and does not call Strip.
func (Ruleset) Scan(b []byte) (Result, error) {
	res := Result{Status: protocol.RedactionScanned, Ruleset: RulesetV2}
	chosen := choose(find(jsonView(b)))
	if len(chosen) == 0 {
		return res, nil
	}
	res.Hits = len(chosen)
	hit := make([]bool, len(rules))
	for _, sp := range chosen {
		hit[sp.rule] = true
	}
	for i, r := range rules {
		if hit[i] {
			res.Rules = append(res.Rules, r.name)
		}
	}
	return res, nil
}

type span struct {
	start int
	end   int
	order int
	rule  int
}

// Strip returns a copy of s with every ruleset v2 match replaced by
// [redacted:<rule>]. The placeholder names the rule and does not contain
// the matched text. Overlapping matches keep the earliest, longest span.
// A string with no match is returned unchanged. The spans are the same
// ones Scan counts; this does not add a second set of patterns. In JSON
// text a span covers the escapes inside the secret, so a PEM block whose
// line breaks are \n escapes is removed whole.
func (Ruleset) Strip(s string) string {
	if s == "" {
		return s
	}
	chosen := choose(find(jsonView([]byte(s))))
	if len(chosen) == 0 {
		return s
	}
	var buf strings.Builder
	prev := 0
	for _, sp := range chosen {
		buf.WriteString(s[prev:sp.start])
		buf.WriteString("[redacted:")
		buf.WriteString(rules[sp.rule].name)
		buf.WriteByte(']')
		prev = sp.end
	}
	buf.WriteString(s[prev:])
	return buf.String()
}

// find returns every match of every rule in v.
func find(v []byte) []span {
	var out []span
	fold := folded{v: v, start: -1}
	for idx, r := range rules {
		for _, p := range r.prefixes {
			for off := 0; off < len(v); {
				var at int
				if r.fold {
					at = fold.index(off, p)
				} else if i := bytes.Index(v[off:], p); i >= 0 {
					at = off + i
				} else {
					at = -1
				}
				if at < 0 {
					break
				}
				off = at + 1
				if r.word && at > 0 && isWord(v[at-1]) {
					continue
				}
				loc := r.re.FindIndex(v[at:])
				if loc == nil {
					continue
				}
				end := at + loc[1]
				if end < len(v) && strings.IndexByte(r.notAfter, v[end]) >= 0 {
					continue
				}
				off = end
				if r.example(v[at:end]) {
					continue
				}
				start := at
				if n := len(r.lead); n > 0 && at >= n && bytes.EqualFold(v[at-n:at], r.lead) {
					start = at - n
				}
				out = append(out, span{start: start, end: end, order: len(out), rule: idx})
			}
		}
	}
	return out
}

func (r rule) example(m []byte) bool {
	for _, e := range r.examples {
		if bytes.Contains(m, e) {
			return true
		}
	}
	return false
}

// choose keeps the earliest, longest span where spans overlap. A tie
// goes to the span found first.
func choose(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		if spans[i].end != spans[j].end {
			return spans[i].end > spans[j].end
		}
		return spans[i].order < spans[j].order
	})
	chosen := make([]span, 0, len(spans))
	covered := 0
	for _, sp := range spans {
		if sp.start < covered {
			continue
		}
		chosen = append(chosen, sp)
		covered = sp.end
	}
	return chosen
}

// jsonView returns b with JSON string escapes padded in place. A run of
// backslashes and the character after it becomes spaces and then the
// character it stands for: \n is " \n", \" is ` "`, and \\n (a line
// break in a JSON string that is itself inside a JSON string) is
// "  \n". \/ becomes "//", since a slash can sit inside a key. \uXXXX
// becomes spaces and then the ASCII character, or only spaces. Anything
// else is left alone. Every offset in the view is the same offset in b,
// so a span found in the view is the span to replace in b. A buffer with
// no backslash is returned as is.
func jsonView(b []byte) []byte {
	i := bytes.IndexByte(b, '\\')
	if i < 0 {
		return b
	}
	v := bytes.Clone(b)
	for {
		j := i
		for j < len(b) && b[j] == '\\' {
			j++
		}
		if j == len(b) {
			break
		}
		next := j + 1
		switch b[j] {
		case 'n':
			pad(v[i:j], ' ')
			v[j] = '\n'
		case 't':
			pad(v[i:j], ' ')
			v[j] = '\t'
		case 'r':
			pad(v[i:j], ' ')
			v[j] = '\r'
		case '"':
			pad(v[i:j], ' ')
		case 'b', 'f':
			pad(v[i:next], ' ')
		case '/':
			pad(v[i:next], '/')
		case 'u':
			cp, ok := hex4(b[next:])
			if !ok {
				next = j
				break
			}
			next = j + 5
			pad(v[i:next], ' ')
			if cp == '\n' || cp == '\t' || cp == '\r' || (cp >= 0x20 && cp < 0x7f) {
				v[next-1] = byte(cp)
			}
		default:
			next = j
		}
		k := bytes.IndexByte(b[next:], '\\')
		if k < 0 {
			break
		}
		i = next + k
	}
	return v
}

func pad(v []byte, c byte) {
	for i := range v {
		v[i] = c
	}
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var cp rune
	for _, c := range b[:4] {
		switch {
		case c >= '0' && c <= '9':
			cp = cp<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			cp = cp<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			cp = cp<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return cp, true
}

func isWord(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// foldWindow is how much of the buffer folded lowers at a time. Each
// window also lowers the next foldPad bytes, so a literal of up to
// foldPad bytes that starts in the window is found there.
const (
	foldWindow = 64 << 10
	foldPad    = 64
)

// folded finds a lowercase literal in v ignoring ASCII case. It lowers
// one window at a time into buf, so a fold rule does not hold a second
// copy of the buffer. Only A-Z change, so offsets stay where they were.
type folded struct {
	v     []byte
	buf   []byte
	start int // offset of buf in v, or -1
}

func (f *folded) index(off int, p []byte) int {
	for w := off - off%foldWindow; w < len(f.v); w += foldWindow {
		if f.start != w {
			end := min(w+foldWindow+foldPad, len(f.v))
			f.buf = append(f.buf[:0], f.v[w:end]...)
			for i, c := range f.buf {
				if c >= 'A' && c <= 'Z' {
					f.buf[i] = c + 'a' - 'A'
				}
			}
			f.start = w
		}
		from := max(off-w, 0)
		if i := bytes.Index(f.buf[from:], p); i >= 0 {
			return w + from + i
		}
	}
	return -1
}
