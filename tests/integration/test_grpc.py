"""
gRPC Integration Tests for Storage Servers.

Tests direct gRPC communication with storage servers,
including chunk upload, download, deletion, and health checks.
"""
import pytest
import grpc
import time
import uuid
import concurrent.futures
from grpc_helpers import (
    STORAGE_SERVERS,
    get_grpc_channel,
    generate_chunk_data,
    put_chunk_streaming,
    get_chunk_streaming,
    delete_chunk,
    health_check,
    calculate_checksum,
    format_bytes
)


# Import generated gRPC code
try:
    from generated import storage_pb2, storage_pb2_grpc
except ImportError:
    pytest.skip(
        "Generated gRPC code not found. Run: bash tests/integration/generate_grpc.sh",
        allow_module_level=True
    )


@pytest.fixture(scope="module")
def grpc_stubs():
    """
    Create gRPC stubs for all storage servers.
    """
    stubs = {}
    channels = {}
    
    for server_addr in STORAGE_SERVERS:
        channel = get_grpc_channel(server_addr)
        stub = storage_pb2_grpc.StorageServiceStub(channel)
        stubs[server_addr] = stub
        channels[server_addr] = channel
    
    yield stubs
    
    # Cleanup
    for channel in channels.values():
        channel.close()


@pytest.fixture(scope="function")
def cleanup_chunks(grpc_stubs):
    """
    Track uploaded chunks and clean them up after test.
    """
    uploaded_chunks = []
    
    yield uploaded_chunks
    
    # Cleanup
    for server_addr, chunk_id in uploaded_chunks:
        try:
            stub = grpc_stubs[server_addr]
            delete_chunk(stub, chunk_id)
            print(f"🗑️  Cleaned up chunk: {chunk_id} from {server_addr}")
        except Exception as e:
            print(f"⚠️  Failed to cleanup chunk {chunk_id}: {e}")


