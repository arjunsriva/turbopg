package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arjunsriva/turbopg"
)

func TestParseEmbedSpecs(t *testing.T) {
	specs := parseEmbedSpecs(map[string]interface{}{
		"id":     map[string]interface{}{"type": "string"},
		"vector": map[string]interface{}{"type": "[2]f32"},
		"text": map[string]interface{}{
			"type":  "string",
			"embed": "voyage/voyage-4",
		},
		"title": map[string]interface{}{
			"type": "string",
			"embed": map[string]interface{}{
				"model":     "openai/text-embedding-3-small",
				"dims":      json.Number("512"),
				"attribute": "vector",
			},
		},
		"body": map[string]interface{}{
			"type":  "string",
			"embed": nil,
		},
	})
	if len(specs) != 2 {
		t.Fatalf("specs=%v", specs)
	}
	byField := map[string]turbopg.EmbedSpec{}
	for _, s := range specs {
		byField[s.Field] = s
	}
	if byField["text"].Model != "voyage/voyage-4" || byField["text"].ComputedAttr() != "embed_text" {
		t.Fatalf("text=%v attr=%s", byField["text"], byField["text"].ComputedAttr())
	}
	if byField["title"].Model != "openai/text-embedding-3-small" || byField["title"].Dims != 512 || byField["title"].ComputedAttr() != "" {
		t.Fatalf("title=%v attr=%s", byField["title"], byField["title"].ComputedAttr())
	}
}

func TestApplyEmbeddingsRejectsExampleRandom(t *testing.T) {
	h := &Server{Embedder: lexicalEmbedder{dims: 2}}
	docs := []turbopg.Document{{ID: "1", Attributes: map[string]interface{}{"text": "hi"}}}
	err := h.applyEmbeddings(context.Background(), docs, []turbopg.EmbedSpec{{Field: "text", Model: "example/random", Dims: 2}})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyEmbeddingsRequiresProvider(t *testing.T) {
	h := &Server{}
	docs := []turbopg.Document{{ID: "1", Attributes: map[string]interface{}{"text": "hi"}}}
	err := h.applyEmbeddings(context.Background(), docs, []turbopg.EmbedSpec{{Field: "text", Model: "openai/text-embedding-3-small"}})
	if !strings.Contains(fmtErr(err), "native embeddings are not configured") {
		t.Fatalf("err=%v", err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth=%s", got)
		}
		var body openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" || len(body.Input) != 2 || body.Dimensions != 2 {
			t.Errorf("body=%+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"index": 1, "embedding": []float64{0.3, 0.4}},
				{"index": 0, "embedding": []float64{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	e := newOpenAIEmbedder(srv.URL+"/v1", "test-key")
	vecs, err := e.Embed(context.Background(), "test-model", []string{"hello", "world"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][0] != 0.3 {
		t.Fatalf("vecs=%v", vecs)
	}
}

func TestEmbedderFromConfig(t *testing.T) {
	if embedderFromConfig("", "") != nil {
		t.Fatal("expected nil embedder")
	}
	if embedderFromConfig("", "sk-test") == nil {
		t.Fatal("expected default OpenAI embedder")
	}
}
