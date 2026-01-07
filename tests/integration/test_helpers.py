"""
Helper utilities for integration tests.
"""
import os
import hashlib
import io
from typing import BinaryIO, Tuple
from pathlib import Path


def generate_random_file(size_bytes: int, output_path: Path = None) -> Tuple[Path, str]:
    """
    Generate a random file of specified size and return path and SHA-256 checksum.
    
    Args:
        size_bytes: Size of file to generate in bytes
        output_path: Optional path to save file. If None, creates temp file.
    
    Returns:
        Tuple of (file_path, sha256_checksum)
    """
    if output_path is None:
        output_path = Path(f"/tmp/test_file_{size_bytes}.bin")
    
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
    # Generate file with random data
    chunk_size = 1024 * 1024  # 1 MB chunks
    sha256 = hashlib.sha256()
    
    with open(output_path, 'wb') as f:
        remaining = size_bytes
        while remaining > 0:
            chunk = os.urandom(min(chunk_size, remaining))
            f.write(chunk)
            sha256.update(chunk)
            remaining -= len(chunk)
    
    return output_path, sha256.hexdigest()


def calculate_file_checksum(file_path: Path) -> str:
    """
    Calculate SHA-256 checksum of a file.
    
    Args:
        file_path: Path to file
    
    Returns:
        SHA-256 checksum as hex string
    """
    sha256 = hashlib.sha256()
    
    with open(file_path, 'rb') as f:
        while True:
            chunk = f.read(1024 * 1024)  # 1 MB chunks
            if not chunk:
                break
            sha256.update(chunk)
    
    return sha256.hexdigest()


def calculate_stream_checksum(stream: BinaryIO) -> str:
    """
    Calculate SHA-256 checksum of a stream.
    
    Args:
        stream: Binary stream to read
    
    Returns:
        SHA-256 checksum as hex string
    """
    sha256 = hashlib.sha256()
    
    while True:
        chunk = stream.read(1024 * 1024)  # 1 MB chunks
        if not chunk:
            break
        sha256.update(chunk)
    
    return sha256.hexdigest()


def format_bytes(size_bytes: int) -> str:
    """
    Format bytes to human-readable string.
    
    Args:
        size_bytes: Size in bytes
    
    Returns:
        Formatted string (e.g., "1.5 GB")
    """
    for unit in ['B', 'KB', 'MB', 'GB', 'TB']:
        if size_bytes < 1024.0:
            return f"{size_bytes:.2f} {unit}"
        size_bytes /= 1024.0
    return f"{size_bytes:.2f} PB"


def upload_file(api_client: dict, file_path: Path, filename: str = None) -> dict:
    """
    Upload a file to the API Gateway.
    
    Args:
        api_client: API client configuration dict
        file_path: Path to file to upload
        filename: Optional custom filename (defaults to file_path.name)
    
    Returns:
        Response JSON dict
    """
    if filename is None:
        filename = file_path.name
    
    session = api_client["session"]
    base_url = api_client["base_url"]
    timeout = api_client["timeout"]
    
    with open(file_path, 'rb') as f:
        files = {'file': (filename, f, 'application/octet-stream')}
        response = session.post(
            f"{base_url}/files",
            files=files,
            timeout=timeout
        )
    
    response.raise_for_status()
    return response.json()


def download_file(api_client: dict, file_id: str, output_path: Path = None) -> Path:
    """
    Download a file from the API Gateway.
    
    Args:
        api_client: API client configuration dict
        file_id: File ID to download
        output_path: Optional path to save file. If None, creates temp file.
    
    Returns:
        Path to downloaded file
    """
    if output_path is None:
        output_path = Path(f"/tmp/downloaded_{file_id}.bin")
    
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
    session = api_client["session"]
    base_url = api_client["base_url"]
    timeout = api_client["timeout"]
    
    response = session.get(
        f"{base_url}/files/{file_id}",
        stream=True,
        timeout=timeout
    )
    response.raise_for_status()
    
    with open(output_path, 'wb') as f:
        for chunk in response.iter_content(chunk_size=1024 * 1024):
            if chunk:
                f.write(chunk)
    
    return output_path


def get_file_metadata(api_client: dict, file_id: str) -> dict:
    """
    Get file metadata from the API Gateway.
    
    Args:
        api_client: API client configuration dict
        file_id: File ID
    
    Returns:
        Metadata JSON dict
    """
    session = api_client["session"]
    base_url = api_client["base_url"]
    
    response = session.get(
        f"{base_url}/files/{file_id}/metadata",
        timeout=10
    )
    response.raise_for_status()
    return response.json()


def list_files(api_client: dict, page: int = 1, per_page: int = 10) -> dict:
    """
    List files from the API Gateway.
    
    Args:
        api_client: API client configuration dict
        page: Page number (1-indexed)
        per_page: Number of files per page
    
    Returns:
        List response JSON dict
    """
    session = api_client["session"]
    base_url = api_client["base_url"]
    
    response = session.get(
        f"{base_url}/files",
        params={"page": page, "per_page": per_page},
        timeout=10
    )
    response.raise_for_status()
    return response.json()


def delete_file(api_client: dict, file_id: str) -> dict:
    """
    Delete a file from the API Gateway.
    
    Args:
        api_client: API client configuration dict
        file_id: File ID to delete
    
    Returns:
        Response JSON dict
    """
    session = api_client["session"]
    base_url = api_client["base_url"]
    
    response = session.delete(
        f"{base_url}/files/{file_id}",
        timeout=10
    )
    response.raise_for_status()
    return response.json()