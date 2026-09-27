package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
	"terva.sh/lampi/internal/upload"
)

const registerUsage = `terva-lampi register — join a lake with a registration code

usage:
  terva-lampi register [--code-file PATH] [--fingerprint SHA256:...]
                       [--lake NAME] [--replace] [--install-service]

The code comes from terva-lampi serve register on the lake host. It is
a secret, so register reads it from --code-file, from stdin, or from a
prompt when stdin is a terminal, and never from an argument.

Before it uses the code, register checks, in order, and stops at the
first check that fails, naming it:

  1. the code's signature verifies against the key it carries, and the
     code has not expired (five minutes of clock skew are allowed);
  2. the URL is https, or http to localhost, 127.0.0.0/8 or ::1;
  3. the key list at that URL, fetched over a fresh nonce, lists the
     code's key as active under the code's lake id and is signed by it;
  4. you confirm the URL, lake id and key fingerprint. On a terminal it
     asks. Otherwise pass --fingerprint with the value serve identity
     prints on the lake host; with neither, register refuses.

Then it makes a device token, redeems the code, and writes the token to
tokens/<name>.token in the config directory (0600; the token is never
printed), the lake into config.json with its pinned lake id and key,
and the lake's base configuration into the lake's state directory. A
running agent is told to reload its lakes; on Windows it must be
restarted.

--lake names the lake in config.json. The default is default when no
default lake is configured, else the first label of the URL's host. A
lake with the code's lake id is already configured: register refuses
and names it, unless --replace, which re-registers that entry under
its name. --replace never takes over an entry for another lake, or a
token file another lake names as its token_file.

--install-service writes and enables the agent's systemd user unit or
launchd agent, running this binary. XDG_CONFIG_HOME, XDG_STATE_HOME
and XDG_DATA_HOME, when set away from their defaults, are set in it
too, so the agent reads what register wrote. On Linux it suggests loginctl
enable-linger when the user has no lingering session.

A lake from before key lists cannot be registered with; upgrade it
first.
`

// codeClockSkew is how far past its expiry a code is still tried. The
// lake is the authority on expiry.
const codeClockSkew = 5 * time.Minute

