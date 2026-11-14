package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/intentd/tools/command"
)

const (
	metricsInstallURL = "https://github.com/kubernetes-sigs/metrics-server#installation"
	metricsHelmRepo   = "https://kubernetes-sigs.github.io/metrics-server/"
)

// EnsureMetricsServer ensures the Kubernetes metrics-server is installed.
// Checks for the metrics-server Deployment in kube-system; installs via Helm
// if missing, with --kubelet-insecure-tls when running on kind. Failures are
// non-fatal — the user is prompted to install manually instead.
func EnsureMetricsServer() error {
	ctx := context.Background()
	installed := command.Args("kubectl", "get", "deployment", "metrics-server", "-n", "kube-system", "-o", "json")
	if command.Succeeds(ctx, installed) {
		return nil
	}

	spin := startSpinner("Installing metrics-server (required by kubectl.top)...")

	ctxOut, err := command.Output(ctx, command.Args("kubectl", "config", "current-context"))
	if err != nil {
		spin.Failure()
		fmt.Printf("  %s Could not determine current context: %v\n", warningMark(), err)
		fmt.Printf("  %s Install metrics-server manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}
	isKind := strings.Contains(ctxOut, "kind")

	if _, err := command.Output(ctx, command.Args("helm", "repo", "add", "metrics-server", metricsHelmRepo)); err != nil {
		spin.Failure()
		fmt.Printf("  %s Failed to add Helm repo: %v\n", warningMark(), err)
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}
	_, _ = command.Run(ctx, command.Args("helm", "repo", "update"))

	args := []string{
		"upgrade", "--install", "metrics-server",
		"metrics-server/metrics-server",
		"--namespace", "kube-system",
		"--wait", "--timeout=120s",
	}
	if isKind {
		args = append(args, "--set", "args={--kubelet-insecure-tls}")
	}
	if _, err := command.Output(ctx, command.Args("helm", args...)); err != nil {
		spin.Failure()
		fmt.Printf("  %s Failed to install metrics-server: %v\n", warningMark(), err)
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}

	if !command.Succeeds(ctx, installed) {
		spin.Failure()
		fmt.Printf("  %s metrics-server install could not be verified\n", warningMark())
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}

	spin.Success()
	return nil
}
