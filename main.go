package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// Env holds references to useful objects in router funcs
type Env struct{}

func (e *Env) create(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "foo",
	})
}

func main() {
	// Get apps domain from the route

	// setup webhook listener
	r := gin.Default()

	env := &Env{}

	// create prometheus instance
	r.POST("/create", env.create)

	r.Run(":8080")
}
