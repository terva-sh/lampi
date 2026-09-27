package cli

import (
	"fmt"
	"io"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
)

// clientConfig is config.json with each pinned lake's cached profile
// applied: the machine-wide fields, and each lake's own project rules.
// config.json wins over every field.
type clientConfig struct {
	// file is the effective config: config.json with the machine-wide
	// fields it leaves unset filled from the lakes' profiles.
	file config.File
	// lakes are the lakes the flags select, each with its own profile's
	// project rules applied.
	lakes   []config.Lake
	origins config.Origins
	// docs are the verified cached profiles by lake name.
	docs map[string]lakeprofile.Doc
}

// loadClientConfig reads config.json, resolves the lakes, and applies
// the cached profile of every pinned lake. A cached profile that does
// not verify is named on warn and not used. Machine-wide fields come
// from every lake, not only the ones the flags select, so --lake does
// not change which harnesses are read.
func loadClientConfig(env Env, warn io.Writer, flags config.LakeFlags) (clientConfig, error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return clientConfig{}, err
	}
	selected, err := config.ResolveLakes(file, env.getenv, flags)
	if err != nil {
		return clientConfig{}, err
	}
	all, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{})
	if err != nil {
		return clientConfig{}, err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return clientConfig{}, err
	}
	docs := map[string]lakeprofile.Doc{}
	profiles := map[string]config.Profile{}
	order := make([]string, 0, len(all))
	for _, l := range all {
		order = append(order, l.Name)
		if !lakeprofile.Pinned(l) {
			continue
		}
		d, ok, err := lakeprofile.Load(lakestate.Dir(state, l.Name), l)
		if err != nil {
			fmt.Fprintf(warn, "terva-lampi: lake %s: %v; not using it\n", l.Name, err)
			continue
		}
		if ok {
			docs[l.Name] = d
			profiles[l.Name] = d.Profile
		}
	}
	eff, origins := config.ApplyMachineProfiles(file, order, profiles)
	lakes := make([]config.Lake, len(selected))
	for i, l := range selected {
		p, ok := profiles[l.Name]
		lakes[i] = config.ApplyLakeProfile(l, p, ok)
	}
	return clientConfig{file: eff, lakes: lakes, origins: origins, docs: docs}, nil
}
