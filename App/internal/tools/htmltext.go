package tools

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// ---------------------------------------------------------------------------
// HTML to readable text
// ---------------------------------------------------------------------------

// skipTags hold no readable text.
var skipTags = map[string]bool{"script": true, "style": true, "noscript": true, "template": true,
	"svg": true, "iframe": true, "meta": true, "link": true,
	"nav": true, "aside": true, "footer": true} // site chrome, not page text (R-71)

// blockTags start and end a line.
var blockTags = map[string]bool{"title": true, "p": true, "div": true, "section": true, "article": true,
	"header": true, "footer": true, "nav": true, "main": true, "ul": true, "ol": true, "table": true,
	"tr": true, "pre": true, "blockquote": true, "br": true, "hr": true, "form": true, "dl": true,
	"dt": true, "dd": true, "figure": true, "figcaption": true}

// htmlToText returns the readable text of an HTML page: no scripts or styles, headings as
// "# ", list items as "- ", whitespace collapsed. ctype helps to pick the charset. With a base URL
// a link to another page is written as "text (absolute url)" (R-72); without one only its text.
func htmlToText(r io.Reader, ctype string, base *url.URL) (string, error) {
	cr, err := charset.NewReader(r, ctype)
	if err == io.EOF { // an empty page
		return "", nil
	}
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(cr)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(n.Data))
			return
		case n.Type == html.ElementNode && skipTags[n.Data]:
			return
		}
		tag := ""
		if n.Type == html.ElementNode {
			tag = n.Data
		}
		switch {
		case len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6':
			b.WriteString("\n\n" + strings.Repeat("#", int(tag[1]-'0')) + " ")
		case tag == "li":
			b.WriteString("\n- ")
		case blockTags[tag]:
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if tag == "a" && base != nil {
			if u := linkURL(base, attr(n, "href")); u != "" {
				b.WriteString(" (" + u + ")")
			}
		}
		if blockTags[tag] || (len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6') {
			b.WriteString("\n")
		}
	}
	walk(doc)
	return tidyLines(b.String()), nil
}

// linkURL is href made absolute against base; "" for non-http links and for links within the
// same page (fragments).
func linkURL(base *url.URL, href string) string {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil || href == "" {
		return ""
	}
	u := base.ResolveReference(ref)
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	page, self := *u, *base
	page.Fragment, self.Fragment = "", ""
	if page == self {
		return ""
	}
	return u.String()
}

// tidyLines collapses the whitespace of every line and at most one blank line in a row.
func tidyLines(s string) string {
	var out []string
	blank := true // drops leading blank lines
	for _, line := range strings.Split(s, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || line == "#" || line == "-" { // an empty heading or item
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
