package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Meta test: the feature files follow the project's scenario rules.
// ---------------------------------------------------------------------------

var scenarioIDRe = regexp.MustCompile(`^@S-(\d{3})$`)

// featureScenario is one parsed scenario.
type featureScenario struct {
	file, name string
	tags       []string
	whens      int
	thens      int
}

// parseFeature reads the scenarios of one feature file. And/But steps count for the keyword
// of the step before them.
func parseFeature(t *testing.T, path string) (out []featureScenario) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "@"):
			tags = append(tags, strings.Fields(l)...)
		case strings.HasPrefix(l, "Scenario"):
			out = append(out, featureScenario{file: filepath.Base(path), name: l, tags: tags})
			tags, last = nil, ""
		case len(out) > 0 && l != "":
			kw, _, _ := strings.Cut(l, " ")
			if kw == "And" || kw == "But" {
				kw = last
			}
			last = kw
			s := &out[len(out)-1]
			switch kw {
			case "When":
				s.whens++
			case "Then":
				s.thens++
			}
		}
	}
	return out
}

func TestFeatureFilesFollowProcess(t *testing.T) {
	files, err := filepath.Glob("../../features/*.feature")
	if err != nil || len(files) == 0 {
		t.Fatalf("no feature files found: %v", err)
	}
	var ids []int
	seen := map[int]string{}
	for _, f := range files {
		for _, sc := range parseFeature(t, f) {
			var own []int
			for _, tag := range sc.tags {
				if m := scenarioIDRe.FindStringSubmatch(tag); m != nil {
					n, _ := strconv.Atoi(m[1])
					own = append(own, n)
				}
			}
			if len(own) != 1 {
				t.Errorf("%s: %q has %d @S-nnn tags, want exactly 1", sc.file, sc.name, len(own))
				continue
			}
			if prev, dup := seen[own[0]]; dup {
				t.Errorf("S-%03d is used twice: %s and %s", own[0], prev, sc.file)
			}
			seen[own[0]] = sc.file
			ids = append(ids, own[0])
			if sc.whens != 1 || sc.thens != 1 {
				t.Errorf("S-%03d: %d When and %d Then steps, want 1 and 1", own[0], sc.whens, sc.thens)
			}
		}
	}
	// Ids are unique (checked above) and start at 1; gaps are allowed since T3, where parallel
	// task parts own id ranges (S-200.., S-250..) and unused ids simply stay free.
	sort.Ints(ids)
	if len(ids) == 0 {
		t.Fatal("no scenario ids")
	}
	if ids[0] != 1 {
		t.Fatalf("scenario ids must start at S-001, lowest is S-%03d", ids[0])
	}
}
