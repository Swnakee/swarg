// Package webget is the shared web access of web_search and web_fetch: an HTTP GET that refuses private addresses (R-47, I-15), and HTML to text.
package webget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ---------------------------------------------------------------------------
// Getter
// ---------------------------------------------------------------------------

const (
	defaultWebTimeout = 15 * time.Second
	MaxBody           = 1 << 20 // bytes read from one response
	webUserAgent      = "Mozilla/5.0 (compatible; swarg)"
)

// Getter is the web tools' part of tools.Env. The zero value is a safe default: 15 s timeout,
// private addresses refused, DuckDuckGo for search.
type Getter struct {
	SearxURL     string        // SearXNG base URL; when set, web_search uses it instead of DuckDuckGo
	AllowPrivate bool          // let web_fetch reach loopback, private and link-local addresses
	Timeout      time.Duration // whole request; 0 = 15 s
	// Seams for tests; nil = the real resolver and the real DuckDuckGo endpoint.
	Lookup    func(ctx context.Context, host string) ([]net.IP, error)
	SearchURL string
}

// refusedError says web_fetch will not talk to an address.
type refusedError struct {
	host string
	ip   net.IP
}

func (e *refusedError) Error() string {
	return fmt.Sprintf("refused: %s is a private or local address (%s)", e.host, e.ip)
}

// blockedIP reports addresses a model-chosen URL must not reach: loopback, private (also
// carrier-grade NAT, e.g. Tailscale), link-local, multicast and unspecified.
func blockedIP(ip net.IP) bool {
	ip = ip.To16()
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		if v4[0] == 100 && v4[1]&0xC0 == 64 { // 100.64.0.0/10
			return true
		}
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified()
}

// dial resolves the host, checks EVERY address, then connects to a checked one (so a second
// resolution cannot swap in a private address); it runs for every redirect hop too.
func (w Getter) dial(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else if w.Lookup != nil {
			ips, err = w.Lookup(ctx, host)
		} else {
			ips, err = net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no address for %s", host)
		}
		if !allowPrivate {
			for _, ip := range ips {
				if blockedIP(ip) {
					return nil, &refusedError{host, ip}
				}
			}
		}
		d := net.Dialer{Timeout: 5 * time.Second}
		for _, ip := range ips {
			var c net.Conn
			if c, err = d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
				return c, nil
			}
		}
		return nil, err
	}
}

// Get fetches rawURL and returns at most MaxBody bytes of the body with its Content-Type.
// allowPrivate is for the search backends the user configured, never for model-chosen URLs.
func (w Getter) Get(ctx context.Context, rawURL string, allowPrivate bool) (body []byte, ctype string, err error) {
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = defaultWebTimeout
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:       w.dial(allowPrivate), // no Proxy: a proxy would hide the target from the check
			DisableKeepAlives: true,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", webError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, "", webError(err)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// webError turns a transport error into one clear line.
func webError(err error) error {
	var re *refusedError
	var ue *url.Error
	switch {
	case errors.As(err, &re):
		return re
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ue) && ue.Timeout()):
		return errors.New("timed out")
	case errors.As(err, &ue):
		return fmt.Errorf("cannot reach the server (%v); the network may be unavailable", ue.Err)
	}
	return err
}

// ParseURL accepts only absolute http and https URLs.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("url must be an absolute http or https address, e.g. https://example.com/page")
	}
	return u, nil
}
