package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupTestDB creates a PostgreSQL container and returns a connection pool
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()

	// Create PostgreSQL container
	postgresContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("s3storage_test"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err, "Failed to start PostgreSQL container")

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "Failed to get connection string")

	// Create connection pool
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err, "Failed to create connection pool")

	// Load and execute schema
	schemaPath := filepath.Join("..", "..", "migrations", "001_initial_schema.sql")
	schemaSQL, err := os.ReadFile(schemaPath)
	require.NoError(t, err, "Failed to read schema file")

	_, err = pool.Exec(ctx, string(schemaSQL))
	require.NoError(t, err, "Failed to execute schema")

	// Cleanup function
	cleanup := func() {
		pool.Close()
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}

	return pool, cleanup
}

// TestCreateFile_Success tests successful file creation
func TestCreateFile_Success(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}

	err := storage.CreateFile(ctx, file)
	require.NoError(t, err, "CreateFile should succeed")

	// Verify file_id was generated
	assert.NotEqual(t, uuid.Nil, file.FileID, "FileID should be generated")

	// Verify upload_status is 'pending'
	assert.Equal(t, "pending", file.UploadStatus, "UploadStatus should be 'pending'")

	// Verify all fields are saved correctly
	var savedFile File
	err = pool.QueryRow(ctx, `
		SELECT file_id, filename, content_type, total_size, upload_status
		FROM files WHERE file_id = $1
	`, file.FileID).Scan(
		&savedFile.FileID,
		&savedFile.Filename,
		&savedFile.ContentType,
		&savedFile.TotalSize,
		&savedFile.UploadStatus,
	)
	require.NoError(t, err, "Should retrieve saved file")
	assert.Equal(t, file.Filename, savedFile.Filename)
	assert.Equal(t, file.ContentType, savedFile.ContentType)
	assert.Equal(t, file.TotalSize, savedFile.TotalSize)
	assert.Equal(t, "pending", savedFile.UploadStatus)
}

