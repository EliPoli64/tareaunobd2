package main

// Unit tests for routes and input validation.
// The DB handle stays nil: every case here returns before touching it
// (validation happens before any query). DB-backed paths are covered
// by the integration suite instead.

import (
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