// terminalStdin reports whether stdin is a terminal. Tests replace it.
var terminalStdin = func(env Env) bool {
	f, ok := env.stdin().(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runRegister(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), registerUsage)
		return nil
	}
	var codeFile, fingerprint, lakeName string
	var replace, install bool
	rest, err := parseFlags(env, args, registerUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&codeFile, "code-file", "", "file holding the registration code")
		fs.StringVar(&fingerprint, "fingerprint", "", "the lake key fingerprint serve identity prints")
		fs.StringVar(&lakeName, "lake", "", "name for the lake in config.json")
		fs.BoolVar(&replace, "replace", false, "re-register a lake already configured")
		fs.BoolVar(&install, "install-service", false, "install and enable the agent's user service")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), registerUsage)
		return errors.New("register does not take the code as an argument; pass --code-file, or give it on stdin")
	}
	if lakeName != "" && !config.ValidLakeName(lakeName) {
		return fmt.Errorf("--lake %q is not a lake name: lowercase letters, digits, '-' and '_', at most 32 characters", lakeName)
	}
	interactive := terminalStdin(env)
	in := bufio.NewReader(env.stdin())
	raw, err := readCode(env, in, codeFile, interactive)
	if err != nil {
		return err
	}
	now := time.Now()
	ctx := context.Background()

	// 1. The code is whole and current.
	c, err := regcode.Decode(raw)
	if err != nil {
		return fmt.Errorf("check 1, the code: %w", err)
	}
	if now.After(c.Expires.Add(codeClockSkew)) {
		return fmt.Errorf("check 1, the code: it expired at %s and this machine's clock says %s; ask for a new code, or fix the clock if it is wrong", c.Expires.Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	// 2. The token will not cross the network in the clear.
	if err := upload.CheckToken(c.URL, "x"); err != nil {
		return fmt.Errorf("check 2, the URL: %s is plain http to a host that is not loopback; the lake must be served over https", c.URL)
	}
	// 3. The lake at that URL holds the code's key.
	key := protocol.LakeKey{ID: c.KeyID, Alg: "ed25519", PublicKey: c.PublicKey}
	if err := verifyKeyList(ctx, c.URL, c.LakeID, key); err != nil {
		if errors.Is(err, upload.ErrNoKeyEndpoint) {
			return fmt.Errorf("check 3, the lake's keys: %w", err)
		}
		return fmt.Errorf("check 3, the lake's keys at %s: %w", c.URL, err)
	}
	// 4. A person says it is the lake they meant.
	fmt.Fprintf(env.stderr(), "lake:        %s\nlake_id:     %s\nfingerprint: %s\n", c.URL, c.LakeID, c.Fingerprint())
	switch {
	case fingerprint != "":
		if !sameFingerprint(fingerprint, c.Fingerprint()) {
			return fmt.Errorf("check 4, the fingerprint: the lake's key is %s, not %s", c.Fingerprint(), fingerprint)
		}
	case interactive:
		fmt.Fprint(env.stderr(), "Compare the fingerprint with serve identity on the lake host. Register this machine with that lake? [y/N] ")
		answer, _ := in.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("check 4, confirmation: not confirmed; nothing was registered")
		}
	default:
		return errors.New("check 4, confirmation: stdin is not a terminal, so pass --fingerprint with the value serve identity prints on the lake host")
	}

	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	lakes, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{})
	if err != nil {
		return err
	}
	var existing *config.Lake
	for i := range lakes {
		if lakes[i].LakeID == c.LakeID {
			existing = &lakes[i]
		}
	}
	switch {
	case existing != nil && !replace:
		return fmt.Errorf("lake %s in config.json is already this lake (%s); pass --replace to register again", existing.Name, c.LakeID)
	case existing != nil && lakeName == "":
		lakeName = existing.Name
	case existing != nil && lakeName != existing.Name:
		// A second name would leave two entries for one lake, the old
		// one still holding its old token.
		return fmt.Errorf("lake %s in config.json is already this lake (%s); --replace registers it again under that name, so drop --lake, or run terva-lampi lakes remove %s first", existing.Name, c.LakeID, existing.Name)
	case lakeName == "":
		lakeName = chooseLakeName(file, lakes, c.URL)
	}
	if lakeName == config.DefaultLake && (file.Server != "" || file.TokenFile != "") {
		return errors.New("config.json sets the default lake with its top-level server or token_file; pass --lake NAME for this one")
	}
	entry := config.LakeConfig{}
	if prev, ok := file.Lakes[lakeName]; ok {
		// --replace takes over the entry that is this lake, or one for
		// the same server that no lake id pins yet. It never takes over
		// another lake's entry, or the token that entry names.
		switch {
		case prev.LakeID != c.LakeID && (prev.LakeID != "" || prev.Server != c.URL):
			return fmt.Errorf("a lake named %s is configured for another lake; pass --lake with another name", lakeName)
		case !replace:
			return fmt.Errorf("a lake named %s is configured for %s with no pinned lake id; pass --replace to register it, or --lake for another name", lakeName, c.URL)
		}
		entry = prev
	}
	// The token always goes to this lake's own file under tokens/. A
	// token_file the replaced entry names may be placed by hand or
	// shared with another lake, so it is left as it is.
	dir, err := config.ConfigDir(env.getenv)
	if err != nil {
		return err
	}
	tokenPath := tokensPath(dir, lakeName)
	// Another lake can name this file as its token_file. Writing it
	// would lock that lake out, or hand it this lake's token.
	if sharers := tokenSharers(lakes, lakeName, tokenPath); len(sharers) > 0 {
		return fmt.Errorf("%s is the token_file of lake %s in config.json, so register does not overwrite it; pass --lake for another name, or give that lake its own token_file", tokenPath, strings.Join(sharers, ", "))
	}
	if _, err := os.Stat(tokenPath); err == nil && !replace {
		return fmt.Errorf("%s exists; pass --replace to overwrite it, or --lake for another name", tokenPath)
	}
	m, err := config.EnsureLakeMachine(env.getenv, lakeName)
	if err != nil {
		return err
	}
	token, err := auth.Generate()
	if err != nil {
		return err
	}
	// The token lands beside its final path first, so a refused code
	// leaves the old token, or none, in place.
	pending := tokenPath + ".pending"
	if err := auth.Write(pending, token); err != nil {
		return err
	}
	host, _ := os.Hostname()
	resp, err := upload.Register(ctx, c.URL, protocol.RegisterRequest{
		Secret: c.Secret, TokenSHA256: auth.HashToken(token), MachineID: m.MachineID, Name: host,
	})
	if err != nil {
		os.Remove(pending)
		return fmt.Errorf("redeeming the code: %w", err)
	}
	if err := os.Rename(pending, tokenPath); err != nil {
		return fmt.Errorf("the lake made device %s, but moving its token into %s failed: %w; the token is in %s", resp.Name, tokenPath, err, pending)
	}

	entry.Server, entry.LakeID, entry.KeyID, entry.PublicKey, entry.TokenFile = c.URL, c.LakeID, c.KeyID, c.PublicKey, tokenPath
	// The device id binds the lake's profiles to this device: one signed
	// for another device on the same lake does not verify here.
	entry.DeviceID = resp.DeviceID
	pinned := config.Lake{Name: lakeName, LakeID: c.LakeID, KeyID: c.KeyID, PublicKey: c.PublicKey, DeviceID: resp.DeviceID}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	if resp.Config != nil {
		d, err := lakeprofile.Verify(resp.Config, pinned)
		if err != nil {
			fmt.Fprintf(env.stderr(), "terva-lampi: the lake's base configuration does not verify, so it is not used: %v\n", err)
		} else if err := lakeprofile.Save(lakestate.Dir(state, lakeName), d); err != nil {
			fmt.Fprintf(env.stderr(), "terva-lampi: saving the base configuration: %v; the agent fetches it again\n", err)
		}
	}
	if err := config.SetLake(env.getenv, lakeName, entry); err != nil {
		return fmt.Errorf("the lake made device %s and its token is in %s, but writing config.json failed: %w", resp.Name, tokenPath, err)
	}
	fmt.Fprintf(env.stdout(), "registered as device %s (%s) with lake %s\n", resp.Name, resp.DeviceID, lakeName)
	fmt.Fprintf(env.stdout(), "token: %s (not printed)\n", tokenPath)
	fmt.Fprintln(env.stdout(), reloadAgent(state))
	if install {
		return installService(env, state)
	}
	return nil
}

