package main

// Unit tests for the Keycloak auth middleware.
// No Docker, no Keycloak, no DB: tokens are minted with a throwaway RSA key
// and the JWKS endpoint is a local httptest server.

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKid    = "test-key"
	testIssuer = "http://test/realms/tareauno"
	testRole   = "reserva-writer"
)

type testKeys struct {
	priv *rsa.PrivateKey
}

// newTestKeys generates a throwaway RSA key for signing test tokens.
func newTestKeys(t *testing.T) *testKeys {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &testKeys{priv: priv}
}

func b64urlBig(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// jwksServer serves the public half of tk as a JWKS document.
func (tk *testKeys) jwksServer() *httptest.Server {
	n := b64urlBig(tk.priv.N.Bytes())
	e := b64urlBig(big.NewInt(int64(tk.priv.E)).Bytes())
	body := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":%q,"n":%q,"e":%q}]}`, testKid, n, e)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

// withTestIssuer points the middleware at the fake JWKS + issuer, restoring after.
func withTestIssuer(t *testing.T, jwksURL string) {
	t.Helper()
	oldIssuer, oldJWKS := kcIssuer, kcJWKSURL
	kcIssuer, kcJWKSURL = testIssuer, jwksURL
	jwksCache.Lock()
	jwksCache.keys = make(map[string]*rsa.PublicKey)
	jwksCache.lastFetch = time.Time{}
	jwksCache.Unlock()
	t.Cleanup(func() {
		kcIssuer, kcJWKSURL = oldIssuer, oldJWKS
		jwksCache.Lock()
		jwksCache.keys = make(map[string]*rsa.PublicKey)
		jwksCache.lastFetch = time.Time{}
		jwksCache.Unlock()
	})
}

// mint creates a signed test token. roles=nil omits the claim; expZero omits exp.
func (tk *testKeys) mint(issuer string, exp time.Time, withExp bool, roles []string, kid string) string {
	claims := jwt.MapClaims{"iss": issuer, "sub": "test-user"}
	if withExp {
		claims["exp"] = exp.Unix()
	}
	if roles != nil {
		rs := make([]any, len(roles))
		for i, r := range roles {
			rs[i] = r
		}
		claims["realm_access"] = map[string]any{"roles": rs}
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(tk.priv)
	if err != nil {
		panic(err)
	}
	return s
}

// probeRouter runs requests through ONLY the middleware + a 200 stub.
func probeRouter(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", authMiddleware(role), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doGet(t *testing.T, r http.Handler, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAuthNoHeader401(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	if got := doGet(t, probeRouter(testRole), ""); got != http.StatusUnauthorized {
		t.Fatalf("sin header: got %d, want 401", got)
	}
}

func TestAuthGarbage401(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	for _, bad := range []string{"basura", "a.b.c", "Bearer"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if bad != "Bearer" {
			req.Header.Set("Authorization", "Bearer "+bad)
		} else {
			req.Header.Set("Authorization", bad) // scheme without token
		}
		w := httptest.NewRecorder()
		probeRouter(testRole).ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: got %d, want 401", bad, w.Code)
		}
	}
}

func TestAuthExpired401(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	tok := tk.mint(testIssuer, time.Now().Add(-time.Hour), true, []string{testRole}, testKid)
	if got := doGet(t, probeRouter(testRole), tok); got != http.StatusUnauthorized {
		t.Fatalf("expirado: got %d, want 401", got)
	}
}

func TestAuthWrongIssuer401(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	tok := tk.mint("http://evil/realms/x", time.Now().Add(time.Hour), true, []string{testRole}, testKid)
	if got := doGet(t, probeRouter(testRole), tok); got != http.StatusUnauthorized {
		t.Fatalf("issuer ajeno: got %d, want 401", got)
	}
}

func TestAuthNoRole403(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	// Valid signature, no realm_access at all.
	tok := tk.mint(testIssuer, time.Now().Add(time.Hour), true, nil, testKid)
	if got := doGet(t, probeRouter(testRole), tok); got != http.StatusForbidden {
		t.Fatalf("sin roles: got %d, want 403", got)
	}
	// Valid signature, other roles only.
	tok = tk.mint(testIssuer, time.Now().Add(time.Hour), true, []string{"otro-rol"}, testKid)
	if got := doGet(t, probeRouter(testRole), tok); got != http.StatusForbidden {
		t.Fatalf("rol distinto: got %d, want 403", got)
	}
}

func TestAuthWithRolePasses(t *testing.T) {
	tk := newTestKeys(t)
	srv := tk.jwksServer()
	defer srv.Close()
	withTestIssuer(t, srv.URL)
	tok := tk.mint(testIssuer, time.Now().Add(time.Hour), true, []string{"otro", testRole}, testKid)
	if got := doGet(t, probeRouter(testRole), tok); got != http.StatusOK {
		t.Fatalf("con rol: got %d, want 200", got)
	}
}

func TestHasRoleTable(t *testing.T) {
	cases := []struct {
		name   string
		claims jwt.MapClaims
		want   bool
	}{
		{"presente", jwt.MapClaims{"realm_access": map[string]any{"roles": []any{"a", testRole}}}, true},
		{"ausente", jwt.MapClaims{"realm_access": map[string]any{"roles": []any{"a"}}}, false},
		{"sin claim", jwt.MapClaims{}, false},
		{"tipo roto", jwt.MapClaims{"realm_access": "no"}, false},
	}
	for _, tc := range cases {
		if got := hasRole(tc.claims, testRole); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
