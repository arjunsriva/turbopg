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

## HTTP API

The server exposes TurboPuffer `/v1` and `/v2`. Official clients (`turbopuffer-go`, `turbopuffer` Python, `tpuf-benchmark`) set `base_url`.

*   `POST /v2/namespaces/{namespace}`: writes (`upsert_rows` / `upsert_columns` / `patch_rows` / `patch_columns` / `patch_by_filter` / `deletes` / `delete_by_filter`) and copy/branch (`copy_from_namespace` / `branch_from_namespace`). Namespaces are created on first write.
*   `POST /v2/namespaces/{namespace}/query`: ANN/kNN, BM25, attribute ranking (export: `rank_by: ["id","asc"]` + `id Gt` paging), filters, `aggregate_by` (`Count`, `Sum`), `include_attributes`. `multi_query` + `rerank_by: ["RRF"]` for hybrid search.
*   `GET|POST /v1/namespaces/{namespace}/schema`: read/update attribute schema (including `full_text_search` BM25 indexes).
*   `GET|PATCH /v1/namespaces/{namespace}/metadata`: namespace stats (`approx_row_count`, `index.status=up-to-date`). Pinning is a no-op.
*   `GET /v1/namespaces` / `GET /v2/namespaces`: list namespaces.
*   `POST /v2/namespaces/{namespace}/explain_query`, `GET .../hint_cache_warm`, `POST .../_debug/recall`, `GET .../_debug/{purge,warm}_cache`.
*   `DELETE /v2/namespaces/{namespace}`: drop the namespace (404 if missing).
*   Unauthenticated: `GET /healthz`, `GET /readyz`, `GET /version`, `GET /metrics`.

## Development

```bash
make test          # go test -race ./...
make lint
make coverage
```

Integration tests start Postgres 17 + pgvector + pg_textsearch from `docker/postgres` via testcontainers. `make test` is that suite; there is no separate `-tags=integration` gate.

## License

This project is currently under development and does not have a specific license yet.

**TurboPG** - TurboPuffer's API, on the Postgres you already run.
