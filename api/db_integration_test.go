//go:build integration

package main

// In-process DB tests: point the package-global handle at the live
// Postgres (reachable at localhost from the host) and drive setupRouter
// via httptest. This covers handler + scan + json statements that the
// black-box compose test cannot reach. Requires docker compose up.

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// envFirst devuelve la primera variable definida, o def.
// Permite a los tests de integración heredar el .env (POSTGRES_*/DB_*)
// sin renunciar a un valor por defecto.
func envFirst(def string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
}

func liveDB(t *testing.T) {
	t.Helper()
	host := envFirst("localhost", "TEST_DB_HOST")
	port := envFirst("5432", "TEST_DB_PORT", "POSTGRES_PORT", "DB_PORT")
	user := envFirst("tareaunobd2", "TEST_DB_USER", "POSTGRES_USER", "DB_USER")
	pass := envFirst("holaHolaComoEstan", "TEST_DB_PASSWORD", "POSTGRES_PASSWORD", "DB_PASSWORD")
	name := envFirst("TareaUnoDB", "TEST_DB_NAME", "POSTGRES_DB", "DB_NAME")
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", user, pass, host, port, name)
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("sin postgres local: %v", err)
	}
	if err := pool.Ping(); err != nil {
		t.Skipf("sin postgres local: %v", err)
	}
	old := db
	db = pool
	t.Cleanup(func() {
		pool.Close()
		db = old
	})
}

func dbRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	liveDB(t)
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	t.Cleanup(srv.Close)
	withTestIssuer(t, srv.URL)
	gin.SetMode(gin.TestMode)
	token := tk.mint(testIssuer, time.Now().Add(time.Hour), true, []string{testRole}, testKid)
	return setupRouter(), token
}

func TestPingDBUpDown(t *testing.T) {
	r, _ := dbRouter(t)
	if w := serve(t, r, http.MethodGet, "/ready", "", ""); w.Code != http.StatusOK {
		t.Fatalf("ready up: got %d, want 200", w.Code)
	}
	// Point at a closed port -> 503.
	dead, _ := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/d")
	old := db
	db = dead
	t.Cleanup(func() { db = old })
	defer dead.Close()
	if w := serve(t, r, http.MethodGet, "/ready", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready down: got %d, want 503", w.Code)
	}
}

func TestDBCrudRoundTrip(t *testing.T) {
	r, token := dbRouter(t)
	nombre := fmt.Sprintf("UnitDB-%d", time.Now().UnixNano())

	// Create via stored procedure.
	w := serve(t, r, http.MethodPost, "/reservas",
		fmt.Sprintf(`{"nombre":%q,"fecha":"2026-08-20","cantidadPersonas":3,"estado":"pendiente"}`, nombre), token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: got %d (%s), want 201", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), nombre) {
		t.Fatalf("POST: respuesta sin nombre: %s", w.Body.String())
	}

	// List with filter contains it.
	w = serve(t, r, http.MethodGet, "/reservas?fecha=2026-08-20", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), nombre) {
		t.Fatalf("GET filtro: got %d, falta fila en %s", w.Code, w.Body.String())
	}
	// List all contains it too.
	w = serve(t, r, http.MethodGet, "/reservas", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), nombre) {
		t.Fatalf("GET todas: got %d, falta fila", w.Code)
	}

	// Extract id from list output (simple scan for the created row's id).
	// Re-query by filter and delete each match is overkill: fetch id via DB.
	var id int
	if err := db.QueryRow(`SELECT reservaId FROM Reserva WHERE nombre = $1 ORDER BY reservaId DESC LIMIT 1`, nombre).Scan(&id); err != nil {
		t.Fatalf("id: %v", err)
	}

	// Get by id.
	w = serve(t, r, http.MethodGet, fmt.Sprintf("/reservas/%d", id), "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), nombre) {
		t.Fatalf("GET id: got %d (%s), want 200", w.Code, w.Body.String())
	}
	// Get missing.
	if w := serve(t, r, http.MethodGet, "/reservas/999999", "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("GET inexistente: got %d, want 404", w.Code)
	}
	// Delete + delete again.
	if w := serve(t, r, http.MethodDelete, fmt.Sprintf("/reservas/%d", id), "", token); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: got %d, want 204", w.Code)
	}
	if w := serve(t, r, http.MethodDelete, fmt.Sprintf("/reservas/%d", id), "", token); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE repetido: got %d, want 404", w.Code)
	}
}

