package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.env")
	t.Setenv("XBOARD_TEST_EXISTING", "from-env")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# generated\r\nXBOARD_TEST_TOKEN=abc=def\r\nXBOARD_TEST_QUOTED='hello world'\r\nXBOARD_TEST_EXISTING=from-file\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadCredentialsFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("XBOARD_TEST_TOKEN"); got != "abc=def" {
		t.Fatalf("token = %q, want abc=def", got)
	}
	if got := os.Getenv("XBOARD_TEST_QUOTED"); got != "hello world" {
		t.Fatalf("quoted = %q, want hello world", got)
	}
	if got := os.Getenv("XBOARD_TEST_EXISTING"); got != "from-env" {
		t.Fatalf("existing = %q, want from-env", got)
	}
}

func TestLoadCredentialsFileRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(path, []byte("not-an-assignment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadCredentialsFile(path); err == nil {
		t.Fatal("expected malformed credentials file to fail")
	}
}
