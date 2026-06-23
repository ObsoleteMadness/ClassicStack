//go:build macgarden || all

package macgarden

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) *Document {
	t.Helper()
	doc, err := NewDocumentFromReader(bytes.NewReader([]byte(s)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

// TestSelectorBasics exercises every selector feature the scraper relies on against
// hand-written markup with known structure.
func TestSelectorBasics(t *testing.T) {
	html := `<html><body>
	  <div id="paper">
	    <h1>Title Here</h1>
	    <p>First para</p>
	    <p>Second para</p>
	    <div class="box"><div><dl><dt class="title"><a href="/apps/foo">Foo</a></dt></dl></div></div>
	    <div class="game-preview">
	      <div class="images"><a class="thickbox" href="/files/shot.png">shot</a></div>
	      <div class="descr"><div class="note download">Mac OS<br><small>Disk 1 <i>(800 KB)</i></small><a href="/dl/a.sit">A</a><a href="/dl/b.sit">B</a></div></div>
	    </div>
	  </div>
	  <a href="/category/games">Cat</a>
	  <a href="/games/strategy">Strategy</a>
	  <a href="/apps/all">AppsAll</a>
	  <h2><a href="/apps/foo?page=1">Pager</a></h2>
	  <dd><span class="search-snippet">snip</span><span class="search-info">App - bob - 2020 Jan 2 - 3:04pm - 0 comments</span></dd>
	</body></html>`
	doc := parse(t, html)

	// child combinator + id
	if got := doc.Find("#paper > h1").Text(); got != "Title Here" {
		t.Errorf("#paper > h1 = %q", got)
	}
	// #paper > p (two paras, direct children only)
	if n := doc.Find("#paper > p").Length(); n != 2 {
		t.Errorf("#paper > p count = %d, want 2", n)
	}
	// deep descendant + compound class + tag
	sel := doc.Find("#paper > div.box > div > dl > dt.title a")
	if sel.Length() != 1 {
		t.Fatalf("title anchor count = %d, want 1", sel.Length())
	}
	if href, _ := sel.Attr("href"); href != "/apps/foo" {
		t.Errorf("title anchor href = %q", href)
	}
	// attribute substring
	if doc.Find("a[href*='/category/']").Length() != 1 {
		t.Errorf("category anchors = %d, want 1", doc.Find("a[href*='/category/']").Length())
	}
	// selector list with prefix matches
	if n := doc.Find("a[href^='/games/'], a[href^='/apps/']").Length(); n < 2 {
		t.Errorf("games/apps anchors = %d, want >=2", n)
	}
	// adjacent sibling: br + small
	if got := strings.TrimSpace(doc.Find("br + small").First().Contents().First().Text()); got != "Disk 1" {
		t.Errorf("br + small first content = %q, want 'Disk 1'", got)
	}
	// br + small > i
	if got := doc.Find("br + small > i").First().Text(); !strings.Contains(got, "800 KB") {
		t.Errorf("br + small > i = %q", got)
	}
	// compound class .note.download with two download anchors
	dl := doc.Find("#paper > div.game-preview > div.descr .note.download")
	if dl.Length() != 1 {
		t.Fatalf("note.download count = %d, want 1", dl.Length())
	}
	if dl.Find("a").Length() != 2 {
		t.Errorf("download anchors = %d, want 2", dl.Find("a").Length())
	}
	// thickbox screenshot
	if href, _ := doc.Find("#paper > div.game-preview > div.images a.thickbox").Attr("href"); href != "/files/shot.png" {
		t.Errorf("thickbox href = %q", href)
	}
	// search snippet/info
	if got := doc.Find("dd .search-snippet").First().Text(); got != "snip" {
		t.Errorf("snippet = %q", got)
	}
	if got := doc.Find("dd .search-info").First().Text(); !strings.HasPrefix(got, "App - bob") {
		t.Errorf("search-info = %q", got)
	}
	// h2 a[href] pager
	if href, _ := doc.Find("h2 a[href]").Attr("href"); href != "/apps/foo?page=1" {
		t.Errorf("h2 a href = %q", href)
	}
}

// TestSelectorAgainstFixture parses a real captured macintoshgarden.org category page
// and confirms the category-results + pagination selectors find sane content.
func TestSelectorAgainstFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "category_antivirus_page1.html"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	doc := parse(t, string(raw))

	// The category listing puts item links under h2 anchors.
	items := doc.Find("h2 a[href]")
	if items.Length() == 0 {
		t.Error("expected at least one h2 a[href] item on the captured category page")
	}
	// Every matched anchor must actually carry an href (Attr presence).
	items.Each(func(_ int, s *Selection) {
		if _, ok := s.Attr("href"); !ok {
			t.Error("h2 anchor matched but has no href")
		}
	})
	// Eq / First / Last sanity within document order.
	if items.Length() >= 2 {
		if items.First().Text() == items.Last().Text() {
			t.Log("first and last item text coincide (small page) — acceptable")
		}
	}
}
