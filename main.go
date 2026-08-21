package main

import (
	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()
	router.GET("/", func(c *gin.Context) { // home
		c.JSON(200, gin.H{
			"message": "Welcome to My Website",
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
