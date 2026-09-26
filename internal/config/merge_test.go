package config

import "testing"

func TestApplyMachineProfilesLocalWinsThenFirstLake(t *testing.T) {
	file := parseFile(t, `{"harnesses":{"codex":{"enabled":true}},"agent":{"debounce_max":"9s"}}`)
	profiles := map[string]Profile{
		"default": {Harnesses: Harnesses{"codex": {Enabled: false}}, Agent: AgentConfig{DebounceMax: "1s"}},
		"work":    {Harnesses: Harnesses{"cursor": {Enabled: false}}, Agent: AgentConfig{Debounce: "2s", DebounceMax: "3s"}},
		"zeta":    {Harnesses: Harnesses{"cursor": {Enabled: true}}, Agent: AgentConfig{Debounce: "7s"}},
	}
	got, origins := ApplyMachineProfiles(file, []string{"default", "work", "zeta"}, profiles)
	if !got.Harnesses.Enabled("codex") || origins["harnesses.codex"] != "local" {
		t.Fatalf("local harness lost: %+v %v", got.Harnesses, origins)
	}
	if got.Harnesses.Enabled("cursor") || origins["harnesses.cursor"] != "lake work" {
		t.Fatalf("first lake did not win: %+v %v", got.Harnesses, origins)
	}
	if got.Agent.Debounce != "2s" || origins["agent.debounce"] != "lake work" {
		t.Fatalf("debounce %q %v", got.Agent.Debounce, origins)
	}
	if got.Agent.DebounceMax != "9s" || origins["agent.debounce_max"] != "local" {
		t.Fatalf("debounce_max %q %v", got.Agent.DebounceMax, origins)
	}
	_, origins = ApplyMachineProfiles(File{}, nil, nil)
	if origins["agent.debounce"] != "default" {
		t.Fatalf("no profile: %v", origins)
	}
}

func TestApplyLakeProfileNeverWidensALocalAllowAndDenyWins(t *testing.T) {
	id := ProjectID{CWD: "/work/app/secret", NoRepo: true}
	p := Profile{Projects: Projects{Allow: []ProjectMatch{{CWDPrefix: "/work"}}, Deny: []ProjectMatch{{CWDPrefix: "/work/tmp"}}}}

	// No local allow: the lake's allow applies, beneath a local deny.
	l := Lake{Name: "work", Projects: Projects{Deny: []ProjectMatch{{CWDPrefix: "/work/app/secret"}}}}
	got := ApplyLakeProfile(l, p, true)
	if got.AllowFrom != "lake work" || len(got.Projects.Deny) != 2 {
		t.Fatalf("%+v", got)
	}
	if got.Projects.Permitted(id) {
		t.Fatal("a lake allow beat a local deny")
	}
	if !got.Projects.Permitted(ProjectID{CWD: "/work/other", NoRepo: true}) {
		t.Fatal("lake allow not applied")
	}

	// A local allow is kept as it is.
	l = Lake{Name: "work", Projects: Projects{Allow: []ProjectMatch{{CWDPrefix: "/home"}}}}
	got = ApplyLakeProfile(l, p, true)
	if got.AllowFrom != "local" || len(got.Projects.Allow) != 1 || got.Projects.Allow[0].CWDPrefix != "/home" {
		t.Fatalf("local allow widened: %+v", got)
	}
	// The input lake is not changed underneath the caller.
	if len(l.Projects.Deny) != 0 {
		t.Fatal("ApplyLakeProfile wrote through to the caller's slice")
	}
}
