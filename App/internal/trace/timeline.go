package trace

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

const (
	outputLines = 3   // lines of a tool output shown under tool_end
	lineRunes   = 120 // cut of one shown line
)

// Timeline renders the trace read from r as a readable timeline, one line per
// record (tool_end adds the first non-blank lines of the output). Lines that are not
// valid records (a torn last line, garbage) are skipped; unknown kinds are
// printed generically as key=value of their scalar fields.
func Timeline(r io.Reader, w io.Writer) error {
	rd := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			if werr := renderLine(line, w); werr != nil {
				return werr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func renderLine(line []byte, w io.Writer) error {
	var e struct {
		Ms   int64          `json:"ms"`
		Kind string         `json:"kind"`
		D    map[string]any `json:"d"`
	}
	if json.Unmarshal(line, &e) != nil || e.Kind == "" {
		return nil
	}
	head := fmt.Sprintf("%7s %-10s", fmt.Sprintf("+%d.%ds", e.Ms/1000, e.Ms%1000/100), e.Kind)
	text, extra := describe(e.Kind, e.D)
	if id, _ := e.D["task"].(string); id != "" { // a record made inside a Task (T4)
		text = "task=" + id + " " + text
	}
	if _, err := fmt.Fprintf(w, "%s\n", strings.TrimRight(head+" "+text, " ")); err != nil {
		return err
	}
	for _, l := range extra {
		if _, err := fmt.Fprintf(w, "%*s%s\n", len(head)+1, "", l); err != nil {
			return err
		}
	}
	return nil
}

// describe returns the summary of a record and extra lines shown below it.
func describe(kind string, d map[string]any) (string, []string) {
	switch kind {
	case "request":
		s := fmt.Sprintf("step=%s msgs=+%d", field(d, "step"), added(d))
		if v := field(d, "msgs"); v != "" {
			s += " total=" + v
		}
		return s + opt(d, "model", "model"), nil
	case "delta":
		return fmt.Sprintf("text=%q calls=%d", short(str(d, "text"), 100), count(d["calls"])), nil
	case "usage":
		p, c := num(d, "prompt"), num(d, "cached")
		s := fmt.Sprintf("prompt=%d completion=%d cached=%d", p, num(d, "completion"), c)
		if p > 0 {
			s += fmt.Sprintf(" cache=%d%%", c*100/p)
		}
		return s, nil
	case "tool_start":
		return str(d, "name") + " " + short(anyText(d["args"]), 200), nil
	case "tool_end":
		out := str(d, "output")
		s := str(d, "name")
		if b := str(d, "brief"); b != "" {
			s += " brief=" + fmt.Sprintf("%q", short(b, 80))
		}
		s += fmt.Sprintf(" %dms size=%dB", num(d, "ms"), len(out))
		if e := str(d, "error"); e != "" {
			s += " error=" + fmt.Sprintf("%q", short(e, 100))
		}
		var extra []string
		for _, l := range strings.Split(out, "\n") {
			if len(extra) == outputLines {
				break
			}
			if strings.TrimSpace(l) != "" {
				extra = append(extra, "| "+short(l, lineRunes))
			}
		}
		return s, extra
	}
	return generic(d), nil
}

// ---------------------------------------------------------------------------
// Field helpers (payloads are free-form JSON)
// ---------------------------------------------------------------------------

func str(d map[string]any, k string) string {
	if s, ok := d[k].(string); ok {
		return s
	}
	return ""
}

func num(d map[string]any, k string) int {
	if f, ok := d[k].(float64); ok {
		return int(f)
	}
	return 0
}

// field renders a scalar field of any type, "" when absent.
func field(d map[string]any, k string) string {
	switch v := d[k].(type) {
	case nil:
		return ""
	case float64:
		return fmt.Sprintf("%d", int(v))
	case string:
		return v
	default:
		return anyText(v)
	}
}

func opt(d map[string]any, k, label string) string {
	if v := field(d, k); v != "" {
		return " " + label + "=" + v
	}
	return ""
}

// added is the number of messages appended since the previous request: the
// length of "added" when it is a list, else the number in "added" / "new".
func added(d map[string]any) int {
	if l, ok := d["added"].([]any); ok {
		return len(l)
	}
	if n := num(d, "added"); n > 0 {
		return n
	}
	return num(d, "new")
}

func count(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case float64:
		return int(x)
	}
	return 0
}

// anyText renders a payload value: strings as they are, the rest as JSON.
func anyText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := encodeLine(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// generic prints scalar fields as key=value (sorted) and sizes of the others.
func generic(d map[string]any) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(d)) {
		switch v := d[k].(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", k, short(v, 100)))
		case []any:
			if j := anyText(v); len(j) <= 60 {
				parts = append(parts, k+"="+j)
			} else {
				parts = append(parts, fmt.Sprintf("%s=[%d]", k, len(v)))
			}
		case map[string]any:
			parts = append(parts, fmt.Sprintf("%s={%d}", k, len(v)))
		default:
			parts = append(parts, k+"="+field(d, k))
		}
	}
	return strings.Join(parts, " ")
}
