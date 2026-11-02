package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"swarg/internal/webget"
)

// ddgPage imitates html.duckduckgo.com/html/ (layout recorded by hand: redirect links in
// result__a, text in result__snippet, an ad that must be skipped).
const ddgPage = `<html><body><div class="serp__results">
<div class="result results_links result--ad"><h2 class="result__title"><a class="result__a" rel="nofollow" href="//duckduckgo.com/y.js?ad_provider=bing&u3=x">Buy stuff now</a></h2>
<a class="result__snippet" href="//duckduckgo.com/y.js?x">Sponsored</a></div>
<div class="result results_links results_links_deep web-result"><div class="links_main links_deep result__body">
<h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fos%23Root&amp;rut=abc">The <b>os</b> package - Go</a></h2>
<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2Fos">Package os provides a platform-independent
   interface to operating system functionality.</a></div></div>
<div class="result"><h2 class="result__title"><a class="result__a" href="https://example.org/direct">Direct link</a></h2></div>
</div></body></html>`

func search(env Env, query string) (Result, error) {
	args, _ := json.Marshal(map[string]string{"query": query})
	return WebSearch{}.Run(context.Background(), env, args)
}

func TestWebSearch_DuckDuckGoNumberedResults(t *testing.T) {
	var gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotUA = r.URL.Query().Get("q"), r.UserAgent()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, ddgPage)
	}))
	defer srv.Close()
	res, err := search(Env{Web: webget.Getter{SearchURL: srv.URL}}, "go os root")
	if err != nil {
		t.Fatal(err)
	}
	want := "1. The os package - Go\n   https://go.dev/doc/os#Root\n   Package os provides a platform-independent interface to operating system functionality.\n" +
		"2. Direct link\n   https://example.org/direct"
	if res.Output != want || res.Brief != "2 results" {
		t.Errorf("got:\n%s\nbrief %q", res.Output, res.Brief)
	}
	if gotQuery != "go os root" || gotUA == "" {
		t.Errorf("query %q, user agent %q", gotQuery, gotUA)
	}
}

func TestWebSearch_AtMostEightAndSnippetClipped(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, `<a class="result__a" href="https://e.org/%d">T%d</a><a class="result__snippet">%s</a>`, i, i, strings.Repeat("w", 500))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, b.String()) }))
	defer srv.Close()
	res, err := search(Env{Web: webget.Getter{SearchURL: srv.URL}}, "q")
	if err != nil || res.Brief != "8 results" || strings.Contains(res.Output, "9. ") || strings.Contains(res.Output, strings.Repeat("w", 300)) {
		t.Errorf("%v brief %q len %d", err, res.Brief, len(res.Output))
	}
}

func TestWebSearch_NoResultsAndBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "blocked" {
			http.Error(w, "anomaly", http.StatusAccepted+200) // 402-ish
			return
		}
		fmt.Fprint(w, "<html><body>No results.</body></html>")
	}))
	defer srv.Close()
	env := Env{Web: webget.Getter{SearchURL: srv.URL}}
	if res, err := search(env, "zzz"); err != nil || res.Output != "no results" {
		t.Errorf("no results: %q %v", res.Output, err)
	}
	if _, err := search(env, "blocked"); err == nil || !strings.HasPrefix(err.Error(), "web_search: HTTP 4") {
		t.Errorf("blocked: %v", err)
	}
}

func TestWebSearch_SearxJSON(t *testing.T) {
	var path, q, format string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, q, format = r.URL.Path, r.URL.Query().Get("q"), r.URL.Query().Get("format")
		fmt.Fprint(w, `{"results":[{"title":"Hit  one","url":"https://a.org/1","content":"first\nsnippet"},{"title":"Two","url":"https://b.org/2","content":""}]}`)
	}))
	defer srv.Close()
	res, err := search(Env{Web: webget.Getter{SearxURL: srv.URL + "/"}}, "x y")
	want := "1. Hit one\n   https://a.org/1\n   first snippet\n2. Two\n   https://b.org/2"
	if err != nil || res.Output != want {
		t.Errorf("got %q %v", res.Output, err)
	}
	if path != "/search" || q != "x y" || format != "json" {
		t.Errorf("request: %s q=%q format=%q", path, q, format)
	}
}

func TestWebSearch_SearxNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>") }))
	defer srv.Close()
	if _, err := search(Env{Web: webget.Getter{SearxURL: srv.URL}}, "q"); err == nil || !strings.Contains(err.Error(), "format=json") {
		t.Errorf("got %v", err)
	}
}

func TestWebSearch_BadArgsAndFlags(t *testing.T) {
	for _, a := range []string{`{}`, `{"query":"  "}`, `nope`} {
		if _, err := (WebSearch{}).Run(context.Background(), Env{}, json.RawMessage(a)); err == nil {
			t.Errorf("%s: want error", a)
		}
	}
	if (WebSearch{}).ReadOnly() || !(WebSearch{}).Confirm() || !(WebSearch{Free: true}).ReadOnly() || (WebSearch{Free: true}).Confirm() {
		t.Error("web_search: approval and sequential unless Free")
	}
	if (WebSearch{}).Summary(json.RawMessage(`{"query":"abc"}`)) != "abc" {
		t.Error("summary")
	}
}
