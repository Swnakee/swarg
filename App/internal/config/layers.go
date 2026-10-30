package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Layers: global <- project <- local (R-95), the project safety guard (R-96, I-33)
// ---------------------------------------------------------------------------

// Project config files, relative to the project directory.
const (
	ProjectDir    = ".swarg"
	ProjectConfig = ".swarg/config.json"
	ProjectLocal  = ".swarg/config.local.json" // personal overrides; the user git-ignores it
)

// Layers tells what the project files contributed.
type Layers struct {
	Project        bool            // a project file was read
	ProjectServers map[string]bool // MCP servers defined (or redefined) by a project file
	ProjectRaw     map[string]any  // the merged JSON of the project files (what FromProject answers)
	Warnings       []string        // values a project file may not set (R-96)
}

// FromProject reports whether the dotted key is set by a project file.
func (l Layers) FromProject(key string) bool {
	_, ok := lookup(l.ProjectRaw, strings.Split(key, ".")...)
	return ok
}

// unsafeKeys are the settings a project file may not move in the unsafe direction.
var unsafeKeys = []struct {
	key string
	bad any
}{
	{"tools.auto_approve", true},
	{"web.confirm", false},
	{"mcp.confirm", false},
}

var trustKey = regexp.MustCompile(`^mcp\.servers\.([^.]+)\.trust$`)

// LoadLayered loads the global file like Load, then the project's `.swarg/config.json` and
// `.swarg/config.local.json` over it: the files are merged as JSON (objects key by key, so a
// project entry of `mcp.servers.x` or `providers.p` changes only the keys it names; lists and
// scalars replace), then decoded once over Defaults. Each project file is checked alone first,
// so an unknown field names its file. A project file cannot lower safety (unsafeKeys, trust):
// such values are reset to the global ones and reported in Layers.Warnings.
func LoadLayered(globalPath string, explicit bool, projectDir string) (Config, Layers, error) {
	global, err := Load(globalPath, explicit)
	l := Layers{ProjectServers: map[string]bool{}, ProjectRaw: map[string]any{}}
	if err != nil {
		return global, l, err
	}
	merged := map[string]any{}
	if data, err := os.ReadFile(globalPath); err == nil {
		json.Unmarshal(data, &merged)
	}
	for _, rel := range []string{ProjectConfig, ProjectLocal} {
		path := filepath.Join(projectDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return global, l, fmt.Errorf("config: read %s: %w", rel, err)
		}
		probe := Defaults()
		if err := decodeOver(&probe, data); err != nil {
			return global, l, fmt.Errorf("config: parse %s: %w", rel, err)
		}
		var raw map[string]any
		json.Unmarshal(data, &raw)
		l.Project = true
		for n := range nested(raw, "mcp", "servers") {
			l.ProjectServers[n] = true
		}
		l.ProjectRaw = deepMerge(l.ProjectRaw, raw)
		merged = deepMerge(merged, raw)
		l.Warnings = append(l.Warnings, guardWarnings(raw, global, rel)...)
	}
	if !l.Project {
		return global, l, nil
	}
	cfg := Defaults()
	data, _ := json.Marshal(merged)
	if err := decodeOver(&cfg, data); err != nil {
		return global, l, fmt.Errorf("config: %w", err)
	}
	guard(&cfg, global, l.ProjectRaw)
	if err := cfg.Validate(); err != nil {
		return global, l, err
	}
	return cfg, l, nil
}

// deepMerge lays over onto base: objects merge key by key, anything else replaces.
func deepMerge(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bo, ok := out[k].(map[string]any); ok {
			if vo, ok := v.(map[string]any); ok {
				out[k] = deepMerge(bo, vo)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// decodeOver decodes one file over cfg: unknown fields and trailing data are errors.
func decodeOver(cfg *Config, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

// guardWarnings names the unsafe values a project file sets (R-96).
func guardWarnings(raw map[string]any, global Config, file string) (warnings []string) {
	warn := func(key string) {
		warnings = append(warnings, fmt.Sprintf("config: %s: %s is not allowed in a project file; the global value is kept", file, key))
	}
	for _, u := range unsafeKeys {
		if v, ok := lookup(raw, strings.Split(u.key, ".")...); ok && v == u.bad {
			warn(u.key)
		}
	}
	for _, n := range sortedKeys(nested(raw, "mcp", "servers")) {
		if v, ok := lookup(raw, "mcp", "servers", n, "trust"); ok && v == true && !global.MCP.Servers[n].Trust {
			warn("mcp.servers." + n + ".trust")
		}
	}
	return warnings
}

// guard resets the unsafe values the project files set to the global ones (R-96, I-33).
func guard(cfg *Config, global Config, raw map[string]any) {
	for _, u := range unsafeKeys {
		if v, ok := lookup(raw, strings.Split(u.key, ".")...); ok && v == u.bad {
			switch u.key {
			case "tools.auto_approve":
				cfg.Tools.AutoApprove = global.Tools.AutoApprove
			case "web.confirm":
				cfg.Web.Confirm = global.Web.Confirm
			case "mcp.confirm":
				cfg.MCP.Confirm = global.MCP.Confirm
			}
		}
	}
	for n := range nested(raw, "mcp", "servers") {
		if v, ok := lookup(raw, "mcp", "servers", n, "trust"); ok && v == true {
			s := cfg.MCP.Servers[n]
			s.Trust = global.MCP.Servers[n].Trust
			cfg.MCP.Servers[n] = s
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// lookup walks a generic JSON object along keys.
func lookup(m map[string]any, keys ...string) (any, bool) {
	var v any = m
	for _, k := range keys {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = obj[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

// nested is the object at keys, nil when absent or not an object.
func nested(m map[string]any, keys ...string) map[string]any {
	v, _ := lookup(m, keys...)
	obj, _ := v.(map[string]any)
	return obj
}

// ---------------------------------------------------------------------------
// Set: one key into one file (R-98)
// ---------------------------------------------------------------------------

// Set writes value under the dotted key into the JSON file at path (created when missing; the
// other keys are kept; indented). project refuses the values a project file may not hold (R-96).
func Set(path, key string, value any, project bool) error {
	if project && unsafeValue(key, value) {
		return fmt.Errorf("config: %s = %v is not allowed in a project file; use the global config", key, value)
	}
	root := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("config: read %s: %w", path, err)
	case len(bytes.TrimSpace(data)) > 0:
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	keys := strings.Split(key, ".")
	obj := root
	for _, k := range keys[:len(keys)-1] {
		next, ok := obj[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			obj[k] = next
		}
		obj = next
	}
	obj[keys[len(keys)-1]] = value
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// unsafeValue reports whether key = value lowers safety (what a project file may not do).
func unsafeValue(key string, value any) bool {
	for _, u := range unsafeKeys {
		if u.key == key && value == u.bad {
			return true
		}
	}
	return trustKey.MatchString(key) && value == true
}
