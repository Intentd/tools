package cluster

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intentd/tools/structured"
	"github.com/intentd/tools/structured/structuredtest"
)

func TestImageLoaderLoad(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		images    Images
		detect    func() (containerRuntime, error)
		build     func(context.Context, containerRuntime, Build, string) error
		inspect   func(context.Context, containerRuntime, string) error
		load      func(context.Context, containerRuntime, string, string) error
		wantErr   bool
		wantErrIs error
		wantInErr string
		wantOrder []string // "build:<img>" / "inspect:<img>" / "load:<img>"
	}{
		{
			name:   "no images is a no-op",
			images: nil,
			detect: func() (containerRuntime, error) {
				t.Fatal("detect must not run with no images")
				return runtimeUnknown, nil
			},
		},
		{
			name:   "detect error is returned",
			images: Images{{Image: "a:1"}},
			detect: func() (containerRuntime, error) {
				return runtimeUnknown, errors.New("no runtime")
			},
			wantErr:   true,
			wantInErr: "no runtime",
		},
		{
			name:   "missing image fails before any load",
			images: Images{{Image: "a:1"}, {Image: "b:1"}},
			detect: func() (containerRuntime, error) { return runtimeDocker, nil },
			inspect: func(_ context.Context, _ containerRuntime, img string) error {
				if img == "b:1" {
					return ErrImageNotFound
				}
				return nil
			},
			load: func(context.Context, containerRuntime, string, string) error {
				t.Fatal("load must not run when an image is missing")
				return nil
			},
			wantErr:   true,
			wantErrIs: ErrImageNotFound,
		},
		{
			name:   "all images inspect then load in order",
			images: Images{{Image: "a:1"}, {Image: "b:1"}},
			detect: func() (containerRuntime, error) { return runtimeDocker, nil },
			inspect: func(_ context.Context, rt containerRuntime, _ string) error {
				if rt != runtimeDocker {
					t.Errorf("inspect runtime = %v, want docker", rt)
				}
				return nil
			},
			load: func(_ context.Context, rt containerRuntime, cluster, _ string) error {
				if rt != runtimeDocker || cluster != "test" {
					t.Errorf("load runtime/cluster = %v/%q, want docker/test", rt, cluster)
				}
				return nil
			},
			wantOrder: []string{"inspect:a:1", "inspect:b:1", "load:a:1", "load:b:1"},
		},
		{
			name:   "podman runtime is passed through unchanged",
			images: Images{{Image: "a:1"}},
			detect: func() (containerRuntime, error) { return runtimePodman, nil },
			inspect: func(_ context.Context, rt containerRuntime, _ string) error {
				if rt != runtimePodman {
					t.Errorf("inspect runtime = %v, want podman", rt)
				}
				return nil
			},
			load: func(_ context.Context, rt containerRuntime, _, _ string) error {
				if rt != runtimePodman {
					t.Errorf("load runtime = %v, want podman", rt)
				}
				return nil
			},
			wantOrder: []string{"inspect:a:1", "load:a:1"},
		},
		{
			name:    "load error names the image and cluster",
			images:  Images{{Image: "a:1"}},
			detect:  func() (containerRuntime, error) { return runtimeDocker, nil },
			inspect: func(context.Context, containerRuntime, string) error { return nil },
			load: func(context.Context, containerRuntime, string, string) error {
				return errors.New("boom")
			},
			wantErr:   true,
			wantInErr: `loading "a:1" into kind cluster "test"`,
		},
		{
			name: "build runs before inspect and load",
			images: Images{
				{Image: "prebuilt:1"},
				{Image: "built:1", Build: &Build{Context: "./app"}},
			},
			detect: func() (containerRuntime, error) { return runtimeDocker, nil },
			build: func(_ context.Context, rt containerRuntime, _ Build, img string) error {
				if rt != runtimeDocker {
					t.Errorf("build runtime = %v, want docker", rt)
				}
				return nil
			},
			inspect: func(context.Context, containerRuntime, string) error { return nil },
			load:    func(context.Context, containerRuntime, string, string) error { return nil },
			wantOrder: []string{
				"build:built:1",
				"inspect:prebuilt:1", "inspect:built:1",
				"load:prebuilt:1", "load:built:1",
			},
		},
		{
			name:   "build failure stops before any inspect or load",
			images: Images{{Image: "built:1", Build: &Build{Context: "./app"}}},
			detect: func() (containerRuntime, error) { return runtimeDocker, nil },
			build: func(context.Context, containerRuntime, Build, string) error {
				return errors.New("build boom")
			},
			inspect: func(context.Context, containerRuntime, string) error {
				t.Fatal("inspect must not run after a build failure")
				return nil
			},
			load: func(context.Context, containerRuntime, string, string) error {
				t.Fatal("load must not run after a build failure")
				return nil
			},
			wantErr:   true,
			wantInErr: "build boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var order []string
			l := imageLoader{
				detect: tc.detect,
				build: func(ctx context.Context, rt containerRuntime, b Build, img string) error {
					order = append(order, "build:"+img)
					if tc.build != nil {
						return tc.build(ctx, rt, b, img)
					}
					return nil
				},
				inspect: func(ctx context.Context, rt containerRuntime, img string) error {
					order = append(order, "inspect:"+img)
					if tc.inspect != nil {
						return tc.inspect(ctx, rt, img)
					}
					return nil
				},
				loadImage: func(ctx context.Context, rt containerRuntime, cluster, img string) error {
					order = append(order, "load:"+img)
					if tc.load != nil {
						return tc.load(ctx, rt, cluster, img)
					}
					return nil
				},
			}
			err := l.load(ctx, "test", tc.images)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Errorf("error = %v, want Is %v", err, tc.wantErrIs)
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantInErr)
				}
				if tc.wantOrder != nil && !structured.StringSliceEqual(order, tc.wantOrder) {
					t.Errorf("order = %v, want %v", order, tc.wantOrder)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantOrder != nil && !structured.StringSliceEqual(order, tc.wantOrder) {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestDetectRuntime(t *testing.T) {
	tests := []struct {
		name    string
		bins    []string
		want    containerRuntime
		wantErr bool
	}{
		{name: "none", wantErr: true},
		{name: "docker only", bins: []string{"docker"}, want: runtimeDocker},
		{name: "podman only", bins: []string{"podman"}, want: runtimePodman},
		{name: "both prefers docker", bins: []string{"docker", "podman"}, want: runtimeDocker},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, b := range tc.bins {
				structuredtest.WriteExecutable(t, filepath.Join(dir, b))
			}
			t.Setenv("PATH", dir)
			got, err := detectRuntime()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("runtime = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestContainerRuntimeMethods(t *testing.T) {
	tests := []struct {
		rt          containerRuntime
		wantString  string
		wantInspect []string
		wantArchive bool
	}{
		{runtimeDocker, "docker", []string{"image", "inspect", "app:1"}, false},
		{runtimePodman, "podman", []string{"image", "inspect", "app:1"}, true},
		{runtimeUnknown, "unknown", []string{"image", "inspect", "app:1"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.wantString, func(t *testing.T) {
			if got := tc.rt.String(); got != tc.wantString {
				t.Errorf("String() = %q, want %q", got, tc.wantString)
			}
			if got := tc.rt.inspectArgs("app:1"); !structured.StringSliceEqual(got, tc.wantInspect) {
				t.Errorf("inspectArgs() = %v, want %v", got, tc.wantInspect)
			}
			if got := tc.rt.saveArchive(); got != tc.wantArchive {
				t.Errorf("saveArchive() = %v, want %v", got, tc.wantArchive)
			}
		})
	}
}

// TestLoadImageDockerUsesPlatformArchive is the regression test for the
// containerd image store failure: the Docker path must save with --platform
// and load with image-archive, never `kind load docker-image`.
func TestLoadImageDockerUsesPlatformArchive(t *testing.T) {
	ctx := context.Background()

	var calls []string
	run := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if strings.Contains(name+" "+strings.Join(args, " "), "uname") {
			// Return a command whose stdout is "Linux x86_64".
			return exec.CommandContext(ctx, "echo", "Linux", "x86_64")
		}
		return exec.CommandContext(ctx, "true")
	}

	if err := loadImageWith(ctx, run, runtimeDocker, "test", "nginx:latest"); err != nil {
		t.Fatalf("loadImageWith: %v", err)
	}

	for _, c := range calls {
		if strings.Contains(c, "kind load docker-image") {
			t.Errorf("Docker path called kind load docker-image: %s", c)
		}
	}
	if !containsCall(calls, "docker exec test-control-plane uname -s -m") {
		t.Errorf("missing node platform detection; calls: %v", calls)
	}
	if !containsCall(calls, "docker image save --platform linux/amd64") {
		t.Errorf("missing platform-narrowed save; calls: %v", calls)
	}
	if !containsCall(calls, "kind load image-archive") {
		t.Errorf("missing image-archive load; calls: %v", calls)
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name  string
		build Build
		image string
		want  []string
	}{
		{
			name:  "explicit dockerfile",
			build: Build{Context: "./app", Dockerfile: "Dockerfile.dev"},
			image: "myapp:latest",
			want:  []string{"build", "-f", "Dockerfile.dev", "-t", "myapp:latest", "./app"},
		},
		{
			name:  "default dockerfile",
			build: Build{Context: "./app"},
			image: "myapp:latest",
			want:  []string{"build", "-f", "Dockerfile", "-t", "myapp:latest", "./app"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildArgs(tc.build, tc.image); !structured.StringSliceEqual(got, tc.want) {
				t.Errorf("buildArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Paths resolve the way Docker Compose resolves them: context against the
// declaring file's directory, dockerfile against the context.
func TestBuildResolvePaths(t *testing.T) {
	tests := []struct {
		name  string
		build Build
		want  Build
	}{
		{
			name:  "relative context, default dockerfile",
			build: Build{Context: "./app"},
			want:  Build{Context: "/spec/app", Dockerfile: "/spec/app/Dockerfile"},
		},
		{
			name:  "dockerfile is relative to the context",
			build: Build{Context: "./app", Dockerfile: "docker/Dockerfile.dev"},
			want:  Build{Context: "/spec/app", Dockerfile: "/spec/app/docker/Dockerfile.dev"},
		},
		{
			name:  "dockerfile may climb out of the context",
			build: Build{Context: "./app", Dockerfile: "../Dockerfile"},
			want:  Build{Context: "/spec/app", Dockerfile: "/spec/Dockerfile"},
		},
		{
			name:  "absolute dockerfile is kept",
			build: Build{Context: "./app", Dockerfile: "/df/Dockerfile"},
			want:  Build{Context: "/spec/app", Dockerfile: "/df/Dockerfile"},
		},
		{
			name:  "absolute context is kept",
			build: Build{Context: "/src", Dockerfile: "Dockerfile.dev"},
			want:  Build{Context: "/src", Dockerfile: "/src/Dockerfile.dev"},
		},
		{
			name:  "empty context is the spec directory",
			build: Build{},
			want:  Build{Context: "/spec", Dockerfile: "/spec/Dockerfile"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.build.ResolvePaths("/spec"); got != tc.want {
				t.Errorf("ResolvePaths() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestImagesResolvePaths_DoesNotModifyCaller(t *testing.T) {
	imgs := Images{{Image: "nginx:1"}, {Image: "app:1", Build: &Build{Context: "./app"}}}
	got := imgs.ResolvePaths("/spec")
	if got[0].Build != nil {
		t.Errorf("a pre-built image gained a build: %+v", got[0])
	}
	if got[1].Build.Dockerfile != "/spec/app/Dockerfile" {
		t.Errorf("build = %+v", got[1].Build)
	}
	if imgs[1].Build.Context != "./app" || imgs[1].Build.Dockerfile != "" {
		t.Errorf("caller's build was modified: %+v", imgs[1].Build)
	}
}

func TestBuildImageWith(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var calls []string
		run := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return exec.CommandContext(ctx, "true")
		}
		err := buildImageWith(ctx, run, runtimeDocker, Build{Context: "./app"}, "myapp:latest")
		if err != nil {
			t.Fatalf("buildImageWith: %v", err)
		}
		if len(calls) != 1 || !strings.HasPrefix(calls[0], "docker build ") {
			t.Errorf("calls = %v, want one docker build", calls)
		}
	})

	t.Run("failure names the command", func(t *testing.T) {
		run := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// A command that fails: `false`.
			return exec.CommandContext(ctx, "false")
		}
		err := buildImageWith(ctx, run, runtimeDocker, Build{Context: "./app"}, "myapp:latest")
		if err == nil || !strings.Contains(err.Error(), "build failed") {
			t.Errorf("error = %v, want it to mention the build failure", err)
		}
	})
}

func TestParseUname(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Linux x86_64\n", "linux/amd64", false},
		{"Linux aarch64\n", "linux/arm64", false},
		{"Linux arm64\n", "linux/arm64", false},
		{"garbage", "", true},
	}
	for _, tc := range tests {
		got, err := parseUname(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseUname(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseUname(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseUname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// helpers
func containsCall(calls []string, sub string) bool {
	for _, c := range calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}
