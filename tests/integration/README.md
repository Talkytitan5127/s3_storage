# Integration Tests

Comprehensive integration tests for the S3-like storage system using Python and pytest.

## Overview

This test suite includes:
- **E2E Tests** (10 tests): End-to-end API testing
- **gRPC Tests** (10 tests): Direct storage server testing
- **Concurrent Tests** (5 tests): Concurrency and race condition testing
- **Total**: 25+ integration tests

## Prerequisites

- Docker and Docker Compose
- Python 3.11+
- Go 1.21+ (for building services)

## Setup

### 1. Install Python Dependencies

```bash
cd tests/integration
pip install -r requirements.txt
```

### 2. Generate gRPC Code

```bash
bash generate_grpc.sh
```

### 3. Start Services

```bash
# From project root
docker-compose -f docker-compose.test.yml up -d --build
```

Wait for services to be ready (~30 seconds).

## Running Tests

### Run All Tests

```bash
cd tests/integration
pytest -v
```

### Run Specific Test Suites

```bash
# E2E tests only
pytest test_e2e.py -v

# gRPC tests only
pytest test_grpc.py -v

# Concurrent tests only
pytest test_concurrent.py -v
```

### Run Tests by Marker

```bash
# Skip slow tests
pytest -v -m "not slow"

# Run only concurrent tests
pytest -v -m concurrent

# Run only large file tests
pytest -v -m large_file
```

### Run Specific Tests

```bash
# Run a single test
pytest test_e2e.py::TestBasicE2E::test_upload_download_small_file -v

# Run a test class
pytest test_e2e.py::TestBasicE2E -v
```

### Parallel Execution

```bash
# Run tests in parallel (4 workers)
pytest -v -n 4
```

## Test Categories

### E2E Tests (`test_e2e.py`)

**Basic Tests:**
1. `test_upload_download_small_file` - 10 MB file upload/download
2. `test_upload_download_large_file` - 5 GB file upload/download (slow)
3. `test_upload_exceeds_max_size` - 11 GB rejection
4. `test_download_nonexistent_file` - 404 handling
5. `test_list_files` - Pagination

**Advanced Tests:**
6. `test_delete_file` - Cascade deletion
7. `test_get_file_metadata` - Metadata accuracy
8. `test_upload_invalid_content_type` - Validation
9. `test_upload_download_max_size` - 10 GB boundary (slow)
10. `test_concurrent_operations` - Concurrent upload/download

### gRPC Tests (`test_grpc.py`)

**Basic Tests:**
1. `test_put_chunk_success` - 1 GB chunk upload
2. `test_put_chunk_invalid_chunk_id` - Validation
3. `test_get_chunk_success` - Streaming download
4. `test_get_chunk_not_found` - Missing chunk
5. `test_delete_chunk_success` - Chunk removal

**Advanced Tests:**
6. `test_health_check` - Disk space reporting
7. `test_streaming_performance` - Throughput benchmark (slow)
8. `test_concurrent_streams` - Race condition check
9. `test_chunk_distribution` - Multi-server distribution
10. `test_large_chunk_handling` - 1.67 GB chunk (slow)

### Concurrent Tests (`test_concurrent.py`)

1. `test_concurrent_uploads` - 50 concurrent uploads
2. `test_concurrent_downloads` - 100 concurrent downloads
3. `test_mixed_operations` - Upload/download/delete mix
4. `test_database_connection_pool` - Pool management
5. `test_race_conditions` - Race detector

## Test Markers

- `@pytest.mark.slow` - Tests that take > 1 minute
- `@pytest.mark.large_file` - Tests using files > 1 GB
- `@pytest.mark.concurrent` - Concurrency tests

## Configuration

### Environment Variables

```bash
# API Gateway URL (default: http://localhost:8080)
export API_BASE_URL=http://localhost:8080

# Test timeout (default: 300 seconds)
export PYTEST_TIMEOUT=300
```

### Docker Compose

The test environment uses `docker-compose.test.yml` which includes:
- PostgreSQL database
- API Gateway (port 8080)
- 6 Storage Servers (ports 50051-50056)

## Troubleshooting

### Services Not Ready

If tests fail with connection errors:

```bash
# Check service health
curl http://localhost:8080/health

# View logs
docker-compose -f docker-compose.test.yml logs

# Restart services
docker-compose -f docker-compose.test.yml restart
```

### Cleanup

```bash
# Stop and remove all containers
docker-compose -f docker-compose.test.yml down -v

# Remove test data
rm -rf tests/integration/test_data
```

### Disk Space

Large file tests require significant disk space:
- 5 GB test: ~10 GB free space
- 10 GB test: ~20 GB free space

Skip slow tests if disk space is limited:
```bash
pytest -v -m "not slow"
```

## CI/CD Integration

Tests are automatically run in GitHub Actions:

```yaml
# .github/workflows/test.yml
- Unit tests (Go)
- Integration tests (Python)
- E2E tests (Docker Compose)
- Code coverage reporting
```

## Performance Targets

### API Gateway
- Upload throughput: > 100 MB/s
- Download throughput: > 100 MB/s
- Metadata operations: < 50ms p99

### Storage Servers
- Chunk write: > 200 MB/s
- Chunk read: > 200 MB/s

### Database
- Query latency: < 10ms average
- Transaction throughput: > 1000 TPS

## Coverage Goals

Target code coverage: **85%+**

```bash
# Generate coverage report
pytest --cov=. --cov-report=html --cov-report=term

# View HTML report
open htmlcov/index.html
```

## Contributing

When adding new tests:

1. Follow existing test structure
2. Add appropriate markers (`@pytest.mark.slow`, etc.)
3. Include cleanup in fixtures
4. Document test purpose in docstring
5. Update this README

## Support

For issues or questions:
- Check logs: `docker-compose -f docker-compose.test.yml logs`
- Review test output: `pytest -v --tb=long`
- File an issue with logs and test output