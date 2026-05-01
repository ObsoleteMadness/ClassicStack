package macgarden

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/PuerkitoBio/goquery"
)

const (
	BaseURL            = "http://macintoshgarden.org"
	headRequestTimeout = 1000 * time.Millisecond

	clientUserAgent = "Mozilla/2.0 (Macintosh; I; 68K)"
	clientAccept    = "image/gif, image/x-xbitmap, image/jpeg, image/pjpeg, */*"
)

type Category struct {
	Name string
	URL  string
}

type SearchResult struct {
	Name       string
	URL        string
	Snippet    string
	Type       string
	UploadDate time.Time
}

type DownloadLink struct {
	Text string
	URL  string
}

type DownloadDetails struct {
	Title string
	Size  string
	OS    string
	Links []DownloadLink
}

type SoftwareItem struct {
	Title       string
	URL         string
	Description string
	Downloads   []DownloadDetails
	Screenshots []string
}

type CategoryPageInfo struct {
	FirstPage      []SearchResult
	LastPage       []SearchResult
	FirstPageCount int
	LastPageCount  int
	PageSize       int
	LastPageNumber int
	TotalCount     int
}

type headCacheEntry struct {
	size int64
	err  error
}

type Client struct {
	httpClient   *http.Client
	allowedHost  map[string]struct{}
	rateLimiter  <-chan time.Time
	cacheDir     string
	fetchHead    bool
	maxRangeSize int // 0 = unlimited; capped per ReadURLRange call
	headMu       sync.RWMutex
	headCache    map[string]headCacheEntry
	itemCacheMu  sync.RWMutex
	itemCache    map[string]cachedItemDetails
}

func (c *Client) SetFetchHead(v bool)   { c.fetchHead = v }
func (c *Client) FetchHead() bool       { return c.fetchHead }
func (c *Client) SetMaxRangeSize(n int) { c.maxRangeSize = n }
func (c *Client) MaxRangeSize() int     { return c.maxRangeSize }

type cachedItemDetails struct {
	FetchedAt    time.Time        `json:"fetched_at"`
	SoftwareItem *SoftwareItem    `json:"software_item,omitempty"`
	HeadResults  map[string]int64 `json:"head_results,omitempty"` // fileURL -> size
}

func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	ticker := time.NewTicker(1 * time.Second)
	c := &Client{
		rateLimiter: ticker.C,
		cacheDir:    "._htmlcache",
		headCache:   make(map[string]headCacheEntry),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Jar:     jar,
			// Copy our standard headers onto every redirected request so the
			// server sees a consistent client regardless of hop count.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				if len(via) > 0 {
					for key, vals := range via[0].Header {
						if _, ok := req.Header[key]; !ok {
							req.Header[key] = vals
						}
					}
				}
				return nil
			},
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		allowedHost: map[string]struct{}{
			"macintoshgarden.org":          {},
			"mirror.macintoshgarden.org":   {},
			"download.macintoshgarden.org": {},
			"old.mac.gdn":                  {},
		},
		itemCache: make(map[string]cachedItemDetails),
	}
	c.loadItemCache()
	return c
}

// Prime establishes a session cookie by fetching the site index. Production
// callers invoke this once after construction; tests skip it so mock
// transports aren't perturbed by an unsolicited GET.
func (c *Client) Prime() { c.primeSession() }

