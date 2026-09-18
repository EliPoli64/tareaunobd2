//go:build integration

package main

// Integration tests: full request cycle against the live Compose stack.
// Run: go test -tags integration ./...
// Requires: docker compose up (db + keycloak + api healthy).
// Env overrides: API_BASE, KC_TOKEN_URL.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	intAPI      = envOr("API_BASE", "http://localhost:1412")
	intTokenURL = envOr("KC_TOKEN_URL", "http://localhost:6767/realms/tareauno/protocol/openid-connect/token")
	intClient   = &http.Client{Timeout: 10 * time.Second}
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intToken(t *testing.T, username, password string) string {
	t.Helper()
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {"api"},
		"username":   {username},
		"password":   {password},
	}
	resp, err := intClient.Post(intTokenURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token %s: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token %s: status %d: %s", username, resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("token %s: respuesta sin access_token", username)
	}
	return out.AccessToken
}

func intReq(t *testing.T, method, path, token, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, intAPI+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := intClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func intReservaBody(nombre string) string {
	return fmt.Sprintf(`{"nombre":%q,"fecha":"2026-08-20","cantidadPersonas":2,"estado":"pendiente"}`, nombre)
}

func TestIntegrationFlow(t *testing.T) {
	nombre := fmt.Sprintf("TestInt-%d", time.Now().Unix())

	// Liveness + readiness abiertas.
	if code, _ := intReq(t, http.MethodGet, "/health", "", ""); code != http.StatusOK {
		t.Fatalf("GET /health: got %d, want 200", code)
	}
	if code, _ := intReq(t, http.MethodGet, "/ready", "", ""); code != http.StatusOK {
		t.Fatalf("GET /ready: got %d, want 200", code)
	}

	writer := intToken(t, "writer", "writer123")
	reader := intToken(t, "reader", "reader123")

	// Sin token -> 401.
	if code, _ := intReq(t, http.MethodPost, "/reservas", "", intReservaBody(nombre)); code != http.StatusUnauthorized {
		t.Fatalf("POST sin token: got %d, want 401", code)
	}
	// Reader sin rol -> 403.
	if code, _ := intReq(t, http.MethodPost, "/reservas", reader, intReservaBody(nombre)); code != http.StatusForbidden {
		t.Fatalf("POST reader: got %d, want 403", code)
	}
	// Body inválido con token válido -> 400.
	if code, _ := intReq(t, http.MethodPost, "/reservas", writer, `{"nombre":""}`); code != http.StatusBadRequest {
		t.Fatalf("POST inválido: got %d, want 400", code)
	}
	// Writer con rol -> 201 con id.
	code, body := intReq(t, http.MethodPost, "/reservas", writer, intReservaBody(nombre))
	if code != http.StatusCreated {
		t.Fatalf("POST writer: got %d (%s), want 201", code, body)
	}
	var created struct {
		ReservaID int `json:"reservaId"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ReservaID == 0 {
		t.Fatalf("POST writer: sin reservaId en %s", body)
	}
	idPath := fmt.Sprintf("/reservas/%d", created.ReservaID)

	// Lista con filtro refleja lo escrito.
	code, body = intReq(t, http.MethodGet, "/reservas?fecha=2026-08-20", "", "")
	if code != http.StatusOK || !strings.Contains(string(body), nombre) {
		t.Fatalf("GET filtro: got %d, falta %q en %s", code, nombre, body)
	}
	// Consulta por id.
	if code, _ := intReq(t, http.MethodGet, idPath, "", ""); code != http.StatusOK {
		t.Fatalf("GET %s: got %d, want 200", idPath, code)
	}
	if code, _ := intReq(t, http.MethodGet, "/reservas/999999", "", ""); code != http.StatusNotFound {
		t.Fatalf("GET inexistente: got %d, want 404", code)
	}

	// Actualizar: sin token -> 401, reader -> 403, inválido -> 400, inexistente -> 404.
	updatedBody := fmt.Sprintf(`{"nombre":%q,"fecha":"2026-08-21","cantidadPersonas":4,"estado":"confirmada"}`, nombre+"-upd")
	if code, _ := intReq(t, http.MethodPut, idPath, "", updatedBody); code != http.StatusUnauthorized {
		t.Fatalf("PUT sin token: got %d, want 401", code)
	}
	if code, _ := intReq(t, http.MethodPut, idPath, reader, updatedBody); code != http.StatusForbidden {
		t.Fatalf("PUT reader: got %d, want 403", code)
	}
	if code, _ := intReq(t, http.MethodPut, idPath, writer, `{"nombre":""}`); code != http.StatusBadRequest {
		t.Fatalf("PUT inválido: got %d, want 400", code)
	}
	if code, _ := intReq(t, http.MethodPut, "/reservas/999999", writer, updatedBody); code != http.StatusNotFound {
		t.Fatalf("PUT inexistente: got %d, want 404", code)
	}
	// Writer -> 200 con la versión actualizada.
	code, body = intReq(t, http.MethodPut, idPath, writer, updatedBody)
	if code != http.StatusOK || !strings.Contains(string(body), nombre+"-upd") {
		t.Fatalf("PUT writer: got %d (%s), want 200 con datos actualizados", code, body)
	}
	// La lista con filtro refleja la actualización.
	code, body = intReq(t, http.MethodGet, "/reservas?fecha=2026-08-21", "", "")
	if code != http.StatusOK || !strings.Contains(string(body), nombre+"-upd") {
		t.Fatalf("GET filtro tras PUT: got %d, falta %q en %s", code, nombre+"-upd", body)
	}

	// DELETE reader -> 403, writer -> 204, repetido -> 404.
	if code, _ := intReq(t, http.MethodDelete, idPath, reader, ""); code != http.StatusForbidden {
		t.Fatalf("DELETE reader: got %d, want 403", code)
	}
	if code, _ := intReq(t, http.MethodDelete, idPath, writer, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE writer: got %d, want 204", code)
	}
	if code, _ := intReq(t, http.MethodDelete, idPath, writer, ""); code != http.StatusNotFound {
		t.Fatalf("DELETE repetido: got %d, want 404", code)
	}
}
