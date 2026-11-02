package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

const bayMigrationUsage = `terva-lampi serve bays — reviewed session selections and multi-bay plans

usage:
  terva-lampi serve bays select [--session UID ... | --sessions-file FILE | filters]
                               [--exclude UID ...] [--bays ID,NAME] [--out FILE]
  terva-lampi serve bays plan [--session UID ... | --sessions-file FILE]
                             [--add BAY,...] [--from BAY,...] [--propose NAME ...]
                             [--exclude UID ...] [--reason TEXT] [--out FILE]
  terva-lampi serve bays plan --request FILE [--out FILE]
  terva-lampi serve bays apply-plan FILE --yes [--create-proposed]
  terva-lampi serve bays verify-plan PLAN_ID [--out FILE]
  terva-lampi serve bays recover-plan PLAN_ID [--out FILE]
  terva-lampi serve bays preview-rule --to BAY [--proposed-target]
                                     --rule-action add|deny|hold MATCH... [--out FILE]

All accept --data DIR. JSON output defaults to stdout; --out is atomic, mode
0600. A sessions file is one exact UID per line, or the complete JSON object
from select (session_uids and complete=true). No transcript text is parsed.

select enumerates a complete bounded metadata snapshot, freezes exact IDs and
applies deduplication and explicit exclusions. Filters combine with AND: --harness, --project,
--unlinked, --cwd-prefix, --cwd-hash, --machine. --bays means any selected bay,
intersected with host access. Limit 500 candidates per atomic batch; larger
selections fail instead of sampling. Split them explicitly.

plan only previews. --add can name several bays; omit --from to retain all
existing memberships. --from removes only those named sources, preserving
unrelated bays. IDs, names and aliases resolve before conflicts are checked.
--propose NAME includes an uncreated destination; add it to --add too.
--request accepts a documented BayMigrationRequest JSON object for exact
per-session edits, including recovery plans. A filtered select output must
be frozen and reviewed before plan/apply; apply never reruns discovery.

Review every before/after row, no-op, rejected item and access warning.
apply-plan requires current host authority, a full administrator preview,
matching membership/routing/destination/grant state and an unexpired plan
(24 hours). Held or flagged items block the whole atomic batch. New bay
creation requires --create-proposed; no grants are created. --yes confirms
this exact reviewed artifact. Retries return the stored receipt without
replaying changes. Receipts persist in the catalog and all changes are audited.

verify-plan compares actual memberships with the receipt. recover-plan emits
a fresh inverse preview, retaining unrelated later memberships and refusing
touched memberships changed since apply. Review and apply it separately.
Newly created bays remain after recovery; grants are never restored.

preview-rule is hypothetical and writes nothing. MATCH accepts --cwd-prefix,
--cwd-hash, --cwd-glob, --git-remote, --git-remote-prefix and --harness.
Its complete catchment includes all metadata matches, independently of a
historical UID selection. Installing a future rule remains a separate serve
bays rule operation. Later manifests/requests/rules may re-add memberships.
Removing a membership cannot retract content already read.
`

