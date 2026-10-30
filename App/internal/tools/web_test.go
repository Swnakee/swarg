package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pageServer serves body with the content type on every path.
func pageServer(t *testing.T, ctype, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ctype)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// localWeb is a Web that may reach the loopback test server.
func localWeb() Env { return Env{Web: Web{AllowPrivate: true, Timeout: 5 * time.Second}} }

func fetch(env Env, url string) (Result, error) {
	args, _ := json.Marshal(map[string]string{"url": url})
	return WebFetch{}.Run(context.Background(), env, args)
}

// ---------------------------------------------------------------------------
// Address check (R-47, I-15)
// ---------------------------------------------------------------------------

func TestBlockedIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "10.1.2.3": true, "192.168.1.5": true, "172.16.0.1": true,
		"169.254.169.254": true, "100.64.0.1": true, "0.0.0.0": true, "fe80::1": true, "fc00::1": true,
		"::ffff:127.0.0.1": true, "224.0.0.1": true,
		"8.8.8.8": false, "93.184.216.34": false, "100.63.0.1": false, "2606:4700::1111": false,
	} {
		if got := blockedIP(net.ParseIP(ip)); got != want {
			t.Errorf("blockedIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestWebFetch_RefusesPrivateAddresses(t *testing.T) {
	srv := pageServer(t, "text/plain", "secret")
	lookup := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "mixed.test": // one public and one private answer: the private one decides
			return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	for _, u := range []string{
		srv.URL, // literal 127.0.0.1
		"http://localhost:1/", "http://internal.test/", "http://mixed.test/",
		"http://[::1]:1/", "http://169.254.169.254/latest/meta-data/", fmt.Sprintf("http://mixed.test:%d/", port),
	} {
		_, err := fetch(Env{Web: Web{Lookup: lookup}}, u)
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: want a refusal, got %v", u, err)
		}
	}
}

func TestWebFetch_ConfigAllowsPrivate(t *testing.T) {
	srv := pageServer(t, "text/plain", "hello from lan")
	res, err := fetch(localWeb(), srv.URL)
	if err != nil || res.Output != "hello from lan" {
		t.Fatalf("got %q %v", res.Output, err)
	}
}

// ---------------------------------------------------------------------------
// web_fetch
// ---------------------------------------------------------------------------

func TestWebFetch_OnlyHTTP(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "example.com", "http://", "javascript:alert(1)"} { // "" is a missing argument (R-46)
		if _, err := fetch(localWeb(), u); err == nil || !strings.Contains(err.Error(), "http or https") {
			t.Errorf("%q: got %v", u, err)
		}
	}
}

func TestWebFetch_HTMLToText(t *testing.T) {
	page := `<html><head><title>My  Page</title><style>p{color:red}</style><script>var x="secret";</script></head>
<body><h1>Intro</h1><p>Hello <a href="/x">the link</a> and <b>bold</b>
  text.</p><ul><li>one</li><li>two</li></ul><noscript>no js</noscript><h2>Next</h2><div>tail</div></body></html>`
	srv := pageServer(t, "text/html; charset=utf-8", page)
	res, err := fetch(localWeb(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := "My Page\n\n# Intro\n\nHello the link (" + srv.URL + "/x) and bold text.\n\n- one\n- two\n\n## Next\n\ntail"
	if res.Output != want {
		t.Errorf("got:\n%q\nwant:\n%q", res.Output, want)
	}
	if strings.Contains(res.Output, "secret") || strings.Contains(res.Output, "color") {
		t.Error("script or style leaked")
	}
}

func TestWebFetch_Charset(t *testing.T) {
	// windows-1251 "Привет" announced in the header
	srv := pageServer(t, "text/html; charset=windows-1251", "<p>\xcf\xf0\xe8\xe2\xe5\xf2</p>")
	res, err := fetch(localWeb(), srv.URL)
	if err != nil || res.Output != "Привет" {
		t.Fatalf("got %q %v", res.Output, err)
	}
}

func fetchPage(env Env, url string, page int) (Result, error) {
	args, _ := json.Marshal(map[string]any{"url": url, "page": page})
	return WebFetch{}.Run(context.Background(), env, args)
}

// longText has numbered lines, so every part of it can be found again (about 3 pages).
func longText() string {
	var b strings.Builder
	for i := 1; i <= 1200; i++ {
		fmt.Fprintf(&b, "line %04d of text\n", i)
	}
	return b.String()
}

// R-70: a long page comes in pages; nothing in the middle is lost.
func TestWebFetch_LongPageIsPaged(t *testing.T) {
	srv := pageServer(t, "text/plain", longText())
	res, err := fetch(localWeb(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Output) > maxPageText+200 || !strings.HasPrefix(res.Output, "line 0001") ||
		!strings.HasSuffix(res.Output, "[page 1 of 3; for more call web_fetch with the same url and page 2]") {
		t.Errorf("page 1: %d bytes ...%q", len(res.Output), res.Output[len(res.Output)-80:])
	}
	var all strings.Builder
	for p := 1; p <= 3; p++ {
		r, err := fetchPage(localWeb(), srv.URL, p)
		if err != nil {
			t.Fatalf("page %d: %v", p, err)
		}
		all.WriteString(r.Output)
	}
	for _, want := range []string{"line 0001 ", "line 0600 ", "line 1200 "} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("%q lost across the pages", want)
		}
	}
	if r, _ := fetchPage(localWeb(), srv.URL, 3); strings.Contains(r.Output, "page 4") || !strings.Contains(r.Output, "[page 3 of 3]") {
		t.Errorf("last page note: %q", r.Output[len(r.Output)-60:])
	}
}

// R-70 boundary and failure: a short page has no page note; a page past the end is an error.
func TestWebFetch_PageBounds(t *testing.T) {
	srv := pageServer(t, "text/plain", "short")
	if r, err := fetchPage(localWeb(), srv.URL, 1); err != nil || r.Output != "short" {
		t.Errorf("got %q %v", r.Output, err)
	}
	if _, err := fetchPage(localWeb(), srv.URL, 2); err == nil || !strings.Contains(err.Error(), "only 1 page") {
		t.Errorf("past the end: %v", err)
	}
	if _, err := fetchPage(localWeb(), srv.URL, -1); err == nil || !strings.Contains(err.Error(), "page must be") {
		t.Errorf("page -1: %v", err)
	}
}

// R-71: navigation, sidebars and footers are not page text.
func TestWebFetch_DropsNavAsideFooter(t *testing.T) {
	page := `<html><body><header><nav><a href="/">Home</a> | <a href="/x">Menu item</a></nav></header>
<aside><ul><li>On this page</li></ul></aside><main><h1>Title</h1><p>Body text.</p></main>
<footer>Copyright cookies</footer></body></html>`
	srv := pageServer(t, "text/html", page)
	res, err := fetch(localWeb(), srv.URL)
	if err != nil || res.Output != "# Title\n\nBody text." {
		t.Errorf("got %q %v", res.Output, err)
	}
}

func TestWebFetch_BodyLimit(t *testing.T) {
	srv := pageServer(t, "text/plain", strings.Repeat("x", maxWebBody*2))
	if _, err := fetch(localWeb(), srv.URL); err != nil { // reads only 1 MB, no error
		t.Fatal(err)
	}
}

func TestWebFetch_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/bin":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "\x89PNG")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for path, want := range map[string]string{"/missing": "HTTP 404", "/loop": "redirects", "/bin": "not a text page (image/png)"} {
		if _, err := fetch(localWeb(), srv.URL+path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", path, err, want)
		}
	}
}