// primeSession fetches the site index so the server can set a session cookie.
// The cookie jar on httpClient stores it automatically; all subsequent requests
// (fetchDocument, ReadURLRange, FetchFull, rangeContentLength) send it back.
func (c *Client) primeSession() {
	netlog.Info("[MacGarden] establishing session: GET %s", BaseURL)
	req, err := http.NewRequest(http.MethodGet, BaseURL, nil)
	if err != nil {
		netlog.Warn("[MacGarden] session prime request error: %v", err)
		return
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req) // no rate-limit: one-time startup call
	if err != nil {
		netlog.Warn("[MacGarden] session prime failed: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	u, _ := url.Parse(BaseURL)
	netlog.Info("[MacGarden] session established, %d cookie(s) stored", len(c.httpClient.Jar.Cookies(u)))
}

// setHeaders stamps every outbound request with our standard browser identity.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", clientUserAgent)
	req.Header.Set("Accept", clientAccept)
	req.Header.Set("Referer", BaseURL+"/")
}

// throttledDo drains one rate-limiter token then executes the request.
// Every network call (except the startup session prime) must go through here.
func (c *Client) throttledDo(req *http.Request) (*http.Response, error) {
	<-c.rateLimiter
	return c.httpClient.Do(req)
}

// getCachedHead returns a previously stored size from the in-memory head cache.
func (c *Client) getCachedHead(fileURL string) (int64, bool) {
	c.headMu.RLock()
	defer c.headMu.RUnlock()
	if e, ok := c.headCache[fileURL]; ok {
		return e.size, true
	}
	return 0, false
}

// setCachedHead stores a size in the in-memory head cache.
func (c *Client) setCachedHead(fileURL string, size int64) {
	c.headMu.Lock()
	c.headCache[fileURL] = headCacheEntry{size: size}
	c.headMu.Unlock()
}

// lookupItemCacheHead checks the persistent item cache for a previously stored
// content-length, avoiding a network round-trip on repeated calls.
func (c *Client) lookupItemCacheHead(fileURL string) (int64, bool) {
	c.itemCacheMu.RLock()
	defer c.itemCacheMu.RUnlock()
	for _, v := range c.itemCache {
		if v.HeadResults != nil {
			if sz, ok := v.HeadResults[fileURL]; ok {
				return sz, true
			}
		}
	}
	return 0, false
}

// recordHeadResult persists a content-length in the item cache and flushes to
// disk. It tries to attach the size to an existing item entry; otherwise it
// creates a stand-alone entry keyed by the file URL.
func (c *Client) recordHeadResult(fileURL string, size int64) {
	c.itemCacheMu.Lock()
	found := false
	for k, v := range c.itemCache {
		if k == fileURL || (v.SoftwareItem != nil && containsDownloadURL(v.SoftwareItem, fileURL)) {
			if v.HeadResults == nil {
				v.HeadResults = make(map[string]int64)
			}
			v.HeadResults[fileURL] = size
			c.itemCache[k] = v
			found = true
			break
		}
	}
	if !found {
		c.itemCache[fileURL] = cachedItemDetails{
			FetchedAt:   time.Now(),
			HeadResults: map[string]int64{fileURL: size},
		}
	}
	c.itemCacheMu.Unlock()
	c.saveItemCache()
}

func (c *Client) itemCachePath() string {
	return filepath.Join("._itemcache", "itemcache.json")
}

func (c *Client) loadItemCache() {
	c.itemCacheMu.Lock()
	defer c.itemCacheMu.Unlock()
	cachePath := c.itemCachePath()
	body, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.itemCache = make(map[string]cachedItemDetails)
			return
		}
		return
	}
	tmp := make(map[string]cachedItemDetails)
	if err := json.Unmarshal(body, &tmp); err == nil {
		c.itemCache = tmp
	}
}

func (c *Client) saveItemCache() {
	c.itemCacheMu.RLock()
	defer c.itemCacheMu.RUnlock()
	cachePath := c.itemCachePath()
	cacheDir := filepath.Dir(cachePath)
	_ = os.MkdirAll(cacheDir, 0o755)
	body, err := json.MarshalIndent(c.itemCache, "", "  ")
	if err != nil {
		return
	}
	tmpPath := cachePath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, cachePath)
}

func (c *Client) GetCategories() ([]Category, error) {
	netlog.Info("[MacGarden] fetching categories from %s", BaseURL)
	doc, err := c.fetchDocument(BaseURL)
	if err != nil {
		netlog.Warn("[MacGarden] failed to fetch categories: %v", err)
		return nil, err
	}
	return c.parseCategoriesFromDocument(doc), nil
}

