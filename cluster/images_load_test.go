package cluster

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/structured"
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
					return errImageNotFound
				}
				return nil
			},
			load: func(context.Context, containerRuntime, string, string) error {
				t.Fatal("load must not run when an image is missing")
				return nil
			},
			wantErr:   true,
			wantErrIs: errImageNotFound,
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
			var mu sync.Mutex
			l := imageLoader{
				detect: tc.detect,
				build: func(ctx context.Context, rt containerRuntime, b Build, img string) error {
					mu.Lock()
					order = append(order, "build:"+img)
					mu.Unlock()
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

// TestLoadImageDockerUsesPlatformArchive is the regression test for the
// containerd image store failure: the Docker path must save with --platform
// and load with image-archive, never `kind load docker-image`.
func TestLoadImageDockerUsesPlatformArchive(t *testing.T) {
	ctx := context.Background()

	var calls []string
	t.Cleanup(command.Fake(func(c command.Command) command.Result {
		calls = append(calls, c.String())
		if strings.Contains(c.String(), "uname") {
			return command.Result{Stdout: "Linux x86_64\n"}
		}
		return command.Result{}
	}))

	if err := loadImage(ctx, runtimeDocker, "test", "nginx:latest"); err != nil {
		t.Fatalf("loadImage: %v", err)
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
