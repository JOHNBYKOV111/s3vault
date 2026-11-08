package config

import (
	"testing"
)

func TestLoad_DefaultPort(t *testing.T) {
	t.Setenv("S3_PORT", "")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/db")
	t.Setenv("NODES_FILE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9000" {
		t.Errorf("expected default port 9000, got %s", cfg.Port)
	}
}

func TestLoad_MissingDB(t *testing.T) {
	t.Setenv("S3_PORT", "9000")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("NODES_FILE", "")

	cfg, err := Load()
	if err == nil {
		t.Fatal("expected error when DB is missing")
	}
	if cfg != nil {
		t.Fatal("expected nil config on error")
	}
}

func TestLoad_MissingNodesFile(t *testing.T) {
	t.Setenv("S3_PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/db")
	t.Setenv("NODES_FILE", "/nonexistent/nodes.yaml")

	cfg, err := Load()
	if err == nil {
		t.Fatal("expected error when NODES_FILE does not exist")
	}
	if cfg != nil {
		t.Fatal("expected nil config on error")
	}
}
