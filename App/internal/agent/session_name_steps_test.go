package agent

import (
	"errors"
	"fmt"
	"os"

	"github.com/cucumber/godog"

	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// Steps for session_name.feature: the Name record of a SessionFile (R-32, I-11)
// ---------------------------------------------------------------------------

func registerSessionNameSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the user names the Session "([^"]*)"$`, func(name string) error {
		if err := w.begin(); err != nil {
			return err
		}
		s := w.sessions()
		if s.writer == nil {
			return errors.New("the scenario has no SessionFile being written")
		}
		s.nameErr = s.writer.Name(name)
		return nil
	})
	sc.Step(`^the user named the Session "([^"]*)"$`, func(name string) error {
		w.act(func() error {
			s := w.sessions()
			if s.writer == nil {
				return errors.New("the scenario has no SessionFile being written")
			}
			return s.writer.Name(name)
		})
		return nil
	})
	sc.Step(`^that Session was named "([^"]*)"$`, func(name string) error {
		s := w.sessions()
		wr, err := session.Open(s.path)
		if err != nil {
			return err
		}
		if err := wr.Name(name); err != nil {
			return err
		}
		return wr.Close()
	})
	sc.Step(`^its file holds a record of an unknown kind$`, func() error {
		f, err := os.OpenFile(w.sessions().path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(`{"future_kind":{"a":1}}` + "\n")
		f.Close()
		return err
	})
	sc.Step(`^the user lists the saved Sessions$`, func() error {
		infos, err := session.List(w.sessionsDir())
		if err != nil {
			return err
		}
		w.sessions().listed = infos
		return nil
	})
	sc.Step(`^the SessionFile's Name is "([^"]*)"$`, func(want string) error { return w.wantName(want) })
	sc.Step(`^the naming is refused and the SessionFile's Name is "([^"]*)"$`, func(want string) error {
		if w.sessions().nameErr == nil {
			return errors.New("the naming was not refused")
		}
		return w.wantName(want)
	})
	sc.Step(`^the Session is listed as "([^"]*)"$`, func(want string) error {
		infos := w.sessions().listed
		if len(infos) != 1 {
			return fmt.Errorf("%d Sessions listed, want 1", len(infos))
		}
		if infos[0].Title != want {
			return fmt.Errorf("listed as %q, want %q", infos[0].Title, want)
		}
		return nil
	})
}

// wantName settles the Agent and checks the Name the SessionFile replays to.
func (w *world) wantName(want string) error {
	if err := w.settle(); err != nil {
		return err
	}
	l, err := session.Load(w.sessions().path)
	if err != nil {
		return err
	}
	if l.Name != want {
		return fmt.Errorf("the SessionFile's Name is %q, want %q", l.Name, want)
	}
	return nil
}
