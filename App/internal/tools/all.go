package tools

// All returns every built-in tool. Add or remove a tool here.
func All() []Tool {
	return []Tool{ReadFile{}, ListDir{}, Grep{}, EditFile{}, WriteFile{}, Shell{}, ShellOutput{}, ShellKill{},
		WebSearch{}, WebFetch{}}
}

// FreeWeb returns list with the web tools set to run without approval (config web.confirm = false).
func FreeWeb(list []Tool) []Tool {
	out := make([]Tool, len(list))
	for i, t := range list {
		switch t.(type) {
		case WebSearch:
			t = WebSearch{Free: true}
		case WebFetch:
			t = WebFetch{Free: true}
		}
		out[i] = t
	}
	return out
}