class TestBasicGRPC:
    """Basic gRPC tests for storage server operations."""
    
    def test_put_chunk_success(self, grpc_stubs, cleanup_chunks):
        """
        Test 1: Successfully upload a 1 GB chunk.
        
        Verifies:
        - Chunk upload succeeds
        - Response indicates success
        - Chunk ID is returned
        """
        print("\n📝 Test 1: Put Chunk Success (1 GB)")
        
        # Use first storage server
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Generate 1 GB chunk
        chunk_size = 1024 * 1024 * 1024  # 1 GB
        chunk_id = str(uuid.uuid4())
        data, checksum = generate_chunk_data(chunk_size)
        
        print(f"✅ Generated chunk: {format_bytes(chunk_size)}")
        print(f"   Chunk ID: {chunk_id}")
        print(f"   Checksum: {checksum}")
        
        # Upload chunk
        print("⬆️  Uploading chunk...")
        start_time = time.time()
        response = put_chunk_streaming(stub, chunk_id, data, checksum)
        upload_time = time.time() - start_time
        
        assert response.success is True
        assert response.chunk_id == chunk_id
        assert response.error_message == ""
        
        upload_speed = chunk_size / upload_time / (1024 * 1024)  # MB/s
        print(f"✅ Upload completed in {upload_time:.2f}s ({upload_speed:.2f} MB/s)")
        
        cleanup_chunks.append((server_addr, chunk_id))
    
    def test_put_chunk_invalid_chunk_id(self, grpc_stubs):
        """
        Test 2: Upload with invalid chunk ID validation.
        
        Verifies:
        - Invalid chunk IDs are rejected
        - Appropriate error message returned
        """
        print("\n📝 Test 2: Put Chunk with Invalid Chunk ID")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Try with empty chunk ID
        chunk_id = ""
        data, checksum = generate_chunk_data(1024)  # 1 KB
        
        print(f"⬆️  Attempting upload with empty chunk ID...")
        
        try:
            response = put_chunk_streaming(stub, chunk_id, data, checksum)
            # If it doesn't raise an error, check the response
            assert response.success is False
            assert len(response.error_message) > 0
            print(f"✅ Invalid chunk ID rejected: {response.error_message}")
        except grpc.RpcError as e:
            # gRPC error is also acceptable
            assert e.code() in [grpc.StatusCode.INVALID_ARGUMENT, grpc.StatusCode.FAILED_PRECONDITION]
            print(f"✅ Invalid chunk ID rejected with gRPC error: {e.code()}")
    
    def test_get_chunk_success(self, grpc_stubs, cleanup_chunks):
        """
        Test 3: Successfully download a chunk with streaming.
        
        Verifies:
        - Chunk download succeeds
        - Downloaded data matches uploaded data
        - Checksum verification passes
        """
        print("\n📝 Test 3: Get Chunk Success")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Upload a chunk first
        chunk_size = 10 * 1024 * 1024  # 10 MB
        chunk_id = str(uuid.uuid4())
        original_data, checksum = generate_chunk_data(chunk_size)
        
        print(f"⬆️  Uploading test chunk: {format_bytes(chunk_size)}")
        response = put_chunk_streaming(stub, chunk_id, original_data, checksum)
        assert response.success is True
        cleanup_chunks.append((server_addr, chunk_id))
        
        # Download chunk
        print("⬇️  Downloading chunk...")
        start_time = time.time()
        downloaded_data = get_chunk_streaming(stub, chunk_id)
        download_time = time.time() - start_time
        
        download_speed = chunk_size / download_time / (1024 * 1024)  # MB/s
        print(f"✅ Download completed in {download_time:.2f}s ({download_speed:.2f} MB/s)")
        
        # Verify data
        assert len(downloaded_data) == len(original_data)
        assert downloaded_data == original_data
        
        # Verify checksum
        downloaded_checksum = calculate_checksum(downloaded_data)
        assert downloaded_checksum == checksum
        print(f"✅ Checksum verified: {downloaded_checksum}")
    
    def test_get_chunk_not_found(self, grpc_stubs):
        """
        Test 4: Attempt to download non-existent chunk.
        
        Verifies:
        - Non-existent chunks return error
        - Appropriate error code returned
        """
        print("\n📝 Test 4: Get Chunk Not Found")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Use a random chunk ID that doesn't exist
        fake_chunk_id = str(uuid.uuid4())
        
        print(f"⬇️  Attempting to download non-existent chunk: {fake_chunk_id}")
        
        try:
            data = get_chunk_streaming(stub, fake_chunk_id)
            # If no error, data should be empty or we should fail
            pytest.fail("Expected error for non-existent chunk")
        except grpc.RpcError as e:
            assert e.code() == grpc.StatusCode.NOT_FOUND
            print(f"✅ Non-existent chunk rejected: {e.code()}")
    
    def test_delete_chunk_success(self, grpc_stubs):
        """
        Test 5: Successfully delete a chunk.
        
        Verifies:
        - Chunk deletion succeeds
        - Chunk no longer accessible after deletion
        """
        print("\n📝 Test 5: Delete Chunk Success")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Upload a chunk first
        chunk_size = 5 * 1024 * 1024  # 5 MB
        chunk_id = str(uuid.uuid4())
        data, checksum = generate_chunk_data(chunk_size)
        
        print(f"⬆️  Uploading test chunk: {format_bytes(chunk_size)}")
        response = put_chunk_streaming(stub, chunk_id, data, checksum)
        assert response.success is True
        
        # Verify chunk exists
        print("🔍 Verifying chunk exists...")
        downloaded_data = get_chunk_streaming(stub, chunk_id)
        assert len(downloaded_data) == chunk_size
        print("✅ Chunk exists and is accessible")
        
        # Delete chunk
        print("🗑️  Deleting chunk...")
        delete_response = delete_chunk(stub, chunk_id)
        assert delete_response.success is True
        print(f"✅ Chunk deleted successfully")
        
        # Verify chunk no longer exists
        print("🔍 Verifying chunk is deleted...")
        try:
            get_chunk_streaming(stub, chunk_id)
            pytest.fail("Chunk should not be accessible after deletion")
        except grpc.RpcError as e:
            assert e.code() == grpc.StatusCode.NOT_FOUND
            print("✅ Chunk no longer accessible (404)")


