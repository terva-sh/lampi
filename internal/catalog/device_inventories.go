package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// migrateDeviceInventories adds device_inventories: the newest
// inventory each device sent, and when the lake received it, in Unix
// nanoseconds. Paths and remotes can name a client, so an inventory
// replaces the one before and the table holds no history.
func migrateDeviceInventories(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE device_inventories (
		device_id TEXT PRIMARY KEY REFERENCES devices(id),
		received_ns INTEGER NOT NULL,
		inventory TEXT NOT NULL
	)`)
	return err
}

// DeviceInventory is a device's newest inventory.
type DeviceInventory struct {
	DeviceID  string
	Received  time.Time
	Inventory protocol.AgentInventory
}

// PutDeviceInventory records inv as device id's newest inventory,
// received at now. An inventory received after now stays, as for
// reports.
func (c *Catalog) PutDeviceInventory(ctx context.Context, id string, inv protocol.AgentInventory, now time.Time) error {
	raw, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO device_inventories (device_id, received_ns, inventory) VALUES (?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET received_ns = excluded.received_ns, inventory = excluded.inventory
		WHERE excluded.received_ns >= device_inventories.received_ns`,
		id, now.UnixNano(), string(raw))
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// DeviceInventoryOf returns device id's newest inventory; false when it
// has sent none.
func (c *Catalog) DeviceInventoryOf(ctx context.Context, id string) (DeviceInventory, bool, error) {
	var d DeviceInventory
	var ns int64
	var raw string
	err := c.db.QueryRowContext(ctx, `SELECT device_id, received_ns, inventory FROM device_inventories WHERE device_id = ?`, id).Scan(&d.DeviceID, &ns, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceInventory{}, false, nil
	}
	if err != nil {
		return DeviceInventory{}, false, fmt.Errorf("catalog: %w", err)
	}
	d.Received = time.Unix(0, ns).UTC()
	if err := json.Unmarshal([]byte(raw), &d.Inventory); err != nil {
		return DeviceInventory{}, false, fmt.Errorf("catalog: device %s inventory: %w", id, err)
	}
	return d, true, nil
}
