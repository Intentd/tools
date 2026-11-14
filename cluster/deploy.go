package cluster

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/term"
)

var (
	readLocal    = fileio.ReadLocal
	startSpinner = term.StartSpinner
	warningMark  = term.WarningMark
	infoMark     = term.InfoMark
)

// DeploymentHealthChecker handles health checks for any deployment
type DeploymentHealthChecker struct {
	Name      string
	Namespace string
}

// DeploymentStatus represents the status of a Kubernetes deployment
type DeploymentStatus struct {
	Running bool
	Reason  string // set when Running is false
}

// SyncDeployment restarts a Kubernetes Deployment and waits for the rollout to complete.
// Returns an error if the restart or rollout status check fails.
// Timeout is configurable (e.g., 3*time.Minute).
func SyncDeployment(deploymentName, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if _, err := command.Output(ctx, command.Args("kubectl", "rollout", "restart",
		"deploy/"+deploymentName, "-n", namespace)); err != nil {
		return fmt.Errorf("restarting deployment %s/%s: %w", namespace, deploymentName, err)
	}

	if _, err := command.Output(ctx, command.Args("kubectl", "rollout", "status",
		"deploy/"+deploymentName, "-n", namespace)); err != nil {
		return fmt.Errorf("waiting for rollout of %s/%s: %w", namespace, deploymentName, err)
	}

	return nil
}

// resourceExistsTimeout bounds one ResourceExists call.
const resourceExistsTimeout = 10 * time.Second

// ResourceExists reports whether resourceType/resourceName exists in
// namespace, with resourceType as kubectl names it ("deploy", "service").
func ResourceExists(resourceType, resourceName, namespace string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), resourceExistsTimeout)
	defer cancel()

	out, err := command.Output(ctx, command.Args("kubectl", "get", resourceType, resourceName,
		"-n", namespace, "--ignore-not-found", "-o", "name"))

	return err == nil && out != ""
}

// CheckHealth waits up to timeout for a deployment to have at least one ready replica.
// Polls every 2 seconds. Returns immediately if a custom failure condition is detected.
func (d DeploymentHealthChecker) CheckHealth(timeout time.Duration, checkFailure func(ctx context.Context) string) DeploymentStatus {
	spin := term.StartSpinner(fmt.Sprintf("  → Checking %s health...", d.Name))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			spin.Failure()
			return DeploymentStatus{
				Reason: fmt.Sprintf("timeout (%s) waiting for %s to become ready", timeout, d.Name),
			}
		case <-ticker.C:
			status := d.checkOnce(ctx)
			if status.Running {
				spin.Success()
				return status
			}
			// Only fail fast on crashloop — everything else
			// ("not found", "no ready replicas") is transient and should
			// keep polling until the timeout expires.
			if checkFailure != nil {
				if reason := checkFailure(ctx); reason != "" {
					spin.Failure()
					return DeploymentStatus{Reason: reason}
				}
			}
		}
	}
}

// checkOnce performs a single health check on the deployment
func (d DeploymentHealthChecker) checkOnce(ctx context.Context) DeploymentStatus {
	// Check if deployment exists
	nameOut, err := command.Output(ctx, command.Args(
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "--ignore-not-found", "-o", "name",
	))
	if err != nil || nameOut == "" {
		return DeploymentStatus{
			Reason: fmt.Sprintf("deployment %s not found in %s", d.Name, d.Namespace),
		}
	}

	// Check ready replicas
	ready, _ := command.Output(ctx, command.Args(
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "-o", `jsonpath={.status.readyReplicas}`,
	))
	if ready == "" || ready == "0" {
		return DeploymentStatus{
			Reason: fmt.Sprintf("no ready replicas for %s/%s", d.Namespace, d.Name),
		}
	}
	return DeploymentStatus{Running: true}
}

// FetchLogs saves the last maxLines from the deployment to a file and returns the last 10 lines
func (d DeploymentHealthChecker) FetchLogs(maxLines int, logDir, logPath string) (tail string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("creating log dir: %w", err)
	}

	logs := fetchDeployLogs(ctx, d.Name, d.Namespace, maxLines)
	if err := os.WriteFile(logPath, []byte(logs), 0644); err != nil {
		return "", fmt.Errorf("writing %s log: %w", d.Name, err)
	}

	lines := strings.Split(strings.TrimRight(logs, "\n"), "\n")
	if start := len(lines) - 10; start > 0 {
		lines = lines[start:]
	}
	return strings.Join(lines, "\n"), nil
}

// Exists checks if the deployment exists
func (d DeploymentHealthChecker) Exists() bool {
	return ResourceExists("deploy", d.Name, d.Namespace)
}

// HasReadyReplicas checks if the deployment has at least one ready replica
func (d DeploymentHealthChecker) HasReadyReplicas(ctx context.Context) bool {
	ready, err := command.Output(ctx, command.Args(
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "-o", `jsonpath={.status.readyReplicas}`,
	))
	if err != nil {
		return false
	}
	return ready != "" && ready != "0"
}

func fetchDeployLogs(ctx context.Context, deploy, ns string, tailLines int) string {
	r, err := command.Run(ctx, command.Args("kubectl", "logs",
		"deploy/"+deploy, "-n", ns, fmt.Sprintf("--tail=%d", tailLines)))
	if err == nil {
		err = r.Err()
	}
	if err != nil {
		return fmt.Sprintf("[failed to fetch logs for %s: %v]", deploy, err)
	}
	return r.Output
}
