package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func parseFile(t *testing.T, raw string) File {
	t.Helper()
	var f File
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

const cfgHome = "/cfg"

func baseEnv() map[string]string {
	return map[string]string{"XDG_CONFIG_HOME": cfgHome, "HOME": "/home/x"}
}

func TestLegacyConfigIsOneDefaultLake(t *testing.T) {
	for _, tc := range []struct {
		name, raw  string
		env        map[string]string
		wantServer Setting
		wantToken  Setting
	}{
		{"empty", `{}`, nil, Setting{DefaultServer, SourceDefault}, Setting{filepath.Join(cfgHome, "terva-lampi", "token"), SourceDefault}},
		{"file", `{"server":"https://a.example","token_file":"/t/a"}`, nil, Setting{"https://a.example", SourceConfig}, Setting{"/t/a", SourceConfig}},
		{"env", `{"server":"https://a.example"}`, map[string]string{"LAMPI_SERVER": "https://b.example"}, Setting{"https://b.example", SourceEnv}, Setting{filepath.Join(cfgHome, "terva-lampi", "token"), SourceDefault}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			for k, v := range tc.env {
				env[k] = v
			}
			f := parseFile(t, tc.raw)
			lakes, err := ResolveLakes(f, envOf(env), LakeFlags{})
			if err != nil {
				t.Fatal(err)
			}
			if len(lakes) != 1 || lakes[0].Name != DefaultLake || !lakes[0].Legacy {
				t.Fatalf("lakes %+v", lakes)
			}
			if lakes[0].Server != tc.wantServer || lakes[0].TokenFile != tc.wantToken {
				t.Fatalf("server %+v token %+v", lakes[0].Server, lakes[0].TokenFile)
			}
			// The same answer the single-lake resolvers give.
			if lakes[0].Server != ResolveServer(f, envOf(env), "") {
				t.Fatal("differs from ResolveServer")
			}
		})
	}
}

func TestLegacyFlagsStillApply(t *testing.T) {
	lakes, err := ResolveLakes(File{}, envOf(baseEnv()), LakeFlags{Server: "http://127.0.0.1:9", TokenFile: "/t/f"})
	if err != nil {
		t.Fatal(err)
	}
	if lakes[0].Server != (Setting{"http://127.0.0.1:9", SourceFlag}) || lakes[0].TokenFile != (Setting{"/t/f", SourceFlag}) {
		t.Fatalf("%+v", lakes[0])
	}
}

const twoLakes = `{
  "server": "https://home.example",
  "projects": {
    "allow": [{"cwd_prefix": "/src/home"}],
    "deny": [{"cwd_prefix": "/src/secret"}]
  },
  "lakes": {
    "work": {
      "server": "https://work.example",
      "lake_id": "lake_aaaaaaaaaaaaaaaaaaaaaaaaaa",
      "projects": {
        "allow": [{"cwd_prefix": "/src/work"}],
        "deny": [{"cwd_prefix": "/src/work/private"}]
      }
    },
    "archive": {"server": "https://archive.example", "token_file": "/t/archive"}
  }
}`

