# Task 4: API Gateway Implementation and E2E Testing

## Status: Ready to Start
**Created:** 2026-01-06  
**Based on:** Task 3 (Partial Completion)  
**Priority:** 🔴 P0 - CRITICAL

---

## Task 3 Completion Summary

### ✅ Completed Components (40% of Task 3)

#### 1. Protobuf Definitions
- **File:** `api/proto/storage.proto`
- **Status:** ✅ Complete
- **Generated Files:**
  - `api/proto/storage.pb.go` - Protocol buffer messages
  - `api/proto/storage_grpc.pb.go` - gRPC service definitions
- **Services Defined:**
  - `PutChunk` - Streaming chunk upload
  - `GetChunk` - Streaming chunk download
  - `DeleteChunk` - Chunk deletion
  - `HealthCheck` - Server health status

#### 2. gRPC Handlers Implementation
- **File:** `internal/grpc/handlers.go` (259 lines)
- **Status:** ✅ Complete
- **Features:**
  - Streaming chunk upload with checksum verification
  - Streaming chunk download with integrity checks
  - Chunk deletion with error handling
  - Health check with disk space reporting
  - Disk full detection (ENOSPC)
  - Automatic subdirectory organization (first 2 chars of chunk ID)
  - 64KB buffer for efficient streaming

#### 3. Storage Server Implementation
- **File:** `cmd/storage-server/main.go` (143 lines)
- **Status:** ✅ Complete
- **Features:**
  - gRPC server with 1GB max message size
  - PostgreSQL integration for metadata
  - Automatic server registration in database
  - 150 virtual nodes for consistent hashing
  - Heartbeat mechanism (10-second interval)
  - Graceful shutdown handling
  - Environment-based configuration
  - Reflection API for debugging

#### 4. Dependencies Updated
- **File:** `go.mod`
- **Added:**
  - `github.com/gin-gonic/gin v1.9.1` - REST API framework
  - `google.golang.org/grpc v1.60.1` - gRPC framework
  - `google.golang.org/protobuf v1.32.0` - Protocol buffers

---

## Remaining Task 3 Components (60%)

### 🚧 Not Yet Implemented

1. **API Gateway** (cmd/api-gateway/main.go)
   - REST API endpoints
   - File upload/download logic
   - Chunking integration
   - Consistent hashing for chunk distribution
   - gRPC client connections to storage servers

2. **Docker Infrastructure**
   - docker-compose.test.yml
   - Dockerfiles for API Gateway and Storage Server
   - Multi-container orchestration

3. **E2E Integration Tests** (10 tests)
   - Upload/Download flows
   - File size limits
   - Error handling
   - Metadata operations

4. **gRPC Handler Tests** (10 tests)
   - Unit tests for handlers
   - Mock streaming
   - Error scenarios

5. **Additional Integration Tests** (15 tests)
   - Interrupted uploads
   - Storage server management
   - Concurrent operations

6. **CI/CD Pipeline**
   - GitHub Actions workflow
   - Automated testing
   - Code coverage reporting

---

## Task 4 Objectives

### Primary Goal
Complete the remaining 60% of Task 3 to achieve a fully functional, production-ready S3-like storage system with comprehensive testing.

### Success Criteria
- ✅ API Gateway fully implemented and tested
- ✅ All 35+ integration tests passing
- ✅ Docker Compose environment working
- ✅ CI/CD pipeline operational
- ✅ Code coverage ≥ 80%
- ✅ System can handle 10GB file uploads
- ✅ E2E tests complete in < 10 minutes

---

## Implementation Plan

### Phase 1: API Gateway (Days 1-3)

#### Day 1: Core API Gateway Structure
**File:** `cmd/api-gateway/main.go`

**Endpoints to implement:**
```go
POST   /files              - Upload file
GET    /files/{file_id}    - Download file
GET    /files/{file_id}/metadata - Get file metadata
GET    /files              - List files
DELETE /files/{file_id}    - Delete file
GET    /health             - Health check
```

**Key Components:**
- Gin router setup
- PostgreSQL connection pool
- gRPC client pool for storage servers
- Consistent hash ring initialization
- Request validation middleware
- Error handling middleware

#### Day 2: Upload Logic
**File:** `internal/api/upload.go`

