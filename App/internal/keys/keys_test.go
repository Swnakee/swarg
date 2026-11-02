package keys

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testStore is a store in a temp dir (never the user's config dir).
func testStore(t *testing.T) Store {
	t.Helper()
	d := t.TempDir()
	return Store{File: filepath.Join(d, "keys.env"), OldDir: filepath.Join(d, "keys")}
}

// writeFile writes body with exactly mode (the umask may have cut the bits).
func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestKeysFile_ParsesLinesCommentsQuotesAndBlanks(t *testing.T) {
	ks := testStore(t)
	t.Setenv("T12_A", "")
	t.Setenv("T12_B", "")
	t.Setenv("T12_C", "")
	t.Setenv("T12_D", "")
	writeFile(t, ks.File, "\ufeff# comment\n\n  T12_A = test-key-value-1  \r\nT12_B=\"test key 2\"\nT12_C='test-key-3'\nT12_D=\n# T12_E=test-key-5\nT12_F=a=b\n", 0o600)
	for name, want := range map[string]string{"T12_A": "test-key-value-1", "T12_B": "test key 2", "T12_C": "test-key-3", "T12_D": "", "T12_E": "", "T12_F": "a=b"} {
		if got, _ := ks.Lookup(name); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", name, got, want)
		}
	}
	if w := ks.Warnings(); len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
}

func TestKeysFile_BadLinesWarnWithTheNumberNeverTheContent(t *testing.T) {
	ks := testStore(t)
	t.Setenv("T12_OK", "")
	writeFile(t, ks.File, "test-secret-without-name\nT12_OK=test-key-value-1\nlower=test-secret-two\n1BAD=x\nT12_CTL=a\x01b\n", 0o600)
	if got, _ := ks.Lookup("T12_OK"); got != "test-key-value-1" {
		t.Errorf("a good line next to bad ones: %q", got)
	}
	w := strings.Join(ks.Warnings(), "\n")
	for _, want := range []string{"line 1", "line 3", "line 4", "line 5"} {
		if !strings.Contains(w, want) {
			t.Errorf("warnings lack %q: %s", want, w)
		}
	}
	if strings.Contains(w, "secret") || strings.Contains(w, "test-key") || strings.Contains(w, "line 2") {
		t.Errorf("warnings leak content or flag a good line: %s", w)
	}
}

func TestKeysFile_NotPrivateIsIgnoredWithAWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no mode bits")
	}
	ks := testStore(t)
	t.Setenv("T12_G", "")
	writeFile(t, ks.File, "T12_G=test-key-value-1\n", 0o644)
	if v, _ := ks.Lookup("T12_G"); v != "" {
		t.Errorf("a group/other readable file must be ignored, got %q", v)
	}
	w := strings.Join(ks.Warnings(), "\n")
	if !strings.Contains(w, ks.File) || !strings.Contains(w, "chmod 600") || strings.Contains(w, "test-key") {
		t.Errorf("warning %q", w)
	}
	if err := os.Chmod(ks.File, 0o600); err != nil {
		t.Fatal(err)
	}
	if v, _ := ks.Lookup("T12_G"); v != "test-key-value-1" {
		t.Errorf("after chmod 600 the next lookup must see it, got %q", v)
	}
}

func TestKeysFile_MissingIsSilent(t *testing.T) {
	ks := testStore(t)
	if w := ks.Warnings(); len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	if v, src := ks.Lookup("T12_NOPE"); v != "" || src != "" {
		t.Errorf("Lookup = %q %q", v, src)
	}
}

func TestStore_LookupOrderEnvFileOldDir(t *testing.T) {
	ks := testStore(t)
	t.Setenv("T12_H", "")
	writeFile(t, filepath.Join(ks.OldDir, "T12_H"), "test-old-value\nsecond\n", 0o600)
	if v, src := ks.Lookup("T12_H"); v != "test-old-value" || src != SrcOld {
		t.Errorf("old dir: %q %q", v, src)
	}
	writeFile(t, ks.File, "T12_H=test-file-value\n", 0o600)
	if v, src := ks.Lookup("T12_H"); v != "test-file-value" || src != SrcFile {
		t.Errorf("file beats the old dir: %q %q", v, src)
	}
	t.Setenv("T12_H", "test-env-value")
	v, src := ks.Lookup("T12_H")
	st := ks.State("T12_H")
	if v != "test-env-value" || src != SrcEnv || !st.Env || !st.File || !st.Old || st.Value != v {
		t.Errorf("env wins: %q %q %+v", v, src, st)
	}
}

