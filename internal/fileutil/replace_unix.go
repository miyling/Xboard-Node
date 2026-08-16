//go:build !windows

package fileutil

import (
	"fmt"
	"os"
)

func ReplaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("replace %q: %w", dst, err)
	}
	return nil
}
