// Package pkggodev is the library behind the gopkg command: the HTTP client,
// request shaping, and typed data models for pkg.go.dev and the Go module
// proxy.
//
// Three data sources: pkg.go.dev for package search (HTML scraping),
// proxy.golang.org for version lists and latest info (plain text and JSON),
// and sum.golang.org for module hashes. All are open and require no API key.
package pkggodev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const defaultUserAgent = "gopkg/dev (+https://github.com/tamnd/pkggodev-cli)"

// ErrNotFound is returned when a module or version is not found.
var ErrNotFound = errors.New("not found")

// Config holds all Client constructor parameters.
type Config struct {
	// PkgBaseURL is the base URL for pkg.go.dev. Default: "https://pkg.go.dev".
	PkgBaseURL string
	// ProxyBaseURL is the base URL for the Go module proxy. Default: "https://proxy.golang.org".
	ProxyBaseURL string
	// SumBaseURL is the base URL for sum.golang.org. Default: "https://sum.golang.org".
	SumBaseURL string
	// UserAgent is the User-Agent header value.
	UserAgent string
	// Rate is the minimum gap between consecutive requests. Zero disables pacing.
	Rate time.Duration
	// Retries is the number of retry attempts on transient errors (429, 5xx).
	Retries int
	// Timeout is the per-request HTTP timeout.
	Timeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PkgBaseURL:   "https://pkg.go.dev",
		ProxyBaseURL: "https://proxy.golang.org",
		SumBaseURL:   "https://sum.golang.org",
		UserAgent:    defaultUserAgent,
		Rate:         200 * time.Millisecond,
		Retries:      3,
		Timeout:      30 * time.Second,
	}
}

// Client talks to pkg.go.dev, proxy.golang.org, and sum.golang.org.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured by cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Search returns packages matching query from pkg.go.dev/search.
// limit <= 0 returns all results on the first page.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Package, error) {
	u := c.cfg.PkgBaseURL + "/search?q=" + url.QueryEscape(query) + "&m=package"
	body, err := c.get(ctx, u, "text/html")
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	pkgs, err := parseSearch(body, c.cfg.PkgBaseURL)
	if err != nil {
		return nil, fmt.Errorf("search parse: %w", err)
	}
	if limit > 0 && limit < len(pkgs) {
		pkgs = pkgs[:limit]
	}
	return pkgs, nil
}

// Versions returns the version list for module from the GOPROXY, newest first.
// limit <= 0 returns all versions.
func (c *Client) Versions(ctx context.Context, module string, limit int) ([]Version, error) {
	u := c.cfg.ProxyBaseURL + "/" + module + "/@v/list"
	body, err := c.get(ctx, u, "text/plain")
	if err != nil {
		return nil, fmt.Errorf("versions %s: %w", module, err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var versions []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			versions = append(versions, l)
		}
	}
	// Sort descending by semver: simple lexicographic within major is fine for
	// standard semver strings; for full correctness we sort by component.
	sort.Slice(versions, func(i, j int) bool {
		return semverGT(versions[i], versions[j])
	})
	if limit > 0 && limit < len(versions) {
		versions = versions[:limit]
	}
	out := make([]Version, len(versions))
	for i, v := range versions {
		out[i] = Version{
			Rank:    i + 1,
			Version: v,
			URL:     moduleVersionURL(c.cfg.PkgBaseURL, module, v),
		}
	}
	return out, nil
}

// Latest returns the latest published version info for module.
func (c *Client) Latest(ctx context.Context, module string) (LatestInfo, error) {
	u := c.cfg.ProxyBaseURL + "/" + module + "/@latest"
	body, err := c.get(ctx, u, "application/json")
	if err != nil {
		return LatestInfo{}, fmt.Errorf("latest %s: %w", module, err)
	}
	var wire struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LatestInfo{}, fmt.Errorf("latest %s decode: %w", module, err)
	}
	return LatestInfo{
		Module:  module,
		Version: wire.Version,
		Time:    wire.Time,
		URL:     moduleVersionURL(c.cfg.PkgBaseURL, module, wire.Version),
	}, nil
}

// Hash looks up the checksum database entry for moduleAt ("module@version").
func (c *Client) Hash(ctx context.Context, moduleAt string) (HashInfo, error) {
	module, version, ok := strings.Cut(moduleAt, "@")
	if !ok {
		return HashInfo{}, fmt.Errorf("hash: argument must be module@version, got %q", moduleAt)
	}
	u := c.cfg.SumBaseURL + "/lookup/" + module + "@" + version
	body, err := c.get(ctx, u, "text/plain")
	if err != nil {
		return HashInfo{}, fmt.Errorf("hash %s: %w", moduleAt, err)
	}
	// sum.golang.org lookup format:
	//   <tile-number>
	//   <module> <version> h1:<hash>=
	//   <module> <version>/go.mod h1:<hash>=
	//   <blank line>
	//   go.sum database tree ...
	//
	// We extract the h1: hashes from lines 2 and 3.
	lines := strings.Split(string(body), "\n")
	var h1, goModH1 string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// lines like: "module v1.2.3 h1:abc="
		// or:         "module v1.2.3/go.mod h1:abc="
		parts := strings.Fields(line)
		if len(parts) == 3 && strings.HasPrefix(parts[2], "h1:") {
			if strings.HasSuffix(parts[1], "/go.mod") {
				if goModH1 == "" {
					goModH1 = parts[2]
				}
			} else {
				if h1 == "" {
					h1 = parts[2]
				}
			}
		}
		if h1 != "" && goModH1 != "" {
			break
		}
	}
	return HashInfo{
		Module:    module,
		Version:   version,
		Hash:      h1,
		GoModHash: goModH1,
		URL:       c.cfg.SumBaseURL + "/lookup/" + module + "@" + version,
	}, nil
}

