package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"terva.sh/lampi/internal/identity"
)

// DefaultLake is the name of the lake the legacy top-level server and
// token_file describe, and of the lake a flag or LAMPI_SERVER sets.
const DefaultLake = "default"

// LakeConfig is one entry of the lakes map in config.json. Server is
// required. TokenFile defaults to tokens/<name>.token in the config
// directory, or to the legacy token path for the lake named default.
// LakeID, KeyID and PublicKey are the lake's pinned identity, written by
// register; a hand-written entry may leave them empty. Projects are the
// rules for uploads to this lake only. Top-level projects.deny applies
// to every lake as well.
type LakeConfig struct {
	Server    string   `json:"server"`
	TokenFile string   `json:"token_file,omitempty"`
	LakeID    string   `json:"lake_id,omitempty"`
	KeyID     string   `json:"key_id,omitempty"`
	PublicKey string   `json:"public_key,omitempty"`
	Projects  Projects `json:"projects,omitempty"`
}

// Lake is one resolved lake. Projects is what uploads to it are checked
// against: its own allow rules, and its own deny rules plus the
// top-level ones. Legacy is true for the default lake described by the
// top-level server and token_file, LAMPI_SERVER, or nothing at all.
type Lake struct {
	Name      string
	Server    Setting
	TokenFile Setting
	LakeID    string
	KeyID     string
	PublicKey string
	Projects  Projects
	Legacy    bool
}

// LakeFlags are the command-line inputs to ResolveLakes. Lake selects one
// lake by name. Server and TokenFile override the selected lake's values.
type LakeFlags struct {
	Lake      string
	Server    string
	TokenFile string
}

var lakeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidLakeName reports whether name can key the lakes map: lowercase
// letters, digits, '-' and '_', starting with a letter or digit, at most
// 32 characters. A name is also a file name under tokens/.
func ValidLakeName(name string) bool { return lakeNamePattern.MatchString(name) }

