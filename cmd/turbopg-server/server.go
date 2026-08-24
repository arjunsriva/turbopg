package main

import (
	"net/http"
	"strings"

	"github.com/arjunsriva/turbopg"
)

// Server is the TurboPuffer-compatible HTTP API. Tests construct one and
// serve s.mux(); production wires Store, Embedder, and APIKey from config.
type Server struct {
	Store        *turbopg.Store
	Embedder     Embedder
	APIKey       string
	Logger       turbopg.Logger
	MaxBodyBytes int64
	DefaultLists int
	metrics      *metrics
}

func (s *Server) mux() http.Handler {
	if s.metrics == nil {
		s.metrics = &metrics{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/namespaces", s.auth(s.handleListNamespaces))
	mux.HandleFunc("/v2/namespaces", s.auth(s.handleListNamespaces))
	mux.HandleFunc("/v1/namespaces/", s.auth(s.handleNamespaceRequest))
	mux.HandleFunc("/v2/namespaces/", s.auth(s.handleNamespaceRequest))
	return s.wrap(mux)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			respondWithError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != s.APIKey {
			respondWithError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireStore(w http.ResponseWriter) bool {
	if s == nil || s.Store == nil {
		respondWithError(w, http.StatusInternalServerError, "Store not initialized")
		return false
	}
	return true
}

func (s *Server) handleNamespaceRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/v1/namespaces/")
	path = strings.TrimPrefix(path, "/v2/namespaces/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) == 0 || pathParts[0] == "" {
		respondWithError(w, http.StatusBadRequest, "namespace name is required")
		return
	}
	namespaceName := pathParts[0]

	switch len(pathParts) {
	case 1:
		switch r.Method {
		case http.MethodPost:
			s.handleWrite(w, r, namespaceName)
		case http.MethodDelete:
			s.handleDeleteNamespace(w, r, namespaceName)
		case http.MethodHead:
			s.handleHeadNamespace(w, r, namespaceName)
		default:
			respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case 2:
		switch pathParts[1] {
		case "query":
			if r.Method == http.MethodPost {
				s.handleQuery(w, r, namespaceName)
				return
			}
			respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		case "metadata":
			switch r.Method {
			case http.MethodGet:
				s.handleMetadata(w, r, namespaceName)
			case http.MethodPatch:
				s.handleUpdateMetadata(w, r, namespaceName)
			default:
				respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case "schema":
			switch r.Method {
			case http.MethodGet:
				s.handleGetSchema(w, r, namespaceName)
			case http.MethodPost:
				s.handleUpdateSchema(w, r, namespaceName)
			default:
				respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case "explain_query":
			if r.Method == http.MethodPost {
				s.handleExplainQuery(w, r, namespaceName)
				return
			}
			respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		case "hint_cache_warm":
			if r.Method == http.MethodGet {
				s.handleHintCacheWarm(w, r, namespaceName)
				return
			}
			respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		default:
			respondWithError(w, http.StatusNotFound, "not found")
		}
	case 3:
		if pathParts[1] == "_debug" {
			switch pathParts[2] {
			case "recall":
				if r.Method == http.MethodPost {
					s.handleRecall(w, r, namespaceName)
					return
				}
				respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			case "purge_cache", "warm_cache":
				if r.Method == http.MethodGet {
					s.handleDebugOperation(w, r, namespaceName, pathParts[2])
					return
				}
				respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
		}
		respondWithError(w, http.StatusNotFound, "not found")
	default:
		respondWithError(w, http.StatusNotFound, "not found")
	}
}