// ─── HTTP core ───────────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, rawURL, accept string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL, accept)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL, accept string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// ─── HTML search parser ───────────────────────────────────────────────────────

// parseSearch extracts Package records from a pkg.go.dev search result page.
//
// The site renders each result as:
//
//	<div class="SearchSnippet">
//	  <div class="SearchSnippet-headerContainer">
//	    <h2>
//	      <a href="/import/path" data-test-id="snippet-title">...</a>
//	    </h2>
//	  </div>
//	  <p class="SearchSnippet-synopsis">...</p>
//	  <div class="SearchSnippet-infoLabel">
//	    ...
//	    <span class="go-textSubtle">
//	      <strong>v1.2.3</strong> published on
//	      <span data-test-id="snippet-published"><strong>Jan 2, 2024</strong></span>
//	    </span>
//	  </div>
//	</div>
func parseSearch(body []byte, pkgBase string) ([]Package, error) {
	z := html.NewTokenizer(bytes.NewReader(body))

	var results []Package
	var (
		inSnippet         bool
		snippetDepth      int
		inSynopsis        bool
		inInfoDiv         bool
		inInfoAnchor      bool // inside <a> within the info div (imported-by link)
		inVersionStrong   bool // <strong> in info div but not inside <a> = version
		inPublishedSpan   bool // inside data-test-id="snippet-published"
		inPublishedStrong bool
		current           Package
	)
	rank := 1

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		switch tt {
		case html.StartTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)

			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[string(k)] = string(v)
			}

			cls := attrs["class"]
			testID := attrs["data-test-id"]

			// detect start of a SearchSnippet div
			if tag == "div" && cls == "SearchSnippet" {
				if !inSnippet {
					inSnippet = true
					snippetDepth = 1
					current = Package{}
				} else {
					snippetDepth++
				}
				continue
			}

			if !inSnippet {
				continue
			}

			if tag == "div" {
				snippetDepth++
				if strings.Contains(cls, "SearchSnippet-infoLabel") {
					inInfoDiv = true
				}
			}

			// import path: <a data-test-id="snippet-title" href="/...">
			if tag == "a" && testID == "snippet-title" {
				if href := attrs["href"]; href != "" {
					importPath := strings.TrimPrefix(href, "/")
					current.ImportPath = importPath
					current.URL = pkgURL(pkgBase, importPath)
				}
			}

			// track anchor tags inside the info div to skip the imported-by count
			if tag == "a" && inInfoDiv {
				inInfoAnchor = true
			}

			// synopsis paragraph
			if tag == "p" && strings.Contains(cls, "SearchSnippet-synopsis") {
				inSynopsis = true
			}

			// published date span
			if tag == "span" && testID == "snippet-published" {
				inPublishedSpan = true
			}

			// strong tags inside the info div: skip the one inside <a> (imported-by count)
			// the version is the first <strong> outside of <a> in the info div
			if tag == "strong" && inInfoDiv {
				if inPublishedSpan {
					inPublishedStrong = true
				} else if !inInfoAnchor && current.Version == "" {
					inVersionStrong = true
				}
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)

			if !inSnippet {
				continue
			}

			if tag == "div" {
				snippetDepth--
				if snippetDepth <= 0 {
					// leaving the SearchSnippet
					if current.ImportPath != "" {
						current.Rank = rank
						rank++
						results = append(results, current)
					}
					inSnippet = false
					snippetDepth = 0
					inSynopsis = false
					inInfoDiv = false
					inInfoAnchor = false
					inVersionStrong = false
					inPublishedSpan = false
					inPublishedStrong = false
				} else if inInfoDiv {
					// could be exiting the info div
					inInfoDiv = false
				}
			}
			if tag == "p" {
				inSynopsis = false
			}
			if tag == "a" && inInfoDiv {
				inInfoAnchor = false
			}
			if tag == "span" {
				if inPublishedSpan {
					inPublishedSpan = false
				}
			}
			if tag == "strong" {
				inVersionStrong = false
				inPublishedStrong = false
			}

		case html.TextToken:
			if !inSnippet {
				continue
			}
			text := strings.TrimSpace(string(z.Text()))
			if text == "" {
				continue
			}

			if inSynopsis {
				if current.Synopsis != "" {
					current.Synopsis += " "
				}
				current.Synopsis += text
			}
			if inVersionStrong {
				current.Version = text
				inVersionStrong = false
			}
			if inPublishedStrong {
				current.Published = text
				inPublishedStrong = false
			}
		}
	}

	return results, nil
}

// ─── semver helpers ───────────────────────────────────────────────────────────

// semverGT reports whether a > b using a simple lexicographic comparison that
// works for standard Go semver strings (v1.2.3, v1.10.0, etc.).
// We split on "." and compare numerically segment by segment.
func semverGT(a, b string) bool {
	// strip leading "v"
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	// strip pre-release suffix for comparison
	if i := strings.Index(a, "-"); i >= 0 {
		a = a[:i]
	}
	if i := strings.Index(b, "-"); i >= 0 {
		b = b[:i]
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	max := len(aParts)
	if len(bParts) > max {
		max = len(bParts)
	}
	for i := 0; i < max; i++ {
		ai := segInt(aParts, i)
		bi := segInt(bParts, i)
		if ai != bi {
			return ai > bi
		}
	}
	return false
}

func segInt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n := 0
	for _, c := range parts[i] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
