# Variant 2 with PostgreSQL: Summary and Key Decisions

## Architecture Choice

**Variant 2: Distributed Architecture with Consistent Hashing** was chosen, adapted to use **PostgreSQL instead of Redis**.

## Why PostgreSQL instead of Redis?

### Key Advantages

#### 1. Reliability and Durability
- **ACID transactions** ensure strict metadata consistency
- **Write-Ahead Log (WAL)** guarantees data won't be lost during failures
- Redis with AOF/RDB has risk of data loss during crashes
- Meets the **strong consistency** requirement from the specification

#### 2. Structured Data
- Relational model is ideal for file and chunk metadata
- Foreign keys ensure referential integrity
- Complex JOIN queries for analytics and reports
- JSONB for flexible storage of additional metadata

#### 3. Operational Advantages
- Mature ecosystem of backup/restore tools
- Rich choice of monitoring tools (pgAdmin, Grafana, Prometheus)
- Simple debugging through SQL queries
- Schema management through migrations

#### 4. Scalability
- Streaming replication for read replicas
- Table partitioning as data grows
- Connection pooling (pgBouncer)
- Vertical scaling + horizontal reads

### Compensating for Redis Disadvantages

**Problem:** Redis is faster for simple operations  
**Solution:** In-memory hash ring cache in API Gateway minimizes DB queries

**Problem:** Redis is better for high-load systems  
**Solution:** 
- Prepared statements and connection pooling
- Indexes on all critical fields
- Batch operations where possible
- PostgreSQL performance is sufficient for MVP

## Key Architectural Decisions

### 1. Consistent Hashing

**Why:** Deterministic chunk distribution with minimal redistribution when adding servers

**How it works:**
- Each storage server is represented by 150 virtual nodes on the hash ring
- Hash function: xxHash (fast and high-quality)
- Chunk is placed on the first server clockwise from its hash value
- When adding a new server, only ~1/N chunks are redistributed

**Advantages:**
- Uniform load distribution
- Easy addition of new storage servers
- Determinism (one chunk always on one server)
- O(log N) server lookup

### 2. gRPC with Streaming

**Why:** Efficient transfer of large files (up to 10 GiB)

**Advantages:**
- Binary protocol (less overhead than REST)
- Bidirectional streaming for upload/download
- HTTP/2 multiplexing
- Type-safe contracts through protobuf
- Built-in support in Go

### 3. Chunking Strategy

**Parameters:**
- Number of chunks: **6** (fixed per requirements)
- Chunk size: `file_size / 6` (equal parts)
- For a 10 GiB file: ~1.67 GiB per chunk

**Process:**
1. API Gateway receives file
2. Splits into 6 equal parts in memory (streaming)
3. For each chunk, calculates storage server via consistent hashing
4. Sends chunks to storage servers in parallel via gRPC
5. Writes metadata to PostgreSQL

### 4. Interrupted Upload Handling

**Problem:** User may disconnect during upload

**Solution:**
- `upload_sessions` table with `expires_at` field
- Upload session created with 1-hour TTL at start
- Background job checks expired sessions every 5 minutes
- Deletes incomplete chunks from storage servers
- Cleans up metadata from DB

**Alternative:** Multipart upload API (like S3) for resume capability

### 5. Storage Server Registration

**Process:**
1. Storage server starts
2. Registers in PostgreSQL (`storage_servers` table)
3. Creates 150 virtual nodes in `hash_ring_nodes`
4. Sends heartbeat every 10 seconds
5. API Gateway periodically updates in-memory hash ring

**Advantages:**
- Dynamic server addition without API Gateway restart
- Automatic detection of unavailable servers
- Centralized topology management

## Database Schema

### Main Tables

**files** - file metadata
- file_id (UUID, PK)
- filename, content_type, total_size
- upload_status (pending, uploading, completed, failed)
- checksum (SHA-256)

**chunks** - file chunk information
- chunk_id (UUID, PK)
- file_id (FK → files)
- chunk_number (0-5)
- storage_server_id (FK → storage_servers)
- chunk_hash (SHA-256)

**storage_servers** - storage server registry
- server_id (UUID, PK)
- grpc_address, status
- available_space, used_space
- last_heartbeat

**hash_ring_nodes** - virtual nodes for consistent hashing
- node_id (UUID, PK)
- server_id (FK → storage_servers)
- virtual_node_index (0-149)
- hash_value (BIGINT)

**upload_sessions** - tracking incomplete uploads
- session_id (UUID, PK)
- file_id (FK → files)
- expires_at, status

## Workflow Examples

### Upload 6 GiB File

```
1. Client → API Gateway: POST /files (multipart/form-data)
2. API Gateway: Generate file_id, create upload_session
3. API Gateway: Split file into 6 chunks (~1 GiB each)
4. API Gateway → PostgreSQL: INSERT files, chunks, upload_session
5. For each chunk (parallel):
   - Calculate storage server via consistent hashing
   - API Gateway → Storage Server: gRPC PutChunk(stream)
   - Storage Server: Save to disk /data/chunks/{chunk_id}
   - API Gateway → PostgreSQL: UPDATE chunk status='completed'
6. API Gateway → PostgreSQL: UPDATE file status='completed'
7. API Gateway → Client: 201 Created {file_id}
```

### Download File

```
1. Client → API Gateway: GET /files/{file_id}
2. API Gateway → PostgreSQL: SELECT file metadata
3. API Gateway → PostgreSQL: SELECT chunks ORDER BY chunk_number
4. API Gateway → Client: Start streaming response
5. For each chunk (sequential):
   - API Gateway → Storage Server: gRPC GetChunk(chunk_id)
   - Storage Server → API Gateway: Stream chunk data
   - API Gateway → Client: Stream to client
6. Complete response
```

