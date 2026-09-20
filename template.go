package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func createFile(path string) error {
	// Create parent directory
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	// Create file
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer f.Close()

	return nil
}

func main() {
	files := []string{
		"cmd/cli/main.go",
		"cmd/server/main.go",
		"internal/pool/handlers.go",
		"internal/pool/service.go",
		"internal/job/service.go",
		"internal/job/handlers.go",
		"internal/processor/handlers.go",
		"internal/processor/service.go",
		"internal/api/handlers.go",
		"internal/api/services.go",
		"internal/database/db.go",
		"internal/config/handlers.go",
		"internal/config/service.go",
		"migrations",
		"testdata/urls.txt",
		"Dockerfile",
		"docker-compose.yml",
		"Caddyfile",
	}

	for _, file := range files {
		if err := createFile(file); err != nil {
			fmt.Println("Error:", err)
			return
		}

		fmt.Println("Created:", file)
	}
}
