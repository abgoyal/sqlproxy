# SQL Proxy Service

A lightweight, production-grade Go service that exposes predefined SQL queries as HTTP endpoints. Supports **SQL Server** and **SQLite** databases. Runs as a system service on **Windows**, **Linux**, and **macOS** with **zero impact on the source database** and **zero maintenance** requirements.

## Features

- **Multi-Database Support** - SQL Server and SQLite (same query syntax)
- **Cross-Platform Service** - Windows Service, Linux systemd, macOS launchd
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
| **Service Auto-Restart** | Automatic restart on crash (Windows/Linux/macOS) |

## Safety Features (Read-Only Guarantees)

This service is designed to safely read from production databases without interfering with existing applications.

### SQL Server Safety

| Level | Setting | Purpose |
|-------|---------|---------|
| Connection | `ApplicationIntent=ReadOnly` | Signals read-only intent, enables AG routing |
| Connection | Max 5 connections, 5min lifetime | Conservative pool footprint |
| Session | `READ UNCOMMITTED` isolation | No shared locks, never blocks writers |
| Session | `LOCK_TIMEOUT 5000` | Fails fast (5s) if any lock needed |
| Session | `DEADLOCK_PRIORITY LOW` | If in deadlock, this connection is the victim |
| Session | `IMPLICIT_TRANSACTIONS OFF` | No accidental open transactions |
| Database | Read-only SQL user | Database enforces no writes possible |

### SQLite Safety

| Level | Setting | Purpose |
|-------|---------|---------|
| Connection | `mode=ro` (for readonly) | Prevents any writes at driver level |
| Connection | `_txlock=immediate` (for writes) | Prevents write deadlocks |
| Session | `journal_mode=WAL` | Concurrent reads during writes |
| Session | `busy_timeout=5000` | Waits 5s instead of failing immediately on lock |
| Session | `synchronous=NORMAL` | Safe for WAL mode, better performance |

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
# Build for current platform (development)
go build -ldflags '-s -w' -o sql-proxy .

# Build with version and build time (release)
VERSION=1.0.0
BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
go build -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o sql-proxy .

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o sql-proxy.exe .

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o sql-proxy .

# Cross-compile for macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o sql-proxy .

# Cross-compile for macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o sql-proxy .

# Using make (recommended - sets version and build time automatically)
make build              # Current platform
make build-windows      # Windows
make build-linux        # Linux
make build-darwin       # macOS Intel
make build-darwin-arm   # macOS Apple Silicon

# Check version
./sql-proxy -version
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

### Windows

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

6. Manage the service:
   ```cmd
   sql-proxy.exe -start      # Start the service
   sql-proxy.exe -stop       # Stop the service
   sql-proxy.exe -restart    # Restart the service
   sql-proxy.exe -status     # Check service status
   sql-proxy.exe -uninstall  # Remove the service
   ```

### Linux (systemd)

1. Copy files to installation directory:
   ```bash
   sudo mkdir -p /opt/sql-proxy
   sudo mkdir -p /var/log/sql-proxy
   sudo cp sql-proxy /opt/sql-proxy/
   sudo cp config.yaml /opt/sql-proxy/
   sudo chmod +x /opt/sql-proxy/sql-proxy
   ```

2. Create service user:
   ```bash
   sudo useradd -r -s /bin/false sqlproxy
   sudo chown -R sqlproxy:sqlproxy /opt/sql-proxy
   sudo chown -R sqlproxy:sqlproxy /var/log/sql-proxy
   ```

3. Generate the systemd unit file:
   ```bash
   # Generate the unit file from template with your config path
   /opt/sql-proxy/sql-proxy -install -config /opt/sql-proxy/config.yaml

   # For multiple instances, use -service-name:
   # /opt/sql-proxy/sql-proxy -install -service-name sql-proxy-prod -config /opt/sql-proxy/prod.yaml
   # /opt/sql-proxy/sql-proxy -install -service-name sql-proxy-staging -config /opt/sql-proxy/staging.yaml
   ```

   This generates a templated systemd unit file with your executable path, config path, and service name embedded. Follow the printed instructions to create the service file.

