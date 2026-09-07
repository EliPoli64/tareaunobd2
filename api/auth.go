package main

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Configuración del proveedor, leída una vez al arrancar.
// KC_ISSUER_URL usa el host externo (localhost): es el `iss` que traen los tokens.
// KC_JWKS_URL usa el nombre de servicio interno (keycloak): de ahí se leen las llaves.
var (
	kcIssuer  = os.Getenv("KC_ISSUER_URL")
	kcJWKSURL = os.Getenv("KC_JWKS_URL")
)

var jwksCache = struct {
	sync.RWMutex
	keys       map[string]*rsa.PublicKey
	lastFetch  time.Time
	httpClient *http.Client
}{
	keys:       make(map[string]*rsa.PublicKey),
	httpClient: &http.Client{Timeout: 5 * time.Second},
}

const (
	jwksRefreshInterval = 10 * time.Minute
	tokenLeeway         = 60 * time.Second
)

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

// fetchJWKS descarga las llaves públicas y reemplaza la caché.
func fetchJWKS() error {
	if kcJWKSURL == "" {
		return fmt.Errorf("KC_JWKS_URL no está configurado")
	}
	resp, err := jwksCache.httpClient.Get(kcJWKSURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS respondió %d", resp.StatusCode)
	}
	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := new(big.Int).SetBytes(eBytes).Int64()
		if e <= 0 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(e),
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("JWKS no trajo llaves RSA utilizables")
	}
	jwksCache.Lock()
	jwksCache.keys = keys
	jwksCache.lastFetch = time.Now()
	jwksCache.Unlock()
	return nil
}

// jwksKey devuelve la llave para un kid, refrescando si expira o si el kid es desconocido.
func jwksKey(kid string) (*rsa.PublicKey, error) {
	jwksCache.RLock()
	key, ok := jwksCache.keys[kid]
	fresh := time.Since(jwksCache.lastFetch) < jwksRefreshInterval
	jwksCache.RUnlock()
	if ok && fresh {
		return key, nil
	}
	if err := fetchJWKS(); err != nil {
		// Si el refresh falla pero tenemos la llave vieja, úsala.
		if ok {
			log.Printf("auth: no se pudo refrescar JWKS (%v), usando caché", err)
			return key, nil
		}
		return nil, err
	}
	jwksCache.RLock()
	defer jwksCache.RUnlock()
	key, ok = jwksCache.keys[kid]
	if !ok {
		return nil, fmt.Errorf("kid desconocido: %s", kid)
	}
	return key, nil
}

// verifyToken valida firma RS256, expiración, emisor y devuelve los claims.
func verifyToken(tokenString string) (jwt.MapClaims, error) {
	if kcIssuer == "" {
		return nil, fmt.Errorf("KC_ISSUER_URL no está configurado")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims,
		func(t *jwt.Token) (any, error) {
			// Fijar el algoritmo bloquea ataques de confusión (p. ej. `none`).
			if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, fmt.Errorf("algoritmo inesperado: %s", t.Header["alg"])
			}
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, fmt.Errorf("token sin kid")
			}
			return jwksKey(kid)
		},
		jwt.WithLeeway(tokenLeeway),
		jwt.WithIssuer(kcIssuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("token inválido")
	}
	return claims, nil
}

// hasRole revisa realm_access.roles del JWT.
func hasRole(claims jwt.MapClaims, role string) bool {
	access, ok := claims["realm_access"].(map[string]any)
	if !ok {
		return false
	}
	roles, ok := access["roles"].([]any)
	if !ok {
		return false
	}
	for _, r := range roles {
		if s, ok := r.(string); ok && s == role {
			return true
		}
	}
	return false
}

// authMiddleware exige un JWT válido de Keycloak con el rol indicado.
// Sin token o token inválido -> 401. Token válido sin el rol -> 403.
func authMiddleware(requiredRole string) gin.HandlerFunc {
	if kcIssuer == "" || kcJWKSURL == "" {
		log.Printf("auth: falta KC_ISSUER_URL o KC_JWKS_URL en el entorno")
	}
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		parts := strings.SplitN(h, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "se requiere token Bearer"})
			return
		}
		claims, err := verifyToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token inválido o expirado"})
			return
		}
		if !hasRole(claims, requiredRole) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "sin el rol requerido"})
			return
		}
		c.Next()
	}
}
