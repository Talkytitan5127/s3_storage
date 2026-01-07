package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pb "github.com/s3storage/api/proto"
	"github.com/s3storage/internal/retry"
)

const (
	downloadTimeout    = 10 * time.Minute
	downloadBufferSize = 64 * 1024 // 64KB
)

// DownloadFile handles file download requests
func (gw *APIGateway) DownloadFile(c *gin.Context) {
	fileIDStr := c.Param("file_id")

	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid file_id",
			"details": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), downloadTimeout)
	defer cancel()

	// Get file metadata
	file, err := gw.Storage.GetFileByID(ctx, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "file not found",
			"file_id": fileIDStr,
		})
		return
	}

	// Check if file upload is completed
	if file.UploadStatus != "completed" {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "file upload not completed",
			"status": file.UploadStatus,
		})
		return
	}

	// Set response headers
	c.Header("Content-Type", file.ContentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Filename))
	c.Header("Content-Length", fmt.Sprintf("%d", file.TotalSize))

	// Stream chunks to client
	c.Status(http.StatusOK)

	for _, chunk := range file.Chunks {
		// Get storage client
		client, err := gw.getStorageClient(chunk.StorageServerID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":        "storage server unavailable",
				"chunk_number": chunk.ChunkNumber,
				"details":      err.Error(),
			})
			return
		}

		// Get circuit breaker for this server
		cb := gw.getCircuitBreaker(chunk.StorageServerID)

		// Download chunk from storage server with retry and circuit breaker
		downloadErr := cb.Execute(func() error {
			return gw.downloadChunkFromServerWithRetry(ctx, client, chunk.ChunkID.String(), c.Writer)
		})

		if downloadErr != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":        "failed to download chunk",
				"chunk_number": chunk.ChunkNumber,
				"details":      downloadErr.Error(),
			})
			return
		}
	}
}

// downloadChunkFromServer downloads a chunk from a storage server via gRPC
func (gw *APIGateway) downloadChunkFromServer(ctx context.Context, client pb.StorageServiceClient, chunkID string, writer io.Writer) error {
	stream, err := client.GetChunk(ctx, &pb.GetChunkRequest{
		ChunkId: chunkID,
	})
	if err != nil {
		return fmt.Errorf("failed to create download stream: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive chunk data: %w", err)
		}

		if _, err := writer.Write(resp.Data); err != nil {
			return fmt.Errorf("failed to write chunk data: %w", err)
		}
	}

	return nil
}

// downloadChunkFromServerWithRetry downloads a chunk with retry logic
func (gw *APIGateway) downloadChunkFromServerWithRetry(ctx context.Context, client pb.StorageServiceClient, chunkID string, writer io.Writer) error {
	return retry.Do(ctx, gw.RetryConfig, func() error {
		return gw.downloadChunkFromServer(ctx, client, chunkID, writer)
	})
}