4. Install and enable:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable sql-proxy   # Enable auto-start
   sudo systemctl start sql-proxy    # Start the service
   ```

5. Manage the service:
   ```bash
   sudo systemctl stop sql-proxy     # Stop the service
   sudo systemctl restart sql-proxy  # Restart the service
   sudo systemctl status sql-proxy   # Check status
   journalctl -u sql-proxy -f        # View logs
   ```

### macOS (launchd)

1. Copy files to installation directory:
   ```bash
   sudo mkdir -p /usr/local/etc/sql-proxy
   sudo mkdir -p /usr/local/var/log/sql-proxy
   sudo cp sql-proxy /usr/local/bin/
   sudo cp config.yaml /usr/local/etc/sql-proxy/
   sudo chmod +x /usr/local/bin/sql-proxy
   ```

2. Create service user (optional):
   ```bash
   sudo dscl . -create /Users/_sqlproxy
   sudo dscl . -create /Users/_sqlproxy UserShell /usr/bin/false
   sudo dscl . -create /Users/_sqlproxy RealName "SQL Proxy Service"
   sudo dscl . -create /Users/_sqlproxy UniqueID 299
   sudo dscl . -create /Users/_sqlproxy PrimaryGroupID 299
   ```

3. Generate the launchd plist file:
   ```bash
   # Generate the plist file from template with your config path
   /usr/local/bin/sql-proxy -install -config /usr/local/etc/sql-proxy/config.yaml

   # For multiple instances, use -service-name:
   # /usr/local/bin/sql-proxy -install -service-name sql-proxy-prod -config /usr/local/etc/sql-proxy/prod.yaml
   ```

   This generates a templated launchd plist with your executable path, config path, and service name embedded. Follow the printed instructions to create the plist file.

4. Manage the service:
   ```bash
   sudo launchctl load /Library/LaunchDaemons/com.sqlproxy.sql-proxy.plist     # Start
   sudo launchctl unload /Library/LaunchDaemons/com.sqlproxy.sql-proxy.plist   # Stop
   sudo launchctl list | grep sqlproxy                                          # Status
   tail -f /usr/local/var/log/sql-proxy/sql-proxy.log                          # View logs
   ```

## Configuration

All configuration fields are **required** unless noted otherwise. This ensures explicit, predictable behavior.

### Complete Example

```yaml
server:
  host: "127.0.0.1"
  port: 8081
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlserver"             # sqlserver (default), sqlite
    host: "your-server.rds.amazonaws.com"
    port: 1433
    user: "sqlproxy_reader"
    password: "${DB_PASSWORD}"
    database: "YourDB"
    readonly: true                # Defaults to true if omitted

logging:
  level: "info"
  file_path: "./logs/sql-proxy.log"  # Empty string = log to stdout (interactive mode)
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
    type: "sqlserver"
    host: "server1.example.com"
    port: 1433
    user: "reader"
    password: "${PRIMARY_DB_PASSWORD}"
    database: "MainDB"
    readonly: true               # Full safety measures

  - name: "reporting"
    type: "sqlserver"
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

### SQLite Support

SQLite databases are supported for testing and lightweight deployments. Use `type: "sqlite"` with a `path` instead of host/port/user/password:

```yaml
databases:
  - name: "test_db"
    type: "sqlite"
    path: ":memory:"             # In-memory database (useful for testing)
    # Or use a file path:
    # path: "/data/app.db"

    # SQLite-specific settings (optional)
    journal_mode: "wal"          # wal (default), delete, truncate, memory, off
    busy_timeout_ms: 5000        # Wait time for locked database (default: 5000)

queries:
  - name: "list_items"
    database: "test_db"
    path: "/api/items"
    method: "GET"
    sql: "SELECT * FROM items ORDER BY name"
```

#### SQLite Configuration Options

| Setting | Default | Description |
|---------|---------|-------------|
| `path` | (required) | File path or `:memory:` for in-memory database |
| `readonly` | `true` | Opens database in read-only mode |
| `journal_mode` | `wal` | WAL mode enables concurrent reads during writes |
| `busy_timeout_ms` | `5000` | How long to wait when database is locked (ms) |

### Connection Pool Configuration

All database types support connection pool tuning. These settings are optional and have sensible defaults:

```yaml
databases:
  - name: "primary"
    type: "sqlserver"
    host: "server.example.com"
    # ... other connection settings ...

    # Connection pool settings (optional)
    max_open_conns: 10       # Max open connections (default: 5)
    max_idle_conns: 5        # Max idle connections (default: 2)
    conn_max_lifetime: 300   # Max connection lifetime in seconds (default: 300)
    conn_max_idle_time: 120  # Max idle time in seconds (default: 120)
```

