package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/turbopuffer/turbopuffer-go/v2"
)

func TestOfficialWriteSemantics(t *testing.T) {
	client, _, srv, cleanup := officialSetup(t, "semw_")
	defer cleanup()
	ctx := context.Background()

	t.Run("upsert_overwrites_omitted_attrs", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a", "price": 10},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "b"},
			},
		})
		row := mustGet(t, ns, ctx, 1)
		if row["name"] != "b" {
			t.Fatalf("name=%v", row["name"])
		}
		if _, ok := row["price"]; ok && row["price"] != nil {
			t.Fatalf("price should be cleared on upsert overwrite, row=%v", row)
		}
	})

	t.Run("upsert_columns", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertColumns: turbopuffer.ColumnsParam{
				"id":     []any{1, 2},
				"vector": []any{[]float32{1, 0, 0}, []float32{0, 1, 0}},
				"name":   []any{"a", "b"},
			},
		})
		rows := mustExport(t, ns, ctx)
		if len(rows) != 2 || rows[0]["name"] != "a" || rows[1]["name"] != "b" {
			t.Fatalf("rows=%v", rows)
		}
	})

	t.Run("patch_does_not_create_and_merges", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a", "price": 10},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchRows: []turbopuffer.RowParam{{"id": 99, "name": "ghost"}},
		})
		if n := mustCount(t, ns, ctx); n != 1 {
			t.Fatalf("patch created a row: count=%d", n)
		}
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchRows: []turbopuffer.RowParam{{"id": 1, "name": "b"}},
		})
		row := mustGet(t, ns, ctx, 1)
		if row["name"] != "b" {
			t.Fatalf("name=%v", row["name"])
		}
		if toInt(row["price"]) != 10 {
			t.Fatalf("price should be preserved, row=%v", row)
		}
	})

	t.Run("patch_rejects_vectors", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a"},
			},
		})
		_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			PatchRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{0, 1, 0}},
			},
		})
		if err == nil {
			t.Fatal("expected vector patch to fail")
		}
	})

	t.Run("patch_columns_and_patch_by_filter", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a", "color": "red"},
				{"id": 2, "vector": []float32{0, 1, 0}, "name": "b", "color": "red"},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchColumns: turbopuffer.ColumnsParam{
				"id":   []any{1},
				"name": []any{"aa"},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchByFilter: turbopuffer.NamespaceWriteParamsPatchByFilter{
				Filters: turbopuffer.NewFilterEq("color", "red"),
				Patch:   map[string]any{"color": "blue"},
			},
		})
		rows := mustExport(t, ns, ctx)
		if rows[0]["name"] != "aa" || rows[0]["color"] != "blue" || rows[1]["color"] != "blue" {
			t.Fatalf("rows=%v", rows)
		}
	})

	t.Run("write_order_delete_by_filter_before_upsert", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "keep"},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DeleteByFilter: turbopuffer.NewFilterEq("name", "keep"),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "keep"},
			},
		})
		if n := mustCount(t, ns, ctx); n != 1 {
			t.Fatalf("expected upsert after delete_by_filter to recreate id=1, count=%d", n)
		}
	})

	t.Run("deletes_and_delete_by_filter", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a"},
				{"id": 2, "vector": []float32{0, 1, 0}, "name": "b"},
				{"id": 3, "vector": []float32{0, 0, 1}, "name": "c"},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{Deletes: []any{1}})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DeleteByFilter: turbopuffer.NewFilterEq("name", "b"),
		})
		rows := mustExport(t, ns, ctx)
		if len(rows) != 1 || toInt(rows[0]["id"]) != 3 {
			t.Fatalf("rows=%v", rows)
		}
	})

	t.Run("upsert_condition_insert_if_not_exists", func(t *testing.T) {
		ns := officialNS(t, client)
		params := turbopuffer.NamespaceWriteParams{
			DistanceMetric:  turbopuffer.DistanceMetricCosineDistance,
			UpsertCondition: turbopuffer.NewFilterEq("id", nil),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "first"},
			},
		}
		mustWrite(t, ns, ctx, params)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			UpsertCondition: turbopuffer.NewFilterEq("id", nil),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "second"},
			},
		})
		row := mustGet(t, ns, ctx, 1)
		if row["name"] != "first" {
			t.Fatalf("existing row should be skipped, row=%v", row)
		}
	})

	t.Run("upsert_condition_ref_new", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "updated_at": 10},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			UpsertCondition: turbopuffer.NewFilterLt("updated_at", turbopuffer.NewExprRefNew("updated_at")),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "updated_at": 20},
			},
		})
		if toInt(mustGet(t, ns, ctx, 1)["updated_at"]) != 20 {
			t.Fatalf("newer write should apply")
		}
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			UpsertCondition: turbopuffer.NewFilterLt("updated_at", turbopuffer.NewExprRefNew("updated_at")),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "updated_at": 15},
			},
		})
		if toInt(mustGet(t, ns, ctx, 1)["updated_at"]) != 20 {
			t.Fatalf("stale write should be skipped")
		}
	})

	t.Run("patch_and_delete_conditions", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "keep", "n": 1},
				{"id": 2, "vector": []float32{0, 1, 0}, "name": "drop", "n": 1},
			},
		})
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			PatchCondition: turbopuffer.NewFilterEq("name", "keep"),
			PatchRows: []turbopuffer.RowParam{
				{"id": 1, "n": 2},
				{"id": 2, "n": 2},
			},
		})
		if toInt(mustGet(t, ns, ctx, 1)["n"]) != 2 || toInt(mustGet(t, ns, ctx, 2)["n"]) != 1 {
			t.Fatalf("patch_condition applied to the wrong rows")
		}
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DeleteCondition: turbopuffer.NewFilterEq("name", "drop"),
			Deletes:         []any{1, 2},
		})
		rows := mustExport(t, ns, ctx)
		if len(rows) != 1 || toInt(rows[0]["id"]) != 1 {
			t.Fatalf("delete_condition=%v", rows)
		}
	})

	t.Run("mixed_write_is_atomic", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "keep"},
			},
		})
		_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			DeleteByFilter: turbopuffer.NewFilterEq("name", "keep"),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 2, "vector": []float32{1, 0}, "name": "bad-dims"},
			},
		})
		if err == nil {
			t.Fatal("expected mixed write with bad upsert to fail")
		}
		if n := mustCount(t, ns, ctx); n != 1 {
			t.Fatalf("delete_by_filter should roll back with the failed upsert, count=%d", n)
		}
	})

	t.Run("rows_affected_counts_applied_rows", func(t *testing.T) {
		ns := officialNS(t, client)
		mustWrite(t, ns, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "first"},
			},
		})
		skipped, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			UpsertCondition: turbopuffer.NewFilterEq("id", nil),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "second"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if skipped.RowsAffected != 0 || skipped.RowsUpserted != 0 {
			t.Fatalf("skipped conditional upsert should not count, affected=%d upserted=%d", skipped.RowsAffected, skipped.RowsUpserted)
		}
		missing, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			Deletes: []any{99},
		})
		if err != nil {
			t.Fatal(err)
		}
		if missing.RowsAffected != 0 || missing.RowsDeleted != 0 {
			t.Fatalf("missing delete should not count, affected=%d deleted=%d", missing.RowsAffected, missing.RowsDeleted)
		}
		patched, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			PatchCondition: turbopuffer.NewFilterEq("name", "nope"),
			PatchRows:      []turbopuffer.RowParam{{"id": 1, "name": "x"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if patched.RowsAffected != 0 || patched.RowsPatched != 0 {
			t.Fatalf("skipped patch should not count, affected=%d patched=%d", patched.RowsAffected, patched.RowsPatched)
		}
	})

	t.Run("return_affected_ids", func(t *testing.T) {
		ns := officialNS(t, client)
		wrote, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric:    turbopuffer.DistanceMetricCosineDistance,
			ReturnAffectedIDs: turbopuffer.Bool(true),
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "keep"},
				{"id": 2, "vector": []float32{0, 1, 0}, "name": "drop"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(wrote.UpsertedIDs) != 2 {
			t.Fatalf("upserted_ids=%v", wrote.UpsertedIDs)
		}
		patched, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			ReturnAffectedIDs: turbopuffer.Bool(true),
			PatchCondition:    turbopuffer.NewFilterEq("name", "keep"),
			PatchRows: []turbopuffer.RowParam{
				{"id": 1, "name": "kept"},
				{"id": 2, "name": "nope"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if patched.RowsPatched != 1 || len(patched.PatchedIDs) != 1 || patched.PatchedIDs[0].AsInt() != 1 {
			t.Fatalf("patched=%d ids=%v", patched.RowsPatched, patched.PatchedIDs)
		}
		deleted, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			ReturnAffectedIDs: turbopuffer.Bool(true),
			Deletes:           []any{2, 99},
		})
		if err != nil {
			t.Fatal(err)
		}
		if deleted.RowsDeleted != 1 || len(deleted.DeletedIDs) != 1 || deleted.DeletedIDs[0].AsInt() != 2 {
			t.Fatalf("deleted=%d ids=%v", deleted.RowsDeleted, deleted.DeletedIDs)
		}
	})

	t.Run("duplicate_id_in_write_is_400", func(t *testing.T) {
		ns := officialNS(t, client)
		_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}},
			},
			Deletes: []any{1},
		})
		if err == nil {
			t.Fatal("expected duplicate id to fail")
		}
		var apierr *turbopuffer.Error
		if !errors.As(err, &apierr) || apierr.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %v", err)
		}
	})

	t.Run("copy_requires_empty_dest", func(t *testing.T) {
		src := officialNS(t, client)
		mustWrite(t, src, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "a"},
			},
		})
		dest := officialNS(t, client)
		copied, err := dest.CopyFrom(ctx, turbopuffer.NamespaceCopyFromParams{SourceNamespace: src.ID()})
		if err != nil {
			t.Fatalf("copy: %v", err)
		}
		if copied.RowsAffected != 1 {
			t.Fatalf("copied=%d", copied.RowsAffected)
		}
		_, err = dest.CopyFrom(ctx, turbopuffer.NamespaceCopyFromParams{SourceNamespace: src.ID()})
		if err == nil {
			t.Fatal("expected copy into non-empty dest to fail")
		}
	})

	t.Run("branch_namespaces_are_independent", func(t *testing.T) {
		src := officialNS(t, client)
		mustWrite(t, src, ctx, turbopuffer.NamespaceWriteParams{
			DistanceMetric: turbopuffer.DistanceMetricCosineDistance,
			UpsertRows: []turbopuffer.RowParam{
				{"id": 1, "vector": []float32{1, 0, 0}, "name": "orig"},
			},
		})
		dest := officialNS(t, client)
		if _, err := dest.BranchFrom(ctx, turbopuffer.NamespaceBranchFromParams{SourceNamespace: src.ID()}); err != nil {
			t.Fatalf("branch: %v", err)
		}
		mustWrite(t, dest, ctx, turbopuffer.NamespaceWriteParams{
			PatchRows: []turbopuffer.RowParam{{"id": 1, "name": "branch"}},
		})
		if mustGet(t, src, ctx, 1)["name"] != "orig" {
			t.Fatalf("source mutated by branch write")
		}
		if mustGet(t, dest, ctx, 1)["name"] != "branch" {
			t.Fatalf("branch not independent")
		}
	})

	t.Run("upsert_rows_object_422", func(t *testing.T) {
		ns := officialNS(t, client)
		body, _ := json.Marshal(map[string]interface{}{
			"distance_metric": "euclidean_squared",
			"upsert_rows":     map[string]interface{}{"id": 2, "vector": []float32{2, 2}},
		})
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v2/namespaces/"+ns.ID(), bytes.NewReader(body))
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
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d", res.StatusCode)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		msg := fmt.Sprint(payload["error"])
		if !strings.Contains(msg, "invalid type: map, expected a sequence") {
			t.Fatalf("error=%v", payload)
		}
	})
}
