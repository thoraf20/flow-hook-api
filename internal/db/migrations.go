package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RunMigrations executes all SQL migration files in order
// Looks for migrations in ../../migrations/ relative to this file
func RunMigrations(ctx context.Context) error {
	if Pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	// Get migrations directory
	// Try multiple paths depending on where the binary is run from
	var migrationsDir string
	var err error

	// Path 1: From backend/internal/db -> ../../../migrations (when running from flow-hook/)
	migrationsDir = filepath.Join("..", "..", "..", "migrations")
	if _, err = os.Stat(migrationsDir); err == nil {
		// Found it
	} else {
		// Path 2: From backend/ -> ../migrations (when running from backend/)
		migrationsDir = filepath.Join("..", "migrations")
		if _, err = os.Stat(migrationsDir); err == nil {
			// Found it
		} else {
			// Path 3: migrations/ (when running from flow-hook/)
			migrationsDir = "migrations"
			if _, err = os.Stat(migrationsDir); err != nil {
				return fmt.Errorf("could not find migrations directory. Tried: ../../../migrations, ../migrations, migrations")
			}
		}
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory %s: %w", migrationsDir, err)
	}

	// Sort and execute migrations
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		migrationPath := filepath.Join(migrationsDir, entry.Name())
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
		}

		// Execute migration in a transaction
		tx, txErr := Pool.BeginTx(ctx, pgx.TxOptions{})
		if txErr != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", entry.Name(), txErr)
		}
		_, execErr := tx.Exec(ctx, string(content))
		if execErr != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to execute migration %s: %w", entry.Name(), execErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to commit migration %s: %w", entry.Name(), commitErr)
		}

		fmt.Printf("✓ Executed migration: %s\n", entry.Name())
	}

	return nil
}