| Setting | Default | Description |
|---------|---------|-------------|
| `max_open_conns` | 5 | Maximum number of open connections to the database |
| `max_idle_conns` | 2 | Maximum number of idle connections in the pool |
| `conn_max_lifetime` | 300 | Maximum time (seconds) a connection can be reused |
| `conn_max_idle_time` | 120 | Maximum time (seconds) a connection can be idle |

**When to tune:**
- High-throughput services: Increase `max_open_conns` and `max_idle_conns`
- Connection limits on DB server: Reduce `max_open_conns`
- Network instability: Reduce `conn_max_lifetime` to recycle connections more often

#### SQLite Automatic Pragmas

The driver automatically configures SQLite for optimal concurrent performance:

| Pragma | Value | Purpose |
|--------|-------|---------|
| `busy_timeout` | 5000ms | **Critical**: Wait instead of failing on lock |
| `journal_mode` | WAL | Readers don't block writers |
| `synchronous` | NORMAL | Safe for WAL, faster than FULL |
| `foreign_keys` | ON | Enable referential integrity |
| `temp_store` | MEMORY | Faster temp table operations |
| `cache_size` | 64MB | Larger cache for concurrent reads |
| `mmap_size` | 256MB | Memory-mapped I/O for faster reads |
| `wal_autocheckpoint` | 1000 | Prevents WAL file from growing too large |

#### SQLite Operational Considerations

**Concurrency Model:**
- WAL mode allows **unlimited concurrent readers** with **one writer**
- Writers don't block readers; readers don't block writers
- Multiple simultaneous write attempts will queue (up to `busy_timeout`)
- After `busy_timeout`, write returns "database is locked" error

**When to use SQLite:**
- Testing and development (`:memory:` for fast unit tests)
- Single-server deployments with moderate write load
- Read-heavy workloads with occasional writes
- Embedded/edge deployments

**When NOT to use SQLite:**
- High write concurrency (multiple writers)
- Multi-server deployments (no network access)
- Very large databases (>100GB)

**File vs In-Memory:**

| Mode | Path | Persistence | Use Case |
|------|------|-------------|----------|
| File | `/data/app.db` | Survives restart | Production |
| In-memory | `:memory:` | Lost on restart | Testing |

**Query Syntax:**
- All queries use `@param` syntax (driver translates to `$param` for SQLite)
- Use `LIMIT` instead of `TOP` for pagination:
  ```sql
  -- SQL Server style (works on SQL Server)
  SELECT TOP (@limit) * FROM items

  -- SQLite style (works on SQLite)
  SELECT * FROM items LIMIT @limit
  ```

### Session Configuration

Session settings control database behavior at query execution time. Settings can be defined at connection level (defaults) and overridden per-query.

**Database-Specific Behavior:**

| Setting | SQL Server | SQLite |
|---------|------------|--------|
| `isolation` | Sets transaction isolation level | Ignored (SQLite has limited isolation) |
| `lock_timeout_ms` | `SET LOCK_TIMEOUT` | Maps to `busy_timeout` pragma |
| `deadlock_priority` | `SET DEADLOCK_PRIORITY` | Ignored (SQLite handles differently) |

**SQL Server Implicit Defaults (based on `readonly` flag):**

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

Pagination is handled at the query level using database-native syntax. This is more efficient than service-level truncation because the database stops scanning once the limit is reached.

> **Note:** SQL Server uses `TOP`, SQLite uses `LIMIT`. Write queries for your specific database type.

#### Simple Limit (SQL Server: TOP, SQLite: LIMIT)

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

**Output destination (controlled by `file_path`):**
- `file_path: ""` (empty): logs to stdout (default for interactive mode)
- `file_path: "/path/to/file.log"`: logs to file with rotation (recommended for service mode)

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
  "version": "1.0.0",
  "build_time": "2024-01-15T10:30:00Z",
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
| `/health` | GET | Aggregate health check (always 200, parse `status` field) |
| `/health/{dbname}` | GET | Per-database health check (200 or 404 if not found) |
| `/metrics` | GET | Current metrics snapshot |
| `/openapi.json` | GET | OpenAPI 3.0 specification |
| `/config/loglevel` | GET/POST | View/change log level |
| `/cache/clear` | POST/DELETE | Clear cache (all or specific endpoint via `?endpoint=/api/path`) |

### Health Check Design

Health endpoints always return HTTP 200 with status details in the response body. This design:
- Avoids confusion with proxy/middleware 503 errors
- Allows clients to always receive health details
- Enables monitoring tools to parse JSON for actual status