func TestStore_OldDirLooseFileIsIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no mode bits")
	}
	ks := testStore(t)
	t.Setenv("T12_I", "")
	writeFile(t, filepath.Join(ks.OldDir, "T12_I"), "test-old-value\n", 0o644)
	if st := ks.State("T12_I"); st.Old || st.Value != "" {
		t.Errorf("state %+v", st)
	}
}

func TestStore_NamesAndStates(t *testing.T) {
	ks := testStore(t)
	t.Setenv("T12_J", "")
	writeFile(t, ks.File, "B_KEY=test-key-value-1\nA_KEY=test-key-value-2\n", 0o600)
	writeFile(t, filepath.Join(ks.OldDir, "C_KEY"), "test-key-value-3\n", 0o600)
	for _, junk := range []string{"notes.txt", "lower", ".tmp-1"} {
		writeFile(t, filepath.Join(ks.OldDir, junk), "x", 0o600)
	}
	if got := strings.Join(ks.Names(), ","); got != "A_KEY,B_KEY,C_KEY" {
		t.Errorf("Names = %q", got)
	}
	st := ks.States([]string{"A_KEY", "C_KEY", "T12_J"})
	if !st[0].File || st[0].Old || !st[1].Old || st[1].File || st[2].Value != "" {
		t.Errorf("States %+v", st)
	}
	if (Store{}).Names() != nil && len((Store{}).Names()) != 0 {
		t.Error("an empty store has no names")
	}
}

func TestStore_OldHintOnlyWhenOldKeysExist(t *testing.T) {
	ks := testStore(t)
	if ks.OldHint() != "" {
		t.Error("no old dir, no hint")
	}
	writeFile(t, filepath.Join(ks.OldDir, "T12_K"), "test-key-value-1\n", 0o600)
	h := ks.OldHint()
	if !strings.Contains(h, ks.File) || !strings.Contains(h, ks.OldDir) || strings.Contains(h, "test-key") || strings.Contains(h, "\n") {
		t.Errorf("hint %q", h)
	}
	if _, err := os.Stat(filepath.Join(ks.OldDir, "T12_K")); err != nil {
		t.Error("the old file must stay")
	}
}

func TestStore_ExpandEnvThenFileThenOldDir(t *testing.T) {
	ks := testStore(t)
	t.Setenv("T12_TOK_A", "")
	t.Setenv("T12_TOK_B", "from-env")
	t.Setenv("T12_TOK_C", "")
	writeFile(t, ks.File, "T12_TOK_A=from-file\nT12_TOK_B=from-file\n", 0o600)
	writeFile(t, filepath.Join(ks.OldDir, "T12_TOK_C"), "from-old\n", 0o600)
	if got := ks.Expand("Bearer ${T12_TOK_A} $T12_TOK_B $T12_TOK_C ${T12_NONE}."); got != "Bearer from-file from-env from-old ." {
		t.Errorf("Expand = %q", got)
	}
}

func TestStore_InitWritesPrivateTemplateAndNeverOverwrites(t *testing.T) {
	ks := testStore(t)
	if err := ks.Init([]string{"OPENROUTER_API_KEY", "bad name", "A_KEY"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ks.File)
	body := string(b)
	if !strings.Contains(body, "# A_KEY=\n# OPENROUTER_API_KEY=\n") || strings.Contains(body, "bad name") {
		t.Errorf("template:\n%s", body)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(ks.File); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", fi.Mode().Perm())
		}
	}
	if w := ks.Warnings(); len(w) != 0 {
		t.Errorf("the template must parse clean: %v", w)
	}
	writeFile(t, ks.File, "T12_L=test-key-value-1\n", 0o600)
	if err := ks.Init(nil); err != ErrKeysFileExists {
		t.Errorf("Init over a file = %v", err)
	}
	if b, _ := os.ReadFile(ks.File); string(b) != "T12_L=test-key-value-1\n" {
		t.Errorf("the file was changed: %q", b)
	}
}

func TestStore_InitCreatesTheConfigDir(t *testing.T) {
	ks := Store{File: filepath.Join(t.TempDir(), "swarg", "keys.env")}
	if err := ks.Init(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ks.File); err != nil {
		t.Error(err)
	}
}

func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":                   "set",
		"sk-test":            "set",
		"123456789012345":    "set", // 15
		"1234567890123456":   "123456…3456",
		"sk-or-v1-abcd-a1b2": "sk-or-…a1b2",
	}
	for in, want := range cases {
		if got := MaskKey(in); got != want {
			t.Errorf("MaskKey(%q) = %q, want %q", in, got, want)
		}
	}
}
