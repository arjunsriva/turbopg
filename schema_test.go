package turbopg

import (
	"encoding/json"
	"testing"
)

func TestEmbedSpecs(t *testing.T) {
	specs := EmbedSpecs(map[string]interface{}{
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
	byField := map[string]EmbedSpec{}
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

func TestAttributeDefFilterable(t *testing.T) {
	fts := map[string]interface{}{"type": "string", "full_text_search": true}
	d := ParseAttributeDef(fts)
	if d.IsFilterable(fts) {
		t.Fatal("fts field should not be filterable by default")
	}
	explicit := map[string]interface{}{"type": "string", "filterable": true, "full_text_search": true}
	if !ParseAttributeDef(explicit).IsFilterable(explicit) {
		t.Fatal("filterable: true should win")
	}
	if ParseAttributeDef(true).IsFilterable(true) {
		t.Fatal("bool shorthand is not a filterable object")
	}
}
