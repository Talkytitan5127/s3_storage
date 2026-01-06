"""
Helper utilities for gRPC tests.
"""
import os
import grpc
import hashlib
from typing import Iterator, Tuple
from pathlib import Path


# Storage server addresses
STORAGE_SERVERS = [
    "localhost:50051",
    "localhost:50052",
    "localhost:50053",
    "localhost:50054",
    "localhost:50055",
    "localhost:50056",
]


def get_grpc_channel(server_address: str) -> grpc.Channel:
    """
    Create a gRPC channel to a storage server.
    
    Args:
        server_address: Server address (e.g., "localhost:50051")
    
    Returns:
        gRPC channel
    """
    return grpc.insecure_channel(server_address)


def chunk_data(data: bytes, chunk_size: int = 1024 * 1024) -> Iterator[bytes]:
    """
    Split data into chunks for streaming.
    
    Args:
        data: Data to chunk
        chunk_size: Size of each chunk (default 1 MB)
    
    Yields:
        Chunks of data
    """
    for i in range(0, len(data), chunk_size):
        yield data[i:i + chunk_size]


def calculate_checksum(data: bytes) -> str:
    """
    Calculate SHA-256 checksum of data.
    
    Args:
        data: Data to hash
    
    Returns:
        SHA-256 checksum as hex string
    """
    return hashlib.sha256(data).hexdigest()


def generate_chunk_data(size_bytes: int) -> Tuple[bytes, str]:
    """
    Generate random chunk data and its checksum.
    
    Args:
        size_bytes: Size of data to generate
    
    Returns:
        Tuple of (data, checksum)
    """
    data = os.urandom(size_bytes)
    checksum = calculate_checksum(data)
    return data, checksum


def put_chunk_streaming(stub, chunk_id: str, data: bytes, checksum: str, chunk_size: int = 1024 * 1024):
    """
    Upload a chunk using streaming.
    
    Args:
        stub: gRPC stub
        chunk_id: Chunk ID
        data: Chunk data
        checksum: SHA-256 checksum
        chunk_size: Size of each stream chunk (default 1 MB)
    
    Returns:
        PutChunkResponse
    """
    # Import here to avoid circular dependency
    from generated import storage_pb2
    
    def request_iterator():
        for i, chunk in enumerate(chunk_data(data, chunk_size)):
            request = storage_pb2.PutChunkRequest(
                chunk_id=chunk_id,
                data=chunk,
                checksum=checksum if i == 0 else ""  # Only send checksum in first request
            )
            yield request
    
    response = stub.PutChunk(request_iterator())
    return response


def get_chunk_streaming(stub, chunk_id: str) -> bytes:
    """
    Download a chunk using streaming.
    
    Args:
        stub: gRPC stub
        chunk_id: Chunk ID
    
    Returns:
        Chunk data
    """
    # Import here to avoid circular dependency
    from generated import storage_pb2
    
    request = storage_pb2.GetChunkRequest(chunk_id=chunk_id)
    
    data = b""
    for response in stub.GetChunk(request):
        data += response.data
    
    return data


def delete_chunk(stub, chunk_id: str):
    """
    Delete a chunk.
    
    Args:
        stub: gRPC stub
        chunk_id: Chunk ID
    
    Returns:
        DeleteChunkResponse
    """
    # Import here to avoid circular dependency
    from generated import storage_pb2
    
    request = storage_pb2.DeleteChunkRequest(chunk_id=chunk_id)
    response = stub.DeleteChunk(request)
    return response


def health_check(stub):
    """
    Perform health check on storage server.
    
    Args:
        stub: gRPC stub
    
    Returns:
        HealthCheckResponse
    """
    # Import here to avoid circular dependency
    from generated import storage_pb2
    
    request = storage_pb2.HealthCheckRequest()
    response = stub.HealthCheck(request)
    return response


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