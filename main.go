package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// health is k8s endpoint for liveness check
func health(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// show index page
func index(c *gin.Context) {
	c.HTML(http.StatusOK, "html/index.html", gin.H{})
}

func jsx(c *gin.Context) {
	c.HTML(http.StatusOK, "html/app.jsx", gin.H{})
}

func main() {
	server := &ServerSettings{}
	r := gin.New()

	// Load templates from bin assets
	t, err := loadTemplate()
	if err != nil {
		panic(err)
	}
	r.SetHTMLTemplate(t)

	// Don't log k8s health endpoint
	r.Use(
		gin.LoggerWithWriter(gin.DefaultWriter, "/health"),
		gin.Recovery(),
	)
	r.GET("/health", health)
	r.GET("/", index)
	r.GET("/app.jsx", jsx)
	r.GET("/ws/status", server.handleStatusViaWS)

	r.Run(":8080")
}
