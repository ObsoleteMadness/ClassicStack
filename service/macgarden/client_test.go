package macgarden

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// requireLiveTests skips tests that reach the public Macintosh Garden site
// unless CLASSICSTACK_LIVE_TESTS=1 is set. CI runners do not run these.
func requireLiveTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CLASSICSTACK_LIVE_TESTS") != "1" {
		t.Skip("skipping live macintoshgarden.org test; set CLASSICSTACK_LIVE_TESTS=1 to enable")
	}
}

// loadCapturedPage reads a captured macintoshgarden.org HTML page from
// testdata and rewrites its root-relative hrefs ("/apps/...", pager links) to
// absolute URLs under serverURL, so a test http server can serve the real
// markup while the client's allowedHost check (keyed on the server host) still
// passes. Using captured HTML keeps these tests faithful to the live site's
// structure rather than hand-written fixtures that can drift from how the
// goquery/x-net HTML parser actually treats the page.
func loadCapturedPage(t *testing.T, name, serverURL string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read captured page %s: %v", name, err)
	}
	// Rewrite href="/path" -> href="<serverURL>/path". The captured pages use
	// root-relative links throughout (items and pager), so a single prefix
	// rewrite reroutes every link to the test server.
	return strings.ReplaceAll(string(raw), `href="/`, `href="`+serverURL+`/`)
}

type headErrorRoundTripper struct {
	hits int
}

func (rt *headErrorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodHead {
		rt.hits++
		return nil, errors.New("head failed")
	}
	return nil, errors.New("unexpected method")
}

type probeRoundTripper struct {
	headHits  int
	getHits   int
	rangeSeen string
	mode      string
}

func (rt *probeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.Method {
	case http.MethodHead:
		rt.headHits++
		if rt.mode == "head-no-length" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), ContentLength: -1}, nil
		}
		return nil, errors.New("unexpected HEAD")
	case http.MethodGet:
		rt.getHits++
		rt.rangeSeen = req.Header.Get("Range")
		if rt.rangeSeen != "bytes=0-0" {
			return nil, errors.New("missing range header")
		}
		resp := &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("x")), Header: make(http.Header), ContentLength: 1}
		resp.Header.Set("Content-Range", "bytes 0-0/12345")
		return resp, nil
	default:
		return nil, errors.New("unexpected method")
	}
}

func readyRateLimiter() <-chan time.Time {
	ch := make(chan time.Time, 32)
	for i := 0; i < cap(ch); i++ {
		ch <- time.Now()
	}
	return ch
}

func TestParseCategoriesFromDocument_ModernNavFallback(t *testing.T) {
	html := `
	<html><body>
	<a href="/games/all">Games</a>
	<a href="/apps/all">Apps</a>
	<a href="/games/strategy">Strategy</a>
	<a href="/apps/utilities/compression-archiving">Compression &amp; Archiving</a>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("NewDocumentFromReader: %v", err)
	}

	c := NewClient()
	c.rateLimiter = readyRateLimiter()
	cats := c.parseCategoriesFromDocument(doc)
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories from fallback parse, got %d", len(cats))
	}
	if cats[0].URL == "" || cats[1].URL == "" {
		t.Fatal("expected normalized URLs for parsed categories")
	}
}

func TestParseSearchResults_ExtractsTypeAndUploadDate(t *testing.T) {
	html := `
	<html><body>
	<div id="paper"><div class="box"><div><dl>
	<dt class="title"><a href="https://macintoshgarden.org/apps/clarisworks-40">ClarisWorks 4.0</a></dt>
	<dd>
	  <p class="search-snippet">Snippet text</p>
	  <p class="search-info">App - MikeTomTom - 2025 Jul 24 - 5:53pm - 8 comments</p>
	</dd>
	</dl></div></div></div>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("NewDocumentFromReader: %v", err)
	}

	c := NewClient()
	c.rateLimiter = readyRateLimiter()
	results := c.parseSearchResults(doc, 0)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Type != "App" {
		t.Fatalf("Type = %q, want App", results[0].Type)
	}
	if results[0].UploadDate.IsZero() {
		t.Fatal("UploadDate is zero, want parsed timestamp")
	}
	if got := results[0].UploadDate.Format("2006-01-02 15:04"); got != "2025-07-24 17:53" {
		t.Fatalf("UploadDate = %q, want %q", got, "2025-07-24 17:53")
	}
}

