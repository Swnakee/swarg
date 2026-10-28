package agent

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// feedAll feeds text to a guard in chunks of n bytes (rune-safe) and returns the cut and
// whether the guard tripped; the text it reports is what was fed up to the trip.
func feedAll(g *loopGuard, text string, n int) (string, bool) {
	var fed strings.Builder
	rs := []rune(text)
	for i := 0; i < len(rs); i += n {
		chunk := string(rs[i:min(len(rs), i+n)])
		fed.WriteString(chunk)
		if cut, ok := g.feed(chunk); ok {
			return cut, true
		}
	}
	return fed.String(), false
}

// ownerList is the owner's live case: a numbered list whose item never changes.
func ownerList(first, last int) string {
	var b strings.Builder
	for i := first; i <= last; i++ {
		fmt.Fprintf(&b, "%d. **Сборка и тестирование** — могу собирать и тестировать проекты.\n\n", i)
	}
	return b.String()
}

func TestLoopGuard_OwnerCaseStopsAtTheFourthCopy(t *testing.T) {
	text := "Вот что я умею:\n\n" + ownerList(67, 90)
	cut, ok := feedAll(&loopGuard{}, text, 7)
	if !ok {
		t.Fatal("the guard did not trip on a list that repeats one item")
	}
	if n := strings.Count(cut, "Сборка и тестирование"); n != 1 {
		t.Errorf("cut keeps %d copies, want exactly the first one:\n%s", n, cut)
	}
	if !strings.HasPrefix(cut, "Вот что я умею:") || !strings.HasSuffix(cut, "проекты.") {
		t.Errorf("cut = %q, want the intro and the first item without trailing blanks", cut)
	}
}

func TestLoopGuard_LegitListIsNotStopped(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "%d. Пункт номер %c%c — уникальное описание шага %d.\n\n", i, rune('a'+i%26), rune('a'+i/26), i*i)
	}
	if _, ok := feedAll(&loopGuard{}, b.String(), 13); ok {
		t.Error("a list of different items was stopped")
	}
}

func TestLoopGuard_ThreeCopiesAreFine(t *testing.T) {
	if _, ok := feedAll(&loopGuard{}, ownerList(1, 3)+"Конец списка.\n", 5); ok {
		t.Error("three copies of an item must not trip the guard (boundary: the fourth does)")
	}
}

func TestLoopGuard_FourCopiesFarApartAreFine(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 4; i++ {
		b.WriteString("Это повторяющаяся строка списка.\n")
		for j := 0; j < 40; j++ {
			fmt.Fprintf(&b, "промежуточная строка %c%c%c%c %d\n", rune('a'+(i+j)%26), rune('b'+j%26), rune('c'+i*7%26), rune('d'+j*3%26), i*100+j)
		}
	}
	if _, ok := feedAll(&loopGuard{}, b.String(), 9); ok {
		t.Error("copies more than 40 lines apart were stopped")
	}
}

func TestLoopGuard_ShortLinesAreIgnored(t *testing.T) {
	if _, ok := feedAll(&loopGuard{}, strings.Repeat("---\n", 30)+strings.Repeat("1. ok\n", 30), 4); ok {
		t.Error("lines shorter than 8 characters tripped the guard")
	}
}

func TestLoopGuard_RepeatedCodeLinesInAFenceAreFine(t *testing.T) {
	text := "```go\n" + strings.Repeat("\tif err != nil {\n\t\treturn err\n\t}\n", 6) + "```\n"
	if _, ok := feedAll(&loopGuard{}, text, 6); ok {
		t.Error("repeated lines of a code block tripped the line rule")
	}
}

func TestLoopGuard_LongRepeatWithoutNewlines(t *testing.T) {
	unit := "это длинный абзац без переносов строк который модель повторяет снова и снова, пока не кончатся токены, "
	text := "Начало ответа. " + strings.Repeat(unit, 12)
	cut, ok := feedAll(&loopGuard{}, text, 11)
	if !ok {
		t.Fatal("the 300-character rule did not trip")
	}
	// the tail rule needs the tail and two copies of it, so the cut keeps at most about one
	// copy of the 300-character window (3 units) plus the intro
	if len(cut) > len("Начало ответа. ")+len(unit)*5 || !strings.Contains(cut, "длинный абзац") {
		t.Errorf("cut is %d bytes, want the beginning only: %q", len(cut), cut)
	}
	if !strings.HasPrefix(cut, "Начало ответа.") {
		t.Errorf("cut = %q", cut)
	}
}

