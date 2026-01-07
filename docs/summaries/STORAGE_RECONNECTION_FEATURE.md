# Storage Server Reconnection Feature

## Overview

Added automatic reconnection mechanism for storage server gRPC connections in API Gateway. This ensures high availability and resilience when storage servers experience temporary network issues or restarts.

## Problem

Previously, if a gRPC connection to a storage server was lost due to:
- Network issues
- Storage server restart
- Temporary failures

The API Gateway would not automatically reconnect, leading to:
- Failed requests to that storage server
- Manual intervention required to restore connectivity
- Reduced system availability

## Solution

Implemented a multi-layered reconnection strategy:

### 1. On-Demand Reconnection

When a request needs to use a storage server connection, the system checks the connection state:

```go
// Check connection state before use
state := conn.GetState()
if state == connectivity.TransientFailure || state == connectivity.Shutdown {
    // Automatically reconnect
    reconnectToStorageServer(serverID, serverAddr)
}
```

**States that trigger reconnection:**
- `TransientFailure` - Connection failed but may recover
- `Shutdown` - Connection is closed

### 2. Proactive Health Checks

Background goroutine periodically checks all storage server connections:

```go
// Runs every 10 seconds
StartConnectionHealthCheck(ctx)
```

**Checks for problematic states:**
- `TransientFailure` - Connection issues
- `Shutdown` - Connection closed
- `Idle` - Connection inactive

**Actions taken:**
- Automatically reconnect to affected servers
- Log reconnection attempts and results
- Reset circuit breakers on connection failure

## Implementation Details

### New Constants

```go
const (
    // ConnectionHealthCheckInterval is how often to check connection health
    ConnectionHealthCheckInterval = 10 * time.Second
)
```

### New Methods

#### `getStorageClient()` - Enhanced
- Checks connection state before returning client
- Automatically reconnects if connection is broken
- Returns error if reconnection fails

#### `reconnectToStorageServer()`
- Closes existing broken connection
- Creates new connection with same parameters
- Updates connection map atomically
- Logs reconnection status

#### `StartConnectionHealthCheck()`
- Starts background health check loop
- Runs every 10 seconds
- Checks all storage server connections
- Triggers reconnection for unhealthy connections

#### `checkAndReconnectStorageServers()`
- Iterates through all storage connections
- Checks connection state
- Attempts reconnection for bad states
- Resets circuit breakers on failure

### Modified Files

1. **internal/api/gateway.go**
   - Added `connectivity` import for connection states
   - Enhanced `getStorageClient()` with reconnection logic
   - Added `reconnectToStorageServer()` method
   - Added `StartConnectionHealthCheck()` method
   - Added `StopConnectionHealthCheck()` method
   - Added `checkAndReconnectStorageServers()` method
   - Added `stopHealthCheck` channel to struct

2. **cmd/api-gateway/main.go**
   - Start connection health check loop on startup
   - Stop connection health check loop on shutdown

## Connection States

gRPC connections can be in the following states:

| State | Description | Action |
|-------|-------------|--------|
| `Idle` | Connection not actively used | Reconnect (proactive) |
| `Connecting` | Attempting to connect | Wait |
| `Ready` | Connection healthy | No action |
| `TransientFailure` | Temporary failure | Reconnect |
| `Shutdown` | Connection closed | Reconnect |

## Benefits

### 1. High Availability
- Automatic recovery from temporary failures
- No manual intervention required
- Reduced downtime

### 2. Resilience
- Handles storage server restarts gracefully
- Recovers from network issues automatically
- Maintains service continuity

### 3. Monitoring
- Logs all reconnection attempts
- Tracks connection states
- Provides visibility into connection health

### 4. Performance
- On-demand reconnection minimizes latency
- Proactive checks prevent request failures
- Circuit breakers prevent cascading failures

## Usage

The reconnection mechanism works automatically. No configuration changes required.

### Monitoring Logs

Look for these log messages:

```
Connection to server <uuid> is in state <state>, attempting reconnect
Reconnected to storage server: <uuid> at <address>
Failed to reconnect to server <uuid>: <error>
Connection health check loop started (interval: 10s)
```

### Metrics to Monitor

- Reconnection frequency per server
- Failed reconnection attempts
- Connection state distribution
- Time to reconnect

## Testing

### Manual Testing

1. Start API Gateway and storage servers
2. Upload a file successfully
3. Restart a storage server
4. Wait 10 seconds for health check
5. Verify reconnection in logs
6. Upload another file successfully

### Integration Tests

Existing integration tests will benefit from this feature:
- `test_e2e.py` - More resilient to temporary failures
- `test_concurrent.py` - Better handling of connection issues
- `test_grpc.py` - Automatic recovery from server restarts

## Configuration

### Environment Variables

No new environment variables required. Uses existing:
- `DATABASE_URL` - For hash ring refresh
- Connection parameters from Docker Compose

### Tuning Parameters

Can be adjusted in `internal/api/gateway.go`:

```go
// How often to check connection health
ConnectionHealthCheckInterval = 10 * time.Second

// Timeout for reconnection attempts
StorageServerConnectTimeout = 10 * time.Second
```

## Error Handling

### Reconnection Failures

If reconnection fails:
1. Error is logged
2. Circuit breaker is reset
3. Next health check will retry
4. Requests will fail until reconnection succeeds

### Circuit Breaker Integration

- Circuit breaker opens after repeated failures
- Prevents overwhelming failed servers
- Automatically resets on successful reconnection

## Future Improvements

1. **Exponential Backoff**
   - Add delay between reconnection attempts
   - Prevent rapid reconnection loops

2. **Connection Pooling**
   - Multiple connections per server
   - Better load distribution

3. **Health Check Optimization**
   - Skip healthy connections
   - Focus on problematic servers

4. **Metrics Export**
   - Prometheus metrics for reconnections
   - Grafana dashboards

5. **Configurable Intervals**
   - Environment variable for health check interval
   - Per-server reconnection strategies

## Related Components

- **Hash Ring Refresh** - Removes inactive servers
- **Circuit Breaker** - Prevents cascading failures
- **Retry Logic** - Retries failed requests
- **Cleanup Job** - Removes orphaned data

## Conclusion

The storage server reconnection feature significantly improves system reliability by automatically recovering from connection failures. This reduces operational overhead and improves user experience by maintaining service availability during temporary issues.