package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/handlers"
	"azure-openai-proxy/internal/instance"
	"azure-openai-proxy/internal/middleware"
	"azure-openai-proxy/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func main() {
	// Parse command line flags
	configDir := flag.String("config", "configs", "Configuration directory path")
	port := flag.String("port", "", "Port to run server on (overrides config)")
	flag.Parse()

	// Load configuration
	loader := config.NewLoader()
	cfg, err := loader.LoadConfig(*configDir)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override port if specified
	if *port != "" {
		fmt.Sscanf(*port, "%d", &cfg.Port)
	}

	// Setup logging
	setupLogging(cfg.Logging)

	// Initialize storage
	stateStore, err := storage.NewRedisStore("redis://localhost:6379", "")
	if err != nil {
		logrus.Fatalf("Failed to initialize Redis store: %v", err)
	}

	configStore, err := storage.NewSQLiteStore("proxy.db")
	if err != nil {
		logrus.Fatalf("Failed to initialize SQLite store: %v", err)
	}

	// Initialize instance manager
	instanceManager, err := instance.NewManager(cfg.Instances, cfg.Routing.Strategy, stateStore, configStore)
	if err != nil {
		logrus.Fatalf("Failed to initialize instance manager: %v", err)
	}

	// Start health monitoring
	go instanceManager.StartHealthMonitoring()

	// Setup HTTP server
	if cfg.Logging.Level != "DEBUG" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	
	// Add middleware
	router.Use(middleware.RequestLogger())
	router.Use(middleware.RequestID())
	router.Use(middleware.CORS())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.Metrics())
	router.Use(gin.Recovery())

	// Initialize handlers
	proxyHandler := handlers.NewProxyHandler(instanceManager)
	adminHandler := handlers.NewAdminHandler(instanceManager)
	statsHandler := handlers.NewStatsHandler(instanceManager)

	// Setup routes
	setupRoutes(router, proxyHandler, adminHandler, statsHandler)

	// Start server
	address := fmt.Sprintf(":%d", cfg.Port)
	logrus.Infof("Starting Azure OpenAI Proxy server on %s", address)
	
	if err := router.Run(address); err != nil {
		logrus.Fatalf("Failed to start server: %v", err)
	}
}

func setupLogging(cfg config.LoggingConfig) {
	// Set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	// Set log format
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z",
	})

	// Set log output
	if cfg.File != "" {
		// Ensure log directory exists
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0755); err != nil {
			logrus.Warnf("Failed to create log directory: %v", err)
		} else {
			file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				logrus.Warnf("Failed to open log file: %v", err)
			} else {
				logrus.SetOutput(file)
			}
		}
	}
}

func setupRoutes(router *gin.Engine, proxy *handlers.ProxyHandler, admin *handlers.AdminHandler, stats *handlers.StatsHandler) {
	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// OpenAI API proxy routes
	v1 := router.Group("/v1")
	{
		v1.POST("/chat/completions", proxy.ChatCompletions)
		v1.POST("/completions", proxy.Completions)
		v1.POST("/embeddings", proxy.Embeddings)
	}

	// Admin routes (with optional authentication)
	adminGroup := router.Group("/admin")
	if os.Getenv("ADMIN_TOKEN") != "" {
		adminGroup.Use(middleware.AdminAuth())
	}
	{
		adminGroup.GET("/health", admin.GetHealth)
		adminGroup.GET("/instances", admin.GetInstances)
		adminGroup.GET("/instances/:name", admin.GetInstance)
		adminGroup.POST("/instances/:name/reset", admin.ResetInstance)
		adminGroup.PUT("/instances/:name/config", admin.UpdateInstanceConfig)
		adminGroup.GET("/config", admin.GetConfig)
	}

	// Stats routes
	statsGroup := router.Group("/stats")
	{
		statsGroup.GET("/", stats.GetOverallStats)
		statsGroup.GET("/instances", stats.GetInstanceStats)
		statsGroup.GET("/usage", stats.GetUsageStats)
	}
}