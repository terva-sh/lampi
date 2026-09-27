package config

import "sort"

// Origin sources for an effective value.
const (
	OriginLocal   = "local"
	OriginDefault = "default"
)

// OriginLake is the origin of a value a lake's profile set.
func OriginLake(name string) string { return "lake " + name }

// Origins names where each machine-wide effective value came from:
// "local" for config.json, "lake NAME" for that lake's profile, or
// "default". Keys are "harnesses.<id>", "agent.debounce" and
// "agent.debounce_max".
type Origins map[string]string

// ApplyMachineProfiles fills the machine-wide fields config.json leaves
// unset from the lakes' profiles. config.json wins over every field.
// Between lakes, the first in order that sets a field wins; order is
// the ResolveLakes order, the default lake first and the rest by name.
// A harness root and redaction never come from a profile.
func ApplyMachineProfiles(file File, order []string, profiles map[string]Profile) (File, Origins) {
	origins := Origins{}
	harnesses := Harnesses{}
	for id, h := range file.Harnesses {
		harnesses[id] = h
		origins["harnesses."+id] = OriginLocal
	}
	for _, name := range order {
		p, ok := profiles[name]
		if !ok {
			continue
		}
		ids := make([]string, 0, len(p.Harnesses))
		for id := range p.Harnesses {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if _, set := harnesses[id]; set {
				continue
			}
			harnesses[id] = HarnessConfig{Enabled: p.Harnesses[id].Enabled}
			origins["harnesses."+id] = OriginLake(name)
		}
	}
	if len(harnesses) > 0 {
		file.Harnesses = harnesses
	}
	file.Agent.Debounce = pickProfile(file.Agent.Debounce, "agent.debounce", order, profiles, origins, func(p Profile) string { return p.Agent.Debounce })
	file.Agent.DebounceMax = pickProfile(file.Agent.DebounceMax, "agent.debounce_max", order, profiles, origins, func(p Profile) string { return p.Agent.DebounceMax })
	return file, origins
}

func pickProfile(local, key string, order []string, profiles map[string]Profile, origins Origins, get func(Profile) string) string {
	if local != "" {
		origins[key] = OriginLocal
		return local
	}
	for _, name := range order {
		if p, ok := profiles[name]; ok && get(p) != "" {
			origins[key] = OriginLake(name)
			return get(p)
		}
	}
	origins[key] = OriginDefault
	return ""
}

// ApplyLakeProfile adds a lake's own profile to its project rules. The
// profile's allow rules apply only when config.json gives this lake
// none, so a local allowlist is never widened. The profile's deny rules
// are added to the local ones; a deny can only narrow, and a local deny
// wins over any allow because deny is checked first. The result applies
// to uploads to this lake only. AllowFrom records which allowlist is in
// force, and DenyLocal and DenyLake where the deny rules came from.
func ApplyLakeProfile(l Lake, p Profile, ok bool) Lake {
	l.AllowFrom = OriginLocal
	l.DenyLocal, l.DenyLake = len(l.Projects.Deny), 0
	if !ok {
		return l
	}
	if len(l.Projects.Allow) == 0 && len(p.Projects.Allow) > 0 {
		l.Projects.Allow = append([]ProjectMatch(nil), p.Projects.Allow...)
		l.AllowFrom = OriginLake(l.Name)
	}
	l.Projects.Deny = append(append([]ProjectMatch(nil), l.Projects.Deny...), p.Projects.Deny...)
	l.DenyLake = len(p.Projects.Deny)
	return l
}
