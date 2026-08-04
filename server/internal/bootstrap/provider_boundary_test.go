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
		"internal/avatar",
		"internal/coaching/practice",
		"internal/coaching/evaluation",
		"internal/coaching/preparation",
		"internal/resume",
	} {
		assertSourceTreeDoesNotImport(
			t,
			serverRoot,
			relativeRoot,
			"github.com/1024XEngineer/XE3-ESL/server/internal/providers/",
		)
	}
	assertSourceTreeDoesNotImport(
		t,
		serverRoot,
		"cmd/server",
		"github.com/1024XEngineer/XE3-ESL/server/internal/providers/spatius",
	)

	for _, oldPath := range []string{
		"internal/ai",
		"internal/avatar/spatius_client.go",
	} {
		_, err := os.Stat(filepath.Join(serverRoot, oldPath))
		if !os.IsNotExist(err) {
			t.Fatalf("old provider path %s still exists: %v", oldPath, err)
		}
	}
}

func assertSourceTreeDoesNotImport(
	t *testing.T,
	serverRoot string,
	relativeRoot string,
	forbiddenPrefix string,
) {
	t.Helper()
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
			if strings.HasPrefix(pathValue, forbiddenPrefix) {
				t.Errorf("%s imports forbidden package %s", path, pathValue)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", relativeRoot, err)
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
