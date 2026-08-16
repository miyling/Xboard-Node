//go:build windows

package config

import "strings"

func samePath(a, b string) bool { return strings.EqualFold(a, b) }
