package cluster

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/structured"
	"gopkg.in/yaml.v3"
)

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
	// Name is how other images' needs refer to this one. Defaults to its
	// Deployment's name.
	Name string `yaml:"name,omitempty"`

	// Image is the reference the entry loads or deploys.
	// For a build, it is also the reference the build produces.
	// Defaults to jig-<name>-<index> or jig-image-<index> when omitted on a build.
	Image string `yaml:"image,omitempty"`

	// Build, when set, builds the image before loading it.
	Build *Build `yaml:"build,omitempty"`

	// Deploy, when set, deploys this image as a Kubernetes Deployment
	// and exposes it through a ClusterIP Service.
	Deploy *ImageDeploy `yaml:"deploy,omitempty"`

	// Git is the repository the build comes from: build.context and
	// dockerfile resolve inside its checkout. Without it, they resolve
	// from the file that declares the image.
	Git *Git `yaml:"git,omitempty"`

	// Needs are the images this one's Deployment comes after: each is
	// deployed and ready first, as GitHub Actions jobs need others.
	Needs []string `yaml:"needs,omitempty"`

	// Include names a file whose images replace this entry, with their
	// build paths resolved from that file. It stands alone: an entry with
	// include sets nothing else. The caller expands it before using the list.
	Include string `yaml:"include,omitempty"`

	// ForEach repeats the entry once per row (Images.ExpandForEach): until
	// then its other values are templates that read .item, kept as written.
	ForEach *structured.ForEach `yaml:"forEach,omitempty"`

	// Row is where an entry ExpandForEach made came from; nil for an entry
	// written as it is.
	Row *ImageRow `yaml:"-"`

	// Dir is the directory a forEach entry's relative paths resolve from,
	// for one that came from an include file: its rows are decoded after
	// the include, so they resolve them then.
	Dir string `yaml:"-"`

	// template is a forEach entry as written, without forEach.
	template *yaml.Node
}

// ImageRow is where an expanded entry came from: the entry's index in its
// list, the row's index, and the entry's base name, which needs uses to
// mean every row.
type ImageRow struct {
	Entry, Index int
	Base         string
}

// Git is where an image's build comes from: a remote repository, cloned,
// or a local one, used as it is or at a branch or ref.
type Git struct {
	// URL is a remote repository (https://…, git@host:org/repo.git) or a
	// local path to one.
	URL string `yaml:"url"`
	// Branch or Ref picks what to build; neither is the default branch
	// (remote) or the working tree as it is (local).
	Branch string `yaml:"branch,omitempty"`
	Ref    string `yaml:"ref,omitempty"`
	// Pull says when a remote clone is fetched: Always (the default: every
	// run, so a branch is its latest commit) or IfNotPresent (a cached
	// clone is used as it is). A local repository is never pulled.
	Pull string `yaml:"pull,omitempty"`
}

// Pull policies for Git.Pull, in Kubernetes' words.
const (
	PullAlways       = "Always"
	PullIfNotPresent = "IfNotPresent"
)

// Build is the build block of an Image.
type Build struct {
	// Context is the build context directory.
	Context string `yaml:"context"`
	// Dockerfile is the Dockerfile path. Defaults to "Dockerfile".
	Dockerfile string `yaml:"dockerfile,omitempty"`
	// Args are the Dockerfile's ARG values, as Docker Compose's build.args:
	// --build-arg NAME=VALUE, in name order.
	Args map[string]string `yaml:"args,omitempty"`
	// Target is the stage of a multi-stage Dockerfile to build: --target.
	Target string `yaml:"target,omitempty"`
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
	// A forEach entry is kept as written: its values are templates (a port
	// "{{ .item.port }}") that decode only once a row resolves them.
	if value.Kind == yaml.MappingNode {
		for k := 0; k+1 < len(value.Content); k += 2 {
			if value.Content[k].Value != "forEach" {
				continue
			}
			var f structured.ForEach
			raw, err := yaml.Marshal(value.Content[k+1])
			if err != nil {
				return err
			}
			if err := structured.StrictUnmarshal(raw, &f); err != nil {
				return fmt.Errorf("forEach: %w", err)
			}
			tmpl := *value
			tmpl.Content = append(append([]*yaml.Node{}, value.Content[:k]...), value.Content[k+2:]...)
			*i = Image{ForEach: &f, template: &tmpl}
			return nil
		}
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

		// An image from git is named for its repository when it has no
		// name: git@github.com:acme/payments-api.git → payments-api.
		if img.Name == "" && img.Git != nil && img.Git.URL != "" {
			img.Name = core.RepoSlug(img.Git.URL)
		}

		if img.Build != nil && img.Image == "" {
			img.Image = img.DefaultImageName(specName, i)
		}

		if img.Deploy != nil && img.Deploy.Name == "" {
			deploy := *img.Deploy
			deploy.Name = or(img.Name, img.DefaultDeployName())
			img.Deploy = &deploy
		}
		if img.Name == "" {
			if img.Deploy != nil {
				img.Name = img.Deploy.Name
			} else {
				img.Name = img.DefaultDeployName()
			}
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

// or returns v, or def when v is empty.
func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// plural is n and noun, plural unless n is 1: "1 image", "2 images".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Needs returns img's needs with the base name of a forEach entry replaced
// by its rows' names, so needs: [api] is every row of api. A name an image
// has itself (a row's own, an entry's without forEach) stays as it is.
// imgs has its default names resolved.
func (imgs Images) Needs(img Image) []string {
	var out []string
	for _, n := range img.Needs {
		if slices.ContainsFunc(imgs, func(o Image) bool { return o.Name == n }) {
			out = append(out, n)
			continue
		}
		var rows []string
		for _, o := range imgs {
			if o.Row != nil && o.Row.Base == n {
				rows = append(rows, o.Name)
			}
		}
		if len(rows) == 0 {
			rows = []string{n} // unknown: the caller reports it
		}
		out = append(out, rows...)
	}
	return out
}
