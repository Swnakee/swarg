package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Spend in the panel (R-112) and /cost (R-113)
// ---------------------------------------------------------------------------

// spendAgent is the fake agent plus Spend (the optional spender part).
type spendAgent struct {
	*fakeAgent
	sp agent.Spend
}

func (a *spendAgent) Spend() agent.Spend { return a.sp }

type fakeCost struct {
	ku    KeyUsage
	err   error
	calls int
}

func (f *fakeCost) KeyUsage(context.Context) (KeyUsage, error) { f.calls++; return f.ku, f.err }

func ptr(v float64) *float64 { return &v }

func TestPanel_SpendShownNextToMeterOnlyWhenPositive(t *testing.T) {
	r := newRig(t)
	if strings.Contains(r.panelText(), "$") {
		t.Fatalf("no spend yet, panel %q", r.panelText())
	}
	r.events(agent.Event{Kind: agent.EvSpend, Spend: 0.0012})
	p := r.panelText()
	if !strings.Contains(p, "$0.0012") {
		t.Fatalf("panel %q lacks the spend", p)
	}
	if strings.Index(p, "$0.0012") < strings.Index(p, "16k") || strings.Index(p, "$0.0012") > strings.Index(p, "tok/s") {
		t.Errorf("spend is not between the meter and the speed: %q", p)
	}
	r.events(agent.Event{Kind: agent.EvSpend, Spend: 0})
	if strings.Contains(r.panelText(), "$") {
		t.Errorf("zero spend must hide the item: %q", r.panelText())
	}
}

func TestCost_ShowsSessionPerModelAndKey(t *testing.T) {
	r := newRig(t)
	r.m.d.Agent = &spendAgent{fakeAgent: r.ag, sp: agent.Spend{Total: 0.75, ByModel: map[string]float64{"cheap/m": 0.25, "dear/m": 0.5}}}
	fc := &fakeCost{}
	r.m.d.Cost = fc
	r.cmd("/cost")
	c := r.convText()
	if fc.calls != 1 {
		t.Fatalf("key usage asked %d times, want once (asynchronously)", fc.calls)
	}
	if !strings.Contains(c, "Session spend: $0.750") || strings.Index(c, "dear/m") > strings.Index(c, "cheap/m") || !strings.Contains(c, "$0.500") {
		t.Errorf("session part: %q", c)
	}
	r.send(costMsg{ku: KeyUsage{Label: "work", Limit: ptr(10), Remaining: ptr(7.5), Usage: 2.5, Daily: 0.5, Weekly: 1, Monthly: 2.5}})
	c = r.convText()
	for _, want := range []string{"Key work:", "today $0.500", "month $2.50", "limit $10.00,", "remaining $7.50"} {
		if !strings.Contains(c, want) {
			t.Errorf("key part lacks %q: %q", want, c)
		}
	}
}

func TestCost_KeyWithoutLimit(t *testing.T) {
	r := newRig(t)
	r.send(costMsg{ku: KeyUsage{Usage: 1}})
	if c := r.convText(); !strings.Contains(c, "no limit") {
		t.Errorf("%q", c)
	}
}

func TestCost_NoSpendYetAndNoKeyInfo(t *testing.T) {
	r := newRig(t)
	r.m.d.Cost = &fakeCost{err: ErrNoKeyInfo}
	r.cmd("/cost")
	r.send(costMsg{err: ErrNoKeyInfo})
	c := r.convText()
	if !strings.Contains(c, "Session spend: $0.0000 (no paid response yet)") {
		t.Errorf("session part: %q", c)
	}
	if strings.Contains(c, "Key") || strings.Contains(c, "key usage") {
		t.Errorf("a source without key info must add nothing: %q", c)
	}
}

func TestCost_KeyQueryFailureKeepsSessionPartAndLeaksNothing(t *testing.T) {
	r := newRig(t)
	r.m.d.Agent = &spendAgent{fakeAgent: r.ag, sp: agent.Spend{Total: 0.1, ByModel: map[string]float64{"m": 0.1}}}
	r.m.d.Cost = &fakeCost{}
	r.cmd("/cost")
	r.send(costMsg{err: errors.New("the model server refused the API key (401 Unauthorized)\nsecond line")})
	c := r.convText()
	if !strings.Contains(c, "Session spend: $0.100") || !strings.Contains(c, "key usage: the model server refused the API key (401 Unauthorized)") {
		t.Errorf("%q", c)
	}
	if strings.Contains(c, "second line") {
		t.Errorf("only the first line of the error is shown: %q", c)
	}
}

func TestCost_ListedInHelp(t *testing.T) {
	if !strings.Contains(helpText(), "/cost") {
		t.Error("/cost is not in /help")
	}
}
