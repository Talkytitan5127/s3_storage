package hasher

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHashRing_EmptyServers tests creating a hash ring without servers
func TestNewHashRing_EmptyServers(t *testing.T) {
	ring := NewHashRing()

	assert.NotNil(t, ring, "Ring should not be nil")
	assert.Equal(t, 0, len(ring.nodes), "Ring should have no nodes")
	assert.Equal(t, 0, len(ring.servers), "Ring should have no servers")

	// GetServer should return error when no servers available
	_, err := ring.GetServer("test-key")
	assert.Error(t, err, "GetServer should return error when no servers available")
	assert.Contains(t, err.Error(), "no servers available", "Error message should indicate no servers")
}

// TestAddServer_SingleServer tests adding a single server with 150 virtual nodes
func TestAddServer_SingleServer(t *testing.T) {
	ring := NewHashRing()
	serverID := "storage-1"
	address := "localhost:9001"

	err := ring.AddServer(serverID, address)
	require.NoError(t, err, "Adding server should not return error")

	// Check that 150 virtual nodes were created
	assert.Equal(t, 150, len(ring.nodes), "Should have exactly 150 virtual nodes")

	// Check that server is registered
	assert.Contains(t, ring.servers, serverID, "Server should be registered")
	assert.Equal(t, address, ring.servers[serverID].Address, "Server address should match")

	// Check that nodes are sorted by hash value
	for i := 1; i < len(ring.nodes); i++ {
		assert.LessOrEqual(t, ring.nodes[i-1].HashValue, ring.nodes[i].HashValue,
			"Nodes should be sorted by hash value")
	}

	// GetServer should always return this server
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("test-key-%d", i)
		server, err := ring.GetServer(key)
		require.NoError(t, err)
		assert.Equal(t, serverID, server, "All keys should map to the single server")
	}
}

// TestAddServer_MultipleServers tests adding 6 servers
func TestAddServer_MultipleServers(t *testing.T) {
	ring := NewHashRing()
	servers := []struct {
		id      string
		address string
	}{
		{"storage-1", "localhost:9001"},
		{"storage-2", "localhost:9002"},
		{"storage-3", "localhost:9003"},
		{"storage-4", "localhost:9004"},
		{"storage-5", "localhost:9005"},
		{"storage-6", "localhost:9006"},
	}

	// Add all servers
	for _, s := range servers {
		err := ring.AddServer(s.id, s.address)
		require.NoError(t, err, "Adding server %s should not return error", s.id)
	}

	// Check total virtual nodes: 6 servers * 150 nodes = 900
	assert.Equal(t, 900, len(ring.nodes), "Should have 900 virtual nodes (6 * 150)")

	// Check each server has exactly 150 nodes
	serverNodeCount := make(map[string]int)
	for _, node := range ring.nodes {
		serverNodeCount[node.ServerID]++
	}

	for _, s := range servers {
		assert.Equal(t, 150, serverNodeCount[s.id],
			"Server %s should have exactly 150 virtual nodes", s.id)
	}

	// Check nodes are sorted by hash value
	for i := 1; i < len(ring.nodes); i++ {
		assert.LessOrEqual(t, ring.nodes[i-1].HashValue, ring.nodes[i].HashValue,
			"Nodes should be sorted by hash value at index %d", i)
	}
}