func TestParseCategoryResults_FromCategoryPage(t *testing.T) {
	requireLiveTests(t)
	html := `
	<html><body>
	<h2><a href="/apps/anti-virus-boot-disk">Anti-Virus Boot Disk</a></h2>
	<h2><a href="/apps/clamav-upgrade-leopard-server">ClamAV upgrade for Leopard Server</a></h2>
	<h2><a href="/apps/utilities/antivirus">Antivirus</a></h2>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("NewDocumentFromReader: %v", err)
	}

	c := NewClient()
	c.rateLimiter = readyRateLimiter()
	results, err := c.parseCategoryResults("https://macintoshgarden.org/apps/utilities/antivirus", doc, 0)
	if err != nil {
		t.Fatalf("parseCategoryResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 item results, got %d", len(results))
	}
	if results[0].Name != "Anti-Virus Boot Disk" {
		t.Fatalf("first result name = %q", results[0].Name)
	}
	if results[1].URL != "https://macintoshgarden.org/apps/clamav-upgrade-leopard-server" {
		t.Fatalf("second result URL = %q", results[1].URL)
	}
}

func TestParseCategoryResults_FollowsPagination(t *testing.T) {
	pages := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pages[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	pages["/apps/utilities/antivirus"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/anti-virus-boot-disk">Anti-Virus Boot Disk</a></h2>
		<a href="%s/apps/utilities/antivirus?page=1">1</a>
		<a href="%s/apps/utilities/antivirus?page=2">2</a>
		</body></html>`, server.URL, server.URL, server.URL)
	pages["/apps/utilities/antivirus?page=1"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/clamav-upgrade-leopard-server">ClamAV upgrade for Leopard Server</a></h2>
		</body></html>`, server.URL)
	pages["/apps/utilities/antivirus?page=2"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/secureinit">SecureInit</a></h2>
		</body></html>`, server.URL)

	c := NewClient()
	c.httpClient = server.Client()
	c.rateLimiter = readyRateLimiter()
	host := strings.TrimPrefix(server.URL, "https://")
	c.allowedHost = map[string]struct{}{host: struct{}{}}

	doc, err := c.fetchDocument(server.URL + "/apps/utilities/antivirus")
	if err != nil {
		t.Fatalf("fetchDocument: %v", err)
	}
	results, err := c.parseCategoryResults(server.URL+"/apps/utilities/antivirus", doc, 0)
	if err != nil {
		t.Fatalf("parseCategoryResults: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 paginated results, got %d", len(results))
	}
	if results[2].Name != "SecureInit" {
		t.Fatalf("last result name = %q", results[2].Name)
	}
}

