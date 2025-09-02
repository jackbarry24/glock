package main

import (
	"fmt"
	"glock-server/core"
	"log"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "glock-server/docs"
)

// @title Glock Server API
// @version 1.0
// @description A distributed lock server with queuing support
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /

// @externalDocs.description OpenAPI
// @externalDocs.url https://swagger.io/resources/open-api/
func main() {
	// Load configuration
	configFile := os.Getenv("GLOCK_CONFIG_FILE")
	config, err := core.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting glock server with config: %+v", config)

	// Initialize server
	locks := &core.GlockServer{
		Size:     0,
		Capacity: config.Capacity,
		Locks:    sync.Map{},
		Config:   config,
	}

	// Setup Gin router
	r := gin.Default()

	// Lock management endpoints
	r.POST("/create", func(c *gin.Context) {
		core.CreateHandler(c, locks)
	})
	r.POST("/update", func(c *gin.Context) {
		core.UpdateHandler(c, locks)
	})
	r.DELETE("/delete/:name", func(c *gin.Context) {
		core.DeleteHandler(c, locks)
	})
	r.POST("/acquire", func(c *gin.Context) {
		core.AcquireHandler(c, locks)
	})
	r.POST("/refresh", func(c *gin.Context) {
		core.RefreshHandler(c, locks)
	})
	r.POST("/verify", func(c *gin.Context) {
		core.VerifyHandler(c, locks)
	})
	r.POST("/release", func(c *gin.Context) {
		core.ReleaseHandler(c, locks)
	})
	r.POST("/poll", func(c *gin.Context) {
		core.PollHandler(c, locks)
	})

	// Status and listing endpoints
	r.GET("/status", func(c *gin.Context) {
		core.StatusHandler(c, locks)
	})
	r.GET("/list", func(c *gin.Context) {
		core.ListHandler(c, locks)
	})

	// Configuration endpoints
	r.GET("/config", func(c *gin.Context) {
		core.ConfigHandler(c, locks)
	})
	r.POST("/config/update", func(c *gin.Context) {
		core.UpdateConfigHandler(c, locks)
	})

	// Swagger UI endpoint
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Start server
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	log.Printf("Starting server on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
