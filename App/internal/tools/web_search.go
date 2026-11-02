package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"swarg/internal/chat"
	"swarg/internal/webget"
)

// ---------------------------------------------------------------------------
// web_search
// ---------------------------------------------------------------------------

const (
	maxSearchResults = 8
	snippetMax       = 200 // bytes of one snippet
	defaultSearchURL = "https://html.duckduckgo.com/html/"
)

// WebSearch searches the web and returns numbered title, url and snippet lines.
type WebSearch struct{ Free bool } // Free: no approval needed (config web.confirm = false)

// hit is one search result.
type hit struct{ title, url, snippet string }

// Spec describes web_search for the model.
func (WebSearch) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "web_search",
		Description: "Search the web. Returns titles, urls, snippets.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"query":{"type":"string","description":"search words"}},` +
			`"required":["query"],"additionalProperties":false}`),
	}
}

// ReadOnly is true only when freed: a tool that asks for approval runs on its own, never in a
// parallel batch (changes no files either way).
func (w WebSearch) ReadOnly() bool { return w.Free }

// Confirm is true unless the config frees the web tools.
func (w WebSearch) Confirm() bool { return !w.Free }

// Block is the system prompt text for web_search.
func (WebSearch) Block() string {
	return "web_search: search the web; then web_fetch a url to read it.\n" +
		`Example: {"query": "go os.Root documentation"}`
}

// Summary is the query of the call, or "" for bad JSON.
func (WebSearch) Summary(args json.RawMessage) string {
	var a struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	return a.Query
}

// Run asks SearXNG when the config names one, DuckDuckGo otherwise; at most 8 results.
func (t WebSearch) Run(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Query string `json:"query"`
	}
	if err := DecodeFor(t, args, &a, "query"); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(a.Query) == "" {
		return Result{}, errors.New("query is empty; give the words to search for")
	}
	var hits []hit
	var err error
	if env.Web.SearxURL != "" {
		hits, err = searchSearx(ctx, env.Web, a.Query)
	} else {
		hits, err = searchDDG(ctx, env.Web, a.Query)
	}
	if err != nil {
		return Result{}, fmt.Errorf("web_search: %w", err)
	}
	if len(hits) == 0 {
		return Result{Output: "no results", Brief: "0 results"}, nil
	}
	if len(hits) > maxSearchResults {
		hits = hits[:maxSearchResults]
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, h.title, h.url)
		if h.snippet != "" {
			fmt.Fprintf(&b, "   %s\n", clipBytes(h.snippet, snippetMax))
		}
	}
	return Result{Output: strings.TrimRight(b.String(), "\n"), Brief: fmt.Sprintf("%d results", len(hits))}, nil
}

// searchSearx queries the JSON API of a SearXNG instance the user configured.
func searchSearx(ctx context.Context, w webget.Getter, query string) ([]hit, error) {
	base, err := url.Parse(w.SearxURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("web.searx_url is not a valid URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/search"
	base.RawQuery = url.Values{"q": {query}, "format": {"json"}}.Encode()
	body, _, err := w.Get(ctx, base.String(), true)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []struct{ Title, URL, Content string }
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.New("SearXNG did not return JSON (is format=json enabled on it?)")
	}
	var hits []hit
	for _, r := range resp.Results {
		hits = append(hits, hit{oneLine(r.Title), r.URL, oneLine(r.Content)})
	}
	return hits, nil
}

// searchDDG reads the result links of the DuckDuckGo HTML page (class result__a / result__snippet).
func searchDDG(ctx context.Context, w webget.Getter, query string) ([]hit, error) {
	endpoint := w.SearchURL
	if endpoint == "" {
		endpoint = defaultSearchURL
	}
	body, _, err := w.Get(ctx, endpoint+"?"+url.Values{"q": {query}}.Encode(), true)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var hits []hit
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "a" {
			continue
		}
		switch {
		case hasClass(n, "result__a"):
			if u := resultURL(webget.Attr(n, "href")); u != "" {
				hits = append(hits, hit{title: oneLine(nodeText(n)), url: u})
			}
		case hasClass(n, "result__snippet") && len(hits) > 0 && hits[len(hits)-1].snippet == "":
			hits[len(hits)-1].snippet = oneLine(nodeText(n))
		}
	}
	return hits, nil
}

// resultURL unwraps a DuckDuckGo redirect link (//duckduckgo.com/l/?uddg=<url>); "" for ads
// and anything that is not an http(s) address.
func resultURL(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if t := u.Query().Get("uddg"); t != "" {
		href = t
	} else if strings.HasSuffix(u.Host, "duckduckgo.com") { // an ad (y.js) or an internal link
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	if _, err := webget.ParseURL(href); err != nil {
		return ""
	}
	return href
}

// hasClass reports whether the class attribute of n lists class.
func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(webget.Attr(n, "class")), class)
}

// nodeText is all the text under n.
func nodeText(n *html.Node) string {
	var b strings.Builder
	for c := range n.Descendants() {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// oneLine collapses all whitespace to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
