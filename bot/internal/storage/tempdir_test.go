package storage

import (
	"os"
	"testing"
	"time"
)

// tempDir stands in for t.TempDir in tests with a database. On Windows the
// files of a closed SQLite database may stay for a moment (the antivirus, the
// delayed removal of -wal and -shm), and t.TempDir then fails the test with
// "The directory is not empty", which it does not retry. Here the removal is
// retried for 5 s, and a directory that stays is only logged.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fragmentor-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Logf("temporary directory left behind: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}
