package cli

import (
	"io"
	"strings"
	"testing"
)

func TestDiagnosticOptionsRejectBeforeConfig(t *testing.T) {
	env := Env{Stdout: io.Discard, Stderr: io.Discard, Getenv: func(string) string { t.Fatal("read configuration before rejecting options"); return "" }}
	for _, run := range []func(Env, []string) error{runServe, runAgentDaemon} {
		for _, duration := range []string{"0s", "-1s", "301s"} {
			err := run(env, []string{"--diagnostics-duration", duration})
			if err == nil || !strings.Contains(err.Error(), "duration") {
				t.Fatalf("accepted %s: %v", duration, err)
			}
		}
	}
	for _, args := range [][]string{{"--normalize-workers", "0"}, {"--normalize-workers", "65"}, {"--normalize-input-budget-mib", "0"}} {
		if err := runServe(env, args); err == nil {
			t.Fatal("accepted invalid admission", args)
		}
	}
}