func (c *Client) parseCategoriesFromDocument(doc *goquery.Document) []Category {
	seen := map[string]struct{}{}
	result := make([]Category, 0, 64)
	addCategory := func(name string, href string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		u := c.normalizeURL(href)
		if u == "" {
			return
		}
		key := strings.ToLower(name) + "|" + u
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, Category{Name: name, URL: u})
	}

	// Legacy selector used by older Macintosh Garden markup.
	doc.Find("a[href*='/category/']").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		addCategory(s.Text(), href)
	})

	// Modern navigation includes taxonomy paths under /games and /apps.
	if len(result) == 0 {
		doc.Find("a[href^='/games/'], a[href^='/apps/']").Each(func(_ int, s *goquery.Selection) {
			href, ok := s.Attr("href")
			if !ok {
				return
			}
			href = strings.TrimSpace(href)
			if href == "/games/all" || href == "/apps/all" {
				return
			}
			name := strings.TrimSpace(s.Text())
			if name == "" {
				name = strings.Trim(strings.TrimPrefix(href, "/games/"), "/")
				if name == href {
					name = strings.Trim(strings.TrimPrefix(href, "/apps/"), "/")
				}
				name = strings.ReplaceAll(name, "-", " ")
			}
			addCategory(name, href)
		})
	}
	return result
}

