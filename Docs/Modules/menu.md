# Module menu (`App/internal/ui/menu`)

Purpose: the one list engine behind every picker of the app (R-99): rows, a cursor, a filter typed over the list, hover and click, the wheel, Esc, a footer with key hints or a confirmation. Imports only `ui/hover` and `ui/mdstream` (colours). Hosts in `ui` (`menus.go`, `agents_cmd.go`, `keys_menu.go`, `servers.go`, `prompt_cmd.go`, `picker.go`) format the rows and wire what happens. Standalone it is a small full-screen Bubble Tea program (`PickSession`); embedded it takes the list area of the main UI.

API:
- `New(Options) *Menu` - `Options{Title, Rows, Hint, Empty, OnChoose, OnRight, Keys, OnScope, Clean, Fit}`. `OnChoose(id) bool` runs on Enter / click (true closes; nil = close); `OnRight(id)` on a click of the Right label; `Keys` maps hotkeys (`"ctrl+d"`, `"space"`, `"r"`, `"left"`) to `func(selectedID)`; `OnScope` set marks a scope-aware menu. `Clean` (typed text) and `Fit` (colour profile of the standalone screen) are the host's functions, so the package needs no `ui` import; nil = identity. The working state (rows, cursor, filter, footer) stays unexported.
- `Row{ID, Text, Match, Right, Dim}` - `Text` is laid out by the host, `Match` is what the filter searches (`Text` when empty; a `Dim` row matches only the empty filter), `Right` a clickable right-aligned label, `Dim` rows are skipped by the cursor.
- Input: `Key(tea.KeyPressMsg) Action`, `Mouse(tea.MouseMsg) Action`, `Wheel(tea.MouseWheelMsg)`; `Action` = `ActNone | ActClose | ActChosen`. Draw: `Lines() []string` (at most `h` rows), `View()`/`Update`/`Init` (standalone `tea.Model`), `Resize(w, h)`.
- Host state: `SetRows`, `SelectedID`, `VisIndex(id)`, `SelectNear(pos)`, `Confirm(ask, yesLabel, fn)`, `Note`/`SetNote`, `Chosen`/`TakeChosen`/`Choose(id)`, `Parent`/`SetParent`, `Attach(*hover.Hover)`, `ScopeAware`/`SetScopes`/`Scope`, `VisibleIDs`, `Top`, `Cursor`, `Page`, `Filter`, `Confirming`, `SpotAt(x, y)`.
- `Sel` (style of the selected row, also used by the command suggestions), `WheelRows` (3), `BtnCancel`.

Invariants:
- Hover spots: `row:<id>` (text), `right:<id>` (label plus two blanks), `btn:yes` / `btn:no` (confirmation); only for rows that are drawn.
- A pending confirmation takes the next key: `y` runs it, anything else cancels. `SetRows` keeps the cursor on the same id. Dim rows are never chosen.

Gotchas:
- Closures given in `Options` often need the menu itself (rebuild after a toggle): declare `var mn *menu.Menu`, build the options, then `mn = menu.New(o)` before the first `rebuild()`.
- `Resize` must run before `Lines()` in the embedded case (the host's `screen()` does it each draw).
