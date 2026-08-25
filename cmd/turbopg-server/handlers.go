package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arjunsriva/turbopg"
)

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, APIError{Status: "error", Error: message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"status":"error","error":"Internal server error"}`)); err != nil {
			log.Printf("Error writing response: %v", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := w.Write(response); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

func decodeJSON(r *http.Request, dest interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	return dec.Decode(dest)
}

func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request, namespaceName string) {
	started := time.Now()
	var req WriteRequest
	if err := decodeJSON(r, &req); err != nil {
		if errors.Is(err, errUpsertRowsNotSequence) || strings.Contains(err.Error(), errUpsertRowsNotSequence.Error()) {
			respondWithError(w, http.StatusUnprocessableEntity, errUpsertRowsNotSequence.Error())
			return
		}
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	if req.Encryption.customerManaged() {
		key := req.Encryption.KeyName
		if key == "" {
			key = "unknown"
		}
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Malformed Cloud KMS crypto key: %s", key))
		return
	}

	if !s.requireStore(w) {
		return
	}

	if spec := copySpec(req); spec != nil {
		if writeHasDocuments(req) {
			respondWithError(w, http.StatusBadRequest, "copy_from/branch_from cannot be combined with document writes or schema")
			return
		}
		s.handleCopyNamespace(w, r, namespaceName, spec.SourceNamespace, started)
		return
	}

	var reqSchema map[string]interface{}
	if len(req.Schema) > 0 {
		if err := json.Unmarshal(req.Schema, &reqSchema); err != nil {
			respondWithError(w, http.StatusBadRequest, fmt.Sprintf("invalid schema: %v", err))
			return
		}
	}

	docs := []turbopg.Document{}
	if len(req.UpsertRows) > 0 {
		parsed, err := documentsFromRows(req.UpsertRows)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		docs = append(docs, parsed...)
	}
	if len(req.UpsertColumns) > 0 {
		parsed, err := documentsFromColumns(req.UpsertColumns)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		docs = append(docs, parsed...)
	}

	var patchDocs []turbopg.Document
	if len(req.PatchRows) > 0 {
		parsed, err := documentsFromRows(req.PatchRows)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		patchDocs = append(patchDocs, parsed...)
	}
	if len(req.PatchColumns) > 0 {
		parsed, err := documentsFromColumns(req.PatchColumns)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		patchDocs = append(patchDocs, parsed...)
	}

	upsertCondition, err := parseFilter(req.UpsertCondition)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("upsert_condition: %v", err))
		return
	}
	patchCondition, err := parseFilter(req.PatchCondition)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("patch_condition: %v", err))
		return
	}
	deleteCondition, err := parseFilter(req.DeleteCondition)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("delete_condition: %v", err))
		return
	}

	_, err = s.Store.GetNamespace(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		dims, dimErr := s.namespaceDimsForCreate(r.Context(), reqSchema, docs, patchDocs)
		if dimErr != nil {
			respondWithError(w, writeStatus(dimErr), dimErr.Error())
			return
		}
		metric := req.DistanceMetric
		if dims > 0 && metric == "" {
			respondWithError(w, http.StatusBadRequest, "distance_metric is required when the namespace has vectors")
			return
		}
		if metric == "" {
			metric = "cosine_distance"
		}
		lists := req.Lists
		if lists <= 0 {
			lists = s.DefaultLists
		}
		if _, err = s.Store.EnsureNamespace(r.Context(), namespaceName, turbopg.CreateNamespaceOptions{
			Dimensions: dims,
			IndexConfig: &turbopg.IndexConfig{
				DistanceMetric: metric,
				Lists:          lists,
			},
		}); err != nil {
			respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create namespace: %v", err))
			return
		}
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(reqSchema) > 0 {
		if err := s.Store.UpdateSchema(r.Context(), namespaceName, reqSchema); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	specs := s.loadEmbedSpecs(r.Context(), namespaceName, reqSchema)
	if err := s.applyEmbeddings(r.Context(), docs, specs); err != nil {
		respondWithError(w, writeStatus(err), err.Error())
		return
	}
	if hasClientVector(patchDocs) {
		respondWithError(w, http.StatusBadRequest, "vector attributes cannot be patched")
		return
	}
	if err := s.applyEmbeddings(r.Context(), patchDocs, specs); err != nil {
		respondWithError(w, writeStatus(err), err.Error())
		return
	}

	// Document mutations run in one Store.Write transaction.
	write := turbopg.Write{
		Namespace:       namespaceName,
		Upserts:         docs,
		UpsertCondition: upsertCondition,
		Patches:         patchDocs,
		PatchCondition:  patchCondition,
		Deletes:         idsFromJSON(req.Deletes),
		DeleteCondition: deleteCondition,
	}
	if req.DeleteByFilter != nil {
		filter, err := parseFilter(req.DeleteByFilter)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.rejectUnfilterable(r, namespaceName, filter); err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		write.DeleteByFilter = filter
	}
	if req.PatchByFilter != nil {
		filter, err := parseFilter(req.PatchByFilter.Filters)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.rejectUnfilterable(r, namespaceName, filter); err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		attrs := req.PatchByFilter.Patch
		if attrs == nil {
			attrs = map[string]interface{}{}
		}
		if _, ok := attrs["vector"]; ok {
			respondWithError(w, http.StatusBadRequest, "vector attributes cannot be patched")
			return
		}
		delete(attrs, "id")
		vector, attrs, err := s.embedPatchAttrs(r.Context(), attrs, specs)
		if err != nil {
			respondWithError(w, writeStatus(err), err.Error())
			return
		}
		write.PatchByFilter = &turbopg.PatchByFilterWrite{
			Filter:     filter,
			Attributes: attrs,
			Vector:     vector,
		}
	}

	result, err := s.Store.Write(r.Context(), write)
	if err != nil {
		respondWithError(w, writeStatus(err), err.Error())
		return
	}

	resp := WriteResponse{
		Status:       "OK",
		Message:      "OK",
		RowsAffected: result.Affected(),
		RowsUpserted: result.Upserted,
		RowsDeleted:  result.Deleted,
		RowsPatched:  result.Patched,
		Billing: map[string]interface{}{
			"billable_logical_bytes_written": 0,
		},
		Performance: writePerformance(started),
	}
	if req.ReturnAffectedIDs {
		resp.UpsertedIDs = exportIDs(result.UpsertedIDs)
		resp.PatchedIDs = exportIDs(result.PatchedIDs)
		resp.DeletedIDs = exportIDs(result.DeletedIDs)
	}
	if result.RowsRemaining > 0 {
		resp.RowsRemaining = result.RowsRemaining
	}
	respondWithJSON(w, http.StatusOK, resp)
}

func copySpec(req WriteRequest) *CopyNamespaceSpec {
	if req.CopyFromNamespace != nil && req.CopyFromNamespace.SourceNamespace != "" {
		return req.CopyFromNamespace
	}
	if req.BranchFromNamespace != nil && req.BranchFromNamespace.SourceNamespace != "" {
		return req.BranchFromNamespace
	}
	return nil
}

func (s *Server) handleDeleteNamespace(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if !s.requireStore(w) {
		return
	}

	_, err := s.Store.GetNamespace(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("namespace '%s' not found", namespaceName))
		return
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Store.DeleteNamespace(r.Context(), namespaceName); err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "OK"})
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if !s.requireStore(w) {
		return
	}
	stats, err := s.Store.GetNamespaceStats(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("namespace '%s' not found", namespaceName))
		return
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	schema, err := s.Store.GetSchema(r.Context(), namespaceName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created := stats.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	updated := stats.UpdatedAt
	if updated.IsZero() {
		updated = created
	}
	bytes := stats.ApproximateCount * int64(stats.Dimensions) * 4
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"approx_logical_bytes": bytes,
		"approx_row_count":     stats.ApproximateCount,
		"created_at":           created.UTC().Format(time.RFC3339),
		"updated_at":           updated.UTC().Format(time.RFC3339),
		"last_write_at":        updated.UTC().Format(time.RFC3339),
		"encryption":           map[string]string{"mode": "default"},
		"index":                map[string]string{"status": "up-to-date"},
		"schema":               schema,
	})
}

func (s *Server) handleHeadNamespace(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if s.Store == nil {
		http.Error(w, "store not initialized", http.StatusInternalServerError)
		return
	}
	stats, err := s.Store.GetNamespaceStats(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-turbopuffer-Approx-Num-Vectors", fmt.Sprintf("%d", stats.ApproximateCount))
	w.Header().Set("X-turbopuffer-Dimensions", fmt.Sprintf("%d", stats.Dimensions))
	w.Header().Set("X-turbopuffer-Distance-Metric", stats.DistanceMetric)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request, namespaceName string) {
	started := time.Now()
	var req QueryRequest
	if err := decodeJSON(r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	if !s.requireStore(w) {
		return
	}

	if len(req.Queries) > 0 || r.URL.Query().Get("stainless_overload") == "multiQuery" {
		s.handleMultiQuery(w, r, namespaceName, req, started)
		return
	}

	resp, err := s.runQuery(r, namespaceName, req, started)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, resp)
}

func metricOrDefault(req, fallback string) string {
	if req != "" {
		return req
	}
	return fallback
}

func (s *Server) handleDebugOperation(w http.ResponseWriter, r *http.Request, namespaceName string, operation string) {
	if !s.requireStore(w) {
		return
	}
	_, err := s.Store.GetNamespace(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("namespace '%s' not found", namespaceName))
		return
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{
		"status":  "OK",
		"message": fmt.Sprintf("Debug operation '%s' is a no-op for turbopg-server.", operation),
	})
}

type clientError struct {
	status int
	msg    string
}

func (e *clientError) Error() string { return e.msg }

type queryError = clientError

func writeQueryError(w http.ResponseWriter, err error) {
	var qe *clientError
	if errors.As(err, &qe) {
		respondWithError(w, qe.status, qe.msg)
		return
	}
	if turbopg.IsInvalidInput(err) {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}
	respondWithError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) runQuery(r *http.Request, namespaceName string, req QueryRequest, started time.Time) (QueryResponse, error) {
	if err := validateConsistency(req.Consistency); err != nil {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
	}
	if req.resultLimit() > turbopg.MaxTopK {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: fmt.Sprintf("top_k exceeds %d", turbopg.MaxTopK)}
	}
	stats, err := s.Store.GetNamespaceStats(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		return QueryResponse{}, &queryError{status: http.StatusNotFound, msg: fmt.Sprintf("namespace '%s' not found", namespaceName)}
	}
	if err != nil {
		return QueryResponse{}, err
	}

	filterRaw := req.Filters
	if filterRaw == nil {
		filterRaw = req.Filter
	}
	filter, err := parseFilter(filterRaw)
	if err != nil {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
	}

	if err := s.rejectUnfilterable(r, namespaceName, filter); err != nil {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
	}

	include := parseIncludeAttributes(req.IncludeAttributes)
	if include.set && len(req.ExcludeAttributes) > 0 {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "include_attributes and exclude_attributes are mutually exclusive"}
	}

	resp := QueryResponse{
		Billing:     QueryBilling{},
		Performance: queryPerformance(stats.ApproximateCount, started),
	}

	if len(req.GroupBy) > 0 && len(req.AggregateBy) == 0 {
		return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "group_by requires aggregate_by"}
	}

	if len(req.AggregateBy) > 0 {
		if len(req.RankBy) > 0 || len(req.Vector) > 0 {
			return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "aggregations cannot be combined with rank_by"}
		}
		if include.set {
			return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "aggregations cannot be combined with include_attributes"}
		}
		aggSpecs := make([]turbopg.AggregateSpec, 0, len(req.AggregateBy))
		for label, spec := range req.AggregateBy {
			if len(spec) == 0 {
				return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "aggregate_by values must be non-empty arrays"}
			}
			op, _ := spec[0].(string)
			as := turbopg.AggregateSpec{Label: label, Op: op}
			if op == "Sum" {
				if len(spec) < 2 {
					return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "Sum aggregation requires a field"}
				}
				field, _ := spec[1].(string)
				as.Field = field
			} else if op != "Count" {
				return QueryResponse{}, &queryError{status: http.StatusNotImplemented, msg: fmt.Sprintf("aggregation %q is not supported", op)}
			}
			aggSpecs = append(aggSpecs, as)
		}
		groupBy, err := parseGroupBy(req.GroupBy)
		if err != nil {
			return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
		}
		if len(groupBy) > 0 {
			groups, err := s.Store.AggregateGrouped(r.Context(), namespaceName, groupBy, filter, aggSpecs)
			if err != nil {
				return QueryResponse{}, err
			}
			if per := req.limitPer(); per > 0 && len(groups) > per {
				groups = groups[:per]
			}
			resp.AggregationGroups = groups
			resp.Performance = queryPerformance(stats.ApproximateCount, started)
			return resp, nil
		}
		aggregations := map[string]interface{}{}
		for _, spec := range aggSpecs {
			switch spec.Op {
			case "Count":
				count, err := s.Store.CountDocuments(r.Context(), namespaceName, filter)
				if err != nil {
					return QueryResponse{}, err
				}
				aggregations[spec.Label] = count
			case "Sum":
				sum, err := s.Store.SumAttribute(r.Context(), namespaceName, spec.Field, filter)
				if err != nil {
					return QueryResponse{}, err
				}
				aggregations[spec.Label] = sum
			}
		}
		resp.Aggregations = aggregations
		resp.Performance = queryPerformance(stats.ApproximateCount, started)
		return resp, nil
	}

	opts := turbopg.QueryOptions{
		Namespace: namespaceName,
		Filter:    filter,
		TopK:      req.resultLimit(),
		Metric:    metricOrDefault(req.Metric, stats.DistanceMetric),
		Vector:    req.Vector,
	}
	if len(req.RankBy) > 0 {
		spec, err := parseRankBy(req.RankBy)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "unsupported rank_by") {
				status = http.StatusNotImplemented
			}
			return QueryResponse{}, &queryError{status: status, msg: err.Error()}
		}
		if spec.Exact && filter == nil {
			return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: "kNN requires filters"}
		}
		opts.Rank = spec
		opts.Exact = spec.Exact
		opts.Vector = nil
		opts.ComputeAttributes = computeAttributesEnabled(req.ComputeAttributes)
		if spec.Kind == turbopg.RankAttribute {
			if err := s.rejectUnfilterableField(r, namespaceName, spec.Field); err != nil {
				return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
			}
		}
		if spec.EmbedQuery != "" {
			resolved, err := s.resolveEmbedQuery(r.Context(), spec, stats.Schema, stats.Dimensions)
			if err != nil {
				return QueryResponse{}, &queryError{status: writeStatus(err), msg: err.Error()}
			}
			opts.Rank = resolved
		}
	}

	results, err := s.Store.Query(r.Context(), opts)
	if err != nil {
		if errors.Is(err, turbopg.ErrTextSearchUnavailable) {
			return QueryResponse{}, &queryError{status: http.StatusBadRequest, msg: err.Error()}
		}
		return QueryResponse{}, err
	}

	rows := make([]map[string]interface{}, 0, len(results))
	for _, res := range results {
		rows = append(rows, resultRow(res, include, req.ExcludeAttributes))
	}
	resp.Rows = rows
	resp.Performance = queryPerformance(stats.ApproximateCount, started)
	return resp, nil
}

func writeHasDocuments(req WriteRequest) bool {
	return len(req.UpsertRows) > 0 || len(req.UpsertColumns) > 0 ||
		len(req.PatchRows) > 0 || len(req.PatchColumns) > 0 ||
		req.PatchByFilter != nil || len(req.Deletes) > 0 || req.DeleteByFilter != nil ||
		len(req.Schema) > 0
}

func writeStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var ce *clientError
	if errors.As(err, &ce) {
		return ce.status
	}
	if turbopg.IsInvalidInput(err) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func parseGroupBy(raw []interface{}) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("group_by expressions are not supported")
		}
		out = append(out, s)
	}
	return out, nil
}

func (s *Server) rejectUnfilterable(r *http.Request, namespace string, filter turbopg.Filter) error {
	if filter == nil {
		return nil
	}
	schema, err := s.Store.GetSchema(r.Context(), namespace)
	if err != nil {
		if turbopg.IsNotFound(err) {
			return nil
		}
		return err
	}
	return checkFilterable(schema, filter)
}

func checkFilterable(schema map[string]interface{}, filter turbopg.Filter) error {
	if filter == nil {
		return nil
	}
	switch f := filter.(type) {
	case turbopg.FilterCondition:
		if !schemaFieldFilterable(schema, f.Field, f.Op) {
			return fmt.Errorf("attribute %q is not filterable", f.Field)
		}
	case *turbopg.FilterCondition:
		return checkFilterable(schema, *f)
	case turbopg.LogicalFilter:
		for _, inner := range f.Filters {
			if err := checkFilterable(schema, inner); err != nil {
				return err
			}
		}
	case *turbopg.LogicalFilter:
		return checkFilterable(schema, *f)
	case turbopg.NotFilter:
		return checkFilterable(schema, f.Filter)
	case *turbopg.NotFilter:
		return checkFilterable(schema, *f)
	}
	return nil
}

func schemaFieldFilterable(schema map[string]interface{}, field string, op turbopg.FilterOp) bool {
	if field == "id" || field == "vector" {
		return true
	}
	raw, ok := schema[field]
	if !ok {
		return true
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return true
	}
	fts := hasFTSEnabled(m)
	if fts && isTokenFilterOp(op) {
		return true
	}
	if v, ok := m["filterable"].(bool); ok {
		return v
	}
	if fts {
		return false
	}
	return true
}

func hasFTSEnabled(m map[string]interface{}) bool {
	fts, ok := m["full_text_search"]
	if !ok || fts == nil {
		return false
	}
	if b, isBool := fts.(bool); isBool {
		return b
	}
	return true
}

func isTokenFilterOp(op turbopg.FilterOp) bool {
	switch op {
	case turbopg.FilterOpContainsAllTokens, turbopg.FilterOpContainsAnyToken, turbopg.FilterOpContainsTokenSequence:
		return true
	default:
		return false
	}
}

func validateConsistency(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var level string
	if err := json.Unmarshal(raw, &level); err != nil {
		var obj struct {
			Level string `json:"level"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("invalid consistency")
		}
		level = obj.Level
	}
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "strong", "eventual":
		return nil
	default:
		return fmt.Errorf("consistency %q is not supported", level)
	}
}

func computeAttributesEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "true" {
		return true
	}
	var fields []string
	if err := json.Unmarshal(raw, &fields); err == nil && len(fields) > 0 {
		return true
	}
	return false
}

func (s *Server) rejectUnfilterableField(r *http.Request, namespace, field string) error {
	if field == "" || field == "id" {
		return nil
	}
	schema, err := s.Store.GetSchema(r.Context(), namespace)
	if err != nil {
		if turbopg.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !schemaFieldFilterable(schema, field, "") {
		return fmt.Errorf("attribute %q is not filterable", field)
	}
	return nil
}
