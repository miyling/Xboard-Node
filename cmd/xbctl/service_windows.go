//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func ensureRoot(cmd string) error {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if !elevated {
		return fmt.Errorf("%s requires administrator privileges; run PowerShell as Administrator", cmd)
	}
	return nil
}

func withWindowsService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s)
}

func serviceAction(sub string, _ []string) error {
	switch sub {
	case "install":
		return installWindowsService()
	case "uninstall":
		return uninstallWindowsService()
	case "status":
		return withWindowsService(func(s *mgr.Service) error {
			st, err := s.Query()
			if err != nil {
				return err
			}
			fmt.Printf("SERVICE_NAME: %s\n\tSTATE: %s\n", serviceName, windowsServiceState(st.State))
			return nil
		})
	case "start":
		if err := ensureRoot("service start"); err != nil {
			return err
		}
		return startWindowsService()
	case "stop":
		if err := ensureRoot("service stop"); err != nil {
			return err
		}
		return stopWindowsService()
	case "restart":
		if err := ensureRoot("service restart"); err != nil {
			return err
		}
		if err := stopWindowsService(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
		return startWindowsService()
	case "enable", "disable":
		if err := ensureRoot("service " + sub); err != nil {
			return err
		}
		return updateWindowsServiceStartType(sub == "enable")
	default:
		return fmt.Errorf("unknown service command: %s", sub)
	}
}

func serviceLogs(args []string) error {
	follow := false
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" {
			follow = true
		}
	}
	f, err := os.Open(defaultLogPath)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", defaultLogPath, err)
	}
	defer f.Close()
	if !follow {
		_, err := f.Seek(0, 0)
		if err != nil {
			return err
		}
		_, err = io.Copy(os.Stdout, f)
		return err
	}

	var offset int64
	for {
		if _, err := f.Seek(offset, 0); err != nil {
			return err
		}
		data := make([]byte, 32*1024)
		n, readErr := f.Read(data)
		if n > 0 {
			if _, err := os.Stdout.Write(data[:n]); err != nil {
				return err
			}
			offset += int64(n)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func restartService() error { return serviceAction("restart", nil) }
func stopService() error    { return stopWindowsService() }
func disableService() error { return updateWindowsServiceStartType(false) }
func reloadService()        {}

func serviceInstalled() bool {
	err := withWindowsService(func(*mgr.Service) error { return nil })
	return err == nil
}

func serviceState() string {
	state := "not-installed"
	_ = withWindowsService(func(s *mgr.Service) error {
		st, err := s.Query()
		if err == nil {
			state = windowsServiceState(st.State)
		}
		return err
	})
	return state
}

func windowsServiceState(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start-pending"
	case svc.Running:
		return "running"
	case svc.StopPending:
		return "stop-pending"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("state-%d", state)
	}
}

func installWindowsService() error {
	if err := ensureRoot("service install"); err != nil {
		return err
	}
	if _, err := os.Stat(defaultBinaryPath); err != nil {
		return fmt.Errorf("node binary %q is missing: %w", defaultBinaryPath, err)
	}
	if err := os.MkdirAll(defaultInstallRoot, 0o755); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if old, err := m.OpenService(serviceName); err == nil {
		_ = old.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}
	s, err := m.CreateService(serviceName, defaultBinaryPath, mgr.Config{
		DisplayName:  "Xboard Node",
		Description:  "Xboard Node backend",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "-c", defaultConfigPath, "-credentials", defaultCredentialsPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60); err != nil {
		_ = s.Delete()
		return fmt.Errorf("configure service recovery: %w", err)
	}
	// A fatal application error is reported as SERVICE_STOPPED with a non-zero
	// exit code. Count that as a failure, while a normal Stop/Shutdown still
	// exits with code zero and remains a controlled stop.
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		_ = s.Delete()
		return fmt.Errorf("configure non-crash service recovery: %w", err)
	}
	fmt.Printf("Installed Windows service %q\n", serviceName)
	return nil
}

func uninstallWindowsService() error {
	if err := ensureRoot("service uninstall"); err != nil {
		return err
	}
	return withWindowsService(func(s *mgr.Service) error {
		if err := stopWindowsService(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
		if err := s.Delete(); err != nil {
			return fmt.Errorf("delete service: %w", err)
		}
		fmt.Printf("Uninstalled Windows service %q\n", serviceName)
		return nil
	})
}

func startWindowsService() error {
	return withWindowsService(func(s *mgr.Service) error {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Running {
			return nil
		}
		if st.State == svc.StartPending {
			return waitWindowsService(s, svc.Running)
		}
		if err := s.Start(); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		return waitWindowsService(s, svc.Running)
	})
}

func stopWindowsService() error {
	return withWindowsService(func(s *mgr.Service) error {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Stopped {
			return nil
		}
		if st.State == svc.StopPending {
			return waitWindowsService(s, svc.Stopped)
		}
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("stop service: %w", err)
		}
		return waitWindowsService(s, svc.Stopped)
	})
}

func waitWindowsService(s *mgr.Service, want svc.State) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == want {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach %s", windowsServiceState(want))
}

func updateWindowsServiceStartType(enable bool) error {
	return withWindowsService(func(s *mgr.Service) error {
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		if enable {
			cfg.StartType = mgr.StartAutomatic
		} else {
			cfg.StartType = mgr.StartDisabled
		}
		return s.UpdateConfig(cfg)
	})
}
