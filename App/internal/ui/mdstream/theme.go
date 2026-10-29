package mdstream

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// ---------------------------------------------------------------------------
// palette
// ---------------------------------------------------------------------------

// Palette holds every colour of the project as hex strings. It is the only place to retune
// the look: Glamour (markdown and code blocks) and the ui package read from Colors.
type Palette struct {
	Code    string // inline code and numbers in code blocks (cyan)
	CodeBg  string // background of code blocks and inline code (dark blue)
	Heading string // headings, bold and keywords (green)
	Bold    string // strong text (green)
	Link    string // links and names (light blue)
	Dim     string // comments, rules, secondary text (grey)
	Error   string // the only red in the theme
	Accent  string // strings and link text (teal)
	UserBg  string // background of the user's own messages (ui package)
	UserFg  string // text of the user's own messages, never the terminal default (ui package)
	TaskBg  string // background of the rows inside a Task block, a little lighter than the Session (ui package)
}

// Colors is the project palette. Values are chosen to survive quantisation to the xterm
// 256-colour cube and grey ramp, so terminal256 output keeps them exactly.
var Colors = Palette{
	Code:    "#5fd7d7",
	CodeBg:  "#1c2b3a",
	Heading: "#87d787",
	Bold:    "#87d787",
	Link:    "#5fafff",
	Dim:     "#808080",
	Error:   "#d75f5f",
	Accent:  "#5fd7af",
	UserBg:  "#26323f",
	UserFg:  "#e4e4e4",
	TaskBg:  "#121212",
}

// ---------------------------------------------------------------------------
// glamour style
// ---------------------------------------------------------------------------

// GlamourStyle returns the dark Glamour layout with the Colors palette applied. The result
// is built from a copy of styles.DarkStyleConfig: every colour pointer is new, and the global
// is never written. The Chroma block is replaced whole, so no stock token colour survives.
func GlamourStyle() ansi.StyleConfig {
	c := Colors
	s := styles.DarkStyleConfig // value copy; colour pointers below are replaced, never written

	s.Document.Color = nil
	s.Heading.Color = str(c.Heading)
	s.H1 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
		Prefix: " ", Suffix: " ", Color: str(c.CodeBg), BackgroundColor: str(c.Heading), Bold: boolp(true),
	}}
	s.H2.Color = str(c.Heading)
	s.H3.Color = str(c.Heading)
	s.H4.Color = str(c.Heading)
	s.H5.Color = str(c.Heading)
	s.H6.Color = str(c.Dim)
	s.H6.Bold = boolp(false)
	s.HorizontalRule.Color = str(c.Dim)
	s.Strong = ansi.StylePrimitive{Bold: boolp(true), Color: str(c.Bold)}
	s.Link = ansi.StylePrimitive{Color: str(c.Link), Underline: boolp(true)}
	s.LinkText = ansi.StylePrimitive{Color: str(c.Accent), Bold: boolp(true)}
	s.Image = ansi.StylePrimitive{Color: str(c.Link), Underline: boolp(true)}
	s.ImageText.Color = str(c.Dim)
	s.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
		Prefix: " ", Suffix: " ", Color: str(c.Code), BackgroundColor: str(c.CodeBg),
	}}
	s.CodeBlock.Color = nil
	s.CodeBlock.BackgroundColor = str(c.CodeBg)
	s.CodeBlock.Chroma = chromaTokens()
	return s
}

// chromaTokens is the syntax palette for fenced code blocks: keywords green, strings teal,
// numbers cyan, comments dim grey, names light blue, and red only for Error. Every token
// reads a Colors field, so the whole block is replaced rather than merged with dark's.
func chromaTokens() *ansi.Chroma {
	c := Colors
	fg := func(hex string) ansi.StylePrimitive { return ansi.StylePrimitive{Color: str(hex)} }
	keyword := fg(c.Heading)
	strLit := fg(c.Accent)
	number := fg(c.Code)
	comment := fg(c.Dim)
	name := fg(c.Link)
	return &ansi.Chroma{
		Text:                ansi.StylePrimitive{},
		Error:               ansi.StylePrimitive{Color: str(c.Error), Bold: boolp(true)},
		Comment:             comment,
		CommentPreproc:      strLit,
		Keyword:             keyword,
		KeywordReserved:     keyword,
		KeywordNamespace:    keyword,
		KeywordType:         name,
		Operator:            comment,
		Punctuation:         comment,
		Name:                name,
		NameBuiltin:         name,
		NameTag:             name,
		NameAttribute:       name,
		NameClass:           name,
		NameConstant:        name,
		NameDecorator:       strLit,
		NameException:       name,
		NameFunction:        name,
		NameOther:           name,
		Literal:             strLit,
		LiteralNumber:       number,
		LiteralDate:         number,
		LiteralString:       strLit,
		LiteralStringEscape: number,
		GenericDeleted:      comment,
		GenericEmph:         ansi.StylePrimitive{Italic: boolp(true)},
		GenericInserted:     keyword,
		GenericStrong:       ansi.StylePrimitive{Bold: boolp(true)},
		GenericSubheading:   name,
		Background:          ansi.StylePrimitive{BackgroundColor: str(c.CodeBg)},
	}
}

// str returns a pointer to a copy of v (Glamour style fields are pointers).
func str(v string) *string { return &v }

// boolp returns a pointer to a copy of v.
func boolp(v bool) *bool { return &v }
