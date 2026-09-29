// Package registrar mints, lists and revokes registration codes for a
// lake. serve register and the dashboard both call it, so a code minted
// either way is checked, stored and audited the same way.
package registrar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
	"terva.sh/lampi/internal/upload"
)

// MaxLifetime bounds how long a code stays valid.
const MaxLifetime = 30 * 24 * time.Hour

// Lake is what minting reads and writes.
type Lake struct {
	Catalog  *catalog.Catalog
	Identity *identity.Identity
	// Dir is the lake directory, where audit.jsonl lives.
	Dir string
}

// Actor names who acted. Catalog goes into created_by or revoked_by,
// and Audit into the audit line's actor.
type Actor struct {
	Catalog string
	Audit   string
	// Minter is what bays the actor may add a device to. It is checked
	// when the code is stored; the zero value may add to none.
	Minter catalog.Minter
}

// Minted is a code as it is shown once.
type Minted struct {
	Registration catalog.Registration
	// Code is the secret. It is shown to the operator once and never
	// logged or stored.
	Code        string
	LakeID      string
	Fingerprint string
	PublicURL   string
}

// Refusals a caller can name to the operator.
var (
	ErrLifetime   = fmt.Errorf("a code lives more than 0 and at most %s", MaxLifetime)
	ErrNoProfile  = errors.New("no such profile")
	ErrNoIdentity = errors.New("the lake has no identity yet; serve makes one on its next start")
	ErrNoURL      = errors.New("the lake has no public URL; set it with serve identity set-url URL")
)

// URLError is a public URL that does not reach this lake.
type URLError struct {
	URL string
	Err error
}

func (e *URLError) Error() string {
	return fmt.Sprintf("the public URL %s does not reach this lake: %v; check serve identity set-url, and that the proxy forwards %s", e.URL, e.Err, protocol.KeysPath)
}

func (e *URLError) Unwrap() error { return e.Err }

// Mint checks the lake can be reached at its public URL, records a
// pending code for a device called name, and audits the mint before it
// returns the code. A code whose mint the audit log does not hold is
// revoked and not returned. An empty or default profile is stored as "".
// bays are the bays the device may write, by id, name or alias; none is
// the default bay. The caller checks that by may grant them.
func Mint(ctx context.Context, l Lake, name, profile string, bays []string, lifetime time.Duration, by Actor, now time.Time) (Minted, error) {
	if lifetime <= 0 || lifetime > MaxLifetime {
		return Minted{}, fmt.Errorf("%w: %s", ErrLifetime, lifetime)
	}
	if profile == config.DefaultProfile {
		profile = ""
	}
	// The default profile is always there, stored or not.
	known, err := l.Catalog.HasProfile(ctx, profile)
	if err != nil {
		return Minted{}, err
	}
	if !known {
		return Minted{}, fmt.Errorf("%w: %s", ErrNoProfile, profile)
	}
	if l.Identity == nil {
		return Minted{}, ErrNoIdentity
	}
	public, err := l.Catalog.PublicURL(ctx)
	if err != nil {
		return Minted{}, err
	}
	if public == "" {
		return Minted{}, ErrNoURL
	}
	if err := CheckPublicURL(ctx, public, l.Identity, now); err != nil {
		return Minted{}, &URLError{URL: public, Err: err}
	}
	secret, err := regcode.NewSecret()
	if err != nil {
		return Minted{}, err
	}
	cur, _ := l.Identity.Current(now)
	reg, err := l.Catalog.CreateRegistrationInBays(ctx, name, regcode.HashSecret(secret), profile, cur.ID, by.Catalog, bays, by.Minter, now, now.Add(lifetime))
	if err != nil {
		return Minted{}, err
	}
	code, err := regcode.Encode(l.Identity, regcode.Code{URL: public, Secret: secret, Expires: reg.Expires}, now)
	if err != nil {
		return Minted{}, err
	}
	prof := profile
	if prof == "" {
		prof = config.DefaultProfile
	}
	if err := audit.Append(l.Dir, audit.Event{Time: now, Kind: audit.RegistrationCreated, Device: reg.Name, Actor: by.Audit,
		Detail: fmt.Sprintf("registration=%s profile=%s bays=%s expires=%s", reg.ID, prof, codeBays(reg.Bays), reg.Expires.Format(time.RFC3339))}); err != nil {
		if _, rerr := l.Catalog.RevokeRegistration(ctx, reg.ID, by.Catalog, by.Audit, now); rerr != nil {
			return Minted{}, fmt.Errorf("writing the mint of %s to %s failed: %w; the code was not printed, but revoking it also failed: %v; run serve register --revoke %s", reg.ID, audit.FileName, err, rerr, reg.ID)
		}
		return Minted{}, fmt.Errorf("writing the mint of %s to %s failed: %w; the code was revoked and not printed; fix the audit log and mint again", reg.ID, audit.FileName, err)
	}
	return Minted{Registration: reg, Code: code, LakeID: l.Identity.LakeID, Fingerprint: Fingerprint(l.Identity, now), PublicURL: public}, nil
}

