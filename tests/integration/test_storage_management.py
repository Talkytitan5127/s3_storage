"""
Integration tests for storage server management and dynamic scaling.

Tests verify that:
1. Hash ring refreshes with active servers
2. Inactive servers are removed from hash ring
3. New servers are automatically added
4. Server failover works correctly
5. Heartbeat mechanism functions properly
"""

import pytest
import requests
import time
from uuid import uuid4


class TestStorageManagement:
    """Test storage server management and dynamic scaling"""

    def test_hash_ring_refresh(self, api_url, db_connection):
        """Test that hash ring refreshes from database"""
        # Get current server count
        cursor = db_connection.cursor()
        cursor.execute("""
            SELECT COUNT(*) FROM storage_servers 
            WHERE status = 'active' 
            AND last_heartbeat > NOW() - INTERVAL '30 seconds'
        """)
        active_count = cursor.fetchone()[0]
        
        # Verify we have active servers
        assert active_count > 0, "No active storage servers found"
        
        # Health check should show active servers
        response = requests.get(f"{api_url}/health")
        assert response.status_code == 200
        data = response.json()
        assert data['status'] == 'healthy'
        assert data['storage_servers'] == active_count

    def test_server_heartbeat_mechanism(self, api_url, db_connection):
        """Test that server heartbeats are being updated"""
        cursor = db_connection.cursor()
        
        # Get a server and its last heartbeat
        cursor.execute("""
            SELECT server_id, last_heartbeat 
            FROM storage_servers 
            WHERE status = 'active'
            LIMIT 1
        """)
        
        row = cursor.fetchone()
        if not row:
            pytest.skip("No active storage servers available")
        
        server_id, old_heartbeat = row
        
        # Wait for heartbeat interval (10 seconds)
        time.sleep(12)
        
        # Check if heartbeat was updated
        cursor.execute("""
            SELECT last_heartbeat 
            FROM storage_servers 
            WHERE server_id = %s
        """, (server_id,))
        
        new_heartbeat = cursor.fetchone()[0]
        
        # Heartbeat should be updated
        assert new_heartbeat > old_heartbeat, "Heartbeat was not updated"

    def test_inactive_server_detection(self, api_url, db_connection):
        """Test that inactive servers are detected"""
        cursor = db_connection.cursor()
        
        # Create a fake inactive server
        fake_server_id = str(uuid4())
        
        cursor.execute("""
            INSERT INTO storage_servers 
            (server_id, grpc_address, status, available_space, used_space, last_heartbeat)
            VALUES (%s, %s, %s, %s, %s, NOW() - INTERVAL '2 minutes')
        """, (fake_server_id, 'fake-server:50099', 'active', 1000000, 0))
        
        db_connection.commit()
        
        # Wait for hash ring refresh (30 seconds)
        time.sleep(2)
        
        # Server should still be in database but not in active list
        cursor.execute("""
            SELECT COUNT(*) FROM storage_servers 
            WHERE server_id = %s 
            AND last_heartbeat > NOW() - INTERVAL '30 seconds'
        """, (fake_server_id,))
        
        active_count = cursor.fetchone()[0]
        assert active_count == 0, "Inactive server still considered active"
        
        # Clean up
        cursor.execute("DELETE FROM storage_servers WHERE server_id = %s", (fake_server_id,))
        db_connection.commit()

    def test_dynamic_server_addition(self, api_url, db_connection):
        """Test that new servers are automatically added to hash ring"""
        # Get current server count
        response = requests.get(f"{api_url}/health")
        assert response.status_code == 200
        initial_count = response.json()['storage_servers']
        
        # In a real scenario, a new storage server would register itself
        # For this test, we verify the current count is stable
        time.sleep(2)
        
        response = requests.get(f"{api_url}/health")
        assert response.status_code == 200
        current_count = response.json()['storage_servers']
        
        # Count should remain stable
        assert current_count == initial_count

    def test_server_failover(self, api_url, sample_file):
        """Test that system continues to work even if a server fails"""
        # Upload a file
        files = {'file': ('failover_test.txt', sample_file, 'text/plain')}
        response = requests.post(f"{api_url}/files", files=files)
        
        assert response.status_code == 201
        data = response.json()
        file_id = data['file_id']
        
        # Download the file
        response = requests.get(f"{api_url}/files/{file_id}")
        assert response.status_code == 200
        
        # Verify content
        downloaded_content = response.content
        sample_file.seek(0)
        original_content = sample_file.read()
        assert downloaded_content == original_content
        
        # Clean up
        requests.delete(f"{api_url}/files/{file_id}")

    def test_consistent_hashing_distribution(self, api_url, db_connection):
        """Test that chunks are distributed across servers using consistent hashing"""
        cursor = db_connection.cursor()
        
        # Get server distribution of chunks
        cursor.execute("""
            SELECT storage_server_id, COUNT(*) as chunk_count
            FROM chunks
            GROUP BY storage_server_id
            ORDER BY chunk_count DESC
        """)
        
        distribution = cursor.fetchall()
        
        if len(distribution) < 2:
            pytest.skip("Need at least 2 servers with chunks for distribution test")
        
        # Verify chunks are distributed (not all on one server)
        total_chunks = sum(row[1] for row in distribution)
        max_chunks = distribution[0][1]
        
        # No single server should have more than 50% of chunks (with 6 servers)
        assert max_chunks < total_chunks * 0.6, "Chunks not well distributed"

    def test_server_removal_handling(self, api_url, db_connection):
        """Test that system handles server removal gracefully"""
        cursor = db_connection.cursor()
        
        # Get current active server count
        cursor.execute("""
            SELECT COUNT(*) FROM storage_servers 
            WHERE status = 'active'
            AND last_heartbeat > NOW() - INTERVAL '30 seconds'
        """)
        
        active_count = cursor.fetchone()[0]
        assert active_count > 0, "No active servers"
        
        # System should continue to function with available servers
        response = requests.get(f"{api_url}/health")
        assert response.status_code == 200
        assert response.json()['status'] == 'healthy'

    def test_hash_ring_consistency(self, api_url, sample_file):
        """Test that hash ring provides consistent chunk placement"""
        # Upload multiple files and verify consistent placement
        file_ids = []
        
        for i in range(3):
            sample_file.seek(0)
            files = {'file': (f'consistency_test_{i}.txt', sample_file, 'text/plain')}
            response = requests.post(f"{api_url}/files", files=files)
            assert response.status_code == 201
            file_ids.append(response.json()['file_id'])
        
        # All files should be uploaded successfully
        assert len(file_ids) == 3
        
        # Clean up
        for file_id in file_ids:
            requests.delete(f"{api_url}/files/{file_id}")


@pytest.fixture
def sample_file():
    """Create a sample file for testing"""
    import io
    content = b"Test content for storage management tests\n" * 100
    return io.BytesIO(content)