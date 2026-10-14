package main

import (
	"testing"
)

func TestCLI_DevURLFlag_Parsed(t *testing.T) {
	// The --dev-url flag must be defined on the flagset and available to all
	// subcommands (plan, apply, check, lint). We exercise the parse path
	// without opening a database; the flag must not cause an error and must
	// populate the devURL variable.

	tests := []struct {
		name    string
		args    []string
		wantURL string
	}{
		{
			name:    "dev-url equals syntax",
			args:    []string{"plan", "--dsn", "sqlite::memory:", "--dev-url=postgres://dev/db"},
			wantURL: "postgres://dev/db",
		},
		{
			name:    "dev-url space syntax",
			args:    []string{"plan", "--dsn", "sqlite::memory:", "--dev-url", "postgres://dev/db2"},
			wantURL: "postgres://dev/db2",
		},
		{
			name:    "dev-url empty (not provided)",
			args:    []string{"plan", "--dsn", "sqlite::memory:"},
			wantURL: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDevURL(tc.args)
			if err != nil {
				t.Fatalf("parseDevURL(%v): %v", tc.args, err)
			}
			if got != tc.wantURL {
				t.Errorf("parseDevURL(%v) = %q, want %q", tc.args, got, tc.wantURL)
			}
		})
	}
}
