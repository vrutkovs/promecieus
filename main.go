package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
)

// Env holds references to useful objects in router funcs
type Env struct {
	route string
}

func (e *Env) create(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"url": "https://foo",
	})
}

func main() {
	// Check neccessary env vars are set
	route := os.Getenv("ROUTE")
	if len(route) == 0 {
		panic("ROUTE env var is not set")
	}

	// setup webhook listener
	r := gin.Default()

	env := &Env{}

	// create prometheus instance
	r.POST("/create/:url", env.create)

	r.Run(":8080")
}
