package load

import (
	"os"

	"github.com/gopherex/xconf"
)

// EnvSource resolves field values from environment variables using the env
// name declared on each FieldDesc (auto-derived or explicit).
//
// When Vars is non-nil it is used in place of the real process env (useful
// for tests).
type EnvSource struct {
	Vars map[string]string
}

// FromEnv returns an EnvSource. Pass nil to read from the real process
// environment.
func FromEnv(vars map[string]string) Source {
	return EnvSource{Vars: vars}
}

func (e EnvSource) Lookup(d xconf.FieldDesc, _ []string) (any, bool, error) {
	if d.Env == "" {
		return nil, false, nil
	}
	if e.Vars != nil {
		v, ok := e.Vars[d.Env]
		if !ok {
			return nil, false, nil
		}
		return v, true, nil
	}
	v, ok := os.LookupEnv(d.Env)
	if !ok {
		return nil, false, nil
	}
	return v, true, nil
}
