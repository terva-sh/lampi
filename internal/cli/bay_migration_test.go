package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

func TestBayMigrationCLISelectionPlanApplyVerifyRecover(t *testing.T) {
	lake, _, _ := mcpLake(t)
	now := time.Now()
	// Seed a plain catalog session; no transcript or index is required.
	// mcpLake already contains its synthetic Codex session.
	page, err := lake.Catalog.DiscoverSessions(t.Context(), catalog.AllBays(), catalog.DiscoveryRequest{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("fixture %+v %v", page, err)
	}
	uid := page.Items[0].UID
	// The normalized directory is directly beneath the lake root.
	data := filepath.Dir(lake.Normalized)
	if _, err := lake.Catalog.CreateBay(t.Context(), "project", "admin", now); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		return Run(append(append([]string{"serve", "bays"}, args...), "--data", data), Env{Stdout: &out, Stderr: &stderr})
	}
	selection := filepath.Join(t.TempDir(), "selection.json")
	if err := run("select", "--session", uid, "--session", uid, "--out", selection); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(selection)
	var selected catalog.SessionSelection
	if json.Unmarshal(raw, &selected) != nil || !selected.Complete || len(selected.UIDs) != 1 {
		t.Fatalf("selection %s", raw)
	}
	planFile := filepath.Join(t.TempDir(), "plan.json")
	if err := run("plan", "--sessions-file", selection, "--add", "code-reviews,project", "--propose", "code-reviews", "--from", "default", "--out", planFile); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(planFile)
	var plan catalog.BayMigrationPlan
	if json.Unmarshal(raw, &plan) != nil || plan.Changed != 1 {
		t.Fatalf("plan %s", raw)
	}
	if st, err := os.Stat(planFile); err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("plan mode %v %v", st, err)
	}
	if err := run("apply-plan", planFile); err == nil {
		t.Fatal("applied without confirmation")
	}
	if err := run("apply-plan", planFile, "--yes"); err == nil {
		t.Fatal("created bay without explicit creation")
	}
	if err := run("apply-plan", planFile, "--yes", "--create-proposed"); err != nil {
		t.Fatal(err)
	}
	var receipt catalog.BayMigrationReceipt
	if json.Unmarshal(out.Bytes(), &receipt) != nil || !receipt.Complete || len(receipt.CreatedBays) != 1 {
		t.Fatalf("receipt %s", out.String())
	}
	if err := run("verify-plan", plan.ID); err != nil || !strings.Contains(out.String(), `"matches_receipt":true`) {
		t.Fatalf("verify %v %s", err, out.String())
	}
	if err := run("apply-plan", planFile, "--yes", "--create-proposed"); err != nil || !strings.Contains(out.String(), `"already_applied":true`) {
		t.Fatalf("retry %v %s", err, out.String())
	}
	inverse := filepath.Join(t.TempDir(), "inverse.json")
	if err := run("recover-plan", plan.ID, "--out", inverse); err != nil {
		t.Fatal(err)
	}
	if err := run("apply-plan", inverse, "--yes"); err != nil {
		t.Fatal(err)
	}
	bays, err := lake.Catalog.SessionBays(t.Context(), uid)
	if err != nil || len(bays) != 1 || bays[0] != catalog.DefaultBayID {
		t.Fatalf("recovery %v %v", bays, err)
	}
	if err := run("preview-rule", "--harness", "codex", "--to", "future-reviews", "--proposed-target", "--rule-action", "add"); err != nil || !strings.Contains(out.String(), `"complete":true`) {
		t.Fatalf("rule %v %s", err, out.String())
	}
}

func TestBayMigrationCLIRejectsIncompleteInputsAndIgnoredFlags(t *testing.T) {
	lake, _, _ := mcpLake(t)
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: &bytes.Buffer{}}
	for _, args := range [][]string{
		{"plan", "--harness", "claude", "--add", "default"},
		{"select", "--add", "default"},
		{"apply-plan", "anything", "--yes", "--add", "default"},
		{"verify-plan", "anything", "--session", "uid"},
		{"preview-rule", "--session", "uid", "--to", "default", "--rule-action", "add"},
	} {
		full := append(append([]string{"serve", "bays"}, args...), "--data", filepath.Dir(lake.Normalized))
		if err := Run(full, env); err == nil {
			t.Fatalf("accepted irrelevant flags %v", args)
		}
	}
	path := filepath.Join(t.TempDir(), "incomplete.json")
	if err := os.WriteFile(path, []byte(`{"session_uids":["uid"],"complete":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionIDs(path); err == nil {
		t.Fatal("incomplete selection accepted")
	}
	if err := os.WriteFile(path, []byte(`{"items":[{"session_uid":"uid"}],"next_cursor":"more"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionIDs(path); err == nil {
		t.Fatal("discovery page accepted as complete selection")
	}
	for _, sub := range []string{"select", "plan", "apply-plan", "verify-plan", "recover-plan", "preview-rule"} {
		out.Reset()
		if err := Run([]string{"serve", "bays", sub, "--help"}, env); err != nil || !strings.Contains(out.String(), "24 hours") {
			t.Fatalf("help %s %v", sub, err)
		}
	}
}
