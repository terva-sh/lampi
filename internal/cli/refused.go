package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

const agentRefusedUsage = `terva-lampi agent refused — which projects stay on this machine

usage:
  terva-lampi agent refused [--lake NAME]

Reads every session the agent would read and lists each project the
allowlist refuses, most sessions first: the session count, the
harnesses, the reason, the cwd and the git remote. A project with a
git remote is one line however many checkouts it has, since allow rules
name repositories; the cwd column then shows one checkout and how many
more there are. Checkouts refused for different reasons are separate
lines. A project without a remote is its cwd. The reason is one
of: a deny rule matches, the allow list is empty, the session has no
cwd, or no allow rule matches. The rules are each lake's effective
rules: config.json with that lake's cached profile applied. With
several lakes and no --lake, each lake gets its own list. With no lake,
the rules in config.json are used.

It prints to this terminal only. Nothing is uploaded, no lake is
contacted, and no sync state is read or written. To allow a project,
add a rule to projects.allow: a cwd_prefix names a path on this
machine, and a git_remote_prefix covers every repository of an owner
on every machine.
`

func runAgentRefused(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), agentRefusedUsage)
		return nil
	}
	var lakeFlag string
	rest, err := parseFlags(env, args, agentRefusedUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lakeFlag, "lake", "", "lake name from config.json")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), agentRefusedUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	cc, err := loadClientConfig(env, env.stderr(), config.LakeFlags{Lake: lakeFlag})
	if err != nil {
		return err
	}
	opt, err := readOnlyOptions(env, cc.file.Harnesses)
	if err != nil {
		return err
	}
	type ruleset struct {
		label    string
		projects config.Projects
	}
	var sets []ruleset
	for _, l := range cc.lakes {
		sets = append(sets, ruleset{"lake " + l.Name, l.Projects})
	}
	if len(sets) == 0 {
		if lakeFlag != "" {
			return errNoLake
		}
		sets = []ruleset{{"config.json", cc.file.Projects}}
	}
	var skippedOnce []string
	for i, set := range sets {
		opt.Projects = set.projects
		projects, skipped := upload.Refusals(opt)
		if i == 0 {
			skippedOnce = skipped
		} else {
			fmt.Fprintln(env.stdout())
		}
		writeRefused(env.stdout(), set.label, projects)
	}
	for _, s := range skippedOnce {
		fmt.Fprintf(env.stderr(), "terva-lampi: skipped %s\n", s)
	}
	return nil
}

// readOnlyOptions reads every session the agent would read, for a report
// that uploads nothing and reads no sync state.
func readOnlyOptions(env Env, harnesses config.Harnesses) (upload.Options, error) {
	src, err := sources(env.getenv, harnesses)
	if err != nil {
		return upload.Options{}, err
	}
	return upload.Options{
		TervaHome:     homeOf(src, protocol.HarnessTerva),
		ClaudeHome:    homeOf(src, protocol.HarnessClaude),
		CodexHome:     homeOf(src, protocol.HarnessCodex),
		OpenCodeHome:  homeOf(src, protocol.HarnessOpenCode),
		CursorHome:    homeOf(src, protocol.HarnessCursor),
		CursorCLIHome: homeOf(src, protocol.HarnessCursorCLI),
		GrokHome:      homeOf(src, protocol.HarnessGrok),
		GrokBotHome:   homeOf(src, protocol.HarnessGrokBot),
		// Manifests carry a machine id, and nothing here leaves the
		// machine, so a fixed one avoids creating the real one.
		MachineID: "refused-report",
	}, nil
}

func writeRefused(w io.Writer, label string, projects []upload.RefusedProject) {
	n := 0
	for _, p := range projects {
		n += p.Sessions
	}
	if len(projects) == 0 {
		fmt.Fprintf(w, "%s: nothing is refused\n", label)
		return
	}
	fmt.Fprintf(w, "%s: %d sessions refused in %d projects\n", label, n, len(projects))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSIONS\tHARNESS\tREASON\tCWD\tGIT REMOTE")
	for _, p := range projects {
		cwd, remote := p.CWD, p.GitRemote
		if cwd == "" {
			cwd = "-"
		}
		if p.CWDs > 1 {
			cwd = fmt.Sprintf("%s (+%d more)", cwd, p.CWDs-1)
		}
		if remote == "" {
			remote = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", p.Sessions, strings.Join(p.Harnesses, ","), p.Reason, cwd, remote)
	}
	tw.Flush()
}
