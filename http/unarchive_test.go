package fbhttp

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestUnarchiveDestination(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/demo.zip":     "/demo",
		"/demo.tar.gz":  "/demo",
		"/demo.tar.xz":  "/demo",
		"/demo.txt.gz":  "/demo.txt",
		"/demo.txt.xz":  "/demo.txt",
		"/folder/a.7z":  "/folder/a",
		"/folder/a.rar": "/folder/a",
	}

	for input, want := range tests {
		got, err := unarchiveDestination(input)
		if err != nil {
			t.Fatalf("unarchiveDestination(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("unarchiveDestination(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUnarchiveZip(t *testing.T) {
	t.Parallel()

	fs := afero.NewBasePathFs(afero.NewOsFs(), t.TempDir())

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	file, err := writer.Create("nested/hello.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := io.WriteString(file, "hello from zip"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := afero.WriteFile(fs, "/archive.zip", buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := unarchive(context.Background(), fs, "/archive.zip", "/archive", 0o644, 0o755); err != nil {
		t.Fatalf("unarchive: %v", err)
	}

	content, err := afero.ReadFile(fs, "/archive/nested/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "hello from zip" {
		t.Fatalf("unexpected extracted content: %q", string(content))
	}
}

func TestUnarchiveGzip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := afero.NewBasePathFs(afero.NewOsFs(), root)

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := io.WriteString(writer, "hello from gzip"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "sample.txt.gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := unarchive(context.Background(), fs, "/sample.txt.gz", "/sample.txt", 0o644, 0o755); err != nil {
		t.Fatalf("unarchive: %v", err)
	}

	content, err := afero.ReadFile(fs, "/sample.txt/sample.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "hello from gzip" {
		t.Fatalf("unexpected extracted content: %q", string(content))
	}
}
