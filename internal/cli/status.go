package cli

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/discover"
)

const statusUsage = `terva-lampi status — machine identity and lake health

usage:
  terva-lampi status [--server URL] [--token-file PATH]

Prints the local machine id (creating it if needed), where terva sessions
are read from, and whether GET /healthz on the lake answers. healthz
carries no catalog data and does not need the token.
`

func runStatus(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), statusUsage)
		return nil
	}
	var serverFlag, tokenFlag string
	rest, err := parseFlags(env, args, statusUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&serverFlag, "server", "", "lake base URL")
		fs.StringVar(&tokenFlag, "token-file", "", "device token file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), statusUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return err
	}
	home, err := discover.TervaHome(env.getenv)
	if err != nil {
		return err
	}
	files, err := discover.Sessions(home)
	if err != nil {
		return err
	}
	server := config.ServerURL(file, serverFlag)
	tokenPath, err := tokenPathFor(env, tokenFlag, file)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", m.MachineID)
	if m.Hostname != "" {
		fmt.Fprintf(env.stdout(), "hostname: %s\n", m.Hostname)
	}
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", home)
	fmt.Fprintf(env.stdout(), "sessions: %d\n", len(files))
	fmt.Fprintf(env.stdout(), "server: %s\n", server)
	fmt.Fprintf(env.stdout(), "token_file: %s\n", tokenPath)
	fmt.Fprintf(env.stdout(), "health: %s\n", probeHealth(server))
	return nil
}

func probeHealth(server string) string {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(server, "/") + "/healthz")
	if err != nil {
		return "unreachable (" + err.Error() + ")"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("http %d", resp.StatusCode)
	}
	if strings.Contains(string(body), `"ok"`) {
		return "ok"
	}
	return "http 200"
}
