//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeStartupErrorRedactsCredentialValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(path, []byte("TOKEN=secret-value\r\nOTHER='another-secret'\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := sanitizeStartupError("panel rejected secret-value and another-secret", path)
	if strings.Contains(got, "secret-value") || strings.Contains(got, "another-secret") {
		t.Fatalf("sanitized error leaked a credential: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("sanitized error = %q, want redaction marker", got)
	}
}

func TestReportHostErrorWritesRedactedFallbackLog(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yml")
	credentialsPath := filepath.Join(root, "credentials.env")
	if err := os.WriteFile(credentialsPath, []byte("TOKEN=secret-value\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reportHostError(configPath, credentialsPath, fmt.Errorf("panel rejected secret-value"))
	data, err := os.ReadFile(startupLogPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "secret-value") || !strings.Contains(got, "panel rejected <redacted>") {
		t.Fatalf("fallback log = %q, want redacted actionable error", got)
	}
}

func TestStartupLogPathUsesConfigDirectory(t *testing.T) {
	configPath := filepath.Join(`C:\ProgramData`, "xboard-node", "config.yml")
	got := startupLogPath(configPath)
	want := filepath.Join(`C:\ProgramData`, "xboard-node", "logs", windowsStartupLog)
	if !strings.EqualFold(got, want) {
		t.Fatalf("startup log path = %q, want %q", got, want)
	}
}