**Aggregate health (`/health`):**
```json
{
  "status": "healthy",
  "databases": {
    "primary": "connected",
    "reporting": "connected"
  },
  "uptime": "24h35m12s"
}
```

Status values:
- `healthy` - All databases connected
- `degraded` - Some databases connected, some disconnected
- `unhealthy` - All databases disconnected

**Per-database health (`/health/{dbname}`):**
```json
{
  "database": "primary",
  "status": "connected",
  "type": "sqlserver",
  "readonly": true
}
```

Returns 404 only if the database name doesn't exist in configuration.

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

### JSON Body Support for POST

POST endpoints accept parameters via JSON body (in addition to query parameters and form data):

```bash
# JSON body
curl -X POST http://localhost:8081/api/reports \
  -H "Content-Type: application/json" \
  -d '{"date": "2024-01-15", "status": "active", "count": 100}'

# Query parameters still work
curl -X POST "http://localhost:8081/api/reports?date=2024-01-15&status=active"

# Form data still works
curl -X POST http://localhost:8081/api/reports \
  -d "date=2024-01-15" -d "status=active"
```

**Important notes:**
- JSON body must be flat unless parameter type is `json` or an array type
- Query parameters override JSON body values
- Maximum request body size: 1MB
- Nested objects/arrays are rejected for scalar types (string, int, etc.)

### Parameter Types

The following parameter types are supported:

| Type | Description | Example Value |
|------|-------------|---------------|
| `string` | Text value (default) | `"hello"` |
| `int`, `integer` | Integer number | `42` |
| `float`, `double` | Floating point number | `3.14` |
| `bool`, `boolean` | Boolean value | `true`, `false`, `1`, `0` |
| `datetime`, `date` | Date/time value | `"2024-01-15"`, `"2024-01-15T10:30:00Z"` |
| `json` | Any JSON value (serialized to string) | `{"key": "value"}` |
| `int[]` | Array of integers | `[1, 2, 3]` |
| `string[]` | Array of strings | `["a", "b", "c"]` |
| `float[]` | Array of numbers | `[1.5, 2.5]` |
| `bool[]` | Array of booleans | `[true, false]` |

### JSON Type Parameter

Use `json` type when you need to accept nested objects or arbitrary JSON. The value is serialized to a JSON string for use with SQL JSON functions:

```yaml
queries:
  - name: "save_config"
    path: "/api/config"
    method: "POST"
    sql: |
      INSERT INTO configs (name, data) VALUES (@name, @data)
    parameters:
      - name: "name"
        type: "string"
        required: true
      - name: "data"
        type: "json"
        required: true
```

```bash
# Send nested JSON object
curl -X POST http://localhost:8081/api/config \
  -H "Content-Type: application/json" \
  -d '{"name": "settings", "data": {"theme": "dark", "notifications": {"email": true}}}'
```

In SQL, use JSON functions to extract values:

```sql
-- SQL Server: JSON_VALUE
SELECT name, JSON_VALUE(data, '$.theme') AS theme FROM configs

-- SQLite: json_extract
SELECT name, json_extract(data, '$.theme') AS theme FROM configs
```

### Array Type Parameters

Use array types (`int[]`, `string[]`, etc.) for IN clause queries. Arrays are serialized to JSON strings and used with `json_each` (SQLite) or `OPENJSON` (SQL Server):

```yaml
queries:
  - name: "get_users_by_ids"
    path: "/api/users/batch"
    method: "POST"
    sql: |
      SELECT * FROM users
      WHERE id IN (SELECT value FROM json_each(@ids))
    parameters:
      - name: "ids"
        type: "int[]"
        required: true

  - name: "filter_by_status"
    path: "/api/users/filter"
    method: "POST"
    sql: |
      SELECT * FROM users
      WHERE status IN (SELECT value FROM json_each(@statuses))
    parameters:
      - name: "statuses"
        type: "string[]"
        required: true
```

```bash
# Get users with IDs 1, 2, and 3
curl -X POST http://localhost:8081/api/users/batch \
  -H "Content-Type: application/json" \
  -d '{"ids": [1, 2, 3]}'

# Filter by multiple statuses
curl -X POST http://localhost:8081/api/users/filter \
  -H "Content-Type: application/json" \
  -d '{"statuses": ["active", "pending"]}'
```

For SQL Server, use `OPENJSON`:

