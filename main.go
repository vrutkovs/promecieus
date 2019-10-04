package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// Env holds references to useful objects in router funcs
type Env struct{}

func (e *Env) create(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"url": "https://foo",
	})
}

func main() {
	// setup webhook listener
	r := gin.Default()

	env := &Env{}

	// create prometheus instance
	r.POST("/create/:url", env.create)

	r.Run(":8080")
}
