//go:build !windows

package monitor

func diskUsagePath(_ string) string { return "/" }
