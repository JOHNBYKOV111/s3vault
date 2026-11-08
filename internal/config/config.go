package config

import (
	"errors"
	"fmt"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	NodesFile   string
}

func Load() (*Config, error) {
	port := os.Getenv("S3_PORT")
	if port == "" {
		port = "9000"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	// NODES_FILE — опционально. Если задан — проверяем, что файл существует.
	nodesFile := os.Getenv("NODES_FILE")
	if nodesFile != "" {
		if _, err := os.Stat(nodesFile); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("NODES_FILE does not exist: %s", nodesFile)
			}
			return nil, fmt.Errorf("failed to stat NODES_FILE: %w", err)
		}
	}

	return &Config{
		Port:        port,
		DatabaseURL: dbURL,
		NodesFile:   nodesFile,
	}, nil
}
