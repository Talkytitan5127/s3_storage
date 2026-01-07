package cleanup

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/s3storage/api/proto"
	"github.com/s3storage/internal/storage"
	"google.golang.org/grpc"
)

const (
	// DefaultCleanupInterval is the default interval for cleanup job
	DefaultCleanupInterval = 5 * time.Minute
	// ChunkDeleteTimeout is the timeout for deleting a chunk from storage server
	ChunkDeleteTimeout = 10 * time.Second
)

// CleanupJob handles cleanup of expired upload sessions and orphaned chunks
type CleanupJob struct {
	storage        *storage.PostgresStorage
	storageClients map[uuid.UUID]*grpc.ClientConn
	clientsMu      *sync.RWMutex
	interval       time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// NewCleanupJob creates a new cleanup job
func NewCleanupJob(
	storage *storage.PostgresStorage,
	storageClients map[uuid.UUID]*grpc.ClientConn,
	clientsMu *sync.RWMutex,
) *CleanupJob {
	return &CleanupJob{
		storage:        storage,
		storageClients: storageClients,
		clientsMu:      clientsMu,
		interval:       DefaultCleanupInterval,
		stopChan:       make(chan struct{}),
	}
}

// NewCleanupJobWithInterval creates a new cleanup job with custom interval
func NewCleanupJobWithInterval(
	storage *storage.PostgresStorage,
	storageClients map[uuid.UUID]*grpc.ClientConn,
	clientsMu *sync.RWMutex,
	interval time.Duration,
) *CleanupJob {
	return &CleanupJob{
		storage:        storage,
		storageClients: storageClients,
		clientsMu:      clientsMu,
		interval:       interval,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the cleanup job background worker
func (j *CleanupJob) Start(ctx context.Context) {
	j.wg.Add(1)
	go j.run(ctx)
	log.Printf("Cleanup job started (interval: %v)", j.interval)
}

// Stop stops the cleanup job
func (j *CleanupJob) Stop() {
	close(j.stopChan)
	j.wg.Wait()
	log.Println("Cleanup job stopped")
}

// run is the main cleanup loop
func (j *CleanupJob) run(ctx context.Context) {
	defer j.wg.Done()

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Run cleanup immediately on start
	if err := j.cleanupExpiredSessions(ctx); err != nil {
		log.Printf("Error during initial cleanup: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := j.cleanupExpiredSessions(ctx); err != nil {
				log.Printf("Error during cleanup: %v", err)
			}
		case <-j.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// cleanupExpiredSessions cleans up expired upload sessions and their orphaned chunks
func (j *CleanupJob) cleanupExpiredSessions(ctx context.Context) error {
	// Get expired sessions
	sessions, err := j.storage.GetExpiredSessions(ctx)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		log.Println("No expired sessions to clean up")
		return nil
	}

	log.Printf("Found %d expired sessions to clean up", len(sessions))

	cleanedCount := 0
	errorCount := 0

	for _, session := range sessions {
		if err := j.cleanupSession(ctx, session); err != nil {
			log.Printf("Error cleaning up session %s: %v", session.SessionID, err)
			errorCount++
		} else {
			cleanedCount++
		}
	}

	log.Printf("Cleanup completed: %d sessions cleaned, %d errors", cleanedCount, errorCount)
	return nil
}

// cleanupSession cleans up a single expired session
func (j *CleanupJob) cleanupSession(ctx context.Context, session *storage.UploadSession) error {
	log.Printf("Cleaning up expired session %s for file %s", session.SessionID, session.FileID)

	// Get chunks associated with this file
	chunks, err := j.storage.GetChunksByFileID(ctx, session.FileID)
	if err != nil {
		return err
	}

	// Delete chunks from storage servers
	deletedChunks := 0
	for _, chunk := range chunks {
		if err := j.deleteChunkFromServer(ctx, chunk); err != nil {
			log.Printf("Warning: failed to delete chunk %s from server %s: %v",
				chunk.ChunkID, chunk.StorageServerID, err)
			// Continue with other chunks even if one fails
		} else {
			deletedChunks++
		}
	}

	log.Printf("Deleted %d/%d chunks for session %s", deletedChunks, len(chunks), session.SessionID)

	// Delete file record (this will cascade delete chunks via foreign key)
	if err := j.storage.DeleteFile(ctx, session.FileID); err != nil {
		log.Printf("Warning: failed to delete file record %s: %v", session.FileID, err)
	}

	// Delete session record
	if err := j.storage.DeleteUploadSession(ctx, session.SessionID); err != nil {
		log.Printf("Warning: failed to delete session record %s: %v", session.SessionID, err)
	}

	return nil
}

// deleteChunkFromServer deletes a chunk from a storage server via gRPC
func (j *CleanupJob) deleteChunkFromServer(ctx context.Context, chunk *storage.Chunk) error {
	// Get storage client
	j.clientsMu.RLock()
	conn, exists := j.storageClients[chunk.StorageServerID]
	j.clientsMu.RUnlock()

	if !exists {
		// Server might be offline, log and continue
		log.Printf("Storage server %s not available for chunk %s deletion",
			chunk.StorageServerID, chunk.ChunkID)
		return nil
	}

	client := pb.NewStorageServiceClient(conn)

	// Create context with timeout
	deleteCtx, cancel := context.WithTimeout(ctx, ChunkDeleteTimeout)
	defer cancel()

	// Delete chunk
	req := &pb.DeleteChunkRequest{
		ChunkId: chunk.ChunkID.String(),
	}

	resp, err := client.DeleteChunk(deleteCtx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		log.Printf("Failed to delete chunk %s from server %s",
			chunk.ChunkID, chunk.StorageServerID)
	}

	return nil
}
