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
// @description In-memory lock queue

// @host localhost:8080
// @BasePath /api
func main() {
	configFile := os.Getenv("GLOCK_CONFIG_FILE")
	config, err := core.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting glock server with config: %+v", config)

	locks := &core.GlockServer{
		Size:     0,
		Capacity: config.Capacity,
		Locks:    sync.Map{},
		LockTree: core.NewLockTree(),
		Config:   config,
	}

	r := gin.Default()

	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/", "./static/index.html")

	api := r.Group("/api")
	{
		api.POST("/create", func(c *gin.Context) {
			core.CreateHandler(c, locks)
		})
		api.POST("/update", func(c *gin.Context) {
			core.UpdateHandler(c, locks)
		})
		api.DELETE("/delete/:name", func(c *gin.Context) {
			core.DeleteHandler(c, locks)
		})
		api.POST("/acquire", func(c *gin.Context) {
			core.AcquireHandler(c, locks)
		})
		api.POST("/refresh", func(c *gin.Context) {
			core.RefreshHandler(c, locks)
		})
		api.POST("/verify", func(c *gin.Context) {
			core.VerifyHandler(c, locks)
		})
		api.POST("/release", func(c *gin.Context) {
			core.ReleaseHandler(c, locks)
		})
		api.POST("/poll", func(c *gin.Context) {
			core.PollHandler(c, locks)
		})
		api.POST("/remove", func(c *gin.Context) {
			core.RemoveFromQueueHandler(c, locks)
		})
		api.POST("/queue/list", func(c *gin.Context) {
			core.ListQueueHandler(c, locks)
		})
		api.POST("/freeze/:name", func(c *gin.Context) {
			core.FreezeLockHandler(c, locks)
		})
		api.POST("/unfreeze/:name", func(c *gin.Context) {
			core.UnfreezeLockHandler(c, locks)
		})

		api.GET("/status", func(c *gin.Context) {
			core.StatusHandler(c, locks)
		})
		api.GET("/list", func(c *gin.Context) {
			core.ListHandler(c, locks)
		})
		api.GET("/metrics/:name", func(c *gin.Context) {
			core.MetricsHandler(c, locks)
		})

		api.GET("/config", func(c *gin.Context) {
			core.ConfigHandler(c, locks)
		})
		api.POST("/config/update", func(c *gin.Context) {
			core.UpdateConfigHandler(c, locks)
		})
	}

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	log.Printf("Starting server on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
