package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
)

func TestOfficialEmbedSemantics(t *testing.T) {
	client, h, _, cleanup := officialSetup(t, "seme_")
	defer cleanup()
	ctx := context.Background()

	t.Run("native_embeddings_unconfigured_400", func(t *testing.T) {
		prev := h.Embedder
		h.Embedder = nil
		defer func() { h.Embedder = prev }()

		ns := officialNS(t, client)
		_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"text": {Type: "string", Embed: turbopuffer.AttributeEmbedConfigParam{Model: "openai/text-embedding-3-small"}},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "text": "A cat sleeping on a windowsill"},
			},
		})
		if err == nil {
			t.Fatal("expected embed without provider to fail")
		}
		var apierr *turbopuffer.Error
		if !errors.As(err, &apierr) || apierr.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %v", err)
		}
		if !strings.Contains(err.Error(), "native embeddings are not configured") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("native_embeddings_write_and_query", func(t *testing.T) {
		prev := h.Embedder
		h.Embedder = lexicalEmbedder{dims: 2}
		defer func() { h.Embedder = prev }()

		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"text": {
					Type: "string",
					Embed: turbopuffer.AttributeEmbedConfigParam{
						Model: "test/lexical",
						Dims:  turbopuffer.Int(2),
					},
				},
			},
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "text": "A cat sleeping on a windowsill", "category": "animal"},
				{"id": 2, "text": "A shiny red sports car", "category": "vehicle"},
			},
		})
		got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnnExpr("text", turbopuffer.NewExprEmbed("playful kitten")),
			IncludeAttributes: turbopuffer.IncludeAttributesParam{
				Bool: turbopuffer.Bool(true),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 1 {
			t.Fatalf("embed query=%v", got.Rows)
		}
		if got.Rows[0]["text"] != "A cat sleeping on a windowsill" {
			t.Fatalf("source text missing: %v", got.Rows[0])
		}
		if _, ok := got.Rows[0]["embed_text"]; !ok {
			t.Fatalf("computed embed_text missing: %v", got.Rows[0])
		}

		got, err = ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnnExpr("vector", turbopuffer.NewExprEmbedWithParams("sports car", turbopuffer.EmbedParams{Model: turbopuffer.String("test/lexical")})),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) != 2 {
			t.Fatalf("query-only embed=%v", got.Rows)
		}

		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			UpsertRows: []turbopuffer.RowParam{
				{"id": 3, "text": "An airplane flying through clouds", "category": "vehicle"},
			},
		})
		got, err = ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnnExpr("vector", turbopuffer.NewExprEmbedWithParams("sports car", turbopuffer.EmbedParams{Model: turbopuffer.String("test/lexical")})),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 1 || toInt(got.Rows[0]["id"]) == 1 {
			t.Fatalf("schema-less write should still embed vehicles: %v", got.Rows)
		}

		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchRows: []turbopuffer.RowParam{
				{"id": 1, "text": "A shiny red sports car"},
			},
		})
		patched, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
			TopK:    turbopuffer.Int(1),
			Filters: turbopuffer.NewFilterEq("id", 1),
			RankBy:  turbopuffer.NewRankByAnnExpr("text", turbopuffer.NewExprEmbed("playful kitten")),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(patched.Rows) != 1 {
			t.Fatalf("patched query=%v", patched.Rows)
		}
		if dist := toFloat(patched.Rows[0]["$dist"]); dist < 0.5 {
			t.Fatalf("expected re-embedded vehicle far from kitten, $dist=%v row=%v", patched.Rows[0]["$dist"], patched.Rows[0])
		}
	})
}
