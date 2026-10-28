package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ask runs Approve in a goroutine against a rig and delivers the askMsg to the model.
func (r *rig) ask(ctx context.Context, name, args string) <-chan agent.Approval {
	out := make(chan agent.Approval, 1)
	msgs := make(chan tea.Msg, 4)
	r.m.d.Approver.Bind(func(m tea.Msg) { msgs <- m })
	go func() { out <- r.m.d.Approver.Approve(ctx, chat.ToolCall{Name: name, Args: args}) }()
	r.send(<-msgs)
	return out
}

func (r *rig) type_(s string) {
	for _, c := range s {
		r.send(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
}

func recv(t *testing.T, ch <-chan agent.Approval) agent.Approval {
	t.Helper()
	select {
	case ap := <-ch:
		return ap
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
		return agent.Approval{}
	}
}

func TestApprover_UnboundAnswersByFlag(t *testing.T) {
	call := chat.ToolCall{Name: "shell", Args: `{"command":"ls"}`}
	if ap := NewApprover(true).Approve(context.Background(), call); !ap.Allow {
		t.Error("--yes must allow")
	}
	ap := NewApprover(false).Approve(context.Background(), call)
	if ap.Allow || ap.Note != "not approved: run with --yes to allow" {
		t.Errorf("got %+v", ap)
	}
	a := NewApprover(false)
	a.SetAuto(true)
	if !a.Auto() || !a.Approve(context.Background(), call).Allow {
		t.Error("auto must allow")
	}
}

func TestApprover_CancelledContextDenies(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	msgs := make(chan tea.Msg, 4)
	r.m.d.Approver.Bind(func(m tea.Msg) { msgs <- m })
	out := make(chan agent.Approval, 1)
	go func() { out <- r.m.d.Approver.Approve(ctx, chat.ToolCall{Name: "shell", Args: `{}`}) }()
	r.send(<-msgs)
	if r.m.ask == nil {
		t.Fatal("no prompt")
	}
	cancel()
	if ap := recv(t, out); ap.Allow {
		t.Error("cancel must deny")
	}
	r.send(<-msgs) // askGoneMsg
	if r.m.ask != nil {
		t.Error("prompt not dismissed")
	}
}

func TestModel_ConfirmShowsFullCommand(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.ask(context.Background(), "shell", `{"command":"echo one && echo two && rm -rf build/ && echo this-is-the-end"}`)
	v := r.view()
	if !strings.Contains(v, "Allow shell:") || !strings.Contains(v, "this-is-the-end") || !strings.Contains(v, "[y]es / [n]o / [a]lways") {
		t.Errorf("prompt:\n%s", v)
	}
}

func TestModel_ConfirmKeys(t *testing.T) {
	tests := []struct {
		key       string
		allow     bool
		wantAuto  bool
		wantAfter string
	}{{"y", true, false, ""}, {"n", false, false, ""}, {"a", true, true, "auto"}}
	for _, tc := range tests {
		r := newRig(t)
		r.events(started("q"))
		ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
		r.now = r.now.Add(askDelay)
		r.type_(tc.key)
		ap := recv(t, ch)
		if ap.Allow != tc.allow || r.m.d.Approver.Auto() != tc.wantAuto || r.m.ask != nil {
			t.Errorf("%s: %+v auto %v ask %v", tc.key, ap, r.m.d.Approver.Auto(), r.m.ask)
		}
		if tc.wantAuto && !strings.Contains(r.view(), "auto") {
			t.Error("status lacks auto")
		}
	}
}

func TestModel_ConfirmKeysAreSafe(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.type_("y") // too early: dropped, neither an answer nor text (it would leave the prompt stuck)
	if r.m.ask == nil || r.m.ta.Value() != "" {
		t.Fatalf("early y: ask %v input %q", r.m.ask, r.m.ta.Value())
	}
	r.type_("x") // other keys typed early start a note
	r.now = r.now.Add(askDelay)
	r.type_("n") // input not empty: text, not an answer
	if r.m.ask == nil || r.m.ta.Value() != "xn" {
		t.Fatalf("n over text: ask %v input %q", r.m.ask, r.m.ta.Value())
	}
	r.m.ta.Reset()
	r.type_("x") // other keys never answer
	if r.m.ask == nil {
		t.Error("x answered")
	}
	select {
	case ap := <-ch:
		t.Errorf("answered early: %+v", ap)
	default:
	}
}

func TestModel_ConfirmTextDeniesAndGoesToModel(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.type_("use ls -a")
	r.send(key(tea.KeyEnter, 0))
	ap := recv(t, ch)
	if ap.Allow || ap.Note != "use ls -a" || len(r.ag.submitted) != 1 || r.ag.submitted[0] != "use ls -a" {
		t.Errorf("approval %+v submitted %q", ap, r.ag.submitted)
	}
}

func TestModel_ConfirmEscDeniesAndInterrupts(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.send(key(tea.KeyEscape, 0))
	if ap := recv(t, ch); ap.Allow || r.ag.interrupts != 1 || r.m.ask != nil {
		t.Errorf("approval %+v interrupts %d", ap, r.ag.interrupts)
	}
}

func TestModel_AutoCommand(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("/auto")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "auto-approve on")
	if !r.m.d.Approver.Auto() || !strings.Contains(r.view(), "auto") {
		t.Error("auto not on")
	}
	r.m.ta.SetValue("/auto")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "auto-approve off")
	if r.m.d.Approver.Auto() {
		t.Error("auto not off")
	}
	d := Deps{Agent: r.ag, AutoApprove: true}
	if m := newModel(context.Background(), d, time.Now); !m.d.Approver.Auto() {
		t.Error("AutoApprove must start auto on")
	}
}

