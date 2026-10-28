package trace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func open(t *testing.T) (*Writer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traces", "s.trace.jsonl")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, path
}

type rec struct {
	T    string          `json:"t"`
	Ms   int64           `json:"ms"`
	Kind string          `json:"kind"`
	D    json.RawMessage `json:"d"`
}

func readRecs(t *testing.T, path string) []rec {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []rec
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			var r rec
			if e := json.Unmarshal(line, &r); e != nil {
				t.Fatalf("invalid line %q: %v", line[:min(len(line), 80)], e)
			}
			out = append(out, r)
		}
		if err != nil {
			return out
		}
	}
}

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

func TestTrace_RecordShape(t *testing.T) {
	w, path := open(t)
	base := time.Now()
	ticks := []time.Duration{0, 1500 * time.Millisecond}
	i := 0
	w.start = base
	w.now = func() time.Time { d := ticks[min(i, len(ticks)-1)]; i++; return base.Add(d) }
	w.Record("request", map[string]any{"step": 1, "text": "a<b>&\n"})
	w.Record("approval", nil)
	recs := readRecs(t, path)
	if len(recs) != 2 {
		t.Fatalf("%d records", len(recs))
	}
	if _, err := time.Parse(time.RFC3339Nano, recs[0].T); err != nil {
		t.Errorf("t %q: %v", recs[0].T, err)
	}
	if recs[0].Kind != "request" || recs[0].Ms != 0 || recs[1].Ms != 1500 || recs[1].Kind != "approval" {
		t.Errorf("recs %+v", recs)
	}
	if !strings.Contains(string(recs[0].D), `"text":"a<b>&\n"`) {
		t.Errorf("payload %s", recs[0].D)
	}
	if len(recs[1].D) != 0 {
		t.Errorf("nil payload stored as %s", recs[1].D)
	}
	b, _ := os.ReadFile(path)
	if bytes.Count(b, []byte("\n")) != 2 {
		t.Error("not one line per record")
	}
}

func TestTrace_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix modes")
	}
	w, path := open(t)
	w.Record("x", nil)
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir %v", fi.Mode().Perm())
	}
}

func TestTrace_OnDiskImmediately(t *testing.T) {
	w, path := open(t)
	for i := range 3 {
		w.Record("n", map[string]int{"i": i})
		if got := len(readRecs(t, path)); got != i+1 { // crash-safe: no Flush needed
			t.Fatalf("after %d records %d are on disk", i+1, got)
		}
	}
	if err := w.Flush(); err != nil {
		t.Error(err)
	}
}

func TestTrace_NilSafe(t *testing.T) {
	var w *Writer
	w.Record("x", map[string]string{"a": "b"})
	if w.Flush() != nil || w.Err() != nil || w.Close() != nil {
		t.Error("nil writer returned an error")
	}
}

func TestTrace_CapsStrings(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		wantCut bool
	}{{"1MB", 1 << 20, true}, {"just over", MaxString + 1, true}, {"at the cap", MaxString, false}, {"small", 1000, false}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, path := open(t)
			out := strings.Repeat("я", c.size/2) + strings.Repeat("x", c.size%2)
			w.Record("tool_end", map[string]any{"name": "shell", "output": out, "nested": []string{out}})
			var d struct {
				Name   string
				Output string
				Nested []string
			}
			if err := json.Unmarshal(readRecs(t, path)[0].D, &d); err != nil {
				t.Fatal(err)
			}
			if d.Name != "shell" {
				t.Errorf("name %q", d.Name)
			}
			if !c.wantCut {
				if d.Output != out || d.Nested[0] != out {
					t.Error("small output was changed")
				}
				return
			}
			for _, s := range []string{d.Output, d.Nested[0]} {
				if len(s) > MaxString+100 || !strings.Contains(s, "cut at 262144 of ") {
					t.Fatalf("not cut with a marker: len %d", len(s))
				}
				if !strings.Contains(s, "of "+itoa(c.size)+" bytes") {
					t.Errorf("marker lacks the original size %d", c.size)
				}
				if strings.ContainsRune(s, '�') {
					t.Error("cut inside a rune")
				}
			}
		})
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestTrace_FileCapReplacesLargeStrings(t *testing.T) {
	w, path := open(t)
	w.maxFile = 2000
	big := strings.Repeat("z", 5000)
	w.Record("a", map[string]any{"output": big, "n": 7})
	w.Record("b", map[string]any{"output": big, "small": "keep me", "n": 8})
	w.Record("c", map[string]any{"output": big})
	recs := readRecs(t, path)
	var first, third map[string]any
	json.Unmarshal(recs[0].D, &first)
	json.Unmarshal(recs[2].D, &third)
	if first["output"] != big && len(first["output"].(string)) != 5000 {
		t.Error("first record changed before the cap")
	}
	if s := third["output"].(string); !strings.Contains(s, "omitted 5000 bytes") || len(s) > 100 {
		t.Errorf("after the cap: %q", s)
	}
	var second map[string]any
	json.Unmarshal(recs[1].D, &second)
	if second["small"] != "keep me" || second["n"] != float64(8) {
		t.Errorf("small fields: %v", second)
	}
}

