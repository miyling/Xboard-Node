//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "xboard-node"

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
	// os.Interrupt is delivered for Ctrl+C in an interactive Windows console.
	// A service never enters this branch.
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	// os/signal is intentionally registered lazily here to keep the service
	// handler independent of console signal delivery.
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
	done := make(chan error, 1)
	go func() {
		done <- runApplication(ctx, s.configPath, s.credentialsPath)
	}()

	accepted := svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case err := <-done:
			if err != nil {
				status <- svc.Status{State: svc.Stopped, Win32ExitCode: 1}
				return false, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		case req, ok := <-requests:
			if !ok {
				cancel()
				err := <-done
				if err != nil {
					return false, 1
				}
				return false, 0
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 15_000}
				cancel()
				err := <-done
				if err != nil {
					status <- svc.Status{State: svc.Stopped, Win32ExitCode: 1}
					return false, 1
				}
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
