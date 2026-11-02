package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
)

// ---------------------------------------------------------------------------
// Key users (R-110)
// ---------------------------------------------------------------------------

// varNameRe is what counts as a variable reference in an MCP value (not "$1" or "$*").
var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// KeyUsers maps every key name the config refers to onto who uses it: "provider <name>" for an
// api_key_env, "mcp <server> header <Name>" / "mcp <server> env <NAME>" for a $VAR in those values.
// Users are sorted; a name used twice lists both.
func KeyUsers(cfg Config) map[string][]string {
	out := map[string][]string{}
	add := func(name, who string) {
		if varNameRe.MatchString(name) {
			out[name] = append(out[name], who)
		}
	}
	for n, p := range cfg.Providers {
		add(p.APIKeyEnv, "provider "+n)
	}
	refs := func(server, kind string, m map[string]string) {
		for k, v := range m {
			os.Expand(v, func(name string) string {
				add(name, fmt.Sprintf("mcp %s %s %s", server, kind, k))
				return ""
			})
		}
	}
	for n, s := range cfg.MCP.Servers {
		refs(n, "header", s.Headers)
		refs(n, "env", s.Env)
	}
	for _, u := range out {
		sort.Strings(u)
	}
	return out
}
