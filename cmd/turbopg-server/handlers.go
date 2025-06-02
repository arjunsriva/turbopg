package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arjunsriva/turbopg"
)

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Internal server error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func handleUpsert(w http.ResponseWriter, r *http.Request, namespaceName string) {
	w.Header().Set("Content-Type", "application/json")

	var req APIUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	defer r.Body.Close()

	if globalStore == nil {
		respondWithError(w, http.StatusInternalServerError, "Store not initialized")
		return
	}

	_, err := globalStore.GetNamespace(r.Context(), namespaceName)
	if err != nil {
		// Assuming any error means namespace not found for now.
		// TODO: Check for specific turbopg.NamespaceError if available for more precise error handling.
		log.Printf("Namespace '%s' not found, attempting to create. Error: %v", namespaceName, err)

		if len(req.Upserts) == 0 || len(req.Upserts[0].Vector) == 0 {
			respondWithError(w, http.StatusBadRequest, "cannot create namespace without dimensions from a vector")
			return
		}

		dimensions := len(req.Upserts[0].Vector)
		distanceMetric := req.DistanceMetric
		if distanceMetric == "" {
			distanceMetric = "cosine_distance" // Default distance metric
		}

		opts := turbopg.CreateNamespaceOptions{
			Dimensions:     int32(dimensions),
			DistanceMetric: distanceMetric,
		}

		log.Printf("Creating namespace '%s' with dimensions %d and distance metric '%s'", namespaceName, dimensions, distanceMetric)
		if _, err := globalStore.CreateNamespace(r.Context(), namespaceName, opts); err != nil {
			respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create namespace '%s': %v", namespaceName, err))
			return
		}
		log.Printf("Namespace '%s' created successfully.", namespaceName)
	} else {
		// Namespace exists
		if req.DistanceMetric != "" {
			log.Printf("Info: DistanceMetric in upsert request for existing namespace '%s' is ignored.", namespaceName)
		}
		// TODO: Implement dimension checking for existing namespaces.
		// This is a placeholder for future enhancement as per the issue tracker.
		// For now, we assume vectors will match the existing namespace's dimensions.
		// If not, turbopg.Upsert might return an error which will be caught below.
	}

	if len(req.Upserts) == 0 {
		// No documents to upsert, but namespace might have been created.
		respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "No documents to upsert."})
		return
	}
	
	var docsToUpsert []turbopg.Document
	for _, apiDoc := range req.Upserts {
		docsToUpsert = append(docsToUpsert, turbopg.Document{
			ID:         apiDoc.ID,
			Vector:     apiDoc.Vector,
			Attributes: apiDoc.Attributes,
		})
	}

	upsertOpts := turbopg.UpsertOptions{Namespace: namespaceName}
	if err := globalStore.Upsert(r.Context(), docsToUpsert, upsertOpts); err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to upsert documents into namespace '%s': %v", namespaceName, err))
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleClearNamespace(w http.ResponseWriter, r *http.Request, namespaceName string) {
	w.Header().Set("Content-Type", "application/json")

	if globalStore == nil {
		respondWithError(w, http.StatusInternalServerError, "Store not initialized")
		return
	}

	log.Printf("Interim solution: Attempting to clear namespace '%s' by Get -> Delete -> Recreate.", namespaceName)

	// Step 1: Get existing namespace details
	nsInfo, err := globalStore.GetNamespace(r.Context(), namespaceName)
	if err != nil {
		// TODO: Differentiate "not found" errors from other errors.
		// For now, assume any error from GetNamespace means it's not found or a critical error before deletion.
		// turbopg.IsNotFound(err) or similar would be ideal.
		log.Printf("Namespace '%s' not found or error during GetNamespace: %v. No action taken for clear.", namespaceName, err)
		// The benchmark expects DELETE on a non-existent namespace to be a no-op (effectively success).
		// So, if we can't find it to get its details, it's like it's already "cleared" in a way.
		respondWithJSON(w, http.StatusOK, map[string]string{"message": "namespace did not exist or could not be fetched, no action taken"})
		return
	}

	// Step 2: Delete the namespace
	log.Printf("Deleting namespace '%s' (Dimensions: %d, DistanceMetric: %s) as part of clear operation.",
		namespaceName, nsInfo.Dimensions, nsInfo.DistanceMetric) // Assuming nsInfo has DistanceMetric
	if err := globalStore.DeleteNamespace(r.Context(), namespaceName); err != nil {
		// TODO: Differentiate "not found" errors from other errors.
		// If DeleteNamespace fails because it was *just* deleted by another request, that's technically not an error for "clear".
		// However, a robust check would be complex here. For now, any delete error is treated as a server error.
		log.Printf("Failed to delete namespace '%s': %v", namespaceName, err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete namespace '%s': %v", namespaceName, err))
		return
	}
	log.Printf("Namespace '%s' deleted successfully.", namespaceName)

	// Step 3: Recreate the namespace with the same configuration
	log.Printf("Recreating namespace '%s' with Dimensions: %d, DistanceMetric: %s, IndexConfig: %v",
		namespaceName, nsInfo.Dimensions, nsInfo.DistanceMetric, nsInfo.IndexConfig)

	createOpts := turbopg.CreateNamespaceOptions{
		Dimensions:     nsInfo.Dimensions,
		DistanceMetric: nsInfo.DistanceMetric, // Assumed to exist on nsInfo
		IndexConfig:    nsInfo.IndexConfig,    // Assumed to exist on nsInfo
	}

	if _, err := globalStore.CreateNamespace(r.Context(), namespaceName, createOpts); err != nil {
		log.Printf("Failed to recreate namespace '%s' after clearing: %v", namespaceName, err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to recreate namespace '%s' after clearing: %v", namespaceName, err))
		return
	}

	log.Printf("Interim solution: Namespace '%s' cleared and recreated successfully.", namespaceName)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok, namespace cleared and metadata recreated (interim)"})
}

// Placeholder until available in turbopg library
// Or until we confirm turbopg.NamespaceInfo or similar can be used directly.
type NamespaceStats struct {
	ApproximateCount int64
	Dimensions       int
	DistanceMetric   string // This might come from an IndexConfig sub-struct in the actual turbopg lib
}

func handleHeadNamespace(w http.ResponseWriter, r *http.Request, namespaceName string) {
	log.Printf("Handling HEAD request for namespace: %s", namespaceName)

	if globalStore == nil {
		// This should ideally not happen if main.go setup is correct
		http.Error(w, "Internal server error: store not initialized", http.StatusInternalServerError)
		return
	}

	// Assumption: globalStore.GetNamespaceStats exists or will exist.
	// For now, let's simulate a call to a function that would return NamespaceStats or an error.
	// In a real scenario, this would be:
	// stats, err := globalStore.GetNamespaceStats(r.Context(), namespaceName)
	
	// Simulation / Stand-in for globalStore.GetNamespaceStats
	// Replace this with actual call when available.
	var stats NamespaceStats
	var err error

	// To make this runnable, we need to get this info from existing GetNamespace
	// This is a temporary adaptation. Ideally GetNamespaceStats is a more direct call.
	nsInfo, err := globalStore.GetNamespace(r.Context(), namespaceName)
	if err != nil {
		// TODO: Differentiate "not found" errors from other errors.
		// e.g. if errors.Is(err, turbopg.ErrNamespaceNotFound)
		log.Printf("Error getting namespace info for HEAD request on '%s': %v", namespaceName, err)
		// Assuming any error from GetNamespace here could be a "not found" type error.
		// A more specific error check (e.g., errors.Is(err, turbopg.ErrNamespaceNotFound)) is needed.
		http.Error(w, "Namespace not found", http.StatusNotFound) // Respond with 404
		return
	}
	
	// Populate stats from nsInfo (assuming fields match or can be derived)
	stats.ApproximateCount = nsInfo.Count // Assuming NamespaceInfo has 'Count'
	stats.Dimensions = int(nsInfo.Dimensions) // Assuming NamespaceInfo has 'Dimensions'
	stats.DistanceMetric = nsInfo.DistanceMetric // Assuming NamespaceInfo has 'DistanceMetric'


	// After actual call:
	if err != nil {
		// TODO: Differentiate "not found" errors from other errors.
		// For example, if errors.Is(err, turbopg.ErrNamespaceNotFound) {
		//    w.WriteHeader(http.StatusNotFound)
		//    return
		// }
		log.Printf("Error from GetNamespaceStats for namespace '%s': %v", namespaceName, err)
		// Assuming a generic error means internal server error for now,
		// unless we can specifically identify it as a "not found" error.
		// If GetNamespace above already handled "not found", this block might only see other errors.
		// However, the structure implies GetNamespaceStats is a distinct call.
		// For this placeholder structure, the error from GetNamespace is what we check.
		// The double error check is because of the placeholder nature.
		// If GetNamespace above returned an error, we'd have already exited.
		// So, this 'err' here is from the hypothetical GetNamespaceStats, currently shadowed.
		// Let's assume the above GetNamespace call IS the GetNamespaceStats call for now.
		// The previous `if err != nil` for GetNamespace handles the error.
	}

	w.Header().Set("X-turbopuffer-Approx-Num-Vectors", fmt.Sprintf("%d", stats.ApproximateCount))
	w.Header().Set("X-turbopuffer-Dimensions", fmt.Sprintf("%d", stats.Dimensions))
	w.Header().Set("X-turbopuffer-Distance-Metric", stats.DistanceMetric)
	w.WriteHeader(http.StatusOK)
	// No body is written for a HEAD request; net/http handles this.
}

func handleQuery(w http.ResponseWriter, r *http.Request, namespaceName string) {
	startTime := time.Now()
	w.Header().Set("Content-Type", "application/json")

	var req APIQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	defer r.Body.Close()

	if req.TopK <= 0 {
		respondWithError(w, http.StatusBadRequest, "top_k must be positive")
		return
	}

	if globalStore == nil {
		respondWithError(w, http.StatusInternalServerError, "Store not initialized")
		return
	}

	// Handle RankBy (Full-Text Search) - Not Implemented
	if req.RankBy != nil && len(req.RankBy) > 0 {
		log.Printf("Info: rank_by (FTS) is requested but not implemented: %v", req.RankBy)
		respondWithError(w, http.StatusNotImplemented, "Full-text search (rank_by) is not implemented")
		return
	}

	queryOpts := turbopg.QueryOptions{
		Namespace: namespaceName,
		TopK:      req.TopK,
		Metric:    req.Metric, // If empty, turbopg uses its default
	}

	if len(req.Vector) > 0 {
		queryOpts.Vector = req.Vector
	}


	// Filter Parsing (Initial Version)
	if req.Filter != nil && len(req.Filter) > 0 {
		log.Printf("Received filter: %v", req.Filter)
		// Expecting ["field_name", "OperatorString", value]
		if len(req.Filter) == 3 {
			fieldName, okField := req.Filter[0].(string)
			opStr, okOpStr := req.Filter[1].(string)
			value := req.Filter[2] // Value can be of any type

			if okField && okOpStr {
				var filterOp turbopg.FilterOp
				opFound := true
				switch opStr {
				case "Eq":
					filterOp = turbopg.FilterOpEq
				case "NotEq":
					filterOp = turbopg.FilterOpNotEq
				case "Lt":
					filterOp = turbopg.FilterOpLt
				case "Lte":
					filterOp = turbopg.FilterOpLte
				case "Gt":
					filterOp = turbopg.FilterOpGt
				case "Gte":
					filterOp = turbopg.FilterOpGte
				// TODO: Add other operators like In, NotIn if supported by turbopg and benchmark
				default:
					opFound = false
					log.Printf("Warning: Unknown filter operator string: %s", opStr)
					// Decide if to error out or proceed without filter. Subtask says "proceed without filter".
				}

				if opFound {
					queryOpts.Filter = &turbopg.FilterCondition{
						Field: fieldName,
						Op:    filterOp,
						Value: value,
					}
					log.Printf("Applied filter: Field=%s, Op=%s, Value=%v", fieldName, opStr, value)
				}
			} else {
				log.Printf("Warning: Could not parse filter structure (expected string, string, any): %v", req.Filter)
			}
		} else {
			log.Printf("Warning: Unsupported filter format (expected 3 elements): %v", req.Filter)
		}
		// TODO: Note that complex/nested filters are a stretch goal.
	}

	// Handle IncludeAttributes - Log but ignore for V1
	if req.IncludeAttributes != nil && len(req.IncludeAttributes) > 0 {
		log.Printf("Info: include_attributes is present but not yet supported: %v", req.IncludeAttributes)
		// turbopg will return all attributes by default if queryOpts.IncludeAttributes is nil
	}


	results, err := globalStore.Query(r.Context(), queryOpts)
	if err != nil {
		// TODO: Differentiate "namespace not found" error from turbopg
		// e.g., if errors.Is(err, turbopg.ErrNamespaceNotFound)
		log.Printf("Error during Query for namespace '%s': %v", namespaceName, err)
		if strings.Contains(err.Error(), "not found") { // Basic check, improve with specific error types
			respondWithError(w, http.StatusNotFound, fmt.Sprintf("Namespace '%s' not found", namespaceName))
		} else {
			respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query namespace '%s': %v", namespaceName, err))
		}
		return
	}

	duration := time.Since(startTime)
	w.Header().Set("Server-Timing", fmt.Sprintf("processing_time;dur=%d", duration.Milliseconds()))

	apiResults := make([]APIQueryResult, len(results))
	for i, res := range results {
		apiResults[i] = APIQueryResult{
			ID:         res.Document.ID,
			Attributes: res.Document.Attributes,
			Score:      res.Score,
		}
		// Include vector in response if it was part of the query request
		if len(req.Vector) > 0 && res.Document.Vector != nil {
			apiResults[i].Vector = res.Document.Vector
		}
	}

	respondWithJSON(w, http.StatusOK, apiResults)
}

func handleDebugOperation(w http.ResponseWriter, r *http.Request, namespaceName string, operation string) {
	w.Header().Set("Content-Type", "application/json")
	log.Printf("Received debug operation: %s for namespace: %s", operation, namespaceName)

	// For turbopg-api, these are currently no-op.
	// turbopg itself manages its data and caching internally if any.
	// The original Turbopuffer API might have explicit cache control.
	// This handler ensures endpoint compatibility with benchmarks that might call it.

	if globalStore == nil {
		// This check is more for robustness, globalStore should be initialized.
		respondWithError(w, http.StatusInternalServerError, "Store not initialized")
		return
	}
	
	// Check if namespace exists, just to mimic some level of validation, though it's a no-op.
	// This part is optional for a strict no-op, but makes it slightly more realistic.
	_, err := globalStore.GetNamespace(r.Context(), namespaceName)
	if err != nil {
		// TODO: Differentiate "not found" errors from other errors.
		log.Printf("Error checking namespace '%s' for debug operation '%s': %v", namespaceName, operation, err)
		if strings.Contains(err.Error(), "not found") { // Basic check
			respondWithError(w, http.StatusNotFound, fmt.Sprintf("Namespace '%s' not found", namespaceName))
		} else {
			respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Error checking namespace '%s': %v", namespaceName, err))
		}
		return
	}

	response := map[string]string{
		"status":  "ok",
		"message": fmt.Sprintf("Debug operation '%s' is a no-op for this turbopg-api implementation.", operation),
	}
	respondWithJSON(w, http.StatusOK, response)
}
