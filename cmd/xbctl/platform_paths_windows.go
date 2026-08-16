//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

var (
	windowsProgramData  = envOrDefault("ProgramData", `C:\ProgramData`)
	windowsProgramFiles = envOrDefault("ProgramFiles", `C:\Program Files`)

	defaultInstallRoot     = filepath.Join(windowsProgramData, "xboard-node")
	defaultConfigPath      = filepath.Join(defaultInstallRoot, "config.yml")
	defaultMetaPath        = filepath.Join(defaultInstallRoot, "install-meta.json")
	defaultCredentialsPath = filepath.Join(defaultInstallRoot, "credentials.env")
	defaultLogPath         = filepath.Join(defaultInstallRoot, "logs", "xboard-node.log")
	defaultBinaryPath      = filepath.Join(windowsProgramFiles, "Xboard Node", "bin", "xboard-node.exe")
	defaultCLIPath         = filepath.Join(windowsProgramFiles, "Xboard Node", "bin", "xbctl.exe")
	serviceName            = "xboard-node"
	serviceFilePath        = ""
)

func isWindowsPlatform() bool { return true }

func releaseArtifact(kind, arch string) string {
	return fmt.Sprintf("%s-windows-%s.exe", kind, arch)
}