func TestCountCategoryItems_UsesFirstAndLastPages(t *testing.T) {
	pages := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pages[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	// Real captured pages: the antivirus category has 6 pages (last is
	// ?page=5), 10 items on page 1 and 8 on the last page.
	pages["/apps/utilities/antivirus"] = loadCapturedPage(t, "category_antivirus_page1.html", server.URL)
	pages["/apps/utilities/antivirus?page=5"] = loadCapturedPage(t, "category_antivirus_page5.html", server.URL)

	c := NewClient()
	c.httpClient = server.Client()
	c.rateLimiter = readyRateLimiter()
	host := strings.TrimPrefix(server.URL, "https://")
	c.allowedHost = map[string]struct{}{host: struct{}{}}

	count, err := c.CountCategoryItems(server.URL + "/apps/utilities/antivirus")
	if err != nil {
		t.Fatalf("CountCategoryItems: %v", err)
	}
	// firstPageCount*(pageCount-1) + lastPageCount = 10*5 + 8.
	if count != 58 {
		t.Fatalf("count = %d, want 58", count)
	}
}

func TestGetCategoryPageInfo_UsesFirstAndLastPages(t *testing.T) {
	pages := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pages[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	// Real captured antivirus category pages (first page + last page ?page=5).
	pages["/apps/utilities/antivirus"] = loadCapturedPage(t, "category_antivirus_page1.html", server.URL)
	pages["/apps/utilities/antivirus?page=5"] = loadCapturedPage(t, "category_antivirus_page5.html", server.URL)

	c := NewClient()
	c.httpClient = server.Client()
	c.rateLimiter = readyRateLimiter()
	host := strings.TrimPrefix(server.URL, "https://")
	c.allowedHost = map[string]struct{}{host: struct{}{}}

	info, err := c.GetCategoryPageInfo(server.URL + "/apps/utilities/antivirus")
	if err != nil {
		t.Fatalf("GetCategoryPageInfo: %v", err)
	}
	// Page 1 lists 10 items; the last page (?page=5) lists 8. Pagination is
	// zero-based, so a last query of page=5 means 6 pages: 10*5 + 8 = 58.
	if info.TotalCount != 58 {
		t.Fatalf("TotalCount = %d, want 58", info.TotalCount)
	}
	if info.FirstPageCount != 10 {
		t.Fatalf("FirstPageCount = %d, want 10", info.FirstPageCount)
	}
	if info.LastPageNumber != 5 {
		t.Fatalf("LastPageNumber = %d, want 5", info.LastPageNumber)
	}
	if len(info.LastPage) != 8 || info.LastPage[len(info.LastPage)-1].Name != "VirusDetective" {
		t.Fatalf("LastPage = %+v, want 8 items ending in VirusDetective", info.LastPage)
	}
	if info.PageSize != 10 {
		t.Fatalf("PageSize = %d, want 10", info.PageSize)
	}
}

func TestGetCategoryPageInfo_PageOneMeansSecondPage(t *testing.T) {
	pages := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pages[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	pages["/apps/utilities/antivirus"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/anti-virus-boot-disk">Anti-Virus Boot Disk</a></h2>
		<h2><a href="%s/apps/clamav-upgrade-leopard-server">ClamAV upgrade for Leopard Server</a></h2>
		<a href="%s/apps/utilities/antivirus?page=1">2</a>
		<a href="%s/apps/utilities/antivirus?page=1">last »</a>
		</body></html>`, server.URL, server.URL, server.URL, server.URL)
	pages["/apps/utilities/antivirus?page=1"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/secureinit">SecureInit</a></h2>
		</body></html>`, server.URL)

	c := NewClient()
	c.httpClient = server.Client()
	c.rateLimiter = readyRateLimiter()
	host := strings.TrimPrefix(server.URL, "https://")
	c.allowedHost = map[string]struct{}{host: {}}

	info, err := c.GetCategoryPageInfo(server.URL + "/apps/utilities/antivirus")
	if err != nil {
		t.Fatalf("GetCategoryPageInfo: %v", err)
	}
	if info.LastPageNumber != 1 {
		t.Fatalf("LastPageNumber = %d, want 1", info.LastPageNumber)
	}
	if info.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3", info.TotalCount)
	}
}

func TestGetCategoryPage_ReturnsSpecificPage(t *testing.T) {
	pages := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pages[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	pages["/apps/utilities/antivirus?page=1"] = fmt.Sprintf(`
		<html><body>
		<h2><a href="%s/apps/clamav-upgrade-leopard-server">ClamAV upgrade for Leopard Server</a></h2>
		<h2><a href="%s/apps/disinfectant">Disinfectant</a></h2>
		</body></html>`, server.URL, server.URL)

	c := NewClient()
	c.httpClient = server.Client()
	c.rateLimiter = readyRateLimiter()
	host := strings.TrimPrefix(server.URL, "https://")
	c.allowedHost = map[string]struct{}{host: struct{}{}}

	results, err := c.GetCategoryPage(server.URL+"/apps/utilities/antivirus", 1)
	if err != nil {
		t.Fatalf("GetCategoryPage: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Name != "ClamAV upgrade for Leopard Server" {
		t.Fatalf("first result = %q", results[0].Name)
	}
	if results[1].URL != server.URL+"/apps/disinfectant" {
		t.Fatalf("second result URL = %q", results[1].URL)
	}
}

func TestFetchDocument_UsesDiskCacheAcrossClients(t *testing.T) {
	var mu sync.Mutex
	hitCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/utilities/antivirus" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hitCount++
		mu.Unlock()
		_, _ = fmt.Fprint(w, `<html><body><h2><a href="/apps/anti-virus-boot-disk">Anti-Virus Boot Disk</a></h2></body></html>`)
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	cacheDir := filepath.Join(t.TempDir(), "._htmlcache")
	url := server.URL + "/apps/utilities/antivirus"

	c1 := NewClient()
	c1.httpClient = server.Client()
	c1.rateLimiter = readyRateLimiter()
	c1.allowedHost = map[string]struct{}{host: {}}
	c1.cacheDir = cacheDir

	if _, err := c1.fetchDocument(url); err != nil {
		t.Fatalf("first fetchDocument: %v", err)
	}

	c2 := NewClient()
	c2.httpClient = server.Client()
	c2.rateLimiter = readyRateLimiter()
	c2.allowedHost = map[string]struct{}{host: {}}
	c2.cacheDir = cacheDir

	if _, err := c2.fetchDocument(url); err != nil {
		t.Fatalf("second fetchDocument: %v", err)
	}

	mu.Lock()
	gotHits := hitCount
	mu.Unlock()
	if gotHits != 1 {
		t.Fatalf("network hit count = %d, want 1", gotHits)
	}
}

func TestHeadContentLength_FailureIsCached_NoRetry(t *testing.T) {
	requireLiveTests(t)
	rt := &headErrorRoundTripper{}
	c := NewClient()
	c.httpClient = &http.Client{Transport: rt}
	c.rateLimiter = readyRateLimiter()
	c.allowedHost = map[string]struct{}{"macintoshgarden.org": {}}

	_, err1 := c.HeadContentLength("https://macintoshgarden.org/files/fail.sit")
	if err1 == nil {
		t.Fatal("first HeadContentLength error = nil, want non-nil")
	}
	_, err2 := c.HeadContentLength("https://macintoshgarden.org/files/fail.sit")
	if err2 == nil {
		t.Fatal("second HeadContentLength error = nil, want cached non-nil")
	}
	if rt.hits != 1 {
		t.Fatalf("HEAD hits = %d, want 1 (no retry)", rt.hits)
	}
}

func TestHeadContentLength_DownloadHost_UsesRangedProbe(t *testing.T) {
	requireLiveTests(t)
	rt := &probeRoundTripper{}
	c := NewClient()
	c.httpClient = &http.Client{Transport: rt}
	c.rateLimiter = readyRateLimiter()
	c.allowedHost = map[string]struct{}{"download.macintoshgarden.org": {}}

	size, err := c.HeadContentLength("https://download.macintoshgarden.org/files/demo.sit")
	if err != nil {
		t.Fatalf("HeadContentLength error: %v", err)
	}
	if size != 12345 {
		t.Fatalf("size = %d, want 12345", size)
	}
	if rt.headHits != 0 {
		t.Fatalf("HEAD hits = %d, want 0", rt.headHits)
	}
	if rt.getHits != 1 {
		t.Fatalf("GET hits = %d, want 1", rt.getHits)
	}
}

func TestHeadContentLength_FallbackToRangedProbe_WhenHeadHasNoLength(t *testing.T) {
	requireLiveTests(t)
	rt := &probeRoundTripper{mode: "head-no-length"}
	c := NewClient()
	c.httpClient = &http.Client{Transport: rt}
	c.rateLimiter = readyRateLimiter()
	c.allowedHost = map[string]struct{}{"macintoshgarden.org": {}}

	size, err := c.HeadContentLength("https://macintoshgarden.org/files/demo.sit")
	if err != nil {
		t.Fatalf("HeadContentLength error: %v", err)
	}
	if size != 12345 {
		t.Fatalf("size = %d, want 12345", size)
	}
	if rt.headHits != 1 {
		t.Fatalf("HEAD hits = %d, want 1", rt.headHits)
	}
	if rt.getHits != 1 {
		t.Fatalf("GET hits = %d, want 1", rt.getHits)
	}
}
