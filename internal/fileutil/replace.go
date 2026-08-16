// Package fileutil contains small cross-platform filesystem helpers.
package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to a same-directory temporary file and replaces path
// only after the complete contents have been flushed and closed. Keeping the
// temporary file beside the destination makes the final replacement atomic on
// filesystems used by both Linux and Windows.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, nil)
}

// WriteAtomicSecret is the protected variant of WriteAtomic for credentials
// and other private files. The temporary file is protected before it can
// replace the destination, so a failed ACL operation cannot expose a newly
// written secret through inherited directory permissions.
func WriteAtomicSecret(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, ProtectSecretFile)
}

func writeAtomic(path string, data []byte, perm os.FileMode, protect func(string) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if err := f.Chmod(perm); err != nil {
		f.Close()
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("flush temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if protect != nil {
		if err := protect(tmp); err != nil {
			return fmt.Errorf("protect temporary file: %w", err)
		}
	}
	if err := ReplaceFile(tmp, path); err != nil {
		return err
	}
	if protect != nil {
		// The source is protected before replacement, but reapply protection to
		// cover platform-specific replacement semantics and existing targets.
		if err := protect(path); err != nil {
			return fmt.Errorf("protect destination file: %w", err)
		}
	}
	return nil
}

// CopyAndReplace streams src into a same-directory temporary file and then
// performs the platform-native replacement. It is useful for large downloads
// that should not be buffered in memory.
func CopyAndReplace(dst string, src io.Reader, perm os.FileMode) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if err := f.Chmod(perm); err != nil {
		f.Close()
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("flush temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	return ReplaceFile(tmp, dst)
}
