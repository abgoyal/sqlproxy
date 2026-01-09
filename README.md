# SQL Proxy Service

A lightweight, production-grade Go service that exposes predefined SQL Server queries as HTTP endpoints. Designed to run as a Windows service with **zero impact on the source database** and **zero maintenance** requirements.

## Features

- **Windows Service** - Auto-start on boot, proper service lifecycle
- **YAML Configuration** - Easy query management, no code changes
- **Read-only Safety** - Zero interference with production database
- **Scheduled Queries** - Run queries on cron schedule with retry
- **Structured Logging** - JSON logs with automatic rotation
- **Metrics Endpoint** - `/metrics` for monitoring
- **Request Tracing** - Wide events with request IDs
- **Runtime Config** - Change log level without restart
- **Auto-Recovery** - Automatic database reconnection
- **Health Monitoring** - Background health checks

## Reliability Features

This service is designed for long-running, fire-and-forget operation:

| Feature | Description |
|---------|-------------|
| **Log Rotation** | Automatic rotation by size, age retention, compression |
| **Metrics Endpoint** | `/metrics` endpoint for monitoring |
| **Scheduled Queries** | Cron-based execution with retry and backoff |
| **DB Health Checks** | Every 30s, auto-reconnect after 3 failures |
| **Panic Recovery** | Catches panics, logs them, returns 500 |
| **Connection Recycling** | Pool connections expire after 5 minutes |
| **Graceful Shutdown** | Closes connections cleanly |
| **Service Auto-Restart** | Windows service restarts on crash (5s, 10s, 30s delays) |

## Safety Features (Read-Only Guarantees)

This service is designed to safely read from a production SQL Server without interfering with existing applications:

### Connection Level
- **`ApplicationIntent=ReadOnly`** - Signals read-only intent to SQL Server
- **Conservative connection pool** - Max 5 connections, short lifetimes

### Session Level (set on every query)
- **`READ UNCOMMITTED` isolation** - No shared locks acquired, never blocks writers
- **`LOCK_TIMEOUT 5000`** - Fails fast (5s) if any lock needed
- **`DEADLOCK_PRIORITY LOW`** - If in deadlock, this connection is the victim
- **`IMPLICIT_TRANSACTIONS OFF`** - No accidental open transactions

### Database Level (you configure this)
- **Read-only SQL user** - Database enforces no writes possible

## SQL Server User Setup (REQUIRED)

Create a dedicated read-only user in SQL Server. Connect to RDS as admin and run:

```sql
-- Create a login (server level)
CREATE LOGIN sqlproxy_reader WITH PASSWORD = 'YourSecurePassword123!';

-- Switch to your database
USE YourDatabaseName;

-- Create user mapped to login
CREATE USER sqlproxy_reader FOR LOGIN sqlproxy_reader;

-- Grant ONLY read access (db_datareader role)
ALTER ROLE db_datareader ADD MEMBER sqlproxy_reader;

-- Explicitly deny write operations (belt and suspenders)
DENY INSERT, UPDATE, DELETE, ALTER ON SCHEMA::dbo TO sqlproxy_reader;

-- Deny DDL operations
DENY CREATE TABLE TO sqlproxy_reader;
DENY CREATE VIEW TO sqlproxy_reader;
DENY CREATE PROCEDURE TO sqlproxy_reader;
DENY CREATE FUNCTION TO sqlproxy_reader;
```

## Building

```bash
# From Linux/Mac
GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o sql-proxy.exe .

# From Windows
go build -ldflags '-s -w' -o sql-proxy.exe .
```

## Configuration Validation

Validate your config file without starting the service (like `caddy validate`):

```bash
# Validates config AND tests database connectivity
sql-proxy.exe -validate -config config.yaml
```

Example output:
```
SQL Proxy Configuration Validator
==================================
Config file: config.yaml

Server: 127.0.0.1:8081
Database: sqlproxy_reader@myserver.rds.amazonaws.com/mydb
Queries: 6 endpoints configured

Endpoints:
  GET /api/machines - list_machines (0 params)
  GET /api/machines/details - get_machine (1 params)
  GET /api/checkins - checkin_logs (3 params)

✓ Configuration valid
```

