package cluster

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/term"
)

// imageLoader is the seam that makes LoadImages testable without a container
// runtime or a cluster. The package-level defaultLoader is the real one.
type imageLoader struct {
	detect    func() (containerRuntime, error)
	build     func(ctx context.Context, rt containerRuntime, b Build, image string) error
	inspect   func(ctx context.Context, rt containerRuntime, image string) error
	loadImage func(ctx context.Context, rt containerRuntime, cluster, image string) error
	// workers bounds concurrent builds; 0 is buildWorkers.
	workers int
}

var defaultLoader = imageLoader{
	detect:    detectRuntime,
	build:     buildImage,
	inspect:   inspectImage,
	loadImage: loadImage,
}

// LoadImages loads images into a kind cluster. It is kind-only: images are
// loaded with `kind load`.
//
// Every image is inspected in the local runtime before any image is loaded,
// so a missing image fails before the cluster is touched.
func LoadImages(ctx context.Context, clusterName string, images Images, opts ...LoadOption) error {
	l := defaultLoader
	for _, o := range opts {
		o(&l)
	}
	return l.load(ctx, clusterName, images)
}

// LoadOption configures LoadImages.
type LoadOption func(*imageLoader)

// WithBuildWorkers sets how many images build at once: 1 builds them one
// after another; 0 or less keeps the default (buildWorkers).
func WithBuildWorkers(n int) LoadOption {
	return func(l *imageLoader) {
		if n > 0 {
			l.workers = n
		}
	}
}

func (l imageLoader) load(ctx context.Context, clusterName string, images Images) error {
	count := len(images)
	if count == 0 {
		return nil
	}
	rt, err := l.detect()
	if err != nil {
		return err
	}

	// build: independent of each other, so several at once; the first
	// failure cancels the rest.
	if err := l.buildAll(ctx, rt, images); err != nil {
		return err
	}

	// inspect
	for _, img := range images {
		if err := l.inspect(ctx, rt, img.Image); err != nil {
			return fmt.Errorf("%q: %w", img.Image, err)
		}
	}

	// load
	loading := fmt.Sprintf("Loading %s", plural(count, "image"))
	spin := term.StartSpinner(loading)
	loaded := 0
	for _, img := range images {
		spin.Update(fmt.Sprintf("%s: %s (%d/%d)", loading, img.Image, loaded+1, count))
		err := l.loadImage(ctx, rt, clusterName, img.Image)
		if err != nil {
			msg := fmt.Sprintf("Loading %s: failed on %s", plural(count, "image"), img.Image)
			if loaded > 0 {
				msg += fmt.Sprintf(" (%d/%d loaded)", loaded, count)
			}
			spin.FailureWithUpdate(msg)
			return fmt.Errorf("loading %q into kind cluster %q: %w", img.Image, clusterName, err)
		}
		spin.Stop()
		fmt.Printf("  %s %s loaded\n", term.SuccessMark(), img.Image)
		loaded++
		spin = term.StartSpinner(loading)
	}
	spin.SuccessWithUpdate(fmt.Sprintf("%s loaded", plural(loaded, "image")))
	return nil
}

// loadImage loads one image into a kind cluster.
//
// Docker: save a single-platform archive with `docker image save --platform`
// and load it with `kind load image-archive`. This avoids the
// `ctr images import --all-platforms` failure that occurs when Docker uses
// the containerd image store and the image is multi-platform: `docker save`
// exports the index but not the layers of platforms that were never pulled.
//
// Podman: save to a tarball and load it with `kind load image-archive`.
func loadImage(ctx context.Context, rt containerRuntime, clusterName, image string) error {
	if rt.saveArchive() {
		return loadArchive(ctx, rt, clusterName, image)
	}
	return loadDockerPlatformArchive(ctx, rt, clusterName, image)
}

// runStreamed runs c with its output on stdout, failing on a non-zero exit.
func runStreamed(ctx context.Context, c command.Command) error {
	c.Stream = os.Stdout
	return command.Exec(ctx, c)
}

// loadDockerPlatformArchive saves image for the kind node's platform and
// loads the archive. The platform is detected from the node, not the host, so
// cross-platform development (e.g. Apple Silicon host, amd64 kind node) works.
func loadDockerPlatformArchive(ctx context.Context, rt containerRuntime, clusterName, image string) error {
	platform, err := nodePlatform(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("detecting kind node platform: %w", err)
	}

	f, err := os.CreateTemp("", "kind-image-*.tar")
	if err != nil {
		return fmt.Errorf("creating image archive: %w", err)
	}
	tarPath := f.Name()
	f.Close()
	defer os.Remove(tarPath)

	save := command.Args(rt.String(), "image", "save", "--platform", platform, "--output", tarPath, image)
	if err := runStreamed(ctx, save); err != nil {
		return fmt.Errorf("saving image with %s --platform %s: %w", rt, platform, err)
	}
	return runStreamed(ctx, command.Args("kind", "load", "image-archive", tarPath, "--name", clusterName))
}

// loadArchive saves image from the runtime to a temporary tarball and
// loads it into kind with `kind load image-archive`.
func loadArchive(ctx context.Context, rt containerRuntime, clusterName, image string) error {
	f, err := os.CreateTemp("", "kind-image-*.tar")
	if err != nil {
		return fmt.Errorf("creating image archive: %w", err)
	}
	tarPath := f.Name()
	f.Close()
	defer os.Remove(tarPath)

	if err := runStreamed(ctx, command.Args(rt.String(), "save", "-o", tarPath, image)); err != nil {
		return fmt.Errorf("saving image with %s: %w", rt, err)
	}
	return runStreamed(ctx, command.Args("kind", "load", "image-archive", tarPath, "--name", clusterName))
}

// nodePlatform returns the kind node's OS/arch as `linux/amd64` or
// `linux/arm64`. It asks the node, not the host, so the archive matches what
// the node can run.
func nodePlatform(ctx context.Context, clusterName string) (string, error) {
	node := clusterName + "-control-plane"
	out, err := command.Output(ctx, command.Args("docker", "exec", node, "uname", "-s", "-m"))
	if err != nil {
		return "", fmt.Errorf("uname in node %s: %w", node, err)
	}
	return parseUname(out)
}

// parseUname turns "Linux x86_64" into "linux/amd64".
func parseUname(out string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return "", fmt.Errorf("unexpected uname output: %q", out)
	}
	osName := strings.ToLower(fields[0])
	arch := fields[1]
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	}
	return osName + "/" + arch, nil
}
