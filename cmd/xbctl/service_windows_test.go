//go:build windows

package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/svc"
)

func TestWindowsServiceStoppedErrorIncludesExitCodesAndLog(t *testing.T) {
	err := windowsServiceStoppedError(svc.Status{
		State:                   svc.Stopped,
		Win32ExitCode:           1067,
		ServiceSpecificExitCode: 42,
	})
	got := err.Error()
	for _, want := range []string{"1067", "42", defaultLogPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q does not contain %q", got, want)
		}
	}
}
