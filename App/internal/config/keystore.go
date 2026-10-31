package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Keys file (R-105, R-106, R-110): named secrets in one private file, <config dir>/keys.env
// ---------------------------------------------------------------------------

// Where a key comes from (Lookup's src).
const (
	SrcEnv  = "env"
	SrcFile = "keys.env"
	SrcOld  = "old keys dir"
)

// keyNameRe is the shape of a key name (an environment variable name).
var keyNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// ErrKeysFileExists says Init found a keys file and left it alone.
var ErrKeysFileExists = errors.New("the keys file already exists; edit it")

// varNameRe is what counts as a variable reference in an MCP value (not "$1" or "$*").
var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidKeyName reports whether name may be a key name.
func ValidKeyName(name string) bool { return keyNameRe.MatchString(name) }

// KeyStore is where keys are read from, in order: the environment, the keys file (lines
// NAME=value), the old keys directory (one file per name, kept as a fallback). It only reads;
// the user edits the file. It is global (the user's config dir), never a project folder.
type KeyStore struct{ File, OldDir string }

// UserKeys is the store of the user: <config dir>/keys.env and <config dir>/keys. Both are "" when
// the config dir is unknown.
func UserKeys() KeyStore {
	d, err := Dir()
	if err != nil {
		return KeyStore{}
	}
	return KeyStore{File: filepath.Join(d, "keys.env"), OldDir: filepath.Join(d, "keys")}
}

// isPrivate reports whether fi is a regular file only the user can read (Windows has no mode bits).
func isPrivate(fi os.FileInfo) bool {
	return fi.Mode().IsRegular() && (runtime.GOOS == "windows" || fi.Mode().Perm()&0o077 == 0)
}

// keysFile is one reading of the keys file: its values and the warnings about it.
type keysFile struct {
	vals  map[string]string
	warns []string
}

// read parses the keys file afresh (it is small and edits must apply at once). A missing file is
// empty and silent; one readable by group or others is ignored with a warning; a bad line is
// skipped with a warning that names the line number only, never the text.
func (k KeyStore) read() keysFile {
	kf := keysFile{vals: map[string]string{}}
	if k.File == "" {
		return kf
	}
	fi, err := os.Stat(k.File)
	switch {
	case err != nil:
		return kf
	case !isPrivate(fi):
		kf.warns = append(kf.warns, fmt.Sprintf("%s is not private and was ignored: chmod 600 %s", filepath.Base(k.File), k.File))
		return kf
	}
	f, err := os.Open(k.File)
	if err != nil {
		kf.warns = append(kf.warns, fmt.Sprintf("%s cannot be read: %v", filepath.Base(k.File), errors.Unwrap(err)))
		return kf
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if line == "" || line[0] == '#' {
			continue
		}
		name, val, ok := parseKeyLine(line)
		if !ok {
			kf.warns = append(kf.warns, fmt.Sprintf("%s line %d is not NAME=value and was skipped", filepath.Base(k.File), n))
			continue
		}
		if val != "" { // "NAME=" is a template line: no key
			kf.vals[name] = val
		}
	}
	if sc.Err() != nil {
		kf.warns = append(kf.warns, fmt.Sprintf("%s could not be read to the end", filepath.Base(k.File)))
	}
	return kf
}

// parseKeyLine splits NAME=value; one pair of surrounding quotes is stripped from the value.
func parseKeyLine(line string) (name, val string, ok bool) {
	name, val, found := strings.Cut(line, "=")
	name, val = strings.TrimSpace(name), strings.TrimSpace(val)
	if !found || !ValidKeyName(name) || strings.ContainsFunc(val, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return "", "", false
	}
	if n := len(val); n >= 2 && (val[0] == '"' || val[0] == '\'') && val[n-1] == val[0] {
		val = val[1 : n-1]
	}
	return name, val, true
}

// Warnings are the problems of the keys file right now (not private, bad lines); names and
// numbers only, never a key.
func (k KeyStore) Warnings() []string { return k.read().warns }

// oldPath is the file of name in the old keys directory; "" for a name that is not a plain key name.
func (k KeyStore) oldPath(name string) string {
	if k.OldDir == "" || !ValidKeyName(name) {
		return ""
	}
	return filepath.Join(k.OldDir, name)
}

// oldGet is the trimmed first line of the old key file of name, "" when it is missing, empty or
// readable by group or others.
func (k KeyStore) oldGet(name string) string {
	path := k.oldPath(name)
	if path == "" {
		return ""
	}
	if fi, err := os.Stat(path); err != nil || !isPrivate(fi) {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line)
}

// Lookup is the key as the program uses it and where it came from (SrcEnv, SrcFile, SrcOld);
// "" and "" when there is none. The environment wins, then the keys file, then the old directory.
func (k KeyStore) Lookup(name string) (value, src string) {
	if v := os.Getenv(name); v != "" {
		return v, SrcEnv
	}
	if v := k.read().vals[name]; v != "" {
		return v, SrcFile
	}
	if v := k.oldGet(name); v != "" {
		return v, SrcOld
	}
	return "", ""
}