func TestLoopGuard_ABlockSeenTwiceIsFine(t *testing.T) {
	// not periodic itself: a phrase repeated inside the block would trip the period rule
	var b strings.Builder
	for i := 0; b.Len() < 600; i++ {
		fmt.Fprintf(&b, "слово%d ", i*i%97)
	}
	block := string([]rune(b.String())[:300])
	if _, ok := feedAll(&loopGuard{}, block+" уникально-1 "+block, 10); ok {
		t.Error("a 300-character block seen twice tripped the guard (boundary: the third copy does)")
	}
}

func TestLoopGuard_CutIsValidUTF8(t *testing.T) {
	cut, ok := feedAll(&loopGuard{}, "Привет! "+strings.Repeat("юникод — это тоже строка без конца, ", 40), 3)
	if !ok {
		t.Fatal("no trip")
	}
	if !strings.HasPrefix(cut, "Привет!") || strings.ContainsRune(cut, '�') {
		t.Errorf("cut = %q", cut)
	}
}

func TestNormalizeLine(t *testing.T) {
	cases := map[string]string{
		"67. **Сборка и тестирование** — могу.": "сборка и тестирование — могу.",
		"  - **Bold**  text 12 ": "bold text",
		"> 1) Quote":             "quote",
		"###  Header 3":          "header",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeLine(in); got != want {
			t.Errorf("normalizeLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Period rule: a phrase repeating inside one line
// ---------------------------------------------------------------------------

// ownerLine is the owner's live case (2026-10-27): a tool list whose last item repeats a run of
// commands inside one line.
const (
	ownerLinePrefix = "6. shell — выполнение команд (кроме  sudo ,  wget ,  curl ,  rsync ,  rm ,  cp ,  "
	ownerLineUnit   = "go install ,  go build ,  go test ,  go mod download ,  go mod tidy ,  go mod vendor ,  go get -u ,  "
)

func TestLoopGuard_PhraseRepeatingInsideOneLineIsCutEarly(t *testing.T) {
	text := ownerLinePrefix + strings.Repeat(ownerLineUnit, 30)
	g := &loopGuard{}
	cut, ok := feedAll(g, text, 3)
	if !ok {
		t.Fatal("the repeating line was not stopped")
	}
	if fed := len(g.buf); fed > len(ownerLinePrefix)+3*len(ownerLineUnit)+8 {
		t.Errorf("stopped after %d bytes, want within the third copy (%d)", fed, len(ownerLinePrefix)+3*len(ownerLineUnit))
	}
	if !strings.HasPrefix(cut, ownerLinePrefix) || strings.Count(cut, ownerLineUnit) > 1 || !utf8.ValidString(cut) {
		t.Errorf("cut = %q, want the prefix and at most one copy", cut)
	}
}

func TestLoopGuard_PeriodRuleBoundaries(t *testing.T) {
	var diff strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&diff, "item-%c%c, ", 'a'+rune(i%26), 'a'+rune(i/26))
	}
	for _, tc := range []struct {
		name string
		text string
		trip bool
	}{
		{"two copies are fine", ownerLinePrefix + strings.Repeat(ownerLineUnit, 2) + "end", false},
		{"three copies trip", ownerLinePrefix + strings.Repeat(ownerLineUnit, 3), true},
		{"a long line of different items", diff.String(), false},
		{"a ruler of one symbol", strings.Repeat("─", 200), false},
		{"a ruler of two symbols", strings.Repeat("=-", 200), false},
		{"a short unit needs a long run", strings.Repeat("go test, ", 12), false},
		{"a short unit with a long run trips", "x " + strings.Repeat("go test, ", 30), true},
		{"repetition inside a code fence", "```\n" + strings.Repeat("0x00, 0x01, 0x02, ", 30), false},
		{"copies on separate short lines are the line rule's job", strings.Repeat("go build\n", 3), false},
	} {
		_, ok := feedAll(&loopGuard{}, tc.text, 4)
		if ok != tc.trip {
			t.Errorf("%s: tripped = %v, want %v", tc.name, ok, tc.trip)
		}
	}
}
