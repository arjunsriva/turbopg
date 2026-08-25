package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

var errUpsertRowsNotSequence = errors.New("invalid type: map, expected a sequence")

// WriteRequest is the TurboPuffer v2 write body.
type WriteRequest struct {
	UpsertRows          rowList                  `json:"upsert_rows"`
	UpsertColumns       map[string]interface{}   `json:"upsert_columns"`
	PatchRows           []map[string]interface{} `json:"patch_rows"`
	PatchColumns        map[string]interface{}   `json:"patch_columns"`
	PatchByFilter       *PatchByFilterSpec       `json:"patch_by_filter"`
	Deletes             []interface{}            `json:"deletes"`
	DeleteByFilter      interface{}              `json:"delete_by_filter"`
	DistanceMetric      string                   `json:"distance_metric"`
	Schema              json.RawMessage          `json:"schema"`
	DisableBackpressure bool                     `json:"disable_backpressure"`
	CopyFromNamespace   *CopyNamespaceSpec       `json:"copy_from_namespace"`
	BranchFromNamespace *CopyNamespaceSpec       `json:"branch_from_namespace"`
	UpsertCondition     interface{}              `json:"upsert_condition"`
	PatchCondition      interface{}              `json:"patch_condition"`
	DeleteCondition     interface{}              `json:"delete_condition"`
	ReturnAffectedIDs   bool                     `json:"return_affected_ids"`
	Encryption          EncryptionSpec           `json:"encryption"`
	Sharding            *ShardingSpec            `json:"sharding"`
	Lists               int                      `json:"lists"`
}

type rowList []map[string]interface{}

