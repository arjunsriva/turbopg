package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
)

func TestOfficialAdminSemantics(t *testing.T) {
	client, _, _, cleanup := officialSetup(t, "sema_")
	defer cleanup()
	ctx := context.Background()

	t.Run("schema_metadata_explain_recall_hint", func(t *testing.T) {
		ns := officialNS(t, client)
		seedVectors(t, ns, ctx)
		if _, err := ns.Schema(ctx, turbopuffer.NamespaceSchemaParams{}); err != nil {
			t.Fatal(err)
		}
		if _, err := ns.UpdateSchema(ctx, turbopuffer.NamespaceUpdateSchemaParams{
			Schema: map[string]turbopuffer.AttributeSchemaConfigParam{
				"name": {Type: "string", Filterable: turbopuffer.Bool(true)},
			},
		}); err != nil {
			t.Fatal(err)
		}
		meta, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
		if err != nil {
			t.Fatal(err)
		}
		if meta.ApproxRowCount != 3 || meta.Index.Status != "up-to-date" {
			t.Fatalf("metadata=%v", meta)
		}
		if _, err := ns.UpdateMetadata(ctx, turbopuffer.NamespaceUpdateMetadataParams{}); err != nil {
			t.Fatal(err)
		}
		if _, err := ns.HintCacheWarm(ctx, turbopuffer.NamespaceHintCacheWarmParams{}); err != nil {
			t.Fatal(err)
		}
		if _, err := ns.ExplainQuery(ctx, turbopuffer.NamespaceExplainQueryParams{
			TopK:   turbopuffer.Int(1),
			RankBy: turbopuffer.NewRankByAnn("vector", []float32{1, 0, 0}),
		}); err != nil {
			t.Fatal(err)
		}
		recall, err := ns.Recall(ctx, turbopuffer.NamespaceRecallParams{
			Num:                turbopuffer.Int(1),
			TopK:               turbopuffer.Int(1),
			IncludeGroundTruth: turbopuffer.Bool(true),
		})
		if err != nil {
			t.Fatal(err)
		}
		if recall.AvgRecall < 0 || recall.AvgRecall > 1 {
			t.Fatalf("avg_recall=%v", recall.AvgRecall)
		}
		if len(recall.GroundTruth) == 0 || len(recall.GroundTruth[0].NearestNeighbors) == 0 {
			t.Fatalf("ground_truth=%v", recall.GroundTruth)
		}
	})

	t.Run("list_pagination_and_delete_reuse", func(t *testing.T) {
		prefix := "sem-list-" + strings.ReplaceAll(t.Name(), "/", "-")
		var names []string
		for i := 0; i < 3; i++ {
			name := prefix + string(rune('a'+i))
			names = append(names, name)
			ns := client.Namespace(name)
			mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
				DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
				UpsertRows: []turbopuffer.RowParam{
					{"id": 1, "vector": []float32{1, 0, 0}},
				},
			})
		}
		page1, err := client.Namespaces(ctx, turbopuffer.NamespacesParams{
			Prefix:   turbopuffer.String(prefix),
			PageSize: turbopuffer.Int(1),
		})
		if err != nil {
			t.Fatal(err)
		}
		if page1 == nil || len(page1.Namespaces) != 1 || page1.NextCursor == "" {
			t.Fatalf("page1=%v", page1)
		}
		page2, err := client.Namespaces(ctx, turbopuffer.NamespacesParams{
			Prefix:   turbopuffer.String(prefix),
			PageSize: turbopuffer.Int(1),
			Cursor:   turbopuffer.String(page1.NextCursor),
		})
		if err != nil {
			t.Fatal(err)
		}
		if page2 == nil || len(page2.Namespaces) != 1 || page2.Namespaces[0].ID == page1.Namespaces[0].ID {
			t.Fatalf("page2=%v page1=%v", page2, page1)
		}

		drop := client.Namespace(names[0])
		if _, err := drop.DeleteAll(ctx, turbopuffer.NamespaceDeleteAllParams{}); err != nil {
			t.Fatal(err)
		}
		if _, err := drop.DeleteAll(ctx, turbopuffer.NamespaceDeleteAllParams{}); err == nil {
			t.Fatal("expected 404 after delete")
		}
		mustWrite(t, drop, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}},
			},
		})
	})

	t.Run("cmek_rejected", func(t *testing.T) {
		ns := officialNS(t, client)
		_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows:     []turbopuffer.RowParam{{"id": 1, "vector": []float32{0.1, 0.1}}},
			Encryption:     turbopuffer.EncryptionParamCustomerManaged("mykey"),
		})
		if err == nil {
			t.Fatal("expected CMEK write to fail")
		}
		var apierr *turbopuffer.Error
		if !errors.As(err, &apierr) || apierr.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %v", err)
		}
		if !strings.Contains(err.Error(), "Malformed Cloud KMS crypto key: mykey") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("sharding_ignored", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			Sharding:       turbopuffer.ShardingConfigParam{NumShards: 8},
			UpsertRows:     []turbopuffer.RowParam{{"id": 1, "vector": []float32{1, 0, 0}}},
		})
		if mustCount(t, ns, ctx) != 1 {
			t.Fatal("expected sharded write to land in one table")
		}
	})

	t.Run("copy_source_region_local", func(t *testing.T) {
		src := officialNS(t, client)
		mustWrite(t, src, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows:     []turbopuffer.RowParam{{"id": 1, "vector": []float32{1, 0, 0}}},
		})
		dest := officialNS(t, client)
		copied, err := dest.CopyFrom(ctx, turbopuffer.NamespaceCopyFromParams{
			SourceNamespace: src.ID(),
			SourceRegion:    turbopuffer.String("gcp-us-central1"),
		})
		if err != nil {
			t.Fatalf("copy with source_region: %v", err)
		}
		if copied.RowsAffected != 1 {
			t.Fatalf("copied=%d", copied.RowsAffected)
		}
	})
}
