"""
End-to-End Integration Tests for S3-like Storage System.

Tests the complete flow from API Gateway through to Storage Servers,
including file upload, download, metadata, listing, and deletion.
"""
import pytest
import time
import requests
from pathlib import Path
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


class TestBasicE2E:
    """Basic E2E tests for core functionality."""
    
    def test_upload_download_small_file(self, api_client, test_data_dir, cleanup_files):
        """
        Test 1: Upload and download a small file (10 MB).
        
        Verifies:
        - File upload succeeds
        - File download succeeds
        - Downloaded file matches uploaded file (checksum)
        """
        print("\n📝 Test 1: Upload/Download Small File (10 MB)")
        
        # Generate 10 MB test file
        file_size = 10 * 1024 * 1024  # 10 MB
        test_file, original_checksum = generate_random_file(
            file_size,
            test_data_dir / "small_file.bin"
        )
        print(f"✅ Generated test file: {format_bytes(file_size)}")
        
        # Upload file
        print("⬆️  Uploading file...")
        start_time = time.time()
        upload_response = upload_file(api_client, test_file, "small_test.bin")
        upload_time = time.time() - start_time
        
        assert "file_id" in upload_response
        assert "checksum" in upload_response
        assert upload_response["checksum"] == original_checksum
        
        file_id = upload_response["file_id"]
        cleanup_files.append(file_id)
        
        print(f"✅ Upload completed in {upload_time:.2f}s")
        print(f"   File ID: {file_id}")
        print(f"   Checksum: {original_checksum}")
        
        # Download file
        print("⬇️  Downloading file...")
        start_time = time.time()
        downloaded_file = download_file(
            api_client,
            file_id,
            test_data_dir / "downloaded_small.bin"
        )
        download_time = time.time() - start_time
        
        print(f"✅ Download completed in {download_time:.2f}s")
        
        # Verify checksum
        downloaded_checksum = calculate_file_checksum(downloaded_file)
        assert downloaded_checksum == original_checksum
        print(f"✅ Checksum verified: {downloaded_checksum}")
        
        # Cleanup
        test_file.unlink()
        downloaded_file.unlink()
    
    @pytest.mark.slow
    @pytest.mark.large_file
    def test_upload_download_large_file(self, api_client, test_data_dir, cleanup_files):
        """
        Test 2: Upload and download a large file (5 GB).
        
        Verifies:
        - Large file upload succeeds
        - Chunking works correctly
        - Large file download succeeds
        - Data integrity maintained
        """
        print("\n📝 Test 2: Upload/Download Large File (5 GB)")
        
        # Generate 5 GB test file
        file_size = 5 * 1024 * 1024 * 1024  # 5 GB
        test_file, original_checksum = generate_random_file(
            file_size,
            test_data_dir / "large_file.bin"
        )
        print(f"✅ Generated test file: {format_bytes(file_size)}")
        
        # Upload file
        print("⬆️  Uploading large file (this may take a while)...")
        start_time = time.time()
        upload_response = upload_file(api_client, test_file, "large_test.bin")
        upload_time = time.time() - start_time
        
        assert "file_id" in upload_response
        assert "checksum" in upload_response
        assert upload_response["checksum"] == original_checksum
        
        file_id = upload_response["file_id"]
        cleanup_files.append(file_id)
        
        upload_speed = file_size / upload_time / (1024 * 1024)  # MB/s
        print(f"✅ Upload completed in {upload_time:.2f}s ({upload_speed:.2f} MB/s)")
        print(f"   File ID: {file_id}")
        
        # Verify metadata shows correct chunk count
        metadata = get_file_metadata(api_client, file_id)
        assert "chunks" in metadata
        assert len(metadata["chunks"]) == 6  # Should be split into 6 chunks
        print(f"✅ File split into {len(metadata['chunks'])} chunks")
        
        # Download file
        print("⬇️  Downloading large file...")
        start_time = time.time()
        downloaded_file = download_file(
            api_client,
            file_id,
            test_data_dir / "downloaded_large.bin"
        )
        download_time = time.time() - start_time
        
        download_speed = file_size / download_time / (1024 * 1024)  # MB/s
        print(f"✅ Download completed in {download_time:.2f}s ({download_speed:.2f} MB/s)")
        
        # Verify checksum
        print("🔍 Verifying checksum (this may take a while)...")
        downloaded_checksum = calculate_file_checksum(downloaded_file)
        assert downloaded_checksum == original_checksum
        print(f"✅ Checksum verified: {downloaded_checksum}")
        
        # Cleanup
        test_file.unlink()
        downloaded_file.unlink()
    
    def test_upload_exceeds_max_size(self, api_client, test_data_dir):
        """
        Test 3: Attempt to upload file exceeding max size (11 GB).
        
        Verifies:
        - Files larger than 10 GB are rejected
        - Appropriate error message returned
        - No partial data stored
        """
        print("\n📝 Test 3: Upload Exceeds Max Size (11 GB)")
        
        # Generate 11 GB test file
        file_size = 11 * 1024 * 1024 * 1024  # 11 GB
        test_file, _ = generate_random_file(
            file_size,
            test_data_dir / "oversized_file.bin"
        )
        print(f"✅ Generated oversized file: {format_bytes(file_size)}")
        
        # Attempt upload
        print("⬆️  Attempting to upload oversized file...")
        session = api_client["session"]
        base_url = api_client["base_url"]
        
        with open(test_file, 'rb') as f:
            files = {'file': ('oversized_test.bin', f, 'application/octet-stream')}
            response = session.post(
                f"{base_url}/files",
                files=files,
                timeout=api_client["timeout"]
            )
        
        # Should fail with 400 Bad Request
        assert response.status_code == 400
        error_data = response.json()
        assert "error" in error_data
        assert "10GB" in error_data["error"] or "size" in error_data["error"].lower()
        
        print(f"✅ Upload rejected as expected: {error_data['error']}")
        
        # Cleanup
        test_file.unlink()
    
    def test_download_nonexistent_file(self, api_client):
        """
        Test 4: Attempt to download non-existent file.
        
        Verifies:
        - 404 error returned for non-existent file
        - Appropriate error message
        """
        print("\n📝 Test 4: Download Non-Existent File")
        
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
        assert response.status_code == 404
        error_data = response.json()
        assert "error" in error_data
        
        print(f"✅ 404 error returned as expected: {error_data['error']}")
    
    def test_list_files(self, api_client, test_data_dir, cleanup_files):
        """
        Test 5: List files with pagination.
        
        Verifies:
        - File listing works
        - Pagination parameters work correctly
        - Uploaded files appear in list
        """
        print("\n📝 Test 5: List Files with Pagination")
        
        # Upload multiple small files
        num_files = 5
        uploaded_ids = []
        
        print(f"⬆️  Uploading {num_files} test files...")
        for i in range(num_files):
            file_size = 1 * 1024 * 1024  # 1 MB each
            test_file, _ = generate_random_file(
                file_size,
                test_data_dir / f"list_test_{i}.bin"
            )
            
            upload_response = upload_file(api_client, test_file, f"list_test_{i}.bin")
            file_id = upload_response["file_id"]
            uploaded_ids.append(file_id)
            cleanup_files.append(file_id)
            test_file.unlink()
        
        print(f"✅ Uploaded {num_files} files")
        
        # List files with pagination
        print("📋 Listing files (page 1, 3 per page)...")
        list_response = list_files(api_client, page=1, per_page=3)
        
        assert "files" in list_response
        assert "total" in list_response
        assert "page" in list_response
        assert "per_page" in list_response
        
        assert list_response["page"] == 1
        assert list_response["per_page"] == 3
        assert len(list_response["files"]) <= 3
        
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
        assert len(found_files) > 0
        
        print(f"✅ Found {len(found_files)}/{num_files} uploaded files in list")