// TestCreateFile_DuplicateID tests duplicate file_id handling
func TestCreateFile_DuplicateID(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create first file
	file1 := &File{
		FileID:      uuid.New(),
		Filename:    "test1.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err := storage.CreateFile(ctx, file1)
	require.NoError(t, err)

	// Try to create second file with same ID
	file2 := &File{
		FileID:      file1.FileID, // Same ID
		Filename:    "test2.txt",
		ContentType: "text/plain",
		TotalSize:   2048,
	}
	err = storage.CreateFile(ctx, file2)
	assert.Error(t, err, "Should return error for duplicate file_id")
	assert.Contains(t, err.Error(), "duplicate", "Error should mention duplicate")
}

// TestCreateChunks_Batch tests batch chunk creation
func TestCreateChunks_Batch(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file first
	file := &File{
		Filename:    "large.bin",
		ContentType: "application/octet-stream",
		TotalSize:   6 * 1024 * 1024 * 1024, // 6 GB
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Create storage server
	server := &StorageServer{
		GRPCAddress: "localhost:50051",
	}
	err = storage.CreateStorageServer(ctx, server)
	require.NoError(t, err)

	// Create 6 chunks
	chunks := make([]*Chunk, 6)
	for i := 0; i < 6; i++ {
		chunks[i] = &Chunk{
			FileID:          file.FileID,
			ChunkNumber:     i,
			StorageServerID: server.ServerID,
			ChunkSize:       1024 * 1024 * 1024, // 1 GB
			ChunkHash:       fmt.Sprintf("hash%d", i),
		}
	}

	err = storage.CreateChunksBatch(ctx, chunks)
	require.NoError(t, err, "Batch insert should succeed")

	// Verify all 6 chunks were created
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM chunks WHERE file_id = $1", file.FileID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count, "Should have 6 chunks")

	// Verify foreign key constraint
	for _, chunk := range chunks {
		var fkFileID uuid.UUID
		err = pool.QueryRow(ctx, "SELECT file_id FROM chunks WHERE chunk_id = $1", chunk.ChunkID).Scan(&fkFileID)
		require.NoError(t, err)
		assert.Equal(t, file.FileID, fkFileID, "Foreign key should be correct")
	}
}

// TestGetFile_ByID tests file retrieval by ID
func TestGetFile_ByID(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file with chunks
	file := &File{
		Filename:    "test.dat",
		ContentType: "application/octet-stream",
		TotalSize:   6000,
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Create storage server
	server := &StorageServer{GRPCAddress: "localhost:50051"}
	err = storage.CreateStorageServer(ctx, server)
	require.NoError(t, err)

	// Create chunks
	chunks := make([]*Chunk, 6)
	for i := 0; i < 6; i++ {
		chunks[i] = &Chunk{
			FileID:          file.FileID,
			ChunkNumber:     i,
			StorageServerID: server.ServerID,
			ChunkSize:       1000,
			ChunkHash:       fmt.Sprintf("hash%d", i),
		}
	}
	err = storage.CreateChunksBatch(ctx, chunks)
	require.NoError(t, err)

	// Get file with chunks
	retrievedFile, err := storage.GetFileByID(ctx, file.FileID)
	require.NoError(t, err, "Should retrieve file")
	assert.Equal(t, file.FileID, retrievedFile.FileID)
	assert.Equal(t, file.Filename, retrievedFile.Filename)
	assert.Equal(t, file.ContentType, retrievedFile.ContentType)
	assert.Equal(t, file.TotalSize, retrievedFile.TotalSize)

	// Verify chunks are loaded
	assert.Len(t, retrievedFile.Chunks, 6, "Should have 6 chunks")
	for i, chunk := range retrievedFile.Chunks {
		assert.Equal(t, i, chunk.ChunkNumber, "Chunks should be ordered by chunk_number")
	}
}

// TestGetFile_NotFound tests non-existent file retrieval
func TestGetFile_NotFound(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	nonExistentID := uuid.New()
	_, err := storage.GetFileByID(ctx, nonExistentID)
	assert.Error(t, err, "Should return error for non-existent file")
	assert.Equal(t, ErrNotFound, err, "Should return ErrNotFound")
}

// TestUpdateFileStatus tests file status updates
func TestUpdateFileStatus(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file
	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Get initial updated_at
	var initialUpdatedAt time.Time
	err = pool.QueryRow(ctx, "SELECT updated_at FROM files WHERE file_id = $1", file.FileID).Scan(&initialUpdatedAt)
	require.NoError(t, err)

	// Wait a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Update status: pending → uploading
	err = storage.UpdateFileStatus(ctx, file.FileID, "uploading")
	require.NoError(t, err)

	var status string
	var updatedAt time.Time
	err = pool.QueryRow(ctx, "SELECT upload_status, updated_at FROM files WHERE file_id = $1", file.FileID).Scan(&status, &updatedAt)
	require.NoError(t, err)
	assert.Equal(t, "uploading", status)
	assert.True(t, updatedAt.After(initialUpdatedAt), "updated_at should be updated")

	// Update status: uploading → completed
	time.Sleep(10 * time.Millisecond)
	err = storage.UpdateFileStatus(ctx, file.FileID, "completed")
	require.NoError(t, err)

	err = pool.QueryRow(ctx, "SELECT upload_status, updated_at FROM files WHERE file_id = $1", file.FileID).Scan(&status, &updatedAt)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
}

// TestGetChunksByFileID tests chunk retrieval for a file
func TestGetChunksByFileID(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file
	file := &File{
		Filename:    "test.bin",
		ContentType: "application/octet-stream",
		TotalSize:   6000,
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Create storage server
	server := &StorageServer{GRPCAddress: "localhost:50051"}
	err = storage.CreateStorageServer(ctx, server)
	require.NoError(t, err)

	// Create 6 chunks in random order
	chunkNumbers := []int{3, 1, 5, 0, 4, 2}
	for _, num := range chunkNumbers {
		chunk := &Chunk{
			FileID:          file.FileID,
			ChunkNumber:     num,
			StorageServerID: server.ServerID,
			ChunkSize:       1000,
			ChunkHash:       fmt.Sprintf("hash%d", num),
		}
		err = storage.CreateChunk(ctx, chunk)
		require.NoError(t, err)
	}

	// Get chunks
	chunks, err := storage.GetChunksByFileID(ctx, file.FileID)
	require.NoError(t, err)
	assert.Len(t, chunks, 6, "Should return 6 chunks")

	// Verify chunks are sorted by chunk_number
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.ChunkNumber, "Chunks should be sorted by chunk_number")
	}
}

// TestTransaction_Rollback tests transaction rollback
func TestTransaction_Rollback(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Begin transaction
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	// Create file in transaction
	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err = storage.CreateFileInTx(ctx, tx, file)
	require.NoError(t, err)

	// Create storage server
	server := &StorageServer{GRPCAddress: "localhost:50051"}
	err = storage.CreateStorageServerInTx(ctx, tx, server)
	require.NoError(t, err)

	// Create chunk
	chunk := &Chunk{
		FileID:          file.FileID,
		ChunkNumber:     0,
		StorageServerID: server.ServerID,
		ChunkSize:       1024,
		ChunkHash:       "hash0",
	}
	err = storage.CreateChunkInTx(ctx, tx, chunk)
	require.NoError(t, err)

	// Rollback transaction
	err = tx.Rollback(ctx)
	require.NoError(t, err)

	// Verify nothing was saved
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM files WHERE file_id = $1", file.FileID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "File should not exist after rollback")

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM chunks WHERE chunk_id = $1", chunk.ChunkID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Chunk should not exist after rollback")
}

// TestTransaction_Commit tests transaction commit
func TestTransaction_Commit(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Begin transaction
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	// Create file in transaction
	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err = storage.CreateFileInTx(ctx, tx, file)
	require.NoError(t, err)

	// Create storage server
	server := &StorageServer{GRPCAddress: "localhost:50051"}
	err = storage.CreateStorageServerInTx(ctx, tx, server)
	require.NoError(t, err)

	// Create chunk
	chunk := &Chunk{
		FileID:          file.FileID,
		ChunkNumber:     0,
		StorageServerID: server.ServerID,
		ChunkSize:       1024,
		ChunkHash:       "hash0",
	}
	err = storage.CreateChunkInTx(ctx, tx, chunk)
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit(ctx)
	require.NoError(t, err)

	// Verify everything was saved
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM files WHERE file_id = $1", file.FileID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "File should exist after commit")

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM chunks WHERE chunk_id = $1", chunk.ChunkID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Chunk should exist after commit")
}

