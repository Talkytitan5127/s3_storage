package api

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pb "github.com/s3storage/api/proto"
	"github.com/s3storage/internal/circuitbreaker"
	"github.com/s3storage/internal/cleanup"
	"github.com/s3storage/internal/hasher"
	"github.com/s3storage/internal/retry"
	"github.com/s3storage/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// HashRingRefreshInterval is how often to refresh the hash ring from database
	HashRingRefreshInterval = 30 * time.Second
	// ServerHeartbeatTimeout is the maximum age for a server heartbeat to be considered active
	ServerHeartbeatTimeout = 30 * time.Second
	// StorageServerConnectTimeout is the timeout for connecting to storage servers
	StorageServerConnectTimeout = 10 * time.Second
	// ConnectionHealthCheckInterval is how often to check connection health
	ConnectionHealthCheckInterval = 10 * time.Second
)

var (
	// ErrStorageServerNotFound is returned when a storage server is not found
	ErrStorageServerNotFound = errors.New("storage server not found")
)

// APIGateway represents the API Gateway server
type APIGateway struct {
	Router          *gin.Engine
	DB              *pgxpool.Pool
	Storage         *storage.PostgresStorage
	StorageClients  map[uuid.UUID]*grpc.ClientConn
	CircuitBreakers map[uuid.UUID]*circuitbreaker.CircuitBreaker
	HashRing        *hasher.HashRing
	CleanupJob      *cleanup.CleanupJob
	RetryConfig     *retry.RetryConfig
	clientsMu       sync.RWMutex
	stopRefresh     chan struct{}
	stopHealthCheck chan struct{}
}

// getStorageClient returns a gRPC client for a storage server
// If connection is broken, attempts to reconnect
func (gw *APIGateway) getStorageClient(serverID uuid.UUID) (pb.StorageServiceClient, error) {
	gw.clientsMu.RLock()
	conn, exists := gw.StorageClients[serverID]
	gw.clientsMu.RUnlock()

	if !exists {
		log.Printf("Storage client not found for server %s", serverID)
		return nil, ErrStorageServerNotFound
	}

	// Check connection state and reconnect if needed
	state := conn.GetState()
	if state == connectivity.TransientFailure || state == connectivity.Shutdown {
		log.Printf("Connection to server %s is in state %v, attempting reconnect", serverID, state)

		// Get server address from hash ring
		servers := gw.HashRing.GetAllServers()
		var serverAddr string
		for _, s := range servers {
			if s.ID == serverID.String() {
				serverAddr = s.Address
				break
			}
		}

		if serverAddr == "" {
			log.Printf("Server %s not found in hash ring (total servers: %d)", serverID, len(servers))
			return nil, ErrStorageServerNotFound
		}

		// Attempt reconnection
		if err := gw.reconnectToStorageServer(serverID, serverAddr); err != nil {
			log.Printf("Failed to reconnect to server %s at %s: %v", serverID, serverAddr, err)
			return nil, err
		}

		log.Printf("Successfully reconnected to server %s", serverID)

		// Get new connection
		gw.clientsMu.RLock()
		conn = gw.StorageClients[serverID]
		gw.clientsMu.RUnlock()
	}

	return pb.NewStorageServiceClient(conn), nil
}

// getCircuitBreaker returns the circuit breaker for a storage server
func (gw *APIGateway) getCircuitBreaker(serverID uuid.UUID) *circuitbreaker.CircuitBreaker {
	gw.clientsMu.Lock()
	defer gw.clientsMu.Unlock()

	if cb, exists := gw.CircuitBreakers[serverID]; exists {
		return cb
	}

	// Create new circuit breaker for this server
	cb := circuitbreaker.NewCircuitBreaker(circuitbreaker.DefaultConfig())
	gw.CircuitBreakers[serverID] = cb
	return cb
}

// StartHashRingRefreshLoop starts the background loop that refreshes the hash ring
func (gw *APIGateway) StartHashRingRefreshLoop(ctx context.Context) {
	gw.stopRefresh = make(chan struct{})
	ticker := time.NewTicker(HashRingRefreshInterval)

	go func() {
		defer ticker.Stop()
		log.Printf("Hash ring refresh loop started (interval: %v)", HashRingRefreshInterval)

		for {
			select {
			case <-ticker.C:
				if err := gw.RefreshHashRing(ctx); err != nil {
					log.Printf("Error refreshing hash ring: %v", err)
				}
			case <-gw.stopRefresh:
				log.Println("Hash ring refresh loop stopped")
				return
			case <-ctx.Done():
				log.Println("Hash ring refresh loop stopped due to context cancellation")
				return
			}
		}
	}()
}