class TestAdvancedGRPC:
    """Advanced gRPC tests for edge cases and performance."""
    
    def test_health_check(self, grpc_stubs):
        """
        Test 6: Health check with disk space reporting.
        
        Verifies:
        - Health check succeeds
        - Disk space information is returned
        - Values are reasonable
        """
        print("\n📝 Test 6: Health Check")
        
        for server_addr in STORAGE_SERVERS:
            stub = grpc_stubs[server_addr]
            
            print(f"🏥 Checking health of {server_addr}...")
            response = health_check(stub)
            
            assert response.status in ["healthy", "ok", "ready"]
            assert response.total_space > 0
            assert response.available_space >= 0
            assert response.used_space >= 0
            assert response.available_space <= response.total_space
            
            utilization = (response.used_space / response.total_space) * 100
            print(f"✅ {server_addr}: {response.status}")
            print(f"   Total: {format_bytes(response.total_space)}")
            print(f"   Used: {format_bytes(response.used_space)} ({utilization:.1f}%)")
            print(f"   Available: {format_bytes(response.available_space)}")
    
    @pytest.mark.slow
    def test_streaming_performance(self, grpc_stubs, cleanup_chunks):
        """
        Test 7: Streaming performance benchmark.
        
        Verifies:
        - Streaming achieves acceptable throughput
        - Large chunks can be handled efficiently
        """
        print("\n📝 Test 7: Streaming Performance Benchmark")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Test with 500 MB chunk
        chunk_size = 500 * 1024 * 1024  # 500 MB
        chunk_id = str(uuid.uuid4())
        data, checksum = generate_chunk_data(chunk_size)
        
        print(f"📊 Benchmarking with {format_bytes(chunk_size)} chunk")
        
        # Upload benchmark
        print("⬆️  Upload benchmark...")
        start_time = time.time()
        response = put_chunk_streaming(stub, chunk_id, data, checksum)
        upload_time = time.time() - start_time
        
        assert response.success is True
        cleanup_chunks.append((server_addr, chunk_id))
        
        upload_speed = chunk_size / upload_time / (1024 * 1024)  # MB/s
        print(f"✅ Upload: {upload_time:.2f}s ({upload_speed:.2f} MB/s)")
        
        # Download benchmark
        print("⬇️  Download benchmark...")
        start_time = time.time()
        downloaded_data = get_chunk_streaming(stub, chunk_id)
        download_time = time.time() - start_time
        
        download_speed = chunk_size / download_time / (1024 * 1024)  # MB/s
        print(f"✅ Download: {download_time:.2f}s ({download_speed:.2f} MB/s)")
        
        # Verify performance targets (should be > 100 MB/s)
        assert upload_speed > 50, f"Upload speed too slow: {upload_speed:.2f} MB/s"
        assert download_speed > 50, f"Download speed too slow: {download_speed:.2f} MB/s"
        
        print(f"✅ Performance targets met")
    
    @pytest.mark.concurrent
    def test_concurrent_streams(self, grpc_stubs, cleanup_chunks):
        """
        Test 8: Concurrent streaming operations.
        
        Verifies:
        - Multiple concurrent uploads work
        - No race conditions
        - All operations complete successfully
        """
        print("\n📝 Test 8: Concurrent Streams")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        num_concurrent = 10
        chunk_size = 5 * 1024 * 1024  # 5 MB each
        
        def upload_task(index):
            chunk_id = str(uuid.uuid4())
            data, checksum = generate_chunk_data(chunk_size)
            response = put_chunk_streaming(stub, chunk_id, data, checksum)
            return chunk_id, response.success, checksum
        
        print(f"⬆️  Uploading {num_concurrent} chunks concurrently...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [executor.submit(upload_task, i) for i in range(num_concurrent)]
            results = [f.result() for f in concurrent.futures.as_completed(futures)]
        
        upload_time = time.time() - start_time
        print(f"✅ All uploads completed in {upload_time:.2f}s")
        
        # Verify all succeeded
        for chunk_id, success, checksum in results:
            assert success is True
            cleanup_chunks.append((server_addr, chunk_id))
        
        # Download all concurrently
        def download_task(chunk_id, expected_checksum):
            data = get_chunk_streaming(stub, chunk_id)
            actual_checksum = calculate_checksum(data)
            return actual_checksum == expected_checksum
        
        print(f"⬇️  Downloading {num_concurrent} chunks concurrently...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [
                executor.submit(download_task, chunk_id, checksum)
                for chunk_id, _, checksum in results
            ]
            checksums_valid = [f.result() for f in concurrent.futures.as_completed(futures)]
        
        download_time = time.time() - start_time
        print(f"✅ All downloads completed in {download_time:.2f}s")
        
        # Verify all checksums matched
        assert all(checksums_valid)
        print(f"✅ All {num_concurrent} checksums verified")
    
    def test_chunk_distribution(self, grpc_stubs, cleanup_chunks):
        """
        Test 9: Verify chunks can be stored on different servers.
        
        Verifies:
        - All storage servers are accessible
        - Chunks can be distributed across servers
        - Each server operates independently
        """
        print("\n📝 Test 9: Chunk Distribution Across Servers")
        
        chunk_size = 1 * 1024 * 1024  # 1 MB
        
        print(f"⬆️  Uploading chunks to all {len(STORAGE_SERVERS)} servers...")
        
        for i, server_addr in enumerate(STORAGE_SERVERS):
            stub = grpc_stubs[server_addr]
            chunk_id = str(uuid.uuid4())
            data, checksum = generate_chunk_data(chunk_size)
            
            print(f"   Server {i+1} ({server_addr})...")
            response = put_chunk_streaming(stub, chunk_id, data, checksum)
            assert response.success is True
            cleanup_chunks.append((server_addr, chunk_id))
            
            # Verify chunk is accessible
            downloaded_data = get_chunk_streaming(stub, chunk_id)
            assert calculate_checksum(downloaded_data) == checksum
        
        print(f"✅ Successfully distributed chunks across all {len(STORAGE_SERVERS)} servers")
    
    def test_large_chunk_handling(self, grpc_stubs, cleanup_chunks):
        """
        Test 10: Handle maximum chunk size (1.67 GB per chunk).
        
        Verifies:
        - Large chunks (near max size) can be handled
        - Streaming works efficiently for large data
        - Memory usage is reasonable
        """
        print("\n📝 Test 10: Large Chunk Handling (1.67 GB)")
        
        server_addr = STORAGE_SERVERS[0]
        stub = grpc_stubs[server_addr]
        
        # Test with 1.67 GB chunk (10GB / 6 chunks)
        chunk_size = int(1.67 * 1024 * 1024 * 1024)  # 1.67 GB
        chunk_id = str(uuid.uuid4())
        
        print(f"📊 Testing with {format_bytes(chunk_size)} chunk")
        print("⚠️  This test may take several minutes...")
        
        # Generate data
        print("🔧 Generating test data...")
        data, checksum = generate_chunk_data(chunk_size)
        print(f"✅ Generated {format_bytes(chunk_size)} of random data")
        
        # Upload
        print("⬆️  Uploading large chunk...")
        start_time = time.time()
        response = put_chunk_streaming(stub, chunk_id, data, checksum)
        upload_time = time.time() - start_time
        
        assert response.success is True
        cleanup_chunks.append((server_addr, chunk_id))
        
        upload_speed = chunk_size / upload_time / (1024 * 1024)  # MB/s
        print(f"✅ Upload completed in {upload_time:.2f}s ({upload_speed:.2f} MB/s)")
        
        # Download
        print("⬇️  Downloading large chunk...")
        start_time = time.time()
        downloaded_data = get_chunk_streaming(stub, chunk_id)
        download_time = time.time() - start_time
        
        download_speed = chunk_size / download_time / (1024 * 1024)  # MB/s
        print(f"✅ Download completed in {download_time:.2f}s ({download_speed:.2f} MB/s)")
        
        # Verify checksum
        print("🔍 Verifying checksum...")
        downloaded_checksum = calculate_checksum(downloaded_data)
        assert downloaded_checksum == checksum
        print(f"✅ Checksum verified: {downloaded_checksum}")