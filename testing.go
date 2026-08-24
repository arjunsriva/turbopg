package turbopg

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/docker/go-connections/nat"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type testLogger struct {
	t *testing.T
}

func (l *testLogger) Info(msg string, fields ...Field) {
	l.t.Logf("INFO: %s %v", msg, fields)
}

func (l *testLogger) Error(msg string, fields ...Field) {
	l.t.Logf("ERROR: %s %v", msg, fields)
}

func (l *testLogger) Debug(msg string, fields ...Field) {
	l.t.Logf("DEBUG: %s %v", msg, fields)
}

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

	req := postgresTestRequest()

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

func postgresTestRequest() testcontainers.ContainerRequest {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)
	return testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    filepath.Join(root, "docker", "postgres"),
			Dockerfile: "Dockerfile",
			Repo:       "turbopg-test-pg",
			Tag:        "17-pgvector-textsearch",
			KeepImage:  true,
		},
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_DB":       "postgres",
		},
		Cmd: []string{"postgres", "-c", "shared_preload_libraries=pg_textsearch"},
		WaitingFor: wait.ForSQL("5432/tcp", "postgres", func(host string, port nat.Port) string {
			return fmt.Sprintf("host=%s port=%s user=postgres password=postgres dbname=postgres sslmode=disable",
				host, port.Port())
		}),
	}
}

// cleanup terminates the test container
func (db *TestDB) cleanup(t *testing.T) {
	if err := db.container.Terminate(context.Background()); err != nil {
		t.Fatalf("failed to terminate container: %v", err)
	}
}

// Cleanup terminates the testcontainer. Exported for tests in other packages.
func (db *TestDB) Cleanup(t *testing.T) {
	t.Helper()
	db.cleanup(t)
}

// DatabaseURL returns the URL for connecting to the test database
func (db *TestDB) DatabaseURL(t *testing.T) string {
	t.Helper()
	host, err := db.container.Host(db.ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	mappedPort, err := db.container.MappedPort(db.ctx, "5432")
	if err != nil {
		t.Fatalf("failed to get container port: %v", err)
	}
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/postgres?sslmode=disable",
		host, mappedPort.Port())
}

// TestStore creates a new Store instance for testing
func TestStore(t *testing.T, prefix string) (*Store, *TestDB) {
	db := setupTestDB(t)
	store, err := New(db.DB, Config{Prefix: prefix})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store, db
}

// NewTestDB creates a new test database
func NewTestDB(t *testing.T) *TestDB {
	req := postgresTestRequest()

	// Start container
	container, err := testcontainers.GenericContainer(context.Background(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Get mapped port
	mappedPort, err := container.MappedPort(context.Background(), "5432")
	if err != nil {
		t.Fatalf("failed to get container port: %v", err)
	}

	// Create connection string
	dbURL := fmt.Sprintf("postgres://postgres:postgres@localhost:%s/postgres?sslmode=disable", mappedPort.Port())

	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	// Return test database
	return &TestDB{
		container: container,
		DB:        db,
	}
}

// NewTestStore creates a new Store for testing
func NewTestStore(t *testing.T, prefix string) (*Store, testcontainers.Container) {
	db := NewTestDB(t)
	store, err := New(db.DB, Config{Prefix: prefix})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store, db.container
}

// AssertError checks if an error occurred as expected in tests.
func AssertError(t *testing.T, err error, wantErr bool, testName string) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("%s: error = %v, wantErr %v", testName, err, wantErr)
		t.FailNow() // Stop test execution if error assertion fails
	}
	if err != nil && !wantErr {
		t.Fatalf("%s: unexpected error: %v", testName, err) // Fail hard on unexpected error
	}
}

// SetupTestStore creates a new Store instance and TestDB for testing,
// handling common setup steps.
func SetupTestStore(t *testing.T, prefix string, withLogger bool) (*Store, *TestDB, context.Context) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	var logger Logger
	if withLogger {
		logger = &testLogger{t: t}
	}

	store, err := New(db.DB, Config{
		Prefix: prefix,
		DBURL:  db.DatabaseURL(t),
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store, db, ctx
}

// AssertQueryResultIDs checks if the result document IDs match the expected IDs in order.
func AssertQueryResultIDs(t *testing.T, results []QueryResult, wantIDs []DocumentID, testName string) {
	t.Helper()
	if len(results) != len(wantIDs) {
		t.Errorf("%s: got %d results, want %d", testName, len(results), len(wantIDs))
		return
	}
	for i, want := range wantIDs {
		if results[i].Document.ID != want {
			t.Errorf("%s: result %d got ID %s, want %s", testName, i, results[i].Document.ID, want)
		}
	}
}

// NewTestDocument creates a Document struct with default or provided values.
func NewTestDocument(id DocumentID, vector []float32, attributes map[string]interface{}) Document {
	return Document{
		ID:         id,
		Vector:     vector,
		Attributes: attributes,
	}
}
