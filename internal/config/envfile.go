package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadCredentialsFile loads a small dotenv-compatible credentials file into
// the process environment. Existing environment variables always win. The
// parser deliberately does not execute shell syntax, which keeps it safe on
// both Unix and Windows and preserves values containing '='.
func LoadCredentialsFile(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read credentials file: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	values := make(map[string]string)
	order := make([]string, 0)
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("credentials line %d: expected KEY=VALUE", lineNo+1)
		}
		key = strings.TrimSpace(key)
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("credentials line %d: invalid key %q", lineNo+1, key)
		}
		value = strings.TrimSpace(value)
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			quote := value[0]
			if len(value) < 2 || value[len(value)-1] != quote {
				return fmt.Errorf("credentials line %d: unmatched value quote", lineNo+1)
			}
			value = value[1 : len(value)-1]
		}
		if strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("credentials line %d: value contains NUL", lineNo+1)
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}

	// Parse the complete file before mutating the process environment. This
	// prevents a later malformed line from leaving a partially loaded set of
	// credentials behind, and makes duplicate keys use the final file value.
	for _, key := range order {
		value := values[key]
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set credentials variable %q: %w", key, err)
		}
	}
	return nil
}
