// Package redact is the scan that must run before bytes leave the machine.
//
// AllowAll is a stub. It does not look at the buffer, and it reports
// status "unscanned" so a manifest cannot claim a ruleset ran. A real
// ruleset v1 (secret regexes, project allowlist, quarantine) replaces it.
package redact

// Result is what a manifest's redaction object records.
type Result struct {
	Status  string
	Ruleset string
	Hits    int
}

// Redactor inspects raw bytes. Hits are a count, never the secret itself.
type Redactor interface {
	Scan(b []byte) (Result, error)
}

// AllowAll implements Redactor by refusing to pretend it scanned.
type AllowAll struct{}

// Scan ignores b. The unread contents are the point: nothing was checked.
func (AllowAll) Scan([]byte) (Result, error) {
	return Result{Status: "unscanned", Ruleset: "none", Hits: 0}, nil
}
