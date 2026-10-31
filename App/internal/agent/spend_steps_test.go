package agent

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// spend.feature
// ---------------------------------------------------------------------------

// spendWorld is the state of the spend scenarios.
type spendWorld struct{ warn float64 }

func (w *world) spendW() *spendWorld {
	if w.spd == nil {
		w.spd = &spendWorld{}
	}
	return w.spd
}

// plugSpend gives the Agent the warning level; called by begin.
func (w *world) plugSpend(d *Deps) {
	if w.spd != nil {
		d.WarnUSD = w.spd.warn
	}
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

func registerSpendSteps(sc *godog.ScenarioContext, w *world) {
	// the cost goes onto the latest scripted reply, as the usage chunk of a paid server
	sc.Step(`^the reply costs ([\d.]+) USD$`, func(usd float64) error {
		if len(w.replies) == 0 {
			return fmt.Errorf("no scripted reply to put the cost on")
		}
		r := &w.replies[len(w.replies)-1]
		r.Usage = &chat.Usage{PromptTokens: 10, CompletionTokens: 2, Cost: usd}
		return nil
	})
	sc.Step(`^the warning level is ([\d.]+) USD$`, func(usd float64) error {
		w.spendW().warn = usd
		return nil
	})
	sc.Step(`^a saved Session with a spend of ([\d.]+) USD on the model "([^"]*)"$`, func(usd float64, model string) error {
		return w.saveSession(twoTurns(), fmt.Sprintf(`{"spend":{"model":%q,"usd":%v}}`+"\n", model, usd))
	})
	sc.Step(`^the Agent has loaded it$`, w.settle)
	sc.Step(`^the Session spend is ([\d.]+) USD$`, func(want float64) error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.ag.Spend().Total; !near(got, want) {
			return fmt.Errorf("Session spend %v, want %v", got, want)
		}
		return nil
	})
	sc.Step(`^the Session spend is ([\d.]+) USD on the model "([^"]*)" and ([\d.]+) USD on the model "([^"]*)"$`,
		func(a float64, ma string, b float64, mb string) error {
			if err := w.settle(); err != nil {
				return err
			}
			by := w.ag.Spend().ByModel
			if !near(by[ma], a) || !near(by[mb], b) || len(by) != 2 {
				return fmt.Errorf("spend per model %v, want %s %v and %s %v", by, ma, a, mb, b)
			}
			return nil
		})
	sc.Step(`^the Session spend is 0 USD and the SessionFile holds no spend line$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if t := w.ag.Spend().Total; t != 0 {
			return fmt.Errorf("Session spend %v, want 0", t)
		}
		b, err := os.ReadFile(w.sessions().path)
		if err == nil && strings.Contains(string(b), `"spend"`) {
			err = fmt.Errorf("the SessionFile holds a spend line")
		}
		return err
	})
	sc.Step(`^the Session spend and the replayed SessionFile spend are ([\d.]+) USD$`, func(want float64) error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.ag.Spend().Total; !near(got, want) {
			return fmt.Errorf("Session spend %v, want %v", got, want)
		}
		l, err := session.Load(w.sessions().path)
		if err != nil {
			return err
		}
		var got float64
		for _, v := range l.Spend() {
			got += v
		}
		if !near(got, want) {
			return fmt.Errorf("replayed spend %v, want %v", got, want)
		}
		return nil
	})
	sc.Step(`^the user was warned about the spend once$`, func() error { return w.spendWarnings(1) })
	sc.Step(`^the user was warned about the spend once and the Turn answered "([^"]*)"$`, func(ans string) error {
		if err := w.spendWarnings(1); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err == nil && m.Content != ans {
			err = fmt.Errorf("last answer %q, want %q", m.Content, ans)
		}
		return err
	})
	sc.Step(`^the user was not warned about the spend$`, func() error { return w.spendWarnings(0) })
}

// spendWarnings checks the number of spend warnings shown.
func (w *world) spendWarnings(want int) error {
	if err := w.settle(); err != nil {
		return err
	}
	n := 0
	for _, t := range w.warnings() {
		if strings.Contains(t, "warning level") {
			n++
		}
	}
	if n != want {
		return fmt.Errorf("%d spend warnings, want %d (all: %q)", n, want, w.warnings())
	}
	return nil
}
