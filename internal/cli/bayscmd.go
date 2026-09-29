package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/config"
)

const baysCmdUsage = `terva-lampi bays — which bays this machine's sessions ask for

usage:
  terva-lampi bays which [PATH] [--harness H]

A lake can be split into bays (docs/policy.md#bays). Each lake in
config.json can ask for bays for a session: lakes.NAME.bays.rules name
bays for the sessions they match, and lakes.NAME.bays.default names the
bays for a session no rule matches. The lake decides. It places a
session only in the bays this device may write, and applies its own
rules.

which prints, for each lake, whether a session started at PATH would
upload there, the bays it would ask for, and the rule or default that
named each. PATH defaults to the current directory. A rule that names a
harness matches only with --harness.
`

func runBays(env Env, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(env.stdout(), baysCmdUsage)
		return nil
	}
	sub, args := args[0], args[1:]
	if sub != "which" {
		fmt.Fprint(env.stdout(), baysCmdUsage)
		return fmt.Errorf("unknown bays command %q", sub)
	}
	var path string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		path, args = args[0], args[1:]
	}
	var harness string
	rest, err := parseFlags(env, args, baysCmdUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&harness, "harness", "", "the session's harness, for rules that name one")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), baysCmdUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if path == "" {
		if path, err = os.Getwd(); err != nil {
			return err
		}
	}
	if path, err = filepath.Abs(path); err != nil {
		return err
	}
	cc, err := loadClientConfig(env, env.stderr(), config.LakeFlags{})
	if err != nil {
		return err
	}
	if len(cc.lakes) == 0 {
		return errors.New("no lake is configured; terva-lampi register adds one")
	}
	p := adapter.ProjectAt(path)
	id := config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote, NoRepo: p.GitRemote == "" && adapter.OutsideCheckout(p.CWD)}
	fmt.Fprintf(env.stdout(), "%s", path)
	if p.GitRemote != "" {
		fmt.Fprintf(env.stdout(), " (%s)", p.GitRemote)
	}
	fmt.Fprintln(env.stdout())
	for _, l := range cc.lakes {
		// The bays are named for a refused project too, so a rule can be
		// checked before the project is allowed (review 1440).
		c := l.Bays.For(harness, id)
		why := l.Projects.Refusal(id)
		asks := "asks for"
		switch {
		case why != "" && len(c.Bays) == 0:
			fmt.Fprintf(env.stdout(), "  %s: does not upload: %s\n", l.Name, why)
			continue
		case why != "":
			fmt.Fprintf(env.stdout(), "  %s: does not upload: %s\n", l.Name, why)
			asks = "would ask for"
		case len(c.Bays) == 0:
			fmt.Fprintf(env.stdout(), "  %s: asks for no bay; the lake's rules place it, or its default bay\n", l.Name)
			continue
		}
		if why != "" {
			fmt.Fprintf(env.stdout(), "    %s %s\n", asks, strings.Join(c.Bays, ", "))
		} else {
			fmt.Fprintf(env.stdout(), "  %s: %s %s\n", l.Name, asks, strings.Join(c.Bays, ", "))
		}
		if c.Default {
			fmt.Fprintf(env.stdout(), "    bays.default: %s\n", strings.Join(l.Bays.Default, ", "))
		}
		for _, i := range c.Rules {
			fmt.Fprintf(env.stdout(), "    bays.rules[%s] %s: %s\n", strconv.Itoa(i), describeBayRule(l.Bays.Rules[i]), strings.Join(l.Bays.Rules[i].Bays, ", "))
		}
	}
	return nil
}

func describeBayRule(r config.BayRequestRule) string {
	var parts []string
	for _, f := range []struct{ key, v string }{
		{"cwd_prefix", r.CWDPrefix}, {"cwd_glob", r.CWDGlob}, {"cwd_hash", r.CWDHash},
		{"git_remote", r.GitRemote}, {"git_remote_prefix", r.GitRemotePrefix}, {"harness", r.Harness},
	} {
		if f.v != "" {
			parts = append(parts, f.key+"="+f.v)
		}
	}
	return strings.Join(parts, " ")
}
