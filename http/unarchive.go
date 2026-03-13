package fbhttp

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/mholt/archives"
	"github.com/spf13/afero"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
)

var supportedArchiveExtensions = []string{
	".tar.gz",
	".tar.xz",
	".zip",
	".rar",
	".7z",
	".tar",
	".gz",
	".xz",
}

func unarchiveDestination(src string) (string, error) {
	base, ok := stripArchiveExtension(path.Base(src))
	if !ok {
		return "", fmt.Errorf("unsupported archive type: %w", fberrors.ErrInvalidRequestParams)
	}

	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("invalid archive name: %w", fberrors.ErrInvalidRequestParams)
	}

	return path.Join(path.Dir(src), base), nil
}

func stripArchiveExtension(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, ext := range supportedArchiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)], true
		}
	}

	return "", false
}

func unarchive(ctx context.Context, afs afero.Fs, src, dst string, fileMode, dirMode fs.FileMode) error {
	info, err := afs.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("source must be a file: %w", fberrors.ErrInvalidRequestParams)
	}

	if _, err := afs.Stat(dst); err == nil {
		return fberrors.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}

	archiveFile, err := afs.Open(src)
	if err != nil {
		return err
	}
	defer archiveFile.Close()

	format, stream, err := archives.Identify(ctx, path.Base(src), archiveFile)
	if err != nil {
		return err
	}

	if err := afs.MkdirAll(dst, dirMode); err != nil {
		return err
	}

	if err := extractArchive(ctx, afs, dst, path.Base(dst), format, stream, fileMode, dirMode); err != nil {
		_ = afs.RemoveAll(dst)
		return err
	}

	return nil
}

func extractArchive(
	ctx context.Context,
	afs afero.Fs,
	dstRoot string,
	singleFileName string,
	format archives.Format,
	stream io.Reader,
	fileMode fs.FileMode,
	dirMode fs.FileMode,
) error {
	if extractor, ok := format.(archives.Extractor); ok {
		return extractor.Extract(ctx, stream, func(_ context.Context, file archives.FileInfo) error {
			return writeExtractedFile(afs, dstRoot, file, fileMode, dirMode)
		})
	}

	decompressor, ok := format.(archives.Decompressor)
	if !ok {
		return fmt.Errorf("unsupported archive type: %w", fberrors.ErrInvalidRequestParams)
	}

	reader, err := decompressor.OpenReader(stream)
	if err != nil {
		return err
	}
	defer reader.Close()

	target := path.Join(dstRoot, singleFileName)
	if err := ensureSafeExtractPath(afs, dstRoot, singleFileName); err != nil {
		return err
	}

	_, err = writeFile(afs, target, reader, fileMode, dirMode)
	return err
}

func writeExtractedFile(
	afs afero.Fs,
	dstRoot string,
	file archives.FileInfo,
	fileMode fs.FileMode,
	dirMode fs.FileMode,
) error {
	name, skip, err := cleanArchivePath(file.NameInArchive)
	if err != nil || skip {
		return err
	}

	if err := ensureSafeExtractPath(afs, dstRoot, name); err != nil {
		return err
	}

	target := path.Join(dstRoot, name)
	mode := file.Mode()

	switch {
	case mode.IsDir():
		return afs.MkdirAll(target, fallbackDirMode(mode, dirMode))
	case mode&fs.ModeSymlink != 0:
		return fmt.Errorf("archive contains unsupported symbolic links: %w", fberrors.ErrInvalidRequestParams)
	case !mode.IsRegular():
		return fmt.Errorf("archive contains unsupported file type: %w", fberrors.ErrInvalidRequestParams)
	}

	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if _, err := writeFile(afs, target, rc, fallbackFileMode(mode, fileMode), dirMode); err != nil {
		return err
	}

	return afs.Chmod(target, fallbackFileMode(mode, fileMode))
}

func cleanArchivePath(name string) (string, bool, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return "", true, nil
	}

	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", true, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("archive entry escapes destination: %w", fberrors.ErrInvalidRequestParams)
	}

	return cleaned, false, nil
}

func ensureSafeExtractPath(afs afero.Fs, dstRoot, relPath string) error {
	current := dstRoot
	parent := path.Dir(relPath)
	if parent == "." {
		return nil
	}

	lstater, ok := afs.(afero.Lstater)
	if !ok {
		return nil
	}

	for _, part := range strings.Split(parent, "/") {
		current = path.Join(current, part)
		info, _, err := lstater.LstatIfPossible(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to extract through symbolic links: %w", fberrors.ErrInvalidRequestParams)
		}
	}

	return nil
}

func fallbackDirMode(mode fs.FileMode, fallback fs.FileMode) fs.FileMode {
	if perm := mode.Perm(); perm != 0 {
		return perm
	}
	return fallback
}

func fallbackFileMode(mode fs.FileMode, fallback fs.FileMode) fs.FileMode {
	if perm := mode.Perm(); perm != 0 {
		return perm
	}
	return fallback
}
