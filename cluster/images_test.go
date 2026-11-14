package cluster

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			if got := tc.build.ResolvePaths("/spec"); !reflect.DeepEqual(got, tc.want) {
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

// An image's name is what needs refer to: its deploy name, the derived one
// when neither is set, its repository's for one from git; a declared name
// names the Deployment too.
func TestResolveDefaultNames_Name(t *testing.T) {
	got := Images{
		{Image: "ghcr.io/acme/auth:dev", Deploy: &ImageDeploy{Ports: Ports{{Port: 8080}}}},
		{Name: "api", Image: "acme/api-server:dev", Deploy: &ImageDeploy{Ports: Ports{{Port: 8090}}}},
		{Image: "worker:dev"},
		{Git: &Git{URL: "git@github.com:acme/payments-api.git"}, Build: &Build{Context: "."}, Deploy: &ImageDeploy{Ports: Ports{{Port: 80}}}},
	}.ResolveDefaultNames("shop")
	assert.Equal(t, "auth", got[0].Name)
	assert.Equal(t, "auth", got[0].Deploy.Name)
	assert.Equal(t, "api", got[1].Name)
	assert.Equal(t, "api", got[1].Deploy.Name)
	assert.Equal(t, "worker", got[2].Name)
	assert.Equal(t, "payments-api", got[3].Name, "named for its repository")
	assert.Equal(t, "payments-api", got[3].Deploy.Name)
	assert.Equal(t, "jig-shop-3", got[3].Image)
}

// needs: [api] is every row of api; a row or a plain image by its own name
// is itself; an unknown name stays for the caller to report.
func TestImagesNeeds(t *testing.T) {
	imgs := Images{
		{Name: "api-0-0", Row: &ImageRow{Base: "api"}},
		{Name: "api-0-1", Row: &ImageRow{Index: 1, Base: "api"}},
		{Name: "auth"},
	}
	web := Image{Needs: []string{"api", "auth", "api-0-1", "ghost"}}
	assert.Equal(t, []string{"api-0-0", "api-0-1", "auth", "api-0-1", "ghost"}, imgs.Needs(web))
}
