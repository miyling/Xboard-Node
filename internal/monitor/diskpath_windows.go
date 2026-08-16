//go:build windows

package monitor

import (
	"os"
	"path/filepath"
)

func diskUsagePath(dataRoot string) string {
	root := dataRoot
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			root = exe
		}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if volume := filepath.VolumeName(root); volume != "" {
		return volume + string(filepath.Separator)
	}
	return root
}
