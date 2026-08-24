package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arjunsriva/turbopg"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger := &turbopg.StdLogger{MinLevel: cfg.LogLevel}
	dsn := withStatementTimeout(cfg.DatabaseURL, cfg.StatementTimeout)

	log.Println("Connecting to database...")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	db.SetMaxOpenConns(cfg.DBMaxOpen)
	db.SetMaxIdleConns(cfg.DBMaxIdle)
	db.SetConnMaxLifetime(cfg.DBConnLifetime)

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
		Prefix:            cfg.StorePrefix,
		DBURL:             dsn,
		Logger:            logger,
		IVFFlatProbes:     cfg.IVFFlatProbes,
		DefaultLists:      cfg.IVFFlatLists,
		PatchByFilterMax:  cfg.PatchByFilterMax,
		DeleteByFilterMax: cfg.DeleteByFilterMax,
	})
	if err != nil {
		log.Fatalf("Failed to create turbopg store: %v", err)
	}
	log.Println("turbopg store created successfully.")

	embedder := embedderFromConfig(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey)
	server := &Server{
		Store:        store,
		Embedder:     embedder,
		APIKey:       cfg.effectiveAPIKey(),
		Logger:       logger,
		MaxBodyBytes: cfg.MaxBodyBytes,
		DefaultLists: cfg.IVFFlatLists,
	}

	emb := "off"
	if embedder != nil {
		emb = "on"
	}
	log.Printf("turbopg-server version=%s commit=%s listen=%s prefix=%s pool=%d/%d lists=%d probes=%d embeddings=%s tls=%v",
		Version, Commit, cfg.Listen, cfg.StorePrefix, cfg.DBMaxOpen, cfg.DBMaxIdle,
		cfg.IVFFlatLists, serverProbes(cfg), emb, cfg.TLSCertFile != "")

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.mux(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	if cfg.PprofListen != "" {
		go func() {
			log.Printf("pprof listening on %s", cfg.PprofListen)
			ps := &http.Server{Addr: cfg.PprofListen, Handler: nil, ReadHeaderTimeout: 5 * time.Second}
			if err := ps.ListenAndServe(); err != nil {
				log.Printf("pprof server: %v", err)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSCertFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		log.Printf("close db: %v", err)
	}
}

func serverProbes(cfg ServerConfig) int {
	if cfg.IVFFlatProbes > 0 {
		return cfg.IVFFlatProbes
	}
	return turbopg.DefaultIVFFlatProbes
}
