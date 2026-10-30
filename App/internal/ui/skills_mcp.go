package ui

import (
	"errors"
	"fmt"
	"strings"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// /skill, /skills (R-83, R-84)
// ---------------------------------------------------------------------------

// skillLine reads "/<name> [text]" as /skill when <name> is a loaded Skill (a built-in command
// of that name wins: ParseCommand succeeded before this is asked).
func (m *model) skillLine(line string) (name, text string, ok bool) {
	f := strings.Fields(strings.TrimSpace(line))
	if m.d.Skills == nil || len(f) == 0 || !strings.HasPrefix(f[0], "/") || strings.HasPrefix(f[0], "//") {
		return "", "", false
	}
	name = f[0][1:]
	for _, s := range m.d.Skills.List() {
		if s.Name == name {
			return name, strings.Join(f[1:], " "), true
		}
	}
	return "", "", false
}

// submitSkill queues a /skill Turn; the waiting line shows the text the agent will answer.
func (m *model) submitSkill(name, text string) {
	ss, ok := m.d.Agent.(skillSubmitter)
	err := agent.ErrNoSkills
	if ok {
		err = ss.SubmitSkill(name, text)
	}
	switch {
	case errors.Is(err, agent.ErrNoSkills):
		m.say("no skills loaded (see /skills)")
	case err != nil:
		m.say("skill: " + err.Error())
	default:
		if strings.TrimSpace(text) == "" {
			text = agent.SkillOnlyText
		}
		m.addWaiting(text) // shown at once, before the agent takes it (R-27)
	}
}

// skills opens the Skills menu or switches one (applied now, saved to the scope).
func (m *model) skills(c Command) {
	if m.d.Skills == nil {
		m.say("no skills loaded: put a folder with SKILL.md under <config>/skills or .swarg/skills")
		return
	}
	if c.Name == "" {
		m.openMenu(m.skillsMenu(), nil)
		return
	}
	scope := m.scopeOf(c.Scope)
	note, err := saved(scope, fmt.Sprintf("skill %s: %s", c.Name, onOff(c.On)), m.d.Skills.SetEnabled(c.Name, c.On, scope))
	if err != nil {
		m.say(err.Error())
		return
	}
	m.say(note + " (from the next request)")
}

// ---------------------------------------------------------------------------
// /mcp (R-85..R-88)
// ---------------------------------------------------------------------------

// mcp opens the servers menu, one server's tools, or switches / restarts / approves one.
func (m *model) mcp(c Command) {
	if m.d.MCP == nil {
		m.say("no MCP servers configured (mcp.servers in config.json)")
		return
	}
	switch c.Text {
	case "":
		m.openMenu(m.mcpMenu(), nil)
	case "show":
		if sub := m.mcpServerMenu(c.Name); sub != nil {
			m.openMenu(sub, nil)
		}
	case "restart":
		if err := m.d.MCP.Restart(c.Name); err != nil {
			m.say("mcp " + c.Name + ": " + err.Error())
			return
		}
		m.say("mcp " + c.Name + ": running (tools offered from the next request)")
	case "approve":
		if err := m.d.MCP.Approve(c.Name); err != nil {
			m.say("mcp " + c.Name + ": " + err.Error())
			return
		}
		m.say("mcp " + c.Name + ": approved and started (tools offered from the next request)")
	default: // on | off
		scope := m.scopeOf(c.Scope)
		note, err := saved(scope, fmt.Sprintf("mcp %s: %s", c.Name, onOff(c.On)), m.d.MCP.SetEnabled(c.Name, c.On, scope))
		if err != nil {
			m.say(err.Error())
			return
		}
		m.say(note + " (from the next request)")
	}
}