func runBayMigration(env Env, sub string, args []string) error {
	var data, outPath, position, input, requestPath, add, from, bays, reason, to, action string
	var yes, create, proposedTarget bool
	var ids, exclude, proposed []string
	var filter catalog.DiscoveryRequest
	var match config.ProjectMatch
	for {
		rest, err := parseFlags(env, args, bayMigrationUsage, func(fs *flag.FlagSet) {
			fs.StringVar(&data, "data", data, "lake directory")
			fs.StringVar(&outPath, "out", outPath, "JSON output file")
			fs.StringVar(&input, "sessions-file", input, "exact UID lines or complete selection JSON")
			fs.StringVar(&requestPath, "request", requestPath, "migration request JSON")
			fs.Func("session", "include exact session UID (repeatable)", func(v string) error { ids = append(ids, v); return nil })
			fs.Func("exclude", "exclude exact session UID (repeatable)", func(v string) error { exclude = append(exclude, v); return nil })
			fs.Func("propose", "propose new destination name (repeatable)", func(v string) error { proposed = append(proposed, v); return nil })
			fs.StringVar(&add, "add", add, "destination bays, comma-separated")
			fs.StringVar(&from, "from", from, "explicit source bays, comma-separated")
			fs.StringVar(&bays, "bays", bays, "select any bays, comma-separated")
			fs.StringVar(&reason, "reason", reason, "why these exact sessions were selected")
			fs.StringVar(&to, "to", to, "hypothetical routing destination")
			fs.StringVar(&action, "rule-action", action, "hypothetical add, deny or hold")
			fs.BoolVar(&yes, "yes", yes, "apply this reviewed plan")
			fs.BoolVar(&create, "create-proposed", create, "explicitly create proposed destination bays")
			fs.BoolVar(&proposedTarget, "proposed-target", proposedTarget, "hypothetical bay does not exist")
			fs.StringVar(&filter.Harness, "harness", filter.Harness, "match harness")
			fs.StringVar(&filter.Project, "project", filter.Project, "match project ID")
			fs.BoolVar(&filter.Unlinked, "unlinked", filter.Unlinked, "match sessions without project ID")
			fs.StringVar(&filter.CWDPrefix, "cwd-prefix", filter.CWDPrefix, "directory and descendants")
			fs.StringVar(&filter.CWDHash, "cwd-hash", filter.CWDHash, "exact directory hash")
			fs.StringVar(&filter.Machine, "machine", filter.Machine, "provenance machine ID")
			fs.StringVar(&match.GitRemote, "git-remote", match.GitRemote, "hypothetical exact remote")
			fs.StringVar(&match.GitRemotePrefix, "git-remote-prefix", match.GitRemotePrefix, "hypothetical remote prefix")
			fs.StringVar(&match.CWDGlob, "cwd-glob", match.CWDGlob, "hypothetical directory layout")
		})
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			break
		}
		if position != "" || sub != "apply-plan" && sub != "verify-plan" && sub != "recover-plan" {
			return fmt.Errorf("unexpected argument %q", rest[0])
		}
		position, args = rest[0], rest[1:]
	}
	selectionFlags := len(ids) > 0 || len(exclude) > 0 || input != "" || filter != (catalog.DiscoveryRequest{})
	editFlags := add != "" || from != "" || len(proposed) > 0 || reason != "" || requestPath != ""
	ruleFlags := to != "" || action != "" || proposedTarget || !match.Empty()
	switch sub {
	case "select":
		if editFlags || ruleFlags || yes || create {
			return fmt.Errorf("select accepts session and metadata selection flags only")
		}
	case "plan":
		if ruleFlags || yes || create || bays != "" {
			return fmt.Errorf("plan accepts exact membership edits; run select for metadata and bay filters first")
		}
	case "apply-plan":
		if selectionFlags || editFlags || ruleFlags || bays != "" {
			return fmt.Errorf("apply-plan uses only the exact reviewed file; edit flags are refused")
		}
	case "verify-plan", "recover-plan":
		if selectionFlags || editFlags || ruleFlags || yes || create || bays != "" {
			return fmt.Errorf("%s accepts a receipt ID and output/data flags only", sub)
		}
	case "preview-rule":
		if len(ids) > 0 || len(exclude) > 0 || input != "" || editFlags || yes || create {
			return fmt.Errorf("preview-rule accepts a hypothetical metadata rule only")
		}
	}

	needsPosition := sub == "apply-plan" || sub == "verify-plan" || sub == "recover-plan"
	if needsPosition && position == "" {
		return fmt.Errorf("%s needs a plan file or ID", sub)
	}
	if sub == "apply-plan" && !yes {
		return fmt.Errorf("apply-plan requires --yes after reviewing the complete plan")
	}
	if data == "" {
		var err error
		data, err = lakeDir(env, data)
		if err != nil {
			return err
		}
	}
	path := filepath.Join(data, "catalog.db")
	var cat *catalog.Catalog
	var err error
	if sub == "apply-plan" {
		cat, err = catalog.OpenCurrent(path)
	} else {
		cat, err = catalog.OpenReadOnly(path)
	}
	if err != nil {
		return err
	}
	defer cat.Close()
	ctx, now := context.Background(), time.Now()
	scope := catalog.AllBays()
	if bays != "" {
		scope, err = cat.SelectReadBays(ctx, scope, bays)
		if err != nil {
			return err
		}
	}
	var result any
	switch sub {
	case "select":
		if input != "" {
			loaded, err := readSessionIDs(input)
			if err != nil {
				return err
			}
			ids = append(ids, loaded...)
		}
		req := catalog.SessionSelectionRequest{UIDs: ids, Exclude: exclude}
		if filter != (catalog.DiscoveryRequest{}) || bays != "" {
			req.Filter = &filter
		}
		result, err = cat.SelectSessions(ctx, scope, req)
	case "plan":
		req := catalog.BayMigrationRequest{UIDs: ids, Exclude: exclude, Add: splitBayRefs(add), Remove: splitBayRefs(from), Proposed: proposed, Reason: reason}
		if input != "" {
			loaded, err := readSessionIDs(input)
			if err != nil {
				return err
			}
			req.UIDs = append(req.UIDs, loaded...)
		}
		if requestPath != "" {
			if len(ids) > 0 || input != "" || add != "" || from != "" || len(proposed) > 0 || len(exclude) > 0 || reason != "" {
				return fmt.Errorf("--request cannot be combined with migration edit flags")
			}
			if err := readBoundedJSON(requestPath, &req); err != nil {
				return err
			}
		}
		if filter != (catalog.DiscoveryRequest{}) {
			return fmt.Errorf("plan uses exact IDs; run select with filters first")
		}
		result, err = cat.PlanBayMigration(ctx, scope, req, now)
	case "apply-plan":
		var p catalog.BayMigrationPlan
		if err := readBoundedJSON(position, &p); err != nil {
			return err
		}
		result, err = cat.ApplyBayMigration(ctx, scope, true, p, create, "serve bays apply-plan", catalog.ViaCLI, now)
		if err == nil {
			if flushErr := cat.FlushAudit(ctx, data); flushErr != nil {
				return fmt.Errorf("migration committed; audit remains queued: %w", flushErr)
			}
		}
	case "verify-plan":
		result, err = cat.VerifyBayMigration(ctx, scope, position)
	case "recover-plan":
		result, err = cat.RecoverBayMigration(ctx, scope, position, now)
	case "preview-rule":
		match.CWDPrefix, match.CWDHash = filter.CWDPrefix, filter.CWDHash
		if filter.Project != "" || filter.Unlinked || filter.Machine != "" {
			return fmt.Errorf("future routing does not support project IDs, unlinked or machine filters")
		}
		result, err = cat.PreviewBayRule(ctx, scope, catalog.BayRulePreviewRequest{Match: match, Harness: filter.Harness, Action: action, Bay: to, Proposed: proposedTarget}, now)
	}
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(result); err != nil {
		return err
	}
	return writeQueryOut(env, outPath, &out)
}

func splitBayRefs(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

const migrationFileLimit = 8 << 20

func readBoundedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, migrationFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > migrationFileLimit {
		return nil, fmt.Errorf("migration file exceeds 8 MiB")
	}
	return b, nil
}

func readBoundedJSON(path string, v any) error {
	b, err := readBoundedFile(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid migration JSON: %w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("migration file must contain one JSON value")
	}
	return nil
}

func readSessionIDs(path string) ([]string, error) {
	b, err := readBoundedFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		var selection catalog.SessionSelection
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&selection); err != nil {
			return nil, err
		}
		if !selection.Complete || d.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("selection is incomplete or malformed; consume every discovery page first")
		}
		return selection.UIDs, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	ids := []string{}
	for scanner.Scan() {
		uid := scanner.Text()
		if uid != "" {
			ids = append(ids, uid)
		}
	}
	return ids, scanner.Err()
}
