package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// keysView and the live use of a key written in the keys file (R-106, R-107). The config dir is a temp
// dir (isolateConfigDir); keys are placeholders.
// ---------------------------------------------------------------------------

const keySecret = "sk-test-placeholder-value-WXYZ"

// writeKeysFile writes the user's keys file (private) for one test; isolateConfigDir came first.
func writeKeysFile(t *testing.T, body string) {
	t.Helper()
	f := config.UserKeys().File
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestKeysView_RowsStatesAndReread(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("T12_ENV_ONLY", "sk-test-envvalue-0123456789")
	t.Setenv("T12_MCP", "")
	t.Setenv("T12_STORED_ONLY", "")
	cfg := config.Defaults()
	cfg.Providers["x"] = config.Provider{Type: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m", ContextLimit: 1, MaxTokens: 1, APIKeyEnv: "T12_ENV_ONLY"}
	cfg.MCP.Servers = map[string]config.MCPServer{"web": {URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${T12_MCP}"}}}
	v := keysView{cfg: &cfg}
	rows := func() map[string]ui.KeyRow {
		m := map[string]ui.KeyRow{}
		for _, r := range v.Keys() {
			m[r.Name] = r
		}
		return m
	}
	r := rows()
	if got := r["OPENROUTER_API_KEY"]; got.State != "missing" || strings.Join(got.Users, ",") != "provider openrouter" {
		t.Errorf("openrouter: %+v", got)
	}
	if got := r["T12_ENV_ONLY"]; got.State != "set (env)" || got.Masked != "sk-tes…6789" {
		t.Errorf("env only: %+v", got)
	}
	if got := r["T12_MCP"]; got.State != "missing" || strings.Join(got.Users, ",") != "mcp web header Authorization" {
		t.Errorf("mcp: %+v", got)
	}

	// the user edits the file; the next call sees it (no restart)
	writeKeysFile(t, "T12_MCP="+keySecret+"\nT12_STORED_ONLY=short\nT12_ENV_ONLY=sk-test-filevalue-9876543210\n")
	oldDir := config.UserKeys().OldDir
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "T12_OLD_ONLY"), []byte("sk-test-oldvalue-0123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T12_OLD_ONLY", "")
	r = rows()
	if got := r["T12_MCP"]; got.State != "set (keys.env)" || got.Masked != "sk-tes…WXYZ" {
		t.Errorf("after edit: %+v", got)
	}
	if got := r["T12_STORED_ONLY"]; got.State != "set (keys.env)" || got.Masked != "set" || len(got.Users) != 0 {
		t.Errorf("file only: %+v", got)
	}
	if got := r["T12_ENV_ONLY"]; got.State != "set (env, wins)" || got.Masked != "sk-tes…6789" {
		t.Errorf("env wins: %+v", got)
	}
	if got := r["T12_OLD_ONLY"]; got.State != "set (old keys dir)" || got.Masked != "sk-tes…6789" {
		t.Errorf("old dir: %+v", got)
	}
	writeKeysFile(t, "")
	if got := rows()["T12_MCP"]; got.State != "missing" {
		t.Errorf("after the line is removed: %+v", got)
	}
}

func TestKeysView_InitTemplateListsConfigNamesAndNeverOverwrites(t *testing.T) {
	isolateConfigDir(t)
	cfg := config.Defaults()
	cfg.MCP.Servers = map[string]config.MCPServer{"web": {URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${T12_MCP}"}}}
	v := keysView{cfg: &cfg}
	if v.File() == "" || !strings.HasSuffix(v.File(), "keys.env") {
		t.Fatalf("File = %q", v.File())
	}
	path, err := v.Init()
	if err != nil || path != v.File() {
		t.Fatalf("Init = %q, %v", path, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# OPENROUTER_API_KEY=\n") || !strings.Contains(string(b), "# T12_MCP=\n") {
		t.Errorf("template:\n%s", b)
	}
	if _, err := v.Init(); err == nil {
		t.Error("a second Init must refuse")
	}
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Error("the file was changed")
	}
}

func TestKeysNotices_WarningsAndTheOldDirHintNeverAKey(t *testing.T) {
	isolateConfigDir(t)
	var b strings.Builder
	keysNotices(&b)
	if b.Len() != 0 {
		t.Fatalf("nothing to say, got %q", b.String())
	}
	writeKeysFile(t, "not a pair "+keySecret+"\n")
	oldDir := config.UserKeys().OldDir
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "T12_NOTICE"), []byte(keySecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keysNotices(&b)
	got := b.String()
	if !strings.Contains(got, "line 1") || !strings.Contains(got, "still read") || strings.Contains(got, "sk-test") {
		t.Errorf("notices %q", got)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "T12_NOTICE")); err != nil {
		t.Error("the old key file must stay")
	}
}

// A key written in the keys file turns the /server row from "no key" to "ok" with no restart (K3).
func TestSources_KeyInFileLiveMakesServerOK(t *testing.T) {
	isolateConfigDir(t)
	srv := orServer(t)
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T12_LIVE_KEY"
	cfg.Providers["openrouter"] = p
	t.Setenv("T12_LIVE_KEY", "")
	s, _ := testSources(t, &cfg)
	state := func() string {
		rows, _ := s.Servers(t.Context())
		for _, r := range rows {
			if r.Name == "openrouter" {
				return r.State
			}
		}
		return "?"
	}
	if got := state(); !strings.HasPrefix(got, "no key: set T12_LIVE_KEY") {
		t.Fatalf("before: %q", got)
	}
	if _, err := s.Switch("openrouter"); err == nil {
		t.Fatal("a switch without a key must be refused")
	}
	writeKeysFile(t, "T12_LIVE_KEY="+keySecret+"\n")
	if got := state(); got != "ok" {
		t.Fatalf("after the edit: %q", got)
	}
	if _, err := s.Switch("openrouter"); err != nil {
		t.Fatalf("switch after the edit: %v", err)
	}
}

// The client of a provider reads the key on every request, so a rotated key is used by the very
// client the running Source already holds (K3).
func TestSources_ClientReadsRotatedKey(t *testing.T) {
	isolateConfigDir(t)
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"a/alpha"}]}`))
	}))
	defer srv.Close()
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T12_ROT_KEY"
	cfg.Providers["openrouter"] = p
	t.Setenv("T12_ROT_KEY", "")
	s, _ := testSources(t, &cfg)
	c := s.client("openrouter") // made before any key exists
	for _, k := range []string{"sk-test-first-0123456789", "sk-test-second-0123456789"} {
		writeKeysFile(t, "T12_ROT_KEY="+k+"\n")
		if _, err := c.Catalog(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auth) != 2 || auth[0] != "Bearer sk-test-first-0123456789" || auth[1] != "Bearer sk-test-second-0123456789" {
		t.Fatalf("Authorization headers: %q", auth)
	}
}

// I-37: the value is only in the keys file: not in another file of the config dir (config.json,
// approvals, recent models), not in a state text, a refusal or an error of the client.
func TestKeys_ValueOnlyInTheKeyFile(t *testing.T) {
	isolateConfigDir(t)
	srv := orServer(t)
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T12_SEC_KEY"
	cfg.Providers["openrouter"] = p
	t.Setenv("T12_SEC_KEY", "")
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "config.json")
	if err := config.Set(global, "tools.max_steps", 7, false); err != nil {
		t.Fatal(err)
	}
	writeKeysFile(t, "T12_SEC_KEY="+keySecret+"\n")
	s, _ := testSources(t, &cfg)
	var texts []string
	rows, _ := s.Servers(t.Context())
	for _, r := range rows {
		texts = append(texts, r.State, r.Address)
	}
	// the fake has no scripted reply, so it answers 500: a failing request whose text must be clean
	_, serr := s.client("openrouter").Stream(t.Context(), chat.Request{Model: "m", Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}}})
	if serr != nil {
		texts = append(texts, serr.Error())
	}
	for _, tx := range texts {
		if strings.Contains(tx, keySecret) {
			t.Errorf("the key is in a text: %q", tx)
		}
	}
	keyFile := config.UserKeys().File
	_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || path == keyFile {
			return nil
		}
		if b, _ := os.ReadFile(path); strings.Contains(string(b), keySecret) {
			t.Errorf("the key is in %s", path)
		}
		return nil
	})
	if b, _ := os.ReadFile(keyFile); !strings.Contains(string(b), keySecret) {
		t.Error("the keys file lost the key")
	}
}
