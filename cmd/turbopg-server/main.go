package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/arjunsriva/turbopg"
	_ "github.com/lib/pq"
)

var (
	globalStore *turbopg.Store
	apiKey      string
)

func main() {
	cfg := loadConfig()
	apiKey = cfg.APIKey

	log.Println("Connecting to database...")
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Pinging database...")
	if err := db.PingContext(context.Background()); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Database ping successful.")

	log.Println("Initializing turbopg...")
	if err := turbopg.Initialize(context.Background(), db); err != nil {
		log.Fatalf("Failed to initialize turbopg: %v", err)
	}
	log.Println("turbopg initialized successfully.")

	log.Println("Creating turbopg store...")
	store, err := turbopg.New(db, turbopg.Config{
		Prefix: cfg.StorePrefix,
		DBURL:  cfg.DatabaseURL,
		Logger: &turbopg.NoOpLogger{},
	})
	if err != nil {
		log.Fatalf("Failed to create turbopg store: %v", err)
	}
	globalStore = store
	log.Println("turbopg store created successfully.")

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/namespaces/", authMiddleware(handleNamespaceRequest))

	log.Printf("Starting server on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != apiKey {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleNamespaceRequest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/namespaces/")
	pathParts := strings.Split(path, "/")

	// Basic validation for namespace name
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, `{"error": "namespace name is required"}`, http.StatusBadRequest)
		return
	}
	namespaceName := pathParts[0]

	// Routing logic
	switch len(pathParts) {
	case 1: // /v1/namespaces/{namespace_name}
		switch r.Method {
		case http.MethodPost: // Upsert into namespace (implicitly creates if not exists)
			handleUpsert(w, r, namespaceName)
		case http.MethodDelete: // Clear namespace
			handleClearNamespace(w, r, namespaceName)
		case http.MethodHead: // Get namespace stats/info
			handleHeadNamespace(w, r, namespaceName)
		default:
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		}
	case 2: // /v1/namespaces/{namespace_name}/query
		if pathParts[1] == "query" {
			if r.Method == http.MethodPost {
				handleQuery(w, r, namespaceName)
			} else {
				http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
		}
	case 3: // /v1/namespaces/{namespace_name}/_debug/{operation}
		if pathParts[1] == "_debug" {
			operation := pathParts[2]
			if r.Method == http.MethodGet { // Debug operations are usually GET
				handleDebugOperation(w, r, namespaceName, operation)
			} else {
				http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
		}
	default: // Path doesn't match known patterns
		http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
	}
}
