//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func runHosted(configPath, credentialsPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runApplication(ctx, configPath, credentialsPath)
}

func reportHostError(_, _ string, _ error) {}
