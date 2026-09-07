package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var db *sql.DB

// Retry knobs as vars so tests can shrink them.
var (
	dbMaxRetries = 15
	dbRetryDelay = 2 * time.Second
)

type reservaIn struct {
	Nombre           string `json:"nombre"`
	Fecha            string `json:"fecha"`
	CantidadPersonas int    `json:"cantidadPersonas"`
	Estado           string `json:"estado"`
}

type reserva struct {
	ReservaID        int       `json:"reservaId"`
	Nombre           string    `json:"nombre"`
	Fecha            time.Time `json:"-"`
	CantidadPersonas int       `json:"cantidadPersonas"`
	Estado           string    `json:"estado"`
}

func (r reserva) json() gin.H {
	return gin.H{
		"reservaId":        r.ReservaID,
		"nombre":           r.Nombre,
		"fecha":            r.Fecha.Format("2006-01-02"),
		"cantidadPersonas": r.CantidadPersonas,
		"estado":           r.Estado,
	}
}

func scanReserva(row interface {
	Scan(dest ...any) error
}) (reserva, error) {
	var r reserva
	err := row.Scan(&r.ReservaID, &r.Nombre, &r.Fecha, &r.CantidadPersonas, &r.Estado)
	return r, err
}

func dbURL() string {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "db"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = os.Getenv("POSTGRES_USER")
	}
	pass := os.Getenv("DB_PASSWORD")
	if pass == "" {
		pass = os.Getenv("POSTGRES_PASSWORD")
	}
	name := os.Getenv("DB_NAME")
	if name == "" {
		name = os.Getenv("POSTGRES_DB")
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s", user, pass, host, port, name)
}

func initDB() error {
	var err error
	db, err = sql.Open("pgx", dbURL())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(time.Minute * 5)
	// Retry: container may start slightly before the migrator finishes.
	for i := 0; i < dbMaxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(dbRetryDelay)
	}
	return err
}

func reservasPOST(c *gin.Context) {
	var in reservaIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cuerpo inválido: se requiere JSON con nombre, fecha, cantidadPersonas y estado"})
		return
	}
	if in.Nombre == "" || in.Fecha == "" || in.Estado == "" || in.CantidadPersonas <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nombre, fecha y estado son obligatorios y cantidadPersonas debe ser > 0"})
		return
	}
	if _, err := time.Parse("2006-01-02", in.Fecha); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fecha debe tener formato YYYY-MM-DD"})
		return
	}
	var id int
	err := db.QueryRowContext(c.Request.Context(),
		`SELECT insertarReserva($1, $2::DATE, $3, $4)`,
		in.Nombre, in.Fecha, in.CantidadPersonas, in.Estado).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo crear la reserva"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"reservaId":        id,
		"nombre":           in.Nombre,
		"fecha":            in.Fecha,
		"cantidadPersonas": in.CantidadPersonas,
		"estado":           in.Estado,
	})
}

func reservasList(c *gin.Context) {
	var fecha, estado, nombre any
	var cantidad any
	if q := c.Query("fecha"); q != "" {
		if _, err := time.Parse("2006-01-02", q); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "fecha debe tener formato YYYY-MM-DD"})
			return
		}
		fecha = q
	}
	if q := c.Query("estado"); q != "" {
		estado = q
	}
	if q := c.Query("nombre"); q != "" {
		nombre = q
	}
	if q := c.Query("cantidadPersonas"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cantidadPersonas debe ser un entero"})
			return
		}
		cantidad = n
	}
	rows, err := db.QueryContext(c.Request.Context(),
		`SELECT * FROM obtenerReservas($1::DATE, $2::VARCHAR, $3::VARCHAR, $4::INTEGER)`,
		fecha, estado, nombre, cantidad)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo listar las reservas"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		r, err := scanReserva(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo listar las reservas"})
			return
		}
		out = append(out, r.json())
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo listar las reservas"})
		return
	}
	c.JSON(http.StatusOK, out)
}

func reservasGET(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id debe ser un entero"})
		return
	}
	r, err := scanReserva(db.QueryRowContext(c.Request.Context(),
		`SELECT * FROM obtenerReservaPorId($1::INTEGER)`, id))
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "reserva no encontrada"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo consultar la reserva"})
		return
	}
	c.JSON(http.StatusOK, r.json())
}

func reservasDELETE(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id debe ser un entero"})
		return
	}
	var borrada bool
	err = db.QueryRowContext(c.Request.Context(),
		`SELECT eliminarReserva($1::INTEGER)`, id).Scan(&borrada)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo eliminar la reserva"})
		return
	}
	if !borrada {
		c.JSON(http.StatusNotFound, gin.H{"error": "reserva no encontrada"})
		return
	}
	c.Status(http.StatusNoContent)
}

func pingDB(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "OK"})
}

func setupRouter() *gin.Engine {
	router := gin.Default()

	router.GET("/", func(c *gin.Context) { // home
		c.JSON(200, gin.H{
			"message": "Welcome to My Website",
		})
	})

	router.GET("/reservas", reservasList) // getall reservas (public)

	router.GET("/reservas/:id", reservasGET) // get reservas by id (public)

	router.GET("/health", func(c *gin.Context) { // health
		c.JSON(200, gin.H{
			"status": "OK",
		})
	})

	router.GET("/ready", pingDB)

	protected := router.Group("/", authMiddleware("reserva-writer"))
	protected.DELETE("/reservas/:id", reservasDELETE) // delete reservas by id
	protected.POST("/reservas", reservasPOST)         // post reservas

	return router
}

func main() {
	if err := initDB(); err != nil {
		panic(fmt.Sprintf("no se pudo conectar a PostgreSQL: %v", err))
	}
	defer db.Close()

	router := setupRouter()

	port := os.Getenv("PORT")
	if port == "" {
		port = "1412"
	}
	router.Run(":" + port)
	return
}
