package pkggodev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(pkgURL, proxyURL, sumURL string) *Client {
	cfg := DefaultConfig()
	cfg.Rate = 0
	cfg.Retries = 0
	cfg.PkgBaseURL = pkgURL
	cfg.ProxyBaseURL = proxyURL
	cfg.SumBaseURL = sumURL
	return NewClient(cfg)
}

func TestGetSendsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Rate = 0
	c := NewClient(cfg)
	body, err := c.get(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Rate = 0
	cfg.Retries = 5
	c := NewClient(cfg)

	start := time.Now()
	body, err := c.get(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetNon200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Rate = 0
	c := NewClient(cfg)
	_, err := c.get(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestSearchParsesResults(t *testing.T) {
	// mirrors the actual pkg.go.dev HTML structure
	const page = `<!DOCTYPE html>
<html><body>
<div class="SearchSnippet">
  <div class="SearchSnippet-headerContainer">
    <h2>
      <a href="/github.com/example/alpha" data-test-id="snippet-title">alpha</a>
    </h2>
  </div>
  <p class="SearchSnippet-synopsis">Alpha package does things.</p>
  <div class="SearchSnippet-infoLabel">
    <span class="go-textSubtle">
      <strong>v1.2.3</strong> published on
      <span data-test-id="snippet-published"><strong>Jan 2, 2024</strong></span>
    </span>
  </div>
</div>
<div class="SearchSnippet">
  <div class="SearchSnippet-headerContainer">
    <h2>
      <a href="/github.com/example/beta" data-test-id="snippet-title">beta</a>
    </h2>
  </div>
  <p class="SearchSnippet-synopsis">Beta package does other things.</p>
  <div class="SearchSnippet-infoLabel">
    <span class="go-textSubtle">
      <strong>v0.5.0</strong> published on
      <span data-test-id="snippet-published"><strong>Mar 10, 2023</strong></span>
    </span>
  </div>
</div>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	c := testClient(srv.URL, srv.URL, srv.URL)
	pkgs, err := c.Search(context.Background(), "example", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}
	if pkgs[0].ImportPath != "github.com/example/alpha" {
		t.Errorf("pkg[0].ImportPath = %q", pkgs[0].ImportPath)
	}
	if pkgs[0].Synopsis != "Alpha package does things." {
		t.Errorf("pkg[0].Synopsis = %q", pkgs[0].Synopsis)
	}
	if pkgs[0].Version != "v1.2.3" {
		t.Errorf("pkg[0].Version = %q", pkgs[0].Version)
	}
	if pkgs[0].Published != "Jan 2, 2024" {
		t.Errorf("pkg[0].Published = %q", pkgs[0].Published)
	}
	if pkgs[1].ImportPath != "github.com/example/beta" {
		t.Errorf("pkg[1].ImportPath = %q", pkgs[1].ImportPath)
	}
	if pkgs[0].Rank != 1 {
		t.Errorf("pkg[0].Rank = %d, want 1", pkgs[0].Rank)
	}
	if pkgs[1].Rank != 2 {
		t.Errorf("pkg[1].Rank = %d, want 2", pkgs[1].Rank)
	}
}

func TestSearchEmptyPage(t *testing.T) {
	const page = `<!DOCTYPE html><html><body><p>No results.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	c := testClient(srv.URL, srv.URL, srv.URL)
	pkgs, err := c.Search(context.Background(), "zzznoresults", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Errorf("got %d packages, want 0", len(pkgs))
	}
}

func TestVersionsParseslist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v1.0.0\nv1.2.0\nv1.10.0\nv0.5.0\n"))
	}))
	defer srv.Close()

	c := testClient(srv.URL, srv.URL, srv.URL)
	versions, err := c.Versions(context.Background(), "github.com/example/mod", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 4 {
		t.Fatalf("got %d versions, want 4", len(versions))
	}
	// sorted newest first: v1.10.0 > v1.2.0 > v1.0.0 > v0.5.0
	if versions[0].Version != "v1.10.0" {
		t.Errorf("versions[0] = %q, want v1.10.0", versions[0].Version)
	}
	if versions[1].Version != "v1.2.0" {
		t.Errorf("versions[1] = %q, want v1.2.0", versions[1].Version)
	}
	if versions[0].Rank != 1 {
		t.Errorf("versions[0].Rank = %d, want 1", versions[0].Rank)
	}
}

func TestLatestParsesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"v1.2.3","Time":"2024-01-15T00:00:00Z"}`))
	}))
	defer srv.Close()

	c := testClient(srv.URL, srv.URL, srv.URL)
	info, err := c.Latest(context.Background(), "github.com/example/mod")
	if err != nil {
		t.Fatal(err)
	}
	if info.Module != "github.com/example/mod" {
		t.Errorf("Module = %q", info.Module)
	}
	if info.Version != "v1.2.3" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.Time != "2024-01-15T00:00:00Z" {
		t.Errorf("Time = %q", info.Time)
	}
	if info.URL == "" {
		t.Error("URL is empty")
	}
}

func TestHashParsesResponse(t *testing.T) {
	// matches the actual sum.golang.org lookup format
	const body = "20487779\ngithub.com/example/mod v1.2.3 h1:abc123hashXYZabc123hashXYZabc123hashXYZabc1=\ngithub.com/example/mod v1.2.3/go.mod h1:def456gomodHashdef456gomodHashdef456gomod==\n\ngo.sum database tree\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := testClient(srv.URL, srv.URL, srv.URL)
	info, err := c.Hash(context.Background(), "github.com/example/mod@v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if info.Module != "github.com/example/mod" {
		t.Errorf("Module = %q", info.Module)
	}
	if info.Version != "v1.2.3" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.Hash != "h1:abc123hashXYZabc123hashXYZabc123hashXYZabc1=" {
		t.Errorf("Hash = %q", info.Hash)
	}
	if info.GoModHash != "h1:def456gomodHashdef456gomodHashdef456gomod==" {
		t.Errorf("GoModHash = %q", info.GoModHash)
	}
}

func TestHashRequiresAt(t *testing.T) {
	c := testClient("http://x", "http://x", "http://x")
	_, err := c.Hash(context.Background(), "github.com/example/mod")
	if err == nil {
		t.Fatal("expected error for missing @version")
	}
}
