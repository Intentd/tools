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
		{
			name:  "args in name order, then the target",
			build: Build{Context: "./app", Args: map[string]string{"VERSION": "1.2", "GO_VERSION": "1.27"}, Target: "runtime"},
			image: "myapp:latest",
			want: []string{"build", "-f", "Dockerfile", "-t", "myapp:latest",
				"--build-arg", "GO_VERSION=1.27", "--build-arg", "VERSION=1.2", "--target", "runtime", "./app"},
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

func TestBuildImage(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var calls []string
		t.Cleanup(command.Fake(func(c command.Command) command.Result {
			calls = append(calls, c.String())
			return command.Result{}
		}))
		err := buildImage(ctx, runtimeDocker, Build{Context: "./app"}, "myapp:latest")
		if err != nil {
			t.Fatalf("buildImage: %v", err)
		}
		if len(calls) != 1 || !strings.HasPrefix(calls[0], "docker build ") {
			t.Errorf("calls = %v, want one docker build", calls)
		}
	})

	t.Run("failure names the command and its output", func(t *testing.T) {
		t.Cleanup(command.Fake(func(command.Command) command.Result {
			return command.Result{Output: "no such file: Dockerfile", ExitCode: 1}
		}))
		err := buildImage(ctx, runtimeDocker, Build{Context: "./app"}, "myapp:latest")
		if err == nil || !strings.Contains(err.Error(), "build failed") || !strings.Contains(err.Error(), "no such file") {
			t.Errorf("error = %v, want the build failure and its output", err)
		}
	})
}

// Builds run together, at most buildWorkers at a time; a failure cancels
// the others and nothing is inspected or loaded.
func TestImageLoaderBuildsConcurrently(t *testing.T) {
	old := buildWorkers
	buildWorkers = 2
	t.Cleanup(func() { buildWorkers = old })

	images := Images{
		{Image: "a:1", Build: &Build{Context: "."}},
		{Image: "b:1", Build: &Build{Context: "."}},
		{Image: "c:1", Build: &Build{Context: "."}},
	}
	var mu sync.Mutex
	running, peak := 0, 0
	release := make(chan struct{})
	l := imageLoader{
		detect: func() (containerRuntime, error) { return runtimeDocker, nil },
		build: func(ctx context.Context, _ containerRuntime, _ Build, _ string) error {
			mu.Lock()
			running++
			peak = max(peak, running)
			mu.Unlock()
			<-release
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		},
		inspect:   func(context.Context, containerRuntime, string) error { return nil },
		loadImage: func(context.Context, containerRuntime, string, string) error { return nil },
	}
	done := make(chan error)
	go func() { done <- l.load(context.Background(), "test", images) }()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak > 2 {
		t.Errorf("%d builds ran at once, want at most 2", peak)
	}

	// Every build has a slot here, so the failing one runs while the
	// others wait to be cancelled.
	buildWorkers = len(images)
	failing := l
	failing.build = func(ctx context.Context, _ containerRuntime, _ Build, img string) error {
		if img == "b:1" {
			return errors.New("b broke")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	failing.inspect = func(context.Context, containerRuntime, string) error {
		t.Fatal("inspect must not run after a build failure")
		return nil
	}
	err := failing.load(context.Background(), "test", images)
	if err == nil || !strings.Contains(err.Error(), `building "b:1": b broke`) {
		t.Fatalf("error = %v, want b's failure", err)
	}
}
