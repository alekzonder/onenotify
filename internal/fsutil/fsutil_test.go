package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesAndLeavesNoTempFile(t *testing.T) {
	for name, write := range map[string]func(string, []byte, os.FileMode) error{
		"atomic": WriteFileAtomic, "durable": WriteFileDurable,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.json")
		for _, content := range []string{"one", "two"} {
			if err := write(path, []byte(content), 0o640); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		data, _ := os.ReadFile(path)
		entries, _ := os.ReadDir(dir)
		info, _ := os.Stat(path)
		if string(data) != "two" || len(entries) != 1 || info.Mode().Perm() != 0o640 {
			t.Errorf("%s: content=%q entries=%d mode=%v", name, data, len(entries), info.Mode().Perm())
		}
	}
}

func TestWriteIntoMissingDirFails(t *testing.T) {
	if err := WriteFileDurable(filepath.Join(t.TempDir(), "no", "a"), []byte("x"), 0o640); err == nil {
		t.Error("want an error")
	}
}
