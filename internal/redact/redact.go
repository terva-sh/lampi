// Package redact is the scan that must run before bytes leave the machine.
//
// Ruleset v1 is a set of regular expressions for common tokens and private
// keys. Scan counts hits and names the rules that matched. It does not
// keep the matched text, and it does not rewrite the buffer: raw bytes
// stay intact. A hit is quarantined by the upload path unless the operator
// set the explicit override.
package redact

import (
	"regexp"

	"terva.sh/lampi/internal/protocol"
)

// RulesetV1 is the ruleset string stamped on artifact metadata.
const RulesetV1 = "v1"

// Result is what a manifest's redaction object records, plus the rule
// names a quarantine line needs. Rules never contains the secret.
type Result struct {
	Status  string
	Ruleset string
	Hits    int
	Rules   []string
}

// Redactor inspects raw bytes. Hits are a count, never the secret itself.
type Redactor interface {
	Scan(b []byte) (Result, error)
}

// Ruleset is ruleset v1.
type Ruleset struct{}

var _ Redactor = Ruleset{}

type rule struct {
	name string
	re   *regexp.Regexp
}

// v1Rules is the high-signal set: cloud keys, PATs, and private-key
// blocks. JWTs and generic password= / api_key= assignments are not
// in v1. Both show up in ordinary JSONL (tool output, docs, examples)
// and a hit quarantines the raw upload. They can be a later opt-in
// once the agent calls this scan continuously.
var v1Rules = []rule{
	{name: "private-key", re: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----`)},
	{name: "aws-access-key-id", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{name: "aws-secret-access-key", re: regexp.MustCompile(`(?i)\baws_secret_access_key\b\s*[=:]\s*['"]?[A-Za-z0-9/+=]{40}`)},
	{name: "github-pat", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{20,})\b`)},
	{name: "gitlab-pat", re: regexp.MustCompile(`\bglpat-[A-Za-z0-9\-_]{20,}\b`)},
	{name: "slack-token", re: regexp.MustCompile(`\bxox[a-z]-[A-Za-z0-9-]{10,}`)},
	{name: "slack-webhook", re: regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Z0-9]+/[A-Z0-9]+/[A-Za-z0-9]+`)},
	{name: "openai-key", re: regexp.MustCompile(`\bsk-(?:proj-|ant-)?[A-Za-z0-9_\-]{20,}\b`)},
	{name: "google-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`)},
	{name: "stripe-key", re: regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`)},
	{name: "npm-token", re: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
}

// Scan runs ruleset v1 over b. A nil or empty buffer is a clean scan.
// Status is "scanned" either way; the caller decides whether Hits blocks
// the upload. The matched bytes are not copied onto Result.
func (Ruleset) Scan(b []byte) (Result, error) {
	res := Result{Status: protocol.RedactionScanned, Ruleset: RulesetV1}
	for _, rule := range v1Rules {
		n := len(rule.re.FindAllIndex(b, -1))
		if n == 0 {
			continue
		}
		res.Hits += n
		res.Rules = append(res.Rules, rule.name)
	}
	return res, nil
}
