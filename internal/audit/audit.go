// Package audit is the lake's append-only record of who may upload:
// devices created, bound, unbound and revoked, and, as registration
// lands, codes minted and redeemed and keys added and retired. Profile
// saves and deletions are recorded too.
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
	DeviceCreated  = "device.created"
	DeviceBound    = "device.bound"
	DeviceUnbound  = "device.unbound"
	DeviceRevoked  = "device.revoked"
	DeviceRefused  = "device.refused"
	DeviceDetached = "device.detached"
	DeviceProfile  = "device.profile"

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
