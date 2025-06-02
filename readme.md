# TurboPG: Zero-Ops Vector Store for PostgreSQL

[![GoDoc](https://godoc.org/github.com/arjunsriva/turbopg?status.svg)](https://godoc.org/github.com/arjunsriva/turbopg)
[![Go Report Card](https://goreportcard.com/badge/github.com/arjunsriva/turbopg)](https://goreportcard.com/report/github.com/arjunsriva/turbopg)

**TurboPG** is a lightweight, zero-ops vector store implementation that brings the power of vector similarity search to your Go applications, leveraging the robust and familiar PostgreSQL database. Built on top of the [pgvector](https://github.com/pgvector/pgvector) extension, TurboPG allows you to easily add vector embeddings and perform fast similarity queries without managing separate vector database infrastructure.

**Key Features:**

*   **Zero-Ops**:  Uses your existing PostgreSQL database. No new infrastructure to manage!
*   **Simple Integration**:  Easy to integrate into your Go applications with a straightforward API.
*   **TurboPuffer API Compatibility**: Designed with compatibility in mind for potential migration to TurboPuffer in the future.
*   **Namespaces**: Organize your vectors into logical namespaces for multi-tenancy or feature experimentation.
*   **Vector Similarity Search**:  Perform fast Approximate Nearest Neighbor (ANN) search using cosine, Euclidean, and squared Euclidean distance metrics.
*   **Filtering**: Combine vector search with attribute-based filtering for precise results.
*   **Dynamic Migrations**:  Manages database schema migrations automatically.
*   **Flexible Attributes**: Store arbitrary JSON attributes alongside your vectors.
*   **Batch Operations**: Efficiently upsert and delete documents in batches.
*   **Testable**: Includes comprehensive unit and integration tests.

## Getting Started

### Prerequisites

*   **PostgreSQL 12+**
*   **pgvector extension** installed in your PostgreSQL database. (or call .Initialize() )
*   **Go 1.21+**

### Installation

```bash
go get github.com/arjunsriva/turbopg
```

### Initialization

First, you need to initialize the `turbopg` library against your PostgreSQL database. This ensures the `pgvector` extension is enabled and sets up the necessary system tables.

```go
package main

import (
	"context"
	"database/sql"
	"log"

	_ "github.com/lib/pq" // Import PostgreSQL driver
	"github.com/arjunsriva/turbopg"
)

func main() {
	ctx := context.Background()

	// Replace with your PostgreSQL connection string
	dbURL := "postgres://user:password@host:port/database?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := turbopg.Initialize(ctx, db); err != nil {
		log.Fatalf("Failed to initialize TurboPG: %v", err)
	}

	log.Println("TurboPG initialized successfully!")
}
```

### Basic Usage

Here's a quick example of creating a namespace, upserting documents, and performing a vector search:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/arjunsriva/turbopg"
)

func main() {
	ctx := context.Background()

	// Initialize database connection (as shown in Initialization section)
	dbURL := "postgres://user:password@host:port/database?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	turbopg.Initialize(ctx, db)

	// Create a new TurboPG store
	store, err := turbopg.NewDefault(db)
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}

	// Define namespace and dimensions
	namespaceName := "my_documents"
	dimensions := 128

	// Create a namespace
	err = store.CreateNamespace(ctx, namespaceName, turbopg.CreateNamespaceOptions{
		Dimensions: dimensions,
	})
	if err != nil {
		log.Fatalf("Failed to create namespace: %v", err)
	}
	log.Printf("Namespace '%s' created\n", namespaceName)

	// Upsert documents
	documents := []turbopg.Document{
		{
			ID:     "doc1",
			Vector: generateRandomVector(dimensions), // Replace with your embeddings
			Attributes: map[string]interface{}{
				"title": "Document 1",
				"category": "articles",
			},
		},
		{
			ID:     "doc2",
			Vector: generateRandomVector(dimensions),
			Attributes: map[string]interface{}{
				"title": "Document 2",
				"category": "blog posts",
			},
		},
	}

	upsertOpts := turbopg.UpsertOptions{Namespace: namespaceName}
	err = store.Upsert(ctx, documents, upsertOpts)
	if err != nil {
		log.Fatalf("Failed to upsert documents: %v", err)
	}
	log.Println("Documents upserted")

	// Perform vector search
	queryVector := generateRandomVector(dimensions)
	searchResults, err := store.SearchVector(ctx, namespaceName, queryVector, 2, "cosine")
	if err != nil {
		log.Fatalf("Vector search failed: %v", err)
	}

	fmt.Println("\nSearch Results:")
	for _, result := range searchResults {
		fmt.Printf("Document ID: %s, Score: %f, Title: %s\n",
			result.Document.ID, result.Score, result.Document.Attributes["title"])
	}
}


func generateRandomVector(dimensions int) []float32 {
	vector := make([]float32, dimensions)
	// In real application, replace with actual embedding generation logic
	for i := range vector {
		vector[i] = float32(i+1) / float32(dimensions)
	}
	return vector
}
```

**Example Output:**

```
2024/07/01 10:00:00 TurboPG initialized successfully!
2024/07/01 10:00:01 Namespace 'my_documents' created
2024/07/01 10:00:01 Documents upserted

Search Results:
Document ID: doc1, Score: 0.000000, Title: Document 1
Document ID: doc2, Score: 0.000000, Title: Document 2
```

## Usage

### Creating a Store

You can create a `Store` instance using `turbopg.New` with a custom configuration or `turbopg.NewDefault` for default settings.

```go
// Custom configuration
store, err := turbopg.New(db, turbopg.Config{
    Prefix: "myapp_", // Custom table prefix
    Logger: myLoggerInstance, // Your custom logger implementation
    DBURL:  "postgres://...", // Optional DB URL for migrations (defaults to postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable)
})

// Default configuration (prefix: "turbopg_", no-op logger)
store, err := turbopg.NewDefault(db)
```

### Namespace Operations

*   **Create Namespace**:
    ```go
    err := store.CreateNamespace(ctx, "products", turbopg.CreateNamespaceOptions{
        Dimensions: 512,
        IndexConfig: &turbopg.IndexConfig{ // Optional, defaults to cosine and lists=100
            DistanceMetric: "euclidean_squared",
            Lists:          250,
        },
    })
    ```

*   **Get Namespace**:
    ```go
    namespaceInfo, err := store.GetNamespace(ctx, "products")
    if err != nil {
        // Handle namespace not found or other errors
    }
    fmt.Printf("Namespace: %s, Dimensions: %d, Metric: %s\n",
        namespaceInfo.Name, namespaceInfo.Dimensions, namespaceInfo.IndexConfig.DistanceMetric)
    ```

*   **List Namespaces**:
    ```go
    namespaces, err := store.ListNamespaces(ctx, turbopg.ListNamespacesOptions{
        Prefix: "prod", // Optional prefix filter
        Limit:  10,    // Optional limit
    })
    if err != nil {
        // Handle error
    }
    fmt.Println("Total Namespaces:", namespaces.Total)
    fmt.Println("Namespaces:", namespaces.Namespaces)
    ```

*   **Delete Namespace**:
    ```go
    err = store.DeleteNamespace(ctx, "old_namespace")
    if err != nil {
        // Handle error
    }
    ```

### Document Operations

*   **Upsert Documents**:
    ```go
    docs := []turbopg.Document{ /* ... */ }
    err = store.Upsert(ctx, docs, turbopg.UpsertOptions{Namespace: "products"})

    // Batch Upsert for better performance with large datasets
    err = store.UpsertBatch(ctx, docs, turbopg.BatchUpsertOptions{
        UpsertOptions: turbopg.UpsertOptions{Namespace: "products"},
        BatchSize:     1000, // Optional batch size
    })
    ```

*   **Delete Documents by IDs**:
    ```go
    ids := []turbopg.DocumentID{"doc1", "doc2", "doc3"}
    err = store.Delete(ctx, "products", ids)
    ```

*   **Delete Documents by Filter**:
    ```go
    filter := turbopg.FilterCondition{
        Field: "category",
        Op:    turbopg.FilterOpEq,
        Value: "outdated",
    }
    err = store.DeleteByFilter(ctx, "products", filter)
    ```

### Query Operations

*   **Vector Search**:
    ```go
    queryVector := generateRandomVector(512)
    results, err := store.SearchVector(ctx, "products", queryVector, 5, "cosine")
    // or "euclidean", "euclidean_squared"
    ```

*   **Filtered Vector Search**:
    ```go
    queryVector := generateRandomVector(512)
    filter := turbopg.FilterCondition{
        Field: "price",
        Op:    turbopg.FilterOpLt,
        Value: 100, // Price less than 100
    }
    results, err := store.SearchFiltered(ctx, "products", queryVector, filter, 3, "euclidean")
    ```

*   **Advanced Query with Options**:
    ```go
    queryOpts := turbopg.QueryOptions{
        Namespace: "products",
        Vector:    queryVector, // Optional vector for similarity search
        Filter: turbopg.LogicalFilter{ // Optional filter
            Op: turbopg.LogicalOpAnd,
            Filters: []turbopg.Filter{
                turbopg.FilterCondition{Field: "in_stock", Op: turbopg.FilterOpEq, Value: true},
                turbopg.FilterCondition{Field: "category", Op: turbopg.FilterOpIn, Value: []interface{}{"electronics", "books"}},
            },
        },
        TopK:   10,
        Metric: "cosine", // Optional metric, defaults to cosine
    }
    results, err := store.Query(ctx, queryOpts)
    ```

### Filters

TurboPG supports a rich set of filter operations:

*   **Equality**: `FilterOpEq`, `FilterOpNotEq`
*   **Numeric Comparisons**: `FilterOpLt`, `FilterOpLte`, `FilterOpGt`, `FilterOpGte`
*   **String Matching**: `FilterOpGlob` (LIKE), `FilterOpNotGlob`, `FilterOpIGlob` (ILIKE), `FilterOpNotIGlob`
*   **IN/NOT IN**: `FilterOpIn`, `FilterOpNotIn`
*   **Logical Operations**: `LogicalOpAnd`, `LogicalOpOr` for combining filters

Filters can be nested for complex queries. See `filter.go` for full filter definition.

## Development

### Prerequisites

*   [VS Code](https://code.visualstudio.com/) (Recommended)
*   [Docker](https://www.docker.com/)
*   [VS Code Remote - Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) (Optional, for development container)

### Setting up Development Environment (using DevContainers)

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/arjunsriva/turbopg.git
    cd turbopg
    ```
2.  **Open in VS Code:**
    ```bash
    code .
    ```
3.  When prompted "Reopen in Container", click "Reopen in Container". VS Code will build a development container with all necessary tools and dependencies, including PostgreSQL with pgvector.

### Running Tests

```bash
# All tests (unit and integration)
make test

# Unit tests only (faster)
go test -v ./...

# Integration tests (requires Docker)
go test -tags=integration -v ./...

# Run linter
make lint

# Run tests with coverage
make coverage
```

### Development Tools

The development environment includes:

*   Go 1.21+
*   PostgreSQL 15 with pgvector extension
*   `golangci-lint` for linting
*   `goimports` for import formatting
*   `mockgen` for generating mocks

## Contributing

Contributions are welcome! Please feel free to:

*   **Report issues**: If you find a bug or have a feature request, please open an issue on GitHub.
*   **Submit pull requests**: If you'd like to contribute code, please fork the repository and submit a pull request with your changes.

Please follow the existing code style and ensure your contributions include relevant tests.

## License

This project is currently under development and does not have a specific license yet. It will be open-sourced under a permissive license (e.g., MIT or Apache 2.0) in the future.

---

## TurboPG API Server

A simple HTTP server that provides a [Turbopuffer](https://turbopuffer.com/)-compatible API for interacting with TurboPG. This server is useful for testing, local development, and for use with tools like `tpuf-benchmark`.

### Configuration

The server is configured using the following environment variables:

*   `DATABASE_URL`: The PostgreSQL connection string.
    *   Default: `postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable`
*   `TURBOPG_API_KEY`: The API key for authorizing requests.
    *   Default: `testapikey`
*   `TURBOPG_PORT`: The port on which the server will listen.
    *   Default: `8080`
*   `TURBOPG_STORE_PREFIX`: The prefix used for TurboPG's internal tables.
    *   Default: `tpga_` (note: the library `turbopg` defaults to `turbopg_`)

**Example of setting environment variables:**
```bash
export DATABASE_URL="postgres://myuser:mypass@localhost:5432/mydb?sslmode=verify-full"
export TURBOPG_API_KEY="yoursecureapikey123"
export TURBOPG_PORT="9000"
```

### Running the server

You can build and run the server using the provided Makefile targets:

```bash
# To build the server binary (output to ./bin/turbopg-server)
make build-server

# To run the server (after building)
# Ensure environment variables are set if you are not using defaults.
make run-server

# Example with custom API key:
# export TURBOPG_API_KEY="anothersecurekey"
# make run-server
```

The server will start and listen on the configured port (default 8080).

### Endpoints

The server exposes the following primary endpoints, designed for compatibility with `turbopuffer-tpuf-benchmark` and similar tools:

*   `POST /v1/namespaces/{namespace_name}`: Upserts data into the specified namespace. If the namespace does not exist, it will be created based on the data in the first upsert (vector dimensions, distance metric).
*   `DELETE /v1/namespaces/{namespace_name}`: Clears all data from a namespace by deleting and recreating it with the same configuration.
*   `HEAD /v1/namespaces/{namespace_name}`: Retrieves metadata about a namespace (e.g., approximate vector count, dimensions, distance metric) in response headers.
*   `POST /v1/namespaces/{namespace_name}/query`: Queries a namespace using vector similarity search and/or attribute filters.
*   `GET /v1/namespaces/{namespace_name}/_debug/{operation}`: Handles debug operations like `purge_cache` or `warm_cache`. These are currently no-ops for the `turbopg-api` server but are provided for benchmark compatibility.

---

**TurboPG** - Bring vector search to your PostgreSQL database effortlessly!