func (c *Client) Search(query string, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	query = strings.TrimSpace(query)
	var searchURL string
	isDirectURL := false

	// If query looks like a URL (absolute or category path), fetch it directly
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") || strings.HasPrefix(query, "/apps/") || strings.HasPrefix(query, "/games/") {
		isDirectURL = true
		if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
			searchURL = query
		} else {
			searchURL = BaseURL + query
		}
	} else {
		// Regular search query
		searchURL = fmt.Sprintf("%s/search/node/%s", BaseURL, url.PathEscape(query+" type:app,game"))
	}

	netlog.Info("[MacGarden] searching URL: %s", searchURL)
	doc, err := c.fetchDocument(searchURL)
	if err != nil {
		netlog.Warn("[MacGarden] search failed: %v", err)
		return nil, err
	}
	if isDirectURL {
		return c.parseCategoryResults(searchURL, doc, limit)
	}

	searchBaseURL, err := url.Parse(searchURL)
	if err != nil {
		return c.parseSearchResults(doc, limit), nil
	}
	results := c.parseSearchResults(doc, 0)
	for _, pageURL := range c.categoryPaginationURLs(searchBaseURL.Path, doc) {
		if limit > 0 && len(results) >= limit {
			break
		}
		pageDoc, err := c.fetchDocument(pageURL)
		if err != nil {
			netlog.Warn("[MacGarden] search page fetch failed: %v", err)
			return nil, err
		}
		results = append(results, c.parseSearchResults(pageDoc, 0)...)
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (c *Client) parseSearchResults(doc *goquery.Document, limit int) []SearchResult {
	titleNodes := doc.Find("#paper > div.box > div > dl > dt.title a")
	snippetNodes := doc.Find("dd .search-snippet")
	infoNodes := doc.Find("dd .search-info")
	count := titleNodes.Length()
	if snippetNodes.Length() < count {
		count = snippetNodes.Length()
	}
	if limit > 0 && count > limit {
		count = limit
	}
	results := make([]SearchResult, 0, count)
	for i := 0; i < count; i++ {
		titleSel := titleNodes.Eq(i)
		snippetSel := snippetNodes.Eq(i)
		href, ok := titleSel.Attr("href")
		if !ok {
			continue
		}
		resultType := ""
		uploadDate := time.Time{}
		if i < infoNodes.Length() {
			resultType, uploadDate = parseSearchInfo(strings.TrimSpace(infoNodes.Eq(i).Text()))
		}
		results = append(results, SearchResult{
			Name:       strings.TrimSpace(titleSel.Text()),
			URL:        c.normalizeURL(href),
			Snippet:    strings.TrimSpace(snippetSel.Text()),
			Type:       resultType,
			UploadDate: uploadDate,
		})
	}
	return results
}

// parseSearchInfo parses "Type - User - Date - Time - N comments" from search-info.
// We currently care only about Type (App/Game) and upload timestamp.
func parseSearchInfo(info string) (string, time.Time) {
	parts := strings.Split(info, " - ")
	if len(parts) < 4 {
		return "", time.Time{}
	}
	resultType := strings.TrimSpace(parts[0])
	if resultType != "App" && resultType != "Game" {
		resultType = ""
	}

	datePart := strings.TrimSpace(parts[2])
	timePart := strings.ToLower(strings.TrimSpace(parts[3]))
	ts := strings.TrimSpace(datePart + " " + timePart)
	if ts == "" {
		return resultType, time.Time{}
	}
	for _, layout := range []string{"2006 Jan 2 3:04pm", "2006 Jan 2 03:04pm"} {
		if t, err := time.ParseInLocation(layout, ts, time.Local); err == nil {
			return resultType, t
		}
	}
	return resultType, time.Time{}
}

func (c *Client) parseCategoryResults(categoryURL string, doc *goquery.Document, limit int) ([]SearchResult, error) {
	baseURL, err := url.Parse(categoryURL)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	results := c.appendCategoryResults(nil, seen, baseURL.Path, doc)

	for _, pageURL := range c.categoryPaginationURLs(baseURL.Path, doc) {
		if limit > 0 && len(results) >= limit {
			break
		}
		pageDoc, err := c.fetchDocument(pageURL)
		if err != nil {
			netlog.Warn("[MacGarden] category page fetch failed: %v", err)
			return nil, err
		}
		results = c.appendCategoryResults(results, seen, baseURL.Path, pageDoc)
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (c *Client) GetCategoryPageInfo(categoryURL string) (CategoryPageInfo, error) {
	doc, err := c.fetchDocument(categoryURL)
	if err != nil {
		return CategoryPageInfo{}, err
	}
	baseURL, err := url.Parse(categoryURL)
	if err != nil {
		return CategoryPageInfo{}, err
	}
	categoryPath := baseURL.Path
	firstPage := c.appendCategoryResults(nil, map[string]struct{}{}, categoryPath, doc)
	firstPageCount := len(firstPage)
	pageURLs := c.categoryPaginationURLs(categoryPath, doc)
	if len(pageURLs) == 0 {
		return CategoryPageInfo{
			FirstPage:      firstPage,
			LastPage:       firstPage,
			FirstPageCount: firstPageCount,
			LastPageCount:  firstPageCount,
			PageSize:       firstPageCount,
			LastPageNumber: 0,
			TotalCount:     firstPageCount,
		}, nil
	}

	lastPageURL := pageURLs[len(pageURLs)-1]
	lastPageNumber := categoryPageNumber(lastPageURL)
	if lastPageNumber <= 0 {
		return CategoryPageInfo{
			FirstPage:      firstPage,
			LastPage:       firstPage,
			FirstPageCount: firstPageCount,
			LastPageCount:  firstPageCount,
			PageSize:       firstPageCount,
			LastPageNumber: 0,
			TotalCount:     firstPageCount,
		}, nil
	}

	lastDoc, err := c.fetchDocument(lastPageURL)
	if err != nil {
		return CategoryPageInfo{}, err
	}
	lastPage := c.appendCategoryResults(nil, map[string]struct{}{}, categoryPath, lastDoc)
	lastPageCount := len(lastPage)
	// Pagination is zero-based: the root category/search page is logical page 0,
	// so a last page query of ?page=1 means there are two pages total.
	pageCount := 1 + lastPageNumber
	return CategoryPageInfo{
		FirstPage:      firstPage,
		LastPage:       lastPage,
		FirstPageCount: firstPageCount,
		LastPageCount:  lastPageCount,
		PageSize:       firstPageCount,
		LastPageNumber: lastPageNumber,
		TotalCount:     firstPageCount*(pageCount-1) + lastPageCount,
	}, nil
}

func (c *Client) CountCategoryItems(categoryURL string) (int, error) {
	info, err := c.GetCategoryPageInfo(categoryURL)
	if err != nil {
		return 0, err
	}
	return info.TotalCount, nil
}

// GetSearchPage fetches a single page of text-search results for query.
// pageNumber 0 is the first (unparameterized) page; subsequent pages use ?page=N.
func (c *Client) GetSearchPage(query string, pageNumber int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	searchURL := fmt.Sprintf("%s/search/node/%s", BaseURL, url.PathEscape(query+" type:app,game"))
	if pageNumber > 0 {
		u, err := url.Parse(searchURL)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("page", strconv.Itoa(pageNumber))
		u.RawQuery = q.Encode()
		searchURL = u.String()
	}
	netlog.Info("[MacGarden] fetching search page %d: %s", pageNumber, searchURL)
	doc, err := c.fetchDocument(searchURL)
	if err != nil {
		return nil, err
	}
	return c.parseSearchResults(doc, 0), nil
}

func (c *Client) GetCategoryPage(categoryURL string, pageNumber int) ([]SearchResult, error) {
	pageURL, err := categoryPageURL(categoryURL, pageNumber)
	if err != nil {
		return nil, err
	}
	doc, err := c.fetchDocument(pageURL)
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(categoryURL)
	if err != nil {
		return nil, err
	}
	return c.appendCategoryResults(nil, map[string]struct{}{}, baseURL.Path, doc), nil
}

func (c *Client) appendCategoryResults(results []SearchResult, seen map[string]struct{}, categoryPath string, doc *goquery.Document) []SearchResult {
	doc.Find("h2 a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		normalized := c.normalizeURL(href)
		if normalized == "" {
			return
		}
		u, err := url.Parse(normalized)
		if err != nil {
			return
		}
		if u.Path == categoryPath || strings.Contains(u.RawQuery, "page=") {
			return
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		results = append(results, SearchResult{
			Name: strings.TrimSpace(s.Text()),
			URL:  normalized,
		})
	})
	return results
}

func (c *Client) countCategoryResultsOnPage(categoryPath string, doc *goquery.Document) int {
	count := 0
	doc.Find("h2 a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		normalized := c.normalizeURL(href)
		if normalized == "" {
			return
		}
		u, err := url.Parse(normalized)
		if err != nil {
			return
		}
		if u.Path == categoryPath || strings.Contains(u.RawQuery, "page=") {
			return
		}
		count++
	})
	return count
}

func (c *Client) categoryPaginationURLs(categoryPath string, doc *goquery.Document) []string {
	pages := map[string]struct{}{}
	urls := make([]string, 0, 4)
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		normalized := c.normalizeURL(href)
		if normalized == "" {
			return
		}
		u, err := url.Parse(normalized)
		if err != nil {
			return
		}
		if u.Path != categoryPath || !strings.Contains(u.RawQuery, "page=") {
			return
		}
		if _, exists := pages[normalized]; exists {
			return
		}
		pages[normalized] = struct{}{}
		urls = append(urls, normalized)
	})
	sort.Slice(urls, func(i, j int) bool {
		return categoryPageNumber(urls[i]) < categoryPageNumber(urls[j])
	})
	return urls
}

