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

// rule is one secret shape. A match starts with one of prefixes. The
// scan finds every prefix in one pass (see find) and runs re anchored
// there. A leading \b in re would stop Go's regexp from using the
// literal, and every rule would walk every byte.
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
	// whose key material is exactly one of them is not a hit, so a
	// session that read SDK docs is not quarantined. The key material is
	// the group named key in re, or the whole match when re has none.
	// A real key that merely contains an example is still a hit.
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
		`(?i)secret_?access_?key\b[\s'"]*[=:][\s'"]*(?P<key>[A-Za-z0-9/+=]{40,})`, "secret").
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
		newRule("slack-webhook", `hooks\.slack\.com/+services/+(?P<key>[A-Z0-9]+/+[A-Z0-9]+/+[A-Za-z0-9]+)`, "hooks.slack.com").
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
	return result(choose(find(jsonView(b)))), nil
}

// Overlap is how far before the end of a clean prefix ScanAppended
// starts. It is more than any rule's span with a bounded length: the
// longest literal plus its fixed part is under 100 bytes, and a PEM
// block for a 16384-bit RSA key is about 13 KiB even with every line
// break escaped. A span with no bound, a run a rule repeats, is found
// by walking back over that run (see appendedFrom).
const Overlap = 64 << 10

// ScanAppended is Scan for b whose first clean bytes an earlier Scan
// with this ruleset found nothing in. Only a span that ends past clean
// can be new, so it scans from before clean and counts only those.
//
// A span that ends past clean and starts before it is caught when its
// start is inside the window. Every rule whose span can be longer than
// Overlap repeats a run of whitespace, quotes, slashes, '=' or ':',
// upper-case letters, or digits, so the window's start walks back over
// such a run to the literal before it. A private-key BEGIN line
// matches on its own, so a clean prefix cannot hold an open block;
// only a BEGIN line cut by the boundary is open, and that is short.
// clean 0 is Scan. clean at or past len(b) has nothing new to scan.
func (r Ruleset) ScanAppended(b []byte, clean int) (Result, error) {
	if clean <= 0 {
		return r.Scan(b)
	}
	if clean >= len(b) {
		return result(nil), nil
	}
	from := appendedFrom(b, clean)
	var fresh []span
	for _, sp := range find(jsonView(b[from:])) {
		if sp.end > clean-from {
			fresh = append(fresh, sp)
		}
	}
	return result(choose(fresh)), nil
}

// appendedFrom is where ScanAppended starts for a prefix of clean
// bytes: Overlap before clean, then back over a repeatable run and the
// literal before it, twice. The Slack webhook is the case that needs
// two: a run of slashes, "services", another run, then the host.
func appendedFrom(b []byte, clean int) int {
	from := max(clean-Overlap, 0)
	for range 2 {
		from = max(runStart(b, from)-literalSlack, 0)
	}
	return from
}

// literalSlack covers the literal and fixed text in front of a run:
// "aws_secret_access_key", "hooks.slack.com", "services", "-----BEGIN".
const literalSlack = 32

// runStart walks back from i over bytes a rule can repeat without a
// bound, in the raw form the view is built from: a backslash and the
// escape after it become whitespace, a quote, or a slash.
func runStart(b []byte, i int) int {
	for i > 0 {
		c := b[i-1]
		switch {
		case repeatable(c) || c == '\\':
			i--
		case i >= 2 && b[i-2] == '\\' && strings.IndexByte("ntrbf", c) >= 0:
			i -= 2
		case i >= 6 && b[i-6] == '\\' && b[i-5] == 'u':
			if _, ok := hex4(b[i-4 : i]); !ok {
				return i
			}
			i -= 6
		default:
			return i
		}
	}
	return 0
}

func repeatable(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte(" \t\r\n\f\v\"'/=:", c) >= 0
}

