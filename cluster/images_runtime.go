package cluster

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/intentd/tools/command"
)

// errImageNotFound reports that an image is not present in the local
// container runtime.
var errImageNotFound = errors.New("image not found locally")

// containerRuntime is the local container runtime: which binary holds the
// images, and how kind loads from it.
type containerRuntime int

const (
	runtimeUnknown containerRuntime = iota
	runtimeDocker
	runtimePodman
)

// String is the binary name, for messages and exec.
func (r containerRuntime) String() string {
	switch r {
	case runtimeDocker:
		return "docker"
	case runtimePodman:
		return "podman"
	}
	return "unknown"
}

// inspectArgs is the command that checks an image is present.
func (r containerRuntime) inspectArgs(image string) []string {
	return []string{"image", "inspect", image}
}

// saveArchive reports whether kind loads from this runtime through an image
// archive rather than `kind load docker-image`.
func (r containerRuntime) saveArchive() bool { return r == runtimePodman }

// detectRuntime returns the local container runtime, preferring Docker.
func detectRuntime() (containerRuntime, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return runtimeDocker, nil
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return runtimePodman, nil
	}
	return runtimeUnknown, errors.New("neither docker nor podman is in PATH; one is needed to inspect and load images")
}

func inspectImage(ctx context.Context, rt containerRuntime, image string) error {
	r, err := command.Run(ctx, command.Args(rt.String(), rt.inspectArgs(image)...))
	if err == nil {
		err = r.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(r.Stderr)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%w: %s\n  pull it:  %s pull %s\n  or build: %s build -t %s <context>",
			errImageNotFound, msg, rt, image, rt, image)
	}
	return nil
}