// StopHashRingRefreshLoop stops the hash ring refresh loop
func (gw *APIGateway) StopHashRingRefreshLoop() {
	if gw.stopRefresh != nil {
		close(gw.stopRefresh)
	}
}

// StartCleanupJob starts the cleanup job for expired sessions
func (gw *APIGateway) StartCleanupJob(ctx context.Context) {
	gw.CleanupJob = cleanup.NewCleanupJob(gw.Storage, gw.StorageClients, &gw.clientsMu)
	gw.CleanupJob.Start(ctx)
}

// StopCleanupJob stops the cleanup job
func (gw *APIGateway) StopCleanupJob() {
	if gw.CleanupJob != nil {
		gw.CleanupJob.Stop()
	}
}

// StartConnectionHealthCheck starts periodic health checks for storage server connections
func (gw *APIGateway) StartConnectionHealthCheck(ctx context.Context) {
	gw.stopHealthCheck = make(chan struct{})
	ticker := time.NewTicker(ConnectionHealthCheckInterval)

	go func() {
		defer ticker.Stop()
		log.Printf("Connection health check loop started (interval: %v)", ConnectionHealthCheckInterval)

		for {
			select {
			case <-ticker.C:
				gw.checkAndReconnectStorageServers(ctx)
			case <-gw.stopHealthCheck:
				log.Println("Connection health check loop stopped")
				return
			case <-ctx.Done():
				log.Println("Connection health check loop stopped due to context cancellation")
				return
			}
		}
	}()
}

// StopConnectionHealthCheck stops the connection health check loop
func (gw *APIGateway) StopConnectionHealthCheck() {
	if gw.stopHealthCheck != nil {
		close(gw.stopHealthCheck)
	}
}

// checkAndReconnectStorageServers checks all storage server connections and reconnects if needed
func (gw *APIGateway) checkAndReconnectStorageServers(ctx context.Context) {
	gw.clientsMu.RLock()
	serverIDs := make([]uuid.UUID, 0, len(gw.StorageClients))
	for serverID := range gw.StorageClients {
		serverIDs = append(serverIDs, serverID)
	}
	gw.clientsMu.RUnlock()

	for _, serverID := range serverIDs {
		gw.clientsMu.RLock()
		conn, exists := gw.StorageClients[serverID]
		gw.clientsMu.RUnlock()

		if !exists {
			continue
		}

		state := conn.GetState()

		// Reconnect if connection is in bad state
		if state == connectivity.TransientFailure || state == connectivity.Shutdown || state == connectivity.Idle {
			log.Printf("Connection to server %s is in state %v, attempting reconnect", serverID, state)

			// Get server address from hash ring
			servers := gw.HashRing.GetAllServers()
			var serverAddr string
			for _, s := range servers {
				if s.ID == serverID.String() {
					serverAddr = s.Address
					break
				}
			}

			if serverAddr == "" {
				log.Printf("Server %s not found in hash ring, skipping reconnect", serverID)
				continue
			}

			// Attempt reconnection
			if err := gw.reconnectToStorageServer(serverID, serverAddr); err != nil {
				log.Printf("Failed to reconnect to server %s: %v", serverID, err)

				// Reset circuit breaker on connection failure
				if cb := gw.getCircuitBreaker(serverID); cb != nil {
					cb.Reset()
				}
			} else {
				log.Printf("Successfully reconnected to server %s", serverID)
			}
		}
	}
}

