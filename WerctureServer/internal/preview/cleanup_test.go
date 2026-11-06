package preview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedPreviewCommitRemovesTemporaryFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "preview.png")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("preview")); err == nil {
		t.Fatal("expected rename to directory to fail")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary preview retained: %v", err)
	}
}
