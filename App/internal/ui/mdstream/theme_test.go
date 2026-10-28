package mdstream

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// ---------------------------------------------------------------------------
// SGR parsing and hue classification (test helpers)
// ---------------------------------------------------------------------------

// rgb is one 8-bit colour.
type rgb struct{ r, g, b int }

// sgrColour is one colour set by an SGR sequence (foreground or background).
type sgrColour struct {
	c   rgb
	seq string
}

var (
	sgrRe      = regexp.MustCompile("\x1b\\[([0-9;:]*)m")
	cubeLevels = []int{0, 95, 135, 175, 215, 255}
	// basic16 is the xterm default palette for colour indices 0-15.
	basic16 = []rgb{
		{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
		{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
		{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
)

// rgb256 maps a 256-colour index through the xterm cube and grey ramp.
func rgb256(n int) rgb {
	switch {
	case n < 16:
		return basic16[n]
	case n < 232:
		n -= 16
		return rgb{cubeLevels[n/36], cubeLevels[(n/6)%6], cubeLevels[n%6]}
	default:
		v := 8 + 10*(n-232)
		return rgb{v, v, v}
	}
}

// sgrColours returns every colour set by the SGR sequences in s, in order. It fails the test
// on any sequence it cannot parse, so new escape forms cannot slip past the hue check.
func sgrColours(t *testing.T, s string) []sgrColour {
	t.Helper()
	var out []sgrColour
	for _, m := range sgrRe.FindAllStringSubmatch(s, -1) {
		seq := m[0]
		if strings.Contains(m[1], ":") {
			t.Fatalf("colon-form SGR not handled: %q", seq)
		}
		params := strings.Split(m[1], ";")
		for i := 0; i < len(params); i++ {
			p, _ := strconv.Atoi(params[i])
			switch {
			case p == 38 || p == 48:
				if i+2 < len(params) && params[i+1] == "5" {
					n, _ := strconv.Atoi(params[i+2])
					out = append(out, sgrColour{rgb256(n), seq})
					i += 2
				} else if i+4 < len(params) && params[i+1] == "2" {
					r, _ := strconv.Atoi(params[i+2])
					g, _ := strconv.Atoi(params[i+3])
					b, _ := strconv.Atoi(params[i+4])
					out = append(out, sgrColour{rgb{r, g, b}, seq})
					i += 4
				} else {
					t.Fatalf("unparsed extended colour in %q", seq)
				}
			case p >= 30 && p <= 37:
				out = append(out, sgrColour{basic16[p-30], seq})
			case p >= 90 && p <= 97:
				out = append(out, sgrColour{basic16[p-90+8], seq})
			case p >= 40 && p <= 47:
				out = append(out, sgrColour{basic16[p-40], seq})
			case p >= 100 && p <= 107:
				out = append(out, sgrColour{basic16[p-100+8], seq})
			}
		}
	}
	return out
}

// hueSat returns the HSV hue in degrees and saturation of c (hue 0 for greys).
func hueSat(c rgb) (float64, float64) {
	r, g, b := float64(c.r)/255, float64(c.g)/255, float64(c.b)/255
	mx, mn := max(r, g, b), min(r, g, b)
	d := mx - mn
	if mx == 0 || d == 0 {
		return 0, 0
	}
	var h float64
	switch mx {
	case r:
		h = 60 * ((g - b) / d)
	case g:
		h = 60 * ((b-r)/d + 2)
	default:
		h = 60 * ((r-g)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	return h, d / mx
}

// isRed reports whether c is a red hue: hue < 20 or > 340 with saturation above 0.4.
func isRed(c rgb) bool {
	h, s := hueSat(c)
	return s > 0.4 && (h < 20 || h > 340)
}

// parseHex converts "#rrggbb" to rgb.
func parseHex(t *testing.T, h string) rgb {
	t.Helper()
	if len(h) != 7 || h[0] != '#' {
		t.Fatalf("not a #rrggbb colour: %q", h)
	}
	v, err := strconv.ParseUint(h[1:], 16, 32)
	if err != nil {
		t.Fatalf("bad hex %q: %v", h, err)
	}
	return rgb{int(v>>16) & 0xff, int(v>>8) & 0xff, int(v) & 0xff}
}

// themeSampleSrc is the markdown sample for the theme checks. {{F}} stands for a fence and
// {{I}} for one backtick, so the source stays readable.
const themeSampleSrc = `# H1 title
## H2 title
### H3 title
#### H4 title
##### H5 title
###### H6 title

Plain paragraph with **bold text**, *italic text*, ~~struck~~ and inline {{I}}inline code{{I}} here.

A [link to docs](https://example.com/docs) inside text.

- bullet one
  - nested bullet
- bullet two

1. first
2. second

> quoted line
> second quoted line

| name  | value |
|-------|-------|
| alpha | 1     |
| beta  | 22    |

---

{{F}}go
package main

// main says hi
func main() {
	name := "swarg"
	n := 42
	fmt.Println("hi", name, n, true, nil) // trailing comment
}
{{F}}

{{F}}python
def add(a: int, b: int) -> int:
    # sum of two
    return a + b  # "ok"
{{F}}

{{F}}json
{"name": "swarg", "port": 8080, "debug": false, "tags": ["a", "b"]}
{{F}}

{{F}}bash
# build step
echo "hello" && ls -la | grep go
{{F}}

{{F}}diff
--- a/main.go
+++ b/main.go
@@ -1,2 +1,2 @@
-old line
+new line
 context line
{{F}}

{{F}}zzlang
some unknown_lang code 123 "str" ; end
{{F}}
`

// themeSample returns the markdown sample with the placeholders expanded.
func themeSample() string {
	return strings.NewReplacer("{{F}}", "```", "{{I}}", "`").Replace(themeSampleSrc)
}

// walkColours calls fn for every non-nil Color or BackgroundColor string reachable from v.
func walkColours(v reflect.Value, path string, fn func(path, colour string)) {
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			walkColours(v.Elem(), path, fn)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			p := path + "." + f.Name
			if (f.Name == "Color" || f.Name == "BackgroundColor") && f.Type == reflect.TypeOf((*string)(nil)) {
				if !v.Field(i).IsNil() {
					fn(p, v.Field(i).Elem().String())
				}
				continue
			}
			walkColours(v.Field(i), p, fn)
		}
	}
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestTheme_NoRedInRenderedSample(t *testing.T) {
	errRGB := parseHex(t, Colors.Error)
	bad := map[string]bool{}
	for _, c := range sgrColours(t, Render(themeSample(), 100)) {
		if isRed(c.c) && c.c != errRGB {
			bad[fmt.Sprintf("%v from %q", c.c, c.seq)] = true
		}
	}
	for b := range bad {
		t.Errorf("red colour outside Colors.Error: %s", b)
	}
}

func TestTheme_SampleIsActuallyColoured(t *testing.T) {
	seen := map[rgb]bool{}
	for _, c := range sgrColours(t, Render(themeSample(), 100)) {
		seen[c.c] = true
	}
	for name, hex := range map[string]string{
		"Code": Colors.Code, "CodeBg": Colors.CodeBg, "Heading": Colors.Heading,
		"Link": Colors.Link, "Dim": Colors.Dim, "Accent": Colors.Accent,
	} {
		if !seen[parseHex(t, hex)] {
			t.Errorf("palette %s (%s) never appears in the rendered sample", name, hex)
		}
	}
}

func TestTheme_PaletteIsSingleSource(t *testing.T) {
	pv := reflect.ValueOf(Colors)
	fieldOf := map[string]string{} // lower-case hex -> Palette field name
	for i := 0; i < pv.NumField(); i++ {
		fieldOf[strings.ToLower(pv.Field(i).String())] = pv.Type().Field(i).Name
	}
	n := 0
	walkColours(reflect.ValueOf(GlamourStyle()), "GlamourStyle", func(path, colour string) {
		n++
		field, ok := fieldOf[strings.ToLower(colour)]
		if !ok {
			t.Errorf("%s = %q is not a Colors value", path, colour)
			return
		}
		if field != "Error" && isRed(parseHex(t, colour)) {
			t.Errorf("%s uses red %s (Colors.%s); only Error may be red", path, colour, field)
		}
	})
	if n < 20 {
		t.Fatalf("walker found only %d colours; the style walk is broken", n)
	}
}

func TestTheme_GlamourStyleStable(t *testing.T) {
	before, err := json.Marshal(styles.DarkStyleConfig)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(GlamourStyle())
	b, _ := json.Marshal(GlamourStyle())
	if string(a) != string(b) {
		t.Errorf("GlamourStyle is not deterministic")
	}
	after, _ := json.Marshal(styles.DarkStyleConfig)
	if string(before) != string(after) {
		t.Errorf("GlamourStyle mutated styles.DarkStyleConfig")
	}
	if GlamourStyle().CodeBlock.Chroma == styles.DarkStyleConfig.CodeBlock.Chroma {
		t.Errorf("Chroma must be a new struct, not the shared global")
	}
}

func TestTheme_LayoutMatchesDark(t *testing.T) {
	// The stock dark style, with only the Chroma block swapped for ours so that no stock
	// token colour is registered for the process: the plain-text layout must be the same.
	dark := styles.DarkStyleConfig
	dark.CodeBlock.Chroma = GlamourStyle().CodeBlock.Chroma
	r, err := glamour.NewTermRenderer(glamour.WithStyles(dark), glamour.WithWordWrap(100))
	if err != nil {
		t.Fatal(err)
	}
	old, err := r.Render(themeSample())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stripANSI(Render(themeSample(), 100)), stripANSI(trimBlank(old)); got != want {
		t.Errorf("themed layout differs from dark\n got: %q\nwant: %q", got, want)
	}
}

func TestTheme_RenderedSampleNeverPanics(t *testing.T) {
	src := themeSample()
	for _, w := range []int{-1, 0, 1, 2, 10, 40, 100, 400} {
		// Render falls back to the raw block on any error or panic; a themed render differs.
		if got := Render(src, w); got == src || !strings.Contains(got, "\x1b[") {
			t.Errorf("width %d: the sample was not rendered (fallback to the raw block)", w)
		}
	}
}

// compile-time check that GlamourStyle returns the Glamour v2 type.
var _ ansi.StyleConfig = GlamourStyle()
