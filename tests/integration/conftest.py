"""
Pytest configuration and fixtures for integration tests.

NOTE: This assumes infrastructure (Docker Compose services) is already running.
Start services before running tests with:
    docker compose -f docker-compose.test.yml up -d --build
"""
import os
import time
import pytest
import requests
from typing import Generator, Dict, Any
from pathlib import Path


# Test configuration
API_BASE_URL = os.getenv("API_BASE_URL", "http://localhost:8080")
TEST_DATA_DIR = Path(__file__).parent / "test_data"


def wait_for_api_ready(max_attempts: int = 30, delay: int = 2) -> None:
    """
    Wait for API Gateway to be ready.
    Raises RuntimeError if API is not available.
    """
    health_url = f"{API_BASE_URL}/health"
    
    for attempt in range(max_attempts):
        try:
            response = requests.get(health_url, timeout=5)
            if response.status_code == 200:
                print(f"✅ API Gateway is ready (attempt {attempt + 1})")
                return
        except requests.exceptions.RequestException:
            pass
        
        if attempt < max_attempts - 1:
            time.sleep(delay)
    
    raise RuntimeError(
        f"API Gateway did not become ready after {max_attempts} attempts. "
        f"Please ensure infrastructure is running: docker compose -f docker-compose.test.yml up -d"
    )


@pytest.fixture(scope="session")
def api_client() -> Dict[str, Any]:
    """
    Provide API client configuration.
    Verifies that API is available before running tests.
    """
    print("\n🔍 Checking if API Gateway is available...")
    wait_for_api_ready(max_attempts=10, delay=1)
    
    return {
        "base_url": API_BASE_URL,
        "timeout": 300,  # 5 minutes for large file operations
        "session": requests.Session()
    }


@pytest.fixture(scope="function")
def test_data_dir() -> Path:
    """
    Provide test data directory and ensure it exists.
    """
    TEST_DATA_DIR.mkdir(parents=True, exist_ok=True)
    return TEST_DATA_DIR


@pytest.fixture(scope="function")
def cleanup_files(api_client) -> Generator[list, None, None]:
    """
    Track uploaded files and clean them up after test.
    """
    uploaded_files = []
    
    yield uploaded_files
    
    # Cleanup
    session = api_client["session"]
    base_url = api_client["base_url"]
    
    for file_id in uploaded_files:
        try:
            response = session.delete(f"{base_url}/files/{file_id}", timeout=10)
            if response.status_code == 200:
                print(f"🗑️  Cleaned up file: {file_id}")
        except Exception as e:
            print(f"⚠️  Failed to cleanup file {file_id}: {e}")


@pytest.fixture(scope="session")
def api_url() -> str:
    """
    Provide API base URL for tests that use api_url fixture.
    """
    return API_BASE_URL


@pytest.fixture(scope="session")
def db_connection():
    """
    Provide database connection for tests that need direct DB access.
    Assumes PostgreSQL is running and accessible.
    """
    import psycopg2
    
    # Database configuration from environment or defaults
    db_config = {
        "host": os.getenv("DB_HOST", "localhost"),
        "port": int(os.getenv("DB_PORT", "5432")),
        "database": os.getenv("DB_NAME", "s3_storage"),
        "user": os.getenv("DB_USER", "postgres"),
        "password": os.getenv("DB_PASSWORD", "postgres")
    }
    
    try:
        conn = psycopg2.connect(**db_config)
        conn.autocommit = False
        yield conn
        conn.close()
    except psycopg2.Error as e:
        pytest.skip(f"Database connection failed: {e}. Ensure PostgreSQL is running.")


def pytest_configure(config):
    """
    Configure pytest with custom markers.
    """
    config.addinivalue_line(
        "markers", "slow: marks tests as slow (deselect with '-m \"not slow\"')"
    )
    config.addinivalue_line(
        "markers", "large_file: marks tests that use large files"
    )
    config.addinivalue_line(
        "markers", "concurrent: marks tests that test concurrent operations"
    )