func TestTrace_ConcurrentSafe(t *testing.T) {
	w, path := open(t)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				w.Record("n", map[string]any{"g": g, "i": i, "pad": strings.Repeat("p", i*13)})
			}
		}()
	}
	wg.Wait()
	if got := len(readRecs(t, path)); got != 400 {
		t.Errorf("%d records", got)
	}
}

func TestTrace_WriteErrorDoesNotPanic(t *testing.T) {
	w, path := open(t)
	w.Record("ok", nil)
	w.f.Close() // the next write fails
	w.Record("lost", nil)
	if w.Err() == nil {
		t.Fatal("no error kept")
	}
	first := w.Err()
	w.Record("later", nil)
	if w.Err() != first {
		t.Error("error replaced")
	}
	if w.Flush() == nil {
		t.Error("Flush hides the error")
	}
	if got := len(readRecs(t, path)); got != 1 {
		t.Errorf("%d records on disk", got)
	}
}

func TestTrace_UnmarshalablePayloadIsStored(t *testing.T) {
	w, path := open(t)
	w.Record("bad", map[string]any{"f": func() {}})
	w.Record("next", nil)
	recs := readRecs(t, path)
	if len(recs) != 2 || !strings.Contains(string(recs[0].D), "marshal_error") || w.Err() != nil {
		t.Errorf("recs %+v err %v", recs, w.Err())
	}
}

func TestTrace_CloseThenRecordIsNoop(t *testing.T) {
	w, path := open(t)
	w.Record("a", nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w.Record("b", nil)
	if got := len(readRecs(t, path)); got != 1 {
		t.Errorf("%d records", got)
	}
}

func TestOpen_AppendsToExisting(t *testing.T) {
	w, path := open(t)
	w.Record("a", nil)
	w.Close()
	w2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w2.Record("b", nil)
	w2.Close()
	if recs := readRecs(t, path); len(recs) != 2 || recs[1].Kind != "b" {
		t.Errorf("recs %+v", recs)
	}
}

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

func TestTimeline_PrintsReadable(t *testing.T) {
	in, err := os.Open("testdata/session.trace.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var out bytes.Buffer
	if err := Timeline(in, &out); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/session.timeline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("timeline differs from golden:\n%s", out.String())
	}
}

func TestTimeline_TornAndUnknownKinds(t *testing.T) {
	in := `{"t":"x","ms":100,"kind":"future_kind","d":{"a":1,"b":"two","c":[1,2]}}` + "\n" +
		"garbage\n" + `{"ms":5}` + "\n" + `{"t":"x","ms":200,"kind":"warning","d":{"text":"w"}}` + "\n" +
		`{"t":"x","ms":300,"kind":"usage","d":{"prom`
	var out bytes.Buffer
	if err := Timeline(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `a=1 b="two" c=[1,2]`) || !strings.Contains(lines[1], "text=") && !strings.Contains(lines[1], `"w"`) {
		t.Errorf("got %q", lines)
	}
}

func TestTimeline_RoundTripWithWriter(t *testing.T) {
	w, path := open(t)
	w.Record("tool_end", map[string]any{"name": "shell", "ms": 12, "output": strings.Repeat("l\n", 10)})
	f, _ := os.Open(path)
	defer f.Close()
	var out bytes.Buffer
	Timeline(f, &out)
	if n := strings.Count(out.String(), "\n"); n != 1+outputLines {
		t.Errorf("%d lines:\n%s", n, out.String())
	}
}

func TestTimeline_ShowsTaskId(t *testing.T) {
	in := `{"t":"x","ms":100,"kind":"tool_start","d":{"id":"c1","name":"read_file","args":"{}","task":"t1"}}` + "\n" +
		`{"t":"x","ms":200,"kind":"tool_start","d":{"id":"c2","name":"task","args":"{}"}}` + "\n"
	var out bytes.Buffer
	if err := Timeline(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "task=t1 read_file") || strings.Contains(lines[1], "task=") {
		t.Errorf("got %q", lines)
	}
}