// Expand replaces $VAR and ${VAR} in s by Lookup; an unknown name becomes "". It is os.ExpandEnv
// with the keys file as the second source (K4).
func (k KeyStore) Expand(s string) string {
	kf := k.read()
	return os.Expand(s, func(n string) string {
		if v := os.Getenv(n); v != "" {
			return v
		}
		if v := kf.vals[n]; v != "" {
			return v
		}
		return k.oldGet(n)
	})
}

// KeyState tells where a key exists: Env, File and Old say what holds it, Value is the effective
// one (env wins, then the keys file, then the old directory).
type KeyState struct {
	Env, File, Old bool
	Value          string
}

// State is the full picture of name for the keys menu; it reads the file once.
func (k KeyStore) State(name string) KeyState {
	return k.states(k.read())(name)
}

// states returns State over one reading, so a list of names reads the file once.
func (k KeyStore) states(kf keysFile) func(string) KeyState {
	return func(name string) KeyState {
		env, file, old := os.Getenv(name), kf.vals[name], k.oldGet(name)
		st := KeyState{Env: env != "", File: file != "", Old: old != ""}
		for _, v := range []string{env, file, old} {
			if v != "" {
				st.Value = v
				break
			}
		}
		return st
	}
}

// States is State for every name, with one reading of the file.
func (k KeyStore) States(names []string) []KeyState {
	st := k.states(k.read())
	out := make([]KeyState, len(names))
	for i, n := range names {
		out[i] = st(n)
	}
	return out
}

// Names lists the keys that exist in the keys file and the old directory (valid names), sorted.
func (k KeyStore) Names() []string {
	set := map[string]bool{}
	for n := range k.read().vals {
		set[n] = true
	}
	for _, n := range k.oldNames() {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// oldNames lists the valid key files of the old directory.
func (k KeyStore) oldNames() []string {
	ents, err := os.ReadDir(k.OldDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && ValidKeyName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// OldHint is the one-line start hint when key files of the old directory exist: they still work,
// but belong in the keys file. "" when there are none. Nothing is moved or deleted.
func (k KeyStore) OldHint() string {
	if len(k.oldNames()) == 0 {
		return ""
	}
	return fmt.Sprintf("keys in %s are still read; move them into %s (one NAME=value line each) and delete the old files", k.OldDir, k.File)
}

// Init writes a commented template of the keys file, 0600, listing names as "# NAME=". It never
// overwrites: an existing file gives ErrKeysFileExists.
func (k KeyStore) Init(names []string) error {
	if k.File == "" {
		return errors.New("the config dir is unknown")
	}
	if err := os.MkdirAll(filepath.Dir(k.File), 0o700); err != nil {
		return fmt.Errorf("keys file: %w", err)
	}
	f, err := os.OpenFile(k.File, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return ErrKeysFileExists
	}
	if err != nil {
		return fmt.Errorf("keys file: %w", err)
	}
	var b strings.Builder
	b.WriteString("# swarg API keys: one NAME=value per line; lines starting with # are comments.\n" +
		"# Keep this file private (chmod 600): swarg ignores it otherwise.\n" +
		"# An environment variable of the same name wins over this file.\n" +
		"# Edits apply at once, no restart needed.\n\n")
	names = append([]string(nil), names...)
	sort.Strings(names)
	for _, n := range names {
		if ValidKeyName(n) {
			b.WriteString("# " + n + "=\n")
		}
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(k.File)
		return fmt.Errorf("keys file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mask and users
// ---------------------------------------------------------------------------

// MaskKey shows enough of a key to recognise it: the first 6 and last 4 characters around an
// ellipsis when it is at least 16 long, else just "set".
func MaskKey(v string) string {
	r := []rune(v)
	if len(r) < 16 {
		return "set"
	}
	return string(r[:6]) + "…" + string(r[len(r)-4:])
}

// KeyUsers maps every key name the config refers to onto who uses it: "provider <name>" for an
// api_key_env, "mcp <server> header <Name>" / "mcp <server> env <NAME>" for a $VAR in those values.
// Users are sorted; a name used twice lists both.
func KeyUsers(cfg Config) map[string][]string {
	out := map[string][]string{}
	add := func(name, who string) {
		if varNameRe.MatchString(name) {
			out[name] = append(out[name], who)
		}
	}
	for n, p := range cfg.Providers {
		add(p.APIKeyEnv, "provider "+n)
	}
	refs := func(server, kind string, m map[string]string) {
		for k, v := range m {
			os.Expand(v, func(name string) string {
				add(name, fmt.Sprintf("mcp %s %s %s", server, kind, k))
				return ""
			})
		}
	}
	for n, s := range cfg.MCP.Servers {
		refs(n, "header", s.Headers)
		refs(n, "env", s.Env)
	}
	for _, u := range out {
		sort.Strings(u)
	}
	return out
}
