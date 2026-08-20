package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: migrate <up|down|force> [value]")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fatal("DATABASE_URL is required")
	}
	path := os.Getenv("MIGRATIONS_PATH")
	if path == "" {
		path = "db/migrations"
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		fatal("resolve migrations path: %v", err)
	}
	migrations, err := migrate.New("file://"+absolute, databaseURL)
	if err != nil {
		fatal("open migrations: %v", err)
	}
	defer migrations.Close()

	switch os.Args[1] {
	case "up":
		err = migrations.Up()
		if errors.Is(err, migrate.ErrNoChange) {
			err = nil
		}
	case "down":
		steps := 1
		if len(os.Args) > 2 {
			steps, err = strconv.Atoi(os.Args[2])
		}
		if err == nil {
			err = migrations.Steps(-steps)
		}
	case "force":
		if len(os.Args) != 3 {
			fatal("usage: migrate force <version>")
		}
		version, parseErr := strconv.Atoi(os.Args[2])
		if parseErr != nil {
			fatal("invalid migration version: %v", parseErr)
		}
		err = migrations.Force(version)
	default:
		fatal("unknown migration command %q", os.Args[1])
	}
	if err != nil {
		fatal("migration failed: %v", err)
	}
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
