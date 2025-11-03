package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"glock-server/core"
	_ "glock-server/docs"
)

// @title Glock Server API
// @version 1.0
// @description In-memory distributed lock server with queue support

// @host localhost:8080
// @BasePath /api
func main() {
	cfgFile := os.Getenv("GLOCK_CONFIG_FILE")
	cfg, err := core.LoadConfig(cfgFile)
	if err != nil {
		log.Fatalf("Failed to load HTTP configuration: %v", err)
	}

	serverConfig := core.ServerConfig{
		NumShards:            cfg.NumShards,
		ExpiryTick:           50 * time.Millisecond,
		Capacity:             cfg.Capacity,
		DefaultMaxTTL:        cfg.DefaultMaxTTL,
		DefaultTTL:           cfg.DefaultTTL,
		DefaultQueueCapacity: cfg.DefaultQueueCapacity,
		DefaultQueueTimeout:  cfg.DefaultQueueTimeout,
	}

	srv, err := core.NewServer(serverConfig)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	srv.Start()
	log.Println("Lock server started")

	r := gin.Default()

	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.GET("/", func(c *gin.Context) {
		ua := c.GetHeader("User-Agent")
		accept := c.GetHeader("Accept")

		if strings.Contains(ua, "curl") || !strings.Contains(accept, "text/html") {
			c.String(http.StatusOK, "Welcome to GLock Server")
			return
		}

		c.File("./static/index.html")
	})

	api := r.Group("/api")
	{
		api.GET("", func(c *gin.Context) {
			c.String(http.StatusOK, "Welcome to the GLock Server API. Swagger @ /swagger/index.html")
		})
		api.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
			})
		})

		locks := api.Group("/locks")
		{
			locks.GET("", core.ListHandler(srv))
			locks.POST("", core.CreateHandler(srv))
			locks.GET("/:lock_id", core.StatusHandler(srv))
			locks.DELETE("/:lock_id", core.DeleteHandler(srv))
			locks.GET("/:lock_id/metrics", core.MetricsHandler(srv))

			locks.POST("/:lock_id/lease", core.AcquireHandler(srv))
			locks.PUT("/:lock_id/lease", core.RefreshHandler(srv))
			locks.DELETE("/:lock_id/lease", core.ReleaseHandler(srv))

			locks.GET("/:lock_id/queue", core.QueueGetHandler(srv))
			locks.DELETE("/:lock_id/queue/:wait_id", core.QueueDelHandler(srv))
		}
	}

	r.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/swagger/index.html")
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("Starting HTTP server on %s", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := r.Run(addr); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Stop(ctx)

	log.Println("Server stopped")
}
