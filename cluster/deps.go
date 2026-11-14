package cluster

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/term"
)

const (
	getHelm               = "curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash"
	getKubectl            = "curl -LO https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl && "
	makeKubectlExecutable = "chmod +x kubectl && sudo mv kubectl /usr/local/bin/"
)

// KubectlAvailable reports whether kubectl is present in PATH.
func KubectlAvailable() bool {
	_, err := exec.LookPath("kubectl")
	return err == nil
}

// HelmAvailable reports whether helm is present in PATH.
func HelmAvailable() bool {
	_, err := exec.LookPath("helm")
	return err == nil
}

// ClusterReachable reports whether kubectl can reach the cluster configured
// in the current kubeconfig. Times out after 5 seconds.
func ClusterReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return command.Succeeds(ctx, command.Args("kubectl", "cluster-info", "--request-timeout=5s"))
}

// CheckContext reports whether the named context exists in the current kubeconfig.
func CheckContext(name string) bool {
	if name == "" {
		return false
	}

	return command.Succeeds(context.Background(), command.Args("kubectl", "config", "get-contexts", name, "-o", "name"))
}

// EnsureDependencies installs kubectl and helm if they are missing.
func EnsureDependencies() error {
	if !KubectlAvailable() {
		spin := term.StartSpinner("  → Installing kubectl...")
		if err := installKubectl(); err != nil {
			spin.Failure()
			return fmt.Errorf("failed to install kubectl: %w", err)
		}
		spin.Success()
	}

	if !HelmAvailable() {
		spin := term.StartSpinner("  → Installing helm...")
		if err := installHelm(); err != nil {
			spin.Failure()
			return fmt.Errorf("failed to install helm: %w", err)
		}
		spin.Success()
	}
	return nil
}

func installKubectl() error {
	_, err := command.Output(context.Background(), command.Shell(getKubectl+makeKubectlExecutable))
	return err
}

func installHelm() error {
	_, err := command.Output(context.Background(), command.Shell(getHelm))
	return err
}
