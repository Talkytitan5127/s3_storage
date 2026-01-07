package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned when a resource is not found
	ErrNotFound = errors.New("resource not found")
	// ErrDuplicate is returned when trying to create a duplicate resource
	ErrDuplicate = errors.New("duplicate resource")
)

// File represents a file in the storage system
type File struct {
	FileID       uuid.UUID
	Filename     string
	ContentType  string
	TotalSize    int64
	UploadStatus string
	Checksum     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
	Chunks       []*Chunk
}

// Chunk represents a file chunk
type Chunk struct {
	ChunkID         uuid.UUID
	FileID          uuid.UUID
	ChunkNumber     int
	StorageServerID uuid.UUID
	ChunkSize       int64
	ChunkHash       string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StorageServer represents a storage server in the cluster
type StorageServer struct {
	ServerID       uuid.UUID
	GRPCAddress    string
	Status         string
	AvailableSpace int64
	UsedSpace      int64
	LastHeartbeat  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UploadSession represents an upload session
type UploadSession struct {
	SessionID uuid.UUID
	FileID    uuid.UUID
	Status    string
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PostgresStorage implements storage operations using PostgreSQL
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// NewPostgresStorage creates a new PostgresStorage instance
func NewPostgresStorage(pool *pgxpool.Pool) *PostgresStorage {
	return &PostgresStorage{pool: pool}
}

// CreateFile creates a new file record
func (s *PostgresStorage) CreateFile(ctx context.Context, file *File) error {
	if file.FileID == uuid.Nil {
		file.FileID = uuid.New()
	}

	query := `
		INSERT INTO files (file_id, filename, content_type, total_size, upload_status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at
	`

	err := s.pool.QueryRow(ctx, query,
		file.FileID,
		file.Filename,
		file.ContentType,
		file.TotalSize,
		"pending",
	).Scan(&file.CreatedAt, &file.UpdatedAt)

	if err != nil {
		if err.Error() == "ERROR: duplicate key value violates unique constraint \"files_pkey\" (SQLSTATE 23505)" {
			return fmt.Errorf("%w: file_id already exists", ErrDuplicate)
		}
		return fmt.Errorf("failed to create file: %w", err)
	}

	file.UploadStatus = "pending"
	return nil
}

// CreateFileInTx creates a file within a transaction
func (s *PostgresStorage) CreateFileInTx(ctx context.Context, tx pgx.Tx, file *File) error {
	if file.FileID == uuid.Nil {
		file.FileID = uuid.New()
	}

	query := `
		INSERT INTO files (file_id, filename, content_type, total_size, upload_status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at
	`

	err := tx.QueryRow(ctx, query,
		file.FileID,
		file.Filename,
		file.ContentType,
		file.TotalSize,
		"pending",
	).Scan(&file.CreatedAt, &file.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create file in transaction: %w", err)
	}

	file.UploadStatus = "pending"
	return nil
}

// GetFileByID retrieves a file by its ID with associated chunks
func (s *PostgresStorage) GetFileByID(ctx context.Context, fileID uuid.UUID) (*File, error) {
	query := `
		SELECT file_id, filename, content_type, total_size, upload_status, 
		       COALESCE(checksum, ''), created_at, updated_at, completed_at
		FROM files
		WHERE file_id = $1
	`

	file := &File{}
	err := s.pool.QueryRow(ctx, query, fileID).Scan(
		&file.FileID,
		&file.Filename,
		&file.ContentType,
		&file.TotalSize,
		&file.UploadStatus,
		&file.Checksum,
		&file.CreatedAt,
		&file.UpdatedAt,
		&file.CompletedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	// Load chunks
	chunks, err := s.GetChunksByFileID(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}
	file.Chunks = chunks

	return file, nil
}

// UpdateFileStatus updates the upload status of a file
func (s *PostgresStorage) UpdateFileStatus(ctx context.Context, fileID uuid.UUID, status string) error {
	query := `
		UPDATE files
		SET upload_status = $1, updated_at = NOW()
		WHERE file_id = $2
	`

	result, err := s.pool.Exec(ctx, query, status, fileID)
	if err != nil {
		return fmt.Errorf("failed to update file status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// CreateChunk creates a single chunk record
func (s *PostgresStorage) CreateChunk(ctx context.Context, chunk *Chunk) error {
	if chunk.ChunkID == uuid.Nil {
		chunk.ChunkID = uuid.New()
	}

	query := `
		INSERT INTO chunks (chunk_id, file_id, chunk_number, storage_server_id, chunk_size, chunk_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`

	err := s.pool.QueryRow(ctx, query,
		chunk.ChunkID,
		chunk.FileID,
		chunk.ChunkNumber,
		chunk.StorageServerID,
		chunk.ChunkSize,
		chunk.ChunkHash,
		"pending",
	).Scan(&chunk.CreatedAt, &chunk.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create chunk: %w", err)
	}

	chunk.Status = "pending"
	return nil
}

// CreateChunkInTx creates a chunk within a transaction
func (s *PostgresStorage) CreateChunkInTx(ctx context.Context, tx pgx.Tx, chunk *Chunk) error {
	if chunk.ChunkID == uuid.Nil {
		chunk.ChunkID = uuid.New()
	}

	query := `
		INSERT INTO chunks (chunk_id, file_id, chunk_number, storage_server_id, chunk_size, chunk_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`

	err := tx.QueryRow(ctx, query,
		chunk.ChunkID,
		chunk.FileID,
		chunk.ChunkNumber,
		chunk.StorageServerID,
		chunk.ChunkSize,
		chunk.ChunkHash,
		"pending",
	).Scan(&chunk.CreatedAt, &chunk.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create chunk in transaction: %w", err)
	}

	chunk.Status = "pending"
	return nil
}

// CreateChunksBatch creates multiple chunks in a single batch operation
func (s *PostgresStorage) CreateChunksBatch(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO chunks (chunk_id, file_id, chunk_number, storage_server_id, chunk_size, chunk_hash, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING chunk_id, created_at, updated_at
	`

	for _, chunk := range chunks {
		if chunk.ChunkID == uuid.Nil {
			chunk.ChunkID = uuid.New()
		}
		batch.Queue(query,
			chunk.ChunkID,
			chunk.FileID,
			chunk.ChunkNumber,
			chunk.StorageServerID,
			chunk.ChunkSize,
			chunk.ChunkHash,
			"pending",
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i, chunk := range chunks {
		var chunkID uuid.UUID
		err := results.QueryRow().Scan(&chunkID, &chunk.CreatedAt, &chunk.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to create chunk %d: %w", i, err)
		}
		chunk.Status = "pending"
	}

	return nil
}

// GetChunksByFileID retrieves all chunks for a file, ordered by chunk_number
func (s *PostgresStorage) GetChunksByFileID(ctx context.Context, fileID uuid.UUID) ([]*Chunk, error) {
	query := `
		SELECT chunk_id, file_id, chunk_number, storage_server_id, chunk_size, 
		       chunk_hash, status, created_at, updated_at
		FROM chunks
		WHERE file_id = $1
		ORDER BY chunk_number ASC
	`

	rows, err := s.pool.Query(ctx, query, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk
	for rows.Next() {
		chunk := &Chunk{}
		err := rows.Scan(
			&chunk.ChunkID,
			&chunk.FileID,
			&chunk.ChunkNumber,
			&chunk.StorageServerID,
			&chunk.ChunkSize,
			&chunk.ChunkHash,
			&chunk.Status,
			&chunk.CreatedAt,
			&chunk.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// CreateStorageServer creates a new storage server record or updates if address exists
func (s *PostgresStorage) CreateStorageServer(ctx context.Context, server *StorageServer) error {
	if server.ServerID == uuid.Nil {
		server.ServerID = uuid.New()
	}

	query := `
		INSERT INTO storage_servers (server_id, grpc_address, status, available_space, used_space)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (grpc_address)
		DO UPDATE SET
			status = EXCLUDED.status,
			available_space = EXCLUDED.available_space,
			used_space = EXCLUDED.used_space,
			last_heartbeat = NOW(),
			updated_at = NOW()
		RETURNING server_id, created_at, updated_at, last_heartbeat
	`

	err := s.pool.QueryRow(ctx, query,
		server.ServerID,
		server.GRPCAddress,
		"active",
		server.AvailableSpace,
		server.UsedSpace,
	).Scan(&server.ServerID, &server.CreatedAt, &server.UpdatedAt, &server.LastHeartbeat)

	if err != nil {
		return fmt.Errorf("failed to create storage server: %w", err)
	}

	server.Status = "active"
	return nil
}

// CreateStorageServerInTx creates a storage server within a transaction or updates if address exists
func (s *PostgresStorage) CreateStorageServerInTx(ctx context.Context, tx pgx.Tx, server *StorageServer) error {
	if server.ServerID == uuid.Nil {
		server.ServerID = uuid.New()
	}

	query := `
		INSERT INTO storage_servers (server_id, grpc_address, status, available_space, used_space)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (grpc_address)
		DO UPDATE SET
			status = EXCLUDED.status,
			available_space = EXCLUDED.available_space,
			used_space = EXCLUDED.used_space,
			last_heartbeat = NOW(),
			updated_at = NOW()
		RETURNING server_id, created_at, updated_at, last_heartbeat
	`

	err := tx.QueryRow(ctx, query,
		server.ServerID,
		server.GRPCAddress,
		"active",
		server.AvailableSpace,
		server.UsedSpace,
	).Scan(&server.ServerID, &server.CreatedAt, &server.UpdatedAt, &server.LastHeartbeat)

	if err != nil {
		return fmt.Errorf("failed to create storage server in transaction: %w", err)
	}

	server.Status = "active"
	return nil
}

// CreateHashRingNodes creates virtual nodes for consistent hashing
func (s *PostgresStorage) CreateHashRingNodes(ctx context.Context, serverID uuid.UUID, count int) error {
	// First, delete any existing hash ring nodes for this server
	// This handles the case where a server is restarting with the same ID
	deleteQuery := `DELETE FROM hash_ring_nodes WHERE server_id = $1`
	_, err := s.pool.Exec(ctx, deleteQuery, serverID)
	if err != nil {
		return fmt.Errorf("failed to delete existing hash ring nodes: %w", err)
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO hash_ring_nodes (server_id, virtual_node_index, hash_value)
		VALUES ($1, $2, $3)
	`

	// Generate hash values for virtual nodes
	for i := 0; i < count; i++ {
		// Simple hash generation (in production, use proper consistent hashing)
		hashValue := int64(serverID.ID()) + int64(i)*1000000
		batch.Queue(query, serverID, i, hashValue)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < count; i++ {
		_, err := results.Exec()
		if err != nil {
			return fmt.Errorf("failed to create hash ring node %d: %w", i, err)
		}
	}

	return nil
}

// UpdateHeartbeat updates the last_heartbeat timestamp for a storage server
func (s *PostgresStorage) UpdateHeartbeat(ctx context.Context, serverID uuid.UUID) error {
	query := `
		UPDATE storage_servers
		SET last_heartbeat = NOW()
		WHERE server_id = $1
	`

	result, err := s.pool.Exec(ctx, query, serverID)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetActiveStorageServers retrieves storage servers with recent heartbeats
func (s *PostgresStorage) GetActiveStorageServers(ctx context.Context, maxAge time.Duration) ([]*StorageServer, error) {
	query := `
		SELECT server_id, grpc_address, status, available_space, used_space, 
		       last_heartbeat, created_at, updated_at
		FROM storage_servers
		WHERE last_heartbeat > $1 AND status = 'active'
		ORDER BY server_id
	`

	cutoff := time.Now().Add(-maxAge)
	rows, err := s.pool.Query(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query active servers: %w", err)
	}
	defer rows.Close()

	var servers []*StorageServer
	for rows.Next() {
		server := &StorageServer{}
		err := rows.Scan(
			&server.ServerID,
			&server.GRPCAddress,
			&server.Status,
			&server.AvailableSpace,
			&server.UsedSpace,
			&server.LastHeartbeat,
			&server.CreatedAt,
			&server.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		servers = append(servers, server)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// CreateUploadSession creates a new upload session
func (s *PostgresStorage) CreateUploadSession(ctx context.Context, session *UploadSession, ttl time.Duration) error {
	if session.SessionID == uuid.Nil {
		session.SessionID = uuid.New()
	}

	expiresAt := time.Now().Add(ttl)

	query := `
		INSERT INTO upload_sessions (session_id, file_id, status, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at
	`

	err := s.pool.QueryRow(ctx, query,
		session.SessionID,
		session.FileID,
		"active",
		expiresAt,
	).Scan(&session.CreatedAt, &session.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create upload session: %w", err)
	}

	session.Status = "active"
	session.ExpiresAt = expiresAt
	return nil
}

// GetExpiredSessions retrieves expired upload sessions with their associated chunks
func (s *PostgresStorage) GetExpiredSessions(ctx context.Context) ([]*UploadSession, error) {
	query := `
		SELECT session_id, file_id, status, expires_at, created_at, updated_at
		FROM upload_sessions
		WHERE expires_at < NOW() AND status = 'active'
		ORDER BY expires_at ASC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*UploadSession
	for rows.Next() {
		session := &UploadSession{}
		err := rows.Scan(
			&session.SessionID,
			&session.FileID,
			&session.Status,
			&session.ExpiresAt,
			&session.CreatedAt,
			&session.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// DeleteUploadSession deletes an upload session
func (s *PostgresStorage) DeleteUploadSession(ctx context.Context, sessionID uuid.UUID) error {
	query := `
		DELETE FROM upload_sessions
		WHERE session_id = $1
	`

	result, err := s.pool.Exec(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete upload session: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteFile deletes a file and its associated chunks (cascades via foreign key)
func (s *PostgresStorage) DeleteFile(ctx context.Context, fileID uuid.UUID) error {
	query := `
		DELETE FROM files
		WHERE file_id = $1
	`

	result, err := s.pool.Exec(ctx, query, fileID)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// CleanupExpiredSessions deletes expired upload sessions
func (s *PostgresStorage) CleanupExpiredSessions(ctx context.Context) (int, error) {
	query := `
		DELETE FROM upload_sessions
		WHERE expires_at < NOW() AND status = 'active'
	`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired sessions: %w", err)
	}

	return int(result.RowsAffected()), nil
}
