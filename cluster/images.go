package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/intentd/tools/structured"
	"github.com/intentd/tools/term"
	"gopkg.in/yaml.v3"
)

// ErrImageNotFound reports that an image is not present in the local
// container runtime.
var ErrImageNotFound = errors.New("image not found locally")

// LoadImages loads images into a kind cluster. It is kind-only: images are
// loaded with `kind load`.
//
// Every image is inspected in the local runtime before any image is loaded,
// so a missing image fails before the cluster is touched.
func LoadImages(ctx context.Context, clusterName string, images Images) error {
	return defaultLoader.load(ctx, clusterName, images)
}

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

// execer runs an external command. The default is exec.CommandContext; tests
// substitute it to record and stub commands.
type execer func(ctx context.Context, name string, args ...string) *exec.Cmd

// imageLoader is the seam that makes LoadImages testable without a container
// runtime or a cluster. The package-level defaultLoader is the real one.
type imageLoader struct {
	detect    func() (containerRuntime, error)
	build     func(ctx context.Context, rt containerRuntime, b Build, image string) error
	inspect   func(ctx context.Context, rt containerRuntime, image string) error
	loadImage func(ctx context.Context, rt containerRuntime, cluster, image string) error
}

var defaultLoader = imageLoader{
	detect:    detectRuntime,
	build:     buildImage,
	inspect:   inspectImage,
	loadImage: loadImage,
}

// Images is the ordered list of container images a run loads into the cluster.
type Images []Image

// Image is one entry in Images. It is either a bare image reference (the
// image already exists locally) or a build block that produces the image.
//
//	images:
//	  - local-bench:1.25
//	  - build:
//	      context: ./app
//	      dockerfile: Dockerfile.dev
//	    image: myapp:latest
type Image struct {
	// Image is the reference the entry loads or deploys.
	// For a build, it is also the reference the build produces.
	// Defaults to jig-<name>-<index> or jig-image-<index> when omitted on a build.
	Image string `yaml:"image,omitempty"`

	// Build, when set, builds the image before loading it.
	Build *Build `yaml:"build,omitempty"`

	// Deploy, when set, deploys this image as a Kubernetes Deployment
	// and exposes it through a ClusterIP Service.
	Deploy *ImageDeploy `yaml:"deploy,omitempty"`
}

// Build is the build block of an Image.
type Build struct {
	// Context is the build context directory.
	Context string `yaml:"context"`
	// Dockerfile is the Dockerfile path. Defaults to "Dockerfile".
	Dockerfile string `yaml:"dockerfile,omitempty"`
}

// ResolvePaths returns b with its paths resolved the way Docker Compose
// resolves them: Context relative to baseDir (the directory of the file that
// declares the build), and Dockerfile relative to Context. Absolute paths are
// kept as written. The Dockerfile defaults to <context>/Dockerfile.
//
// Resolve before building: `docker build -f` resolves a relative Dockerfile
// against the working directory, not the context.
func (b Build) ResolvePaths(baseDir string) Build {
	if !filepath.IsAbs(b.Context) {
		b.Context = filepath.Join(baseDir, b.Context)
	}
	b.Dockerfile = b.DockerfilePath()
	if !filepath.IsAbs(b.Dockerfile) {
		b.Dockerfile = filepath.Join(b.Context, b.Dockerfile)
	}
	return b
}

// ResolvePaths returns a copy of imgs with every build's paths resolved
// against baseDir; see Build.ResolvePaths. The caller's slice is not modified.
func (imgs Images) ResolvePaths(baseDir string) Images {
	out := make(Images, len(imgs))
	copy(out, imgs)
	for i := range out {
		if out[i].Build != nil {
			b := out[i].Build.ResolvePaths(baseDir)
			out[i].Build = &b
		}
	}
	return out
}

// DockerfilePath returns the Dockerfile to build with, defaulting to
// "Dockerfile".
func (b Build) DockerfilePath() string {
	if b.Dockerfile == "" {
		return "Dockerfile"
	}
	return b.Dockerfile
}

// UnmarshalYAML lets an Image be written as a bare string (the short form).
func (i *Image) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		i.Image = value.Value
		return nil
	}
	type plain Image
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return structured.StrictUnmarshal(raw, (*plain)(i))
}

// DefaultImageName is the image reference a build uses when the entry omits
// one: jig-<spec>-<index>, or jig-image-<index> when the spec has no name.
func (i *Image) DefaultImageName(specName string, index int) string {
	if specName != "" {
		return fmt.Sprintf("jig-%s-%d", specName, index)
	}
	return fmt.Sprintf("jig-image-%d", index)
}

// ResolveDefaultNames fills in default image references and deployment names.
// Image references default to jig-<spec>-<index>, or jig-image-<index>
// when the spec has no name. Deployment names default to the resolved image
// name without its registry, tag or digest.
// It returns the resolved list; the caller's slice is not modified.
func (imgs Images) ResolveDefaultNames(specName string) Images {
	out := make(Images, len(imgs))
	copy(out, imgs)

	for i := range out {
		img := &out[i]

		if img.Build != nil && img.Image == "" {
			img.Image = img.DefaultImageName(specName, i)
		}

		if img.Deploy != nil && img.Deploy.Name == "" {
			deploy := *img.Deploy
			deploy.Name = img.DefaultDeployName()
			img.Deploy = &deploy
		}
	}

	return out
}