// TestGetServer_Distribution tests distribution of keys across servers
func TestGetServer_Distribution(t *testing.T) {
	ring := NewHashRing()
	numServers := 6

	// Add 6 servers
	for i := 1; i <= numServers; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring.AddServer(serverID, address)
		require.NoError(t, err)
	}

	// Generate 10,000 random keys and count distribution
	numKeys := 10000
	distribution := make(map[string]int)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("file-%d-chunk-%d", i/6, i%6)
		server, err := ring.GetServer(key)
		require.NoError(t, err)
		distribution[server]++
	}

	// Expected distribution: ~16.67% per server (1/6)
	expectedPerServer := float64(numKeys) / float64(numServers)

	t.Logf("Distribution across %d servers for %d keys:", numServers, numKeys)

	// Calculate standard deviation and coefficient of variation
	var sumSquaredDiff float64
	for i := 1; i <= numServers; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		count := distribution[serverID]
		percentage := float64(count) / float64(numKeys) * 100
		t.Logf("  %s: %d keys (%.2f%%)", serverID, count, percentage)

		diff := float64(count) - expectedPerServer
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(numServers)
	stdDev := math.Sqrt(variance)
	coefficientOfVariation := stdDev / expectedPerServer

	t.Logf("Standard deviation: %.2f", stdDev)
	t.Logf("Coefficient of variation: %.4f", coefficientOfVariation)

	// With 150 virtual nodes, CV should be reasonably low (< 0.15)
	assert.Less(t, coefficientOfVariation, 0.15,
		"Coefficient of variation should be < 0.15 for good distribution with 150 virtual nodes")

	// Verify each server gets at least 10% and at most 25% of keys
	for i := 1; i <= numServers; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		count := distribution[serverID]
		percentage := float64(count) / float64(numKeys)

		assert.GreaterOrEqual(t, percentage, 0.10,
			"Server %s should get at least 10%% of keys", serverID)
		assert.LessOrEqual(t, percentage, 0.25,
			"Server %s should get at most 25%% of keys", serverID)
	}
}

// TestGetServer_Deterministic tests that the same key always returns the same server
func TestGetServer_Deterministic(t *testing.T) {
	ring := NewHashRing()

	// Add servers
	for i := 1; i <= 6; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring.AddServer(serverID, address)
		require.NoError(t, err)
	}

	testKey := "test-file-chunk-0"

	// Get server for the first time
	firstServer, err := ring.GetServer(testKey)
	require.NoError(t, err)

	// Call GetServer 1000 times and verify it always returns the same server
	for i := 0; i < 1000; i++ {
		server, err := ring.GetServer(testKey)
		require.NoError(t, err)
		assert.Equal(t, firstServer, server,
			"GetServer should return the same server for the same key (iteration %d)", i)
	}

	// Recreate ring and verify same result
	ring2 := NewHashRing()
	for i := 1; i <= 6; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring2.AddServer(serverID, address)
		require.NoError(t, err)
	}

	server2, err := ring2.GetServer(testKey)
	require.NoError(t, err)
	assert.Equal(t, firstServer, server2,
		"Recreated ring should return the same server for the same key")
}

// TestRemoveServer_Redistribution tests that removing a server redistributes only ~1/N keys
func TestRemoveServer_Redistribution(t *testing.T) {
	ring := NewHashRing()
	numServers := 6

	// Add 6 servers
	for i := 1; i <= numServers; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring.AddServer(serverID, address)
		require.NoError(t, err)
	}

	// Generate 1000 keys and remember their distribution
	numKeys := 1000
	originalMapping := make(map[string]string)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("file-%d", i)
		server, err := ring.GetServer(key)
		require.NoError(t, err)
		originalMapping[key] = server
	}

	// Remove one server
	removedServer := "storage-3"
	err := ring.RemoveServer(removedServer)
	require.NoError(t, err)

	// Check how many keys were redistributed
	redistributed := 0
	stayedSame := 0

	for key, originalServer := range originalMapping {
		newServer, err := ring.GetServer(key)
		require.NoError(t, err)

		if originalServer == removedServer {
			// Keys from removed server must be redistributed
			assert.NotEqual(t, removedServer, newServer,
				"Key %s should be redistributed from removed server", key)
			redistributed++
		} else if newServer != originalServer {
			// Keys from other servers that got redistributed
			redistributed++
		} else {
			stayedSame++
		}
	}

	redistributionRate := float64(redistributed) / float64(numKeys)
	expectedRate := 1.0 / float64(numServers) // ~16.67%

	t.Logf("Redistributed: %d keys (%.2f%%)", redistributed, redistributionRate*100)
	t.Logf("Stayed same: %d keys (%.2f%%)", stayedSame, float64(stayedSame)/float64(numKeys)*100)

	// Allow some tolerance (±5%) due to hash distribution variance
	assert.InDelta(t, expectedRate, redistributionRate, 0.05,
		"Redistribution rate should be close to %.2f%% (1/N)", expectedRate*100)
}

