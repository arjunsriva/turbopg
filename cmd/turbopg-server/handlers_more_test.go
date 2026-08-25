package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/arjunsriva/turbopg"
)

func TestServerAPICoverage(t *testing.T) {
	_, srv, cleanup := newTestServer(t, "api_")
	defer cleanup()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/namespaces", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth=%d", res.StatusCode)
	}

	ns := "cover-ns"
	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"distance_metric": "euclidean_squared",
		"schema":          map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		"upsert_columns": map[string]interface{}{
			"id":     []interface{}{1, 2, 3},
			"vector": []interface{}{[]float32{1, 0, 0}, []float32{0, 1, 0}, []float32{0, 0, 1}},
			"name":   []interface{}{"a", "b", "c"},
			"price":  []interface{}{10, 20, 30},
		},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"patch_rows": []map[string]interface{}{
			{"id": 1, "name": "aa"},
		},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"patch_columns": map[string]interface{}{
			"id":   []interface{}{1},
			"name": []interface{}{"aa"},
		},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"patch_by_filter": map[string]interface{}{
			"filters": []interface{}{"name", "Eq", "aa"},
			"patch":   map[string]interface{}{"name": "aaa"},
		},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"deletes": []interface{}{3},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, map[string]interface{}{
		"delete_by_filter": []interface{}{"And", []interface{}{
			[]interface{}{"name", "Eq", "b"},
		}},
	}, http.StatusOK)

	var list map[string]interface{}
	doJSONInto(t, srv, http.MethodGet, "/v1/namespaces?prefix=cover", nil, http.StatusOK, &list)
	if list["namespaces"] == nil {
		t.Fatalf("list=%v", list)
	}

	head, err := http.NewRequest(http.MethodHead, srv.URL+"/v2/namespaces/"+ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	head.Header.Set("Authorization", "Bearer testapikey")
	hres, err := http.DefaultClient.Do(head)
	if err != nil {
		t.Fatal(err)
	}
	hres.Body.Close()
	if hres.StatusCode != http.StatusOK {
		t.Fatalf("head=%d", hres.StatusCode)
	}

	doJSON(t, srv, http.MethodGet, "/v1/namespaces/"+ns+"/schema", nil, http.StatusOK)
	doJSON(t, srv, http.MethodPost, "/v1/namespaces/"+ns+"/schema", map[string]interface{}{
		"name": map[string]interface{}{"type": "string", "filterable": true},
	}, http.StatusOK)
	doJSON(t, srv, http.MethodPatch, "/v1/namespaces/"+ns+"/metadata", map[string]interface{}{"pinning": false}, http.StatusOK)
	doJSON(t, srv, http.MethodGet, "/v1/namespaces/"+ns+"/hint_cache_warm", nil, http.StatusOK)
	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/explain_query", map[string]interface{}{
		"top_k": 1, "rank_by": []interface{}{"vector", "ANN", []float32{1, 0, 0}},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query", map[string]interface{}{
		"top_k":              2,
		"rank_by":            []interface{}{"vector", "kNN", []float32{1, 0, 0}},
		"include_attributes": true,
		"filters":            []interface{}{"id", "Gte", 0},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query", map[string]interface{}{
		"aggregate_by": map[string]interface{}{"n": []string{"Count"}, "s": []interface{}{"Sum", "price"}},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query?stainless_overload=multiQuery", map[string]interface{}{
		"queries": []map[string]interface{}{
			{"aggregate_by": map[string]interface{}{"id_count": []string{"Count"}}},
			{"top_k": 1, "rank_by": []interface{}{"vector", "ANN", []float32{1, 0, 0}}},
		},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v1/namespaces/"+ns+"/_debug/recall", map[string]interface{}{
		"num": 1, "top_k": 1, "include_ground_truth": true,
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/cover-copy", map[string]interface{}{
		"copy_from_namespace": map[string]interface{}{"source_namespace": ns},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodPost, "/v2/namespaces/cover-branch", map[string]interface{}{
		"branch_from_namespace": map[string]interface{}{"source_namespace": ns},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodGet, "/v1/namespaces/"+ns+"/_debug/purge_cache", nil, http.StatusOK)
	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query", map[string]interface{}{
		"rank_by": []interface{}{"name", "BM25", "hello"},
	}, http.StatusOK)

	doJSON(t, srv, http.MethodDelete, "/v2/namespaces/cover-copy", nil, http.StatusOK)
	doJSON(t, srv, http.MethodDelete, "/v2/namespaces/cover-branch", nil, http.StatusOK)
	doJSON(t, srv, http.MethodGet, "/v2/namespaces/missing/metadata", nil, http.StatusNotFound)
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("TURBOPG_API_KEY", "secret-key")
	t.Setenv("TURBOPG_PORT", "9999")
	t.Setenv("TURBOPG_LISTEN", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9999" {
		t.Fatalf("listen=%s", cfg.Listen)
	}
	if getEnv("MISSING_TURBOPG_ENV", "x") != "x" {
		t.Fatal("default")
	}
}

