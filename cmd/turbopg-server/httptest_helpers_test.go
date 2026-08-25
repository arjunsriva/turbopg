package main

import (
	"net/http/httptest"
	"testing"

	"github.com/arjunsriva/turbopg"
)

func newTestServer(t *testing.T, prefix string) (*Server, *httptest.Server, func()) {
	t.Helper()
	store, db, _ := turbopg.SetupTestStore(t, prefix, false)
	h := &Server{Store: store, APIKey: "testapikey"}
	srv := httptest.NewServer(h.mux())
	return h, srv, func() {
		srv.Close()
		db.Cleanup(t)
	}
}
