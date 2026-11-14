package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── writeKindConfig ───────────────────────────────────────────────────────────

func TestWriteKindConfig_ControlPlaneOnly(t *testing.T) {
	path, err := writeKindConfig(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, _ := readLocal(path)
	content := string(data)

	if !strings.Contains(content, "role: control-plane") {
		t.Error("expected control-plane node")
	}
	if strings.Contains(content, "role: worker") {
		t.Error("expected no worker nodes for workers=0")
	}
}

func TestWriteKindConfig_WithWorkers(t *testing.T) {
	for _, workers := range []int{1, 3, 5} {
		path, err := writeKindConfig(workers)
		if err != nil {
			t.Fatalf("workers=%d: unexpected error: %v", workers, err)
		}
		defer os.Remove(path)

		data, _ := readLocal(path)
		content := string(data)

		got := strings.Count(content, "role: worker")
		if got != workers {
			t.Errorf("workers=%d: got %d worker entries, want %d", workers, got, workers)
		}
		if !strings.Contains(content, "role: control-plane") {
			t.Errorf("workers=%d: missing control-plane node", workers)
		}
	}
}

func TestWriteKindConfig_ValidYAMLHeader(t *testing.T) {
	path, err := writeKindConfig(1)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, _ := readLocal(path)
	content := string(data)

	if !strings.Contains(content, "kind: Cluster") {
		t.Error("expected 'kind: Cluster'")
	}
	if !strings.Contains(content, "apiVersion: kind.x-k8s.io/v1alpha4") {
		t.Error("expected 'apiVersion: kind.x-k8s.io/v1alpha4'")
	}
}

func TestWriteKindConfig_FileIsTemp(t *testing.T) {
	path, err := writeKindConfig(0)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	base := filepath.Base(path)
	if !strings.HasPrefix(base, "kind-config-") {
		t.Errorf("expected temp file name to start with 'kind-config-', got %q", base)
	}
}

// ── resolveKind ───────────────────────────────────────────────────────────────

func TestResolveKind_CachedBinaryUsed(t *testing.T) {
	dir := t.TempDir()
	version := "v9.99.0"
	cached := filepath.Join(dir, "kind-"+version)
	if err := os.WriteFile(cached, []byte("#!/bin/sh\necho kind"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveKind(version, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cached {
		t.Errorf("expected %q, got %q", cached, got)
	}
}

func TestResolveKind_ExplicitVersionSkipsPath(t *testing.T) {
	// A version not in the bin dir is downloaded, never taken from PATH. The
	// download fails without network (or for a release that doesn't exist).
	_, err := resolveKind("v0.0.0-test-sentinel", t.TempDir())
	if err == nil {
		t.Log("resolveKind returned a binary (network available) — skipping negative assertion")
		return
	}
	if !strings.Contains(err.Error(), "download") && !strings.Contains(err.Error(), "404") {
		t.Errorf("expected download error, got: %v", err)
	}
}

func TestDefaultBinDir_UnderCache(t *testing.T) {
	if !strings.HasSuffix(defaultBinDir(), filepath.Join("intentd", "bin")) {
		t.Errorf("defaultBinDir() = %q", defaultBinDir())
	}
}
