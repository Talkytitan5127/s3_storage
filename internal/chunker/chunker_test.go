package chunker

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	GiB = 1024 * 1024 * 1024
	MiB = 1024 * 1024
)

// TestSplitFile_ExactDivision tests splitting a file that divides evenly into 6 chunks
func TestSplitFile_ExactDivision(t *testing.T) {
	fileSize := int64(6 * GiB) // 6 GiB

	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err, "Should calculate chunks without error")

	// Should have exactly 6 chunks
	assert.Equal(t, 6, len(chunks), "Should have 6 chunks")

	// Each chunk should be 1 GiB
	expectedChunkSize := int64(1 * GiB)
	for i, chunk := range chunks {
		assert.Equal(t, expectedChunkSize, chunk.Size,
			"Chunk %d should be exactly 1 GiB", i)
		assert.Equal(t, i, chunk.Number,
			"Chunk %d should have correct number", i)
	}

	// Verify sum of sizes equals file size
	totalSize := int64(0)
	for _, chunk := range chunks {
		totalSize += chunk.Size
	}
	assert.Equal(t, fileSize, totalSize,
		"Sum of chunk sizes should equal file size")

	// Verify chunk numbers are 0 to 5
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.Number,
			"Chunk number should be %d", i)
	}

	// Verify offsets are correct
	expectedOffset := int64(0)
	for i, chunk := range chunks {
		assert.Equal(t, expectedOffset, chunk.Offset,
			"Chunk %d should start at offset %d", i, expectedOffset)
		expectedOffset += chunk.Size
	}
}

// TestSplitFile_NotExactDivision tests splitting a file that doesn't divide evenly
func TestSplitFile_NotExactDivision(t *testing.T) {
	fileSize := int64(6*GiB + 512*MiB) // 6.5 GiB

	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	// Should have exactly 6 chunks
	assert.Equal(t, 6, len(chunks), "Should have 6 chunks")

	// With remainder distribution, some chunks will be 1 byte larger
	// For 6.5 GiB = 6979321856 bytes, divided by 6 = 1163220309 base + 2 remainder
	// So first 2 chunks get 1163220310, rest get 1163220309
	baseSize := fileSize / 6
	remainder := fileSize % 6

	for i, chunk := range chunks {
		expectedSize := baseSize
		if int64(i) < remainder {
			expectedSize++
		}
		assert.Equal(t, expectedSize, chunk.Size,
			"Chunk %d should have correct size", i)
		t.Logf("Chunk %d: %d bytes", i, chunk.Size)
	}

	// Verify sum equals file size
	totalSize := int64(0)
	for _, chunk := range chunks {
		totalSize += chunk.Size
	}
	assert.Equal(t, fileSize, totalSize,
		"Sum of chunk sizes should equal file size")

	// Verify no gaps or overlaps
	for i := 1; i < len(chunks); i++ {
		expectedOffset := chunks[i-1].Offset + chunks[i-1].Size
		assert.Equal(t, expectedOffset, chunks[i].Offset,
			"Chunk %d should start where chunk %d ends", i, i-1)
	}
}

// TestSplitFile_SmallFile tests splitting a file smaller than typical chunk size
func TestSplitFile_SmallFile(t *testing.T) {
	fileSize := int64(1 * MiB) // 1 MB

	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	// Should still have 6 chunks
	assert.Equal(t, 6, len(chunks), "Should have 6 chunks even for small file")

	// Some chunks will be very small
	totalSize := int64(0)
	for i, chunk := range chunks {
		assert.Greater(t, chunk.Size, int64(0),
			"Chunk %d should have positive size", i)
		totalSize += chunk.Size
		t.Logf("Chunk %d: %d bytes", i, chunk.Size)
	}

	assert.Equal(t, fileSize, totalSize,
		"Sum of chunk sizes should equal file size")
}

// TestSplitFile_MaxSize tests splitting a 10 GiB file (maximum size)
func TestSplitFile_MaxSize(t *testing.T) {
	fileSize := int64(10 * GiB) // 10 GiB maximum

	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	assert.Equal(t, 6, len(chunks), "Should have 6 chunks")

	// Each chunk should be approximately 1.67 GiB
	expectedChunkSize := fileSize / 6
	t.Logf("Expected chunk size: ~%.2f GiB", float64(expectedChunkSize)/float64(GiB))

	for i, chunk := range chunks {
		t.Logf("Chunk %d: %.2f GiB", i, float64(chunk.Size)/float64(GiB))

		// Verify no int64 overflow
		assert.Greater(t, chunk.Size, int64(0),
			"Chunk %d size should be positive", i)
		assert.LessOrEqual(t, chunk.Size, fileSize,
			"Chunk %d size should not exceed file size", i)
	}

	// Verify sum
	totalSize := int64(0)
	for _, chunk := range chunks {
		totalSize += chunk.Size
	}
	assert.Equal(t, fileSize, totalSize,
		"Sum of chunk sizes should equal file size")
}

