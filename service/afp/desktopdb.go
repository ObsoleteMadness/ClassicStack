package afp

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/pgodw/omnitalk/go/netlog"
)

const desktopDBFilename = ".desktop.db"

// iconEntry holds the data for a stored icon.
type iconEntry struct {
	tag    uint32
	bitmap []byte
}

// applEntry holds a single APPL mapping for an application.
type applEntry struct {
	tag      uint32
	dirID    uint32
	pathname string
}

// DesktopDB provides Desktop database operations used by AFP Desktop commands.
type DesktopDB interface {
	GetComment(relPath string) (string, bool)
	SetComment(relPath, comment string) error
	RemoveComment(relPath string) error
	GetIcon(creator, fileType [4]byte, iconType byte) (iconEntry, bool)
	GetIconInfo(creator [4]byte, index uint16) (iconEntry, [4]byte, byte, bool)
	SetIcon(creator, fileType [4]byte, iconType byte, tag uint32, bitmap []byte) error
	AddAPPL(creator [4]byte, tag uint32, dirID uint32, pathname string) error
	RemoveAPPL(creator [4]byte, dirID uint32, pathname string) error
	GetAPPL(creator [4]byte, index uint16) (applEntry, bool)
	ListAPPL(creator [4]byte) []applEntry
	IconCount(creator [4]byte) (creatorCount int, total int)
}

// DesktopDBBackend creates a per-volume DesktopDB implementation.
type DesktopDBBackend interface {
	Open(volume Volume) DesktopDB
}

// SQLiteDesktopDBBackend stores Desktop database records in SQLite tables.
type SQLiteDesktopDBBackend struct{}

func (SQLiteDesktopDBBackend) Open(volume Volume) DesktopDB {
	db, err := NewSQLiteDesktopDB(volume.Config.Path)
	if err != nil {
		netlog.Warn("[AFP][Desktop] sqlite init failed for volume=%q path=%q: %v", volume.Config.Name, volume.Config.Path, err)
		return newMemoryDesktopDB()
	}
	return db
}

func resolveDesktopDBBackend(options AFPOptions) DesktopDBBackend {
	if options.DesktopStoreBackend != nil {
		return options.DesktopStoreBackend
	}
	switch options.DesktopBackend {
	case "", "sqlite":
		return SQLiteDesktopDBBackend{}
	default:
		return SQLiteDesktopDBBackend{}
	}
}

// ErrIconSizeMismatch is returned by SetIcon when a replacement icon has a
// different bitmap size than the existing entry (AFP spec §FPAddIcon).
var ErrIconSizeMismatch = fmt.Errorf("icon size mismatch")

