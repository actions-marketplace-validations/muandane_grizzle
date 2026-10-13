package grizzle_test

import (
	"testing"

	"github.com/muandane/grizzle"
)

func TestOptions_DevDatabaseURL_FieldExists(t *testing.T) {
	// Verify field is present on the Options struct.
	opts := grizzle.Options{
		SchemaSQL:      "CREATE TABLE t (id int primary key);",
		DevDatabaseURL: "postgres://127.0.0.1:5432/shadow_dev?sslmode=disable",
	}
	if opts.DevDatabaseURL == "" {
		t.Error("DevDatabaseURL field should be non-empty")
	}
}

func TestOptions_WithDevDatabaseURL(t *testing.T) {
	opts := grizzle.Options{SchemaSQL: "CREATE TABLE t (id int primary key);"}
	grizzle.WithDevDatabaseURL("postgres://127.0.0.1:5432/dev?sslmode=disable")(&opts)
	if opts.DevDatabaseURL != "postgres://127.0.0.1:5432/dev?sslmode=disable" {
		t.Errorf("WithDevDatabaseURL: got %q, want postgres://127.0.0.1:5432/dev?sslmode=disable", opts.DevDatabaseURL)
	}
}

func TestOptions_WithDevDatabaseURL_EmptyString(t *testing.T) {
	opts := grizzle.Options{SchemaSQL: "CREATE TABLE t (id int primary key);", DevDatabaseURL: "postgres://old"}
	grizzle.WithDevDatabaseURL("")(&opts)
	if opts.DevDatabaseURL != "" {
		t.Errorf("WithDevDatabaseURL empty string should clear the field, got %q", opts.DevDatabaseURL)
	}
}
