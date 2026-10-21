package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
)

func TestExtractPasswordProtectedArchives(t *testing.T) {
	for _, tc := range []struct {
		archive, path, content string
		extract                func(context.Context, string, string, string, *extractAccessPolicy) error
	}{
		{"password.7z", "nested/hello.txt", "hello archives\n", extractSevenZip},
		{"rar5-password.rar", "d.txt", "This is from d.txt", extractRar},
		{"zip-crypto.zip", "nested/hello.txt", "hello archives\n", extractZIP},
		{"zip-aes256.zip", "nested/hello.txt", "hello archives\n", extractZIP},
	} {
		t.Run(tc.archive, func(t *testing.T) {
			archive := filepath.Join("testdata/archives", tc.archive)
			dest := t.TempDir()
			if err := tc.extract(context.Background(), archive, dest, "password", nil); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(dest, tc.path))
			if err != nil || string(content) != tc.content {
				t.Fatalf("content=%q err=%v", content, err)
			}
			if err := tc.extract(context.Background(), archive, t.TempDir(), "wrong", nil); err == nil {
				t.Fatal("expected wrong password to fail")
			}
		})
	}
}

func TestEncryptedArchiveAccessPolicy(t *testing.T) {
	for _, tc := range []struct {
		archive, path string
		extract       func(context.Context, string, string, string, *extractAccessPolicy) error
	}{
		{"password.7z", "nested/hello.txt", extractSevenZip},
		{"rar5-password.rar", "d.txt", extractRar},
		{"zip-crypto.zip", "nested/hello.txt", extractZIP},
		{"zip-aes256.zip", "nested/hello.txt", extractZIP},
	} {
		t.Run(tc.archive, func(t *testing.T) {
			archive := filepath.Join("testdata/archives", tc.archive)
			t.Run("create without modify", func(t *testing.T) {
				dest, _, source, username := setupExtractAccessPolicyTest(t)
				perms := fullPerms()
				perms.Modify = false
				policy := testExtractPolicy(t, dest, source, username, perms)
				if err := tc.extract(context.Background(), archive, dest, "password", policy); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(dest, tc.path)); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("handler denies before writing or deleting", func(t *testing.T) {
				dest, root, _, username := setupExtractAccessPolicyTest(t)
				if err := state.DenyUser(root, utils.IndexPathFromNormalized("/dest/"+tc.path, true), username); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(archive)
				if err != nil {
					t.Fatal(err)
				}
				archivePath := filepath.Join(root, tc.archive)
				if err = os.WriteFile(archivePath, data, 0o644); err != nil {
					t.Fatal(err)
				}
				user, err := state.GetUserByUsername(username)
				if err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf(`{"fromSource":"data","path":"/%s","destination":"/dest","password":"password","deleteAfter":true}`, tc.archive)
				req := httptest.NewRequest(http.MethodPost, "/api/resources/unarchive", strings.NewReader(body))
				status, err := unarchiveHandler(httptest.NewRecorder(), req, &Context{User: &user})
				if status != http.StatusForbidden {
					t.Fatalf("status=%d err=%v", status, err)
				}
				requireExtractAccessDenied(t, err, dest, tc.path)
				entries, err := os.ReadDir(dest)
				if err != nil || len(entries) != 0 {
					t.Fatalf("preflight wrote entries: %v, err=%v", entries, err)
				}
				if _, err := os.Stat(archivePath); err != nil {
					t.Fatalf("source archive should be preserved: %v", err)
				}
			})
			t.Run("overwrite requires modify", func(t *testing.T) {
				dest, _, source, username := setupExtractAccessPolicyTest(t)
				file := filepath.Join(dest, tc.path)
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("original"), 0o644); err != nil {
					t.Fatal(err)
				}
				perms := fullPerms()
				perms.Modify = false
				policy := testExtractPolicy(t, dest, source, username, perms)
				err := tc.extract(context.Background(), archive, dest, "password", policy)
				requireExtractAccessDenied(t, err, dest, "")
				content, err := os.ReadFile(file)
				if err != nil || string(content) != "original" {
					t.Fatalf("existing file changed: %q err=%v", content, err)
				}
			})
			t.Run("creation requires create", func(t *testing.T) {
				dest, _, source, username := setupExtractAccessPolicyTest(t)
				perms := fullPerms()
				perms.Create = false
				policy := testExtractPolicy(t, dest, source, username, perms)
				err := tc.extract(context.Background(), archive, dest, "password", policy)
				requireExtractAccessDenied(t, err, dest, "")
			})
			t.Run("symlink target access rule", func(t *testing.T) {
				dest, root, source, username := setupExtractAccessPolicyTest(t)
				target := filepath.Join(dest, "blocked")
				if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(dest, tc.path)
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, file); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if err := state.DenyUser(root, utils.IndexPathFromNormalized("/dest/blocked", true), username); err != nil {
					t.Fatal(err)
				}
				policy := testExtractPolicy(t, dest, source, username, fullPerms())
				err := tc.extract(context.Background(), archive, dest, "password", policy)
				requireExtractAccessDenied(t, err, dest, tc.path)
				content, err := os.ReadFile(target)
				if err != nil || string(content) != "original" {
					t.Fatalf("denied target changed: %q err=%v", content, err)
				}
			})
		})
	}
}

func extractZIP(ctx context.Context, archive, dest, password string, policy *extractAccessPolicy) error {
	return extractZipWithOptions(ctx, archive, dest, "", password, policy)
}
