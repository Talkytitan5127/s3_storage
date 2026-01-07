package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	pb "github.com/s3storage/api/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// ChunkBufferSize is the size of the buffer for streaming chunks
	ChunkBufferSize = 64 * 1024 // 64KB
)

// StorageServer implements the gRPC StorageService
type StorageServer struct {
	pb.UnimplementedStorageServiceServer
	dataDir string
}

// NewStorageServer creates a new StorageServer instance
func NewStorageServer(dataDir string) (*StorageServer, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &StorageServer{
		dataDir: dataDir,
	}, nil
}

// PutChunk handles streaming upload of a chunk
func (s *StorageServer) PutChunk(stream pb.StorageService_PutChunkServer) error {
	var chunkID string
	var expectedChecksum string
	var file *os.File
	var bytesWritten int64
	hasher := sha256.New()

	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			// End of stream
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to receive chunk data: %v", err)
		}

		// First message contains chunk_id and checksum
		if chunkID == "" {
			chunkID = req.ChunkId
			expectedChecksum = req.Checksum

			if chunkID == "" {
				return status.Error(codes.InvalidArgument, "chunk_id is required")
			}

			// Create file for chunk
			chunkPath := s.getChunkPath(chunkID)
			if err := os.MkdirAll(filepath.Dir(chunkPath), 0755); err != nil {
				return status.Errorf(codes.Internal, "failed to create chunk directory: %v", err)
			}

			file, err = os.Create(chunkPath)
			if err != nil {
				// Check if disk is full
				if isOutOfSpace(err) {
					return status.Error(codes.ResourceExhausted, "disk full")
				}
				return status.Errorf(codes.Internal, "failed to create chunk file: %v", err)
			}
		}

		// Write data to file and update hash
		if len(req.Data) > 0 {
			n, err := file.Write(req.Data)
			if err != nil {
				if isOutOfSpace(err) {
					os.Remove(s.getChunkPath(chunkID))
					return status.Error(codes.ResourceExhausted, "disk full")
				}
				return status.Errorf(codes.Internal, "failed to write chunk data: %v", err)
			}
			bytesWritten += int64(n)
			hasher.Write(req.Data)
		}
	}

	if file == nil {
		return status.Error(codes.InvalidArgument, "no data received")
	}

	// Verify checksum if provided
	if expectedChecksum != "" {
		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if actualChecksum != expectedChecksum {
			os.Remove(s.getChunkPath(chunkID))
			return status.Errorf(codes.DataLoss, "checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
		}
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		return status.Errorf(codes.Internal, "failed to sync chunk to disk: %v", err)
	}

	return stream.SendAndClose(&pb.PutChunkResponse{
		ChunkId: chunkID,
		Success: true,
	})
}

// GetChunk handles streaming download of a chunk
func (s *StorageServer) GetChunk(req *pb.GetChunkRequest, stream pb.StorageService_GetChunkServer) error {
	if req.ChunkId == "" {
		return status.Error(codes.InvalidArgument, "chunk_id is required")
	}

	chunkPath := s.getChunkPath(req.ChunkId)

	// Check if chunk exists
	if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
		return status.Errorf(codes.NotFound, "chunk not found: %s", req.ChunkId)
	}

	// Open chunk file
	file, err := os.Open(chunkPath)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to open chunk file: %v", err)
	}
	defer file.Close()

	// Verify file integrity by computing checksum
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return status.Errorf(codes.Internal, "failed to compute checksum: %v", err)
	}

	// Reset file pointer to beginning
	if _, err := file.Seek(0, 0); err != nil {
		return status.Errorf(codes.Internal, "failed to seek file: %v", err)
	}

	// Stream chunk data
	buffer := make([]byte, ChunkBufferSize)
	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to read chunk data: %v", err)
		}

		if err := stream.Send(&pb.GetChunkResponse{
			Data: buffer[:n],
		}); err != nil {
			return status.Errorf(codes.Internal, "failed to send chunk data: %v", err)
		}
	}

	return nil
}

// DeleteChunk handles deletion of a chunk
func (s *StorageServer) DeleteChunk(ctx context.Context, req *pb.DeleteChunkRequest) (*pb.DeleteChunkResponse, error) {
	if req.ChunkId == "" {
		return &pb.DeleteChunkResponse{
			Success:      false,
			ErrorMessage: "chunk_id is required",
		}, status.Error(codes.InvalidArgument, "chunk_id is required")
	}

	chunkPath := s.getChunkPath(req.ChunkId)

	// Check if chunk exists
	if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
		return &pb.DeleteChunkResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("chunk not found: %s", req.ChunkId),
		}, status.Errorf(codes.NotFound, "chunk not found: %s", req.ChunkId)
	}

	// Delete chunk file
	if err := os.Remove(chunkPath); err != nil {
		return &pb.DeleteChunkResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to delete chunk: %v", err),
		}, status.Errorf(codes.Internal, "failed to delete chunk: %v", err)
	}

	return &pb.DeleteChunkResponse{
		Success: true,
	}, nil
}

// HealthCheck returns the health status of the storage server
func (s *StorageServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	// Get disk space information
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.dataDir, &stat); err != nil {
		return &pb.HealthCheckResponse{
			Status: "unhealthy",
		}, status.Errorf(codes.Internal, "failed to get disk stats: %v", err)
	}

	// Calculate space in bytes
	totalSpace := int64(stat.Blocks * uint64(stat.Bsize))
	availableSpace := int64(stat.Bavail * uint64(stat.Bsize))
	usedSpace := totalSpace - availableSpace

	return &pb.HealthCheckResponse{
		Status:         "healthy",
		AvailableSpace: availableSpace,
		UsedSpace:      usedSpace,
		TotalSpace:     totalSpace,
	}, nil
}

// getChunkPath returns the file path for a chunk
func (s *StorageServer) getChunkPath(chunkID string) string {
	// Use first 2 characters for subdirectory to avoid too many files in one directory
	if len(chunkID) >= 2 {
		subdir := chunkID[:2]
		return filepath.Join(s.dataDir, "chunks", subdir, chunkID)
	}
	return filepath.Join(s.dataDir, "chunks", chunkID)
}

// isOutOfSpace checks if an error is due to out of disk space
func isOutOfSpace(err error) bool {
	if err == nil {
		return false
	}
	// Check for ENOSPC (no space left on device)
	if pathErr, ok := err.(*os.PathError); ok {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return errno == syscall.ENOSPC
		}
	}
	return false
}