// DefaultDeployName derives the default Kubernetes Deployment and Service
// name from an image reference by removing its registry, tag and digest.
func (i *Image) DefaultDeployName() string {
	if i.Image == "" {
		return ""
	}

	name := i.Image
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}

	return name
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

	// build
	buildCount := 0
	for _, img := range images {
		if img.Build != nil {
			buildCount++
		}
	}
	if buildCount > 0 {
		building := fmt.Sprintf("Building %s", plural(buildCount, "image"))
		spin := term.StartSpinner(building)
		built := 0
		for _, img := range images {
			if img.Build == nil {
				continue
			}
			spin.Update(fmt.Sprintf("%s: %s (%d/%d)", building, img.Image, built+1, buildCount))
			if err := l.build(ctx, rt, *img.Build, img.Image); err != nil {
				msg := fmt.Sprintf("Building %s: failed on %s", plural(buildCount, "image"), img.Image)
				if built > 0 {
					msg += fmt.Sprintf(" (%d/%d built)", built, buildCount)
				}
				spin.FailureWithUpdate(msg)
				return fmt.Errorf("building %q: %w", img.Image, err)
			}
			spin.Stop()
			fmt.Printf("  %s %s built\n", term.SuccessMark(), img.Image)
			built++
			spin = term.StartSpinner(building)
		}
		spin.SuccessWithUpdate(fmt.Sprintf("%s built", plural(built, "image")))
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

// plural is n and noun, plural unless n is 1: "1 image", "2 images".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

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
	return inspectImageWith(ctx, exec.CommandContext, rt, image)
}

func inspectImageWith(ctx context.Context, run execer, rt containerRuntime, image string) error {
	cmd := run(ctx, rt.String(), rt.inspectArgs(image)...)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%w: %s\n  pull it:  %s pull %s\n  or build: %s build -t %s <context>",
			ErrImageNotFound, msg, rt, image, rt, image)
	}
	return nil
}

// buildImage builds one image with the runtime's build command:
// `docker build -f <dockerfile> -t <image> <context>` (or `podman build`).
func buildImage(ctx context.Context, rt containerRuntime, b Build, image string) error {
	return buildImageWith(ctx, exec.CommandContext, rt, b, image)
}

func buildImageWith(ctx context.Context, run execer, rt containerRuntime, b Build, image string) error {
	cmd := run(ctx, rt.String(), buildArgs(b, image)...)
	cmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("build failed: %w\n  %s", err, msg)
	}
	return nil
}

// buildArgs is the runtime's build command: `build -f <dockerfile> -t <image> <context>`.
func buildArgs(b Build, image string) []string {
	return []string{"build", "-f", b.DockerfilePath(), "-t", image, b.Context}
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
	return loadImageWith(ctx, exec.CommandContext, rt, clusterName, image)
}

func loadImageWith(ctx context.Context, run execer, rt containerRuntime, clusterName, image string) error {
	if rt.saveArchive() {
		return loadArchiveWith(ctx, run, rt, clusterName, image)
	}
	return loadDockerPlatformArchiveWith(ctx, run, rt, clusterName, image)
}

// loadDockerPlatformArchiveWith saves image for the kind node's platform and
// loads the archive. The platform is detected from the node, not the host, so
// cross-platform development (e.g. Apple Silicon host, amd64 kind node) works.
func loadDockerPlatformArchiveWith(ctx context.Context, run execer, rt containerRuntime, clusterName, image string) error {
	platform, err := nodePlatformWith(ctx, run, clusterName)
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

	save := run(ctx, rt.String(),
		"image", "save",
		"--platform", platform,
		"--output", tarPath,
		image,
	)
	save.Stdout = os.Stdout
	save.Stderr = os.Stderr
	if err := save.Run(); err != nil {
		return fmt.Errorf("saving image with %s --platform %s: %w", rt, platform, err)
	}

	load := run(ctx, "kind", "load", "image-archive", tarPath, "--name", clusterName)
	load.Stdout = os.Stdout
	load.Stderr = os.Stderr
	return load.Run()
}

// loadArchiveWith saves image from the runtime to a temporary tarball and
// loads it into kind with `kind load image-archive`.
func loadArchiveWith(ctx context.Context, run execer, rt containerRuntime, clusterName, image string) error {
	f, err := os.CreateTemp("", "kind-image-*.tar")
	if err != nil {
		return fmt.Errorf("creating image archive: %w", err)
	}
	tarPath := f.Name()
	f.Close()
	defer os.Remove(tarPath)

	save := run(ctx, rt.String(), "save", "-o", tarPath, image)
	save.Stdout = os.Stdout
	save.Stderr = os.Stderr
	if err := save.Run(); err != nil {
		return fmt.Errorf("saving image with %s: %w", rt, err)
	}

	load := run(ctx, "kind", "load", "image-archive", tarPath, "--name", clusterName)
	load.Stdout = os.Stdout
	load.Stderr = os.Stderr
	return load.Run()
}

// nodePlatform returns the kind node's OS/arch as `linux/amd64` or
// `linux/arm64`. It asks the node, not the host, so the archive matches what
// the node can run.
func nodePlatformWith(ctx context.Context, run execer, clusterName string) (string, error) {
	node := clusterName + "-control-plane"
	cmd := run(ctx, "docker", "exec", node, "uname", "-s", "-m")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("uname in node %s: %w", node, err)
	}
	return parseUname(string(out))
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
