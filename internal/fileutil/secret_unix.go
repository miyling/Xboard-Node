//go:build !windows

package fileutil

import "os"

// ProtectSecretFile keeps credentials private after an atomic replacement.
func ProtectSecretFile(path string) error { return os.Chmod(path, 0o600) }
