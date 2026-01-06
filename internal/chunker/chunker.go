package chunker

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	// MaxFileSize is the maximum allowed file size (10 GiB)
	MaxFileSize = 10 * 1024 * 1024 * 1024
)

var (
	// ErrInvalidFileSize is returned when file size is invalid
	ErrInvalidFileSize = errors.New("invalid file size")
	// ErrInvalidChunkCount is returned when chunk count is invalid
	ErrInvalidChunkCount = errors.New("invalid chunk count")
	// ErrFileTooLarge is returned when file exceeds maximum size
	ErrFileTooLarge = errors.New("file size exceeds maximum allowed size")
	// ErrChecksumMismatch is returned when checksum verification fails
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

// ChunkInfo represents information about a file chunk
type ChunkInfo struct {
	Number int
	Offset int64
	Size   int64
}

// ChunkMetadata represents metadata for a chunk
type ChunkMetadata struct {
	ChunkID     string
	ChunkNumber int
	Size        int64
	Offset      int64
	Checksum    string
}

// CalculateChunkBoundaries calculates the boundaries for splitting a file into chunks
func CalculateChunkBoundaries(fileSize int64, numChunks int) ([]ChunkInfo, error) {
	// Validate inputs
	if fileSize <= 0 {
		return nil, ErrInvalidFileSize
	}

	if fileSize > MaxFileSize {
		return nil, ErrFileTooLarge
	}

	if numChunks <= 0 {
		return nil, ErrInvalidChunkCount
	}

	chunks := make([]ChunkInfo, numChunks)

	// Calculate base chunk size
	baseChunkSize := fileSize / int64(numChunks)
	remainder := fileSize % int64(numChunks)

	offset := int64(0)
	for i := 0; i < numChunks; i++ {
		chunkSize := baseChunkSize

		// Distribute remainder across first chunks
		if int64(i) < remainder {
			chunkSize++
		}

		chunks[i] = ChunkInfo{
			Number: i,
			Offset: offset,
			Size:   chunkSize,
		}

		offset += chunkSize
	}

	return chunks, nil
}

// CalculateChecksum calculates SHA-256 checksum for data
func CalculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// VerifyChecksum verifies that data matches the expected checksum
func VerifyChecksum(data []byte, expectedChecksum string) error {
	actualChecksum := CalculateChecksum(data)
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrChecksumMismatch, expectedChecksum, actualChecksum)
	}
	return nil
}
