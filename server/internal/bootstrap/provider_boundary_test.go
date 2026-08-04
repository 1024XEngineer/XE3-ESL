package bootstrap

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBusinessPackagesDoNotImportConcreteProviders(t *testing.T) {
	serverRoot := bootstrapServerRoot(t)
	for _, relativeRoot := range []string{
		"internal/agent",
		"internal/coaching/practice",
		"internal/coaching/evaluation",
	} {
		root := filepath.Join(serverRoot, relativeRoot)
		err := filepath.WalkDir(root, func(
			path string,
			entry fs.DirEntry,
			err error,
		) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(
				token.NewFileSet(),
				path,
				nil,
				parser.ImportsOnly,
			)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				pathValue := strings.Trim(imported.Path.Value, `"`)
				if strings.HasPrefix(
					pathValue,
					"github.com/1024XEngineer/XE3-ESL/server/internal/providers/",
				) {
					t.Errorf("%s imports concrete provider %s", path, pathValue)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", relativeRoot, err)
		}
	}

	for _, oldPath := range []string{
		"internal/ai/qianwen",
		"internal/ai/xfyun",
	} {
		_, err := os.Stat(filepath.Join(serverRoot, oldPath))
		if !os.IsNotExist(err) {
			t.Fatalf("old provider path %s still exists: %v", oldPath, err)
		}
	}
}

func bootstrapServerRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve bootstrap test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../.."))
}
