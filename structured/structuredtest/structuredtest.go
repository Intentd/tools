package structuredtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RequireErr fails unless at least one error in errs contains substr.
func RequireErr(t *testing.T, errs []error, substr string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return
		}
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	t.Errorf("expected an error containing %q, got: %v", substr, msgs)
}

// WriteFile writes content to path with permissions 0644, creating its
// directory.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	WriteFileMode(t, path, content, 0o644)
}

// WriteExecutable writes a no-op shell script to path with executable permissions.
func WriteExecutable(t *testing.T, path string) {
	t.Helper()
	WriteFileMode(t, path, "#!/bin/sh\nexit 0\n", 0o755)
}

// WriteFileMode writes content to path with the specified permissions,
// creating its directory.
func WriteFileMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating the directory of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
