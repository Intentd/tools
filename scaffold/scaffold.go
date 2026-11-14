package scaffold

import (
	"bytes"
	"fmt"
	"golang.org/x/mod/modfile"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Pack describes an example pack embedded in an fs.FS.
//
// Path is relative to the root of the embedded filesystem.
// First is the first example to open, relative to Path.
type Pack struct {
	Name  string
	Path  string
	First string
}

// CanonicalFile describes a file copied from an embedded example
// into a canonical project layout.
type CanonicalFile struct {
	Source      string
	Destination string
}

// Options configures optional transformations when extracting a pack.
type Options struct {
	// GoModReplacements replaces placeholders in extracted go.mod files.
	// For example: "{{ .InrunVersion }}" -> "v1.2.3".
	GoModReplacements map[string]string
}

// ExtractPack extracts an embedded pack into root.
//
// The pack directory is created using the base name of pack.Path.
// For example, a pack with Path "examples/stack" is extracted into
// root/stack.
//
// Special handling:
//   - go.mod.txt and go.sum.txt are restored to go.mod and go.sum.
//   - The embed-only "//go:build ignore" prefix is removed from Go files.
//   - Shell scripts are written with executable permissions.
//   - Optional replacements are applied to extracted go.mod files.
//
// Existing files are overwritten.
func ExtractPack(root string, source fs.FS, pack Pack, options ...Options) error {
	if source == nil {
		return fmt.Errorf("extract pack %q: source filesystem is nil", pack.Name)
	}

	sourceDir := pack.Path
	targetDir := root

	if sourceDir != "." {
		if !fs.ValidPath(sourceDir) {
			return fmt.Errorf("extract pack %q: invalid embedded path %q", pack.Name, sourceDir)
		}
		targetDir = filepath.Join(root, path.Base(sourceDir))
	}

	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}

	err := fs.WalkDir(source, sourceDir, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(sourceDir, name)
		if err != nil {
			return fmt.Errorf("resolve embedded path %s: %w", name, err)
		}

		dst := targetDir
		if rel != "." {
			dst = filepath.Join(targetDir, filepath.FromSlash(rel))
		}

		if entry.IsDir() {
			return os.MkdirAll(dst, 0755)
		}

		// Nested Go modules are renamed at embed time because go:embed
		// does not include directories containing their own go.mod.
		isGoMod := false
		switch entry.Name() {
		case "go.mod.txt":
			dst = strings.TrimSuffix(dst, ".txt")
			isGoMod = true
		case "go.sum.txt":
			dst = strings.TrimSuffix(dst, ".txt")
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("create directory for %s: %w", dst, err)
		}

		data, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", name, err)
		}

		if filepath.Ext(dst) == ".go" {
			data = bytes.TrimPrefix(data, []byte("//go:build ignore\n\n"))
		}

		if isGoMod {
			data = replaceAll(data, opts.GoModReplacements)

			data, err = removeGoModReplaces(data)
			if err != nil {
				return fmt.Errorf("process %s: %w", name, err)
			}
		}

		mode := fs.FileMode(0644)
		if filepath.Ext(dst) == ".sh" {
			mode = 0755
		}

		if err := os.WriteFile(dst, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("extract pack %q: %w", pack.Name, err)
	}

	return nil
}

// removeGoModReplaces removes all replace directives from a go.mod file.
// Extracted examples must resolve dependencies independently of the
// directory structure of the repository they were embedded from.
func removeGoModReplaces(data []byte) ([]byte, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}

	// Drop from the end because DropReplace modifies f.Replace.
	for i := len(f.Replace) - 1; i >= 0; i-- {
		r := f.Replace[i]
		if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
			return nil, fmt.Errorf(
				"remove replace directive for %q: %w",
				r.Old.Path,
				err,
			)
		}
	}

	return f.Format()
}

// ExtractCanonical copies the selected files from sourceDir in the
// embedded filesystem into their specified destinations under root.
//
// Source paths are relative to sourceDir.
// Destination paths are relative to root.
//
// Only the files explicitly listed are extracted. Existing files
// are overwritten.
func ExtractCanonical(
	root string,
	source fs.FS,
	sourceDir string,
	files []CanonicalFile,
) error {
	if source == nil {
		return fmt.Errorf("extract canonical files: source filesystem is nil")
	}
	if !fs.ValidPath(sourceDir) {
		return fmt.Errorf("extract canonical files: invalid source directory %q", sourceDir)
	}

	for _, file := range files {
		if !fs.ValidPath(file.Source) || file.Source == "." {
			return fmt.Errorf("invalid canonical source path %q", file.Source)
		}
		if !fs.ValidPath(file.Destination) || file.Destination == "." {
			return fmt.Errorf("invalid canonical destination path %q", file.Destination)
		}

		src := path.Join(sourceDir, file.Source)

		data, err := fs.ReadFile(source, src)
		if err != nil {
			return fmt.Errorf("read canonical file %s: %w", src, err)
		}

		dst := filepath.Join(root, filepath.FromSlash(file.Destination))

		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("create directory for %s: %w", dst, err)
		}

		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("write canonical file %s: %w", dst, err)
		}
	}

	return nil
}

// replaceAll applies replacements in deterministic key order.
func replaceAll(data []byte, replacements map[string]string) []byte {
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		data = bytes.ReplaceAll(data, []byte(key), []byte(replacements[key]))
	}

	return data
}
