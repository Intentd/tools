package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"golang.org/x/mod/modfile"
)

func TestExtractPack(t *testing.T) {
	source := fstest.MapFS{
		"examples/stack/jig.yaml": {
			Data: []byte("name: stack\n"),
		},
		"examples/stack/scripts/setup.sh": {
			Data: []byte("#!/bin/sh\necho ready\n"),
		},
		"examples/stack/nested/go.mod.txt": {
			Data: []byte("module example.com/nested\n"),
		},
		"examples/stack/nested/go.sum.txt": {
			Data: []byte("example.com/dependency v1.0.0\n"),
		},
		"examples/stack/nested/main.go": {
			Data: []byte("//go:build ignore\n\npackage main\n"),
		},
		"examples/stack/go.mod.txt": {
			Data: []byte("module example.com/stack\nrequire jig {{ .JigVersion }}\n"),
		},
	}

	root := t.TempDir()

	err := ExtractPack(root, source, Pack{
		Name:  "stack",
		Path:  "examples/stack",
		First: "jig.yaml",
	}, Options{
		GoModReplacements: map[string]string{
			"{{ .JigVersion }}": "v1.2.3",
		},
	})
	if err != nil {
		t.Fatalf("ExtractPack() error = %v", err)
	}

	target := filepath.Join(root, "stack")

	assertFileContent(t, filepath.Join(target, "jig.yaml"), "name: stack\n")
	assertFileContent(t, filepath.Join(target, "go.mod"), "module example.com/stack\n\nrequire jig v1.2.3\n")
	assertFileContent(t, filepath.Join(target, "nested", "go.mod"), "module example.com/nested\n")
	assertFileContent(t, filepath.Join(target, "nested", "go.sum"), "example.com/dependency v1.0.0\n")
	assertFileContent(t, filepath.Join(target, "nested", "main.go"), "package main\n")

	script := filepath.Join(target, "scripts", "setup.sh")
	assertFileContent(t, script, "#!/bin/sh\necho ready\n")
	assertExecutable(t, script)
}

func TestExtractPackWithoutOptions(t *testing.T) {
	source := fstest.MapFS{
		"examples/minimal/jig.yaml": {
			Data: []byte("name: minimal\n"),
		},
	}

	root := t.TempDir()

	err := ExtractPack(root, source, Pack{
		Name: "minimal",
		Path: "examples/minimal",
	})
	if err != nil {
		t.Fatalf("ExtractPack() error = %v", err)
	}

	assertFileContent(t, filepath.Join(root, "minimal", "jig.yaml"), "name: minimal\n")
}

func TestExtractPackOverwritesExistingFiles(t *testing.T) {
	source := fstest.MapFS{
		"examples/app/jig.yaml": {
			Data: []byte("name: new\n"),
		},
	}

	root := t.TempDir()
	target := filepath.Join(root, "app")

	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "jig.yaml"), []byte("name: old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ExtractPack(root, source, Pack{
		Name: "app",
		Path: "examples/app",
	})
	if err != nil {
		t.Fatalf("ExtractPack() error = %v", err)
	}

	assertFileContent(t, filepath.Join(target, "jig.yaml"), "name: new\n")
}