The validator checks:
- Server settings (port range, timeout values)
- Database connection settings
- Logging configuration
- Query definitions (paths, methods, SQL syntax)
- Parameter definitions (types, duplicates, reserved names)
- SQL/parameter consistency (unused params, missing definitions)
- Warnings for write operations in SQL (INSERT, UPDATE, DELETE)

## Installation

1. Copy `sql-proxy.exe` and `config.yaml` to `C:\Services\SQLProxy\`

2. Create log directory:
   ```cmd
   mkdir C:\Services\SQLProxy\logs
   ```

3. Edit `config.yaml` with your settings

4. Test interactively first:
   ```cmd
   sql-proxy.exe -config C:\Services\SQLProxy\config.yaml
   ```

5. Install as Windows service (run as Administrator):
   ```cmd
   sql-proxy.exe -install -config C:\Services\SQLProxy\config.yaml
   ```

6. Start the service:
   ```cmd
   net start SQLProxy
   ```

## Configuration

All configuration fields are **required** - no defaults are assumed. This ensures explicit, predictable behavior.

### Complete Example

```yaml
server:
  host: "127.0.0.1"
  port: 8081
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    host: "your-server.rds.amazonaws.com"
    port: 1433
    user: "sqlproxy_reader"
    password: "${DB_PASSWORD}"
    database: "YourDB"
    readonly: true

logging:
  level: "info"
  file_path: "C:/Services/SQLProxy/logs/sql-proxy.log"
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "list_machines"
    database: "primary"
    path: "/api/machines"
    method: "GET"
    description: "List all biometric machines"
    sql: |
      SELECT MachineId, MachineName, LastPingTime
      FROM Machines
      ORDER BY MachineName
```

### Multiple Database Connections

```yaml
databases:
  - name: "primary"
    host: "server1.example.com"
    port: 1433
    user: "reader"
    password: "${PRIMARY_DB_PASSWORD}"
    database: "MainDB"
    readonly: true               # Full safety measures

  - name: "reporting"
    host: "server2.example.com"
    port: 1433
    user: "writer"
    password: "${REPORTING_DB_PASSWORD}"
    database: "ReportingDB"
    readonly: false              # Allows writes

queries:
  - name: "get_machines"
    database: "primary"
    path: "/api/machines"
    method: "GET"
    sql: "SELECT * FROM Machines"

  - name: "insert_report"
    database: "reporting"
    path: "/api/reports"
    method: "POST"
    sql: "INSERT INTO Reports (Date, Data) VALUES (@date, @data)"
```

### Session Configuration

Session settings control SQL Server isolation level, lock behavior, and deadlock handling. They can be set at the connection level (as defaults) and overridden per-query.

**Implicit defaults based on `readonly` flag:**

| Setting | `readonly: true` | `readonly: false` |
|---------|------------------|-------------------|
| `isolation` | `read_uncommitted` | `read_committed` |
| `lock_timeout_ms` | `5000` | `5000` |
| `deadlock_priority` | `low` | `low` |
| `ApplicationIntent` | `ReadOnly` | (none) |

**Override at connection level:**

```yaml
databases:
  - name: "primary"
    readonly: true
    # Override implicit defaults for all queries on this connection
    isolation: "read_committed"      # Need consistent reads
    lock_timeout_ms: 10000           # Wait longer for locks
    deadlock_priority: "normal"      # Don't always be the victim
```

**Override at query level:**

```yaml
queries:
  - name: "critical_read"
    database: "primary"
    path: "/api/balance"
    method: "GET"
    # Override for this specific query
    isolation: "repeatable_read"     # Need stable reads within query
    lock_timeout_ms: 30000           # Important query, wait longer
    sql: "SELECT Balance FROM Accounts WHERE Id = @id"
