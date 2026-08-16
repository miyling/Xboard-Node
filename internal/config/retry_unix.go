//go:build !windows

package config

import "os"

func isTransientConfigError(err error) bool { return os.IsNotExist(err) }
