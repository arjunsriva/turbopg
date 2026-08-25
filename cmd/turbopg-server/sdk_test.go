package main

import (
	"context"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
	"github.com/turbopuffer/turbopuffer-go/v2/option"
)

func TestOfficialGoClientRoundTrip(t *testing.T) {
	_, srv, cleanup := newTestServer(t, "sdk_")
	defer cleanup()

	client := turbopuffer.NewClient(
		option.WithAPIKey("testapikey"),
		option.WithBaseURL(srv.URL),
	)
	ns := client.Namespace("official-client")
	ctx := context.Background()

	write, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
		Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
			"name":  {Type: "string", Filterable: turbopuffer.Bool(true)},
			"price": {Type: "int", Filterable: turbopuffer.Bool(true)},
		},
		UpsertRows: []turbopuffer.RowParam{
			{"id": 1, "vector": []float32{1, 0, 0}, "name": "a", "price": 10},
			{"id": 2, "vector": []float32{0, 1, 0}, "name": "b", "price": 20},
			{"id": 3, "vector": []float32{0, 0, 1}, "name": "c", "price": 30},
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if write.RowsAffected != 3 {
		t.Fatalf("rows_affected=%d", write.RowsAffected)
	}

	count, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		AggregateBy: map[string]turbopuffer.AggregateBy{
			"id_count":  turbopuffer.NewAggregateByCount(),
			"price_sum": turbopuffer.NewAggregateBySum("price"),
		},
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if toInt(count.Aggregations["id_count"]) != 3 {
		t.Fatalf("id_count=%v", count.Aggregations["id_count"])
	}
	if toInt(count.Aggregations["price_sum"]) != 60 {
		t.Fatalf("price_sum=%v", count.Aggregations["price_sum"])
	}

	query, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		TopK:   turbopuffer.Int(1),
		RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0}),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(query.Rows) != 1 {
		t.Fatalf("rows=%v", query.Rows)
	}
	if query.Performance.CacheTemperature == "" {
		t.Fatal("missing performance.cache_temperature")
	}

	knn, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		TopK:    turbopuffer.Int(1),
		RankBy:  turbopuffer.NewRankByKnn("vector", []float32{1, 0, 0}),
		Filters: turbopuffer.NewFilterGte("id", 0),
	})
	if err != nil {
		t.Fatalf("knn: %v", err)
	}
	if len(knn.Rows) != 1 || toInt(knn.Rows[0]["id"]) != 1 {
		t.Fatalf("knn rows=%v", knn.Rows)
	}

	page1, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
		Limit:  turbopuffer.LimitParam{Total: 1},
		IncludeAttributes: turbopuffer.IncludeAttributesParam{
			Bool: turbopuffer.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("export page1: %v", err)
	}
	if len(page1.Rows) != 1 || toInt(page1.Rows[0]["id"]) != 1 {
		t.Fatalf("export page1=%v", page1.Rows)
	}
	if page1.Rows[0]["name"] != "a" {
		t.Fatalf("export attrs=%v", page1.Rows[0])
	}
	page2, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
		Limit:   turbopuffer.LimitParam{Total: 10},
		Filters: turbopuffer.NewFilterGt("id", page1.Rows[0]["id"]),
		IncludeAttributes: turbopuffer.IncludeAttributesParam{
			Bool: turbopuffer.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("export page2: %v", err)
	}
	if len(page2.Rows) != 2 || toInt(page2.Rows[0]["id"]) != 2 {
		t.Fatalf("export page2=%v", page2.Rows)
	}

	meta, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Index.Status != "up-to-date" {
		t.Fatalf("index status=%s", meta.Index.Status)
	}
	if meta.ApproxRowCount != 3 {
		t.Fatalf("approx_row_count=%d", meta.ApproxRowCount)
	}

	if _, err := ns.Schema(ctx, turbopuffer.NamespaceSchemaParams{}); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := ns.UpdateSchema(ctx, turbopuffer.NamespaceUpdateSchemaParams{
		Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
			"name": {Type: "string", Filterable: turbopuffer.Bool(true)},
		},
	}); err != nil {
		t.Fatalf("update schema: %v", err)
	}
	if _, err := ns.UpdateMetadata(ctx, turbopuffer.NamespaceUpdateMetadataParams{}); err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if _, err := ns.HintCacheWarm(ctx, turbopuffer.NamespaceHintCacheWarmParams{}); err != nil {
		t.Fatalf("hint: %v", err)
	}
	if _, err := ns.ExplainQuery(ctx, turbopuffer.NamespaceExplainQueryParams{
		TopK:   turbopuffer.Int(1),
		RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0}),
	}); err != nil {
		t.Fatalf("explain: %v", err)
	}
	recall, err := ns.Recall(ctx, turbopuffer.NamespaceRecallParams{
		Num:  turbopuffer.Int(1),
		TopK: turbopuffer.Int(1),
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if recall.AvgRecall < 0 {
		t.Fatalf("recall=%v", recall)
	}

	listed, err := client.Namespaces(ctx, turbopuffer.NamespacesParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listed == nil {
		t.Fatal("empty list page")
	}

	if _, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		PatchRows: []turbopuffer.RowParam{
			{"id": 1, "name": "aa"},
		},
	}); err != nil {
		t.Fatalf("patch_rows: %v", err)
	}
	if _, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		PatchColumns: turbopuffer.ColumnsParam{
			"id":   []any{2},
			"name": []any{"bb"},
		},
	}); err != nil {
		t.Fatalf("patch_columns: %v", err)
	}
	if _, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		PatchByFilter: turbopuffer.NamespaceWriteParamsPatchByFilter{
			Filters: turbopuffer.NewFilterEq("name", "aa"),
			Patch:   map[string]any{"name": "patched"},
		},
	}); err != nil {
		t.Fatalf("patch_by_filter: %v", err)
	}
	patched, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
		TopK:    turbopuffer.Int(10),
		Filters: turbopuffer.NewFilterEq("name", "patched"),
		IncludeAttributes: turbopuffer.IncludeAttributesParam{
			Bool: turbopuffer.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("patched query: %v", err)
	}
	if len(patched.Rows) != 1 || toInt(patched.Rows[0]["id"]) != 1 {
		t.Fatalf("patched=%v", patched.Rows)
	}

	if _, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		Deletes: []any{3},
	}); err != nil {
		t.Fatalf("deletes: %v", err)
	}
	if _, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		DeleteByFilter: turbopuffer.NewFilterEq("name", "bb"),
	}); err != nil {
		t.Fatalf("delete_by_filter: %v", err)
	}
	remaining, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		AggregateBy: map[string]turbopuffer.AggregateBy{
			"id_count": turbopuffer.NewAggregateByCount(),
		},
	})
	if err != nil {
		t.Fatalf("remaining count: %v", err)
	}
	if toInt(remaining.Aggregations["id_count"]) != 1 {
		t.Fatalf("remaining=%v", remaining.Aggregations["id_count"])
	}

	if _, err := ns.DeleteAll(ctx, turbopuffer.NamespaceDeleteAllParams{}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	fts := client.Namespace("official-fts")
	if _, err := fts.Write(ctx, turbopuffer.NamespaceWriteParams{
		Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
			"content": {
				Type:           "string",
				Filterable:     turbopuffer.Bool(true),
				FullTextSearch: &turbopuffer.FullTextSearchConfigParam{},
			},
		},
		UpsertRows: []turbopuffer.RowParam{
			{"id": 1, "content": "the quick brown fox"},
			{"id": 2, "content": "lazy walrus sleeping"},
		},
	}); err != nil {
		t.Fatalf("fts write: %v", err)
	}
	bm25, err := fts.Query(ctx, turbopuffer.NamespaceQueryParams{
		TopK:   turbopuffer.Int(1),
		RankBy: turbopuffer.NewRankByTextBM25("content", "walrus"),
	})
	if err != nil {
		t.Fatalf("bm25 query: %v", err)
	}
	if len(bm25.Rows) != 1 {
		t.Fatalf("bm25 rows=%v", bm25.Rows)
	}
	tokens, err := fts.Query(ctx, turbopuffer.NamespaceQueryParams{
		TopK:    turbopuffer.Int(1),
		RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
		Filters: turbopuffer.NewFilterContainsAllTokens("content", "lazy walrus"),
	})
	if err != nil {
		t.Fatalf("contains_all_tokens: %v", err)
	}
	if len(tokens.Rows) != 1 || toInt(tokens.Rows[0]["id"]) != 2 {
		t.Fatalf("contains_all_tokens rows=%v", tokens.Rows)
	}

	hybrid := client.Namespace("official-hybrid")
	if _, err := hybrid.Write(ctx, turbopuffer.NamespaceWriteParams{
		DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
		Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
			"content": {
				Type:           "string",
				FullTextSearch: &turbopuffer.FullTextSearchConfigParam{},
			},
		},
		UpsertRows: []turbopuffer.RowParam{
			{"id": 1, "vector": []float32{1, 0, 0}, "content": "alpha walrus"},
			{"id": 2, "vector": []float32{0, 1, 0}, "content": "beta fox"},
		},
	}); err != nil {
		t.Fatalf("hybrid write: %v", err)
	}
	fused, err := hybrid.MultiQuery(ctx, turbopuffer.NamespaceMultiQueryParams{
		Queries: []turbopuffer.NamespaceMultiQueryParamsQuery{
			{TopK: turbopuffer.Int(2), RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0})},
			{TopK: turbopuffer.Int(2), RankBy: turbopuffer.NewRankByTextBM25("content", "walrus")},
		},
		RerankBy: turbopuffer.NewRerankByRrf(),
	})
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if fused == nil || len(fused.Results) == 0 || len(fused.Results[0].Rows) == 0 {
		t.Fatalf("hybrid results=%v", fused)
	}

	copyNS := client.Namespace("official-copy")
	copied, err := copyNS.CopyFrom(ctx, turbopuffer.NamespaceCopyFromParams{
		SourceNamespace: "official-hybrid",
	})
	if err != nil {
		t.Fatalf("copy_from: %v", err)
	}
	if copied.RowsAffected != 2 {
		t.Fatalf("copy rows_affected=%d", copied.RowsAffected)
	}
	branchNS := client.Namespace("official-branch")
	branched, err := branchNS.BranchFrom(ctx, turbopuffer.NamespaceBranchFromParams{
		SourceNamespace: "official-hybrid",
	})
	if err != nil {
		t.Fatalf("branch_from: %v", err)
	}
	if branched.RowsAffected != 2 {
		t.Fatalf("branch rows_affected=%d", branched.RowsAffected)
	}

	for _, name := range []string{"official-fts", "official-hybrid", "official-copy", "official-branch"} {
		drop := client.Namespace(name)
		if _, err := drop.DeleteAll(ctx, turbopuffer.NamespaceDeleteAllParams{}); err != nil {
			t.Fatalf("delete %s: %v", name, err)
		}
	}
}