```

**Available values:**

| Setting | Values |
|---------|--------|
| `isolation` | `read_uncommitted`, `read_committed`, `repeatable_read`, `serializable`, `snapshot` |
| `lock_timeout_ms` | Any non-negative integer (milliseconds) |
| `deadlock_priority` | `low`, `normal`, `high` |

**Resolution order:** Query settings → Connection settings → Implicit defaults (based on `readonly`)

### Validation

Config validation enforces:
- All required fields present (no defaults)
- Queries with INSERT/UPDATE/DELETE on read-only connections → **error**
- Unknown database references → **error**
- Invalid isolation level or deadlock priority → **error**
- Unused database connections → **warning**

### Timeout Configuration

Timeouts are configurable at three levels (in order of precedence):

1. **Request parameter** (`_timeout`) - Caller specifies per-request
2. **Query config** (`timeout_sec`) - Per-query default in YAML
3. **Server config** (`default_timeout_sec`) - Global default

```bash
# Override timeout for this request
curl "http://localhost:8081/api/checkins?_timeout=120"
```

### Pagination and Row Limits

Pagination is handled at the query level using SQL Server syntax. This is more efficient than service-level truncation because SQL Server stops scanning once the limit is reached.

#### Simple Limit (TOP)

For queries that just need a max row count:

```yaml
- name: "recent_checkins"
  path: "/api/checkins/recent"
  method: "GET"
  description: "Get most recent check-ins"
  sql: |
    SELECT TOP (@limit)
      EmployeeId, PunchTime, MachineId
    FROM AttendanceLog
    ORDER BY PunchTime DESC
  parameters:
    - name: "limit"
      type: "int"
      required: false
      default: "100"
```

```bash
# Get last 50 check-ins
curl "http://localhost:8081/api/checkins/recent?limit=50"
```

#### Offset Pagination (OFFSET/FETCH)

For paginated results with page navigation:

```yaml
- name: "checkins_paginated"
  path: "/api/checkins/page"
  method: "GET"
  description: "Get check-ins with pagination"
  sql: |
    SELECT
      EmployeeId, PunchTime, MachineId
    FROM AttendanceLog
    WHERE PunchTime >= @fromDate
    ORDER BY PunchTime DESC
    OFFSET @offset ROWS FETCH NEXT @limit ROWS ONLY
  parameters:
    - name: "fromDate"
      type: "datetime"
      required: true
    - name: "offset"
      type: "int"
      required: false
      default: "0"
    - name: "limit"
      type: "int"
      required: false
      default: "100"
```

```bash
# Page 1 (first 100)
curl "http://localhost:8081/api/checkins/page?fromDate=2024-01-01&offset=0&limit=100"

# Page 2 (next 100)
curl "http://localhost:8081/api/checkins/page?fromDate=2024-01-01&offset=100&limit=100"

# Page 3
curl "http://localhost:8081/api/checkins/page?fromDate=2024-01-01&offset=200&limit=100"
```

#### Getting Total Count

For UI pagination, you often need the total count. Create a separate endpoint:

```yaml
- name: "checkins_count"
  path: "/api/checkins/count"
  method: "GET"
  description: "Get total count of check-ins"
  sql: |
    SELECT COUNT(*) AS total_count
    FROM AttendanceLog
    WHERE PunchTime >= @fromDate
  parameters:
    - name: "fromDate"
      type: "datetime"
      required: true
```

```bash
# Get total count
curl "http://localhost:8081/api/checkins/count?fromDate=2024-01-01"
# Returns: {"success":true,"data":[{"total_count":15234}],"count":1}
```

#### Keyset Pagination (Most Efficient for Large Tables)

For very large tables, keyset pagination is more efficient than OFFSET:

```yaml
- name: "checkins_keyset"
  path: "/api/checkins/after"
  method: "GET"
  description: "Get check-ins after a specific ID (keyset pagination)"
  sql: |
    SELECT TOP (@limit)
      LogId, EmployeeId, PunchTime, MachineId
    FROM AttendanceLog
    WHERE LogId > @afterId
    ORDER BY LogId ASC
  parameters:
    - name: "afterId"
      type: "int"
      required: false
      default: "0"
    - name: "limit"
      type: "int"
      required: false
      default: "100"
```

```bash
# First page
curl "http://localhost:8081/api/checkins/after?afterId=0&limit=100"
# Returns rows with LogId 1-100, last row has LogId=100