func result(chosen []span) Result {
	res := Result{Status: protocol.RedactionScanned, Ruleset: RulesetV2}
	if len(chosen) == 0 {
		return res
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
	return res
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

// literal is one rule prefix. The scan keeps, per literal, the offset
// where the next match may start, which is how a search per literal
// would go on after a match or a miss.
type literal struct {
	rule int
	lit  []byte
	fold bool
}

// literals lists every prefix in rule order. pairs maps the first two
// bytes of a literal, every case of them for a fold rule, to an index
// into pairGroups, which lists the literals that start with them.
// Index zero is no literal.
var literals, pairs, pairGroups = func() ([]literal, *[1 << 16]uint16, [][]int) {
	var lits []literal
	for idx, r := range rules {
		for _, p := range r.prefixes {
			lits = append(lits, literal{rule: idx, lit: p, fold: r.fold})
		}
	}
	var table [1 << 16]uint16
	groups := [][]int{nil}
	add := func(a, b byte, li int) {
		k := int(a)<<8 | int(b)
		if table[k] == 0 {
			groups = append(groups, nil)
			table[k] = uint16(len(groups) - 1)
		}
		groups[table[k]] = append(groups[table[k]], li)
	}
	for li, l := range lits {
		a, b := l.lit[0], l.lit[1]
		if !l.fold {
			add(a, b, li)
			continue
		}
		for _, x := range cases(a) {
			for _, y := range cases(b) {
				add(x, y, li)
			}
		}
	}
	return lits, &table, groups
}()

func cases(c byte) []byte {
	if c >= 'a' && c <= 'z' {
		return []byte{c, c - 'a' + 'A'}
	}
	return []byte{c}
}

// find returns every match of every rule in v. One pass over v looks
// up each pair of bytes in pairs. Most text has no rule literal at a
// given offset, so this is one table load per byte instead of one
// bytes.Index pass per literal.
func find(v []byte) []span {
	var out []span
	next := make([]int, len(literals))
	for i := 0; i+1 < len(v); i++ {
		g := pairs[int(v[i])<<8|int(v[i+1])]
		if g == 0 {
			continue
		}
		for _, li := range pairGroups[g] {
			l := &literals[li]
			if i < next[li] || !hasLiteral(v[i:], l) {
				continue
			}
			next[li] = i + 1
			r := &rules[l.rule]
			at := i
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
			next[li] = end
			if r.example(v[at:]) {
				continue
			}
			start := at
			if n := len(r.lead); n > 0 && at >= n && bytes.EqualFold(v[at-n:at], r.lead) {
				start = at - n
			}
			out = append(out, span{start: start, end: end, order: li, rule: l.rule})
		}
	}
	return out
}

func hasLiteral(v []byte, l *literal) bool {
	if len(v) < len(l.lit) {
		return false
	}
	if l.fold {
		return bytes.EqualFold(v[:len(l.lit)], l.lit)
	}
	return bytes.HasPrefix(v, l.lit)
}

// example reports whether the match of r at the start of v is a
// published example: its key material, with a run of slashes read as
// one (an escaped slash is two in the view), is exactly one of them.
func (r rule) example(v []byte) bool {
	if len(r.examples) == 0 {
		return false
	}
	loc := r.re.FindSubmatchIndex(v)
	if loc == nil {
		return false
	}
	key := v[loc[0]:loc[1]]
	if i := r.re.SubexpIndex("key"); i > 0 && loc[2*i] >= 0 {
		key = v[loc[2*i]:loc[2*i+1]]
	}
	key = oneSlash(key)
	for _, e := range r.examples {
		if bytes.Equal(key, e) {
			return true
		}
	}
	return false
}

// oneSlash is b with each run of '/' made one. b is returned as is when
// it has no run.
func oneSlash(b []byte) []byte {
	if !bytes.Contains(b, []byte("//")) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i, c := range b {
		if c == '/' && i > 0 && b[i-1] == '/' {
			continue
		}
		out = append(out, c)
	}
	return out
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