// ResolveLakes turns config.json, the environment and the flags into the
// lakes a command talks to, the default lake first and the rest by name.
//
// A config.json with no lakes map resolves to one lake named default,
// exactly as server and token_file resolved before the map existed, so
// an existing machine needs no edit. With a map, the legacy fields and
// LAMPI_SERVER or LAMPI_TOKEN_FILE still add the default lake. An entry
// named default in the map together with a top-level server or
// token_file is refused, because two places would describe one lake.
//
// Top-level projects.allow belongs to the legacy default lake. Without
// one it has no lake to apply to and is refused, so a rule meant for all
// lakes cannot silently apply to none.
func ResolveLakes(file File, getenv func(string) string, flags LakeFlags) ([]Lake, error) {
	for name, lc := range file.Lakes {
		if err := checkLakeEntry(name, lc); err != nil {
			return nil, err
		}
	}
	_, explicitDefault := file.Lakes[DefaultLake]
	if explicitDefault && (file.Server != "" || file.TokenFile != "") {
		return nil, fmt.Errorf("config: lakes.%s and the top-level server or token_file both describe the default lake; keep one", DefaultLake)
	}
	if flags.Lake != "" && !ValidLakeName(flags.Lake) {
		return nil, fmt.Errorf("--lake %q is not a lake name", flags.Lake)
	}
	// Which lakes exist does not depend on the flags: a flag overrides a
	// lake, it does not add one. The legacy default lake exists with no
	// map at all, or when the top-level fields or the environment name it.
	legacy := !explicitDefault && (len(file.Lakes) == 0 ||
		file.Server != "" || file.TokenFile != "" ||
		getenv("LAMPI_SERVER") != "" || getenv("LAMPI_TOKEN_FILE") != "")
	count := len(file.Lakes)
	if legacy {
		count++
	}
	// Flags apply to the lake --lake names, or to the only lake, and need
	// --lake when there are several.
	target := flags.Lake
	if target == "" && (flags.Server != "" || flags.TokenFile != "") {
		if count > 1 {
			names := []string{}
			if legacy {
				names = append(names, DefaultLake)
			}
			for name := range file.Lakes {
				names = append(names, name)
			}
			sort.Strings(names[boolInt(legacy):])
			return nil, fmt.Errorf("--server and --token-file apply to one lake and %d are configured (%s); pass --lake", count, strings.Join(names, ", "))
		}
		if !legacy {
			for name := range file.Lakes {
				target = name
			}
		}
	}
	if target == "" {
		target = DefaultLake
	}
	flagFor := func(name string) LakeFlags {
		if name == target {
			return flags
		}
		return LakeFlags{}
	}

	var lakes []Lake
	if legacy {
		f := flagFor(DefaultLake)
		tok, err := ResolveTokenFile(file, getenv, f.TokenFile)
		if err != nil {
			return nil, err
		}
		lakes = append(lakes, Lake{
			Name:      DefaultLake,
			Server:    ResolveServer(file, getenv, f.Server),
			TokenFile: tok,
			Projects:  file.Projects,
			Legacy:    true,
		})
	} else if len(file.Projects.Allow) > 0 {
		return nil, fmt.Errorf("config: top-level projects.allow applies to the lake set by the top-level server; with only a lakes map, move each allow rule under lakes.<name>.projects")
	}

	names := make([]string, 0, len(file.Lakes))
	for name := range file.Lakes {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		// The default lake sorts first, the rest by name.
		if (names[i] == DefaultLake) != (names[j] == DefaultLake) {
			return names[i] == DefaultLake
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		lc := file.Lakes[name]
		f := flagFor(name)
		server := pick(f.Server, "", lc.Server, "")
		tokenDefault, err := lakeTokenPath(getenv, name)
		if err != nil {
			return nil, err
		}
		token := pick(f.TokenFile, "", lc.TokenFile, tokenDefault)
		if name == DefaultLake {
			// The environment still names the default lake's server and
			// token, as it does for a legacy config.
			server = pick(f.Server, getenv("LAMPI_SERVER"), lc.Server, "")
			token = pick(f.TokenFile, getenv("LAMPI_TOKEN_FILE"), lc.TokenFile, tokenDefault)
		}
		lakes = append(lakes, Lake{
			Name:      name,
			Server:    server,
			TokenFile: token,
			LakeID:    lc.LakeID,
			KeyID:     lc.KeyID,
			PublicKey: lc.PublicKey,
			Projects: Projects{
				Allow: lc.Projects.Allow,
				Deny:  append(append([]ProjectMatch(nil), file.Projects.Deny...), lc.Projects.Deny...),
			},
		})
	}

	if flags.Lake != "" {
		for _, l := range lakes {
			if l.Name == flags.Lake {
				return []Lake{l}, nil
			}
		}
		return nil, fmt.Errorf("no lake named %s; configured: %s", flags.Lake, lakeNames(lakes))
	}
	return lakes, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkLakeEntry(name string, lc LakeConfig) error {
	if !ValidLakeName(name) {
		return fmt.Errorf("config: lakes.%q: a lake name is lowercase letters, digits, '-' and '_', at most 32 characters", name)
	}
	if strings.TrimSpace(lc.Server) == "" {
		return fmt.Errorf("config: lakes.%s: server is required", name)
	}
	if lc.TokenFile != "" && !filepath.IsAbs(lc.TokenFile) {
		return fmt.Errorf("config: lakes.%s: token_file %q is not an absolute path", name, lc.TokenFile)
	}
	if lc.LakeID != "" && !identity.ValidLakeID(lc.LakeID) {
		return fmt.Errorf("config: lakes.%s: lake_id %q is not a lake id", name, lc.LakeID)
	}
	if (lc.KeyID == "") != (lc.PublicKey == "") {
		return fmt.Errorf("config: lakes.%s: key_id and public_key are pinned together", name)
	}
	return nil
}

// lakeTokenPath is the default token file of a lake in the map: the
// legacy token for the lake named default, tokens/<name>.token for the
// rest.
func lakeTokenPath(getenv func(string) string, name string) (string, error) {
	if name == DefaultLake {
		return TokenPath(getenv)
	}
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tokens", name+".token"), nil
}

func lakeNames(lakes []Lake) string {
	if len(lakes) == 0 {
		return "none"
	}
	names := make([]string, len(lakes))
	for i, l := range lakes {
		names[i] = l.Name
	}
	return strings.Join(names, ", ")
}
