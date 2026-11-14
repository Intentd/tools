package cluster

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/intentd/tools/command"
	"github.com/intentd/tools/term"
)

// buildImage builds one image with the runtime's build command:
// `docker build -f <dockerfile> -t <image> <context>` (or `podman build`).
func buildImage(ctx context.Context, rt containerRuntime, b Build, image string) error {
	// Builds run concurrently: their output is kept, not streamed, and
	// shown with the error when one fails.
	r, err := command.Run(ctx, command.Args(rt.String(), buildArgs(b, image)...))
	if err == nil {
		err = r.Err()
	}
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

// buildArgs is the runtime's build command: `build -f <dockerfile> -t <image>
// [--build-arg NAME=VALUE]... [--target <stage>] <context>`.
func buildArgs(b Build, image string) []string {
	args := []string{"build", "-f", b.DockerfilePath(), "-t", image}
	names := make([]string, 0, len(b.Args))
	for k := range b.Args {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		args = append(args, "--build-arg", k+"="+b.Args[k])
	}
	if b.Target != "" {
		args = append(args, "--target", b.Target)
	}
	return append(args, b.Context)
}

// buildWorkers bounds how many images build at once: builds are CPU and
// disk heavy, and BuildKit shares layers between those that run together.
var buildWorkers = min(4, max(1, runtime.NumCPU()/2))

// buildAll builds every image with a build block, buildWorkers at a time.
// Each build's output is kept and shown in the error when it fails, so
// concurrent builds don't interleave; the first failure cancels the others.
func (l imageLoader) buildAll(ctx context.Context, rt containerRuntime, images Images) error {
	var builds Images
	for _, img := range images {
		if img.Build != nil {
			builds = append(builds, img)
		}
	}
	if len(builds) == 0 {
		return nil
	}
	building := fmt.Sprintf("Building %s", plural(len(builds), "image"))
	spin := term.StartSpinner(building)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		built    []string
		firstErr error
	)
	workers := l.workers
	if workers <= 0 {
		workers = buildWorkers
	}
	sem := make(chan struct{}, workers)
	for _, img := range builds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			err := l.build(ctx, rt, *img.Build, img.Image)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("building %q: %w", img.Image, err)
					cancel()
				}
				return
			}
			built = append(built, img.Image)
			spin.Update(fmt.Sprintf("%s: %d/%d built", building, len(built), len(builds)))
		}()
	}
	wg.Wait()
	if firstErr != nil {
		spin.FailureWithUpdate(fmt.Sprintf("%s: failed (%d/%d built)", building, len(built), len(builds)))
		return firstErr
	}
	spin.Stop()
	for _, img := range builds {
		fmt.Printf("  %s %s built\n", term.SuccessMark(), img.Image)
	}
	spin = term.StartSpinner(building)
	spin.SuccessWithUpdate(fmt.Sprintf("%s built", plural(len(builds), "image")))
	return nil
}
