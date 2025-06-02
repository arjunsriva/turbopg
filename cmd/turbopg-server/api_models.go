package main

// APIUpsertRequest represents the request body for the upsert operation.
type APIUpsertRequest struct {
	Upserts         []APIDocument `json:"upserts"`
	DistanceMetric  string        `json:"distance_metric,omitempty"`
}

// APIDocument represents a document to be upserted.
type APIDocument struct {
	ID         string                 `json:"id"`
	Vector     []float32              `json:"vector"`
	Attributes map[string]interface{} `json:"attributes"`
}

// APIQueryRequest represents the request body for the query operation.
type APIQueryRequest struct {
	TopK              int           `json:"top_k"`
	Vector            []float32     `json:"vector"`
	Filter            []interface{} `json:"filter"`
	IncludeAttributes []string      `json:"include_attributes,omitempty"`
	RankBy            []interface{} `json:"rank_by,omitempty"`
	Metric            string        `json:"metric,omitempty"`
}

// APIQueryResult represents a single result item from a query.
type APIQueryResult struct {
	ID         string                 `json:"id"`
	Attributes map[string]interface{} `json:"attributes"`
	Score      float64                `json:"score"`
	Vector     []float32              `json:"vector,omitempty"`
}
