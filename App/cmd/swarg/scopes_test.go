package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/config"
)

func TestScopes_DefaultProjectWhenDotSwargExists(t *testing.T) {
	cwd := t.TempDir()
	s := newScopes(t.TempDir(), cwd)
	if s.Default() != "global" || strings.Join(s.Scopes(), ",") != "project,global" {
		t.Fatalf("%q %v", s.Default(), s.Scopes())
	}
	os.MkdirAll(filepath.Join(cwd, ".swarg"), 0o755)
	if s.Default() != "project" {
		t.Fatal("project expected with .swarg present")
	}
	if err := s.set("", "x", 1); err != nil || s.set("elsewhere", "x", 1) == nil {
		t.Fatal("scope handling")
	}
}

func TestConfigView_SettingsAndSet(t *testing.T) {
	dir, cwd := t.TempDir(), t.TempDir()
	sc := newScopes(dir, cwd)
	cfg, layers, err := config.LoadLayered(sc.global, false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	v := &configView{sc: sc, cwd: cwd, cfg: &cfg, layers: &layers, override: func(c *config.Config) { c.Override("cli-model", "") }}
	v.override(&cfg)
	rows := v.Settings()
	if rows[0].Key != "providers.local.model" || rows[0].Live != "model" || rows[3].Value != 50 || rows[3].Scope != "global" {
		t.Fatalf("rows %+v", rows[:4])
	}
	if err := v.Set("project", "tools.max_steps", 12); err != nil {
		t.Fatal(err)
	}
	if rows = v.Settings(); rows[3].Value != 12 || rows[3].Scope != "project" || rows[0].Value != "cli-model" {
		t.Fatalf("after set %+v", rows[3])
	}
	if err := v.Set("project", "tools.auto_approve", true); err == nil || !strings.Contains(err.Error(), "not allowed in a project file") {
		t.Fatalf("guard %v", err)
	}
	if err := v.Set("global", "tools.auto_approve", true); err != nil || !v.Settings()[2].Value.(bool) {
		t.Fatalf("global set %v %+v", err, v.Settings()[2])
	}
	if _, err := os.Stat(filepath.Join(cwd, ".swarg", "config.json")); err != nil {
		t.Fatal("project file not written")
	}
}