// Revoke revokes a pending code by id or device name and audits it. A
// code that was already revoked comes back with
// catalog.ErrRegistrationRevoked and is not audited again. The revoke
// and its audit event commit together; when the line cannot be written
// now, the revoke stands, the event stays queued, and the error says so.
func Revoke(ctx context.Context, l Lake, ref string, by Actor, now time.Time) (catalog.Registration, error) {
	r, err := l.Catalog.RevokeRegistration(ctx, ref, by.Catalog, by.Audit, now)
	if err != nil {
		return r, err
	}
	if err := l.Catalog.FlushAudit(ctx, l.Dir); err != nil {
		return r, fmt.Errorf("revoked %s, but writing it to %s failed: %w; the change stands, and the line stays queued until the audit log can be written (%w)", r.ID, audit.FileName, err, ErrAuditQueued)
	}
	return r, nil
}

// ErrAuditQueued marks a change that stands and was recorded, whose
// audit lines could not be written yet: they stay queued in the catalog
// and the next flush writes them. It is a warning, not a failure.
var ErrAuditQueued = errors.New("audit lines stay queued")

// List records the expiries since the last look, then returns every
// code, newest first. Nothing runs in the background to notice an
// expiry, so each look records it. When the audit lines cannot be
// written, the codes are still returned, with an error that matches
// ErrAuditQueued.
func List(ctx context.Context, l Lake, actor string, now time.Time) ([]catalog.Registration, error) {
	warn := AuditExpiries(ctx, l, actor, now)
	if warn != nil && !errors.Is(warn, ErrAuditQueued) {
		return nil, warn
	}
	regs, err := l.Catalog.Registrations(ctx)
	if err != nil {
		return nil, err
	}
	return regs, warn
}

// AuditExpiries records the codes that expired since the last look and
// writes a registration.expired line for each. serve writes the same
// line when an expired code is presented; the catalog hands each code to
// one of them. The lines are queued in the catalog with the change, so
// one that cannot be written now is written by the next flush; that
// error matches ErrAuditQueued.
func AuditExpiries(ctx context.Context, l Lake, actor string, now time.Time) error {
	if _, err := l.Catalog.RecordExpiries(ctx, now, actor); err != nil {
		return err
	}
	if err := l.Catalog.FlushAudit(ctx, l.Dir); err != nil {
		return fmt.Errorf("%w: writing to %s failed: %w; they are written once the audit log can be", ErrAuditQueued, audit.FileName, err)
	}
	return nil
}

// CheckPublicURL fetches the key list through public and checks that it
// is this lake's, signed over a fresh nonce.
func CheckPublicURL(ctx context.Context, public string, id *identity.Identity, now time.Time) error {
	cur, ok := id.Current(now)
	if !ok {
		return errors.New("this lake has no active key")
	}
	pub := id.Public()
	for _, k := range pub {
		if k.ID == cur.ID {
			return VerifyKeyList(ctx, public, id.LakeID, k)
		}
	}
	return errors.New("this lake's active key is not in its key list")
}

// VerifyKeyList fetches the key list at server over a fresh nonce and
// checks that it is lake lakeID's, that key is listed there as active,
// and that key signed it over the nonce. register runs the same check
// against the key a code names.
func VerifyKeyList(ctx context.Context, server, lakeID string, key protocol.LakeKey) error {
	pub, err := identity.ParsePublic(key)
	if err != nil {
		return err
	}
	nonce, err := identity.NewNonce()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	signed, p, err := upload.FetchKeys(ctx, server, nonce)
	if err != nil {
		return err
	}
	// The key's place in the list first, for a precise reason. A list
	// that lies about it can only make this refuse.
	listed := false
	for _, k := range p.Keys {
		if k.ID == key.ID && k.PublicKey == key.PublicKey {
			listed = true
			if k.Compromised {
				return fmt.Errorf("key %s is marked compromised there", key.ID)
			}
			if k.Status != identity.StatusActive {
				return fmt.Errorf("key %s is %s there, not active", key.ID, k.Status)
			}
		}
	}
	if !listed {
		return fmt.Errorf("key %s is not in the key list there", key.ID)
	}
	if err := identity.Verify(identity.ContextKeys, signed, pub); err != nil {
		return fmt.Errorf("the key list there is not signed by key %s: %w", key.ID, err)
	}
	if p.Nonce != nonce {
		return errors.New("the key list there does not carry the nonce sent: it is a replay or a cache")
	}
	if p.LakeID != lakeID {
		return fmt.Errorf("the key list there is for lake %s, not %s", p.LakeID, lakeID)
	}
	return nil
}

// Fingerprint is the active key's fingerprint, which an operator
// compares with what register shows.
func Fingerprint(id *identity.Identity, now time.Time) string {
	k, ok := id.Current(now)
	if !ok {
		return "(no active key)"
	}
	return identity.Fingerprint(k.Pub)
}

// codeBays names a code's bays for the audit line.
func codeBays(ids []string) string {
	if len(ids) == 0 {
		return catalog.DefaultBayID
	}
	return strings.Join(ids, ",")
}
