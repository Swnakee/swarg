package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Optional interfaces
// ---------------------------------------------------------------------------

// Stubber is implemented by tools whose old results can shrink in history. Stub returns the
// short text the model gets instead of res.Output once the Turn is over, or "" to keep the
// output as it is. The text always names the call (tool and path / pattern / id).
type Stubber interface {
	Stub(args json.RawMessage, res Result) string
}

// Toucher is implemented by tools that change a file: Touched returns the clean path they
// changed (slash form, as given), or "" when the args are unusable. Used for staleness.
type Toucher interface {
	Touched(args json.RawMessage) string
}

const (
	stubOKLines   = 3  // shell output lines kept in a Stub after success
	stubFailLines = 15 // ... after a failure
	stubNameRunes = 40 // longest command / pattern named in a Stub
	stubLineBytes = 160
)

// errorPrefix starts every error result the agent hands to the model.
const errorPrefix = "error:"

// ---------------------------------------------------------------------------
// Set dispatch
// ---------------------------------------------------------------------------

// Stub returns the history stub of a finished call, or "" (unknown tool, error result, tool
// without a policy, panic, or a stub that would not be shorter than the output).
func (s *Set) Stub(name string, args json.RawMessage, output, brief string) (stub string) {
	t, ok := s.Get(name)
	if !ok || strings.HasPrefix(output, errorPrefix) {
		return ""
	}
	st, ok := t.(Stubber)
	if !ok {
		return ""
	}
	defer func() {
		if recover() != nil {
			stub = ""
		}
	}()
	stub = st.Stub(args, Result{Output: output, Brief: brief})
	if len(stub) >= len(output) {
		return ""
	}
	return stub
}

// Touched returns the path a file-changing tool touched, or "".
func (s *Set) Touched(name string, args json.RawMessage) (path string) {
	t, ok := s.Get(name)
	if !ok {
		return ""
	}
	tc, ok := t.(Toucher)
	if !ok {
		return ""
	}
	defer func() {
		if recover() != nil {
			path = ""
		}
	}()
	return tc.Touched(args)
}

// ---------------------------------------------------------------------------
// Per-tool policy
// ---------------------------------------------------------------------------

// Stub of read_file: the path and the number of lines that were shown.
func (ReadFile) Stub(args json.RawMessage, res Result) string {
	return fmt.Sprintf("[read_file %s: %d lines shown earlier; read it again if needed]",
		pathOf(args), countLines(res.Output))
}

// Stub of grep: the pattern and the number of matches that were shown.
func (Grep) Stub(args json.RawMessage, res Result) string {
	var a struct {
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal(args, &a)
	n, plus := 0, ""
	if res.Output != "no matches" {
		n = countLines(res.Output)
	}
	if strings.Contains(res.Output, "[... more matches]") {
		plus = "+"
	}
	return fmt.Sprintf("[grep %q: %d%s matches shown earlier]", clipRunes(a.Pattern, stubNameRunes), n, plus)
}

// Stub of list_dir: the folder and the number of entries that were shown.
func (ListDir) Stub(args json.RawMessage, res Result) string {
	p := pathOf(args)
	if p == "" {
		p = "."
	}
	return fmt.Sprintf("[list_dir %s: %d entries shown earlier]", p, countLines(res.Output))
}

// Stub of shell: the command, the status and the last output lines (more after a failure).
func (Shell) Stub(args json.RawMessage, res Result) string {
	var a shellArgs
	_ = json.Unmarshal(args, &a)
	cmd, _, _ := strings.Cut(strings.TrimSpace(a.Command), "\n")
	body, status := splitStatus(res.Output)
	if status == "" {
		status = res.Brief
	}
	n := stubFailLines
	if status == "exit 0" {
		n = stubOKLines
	}
	return stubWithTail(fmt.Sprintf("[shell %s: %s]", clipRunes(cmd, stubNameRunes), status), body, n)
}

// Stub of shell_output: the job id, the status and the last lines.
func (ShellOutput) Stub(args json.RawMessage, res Result) string {
	var a jobIDArgs
	_ = json.Unmarshal(args, &a)
	body, status := splitStatus(res.Output)
	id := a.ID
	if m := jobPrefix.FindStringSubmatch(body); id == "" && m != nil {
		id = m[1]
	}
	if id == "" {
		id = "latest"
	}
	if status == "" {
		status = res.Brief
	}
	return stubWithTail(fmt.Sprintf("[shell_output %s: %s]", id, status), body, stubOKLines)
}

// jobPrefix matches the "bgN: " the unnamed shell_output prepends to its output.
var jobPrefix = regexp.MustCompile(`^(bg\d+): `)

// Touched is the path edit_file changed.
func (EditFile) Touched(args json.RawMessage) string { return touchedPath(args) }

// Touched is the path write_file changed.
func (WriteFile) Touched(args json.RawMessage) string { return touchedPath(args) }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// touchedPath is the cleaned path of the args; absolute paths stay absolute.
func touchedPath(args json.RawMessage) string {
	p := pathOf(args)
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// splitStatus splits a shell-style result into its body and the last "[status]" line without
// the brackets; status is "" when the last line is not bracketed.
func splitStatus(out string) (body, status string) {
	out = strings.TrimRight(out, "\n")
	i := strings.LastIndexByte(out, '\n')
	last := out[i+1:]
	if !strings.HasPrefix(last, "[") || !strings.HasSuffix(last, "]") {
		return out, ""
	}
	if i < 0 {
		return "", last[1 : len(last)-1]
	}
	return out[:i], last[1 : len(last)-1]
}

// stubWithTail appends the last n non-empty lines of body to head, each cut to stubLineBytes.
func stubWithTail(head, body string, n int) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, l := range lines {
		head += "\n" + clipBytes(strings.TrimRight(l, "\r"), stubLineBytes)
	}
	return head
}

// countLines counts the content lines of a result, not the "[... ]" notes.
func countLines(out string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if l != "" && !strings.HasPrefix(l, "[... ") {
			n++
		}
	}
	return n
}

// clipRunes cuts s to n runes, marking the cut with "...".
func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// clipBytes cuts s to n bytes without splitting a UTF-8 sequence.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
