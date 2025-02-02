# pgdynmigrate

`pgdynmigrate` is a PostgreSQL-specific migration system that allows for dynamic, runtime creation of database migrations. It integrates with the `golang-migrate` library while providing additional features for safely appending new migrations in a distributed environment.

## Key Features

- **Dynamic Migration Creation**: Add new migrations at runtime, perfect for systems that need to generate migrations programmatically
- **Safe Concurrent Access**: Uses PostgreSQL advisory locks to ensure only one process can add migrations at a time
- **Sequential Versioning**: Guarantees sequential version numbers even in distributed systems
- **Transaction Safety**: All operations are transactional, ensuring consistency
- **golang-migrate Compatible**: Implements the `source.Driver` interface for seamless integration

## Usage

```go
import (
    "database/sql"
    "github.com/arjunsriva/turbopg/internal/pgdynmigrate"
    "github.com/golang-migrate/migrate/v4"
    _ "github.com/lib/pq"
)

// Create a new source
db, err := sql.Open("postgres", "postgres://localhost:5432/mydb?sslmode=disable")
if err != nil {
    log.Fatal(err)
}

// Initialize the source with a prefix for table names
source, err := pgdynmigrate.NewPostgresSource(db, "my_app_")
if err != nil {
    log.Fatal(err)
}

// Add a new migration
err = source.AddMigration(ctx, "create_users", 
    "CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT);",
    "DROP TABLE users;")
if err != nil {
    log.Fatal(err)
}

// Use with golang-migrate
m, err := migrate.NewWithDatabaseInstance(
    "dynmigrate://", 
    "postgres", 
    driver)
if err != nil {
    log.Fatal(err)
}

// Run migrations
err = m.Up()
if err != nil {
    log.Fatal(err)
}
```

## How It Works

1. **Table Structure**:
   - `{prefix}migration_requests`: Stores the actual migrations
   - `{prefix}migration_request_version`: Manages the version counter

2. **Concurrency Control**:
   - Uses PostgreSQL advisory locks to ensure safe concurrent access
   - Lock key is derived from the prefix using CRC32
   - Locks are automatically released when transactions end

3. **Version Management**:
   - Versions are sequential integers starting from 1
   - Each new migration gets the next available version
   - Version allocation is atomic and consistent

4. **Transaction Safety**:
   - All operations (version increment, migration creation) are in a single transaction
   - Rollback on any error maintains consistency
   - No gaps in version numbers

## Best Practices

1. **Prefix Selection**:
   - Use a unique prefix per application to avoid conflicts
   - Example: `myapp_`, `service1_`, etc.

2. **Migration Names**:
   - Use descriptive names that indicate the purpose
   - Include timestamps or sequential numbers if helpful
   - Example: `create_users_table`, `add_email_column`

3. **Error Handling**:
   - Always check for errors when adding migrations
   - Consider implementing retry logic for temporary failures
   - Log failed attempts for debugging

4. **Testing**:
   - Test concurrent access scenarios
   - Verify rollback behavior
   - Ensure migrations are idempotent

## Limitations

1. PostgreSQL-specific implementation
2. Requires PostgreSQL advisory locks
3. No built-in SQL validation (by design)
4. Must maintain unique prefixes across applications

## Contributing

Feel free to open issues or submit pull requests for:
- Bug fixes
- Documentation improvements
- Feature requests
- Test coverage improvements 