func categoryPageNumber(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	page := u.Query().Get("page")
	if page == "" {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(page, "%d", &n)
	return n
}

func categoryPageURL(categoryURL string, pageNumber int) (string, error) {
	u, err := url.Parse(categoryURL)
	if err != nil {
		return "", err
	}
	if pageNumber <= 0 {
		u.RawQuery = ""
		return u.String(), nil
	}
	query := u.Query()
	query.Set("page", fmt.Sprintf("%d", pageNumber))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) GetSoftwareItem(itemURL string) (*SoftwareItem, error) {
	c.itemCacheMu.RLock()
	ci, ok := c.itemCache[itemURL]
	c.itemCacheMu.RUnlock()
	if ok && ci.SoftwareItem != nil {
		netlog.Debug("[MacGarden] item cache hit: %s", itemURL)
		return ci.SoftwareItem, nil
	}
	netlog.Info("[MacGarden] fetching item: %s", itemURL)
	doc, err := c.fetchDocument(itemURL)
	if err != nil {
		netlog.Warn("[MacGarden] failed to fetch item: %v", err)
		return nil, err
	}
	netlog.Debug("[MacGarden] received page for item: %s", itemURL)
	item := &SoftwareItem{URL: itemURL}
	item.Title = strings.TrimSpace(doc.Find("#paper > h1").First().Text())
	if item.Title == "" {
		item.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	descParts := make([]string, 0, 8)
	doc.Find("#paper > p").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" {
			descParts = append(descParts, text)
		}
	})
	item.Description = strings.Join(descParts, "\n\n")
	doc.Find("#paper > div.game-preview > div.images a.thickbox").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		u := c.normalizeURL(href)
		if u != "" {
			item.Screenshots = append(item.Screenshots, u)
		}
	})
	doc.Find("#paper > div.game-preview > div.descr .note.download").Each(func(_ int, s *goquery.Selection) {
		firstAnchor := s.Find("a").First()
		if strings.EqualFold(strings.TrimSpace(firstAnchor.Text()), "Purchase") {
			return
		}
		details := DownloadDetails{}
		title := strings.TrimSpace(s.Find("br + small").First().Contents().First().Text())
		details.Title = title
		details.Size = strings.TrimSpace(strings.TrimPrefix(s.Find("br + small > i").First().Text(), "("))
		details.OS = strings.TrimSpace(s.Contents().Last().Text())
		s.Find("a").Each(func(_ int, a *goquery.Selection) {
			href, ok := a.Attr("href")
			if !ok {
				return
			}
			u := c.normalizeURL(href)
			if u == "" {
				return
			}
			details.Links = append(details.Links, DownloadLink{Text: strings.TrimSpace(a.Text()), URL: u})
		})
		if len(details.Links) > 0 {
			item.Downloads = append(item.Downloads, details)
		}
	})
	netlog.Info("[MacGarden] parsed item %q: %d screenshot(s), %d download group(s)", item.Title, len(item.Screenshots), len(item.Downloads))
	// Save to cache
	c.itemCacheMu.Lock()
	c.itemCache[itemURL] = cachedItemDetails{
		FetchedAt:    time.Now(),
		SoftwareItem: item,
	}
	c.itemCacheMu.Unlock()
	c.saveItemCache()
	return item, nil
}

