"""
Concurrent Operations Tests for S3-like Storage System.

Tests system behavior under concurrent load, including:
- Concurrent uploads
- Concurrent downloads
- Mixed operations
- Database connection pool management
- Race condition detection
"""
import pytest
import time
import concurrent.futures
import threading
from pathlib import Path
from test_helpers import (
    generate_random_file,
    upload_file,
    download_file,
    get_file_metadata,
    delete_file,
    calculate_file_checksum,
    format_bytes
)


@pytest.mark.concurrent
class TestConcurrentOperations:
    """Tests for concurrent operations and race conditions."""
    
    def test_concurrent_uploads(self, api_client, test_data_dir, cleanup_files):
        """
        Test 1: Concurrent uploads with 50 goroutines.
        
        Verifies:
        - System handles 50 concurrent uploads
        - All uploads complete successfully
        - No data corruption
        - Reasonable performance maintained
        """
        print("\n📝 Test 1: Concurrent Uploads (50 files)")
        
        num_concurrent = 50
        file_size = 5 * 1024 * 1024  # 5 MB each
        total_size = num_concurrent * file_size
        
        print(f"⬆️  Uploading {num_concurrent} files concurrently")
        print(f"   File size: {format_bytes(file_size)} each")
        print(f"   Total size: {format_bytes(total_size)}")
        
        # Track results
        results = []
        errors = []
        lock = threading.Lock()
        
        def upload_task(index):
            try:
                # Generate file
                test_file, checksum = generate_random_file(
                    file_size,
                    test_data_dir / f"concurrent_upload_{index}.bin"
                )
                
                # Upload
                upload_response = upload_file(
                    api_client,
                    test_file,
                    f"concurrent_upload_{index}.bin"
                )
                
                # Cleanup local file
                test_file.unlink()
                
                with lock:
                    results.append({
                        'index': index,
                        'file_id': upload_response['file_id'],
                        'checksum': checksum,
                        'success': True
                    })
                
                return upload_response['file_id']
            except Exception as e:
                with lock:
                    errors.append({'index': index, 'error': str(e)})
                raise
        
        # Execute concurrent uploads
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [executor.submit(upload_task, i) for i in range(num_concurrent)]
            
            # Wait for all to complete
            for future in concurrent.futures.as_completed(futures):
                try:
                    file_id = future.result()
                    cleanup_files.append(file_id)
                except Exception as e:
                    print(f"⚠️  Upload failed: {e}")
        
        upload_time = time.time() - start_time
        
        # Report results
        success_count = len(results)
        error_count = len(errors)
        
        print(f"\n📊 Results:")
        print(f"   Successful: {success_count}/{num_concurrent}")
        print(f"   Failed: {error_count}/{num_concurrent}")
        print(f"   Total time: {upload_time:.2f}s")
        print(f"   Average time per file: {upload_time/num_concurrent:.2f}s")
        print(f"   Throughput: {total_size/upload_time/(1024*1024):.2f} MB/s")
        
        # Verify all succeeded
        assert success_count == num_concurrent, f"Some uploads failed: {errors}"
        assert error_count == 0
        
        print("✅ All concurrent uploads completed successfully")
    
    def test_concurrent_downloads(self, api_client, test_data_dir, cleanup_files):
        """
        Test 2: Concurrent downloads with 100 goroutines.
        
        Verifies:
        - System handles 100 concurrent downloads
        - All downloads complete successfully
        - Data integrity maintained
        - No resource exhaustion
        """
        print("\n📝 Test 2: Concurrent Downloads (100 requests)")
        
        # First, upload 10 files
        num_files = 10
        file_size = 5 * 1024 * 1024  # 5 MB each
        
        print(f"⬆️  Uploading {num_files} test files...")
        uploaded_files = []
        
        for i in range(num_files):
            test_file, checksum = generate_random_file(
                file_size,
                test_data_dir / f"download_test_{i}.bin"
            )
            
            upload_response = upload_file(api_client, test_file, f"download_test_{i}.bin")
            uploaded_files.append({
                'file_id': upload_response['file_id'],
                'checksum': checksum
            })
            cleanup_files.append(upload_response['file_id'])
            test_file.unlink()
        
        print(f"✅ Uploaded {num_files} files")
        
        # Now download each file 10 times concurrently (100 total downloads)
        num_downloads_per_file = 10
        total_downloads = num_files * num_downloads_per_file
        
        print(f"\n⬇️  Downloading {total_downloads} times concurrently")
        print(f"   ({num_downloads_per_file} concurrent downloads per file)")
        
        results = []
        errors = []
        lock = threading.Lock()
        
        def download_task(file_info, download_index):
            try:
                file_id = file_info['file_id']
                expected_checksum = file_info['checksum']
                
                # Download file
                downloaded_file = download_file(
                    api_client,
                    file_id,
                    test_data_dir / f"downloaded_{file_id}_{download_index}.bin"
                )
                
                # Verify checksum
                actual_checksum = calculate_file_checksum(downloaded_file)
                checksum_valid = actual_checksum == expected_checksum
                
                # Cleanup
                downloaded_file.unlink()
                
                with lock:
                    results.append({
                        'file_id': file_id,
                        'checksum_valid': checksum_valid,
                        'success': True
                    })
                
                return checksum_valid
            except Exception as e:
                with lock:
                    errors.append({'file_id': file_info['file_id'], 'error': str(e)})
                raise
        
        # Execute concurrent downloads
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=total_downloads) as executor:
            futures = []
            for file_info in uploaded_files:
                for i in range(num_downloads_per_file):
                    futures.append(executor.submit(download_task, file_info, i))
            
            # Wait for all to complete
            checksums_valid = []
            for future in concurrent.futures.as_completed(futures):
                try:
                    checksums_valid.append(future.result())
                except Exception as e:
                    print(f"⚠️  Download failed: {e}")
        
        download_time = time.time() - start_time
        
        # Report results
        success_count = len(results)
        error_count = len(errors)
        valid_checksums = sum(1 for r in results if r['checksum_valid'])
        
        print(f"\n📊 Results:")
        print(f"   Successful: {success_count}/{total_downloads}")
        print(f"   Failed: {error_count}/{total_downloads}")
        print(f"   Valid checksums: {valid_checksums}/{success_count}")
        print(f"   Total time: {download_time:.2f}s")
        print(f"   Average time per download: {download_time/total_downloads:.2f}s")
        print(f"   Throughput: {(file_size*total_downloads)/download_time/(1024*1024):.2f} MB/s")
        
        # Verify all succeeded
        assert success_count == total_downloads, f"Some downloads failed: {errors}"
        assert error_count == 0
        assert valid_checksums == success_count, "Some checksums didn't match"
        
        print("✅ All concurrent downloads completed successfully")
    
    def test_mixed_operations(self, api_client, test_data_dir, cleanup_files):
        """
        Test 3: Mixed operations (upload/download/delete).
        
        Verifies:
        - System handles mixed operation types
        - Operations don't interfere with each other
        - Consistency maintained
        """
        print("\n📝 Test 3: Mixed Operations")
        
        duration = 30  # Run for 30 seconds
        file_size = 2 * 1024 * 1024  # 2 MB
        
        print(f"🔄 Running mixed operations for {duration} seconds...")
        
        # Shared state
        uploaded_files = []
        lock = threading.Lock()
        stop_flag = threading.Event()
        
        # Statistics
        stats = {
            'uploads': 0,
            'downloads': 0,
            'deletes': 0,
            'errors': 0
        }
        
        def upload_worker():
            """Continuously upload files."""
            while not stop_flag.is_set():
                try:
                    test_file, checksum = generate_random_file(
                        file_size,
                        test_data_dir / f"mixed_upload_{time.time()}.bin"
                    )
                    
                    upload_response = upload_file(api_client, test_file, test_file.name)
                    test_file.unlink()
                    
                    with lock:
                        uploaded_files.append({
                            'file_id': upload_response['file_id'],
                            'checksum': checksum
                        })
                        cleanup_files.append(upload_response['file_id'])
                        stats['uploads'] += 1
                    
                    time.sleep(0.1)  # Small delay
                except Exception as e:
                    with lock:
                        stats['errors'] += 1
                    print(f"⚠️  Upload error: {e}")
        
        def download_worker():
            """Continuously download random files."""
            while not stop_flag.is_set():
                try:
                    with lock:
                        if not uploaded_files:
                            time.sleep(0.5)
                            continue
                        file_info = uploaded_files[len(uploaded_files) // 2]  # Pick middle file
                    
                    downloaded_file = download_file(
                        api_client,
                        file_info['file_id'],
                        test_data_dir / f"mixed_download_{time.time()}.bin"
                    )
                    
                    # Verify checksum
                    actual_checksum = calculate_file_checksum(downloaded_file)
                    assert actual_checksum == file_info['checksum']
                    
                    downloaded_file.unlink()
                    
                    with lock:
                        stats['downloads'] += 1
                    
                    time.sleep(0.1)
                except Exception as e:
                    with lock:
                        stats['errors'] += 1
                    print(f"⚠️  Download error: {e}")
        
        def delete_worker():
            """Periodically delete old files."""
            while not stop_flag.is_set():
                try:
                    time.sleep(2)  # Delete less frequently
                    
                    with lock:
                        if len(uploaded_files) < 5:
                            continue
                        file_info = uploaded_files.pop(0)  # Remove oldest
                    
                    delete_file(api_client, file_info['file_id'])
                    
                    with lock:
                        stats['deletes'] += 1
                        if file_info['file_id'] in cleanup_files:
                            cleanup_files.remove(file_info['file_id'])
                except Exception as e:
                    with lock:
                        stats['errors'] += 1
                    print(f"⚠️  Delete error: {e}")
        
        # Start workers
        workers = []
        workers.append(threading.Thread(target=upload_worker))
        workers.append(threading.Thread(target=upload_worker))
        workers.append(threading.Thread(target=download_worker))
        workers.append(threading.Thread(target=download_worker))
        workers.append(threading.Thread(target=download_worker))
        workers.append(threading.Thread(target=delete_worker))
        
        for worker in workers:
            worker.start()
        
        # Run for specified duration
        time.sleep(duration)
        
        # Stop workers
        stop_flag.set()
        for worker in workers:
            worker.join(timeout=5)
        
        # Report results
        print(f"\n📊 Results after {duration}s:")
        print(f"   Uploads: {stats['uploads']}")
        print(f"   Downloads: {stats['downloads']}")
        print(f"   Deletes: {stats['deletes']}")
        print(f"   Errors: {stats['errors']}")
        print(f"   Files remaining: {len(uploaded_files)}")
        
        # Verify reasonable operation counts
        assert stats['uploads'] > 0, "No uploads completed"
        assert stats['downloads'] > 0, "No downloads completed"
        assert stats['errors'] < stats['uploads'] * 0.1, "Too many errors"
        
        print("✅ Mixed operations completed successfully")
    
    def test_database_connection_pool(self, api_client, test_data_dir, cleanup_files):
        """
        Test 4: Database connection pool management.
        
        Verifies:
        - Connection pool handles concurrent requests
        - No connection exhaustion
        - Proper connection reuse
        """
        print("\n📝 Test 4: Database Connection Pool")
        
        num_concurrent = 100
        
        print(f"📊 Testing connection pool with {num_concurrent} concurrent metadata requests")
        
        # Upload a test file
        file_size = 1 * 1024 * 1024  # 1 MB
        test_file, _ = generate_random_file(
            file_size,
            test_data_dir / "pool_test.bin"
        )
        
        upload_response = upload_file(api_client, test_file, "pool_test.bin")
        file_id = upload_response['file_id']
        cleanup_files.append(file_id)
        test_file.unlink()
        
        print(f"✅ Uploaded test file: {file_id}")
        
        # Perform many concurrent metadata requests
        results = []
        errors = []
        lock = threading.Lock()
        
        def metadata_task(index):
            try:
                metadata = get_file_metadata(api_client, file_id)
                
                with lock:
                    results.append({
                        'index': index,
                        'file_id': metadata['file_id'],
                        'success': True
                    })
                
                return True
            except Exception as e:
                with lock:
                    errors.append({'index': index, 'error': str(e)})
                return False
        
        print(f"🔄 Executing {num_concurrent} concurrent metadata requests...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [executor.submit(metadata_task, i) for i in range(num_concurrent)]
            
            # Wait for all to complete
            for future in concurrent.futures.as_completed(futures):
                future.result()
        
        request_time = time.time() - start_time
        
        # Report results
        success_count = len(results)
        error_count = len(errors)
        
        print(f"\n📊 Results:")
        print(f"   Successful: {success_count}/{num_concurrent}")
        print(f"   Failed: {error_count}/{num_concurrent}")
        print(f"   Total time: {request_time:.2f}s")
        print(f"   Average time per request: {request_time/num_concurrent*1000:.2f}ms")
        print(f"   Requests per second: {num_concurrent/request_time:.2f}")
        
        # Verify all succeeded
        assert success_count == num_concurrent, f"Some requests failed: {errors}"
        assert error_count == 0
        
        # Verify reasonable performance (< 100ms average)
        avg_time_ms = request_time / num_concurrent * 1000
        assert avg_time_ms < 100, f"Average request time too high: {avg_time_ms:.2f}ms"
        
        print("✅ Connection pool handled all requests successfully")
    
    def test_race_conditions(self, api_client, test_data_dir, cleanup_files):
        """
        Test 5: Race condition detection.
        
        Verifies:
        - No race conditions in concurrent operations
        - Data consistency maintained
        - Proper locking/synchronization
        """
        print("\n📝 Test 5: Race Condition Detection")
        
        num_concurrent = 20
        file_size = 1 * 1024 * 1024  # 1 MB
        
        print(f"🔍 Testing for race conditions with {num_concurrent} concurrent operations")
        
        # Upload initial file
        test_file, original_checksum = generate_random_file(
            file_size,
            test_data_dir / "race_test.bin"
        )
        
        upload_response = upload_file(api_client, test_file, "race_test.bin")
        file_id = upload_response['file_id']
        cleanup_files.append(file_id)
        test_file.unlink()
        
        print(f"✅ Uploaded test file: {file_id}")
        
        # Perform concurrent reads and metadata requests
        results = []
        errors = []
        lock = threading.Lock()
        
        def concurrent_task(index):
            try:
                # Alternate between download and metadata
                if index % 2 == 0:
                    # Download
                    downloaded_file = download_file(
                        api_client,
                        file_id,
                        test_data_dir / f"race_download_{index}.bin"
                    )
                    checksum = calculate_file_checksum(downloaded_file)
                    downloaded_file.unlink()
                    
                    with lock:
                        results.append({
                            'type': 'download',
                            'checksum_valid': checksum == original_checksum
                        })
                else:
                    # Metadata
                    metadata = get_file_metadata(api_client, file_id)
                    
                    with lock:
                        results.append({
                            'type': 'metadata',
                            'checksum_valid': metadata['checksum'] == original_checksum
                        })
                
                return True
            except Exception as e:
                with lock:
                    errors.append({'index': index, 'error': str(e)})
                return False
        
        print(f"🔄 Executing {num_concurrent} concurrent operations...")
        start_time = time.time()
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=num_concurrent) as executor:
            futures = [executor.submit(concurrent_task, i) for i in range(num_concurrent)]
            
            # Wait for all to complete
            for future in concurrent.futures.as_completed(futures):
                future.result()
        
        operation_time = time.time() - start_time
        
        # Analyze results
        downloads = [r for r in results if r['type'] == 'download']
        metadata_ops = [r for r in results if r['type'] == 'metadata']
        
        download_valid = sum(1 for r in downloads if r['checksum_valid'])
        metadata_valid = sum(1 for r in metadata_ops if r['checksum_valid'])
        
        print(f"\n📊 Results:")
        print(f"   Downloads: {len(downloads)} (valid: {download_valid})")
        print(f"   Metadata: {len(metadata_ops)} (valid: {metadata_valid})")
        print(f"   Errors: {len(errors)}")
        print(f"   Total time: {operation_time:.2f}s")
        
        # Verify no race conditions (all checksums should match)
        assert len(errors) == 0, f"Errors occurred: {errors}"
        assert download_valid == len(downloads), "Some download checksums didn't match"
        assert metadata_valid == len(metadata_ops), "Some metadata checksums didn't match"
        
        print("✅ No race conditions detected - all data consistent")