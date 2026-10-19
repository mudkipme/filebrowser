package archivecrypto

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestZIPRejectsTamperedAES(t *testing.T) {
	data, err := os.ReadFile("../web/testdata/archives/zip-aes256.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entry := archive.File[0]
	offset, err := entry.DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	data[offset+int64(entry.CompressedSize64)-1] ^= 1
	if rc, openErr := Open(context.Background(), entry, "password"); !errors.Is(openErr, ErrAuthentication) || rc != nil {
		t.Fatalf("tampered ZIP returned a plaintext reader: %v", openErr)
	}
}
