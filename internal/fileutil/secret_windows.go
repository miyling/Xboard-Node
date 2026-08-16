//go:build windows

package fileutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// ProtectSecretFile removes inherited ACLs and grants access only to SYSTEM
// and the built-in Administrators group. icacls.exe is part of Windows Server
// and takes a path, never a credential value.
func ProtectSecretFile(path string) error {
	cmd := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", "*S-1-5-18:(F)", "*S-1-5-32-544:(F)")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("protect secret file %q: %w (%s)", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}
