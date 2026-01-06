package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/s3storage/internal/api"
	"github.com/s3storage/internal/circuitbreaker"
	"github.com/s3storage/internal/hasher"
	"github.com/s3storage/internal/retry"
	"github.com/s3storage/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultHTTPPort       = "8080"
	storageServerTimeout  = 30 * time.Second
	heartbeatCheckTimeout = 30 * time.Second
)

func main() {
	// Get configuration from environment
	httpPort := getEnv("HTTP_PORT", defaultHTTPPort)
	databaseURL := getEnv("DATABASE_URL", "")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	log.Printf("Starting API Gateway")
	log.Printf("HTTP Port: %s", httpPort)

	// Initialize database connection
	ctx := context.Background()
	dbPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	// Test database connection
	if err := dbPool.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Database connection established")

	// Initialize storage
	store := storage.NewPostgresStorage(dbPool)

	// Initialize API Gateway
	gateway := &api.APIGateway{
		Router:          gin.Default(),
		DB:              dbPool,
		Storage:         store,
		StorageClients:  make(map[uuid.UUID]*grpc.ClientConn),
		CircuitBreakers: make(map[uuid.UUID]*circuitbreaker.CircuitBreaker),
		HashRing:        hasher.NewHashRing(),
		RetryConfig:     retry.DefaultRetryConfig(),
	}

	// Initialize hash ring with active storage servers
	if err := initializeHashRing(ctx, gateway, store); err != nil {
		log.Fatalf("Failed to initialize hash ring: %v", err)
	}

	// Setup routes
	setupRoutes(gateway)

	// Start hash ring refresh loop
	gateway.StartHashRingRefreshLoop(ctx)
	log.Println("Hash ring refresh loop started")

	// Start cleanup job
	gateway.StartCleanupJob(ctx)
	log.Println("Cleanup job started")

	// Create HTTP server
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", httpPort),
		Handler: gateway.Router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("HTTP server listening on :%s", httpPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	// Stop hash ring refresh loop
	gateway.StopHashRingRefreshLoop()

	// Stop cleanup job
	gateway.StopCleanupJob()

	// Shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Close gRPC connections
	gateway.CloseAllStorageClients()

	log.Println("API Gateway stopped")
}

// setupRoutes configures all API routes
func setupRoutes(gw *api.APIGateway) {
	// Health check
	gw.Router.GET("/health", func(c *gin.Context) {
		healthCheck(c, gw)
	})

	// File operations
	apiGroup := gw.Router.Group("/files")
	{
		apiGroup.POST("", func(c *gin.Context) {
			gw.UploadFile(c)
		})
		apiGroup.GET("/:file_id", func(c *gin.Context) {
			gw.DownloadFile(c)
		})
		apiGroup.GET("/:file_id/metadata", func(c *gin.Context) {
			gw.GetFileMetadata(c)
		})
		apiGroup.GET("", func(c *gin.Context) {
			gw.ListFiles(c)
		})
		apiGroup.DELETE("/:file_id", func(c *gin.Context) {
			gw.DeleteFile(c)
		})
	}
}

// initializeHashRing initializes the consistent hash ring with active storage servers
func initializeHashRing(ctx context.Context, gw *api.APIGateway, store *storage.PostgresStorage) error {
	servers, err := store.GetActiveStorageServers(ctx, heartbeatCheckTimeout)
	if err != nil {
		return fmt.Errorf("failed to get active storage servers: %w", err)
	}

	if len(servers) == 0 {
		return fmt.Errorf("no active storage servers available")
	}

	log.Printf("Found %d active storage servers", len(servers))

	for _, server := range servers {
		// Add server to hash ring
		if err := gw.HashRing.AddServer(server.ServerID.String(), server.GRPCAddress); err != nil {
			return fmt.Errorf("failed to add server to hash ring: %w", err)
		}

		// Create gRPC connection
		conn, err := connectToStorageServer(server.GRPCAddress)
		if err != nil {
			log.Printf("Warning: failed to connect to storage server %s: %v", server.ServerID, err)
			continue
		}

		gw.StorageClients[server.ServerID] = conn
		log.Printf("Connected to storage server: %s at %s", server.ServerID, server.GRPCAddress)
	}

	return nil
}

// connectToStorageServer creates a gRPC connection to a storage server
func connectToStorageServer(address string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storageServerTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024), // 1GB
			grpc.MaxCallSendMsgSize(1024*1024*1024), // 1GB
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to storage server: %w", err)
	}

	return conn, nil
}

// healthCheck handles health check requests
func healthCheck(c *gin.Context, gw *api.APIGateway) {
	ctx := c.Request.Context()

	// Check database connection
	if err := gw.DB.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"error":   "database connection failed",
			"details": err.Error(),
		})
		return
	}

	// Check active storage servers
	servers, err := gw.Storage.GetActiveStorageServers(ctx, heartbeatCheckTimeout)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"error":   "failed to get storage servers",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          "healthy",
		"storage_servers": len(servers),
		"timestamp":       time.Now().UTC(),
	})
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