# Next page - use last LogId from previous response
curl "http://localhost:8081/api/checkins/after?afterId=100&limit=100"
# Returns rows with LogId 101-200
```

### Optional Parameters and NULL

When an optional parameter is not provided and has no default, it's passed to SQL Server as `NULL`. Write your SQL to handle this:

```yaml
# BAD - Won't match any rows when status is NULL
- name: "bad_example"
  sql: |
    SELECT * FROM Users WHERE status = @status
  parameters:
    - name: "status"
      type: "string"
      required: false

# GOOD - Returns all rows when status is not provided
- name: "good_example"
  sql: |
    SELECT * FROM Users
    WHERE (@status IS NULL OR status = @status)
  parameters:
    - name: "status"
      type: "string"
      required: false
```

This pattern lets optional parameters act as filters only when provided.

### Memory Considerations

All query results are loaded into memory before JSON serialization. For large result sets:

- Always use `TOP @limit` or `OFFSET/FETCH` in queries
- Set reasonable default limits (e.g., 100-1000 rows)
- Monitor memory usage for queries returning large text/blob columns

## Scheduled Queries

Queries can run automatically on a cron schedule, independent of HTTP requests. This is useful for periodic data collection, health monitoring, or generating reports.

### Basic Schedule

Add a `schedule` block to any query:

```yaml
queries:
  - name: "machine_status_check"
    path: "/api/status"           # Optional - omit for schedule-only queries
    method: "GET"
    description: "Check machine status"
    sql: |
      SELECT COUNT(*) AS online FROM Machines WHERE IsOnline = 1
    schedule:
      cron: "*/5 * * * *"         # Every 5 minutes
      log_results: false          # Just log row count (default)
```

### Schedule-Only Queries

Omit `path` to create a query that only runs on schedule (no HTTP endpoint):

```yaml
  - name: "daily_attendance_report"
    description: "Count yesterday's attendance"
    sql: |
      SELECT COUNT(*) AS total, COUNT(DISTINCT EmployeeId) AS unique_employees
      FROM AttendanceLog
      WHERE CAST(PunchTime AS DATE) = CAST(@reportDate AS DATE)
    parameters:
      - name: "reportDate"
        type: "datetime"
        required: true
    schedule:
      cron: "0 8 * * *"           # 8 AM daily
      params:
        reportDate: "yesterday"   # Dynamic date value
      log_results: true           # Log first 10 rows
```

### Dynamic Date Values

The following special values are resolved at execution time:

| Value | Resolves To |
|-------|-------------|
| `now` | Current timestamp |
| `today` | Start of today (00:00:00) |
| `yesterday` | Start of yesterday |
| `tomorrow` | Start of tomorrow |

### Cron Expression Format

Standard 5-field cron format: `minute hour day-of-month month day-of-week`

| Expression | Description |
|------------|-------------|
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour |
| `0 8 * * *` | Daily at 8 AM |
| `0 8 * * 1` | Mondays at 8 AM |
| `0 0 1 * *` | First day of month at midnight |

### Retry Behavior

Scheduled queries automatically retry on failure:
- 3 attempts total
- Exponential backoff: 1s, 5s, 25s between retries
- Logs error after all retries exhausted

### Logging

Scheduled query execution is logged:

```json
{"time":"...","level":"INFO","msg":"scheduled_query_started","query_name":"daily_report","cron":"0 8 * * *"}
{"time":"...","level":"INFO","msg":"scheduled_query_completed","query_name":"daily_report","row_count":1,"duration_ms":45}
```

With `log_results: true`, the first 10 rows are included:

```json
{"time":"...","level":"INFO","msg":"scheduled_query_completed","query_name":"daily_report","row_count":1,"duration_ms":45,"sample_rows":"[{\"total\":1523,\"unique_employees\":342}]"}
```

## Logging

Uses Go's `log/slog` with JSON output. Rotation via lumberjack.

**Output destination:**
- Interactive mode (`sql-proxy.exe -config ...`): stdout
- Service mode (Windows service): file only

### Log Format (slog JSON, one line per entry)

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"request_completed","request_id":"a1b2c3d4","endpoint":"/api/machines","query_name":"list_machines","query_duration_ms":45,"row_count":150,"total_duration_ms":48}
```

