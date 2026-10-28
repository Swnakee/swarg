package ui

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sgr returns the true-colour foreground SGR parameters of a hex colour.
func sgr(t *testing.T, hex string) string {
	t.Helper()
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("bad colour %q", hex)
	}
	return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
}

var fgTrueColor = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNoColourLiteralsOutsideTheme(t *testing.T) {
	hex := regexp.MustCompile(`^"#[0-9a-fA-F]{3,8}"$`)
	fset := token.NewFileSet()
	files, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range files {
		for name, f := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BasicLit:
					if n.Kind == token.STRING && hex.MatchString(n.Value) {
						t.Errorf("%s: hex colour literal %s", fset.Position(n.Pos()), n.Value)
					}
				case *ast.CallExpr:
					sel, ok := n.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Color" || len(n.Args) != 1 {
						return true
					}
					if lit, ok := n.Args[0].(*ast.BasicLit); ok {
						if _, err := strconv.Unquote(lit.Value); err == nil {
							t.Errorf("%s: colour literal %s", fset.Position(n.Pos()), lit.Value)
						}
					}
				}
				return true
			})
		}
	}
}

func TestUIUsesPaletteColours(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "read_file", "one"), toolEnd("a", "read_file", "one", "3 lines", "alpha\nbeta\n", nil))
	r.open(1)
	raw := r.m.View().Content
	for name, hex := range map[string]string{"Accent (block heads)": mdstream.Colors.Accent, "Dim (box, output)": mdstream.Colors.Dim} {
		if !strings.Contains(raw, sgr(t, hex)) {
			t.Errorf("view lacks the %s colour %s", name, hex)
		}
	}
	if !strings.Contains(raw, strings.Replace(sgr(t, mdstream.Colors.UserBg), "38;", "48;", 1)) {
		t.Error("user line lacks the UserBg background")
	}
}

func TestNoRedOutsideErrors(t *testing.T) {
	r := newRig(t)
	r.events(started("q"),
		toolStart("a", "read_file", "x"), toolStart("b", "shell", "y"), toolStart("c", "edit_file", "z"),
		toolEnd("a", "read_file", "x", "denied by the user", "", nil),
		toolEnd("b", "shell", "y", "exit 2", "out\n", nil),
		toolEnd("c", "edit_file", "z", "", "", fmt.Errorf("no match")))
	r.open(2)
	r.ask(context.Background(), "shell", `{"command":"ls"}`)
	errCol := sgr(t, mdstream.Colors.Error)
	for _, l := range strings.Split(r.m.View().Content, "\n") {
		hasErrCol := strings.Contains(l, errCol)
		for _, m := range fgTrueColor.FindAllStringSubmatch(l, -1) {
			rr, g, b := atoi(m[1]), atoi(m[2]), atoi(m[3])
			if rr > 150 && g < 130 && b < 130 && !hasErrCol {
				t.Errorf("red %s outside the error colour: %q", m[0], l)
			}
		}
		if hasErrCol && !strings.Contains(l, glyphError) {
			t.Errorf("error colour on a row without %s: %q", glyphError, l)
		}
	}
	if !strings.Contains(r.m.View().Content, errCol) {
		t.Error("the error row is not coloured")
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
