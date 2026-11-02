package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

func TestBayDoctorCLIRemoteHostParityAndPrivateOutput(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	ctx := t.Context()
	token := tokenFile(t, lake, catalog.PermEventsRead)
	page, err := lake.Catalog.DiscoverSessions(ctx, catalog.AllBays(), catalog.DiscoveryRequest{})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(err, page)
	}
	uid := page.Items[0].UID
	// Fixture publication omits directory metadata; a hash mapping exercises
	// explicit missing evidence without requiring content or a search result.
	p := baydoctor.Policy{Mappings: []baydoctor.Mapping{{ID: "fixture", Match: config.ProjectMatch{CWDPrefix: "/verified/reviews"}, Destinations: []string{"code-reviews"}, Proposed: []string{"code-reviews"}, Reason: "Verified fixture directory"}}}
	policyFile := filepath.Join(t.TempDir(), "policy.json")
	raw, _ := json.Marshal(p)
	if os.WriteFile(policyFile, raw, 0600) != nil {
		t.Fatal("policy write")
	}
	var out, stderr bytes.Buffer
	env := Env{Stdout: &out, Stderr: &stderr}
	var reports []baydoctor.Report
	for _, prefix := range [][]string{{"serve", "bays", "doctor", "--data", filepath.Dir(lake.Normalized)}, {"query", "recommend-bays", "--server", srv.URL, "--token-file", token}} {
		out.Reset()
		args := append(append([]string{}, prefix...), "--policy", policyFile, "--json", "--session", uid)
		if err := Run(args, env); err != nil {
			t.Fatal(err, stderr.String())
		}
		var r baydoctor.Report
		if json.Unmarshal(out.Bytes(), &r) != nil || r.Inspected != 1 || !r.Complete || len(r.Findings) != 0 || r.Unmatched != 1 {
			t.Fatal("report", out.String())
		}
		reports = append(reports, r)
	}
	reports[0].AsOf = reports[1].AsOf
	a, _ := json.Marshal(reports[0])
	b, _ := json.Marshal(reports[1])
	if string(a) != string(b) {
		t.Fatal("host remote mismatch", string(a), string(b))
	}
	reportFile := filepath.Join(t.TempDir(), "report.json")
	if err := Run([]string{"query", "recommend-bays", "--server", srv.URL, "--token-file", token, "--policy", policyFile, "--out", reportFile}, env); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(reportFile); err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private report", st, err)
	}
	out.Reset()
	if err := Run([]string{"serve", "bays", "doctor", "--data", filepath.Dir(lake.Normalized), "--policy", policyFile}, env); err != nil || !strings.Contains(out.String(), "bay doctor mapped-metadata-v1") {
		t.Fatal("human output", err, out.String())
	}
	for _, args := range [][]string{{"serve", "bays", "doctor", "--write"}, {"query", "recommend-bays", "--policy", policyFile, "--session", uid, "--harness", "claude", "--server", srv.URL, "--token-file", token}} {
		if err := Run(args, env); err == nil {
			t.Fatal("invalid accepted", args)
		}
	}
	for _, args := range [][]string{{"serve", "bays", "doctor", "--help"}, {"query", "recommend-bays", "--help"}} {
		out.Reset()
		if err := Run(args, env); err != nil || !strings.Contains(out.String(), "No content is read") {
			t.Fatal("help", err)
		}
	}
}
