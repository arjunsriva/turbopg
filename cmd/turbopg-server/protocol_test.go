package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/arjunsriva/turbopg"
)

func TestDocumentsFromRows(t *testing.T) {
	docs, err := documentsFromRows([]map[string]interface{}{
		{"id": json.Number("1"), "vector": []interface{}{1.0, 2.0, 3.0}, "name": "foo"},
		{"id": "doc-2", "vector": []interface{}{4.0, 5.0, 6.0}, "category": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("len=%d", len(docs))
	}
	if docs[0].ID != "1" {
		t.Fatalf("id=%s", docs[0].ID)
	}
	if docs[0].Attributes["name"] != "foo" {
		t.Fatalf("attrs=%v", docs[0].Attributes)
	}
	if got := exportID(docs[0].ID); got != int64(1) {
		t.Fatalf("exportID=%v (%T)", got, got)
	}
}

func TestDocumentsFromColumns(t *testing.T) {
	docs, err := documentsFromColumns(map[string]interface{}{
		"id":     []interface{}{json.Number("1"), json.Number("2")},
		"vector": []interface{}{[]interface{}{1.0, 0.0}, []interface{}{0.0, 1.0}},
		"name":   []interface{}{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[1].Attributes["name"] != "b" {
		t.Fatalf("docs=%v", docs)
	}
}

func TestParseRankBy(t *testing.T) {
	spec, err := parseRankBy([]interface{}{"vector", "ANN", []interface{}{0.1, 0.2}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != turbopg.RankVector || len(spec.Vector) != 2 || spec.Exact {
		t.Fatalf("kind=%s vec=%v exact=%v", spec.Kind, spec.Vector, spec.Exact)
	}

	knn, err := parseRankBy([]interface{}{"vector", "kNN", []interface{}{0.1, 0.2}})
	if err != nil {
		t.Fatal(err)
	}
	if !knn.Exact || knn.Kind != turbopg.RankVector {
		t.Fatalf("knn=%v", knn)
	}

	bm25, err := parseRankBy([]interface{}{"content", "BM25", "walrus"})
	if err != nil {
		t.Fatal(err)
	}
	if bm25.Kind != turbopg.RankBM25 || bm25.Query != "walrus" {
		t.Fatalf("bm25=%v", bm25)
	}

	attr, err := parseRankBy([]interface{}{"id", "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if attr.Kind != turbopg.RankAttribute || attr.Desc {
		t.Fatalf("attr=%v", attr)
	}

	weightFirst, err := parseRankBy([]interface{}{"Product", 0.5, []interface{}{"title", "BM25", "quick brown"}})
	if err != nil {
		t.Fatal(err)
	}
	if weightFirst.Kind != turbopg.RankProduct || weightFirst.Weight != 0.5 || len(weightFirst.Children) != 1 {
		t.Fatalf("product weight-first=%v", weightFirst)
	}

	subFirst, err := parseRankBy([]interface{}{"Product", []interface{}{"title", "BM25", "quick brown"}, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if subFirst.Kind != turbopg.RankProduct || subFirst.Weight != 0.5 || subFirst.Children[0].Query != "quick brown" {
		t.Fatalf("product subquery-first=%v", subFirst)
	}

	sparse, err := parseRankBy([]interface{}{"sparse_vector", "SparseKNN", map[string]interface{}{"dim0": 0.2, "dim3": 0.1}})
	if err != nil {
		t.Fatal(err)
	}
	if sparse.Kind != turbopg.RankSparse || sparse.Field != "sparse_vector" || sparse.Sparse["dim0"] != 0.2 {
		t.Fatalf("sparse=%v", sparse)
	}

	late, err := parseRankBy([]interface{}{"tokens", "ANN", []interface{}{[]interface{}{0.4, 0.3}, []interface{}{1.0, 0.0}}})
	if err != nil {
		t.Fatal(err)
	}
	if late.Kind != turbopg.RankLate || late.Field != "tokens" || len(late.MultiVector) != 2 || late.Exact {
		t.Fatalf("late=%v", late)
	}

	embed, err := parseRankBy([]interface{}{"text", "ANN", []interface{}{"Embed", "foxes that jump"}})
	if err != nil {
		t.Fatal(err)
	}
	if embed.Kind != turbopg.RankVector || embed.Field != "text" || embed.EmbedQuery != "foxes that jump" || embed.EmbedModel != "" {
		t.Fatalf("embed=%v", embed)
	}

	embedModel, err := parseRankBy([]interface{}{"vector", "ANN", []interface{}{"Embed", "foxes that jump", map[string]interface{}{"model": "voyage/voyage-4"}}})
	if err != nil {
		t.Fatal(err)
	}
	if embedModel.EmbedQuery != "foxes that jump" || embedModel.EmbedModel != "voyage/voyage-4" {
		t.Fatalf("embed model=%v", embedModel)
	}
}

func TestWriteRequestDropInFields(t *testing.T) {
	var req WriteRequest
	if err := json.Unmarshal([]byte(`{
		"upsert_rows": [{"id": 1, "vector": [0.1, 0.1]}],
		"distance_metric": "cosine_distance",
		"sharding": {"num_shards": 8},
		"encryption": {"mode": "default"}
	}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Sharding == nil || req.Sharding.NumShards != 8 {
		t.Fatalf("sharding=%v", req.Sharding)
	}
	if req.Encryption.Mode != "default" {
		t.Fatalf("encryption=%v", req.Encryption)
	}

	var cmek WriteRequest
	if err := json.Unmarshal([]byte(`{"encryption": {"key_name": "mykey", "mode": "customer-managed"}}`), &cmek); err != nil {
		t.Fatal(err)
	}
	if !cmek.Encryption.customerManaged() {
		t.Fatal("expected customer-managed encryption")
	}

	var copyReq WriteRequest
	if err := json.Unmarshal([]byte(`{
		"copy_from_namespace": {
			"source_namespace": "src",
			"source_region": "gcp-us-central1",
			"source_api_key": "other"
		}
	}`), &copyReq); err != nil {
		t.Fatal(err)
	}
	if copyReq.CopyFromNamespace.SourceNamespace != "src" || copyReq.CopyFromNamespace.SourceRegion != "gcp-us-central1" {
		t.Fatalf("copy=%v", copyReq.CopyFromNamespace)
	}

	var bad WriteRequest
	err := json.Unmarshal([]byte(`{"upsert_rows": {"id": 2, "vector": [2, 2]}}`), &bad)
	if err == nil || err.Error() != errUpsertRowsNotSequence.Error() {
		t.Fatalf("expected sequence error, got %v", err)
	}
}

func TestParseFilter(t *testing.T) {
	f, err := parseFilter([]interface{}{"id", "Gte", json.Number("0")})
	if err != nil {
		t.Fatal(err)
	}
	cond, ok := f.(turbopg.FilterCondition)
	if !ok || cond.Field != "id" || cond.Op != turbopg.FilterOpGte {
		t.Fatalf("cond=%v", f)
	}

	andFilter, err := parseFilter([]interface{}{
		"And",
		[]interface{}{
			[]interface{}{"color", "Eq", "blue"},
			[]interface{}{"id", "Gte", json.Number("1")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	logical, ok := andFilter.(turbopg.LogicalFilter)
	if !ok || logical.Op != turbopg.LogicalOpAnd || len(logical.Filters) != 2 {
		t.Fatalf("logical=%v", andFilter)
	}
}

func TestParseIncludeAttributes(t *testing.T) {
	all := parseIncludeAttributes(json.RawMessage(`true`))
	if !all.all {
		t.Fatal("expected all")
	}
	fields := parseIncludeAttributes(json.RawMessage(`["name","vector"]`))
	if !reflect.DeepEqual(fields.fields, []string{"name", "vector"}) {
		t.Fatalf("fields=%v", fields.fields)
	}
}
