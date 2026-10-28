# Module mdstream (`App/internal/ui/mdstream`)

Purpose: split streamed markdown into completed blocks (Committer) and render one block to ANSI (Render). Owns the project colour palette (theme.go). Depends only on Glamour v2.

API:
- `New() *Committer` - empty committer.
- `(*Committer).Write(chunk) []string` - blocks that became complete.
- `(*Committer).Tail() string` - open text: unfinished block plus the incomplete last line.
- `(*Committer).OpenFence() string` - marker of the fence the text is inside now, "" if none (the UI closes it on interrupt).
- `(*Committer).Flush() []string` - the rest at turn end (unclosed fence included); resets; a second call returns nil.
- `Render(block, width) string` - Glamour with `GlamourStyle()`, word wrap `width` (<=0 means 80), blank edges trimmed.
- `Colors Palette` (theme.go) - the only place with colours: Code, CodeBg, Heading, Bold, Link, Dim, Error, Accent, UserBg, UserFg (hex). UserFg is the text of user messages and of the picker's selected row: every painted background carries its own foreground. `Error` is the only red.
- `GlamourStyle() ansi.StyleConfig` - copy of Glamour's dark layout with `Colors` applied and the whole Chroma block replaced (keywords green, strings teal, numbers cyan, comments dim, names light blue).

Invariants:
- A block is whole lines with their terminators. Plain text lines and blank lines are blocks of their own. Blocks partition the input exactly: `concat(blocks) + Tail() == input` after every Write; after Flush `concat(blocks) == input` - EXCEPT the synthetic closer/opener marker lines added by fence segmenting (the only text in blocks that is not in the input; the body lines stay exact and in order). `Tail()` / `Flush()` omit a bare synthetic reopener.
- The block sequence is identical for any chunk split (only complete lines are classified; a split rune is just buffered).
- Fence: opener has up to 3 spaces of indent (any indent in a list), 3+ of ` or ~, no backtick in a backtick fence info; closer is the same char, at least as many, spaces only. Nothing splits inside a fence.
- Commit rule: plain text lines (paragraph, heading, other) and blank lines commit one line at a time. Lists (items `-*+`/`N.`/`N)` plus indented and blank lines) are held until the first non-item, non-indented line. Tables (lines starting with `|`) end at a blank (included) or non-`|` line. A fence ends at its closer, but every `segmentLines` (20) body lines it is committed as a closed block (closer line added) and reopened with the same opener line (info string and indent kept), so a long answer, or one whose fence never closes, is visible while it streams. A closer arriving right after a segment adds no empty block.

Gotchas:
- A list block is committed only when the next non-list line (or Flush) arrives.
- A blank-only block renders as ""; the UI must still print it (tea.Println) - it is the one empty line between paragraphs. Do not skip it.
- Accepted limits: setext headings and multi-line blockquotes render line by line.
- Render uses a fixed style, never auto-detection (terminal queries would race with Bubble Tea stdin). It recovers panics and returns the plain block.
- Never render with Glamour's stock "dark" style: the Chroma style name "charm" is registered once per process (first wins), so a stock render would fix the code colours for the whole process. Only GlamourStyle() may be used.
- GlamourStyle() must not write to styles.DarkStyleConfig: every colour pointer is new. Code, Heading, Bold, Link, Dim, Error and Accent are exact in the xterm 256 cube or grey ramp, so terminal256 keeps them; CodeBg and UserBg are not (they are not rendered through Chroma).
- theme_test.go parses every SGR (38/48 with 5 or 2, basic 30-37/40-47/90-97/100-107) and classifies by hue; red = hue <20 or >340 with saturation >0.4; the only allowed red is Colors.Error. Any new colour in the style must come from Colors (TestTheme_PaletteIsSingleSource).
- Glamour pads blank lines with styled spaces, so trimming ignores ANSI codes.
- Segmenting also cuts a long CLOSED fence into 20-line blocks (the committer cannot know a closer will come); each segment renders as its own code block.

## RenderSoft (copy marks)

- `RenderSoft(block, width)` is `Render` plus zero-width APC marks in front of rows: `MarkSoft` (the row is a glamour wrap of the row above) and `Lead(n)` (n leading cells are renderer margin: document margin 2, code block rows their block's least indentation). The ui turns them into the copied text (see `ui.md`, Mouse/selection).
- Soft rows: the block is rendered a second time wide enough to wrap nothing; rows are matched to its logical lines by their text without whitespace. Any mismatch (a table whose cells wrap, for one) marks no row soft for the whole block. A block shorter than the width skips the second render.
- Code rows: rows that move when the code block margin is set to 0 (a third render, only when the block may hold code).
- Gotchas: marks are added after `trimBlank` (it only knows CSI). `docMargin`/`codeMargin` must equal the style's margins (a test asserts it). Blank rows between two code blocks make them share one indentation. Two back-to-back soft-wrapped paragraphs without a blank row cannot occur in glamour output.