func TestExtractPackInvalidPaths(t *testing.T) {
	source := fstest.MapFS{
		"examples/app/jig.yaml": {
			Data: []byte("name: app\n"),
		},
	}

	tests := []struct {
		name string
		pack Pack
	}{
		{
			name: "empty path",
			pack: Pack{Name: "app"},
		},
		{
			name: "parent traversal",
			pack: Pack{Name: "app", Path: "../outside"},
		},
		{
			name: "absolute path",
			pack: Pack{Name: "app", Path: "/examples/app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExtractPack(t.TempDir(), source, tt.pack)
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestExtractPackMissingSource(t *testing.T) {
	err := ExtractPack(t.TempDir(), fstest.MapFS{}, Pack{
		Name: "missing",
		Path: "examples/missing",
	})
	if err == nil {
		t.Fatal("expected an error for missing pack")
	}
}

func TestExtractPackNilFilesystem(t *testing.T) {
	err := ExtractPack(t.TempDir(), nil, Pack{
		Name: "app",
		Path: "examples/app",
	})
	if err == nil {
		t.Fatal("expected an error for nil filesystem")
	}
}

func TestExtractCanonical(t *testing.T) {
	source := fstest.MapFS{
		"examples/hello/blueprint.yaml": {
			Data: []byte("name: hello\n"),
		},
		"examples/hello/manifests/crd.yaml": {
			Data: []byte("kind: CustomResourceDefinition\n"),
		},
		"examples/hello/manifests/cr.yaml": {
			Data: []byte("kind: Hello\n"),
		},
		"examples/hello/test/e2e.yaml": {
			Data: []byte("name: e2e\n"),
		},
		"examples/hello/README.md": {
			Data: []byte("This file should not be extracted.\n"),
		},
	}

	root := t.TempDir()

	files := []CanonicalFile{
		{Source: "blueprint.yaml", Destination: "blueprint.yaml"},
		{Source: "manifests/crd.yaml", Destination: "manifests/crd.yaml"},
		{Source: "manifests/cr.yaml", Destination: "manifests/cr.yaml"},
		{Source: "test/e2e.yaml", Destination: "test/e2e.yaml"},
	}

	err := ExtractCanonical(root, source, "examples/hello", files)
	if err != nil {
		t.Fatalf("ExtractCanonical() error = %v", err)
	}

	assertFileContent(t, filepath.Join(root, "blueprint.yaml"), "name: hello\n")
	assertFileContent(t, filepath.Join(root, "manifests", "crd.yaml"), "kind: CustomResourceDefinition\n")
	assertFileContent(t, filepath.Join(root, "manifests", "cr.yaml"), "kind: Hello\n")
	assertFileContent(t, filepath.Join(root, "test", "e2e.yaml"), "name: e2e\n")

	if _, err := os.Stat(filepath.Join(root, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("README.md should not be extracted; stat error = %v", err)
	}
}

func TestExtractCanonicalOverwritesExistingFiles(t *testing.T) {
	source := fstest.MapFS{
		"examples/hello/blueprint.yaml": {
			Data: []byte("name: new\n"),
		},
	}

	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "blueprint.yaml"), []byte("name: old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ExtractCanonical(root, source, "examples/hello", []CanonicalFile{
		{Source: "blueprint.yaml", Destination: "blueprint.yaml"},
	})
	if err != nil {
		t.Fatalf("ExtractCanonical() error = %v", err)
	}

	assertFileContent(t, filepath.Join(root, "blueprint.yaml"), "name: new\n")
}

func TestExtractCanonicalInvalidPaths(t *testing.T) {
	source := fstest.MapFS{
		"examples/hello/blueprint.yaml": {
			Data: []byte("name: hello\n"),
		},
	}

	tests := []struct {
		name      string
		sourceDir string
		files     []CanonicalFile
	}{
		{
			name:      "invalid source directory",
			sourceDir: "../outside",
			files:     []CanonicalFile{{Source: "blueprint.yaml", Destination: "blueprint.yaml"}},
		},
		{
			name:      "invalid source file",
			sourceDir: "examples/hello",
			files:     []CanonicalFile{{Source: "../outside", Destination: "blueprint.yaml"}},
		},
		{
			name:      "invalid destination",
			sourceDir: "examples/hello",
			files:     []CanonicalFile{{Source: "blueprint.yaml", Destination: "../outside"}},
		},
		{
			name:      "empty destination",
			sourceDir: "examples/hello",
			files:     []CanonicalFile{{Source: "blueprint.yaml"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExtractCanonical(t.TempDir(), source, tt.sourceDir, tt.files)
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestExtractCanonicalMissingFile(t *testing.T) {
	err := ExtractCanonical(
		t.TempDir(),
		fstest.MapFS{},
		"examples/hello",
		[]CanonicalFile{
			{Source: "blueprint.yaml", Destination: "blueprint.yaml"},
		},
	)
	if err == nil {
		t.Fatal("expected an error for missing source file")
	}
}

func TestExtractCanonicalNilFilesystem(t *testing.T) {
	err := ExtractCanonical(
		t.TempDir(),
		nil,
		"examples/hello",
		nil,
	)
	if err == nil {
		t.Fatal("expected an error for nil filesystem")
	}
}

func TestReplaceAllDeterministic(t *testing.T) {
	input := []byte("{{ .A }} {{ .B }}")
	got := string(replaceAll(input, map[string]string{
		"{{ .A }}": "first",
		"{{ .B }}": "second",
	}))

	if got != "first second" {
		t.Fatalf("replaceAll() = %q, want %q", got, "first second")
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}

	if string(got) != want {
		t.Errorf("content of %s:\n got: %q\nwant: %q", path, got, want)
	}
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}

	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("%s is not executable: mode %s", path, info.Mode().Perm())
	}
}

// Ensure the test filesystem satisfies fs.FS.
var _ fs.FS = fstest.MapFS{}

func TestRemoveGoModReplaces(t *testing.T) {
	input := []byte(`module example.com/project

go 1.27.4

require (
	github.com/intentd/jig v1.0.0
	github.com/stretchr/testify v1.12.1
)

replace github.com/intentd/jig => ../..

replace (
	example.com/old v1.0.0 => example.com/new v1.0.0
	example.com/another => ../another
)
`)

	got, err := removeGoModReplaces(input)
	if err != nil {
		t.Fatalf("removeGoModReplaces() error = %v", err)
	}

	f, err := modfile.Parse("go.mod", got, nil)
	if err != nil {
		t.Fatalf("result is not valid go.mod: %v", err)
	}

	if len(f.Replace) != 0 {
		t.Errorf("expected no replace directives, got %d", len(f.Replace))
	}

	if f.Module == nil || f.Module.Mod.Path != "example.com/project" {
		t.Errorf("module declaration was not preserved")
	}

	if f.Go == nil || f.Go.Version != "1.27.4" {
		t.Errorf("go version was not preserved")
	}

	if len(f.Require) != 2 {
		t.Errorf("expected 2 require directives, got %d", len(f.Require))
	}
}

func TestRemoveGoModReplacesWithoutReplaces(t *testing.T) {
	input := []byte(`module example.com/project

go 1.27.4

require github.com/stretchr/testify v1.12.1
`)

	got, err := removeGoModReplaces(input)
	if err != nil {
		t.Fatalf("removeGoModReplaces() error = %v", err)
	}

	if _, err := modfile.Parse("go.mod", got, nil); err != nil {
		t.Fatalf("result is not valid go.mod: %v", err)
	}
}

func TestRemoveGoModReplacesInvalidGoMod(t *testing.T) {
	input := []byte(`this is not a go.mod
`)

	_, err := removeGoModReplaces(input)
	if err == nil {
		t.Fatal("expected an error for invalid go.mod")
	}
}
