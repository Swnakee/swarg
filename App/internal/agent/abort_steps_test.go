package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/cucumber/godog"
)

// ---------------------------------------------------------------------------
// Steps for abort.feature; the Provider scripts and the first-interrupt steps
// are shared with interrupt.feature. Health records that it ran, so a scenario
// can wait for the asynchronous check without sleeping.
// ---------------------------------------------------------------------------

// healthCheck sets a Health function with the given outcome; ran closes after it returned.
func (w *world) healthCheck(err error) {
	ran := make(chan struct{})
	w.healthRan = ran
	w.health = func(context.Context) error {
		defer close(ran)
		return err
	}
}

func registerAbortSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a health check that fails$`, func() error { w.healthCheck(errors.New("down")); return nil })
	sc.Step(`^a health check that succeeds$`, func() error { w.healthCheck(nil); return nil })
	sc.Step(`^the user interrupts the Turn again$`, func() error {
		if err := w.begin(); err != nil {
			return err
		}
		w.ag.Interrupt()
		if w.healthRan != nil { // the abort ran the check: wait until it returned
			return waitChan("the health check", w.healthRan)
		}
		return nil
	})
	sc.Step(`^the user aborts the Turn$`, func() error {
		if err := w.begin(); err != nil {
			return err
		}
		w.ag.Abort()
		return nil
	})
	sc.Step(`^the ToolResult is "([^"]*)"$`, func(want string) error { return w.wantToolResult("", want) })

	sc.Step(`^the Provider connection stays open$`, func() error {
		if err := w.waitFor("the stream drain", func(evs []Event) bool { return countKind(evs, EvDraining) >= 1 }); err != nil {
			return err
		}
		select {
		case <-w.srv.Gone():
			return errors.New("the Provider connection was closed by the first interrupt")
		default:
		}
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.srv.Completed(); got != 1 {
			return fmt.Errorf("responses fully read = %d, want 1", got)
		}
		return nil
	})
	sc.Step(`^the Provider connection is closed$`, func() error {
		return waitChan("the Provider connection to close", w.srv.Gone())
	})
	sc.Step(`^the next request is sent before the first response is read to its end$`, func() error {
		// The server must see the dropped connection before settle releases the gate;
		// otherwise it may finish writing into the dead socket and count it completed.
		if err := waitChan("the Provider connection to close", w.srv.Gone()); err != nil {
			return err
		}
		if err := w.settle(); err != nil {
			return err
		}
		at := w.srv.CompletedAt()
		if len(at) != 2 || at[1] != 0 {
			return fmt.Errorf("Completed() at request arrival = %v, want 2 requests and the second at 0", at)
		}
		return nil
	})
}
