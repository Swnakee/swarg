package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Guard: the import direction between swarg packages
// ---------------------------------------------------------------------------

// forbiddenDeps lists, per package, the swarg packages its non-test code must not depend on
// (directly or through another package). Extend it when a new package lands.
var forbiddenDeps = map[string][]string{
	// the domain core depends only on interfaces, never on concrete I/O
	"swarg/internal/agent": {
		"swarg/internal/tools", "swarg/internal/mcp", "swarg/internal/config",
		"swarg/internal/session", "swarg/internal/llm", "swarg/internal/prompt",
	},
	// leaf modules split out in T14: small, reusable, no reach back into the app
	"swarg/internal/agent/guard": {"swarg/internal/tools", "swarg/internal/llm", "swarg/internal/ui", "swarg/internal/config"},
	"swarg/internal/keys":        {"swarg/internal/agent", "swarg/internal/tools", "swarg/internal/ui", "swarg/internal/config"},
	"swarg/internal/webget":      {"swarg/internal/agent", "swarg/internal/tools", "swarg/internal/ui", "swarg/internal/config"},
	"swarg/internal/ui/hover":    {"swarg/internal/agent", "swarg/internal/tools", "swarg/internal/config", "swarg/internal/ui/menu"},
	"swarg/internal/ui/menu":     {"swarg/internal/agent", "swarg/internal/tools", "swarg/internal/config"},
	"swarg/internal/ui/command":  {"swarg/internal/agent", "swarg/internal/tools", "swarg/internal/config", "swarg/internal/ui/menu"},
}

func TestImports_Direction(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH")
	}
	for pkg, banned := range forbiddenDeps {
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", pkg, err, out)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, b := range banned {
				if dep == b || strings.HasPrefix(dep, b+"/") {
					t.Errorf("%s depends on %s", pkg, dep)
				}
			}
		}
	}
}