**Implementation:**
1. Receive multipart file upload
2. Validate file size (max 10GB)
3. Create file record in database
4. Split file into 6 chunks using chunker
5. Use consistent hashing to assign chunks to storage servers
6. Stream chunks to storage servers via gRPC
7. Update file status to "completed"
8. Return file_id to client

**Error Handling:**
- File too large (413)
- Invalid content type (400)
- Storage server unavailable (503)
- Disk full on storage server (507)

#### Day 3: Download and Metadata Logic
**Files:** 
- `internal/api/download.go`
- `internal/api/metadata.go`
- `internal/api/list.go`
- `internal/api/delete.go`

**Download Implementation:**
1. Validate file_id
2. Get file metadata from database
3. Get chunk locations from database
4. Stream chunks from storage servers
5. Reassemble and stream to client

**Metadata Implementation:**
- Return file info (size, type, status, timestamps)
- Include chunk distribution information

**List Implementation:**
- Pagination support (page, per_page)
- Filter by status
- Sort by created_at

**Delete Implementation:**
- Delete file record from database
- Delete chunks from storage servers via gRPC
- CASCADE delete chunks from database

---

### Phase 2: Docker Infrastructure (Days 4-5)

#### Day 4: Docker Compose Configuration
**File:** `docker-compose.test.yml`

**Services:**
1. **postgres** - PostgreSQL 15
   - Auto-initialize with migrations
   - Health check
   - Persistent volume

2. **api-gateway** - REST API
   - Port 8080
   - Depends on postgres
   - Health check endpoint

3. **storage-1 to storage-6** - Storage servers
   - Ports 50051-50056
   - Individual data volumes
   - Depends on postgres
   - Auto-register on startup

**Network:**
- Custom bridge network: s3storage_network
- All services can communicate

#### Day 5: Dockerfiles
**Files:**
- `cmd/api-gateway/Dockerfile`
- `cmd/storage-server/Dockerfile`

**Multi-stage builds:**
1. Builder stage: Compile Go binary
2. Runtime stage: Alpine Linux + binary
3. Minimal image size
4. No unnecessary dependencies

---

### Phase 3: E2E Integration Tests (Days 6-8)

#### Day 6: Basic E2E Tests (5 tests)
**File:** `tests/integration/e2e_test.go`

1. **TestUploadDownload_SmallFile**
   - Upload 10 MB file
   - Verify 201 Created
   - Download file
   - Verify SHA-256 checksum matches

2. **TestUploadDownload_LargeFile**
   - Upload 5 GB file
   - Verify chunks distributed across servers
   - Download and verify

3. **TestUpload_ExceedsMaxSize**
   - Attempt 11 GB upload
   - Verify 413 Payload Too Large

4. **TestDownload_NonExistentFile**
   - GET /files/{invalid-uuid}
   - Verify 404 Not Found

5. **TestListFiles**
   - Upload 10 files
   - GET /files
   - Verify all files listed
   - Test pagination

#### Day 7: Advanced E2E Tests (5 tests)
6. **TestDeleteFile**
   - Upload file
   - DELETE /files/{file_id}
   - Verify file deleted from DB
   - Verify chunks deleted from storage

7. **TestGetFileMetadata**
   - Upload file
   - GET /files/{file_id}/metadata
   - Verify all metadata fields

8. **TestUploadProgress**
   - Upload large file
   - Poll status endpoint
   - Verify status transitions

9. **TestUpload_InvalidContentType**
   - Upload without Content-Type
   - Verify 400 Bad Request

10. **TestUploadDownload_MaxSize**
    - Upload 10 GB file (max)
    - Verify success
    - Download and verify

#### Day 8: Test Infrastructure
- Setup/teardown helpers
- Test data generators
- Docker Compose integration
- Parallel test execution
- Test timeouts and retries

---

### Phase 4: gRPC Handler Tests (Days 9-10)

#### Day 9: gRPC Unit Tests (5 tests)
**File:** `internal/grpc/handlers_test.go`

1. **TestPutChunk_Success**
   - Mock gRPC stream
   - Send 1 GB chunk
   - Verify file created
   - Verify checksum

2. **TestPutChunk_InvalidChunkID**
   - Send empty chunk_id
   - Verify InvalidArgument error

3. **TestPutChunk_DiskFull**
   - Mock disk full condition
   - Verify ResourceExhausted error
   - Verify partial file cleaned up

4. **TestGetChunk_Success**
   - Create test chunk file
   - Stream download
   - Verify data integrity

