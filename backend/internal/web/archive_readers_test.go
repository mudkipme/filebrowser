package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractPasswordProtectedArchives(t *testing.T) {
	for _, tc := range []struct {
		archive, path, content string
		extract                func(context.Context, string, string, string) error
	}{
		{"password.7z", "nested/hello.txt", "hello archives\n", extractSevenZip},
		{"rar5-password.rar", "d.txt", "This is from d.txt", extractRar},
		{"zip-crypto.zip", "nested/hello.txt", "hello archives\n", extractZIP},
		{"zip-aes256.zip", "nested/hello.txt", "hello archives\n", extractZIP},
	} {
		t.Run(tc.archive, func(t *testing.T) {
			archive := filepath.Join("testdata/archives", tc.archive)
			dest := t.TempDir()
			if err := tc.extract(context.Background(), archive, dest, "password"); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(dest, tc.path))
			if err != nil || string(content) != tc.content {
				t.Fatalf("content=%q err=%v", content, err)
			}
			if err := tc.extract(context.Background(), archive, t.TempDir(), "wrong"); err == nil {
				t.Fatal("expected wrong password to fail")
			}
		})
	}
}

func extractZIP(ctx context.Context, archive, dest, password string) error {
	return extractZipWithOptions(ctx, archive, dest, "", password)
}
