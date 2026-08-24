package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
	"github.com/turbopuffer/turbopuffer-go/v2/option"
)

var officialNSSeq int

func officialSetup(t *testing.T, prefix string) (turbopuffer.Client, *Server, *httptest.Server, func()) {
	t.Helper()
	h, srv, cleanup := newTestServer(t, prefix)
	client := turbopuffer.NewClient(
		option.WithAPIKey("testapikey"),
		option.WithBaseURL(srv.URL),
	)
	return client, h, srv, cleanup
}

type lexicalEmbedder struct{ dims int }

func (l lexicalEmbedder) Embed(_ context.Context, _ string, texts []string, dims int) ([][]float32, error) {
	if dims <= 0 {
		dims = l.dims
	}
	if dims <= 0 {
		dims = 2
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, dims)
		lower := strings.ToLower(text)
		if strings.Contains(lower, "cat") || strings.Contains(lower, "kitten") || strings.Contains(lower, "animal") {
			vec[0] = 1
		} else if dims > 1 {
			vec[1] = 1
		}
		out[i] = vec
	}
	return out, nil
}

func officialNS(t *testing.T, client turbopuffer.Client) turbopuffer.Namespace {
	t.Helper()
	officialNSSeq++
	name := strings.ToLower(t.Name())
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
	name = fmt.Sprintf("n-%s-%d", name, officialNSSeq)
	if len(name) > 120 {
		name = name[:120]
	}
	return client.Namespace(name)
}

func mustWrite(t *testing.T, ns turbopuffer.Namespace, ctx context.Context, params turbopuffer.NamespaceWriteParams) {
	t.Helper()
	if _, err := ns.Write(ctx, params); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func mustExport(t *testing.T, ns turbopuffer.Namespace, ctx context.Context) []turbopuffer.Row {
	t.Helper()
	got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy: turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
		TopK:   turbopuffer.Int(100),
		IncludeAttributes: turbopuffer.IncludeAttributesParam{
			Bool: turbopuffer.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return got.Rows
}

func mustGet(t *testing.T, ns turbopuffer.Namespace, ctx context.Context, id int) turbopuffer.Row {
	t.Helper()
	for _, row := range mustExport(t, ns, ctx) {
		if toInt(row["id"]) == id {
			return row
		}
	}
	t.Fatalf("missing id %d", id)
	return nil
}

func mustCount(t *testing.T, ns turbopuffer.Namespace, ctx context.Context) int {
	t.Helper()
	got, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		AggregateBy: map[string]turbopuffer.AggregateBy{
			"n": turbopuffer.NewAggregateByCount(),
		},
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return toInt(got.Aggregations["n"])
}

func seedVectors(t *testing.T, ns turbopuffer.Namespace, ctx context.Context) {
	t.Helper()
	mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
		DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
		UpsertRows: []turbopuffer.RowParam{
			{"id": 1, "vector": []float32{1, 0, 0}, "name": "a"},
			{"id": 2, "vector": []float32{0, 1, 0}, "name": "b"},
			{"id": 3, "vector": []float32{0, 0, 1}, "name": "c"},
		},
	})
}