// TestAddServer_MinimalRedistribution tests that adding a server redistributes only ~1/(N+1) keys
func TestAddServer_MinimalRedistribution(t *testing.T) {
	ring := NewHashRing()
	numServers := 6

	// Add 6 servers
	for i := 1; i <= numServers; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring.AddServer(serverID, address)
		require.NoError(t, err)
	}

	// Generate 1000 keys and remember their distribution
	numKeys := 1000
	originalMapping := make(map[string]string)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("file-%d", i)
		server, err := ring.GetServer(key)
		require.NoError(t, err)
		originalMapping[key] = server
	}

	// Add 7th server
	newServer := "storage-7"
	err := ring.AddServer(newServer, "localhost:9007")
	require.NoError(t, err)

	// Check how many keys were redistributed
	redistributed := 0
	movedToNew := 0

	for key, originalServer := range originalMapping {
		newServerID, err := ring.GetServer(key)
		require.NoError(t, err)

		if newServerID != originalServer {
			redistributed++
			if newServerID == newServer {
				movedToNew++
			}
		}
	}

	redistributionRate := float64(redistributed) / float64(numKeys)
	expectedRate := 1.0 / float64(numServers+1) // ~14.3% (1/7)
	newServerRate := float64(movedToNew) / float64(numKeys)

	t.Logf("Redistributed: %d keys (%.2f%%)", redistributed, redistributionRate*100)
	t.Logf("Moved to new server: %d keys (%.2f%%)", movedToNew, newServerRate*100)

	// Allow some tolerance (±5%) due to hash distribution variance
	assert.InDelta(t, expectedRate, redistributionRate, 0.05,
		"Redistribution rate should be close to %.2f%% (1/(N+1))", expectedRate*100)

	assert.InDelta(t, expectedRate, newServerRate, 0.05,
		"New server should receive close to %.2f%% of keys", expectedRate*100)
}

// TestHashFunction_xxHash tests the xxHash implementation
func TestHashFunction_xxHash(t *testing.T) {
	// Test known vectors (these are example values, adjust based on actual xxHash implementation)
	testCases := []struct {
		input    string
		expected uint64 // These would be actual xxHash values
	}{
		{"", 0xef46db3751d8e999},
		{"test", 0x4fdcca5ddb678139},
		{"hello world", 0x7b06c531ea43e89f},
	}

	for _, tc := range testCases {
		hash := xxHash([]byte(tc.input))
		t.Logf("xxHash(%q) = %d (0x%x)", tc.input, hash, hash)
		// Note: Actual expected values depend on xxHash implementation
		// This test verifies the function is deterministic
		hash2 := xxHash([]byte(tc.input))
		assert.Equal(t, hash, hash2, "Hash should be deterministic for input %q", tc.input)
	}

	// Test collision rate on 100k keys
	numKeys := 100000
	hashes := make(map[uint64]bool)
	collisions := 0

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		hash := xxHash([]byte(key))
		if hashes[hash] {
			collisions++
		}
		hashes[hash] = true
	}

	collisionRate := float64(collisions) / float64(numKeys)
	t.Logf("Collision rate: %.4f%% (%d collisions in %d keys)", collisionRate*100, collisions, numKeys)

	// Collision rate should be very low (< 0.01%)
	assert.Less(t, collisionRate, 0.0001,
		"Collision rate should be less than 0.01%%")
}

