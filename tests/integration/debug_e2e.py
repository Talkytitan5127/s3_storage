#!/usr/bin/env python3
"""
Debug-friendly E2E test script for S3-like Storage System.

This script can be run directly in VS Code debugger or from command line.
Set breakpoints anywhere and step through the code.

Usage:
    python debug_e2e.py                    # Run all tests
    python debug_e2e.py test_small_file    # Run specific test
"""

import sys
import time
import requests
from pathlib import Path

# Add current directory to path for imports
sys.path.insert(0, str(Path(__file__).parent))

from test_helpers import (
    generate_random_file,
    calculate_file_checksum,
    upload_file,
    download_file,
    get_file_metadata,
    list_files,
    delete_file,
    format_bytes
)


# Configuration
API_BASE_URL = "http://localhost:8080"
TEST_DATA_DIR = Path(__file__).parent / "test_data"
TEST_DATA_DIR.mkdir(parents=True, exist_ok=True)


def wait_for_api_ready(max_attempts=30, delay=2):
    """Wait for API Gateway to be ready."""
    health_url = f"{API_BASE_URL}/health"
    
    print(f"🔍 Checking if API Gateway is available at {API_BASE_URL}...")
    for attempt in range(max_attempts):
        try:
            response = requests.get(health_url, timeout=5)
            if response.status_code == 200:
                data = response.json()
                print(f"✅ API Gateway is ready (attempt {attempt + 1})")
                print(f"   Status: {data.get('status')}")
                print(f"   Storage servers: {data.get('storage_servers', 0)}")
                return True
        except requests.exceptions.RequestException as e:
            if attempt == 0:
                print(f"⏳ Waiting for API Gateway... ({e.__class__.__name__})")
        
        if attempt < max_attempts - 1:
            time.sleep(delay)
    
    print(f"❌ API Gateway did not become ready after {max_attempts} attempts")
    print(f"   Please ensure infrastructure is running:")
    print(f"   docker compose -f docker-compose.test.yml up -d --build")
    return False


def get_api_client():
    """Create API client configuration."""
    return {
        "base_url": API_BASE_URL,
        "timeout": 300,  # 5 minutes for large file operations
        "session": requests.Session()
    }


def cleanup_file(api_client, file_id):
    """Delete a file from the system."""
    try:
        session = api_client["session"]
        base_url = api_client["base_url"]
        response = session.delete(f"{base_url}/files/{file_id}", timeout=10)
        if response.status_code == 200:
            print(f"🗑️  Cleaned up file: {file_id}")
            return True
    except Exception as e:
        print(f"⚠️  Failed to cleanup file {file_id}: {e}")
    return False


# ============================================================================
# TEST FUNCTIONS
# ============================================================================

def test_small_file():
    """Test 1: Upload and download a small file (10 MB)."""
    print("\n" + "="*70)
    print("📝 Test 1: Upload/Download Small File (10 MB)")
    print("="*70)
    
    api_client = get_api_client()
    
    # Generate 10 MB test file
    file_size = 10 * 1024 * 1024  # 10 MB
    test_file, original_checksum = generate_random_file(
        file_size,
        TEST_DATA_DIR / "small_file.bin"
    )
    print(f"✅ Generated test file: {format_bytes(file_size)}")
    
    try:
        # Upload file
        print("⬆️  Uploading file...")
        start_time = time.time()
        upload_response = upload_file(api_client, test_file, "small_test.bin")
        upload_time = time.time() - start_time
        
        assert "file_id" in upload_response, "Missing file_id in response"
        assert "checksum" in upload_response, "Missing checksum in response"
        assert upload_response["checksum"] == original_checksum, "Checksum mismatch"
        
        file_id = upload_response["file_id"]
        
        print(f"✅ Upload completed in {upload_time:.2f}s")
        print(f"   File ID: {file_id}")
        print(f"   Checksum: {original_checksum}")
        
        # Download file
        print("⬇️  Downloading file...")
        start_time = time.time()
        downloaded_file = download_file(
            api_client,
            file_id,
            TEST_DATA_DIR / "downloaded_small.bin"
        )
        download_time = time.time() - start_time
        
        print(f"✅ Download completed in {download_time:.2f}s")
        
        # Verify checksum
        downloaded_checksum = calculate_file_checksum(downloaded_file)
        assert downloaded_checksum == original_checksum, "Downloaded file checksum mismatch"
        print(f"✅ Checksum verified: {downloaded_checksum}")
        
        # Cleanup
        downloaded_file.unlink()
        cleanup_file(api_client, file_id)
        
        print("✅ TEST PASSED")
        return True
        
    finally:
        # Always cleanup test file
        if test_file.exists():
            test_file.unlink()


