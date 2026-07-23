package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCreateArchiveIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.txt", "release notes\n", 0o644)
	writeTestFile(t, root, "bin/dsmctl", "binary\n", 0o755)

	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			first := filepath.Join(t.TempDir(), "first."+format)
			second := filepath.Join(t.TempDir(), "second."+format)
			if err := createArchive(format, root, first, 1_700_000_000); err != nil {
				t.Fatal(err)
			}
			if err := createArchive(format, root, second, 1_700_000_000); err != nil {
				t.Fatal(err)
			}
			firstBytes, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			secondBytes, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstBytes, secondBytes) {
				t.Fatalf("%s output is not deterministic", format)
			}
		})
	}
}

func TestCreateArchiveHasFlatSortedContents(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "dsmctl", "cli", 0o755)
	writeTestFile(t, root, "LICENSE", "license", 0o644)
	expected := []string{"LICENSE", "dsmctl"}

	tarPath := filepath.Join(t.TempDir(), "release.tar.gz")
	if err := createArchive("tar.gz", root, tarPath, 1_700_000_000); err != nil {
		t.Fatal(err)
	}
	if got := tarNames(t, tarPath); !reflect.DeepEqual(got, expected) {
		t.Fatalf("tar names = %#v, want %#v", got, expected)
	}

	zipPath := filepath.Join(t.TempDir(), "release.zip")
	if err := createArchive("zip", root, zipPath, 1_700_000_000); err != nil {
		t.Fatal(err)
	}
	if got := zipNames(t, zipPath); !reflect.DeepEqual(got, expected) {
		t.Fatalf("zip names = %#v, want %#v", got, expected)
	}
}

func TestNormalizedMode(t *testing.T) {
	if got := normalizedMode(0o755); got != 0o755 {
		t.Fatalf("normalized executable mode = %#o, want 0755", got)
	}
	if got := normalizedMode(0o600); got != 0o644 {
		t.Fatalf("normalized data mode = %#o, want 0644", got)
	}
}

func writeTestFile(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}
}

func zipNames(t *testing.T, path string) []string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}
