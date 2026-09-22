package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
)

const loginUsage = `terva-lampi login — write a device token file

usage:
  terva-lampi login [--token-file PATH]

Writes a 256-bit token to the token file (0600). The token is not an
argument and is not printed. If stdin is not a terminal and is non-empty,
that single line is stored instead of a generated token.

Point ` + "`terva-lampi serve --token-file`" + ` at a copy of the file on the
lake host. serve hashes each device token and rewrites that copy, so
keep this file as the client's secret. The token is not an argument.
`

func runLogin(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), loginUsage)
		return nil
	}
	var tokenFlag string
	rest, err := parseFlags(env, args, loginUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&tokenFlag, "token-file", "", "where to write the token")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), loginUsage)
		return fmt.Errorf("login does not take a token argument; pass --token-file or pipe the token on stdin")
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	path, err := tokenPathFor(env, tokenFlag, file)
	if err != nil {
		return err
	}
	token, piped, err := stdinToken(env.stdin())
	if err != nil {
		return err
	}
	if !piped {
		token, err = auth.Generate()
		if err != nil {
			return err
		}
	}
	if err := auth.Write(path, token); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "wrote device token to %s\n", path)
	fmt.Fprintln(env.stdout(), "the token is not printed. Copy this file to the lake host and pass the copy to `terva-lampi serve --token-file`. serve stores a hash of it.")
	return nil
}

// tokenPathFor picks the token file: the flag, else config.json, else the
// default path under the config dir. It does not require the file to exist.
func tokenPathFor(env Env, flagPath string, file config.File) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if file.TokenFile != "" {
		return file.TokenFile, nil
	}
	return config.TokenPath(env.getenv)
}

// resolveToken reads the token when a file is configured or already
// present. A missing default file means "no auth", which matches a
// loopback lake started without --token-file.
func resolveToken(env Env, flagPath string, file config.File) (string, error) {
	path, err := tokenPathFor(env, flagPath, file)
	if err != nil {
		return "", err
	}
	if flagPath == "" && file.TokenFile == "" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", nil
		}
	}
	return auth.Read(path)
}

func stdinToken(r io.Reader) (string, bool, error) {
	if r == nil {
		return "", false, nil
	}
	if f, ok := r.(*os.File); ok {
		st, err := f.Stat()
		if err != nil {
			return "", false, err
		}
		if st.Mode()&os.ModeCharDevice != 0 {
			return "", false, nil
		}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", false, err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", false, nil
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", false, fmt.Errorf("token on stdin must be a single line")
	}
	return token, true, nil
}
