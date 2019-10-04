package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Env holds references to useful objects in router funcs
type Env struct{}

func (e *Env) create(c *gin.Context) {
	url := c.PostForm("url")
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("foo %s", url),
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