// readCode reads the code from --code-file, stdin, or a prompt.
func readCode(env Env, in *bufio.Reader, codeFile string, interactive bool) (string, error) {
	if codeFile != "" {
		raw, err := os.ReadFile(codeFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if interactive {
		fmt.Fprint(env.stderr(), "Paste the registration code: ")
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no code entered")
		}
		return strings.TrimSpace(line), nil
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return "", errors.New("no code on stdin; pipe it in, pass --code-file, or run register on a terminal")
	}
	return strings.TrimSpace(line), nil
}

// sameFingerprint compares fingerprints with or without the SHA256:
// prefix. The base64 is case-sensitive.
func sameFingerprint(a, b string) bool {
	trim := func(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "SHA256:") }
	return trim(a) != "" && trim(a) == trim(b)
}

// chooseLakeName is default when no default lake is configured, else
// the first label of the URL's host, made a lake name and unique.
func chooseLakeName(file config.File, lakes []config.Lake, raw string) string {
	taken := map[string]bool{}
	for _, l := range lakes {
		taken[l.Name] = true
	}
	for name := range file.Lakes {
		taken[name] = true
	}
	// A config with no lakes map resolves to the loopback default lake.
	// Registering replaces that implicit lake, so default is free.
	implicit := file.Lakes == nil && file.Server == "" && file.TokenFile == ""
	if !taken[config.DefaultLake] || implicit {
		return config.DefaultLake
	}
	base := "lake"
	if u, err := url.Parse(raw); err == nil {
		label := strings.ToLower(strings.SplitN(u.Hostname(), ".", 2)[0])
		var b strings.Builder
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				b.WriteRune(r)
			}
		}
		if s := strings.Trim(b.String(), "-_"); s != "" && config.ValidLakeName(s) {
			base = s
		}
	}
	// The suffix is kept inside the 32 characters a lake name may have.
	name := base
	for n := 2; taken[name]; n++ {
		suffix := fmt.Sprintf("-%d", n)
		name = base[:min(len(base), 32-len(suffix))] + suffix
	}
	return name
}
