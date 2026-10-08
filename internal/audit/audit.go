// Package audit is the lake's append-only record of who may upload:
// devices created, bound, unbound and revoked, and, as registration
// lands, codes minted and redeemed and keys added and retired. Profile
// saves and deletions are recorded too, and so is every read of a raw
// session artifact.
//
// It is one JSON object per line in audit.jsonl in the lake directory,
// at mode 0600. serve and the operator commands beside it both append.
// Each event is one write with O_APPEND, so lines from two processes do
// not interleave. An event never holds a token, a code secret, or a
// private key; it names the device and says what happened.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName is the audit log in the lake directory.
const FileName = "audit.jsonl"

// Event kinds.
const (
	MaintenanceRequested = "maintenance.requested"
	MaintenanceFinished  = "maintenance.finished"
	MaintenanceFailed    = "maintenance.failed"
	DeviceCreated        = "device.created"
	DeviceBound          = "device.bound"
	DeviceUnbound        = "device.unbound"
	DeviceRevoked        = "device.revoked"
	DeviceRefused        = "device.refused"
	DeviceDetached       = "device.detached"
	DeviceProfile        = "device.profile"

	RegistrationCreated  = "registration.created"
	RegistrationRedeemed = "registration.redeemed"
	RegistrationExpired  = "registration.expired"
	RegistrationRevoked  = "registration.revoked"
	RegistrationRefused  = "registration.refused"

	KeyAdded   = "key.added"
	KeyRetired = "key.retired"

	ProfilePut    = "profile.put"
	ProfileDelete = "profile.delete"

	ProjectHidden   = "project.hidden"
	ProjectUnhidden = "project.unhidden"

	// ArtifactRead is an admin reading a session's raw bytes. Detail
	// names the session, the digest and the byte range, never content.
	ArtifactRead = "artifact.read"

	// EventsRead is a read token streaming normalized events, or calling
	// an MCP tool. Detail is the query: filters and field paths, never
	// content. An MCP call's detail opens with "mcp tool=" and gives
	// search text by its length.
	EventsRead = "events.read"

	// ReadTokenCreated and ReadTokenRevoked record an admin minting and
	// revoking a read token. Detail names the token, never its secret.
	ReadTokenCreated = "read_token.created"
	ReadTokenRevoked = "read_token.revoked"

	// ConflictResolved and ConflictReopened record a divergent copy
	// being resolved, by an operator or a catalog migration, and a
	// resolution being removed. Detail names the artifact and session.
	ConflictResolved = "conflict.resolved"
	ConflictReopened = "conflict.reopened"

	// Bay events (TKT-01M3N8KHW5). Detail names the session, bay and
	// principal by id, and how and why a membership changed.
	BayCreated       = "bay.created"
	BayMemberAdded   = "bay.member.added"
	BayMemberRemoved = "bay.member.removed"
	BayGrantAdded    = "bay.grant.added"
	BayGrantRemoved  = "bay.grant.removed"
	BayRenamed       = "bay.renamed"
	BayAliasAdded    = "bay.alias.added"
	BayAliasRemoved  = "bay.alias.removed"
	BayDeleted       = "bay.deleted"
	BayDefault       = "bay.default"
	BayRuleAdded     = "bay.rule.added"
	BayRuleRemoved   = "bay.rule.removed"
	BayHold          = "bay.hold"
	BayHoldReleased  = "bay.hold.released"

	// ConflictHeadChanged records an operator making a divergent copy
	// the session's head. Detail names the copy and the head it
	// replaced, whose bytes stay stored.
	ConflictHeadChanged = "conflict.head_changed"
)

// Event is one line. Device is the device name. Actor is where the
// change came from: serve, or the operator command that made it.
type Event struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Device    string    `json:"device,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	MachineID string    `json:"machine_id,omitempty"`
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail,omitempty"`
}

// Path is audit.jsonl under the lake directory.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Append writes e as one line.
func Append(dir string, e Event) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Time = e.Time.UTC()
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.OpenFile(Path(dir), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return fmt.Errorf("audit: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("audit: %w", err)
	}
	return f.Close()
}
