"""
Pytest configuration and fixtures for integration tests.
"""
import os
import time
import pytest
import requests
import subprocess
from typing import Generator, Dict, Any
from pathlib import Path


# Test configuration
API_BASE_URL = os.getenv("API_BASE_URL", "http://localhost:8080")
DOCKER_COMPOSE_FILE = "docker-compose.test.yml"
TEST_DATA_DIR = Path(__file__).parent / "test_data"


@pytest.fixture(scope="session")
def docker_services() -> Generator[None, None, None]:
    """
    Start Docker Compose services before tests and stop them after.
    """
    project_root = Path(__file__).parent.parent.parent
    compose_file = project_root / DOCKER_COMPOSE_FILE
    
    if not compose_file.exists():
        pytest.skip(f"Docker Compose file not found: {compose_file}")
    
    # Start services
    print("\n🚀 Starting Docker Compose services...")
    subprocess.run(
        ["docker-compose", "-f", str(compose_file), "up", "-d", "--build"],
        cwd=project_root,
        check=True
    )
    
    # Wait for services to be ready
    print("⏳ Waiting for services to be ready...")
    wait_for_api_ready(max_attempts=30, delay=2)
    
    yield
    
    # Stop services
    print("\n🛑 Stopping Docker Compose services...")
    subprocess.run(
        ["docker-compose", "-f", str(compose_file), "down", "-v"],
        cwd=project_root,
        check=False
    )


def wait_for_api_ready(max_attempts: int = 30, delay: int = 2) -> None:
    """
    Wait for API Gateway to be ready.
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
    
    raise RuntimeError(f"API Gateway did not become ready after {max_attempts} attempts")


@pytest.fixture(scope="session")
def api_client(docker_services) -> Dict[str, Any]:
    """
    Provide API client configuration.
    """
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