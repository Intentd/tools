package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/duration"
)

// HelmRelease is a chart to install as a release.
type HelmRelease struct {
	// Repo is the Helm repository URL. Omit for local chart paths.
	Repo string
	// Chart is the chart name within the repository, or a local path (e.g. ./).
	Chart string
	// Release is the release name. Defaults to the chart name when empty.
	Release string
	// Namespace for the release. Defaults to "default".
	Namespace string
	// Version pins the chart version. Leave empty for latest or local charts.
	Version string
	// ValueFiles is an ordered list of values files (local paths or URLs).
	ValueFiles []string
	// Values are inline key-value overrides, equivalent to helm --set.
	Values map[string]interface{}
	// CreateNamespace passes --create-namespace to helm.
	CreateNamespace bool
}

// Name returns the effective release name.
func (h HelmRelease) Name() string {
	if h.Release != "" {
		return h.Release
	}
	return h.Chart
}

// EffectiveNamespace returns the effective namespace, defaulting to "default".
func (h HelmRelease) EffectiveNamespace() string {
	if h.Namespace != "" {
		return h.Namespace
	}
	return "default"
}

// IsLocalChart reports whether Chart is a local filesystem path.
func (h HelmRelease) IsLocalChart() bool {
	return strings.HasPrefix(h.Chart, "./") || strings.HasPrefix(h.Chart, "../") || strings.HasPrefix(h.Chart, "/") ||
		h.Chart == "." || h.Chart == ".."
}

// IsOCIChart reports whether Chart is an OCI reference (oci://…), which
// Helm installs directly, without a repository.
func (h HelmRelease) IsOCIChart() bool { return strings.HasPrefix(h.Chart, "oci://") }

// Validate returns an error when required fields are missing.
func (h HelmRelease) Validate() error {
	if h.Chart == "" {
		return errors.New("chart is required")
	}
	if !h.IsLocalChart() && !h.IsOCIChart() && h.Repo == "" {
		return errors.New("repo is required for remote charts (or an oci:// chart)")
	}
	return nil
}

// Wait is a resource to wait for.
type Wait struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, Secret, Namespace).
	Kind string
	// Name is the exact resource name.
	Name string
	// Namespace to look in. Omit for cluster-scoped resources.
	Namespace string
	// Ready waits for an available/ready condition, not just existence.
	Ready bool
	// Condition is the condition Ready waits for, other than Deployments'
	// rollout: Ready when empty (a RabbitmqCluster's is AllReplicasReady).
	Condition string
	// Timeout is a duration (see tools/duration). Default: "30s".
	Timeout string
}

// HelmInstall installs or upgrades h with helm upgrade --install, so the
// call is idempotent.
func HelmInstall(ctx context.Context, h HelmRelease) error {
	if err := h.Validate(); err != nil {
		return err
	}

	release := h.Name()
	namespace := h.EffectiveNamespace()

	chartRef := h.Chart
	if !h.IsLocalChart() && !h.IsOCIChart() {
		repoName := release
		_, _ = command.Run(ctx, command.Args("helm", "repo", "add", repoName, h.Repo))
		_, _ = command.Run(ctx, command.Args("helm", "repo", "update", repoName))
		chartRef = fmt.Sprintf("%s/%s", repoName, h.Chart)
	}

	args := []string{
		"upgrade", "--install",
		release,
		chartRef,
		"--namespace", namespace,
	}
	if h.CreateNamespace {
		args = append(args, "--create-namespace")
	}
	if h.Version != "" {
		args = append(args, "--version", h.Version)
	}
	for _, f := range h.ValueFiles {
		if f != "" {
			args = append(args, "-f", f)
		}
	}
	for k, v := range h.Values {
		args = append(args, "--set", fmt.Sprintf("%s=%v", k, v))
	}
	args = append(args, "--wait", "--timeout", "5m")

	if _, err := command.Output(ctx, command.Args("helm", args...)); err != nil {
		return fmt.Errorf("helm install %s: %w", chartRef, err)
	}
	return nil
}

// HelmUninstall removes a release installed by HelmInstall.
func HelmUninstall(ctx context.Context, h HelmRelease) error {
	release := h.Name()
	namespace := h.EffectiveNamespace()
	if _, err := command.Output(ctx, command.Args("helm", "uninstall", release,
		"--namespace", namespace, "--ignore-not-found")); err != nil {
		return fmt.Errorf("helm uninstall %s: %w", release, err)
	}
	return nil
}

// WaitForResource polls until the described resource exists (and is ready when
// w.Ready is true). Times out after w.Timeout (default 30s).
func WaitForResource(ctx context.Context, w Wait) error {
	timeout := 30 * time.Second
	if w.Timeout != "" {
		if d, err := duration.Parse(w.Timeout); err == nil {
			timeout = d
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.Ready {
			var args []string
			if strings.EqualFold(w.Kind, "Deployment") {
				// Deployments — use rollout status.
				args = []string{"rollout", "status", "deployment/" + w.Name, "--timeout=5s"}
				if w.Namespace != "" {
					args = append(args, "-n", w.Namespace)
				}
			} else {
				args = []string{
					"wait",
					fmt.Sprintf("%s/%s", strings.ToLower(w.Kind), w.Name),
					"--for=condition=" + or(w.Condition, "Ready"),
					"--timeout=5s",
				}
				if w.Namespace != "" {
					args = append(args, "-n", w.Namespace)
				}
			}
			if command.Succeeds(ctx, command.Args("kubectl", args...)) {
				return nil
			}
		} else {
			// Just existence check: kubectl get
			args := []string{"get", w.Kind, w.Name, "--ignore-not-found", "-o", "name"}
			if w.Namespace != "" {
				args = append(args, "-n", w.Namespace)
			}
			out, err := command.Output(ctx, command.Args("kubectl", args...))
			if err == nil && out != "" {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	loc := w.Name
	if w.Namespace != "" {
		loc = w.Namespace + "/" + w.Name
	}
	return fmt.Errorf("timed out after %s waiting for %s %s", timeout, w.Kind, loc)
}
