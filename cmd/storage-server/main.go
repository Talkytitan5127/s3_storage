package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pb "github.com/s3storage/api/proto"
	grpchandlers "github.com/s3storage/internal/grpc"
	"github.com/s3storage/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultGRPCPort   = "50051"
	defaultDataDir    = "/data"
	heartbeatInterval = 10 * time.Second
	virtualNodesCount = 150
)

func main() {
	// Get configuration from environment
	serverID := getEnv("SERVER_ID", "storage-1")
	grpcPort := getEnv("GRPC_PORT", defaultGRPCPort)
	dataDir := getEnv("DATA_DIR", defaultDataDir)
	databaseURL := getEnv("DATABASE_URL", "")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	log.Printf("Starting Storage Server: %s", serverID)
	log.Printf("gRPC Port: %s", grpcPort)
	log.Printf("Data Directory: %s", dataDir)

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

	// Register storage server in database
	serverUUID := uuid.New()
	// Use the container hostname which matches the service name in docker-compose
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("Failed to get hostname: %v", err)
	}
	address := fmt.Sprintf("%s:%s", hostname, grpcPort)
	log.Printf("Registering storage server with address: %s", address)

	storageServerRecord := &storage.StorageServer{
		ServerID:       serverUUID,
		GRPCAddress:    address,
		AvailableSpace: 1024 * 1024 * 1024 * 1024, // 1TB default
		UsedSpace:      0,
	}

	if err := store.CreateStorageServer(ctx, storageServerRecord); err != nil {
		log.Fatalf("Failed to register storage server: %v", err)
	}
	// Use the server_id returned from the database (may differ if there was a conflict)
	serverUUID = storageServerRecord.ServerID
	log.Printf("Storage server registered with UUID: %s", serverUUID)

	// Create virtual nodes for consistent hashing
	if err := store.CreateHashRingNodes(ctx, serverUUID, virtualNodesCount); err != nil {
		log.Fatalf("Failed to create hash ring nodes: %v", err)
	}
	log.Printf("Created %d virtual nodes for consistent hashing", virtualNodesCount)

	// Initialize gRPC server
	grpcServer, err := grpchandlers.NewStorageServer(dataDir)
	if err != nil {
		log.Fatalf("Failed to create gRPC server: %v", err)
	}

	// Create gRPC listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", grpcPort, err)
	}

	// Create gRPC server with options
	grpcOpts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(1024 * 1024 * 1024), // 1GB max receive
		grpc.MaxSendMsgSize(1024 * 1024 * 1024), // 1GB max send
	}
	server := grpc.NewServer(grpcOpts...)
	pb.RegisterStorageServiceServer(server, grpcServer)

	// Register reflection service for debugging
	reflection.Register(server)

	// Start heartbeat goroutine
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := store.UpdateHeartbeat(ctx, serverUUID); err != nil {
					log.Printf("Failed to update heartbeat: %v", err)
				}
			case <-stopHeartbeat:
				return
			}
		}
	}()

	// Start gRPC server in a goroutine
	go func() {
		log.Printf("gRPC server listening on :%s", grpcPort)
		if err := server.Serve(listener); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	// Stop heartbeat
	close(stopHeartbeat)

	// Graceful shutdown
	server.GracefulStop()
	log.Println("Storage server stopped")
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
