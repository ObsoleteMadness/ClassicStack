//go:build (afp && macgarden) || all

package macgarden

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/service/afp"
	garden "github.com/ObsoleteMadness/ClassicStack/service/macgarden"
)

func TestMacGardenChildCount_CategoryIsLazyUntilCached(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	fsys := &MacGardenFileSystem{
		root:              root,
		categories:        []garden.Category{{Name: "Antivirus", URL: "https://macintoshgarden.org/apps/utilities/antivirus"}},
		categoryItemCount: make(map[string]uint16),
		categoryPageMeta:  make(map[string]macGardenCategoryPageMeta),
		categoryPageItems: make(map[string]map[int][]garden.SearchResult),
	}

	count, err := fsys.ChildCount(filepath.Join(root, "Apps", "Antivirus"))
	if err != nil {
		t.Fatalf("ChildCount returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("uncached category count = %d, want 0", count)
	}

	fsys.categoryItemCount["https://macintoshgarden.org/apps/utilities/antivirus"] = 7
	count, err = fsys.ChildCount(filepath.Join(root, "Apps", "Antivirus"))
	if err != nil {
		t.Fatalf("ChildCount cached returned error: %v", err)
	}
	if count != 7 {
		t.Fatalf("cached category count = %d, want 7", count)
	}
}

func TestMacGardenReadDirRange_UsesCachedFirstAndLastPages(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	catURL := "https://macintoshgarden.org/apps/utilities/antivirus"
	fsys := &MacGardenFileSystem{
		root:              root,
		categories:        []garden.Category{{Name: "Antivirus", URL: catURL}},
		categoryItemCount: make(map[string]uint16),
		categoryPageMeta: map[string]macGardenCategoryPageMeta{
			catURL: {TotalCount: 5, PageSize: 2, LastPageNumber: 2, LastPageCount: 1},
		},
		categoryPageItems: map[string]map[int][]garden.SearchResult{
			catURL: {
				0: {
					{Name: "Anti-Virus Boot Disk", URL: "https://macintoshgarden.org/apps/anti-virus-boot-disk"},
					{Name: "ClamAV upgrade for Leopard Server", URL: "https://macintoshgarden.org/apps/clamav-upgrade-leopard-server"},
				},
				2: {
					{Name: "SecureInit", URL: "https://macintoshgarden.org/apps/secureinit"},
				},
			},
		},
		itemURLByDir: make(map[string]string),
	}
	fsys.cacheCategoryPageLocked(catURL, 0, fsys.categoryPageItems[catURL][0])
	fsys.cacheCategoryPageLocked(catURL, 2, fsys.categoryPageItems[catURL][2])

	entries, total, err := fsys.ReadDirRange(filepath.Join(root, "Apps", "Antivirus"), 1, 2)
	if err != nil {
		t.Fatalf("ReadDirRange first page: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(entries) != 2 || entries[0].Name() != "Anti-Virus Boot Disk" || entries[1].Name() != "ClamAV upgrade for Leopard Server" {
		t.Fatalf("first page entries = %#v", entries)
	}

	entries, total, err = fsys.ReadDirRange(filepath.Join(root, "Apps", "Antivirus"), 5, 1)
	if err != nil {
		t.Fatalf("ReadDirRange last page: %v", err)
	}
	if total != 5 {
		t.Fatalf("last-page total = %d, want 5", total)
	}
	if len(entries) != 1 || entries[0].Name() != "SecureInit" {
		t.Fatalf("last page entries = %#v", entries)
	}
	if got := fsys.itemURLByDir["SecureInit"]; got != "https://macintoshgarden.org/apps/secureinit" {
		t.Fatalf("cached item URL = %q, want secureinit URL", got)
	}
}

func TestMacGardenGetItemURLInCategory_UsesCachedPageItems(t *testing.T) {
	catURL := "https://macintoshgarden.org/apps/utilities/antivirus"
	fsys := &MacGardenFileSystem{
		categoryPageItems: map[string]map[int][]garden.SearchResult{
			catURL: {
				0: {
					{Name: "SecureInit", URL: "https://macintoshgarden.org/apps/secureinit"},
				},
			},
		},
		itemURLByDir: make(map[string]string),
	}

	got, err := fsys.getItemURLInCategory(catURL, "SecureInit")
	if err != nil {
		t.Fatalf("getItemURLInCategory error: %v", err)
	}
	if got != "https://macintoshgarden.org/apps/secureinit" {
		t.Fatalf("item URL = %q, want secureinit URL", got)
	}
}

func TestMacGardenReadDirRange_CategoryReqCountIsCappedToFirstWindow(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	catURL := "https://macintoshgarden.org/apps/utilities/antivirus"
	firstPage := make([]garden.SearchResult, 0, 10)
	for i := 1; i <= 10; i++ {
		firstPage = append(firstPage, garden.SearchResult{
			Name: "Item " + string(rune('A'+i-1)),
			URL:  "https://macintoshgarden.org/apps/item-" + string(rune('a'+i-1)),
		})
	}

	fsys := &MacGardenFileSystem{
		root:              root,
		categories:        []garden.Category{{Name: "Antivirus", URL: catURL}},
		categoryItemCount: make(map[string]uint16),
		categoryPageMeta: map[string]macGardenCategoryPageMeta{
			catURL: {TotalCount: 100, PageSize: 10, LastPageNumber: 9, LastPageCount: 10},
		},
		categoryPageItems: map[string]map[int][]garden.SearchResult{
			catURL: {
				0: firstPage,
			},
		},
		itemURLByDir: make(map[string]string),
	}

	entries, total, err := fsys.ReadDirRange(filepath.Join(root, "Apps", "Antivirus"), 1, 64)
	if err != nil {
		t.Fatalf("ReadDirRange: %v", err)
	}
	if total != 100 {
		t.Fatalf("total = %d, want 100", total)
	}
	if len(entries) != 10 {
		t.Fatalf("len(entries) = %d, want 10", len(entries))
	}
}
func TestMacGardenStat_ItemChildIsLazyUntilItemOpened(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	catURL := "https://macintoshgarden.org/apps/visual-arts-graphics/3d-rendering-cad"
	itemURL := "https://macintoshgarden.org/apps/alias-upfront-20"

	fsys := &MacGardenFileSystem{
		root:         root,
		categories:   []garden.Category{{Name: "3D Rendering & CAD", URL: catURL}},
		itemURLByDir: map[string]string{"Alias upFRONT 2.0": itemURL},
		itemByURL:    make(map[string]*garden.SoftwareItem),
	}

	_, err := fsys.Stat(filepath.Join(root, "Apps", "3D Rendering & CAD", "Alias upFRONT 2.0", "Configuration"))
	if err == nil {
		t.Fatal("expected fs.ErrNotExist for unopened item child path")
	}
	if err != fs.ErrNotExist {
		t.Fatalf("Stat error = %v, want %v", err, fs.ErrNotExist)
	}
	if len(fsys.itemByURL) != 0 {
		t.Fatalf("item cache size = %d, want 0 (no lazy fetch)", len(fsys.itemByURL))
	}
}

func TestMacGardenReadDir_ItemSkipsAssetsWhenHeadFails(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	catURL := "https://macintoshgarden.org/apps/visual-arts-graphics/3d-rendering-cad"
	itemURL := "https://macintoshgarden.org/apps/alias-upfront-20"
	fsys := &MacGardenFileSystem{
		root:         root,
		client:       garden.NewClient(),
		categories:   []garden.Category{{Name: "3D Rendering & CAD", URL: catURL}},
		itemURLByDir: map[string]string{"Alias upFRONT 2.0": itemURL},
		itemByURL: map[string]*garden.SoftwareItem{
			itemURL: {
				Title:       "Alias upFRONT 2.0",
				URL:         itemURL,
				Description: "desc",
				Screenshots: []string{"://bad-screenshot-url"},
				Downloads: []garden.DownloadDetails{{
					Title: "Alias upFRONT 2.0",
					Links: []garden.DownloadLink{{Text: "Download", URL: "://bad-download-url"}},
				}},
			},
		},
		downloadByPath:    make(map[string]macGardenAsset),
		screenshotByPath:  make(map[string]macGardenAsset),
		descriptionByPath: make(map[string]macGardenAsset),
	}

	entries, err := fsys.ReadDir(filepath.Join(root, "Apps", "3D Rendering & CAD", "Alias upFRONT 2.0"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 description files only", len(entries))
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["Description.txt"] || !names["Description.html"] {
		t.Fatalf("entries = %#v, want description files only", entries)
	}
}

func TestMacGardenStat_SearchHitRootDirExists(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	fsys := &MacGardenFileSystem{
		root: root,
		searchByName: map[string]macGardenCachedResult{
			"ClarisWorks 4.0": {Name: "ClarisWorks 4.0", URL: "https://macintoshgarden.org/apps/clarisworks-40"},
		},
	}

	info, err := fsys.Stat(filepath.Join(root, "ClarisWorks 4.0"))
	if err != nil {
		t.Fatalf("Stat search-hit root dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("search-hit info IsDir = false, want true")
	}
}

func TestNormalizeMacGardenSearchQuery_StripsFinderNoise(t *testing.T) {
	got := normalizeMacGardenSearchQuery(`. " clarisworks$ @ "`)
	if got != "clarisworks" {
		t.Fatalf("normalizeMacGardenSearchQuery() = %q, want %q", got, "clarisworks")
	}
}

func TestMacGardenCatSearch_UsesTypeSubdirectoryWhenKnown(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	query := "clarisworks"
	fsys := &MacGardenFileSystem{
		root: root,
		catSearchCache: map[string]*macGardenSearchCache{
			query: {
				pages: map[int][]garden.SearchResult{
					0: {
						{Name: "ClarisWorks 4.0", URL: "https://macintoshgarden.org/apps/clarisworks-40", Type: "App"},
						{Name: "Mystery Result", URL: "https://macintoshgarden.org/apps/mystery", Type: ""},
					},
				},
				exhausted: true,
			},
		},
		searchByName: make(map[string]macGardenCachedResult),
		itemURLByDir: make(map[string]string),
	}

	cursor := [16]byte{0x01, 'c', 'l', 'a'} // continuation + query hash for "cla..."
	paths, _, errCode := fsys.CatSearch("", query, 10, cursor)
	if errCode != afp.NoErr {
		t.Fatalf("CatSearch errCode=%d, want %d", errCode, afp.NoErr)
	}
	if len(paths) != 2 {
		t.Fatalf("len(paths)=%d, want 2", len(paths))
	}
	if paths[0] != filepath.Join(root, "search", query, "App", "ClarisWorks 4.0") {
		t.Fatalf("paths[0]=%q, want typed path", paths[0])
	}
	if paths[1] != filepath.Join(root, "search", query, "Mystery Result") {
		t.Fatalf("paths[1]=%q, want legacy untyped path", paths[1])
	}
}
