package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Guard: every rule and invariant of Docs/Domain.md is named by a test or a scenario
// ---------------------------------------------------------------------------

// The ids are built from parts so this file never names an id itself.
var (
	ruleDef = regexp.MustCompile(`(?m)^\s*-\s+([RI])-(\d+)\b`)
	ruleRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])([RI])-(\d+)\b`)
)

// Counts of the Rules and Invariants sections; a change in Domain.md updates them here.
const (
	wantRules      = 116
	wantInvariants = 28
)

// domainSection returns the text of the "## <name>" section of Domain.md.
func domainSection(doc, name string) string {
	start := strings.Index(doc, "\n## "+name+"\n")
	if start < 0 {
		return ""
	}
	rest := doc[start+len(name)+5:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// ruleIDs returns the ids found by re in text as "R-n" / "I-n" keys.
func ruleIDs(re *regexp.Regexp, text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]+"-"+m[2]] = true
	}
	return out
}

func TestDomain_EveryRuleAndInvariantIsNamed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "Docs", "Domain.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")
	defs := map[string]bool{}
	var nR, nI int
	for id := range ruleIDs(ruleDef, domainSection(doc, "Rules")) {
		defs[id] = strings.HasPrefix(id, "R")
	}
	for id := range ruleIDs(ruleDef, domainSection(doc, "Invariants")) {
		defs[id] = false
	}
	for _, isRule := range defs {
		if isRule {
			nR++
		} else {
			nI++
		}
	}
	if nR != wantRules || nI != wantInvariants {
		t.Fatalf("Domain.md defines %d rules and %d invariants, want %d and %d", nR, nI, wantRules, wantInvariants)
	}

	self := "domain_rules_test.go"
	named := map[string]bool{}
	root := filepath.Join("..", "..")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name == self || !(strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".feature")) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for id := range ruleIDs(ruleRef, string(b)) {
			named[id] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var missing []string
	for id := range defs {
		if !named[id] {
			missing = append(missing, id)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return idLess(missing[i], missing[j]) })
	if len(missing) > 0 {
		t.Fatalf("named by no test or scenario: %s", strings.Join(missing, " "))
	}
}

// idLess orders ids rules first, then by number.
func idLess(a, b string) bool {
	if a[0] != b[0] {
		return a[0] == 'R'
	}
	na, _ := strconv.Atoi(a[2:])
	nb, _ := strconv.Atoi(b[2:])
	return na < nb
}

func TestRuleRefs_NoPrefixFalsePositive(t *testing.T) {
	id := func(k string, n int) string { return fmt.Sprintf("%s-%d", k, n) }
	cases := []struct {
		text string
		want []string
	}{
		{id("R", 10), []string{id("R", 10)}},
		{id("R", 1) + ".", []string{id("R", 1)}},
		{"@" + id("R", 1), []string{id("R", 1)}},
		{"// " + id("R", 1) + ": text", []string{id("R", 1)}},
		{"X" + id("R", 1), nil},
		{"_" + id("R", 1), nil},
		{id("R", 105), []string{id("R", 105)}},
		{id("I", 38), []string{id("I", 38)}},
		{"(" + id("I", 4) + ")", []string{id("I", 4)}},
	}
	for _, c := range cases {
		got := ruleIDs(ruleRef, c.text)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.text, got, c.want)
			continue
		}
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%q: got %v, want %v", c.text, got, c.want)
			}
		}
	}
}
