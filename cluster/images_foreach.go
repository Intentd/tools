package cluster

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/resolver"
	"github.com/intentd/tools/structured"
)

// ExpandForEach repeats each entry with forEach once per row, in place, and
// returns the list the rest of a run sees: every entry concrete, none with
// forEach. Per row, the entry's templates are resolved with r and the row
// (.item, .index, .<as>), then decoded as an image, so a templated number
// (port: "{{ .item.port }}") becomes one. ${{ env.… }} and the other
// contexts are left for the run to expand. A template that reads anything
// but the row fails: when images expand, only the row is there.
//
// A row's name: a templated name, resolved and slugified; else
// <base>-<entry>-<row>, where the base is the entry's name, the repository's
// for one from git (KEL's RepoSlug), or the image's.
func (imgs Images) ExpandForEach(r *resolver.Resolver) (Images, error) {
	return structured.ExpandForEach(imgs,
		func(img Image) (bool, []any, error) {
			if img.ForEach == nil {
				return false, nil, nil
			}
			rows, err := img.ForEach.Rows()
			return true, rows, err
		},
		func(src Image, entry int, item any, row int) (Image, error) {
			at := fmt.Sprintf("images[%d] forEach row %d", entry, row)
			node := copyNode(src.template)
			if err := resolveNode(r.WithItem(item, src.ForEach.As, row), node); err != nil {
				return Image{}, fmt.Errorf("%s: %w", at, err)
			}
			var img Image
			if err := node.Decode(&img); err != nil {
				return Image{}, fmt.Errorf("%s: %w", at, err)
			}
			if img.ForEach != nil {
				return Image{}, fmt.Errorf("%s: forEach inside a row: one forEach per entry", at)
			}
			name := field(src.template, "name")
			base := rowBase(name, img)
			if structured.IsTemplate(name) {
				img.Name = core.Slugify(img.Name)
			} else {
				img.Name = fmt.Sprintf("%s-%d-%d", base, entry, row)
			}
			img.Row = &ImageRow{Entry: entry, Index: row, Base: base}
			img.Dir = src.Dir
			return img, nil
		})
}

// rowBase is the name an entry's rows share, which needs reads as every row:
// a name's text before its first template (slugified), else the
// repository's for an image from git, else the image's.
func rowBase(name string, img Image) string {
	if i := strings.Index(name, "{{"); i >= 0 {
		name = name[:i]
	}
	if base := core.Slugify(name); base != "" {
		return base
	}
	if img.Git != nil && img.Git.URL != "" {
		return core.RepoSlug(img.Git.URL)
	}
	if base := img.DefaultDeployName(); base != "" {
		return core.Slugify(base)
	}
	return "image"
}

// resolveNode resolves every template in node's scalars with r, in place. A
// scalar it changes loses its tag and quoting, so it decodes as its new
// value does: "8080" into a port.
func resolveNode(r *resolver.Resolver, node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		if !structured.IsTemplate(node.Value) && !structured.IsCEL(node.Value) {
			return nil
		}
		const hint = "when images expand there is .item, .index and .<as>; use ${{ env.… }} or ${{ ecosystem.… }} for the run's values"
		out, missing, err := r.ResolveStrict(node.Value)
		switch {
		case err != nil:
			return fmt.Errorf("%q: %w (if it reads a value a row doesn't have: %s)", node.Value, err, hint)
		case missing:
			return fmt.Errorf("%q reads a value a row doesn't have: %s", node.Value, hint)
		}
		node.Value, node.Tag, node.Style = out, "", 0
		return nil
	}
	for _, c := range node.Content {
		if err := resolveNode(r, c); err != nil {
			return err
		}
	}
	return nil
}

// copyNode is a deep copy of n.
func copyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = copyNode(child)
	}
	return &c
}

// field is the scalar value of key in a mapping node, as written.
func field(n *yaml.Node, key string) string {
	if n == nil || n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i+1].Kind == yaml.ScalarNode {
			return n.Content[i+1].Value
		}
	}
	return ""
}
