//go:build windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func isTransientConfigError(err error) bool {
	return os.IsNotExist(err) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
