package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
)

const bayDoctorUsage = `terva-lampi — advisory bay destination doctor

usage:
  terva-lampi serve bays doctor --data DIR --policy FILE [--json] [--out FILE]
  terva-lampi query recommend-bays --policy FILE [--server URL | --lake NAME]
                                  [--token-file FILE] [--json] [--out FILE]

Both accept --session UID (repeatable), --bays ID,NAME, --harness H,
--cwd-prefix PATH, --cwd-hash HASH, --project ID, --unlinked, --machine ID,
--updated-since TIME and --updated-until TIME. Exact UIDs cannot mix with
metadata filters. Output is human readable unless --json or --out is given.
The report is bounded to 500 sessions and marks incomplete inventories.

Policy JSON: {"mappings":[{"id":"review-layout","match":{"cwd_prefix":
"/verified/review/runs"},"harness":"claude","destinations":["code-reviews"],
"proposed_bays":["code-reviews"],"reason":"Operator-verified layout"}]}.
Directory/hash/glob evidence and a reason are required; repository/harness
alone cannot classify purpose. Multiple destinations are compatible additions;
matching mappings sharing alternative_group abstain as conflicting choices.
Policy ignore entries name finding_id, scope, version and until (RFC3339).
Feedback is private, request-local and expires; retain it in your policy file.

No content is read or purpose inferred. Inspect reviewer/rater companions and
development controls. Findings expose uncertainty and preview_request for the
same exact migration service; choose move sources explicitly when reviewing.
This doctor never writes memberships, profiles, bays, routes or grants and
has no --write or safe-fix mode. See docs/bay-doctor.md.
`

func runBayDoctor(env Env, args []string, remote bool) error {
	var data, policyFile, outPath, server, lake, tokenFile, bays string
	var asJSON bool
	var filter catalog.DiscoveryRequest
	var ids []string
	rest, err := parseFlags(env, args, bayDoctorUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory")
		fs.StringVar(&policyFile, "policy", "", "private mappings and scoped ignore feedback JSON")
		fs.StringVar(&outPath, "out", "", "private JSON report file")
		fs.BoolVar(&asJSON, "json", false, "JSON report")
		fs.StringVar(&server, "server", "", "lake URL")
		fs.StringVar(&lake, "lake", "", "configured lake name")
		fs.StringVar(&tokenFile, "token-file", "", "events:read token file")
		fs.StringVar(&bays, "bays", "", "any selected visible bays")
		fs.Func("session", "exact UID (repeatable)", func(v string) error { ids = append(ids, v); return nil })
		fs.StringVar(&filter.Harness, "harness", "", "capture harness")
		fs.StringVar(&filter.Project, "project", "", "project ID")
		fs.BoolVar(&filter.Unlinked, "unlinked", false, "no project ID")
		fs.StringVar(&filter.CWDPrefix, "cwd-prefix", "", "directory and descendants")
		fs.StringVar(&filter.CWDHash, "cwd-hash", "", "exact directory hash")
		fs.StringVar(&filter.Machine, "machine", "", "provenance machine ID")
		fs.StringVar(&filter.UpdatedSince, "updated-since", "", "head updated on/after UTC time/date")
		fs.StringVar(&filter.UpdatedUntil, "updated-until", "", "head updated before UTC time or through date")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 || policyFile == "" {
		return fmt.Errorf("doctor requires --policy FILE and no positional arguments")
	}
	if remote && data != "" || !remote && (server != "" || lake != "" || tokenFile != "") {
		return fmt.Errorf("use --data for host doctor, --server/--lake/--token-file for remote recommendations")
	}
	raw, err := os.ReadFile(policyFile)
	if err != nil {
		return err
	}
	policy, err := baydoctor.DecodePolicy(raw)
	if err != nil {
		return err
	}
	req := baydoctor.Request{Policy: policy, UIDs: ids, Bays: bays}
	if filter != (catalog.DiscoveryRequest{}) {
		req.Filter = &filter
	}
	var report baydoctor.Report
	if remote {
		server, token, err := queryTarget(env, lake, server, tokenFile)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(req)
		answer, err := fetchReadBody(server, token, "/api/read/v1/recommend-bays", url.Values{}, body, queryHintMetadata)
		if err != nil {
			return err
		}
		if json.Unmarshal(answer, &report) != nil {
			return fmt.Errorf("query: unreadable bay doctor report")
		}
	} else {
		data, err = lakeDir(env, data)
		if err != nil {
			return err
		}
		cat, err := catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
		if err != nil {
			return err
		}
		defer cat.Close()
		ctx, cancel := context.WithTimeout(context.Background(), queryToolsTimeout)
		defer cancel()
		report, err = baydoctor.Recommend(ctx, cat, catalog.AllBays(), req, time.Now())
		if err != nil {
			return err
		}
	}
	if asJSON || outPath != "" {
		var out bytes.Buffer
		if err := json.NewEncoder(&out).Encode(report); err != nil {
			return err
		}
		return writeQueryOut(env, outPath, &out)
	}
	fmt.Fprintf(env.stdout(), "bay doctor %s: inspected %d; complete=%t; %d suggestions; %d already placed; %d suppressed\n", report.Version, report.Inspected, report.Complete, len(report.Findings), report.AlreadyPlaced, report.Suppressed)
	for _, w := range report.Warnings {
		fmt.Fprintln(env.stdout(), w)
	}
	for _, f := range report.Findings {
		fmt.Fprintf(env.stdout(), "%s %s: %s -> %v\n  %s\n", f.Code, f.Status, f.UID, f.Destinations, f.Uncertainty)
		for _, e := range f.Evidence {
			fmt.Fprintln(env.stdout(), "  "+e)
		}
	}
	for _, a := range report.Abstentions {
		fmt.Fprintf(env.stdout(), "abstain %s: %s\n", a.UID, a.Code)
	}
	fmt.Fprintln(env.stdout(), "Use --json or --out FILE for scoped finding IDs, dismissal feedback and exact preview_request handoffs. Review add versus explicit-source move separately.")
	return nil
}
