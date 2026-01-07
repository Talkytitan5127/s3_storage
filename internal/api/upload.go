package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pb "github.com/s3storage/api/proto"
	"github.com/s3storage/internal/chunker"
	"github.com/s3storage/internal/retry"
	"github.com/s3storage/internal/storage"
)

const (
	maxFileSize      = 10 * 1024 * 1024 * 1024 // 10GB
	numChunks        = 6
	uploadBufferSize = 64 * 1024 // 64KB
	uploadTimeout    = 5 * time.Minute
)

// UploadFile handles file upload requests
func (gw *APIGateway) UploadFile(c *gin.Context) {
	// Parse multipart form
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil { // 32MB max memory
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "failed to parse multipart form",
			"details": err.Error(),
		})
		return
	}

	// Get file from form
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "file is required",
			"details": err.Error(),
		})
		return
	}
	defer file.Close()

	// Validate file size
	if header.Size > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error":     fmt.Sprintf("file size exceeds maximum allowed size of %d bytes", maxFileSize),
			"file_size": header.Size,
			"max_size":  maxFileSize,
		})
		return
	}

	if header.Size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "file is empty",
		})
		return
	}

	// Get content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), uploadTimeout)
	defer cancel()

	// Create file record in database
	fileRecord := &storage.File{
		FileID:       uuid.New(),
		Filename:     header.Filename,
		ContentType:  contentType,
		TotalSize:    header.Size,
		UploadStatus: "pending",
	}

	if err := gw.Storage.CreateFile(ctx, fileRecord); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to create file record",
			"details": err.Error(),
		})
		return
	}

	// Calculate chunk boundaries
	chunkBoundaries, err := chunker.CalculateChunkBoundaries(header.Size, numChunks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to calculate chunk boundaries",
			"details": err.Error(),
		})
		return
	}

	// Upload chunks to storage servers
	chunks := make([]*storage.Chunk, 0, numChunks)
	fileHasher := sha256.New()

	for i, boundary := range chunkBoundaries {
		// Read chunk data
		chunkData := make([]byte, boundary.Size)
		n, err := io.ReadFull(file, chunkData)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			// Cleanup: update file status to failed
			gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":        "failed to read chunk data",
				"chunk_number": i,
				"details":      err.Error(),
			})
			return
		}
		chunkData = chunkData[:n]

		// Update file hash
		fileHasher.Write(chunkData)

		// Calculate chunk hash
		chunkHash := sha256.Sum256(chunkData)
		chunkHashStr := hex.EncodeToString(chunkHash[:])

		// Generate chunk ID
		chunkID := uuid.New()

		// Determine storage server using consistent hashing
		serverID, err := gw.HashRing.GetServer(chunkID.String())
		if err != nil {
			gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")

			// Get more details about available servers
			allServers := gw.HashRing.GetAllServers()
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":             "no storage servers available",
				"details":           err.Error(),
				"available_servers": len(allServers),
				"chunk_number":      i,
			})
			return
		}

		serverUUID, err := uuid.Parse(serverID)
		if err != nil {
			gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "invalid server ID",
				"details": err.Error(),
			})
			return
		}

		// Get storage client with retry on connection failure
		var client pb.StorageServiceClient
		var clientErr error

		// Try to get client, with one retry if connection is broken
		for attempt := 0; attempt < 2; attempt++ {
			client, clientErr = gw.getStorageClient(serverUUID)
			if clientErr == nil {
				break
			}

			if attempt == 0 {
				// First attempt failed, wait a bit and try again
				time.Sleep(100 * time.Millisecond)
			}
		}

		if clientErr != nil {
			gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":        "failed to get storage client",
				"server_id":    serverID,
				"chunk_number": i,
				"details":      clientErr.Error(),
			})
			return
		}

		// Get circuit breaker for this server
		cb := gw.getCircuitBreaker(serverUUID)

		// Upload chunk to storage server with retry and circuit breaker
		uploadErr := cb.Execute(func() error {
			return gw.UploadChunkToServerWithRetry(ctx, client, chunkID.String(), chunkData, chunkHashStr)
		})

		if uploadErr != nil {
			gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":        "failed to upload chunk to storage server",
				"chunk_number": i,
				"server_id":    serverID,
				"details":      uploadErr.Error(),
			})
			return
		}

		// Create chunk record
		chunk := &storage.Chunk{
			ChunkID:         chunkID,
			FileID:          fileRecord.FileID,
			ChunkNumber:     i,
			StorageServerID: serverUUID,
			ChunkSize:       int64(len(chunkData)),
			ChunkHash:       chunkHashStr,
			Status:          "completed",
		}

		chunks = append(chunks, chunk)
	}

	// Save all chunks to database in batch
	if err := gw.Storage.CreateChunksBatch(ctx, chunks); err != nil {
		gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "failed")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to save chunk records",
			"details": err.Error(),
		})
		return
	}

	// Calculate final file checksum
	fileChecksum := hex.EncodeToString(fileHasher.Sum(nil))

	// Update file status to completed
	if err := gw.Storage.UpdateFileStatus(ctx, fileRecord.FileID, "completed"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to update file status",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"file_id":      fileRecord.FileID,
		"filename":     fileRecord.Filename,
		"size":         fileRecord.TotalSize,
		"content_type": fileRecord.ContentType,
		"checksum":     fileChecksum,
		"chunks":       len(chunks),
		"status":       "completed",
	})
}

// UploadChunkToServer uploads a chunk to a storage server via gRPC
func (gw *APIGateway) UploadChunkToServer(ctx context.Context, client pb.StorageServiceClient, chunkID string, data []byte, checksum string) error {
	stream, err := client.PutChunk(ctx)
	if err != nil {
		return fmt.Errorf("failed to create upload stream: %w", err)
	}

	// Send chunk metadata and data in chunks
	offset := 0
	for offset < len(data) {
		end := offset + uploadBufferSize
		if end > len(data) {
			end = len(data)
		}

		req := &pb.PutChunkRequest{
			ChunkId:  chunkID,
			Data:     data[offset:end],
			Checksum: checksum,
		}

		if err := stream.Send(req); err != nil {
			return fmt.Errorf("failed to send chunk data: %w", err)
		}

		offset = end
	}

	// Close stream and get response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("failed to close stream: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("upload failed on storage server")
	}

	return nil
}

// UploadChunkToServerWithRetry uploads a chunk with retry logic
func (gw *APIGateway) UploadChunkToServerWithRetry(ctx context.Context, client pb.StorageServiceClient, chunkID string, data []byte, checksum string) error {
	return retry.Do(ctx, gw.RetryConfig, func() error {
		return gw.UploadChunkToServer(ctx, client, chunkID, data, checksum)
	})
}
