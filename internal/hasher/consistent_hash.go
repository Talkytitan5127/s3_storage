package hasher

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const (
	// DefaultVirtualNodes is the default number of virtual nodes per server
	DefaultVirtualNodes = 150
)

var (
	// ErrNoServersAvailable is returned when no servers are available in the ring
	ErrNoServersAvailable = errors.New("no servers available")
	// ErrServerNotFound is returned when trying to remove a non-existent server
	ErrServerNotFound = errors.New("server not found")
)

// HashNode represents a virtual node on the hash ring
type HashNode struct {
	HashValue uint64
	ServerID  string
}

// Server represents a storage server
type Server struct {
	ID      string
	Address string
}

// HashRing implements consistent hashing with virtual nodes
type HashRing struct {
	nodes        []HashNode
	servers      map[string]*Server
	virtualNodes int
	mu           sync.RWMutex
}

// NewHashRing creates a new hash ring with default virtual nodes
func NewHashRing() *HashRing {
	return &HashRing{
		nodes:        make([]HashNode, 0),
		servers:      make(map[string]*Server),
		virtualNodes: DefaultVirtualNodes,
	}
}

// NewHashRingWithVirtualNodes creates a new hash ring with specified virtual nodes count
func NewHashRingWithVirtualNodes(virtualNodes int) *HashRing {
	return &HashRing{
		nodes:        make([]HashNode, 0),
		servers:      make(map[string]*Server),
		virtualNodes: virtualNodes,
	}
}

// AddServer adds a server to the hash ring with virtual nodes
func (hr *HashRing) AddServer(serverID, address string) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Register server
	hr.servers[serverID] = &Server{
		ID:      serverID,
		Address: address,
	}

	// Create virtual nodes
	for i := 0; i < hr.virtualNodes; i++ {
		virtualKey := fmt.Sprintf("%s#%d", serverID, i)
		hashValue := xxHash([]byte(virtualKey))

		hr.nodes = append(hr.nodes, HashNode{
			HashValue: hashValue,
			ServerID:  serverID,
		})
	}

	// Sort nodes by hash value
	sort.Slice(hr.nodes, func(i, j int) bool {
		return hr.nodes[i].HashValue < hr.nodes[j].HashValue
	})

	return nil
}

// RemoveServer removes a server from the hash ring
func (hr *HashRing) RemoveServer(serverID string) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Check if server exists
	if _, exists := hr.servers[serverID]; !exists {
		return ErrServerNotFound
	}

	// Remove server
	delete(hr.servers, serverID)

	// Remove all virtual nodes for this server
	newNodes := make([]HashNode, 0, len(hr.nodes))
	for _, node := range hr.nodes {
		if node.ServerID != serverID {
			newNodes = append(newNodes, node)
		}
	}
	hr.nodes = newNodes

	return nil
}

// GetServer returns the server ID for a given key using consistent hashing
func (hr *HashRing) GetServer(key string) (string, error) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	if len(hr.nodes) == 0 {
		return "", ErrNoServersAvailable
	}

	// Hash the key
	keyHash := xxHash([]byte(key))

	// Binary search for the first node with hash >= keyHash
	idx := sort.Search(len(hr.nodes), func(i int) bool {
		return hr.nodes[i].HashValue >= keyHash
	})

	// Wrap around if we've gone past the end
	if idx >= len(hr.nodes) {
		idx = 0
	}

	return hr.nodes[idx].ServerID, nil
}

// GetServerInfo returns the server information for a given server ID
func (hr *HashRing) GetServerInfo(serverID string) (*Server, error) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	server, exists := hr.servers[serverID]
	if !exists {
		return nil, ErrServerNotFound
	}

	return server, nil
}

// GetAllServers returns all registered servers
func (hr *HashRing) GetAllServers() []*Server {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	servers := make([]*Server, 0, len(hr.servers))
	for _, server := range hr.servers {
		servers = append(servers, server)
	}

	return servers
}

// xxHash computes xxHash64 for the given data
func xxHash(data []byte) uint64 {
	return xxhash.Sum64(data)
}
