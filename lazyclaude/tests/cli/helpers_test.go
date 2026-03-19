package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	binaryPath string
	buildOnce  sync.Once
	buildErr   error
)

// ensureBinary builds the lazyclaude binary once per test run.
func ensureBinary(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		// Resolve project root from test working directory
		wd, _ := os.Getwd()
		root := filepath.Join(wd, "..", "..")
		binaryPath = filepath.Join(root, "bin", "lazyclaude-test")

		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/lazyclaude")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build: %w\n%s", err, out)
		}
	})

	if buildErr != nil {
		t.Fatalf("build lazyclaude: %v", buildErr)
	}

	return binaryPath
}

// testdataPath returns the absolute path to a testdata file.
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("testdata path: %v", err)
	}
	return abs
}
