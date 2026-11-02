package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

func migrationDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Digest rows incrementally: routing/grant tables can grow independently
// of the bounded migration. Raw rows never enter these fingerprints.
func migrationQueryState(ctx context.Context, tx *sql.Tx, query string, args ...any) (string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		if err := json.NewEncoder(sum).Encode(values); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func migrationPolicyState(ctx context.Context, tx *sql.Tx) (string, error) {
	parts := []string{}
	for _, query := range []string{
		`SELECT key,value FROM lake_meta WHERE key='lake_id' ORDER BY key`,
		`SELECT id,name,is_default,disabled,created_at FROM bays ORDER BY id`,
		`SELECT alias,bay_id FROM bay_aliases ORDER BY alias`,
		`SELECT principal_kind,principal,bay_id,permission,granted_at FROM bay_grants ORDER BY principal_kind,principal,bay_id,permission`,
		`SELECT id,match_json,harness,action,bay_id FROM bay_rules ORDER BY id`,
	} {
		state, err := migrationQueryState(ctx, tx, query)
		if err != nil {
			return "", err
		}
		parts = append(parts, state)
	}
	return migrationDigest(parts), nil
}

func migrationSessionState(ctx context.Context, tx *sql.Tx, uid string) (string, error) {
	parts := []string{}
	for _, query := range []string{
		`SELECT bay_id,added_at FROM session_bays WHERE session_uid=? ORDER BY bay_id`,
		`SELECT bay_id,rule_id,state,routed_json,released_at FROM session_holds WHERE session_uid=? ORDER BY bay_id`,
		`SELECT harness,project_id,json_object('cwd',json_extract(manifest_json,'$.project.cwd'),'cwd_hash',json_extract(manifest_json,'$.project.cwd_hash'),'git_remote',json_extract(manifest_json,'$.project.git_remote')) FROM sessions WHERE session_uid=?`,
		`SELECT bay_ref,device_id,outcome,reason FROM session_bay_requests WHERE session_uid=? ORDER BY bay_ref`,
	} {
		state, err := migrationQueryState(ctx, tx, query, uid)
		if err != nil {
			return "", err
		}
		parts = append(parts, state)
	}
	return migrationDigest(parts), nil
}

// TouchedState includes timestamps so recovery detects a membership that
// was removed and re-added by later work, while ignoring unrelated bays.
func migrationTouchedState(ctx context.Context, tx *sql.Tx, uid string, ids []string) (string, error) {
	parts := []string{}
	for _, id := range ids {
		state, err := migrationQueryState(ctx, tx, `SELECT bay_id,added_at FROM session_bays WHERE session_uid=? AND bay_id=?`, uid, id)
		if err != nil {
			return "", err
		}
		parts = append(parts, state)
	}
	return migrationDigest(parts), nil
}
