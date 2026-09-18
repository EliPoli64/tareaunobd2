package main

// Unit tests for routes and input validation.
// The DB handle stays nil: every case here returns before touching it
// (validation happens before any query). DB-backed paths are covered
// by the integration suite instead.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	t.Cleanup(srv.Close)
	withTestIssuer(t, srv.URL)
	gin.SetMode(gin.TestMode)
	token := tk.mint(testIssuer, time.Now().Add(time.Hour), true, []string{testRole}, testKid)
	return setupRouter(), token
}

// testRouterNoRole returns the router plus a valid token WITHOUT the required role.
func testRouterNoRole(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	t.Cleanup(srv.Close)
	withTestIssuer(t, srv.URL)
	gin.SetMode(gin.TestMode)
	token := tk.mint(testIssuer, time.Now().Add(time.Hour), true, []string{"otro-rol"}, testKid)
	return setupRouter(), token
}

func serve(t *testing.T, r http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHealthOpen(t *testing.T) {
	r, _ := testRouter(t)
	if w := serve(t, r, http.MethodGet, "/health", "", ""); w.Code != http.StatusOK {
		t.Fatalf("GET /health: got %d, want 200", w.Code)
	}
}

func TestPostValidation400(t *testing.T) {
	r, token := testRouter(t)
	bad := []struct {
		name string
		body string
	}{
		{"vacio", `{}`},
		{"sin nombre", `{"fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`},
		{"cantidad cero", `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":0,"estado":"pendiente"}`},
		{"cantidad negativa", `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":-1,"estado":"pendiente"}`},
		{"fecha mala", `{"nombre":"Ana","fecha":"20-08-2026","cantidadPersonas":2,"estado":"pendiente"}`},
		{"no json", `esto no es json`},
	}
	for _, tc := range bad {
		if w := serve(t, r, http.MethodPost, "/reservas", tc.body, token); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (body=%s)", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestPostSinToken401(t *testing.T) {
	r, _ := testRouter(t)
	body := `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`
	if w := serve(t, r, http.MethodPost, "/reservas", body, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sin token: got %d, want 401", w.Code)
	}
}

func TestFiltroFechaMala400(t *testing.T) {
	r, _ := testRouter(t)
	if w := serve(t, r, http.MethodGet, "/reservas?fecha=no-fecha", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("filtro malo: got %d, want 400", w.Code)
	}
}

func TestFiltroCantidadMala400(t *testing.T) {
	r, _ := testRouter(t)
	if w := serve(t, r, http.MethodGet, "/reservas?cantidadPersonas=muchas", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("cantidad mala: got %d, want 400", w.Code)
	}
}

func TestIDNoEntero400(t *testing.T) {
	r, token := testRouter(t)
	if w := serve(t, r, http.MethodGet, "/reservas/abc", "", ""); w.Code != http.StatusBadRequest {
		t.Errorf("GET id texto: got %d, want 400", w.Code)
	}
	if w := serve(t, r, http.MethodDelete, "/reservas/abc", "", token); w.Code != http.StatusBadRequest {
		t.Errorf("DELETE id texto: got %d, want 400", w.Code)
	}
	if w := serve(t, r, http.MethodPut, "/reservas/abc", `{"nombre":"A","fecha":"2026-08-20","cantidadPersonas":1,"estado":"pendiente"}`, token); w.Code != http.StatusBadRequest {
		t.Errorf("PUT id texto: got %d, want 400", w.Code)
	}
}

func TestPutValidation400(t *testing.T) {
	r, token := testRouter(t)
	bad := []struct {
		name string
		body string
	}{
		{"vacio", `{}`},
		{"sin nombre", `{"fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`},
		{"cantidad cero", `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":0,"estado":"pendiente"}`},
		{"cantidad negativa", `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":-1,"estado":"pendiente"}`},
		{"fecha mala", `{"nombre":"Ana","fecha":"20-08-2026","cantidadPersonas":2,"estado":"pendiente"}`},
		{"no json", `esto no es json`},
	}
	for _, tc := range bad {
		if w := serve(t, r, http.MethodPut, "/reservas/1", tc.body, token); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (body=%s)", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestPutSinToken401(t *testing.T) {
	r, _ := testRouter(t)
	body := `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`
	if w := serve(t, r, http.MethodPut, "/reservas/1", body, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sin token: got %d, want 401", w.Code)
	}
}

func TestPutSinRol403(t *testing.T) {
	r, noRole := testRouterNoRole(t)
	body := `{"nombre":"Ana","fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`
	if w := serve(t, r, http.MethodPut, "/reservas/1", body, noRole); w.Code != http.StatusForbidden {
		t.Fatalf("sin rol: got %d, want 403", w.Code)
	}
}

func TestValidarReserva(t *testing.T) {
	cases := []struct {
		name string
		in   reservaIn
		want bool
	}{
		{"valida", reservaIn{Nombre: "Ana", Fecha: "2026-08-20", CantidadPersonas: 2, Estado: "pendiente"}, true},
		{"sin nombre", reservaIn{Fecha: "2026-08-20", CantidadPersonas: 2, Estado: "pendiente"}, false},
		{"sin fecha", reservaIn{Nombre: "Ana", CantidadPersonas: 2, Estado: "pendiente"}, false},
		{"sin estado", reservaIn{Nombre: "Ana", Fecha: "2026-08-20", CantidadPersonas: 2}, false},
		{"cantidad cero", reservaIn{Nombre: "Ana", Fecha: "2026-08-20", CantidadPersonas: 0, Estado: "pendiente"}, false},
		{"cantidad negativa", reservaIn{Nombre: "Ana", Fecha: "2026-08-20", CantidadPersonas: -1, Estado: "pendiente"}, false},
		{"fecha mala", reservaIn{Nombre: "Ana", Fecha: "20-08-2026", CantidadPersonas: 2, Estado: "pendiente"}, false},
	}
	for _, tc := range cases {
		if got := validarReservaIn(tc.in) == ""; got != tc.want {
			t.Errorf("%s: got valid=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// fakeScanRow es un scanner mínimamente funcional para probar scanReserva
// sin depender de una conexión real.
type fakeScanRow struct {
	vals []any
	err  error
}

func (f fakeScanRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	for i, d := range dest {
		if i >= len(f.vals) {
			break
		}
		switch v := d.(type) {
		case *int:
			if n, ok := f.vals[i].(int); ok {
				*v = n
			}
		case *string:
			if s, ok := f.vals[i].(string); ok {
				*v = s
			}
		case *time.Time:
			if t, ok := f.vals[i].(time.Time); ok {
				*v = t
			}
		}
	}
	return nil
}

func TestScanReserva(t *testing.T) {
	fecha := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	row := fakeScanRow{vals: []any{5, "Ana", fecha, 3, "pendiente"}}
	r, err := scanReserva(row)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if r.ReservaID != 5 || r.Nombre != "Ana" || r.CantidadPersonas != 3 || r.Estado != "pendiente" || !r.Fecha.Equal(fecha) {
		t.Fatalf("scan result: %+v", r)
	}
	if _, err := scanReserva(fakeScanRow{err: fmt.Errorf("boom")}); err == nil {
		t.Fatal("scan con error: want error")
	}
}

func TestReservaJSON(t *testing.T) {
	r := reserva{
		ReservaID:        7,
		Nombre:           "Ana",
		Fecha:            time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		CantidadPersonas: 2,
		Estado:           "pendiente",
	}
	got := r.json()
	if got["reservaId"] != 7 {
		t.Errorf("reservaId: got %v", got["reservaId"])
	}
	if got["fecha"] != "2026-08-20" {
		t.Errorf("fecha: got %v, want 2026-08-20", got["fecha"])
	}
	if got["nombre"] != "Ana" || got["cantidadPersonas"] != 2 || got["estado"] != "pendiente" {
		t.Errorf("json completo: %v", got)
	}
}

func TestHomeOpen(t *testing.T) {
	r, _ := testRouter(t)
	if w := serve(t, r, http.MethodGet, "/", "", ""); w.Code != http.StatusOK {
		t.Fatalf("GET /: got %d, want 200", w.Code)
	}
}

// fakeReservaRows es un doble de la iteración de *sql.Rows.
type fakeReservaRows struct {
	next    bool
	scanErr error
	rowsErr error
}

func (f fakeReservaRows) Next() bool             { return f.next }
func (f fakeReservaRows) Scan(dest ...any) error { return f.scanErr }
func (f fakeReservaRows) Err() error             { return f.rowsErr }
func (f fakeReservaRows) Close() error           { return nil }

func TestReservasFromRowsScanError(t *testing.T) {
	rows := fakeReservaRows{next: true, scanErr: fmt.Errorf("scan roto")}
	if _, err := reservasFromRows(rows); err == nil {
		t.Fatal("scan fallido en la iteración: want error")
	}
}

func TestReservasFromRowsRowsErr(t *testing.T) {
	rows := fakeReservaRows{rowsErr: fmt.Errorf("rows roto")}
	if _, err := reservasFromRows(rows); err == nil {
		t.Fatal("rows.Err != nil: want error")
	}
}
