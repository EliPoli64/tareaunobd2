package main

// Unit tests for the DB-error branches of the handlers.
// No Docker and no live Postgres: the package handle is pointed at an
// unreachable address, so every query fails fast and the handler must
// answer 500 (503 for /ready) instead of crashing.

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// withDeadDB apunta el handle global a una dirección inalcanzable.
func withDeadDB(t *testing.T) {
	t.Helper()
	dead, err := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/d")
	if err != nil {
		t.Fatal(err)
	}
	old := db
	db = dead
	t.Cleanup(func() {
		dead.Close()
		db = old
	})
}

func TestHandlersDBError500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, token := testRouter(t)
	withDeadDB(t)
	body := `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`

	if w := serve(t, r, http.MethodPost, "/reservas", body, token); w.Code != http.StatusInternalServerError {
		t.Errorf("POST sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodGet, "/reservas", "", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("GET lista sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodGet, "/reservas?fecha=2026-08-20&estado=pendiente&nombre=Ana&cantidadPersonas=2", "", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("GET con filtros sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodGet, "/reservas/1", "", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("GET id sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodPut, "/reservas/1", body, token); w.Code != http.StatusInternalServerError {
		t.Errorf("PUT sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodDelete, "/reservas/1", "", token); w.Code != http.StatusInternalServerError {
		t.Errorf("DELETE sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodGet, "/ready", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("ready sin DB: got %d, want 503", w.Code)
	}
}
