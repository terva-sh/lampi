package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"terva.sh/lampi/internal/config"
)

// EffectiveProfile is the configuration a device's agent fetches: its
// layers applied in order. Name is the device's profile. Version is
// Config's version, the one an agent recomputes, so it changes when,
// and only when, Config does. Layers names each layer, bottom first,
// as "profile:NAME".
type EffectiveProfile struct {
	Name    string
	Config  config.Profile
	Version string
	Layers  []string
}

// querier is a *sql.DB or a *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// profileLayer applies one layer for device d on top of p, and names
// the layer. A later layer, such as a per-device overlay, is a new
// entry in profileLayers; it does not change what the layers under it
// mean.
type profileLayer func(ctx context.Context, q querier, d Device, p *config.Profile) (string, error)

var profileLayers = []profileLayer{baseProfileLayer}

// baseProfileLayer is the device's profile, or the default when it
// names none. A catalog with no default profile has an empty one, so a
// lake no one has given a profile serves an empty default. Any other
// name the catalog does not hold is ErrNoProfile.
func baseProfileLayer(ctx context.Context, q querier, d Device, p *config.Profile) (string, error) {
	name := d.Profile
	if name == "" {
		name = config.DefaultProfile
	}
	row, err := scanProfile(q.QueryRowContext(ctx, `SELECT `+profileCols+` FROM profiles WHERE name=?`, name))
	switch {
	case errors.Is(err, sql.ErrNoRows) && name == config.DefaultProfile:
		*p = config.Profile{}
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("%w: %s", ErrNoProfile, name)
	case err != nil:
		return "", fmt.Errorf("catalog: %w", err)
	default:
		*p = row.Config
	}
	return "profile:" + name, nil
}

// ResolveProfile is device d's effective profile.
func (c *Catalog) ResolveProfile(ctx context.Context, d Device) (EffectiveProfile, error) {
	return resolveProfile(ctx, c.db, d)
}

func resolveProfile(ctx context.Context, q querier, d Device) (EffectiveProfile, error) {
	e := EffectiveProfile{Name: d.Profile}
	if e.Name == "" {
		e.Name = config.DefaultProfile
	}
	for _, layer := range profileLayers {
		name, err := layer(ctx, q, d, &e.Config)
		if err != nil {
			return EffectiveProfile{}, err
		}
		e.Layers = append(e.Layers, name)
	}
	e.Version = e.Config.Version()
	return e, nil
}

// HasProfile reports whether a device or a code may name profile. The
// default profile is always there, stored or not.
func (c *Catalog) HasProfile(ctx context.Context, name string) (bool, error) {
	if name == "" || name == config.DefaultProfile {
		return true, nil
	}
	var one int
	err := c.db.QueryRowContext(ctx, `SELECT 1 FROM profiles WHERE name=?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return true, nil
}

// ProfileNames lists the profiles a device or a code may name: the
// default first, then the rest by name.
func (c *Catalog) ProfileNames(ctx context.Context) ([]string, error) {
	list, err := c.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{config.DefaultProfile}
	for _, p := range list {
		if p.Name != config.DefaultProfile {
			names = append(names, p.Name)
		}
	}
	return names, nil
}
