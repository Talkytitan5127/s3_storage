package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pb "github.com/s3storage/api/proto"
)

// GetFileMetadata handles file metadata requests
func (gw *APIGateway) GetFileMetadata(c *gin.Context) {
	fileIDStr := c.Param("file_id")

	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid file_id",
			"details": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
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

	// Build chunk distribution info
	chunkDistribution := make([]gin.H, 0, len(file.Chunks))
	for _, chunk := range file.Chunks {
		serverInfo, err := gw.HashRing.GetServerInfo(chunk.StorageServerID.String())
		serverAddress := "unknown"
		if err == nil {
			serverAddress = serverInfo.Address
		}

		chunkDistribution = append(chunkDistribution, gin.H{
			"chunk_number":   chunk.ChunkNumber,
			"chunk_id":       chunk.ChunkID,
			"size":           chunk.ChunkSize,
			"server_id":      chunk.StorageServerID,
			"server_address": serverAddress,
			"status":         chunk.Status,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"file_id":      file.FileID,
		"filename":     file.Filename,
		"content_type": file.ContentType,
		"size":         file.TotalSize,
		"status":       file.UploadStatus,
		"checksum":     file.Checksum,
		"created_at":   file.CreatedAt,
		"updated_at":   file.UpdatedAt,
		"completed_at": file.CompletedAt,
		"chunks":       chunkDistribution,
	})
}

// ListFiles handles file listing requests with pagination
func (gw *APIGateway) ListFiles(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Parse pagination parameters
	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	perPage := 20
	if perPageStr := c.Query("per_page"); perPageStr != "" {
		if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 100 {
			perPage = pp
		}
	}

	// Parse filter parameters
	status := c.Query("status")

	// Query files from database
	query := `
		SELECT file_id, filename, content_type, total_size, upload_status, 
		       COALESCE(checksum, ''), created_at, updated_at, completed_at
		FROM files
	`
	args := make([]interface{}, 0)
	argCount := 0

	if status != "" {
		argCount++
		query += ` WHERE upload_status = $` + strconv.Itoa(argCount)
		args = append(args, status)
	}

	query += ` ORDER BY created_at DESC`

	// Add pagination
	argCount++
	query += ` LIMIT $` + strconv.Itoa(argCount)
	args = append(args, perPage)

	argCount++
	query += ` OFFSET $` + strconv.Itoa(argCount)
	args = append(args, (page-1)*perPage)

	rows, err := gw.DB.Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to query files",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	files := make([]gin.H, 0)
	for rows.Next() {
		var fileID uuid.UUID
		var filename, contentType, uploadStatus, checksum string
		var totalSize int64
		var createdAt, updatedAt time.Time
		var completedAt *time.Time

		err := rows.Scan(&fileID, &filename, &contentType, &totalSize, &uploadStatus,
			&checksum, &createdAt, &updatedAt, &completedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to scan file",
				"details": err.Error(),
			})
			return
		}

		files = append(files, gin.H{
			"file_id":      fileID,
			"filename":     filename,
			"content_type": contentType,
			"size":         totalSize,
			"status":       uploadStatus,
			"checksum":     checksum,
			"created_at":   createdAt,
			"updated_at":   updatedAt,
			"completed_at": completedAt,
		})
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "error iterating files",
			"details": err.Error(),
		})
		return
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM files`
	if status != "" {
		countQuery += ` WHERE upload_status = $1`
	}

	var totalCount int64
	var countArgs []interface{}
	if status != "" {
		countArgs = append(countArgs, status)
	}

	if err := gw.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get total count",
			"details": err.Error(),
		})
		return
	}

	totalPages := (totalCount + int64(perPage) - 1) / int64(perPage)

	c.JSON(http.StatusOK, gin.H{
		"files": files,
		"pagination": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_count": totalCount,
			"total_pages": totalPages,
		},
	})
}

// DeleteFile handles file deletion requests
func (gw *APIGateway) DeleteFile(c *gin.Context) {
	fileIDStr := c.Param("file_id")

	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid file_id",
			"details": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()

	// Get file with chunks
	file, err := gw.Storage.GetFileByID(ctx, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "file not found",
			"file_id": fileIDStr,
		})
		return
	}

	// Delete chunks from storage servers
	deletedChunks := 0
	failedChunks := 0

	for _, chunk := range file.Chunks {
		client, err := gw.getStorageClient(chunk.StorageServerID)
		if err != nil {
			failedChunks++
			continue
		}

		_, err = client.DeleteChunk(ctx, &pb.DeleteChunkRequest{
			ChunkId: chunk.ChunkID.String(),
		})
		if err != nil {
			failedChunks++
		} else {
			deletedChunks++
		}
	}

	// Delete file record from database (CASCADE will delete chunks)
	deleteQuery := `DELETE FROM files WHERE file_id = $1`
	result, err := gw.DB.Exec(ctx, deleteQuery, fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to delete file record",
			"details": err.Error(),
		})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "file not found",
			"file_id": fileIDStr,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":        "file deleted successfully",
		"file_id":        fileID,
		"deleted_chunks": deletedChunks,
		"failed_chunks":  failedChunks,
	})
}
