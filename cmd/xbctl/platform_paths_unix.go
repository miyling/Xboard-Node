//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultInstallRoot     = "/etc/xboard-node"
	defaultConfigPath      = "/etc/xboard-node/config.yml"
	defaultMetaPath        = "/etc/xboard-node/install-meta.json"
	defaultCredentialsPath = "/etc/xboard-node/credentials.env"
	defaultBinaryPath      = "/usr/local/bin/xboard-node"
	defaultCLIPath         = "/usr/local/bin/xbctl"
	defaultLogPath         = "/var/log/xboard-node/xboard-node.log"
	serviceName            = "xboard-node.service"
	serviceFilePath        = "/etc/systemd/system/xboard-node.service"
)

func isWindowsPlatform() bool { return false }

func releaseArtifact(kind, arch string) string {
	return fmt.Sprintf("%s-linux-%s", kind, arch)
}

func ensureRoot(cmd string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s requires root privileges; run with sudo", cmd)
	}
	return nil
}

func serviceAction(sub string, args []string) error {
	switch sub {
	case "install":
		if err := ensureRoot("service install"); err != nil {
			return err
		}
		if err := regenerateServiceFile(); err != nil {
			return err
		}
		reloadService()
		return runCommand("systemctl", "enable", serviceName)
	case "uninstall":
		if err := ensureRoot("service uninstall"); err != nil {
			return err
		}
		_ = stopService()
		_ = disableService()
		if err := os.Remove(serviceFilePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		reloadService()
		return nil
	case "status":
		return runCommand("sudo", append([]string{"systemctl", "status", serviceName, "--no-pager"}, args...)...)
	case "start", "stop", "restart", "enable", "disable":
		return runCommand("sudo", append([]string{"systemctl", sub, serviceName}, args...)...)
	default:
		return fmt.Errorf("unknown service command: %s", sub)
	}
}

func serviceLogs(args []string) error {
	return runCommand("sudo", append([]string{"journalctl", "-u", serviceName}, args...)...)
}

func restartService() error { return runCommand("systemctl", "restart", serviceName) }
func stopService() error    { return runCommand("systemctl", "stop", serviceName) }
func disableService() error { return runCommand("systemctl", "disable", serviceName) }
func reloadService()        { _ = runCommand("systemctl", "daemon-reload") }

func serviceInstalled() bool { return fileExists(serviceFilePath) }

func serviceState() string {
	out, err := exec.Command("systemctl", "is-active", serviceName).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state != "" {
		return state
	}
	if err != nil {
		return "unknown"
	}
	return state
}

func scheduleWindowsUpgrade(_, _, _ string) error {
	return errors.New("Windows upgrade helper is unavailable on this platform")
}

func scheduleWindowsUninstall(_ bool) error {
	return errors.New("Windows uninstall helper is unavailable on this platform")
}