func TestDescribeCall(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7"
	tests := []struct {
		name, args string
		want       []string
	}{
		{"shell", "{\"command\":\"ls -la\x1b[2J\"}", []string{"ls -la[2J"}},
		{"edit_file", `{"path":"a.go","old":"` + strings.ReplaceAll(old, "\n", `\n`) + `","new":"x"}`,
			[]string{"a.go", "- 5", "… 2 more lines", "+ x"}},
		{"write_file", `{"path":"b.txt","content":"a\nb\n"}`, []string{"b.txt (2 lines, 4 bytes)"}},
		{"shell", `not json`, []string{"not json"}},
	}
	for _, tc := range tests {
		got := describeCall(chat.ToolCall{Name: tc.name, Args: tc.args}, nil)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", tc.name, got, w)
			}
		}
		if strings.Contains(got, "6") && tc.name == "edit_file" && strings.Contains(got, "- 6") {
			t.Errorf("edit preview not capped: %q", got)
		}
	}
}

func TestModel_ConfirmKeepsOutputArriving(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.events(delta("kept line\n"))
	if v := r.view(); !strings.Contains(v, "kept line") || !strings.Contains(v, "Allow shell: ls?") {
		t.Errorf("output must keep arriving above the prompt:\n%s", v)
	}
	r.now = r.now.Add(askDelay)
	r.type_("y")
	if ap := recv(t, ch); !ap.Allow {
		t.Errorf("approval %+v, want allow", ap)
	}
}

// A prompt over a non-empty input says how to answer; Ctrl+U clears the input and y answers.
func TestModel_ConfirmHintWhenInputNotEmpty(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.now = r.now.Add(askDelay)
	if strings.Contains(r.view(), askHint) {
		t.Fatal("hint shown with an empty input")
	}
	r.type_("ok") // the user started typing a note (or did not see the prompt): the keys are text
	if v := r.view(); !strings.Contains(v, askHint) || r.m.ask == nil {
		t.Fatalf("no hint over the input %q:\n%s", r.m.ta.Value(), v)
	}
	r.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if r.m.ta.Value() != "" || strings.Contains(r.view(), askHint) {
		t.Fatalf("Ctrl+U left %q / the hint", r.m.ta.Value())
	}
	r.type_("y")
	if ap := recv(t, ch); !ap.Allow || r.m.ask != nil {
		t.Fatalf("y after Ctrl+U: %+v ask %v", ap, r.m.ask)
	}
}

// y, n and a that arrive within the guard are dropped; after it the same key answers.
func TestModel_EarlyAnswerKeysDropped(t *testing.T) {
	for _, key := range []string{"y", "n", "a"} {
		r := newRig(t)
		r.events(started("q"))
		ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
		r.type_(key)
		if r.m.ask == nil || r.m.ta.Value() != "" {
			t.Fatalf("%s early: ask %v input %q", key, r.m.ask, r.m.ta.Value())
		}
		r.now = r.now.Add(askDelay)
		r.type_(key)
		if recv(t, ch); r.m.ask != nil {
			t.Fatalf("%s after the guard did not answer", key)
		}
	}
}