5. **TestGetChunk_NotFound**
   - Request non-existent chunk
   - Verify NotFound error

#### Day 10: Advanced gRPC Tests (5 tests)
6. **TestGetChunk_CorruptedFile**
   - Create chunk with wrong checksum
   - Attempt download
   - Verify DataLoss error

7. **TestDeleteChunk_Success**
   - Create chunk
   - Delete chunk
   - Verify file removed

8. **TestHealthCheck**
   - Call HealthCheck
   - Verify disk space reported

9. **TestStreamingPerformance**
   - Benchmark 1 GB chunk upload
   - Verify throughput > 100 MB/s

10. **TestConcurrentStreams**
    - 10 simultaneous PutChunk calls
    - Verify no race conditions
    - Verify all succeed

---

### Phase 5: Additional Integration Tests (Days 11-12)

#### Day 11: Interrupted Upload Tests (5 tests)
**File:** `tests/integration/interrupted_upload_test.go`

1. **TestInterruptedUpload_ClientDisconnect**
   - Start upload
   - Disconnect client mid-stream
   - Verify cleanup

2. **TestInterruptedUpload_ServerCrash**
   - Start upload
   - Kill storage server
   - Verify retry logic

3. **TestInterruptedUpload_NetworkTimeout**
   - Simulate network timeout
   - Verify timeout handling

4. **TestCleanupJob_ExpiredSessions**
   - Create expired sessions
   - Run cleanup
   - Verify sessions deleted

5. **TestCleanupJob_ActiveSessions**
   - Create active sessions
   - Run cleanup
   - Verify sessions preserved

#### Day 12: Storage Management Tests (5 tests)
**File:** `tests/integration/storage_management_test.go`

1. **TestAddStorageServer_Dynamic**
   - Add new storage server
   - Verify hash ring updated
   - Verify new chunks distributed

2. **TestRemoveStorageServer**
   - Remove storage server
   - Verify hash ring updated
   - Verify chunks redistributed

3. **TestStorageServerFailover**
   - Kill storage server
   - Verify heartbeat timeout
   - Verify server marked inactive

4. **TestHeartbeatMechanism**
   - Monitor heartbeats
   - Verify 10-second interval
   - Verify timestamp updates

5. **TestHashRingRefresh**
   - Modify hash ring
   - Verify consistent hashing
   - Verify chunk distribution

---

### Phase 6: Concurrent Operations Tests (Day 13)

**File:** `tests/integration/concurrent_test.go`

1. **TestConcurrentUploads**
   - 50 goroutines uploading
   - Verify no race conditions
   - Verify all succeed

2. **TestConcurrentDownloads**
   - 100 goroutines downloading
   - Verify no deadlocks
   - Verify correct data

3. **TestMixedOperations**
   - Simultaneous uploads, downloads, deletes, lists
   - Verify consistency
   - Verify no data corruption

4. **TestDatabaseConnectionPool**
   - 100 concurrent DB queries
   - Verify pool management
   - Verify no connection leaks

5. **TestRaceConditions**
   - Run with `-race` flag
   - Verify no race conditions detected

---

### Phase 7: CI/CD Pipeline (Day 14)

#### GitHub Actions Workflow
**File:** `.github/workflows/test.yml`

**Jobs:**
1. **unit-tests**
   - Run all unit tests
   - Generate coverage report
   - Upload to Codecov

2. **integration-tests**
   - Start PostgreSQL service
   - Run integration tests
   - Collect logs on failure

3. **e2e-tests**
   - Start Docker Compose
   - Run E2E tests
   - Collect container logs
   - Cleanup containers

4. **lint**
   - Run golangci-lint
   - Check code formatting
   - Verify no errors

**Triggers:**
- Push to main/develop
- Pull requests
- Manual workflow dispatch

---

## Technical Specifications

### API Gateway Architecture

```go
type APIGateway struct {
    router          *gin.Engine
    db              *pgxpool.Pool
    storageClients  map[uuid.UUID]*grpc.ClientConn
    hashRing        *hasher.ConsistentHash
    chunker         *chunker.Chunker
}
```

### Request Flow

1. **Upload:**
   ```
   Client → API Gateway → Chunker → Consistent Hash → Storage Servers
                ↓
           PostgreSQL (metadata)
   ```

2. **Download:**
   ```
   Client ← API Gateway ← Storage Servers
                ↑
           PostgreSQL (chunk locations)
   ```

