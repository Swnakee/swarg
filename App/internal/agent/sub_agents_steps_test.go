package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Steps for sub_agents.feature. The agent's Source is the scenario's fake server; its system
// prompt is "AGENT:<type>", so the requests of agents can be told from the Session's.
// ---------------------------------------------------------------------------

// subAgentWorld is the state of the sub-agent scenarios.
type subAgentWorld struct{ types []AgentType }

func (w *world) subAgents() *subAgentWorld {
	if w.sag == nil {
		w.sag = &subAgentWorld{}
	}
	return w.sag
}

// plugAgents gives the Agent its agent types; called by begin.
func (w *world) plugAgents(d *Deps) {
	if w.sag == nil {
		return
	}
	types := w.sag.types
	d.Agents = AgentDeps{
		Types: func() []AgentType { return types },
		Open: func(t AgentType) (AgentRun, error) {
			model := t.Model
			if model == "" {
				model = "m"
			}
			return AgentRun{Source: Source{Name: "srv", Provider: d.Provider, Model: model, MaxTokens: d.MaxTokens, ContextLimit: d.ContextLimit},
				System: "AGENT:" + t.Name}, nil
		},
	}
}

// agentRequests returns the 0-based numbers of the requests made by agents.
func (w *world) agentRequests() (out []int) {
	for i := range w.srv.Requests() {
		msgs, _ := w.requestMessages(i)
		if len(msgs) > 0 {
			if s, _ := msgs[0]["content"].(string); strings.HasPrefix(s, "AGENT:") {
				out = append(out, i)
			}
		}
	}
	return out
}

// agentResult returns the first ToolResult of an `agent` call.
func (w *world) agentResult() (chat.Message, error) {
	if err := w.settle(); err != nil {
		return chat.Message{}, err
	}
	for _, m := range w.toolMessages() {
		if m.Name == agentTool {
			return m, nil
		}
	}
	return chat.Message{}, fmt.Errorf("no agent result in the Session history")
}

func registerSubAgentSteps(sc *godog.ScenarioContext, w *world) {
	on := func(name, tools string, steps int, model string) error {
		w.real = true
		sag := w.subAgents()
		sag.types = append(sag.types, AgentType{Name: name, Description: "d", Tools: strings.Split(tools, ","), MaxSteps: steps, Model: model})
		return nil
	}
	sc.Step(`^the agent type "([^"]*)" with the tools "([^"]*)" is on offer$`, func(n, tl string) error { return on(n, tl, 0, "") })
	sc.Step(`^the agent type "([^"]*)" with the tools "([^"]*)" and (\d+) steps is on offer$`, func(n, tl string, s int) error {
		return on(n, tl, s, "")
	})
	sc.Step(`^the agent type "([^"]*)" with the tools "([^"]*)" and the model "([^"]*)" is on offer$`, func(n, tl, m string) error {
		return on(n, tl, 0, m)
	})
	sc.Step(`^no agent type is on offer$`, func() error { w.real = true; w.subAgents(); return nil })
	sc.Step(`^the Provider starts an agent "([^"]*)" with the goal "([^"]*)"$`, func(typ, goal string) error {
		w.real = true
		w.reply(w.toolReply([2]string{agentTool, jsonArgs("type", typ, "goal", goal)}))
		return nil
	})

	sc.Step(`^every agent request starts with the system prompt "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		reqs := w.agentRequests()
		for _, i := range reqs {
			msgs, _ := w.requestMessages(i)
			if got, _ := msgs[0]["content"].(string); got != want {
				return fmt.Errorf("system prompt %q, want %q", got, want)
			}
		}
		if len(reqs) == 0 {
			return fmt.Errorf("no agent request was made")
		}
		return nil
	})
	sc.Step(`^every agent request names the model "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		reqs := w.agentRequests()
		for _, i := range reqs {
			r, _ := w.request(i)
			if got, _ := r["model"].(string); got != want {
				return fmt.Errorf("model %q, want %q", got, want)
			}
		}
		if len(reqs) == 0 {
			return fmt.Errorf("no agent request was made")
		}
		return nil
	})
	sc.Step(`^no agent request offers the tool "([^"]*)" and every one offers "([^"]*)"$`, func(no, yes string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, i := range w.agentRequests() {
			names, _ := w.requestTools(i)
			if contains(names, no) || !contains(names, yes) {
				return fmt.Errorf("agent request %d offers %v", i+1, names)
			}
		}
		return nil
	})
	sc.Step(`^no agent request offers the tool "([^"]*)" or "([^"]*)" and the first request offers both$`, func(a, b string) error {
		if err := w.settle(); err != nil {
			return err
		}
		first, _ := w.requestTools(0)
		if !contains(first, a) || !contains(first, b) {
			return fmt.Errorf("the first request offers %v", first)
		}
		for _, i := range w.agentRequests() {
			names, _ := w.requestTools(i)
			if contains(names, a) || contains(names, b) {
				return fmt.Errorf("agent request %d offers %v", i+1, names)
			}
		}
		return nil
	})
	sc.Step(`^the first request (does not offer|offers) the tool "([^"]*)"$`, func(how, tool string) error {
		if err := w.settle(); err != nil {
			return err
		}
		names, err := w.requestTools(0)
		if err != nil {
			return err
		}
		if contains(names, tool) != (how == "offers") {
			return fmt.Errorf("the first request offers %v", names)
		}
		return nil
	})
	sc.Step(`^the agent's report starts with "([^"]*)"$`, func(prefix string) error {
		res, err := w.agentResult()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(res.Content, prefix) {
			return fmt.Errorf("agent result = %q, want the prefix %q", res.Content, prefix)
		}
		return nil
	})
	sc.Step(`^the agent's report starts with "([^"]*)" and no agent request was made$`, func(prefix string) error {
		res, err := w.agentResult()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(res.Content, prefix) || len(w.agentRequests()) != 0 {
			return fmt.Errorf("agent result = %q, agent requests = %d", res.Content, len(w.agentRequests()))
		}
		return nil
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