// TestSplitFile_Streaming tests that chunking works with streaming (low memory)
func TestSplitFile_Streaming(t *testing.T) {
	// Create a 100 MB test file in memory
	fileSize := int64(100 * MiB)
	data := make([]byte, fileSize)
	_, err := rand.Read(data)
	require.NoError(t, err)

	reader := bytes.NewReader(data)

	// Calculate chunks
	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	// Read each chunk using streaming
	for i, chunkInfo := range chunks {
		// Seek to chunk offset
		_, err := reader.Seek(chunkInfo.Offset, io.SeekStart)
		require.NoError(t, err, "Should seek to chunk %d offset", i)

		// Read chunk data
		chunkData := make([]byte, chunkInfo.Size)
		n, err := io.ReadFull(reader, chunkData)
		require.NoError(t, err, "Should read chunk %d", i)
		assert.Equal(t, int(chunkInfo.Size), n,
			"Should read exact chunk size for chunk %d", i)

		// Verify we read the correct portion
		expectedData := data[chunkInfo.Offset : chunkInfo.Offset+chunkInfo.Size]
		assert.Equal(t, expectedData, chunkData,
			"Chunk %d data should match expected portion", i)
	}
}

// TestCalculateChunkBoundaries tests boundary calculation for various file sizes
func TestCalculateChunkBoundaries(t *testing.T) {
	testCases := []struct {
		name      string
		fileSize  int64
		numChunks int
	}{
		{"1KB", 1024, 6},
		{"1MB", 1 * MiB, 6},
		{"1GB", 1 * GiB, 6},
		{"10GB", 10 * GiB, 6},
		{"Odd size", 12345678, 6},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := CalculateChunkBoundaries(tc.fileSize, tc.numChunks)
			require.NoError(t, err)

			assert.Equal(t, tc.numChunks, len(chunks),
				"Should have %d chunks", tc.numChunks)

			// Verify no gaps
			for i := 1; i < len(chunks); i++ {
				prevEnd := chunks[i-1].Offset + chunks[i-1].Size
				assert.Equal(t, prevEnd, chunks[i].Offset,
					"No gap between chunk %d and %d", i-1, i)
			}

			// Verify no overlaps
			for i := 0; i < len(chunks)-1; i++ {
				assert.LessOrEqual(t, chunks[i].Offset+chunks[i].Size, chunks[i+1].Offset,
					"Chunk %d should not overlap with chunk %d", i, i+1)
			}

			// Verify total coverage
			totalSize := int64(0)
			for _, chunk := range chunks {
				totalSize += chunk.Size
			}
			assert.Equal(t, tc.fileSize, totalSize,
				"Chunks should cover entire file")
		})
	}
}

// TestChunkChecksum_SHA256 tests SHA-256 checksum calculation
func TestChunkChecksum_SHA256(t *testing.T) {
	// Create known data
	data := []byte("Hello, World! This is test data for chunking.")

	// Calculate checksum
	hash := sha256.Sum256(data)
	checksum := fmt.Sprintf("%x", hash)

	t.Logf("Data: %s", string(data))
	t.Logf("SHA-256: %s", checksum)

	// Calculate again to verify determinism
	hash2 := sha256.Sum256(data)
	checksum2 := fmt.Sprintf("%x", hash2)

	assert.Equal(t, checksum, checksum2,
		"Checksum should be deterministic")

	// Modify one byte and verify checksum changes
	modifiedData := make([]byte, len(data))
	copy(modifiedData, data)
	modifiedData[0] = 'X'

	hash3 := sha256.Sum256(modifiedData)
	checksum3 := fmt.Sprintf("%x", hash3)

	assert.NotEqual(t, checksum, checksum3,
		"Checksum should change when data changes")
}

