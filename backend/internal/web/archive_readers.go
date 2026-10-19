package web

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/fileutils"
	"github.com/nwaples/rardecode/v2"
)

// Use file readers rather than filename-based openers so decoders cannot open
// additional volumes outside the source path authorized by unarchiveHandler.
func extractSevenZip(ctx context.Context, archivePath, destDir, password string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	r, err := sevenzip.NewReaderWithPassword(f, info.Size(), password)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()
	dest := archiveDestination{root: root}
	// Preserve archive order and close each entry before opening the next so
	// sevenzip can reuse the decompressor for solid archives.
	for _, entry := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Mode().IsDir() {
			if err := dest.write(ctx, entry.Name, entry.Mode(), entry.Modified, nil); err != nil {
				return err
			}
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		checksum := crc32.NewIEEE()
		err = dest.write(ctx, entry.Name, entry.Mode(), entry.Modified, io.TeeReader(rc, checksum))
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		// sevenzip does not verify individual file checksums. In particular,
		// encrypted, uncompressed entries can yield garbage for a wrong password.
		if entry.CRC32 != 0 && checksum.Sum32() != entry.CRC32 {
			return fmt.Errorf("7z checksum mismatch for %q (archive may be corrupt or password incorrect)", entry.Name)
		}
	}
	return dest.finish()
}

func extractRar(ctx context.Context, archivePath, destDir, password string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := rardecode.NewReader(f, rardecode.Password(password))
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()
	dest := archiveDestination{root: root}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := r.Next()
		if err == io.EOF {
			return dest.finish()
		}
		if err != nil {
			return err
		}
		if entry.LinkType != rardecode.LinkTypeNone {
			return fmt.Errorf("unsupported RAR link: %q", entry.Name)
		}
		if err := dest.write(ctx, entry.Name, entry.Mode(), entry.ModificationTime, r); err != nil {
			return err
		}
	}
}

type archivedDirectory struct {
	name    string
	mode    fs.FileMode
	modTime time.Time
}

type archiveDestination struct {
	root *os.Root
	dirs []archivedDirectory
}

// Root confines writes even when the destination contains existing symlinks.
// Links and special archive entries are rejected rather than materialized.
func (d *archiveDestination) write(ctx context.Context, name string, mode fs.FileMode, modTime time.Time, src io.Reader) error {
	rel, err := normalizeArchiveEntryName(name)
	if err != nil {
		return fmt.Errorf("invalid entry path %q: %w", name, err)
	}
	rel = filepath.FromSlash(rel)
	if mode.IsDir() {
		if err = d.root.MkdirAll(rel, fileutils.EffectiveDirPerm()); err != nil {
			return err
		}
		d.dirs = append(d.dirs, archivedDirectory{rel, mode, modTime})
		return nil
	}
	if !mode.IsRegular() {
		return fmt.Errorf("unsupported archive entry type: %q", name)
	}
	if err = d.root.MkdirAll(filepath.Dir(rel), fileutils.EffectiveDirPerm()); err != nil {
		return err
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = fileutils.EffectiveFilePerm()
	}
	out, err := d.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, archiveContextReader{ctx, src})
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("extracting %q: %w", name, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return d.metadata(rel, mode, modTime)
}

func (d *archiveDestination) metadata(name string, mode fs.FileMode, modTime time.Time) error {
	if perm := mode.Perm(); perm != 0 {
		if err := d.root.Chmod(name, perm); err != nil {
			return err
		}
	}
	if !modTime.IsZero() {
		return d.root.Chtimes(name, modTime, modTime)
	}
	return nil
}

func (d *archiveDestination) finish() error {
	// Apply directory metadata after children, deepest first, so read-only
	// directories and restored timestamps do not interfere with extraction.
	sort.SliceStable(d.dirs, func(i, j int) bool {
		return strings.Count(d.dirs[i].name, string(filepath.Separator)) > strings.Count(d.dirs[j].name, string(filepath.Separator))
	})
	for _, dir := range d.dirs {
		if err := d.metadata(dir.name, dir.mode, dir.modTime); err != nil {
			return err
		}
	}
	return nil
}

type archiveContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