func (r *rowList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '{' {
		return errUpsertRowsNotSequence
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var rows []map[string]interface{}
	if err := dec.Decode(&rows); err != nil {
		return err
	}
	*r = rows
	return nil
}

type EncryptionSpec struct {
	Mode    string `json:"mode"`
	KeyName string `json:"key_name"`
}

func (e EncryptionSpec) customerManaged() bool {
	return strings.EqualFold(e.Mode, "customer-managed")
}

type ShardingSpec struct {
	NumShards int `json:"num_shards"`
}

func (c *CopyNamespaceSpec) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &c.SourceNamespace)
	}
	var obj struct {
		SourceNamespace string `json:"source_namespace"`
		SourceRegion    string `json:"source_region"`
		SourceAPIKey    string `json:"source_api_key"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	c.SourceNamespace = obj.SourceNamespace
	c.SourceRegion = obj.SourceRegion
	c.SourceAPIKey = obj.SourceAPIKey
	return nil
}

type CopyNamespaceSpec struct {
	SourceNamespace string `json:"source_namespace"`
	SourceRegion    string `json:"source_region"`
	SourceAPIKey    string `json:"source_api_key"`
}

type PatchByFilterSpec struct {
	Filters interface{}            `json:"filters"`
	Patch   map[string]interface{} `json:"patch"`
}

// WriteResponse is the TurboPuffer v2 write response.
type WriteResponse struct {
	Status        string                 `json:"status"`
	Message       string                 `json:"message"`
	RowsAffected  int                    `json:"rows_affected"`
	RowsUpserted  int                    `json:"rows_upserted,omitempty"`
	RowsDeleted   int                    `json:"rows_deleted,omitempty"`
	RowsPatched   int                    `json:"rows_patched,omitempty"`
	UpsertedIDs   []interface{}          `json:"upserted_ids,omitempty"`
	PatchedIDs    []interface{}          `json:"patched_ids,omitempty"`
	DeletedIDs    []interface{}          `json:"deleted_ids,omitempty"`
	RowsRemaining int                    `json:"rows_remaining,omitempty"`
	Billing       map[string]interface{} `json:"billing"`
	Performance   WritePerformance       `json:"performance"`
}

type WritePerformance struct {
	ServerTotalMs int64 `json:"server_total_ms"`
}

// QueryRequest is the TurboPuffer v2 query body.
type QueryRequest struct {
	TopK              int                      `json:"top_k"`
	RankBy            []interface{}            `json:"rank_by"`
	Filters           interface{}              `json:"filters"`
	Filter            interface{}              `json:"filter"` // older alias
	AggregateBy       map[string][]interface{} `json:"aggregate_by"`
	IncludeAttributes json.RawMessage          `json:"include_attributes"`
	ExcludeAttributes []string                 `json:"exclude_attributes"`
	Metric            string                   `json:"metric"`
	Vector            []float32                `json:"vector"`
	Queries           []QueryRequest           `json:"queries"`
	RerankBy          []interface{}            `json:"rerank_by"`
	Limit             json.RawMessage          `json:"limit"`
	GroupBy           []interface{}            `json:"group_by"`
	RankByRaw         json.RawMessage          `json:"-"`
	Consistency       json.RawMessage          `json:"consistency"`
	ComputeAttributes json.RawMessage          `json:"compute_attributes"`
	// LastAsPrefix is accepted. Without a `last` cursor it is a no-op; it is
	// never treated as a glob.
	LastAsPrefix bool `json:"last_as_prefix"`
}

type RecallRequest struct {
	Num                int           `json:"num"`
	TopK               int           `json:"top_k"`
	Filters            interface{}   `json:"filters"`
	RankBy             []interface{} `json:"rank_by"`
	IncludeGroundTruth bool          `json:"include_ground_truth"`
}

type QueryResponse struct {
	Rows              []map[string]interface{} `json:"rows"`
	Aggregations      map[string]interface{}   `json:"aggregations,omitempty"`
	AggregationGroups []map[string]interface{} `json:"aggregation_groups,omitempty"`
	Performance       QueryPerformance         `json:"performance"`
	Billing           QueryBilling             `json:"billing"`
}

type QueryPerformance struct {
	ApproxNamespaceSize   int64   `json:"approx_namespace_size"`
	CacheHitRatio         float64 `json:"cache_hit_ratio"`
	CacheTemperature      string  `json:"cache_temperature"`
	ExhaustiveSearchCount int64   `json:"exhaustive_search_count"`
	QueryExecutionMs      int64   `json:"query_execution_ms"`
	ServerTotalMs         int64   `json:"server_total_ms"`
}

type QueryBilling struct {
	BillableLogicalBytesQueried  int64 `json:"billable_logical_bytes_queried"`
	BillableLogicalBytesReturned int64 `json:"billable_logical_bytes_returned"`
}

type APIError struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

type includeAttributes struct {
	all    bool
	fields []string
	set    bool
}

func (r QueryRequest) resultLimit() int {
	if r.TopK > 0 {
		return r.TopK
	}
	if len(r.Limit) > 0 && string(r.Limit) != "null" {
		var n int
		if err := json.Unmarshal(r.Limit, &n); err == nil && n > 0 {
			return n
		}
		var obj struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(r.Limit, &obj); err == nil && obj.Total > 0 {
			return obj.Total
		}
	}
	return 10
}

func (r QueryRequest) limitPer() int {
	if len(r.Limit) == 0 || string(r.Limit) == "null" {
		return 0
	}
	var obj struct {
		Per int `json:"per"`
	}
	if err := json.Unmarshal(r.Limit, &obj); err != nil {
		return 0
	}
	return obj.Per
}

func parseIncludeAttributes(raw json.RawMessage) includeAttributes {
	if len(raw) == 0 || string(raw) == "null" {
		return includeAttributes{}
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "true" {
		return includeAttributes{all: true, set: true}
	}
	if trimmed == "false" {
		return includeAttributes{set: true}
	}
	var fields []string
	if err := json.Unmarshal(raw, &fields); err != nil {
		return includeAttributes{}
	}
	return includeAttributes{fields: fields, set: true}
}