### Log Levels

| Level | What's Logged |
|-------|--------------|
| `debug` | Everything: request received, params parsed, query start/end |
| `info` | Request completions, service lifecycle, config changes |
| `warn` | Slow queries (>80% of timeout), health check failures, rejected requests |
| `error` | Query failures, panics, database errors |

### Change Log Level at Runtime

No restart required:

```bash
# Check current level
curl http://localhost:8081/config/loglevel

# Change to debug
curl -X POST "http://localhost:8081/config/loglevel?level=debug"

# Back to info for production
curl -X POST "http://localhost:8081/config/loglevel?level=info"
```

## Metrics

Get metrics via the `/metrics` endpoint:

```bash
curl http://localhost:8081/metrics
```

Response:
```json
{
  "timestamp": "2024-01-15T10:35:00Z",
  "uptime_sec": 86400,
  "total_requests": 15234,
  "total_errors": 12,
  "total_timeouts": 2,
  "db_healthy": true,
  "endpoints": {
    "/api/machines": {
      "endpoint": "/api/machines",
      "query_name": "list_machines",
      "request_count": 1523,
      "error_count": 0,
      "timeout_count": 0,
      "total_rows": 228450,
      "avg_duration_ms": 45.2,
      "max_duration_ms": 234,
      "min_duration_ms": 12,
      "avg_query_ms": 38.1
    }
  },
  "runtime": {
    "go_version": "go1.21.0",
    "goroutines": 12,
    "num_cpu": 4,
    "mem_alloc_bytes": 2458624,
    "mem_total_alloc": 158236744,
    "mem_sys_bytes": 12648448,
    "mem_heap_objects": 18542,
    "gc_runs": 245,
    "gc_pause_total_ns": 12456789,
    "gc_last_pause_ns": 48521
  }
}
```

### Runtime Metrics

| Field | Description |
|-------|-------------|
| `goroutines` | Active goroutines (should be stable, ~10-20) |
| `mem_alloc_bytes` | Currently allocated heap memory |
| `mem_total_alloc` | Cumulative bytes allocated (grows over time) |
| `mem_sys_bytes` | Total memory obtained from OS |
| `mem_heap_objects` | Number of allocated heap objects |
| `gc_runs` | Number of completed GC cycles |
| `gc_pause_total_ns` | Total time spent in GC pauses |
| `gc_last_pause_ns` | Duration of most recent GC pause |

**What to watch for:**
- `mem_alloc_bytes` growing unbounded → memory leak
- `goroutines` growing unbounded → goroutine leak
- `gc_last_pause_ns` > 10ms → GC pressure, may need tuning

## API Endpoints

### Service Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | List all query endpoints with parameters |
| `/health` | GET | Health check (returns 503 if DB disconnected) |
| `/metrics` | GET | Current metrics snapshot |
| `/openapi.json` | GET | OpenAPI 3.0 specification |
| `/config/loglevel` | GET/POST | View/change log level |

### OpenAPI / Swagger

The service auto-generates an OpenAPI 3.0 spec at runtime:

```bash
curl http://localhost:8081/openapi.json
```

You can use this with:
- **Swagger UI** - Paste URL into https://petstore.swagger.io or run Swagger UI locally
- **Postman** - Import > Link > `http://localhost:8081/openapi.json`
- **Code generators** - Generate client SDKs for any language

The spec includes all configured query endpoints with their parameters, types, and response schemas.

### Query Endpoints

Defined in `config.yaml`. Each returns:

```json
{
  "success": true,
  "data": [...],
  "count": 42,
  "timeout_sec": 30,
  "request_id": "a1b2c3d4e5f6"
}
```

The `request_id` can be used to trace the request in logs.

## Request Tracing

Every request gets a unique ID for end-to-end tracing.

### Caller-Provided Request ID

If your Spring Boot service sends a request ID, sql-proxy will use it:

```bash
# Spring Boot sends its trace ID
curl -H "X-Request-ID: spring-trace-abc123" http://localhost:8081/api/machines
```

