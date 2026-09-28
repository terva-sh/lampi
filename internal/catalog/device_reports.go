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

// migrateDeviceReports adds device_reports: the newest heartbeat each
// device sent, and when the lake received it. received_at is the
// device's durable last contact, which outlives a restart of serve.
// A report replaces the one before it, so the table holds no history.
func migrateDeviceReports(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE device_reports (
		device_id TEXT PRIMARY KEY REFERENCES devices(id),
		received_at TEXT NOT NULL,
		report TEXT NOT NULL
	)`)
	return err
}

// DeviceReport is a device's newest heartbeat.
type DeviceReport struct {
	DeviceID string
	Received time.Time
	Report   protocol.AgentReport
}

// PutDeviceReport records r as device id's newest report, received at
// now. The report is stored as the lake re-encodes it, so a field the
// lake does not know is not kept.
func (c *Catalog) PutDeviceReport(ctx context.Context, id string, r protocol.AgentReport, now time.Time) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO device_reports (device_id, received_at, report) VALUES (?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET received_at = excluded.received_at, report = excluded.report`,
		id, stamp(now), string(raw))
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// DeviceReport returns device id's newest report; false when it has
// sent none.
func (c *Catalog) DeviceReport(ctx context.Context, id string) (DeviceReport, bool, error) {
	row := c.db.QueryRowContext(ctx, `SELECT device_id, received_at, report FROM device_reports WHERE device_id = ?`, id)
	d, err := scanDeviceReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceReport{}, false, nil
	}
	if err != nil {
		return DeviceReport{}, false, err
	}
	return d, true, nil
}

// DeviceReports returns every device's newest report, ordered by
// device id.
func (c *Catalog) DeviceReports(ctx context.Context) ([]DeviceReport, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT device_id, received_at, report FROM device_reports ORDER BY device_id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []DeviceReport
	for rows.Next() {
		d, err := scanDeviceReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

func scanDeviceReport(s interface{ Scan(...any) error }) (DeviceReport, error) {
	var d DeviceReport
	var received, raw string
	if err := s.Scan(&d.DeviceID, &received, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return d, err
		}
		return d, fmt.Errorf("catalog: %w", err)
	}
	d.Received = parseStamp(received)
	if err := json.Unmarshal([]byte(raw), &d.Report); err != nil {
		return d, fmt.Errorf("catalog: device %s report: %w", d.DeviceID, err)
	}
	return d, nil
}