```sql
SELECT * FROM users
WHERE id IN (SELECT CAST(value AS INT) FROM OPENJSON(@ids))
```

**Array type validation:**
- Array elements must match the declared base type
- Mixed types are rejected (e.g., `[1, "two"]` for `int[]`)

### JSON Column Output

By default, JSON stored in database columns is returned as escaped strings. Use `json_columns` to parse them as objects in the response:

```yaml
queries:
  - name: "get_config"
    path: "/api/config"
    method: "GET"
    database: "primary"
    sql: "SELECT id, name, data FROM configs WHERE name = @name"
    json_columns: ["data"]   # Parse 'data' column as JSON
    parameters:
      - name: "name"
        type: "string"
        required: true
```

**Without `json_columns`** (default):
```json
{
  "data": [{"id": 1, "name": "settings", "data": "{\"theme\":\"dark\"}"}]
}
```

**With `json_columns: ["data"]`**:
```json
{
  "data": [{"id": 1, "name": "settings", "data": {"theme": "dark"}}]
}
```

**Notes:**
- Only string columns are parsed; other types are left unchanged
- Empty strings are left as empty strings
- Invalid JSON in a configured column returns a 500 error
- Non-existent columns are silently ignored

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

**All Databases:**
1. **No arbitrary SQL** - Only predefined queries executable
2. **Parameterized queries** - SQL injection safe (uses named parameters)
3. **HTTPS via Caddy** - Encrypt all traffic
4. **Configurable timeouts** - Caller-controlled with server max

**SQL Server Specific:**
5. **Read-only SQL user** - `db_datareader` role only, explicit DENYs
6. **Non-blocking reads** - `READ UNCOMMITTED` isolation
7. **Lock timeout (5s)** - Fails fast if lock needed
8. **Low deadlock priority** - Always yields to production app
9. **ApplicationIntent=ReadOnly** - Enables AG read routing

**SQLite Specific:**
5. **Read-only mode** - `readonly: true` opens DB in read-only mode
6. **File permissions** - Ensure appropriate filesystem permissions
7. **WAL mode** - Better concurrency, readers don't block writers
8. **Busy timeout** - Prevents immediate failure on lock contention

## Troubleshooting

### Service won't start

**Windows:**
1. Check log file: `C:\Services\SQLProxy\logs\sql-proxy.log`
2. Run interactively: `sql-proxy.exe -config config.yaml`

**Linux:**
1. Check journal: `journalctl -u sql-proxy -n 50`
2. Check log file: `tail -50 /var/log/sql-proxy/sql-proxy.log`
3. Run interactively: `./sql-proxy -config config.yaml`

**macOS:**
1. Check logs: `tail -100 /usr/local/var/log/sql-proxy/sql-proxy.err`
2. Run interactively: `./sql-proxy -config config.yaml`

**All platforms:**
- Verify config.yaml syntax with a YAML validator
- Check file permissions on config and log directories

### Database connection issues (SQL Server)
- Check `/health` endpoint for status
- Look for `health_check_failed` in logs
- Verify security group allows port 1433
- Check credentials in config

### Database issues (SQLite)

**"database is locked" errors:**
- Increase `busy_timeout_ms` (default 5000ms may not be enough for high contention)
- Check if another process has the database open exclusively
- Ensure WAL mode is enabled (`journal_mode: "wal"`)
- Reduce write concurrency if possible

**"unable to open database file":**
- Check file path is correct and accessible
- Verify directory exists and has write permissions
- For in-memory (`:memory:`), no path issues should occur

**WAL file growing large:**
- WAL checkpoints automatically every 1000 pages
- Manually checkpoint: `PRAGMA wal_checkpoint(TRUNCATE)`
- Check for long-running read transactions blocking checkpoints

**Performance issues:**
- Ensure WAL mode is enabled for concurrent reads
- Check `cache_size` pragma (default: 64MB)
- Consider `mmap_size` for large databases
- Add indexes for frequently-queried columns

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

**Windows:**
```cmd
sql-proxy.exe -stop
copy /Y sql-proxy-new.exe C:\Services\SQLProxy\sql-proxy.exe
sql-proxy.exe -start
curl http://localhost:8081/health
```

**Linux:**
```bash
sudo systemctl stop sql-proxy
sudo cp sql-proxy-new /opt/sql-proxy/sql-proxy
sudo chown sqlproxy:sqlproxy /opt/sql-proxy/sql-proxy
sudo systemctl start sql-proxy
curl http://localhost:8081/health
```