The service checks these headers (in order):
1. `X-Request-ID`
2. `X-Correlation-ID`

If neither is provided, a new ID is generated.

### Response

The request ID appears in:
- Response JSON: `"request_id": "spring-trace-abc123"`
- Response header: `X-Request-ID: spring-trace-abc123`
- All log entries for that request

### Tracing Through Logs

```bash
# Find all log entries for a request
grep "spring-trace-abc123" sql-proxy.log
```

## Response Compression

Responses are automatically gzip-compressed when the client sends `Accept-Encoding: gzip`:

```bash
# Without compression
curl http://localhost:8081/api/machines
# Response: ~50KB

# With compression
curl -H "Accept-Encoding: gzip" http://localhost:8081/api/machines | gunzip
# Response: ~8KB (compressed)
```

Most HTTP clients (including Spring's RestTemplate/WebClient) send this header by default.

## Caddy Configuration

```
your-domain.com {
    handle /sqlproxy/* {
        uri strip_prefix /sqlproxy
        reverse_proxy localhost:8081
    }
}
```

## Security Checklist

1. **Read-only SQL user** - `db_datareader` role only, explicit DENYs
2. **No arbitrary SQL** - Only predefined queries executable
3. **Parameterized queries** - SQL injection safe
4. **HTTPS via Caddy** - Encrypt all traffic
5. **Non-blocking reads** - `READ UNCOMMITTED` isolation
6. **Configurable timeouts** - Caller-controlled with server max
7. **Lock timeout (5s)** - Fails fast if lock needed
8. **Low deadlock priority** - Always yields to production app

## Troubleshooting

### Service won't start
1. Check Windows Event Viewer > Application logs
2. Run interactively to see errors: `sql-proxy.exe -config config.yaml`
3. Verify config.yaml syntax with a YAML validator

### Database connection issues
- Check `/health` endpoint for status
- Look for `health_check_failed` in logs
- Verify security group allows port 1433
- Check credentials in config

### High latency
- Check `/metrics` for `avg_duration_ms` and `max_duration_ms`
- Look for `slow_query` warnings in logs
- Consider adding indexes on SQL Server side
- Increase `timeout_sec` for known slow queries

### Disk filling up
- Logs rotate automatically, but check `max_backups` setting
- Compressed logs use `.gz` extension

### Changing configuration
- Most changes require service restart
- Log level can be changed at runtime via `/config/loglevel`

### Updating the service

To deploy a new version:

```cmd
# 1. Stop the service
net stop SQLProxy

# 2. Replace the executable
copy /Y sql-proxy-new.exe C:\Services\SQLProxy\sql-proxy.exe

# 3. Start the service
net start SQLProxy

# 4. Verify
curl http://localhost:8081/health
```

For config changes only (no exe update):

```cmd
net stop SQLProxy
# Edit config.yaml
net start SQLProxy
```

## Pre-Deployment Checklist

Before deploying to production:

```bash
# 1. Validate config and test database connectivity
sql-proxy.exe -validate -config config.yaml

# 2. Create required directories
mkdir C:\Services\SQLProxy\logs

# 3. Test interactively
sql-proxy.exe -config config.yaml
# In another terminal:
curl http://localhost:8081/health
curl http://localhost:8081/

# 4. Test each endpoint manually
curl "http://localhost:8081/api/your-endpoint?param=value"

# 5. Install as service (run as Administrator)
sql-proxy.exe -install -config C:\Services\SQLProxy\config.yaml

# 6. Start and verify
net start SQLProxy
curl http://localhost:8081/health

# 7. Check Windows Event Viewer for any errors
# 8. Monitor logs: C:\Services\SQLProxy\logs\sql-proxy.log
```

### Production Recommendations

- **Caddy/nginx in front**: Don't expose sql-proxy directly to the internet
- **Monitor `/health`**: Set up alerting on 503 responses
- **Review metrics**: Check `/metrics` endpoint for slow queries
- **Log level**: Use `info` in production, `debug` only for troubleshooting
- **Backup config**: Keep config.yaml in version control
