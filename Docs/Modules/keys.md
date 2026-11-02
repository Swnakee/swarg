# Module keys (`App/internal/keys`)

Purpose: read-only view of the named secrets (Keys): environment, the Keys file, the Old Key directory (R-105, R-106, R-107, R-110, T12). Moved out of `config` in T14 S10. Imports no other swarg package.

API:
- `Store{File, OldDir}` - where keys are read from. `User()` = `<os.UserConfigDir()>/swarg/keys.env` and `.../swarg/keys` (same directory as `config.Dir`; duplicated because keys cannot import config); both "" when the config dir is unknown.
- `(Store) Lookup(name) (value, src)` - env, then file, then old dir; `src` is `SrcEnv` / `SrcFile` / `SrcOld`.
- `(Store) Expand(s)` - `$VAR` / `${VAR}` over the same order, the file read once.
- `(Store) State(name)` / `States(names)` -> `State{Env, File, Old, Value}`; `Names()` (file and old dir, sorted); `Warnings()`; `OldHint()`.
- `(Store) Init(names) error` - commented 0600 template, one `# NAME=` line per valid name; `O_EXCL`, `ErrKeysFileExists` and the file untouched when it exists; MkdirAll 0700.
- `ValidKeyName(name)` - `^[A-Z][A-Z0-9_]{0,63}$`. `MaskKey(v)` - first 6 + `…` + last 4 for 16+ characters, else `set`.

Invariants:
- The file is parsed afresh on every call: `NAME=value` lines, `#` comments, blanks, optional BOM, one pair of quotes stripped, `NAME=` is no key.
- A file readable by group/others (Windows exempt) is ignored with a warning naming the file and `chmod 600`; a bad line warns with its number only, never the content.
- There is no Set or Delete: swarg never writes a key value.

Gotchas:
- Tests use a `Store` over `t.TempDir()` or point HOME / XDG_CONFIG_HOME / AppData at a temp dir; never the user's real keys file.
- `config.KeyUsers(cfg)` (who uses which name) stays in `config` because it reads `Config`.