func TestDBUpdateRoundTrip(t *testing.T) {
	r, token := dbRouter(t)
	nombre := fmt.Sprintf("UnitDBUpd-%d", time.Now().UnixNano())

	// Create via stored procedure.
	w := serve(t, r, http.MethodPost, "/reservas",
		fmt.Sprintf(`{"nombre":%q,"fecha":"2026-08-20","cantidadPersonas":3,"estado":"pendiente"}`, nombre), token)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: got %d (%s), want 201", w.Code, w.Body.String())
	}
	var id int
	if err := db.QueryRow(`SELECT reservaId FROM Reserva WHERE nombre = $1 ORDER BY reservaId DESC LIMIT 1`, nombre).Scan(&id); err != nil {
		t.Fatalf("id: %v", err)
	}
	idPath := fmt.Sprintf("/reservas/%d", id)

	// PUT inválido -> 400 (no toca la fila).
	if w := serve(t, r, http.MethodPut, idPath, `{"nombre":""}`, token); w.Code != http.StatusBadRequest {
		t.Fatalf("PUT inválido: got %d, want 400", w.Code)
	}
	// PUT inexistente -> 404.
	if w := serve(t, r, http.MethodPut, "/reservas/999999",
		`{"nombre":"X","fecha":"2026-08-20","cantidadPersonas":1,"estado":"pendiente"}`, token); w.Code != http.StatusNotFound {
		t.Fatalf("PUT inexistente: got %d, want 404", w.Code)
	}
	// PUT válido -> 200 con la versión actualizada.
	upd := fmt.Sprintf(`{"nombre":%q,"fecha":"2026-08-21","cantidadPersonas":4,"estado":"confirmada"}`, nombre+"-upd")
	w = serve(t, r, http.MethodPut, idPath, upd, token)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: got %d (%s), want 200", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), nombre+"-upd") || !strings.Contains(w.Body.String(), "2026-08-21") || !strings.Contains(w.Body.String(), "confirmada") {
		t.Fatalf("PUT: respuesta sin datos actualizados: %s", w.Body.String())
	}
	// GET refleja la actualización.
	w = serve(t, r, http.MethodGet, idPath, "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), nombre+"-upd") {
		t.Fatalf("GET tras PUT: got %d (%s), want 200 con la versión actualizada", w.Code, w.Body.String())
	}
	// El filtro por la nueva fecha la encuentra.
	w = serve(t, r, http.MethodGet, "/reservas?fecha=2026-08-21", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), nombre+"-upd") {
		t.Fatalf("GET filtro tras PUT: got %d, falta fila en %s", w.Code, w.Body.String())
	}
	// Limpieza.
	if w := serve(t, r, http.MethodDelete, idPath, "", token); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: got %d, want 204", w.Code)
	}
}

// TestEntryPointDefaultPortConflict cubre el camino de éxito de main():
// con la base alcanzable y el puerto por defecto (1412) ya ocupado por la
// pila, router.Run falla al instante y main() retorna en vez de quedarse
// colgado.
func TestEntryPointDefaultPortConflict(t *testing.T) {
	liveDB(t)
	setEnv(t, map[string]string{
		"DB_HOST":     "localhost",
		"DB_PORT":     envFirst("5432", "POSTGRES_PORT", "DB_PORT"),
		"DB_USER":     envFirst("tareaunobd2", "POSTGRES_USER", "DB_USER"),
		"DB_PASSWORD": envFirst("holaHolaComoEstan", "POSTGRES_PASSWORD", "DB_PASSWORD"),
		"DB_NAME":     envFirst("TareaUnoDB", "POSTGRES_DB", "DB_NAME"),
		"PORT":        "",
	})
	// Si el puerto 1412 está libre, main() se quedaría escuchando: se salta.
	probe, err := net.Listen("tcp", ":1412")
	if err == nil {
		probe.Close()
		t.Skip("puerto 1412 libre: main() se quedaría escuchando")
	}
	oldRetries, oldDelay := dbMaxRetries, dbRetryDelay
	dbMaxRetries, dbRetryDelay = 3, time.Millisecond
	t.Cleanup(func() { dbMaxRetries, dbRetryDelay = oldRetries, oldDelay })
	oldDB := db
	t.Cleanup(func() { db = oldDB })
	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("main() no retornó con el puerto 1412 ocupado")
	}
}

func TestInitDBSuccess(t *testing.T) {
	setEnv(t, map[string]string{
		"DB_HOST": "localhost", "DB_PORT": "5432",
		"DB_USER": "tareaunobd2", "DB_PASSWORD": "holaHolaComoEstan", "DB_NAME": "TareaUnoDB",
	})
	oldRetries, oldDelay := dbMaxRetries, dbRetryDelay
	dbMaxRetries, dbRetryDelay = 2, time.Millisecond
	t.Cleanup(func() { dbMaxRetries, dbRetryDelay = oldRetries, oldDelay })
	old := db
	t.Cleanup(func() { db = old })
	if err := initDB(); err != nil {
		t.Skipf("sin postgres local: %v", err)
	}
	db.Close()
}

func TestDBDeadHandle500s(t *testing.T) {
	r, token := dbRouter(t)
	dead, _ := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/d")
	old := db
	db = dead
	t.Cleanup(func() { db = old })
	defer dead.Close()
	if w := serve(t, r, http.MethodGet, "/reservas", "", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("list sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodGet, "/reservas/1", "", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("get sin DB: got %d, want 500", w.Code)
	}
	if w := serve(t, r, http.MethodDelete, "/reservas/1", "", token); w.Code != http.StatusInternalServerError {
		t.Errorf("delete sin DB: got %d, want 500", w.Code)
	}
}

func TestDBPostServerError(t *testing.T) {
	r, token := dbRouter(t)
	// Valid input but dead handle -> 500 from the SP call.
	dead, _ := sql.Open("pgx", "postgres://u:p@127.0.0.1:1/d")
	old := db
	db = dead
	t.Cleanup(func() { db = old })
	defer dead.Close()
	w := serve(t, r, http.MethodPost, "/reservas",
		`{"nombre":"X","fecha":"2026-08-20","cantidadPersonas":1,"estado":"pendiente"}`, token)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("POST sin DB: got %d, want 500", w.Code)
	}
}
