package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// doctorProfile prints what profiledoctor finds in profile name, and
// with write, writes the profile with the safe fixes applied.
func doctorProfile(ctx context.Context, env Env, cat *catalog.Catalog, name string, asJSON bool, write string) error {
	var p config.Profile
	stored, err := cat.ProfileByName(ctx, name)
	switch {
	case errors.Is(err, catalog.ErrNoProfile) && name == config.DefaultProfile:
		// Devices get an empty default; the doctor reads it as that.
	case errors.Is(err, catalog.ErrNoProfile):
		return fmt.Errorf("no profile named %s; serve profiles list shows them", name)
	case err != nil:
		return err
	default:
		p = stored.Config
	}
	in, err := profiledoctor.Gather(ctx, cat, name, p.Projects)
	if err != nil {
		return err
	}
	rep := profiledoctor.Diagnose(in)
	if write != "" {
		fixed := p
		fixed.Projects, _ = profiledoctor.ApplySafe(p.Projects, rep)
		doc, err := json.MarshalIndent(fixed, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(write, append(doc, '\n'), 0o600); err != nil {
			return fmt.Errorf("serve profiles doctor: %w", err)
		}
	}
	if asJSON {
		enc := json.NewEncoder(env.stdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printReport(env, name, in, rep, write)
	return nil
}

func printReport(env Env, name string, in profiledoctor.Input, rep profiledoctor.Report, write string) {
	out := env.stdout()
	fmt.Fprintf(out, "profile %s: %d of %d allow rules, %d deny rules; %d projects wait in review\n", name, rep.Allow, rep.Cap, rep.Deny, rep.Pending)
	var names []string
	for _, d := range in.Devices {
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "no active device takes this profile's allow rules, so nothing is known of what they match")
	} else {
		fmt.Fprintf(out, "devices: %s\n", strings.Join(names, ", "))
	}
	if len(rep.Findings) == 0 {
		fmt.Fprintln(out, "nothing to change")
		return
	}
	fmt.Fprintln(out)
	for _, f := range rep.Findings {
		what := f.Group
		if f.Rule != nil {
			what = fmt.Sprintf("allow[%d] %s", f.Index, profiledoctor.RuleText(*f.Rule))
		}
		fmt.Fprintf(out, "%-8s %-16s %s\n", f.Level, f.Code, what)
		fmt.Fprintf(out, "         %s\n", f.Message)
		if f.By != nil {
			fmt.Fprintf(out, "         by %s\n", profiledoctor.RuleText(*f.By))
		}
		if f.Fix == profiledoctor.FixNarrow {
			var with []string
			for _, r := range f.Replace {
				with = append(with, profiledoctor.RuleText(r))
			}
			if len(with) == 0 {
				fmt.Fprintln(out, "         fix: remove it")
			} else {
				fmt.Fprintf(out, "         fix: replace it with %s\n", strings.Join(with, "; "))
			}
			if len(f.Hide) > 0 {
				fmt.Fprintf(out, "         and hide %d %s from review\n", len(f.Hide), plural(len(f.Hide), "project", "projects"))
			}
		}
	}
	fmt.Fprintln(out)
	n := rep.Safe()
	switch {
	case n == 0:
		fmt.Fprintln(out, "no safe fixes; the dashboard's profile editor offers the rest with a preview")
	case write != "":
		fmt.Fprintf(out, "wrote %s with %d safe %s applied; check it, then: terva-lampi serve profiles set %s %s --note \"...\"\n", write, n, plural(n, "fix", "fixes"), name, write)
	default:
		fmt.Fprintf(out, "%d safe %s no project's verdict; --write FILE writes the profile with %s applied\n", n, plural(n, "fix changes", "fixes change"), plural(n, "it", "them"))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
