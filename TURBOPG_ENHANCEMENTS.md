# TurboPG Library Enhancements for Benchmark Compatibility

This document lists identified gaps, required methods, and beneficial enhancements for the core `turbopg` library. These are aimed at improving compatibility with tools like `turbopuffer-tpuf-benchmark` and making the library more robust for API integration.

## 1. `Store.ClearNamespaceData(ctx context.Context, namespaceName string) error`

*   **Reason**: Currently, the API server implements a workaround for the `DELETE /v1/namespaces/{namespace_name}` endpoint (deleting and then recreating the namespace). A dedicated `ClearNamespaceData` method is needed to efficiently remove all vector data from a namespace's table without dropping the table itself or its metadata in `turbopg`'s system tables. This would be more performant and cleaner.

## 2. `Store.GetNamespaceStats(ctx context.Context, namespaceName string) (NamespaceStats, error)`

*   **Reason**: The `HEAD /v1/namespaces/{namespace_name}` API endpoint needs to return statistics about the namespace, such as the approximate number of vectors, vector dimensions, and the distance metric used. The `turbopuffer-tpuf-benchmark` tool expects these in response headers (e.g., `X-turbopuffer-Approx-Num-Vectors`).
*   **Proposed `NamespaceStats` struct**:
    ```go
    type NamespaceStats struct {
        Name             string // Optional, as namespaceName is an input
        ApproximateCount int64
        Dimensions       int
        IndexConfig      *IndexConfig // Or simply DistanceMetric string if full IndexConfig isn't needed
    }
    ```

## 3. Typed Errors

*   **Reason**: The API server needs to translate errors from the `turbopg` library into specific HTTP status codes. Using more specific, typed errors from `turbopg` (e.g., `turbopg.ErrNamespaceNotFound`, `turbopg.ErrDimensionMismatch`, `turbopg.ErrInvalidParameter`) would make this translation more reliable and robust than string matching or general error types. `turbopg.NamespaceError` is a good starting point but could be expanded for more granularity.

## 4. Filter Operations Parity

*   **Reason**: The `turbopuffer-tpuf-benchmark` may use various filter operations (e.g., "Eq", "NotEq", "Lt", "Lte", "Gt", "Gte") with different value types (strings, numbers, booleans). It's important to confirm and ensure that `turbopg.FilterCondition` and `Store.Query` can correctly and robustly handle all filter constructs implied by the benchmark. The current API implementation makes basic assumptions for simple filters.

## 5. Default Distance Metric in `Store.Query`

*   **Reason**: It should be clearly defined how `Store.Query` behaves if the `Metric` field in `QueryOptions` is left empty. Ideally, `turbopg` should default to using the distance metric defined for the namespace. The API server currently relies on this behavior or defaults to a common metric like "cosine_distance" if the library's behavior isn't explicit.

## 6. Dimension Check on Upsert to Existing Namespace

*   **Reason**: For data integrity, `Store.Upsert` should validate that incoming vectors match the dimensions of an existing namespace. If a vector with mismatched dimensions is provided, `turbopg` should return a specific error (e.g., `turbopg.ErrDimensionMismatch`). The API server currently has a placeholder comment for this check.

## 7. `turbopg.Initialize` Idempotency and Checks

*   **Reason**: The `turbopg.Initialize(ctx, db)` function should be fully idempotent. Additionally, it should perform clear checks for the presence and correct functioning of the `pgvector` extension. If `pgvector` is missing or not working, `Initialize` should return a distinct error, allowing the API server to fail fast during startup with a clear diagnostic message.