def test_metadata():
    """Test 2: Get file metadata with chunk distribution info."""
    print("\n" + "="*70)
    print("📝 Test 2: Get File Metadata")
    print("="*70)
    
    api_client = get_api_client()
    
    # Upload a test file
    file_size = 50 * 1024 * 1024  # 50 MB
    test_file, original_checksum = generate_random_file(
        file_size,
        TEST_DATA_DIR / "metadata_test.bin"
    )
    
    try:
        print(f"⬆️  Uploading test file: {format_bytes(file_size)}")
        upload_response = upload_file(api_client, test_file, "metadata_test.bin")
        file_id = upload_response["file_id"]
        print(f"✅ Uploaded file: {file_id}")
        
        # Get metadata
        print("📋 Retrieving file metadata...")
        metadata = get_file_metadata(api_client, file_id)
        
        # Verify metadata structure
        assert "file_id" in metadata, "Missing file_id"
        assert "filename" in metadata, "Missing filename"
        assert "size" in metadata, "Missing size"
        assert "checksum" in metadata, "Missing checksum"
        assert "chunks" in metadata, "Missing chunks"
        assert "created_at" in metadata, "Missing created_at"
        
        print(f"✅ Metadata retrieved successfully")
        print(f"   Filename: {metadata['filename']}")
        print(f"   Size: {format_bytes(metadata['size'])}")
        print(f"   Checksum: {metadata['checksum']}")
        print(f"   Chunks: {len(metadata['chunks'])}")
        
        # Verify values
        assert metadata["file_id"] == file_id, "File ID mismatch"
        assert metadata["filename"] == "metadata_test.bin", "Filename mismatch"
        assert metadata["size"] == file_size, "Size mismatch"
        assert metadata["checksum"] == original_checksum, "Checksum mismatch"
        
        # Verify chunk distribution
        for i, chunk in enumerate(metadata["chunks"]):
            assert "chunk_id" in chunk, f"Chunk {i} missing chunk_id"
            assert "server_id" in chunk, f"Chunk {i} missing server_id"
            assert "size" in chunk, f"Chunk {i} missing size"
            print(f"   Chunk {i}: {chunk['chunk_id'][:8]}... -> Server {chunk['server_id'][:8]}... ({format_bytes(chunk['size'])})")
        
        print("✅ All metadata fields verified")
        
        # Cleanup
        cleanup_file(api_client, file_id)
        
        print("✅ TEST PASSED")
        return True
        
    finally:
        if test_file.exists():
            test_file.unlink()


def test_delete():
    """Test 3: Delete file with cascade chunk cleanup."""
    print("\n" + "="*70)
    print("📝 Test 3: Delete File with Cascade Cleanup")
    print("="*70)
    
    api_client = get_api_client()
    
    # Upload a test file
    file_size = 10 * 1024 * 1024  # 10 MB
    test_file, _ = generate_random_file(
        file_size,
        TEST_DATA_DIR / "delete_test.bin"
    )
    
    try:
        print("⬆️  Uploading test file...")
        upload_response = upload_file(api_client, test_file, "delete_test.bin")
        file_id = upload_response["file_id"]
        print(f"✅ Uploaded file: {file_id}")
        
        # Verify file exists
        metadata = get_file_metadata(api_client, file_id)
        assert metadata["file_id"] == file_id, "File not found"
        print("✅ File metadata retrieved successfully")
        
        # Delete file
        print("🗑️  Deleting file...")
        delete_response = delete_file(api_client, file_id)
        assert "message" in delete_response, "Missing message in delete response"
        print(f"✅ Delete response: {delete_response['message']}")
        
        # Verify file no longer exists
        print("🔍 Verifying file is deleted...")
        session = api_client["session"]
        base_url = api_client["base_url"]
        
        response = session.get(
            f"{base_url}/files/{file_id}/metadata",
            timeout=10
        )
        assert response.status_code == 404, "File metadata still accessible"
        print("✅ File metadata no longer accessible (404)")
        
        # Try to download deleted file
        response = session.get(
            f"{base_url}/files/{file_id}",
            timeout=10
        )
        assert response.status_code == 404, "File still downloadable"
        print("✅ File download no longer accessible (404)")
        
        print("✅ TEST PASSED")
        return True
        
    finally:
        if test_file.exists():
            test_file.unlink()


