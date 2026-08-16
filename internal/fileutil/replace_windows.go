//go:build windows

package fileutil

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

func ReplaceFile(src, dst string) error {
	// MoveFileEx is the Windows-native replacement primitive. A short retry
	// window covers antivirus/indexer handles that briefly hold the destination
	// after an fsnotify event or service reload.
	for attempt := 0; attempt < 10; attempt++ {
		err := windows.MoveFileEx(
			windows.StringToUTF16Ptr(src),
			windows.StringToUTF16Ptr(dst),
			windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
		)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("replace %q: %w", dst, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("replace %q: destination remained locked", dst)
}