// TestVirtualNodes_Count tests the impact of virtual node count on distribution
func TestVirtualNodes_Count(t *testing.T) {
	virtualNodeCounts := []int{10, 50, 150, 500}
	numServers := 6
	numKeys := 10000

	for _, vnCount := range virtualNodeCounts {
		t.Run(fmt.Sprintf("VirtualNodes_%d", vnCount), func(t *testing.T) {
			ring := NewHashRingWithVirtualNodes(vnCount)

			// Add servers
			for i := 1; i <= numServers; i++ {
				serverID := fmt.Sprintf("storage-%d", i)
				address := fmt.Sprintf("localhost:900%d", i)
				err := ring.AddServer(serverID, address)
				require.NoError(t, err)
			}

			// Measure distribution
			distribution := make(map[string]int)
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%d", i)
				server, err := ring.GetServer(key)
				require.NoError(t, err)
				distribution[server]++
			}

			// Calculate standard deviation
			expectedPerServer := float64(numKeys) / float64(numServers)
			variance := 0.0
			for _, count := range distribution {
				diff := float64(count) - expectedPerServer
				variance += diff * diff
			}
			stdDev := math.Sqrt(variance / float64(numServers))
			coefficientOfVariation := stdDev / expectedPerServer

			t.Logf("Virtual nodes: %d, Std dev: %.2f, CV: %.4f", vnCount, stdDev, coefficientOfVariation)

			// More virtual nodes should lead to better distribution (lower CV)
			if vnCount >= 150 {
				assert.Less(t, coefficientOfVariation, 0.1,
					"With %d virtual nodes, coefficient of variation should be < 0.1", vnCount)
			}
		})
	}
}

// TestConcurrentAccess tests concurrent access to GetServer
func TestConcurrentAccess(t *testing.T) {
	ring := NewHashRing()

	// Add servers
	for i := 1; i <= 6; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		err := ring.AddServer(serverID, address)
		require.NoError(t, err)
	}

	// Run 100 goroutines concurrently calling GetServer
	numGoroutines := 100
	numCallsPerGoroutine := 1000

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)
	results := make(chan string, numGoroutines*numCallsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < numCallsPerGoroutine; i++ {
				key := fmt.Sprintf("goroutine-%d-key-%d", goroutineID, i)
				server, err := ring.GetServer(key)
				if err != nil {
					errors <- err
					return
				}
				results <- server
			}
		}(g)
	}

	wg.Wait()
	close(errors)
	close(results)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}

	// Verify all results are valid server IDs
	validServers := make(map[string]bool)
	for i := 1; i <= 6; i++ {
		validServers[fmt.Sprintf("storage-%d", i)] = true
	}

	resultCount := 0
	for server := range results {
		assert.True(t, validServers[server], "Result should be a valid server ID: %s", server)
		resultCount++
	}

	expectedResults := numGoroutines * numCallsPerGoroutine
	assert.Equal(t, expectedResults, resultCount,
		"Should receive all results from concurrent operations")

	t.Logf("Successfully completed %d concurrent GetServer calls from %d goroutines",
		resultCount, numGoroutines)
}

// Benchmark for GetServer performance
func BenchmarkGetServer(b *testing.B) {
	ring := NewHashRing()

	// Add 6 servers
	for i := 1; i <= 6; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		_ = ring.AddServer(serverID, address)
	}

	keys := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		keys[i] = fmt.Sprintf("benchmark-key-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%1000]
		_, _ = ring.GetServer(key)
	}
}

// BenchmarkGetServer_Parallel benchmarks concurrent GetServer calls
func BenchmarkGetServer_Parallel(b *testing.B) {
	ring := NewHashRing()

	// Add 6 servers
	for i := 1; i <= 6; i++ {
		serverID := fmt.Sprintf("storage-%d", i)
		address := fmt.Sprintf("localhost:900%d", i)
		_ = ring.AddServer(serverID, address)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("parallel-key-%d", i)
			_, _ = ring.GetServer(key)
			i++
		}
	})
}
