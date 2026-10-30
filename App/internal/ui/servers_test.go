package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// /server (R-100) and the model rows of /model (R-101, R-102)
// ---------------------------------------------------------------------------

type fakeServers struct {
	rows     []ServerRow
	err      error
	probes   int
	switched []string
	notice   string
	fail     error
}

func (f *fakeServers) Servers(context.Context) ([]ServerRow, error) { f.probes++; return f.rows, f.err }
func (f *fakeServers) Switch(name string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.switched = append(f.switched, name)
	return f.notice, nil
}

func sampleServers() []ServerRow {
	return []ServerRow{
		{Name: "local", Type: "openai", Address: "http://127.0.0.1:8080/v1", State: "ok", Active: true},
		{Name: "ollama", Type: "ollama", Address: "http://192.0.2.1:11434/v1", State: "down: connection refused"},
		{Name: "openrouter", Type: "openrouter", Address: "https://openrouter.ai/api/v1", State: "no key: set OPENROUTER_API_KEY"},
	}
}

func TestServerMenu_ListsStatesAndSwitches(t *testing.T) {
	r := newRig(t)
	fs := &fakeServers{rows: sampleServers(), notice: "openrouter sends the conversation to a third party"}
	r.m.d.Servers = fs
	r.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	r.cmd("/server")
	if fs.probes != 1 {
		t.Fatal("/server must probe the sources (asynchronously)")
	}
	r.send(serversMsg{rows: fs.rows})
	v := r.view()
	for _, want := range []string{glyphAuto + " local", "ollama", "down: connection refused", "no key: set OPENROUTER_API_KEY", "https://openrouter.ai/api/v1"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu lacks %q:\n%s", want, v)
		}
	}
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick != nil || len(fs.switched) != 1 || fs.switched[0] != "openrouter" {
		t.Fatalf("enter: switched %v, menu open %v", fs.switched, r.m.pick != nil)
	}
	if c := r.convText(); !strings.Contains(c, "server: openrouter") || !strings.Contains(c, "third party") {
		t.Errorf("notice missing: %q", c)
	}
}

func TestServerCommand_ByNameAndErrors(t *testing.T) {
	r := newRig(t)
	r.cmd("/server local")
	if !strings.Contains(r.convText(), "not available") {
		t.Fatalf("nil Servers: %q", r.convText())
	}
	fs := &fakeServers{}
	r.m.d.Servers = fs
	r.cmd("/server ollama")
	if len(fs.switched) != 1 || fs.switched[0] != "ollama" {
		t.Fatalf("switched %v", fs.switched)
	}
	fs.fail = errors.New("no key: set OPENROUTER_API_KEY")
	r.cmd("/server openrouter")
	if c := r.convText(); !strings.Contains(c, "server: no key: set OPENROUTER_API_KEY") {
		t.Errorf("error not shown: %q", c)
	}
	r.send(serversMsg{err: errors.New("boom")})
	if r.m.pick != nil || !strings.Contains(r.convText(), "sources: boom") {
		t.Errorf("probe error: %q", r.convText())
	}
}

// The panel shows provider/model: the provider part opens /server, the model part /model.
func TestPanel_ProviderAndModelClicks(t *testing.T) {
	r := newRig(t)
	fs := &fakeServers{rows: sampleServers()}
	r.m.d.Servers = fs
	_, fm := r.withModels("qwen")
	r.m.providerName = "local"
	if !strings.Contains(r.panelText(), "local/qwen") {
		t.Fatalf("panel %q", r.panelText())
	}
	r.click(r.panelX("local/")+1, r.panelRow())
	if fs.probes != 1 {
		t.Fatal("a click on the provider must probe the sources")
	}
	r.send(serversMsg{rows: fs.rows})
	if r.m.pick == nil || !strings.Contains(r.view(), "Model sources") {
		t.Fatalf("source menu not open: %q", r.view())
	}
	r.send(key(tea.KeyEscape, 0))
	r.click(r.panelX("qwen")+1, r.panelRow())
	if fm.calls != 1 {
		t.Fatalf("model list fetched %d times", fm.calls)
	}
	r.events(agent.Event{Kind: agent.EvProvider, Text: "openrouter"})
	if !strings.Contains(r.panelText(), "openrouter/qwen") {
		t.Errorf("EvProvider not shown: %q", r.panelText())
	}
}

func TestModelMenu_RecentFirstPricesAndSearch(t *testing.T) {
	r := newRig(t)
	ma, _ := r.withModels()
	rows := []ModelRow{
		{ID: "a/alpha", Name: "Alpha Big", Price: "$1.00/$2.00 per M"},
		{ID: "b/beta:free", Name: "Beta", Price: "free"},
		{ID: "c/gamma", Name: "Gamma", Price: "free", Recent: true},
	}
	r.cmd("/model")
	r.send(modelsMsg{rows: rows})
	v := r.view()
	if r.m.pick == nil || strings.Index(v, "c/gamma") > strings.Index(v, "a/alpha") || !strings.Contains(v, "$1.00/$2.00 per M") || !strings.Contains(v, "free") {
		t.Fatalf("menu:\n%s", v)
	}
	if r.m.modelRows[0].ID != "c/gamma" {
		t.Fatalf("/model N order %v", r.m.modelRows)
	}
	for _, ch := range "Big" { // typing searches the names too
		r.send(ptype(string(ch)))
	}
	r.send(key(tea.KeyEnter, 0))
	if len(ma.set) != 1 || ma.set[0] != "a/alpha" {
		t.Fatalf("search + enter set %v", ma.set)
	}
	r.cmd("/model 1")
	if ma.set[len(ma.set)-1] != "c/gamma" {
		t.Fatalf("/model 1 = %v, want the first shown row", ma.set)
	}
}