// sqliteDesktopDB stores Desktop database records in SQLite.
type sqliteDesktopDB struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewSQLiteDesktopDB opens (or creates) the Desktop database for a volume root.
func NewSQLiteDesktopDB(volumeRootPath string) (DesktopDB, error) {
	db, err := openSQLiteDB(volumeRootPath)
	if err != nil {
		return nil, err
	}
	store := &sqliteDesktopDB{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// NewDesktopDB creates the default DesktopDB implementation for a volume root.
func NewDesktopDB(volumeRootPath string) DesktopDB {
	db, err := NewSQLiteDesktopDB(volumeRootPath)
	if err != nil {
		netlog.Warn("[AFP][Desktop] NewDesktopDB sqlite init failed path=%q: %v", volumeRootPath, err)
		return newMemoryDesktopDB()
	}
	return db
}

func (db *sqliteDesktopDB) initSchema() error {
	_, err := db.db.Exec(`
		CREATE TABLE IF NOT EXISTS desktop_comments (
			rel_path TEXT PRIMARY KEY,
			comment TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS desktop_icons (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			creator BLOB NOT NULL,
			file_type BLOB NOT NULL,
			icon_type INTEGER NOT NULL,
			tag INTEGER NOT NULL,
			bitmap BLOB NOT NULL,
			UNIQUE(creator, file_type, icon_type)
		);
		CREATE INDEX IF NOT EXISTS idx_desktop_icons_creator_seq ON desktop_icons(creator, seq);
		CREATE TABLE IF NOT EXISTS desktop_appls (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			creator BLOB NOT NULL,
			tag INTEGER NOT NULL,
			dir_id INTEGER NOT NULL,
			pathname TEXT NOT NULL,
			UNIQUE(creator, dir_id, pathname)
		);
		CREATE INDEX IF NOT EXISTS idx_desktop_appls_creator_seq ON desktop_appls(creator, seq);
	`)
	return err
}

func (db *sqliteDesktopDB) GetComment(relPath string) (string, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var comment string
	err := db.db.QueryRow("SELECT comment FROM desktop_comments WHERE rel_path = ?", relPath).Scan(&comment)
	if err != nil {
		netlog.Debug("[AFP][Desktop] GetComment miss path=%q", relPath)
		return "", false
	}
	return comment, true
}

func (db *sqliteDesktopDB) SetComment(relPath, comment string) error {
	if len(comment) > 199 {
		comment = comment[:199]
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.db.Exec(`
		INSERT INTO desktop_comments(rel_path, comment) VALUES(?, ?)
		ON CONFLICT(rel_path) DO UPDATE SET comment = excluded.comment
	`, relPath, comment)
	return err
}

func (db *sqliteDesktopDB) RemoveComment(relPath string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.db.Exec("DELETE FROM desktop_comments WHERE rel_path = ?", relPath)
	return err
}

func (db *sqliteDesktopDB) GetIcon(creator, fileType [4]byte, iconType byte) (iconEntry, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var tag uint32
	var bitmap []byte
	err := db.db.QueryRow(`
		SELECT tag, bitmap
		FROM desktop_icons
		WHERE creator = ? AND file_type = ? AND icon_type = ?
	`, creator[:], fileType[:], uint32(iconType)).Scan(&tag, &bitmap)
	if err != nil {
		netlog.Debug("[AFP][Desktop] GetIcon miss creator=%q type=%q itype=%d", string(creator[:]), string(fileType[:]), iconType)
		return iconEntry{}, false
	}
	return iconEntry{tag: tag, bitmap: bitmap}, true
}

func (db *sqliteDesktopDB) GetIconInfo(creator [4]byte, index uint16) (iconEntry, [4]byte, byte, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if index == 0 {
		return iconEntry{}, [4]byte{}, 0, false
	}
	var (
		tag      uint32
		bitmap   []byte
		fileType [4]byte
		iconType uint32
	)
	err := db.db.QueryRow(`
		SELECT tag, bitmap, file_type, icon_type
		FROM desktop_icons
		WHERE creator = ?
		ORDER BY seq
		LIMIT 1 OFFSET ?
	`, creator[:], int(index)-1).Scan(&tag, &bitmap, fileType[:], &iconType)
	if err != nil {
		netlog.Debug("[AFP][Desktop] GetIconInfo miss creator=%q index=%d", string(creator[:]), index)
		return iconEntry{}, [4]byte{}, 0, false
	}
	return iconEntry{tag: tag, bitmap: bitmap}, fileType, byte(iconType), true
}

func (db *sqliteDesktopDB) SetIcon(creator, fileType [4]byte, iconType byte, tag uint32, bitmap []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	var existingSize int
	err := db.db.QueryRow(`
		SELECT LENGTH(bitmap)
		FROM desktop_icons
		WHERE creator = ? AND file_type = ? AND icon_type = ?
	`, creator[:], fileType[:], uint32(iconType)).Scan(&existingSize)
	if err == nil && existingSize != len(bitmap) {
		return ErrIconSizeMismatch
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	_, err = db.db.Exec(`
		INSERT INTO desktop_icons(creator, file_type, icon_type, tag, bitmap)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(creator, file_type, icon_type)
		DO UPDATE SET tag = excluded.tag, bitmap = excluded.bitmap
	`, creator[:], fileType[:], uint32(iconType), tag, bitmap)
	return err
}

func (db *sqliteDesktopDB) AddAPPL(creator [4]byte, tag uint32, dirID uint32, pathname string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.db.Exec(`
		INSERT INTO desktop_appls(creator, tag, dir_id, pathname)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(creator, dir_id, pathname)
		DO UPDATE SET tag = excluded.tag
	`, creator[:], tag, dirID, pathname)
	return err
}

func (db *sqliteDesktopDB) RemoveAPPL(creator [4]byte, dirID uint32, pathname string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.db.Exec(
		"DELETE FROM desktop_appls WHERE creator = ? AND dir_id = ? AND pathname = ?",
		creator[:], dirID, pathname,
	)
	return err
}

func (db *sqliteDesktopDB) GetAPPL(creator [4]byte, index uint16) (applEntry, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var e applEntry
	err := db.db.QueryRow(`
		SELECT tag, dir_id, pathname
		FROM desktop_appls
		WHERE creator = ?
		ORDER BY seq
		LIMIT 1 OFFSET ?
	`, creator[:], int(index)).Scan(&e.tag, &e.dirID, &e.pathname)
	if err != nil {
		netlog.Debug("[AFP][Desktop] GetAPPL miss creator=%q index=%d", string(creator[:]), index)
		return applEntry{}, false
	}
	return e, true
}

func (db *sqliteDesktopDB) ListAPPL(creator [4]byte) []applEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()
	rows, err := db.db.Query(`
		SELECT tag, dir_id, pathname
		FROM desktop_appls
		WHERE creator = ?
		ORDER BY seq
	`, creator[:])
	if err != nil {
		return nil
	}
	defer rows.Close()

	entries := make([]applEntry, 0)
	for rows.Next() {
		var e applEntry
		if err := rows.Scan(&e.tag, &e.dirID, &e.pathname); err != nil {
			return entries
		}
		entries = append(entries, e)
	}
	return entries
}

func (db *sqliteDesktopDB) IconCount(creator [4]byte) (creatorCount int, total int) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	_ = db.db.QueryRow("SELECT COUNT(1) FROM desktop_icons WHERE creator = ?", creator[:]).Scan(&creatorCount)
	_ = db.db.QueryRow("SELECT COUNT(1) FROM desktop_icons").Scan(&total)
	return creatorCount, total
}

// memoryDesktopDB is a non-persistent fallback if SQLite cannot be opened.
type memoryDesktopDB struct {
	mu        sync.RWMutex
	comments  map[string]string
	icons     map[iconKey]iconEntry
	iconOrder map[[4]byte][]iconKey
	appls     map[[4]byte][]applEntry
}

type iconKey struct {
	creator  [4]byte
	fileType [4]byte
	iconType byte
}

func newMemoryDesktopDB() *memoryDesktopDB {
	return &memoryDesktopDB{
		comments:  make(map[string]string),
		icons:     make(map[iconKey]iconEntry),
		iconOrder: make(map[[4]byte][]iconKey),
		appls:     make(map[[4]byte][]applEntry),
	}
}

func (db *memoryDesktopDB) GetComment(relPath string) (string, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	c, ok := db.comments[relPath]
	return c, ok
}

func (db *memoryDesktopDB) SetComment(relPath, comment string) error {
	if len(comment) > 199 {
		comment = comment[:199]
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.comments[relPath] = comment
	return nil
}

func (db *memoryDesktopDB) RemoveComment(relPath string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.comments, relPath)
	return nil
}

func (db *memoryDesktopDB) GetIcon(creator, fileType [4]byte, iconType byte) (iconEntry, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	e, ok := db.icons[iconKey{creator: creator, fileType: fileType, iconType: iconType}]
	return e, ok
}

func (db *memoryDesktopDB) GetIconInfo(creator [4]byte, index uint16) (iconEntry, [4]byte, byte, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if index == 0 || int(index) > len(db.iconOrder[creator]) {
		return iconEntry{}, [4]byte{}, 0, false
	}
	k := db.iconOrder[creator][index-1]
	e := db.icons[k]
	return e, k.fileType, k.iconType, true
}

func (db *memoryDesktopDB) SetIcon(creator, fileType [4]byte, iconType byte, tag uint32, bitmap []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	k := iconKey{creator: creator, fileType: fileType, iconType: iconType}
	if existing, ok := db.icons[k]; ok && len(existing.bitmap) != len(bitmap) {
		return ErrIconSizeMismatch
	}
	if _, ok := db.icons[k]; !ok {
		db.iconOrder[creator] = append(db.iconOrder[creator], k)
	}
	db.icons[k] = iconEntry{tag: tag, bitmap: bitmap}
	return nil
}

func (db *memoryDesktopDB) AddAPPL(creator [4]byte, tag uint32, dirID uint32, pathname string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	entries := db.appls[creator]
	for i, e := range entries {
		if e.dirID == dirID && e.pathname == pathname {
			entries[i] = applEntry{tag: tag, dirID: dirID, pathname: pathname}
			db.appls[creator] = entries
			return nil
		}
	}
	db.appls[creator] = append(entries, applEntry{tag: tag, dirID: dirID, pathname: pathname})
	return nil
}

func (db *memoryDesktopDB) RemoveAPPL(creator [4]byte, dirID uint32, pathname string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	entries := db.appls[creator]
	for i, e := range entries {
		if e.dirID == dirID && e.pathname == pathname {
			db.appls[creator] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	return nil
}

func (db *memoryDesktopDB) GetAPPL(creator [4]byte, index uint16) (applEntry, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	entries := db.appls[creator]
	if int(index) >= len(entries) {
		return applEntry{}, false
	}
	return entries[index], true
}

func (db *memoryDesktopDB) ListAPPL(creator [4]byte) []applEntry {
	db.mu.RLock()
	defer db.mu.RUnlock()
	entries := db.appls[creator]
	dup := make([]applEntry, len(entries))
	copy(dup, entries)
	return dup
}

func (db *memoryDesktopDB) IconCount(creator [4]byte) (creatorCount int, total int) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.iconOrder[creator]), len(db.icons)
}