func TestProtocolErrors(t *testing.T) {
	if _, err := documentsFromColumns(map[string]interface{}{"vector": []interface{}{}}); err == nil {
		t.Fatal("expected missing id")
	}
	if _, err := documentsFromColumns(map[string]interface{}{"id": "x"}); err == nil {
		t.Fatal("expected array id")
	}
	if _, err := parseRankBy([]interface{}{"vector"}); err == nil {
		t.Fatal("expected short rank_by")
	}
	if _, err := parseRankBy(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := parseRankBy([]interface{}{"vector", "ANN"}); err == nil {
		t.Fatal("expected missing vector")
	}
	if _, err := parseRankBy([]interface{}{"vector", "BM25", "q"}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseFilter("not-array"); err == nil {
		t.Fatal("expected filter array")
	}
	if _, err := parseFilter([]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseFilter([]interface{}{1}); err == nil {
		t.Fatal("expected string filter0")
	}
	if exportID("abc") != "abc" {
		t.Fatal(exportID("abc"))
	}
	if stringifyMust(1) != "1" {
		t.Fatal(stringifyMust(1))
	}
	if stringifyMust(int64(2)) != "2" {
		t.Fatal(stringifyMust(int64(2)))
	}
	if stringifyMust(uint64(3)) != "3" {
		t.Fatal(stringifyMust(uint64(3)))
	}
	if stringifyMust(1.5) != "1.5" {
		t.Fatal(stringifyMust(1.5))
	}
	incFalse := parseIncludeAttributes(json.RawMessage(`false`))
	if !incFalse.set || incFalse.all {
		t.Fatal(incFalse)
	}
	_ = parseIncludeAttributes(json.RawMessage(`null`))
	vec, err := parseVector([]float64{1, 2})
	if err != nil || len(vec) != 2 {
		t.Fatal(vec, err)
	}
	if _, err := parseVector("nope"); err == nil {
		t.Fatal("expected vector error")
	}
	// Official Python client encodes [0.1, 0.2, 0.3] as little-endian f32 base64.
	b64, err := parseVector("zczMPc3MTD6amZk+")
	if err != nil || len(b64) != 3 {
		t.Fatal(b64, err)
	}
	if abs32(b64[0]-0.1) > 1e-6 || abs32(b64[1]-0.2) > 1e-6 || abs32(b64[2]-0.3) > 1e-6 {
		t.Fatalf("decoded %v", b64)
	}
	if _, err := toFloat32("x"); err == nil {
		t.Fatal("expected number error")
	}
	ids := idsFromJSON([]interface{}{"a", json.Number("1"), ""})
	if len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
	row := resultRow(turbopg.QueryResult{
		Document: turbopg.Document{ID: "1", Vector: []float32{1}, Attributes: map[string]interface{}{"name": "z"}},
		Score:    0.1,
	}, includeAttributes{fields: []string{"name", "vector"}, set: true}, []string{"name"})
	if _, ok := row["name"]; ok {
		t.Fatalf("excluded name still present: %v", row)
	}
	b, _ := json.Marshal(WritePerformance{ServerTotalMs: 1})
	if len(b) == 0 {
		t.Fatal("marshal")
	}
}

func stringifyMust(v interface{}) string {
	s, err := stringifyID(v)
	if err != nil {
		return ""
	}
	return s
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
