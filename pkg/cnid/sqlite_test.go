//go:build sqlite_cnid || all

package cnid

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkSQLiteRemove(b *testing.B) {
	dir, err := os.MkdirTemp("", "cnid_test_*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := NewSQLiteStore(dir)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Setup 1000 paths
		for j := 0; j < 1000; j++ {
			store.EnsureReserved(filepath.Join("test", fmt.Sprintf("file_%d", j)), uint32(1000+j))
		}
		b.StartTimer()
		store.Remove("test")
	}
}