// TestReassembleFile tests splitting and reassembling a file
func TestReassembleFile(t *testing.T) {
	// Create test data
	fileSize := int64(10 * MiB)
	originalData := make([]byte, fileSize)
	_, err := rand.Read(originalData)
	require.NoError(t, err)

	// Calculate original checksum
	originalHash := sha256.Sum256(originalData)
	originalChecksum := fmt.Sprintf("%x", originalHash)

	// Split into chunks
	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	// Extract chunk data
	chunkData := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		chunkData[i] = originalData[chunk.Offset : chunk.Offset+chunk.Size]
	}

	// Reassemble
	reassembled := make([]byte, 0, fileSize)
	for _, data := range chunkData {
		reassembled = append(reassembled, data...)
	}

	// Verify byte-by-byte equality
	assert.Equal(t, originalData, reassembled,
		"Reassembled data should match original")

	// Verify checksum
	reassembledHash := sha256.Sum256(reassembled)
	reassembledChecksum := fmt.Sprintf("%x", reassembledHash)

	assert.Equal(t, originalChecksum, reassembledChecksum,
		"Reassembled checksum should match original")
}

// TestChunkMetadata tests chunk metadata generation
func TestChunkMetadata(t *testing.T) {
	fileSize := int64(6 * GiB)
	fileID := "test-file-123"

	chunks, err := CalculateChunkBoundaries(fileSize, 6)
	require.NoError(t, err)

	// Generate metadata for each chunk
	for i, chunk := range chunks {
		metadata := ChunkMetadata{
			ChunkID:     fmt.Sprintf("%s-chunk-%d", fileID, i),
			ChunkNumber: chunk.Number,
			Size:        chunk.Size,
			Offset:      chunk.Offset,
			Checksum:    "", // Would be calculated from actual data
		}

		// Verify all fields are populated
		assert.NotEmpty(t, metadata.ChunkID,
			"Chunk %d should have ID", i)
		assert.Equal(t, i, metadata.ChunkNumber,
			"Chunk %d should have correct number", i)
		assert.Greater(t, metadata.Size, int64(0),
			"Chunk %d should have positive size", i)
		assert.GreaterOrEqual(t, metadata.Offset, int64(0),
			"Chunk %d should have non-negative offset", i)

		t.Logf("Chunk %d metadata: ID=%s, Size=%d, Offset=%d",
			i, metadata.ChunkID, metadata.Size, metadata.Offset)
	}
}

// TestErrorHandling_CorruptedChunk tests detection of corrupted chunks
func TestErrorHandling_CorruptedChunk(t *testing.T) {
	// Create test data
	data := []byte("This is test data for corruption detection")

	// Calculate correct checksum
	hash := sha256.Sum256(data)
	correctChecksum := fmt.Sprintf("%x", hash)

	// Corrupt the data
	corruptedData := make([]byte, len(data))
	copy(corruptedData, data)
	corruptedData[10] = 'X' // Corrupt one byte

	// Calculate checksum of corrupted data
	corruptedHash := sha256.Sum256(corruptedData)
	corruptedChecksum := fmt.Sprintf("%x", corruptedHash)

	// Verify checksums don't match
	assert.NotEqual(t, correctChecksum, corruptedChecksum,
		"Corrupted data should have different checksum")

	// Simulate checksum verification
	err := VerifyChecksum(corruptedData, correctChecksum)
	assert.Error(t, err, "Should detect checksum mismatch")
	assert.Contains(t, err.Error(), "checksum mismatch",
		"Error should indicate checksum mismatch")
}

// TestChunker_EdgeCases tests edge cases
func TestChunker_EdgeCases(t *testing.T) {
	t.Run("ZeroSize", func(t *testing.T) {
		_, err := CalculateChunkBoundaries(0, 6)
		assert.Error(t, err, "Should error on zero size")
	})

	t.Run("NegativeSize", func(t *testing.T) {
		_, err := CalculateChunkBoundaries(-100, 6)
		assert.Error(t, err, "Should error on negative size")
	})

	t.Run("InvalidChunkCount", func(t *testing.T) {
		_, err := CalculateChunkBoundaries(1000, 0)
		assert.Error(t, err, "Should error on zero chunks")

		_, err = CalculateChunkBoundaries(1000, -1)
		assert.Error(t, err, "Should error on negative chunks")
	})

	t.Run("ExceedsMaxSize", func(t *testing.T) {
		maxSize := int64(10 * GiB)
		_, err := CalculateChunkBoundaries(maxSize+1, 6)
		assert.Error(t, err, "Should error when exceeding max size")
	})
}

// Benchmark for chunk boundary calculation
func BenchmarkCalculateChunkBoundaries(b *testing.B) {
	fileSize := int64(10 * GiB)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CalculateChunkBoundaries(fileSize, 6)
	}
}

// Benchmark for checksum calculation
func BenchmarkChecksumCalculation(b *testing.B) {
	data := make([]byte, 1*GiB)
	rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_ = sha256.Sum256(data)
	}
}
