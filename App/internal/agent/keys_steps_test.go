package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/config"
)

// ---------------------------------------------------------------------------
// Steps for keys.feature: the real config.KeyStore over a temp dir (never the user's config dir).
// ---------------------------------------------------------------------------

type keysWorld struct {
	dir      string
	store    config.KeyStore
	cfg      config.Config
	value    string
	src      string
	expanded string
	users    map[string][]string
	initErr  error
	before   string // the Keys file as it was before an Init
}

func (w *world) keysWorld() *keysWorld {
	if w.kw == nil {
		dir := w.tb.TempDir()
		w.kw = &keysWorld{dir: dir, cfg: config.Defaults(),
			store: config.KeyStore{File: filepath.Join(dir, "keys.env"), OldDir: filepath.Join(dir, "keys")}}
	}
	return w.kw
}

// writeMode writes path with exactly mode, whatever the umask.
func writeMode(path, body string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func registerKeysSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the Keys file holds "([^"]*)"$`, func(body string) error {
		return writeMode(w.keysWorld().store.File, body+"\n", 0o600)
	})
	sc.Step(`^the Keys file is changed to "([^"]*)"$`, func(body string) error {
		return writeMode(w.keysWorld().store.File, body+"\n", 0o600)
	})
	sc.Step(`^the Keys file readable by others holds "([^"]*)"$`, func(body string) error {
		if runtime.GOOS == "windows" {
			return godog.ErrSkip // no permission bits to check
		}
		return writeMode(w.keysWorld().store.File, body+"\n", 0o644)
	})
	sc.Step(`^the old Key directory holds "([^"]*)" as "([^"]*)"$`, func(name, v string) error {
		return writeMode(filepath.Join(w.keysWorld().store.OldDir, name), v+"\n", 0o600)
	})
	sc.Step(`^the environment variable "([^"]*)" is "([^"]*)"$`, func(name, v string) error {
		old, had := os.LookupEnv(name)
		if err := os.Setenv(name, v); err != nil {
			return err
		}
		w.tb.Cleanup(func() {
			if had {
				os.Setenv(name, old)
			} else {
				os.Unsetenv(name)
			}
		})
		return nil
	})
	sc.Step(`^a config file exists$`, func() error {
		return os.WriteFile(filepath.Join(w.keysWorld().dir, "config.json"), []byte(`{"tools":{"max_steps":7}}`), 0o600)
	})
	sc.Step(`^the key of the name "([^"]*)" is read$`, func(name string) error {
		k := w.keysWorld()
		k.value, k.src = k.store.Lookup(name)
		return nil
	})
	sc.Step(`^the Keys file is checked$`, func() error { return nil }) // warnings are read in the Then
	sc.Step(`^the Keys file is initialised$`, func() error {
		k := w.keysWorld()
		if b, err := os.ReadFile(k.store.File); err == nil {
			k.before = string(b)
		}
		var names []string
		for n := range config.KeyUsers(k.cfg) {
			names = append(names, n)
		}
		k.initErr = k.store.Init(names)
		return nil
	})
	sc.Step(`^the value "([^"]*)" is expanded$`, func(s string) error {
		k := w.keysWorld()
		k.expanded = k.store.Expand(s)
		return nil
	})
	sc.Step(`^the config has the provider "([^"]*)" needing the key variable "([^"]*)"$`, func(n, env string) error {
		w.keysWorld().cfg.Providers[n] = config.Provider{Type: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m", ContextLimit: 1, MaxTokens: 1, APIKeyEnv: env}
		return nil
	})
	sc.Step(`^the config has the MCP Server "([^"]*)" with the header "([^"]*)" set to "([^"]*)"$`, func(n, h, v string) error {
		k := w.keysWorld()
		k.cfg.MCP.Servers = map[string]config.MCPServer{n: {URL: "https://example.com/mcp", Headers: map[string]string{h: v}}}
		return nil
	})
	sc.Step(`^the users of the Keys are listed$`, func() error {
		k := w.keysWorld()
		k.users = config.KeyUsers(k.cfg)
		return nil
	})
	sc.Step(`^the key is "([^"]*)" from the "([^"]*)"$`, func(v, src string) error {
		if k := w.keysWorld(); k.value != v || k.src != src {
			return fmt.Errorf("key %q from %q", k.value, k.src)
		}
		return nil
	})
	sc.Step(`^the key of the name "([^"]*)" is now "([^"]*)"$`, func(name, v string) error {
		if got, _ := w.keysWorld().store.Lookup(name); got != v {
			return fmt.Errorf("key %q", got)
		}
		return nil
	})
	sc.Step(`^the warning names "([^"]*)" and shows no part of the line$`, func(part string) error {
		got := strings.Join(w.keysWorld().store.Warnings(), "\n")
		if !strings.Contains(got, part) || strings.Contains(got, "secret") || strings.Contains(got, "lower") {
			return fmt.Errorf("warnings %q", got)
		}
		return nil
	})
	sc.Step(`^no key is found for the name "([^"]*)" and the warning tells to chmod 600 the file$`, func(name string) error {
		k := w.keysWorld()
		if v, src := k.store.Lookup(name); v != "" || src != "" {
			return fmt.Errorf("found a key from %q", src)
		}
		got := strings.Join(k.store.Warnings(), "\n")
		if !strings.Contains(got, k.store.File) || !strings.Contains(got, "chmod 600") {
			return fmt.Errorf("warnings %q", got)
		}
		return nil
	})
	sc.Step(`^the Keys file is private to the user and lists "([^"]*)" as a comment$`, func(name string) error {
		k := w.keysWorld()
		if k.initErr != nil {
			return k.initErr
		}
		fi, err := os.Stat(k.store.File)
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			return fmt.Errorf("file mode %v", fi.Mode().Perm())
		}
		b, _ := os.ReadFile(k.store.File)
		if !strings.Contains(string(b), "\n# "+name+"=\n") {
			return fmt.Errorf("template lacks %s", name)
		}
		return nil
	})
	sc.Step(`^the initialisation is refused and the Keys file is unchanged$`, func() error {
		k := w.keysWorld()
		b, _ := os.ReadFile(k.store.File)
		if !errors.Is(k.initErr, config.ErrKeysFileExists) || string(b) != k.before {
			return fmt.Errorf("err %v, file changed %v", k.initErr, string(b) != k.before)
		}
		return nil
	})
	sc.Step(`^no file of the config dir holds "([^"]*)"$`, func(v string) error {
		k := w.keysWorld()
		return filepath.Walk(k.dir, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}
			if b, _ := os.ReadFile(path); strings.Contains(string(b), v) {
				return fmt.Errorf("%s holds the key", path)
			}
			return nil
		})
	})
	sc.Step(`^the expanded value is "([^"]*)"$`, func(v string) error {
		if got := w.keysWorld().expanded; got != v {
			return fmt.Errorf("expanded %q", got)
		}
		return nil
	})
	sc.Step(`^the Key "([^"]*)" is used by "([^"]*)"$`, func(name, users string) error {
		if got := strings.Join(w.keysWorld().users[name], ", "); got != users {
			return fmt.Errorf("users %q", got)
		}
		return nil
	})
}