func TestLakesMapAddsLakesBesideTheLegacyOne(t *testing.T) {
	lakes, err := ResolveLakes(parseFile(t, twoLakes), envOf(baseEnv()), LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if got := lakeNames(lakes); got != "default, archive, work" {
		t.Fatalf("order %q", got)
	}
	def, archive, work := lakes[0], lakes[1], lakes[2]
	if def.Server.Value != "https://home.example" || !def.Legacy {
		t.Fatalf("default %+v", def)
	}
	if archive.TokenFile != (Setting{"/t/archive", SourceConfig}) {
		t.Fatalf("archive token %+v", archive.TokenFile)
	}
	if work.TokenFile != (Setting{filepath.Join(cfgHome, "terva-lampi", "tokens", "work.token"), SourceDefault}) {
		t.Fatalf("work token %+v", work.TokenFile)
	}
	if work.LakeID != "lake_aaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("work lake id %q", work.LakeID)
	}

	id := func(cwd string) ProjectID { return ProjectID{CWD: cwd, NoRepo: true} }
	// A lake's allow rules apply to that lake only.
	if !def.Projects.Permitted(id("/src/home/a")) || work.Projects.Permitted(id("/src/home/a")) || archive.Projects.Permitted(id("/src/home/a")) {
		t.Fatal("home allow leaked to another lake")
	}
	if def.Projects.Permitted(id("/src/work/a")) || !work.Projects.Permitted(id("/src/work/a")) {
		t.Fatal("work allow is not scoped to work")
	}
	// A lake's deny applies to it; the top-level deny applies to all.
	if work.Projects.Permitted(id("/src/work/private/x")) {
		t.Fatal("work deny ignored")
	}
	workSecret := parseFile(t, strings.Replace(twoLakes, `"/src/work"}]`, `"/src"}]`, 1))
	lakes, err = ResolveLakes(workSecret, envOf(baseEnv()), LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if lakes[2].Projects.Permitted(id("/src/secret/x")) {
		t.Fatal("top-level deny did not apply to a lake whose allow covers it")
	}
	if !lakes[2].Projects.Permitted(id("/src/other")) {
		t.Fatal("work allow of /src refused /src/other")
	}
}

func TestLakeSelection(t *testing.T) {
	f := parseFile(t, twoLakes)
	lakes, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{Lake: "work"})
	if err != nil || len(lakes) != 1 || lakes[0].Name != "work" {
		t.Fatalf("select work: %v %+v", err, lakes)
	}
	if _, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{Lake: "nope"}); err == nil || !strings.Contains(err.Error(), "configured: default, archive, work") {
		t.Fatalf("unknown lake: %v", err)
	}
	if _, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{Lake: "Bad Name"}); err == nil {
		t.Fatal("bad name accepted")
	}
	// --server needs one lake.
	if _, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{Server: "https://x.example"}); err == nil || !strings.Contains(err.Error(), "pass --lake") {
		t.Fatalf("--server across lakes: %v", err)
	}
	lakes, err = ResolveLakes(f, envOf(baseEnv()), LakeFlags{Lake: "work", Server: "https://x.example"})
	if err != nil || lakes[0].Server != (Setting{"https://x.example", SourceFlag}) {
		t.Fatalf("--lake work --server: %v %+v", err, lakes)
	}
	// Environment applies to the default lake, not to work.
	env := baseEnv()
	env["LAMPI_SERVER"] = "https://env.example"
	lakes, err = ResolveLakes(f, envOf(env), LakeFlags{})
	if err != nil || lakes[0].Server.Source != SourceEnv || lakes[2].Server != (Setting{"https://work.example", SourceConfig}) {
		t.Fatalf("env: %v %+v", err, lakes)
	}
}

func TestLakesMapWithoutLegacyServerHasNoDefault(t *testing.T) {
	f := parseFile(t, `{"lakes":{"work":{"server":"https://work.example"}},"projects":{"deny":[{"cwd_prefix":"/x"}]}}`)
	lakes, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{})
	if err != nil || lakeNames(lakes) != "work" {
		t.Fatalf("%v %s", err, lakeNames(lakes))
	}
	// LAMPI_SERVER adds the default lake back.
	env := baseEnv()
	env["LAMPI_SERVER"] = "https://env.example"
	lakes, err = ResolveLakes(f, envOf(env), LakeFlags{})
	if err != nil || lakeNames(lakes) != "default, work" {
		t.Fatalf("%v %s", err, lakeNames(lakes))
	}
}

func TestExplicitDefaultEntry(t *testing.T) {
	f := parseFile(t, `{"lakes":{"default":{"server":"https://d.example"}}}`)
	lakes, err := ResolveLakes(f, envOf(baseEnv()), LakeFlags{})
	if err != nil || len(lakes) != 1 || lakes[0].Legacy {
		t.Fatalf("%v %+v", err, lakes)
	}
	if lakes[0].TokenFile.Value != filepath.Join(cfgHome, "terva-lampi", "token") {
		t.Fatalf("explicit default token %+v", lakes[0].TokenFile)
	}
	env := baseEnv()
	env["LAMPI_TOKEN_FILE"] = "/t/env"
	lakes, _ = ResolveLakes(f, envOf(env), LakeFlags{})
	if lakes[0].TokenFile != (Setting{"/t/env", SourceEnv}) {
		t.Fatalf("env token on explicit default %+v", lakes[0].TokenFile)
	}
}

func TestLakeConfigErrors(t *testing.T) {
	for name, raw := range map[string]string{
		"default twice":        `{"server":"https://a.example","lakes":{"default":{"server":"https://b.example"}}}`,
		"orphan allow":         `{"projects":{"allow":[{"cwd_prefix":"/x"}]},"lakes":{"w":{"server":"https://w.example"}}}`,
		"no server":            `{"lakes":{"w":{}}}`,
		"bad name":             `{"lakes":{"W":{"server":"https://w.example"}}}`,
		"relative token":       `{"lakes":{"w":{"server":"https://w.example","token_file":"tok"}}}`,
		"bad lake id":          `{"lakes":{"w":{"server":"https://w.example","lake_id":"nope"}}}`,
		"half a pinned key":    `{"lakes":{"w":{"server":"https://w.example","key_id":"0011223344556677"}}}`,
		"name that is a path":  `{"lakes":{"../x":{"server":"https://w.example"}}}`,
		"name that is too big": `{"lakes":{"` + strings.Repeat("a", 33) + `":{"server":"https://w.example"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveLakes(parseFile(t, raw), envOf(baseEnv()), LakeFlags{}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