// TestStorageServerRegistration tests storage server registration
func TestStorageServerRegistration(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Register storage server
	server := &StorageServer{
		GRPCAddress:    "localhost:50051",
		AvailableSpace: 1024 * 1024 * 1024 * 1024, // 1 TB
		UsedSpace:      0,
	}
	err := storage.CreateStorageServer(ctx, server)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, server.ServerID, "ServerID should be generated")

	// Verify server was created
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM storage_servers WHERE server_id = $1", server.ServerID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Create 150 virtual nodes
	err = storage.CreateHashRingNodes(ctx, server.ServerID, 150)
	require.NoError(t, err)

	// Verify 150 nodes were created
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM hash_ring_nodes WHERE server_id = $1", server.ServerID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 150, count, "Should have 150 virtual nodes")
}

// TestStorageServerHeartbeat tests heartbeat updates
func TestStorageServerHeartbeat(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create server
	server := &StorageServer{GRPCAddress: "localhost:50051"}
	err := storage.CreateStorageServer(ctx, server)
	require.NoError(t, err)

	// Get initial heartbeat
	var initialHeartbeat time.Time
	err = pool.QueryRow(ctx, "SELECT last_heartbeat FROM storage_servers WHERE server_id = $1", server.ServerID).Scan(&initialHeartbeat)
	require.NoError(t, err)

	// Wait and update heartbeat
	time.Sleep(100 * time.Millisecond)
	err = storage.UpdateHeartbeat(ctx, server.ServerID)
	require.NoError(t, err)

	// Verify heartbeat was updated
	var updatedHeartbeat time.Time
	err = pool.QueryRow(ctx, "SELECT last_heartbeat FROM storage_servers WHERE server_id = $1", server.ServerID).Scan(&updatedHeartbeat)
	require.NoError(t, err)
	assert.True(t, updatedHeartbeat.After(initialHeartbeat), "Heartbeat should be updated")
}

