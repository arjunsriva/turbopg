# TurboPG backlog

Work against this file. Check a box in the same PR that lands the item.

**Product:** HTTP sidecar in front of the operator’s Postgres. Official TurboPuffer clients set `base_url`. 400 only if honoring the request would lie (CMEK → plaintext, `example/random` embeddings). Never invent random vectors, billable bytes, or a cache hierarchy.

**Ownership:** this binary owns process lifecycle, auth, request limits, pool, IVFFlat knobs, probes, and metrics. Postgres owns backup, HA, WAL, disk, and encryption.

---

## How to pick work

1. P0 until origin matches the tree.
2. P1 until the binary is operable (fail-closed key, listen, probes, drain, env knobs, metrics, runbook).
3. P2 protocol/schema only when a client sends it or a filter can rewrite the whole table.
4. Do not empty `tests/official-python/allowlist.txt` for its own sake.
5. Do not pick anything in [Out of scope](#out-of-scope).

---

## Done — do not redo

Architecture pass (Server inject, typed `InvalidInput`, `schema.go`, `Store.Write` / `Query` unification, HTTP timeouts as constants, official test split, `aggregate.go`).

Drop-in that already matches:

- Mixed writes in one transaction; real `rows_affected`; `return_affected_ids`
- CMEK 400; native BYO embeddings (`EMBEDDING_*`); `example/random` 400
- Cosine / euclidean_squared ranking; Contains / In on arrays; Lt/Lte nulls
- Product argument order; inferred GET schema; upsert_rows object → 422
- SparseKNN (exact); late-interaction MaxSim; server RRF
- Sharding / region flags accepted; `disable_backpressure` no-op (correct)
- FTS token filters; base64 vectors; empty export `rows: []`
- v1 + v2 mux; copy/branch as full table copy

Docs hygiene already landed: native embeddings in readme, `tpga_` vs `turbopg_`, CLAUDE.md PG 12+ vector / 17+ BM25, deleted `TURBOPG_ENHANCEMENTS.md`.

---

## P0 — ship the tree

- [x] **P0-branch** Get the dirty tree off `main` (branch, commit, PR). `origin/main` is not this sidecar.
- [x] **P0-ci-go** GitHub workflow: `go test -race ./...` and `golangci-lint` on the pg17 image (`docker/postgres`). Makefile `all` is not a substitute.
  - Files: `.github/workflows/go.yml`. Tests build `docker/postgres` via testcontainers; CI also `make build-server` (ldflags).
- [x] **P0-ci-pg17-smoke** Point `benchmark_test.yml` at pg17 + pg_textsearch (or drop it once Go CI exists). Today it uses `pgvector/pgvector:0.8.0-pg15`.
  - Files: `.github/workflows/benchmark_test.yml` builds `docker/postgres` and curls `/readyz`.

---

## P1 — operator control plane

The missing product. Ship as one slice if possible: fail closed, then probes/drain, then knobs, then signals.

### Process lifecycle

- [x] **P1-api-key** Refuse to start if `TURBOPG_API_KEY` is unset or still `testapikey`. Dev override: `TURBOPG_ALLOW_INSECURE_API_KEY=1`.
  - Files: `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-listen** `TURBOPG_LISTEN` (host:port). Default `127.0.0.1:8080`. Listening on `0.0.0.0` must be explicit. Keep `TURBOPG_PORT` as a fallback.
  - Files: `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-probes** Unauthenticated `GET /healthz` (process up) and `GET /readyz` (`db.Ping`). CI should curl `/readyz`, not `/v1/namespaces` with a key.
  - Files: `cmd/turbopg-server/server.go`, `ops.go`, `.github/workflows/official_correctness.yml`
- [x] **P1-shutdown** SIGINT/SIGTERM → `http.Server.Shutdown` then close the pool. `TURBOPG_SHUTDOWN_TIMEOUT` default 30s. Today `defer db.Close()` never runs on kill.
  - Files: `cmd/turbopg-server/main.go`
- [x] **P1-version** Log version on boot; `GET /version` (no auth). ldflags from CI.
  - Files: `cmd/turbopg-server/main.go`, `version.go`, `Makefile` `LDFLAGS`, `.github/workflows/go.yml` `make build-server`

### Knobs that are currently constants

- [x] **P1-pool** `TURBOPG_DB_MAX_OPEN` / `MAX_IDLE` / `CONN_LIFETIME`. Document: size against Postgres `max_connections`.
  - Files: `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-http-timeouts** Env for read / write / idle / header timeouts (today hardcoded 10/60/120/90s).
  - Files: `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-max-body** `TURBOPG_MAX_BODY_BYTES` (e.g. 32MiB). Unbounded reads are not operable.
  - Files: `cmd/turbopg-server/config.go`, `ops.go`
- [x] **P1-statement-timeout** `TURBOPG_STATEMENT_TIMEOUT` applied on connections (`SET`). Stops one ANN from holding a worker forever.
  - Files: `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-ivfflat-lists** Operator default `TURBOPG_IVFFLAT_LISTS` (readme says 100; `NewDefaultIndexConfig.Lists` is 1). Persist the value used at namespace create. Accept client `lists` if sent. Do not silent-HNSW.
  - Files: `namespace.go`, `cmd/turbopg-server/config.go`, `readme.md`. IVFFlat is created once the table has at least `lists` rows (pgvector will not index an empty table).
- [x] **P1-ivfflat-probes** `TURBOPG_IVFFLAT_PROBES` (today `SET ivfflat.probes = 10` in `Query`).
  - Files: `store.go`, `cmd/turbopg-server/config.go`, `main.go`
- [x] **P1-write-caps** `TURBOPG_PATCH_BY_FILTER_MAX` (50k) / `DELETE_BY_FILTER_MAX` (5M) + `rows_remaining` so the client can retry. Do not fake 429 on `unindexed_bytes`.
  - Files: `write.go`, `document_delete.go`, `cmd/turbopg-server/handlers.go`, `api_models.go`
- [x] **P1-log-level** `TURBOPG_LOG_LEVEL=info|debug`. Access log: method, path, status, duration, `request_id`. Never log vectors or API keys.
  - Files: `logger.go`, `cmd/turbopg-server/ops.go`, `main.go`

### Signals

- [x] **P1-metrics** Prometheus `GET /metrics`: request count, latency histogram, in-flight, db pool (open/idle/wait), 4xx/5xx.
  - Files: `cmd/turbopg-server/ops.go`, `server.go`
- [x] **P1-boot-banner** On start, log listen, prefix, pool, lists/probes, embeddings on/off. Never print the API key.
  - Files: `cmd/turbopg-server/main.go`

### Runbook (docs, not an essay)

- [x] **P1-runbook** “Running in production” in `readme.md`: required env (`DATABASE_URL`, `TURBOPG_API_KEY`), listen loopback + reverse proxy for TLS, `/healthz` `/readyz`, prefix is sticky, systemd or compose example.
- [x] **P1-postgres-owns** What Postgres must provide: pgvector (pg_textsearch on 17 for BM25), `shared_preload_libraries`, backups/PITR, `max_connections` > pool, disk for one table per namespace (thousands of namespaces is fine; millions is not S3 prefixes).
- [x] **P1-will-not** What this binary will not do: CMEK, `example/random`, COW branches, billable bytes, cache hierarchy. `disable_backpressure` is a no-op because indexes are built in the write.
- [x] **P1-compose** Minimal `docker-compose` (or systemd unit): Postgres 17 image + server, `Restart=on-failure`, `TimeoutStopSec` matching drain. No Kubernetes operator until someone asks.
  - Files: `readme.md`, `docker-compose.yml`, `contrib/turbopg-server.service`

TLS in-process (`TURBOPG_TLS_CERT`/`KEY`) is implemented as P3-in-process-tls. The runbook still prefers Caddy/nginx.

---

## P2 — protocol and schema (when a client or corpus forces it)

Cheap validation first; extra columns last.

- [x] **P2-id-limits** 64-byte ids; attribute names must not start with `$`; max `top_k` / `limit` 10k; max 16 multi-queries. 400 with a clear message. Namespace names already match `[A-Za-z0-9-_.]{1,128}`.
  - Files: `limits.go`, `write.go`, `cmd/turbopg-server/handlers.go`, `handlers_admin.go`
- [x] **P2-consistency** Query `consistency`: `strong` is the only real Postgres mode. Accept `eventual` as strong **or** 400. Do not serve stale rows.
  - Files: `cmd/turbopg-server/api_models.go`, `handlers.go`
- [x] **P2-uuid-datetime** Schema types `uuid` / `datetime` when a write sends them. Store as text / timestamptz; do not silently keep them as string forever.
  - Files: `typed_attrs.go`, `write.go` (canonical UUID / RFC3339 in JSONB)
- [x] **P2-trigram** When schema sets `regex` / `glob`, create `pg_trgm` indexes. Today glob/regex is LIKE/~ on JSONB with no index.
  - Files: `bm25.go` `ensureTrigramIndex`
- [x] **P2-second-vector** Extra pgvector column / second `embed` field when a schema has two embeds (hosted allows 4). Schema parse exists; extra columns do not.
  - Files: `extra_vector.go`, `schema.go`, `write.go`, `query.go`, `cmd/turbopg-server/embed.go`
- [x] **P2-compute-attributes** Per-clause BM25 / `$dist` when `rank_by` is Sum/Product.
  - Files: `query.go` `attachComputed`, `cmd/turbopg-server/handlers.go`
- [x] **P2-protocol-leftovers** Only if an official client sends them: `last_as_prefix`, AnyGt family, `limit.per`, `last_write_at`, `filterable: false` also blocking sort, `group_by` order/cap, `distance_metric` required when vectors exist.
  - Files: `filter.go`, `aggregate.go`, `cmd/turbopg-server/`

---

## P3 — hygiene (do not start until P1 is done)

- [x] **P3-handlers-compat** Rename `cmd/turbopg-server/handlers_compat.go` (list/schema/metadata/explain/recall — not “compat”).
- [x] **P3-extra-coverage** Split `extra_coverage_test.go` next to the code it covers; delete the junk drawer.
- [x] **P3-readme-library** Readme library examples still lead with `Upsert` + `SearchVector`. HTTP is the product; keep a short embed-in-process section.
  - Files: `readme.md`
- [x] **P3-query-split** Split `query.go` compile vs execute only if hybrid BM25+MaxSim becomes unreadable. Not a cleanup pass.
  - Skipped: `query.go` is still readable; splitting would be cleanup-for-its-own-sake.
- [x] **P3-pprof** `TURBOPG_PPROF_LISTEN` on a private bind. Never on the public mux.
- [x] **P3-in-process-tls** Optional cert/key env if someone refuses a reverse proxy.

---

## Out of scope

Do not pick these up as backlog items.

| Item | Why |
| --- | --- |
| Extract `internal/query`, `internal/write`, `internal/filter` | Three files moving is not a boundary |
| lib/pq → pgx | No product reason until pipeline mode or LISTEN |
| Silent HNSW migration | Index type is an engine choice; flip with a corpus |
| Multi-tenant API keys, orgs, regions | Different product |
| Fake `billable_*_bytes`, `unindexed_bytes` theater, cache temperature | Lies |
| SPFresh, WAL, LSM on S3, COW branches, hash sharding, pinning | Hosted storage engine |
| Managed Voyage/OpenAI catalog / `example/random` production vectors | BYO `EMBEDDING_*` is the Postgres shape |
| Synthesize 429s for backpressure | Indexes commit in the write; accepting writes is correct |
| FTS highlighting / `pre_tokenized_array` | Until someone queries them (see allowlist) |
| Wrap `pg_dump` / in-process HA | Postgres’s job |
| Kubernetes operator | compose/systemd is enough |
| Rewrite filters as a SQL-builder framework | The AST already is |
| Empty the Python allowlist | Skips are hosted-specific or shared-state tests |

---

## Official Python skips — not a todo

From `tests/official-python/allowlist.txt`. Implement only if a real app hits them.

| Skip | Reason |
| --- | --- |
| `test_bm25_pre_tokenized_array` | Tokenizer + typed 422 |
| `test_perf_metrics` | `exhaustive_search_count` is not a real scan count |
| `test_query_vectors` | Billing bytes are hosted-specific (we are always 0) |
| `test_upsert_base64_vectors` | Id-order assertion after mixed writes |
| `test_delete_vectors` / `test_upsert_columns` / `test_delete_all` | Shared namespace state in their suite |
| `test_compression*` / `respond_async` / `transparent_vector_encoding` | Client-only |

---

## Where new work goes

| Area | Files |
| --- | --- |
| Process / config | `cmd/turbopg-server/config.go`, `main.go`, `server.go` |
| HTTP write/query | `cmd/turbopg-server/handlers.go`, `parse_filter.go`, `parse_rank.go`, `api_models.go` |
| List/schema/metadata | `cmd/turbopg-server/handlers_admin.go` |
| Embeddings | `cmd/turbopg-server/embed.go` |
| Engine write | `write.go`, `document_upsert.go`, `document_patch.go`, `document_delete.go` |
| Engine query | `query.go`, `rank.go`, `filter.go`, `bm25.go`, `aggregate.go` |
| Catalog | `namespace.go`, `schema.go`, `store.go` |
| Official tests | `cmd/turbopg-server/official_*.go` |
| CI | `.github/workflows/`, `tests/official-python/`, `docker/postgres/` |