// RefreshHashRing refreshes the hash ring with active storage servers from database
func (gw *APIGateway) RefreshHashRing(ctx context.Context) error {
	// Get active servers from database
	servers, err := gw.Storage.GetActiveStorageServers(ctx, ServerHeartbeatTimeout)
	if err != nil {
		return err
	}

	// Build map of current active servers
	activeServers := make(map[string]*storage.StorageServer)
	for _, server := range servers {
		activeServers[server.ServerID.String()] = server
	}

	// Get current servers in hash ring
	currentServers := gw.HashRing.GetAllServers()
	currentServerMap := make(map[string]bool)
	for _, server := range currentServers {
		currentServerMap[server.ID] = true
	}

	// Remove inactive servers from hash ring
	for _, server := range currentServers {
		if _, exists := activeServers[server.ID]; !exists {
			log.Printf("Removing inactive server from hash ring: %s", server.ID)
			if err := gw.HashRing.RemoveServer(server.ID); err != nil {
				log.Printf("Error removing server %s from hash ring: %v", server.ID, err)
			}

			// Close gRPC connection
			serverUUID, _ := uuid.Parse(server.ID)
			gw.closeStorageClient(serverUUID)
		}
	}

	// Add new servers to hash ring
	for serverID, server := range activeServers {
		if !currentServerMap[serverID] {
			log.Printf("Adding new server to hash ring: %s at %s", serverID, server.GRPCAddress)
			if err := gw.HashRing.AddServer(serverID, server.GRPCAddress); err != nil {
				log.Printf("Error adding server %s to hash ring: %v", serverID, err)
				continue
			}

			// Create gRPC connection
			if err := gw.connectToStorageServer(server.ServerID, server.GRPCAddress); err != nil {
				log.Printf("Warning: failed to connect to storage server %s: %v", serverID, err)
			} else {
				// Create circuit breaker for new server
				gw.getCircuitBreaker(server.ServerID)
			}
		}
	}

	log.Printf("Hash ring refreshed: %d active servers", len(activeServers))
	return nil
}

// connectToStorageServer creates a gRPC connection to a storage server
// Connection is non-blocking - it will connect in background
func (gw *APIGateway) connectToStorageServer(serverID uuid.UUID, address string) error {
	// Create connection without blocking
	conn, err := grpc.Dial(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024), // 1GB
			grpc.MaxCallSendMsgSize(1024*1024*1024), // 1GB
		),
	)
	if err != nil {
		return err
	}

	gw.clientsMu.Lock()
	gw.StorageClients[serverID] = conn
	gw.clientsMu.Unlock()

	log.Printf("Initiated connection to storage server: %s at %s", serverID, address)

	// Check connection state in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), StorageServerConnectTimeout)
		defer cancel()

		// Wait for connection to be ready
		if conn.WaitForStateChange(ctx, connectivity.Idle) {
			state := conn.GetState()
			if state == connectivity.Ready {
				log.Printf("Successfully connected to storage server: %s at %s", serverID, address)
			} else {
				log.Printf("Connection to storage server %s is in state: %v", serverID, state)
			}
		}
	}()

	return nil
}

// reconnectToStorageServer attempts to reconnect to a storage server
// Connection is non-blocking to avoid hanging on unavailable servers
func (gw *APIGateway) reconnectToStorageServer(serverID uuid.UUID, address string) error {
	// Close existing connection
	gw.clientsMu.Lock()
	if conn, exists := gw.StorageClients[serverID]; exists {
		conn.Close()
		delete(gw.StorageClients, serverID)
	}
	gw.clientsMu.Unlock()

	// Create new connection without blocking
	conn, err := grpc.Dial(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024), // 1GB
			grpc.MaxCallSendMsgSize(1024*1024*1024), // 1GB
		),
	)
	if err != nil {
		return err
	}

	gw.clientsMu.Lock()
	gw.StorageClients[serverID] = conn
	gw.clientsMu.Unlock()

	log.Printf("Initiated reconnection to storage server: %s at %s", serverID, address)

	// Check connection state in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), StorageServerConnectTimeout)
		defer cancel()

		// Wait for connection to be ready
		if conn.WaitForStateChange(ctx, connectivity.Idle) {
			state := conn.GetState()
			if state == connectivity.Ready {
				log.Printf("Successfully reconnected to storage server: %s at %s", serverID, address)
			} else {
				log.Printf("Reconnection to storage server %s is in state: %v", serverID, state)
			}
		}
	}()

	return nil
}

// closeStorageClient closes a gRPC connection to a storage server
func (gw *APIGateway) closeStorageClient(serverID uuid.UUID) {
	gw.clientsMu.Lock()
	defer gw.clientsMu.Unlock()

	if conn, exists := gw.StorageClients[serverID]; exists {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing connection to server %s: %v", serverID, err)
		}
		delete(gw.StorageClients, serverID)
		delete(gw.CircuitBreakers, serverID)
		log.Printf("Closed connection to storage server: %s", serverID)
	}
}

// CloseAllStorageClients closes all gRPC connections
func (gw *APIGateway) CloseAllStorageClients() {
	gw.clientsMu.Lock()
	defer gw.clientsMu.Unlock()

	for serverID, conn := range gw.StorageClients {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing connection to server %s: %v", serverID, err)
		}
	}
	gw.StorageClients = make(map[uuid.UUID]*grpc.ClientConn)
	log.Println("All storage client connections closed")
}
