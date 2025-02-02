package pgdynmigrate

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/docker/go-connections/nat"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestDB wraps a sql.DB with its container for testing
type TestDB struct {
	*sql.DB
	container testcontainers.Container
	ctx       context.Context
}

// setupTestDB creates a new PostgreSQL container and returns a TestDB
func setupTestDB(t *testing.T) *TestDB {
	t.Helper()
	ctx := context.Background()

	// Container configuration
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:0.8.0-pg15",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_DB":       "postgres",
		},
		WaitingFor: wait.ForSQL("5432/tcp", "postgres", func(host string, port nat.Port) string {
			return fmt.Sprintf("host=%s port=%s user=postgres password=postgres dbname=postgres sslmode=disable",
				host, port.Port())
		}),
	}

	// Start container
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Get mapped port
	mappedPort, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("failed to get container port: %v", err)
	}

	// Get host
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%s user=postgres password=postgres dbname=postgres sslmode=disable",
		host, mappedPort.Port())

	// Connect to database
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	return &TestDB{
		DB:        db,
		container: container,
		ctx:       ctx,
	}
}

// cleanup terminates the test container
func (db *TestDB) cleanup(t *testing.T) {
	if err := db.container.Terminate(context.Background()); err != nil {
		t.Fatalf("failed to terminate container: %v", err)
	}
}
