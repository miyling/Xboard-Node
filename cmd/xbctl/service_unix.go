//go:build !windows

package main

// Linux service operations are implemented in platform_paths_unix.go so all
// command paths in main.go use the same platform-neutral interface.