**macOS:**
```bash
sudo launchctl unload /Library/LaunchDaemons/com.sqlproxy.plist
sudo cp sql-proxy-new /usr/local/bin/sql-proxy
sudo launchctl load /Library/LaunchDaemons/com.sqlproxy.plist
curl http://localhost:8081/health
```

For config changes only, just restart the service (no binary replacement needed).

## Pre-Deployment Checklist

Before deploying to production:

### 1. Validate configuration
```bash
./sql-proxy -validate -config config.yaml
```

### 2. Test interactively
```bash
./sql-proxy -config config.yaml
# In another terminal:
curl http://localhost:8081/health
curl http://localhost:8081/
curl "http://localhost:8081/api/your-endpoint?param=value"
```

### 3. Install as service

**Windows (as Administrator):**
```cmd
sql-proxy.exe -install -config C:\Services\SQLProxy\config.yaml
sql-proxy.exe -start
sql-proxy.exe -status
```

**Linux:**
```bash
# Generate systemd unit file from template
/opt/sql-proxy/sql-proxy -install -config /opt/sql-proxy/config.yaml
# Follow printed instructions, then:
sudo systemctl daemon-reload
sudo systemctl enable sql-proxy
sudo systemctl start sql-proxy
sudo systemctl status sql-proxy
```

**macOS:**
```bash
# Generate launchd plist from template
/usr/local/bin/sql-proxy -install -config /usr/local/etc/sql-proxy/config.yaml
# Follow printed instructions, then:
sudo launchctl load /Library/LaunchDaemons/com.sqlproxy.sql-proxy.plist
```

### 4. Verify and monitor
```bash
curl http://localhost:8081/health
curl http://localhost:8081/metrics
```

Check logs:
- **Windows:** `type C:\Services\SQLProxy\logs\sql-proxy.log`
- **Linux:** `journalctl -u sql-proxy -f` or `tail -f /var/log/sql-proxy/sql-proxy.log`
- **macOS:** `tail -f /usr/local/var/log/sql-proxy/sql-proxy.log`

### Production Recommendations

- **Caddy/nginx in front**: Don't expose sql-proxy directly to the internet
- **Monitor `/health`**: Set up alerting on 503 responses
- **Review metrics**: Check `/metrics` endpoint for slow queries
- **Log level**: Use `info` in production, `debug` only for troubleshooting
- **Backup config**: Keep config.yaml in version control

## Testing

The project includes comprehensive tests at multiple levels:

- **Unit tests** - Test individual functions and methods in isolation
- **Integration tests** - Test component interactions using `httptest` (in-process HTTP server)
- **End-to-end tests** - Start the actual binary as a subprocess and test via real HTTP

### Running Tests

```bash
# Run all tests (unit + integration + e2e)
make test

# Run by test type
make test-unit         # Unit tests only (internal packages)
make test-integration  # Integration tests (httptest-based)
make test-e2e          # End-to-end tests (starts actual binary)

# Run by package
make test-db
make test-handler
make test-config
make test-server

# Run with coverage
make test-cover
make test-cover-html

# Run benchmarks
make test-bench
```

### Test Documentation

See [TESTS.md](TESTS.md) for a complete list of all tests with descriptions.

To regenerate test documentation after adding/modifying tests:

```bash
make test-docs
```

### Test Organization

| Type | Location | Description |
|------|----------|-------------|
| Unit tests | `internal/*/` | Test individual functions and methods |
| Integration tests | `internal/server/` | Test component interactions via `httptest` |
| End-to-end tests | `e2e/` | Start binary, make real HTTP requests |
| Benchmarks | `internal/*/benchmark_test.go` | Performance tests |

All unit and integration tests use SQLite in-memory databases (`:memory:`) to avoid external dependencies.

## Roadmap

Planned features for future releases:

- [ ] **MySQL Support** - Add MySQL/MariaDB as a database backend option alongside SQL Server and SQLite.
- [ ] **PostgreSQL Support** - Add PostgreSQL as a database backend option.
- [ ] **TLS Support** - Native HTTPS termination without requiring a reverse proxy (Caddy/nginx). Will support configurable certificate paths and automatic Let's Encrypt integration.
- [ ] **Rate Limiting** - Per-endpoint and per-client rate limiting to protect database resources from excessive requests.
- [ ] **Authentication** - API key and/or JWT-based authentication for endpoint access control.