def test_list_files():
    """Test 4: List files with pagination."""
    print("\n" + "="*70)
    print("📝 Test 4: List Files with Pagination")
    print("="*70)
    
    api_client = get_api_client()
    
    # Upload multiple small files
    num_files = 5
    uploaded_ids = []
    
    try:
        print(f"⬆️  Uploading {num_files} test files...")
        for i in range(num_files):
            file_size = 1 * 1024 * 1024  # 1 MB each
            test_file, _ = generate_random_file(
                file_size,
                TEST_DATA_DIR / f"list_test_{i}.bin"
            )
            
            upload_response = upload_file(api_client, test_file, f"list_test_{i}.bin")
            file_id = upload_response["file_id"]
            uploaded_ids.append(file_id)
            test_file.unlink()
        
        print(f"✅ Uploaded {num_files} files")
        
        # List files with pagination
        print("📋 Listing files (page 1, 3 per page)...")
        list_response = list_files(api_client, page=1, per_page=3)
        
        assert "files" in list_response, "Missing files in response"
        assert "total" in list_response, "Missing total in response"
        assert "page" in list_response, "Missing page in response"
        assert "per_page" in list_response, "Missing per_page in response"
        
        assert list_response["page"] == 1, "Page number mismatch"
        assert list_response["per_page"] == 3, "Per page mismatch"
        assert len(list_response["files"]) <= 3, "Too many files returned"
        
        print(f"✅ Page 1: {len(list_response['files'])} files")
        print(f"   Total files in system: {list_response['total']}")
        
        # Verify our uploaded files are in the system
        all_file_ids = [f["file_id"] for f in list_response["files"]]
        
        # Get more pages if needed
        if list_response["total"] > 3:
            print("📋 Listing files (page 2)...")
            list_response_2 = list_files(api_client, page=2, per_page=3)
            all_file_ids.extend([f["file_id"] for f in list_response_2["files"]])
        
        # Check that at least some of our files are in the list
        found_files = [fid for fid in uploaded_ids if fid in all_file_ids]
        assert len(found_files) > 0, "None of uploaded files found in list"
        
        print(f"✅ Found {len(found_files)}/{num_files} uploaded files in list")
        
        print("✅ TEST PASSED")
        return True
        
    finally:
        # Cleanup all uploaded files
        for file_id in uploaded_ids:
            cleanup_file(api_client, file_id)


def test_nonexistent_file():
    """Test 5: Attempt to download non-existent file."""
    print("\n" + "="*70)
    print("📝 Test 5: Download Non-Existent File")
    print("="*70)
    
    api_client = get_api_client()
    
    # Use a random UUID that doesn't exist
    fake_file_id = "00000000-0000-0000-0000-000000000000"
    
    print(f"⬇️  Attempting to download non-existent file: {fake_file_id}")
    session = api_client["session"]
    base_url = api_client["base_url"]
    
    response = session.get(
        f"{base_url}/files/{fake_file_id}",
        timeout=10
    )
    
    # Should fail with 404 Not Found
    assert response.status_code == 404, f"Expected 404, got {response.status_code}"
    error_data = response.json()
    assert "error" in error_data, "Missing error in response"
    
    print(f"✅ 404 error returned as expected: {error_data['error']}")
    print("✅ TEST PASSED")
    return True


# ============================================================================
# MAIN EXECUTION
# ============================================================================

def main():
    """Run all tests or specific test."""
    print("\n" + "="*70)
    print("🚀 S3-like Storage System - Debug E2E Tests")
    print("="*70)
    
    # Check if API is ready
    if not wait_for_api_ready(max_attempts=10, delay=1):
        print("\n❌ Cannot proceed without API Gateway")
        return 1
    
    # Define available tests
    tests = {
        "test_small_file": test_small_file,
        "test_metadata": test_metadata,
        "test_delete": test_delete,
        "test_list_files": test_list_files,
        "test_nonexistent_file": test_nonexistent_file,
    }
    
    # Determine which tests to run
    if len(sys.argv) > 1:
        # Run specific test
        test_name = sys.argv[1]
        if test_name not in tests:
            print(f"\n❌ Unknown test: {test_name}")
            print(f"Available tests: {', '.join(tests.keys())}")
            return 1
        
        tests_to_run = {test_name: tests[test_name]}
    else:
        # Run all tests
        tests_to_run = tests
    
    # Run tests
    results = {}
    for test_name, test_func in tests_to_run.items():
        try:
            result = test_func()
            results[test_name] = "PASSED" if result else "FAILED"
        except AssertionError as e:
            print(f"\n❌ TEST FAILED: {e}")
            results[test_name] = "FAILED"
        except Exception as e:
            print(f"\n❌ TEST ERROR: {e}")
            import traceback
            traceback.print_exc()
            results[test_name] = "ERROR"
    
    # Print summary
    print("\n" + "="*70)
    print("📊 TEST SUMMARY")
    print("="*70)
    for test_name, result in results.items():
        icon = "✅" if result == "PASSED" else "❌"
        print(f"{icon} {test_name}: {result}")
    
    passed = sum(1 for r in results.values() if r == "PASSED")
    total = len(results)
    print(f"\nTotal: {passed}/{total} tests passed")
    
    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())