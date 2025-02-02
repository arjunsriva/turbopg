package pgdynmigrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
)

type Migrator struct {
	db     *sql.DB
	prefix string
	m      *migrate.Migrate
	source *PostgresSource
}

// New creates a new migrator with the given prefix.
// It sets up both the source tables and the migrate instance.
func New(db *sql.DB, prefix string) (*Migrator, error) {
	// Create our database-backed source
	source, err := NewPostgresSource(db, prefix)
	if err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}

	// Create postgres driver with prefixed table
	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: prefix + "schema_migrations",
	})
	if err != nil {
		return nil, fmt.Errorf("create postgres driver: %w", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithInstance(
		"dynmigrate",
		source,
		"postgres",
		driver,
	)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}

	return &Migrator{
		db:     db,
		prefix: prefix,
		m:      m,
		source: source,
	}, nil
}

// Append adds a new migration to the queue.
// The migration will not be applied until Up() is called.
func (m *Migrator) Append(ctx context.Context, name string, up, down string) error {
	return m.source.AddMigration(ctx, name, up, down)
}

// Up brings the database up to date by applying all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	return m.m.Up()
}