func TestWebFetch_TimeoutAndNoNetwork(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	env := localWeb()
	env.Web.Timeout = 50 * time.Millisecond
	if _, err := fetch(env, srv.URL); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: %v", err)
	}
	// A sandbox without internet: the lookup fails at once and the message says why.
	env = Env{Web: Web{Lookup: func(context.Context, string) ([]net.IP, error) { return nil, errors.New("no such host") }}}
	start := time.Now()
	_, err := fetch(env, "https://example.com/")
	if err == nil || !strings.Contains(err.Error(), "network may be unavailable") || time.Since(start) > 2*time.Second {
		t.Errorf("no network: %v after %v", err, time.Since(start))
	}
}

func TestWebFetch_Flags(t *testing.T) {
	if (WebFetch{}).ReadOnly() || !(WebFetch{}).Confirm() || !(WebFetch{Free: true}).ReadOnly() || (WebFetch{Free: true}).Confirm() {
		t.Error("web_fetch: approval and sequential unless Free")
	}
	if (WebFetch{}).Summary(json.RawMessage(`{"url":"https://a.b/c"}`)) != "https://a.b/c" || (WebFetch{}).Summary(json.RawMessage(`{`)) != "" {
		t.Error("summary")
	}
	if _, err := (WebFetch{}).Run(context.Background(), Env{}, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), `"url"`) {
		t.Errorf("missing url: %v", err)
	}
}

func TestFreeWeb(t *testing.T) {
	s, _ := NewSet(FreeWeb(All()), nil)
	for _, n := range []string{"web_search", "web_fetch"} {
		if _, ro, confirm := s.Kind(n); !ro || confirm { // Free: parallel and unasked
			t.Errorf("%s: readOnly %v confirm %v after FreeWeb", n, ro, confirm)
		}
	}
	if _, _, confirm := s.Kind("shell"); !confirm {
		t.Error("FreeWeb changed shell")
	}
	s, _ = NewSet(All(), nil)
	if _, ro, confirm := s.Kind("web_search"); ro || !confirm {
		t.Error("web_search must ask by default")
	}
}

// R-72: links keep their address (absolute), so the model can follow them.
func TestWebFetch_LinksKeepAddress(t *testing.T) {
	page := `<p>See the <a href="/docs/migrate.html">migration guide</a>, <a href="https://other.example/x">x</a>,
<a href="#top">top</a> and <a href="javascript:void(0)">js</a>.</p>`
	srv := pageServer(t, "text/html", page)
	res, err := fetch(localWeb(), srv.URL+"/docs/upgrade.html")
	want := "See the migration guide (" + srv.URL + "/docs/migrate.html), x (https://other.example/x), top and js."
	if err != nil || res.Output != want {
		t.Errorf("got  %q %v\nwant %q", res.Output, err, want)
	}
}

func TestWebFetch_SummaryShowsPage(t *testing.T) {
	for args, want := range map[string]string{
		`{"url":"https://e.org/a"}`:          "https://e.org/a",
		`{"url":"https://e.org/a","page":1}`: "https://e.org/a",
		`{"url":"https://e.org/a","page":2}`: "https://e.org/a page 2",
	} {
		if got := (WebFetch{}).Summary(json.RawMessage(args)); got != want {
			t.Errorf("%s: got %q, want %q", args, got, want)
		}
	}
}