### Adding New Storage Server

```
1. Start new storage server (storage-7)
2. Storage-7 → PostgreSQL: INSERT INTO storage_servers
3. Storage-7 → PostgreSQL: INSERT 150 rows INTO hash_ring_nodes
4. Storage-7: Start heartbeat loop (every 10s)
5. API Gateway: Refresh hash ring (every 30s)
6. New chunks automatically distributed to storage-7
7. Old chunks remain on original servers (no rebalancing needed)
```

## Performance

### Expected Characteristics

**Upload:**
- Throughput: ~100-200 MB/s (depends on network and disks)
- Latency: ~50-100ms for metadata + file transfer time
- Concurrent uploads: 10-50 (depends on resources)

**Download:**
- Throughput: ~100-200 MB/s
- Latency: ~50-100ms for metadata + file transfer time

**Database:**
- Metadata queries: <10ms
- Hash ring lookup: <1ms (in-memory cache)
- Transaction commit: ~5-10ms

### Bottlenecks

1. **API Gateway** - single point, all data passes through it
   - Mitigation: Load balancer + multiple instances (future)

2. **PostgreSQL** - single master for writes
   - Mitigation: Connection pooling, query optimization, read replicas

3. **Network bandwidth** - large file transfers
   - Mitigation: gRPC streaming, compression (optional)

4. **Disk I/O** on storage servers
   - Mitigation: SSD disks, RAID for performance

## Security

### Data Protection
- TLS for gRPC connections between components
- PostgreSQL SSL connections
- SHA-256 checksums for integrity verification
- File size validation (max 10 GiB)

### Authentication
- API keys for clients (basic)
- Rate limiting for abuse protection
- IP whitelisting (optional)

### Network Isolation
- Docker bridge network
- Only API Gateway exposed externally (port 8080)
- Storage servers and PostgreSQL in private network

## Monitoring

### API Gateway Metrics
- Active uploads/downloads count
- Request latency (p50, p95, p99)
- Throughput (bytes/sec)
- Error rate by type
- Database connection pool stats

### Storage Server Metrics
- Available disk space
- Chunk count
- gRPC request latency
- I/O errors
- Heartbeat status

### PostgreSQL Metrics
- Connection count
- Query latency
- Transaction rate
- Table/index sizes
- Replication lag (if replicas exist)

### Health Checks
- API Gateway: `GET /health`
- Storage Server: `gRPC HealthCheck()`
- PostgreSQL: `pg_isready`

## Testing

### Unit Tests
- Consistent hashing algorithm (distribution, node addition)
- Chunking logic (file splitting)
- Database operations (CRUD)
- gRPC handlers

### Integration Tests
- End-to-end upload/download
- Interrupted uploads and cleanup
- Adding new storage servers
- Concurrent operations

### Load Tests
- 100 concurrent uploads
- 10 GiB files
- Database performance under load
- Storage server throughput

## Deployment

### Docker Compose
```yaml
services:
  postgres:       # PostgreSQL 15
  api-gateway:    # Go REST API + gRPC client
  storage-1..6:   # Go gRPC servers
```

### Volumes
- `postgres_data` - database
- `storage1_data..storage6_data` - chunks on disks

### Networking
- Bridge network for internal communication
- Exposed ports: 8080 (API), 5432 (PostgreSQL for debugging)

## Roadmap

### MVP (current scope)
- ✅ Basic upload/download functionality
- ✅ Consistent hashing
- ✅ PostgreSQL for metadata
- ✅ gRPC streaming
- ✅ Interrupted upload handling
- ✅ Docker Compose

### Phase 2: Production Readiness
- PostgreSQL replication (master-slave)
- Automatic failover (Patroni/Stolon)
- Comprehensive monitoring (Prometheus + Grafana)
- Alerting (AlertManager)
- Backup automation

### Phase 3: Scalability
- Multiple API Gateway instances + Load Balancer
- Read replicas for PostgreSQL
- CDN integration
- Chunk compression

### Phase 4: Advanced Features
- S3-compatible API (multipart upload)
- File versioning
- Access Control Lists (ACLs)
- Metadata search
- Lifecycle policies (auto-delete old files)

## Comparison with Other Variants

### vs Variant 1 (Centralized PostgreSQL)
**Advantages of Variant 2:**
- ✅ Consistent hashing for uniform distribution
- ✅ gRPC streaming for better performance
- ✅ More thoughtful architecture for scaling

**Disadvantages of Variant 2:**
- ❌ Slightly more complex to implement
- ❌ Requires in-memory hash ring cache

### vs Variant 3 (Microservices)
**Advantages of Variant 2:**
- ✅ Simpler to develop and maintain
- ✅ Fewer components (no Consul, separate services)
- ✅ Faster time-to-market

**Disadvantages of Variant 2:**
- ❌ Less flexibility for independent scaling
- ❌ API Gateway - single point of failure

## Conclusion

Variant 2 with PostgreSQL represents an **optimal balance** between:
- Implementation simplicity (fast MVP)
- Reliability (ACID, durability)
- Performance (consistent hashing, gRPC)
- Scalability (easy to add storage servers)

This solution is ideal for:
- ✅ MVP and quick start
- ✅ Production with moderate load
- ✅ Further evolution into enterprise solution

**Recommendation:** Start with this variant, collect metrics in production, and if necessary evolve towards Variant 3 (microservices) or add components as load grows.