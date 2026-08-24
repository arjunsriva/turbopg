# TurboPG: TurboPuffer on your Postgres

**TurboPG** is a point-and-run [TurboPuffer](https://turbopuffer.com/)-compatible vector store on PostgreSQL. Official TurboPuffer clients work by setting the base URL; TurboPG serves the HTTP API against your database using [pgvector](https://github.com/pgvector/pgvector) for ANN/kNN and Timescale [pg_textsearch](https://github.com/timescale/pg_textsearch) for BM25.

**Key Features:**

*   **Point-and-run**: `DATABASE_URL` + `make run-server`. Official clients need only `api_key` and `base_url`.
*   **Zero-ops storage**: Uses your existing PostgreSQL. No new infrastructure to manage.
*   **TurboPuffer HTTP API**: Write (upsert/patch/delete), query (ANN/kNN, BM25, hybrid RRF), schema, filters, aggregations, copy/branch, export paging, metadata, list, explain, recall, and cache hints.
*   **Namespaces**: Logical collections with per-namespace tables and indexes.
*   **Go library**: Embed TurboPG in-process if you do not want the HTTP server.

## Getting Started

### Point-and-run server

Official TurboPuffer clients talk to TurboPG the same way they talk to TurboPuffer: set the API key and base URL.

**Requirements**

*   **PostgreSQL 17+** with [pgvector](https://github.com/pgvector/pgvector) and [pg_textsearch](https://github.com/timescale/pg_textsearch) (`shared_preload_libraries = 'pg_textsearch'`, then restart Postgres). Vector-only workloads can run on PostgreSQL 12+ with pgvector; BM25/hybrid need 17+.
*   **Go 1.22+** to build `turbopg-server`

A ready-made image is `docker/postgres/Dockerfile` (Postgres 17 + pgvector + pg_textsearch).

```bash
export DATABASE_URL="postgres://user:password@localhost:5432/postgres?sslmode=disable"
export TURBOPG_API_KEY="a-long-random-secret"
make run-server
```

`make run-server` sets `TURBOPG_ALLOW_INSECURE_API_KEY=1` so a missing key still starts locally with `testapikey`. Production must set a real `TURBOPG_API_KEY` and must not set the insecure override.

The server listens on `TURBOPG_LISTEN` (default `127.0.0.1:8080`; `TURBOPG_PORT` is the port fallback) and initializes extensions and system tables on startup. Probe `GET /healthz` and `GET /readyz` (no auth).

Go (official client):

```go
client := turbopuffer.NewClient(
	option.WithAPIKey("testapikey"),
	option.WithBaseURL("http://127.0.0.1:8080"),
)
ns := client.Namespace("my-ns")
```

Python (official client):

```python
import turbopuffer

tpuf = turbopuffer.Turbopuffer(
    api_key="testapikey",
    base_url="http://127.0.0.1:8080",
)
ns = tpuf.namespace("my-ns")
```

Native embeddings are optional: set `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY` (or `OPENAI_*` / `OPENROUTER_*`) to an OpenAI-compatible provider. Schema `embed` and query `["Embed", text]` then write real vectors into the namespace. Hosted demo models such as `example/random` are rejected. CMEK is rejected rather than storing plaintext. Sharding and cache-pinning flags are ignored on a single Postgres. Copy/branch is a full table copy, not copy-on-write.

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable` | Postgres connection string |
| `TURBOPG_API_KEY` | (required) | Bearer token official clients send as `api_key`. Refuses to start if unset or `testapikey` unless `TURBOPG_ALLOW_INSECURE_API_KEY=1` |
| `TURBOPG_LISTEN` | `127.0.0.1:8080` | Bind address. Use `0.0.0.0:8080` only when you mean it |
| `TURBOPG_PORT` | `8080` | Port used when `TURBOPG_LISTEN` is unset |
| `TURBOPG_STORE_PREFIX` | `tpga_` | Prefix for HTTP-server tables. The Go library default is `turbopg_`. Do not change a running server's prefix |
| `TURBOPG_DB_MAX_OPEN` / `MAX_IDLE` / `CONN_LIFETIME` | `25` / `5` / `1h` | Connection pool. Size against Postgres `max_connections` |
| `TURBOPG_READ_TIMEOUT` / `WRITE_TIMEOUT` / `IDLE_TIMEOUT` / `READ_HEADER_TIMEOUT` | `60s` / `120s` / `90s` / `10s` | HTTP timeouts |
| `TURBOPG_MAX_BODY_BYTES` | `33554432` (32MiB) | Max request body |
| `TURBOPG_STATEMENT_TIMEOUT` | (unset) | Postgres `statement_timeout` on each connection |
| `TURBOPG_IVFFLAT_LISTS` | `100` | IVFFlat lists for namespaces created by the HTTP server |
| `TURBOPG_IVFFLAT_PROBES` | `10` | `ivfflat.probes` for ANN queries |
| `TURBOPG_PATCH_BY_FILTER_MAX` / `DELETE_BY_FILTER_MAX` | `50000` / `5000000` | Filter-write caps; leftover rows return `rows_remaining` |
| `TURBOPG_LOG_LEVEL` | `info` | `error`, `info`, or `debug` |
| `TURBOPG_SHUTDOWN_TIMEOUT` | `30s` | SIGTERM drain |
| `TURBOPG_TLS_CERT` / `TURBOPG_TLS_KEY` | (unset) | Optional in-process TLS. Prefer a reverse proxy |
| `TURBOPG_PPROF_LISTEN` | (unset) | Optional private pprof bind, never the public mux |
| `EMBEDDING_BASE_URL` | (unset) | OpenAI-compatible embeddings URL (`OPENAI_BASE_URL` / `OPENROUTER_BASE_URL` also work) |
| `EMBEDDING_API_KEY` | (unset) | Embeddings API key (`OPENAI_API_KEY` / `OPENROUTER_API_KEY` also work) |

### Running in production

Required: `DATABASE_URL` and a non-default `TURBOPG_API_KEY`. Listen on loopback and put TLS on Caddy or nginx; or set `TURBOPG_LISTEN=0.0.0.0:8080` only on a private network. Orchestrators should probe `GET /healthz` (alive) and `GET /readyz` (Postgres ping). `GET /metrics` is Prometheus text. `GET /version` reports the build. Prefix is sticky: do not change `TURBOPG_STORE_PREFIX` after the first start.

Compose: `TURBOPG_API_KEY=... docker compose up --build`. systemd: `contrib/turbopg-server.service` with `TimeoutStopSec` matching `TURBOPG_SHUTDOWN_TIMEOUT`.

**Postgres must provide** pgvector (and pg_textsearch on 17 for BM25, with `shared_preload_libraries`), backups/PITR, `max_connections` greater than the pool, and disk for one table per namespace. Thousands of namespaces are fine; millions is not TurboPuffer’s “prefix on S3”.

**This binary will not** honor CMEK, invent `example/random` vectors, copy-on-write branches, billable bytes, or a cache hierarchy. `disable_backpressure` is a no-op because indexes are built in the write. IVFFlat `lists` for the library default is 1 (fast tests); the HTTP server default is 100.

### Library usage

Embed TurboPG in a Go process instead of (or in addition to) the HTTP server:

```bash
go get github.com/arjunsriva/turbopg
```

Initialize against PostgreSQL so `pgvector` (and `pg_textsearch` when present) are enabled and system tables exist:

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

The HTTP server is the product. Official clients set `base_url`. The Go package is the engine that server injects; `Store.Write` / `Store.Query` are the mutation and search units if you embed in-process.

```go
store, err := turbopg.New(db, turbopg.Config{Prefix: "turbopg_"})
_ = store.CreateNamespace(ctx, "docs", turbopg.CreateNamespaceOptions{Dimensions: 128})
_ = store.Write(ctx, turbopg.Write{
	Namespace: "docs",
	Upserts: []turbopg.Document{{ID: "1", Vector: []float32{1, 0 /* ... */}}},
})
hits, _ := store.Query(ctx, turbopg.QueryOptions{Namespace: "docs", Vector: query, TopK: 10})
_ = hits
```

Library IVFFlat default is `lists=1`. The HTTP server uses `TURBOPG_IVFFLAT_LISTS` (100) for new namespaces.

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
        IndexConfig: &turbopg.IndexConfig{ // Optional; library default is cosine and lists=1
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

`make run-server` is the supported way to run TurboPG as a TurboPuffer-compatible HTTP API. See [Point-and-run server](#point-and-run-server) for `DATABASE_URL`, auth, and official-client setup.

The server exposes the TurboPuffer HTTP API (`/v1` and `/v2`) so official clients (`turbopuffer-go`, `turbopuffer` Python, `tpuf-benchmark`) work by setting the base URL:

*   `POST /v2/namespaces/{namespace}`: writes (`upsert_rows` / `upsert_columns` / `patch_rows` / `patch_columns` / `patch_by_filter` / `deletes` / `delete_by_filter`) and copy/branch (`copy_from_namespace` / `branch_from_namespace`). Namespaces are created on first write.
*   `POST /v2/namespaces/{namespace}/query`: ANN/kNN, BM25, attribute ranking (export: `rank_by: ["id","asc"]` + `id Gt` paging), filters, `aggregate_by` (`Count`, `Sum`), `include_attributes`. `multi_query` + `rerank_by: ["RRF"]` for hybrid search.
*   `GET|POST /v1/namespaces/{namespace}/schema`: read/update attribute schema (including `full_text_search` BM25 indexes).
*   `GET|PATCH /v1/namespaces/{namespace}/metadata`: namespace stats (`approx_row_count`, `index.status=up-to-date`). Pinning is a no-op.
*   `GET /v1/namespaces` / `GET /v2/namespaces`: list namespaces.
*   `POST /v2/namespaces/{namespace}/explain_query`, `GET .../hint_cache_warm`, `POST .../_debug/recall`, `GET .../_debug/{purge,warm}_cache`.
*   `DELETE /v2/namespaces/{namespace}`: drop the namespace (404 if missing).

---

**TurboPG** - TurboPuffer's API, on the Postgres you already run.