func (c *Client) ReadURLRange(fileURL string, offset int64, length int) ([]byte, error) {
	if c.maxRangeSize > 0 && length > c.maxRangeSize {
		length = c.maxRangeSize
	}
	rng := ""
	if length > 0 {
		rng = fmt.Sprintf("bytes=%d-%d", offset, offset+int64(length)-1)
	}
	netlog.Info("[MacGarden] reading URL: %s range=%s", fileURL, rng)
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	if length > 0 {
		req.Header.Set("Range", rng)
	}
	c.setHeaders(req)
	resp, err := c.throttledDo(req)
	if err != nil {
		netlog.Warn("[MacGarden] failed to read URL: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// CachedContentLength returns a previously stored size without any network I/O.
func (c *Client) CachedContentLength(fileURL string) (int64, bool) {
	if sz, ok := c.lookupItemCacheHead(fileURL); ok {
		return sz, true
	}
	return c.getCachedHead(fileURL)
}

// FetchFull downloads the complete content of fileURL and returns the bytes.
func (c *Client) FetchFull(fileURL string) ([]byte, error) {
	netlog.Info("[MacGarden] full fetch: %s", fileURL)
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.throttledDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// GetContentLength returns the file size via a ranged GET, using both caches
// so repeated calls are free. Called during FPGetFileDirParms.
func (c *Client) GetContentLength(fileURL string) (int64, error) {
	if sz, ok := c.getCachedHead(fileURL); ok {
		return sz, nil
	}
	if sz, ok := c.lookupItemCacheHead(fileURL); ok {
		return sz, nil
	}
	size, err := c.rangeContentLength(fileURL)
	c.setCachedHead(fileURL, size)
	return size, err
}

func (c *Client) HeadContentLength(fileURL string) (int64, error) {
	if !c.fetchHead {
		return 0, nil
	}
	if sz, ok := c.lookupItemCacheHead(fileURL); ok {
		return sz, nil
	}
	if sz, ok := c.getCachedHead(fileURL); ok {
		return sz, nil
	}
	u, err := url.Parse(fileURL)
	if err != nil {
		c.setCachedHead(fileURL, 0)
		return 0, err
	}
	if _, ok := c.allowedHost[strings.ToLower(u.Host)]; !ok {
		c.setCachedHead(fileURL, 0)
		return 0, nil
	}
	// download.macintoshgarden.org often rejects HEAD; use a ranged GET instead.
	if strings.EqualFold(u.Host, "download.macintoshgarden.org") {
		size, err := c.rangeContentLength(fileURL)
		c.setCachedHead(fileURL, size)
		c.recordHeadResult(fileURL, size)
		return size, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), headRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fileURL, nil)
	if err != nil {
		c.setCachedHead(fileURL, 0)
		return 0, err
	}
	c.setHeaders(req)
	netlog.Info("[MacGarden] HEAD request: %s", fileURL)
	resp, err := c.throttledDo(req)
	if err != nil {
		netlog.Warn("[MacGarden] HEAD request failed: %v", err)
		c.setCachedHead(fileURL, 0)
		return 0, err
	}
	defer resp.Body.Close()
	if resp.ContentLength >= 0 {
		c.setCachedHead(fileURL, resp.ContentLength)
		c.recordHeadResult(fileURL, resp.ContentLength)
		return resp.ContentLength, nil
	}
	// Some hosts omit Content-Length on HEAD; fall back to a ranged GET.
	size, rerr := c.rangeContentLength(fileURL)
	if rerr == nil {
		c.setCachedHead(fileURL, size)
		c.recordHeadResult(fileURL, size)
		return size, nil
	}
	c.setCachedHead(fileURL, 0)
	return 0, nil
}

func containsDownloadURL(item *SoftwareItem, fileURL string) bool {
	if item == nil {
		return false
	}
	for _, d := range item.Downloads {
		for _, l := range d.Links {
			if l.URL == fileURL {
				return true
			}
		}
	}
	return false
}

func (c *Client) rangeContentLength(fileURL string) (int64, error) {
	netlog.Info("[MacGarden] ranged-size probe: %s", fileURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	c.setHeaders(req)
	resp, err := c.throttledDo(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if cr := strings.TrimSpace(resp.Header.Get("Content-Range")); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash >= 0 && slash+1 < len(cr) {
			total := strings.TrimSpace(cr[slash+1:])
			if total != "*" {
				if n, perr := strconv.ParseInt(total, 10, 64); perr == nil && n >= 0 {
					return n, nil
				}
			}
		}
	}
	if resp.ContentLength >= 0 {
		return resp.ContentLength, nil
	}
	return 0, fmt.Errorf("no size headers")
}

func (c *Client) fetchDocument(urlStr string) (*goquery.Document, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if _, ok := c.allowedHost[strings.ToLower(u.Host)]; !ok {
		return nil, fmt.Errorf("host not allowed: %s", u.Host)
	}

	if doc, ok, err := c.readDocumentFromCache(urlStr); err == nil && ok {
		netlog.Debug("[MacGarden] cache hit: %s", urlStr)
		return doc, nil
	} else if err != nil {
		netlog.Warn("[MacGarden] cache read failed for %s: %v", urlStr, err)
	}

	netlog.Debug("[MacGarden] fetching document: %s", urlStr)
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.throttledDo(req)
	if err != nil {
		netlog.Warn("[MacGarden] HTTP request failed (%s): %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := c.writeDocumentToCache(urlStr, body); err != nil {
		netlog.Warn("[MacGarden] cache write failed for %s: %v", urlStr, err)
	}
	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

func (c *Client) readDocumentFromCache(urlStr string) (*goquery.Document, bool, error) {
	cachePath := c.cachePathForURL(urlStr)
	body, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		_ = os.Remove(cachePath)
		return nil, false, err
	}
	return doc, true, nil
}

func (c *Client) writeDocumentToCache(urlStr string, body []byte) error {
	cachePath := c.cachePathForURL(urlStr)
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmpPath := cachePath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(cachePath)
		if retryErr := os.Rename(tmpPath, cachePath); retryErr != nil {
			_ = os.Remove(tmpPath)
			return retryErr
		}
	}
	return nil
}

func (c *Client) cachePathForURL(urlStr string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(urlStr)))
	file := hex.EncodeToString(sum[:]) + ".html"
	cacheDir := c.cacheDir
	if strings.TrimSpace(cacheDir) == "" {
		cacheDir = "._htmlcache"
	}
	return filepath.Join(cacheDir, file)
}

func (c *Client) normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() {
		// Protocol-relative URL (e.g. //old.mac.gdn/path) — supply https scheme.
		if strings.HasPrefix(raw, "//") {
			u, err = url.Parse("http:" + raw)
		} else {
			u, err = url.Parse(BaseURL + "/" + strings.TrimLeft(raw, "/"))
		}
		if err != nil {
			return ""
		}
	}
	if _, ok := c.allowedHost[strings.ToLower(u.Host)]; !ok {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func FileNameFromURL(fileURL string, fallback string) string {
	u, err := url.Parse(fileURL)
	if err != nil {
		return fallback
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return fallback
	}
	return base
}
