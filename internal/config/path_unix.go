//go:build !windows

package config

func samePath(a, b string) bool { return a == b }