// TestGetActiveStorageServers tests active server retrieval
func TestGetActiveStorageServers(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create 3 servers
	server1 := &StorageServer{GRPCAddress: "localhost:50051"}
	server2 := &StorageServer{GRPCAddress: "localhost:50052"}
	server3 := &StorageServer{GRPCAddress: "localhost:50053"}

	err := storage.CreateStorageServer(ctx, server1)
	require.NoError(t, err)
	err = storage.CreateStorageServer(ctx, server2)
	require.NoError(t, err)
	err = storage.CreateStorageServer(ctx, server3)
	require.NoError(t, err)

	// Make server3 inactive by setting old heartbeat
	_, err = pool.Exec(ctx, "UPDATE storage_servers SET last_heartbeat = $1 WHERE server_id = $2",
		time.Now().Add(-60*time.Second), server3.ServerID)
	require.NoError(t, err)

	// Get active servers (heartbeat within last 30 seconds)
	activeServers, err := storage.GetActiveStorageServers(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Len(t, activeServers, 2, "Should return only 2 active servers")

	// Verify server3 is not in the list
	for _, server := range activeServers {
		assert.NotEqual(t, server3.ServerID, server.ServerID, "Inactive server should not be returned")
	}
}

// TestUploadSession_Create tests upload session creation
func TestUploadSession_Create(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file
	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Create upload session
	session := &UploadSession{
		FileID: file.FileID,
	}
	err = storage.CreateUploadSession(ctx, session, 1*time.Hour)
	require.NoError(t, err)

	// Verify session was created
	assert.NotEqual(t, uuid.Nil, session.SessionID, "SessionID should be generated")

	// Verify expires_at is set correctly (within 1 second tolerance)
	var expiresAt time.Time
	err = pool.QueryRow(ctx, "SELECT expires_at FROM upload_sessions WHERE session_id = $1", session.SessionID).Scan(&expiresAt)
	require.NoError(t, err)

	expectedExpiry := time.Now().Add(1 * time.Hour)
	diff := expiresAt.Sub(expectedExpiry).Abs()
	assert.Less(t, diff, 2*time.Second, "expires_at should be ~1 hour from now")
}

// TestUploadSession_Cleanup tests expired session cleanup
func TestUploadSession_Cleanup(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	// Create file
	file := &File{
		Filename:    "test.txt",
		ContentType: "text/plain",
		TotalSize:   1024,
	}
	err := storage.CreateFile(ctx, file)
	require.NoError(t, err)

	// Create expired session
	session := &UploadSession{
		FileID: file.FileID,
	}
	err = storage.CreateUploadSession(ctx, session, -1*time.Hour) // Expired 1 hour ago
	require.NoError(t, err)

	// Verify session exists
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM upload_sessions WHERE session_id = $1", session.SessionID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Run cleanup
	deletedCount, err := storage.CleanupExpiredSessions(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, deletedCount, "Should delete 1 expired session")

	// Verify session was deleted
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM upload_sessions WHERE session_id = $1", session.SessionID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Expired session should be deleted")
}

// TestConcurrentWrites tests concurrent file creation
func TestConcurrentWrites(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	storage := NewPostgresStorage(pool)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Launch 10 goroutines to create files concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			file := &File{
				Filename:    fmt.Sprintf("file%d.txt", index),
				ContentType: "text/plain",
				TotalSize:   1024,
			}

			if err := storage.CreateFile(ctx, file); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Verify all 10 files were created
	var count int
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM files").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, numGoroutines, count, "Should have created 10 files")
}
