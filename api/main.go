package main

import (
	"github.com/gin-gonic/gin"
)

func reservas(c *gin.Context) {
	// TODO: insert en reservas
}

func main() {
	router := gin.Default()
	router.GET("/", func(c *gin.Context) { // home
		c.JSON(200, gin.H{
			"message": "Welcome to My Website",
		})
	})

	router.POST("/reservas", func(c *gin.Context) { // reservas
		c.JSON(200, gin.H{
			"message": "Reserva created successfully",
		})
	})

	router.GET("/health", func(c *gin.Context) { // health
		c.JSON(200, gin.H{
			"status": "OK",
		})
	})

	router.Run(":1412")
	return
}
