package main

// Unit tests for DB helpers and JWKS failure paths.
// No live services: bad hosts, local httptest servers, shrunk retries.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestDBURLDefaults(t *testing.T) {
	for _, k := range []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"} {
		t.Setenv(k, "")
	}
	// os.Unsetenv needed since Setenv("") leaves empty (falsy -> default, same path).
	got := dbURL()
	want := "postgres://:@db:5432/"
	if got != want {
		t.Fatalf("defaults: got %q, want %q", got, want)
	}
}

func TestDBURLFromEnv(t *testing.T) {
	setEnv(t, map[string]string{
		"DB_HOST": "mihost", "DB_PORT": "5433",
		"DB_USER": "u", "DB_PASSWORD": "p", "DB_NAME": "d",
	})
	if got := dbURL(); got != "postgres://u:p@mihost:5433/d" {
		t.Fatalf("got %q", got)
	}
}

func TestDBURLPostgresFallback(t *testing.T) {
	setEnv(t, map[string]string{
		"DB_HOST": "", "DB_PORT": "", "DB_USER": "", "DB_PASSWORD": "", "DB_NAME": "",
		"POSTGRES_USER": "pu", "POSTGRES_PASSWORD": "pp", "POSTGRES_DB": "pd",
	})
	// Empty DB_* fall back to POSTGRES_*; empty host/port use defaults.
	if got, want := dbURL(), "postgres://pu:pp@db:5432/pd"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInitDBFastFail(t *testing.T) {
	oldRetries, oldDelay := dbMaxRetries, dbRetryDelay
	dbMaxRetries, dbRetryDelay = 2, time.Millisecond
	t.Cleanup(func() { dbMaxRetries, dbRetryDelay = oldRetries, oldDelay })
	setEnv(t, map[string]string{
		"DB_HOST": "127.0.0.1", "DB_PORT": "1", // closed port: refuse fast
		"DB_USER": "u", "DB_PASSWORD": "p", "DB_NAME": "d",
		"POSTGRES_USER": "", "POSTGRES_PASSWORD": "", "POSTGRES_DB": "",
	})
	if err := initDB(); err == nil {
		t.Fatal("expected error connecting to closed port")
		if db != nil {
			db.Close()
		}
	} else if db != nil {
		db.Close()
		db = nil
	}
}

func TestFetchJWKSErrors(t *testing.T) {
	oldURL := kcJWKSURL
	t.Cleanup(func() { kcJWKSURL = oldURL })

	kcJWKSURL = ""
	if err := fetchJWKS(); err == nil {
		t.Error("empty URL: want error")
	}
	kcJWKSURL = "http://127.0.0.1:1/certs"
	if err := fetchJWKS(); err == nil {
		t.Error("unreachable: want error")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	kcJWKSURL = bad.URL
	if err := fetchJWKS(); err == nil {
		t.Error("500: want error")
	}
	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no json"))
	}))
	defer notJSON.Close()
	kcJWKSURL = notJSON.URL
	if err := fetchJWKS(); err == nil {
		t.Error("bad json: want error")
	}
	noRSA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[{"kty":"EC","kid":"x","crv":"P-256","x":"a","y":"b"}]}`))
	}))
	defer noRSA.Close()
	kcJWKSURL = noRSA.URL
	if err := fetchJWKS(); err == nil {
		t.Error("no RSA keys: want error")
	}
}

func TestJwksKeyStaleFallback(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	withTestIssuer(t, srv.URL)
	// Prime the cache, then kill the server: refresh fails, cached key serves.
	if _, err := jwksKey(testKid); err != nil {
		t.Fatalf("prime: %v", err)
	}
	srv.Close()
	kcJWKSURL = srv.URL // now unreachable
	if _, err := jwksKey(testKid); err != nil {
		t.Fatalf("cached fallback: %v", err)
	}
	if _, err := jwksKey("otro-kid"); err == nil {
		t.Fatal("unknown kid with dead server: want error")
	}
}

func TestVerifyTokenNoIssuer(t *testing.T) {
	oldIssuer := kcIssuer
	kcIssuer = ""
	t.Cleanup(func() { kcIssuer = oldIssuer })
	if _, err := verifyToken("x.y.z"); err == nil {
		t.Fatal("empty issuer: want error")
	}
}

func TestMiddlewareNoEnv401(t *testing.T) {
	oldIssuer, oldJWKS := kcIssuer, kcJWKSURL
	kcIssuer, kcJWKSURL = "", ""
	t.Cleanup(func() { kcIssuer, kcJWKSURL = oldIssuer, oldJWKS })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer abc.def.ghi")
	w := httptest.NewRecorder()
	probeRouter("cualquiera").ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
}
