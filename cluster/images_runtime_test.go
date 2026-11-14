package cluster

import (
	"path/filepath"
	"testing"

	"github.com/intentd/tools/structured"
	"github.com/intentd/tools/structured/structuredtest"
)

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
