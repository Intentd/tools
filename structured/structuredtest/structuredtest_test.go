package structuredtest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRequireErr(t *testing.T) {
	t.Run("matches", func(t *testing.T) {
		errs := []error{errors.New("foo"), errors.New("bar baz")}
		RequireErr(t, errs, "bar")
	})

	t.Run("no match fails", func(t *testing.T) {
		ft := &testing.T{}
		errs := []error{errors.New("foo"), errors.New("bar")}
		RequireErr(ft, errs, "nope")
		if !ft.Failed() {
			t.Fatal("expected RequireErr to fail")
		}
	})

	t.Run("empty slice fails", func(t *testing.T) {
		ft := &testing.T{}
		RequireErr(ft, nil, "anything")
		if !ft.Failed() {
			t.Fatal("expected RequireErr to fail")
		}
	})
}

func TestWriteFile(t *testing.T) {
	t.Run("writes content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.txt")
		WriteFile(t, path, "hello")

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "hello" {
			t.Fatalf("got %q, want %q", got, "hello")
		}
	})

	t.Run("creates its directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a", "b", "out.txt")
		WriteFile(t, path, "x")
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWriteExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.sh")
	WriteExecutable(t, path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	want := "#!/bin/sh\nexit 0\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("got permissions %04o, want %04o", info.Mode().Perm(), 0o755)
	}
}

func TestWriteFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	WriteFileMode(t, path, "hello", 0o600)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("got permissions %04o, want %04o", info.Mode().Perm(), 0o600)
	}
}
