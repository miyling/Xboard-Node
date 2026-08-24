//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
)

const (
	windowsServiceName = "xboard-node"
	windowsStartupLog  = "xboard-node.log"
)

func runHosted(configPath, credentialsPath string) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows Service host: %w", err)
	}
	if !isService {
		ctx, stop := signalContext()
		defer stop()
		return runApplication(ctx, configPath, credentialsPath)
	}
	return svc.Run(windowsServiceName, &nodeWindowsService{
		configPath:      configPath,
		credentialsPath: credentialsPath,
	})
}

// signalContext is kept in a Windows-only file because console Ctrl+C and
// service control requests are separate host mechanisms on Windows.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	signal.Notify(ch, os.Interrupt)
	return ctx, func() {
		signal.Stop(ch)
		cancel()
	}
}

type nodeWindowsService struct {
	configPath      string
	credentialsPath string
}

func (s *nodeWindowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending, WaitHint: 10_000}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runApplicationWithReady(ctx, s.configPath, s.credentialsPath, func() {
			close(ready)
		})
	}()

	for {
		select {
		case <-ready:
			status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			return s.waitForService(requests, status, cancel, done)
		case err := <-done:
			return s.finish(status, err)
		case req, ok := <-requests:
			if !ok {
				cancel()
				return s.finish(status, <-done)
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 15_000}
				cancel()
				return s.finish(status, <-done)
			}
		}
	}
}

func (s *nodeWindowsService) waitForService(requests <-chan svc.ChangeRequest, status chan<- svc.Status, cancel context.CancelFunc, done <-chan error) (bool, uint32) {
	for {
		select {
		case err := <-done:
			return s.finish(status, err)
		case req, ok := <-requests:
			if !ok {
				cancel()
				return s.finish(status, <-done)
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 15_000}
				cancel()
				return s.finish(status, <-done)
			}
		}
	}
}

func (s *nodeWindowsService) finish(status chan<- svc.Status, err error) (bool, uint32) {
	if err != nil {
		reportHostError(s.configPath, s.credentialsPath, err)
		status <- svc.Status{State: svc.Stopped, Win32ExitCode: 1}
		return false, 1
	}
	status <- svc.Status{State: svc.Stopped}
	return false, 0
}

func startupLogPath(configPath string) string {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	return filepath.Join(filepath.Dir(configPath), "logs", windowsStartupLog)
}

// reportHostError is best-effort so an early startup failure still leaves a
// useful diagnostic when config.InitLogger has not opened the normal log yet.
func reportHostError(configPath, credentialsPath string, err error) {
	if err == nil {
		return
	}
	if credentialsPath == "" {
		credentialsPath = filepath.Join(filepath.Dir(configPath), "credentials.env")
	}
	path := startupLogPath(configPath)
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return
	}
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()
	message := sanitizeStartupError(err.Error(), credentialsPath)
	_, _ = fmt.Fprintf(f, "%s [ERROR] startup failed: %s\r\n", time.Now().Format(time.RFC3339), message)
}

func sanitizeStartupError(message, credentialsPath string) string {
	var values []string
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return message
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "'\"")
		if value != "" {
			values = append(values, value)
		}
		if envValue, exists := os.LookupEnv(key); exists && envValue != "" {
			values = append(values, envValue)
		}
	}
	for _, value := range values {
		message = strings.ReplaceAll(message, value, "<redacted>")
	}
	return message
}