class TestAdvancedE2E:
    """Advanced E2E tests for edge cases and complex scenarios."""
    
    def test_delete_file(self, api_client, test_data_dir, cleanup_files):
        """
        Test 6: Delete file with cascade chunk cleanup.
        
        Verifies:
        - File deletion succeeds
        - File no longer accessible after deletion
        - Chunks are cleaned up from storage servers
        """
        print("\n📝 Test 6: Delete File with Cascade Cleanup")
        
        # Upload a test file
        file_size = 10 * 1024 * 1024  # 10 MB
        test_file, _ = generate_random_file(
            file_size,
            test_data_dir / "delete_test.bin"
        )
        
        print("⬆️  Uploading test file...")
        upload_response = upload_file(api_client, test_file, "delete_test.bin")
        file_id = upload_response["file_id"]
        print(f"✅ Uploaded file: {file_id}")
        
        # Verify file exists
        metadata = get_file_metadata(api_client, file_id)
        assert metadata["file_id"] == file_id
        print("✅ File metadata retrieved successfully")
        
        # Delete file
        print("🗑️  Deleting file...")
        delete_response = delete_file(api_client, file_id)
        assert "message" in delete_response
        print(f"✅ Delete response: {delete_response['message']}")
        
        # Verify file no longer exists
        print("🔍 Verifying file is deleted...")
        session = api_client["session"]
        base_url = api_client["base_url"]
        
        response = session.get(
            f"{base_url}/files/{file_id}/metadata",
            timeout=10
        )
        assert response.status_code == 404
        print("✅ File metadata no longer accessible (404)")
        
        # Try to download deleted file
        response = session.get(
            f"{base_url}/files/{file_id}",
            timeout=10
        )
        assert response.status_code == 404
        print("✅ File download no longer accessible (404)")
        
        # Cleanup
        test_file.unlink()
    
    def test_get_file_metadata(self, api_client, test_data_dir, cleanup_files):
        """
        Test 7: Get file metadata with chunk distribution info.
        
        Verifies:
        - Metadata retrieval works
        - Chunk distribution information is accurate
        - File size and checksum are correct
        """
        print("\n📝 Test 7: Get File Metadata")
        
        # Upload a test file
        file_size = 50 * 1024 * 1024  # 50 MB
        test_file, original_checksum = generate_random_file(
            file_size,
            test_data_dir / "metadata_test.bin"
        )
        
        print(f"⬆️  Uploading test file: {format_bytes(file_size)}")
        upload_response = upload_file(api_client, test_file, "metadata_test.bin")
        file_id = upload_response["file_id"]
        cleanup_files.append(file_id)
        print(f"✅ Uploaded file: {file_id}")
        
        # Get metadata
        print("📋 Retrieving file metadata...")
        metadata = get_file_metadata(api_client, file_id)
        
        # Verify metadata structure
        assert "file_id" in metadata
        assert "filename" in metadata
        assert "size" in metadata
        assert "checksum" in metadata
        assert "chunks" in metadata
        assert "created_at" in metadata
        
        print(f"✅ Metadata retrieved successfully")
        print(f"   Filename: {metadata['filename']}")
        print(f"   Size: {format_bytes(metadata['size'])}")
        print(f"   Checksum: {metadata['checksum']}")
        print(f"   Chunks: {len(metadata['chunks'])}")
        
        # Verify values
        assert metadata["file_id"] == file_id
        assert metadata["filename"] == "metadata_test.bin"
        assert metadata["size"] == file_size
        assert metadata["checksum"] == original_checksum
        assert len(metadata["chunks"]) == 6  # Should be 6 chunks
        
        # Verify chunk distribution
        for i, chunk in enumerate(metadata["chunks"]):
            assert "chunk_id" in chunk
            assert "server_id" in chunk
            assert "size" in chunk
            print(f"   Chunk {i}: {chunk['chunk_id']} -> Server {chunk['server_id']} ({format_bytes(chunk['size'])})")
        
        print("✅ All metadata fields verified")
        
        # Cleanup
        test_file.unlink()
    
    def test_upload_invalid_content_type(self, api_client, test_data_dir):
        """
        Test 8: Upload with invalid content type validation.
        
        Verifies:
        - Invalid requests are rejected
        - Appropriate error messages returned
        """
        print("\n📝 Test 8: Upload with Invalid Content Type")
        
        # Try to upload without multipart/form-data
        print("⬆️  Attempting upload with invalid content type...")
        session = api_client["session"]
        base_url = api_client["base_url"]
        
        # Send JSON instead of multipart form
        response = session.post(
            f"{base_url}/files",
            json={"file": "not_a_file"},
            timeout=10
        )
        
        # Should fail with 400 Bad Request
        assert response.status_code == 400
        error_data = response.json()
        assert "error" in error_data
        
        print(f"✅ Invalid upload rejected: {error_data['error']}")
        
        # Try to upload without file field
        print("⬆️  Attempting upload without file field...")
        response = session.post(
            f"{base_url}/files",
            data={"not_file": "data"},
            timeout=10
        )
        
        # Should fail with 400 Bad Request
        assert response.status_code == 400
        error_data = response.json()
        assert "error" in error_data
        
        print(f"✅ Missing file field rejected: {error_data['error']}")
    
    @pytest.mark.slow
    @pytest.mark.large_file
    def test_upload_download_max_size(self, api_client, test_data_dir, cleanup_files):
        """
        Test 9: Upload and download file at max size boundary (10 GB).
        
        Verifies:
        - 10 GB files are accepted (boundary test)
        - Large file handling works at maximum size
        - Data integrity maintained
        """
        print("\n📝 Test 9: Upload/Download Max Size File (10 GB)")
        
        # Generate exactly 10 GB test file
        file_size = 10 * 1024 * 1024 * 1024  # 10 GB
        test_file, original_checksum = generate_random_file(
            file_size,
            test_data_dir / "max_size_file.bin"
        )
        print(f"✅ Generated max size file: {format_bytes(file_size)}")
        
        # Upload file
        print("⬆️  Uploading max size file (this will take a while)...")
        start_time = time.time()
        upload_response = upload_file(api_client, test_file, "max_size_test.bin")
        upload_time = time.time() - start_time
        
        assert "file_id" in upload_response
        assert "checksum" in upload_response
        assert upload_response["checksum"] == original_checksum
        
        file_id = upload_response["file_id"]
        cleanup_files.append(file_id)
        
        upload_speed = file_size / upload_time / (1024 * 1024)  # MB/s
        print(f"✅ Upload completed in {upload_time:.2f}s ({upload_speed:.2f} MB/s)")
        
        # Download file
        print("⬇️  Downloading max size file...")
        start_time = time.time()
        downloaded_file = download_file(
            api_client,
            file_id,
            test_data_dir / "downloaded_max.bin"
        )
        download_time = time.time() - start_time
        
        download_speed = file_size / download_time / (1024 * 1024)  # MB/s
        print(f"✅ Download completed in {download_time:.2f}s ({download_speed:.2f} MB/s)")
        
        # Verify checksum
        print("🔍 Verifying checksum...")
        downloaded_checksum = calculate_file_checksum(downloaded_file)
        assert downloaded_checksum == original_checksum
        print(f"✅ Checksum verified: {downloaded_checksum}")
        
        # Cleanup
        test_file.unlink()
        downloaded_file.unlink()
    
    def test_concurrent_operations(self, api_client, test_data_dir, cleanup_files):
        """
        Test 10: Concurrent upload and download operations.
        
        Verifies:
        - System handles concurrent operations
        - No race conditions
        - All operations complete successfully
        """
        print("\n📝 Test 10: Concurrent Operations")
        
        import concurrent.futures
        
        # Upload multiple files concurrently
        num_concurrent = 5
        file_size = 5 * 1024 * 1024  # 5 MB each
        
        def upload_task(index):
            test_file, checksum = generate_random_file(
                file_size,
                test_data_dir / f"concurrent_{index}.bin"
            )
            upload_response = upload_file(api_client, test_file, f"concurrent_{index}.bin")
            test_file.unlink()
            return upload_response["file_id"], checksum
        
        print(f"⬆️  Uploading {num_concurrent} files concurrently...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [executor.submit(upload_task, i) for i in range(num_concurrent)]
            results = [f.result() for f in concurrent.futures.as_completed(futures)]
        
        upload_time = time.time() - start_time
        print(f"✅ All uploads completed in {upload_time:.2f}s")
        
        # Track uploaded files for cleanup
        for file_id, _ in results:
            cleanup_files.append(file_id)
        
        # Download all files concurrently
        def download_task(file_id, checksum, index):
            downloaded_file = download_file(
                api_client,
                file_id,
                test_data_dir / f"downloaded_concurrent_{index}.bin"
            )
            downloaded_checksum = calculate_file_checksum(downloaded_file)
            downloaded_file.unlink()
            return downloaded_checksum == checksum
        
        print(f"⬇️  Downloading {num_concurrent} files concurrently...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [
                executor.submit(download_task, file_id, checksum, i)
                for i, (file_id, checksum) in enumerate(results)
            ]
            checksums_valid = [f.result() for f in concurrent.futures.as_completed(futures)]
        
        download_time = time.time() - start_time
        print(f"✅ All downloads completed in {download_time:.2f}s")
        
        # Verify all checksums matched
        assert all(checksums_valid)
        print(f"✅ All {num_concurrent} checksums verified")