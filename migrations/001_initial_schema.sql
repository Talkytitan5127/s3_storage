-- S3 Storage System - Initial Database Schema
-- PostgreSQL 15+

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Storage Servers Table
CREATE TABLE storage_servers (
    server_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    grpc_address VARCHAR(255) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'maintenance')),
    available_space BIGINT NOT NULL DEFAULT 0,
    used_space BIGINT NOT NULL DEFAULT 0,
    last_heartbeat TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_storage_servers_status ON storage_servers(status);
CREATE INDEX idx_storage_servers_heartbeat ON storage_servers(last_heartbeat);

-- Hash Ring Nodes Table (for Consistent Hashing)
CREATE TABLE hash_ring_nodes (
    node_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    server_id UUID NOT NULL REFERENCES storage_servers(server_id) ON DELETE CASCADE,
    virtual_node_index INTEGER NOT NULL CHECK (virtual_node_index >= 0 AND virtual_node_index < 150),
    hash_value BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(server_id, virtual_node_index)
);

CREATE INDEX idx_hash_ring_nodes_server ON hash_ring_nodes(server_id);
CREATE INDEX idx_hash_ring_nodes_hash ON hash_ring_nodes(hash_value);

-- Files Table
CREATE TABLE files (
    file_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    total_size BIGINT NOT NULL CHECK (total_size > 0 AND total_size <= 10737418240), -- Max 10 GiB
    upload_status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (upload_status IN ('pending', 'uploading', 'completed', 'failed')),
    checksum VARCHAR(64), -- SHA-256 hex string
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_files_status ON files(upload_status);
CREATE INDEX idx_files_created ON files(created_at);

-- Chunks Table
CREATE TABLE chunks (
    chunk_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    file_id UUID NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    chunk_number INTEGER NOT NULL CHECK (chunk_number >= 0 AND chunk_number < 6),
    storage_server_id UUID NOT NULL REFERENCES storage_servers(server_id),
    chunk_size BIGINT NOT NULL CHECK (chunk_size > 0),
    chunk_hash VARCHAR(64) NOT NULL, -- SHA-256 hex string
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'uploading', 'completed', 'failed')),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(file_id, chunk_number)
);

CREATE INDEX idx_chunks_file ON chunks(file_id);
CREATE INDEX idx_chunks_server ON chunks(storage_server_id);
CREATE INDEX idx_chunks_status ON chunks(status);

-- Upload Sessions Table (for tracking incomplete uploads)
CREATE TABLE upload_sessions (
    session_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    file_id UUID NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'expired', 'cancelled')),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_upload_sessions_file ON upload_sessions(file_id);
CREATE INDEX idx_upload_sessions_expires ON upload_sessions(expires_at);
CREATE INDEX idx_upload_sessions_status ON upload_sessions(status);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_storage_servers_updated_at BEFORE UPDATE ON storage_servers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_files_updated_at BEFORE UPDATE ON files
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_chunks_updated_at BEFORE UPDATE ON chunks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_upload_sessions_updated_at BEFORE UPDATE ON upload_sessions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE storage_servers IS 'Registry of storage servers in the cluster';
COMMENT ON TABLE hash_ring_nodes IS 'Virtual nodes for consistent hashing (150 per server)';
COMMENT ON TABLE files IS 'Metadata for uploaded files';
COMMENT ON TABLE chunks IS 'Information about file chunks (6 per file)';
COMMENT ON TABLE upload_sessions IS 'Tracking for incomplete uploads with TTL';

COMMENT ON COLUMN files.total_size IS 'File size in bytes (max 10 GiB = 10737418240 bytes)';
COMMENT ON COLUMN chunks.chunk_number IS 'Chunk index 0-5 (6 chunks total)';
COMMENT ON COLUMN hash_ring_nodes.virtual_node_index IS 'Virtual node index 0-149 (150 per server)';