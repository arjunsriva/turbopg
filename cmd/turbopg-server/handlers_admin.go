package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arjunsriva/turbopg"
)

func (s *Server) handleListNamespaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireStore(w) {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			respondWithError(w, http.StatusBadRequest, "invalid page_size")
			return
		}
		limit = n
	}
	listed, err := s.Store.ListNamespaces(r.Context(), turbopg.ListNamespacesOptions{
		Prefix: r.URL.Query().Get("prefix"),
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summaries := make([]map[string]string, 0, len(listed.Namespaces))
	for _, name := range listed.Namespaces {
		summaries = append(summaries, map[string]string{"id": name})
	}
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"namespaces":  summaries,
		"next_cursor": listed.NextCursor,
	})
}

func (s *Server) handleCopyNamespace(w http.ResponseWriter, r *http.Request, dest, source string, started time.Time) {
	if source == "" {
		respondWithError(w, http.StatusBadRequest, "source_namespace is required")
		return
	}
	n, err := s.Store.CopyNamespace(r.Context(), dest, source)
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "must be empty") {
			status = http.StatusBadRequest
		}
		respondWithError(w, status, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, WriteResponse{
		Status:       "OK",
		Message:      "OK",
		RowsAffected: int(n),
		RowsUpserted: int(n),
		Billing: map[string]interface{}{
			"billable_logical_bytes_written": 0,
		},
		Performance: writePerformance(started),
	})
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if !s.requireStore(w) {
		return
	}
	schema, err := s.Store.GetSchema(r.Context(), namespaceName)
	if turbopg.IsNotFound(err) {
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("namespace '%s' not found", namespaceName))
		return
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, schema)
}

func (s *Server) handleUpdateSchema(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if !s.requireStore(w) {
		return
	}
	var schema map[string]interface{}
	if err := decodeJSON(r, &schema); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	if err := s.Store.UpdateSchema(r.Context(), namespaceName, schema); err != nil {
		if turbopg.IsNotFound(err) {
			respondWithError(w, http.StatusNotFound, fmt.Sprintf("namespace '%s' not found", namespaceName))
			return
		}
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := s.Store.GetSchema(r.Context(), namespaceName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateMetadata(w http.ResponseWriter, r *http.Request, namespaceName string) {
	s.handleMetadata(w, r, namespaceName)
}

func (s *Server) handleExplainQuery(w http.ResponseWriter, r *http.Request, namespaceName string) {
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
	respondWithJSON(w, http.StatusOK, map[string]string{
		"plan_text": fmt.Sprintf("pgvector ivfflat scan on %s (%d dims, ~%d rows)", namespaceName, stats.Dimensions, stats.ApproximateCount),
	})
}

func (s *Server) handleHintCacheWarm(w http.ResponseWriter, r *http.Request, namespaceName string) {
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
		"status":  "ACCEPTED",
		"message": "cache already hot",
	})
}

func (s *Server) handleMultiQuery(w http.ResponseWriter, r *http.Request, namespaceName string, req QueryRequest, started time.Time) {
	if len(req.Queries) > turbopg.MaxMultiQueries {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("at most %d queries are allowed", turbopg.MaxMultiQueries))
		return
	}
	results := make([]map[string]interface{}, 0, len(req.Queries))
	var last QueryResponse
	var rowLists [][]map[string]interface{}
	for _, q := range req.Queries {
		resp, err := s.runQuery(r, namespaceName, q, started)
		if err != nil {
			writeQueryError(w, err)
			return
		}
		last = resp
		entry := map[string]interface{}{}
		if resp.Rows != nil {
			entry["rows"] = resp.Rows
		}
		if resp.Aggregations != nil {
			entry["aggregations"] = resp.Aggregations
		}
		results = append(results, entry)
		rowLists = append(rowLists, resp.Rows)
	}

	if fused, ok := rrfFuse(req.RerankBy, rowLists, req.resultLimit()); ok {
		respondWithJSON(w, http.StatusOK, map[string]interface{}{
			"results":     []map[string]interface{}{{"rows": fused}},
			"performance": last.Performance,
			"billing":     last.Billing,
		})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"results":     results,
		"performance": last.Performance,
		"billing":     last.Billing,
	})
}

