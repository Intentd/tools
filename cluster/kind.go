package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/term"
)

// DefaultKindVersion is the kind release downloaded when none is in PATH.
const DefaultKindVersion = "v0.27.0"

// KindOptions configures Ensure and DeleteKindCluster.
type KindOptions struct {
	// Workers is the number of worker nodes beside the control plane.
	Workers int
	// Version selects the kind release to download; empty prefers kind in
	// PATH, then DefaultKindVersion.
	Version string
	// BinDir is where a downloaded kind is kept; empty is the user's cache
	// directory (intentd/bin under it).
	BinDir string
}

// EnsureKind creates a kind cluster named name if it does not already
// exist, then switches kubectl to its context. kind is downloaded from GitHub
// releases into opts.BinDir when it is not found.
func EnsureKind(ctx context.Context, name string, opts KindOptions) error {
	workers := opts.Workers
	kindBin, err := resolveKind(opts.Version, opts.BinDir)
	if err != nil {
		return err
	}

	if kindClusterListed(ctx, kindBin, name) {
		fmt.Printf("  ✓ Cluster '%s' already exists\n", name)
		return useKindContext(ctx, name)
	}

	label := fmt.Sprintf("'%s'", name)
	workerText := "workers"
	if workers == 1 {
		workerText = "worker"
	}
	if workers > 0 {
		label = fmt.Sprintf("'%s' (%d %s)", name, workers, workerText)
	}
	fmt.Printf("  → Creating local cluster %s...\n", label)

	args := []string{"create", "cluster", "--name", name}
	if workers > 0 {
		cfg, cfgErr := writeKindConfig(workers)
		if cfgErr != nil {
			return fmt.Errorf("kind config: %w", cfgErr)
		}
		defer os.Remove(cfg)
		args = append(args, "--config", cfg)
	}

	if err := runStreamed(ctx, command.Args(kindBin, args...)); err != nil {
		return fmt.Errorf("kind create cluster: %w", err)
	}
	fmt.Printf("  %s Cluster '%s' ready\n", term.SuccessMark(), name)

	total := workers + 1 // workers + control-plane
	ready := 0
	spin := term.StartSpinner("   → Waiting for nodes to be ready...")
	defer spin.Failure()
	if err := waitForNodesReady(ctx, 5*time.Minute, func(_ string) {
		ready++
		spin.Update(fmt.Sprintf("   → Waiting for nodes to be ready... (%d/%d)", ready, total))
	}); err != nil {
		return err
	}
	spin.Update(fmt.Sprintf("Nodes ready (%d/%d)", total, total))
	spin.Success()
	return useKindContext(ctx, name)
}

// DeleteKind deletes a kind cluster by name. Safe to call when the
// cluster does not exist.
func DeleteKind(ctx context.Context, name string, opts KindOptions) error {
	version := opts.Version
	if version == "" {
		version = DefaultKindVersion
	}
	kindBin, err := resolveKind(version, opts.BinDir)
	if err != nil {
		return err
	}
	return runStreamed(ctx, command.Args(kindBin, "delete", "cluster", "--name", name))
}

// KindExists checks if the kind cluster name exists
func KindExists(ctx context.Context, name string) bool {
	return kindClusterListed(ctx, "kind", name)
}

// kindClusterListed reports whether `kind get clusters` lists name.
func kindClusterListed(ctx context.Context, kindBin, name string) bool {
	out, _ := command.Output(ctx, command.Args(kindBin, "get", "clusters"))
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// ── private helpers ───────────────────────────────────────────────────────────

func useKindContext(ctx context.Context, name string) error {
	ctxName := "kind-" + name
	if err := runStreamed(ctx, command.Args("kubectl", "config", "use-context", ctxName)); err != nil {
		return fmt.Errorf("switching kubectl context to %s: %w", ctxName, err)
	}
	return nil
}

func resolveKind(version, binDir string) (string, error) {
	if version == "" {
		// No explicit version: prefer whatever is already in PATH.
		if p, err := exec.LookPath("kind"); err == nil {
			return p, nil
		}
		version = DefaultKindVersion
	}
	// Explicit version: use the versioned cache entry, downloading if needed.
	if binDir == "" {
		binDir = defaultBinDir()
	}
	dest := filepath.Join(binDir, "kind-"+version)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	return downloadKind(dest, version)
}

func downloadKind(dest, version string) (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	url := fmt.Sprintf(
		"https://github.com/kubernetes-sigs/kind/releases/download/%s/kind-%s-%s",
		version, goos, goarch,
	)

	fmt.Printf("  → Downloading kind %s...\n", version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("kind: cannot create bin dir: %w", err)
	}

	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("kind: download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kind: download returned %s for %s", resp.Status, url)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("kind: writing binary: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dest)
		return "", fmt.Errorf("kind: writing binary: %w", err)
	}
	f.Close()

	fmt.Printf("  %s kind %s installed (%s)\n", term.SuccessMark(), version, dest)
	return dest, nil
}

func writeKindConfig(workers int) (string, error) {
	f, err := os.CreateTemp("", "kind-config-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintf(f, "kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nnodes:\n- role: control-plane\n")
	for range workers {
		fmt.Fprintf(f, "- role: worker\n")
	}
	return f.Name(), nil
}

func waitForNodesReady(ctx context.Context, timeout time.Duration, onNodeReady func(nodeName string)) error {
	// Per-node "condition met" lines are read as they come, not written to
	// the terminal. kubectl prints: "node/<cluster>-worker condition met".
	lines := &lineWriter{line: func(line string) {
		if strings.Contains(line, "condition met") {
			parts := strings.Fields(line)
			if len(parts) > 0 && onNodeReady != nil {
				onNodeReady(strings.TrimPrefix(parts[0], "node/"))
			}
		}
	}}
	c := command.Args("kubectl", "wait", "--for=condition=Ready",
		"node", "--all", fmt.Sprintf("--timeout=%ds", int(timeout.Seconds())))
	c.Stream = lines
	r, err := command.Run(ctx, c)
	if err != nil {
		return fmt.Errorf("kubectl wait: %w", err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("nodes not ready after %s", timeout)
	}
	return nil
}

// lineWriter calls line for each complete line written to it.
type lineWriter struct {
	line    func(string)
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.line(string(w.pending[:i]))
		w.pending = w.pending[i+1:]
	}
}

// defaultBinDir is intentd/bin under the user's cache directory.
func defaultBinDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "intentd", "bin")
}
