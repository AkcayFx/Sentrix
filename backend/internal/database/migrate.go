package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func RunMigrations(db *gorm.DB, dir string) error {
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("failed to read migration dir: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		version := filepath.Base(f)

		var count int64
		db.Raw("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if count > 0 {
			continue
		}

		sql, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", version, err)
		}

		if err := db.Exec(string(sql)).Error; err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}

		if err := db.Exec(
			"INSERT INTO schema_migrations (version) VALUES (?)", version,
		).Error; err != nil {
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}

		log.Infof("applied migration: %s", version)
	}

	return nil
}
