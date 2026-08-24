package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
)

func TestOfficialQuerySemantics(t *testing.T) {
	client, _, srv, cleanup := officialSetup(t, "semq_")
	defer cleanup()
	ctx := context.Background()

	t.Run("ann_returns_nearest", func(t *testing.T) {
		ns := officialNS(t, client)
		seedVectors(t, ns, ctx)
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("rows=%v", got.Rows)
		}
	})

	t.Run("knn_requires_filters", func(t *testing.T) {
		ns := officialNS(t, client)
		seedVectors(t, ns, ctx)
		_, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByKnn("vector", []float32{1, 0, 0}),
		})
		if err == nil {
			t.Fatal("expected kNN without filters to fail")
		}
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(1),
			RankBy:  turbopuffer.NewRankByKnn("vector", []float32{1, 0, 0}),
			Filters: turbopuffer.NewFilterGte("id", 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("rows=%v", got.Rows)
		}
	})

	t.Run("bm25_and_sum_bm25", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"title": {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
				"body":  {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "title": "walrus", "body": "arctic mammal"},
				{"id": 2, "title": "fox", "body": "quick brown"},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByTextBM25("title", "walrus"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("bm25=%v", got.Rows)
		}
		summed, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK: turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByTextSum([]turbopuffer.RankByText{
				turbopuffer.NewRankByTextBM25("title", "walrus"),
				turbopuffer.NewRankByTextBM25("body", "mammal"),
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(summed.Rows) != 1 || toInt(summed.Rows[0]["id"]) != 1 {
			t.Fatalf("sum bm25=%v", summed.Rows)
		}
	})

	t.Run("count_sum_and_group_by", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "cat": "a", "price": 10},
				{"id": 2, "vector": []float32{0, 1, 0}, "cat": "a", "price": 5},
				{"id": 3, "vector": []float32{0, 0, 1}, "cat": "b", "price": 7},
			},
		})
		agg, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			AggregateBy: map[string]turbopuffer.AggregateBy{
				"n":     turbopuffer.NewAggregateByCount(),
				"price": turbopuffer.NewAggregateBySum("price"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if toInt(agg.Aggregations["n"]) != 3 || toInt(agg.Aggregations["price"]) != 22 {
			t.Fatalf("aggregations=%v", agg.Aggregations)
		}
		grouped, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			AggregateBy: map[string]turbopuffer.AggregateBy{
				"n": turbopuffer.NewAggregateByCount(),
			},
			GroupBy: []turbopuffer.GroupBy{turbopuffer.NewGroupByAttr("cat")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(grouped.AggregationGroups) != 2 {
			t.Fatalf("groups=%v", grouped.AggregationGroups)
		}
	})

	t.Run("export_pages_by_id", func(t *testing.T) {
		ns := officialNS(t, client)
		seedVectors(t, ns, ctx)
		page1, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Limit:  turbopuffer.LimitParam{Total: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page1.Rows) != 1 || toInt(page1.Rows[0]["id"]) != 1 {
			t.Fatalf("page1=%v", page1.Rows)
		}
		page2, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Limit:   turbopuffer.LimitParam{Total: 10},
			Filters: turbopuffer.NewFilterGt("id", page1.Rows[0]["id"]),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page2.Rows) != 2 || toInt(page2.Rows[0]["id"]) != 2 {
			t.Fatalf("page2=%v", page2.Rows)
		}
	})

	t.Run("include_attributes_default_is_id_only", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "hidden"},
			},
		})
		def, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:   turbopuffer.Int(1),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := def.Rows[0]["name"]; ok {
			t.Fatalf("default include leaked attrs: %v", def.Rows[0])
		}
		all, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:   turbopuffer.Int(1),
			IncludeAttributes: turbopuffer.IncludeAttributesParam{
				Bool: turbopuffer.Bool(true),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if all.Rows[0]["name"] != "hidden" {
			t.Fatalf("include true=%v", all.Rows[0])
		}
	})

	t.Run("filters_eq_null_in_glob_regex_contains_logic", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "apple", "tag": "x", "tags": []string{"red"}},
				{"id": 2, "vector": []float32{0, 1, 0}, "name": "banana", "tags": []string{"yellow"}},
			},
		})
		eqNull, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:    turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterEq("tag", nil),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(eqNull.Rows) != 1 || toInt(eqNull.Rows[0]["id"]) != 2 {
			t.Fatalf("eq null=%v", eqNull.Rows)
		}
		glob, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:    turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterGlob("name", "a*"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(glob.Rows) != 1 || toInt(glob.Rows[0]["id"]) != 1 {
			t.Fatalf("glob=%v", glob.Rows)
		}
		re, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:    turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterRegex("name", "^ban"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(re.Rows) != 1 || toInt(re.Rows[0]["id"]) != 2 {
			t.Fatalf("regex=%v", re.Rows)
		}
		contains, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:    turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterContains("tags", "red"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(contains.Rows) != 1 || toInt(contains.Rows[0]["id"]) != 1 {
			t.Fatalf("contains=%v", contains.Rows)
		}
		logic, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:   turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterAnd([]turbopuffer.Filter{
				turbopuffer.NewFilterIn("name", []string{"apple", "banana"}),
				turbopuffer.NewFilterNot(turbopuffer.NewFilterEq("name", "banana")),
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(logic.Rows) != 1 || toInt(logic.Rows[0]["id"]) != 1 {
			t.Fatalf("and/or/not=%v", logic.Rows)
		}
		orFilter, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			TopK:   turbopuffer.Int(10),
			Filters: turbopuffer.NewFilterOr([]turbopuffer.Filter{
				turbopuffer.NewFilterEq("name", "apple"),
				turbopuffer.NewFilterEq("name", "banana"),
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(orFilter.Rows) != 2 {
			t.Fatalf("or=%v", orFilter.Rows)
		}
	})

	t.Run("contains_all_tokens_on_fts", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"content": {
					Type:           "string",
					FullTextSearch: &turbopuffer.FullTextSearchConfigParam{},
				},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "content": "lazy walrus sleeping"},
				{"id": 2, "content": "quick brown fox"},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(10),
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Filters: turbopuffer.NewFilterContainsAllTokens("content", "lazy walrus"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("tokens=%v", got.Rows)
		}
	})

	t.Run("fts_default_not_filterable", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"content": {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "content": "walrus"},
			},
		})
		_, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(1),
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Filters: turbopuffer.NewFilterEq("content", "walrus"),
		})
		if err == nil {
			t.Fatal("expected filter on unfilterable FTS attribute to fail")
		}
	})

	t.Run("hybrid_rrf", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"content": {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "content": "alpha walrus"},
				{"id": 2, "vector": []float32{0, 1, 0}, "content": "beta fox"},
			},
		})
		fused, err := ns.MultiQuery(ctx, turbopuffer.NamespaceMultiQueryParams{
			Queries: []turbopuffer.NamespaceMultiQueryParamsQuery{
				{TopK: turbopuffer.Int(2), RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0})},
				{TopK: turbopuffer.Int(2), RankBy: turbopuffer.NewRankByTextBM25("content", "walrus")},
			},
			RerankBy: turbopuffer.NewRerankByRrf(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if fused == nil || len(fused.Results) == 0 || len(fused.Results[0].Rows) == 0 {
			t.Fatalf("hybrid=%v", fused)
		}
		if toInt(fused.Results[0].Rows[0]["id"]) != 1 {
			t.Fatalf("expected RRF to rank walrus/near vector first, rows=%v", fused.Results[0].Rows)
		}
	})

	t.Run("euclidean_squared_dist", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricEuclideanSquared,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 7, "vector": []float32{0.7, 0.7}},
				{"id": 10, "vector": []float32{1.0, 1.0}},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnn("vector", []float32{0.8, 0.7}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 7 {
			t.Fatalf("rows=%v", got.Rows)
		}
		dist := toFloat(got.Rows[0]["$dist"])
		if math.Abs(dist-0.01) > 1e-5 {
			t.Fatalf("$dist=%v (%T) want 0.01 row=%v", got.Rows[0]["$dist"], got.Rows[0]["$dist"], got.Rows[0])
		}
	})

	t.Run("product_weight_either_side", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricEuclideanSquared,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"title":   {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
				"content": {Type: "string", FullTextSearch: &turbopuffer.FullTextSearchConfigParam{}},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{0.1, 0.1}, "title": "the quick brown fox", "content": "jumped over the lazy dog"},
				{"id": 2, "vector": []float32{0.2, 0.2}, "title": "the lazy dog", "content": "is brown"},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(10),
			RankBy: turbopuffer.NewRankByTextProduct(0.5, turbopuffer.NewRankByTextBM25("title", "quick brown")),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) == 0 {
			t.Fatal("expected Product hits")
		}
		body, _ := json.Marshal(map[string]interface{}{
			"top_k":   10,
			"rank_by": []interface{}{"Product", []interface{}{"title", "BM25", "quick brown"}, 0.5},
		})
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v2/namespaces/"+ns.ID()+"/query", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer testapikey")
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("reversed Product status=%d", res.StatusCode)
		}
	})

	t.Run("sparse_knn", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"sparse_vector": {
					Type: "{}f16",
					SparseKnn: turbopuffer.AttributeSchemaConfigSparseKnnParam{
						DistanceMetric: turbopuffer.SparseDistanceMetricDotProduct,
					},
				},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0}, "sparse_vector": map[string]float32{"dim0": 1, "dim1": 0}},
				{"id": 2, "vector": []float32{0, 1}, "sparse_vector": map[string]float32{"dim0": 0, "dim3": 1}},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(2),
			RankBy: turbopuffer.NewRankBySparseKnn("sparse_vector", map[string]float64{"dim0": 1}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) == 0 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("sparse rows=%v", got.Rows)
		}
	})

	t.Run("late_interaction", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"tokens": {
					Type: "[][2]f32",
					Ann: turbopuffer.AttributeSchemaConfigAnnParam{
						LateInteraction: turbopuffer.Bool(true),
					},
				},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0}, "tokens": [][]float32{{1, 0}, {0, 1}}},
				{"id": 2, "vector": []float32{0, 1}, "tokens": [][]float32{{0.1, 0.9}}},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(2),
			RankBy: turbopuffer.NewRankByAnnMulti("tokens", [][]float32{{1, 0}, {0, 1}}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) == 0 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("late rows=%v", got.Rows)
		}
	})

	t.Run("in_and_contains_on_arrays", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricEuclideanSquared,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 7, "vector": []float32{0.7, 0.7}, "count": 1, "users": []string{"jan", "simon"}},
				{"id": 9, "vector": []float32{0.7, 0.7}, "count": 2, "users": []string{"bojan", "morgan", "simon"}},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(5),
			RankBy: turbopuffer.NewRankByAnn("vector", []float32{0, 0}),
			Filters: turbopuffer.NewFilterAnd([]turbopuffer.Filter{
				turbopuffer.NewFilterGt("count", 1),
				turbopuffer.NewFilterIn("users", []string{"simon"}),
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 9 {
			t.Fatalf("In on array=%v", got.Rows)
		}
		got, err = ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(5),
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Filters: turbopuffer.NewFilterContains("users", "simon"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 2 {
			t.Fatalf("Contains=%v", got.Rows)
		}
		miss, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(5),
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Filters: turbopuffer.NewFilterContains("users", "imo"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(miss.Rows) != 0 {
			t.Fatalf("Contains must not substring-match, rows=%v", miss.Rows)
		}
	})

	t.Run("lt_matches_null", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "price": 10},
				{"id": 2, "vector": []float32{0, 1, 0}},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(10),
			RankBy:  turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			Filters: turbopuffer.NewFilterLt("price", 50),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 2 {
			t.Fatalf("Lt should match null attributes, rows=%v", got.Rows)
		}
	})

	t.Run("inferred_schema", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricEuclideanSquared,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 2, "vector": []float32{2, 2}},
				{"id": 7, "vector": []float32{0.7, 0.7}, "hello": "world", "test": "rows"},
			},
		})
		schema, err := ns.Schema(ctx, turbopuffer.NamespaceSchemaParams{})
		if err != nil {
			t.Fatal(err)
		}
		if schema == nil {
			t.Fatal("schema nil")
		}
		hello, ok := (*schema)["hello"]
		if !ok || hello.Type != "string" || !hello.Filterable {
			t.Fatalf("hello schema=%v", schema)
		}
		if hello.JSON.FullTextSearch.Valid() {
			t.Fatalf("hello should not enable FTS: %#v", hello)
		}
	})
}