### Error Handling Strategy

- **Client Errors (4xx):**
  - 400 Bad Request - Invalid input
  - 404 Not Found - Resource doesn't exist
  - 413 Payload Too Large - File exceeds 10GB

- **Server Errors (5xx):**
  - 500 Internal Server Error - Unexpected error
  - 503 Service Unavailable - Storage server down
  - 507 Insufficient Storage - Disk full

### Performance Targets

- **Upload Speed:** > 100 MB/s per file
- **Download Speed:** > 100 MB/s per file
- **Concurrent Uploads:** 50+ simultaneous
- **Concurrent Downloads:** 100+ simultaneous
- **API Latency:** < 100ms (excluding transfer time)
- **Database Query Time:** < 10ms average

---

## Testing Strategy

### Test Pyramid

```
        E2E Tests (10)
       /              \
    Integration (25)
   /                    \
Unit Tests (36 existing)
```

### Coverage Goals

- **Unit Tests:** 90%+ coverage
- **Integration Tests:** 80%+ coverage
- **E2E Tests:** Critical paths covered
- **Overall:** 85%+ coverage

### Test Data

- **Small Files:** 1 KB - 10 MB
- **Medium Files:** 100 MB - 1 GB
- **Large Files:** 5 GB - 10 GB
- **Chunk Sizes:** ~1.67 GB each (for 10GB file)

---

## Deliverables

### Code
- [ ] API Gateway implementation (500+ lines)
- [ ] Upload/Download handlers (300+ lines)
- [ ] Docker Compose configuration
- [ ] 2 Dockerfiles
- [ ] 35+ integration tests (1500+ lines)
- [ ] CI/CD workflow

### Documentation
- [ ] API documentation (OpenAPI/Swagger)
- [ ] Deployment guide
- [ ] Testing guide
- [ ] Architecture diagrams

### Infrastructure
- [ ] Working Docker Compose setup
- [ ] CI/CD pipeline
- [ ] Test environment

---

## Risk Mitigation

### Risk 1: Large File Handling
**Mitigation:** 
- Stream processing (no full file in memory)
- Chunking to distribute load
- Progress tracking

### Risk 2: Storage Server Failures
**Mitigation:**
- Heartbeat monitoring
- Automatic failover
- Retry logic with exponential backoff

### Risk 3: Database Bottlenecks
**Mitigation:**
- Connection pooling
- Batch operations
- Indexes on critical fields

### Risk 4: Test Flakiness
**Mitigation:**
- Proper cleanup between tests
- Retry logic for network operations
- Increased timeouts for slow operations

---

## Success Metrics

### After Task 4 Completion:

- **Code Coverage:** ≥ 85%
- **Total Tests:** 71+ (36 existing + 35 new)
- **Test Execution Time:** < 10 minutes
- **Docker Build Time:** < 5 minutes
- **API Response Time:** < 100ms (metadata operations)
- **File Upload Speed:** > 100 MB/s
- **System Uptime:** 99.9% (in tests)

---

## Next Steps After Task 4

### Task 5: Production Hardening
- Monitoring (Prometheus + Grafana)
- Structured logging
- Distributed tracing (Jaeger)
- Security hardening
- Performance optimization
- Load testing

### Task 6: Advanced Features
- Multipart upload API (S3-compatible)
- File versioning
- Access Control Lists (ACLs)
- Lifecycle policies
- CDN integration
- Encryption at rest

---

## Timeline

**Total Duration:** 14 days (2 weeks)

- **Week 1:** API Gateway + Docker + Basic E2E Tests
- **Week 2:** Advanced Tests + CI/CD + Documentation

**Estimated Effort:** 80-100 hours

---

## Conclusion

Task 4 represents the completion of the core S3-like storage system. Upon completion, the system will be:

✅ Fully functional with REST API  
✅ Distributed across multiple storage servers  
✅ Comprehensively tested (85%+ coverage)  
✅ Containerized and deployable  
✅ CI/CD automated  
✅ Production-ready (with Task 5 hardening)

The foundation laid in Tasks 1-3 (consistent hashing, chunking, database operations, gRPC handlers, storage server) provides a solid base for completing the remaining components in Task 4.

---

**Status:** Ready to Start  
**Priority:** 🔴 P0 - CRITICAL  
**Blocking:** Production Release  
**Dependencies:** Task 3 (40% complete)