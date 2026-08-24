package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestServerVectorRoundTrip(t *testing.T) {
	_, srv, cleanup := newTestServer(t, "tpga_")
	defer cleanup()

	ns := "testns-ci"
	upsertBody := map[string]interface{}{
		"distance_metric": "cosine_distance",
		"upsert_rows": []map[string]interface{}{
			{"id": 1, "vector": []float32{1, 0, 0}, "name": "a"},
			{"id": 2, "vector": []float32{0, 1, 0}, "name": "b"},
		},
	}
	doJSON(t, srv, http.MethodPost, "/v2/namespaces/"+ns, upsertBody, http.StatusOK)

	var countResp QueryResponse
	doJSONInto(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query", map[string]interface{}{
		"aggregate_by": map[string]interface{}{
			"id_count": []string{"Count"},
		},
	}, http.StatusOK, &countResp)
	if countResp.Aggregations["id_count"] != float64(2) && countResp.Aggregations["id_count"] != int64(2) {
		// json.Unmarshal into interface{} uses float64
		n, _ := countResp.Aggregations["id_count"].(json.Number)
		if n != "2" {
			got := countResp.Aggregations["id_count"]
			if toInt(got) != 2 {
				t.Fatalf("id_count=%v (%T)", got, got)
			}
		}
	}

	var queryResp QueryResponse
	doJSONInto(t, srv, http.MethodPost, "/v2/namespaces/"+ns+"/query", map[string]interface{}{
		"top_k":   1,
		"rank_by": []interface{}{"vector", "ANN", []float32{1, 0, 0}},
	}, http.StatusOK, &queryResp)
	if len(queryResp.Rows) != 1 {
		t.Fatalf("rows=%v", queryResp.Rows)
	}
	if queryResp.Performance.CacheTemperature != "hot" {
		t.Fatalf("performance=%v", queryResp.Performance)
	}
	if toInt(queryResp.Rows[0]["id"]) != 1 {
		t.Fatalf("nearest=%v", queryResp.Rows[0])
	}

	var meta map[string]interface{}
	doJSONInto(t, srv, http.MethodGet, "/v2/namespaces/"+ns+"/metadata", nil, http.StatusOK, &meta)
	if meta["index"].(map[string]interface{})["status"] != "up-to-date" {
		t.Fatalf("metadata=%v", meta)
	}

	doJSON(t, srv, http.MethodGet, "/v1/namespaces/"+ns+"/_debug/warm_cache", nil, http.StatusOK)
	doJSON(t, srv, http.MethodDelete, "/v2/namespaces/"+ns, nil, http.StatusOK)
	doJSON(t, srv, http.MethodDelete, "/v2/namespaces/"+ns, nil, http.StatusNotFound)
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body interface{}, want int) {
	t.Helper()
	doJSONInto(t, srv, method, path, body, want, nil)
}

func doJSONInto(t *testing.T, srv *httptest.Server, method, path string, body interface{}, want int, dest interface{}) {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, buf)
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
	if res.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d", method, path, res.StatusCode, want)
	}
	if dest != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(dest); err != nil {
			t.Fatal(err)
		}
	}
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		n, err := strconv.Atoi(fmt.Sprint(v))
		if err != nil {
			return -1
		}
		return n
	}
}

func toFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		if err != nil {
			return 0
		}
		return f
	}
}

func TestOperatorEndpoints(t *testing.T) {
	_, srv, cleanup := newTestServer(t, "ops_")
	defer cleanup()

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("healthz=%d", res.StatusCode)
	}
	res, err = http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz=%d", res.StatusCode)
	}
	res, err = http.Get(srv.URL + "/version")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("version=%d", res.StatusCode)
	}
	res, err = http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("metrics=%d", res.StatusCode)
	}
}
