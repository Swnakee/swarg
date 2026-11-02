# Module hover (`App/internal/ui/hover`)

Purpose: the pointer position and the clickable spans of the last draw; the underline look of a hovered span. Pure string/cell logic, no Bubble Tea, no other swarg import. I-10: a pointer move changes nothing but the look.

API:
- `Hover` (zero value ready): `Move(x, y)` records the pointer; `Begin()` forgets the spots of the previous draw; `Add(id, y, x0, x1) bool` registers a span of cells `[x0, x1)` on row `y` and says whether the pointer is over it (an empty span registers nothing); `Over(y, x0, x1)`; `At(x, y) (id, ok)` is the spot under a cell, the last registered of overlapping ones; `Apply(rows)` underlines every spot under the pointer and every other spot with the same id (a wrapped head is hovered as a whole).
- `Spot{ID, Y, X0, X1}` - one registered span (used by hosts that build spots before registering them).
- `Span(row, c0, c1)` - underline cells of a styled row, colours kept, trailing blanks left alone, a fresh underline after every SGR inside the span.
- `CutSpan(row, c0, c1) (pre, mid, post, ok)` - split a styled row around cells, widened to whole graphemes; the selection highlight (`ui/select.go`) shares it. `CellSpan(plain, c0, c1)` - the grapheme-aligned cell range and its text.
- `SGR` - regexp of one SGR sequence (ui `colors.go` aliases it). `UnderlineOn`, `UnderlineOff` - the escape strings.

Invariants:
- Spots of a draw are replaced as a whole (`Begin`); click dispatch (`At`) uses the spots of the last draw.
- Cell columns are display cells, not bytes or runes (wide characters count 2).

Gotchas:
- Hover id conventions (`row:<id>`, `tool:<n>`, `panel:<x>`, `cmd:<i>`, `btn:yes`) belong to the hosts, not this package.
