package webget

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
		_, _, err := Getter{Lookup: lookup}.Get(context.Background(), u, false)
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: want a refusal, got %v", u, err)
		}
	}
}

func TestWebFetch_ConfigAllowsPrivate(t *testing.T) {
	srv := pageServer(t, "text/plain", "hello from lan")
	body, _, err := Getter{AllowPrivate: true, Timeout: 5 * time.Second}.Get(context.Background(), srv.URL, true)
	if err != nil || string(body) != "hello from lan" {
		t.Fatalf("got %q %v", body, err)
	}
}