func rrfFuse(rerank []interface{}, lists [][]map[string]interface{}, limit int) ([]map[string]interface{}, bool) {
	if len(rerank) == 0 {
		return nil, false
	}
	op, _ := rerank[0].(string)
	if !strings.EqualFold(op, "RRF") {
		return nil, false
	}
	rankK := 60.0
	var weights []float64
	if len(rerank) > 1 {
		if cfg, ok := rerank[1].(map[string]interface{}); ok {
			if v, err := toFloat64(cfg["rank_constant"]); err == nil && v > 0 {
				rankK = v
			}
			if raw, ok := cfg["weights"].([]interface{}); ok {
				for _, w := range raw {
					f, err := toFloat64(w)
					if err != nil {
						f = 1
					}
					weights = append(weights, f)
				}
			}
		}
	}
	type scored struct {
		row   map[string]interface{}
		score float64
	}
	byID := map[string]*scored{}
	order := make([]string, 0)
	for i, list := range lists {
		w := 1.0
		if i < len(weights) {
			w = weights[i]
		}
		for rank, row := range list {
			id := fmt.Sprint(row["id"])
			s, ok := byID[id]
			if !ok {
				copied := make(map[string]interface{}, len(row))
				for k, v := range row {
					copied[k] = v
				}
				s = &scored{row: copied}
				byID[id] = s
				order = append(order, id)
			}
			s.score += w / (rankK + float64(rank+1))
		}
	}
	fused := make([]scored, 0, len(order))
	for _, id := range order {
		s := byID[id]
		s.row["$dist"] = s.score
		fused = append(fused, *s)
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].score > fused[j].score })
	if limit <= 0 {
		limit = 10
	}
	if limit > len(fused) {
		limit = len(fused)
	}
	out := make([]map[string]interface{}, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, fused[i].row)
	}
	return out, true
}

func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request, namespaceName string) {
	if !s.requireStore(w) {
		return
	}
	var req RecallRequest
	if err := decodeJSON(r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
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
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	num := req.Num
	if num <= 0 {
		num = 1
	}

	filter, err := parseFilter(req.Filters)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	queries := [][]float32{}
	if len(req.RankBy) > 0 {
		spec, err := parseRankBy(req.RankBy)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(spec.Vector) > 0 {
			queries = append(queries, spec.Vector)
			num = 1
		}
	}
	if len(queries) == 0 {
		sample, err := s.Store.Query(r.Context(), turbopg.QueryOptions{
			Namespace: namespaceName,
			TopK:      num,
		})
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, row := range sample {
			if len(row.Document.Vector) > 0 {
				queries = append(queries, row.Document.Vector)
			}
		}
	}
	if len(queries) == 0 {
		respondWithJSON(w, http.StatusOK, map[string]interface{}{
			"avg_ann_count":        0,
			"avg_exhaustive_count": 0,
			"avg_recall":           1,
		})
		return
	}

	var recallSum, annCount, exactCount float64
	var ground []map[string]interface{}
	for _, qvec := range queries {
		ann, err := s.Store.Query(r.Context(), turbopg.QueryOptions{
			Namespace: namespaceName,
			Vector:    qvec,
			Filter:    filter,
			TopK:      topK,
			Metric:    stats.DistanceMetric,
			Exact:     false,
		})
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		exact, err := s.Store.Query(r.Context(), turbopg.QueryOptions{
			Namespace: namespaceName,
			Vector:    qvec,
			Filter:    filter,
			TopK:      topK,
			Metric:    stats.DistanceMetric,
			Exact:     true,
		})
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		exactIDs := map[turbopg.DocumentID]struct{}{}
		for _, row := range exact {
			exactIDs[row.Document.ID] = struct{}{}
		}
		hits := 0
		for _, row := range ann {
			if _, ok := exactIDs[row.Document.ID]; ok {
				hits++
			}
		}
		if len(exact) > 0 {
			recallSum += float64(hits) / float64(len(exact))
		} else {
			recallSum += 1
		}
		annCount += float64(len(ann))
		exactCount += float64(len(exact))
		if req.IncludeGroundTruth {
			neighbors := make([]map[string]interface{}, 0, len(exact))
			for _, row := range exact {
				neighbors = append(neighbors, resultRow(row, includeAttributes{all: true, set: true}, nil))
			}
			qcopy := make([]float64, len(qvec))
			for i, v := range qvec {
				qcopy[i] = float64(v)
			}
			ground = append(ground, map[string]interface{}{
				"query_vector":      qcopy,
				"nearest_neighbors": neighbors,
			})
		}
	}
	n := float64(len(queries))
	out := map[string]interface{}{
		"avg_ann_count":        annCount / n,
		"avg_exhaustive_count": exactCount / n,
		"avg_recall":           recallSum / n,
	}
	if req.IncludeGroundTruth {
		out["ground_truth"] = ground
	}
	respondWithJSON(w, http.StatusOK, out)
}
