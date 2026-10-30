package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/llm/llmtest"
	"swarg/internal/prompt"
	"swarg/internal/skills"
)

// ---------------------------------------------------------------------------
// Steps for skills.feature: real Skill folders under the scenario's global dir
// (root/skills) and project dir (cwd/.swarg/skills), loaded like main does.
// ---------------------------------------------------------------------------

type skillsWorld struct {
	disabled []string
	set      *skills.Set
	warnings []string
}

func (w *world) skillsDir(global bool, name string) string {
	if global {
		return filepath.Join(w.root, "skills", name)
	}
	return filepath.Join(w.cwd(), ".swarg", "skills", name)
}

// writeSkill writes SKILL.md of a Skill; an empty description writes no front matter.
func (w *world) writeSkill(global bool, name, desc, body string) error {
	w.real = true
	if w.skl == nil {
		w.skl = &skillsWorld{}
	}
	dir := w.skillsDir(global, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	text := body + "\n"
	if desc != "" {
		text = "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644)
}

// plugSkills loads the Skills (like main) and returns the `skills` block; nil without Skills.
func (w *world) plugSkills() []prompt.Block {
	if w.skl == nil {
		return nil
	}
	w.skl.set, w.skl.warnings = skills.Load(filepath.Join(w.root, "skills"), w.cwd(), w.skl.disabled)
	if w.skl.set.Len() == 0 {
		return nil
	}
	return []prompt.Block{{Name: "skills", Source: prompt.SourceTool, Order: 1500, Text: w.skl.set.Block()}}
}

func registerSkillsSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Skill "([^"]*)" described as "([^"]*)" with the instructions "([^"]*)"$`, func(name, desc, body string) error {
		return w.writeSkill(false, name, desc, body)
	})
	sc.Step(`^a global Skill "([^"]*)" described as "([^"]*)" with the instructions "([^"]*)"$`, func(name, desc, body string) error {
		return w.writeSkill(true, name, desc, body)
	})
	sc.Step(`^a SKILL.md of "([^"]*)" without a description$`, func(name string) error {
		return w.writeSkill(false, name, "", "no front matter here")
	})
	sc.Step(`^the Skill "([^"]*)" has a file "([^"]*)" with the text "([^"]*)"$`, func(name, file, text string) error {
		return os.WriteFile(filepath.Join(w.skillsDir(false, name), file), []byte(text), 0o644)
	})
	sc.Step(`^the Skill "([^"]*)" is disabled in the config$`, func(name string) error {
		if w.skl == nil {
			w.skl = &skillsWorld{}
		}
		w.skl.disabled = append(w.skl.disabled, name)
		return nil
	})
	sc.Step(`^a Provider that applies the Skill "([^"]*)" and then answers "([^"]*)"$`, func(name, final string) error {
		w.real = true
		w.calls(final, [2]string{"skill", jsonArgs("name", name)})
		return nil
	})
	sc.Step(`^a Provider that asks the Skill "([^"]*)" for the file "([^"]*)" and then answers "([^"]*)"$`, func(name, file, final string) error {
		w.real = true
		w.calls(final, [2]string{"skill", jsonArgs("name", name, "file", file)})
		return nil
	})
	sc.Step(`^a Provider that answers "([^"]*)"$`, func(text string) error {
		w.reply(llmtest.Reply{Chunks: []string{text}})
		return nil
	})
	sc.Step(`^the user switches the Skill "([^"]*)" off$`, func(name string) error {
		w.act(func() error { // like main's skillsView: the Set and the prompt block together
			if err := w.skl.set.SetEnabled(name, false); err != nil {
				return err
			}
			return w.set.SetText("skills", w.skl.set.Block())
		})
		return nil
	})
	sc.Step(`^the user applies the Skill "([^"]*)" with the text "([^"]*)"$`, func(name, text string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.lastUser = text
		if err := w.ag.SubmitSkill(name, text); err != nil {
			return err
		}
		w.ag.Wait()
		return nil
	})
	sc.Step(`^a Skill warning mentioning "([^"]*)" is given$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		if w.skl == nil {
			return errors.New("no Skills in this scenario")
		}
		for _, s := range w.skl.warnings {
			if strings.Contains(s, text) {
				return nil
			}
		}
		return fmt.Errorf("skill warnings = %q, want one mentioning %q", w.skl.warnings, text)
	})
}
