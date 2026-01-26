# SQL Proxy Service

A lightweight, production-grade Go service that exposes predefined SQL queries as HTTP endpoints. Supports **SQL Server** and **SQLite** databases. Runs as a system service on **Windows**, **Linux**, and **macOS** with **zero impact on the source database** and **zero maintenance** requirements.

## Features

- **Multi-Database Support** - SQL Server and SQLite (same query syntax)
- **Cross-Platform Service** - Windows Service, Linux systemd, macOS launchd
- **YAML Configuration** - Easy query management, no code changes
- **Read-only Safety** - Zero interference with production database
- **Workflows** - Multi-step query pipelines with conditions, iteration, and external API calls
- **Scheduled Workflows** - Run workflows on cron schedules with retry
- **Rate Limiting** - Per-endpoint and per-client rate limiting with token bucket
- **Structured Logging** - JSON logs with automatic rotation
- **Metrics Endpoints** - `/_/metrics` (Prometheus) and `/_/metrics.json` (human-readable)
- **Request Tracing** - Wide events with request IDs
- **Runtime Config** - Change log level without restart
- **Auto-Recovery** - Automatic database reconnection
- **Health Monitoring** - Background health checks

## Reliability Features

This service is designed for long-running, fire-and-forget operation:

| Feature | Description |
|---------|-------------|
| **Log Rotation** | Automatic rotation by size, age retention, compression |
| **Metrics Endpoints** | `/_/metrics` (Prometheus) and `/_/metrics.json` (JSON) |
| **Workflows** | Multi-step pipelines with conditions, iteration, external API calls |
| **Scheduled Workflows** | Cron-based execution with retry and backoff |
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
Workflows: 6 endpoints configured

Endpoints:
  GET /api/machines - list_machines (0 params)
  GET /api/machines/details - get_machine (1 params)
  GET /api/checkins - checkin_logs (3 params)

Configuration valid
```

The validator checks:
- Server settings (port range, timeout values)
- Database connection settings
- Logging configuration
- Workflow definitions (triggers, steps, SQL syntax)
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
  # cache:                     # Optional: Enable response caching
  #   enabled: true
  #   max_size_mb: 256
  #   default_ttl_sec: 300

databases:
  - name: "primary"
    type: "sqlserver"             # sqlserver or sqlite (required)
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

# Optional: Debug endpoints (pprof) for profiling
# debug:
#   enabled: true     # Enable /_/debug/pprof/* endpoints
#   port: 6060        # Separate port (0 = same as main server)
#   host: "localhost" # Only valid with separate port; defaults to localhost

# Optional: Rate limit pools
# rate_limits:
#   - name: "default"
#     requests_per_second: 100
#     burst: 200
#     key: "{{.trigger.client_ip}}"

# Optional: Template variables (available as {{.vars.name}} in templates)
# variables:
#   env_file: ".env"              # Load from env file (relative to config)
#   values:
#     api_version: "v1"
#     max_page_size: "${MAX_PAGE:100}"  # Supports ${VAR:default} syntax

# Optional: Encrypted public IDs (prevents PK enumeration)
# public_ids:
#   secret_key: "${PUBLIC_ID_SECRET}"  # Required: 32+ character secret
#   namespaces:
#     - name: "user"
#       prefix: "usr"                   # Output: usr_Xk9mPqR3vL2n
#     - name: "order"
#       prefix: "ord"

workflows:
  - name: "list_machines"
    triggers:
      - type: http
        path: "/api/machines"
        method: GET
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT MachineId, MachineName, LastPingTime
          FROM Machines
          ORDER BY MachineName
      - type: response
        template: |
          {"success": true, "data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}
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

workflows:
  - name: "get_machines"
    triggers:
      - type: http
        path: "/api/machines"
        method: GET
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM Machines"
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'

  - name: "insert_report"
    triggers:
      - type: http
        path: "/api/reports"
        method: POST
        parameters:
          - name: "date"
            type: "datetime"
            required: true
          - name: "data"
            type: "string"
            required: true
    steps:
      - name: insert
        type: query
        database: "reporting"
        sql: "INSERT INTO Reports (Date, Data) VALUES (@date, @data)"
      - type: response
        template: '{"success": true}'
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

workflows:
  - name: "list_items"
    triggers:
      - type: http
        path: "/api/items"
        method: GET
    steps:
      - name: fetch
        type: query
        database: "test_db"
        sql: "SELECT * FROM items ORDER BY name"
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'
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

Session settings control database behavior at query execution time. Settings can be defined at connection level (defaults) and overridden per-step.

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

**Override at step level:**

```yaml
workflows:
  - name: "critical_read"
    triggers:
      - type: http
        path: "/api/balance"
        method: GET
        parameters:
          - name: "id"
            type: "int"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        # Override for this specific step
        isolation: "repeatable_read"     # Need stable reads within query
        lock_timeout_ms: 30000           # Important query, wait longer
        sql: "SELECT Balance FROM Accounts WHERE Id = @id"
      - type: response
        template: '{"success": true, "balance": {{index .steps.fetch.data 0 "Balance"}}}'
```

**Available values:**

| Setting | Values |
|---------|--------|
| `isolation` | `read_uncommitted`, `read_committed`, `repeatable_read`, `serializable`, `snapshot` |
| `lock_timeout_ms` | Any non-negative integer (milliseconds) |
| `deadlock_priority` | `low`, `normal`, `high` |

**Resolution order:** Step settings -> Connection settings -> Implicit defaults (based on `readonly`)

### Validation

Config validation enforces:
- All required fields present (no defaults)
- Steps with INSERT/UPDATE/DELETE on read-only connections -> **error**
- Unknown database references -> **error**
- Invalid isolation level or deadlock priority -> **error**
- Unused database connections -> **warning**

### Timeout Configuration

Timeouts are configurable at three levels (in order of precedence):

1. **Request parameter** (`_timeout`) - Caller specifies per-request
2. **Step config** (`timeout_sec`) - Per-step timeout in YAML
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
workflows:
  - name: "recent_checkins"
    triggers:
      - type: http
        path: "/api/checkins/recent"
        method: GET
        parameters:
          - name: "limit"
            type: "int"
            required: false
            default: "100"
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT TOP (@limit)
            EmployeeId, PunchTime, MachineId
          FROM AttendanceLog
          ORDER BY PunchTime DESC
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}'
```

```bash
# Get last 50 check-ins
curl "http://localhost:8081/api/checkins/recent?limit=50"
```

#### Offset Pagination (OFFSET/FETCH)

For paginated results with page navigation:

```yaml
workflows:
  - name: "checkins_paginated"
    triggers:
      - type: http
        path: "/api/checkins/page"
        method: GET
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
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT
            EmployeeId, PunchTime, MachineId
          FROM AttendanceLog
          WHERE PunchTime >= @fromDate
          ORDER BY PunchTime DESC
          OFFSET @offset ROWS FETCH NEXT @limit ROWS ONLY
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}'
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

For UI pagination, you often need the total count. Create a separate workflow:

```yaml
workflows:
  - name: "checkins_count"
    triggers:
      - type: http
        path: "/api/checkins/count"
        method: GET
        parameters:
          - name: "fromDate"
            type: "datetime"
            required: true
    steps:
      - name: count
        type: query
        database: "primary"
        sql: |
          SELECT COUNT(*) AS total_count
          FROM AttendanceLog
          WHERE PunchTime >= @fromDate
      - type: response
        template: '{"success": true, "total_count": {{index .steps.count.data 0 "total_count"}}}'
```

```bash
# Get total count
curl "http://localhost:8081/api/checkins/count?fromDate=2024-01-01"
# Returns: {"success":true,"total_count":15234}
```

#### Keyset Pagination (Most Efficient for Large Tables)

For very large tables, keyset pagination is more efficient than OFFSET:

```yaml
workflows:
  - name: "checkins_keyset"
    triggers:
      - type: http
        path: "/api/checkins/after"
        method: GET
        parameters:
          - name: "afterId"
            type: "int"
            required: false
            default: "0"
          - name: "limit"
            type: "int"
            required: false
            default: "100"
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT TOP (@limit)
            LogId, EmployeeId, PunchTime, MachineId
          FROM AttendanceLog
          WHERE LogId > @afterId
          ORDER BY LogId ASC
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}'
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
workflows:
  - name: "bad_example"
    triggers:
      - type: http
        path: "/api/users/bad"
        method: GET
        parameters:
          - name: "status"
            type: "string"
            required: false
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT * FROM Users WHERE status = @status
      - type: response
        template: '{"data": {{json .steps.fetch.data}}}'

# GOOD - Returns all rows when status is not provided
workflows:
  - name: "good_example"
    triggers:
      - type: http
        path: "/api/users/good"
        method: GET
        parameters:
          - name: "status"
            type: "string"
            required: false
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT * FROM Users
          WHERE (@status IS NULL OR status = @status)
      - type: response
        template: '{"data": {{json .steps.fetch.data}}}'
```

This pattern lets optional parameters act as filters only when provided.

### Memory Considerations

All query results are loaded into memory before JSON serialization. For large result sets:

- Always use `TOP @limit` or `OFFSET/FETCH` in queries
- Set reasonable default limits (e.g., 100-1000 rows)
- Monitor memory usage for queries returning large text/blob columns

## Workflows

Workflows provide a powerful way to define complex multi-step query pipelines with conditional execution, iteration, and external API calls. A workflow consists of:

- **Triggers** - How the workflow is initiated (HTTP request or cron schedule)
- **Steps** - Sequential execution of query, httpcall, response, or block steps
- **Conditions** - Named expressions for conditional step execution

Workflows support:
- HTTP and cron triggers
- Query steps for database operations
- HTTPCall steps for external API calls
- Response steps with templated output
- Iteration over query results with blocks
- Conditional branching based on results
- Caching and rate limiting per trigger

### HTTP Methods

HTTP triggers support all standard methods: **GET**, **POST**, **PUT**, **DELETE**, **PATCH**, **HEAD**, and **OPTIONS**.

You can define multiple workflows with the same path but different methods (RESTful pattern).

### Path Parameters

URL path parameters allow you to capture values from the URL path itself (e.g., `/api/items/{id}` instead of `/api/items?id=123`). Path parameters use Go 1.22+ routing syntax with curly braces.

```yaml
workflows:
  - name: "get_item"
    triggers:
      - type: http
        path: "/api/items/{id}"
        method: GET
        parameters:
          - name: "id"           # Must match the path parameter name
            type: "int"
            required: true       # Path parameters must be required
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM items WHERE id = @id"
      - type: response
        template: '{"item": {{json (index .steps.fetch.data 0)}}}'

  - name: "get_item_reviews"
    triggers:
      - type: http
        path: "/api/items/{item_id}/reviews/{review_id}"
        method: GET
        parameters:
          - name: "item_id"
            type: "int"
            required: true
          - name: "review_id"
            type: "int"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM reviews WHERE item_id = @item_id AND id = @review_id"
      - type: response
        template: '{"review": {{json (index .steps.fetch.data 0)}}}'
```

**Path parameter rules:**
- Path parameters are declared using `{paramName}` syntax in the path
- Each path parameter must have a matching entry in `parameters`
- Path parameters must be `required: true` (can't have optional path segments)
- Path parameters take precedence over query parameters with the same name
- Parameter names must start with a letter or underscore, followed by alphanumeric characters

**Example usage:**
```bash
# Path parameter captured from URL
curl http://localhost:8081/api/items/42
# Equivalent to: /api/items?id=42

# Multiple path parameters
curl http://localhost:8081/api/items/42/reviews/7
```

### RESTful Pattern Example

Combine path parameters with multiple methods for clean REST APIs:

```yaml
workflows:
  # List items: GET /api/items
  - name: "list_items"
    triggers:
      - type: http
        path: "/api/items"
        method: GET
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM items"
      - type: response
        template: '{"data": {{json .steps.fetch.data}}}'

  # Create item: POST /api/items
  - name: "create_item"
    triggers:
      - type: http
        path: "/api/items"
        method: POST
        parameters:
          - name: "name"
            type: "string"
            required: true
    steps:
      - name: insert
        type: query
        database: "primary"
        sql: "INSERT INTO items (name) VALUES (@name)"
      - type: response
        status_code: 201
        template: '{"success": true}'

  # Get single item: GET /api/items/{id}
  - name: "get_item"
    triggers:
      - type: http
        path: "/api/items/{id}"
        method: GET
        parameters:
          - name: "id"
            type: "int"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM items WHERE id = @id"
      - type: response
        template: '{"item": {{json (index .steps.fetch.data 0)}}}'

  # Update item: PUT /api/items/{id}
  - name: "update_item"
    triggers:
      - type: http
        path: "/api/items/{id}"
        method: PUT
        parameters:
          - name: "id"
            type: "int"
            required: true
          - name: "name"
            type: "string"
            required: true
    steps:
      - name: update
        type: query
        database: "primary"
        sql: "UPDATE items SET name = @name WHERE id = @id"
      - type: response
        template: '{"success": true}'

  # Delete item: DELETE /api/items/{id}
  - name: "delete_item"
    triggers:
      - type: http
        path: "/api/items/{id}"
        method: DELETE
        parameters:
          - name: "id"
            type: "int"
            required: true
    steps:
      - name: delete
        type: query
        database: "primary"
        sql: "DELETE FROM items WHERE id = @id"
      - type: response
        template: '{"deleted": true}'
```

### Basic HTTP Workflow

The simplest workflow exposes a single query as an HTTP endpoint:

```yaml
workflows:
  - name: "get_machines"
    triggers:
      - type: http
        path: "/api/machines"
        method: GET
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM Machines ORDER BY MachineName"
      - type: response
        template: |
          {"success": true, "data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}
```

### Workflow with Parameters

Parameters are defined on triggers and accessed via `.trigger.params`:

```yaml
workflows:
  - name: "get_machine_by_id"
    triggers:
      - type: http
        path: "/api/machines/details"
        method: GET
        parameters:
          - name: "id"
            type: "int"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM Machines WHERE MachineId = @id"
      - type: response
        template: |
          {"success": true, "machine": {{json (index .steps.fetch.data 0)}}}
```

### Conditional Responses

Use named conditions and conditional response steps:

```yaml
workflows:
  - name: "get_user"
    conditions:
      found: "steps.fetch.count > 0"
      not_found: "steps.fetch.count == 0"
    triggers:
      - type: http
        path: "/api/user"
        method: GET
        parameters:
          - name: "id"
            type: "int"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM Users WHERE Id = @id"
      - type: response
        condition: "found"
        template: |
          {"success": true, "user": {{json (index .steps.fetch.data 0)}}}
      - type: response
        condition: "not_found"
        status_code: 404
        template: |
          {"success": false, "error": "User not found"}
```

**Note:** Validation warns if all response steps have conditions with no unconditional fallback. In the example above, `found` and `not_found` are logically exhaustive, so the warning can be safely ignored. Alternatively, make the last response unconditional as a fallback.

### Multi-Step Workflow

Chain multiple queries and combine results:

```yaml
workflows:
  - name: "dashboard_stats"
    triggers:
      - type: http
        path: "/api/dashboard"
        method: GET
    steps:
      - name: machine_stats
        type: query
        database: "primary"
        sql: "SELECT COUNT(*) AS total, SUM(CASE WHEN IsOnline = 1 THEN 1 ELSE 0 END) AS online FROM Machines"

      - name: checkin_stats
        type: query
        database: "primary"
        sql: "SELECT COUNT(*) AS total, COUNT(DISTINCT EmployeeId) AS unique_employees FROM AttendanceLog WHERE PunchTime >= DATE('now', '-1 day')"

      - type: response
        template: |
          {
            "success": true,
            "machines": {{json (index .steps.machine_stats.data 0)}},
            "checkins": {{json (index .steps.checkin_stats.data 0)}}
          }
```

### External API Calls (httpcall)

Call external APIs between queries:

```yaml
workflows:
  - name: "sync_to_external"
    triggers:
      - type: cron
        schedule: "0 * * * *"  # Every hour
    steps:
      - name: fetch_data
        type: query
        database: "primary"
        sql: "SELECT * FROM Machines WHERE UpdatedAt > datetime('now', '-1 hour')"

      - name: notify_external
        type: httpcall
        condition: "steps.fetch_data.count > 0"
        url: "https://api.example.com/webhooks/machines"
        http_method: POST
        headers:
          Authorization: "Bearer ${API_TOKEN}"
          Content-Type: "application/json"
        body: |
          {"machines": {{json .steps.fetch_data.data}}, "count": {{.steps.fetch_data.count}}}
        parse: "json"
        timeout_sec: 30
        retry:
          enabled: true
          max_attempts: 3
          initial_backoff_sec: 1
          max_backoff_sec: 30
```

### Iteration with Blocks

Process each item from a query result:

```yaml
workflows:
  - name: "process_orders"
    triggers:
      - type: cron
        schedule: "*/5 * * * *"
    steps:
      - name: fetch_orders
        type: query
        database: "primary"
        sql: "SELECT * FROM Orders WHERE Status = 'pending' LIMIT 100"

      - name: process_batch
        condition: "steps.fetch_orders.count > 0"
        iterate:
          over: "steps.fetch_orders.data"
          as: "order"
          on_error: continue  # Continue processing remaining items on failure
        steps:
          - name: call_payment
            type: httpcall
            url: "https://payments.example.com/charge"
            http_method: POST
            body: |
              {"order_id": "{{.order.OrderId}}", "amount": {{.order.Amount}}}
```

After iteration, access aggregate results:
- `.steps.process_batch.success_count` - Number of successful iterations
- `.steps.process_batch.failure_count` - Number of failed iterations
- `.steps.process_batch.skipped_count` - Number of skipped iterations
- `.steps.process_batch.iterations` - Array of results from each iteration

**HTTP example with response:**

```yaml
workflows:
  - name: "batch_process"
    triggers:
      - type: http
        path: "/api/batch"
        method: POST
        parameters:
          - name: "ids"
            type: "int[]"
            required: true
    steps:
      - name: process_each
        iterate:
          over: "trigger.params.ids"
          as: "id"
        steps:
          - name: lookup
            type: query
            database: "primary"
            sql: "SELECT {{.id}} AS id, 'processed' AS status"

      - type: response
        template: |
          {
            "processed": {{.steps.process_each.success_count}},
            "failed": {{.steps.process_each.failure_count}},
            "results": {{json .steps.process_each.iterations}}
          }
```

The `as:` value (`id` in this case) becomes the variable name used in templates (`.id`).

### Cron-Triggered Workflows (Scheduled Execution)

Run workflows on a schedule:

```yaml
workflows:
  - name: "daily_report"
    triggers:
      - type: cron
        schedule: "0 8 * * *"  # 8 AM daily
        params:
          report_date: "yesterday"  # Dynamic date value
    steps:
      - name: generate
        type: query
        database: "primary"
        sql: "SELECT COUNT(*) AS total FROM AttendanceLog WHERE DATE(PunchTime) = DATE(@report_date)"
```

**Cron Expression Format:**

Standard 5-field cron format: `minute hour day-of-month month day-of-week`

| Expression | Description |
|------------|-------------|
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour |
| `0 8 * * *` | Daily at 8 AM |
| `0 8 * * 1` | Mondays at 8 AM |
| `0 0 1 * *` | First day of month at midnight |

**Dynamic Date Values:**

The following special values are resolved at execution time:

| Value | Resolves To |
|-------|-------------|
| `now` | Current timestamp |
| `today` | Start of today (00:00:00) |
| `yesterday` | Start of yesterday |
| `tomorrow` | Start of tomorrow |

**Retry Behavior:**

Scheduled workflows automatically retry on failure:
- 3 attempts total
- Exponential backoff: 1s, 5s, 25s between retries
- Logs error after all retries exhausted

### Multiple Triggers

A single workflow can have both HTTP and cron triggers:

```yaml
workflows:
  - name: "status_check"
    triggers:
      - type: http
        path: "/api/status"
        method: GET
      - type: cron
        schedule: "*/5 * * * *"
    steps:
      - name: check
        type: query
        database: "primary"
        sql: "SELECT COUNT(*) AS online FROM Machines WHERE IsOnline = 1"
      - type: response  # Only used for HTTP trigger
        template: |
          {"online_machines": {{index .steps.check.data 0 "online"}}}
```

### Workflow Caching

SQL Proxy supports two levels of caching:

**Trigger-Level Caching** - Cache the entire workflow response. On cache hit, the workflow steps are not executed at all - the cached response is returned immediately.

```yaml
workflows:
  - name: "cached_dashboard"
    triggers:
      - type: http
        path: "/api/dashboard"
        method: GET
        cache:
          enabled: true
          key: "dashboard:{{.trigger.params.period | default \"day\"}}"
          ttl_sec: 60
          evict_cron: "0 0 * * *"  # Optional: evict at midnight
        parameters:
          - name: "period"
            type: "string"
            default: "day"
    steps:
      # ... query steps
```

Response includes `X-Cache: HIT` or `X-Cache: MISS` header.

Trigger cache keys have access to request context:

| Template | Description |
|----------|-------------|
| `{{.trigger.params.name}}` | URL/query parameters |
| `{{.trigger.client_ip}}` | Client IP address |
| `{{.trigger.method}}` | HTTP method (GET, POST, etc.) |
| `{{.trigger.path}}` | Request path |
| `{{.trigger.headers.Authorization}}` | HTTP headers (flattened) |
| `{{.trigger.query.page}}` | Query parameters (flattened) |
| `{{.trigger.cookies.session}}` | Parsed cookies |
| `{{.request_id}}` | Request ID |

**Step-Level Caching** - Cache individual step results (query or httpcall). The workflow still executes, but cached steps return their cached result instead of executing. Useful when multiple endpoints share common queries, or for expensive operations.

```yaml
workflows:
  - name: "user_dashboard"
    triggers:
      - type: http
        path: "/api/user/dashboard"
        method: GET
        parameters:
          - name: "user_id"
            type: "int"
            required: true
    steps:
      # User data cached for 5 minutes
      - name: user
        type: query
        database: "primary"
        sql: "SELECT * FROM users WHERE id = @user_id"
        cache:
          key: "user:{{.trigger.params.user_id}}"
          ttl_sec: 300

      # Stats cached for 1 minute (changes more often)
      - name: stats
        type: query
        database: "primary"
        sql: "SELECT COUNT(*) as count FROM orders WHERE user_id = @user_id"
        cache:
          key: "user_stats:{{.trigger.params.user_id}}"
          ttl_sec: 60

      - type: response
        template: |
          {
            "user": {{json (index .steps.user.data 0)}},
            "user_cached": {{.steps.user.cache_hit}},
            "stats": {{json (index .steps.stats.data 0)}},
            "stats_cached": {{.steps.stats.cache_hit}}
          }
```

You can combine both levels - trigger cache provides fast response for repeated requests, while step cache speeds up workflow execution when the trigger cache misses.

### Workflow Rate Limiting

Apply rate limits to HTTP triggers:

```yaml
workflows:
  - name: "rate_limited_api"
    triggers:
      - type: http
        path: "/api/limited"
        method: GET
        rate_limit:
          - pool: "default"  # Reference named pool
          - requests_per_second: 10  # Or inline config
            burst: 20
            key: "{{.trigger.client_ip}}"
    steps:
      # ... steps
```

### Step Types Reference

| Type | Purpose |
|------|---------|
| `query` | Execute SQL query against a database |
| `httpcall` | Call external HTTP API |
| `response` | Send HTTP response (HTTP triggers only) |

Steps with nested `steps:` are called **blocks**. Blocks provide a scoped namespace for their nested steps and support iteration via `iterate:`.

### Step Configuration

**Query Step:**
```yaml
- name: "step_name"
  type: query
  database: "primary"           # Required: database connection name
  sql: "SELECT * FROM ..."      # Required: SQL query
  isolation: "read_committed"   # Optional: transaction isolation
  lock_timeout_ms: 5000         # Optional: lock timeout
  deadlock_priority: "low"      # Optional: deadlock priority
  json_columns: ["data"]        # Optional: parse JSON columns
  cache:                        # Optional: step-level caching
    key: "item:{{.trigger.params.id}}"   # Cache key (supports templates)
    ttl_sec: 300                # Time to live in seconds
  on_error: fail                # Optional: fail (default) or continue
  disabled: false               # Optional: skip this step if true
```

**HTTPCall Step:**
```yaml
- name: "step_name"
  type: httpcall
  url: "https://api.example.com/..."  # Required (supports templates)
  http_method: POST                    # Required: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
  headers:                             # Optional: HTTP headers
    Authorization: "Bearer token"
  body: '{"key": "value"}'             # Optional: request body (supports templates)
  parse: "json"                        # Optional: json, text, or none
  timeout_sec: 30                      # Optional: request timeout
  retry:                               # Optional: retry configuration
    enabled: true
    max_attempts: 3
    initial_backoff_sec: 1
    max_backoff_sec: 30
  cache:                               # Optional: step-level caching
    key: "api:{{.trigger.params.id}}"           # Cache key (supports templates)
    ttl_sec: 300                       # Time to live in seconds
```

**Response Step:**
```yaml
- type: response
  condition: "condition_name"  # Optional: only send if condition is true
  status_code: 200             # Optional: HTTP status code (default: 200)
  headers:                     # Optional: response headers
    X-Custom: "value"
  template: |                  # Required: response body template
    {"success": true, "data": {{json .steps.fetch.data}}}
```

**Block Step (iteration):**
```yaml
- name: process_items
  condition: "condition_name"    # Optional: only execute if condition is true
  iterate:                       # Optional: iterate over a collection
    over: "steps.fetch.data"     # Expression to iterate over
    as: "item"                   # Variable name for current item
    on_error: continue           # abort, continue, or skip
  steps:                         # Nested steps (creates a block)
    - name: process
      type: query
      database: "primary"
      sql: "UPDATE items SET status = 'done' WHERE id = {{.item.id}}"
```

Block results include:
- `.steps.<name>.iterations` - Array of iteration results
- `.steps.<name>.success_count` - Number of successful iterations
- `.steps.<name>.failure_count` - Number of failed iterations
- `.steps.<name>.skipped_count` - Number of skipped iterations

### Template Context

Templates have access to:

| Variable | Description |
|----------|-------------|
| `.trigger.type` | Trigger type ("http" or "cron") |
| `.trigger.params` | Parameter values from request/schedule |
| `.trigger.headers` | HTTP headers (HTTP trigger only) |
| `.trigger.cookies` | Parsed cookies as map (HTTP trigger only) |
| `.trigger.method` | HTTP method (HTTP trigger only) |
| `.trigger.path` | Request path (HTTP trigger only) |
| `.trigger.client_ip` | Client IP address |
| `.steps.<name>.data` | Query results (array of rows) |
| `.steps.<name>.row` | First row (shortcut for `index .data 0`) |
| `.steps.<name>.count` | Row count |
| `.steps.<name>.found` | True if count > 0 |
| `.steps.<name>.empty` | True if count == 0 |
| `.steps.<name>.one` | True if count == 1 |
| `.steps.<name>.many` | True if count > 1 |
| `.steps.<name>.error` | Error message if step failed |
| `.steps.<name>.status_code` | HTTP status (httpcall only) |
| `.item` | Current item in block iteration |
| `.vars` | Global variables from config `variables:` section |
| `.workflow.request_id` | Request ID |
| `.workflow.name` | Workflow name |

### Template Functions

#### JSON and Data Access

| Function | Description | Example |
|----------|-------------|---------|
| `json` | JSON encode value | `{{json .steps.fetch.data}}` |
| `jsonIndent` | JSON encode with indentation | `{{jsonIndent .steps.fetch.row}}` |
| `index` | Array/map access | `{{index .steps.fetch.data 0 "name"}}` |
| `dig` | Safe nested access (nil-safe) | `{{dig .trigger.params "user" "profile" "name"}}` |
| `pick` | Select keys from map | `{{pick .steps.user.row "id" "name"}}` |
| `omit` | Remove keys from map | `{{omit .steps.user.row "password"}}` |
| `merge` | Merge maps (later wins) | `{{merge .defaults .overrides}}` |

#### Map Access

| Function | Description | Example |
|----------|-------------|---------|
| `require` | Get value, error if missing | `{{require .trigger.headers "Authorization"}}` |
| `getOr` | Get value with fallback | `{{getOr .trigger.query "page" "1"}}` |
| `has` | Check if key exists and non-empty | `{{if has .trigger.headers "X-Api-Key"}}...{{end}}` |
| `header` | Get header (canonical form) | `{{header .trigger.headers "Content-Type" "text/plain"}}` |
| `cookie` | Get cookie value | `{{cookie .trigger.cookies "session" ""}}` |

#### Arrays

| Function | Description | Example |
|----------|-------------|---------|
| `first` | First element | `{{first .steps.fetch.data}}` |
| `last` | Last element | `{{last .steps.fetch.data}}` |
| `len` | Length | `{{len .steps.fetch.data}}` |
| `pluck` | Extract field from array of maps | `{{pluck .steps.fetch.data "id"}}` |
| `isEmpty` | Check if empty | `{{if isEmpty .steps.fetch.data}}...{{end}}` |

#### Type Conversions

| Function | Description | Example |
|----------|-------------|---------|
| `int64` | Convert to integer | `{{int64 .trigger.query.page}}` |
| `float` | Convert to float | `{{float .trigger.query.price}}` |
| `string` | Convert to string | `{{string .steps.fetch.row.id}}` |
| `bool` | Convert to boolean | `{{bool .trigger.query.active}}` |

#### Math

| Function | Description | Example |
|----------|-------------|---------|
| `add`, `sub`, `mul`, `div`, `mod` | Arithmetic | `{{add .a .b}}` |
| `round`, `floor`, `ceil`, `trunc` | Rounding | `{{round .price}}` |
| `abs`, `min`, `max` | Comparisons | `{{max .a .b}}` |
| `zeropad` | Zero-padded number | `{{zeropad .id 6}}` → `000042` |
| `pad` | Custom padding | `{{pad .code 4 "0"}}` |

#### Strings

| Function | Description | Example |
|----------|-------------|---------|
| `upper`, `lower` | Case conversion | `{{upper .name}}` |
| `trim` | Remove whitespace | `{{trim .input}}` |
| `replace` | Replace all occurrences | `{{replace "_" "-" .slug}}` |
| `contains`, `hasPrefix`, `hasSuffix` | String tests | `{{if hasPrefix .path "/api"}}...{{end}}` |
| `truncate` | Truncate with suffix | `{{truncate .desc 100 "..."}}` |
| `split` | Split string | `{{split "," .tags}}` |
| `join` | Join array | `{{join ", " .items}}` |
| `substr` | Substring | `{{substr .code 0 3}}` |
| `quote` | Quote string | `{{quote .value}}` |
| `sprintf` | Format string | `{{sprintf "%s-%d" .prefix .id}}` |
| `repeat` | Repeat string | `{{repeat "*" 5}}` |

#### Validation

| Function | Description | Example |
|----------|-------------|---------|
| `isEmail` | Valid email format | `{{if isEmail .email}}...{{end}}` |
| `isUUID` | Valid UUID format | `{{if isUUID .id}}...{{end}}` |
| `isURL` | Valid URL with scheme | `{{if isURL .link}}...{{end}}` |
| `isIP` | Valid IP address (any type) | `{{if isIP .addr}}...{{end}}` |
| `isIPv4` | IPv4 address (includes IPv4-mapped IPv6) | `{{if isIPv4 .addr}}...{{end}}` |
| `isIPv6` | IPv6 address (excludes IPv4-mapped) | `{{if isIPv6 .addr}}...{{end}}` |
| `isNumeric` | Numeric string | `{{if isNumeric .input}}...{{end}}` |
| `matches` | Regex match (false for invalid patterns) | `{{if matches "^[A-Z]{2}$" .code}}...{{end}}` |

#### IP Network (for rate limiting)

| Function | Description | Example |
|----------|-------------|---------|
| `ipNetwork` | Network portion of IP | `{{ipNetwork .trigger.client_ip 24 64}}` -> `192.168.1.0` |
| `ipPrefix` | Network in CIDR notation | `{{ipPrefix .trigger.client_ip 24}}` -> `192.168.1.0/24` |
| `normalizeIP` | Normalize IP format | `{{normalizeIP .trigger.client_ip}}` |

#### Encoding and Hashing

| Function | Description | Example |
|----------|-------------|---------|
| `urlEncode`, `urlDecode` | URL encoding | `{{urlEncode .query}}` |
| `base64Encode`, `base64Decode` | Base64 encoding | `{{base64Encode .data}}` |
| `sha256`, `md5` | Hash functions | `{{sha256 .password}}` |
| `hmacSHA256` | HMAC signature | `{{hmacSHA256 .secret .payload}}` |

#### Date/Time

| Function | Description | Example |
|----------|-------------|---------|
| `now` | Current time | `{{now "YYYY-MM-DD"}}` |
| `formatTime` | Format timestamp | `{{formatTime .created_at "YYYY-MM-DD"}}` |
| `parseTime` | Parse to Unix timestamp | `{{parseTime .date "YYYY-MM-DD"}}` |
| `unixTime` | Current Unix timestamp | `{{unixTime}}` |

#### ID Generation

| Function | Description | Example |
|----------|-------------|---------|
| `uuid` / `uuid4` | UUID v4 | `{{uuid}}` → `550e8400-e29b-41d4-...` |
| `uuidShort` | UUID without hyphens | `{{uuidShort}}` → `550e8400e29b41d4...` |
| `shortID` | Base62 random ID | `{{shortID 12}}` → `AbC3d5fGh12x` |
| `nanoid` | NanoID-style ID | `{{nanoid 21}}` → `V1StGXR8_Z5jdHi6B-myT` |
| `publicID` | Encrypted public ID | `{{publicID "user" .id}}` → `usr_Xk9mPqR3vL2n` |
| `privateID` | Decode public ID | `{{privateID "user" .public_id}}` → `123` |

**Public ID Configuration:** The `publicID` and `privateID` functions require configuration in the `public_ids` section of your config file. This feature encrypts internal database IDs to prevent enumeration attacks and cross-entity ID reuse.

```yaml
public_ids:
  secret_key: "${PUBLIC_ID_SECRET}"  # Required: 32+ char secret
  namespaces:
    - name: "user"
      prefix: "usr"     # Output: usr_Xk9mPqR3vL2n
    - name: "order"
      prefix: "ord"     # Output: ord_7Kp2mNq8xL4v
```

**Secret Key Security:**
- **Key rotation invalidates all IDs**: Changing `secret_key` will cause all previously generated public IDs to fail decoding. Plan migrations carefully.
- **Use cryptographically random keys**: Generate with `openssl rand -base64 32` or similar. Never use predictable values (passwords, dictionary words, sequential strings).
- **Store securely**: Use environment variables or secrets management. Never commit keys to version control.
- **Minimum 32 characters**: Shorter keys reduce the security of the encryption.

Use `isValidPublicID("namespace", id)` in conditions to validate IDs before decoding:

```yaml
conditions:
  valid_id: 'isValidPublicID("user", trigger.params.id)'
steps:
  - name: fetch_user
    type: query
    params:
      internal_id: '{{privateID "user" .trigger.params.id}}'
    sql: "SELECT * FROM users WHERE id = @internal_id"
    condition: valid_id
```

#### Conditionals

| Function | Description | Example |
|----------|-------------|---------|
| `default` | Default if empty | `{{default "N/A" .value}}` or `{{.value \| default "N/A"}}` |
| `coalesce` | First non-empty value | `{{coalesce .preferred .fallback "default"}}` |
| `ternary` | Conditional value | `{{ternary .active "yes" "no"}}` |
| `when` | Value if true, else empty | `{{when .premium "PRO "}}{{.name}}` |

#### Debug

| Function | Description | Example |
|----------|-------------|---------|
| `typeOf` | Get type name | `{{typeOf .value}}` → `string` |
| `keys` | Get map keys | `{{keys .data}}` |
| `values` | Get map values | `{{values .data}}` |

#### Numeric Formatting

| Function | Description | Example |
|----------|-------------|---------|
| `formatNumber` | With thousand separators | `{{formatNumber 1234567}}` → `1,234,567` |
| `formatPercent` | As percentage | `{{formatPercent 0.156}}` → `15.6%` |
| `formatBytes` | Human-readable bytes (handles negatives) | `{{formatBytes 1572864}}` → `1.5 MB` |

### Condition Expressions

Conditions use the [expr](https://github.com/expr-lang/expr) expression language:

```yaml
conditions:
  has_data: "steps.fetch.count > 0"
  no_data: "steps.fetch.count == 0"
  is_admin: "trigger.params.role == 'admin'"
  large_result: "steps.fetch.count >= 100"
```

Available operators: `==`, `!=`, `<`, `>`, `<=`, `>=`, `&&`, `||`, `!`, `in`, `matches`

**Using condition aliases in compound expressions:**

Named condition aliases can be used in compound expressions with `&&`, `||`, and `!`:

```yaml
conditions:
  valid_id: 'isValidPublicID("task", trigger.params.public_id)'
  found: "steps.fetch.count > 0"
  is_owner: "steps.fetch.row.owner_id == steps.auth.row.id"

steps:
  - type: response
    condition: "!valid_id || !found"  # Aliases work in compound expressions
    status_code: 404
    template: '{"error": "Not found"}'
  - type: response
    condition: "found && is_owner"    # Multiple aliases combined
    template: '{"data": {{json .steps.fetch.row}}}'
```

Aliases are expanded at compile time, so `"found && is_owner"` becomes `"(steps.fetch.count > 0) && (steps.fetch.row.owner_id == steps.auth.row.id)"`.

**Available functions:**

| Function | Description | Example |
|----------|-------------|---------|
| `isValidPublicID` | Check if public ID is valid | `isValidPublicID("user", trigger.params.id)` |

### Error Handling

Control step failure behavior with `on_error`:

```yaml
steps:
  - name: optional_step
    type: query
    database: "primary"
    sql: "SELECT * FROM optional_table"
    on_error: continue  # Continue to next step even if this fails

  - name: required_step
    type: query
    database: "primary"
    sql: "SELECT * FROM required_table"
    on_error: fail  # Stop workflow on failure (default)
```

For blocks, control iteration error behavior:

```yaml
- name: process_items
  iterate:
    over: "steps.fetch.data"
    as: "item"
    on_error: continue  # Process remaining items even if one fails
  steps:
    # ... nested steps
```

## Rate Limiting

Protect your database from excessive requests with configurable rate limiting. Rate limits use the token bucket algorithm with configurable request rate and burst capacity.

### Server-Level Rate Limit Pools

Define reusable rate limit pools at the server level:

```yaml
rate_limits:
  - name: "global"
    requests_per_second: 100
    burst: 200
    key: "{{.trigger.client_ip}}"

  - name: "per_user"
    requests_per_second: 10
    burst: 20
    key: "{{.trigger.headers.Authorization}}"

  - name: "per_tenant"
    requests_per_second: 50
    burst: 100
    key: "{{getOr .trigger.headers \"X-Tenant-ID\" \"default\"}}"
```

### Per-Workflow Rate Limits

Apply rate limits to specific workflows by referencing pools or using inline configuration:

```yaml
workflows:
  - name: "expensive_report"
    triggers:
      - type: http
        path: "/api/report"
        method: GET
        rate_limit:
          - pool: "global"                   # Reference named pool
          - pool: "per_user"                 # Multiple pools can be applied
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM large_table"
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'

  - name: "public_api"
    triggers:
      - type: http
        path: "/api/public"
        method: GET
        rate_limit:
          - requests_per_second: 5           # Inline rate limit
            burst: 10
            key: "{{.trigger.client_ip}}"
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT * FROM items"
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'
```

### Rate Limit Key Templates

Keys determine how requests are grouped for rate limiting. Use Go template syntax with access to request context:

| Template | Description |
|----------|-------------|
| `{{.trigger.client_ip}}` | Client IP address (handles proxies via X-Forwarded-For) |
| `{{.trigger.headers.Authorization}}` | Authorization header value |
| `{{.trigger.headers.X-API-Key}}` | Custom header value |
| `{{.trigger.query.tenant}}` | Query parameter value |
| `{{.trigger.params.user_id}}` | URL parameter value |
| `{{getOr .trigger.headers "X-Tenant" "default"}}` | Header with fallback |
| `{{.trigger.client_ip}}:{{.trigger.headers.X-Tenant-ID}}` | Composite key |

### Rate Limit Behavior

When a request is rate limited:
- HTTP 429 (Too Many Requests) is returned
- Response includes `Retry-After` header with seconds to wait
- Request is logged with `rate_limited: true`

```json
{
  "success": false,
  "error": "rate limit exceeded",
  "retry_after_sec": 2
}
```

### Multiple Rate Limits

When multiple rate limits apply to a workflow, **all must pass** for the request to proceed:

```yaml
rate_limit:
  - pool: "global"      # 100 req/s global limit
  - pool: "per_user"    # 10 req/s per user
```

This allows layered rate limiting (e.g., global cap + per-client fairness).

## Parameter Types

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
workflows:
  - name: "save_config"
    triggers:
      - type: http
        path: "/api/config"
        method: POST
        parameters:
          - name: "name"
            type: "string"
            required: true
          - name: "data"
            type: "json"
            required: true
    steps:
      - name: insert
        type: query
        database: "primary"
        sql: |
          INSERT INTO configs (name, data) VALUES (@name, @data)
      - type: response
        template: '{"success": true}'
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
workflows:
  - name: "get_users_by_ids"
    triggers:
      - type: http
        path: "/api/users/batch"
        method: POST
        parameters:
          - name: "ids"
            type: "int[]"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT * FROM users
          WHERE id IN (SELECT value FROM json_each(@ids))
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'

  - name: "filter_by_status"
    triggers:
      - type: http
        path: "/api/users/filter"
        method: POST
        parameters:
          - name: "statuses"
            type: "string[]"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: |
          SELECT * FROM users
          WHERE status IN (SELECT value FROM json_each(@statuses))
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'
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
workflows:
  - name: "get_config"
    triggers:
      - type: http
        path: "/api/config"
        method: GET
        parameters:
          - name: "name"
            type: "string"
            required: true
    steps:
      - name: fetch
        type: query
        database: "primary"
        sql: "SELECT id, name, data FROM configs WHERE name = @name"
        json_columns: ["data"]   # Parse 'data' column as JSON
      - type: response
        template: '{"success": true, "data": {{json .steps.fetch.data}}}'
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

## Logging

Uses Go's `log/slog` with JSON output. Rotation via lumberjack.

**Output destination (controlled by `file_path`):**
- `file_path: ""` (empty): logs to stdout (default for interactive mode)
- `file_path: "/path/to/file.log"`: logs to file with rotation (recommended for service mode)

### Log Format (slog JSON, one line per entry)

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"request_completed","request_id":"a1b2c3d4","endpoint":"/api/machines","workflow_name":"list_machines","query_duration_ms":45,"row_count":150,"total_duration_ms":48}
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
curl http://localhost:8081/_/config/loglevel

# Change to debug
curl -X POST "http://localhost:8081/_/config/loglevel?level=debug"

# Back to info for production
curl -X POST "http://localhost:8081/_/config/loglevel?level=info"
```

## Metrics

SQL Proxy exposes metrics in two formats:

### Prometheus Format (`/_/metrics`)

For monitoring systems like Prometheus, Grafana, etc.:

```bash
curl http://localhost:8081/_/metrics
```

Returns OpenMetrics/Prometheus format with metrics including:
- `sqlproxy_requests_total` - Request counts by endpoint, method, status
- `sqlproxy_request_duration_seconds` - Request latency histogram
- `sqlproxy_query_duration_seconds` - SQL query latency histogram
- `sqlproxy_errors_total` - Errors by type
- `sqlproxy_db_healthy` - Database health (1=healthy, 0=unhealthy)
- `sqlproxy_cache_hits_total`, `sqlproxy_cache_misses_total` - Cache statistics
- `sqlproxy_ratelimit_allowed_total`, `sqlproxy_ratelimit_denied_total` - Rate limit stats
- `sqlproxy_cron_panics_total` - Panics recovered in cron workflows
- Standard Go runtime metrics (`go_*`, `process_*`)

### JSON Format (`/_/metrics.json`)

Human-readable JSON for debugging and dashboards:

```bash
curl http://localhost:8081/_/metrics.json
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
- `mem_alloc_bytes` growing unbounded -> memory leak
- `goroutines` growing unbounded -> goroutine leak
- `gc_last_pause_ns` > 10ms -> GC pressure, may need tuning

## API Endpoints

### Service Endpoints

All internal service endpoints use the `/_/` prefix (reserved, user workflows cannot use this prefix).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | List all workflow endpoints with parameters |
| `/_/health` | GET | Aggregate health check (always 200, parse `status` field) |
| `/_/health/{dbname}` | GET | Per-database health check (200 or 404 if not found) |
| `/_/metrics` | GET | Prometheus/OpenMetrics format for monitoring |
| `/_/metrics.json` | GET | Human-readable JSON metrics snapshot |
| `/_/openapi.json` | GET | OpenAPI 3.0 specification |
| `/_/config/loglevel` | GET/POST | View/change log level |
| `/_/cache/clear` | POST/DELETE | Clear cache (all or specific endpoint via `?endpoint=/api/path`) |
| `/_/ratelimits` | GET | Rate limit pool status and metrics |
| `/_/debug/pprof/*` | GET | Go profiling endpoints (if enabled) |

### Debug Endpoints (pprof)

When enabled via `debug.enabled: true` in config, Go profiling endpoints are available:

| Endpoint | Description |
|----------|-------------|
| `/_/debug/pprof/` | Index page with links to all profiles |
| `/_/debug/pprof/heap` | Memory allocation profile |
| `/_/debug/pprof/goroutine` | Stack traces of all goroutines |
| `/_/debug/pprof/profile` | CPU profile (30s by default, add `?seconds=N`) |
| `/_/debug/pprof/trace` | Execution trace (add `?seconds=N`) |

**Configuration:**
```yaml
debug:
  enabled: true      # Enable debug endpoints
  port: 6060         # Separate port (0 = same as main server)
  host: "localhost"  # Only valid with separate port; defaults to localhost
```

**Port and host behavior:**
- `port: 0` or same as main server: Debug endpoints share main server's binding (`host` must not be set)
- `port: <different>`: Debug endpoints run on separate server, `host` setting applies
- `host` defaults to `localhost` when using separate port (for security)
- Setting `host` when sharing the main server port is a config validation error

**Security considerations:**
- Debug endpoints can expose sensitive information
- For production: use separate port bound to localhost, restrict via firewall
- If sharing main port: debug endpoints are exposed on same interface as main server
- Consider disabling in production unless actively debugging

**Usage with pprof:**
```bash
# View profiles in browser
curl http://localhost:6060/_/debug/pprof/

# Capture CPU profile and analyze
go tool pprof http://localhost:6060/_/debug/pprof/profile?seconds=30

# Capture heap profile
go tool pprof http://localhost:6060/_/debug/pprof/heap
```

### Health Check Design

Health endpoints always return HTTP 200 with status details in the response body. This design:
- Avoids confusion with proxy/middleware 503 errors
- Allows clients to always receive health details
- Enables monitoring tools to parse JSON for actual status

**Aggregate health (`/_/health`):**
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

**Per-database health (`/_/health/{dbname}`):**
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
curl http://localhost:8081/_/openapi.json
```

You can use this with:
- **Swagger UI** - Paste URL into https://petstore.swagger.io or run Swagger UI locally
- **Postman** - Import > Link > `http://localhost:8081/_/openapi.json`
- **Code generators** - Generate client SDKs for any language

The spec includes all configured workflow endpoints with their parameters, types, and response schemas.

### Workflow Endpoints

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
- Check `/_/health` endpoint for status
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
- Check `/_/metrics.json` for `avg_duration_ms` and `max_duration_ms`
- Look for `slow_query` warnings in logs
- Consider adding indexes on SQL Server side
- Increase `timeout_sec` for known slow queries

### Disk filling up
- Logs rotate automatically, but check `max_backups` setting
- Compressed logs use `.gz` extension

### Changing configuration
- Most changes require service restart
- Log level can be changed at runtime via `/_/config/loglevel`

### Updating the service

**Windows:**
```cmd
sql-proxy.exe -stop
copy /Y sql-proxy-new.exe C:\Services\SQLProxy\sql-proxy.exe
sql-proxy.exe -start
curl http://localhost:8081/_/health
```

**Linux:**
```bash
sudo systemctl stop sql-proxy
sudo cp sql-proxy-new /opt/sql-proxy/sql-proxy
sudo chown sqlproxy:sqlproxy /opt/sql-proxy/sql-proxy
sudo systemctl start sql-proxy
curl http://localhost:8081/_/health
```

**macOS:**
```bash
sudo launchctl unload /Library/LaunchDaemons/com.sqlproxy.plist
sudo cp sql-proxy-new /usr/local/bin/sql-proxy
sudo launchctl load /Library/LaunchDaemons/com.sqlproxy.plist
curl http://localhost:8081/_/health
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
curl http://localhost:8081/_/health
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
curl http://localhost:8081/_/health
curl http://localhost:8081/_/metrics.json
```

Check logs:
- **Windows:** `type C:\Services\SQLProxy\logs\sql-proxy.log`
- **Linux:** `journalctl -u sql-proxy -f` or `tail -f /var/log/sql-proxy/sql-proxy.log`
- **macOS:** `tail -f /usr/local/var/log/sql-proxy/sql-proxy.log`

### Production Recommendations

- **Caddy/nginx in front**: Don't expose sql-proxy directly to the internet
- **Monitor `/_/health`**: Set up alerting on unhealthy status
- **Review metrics**: Check `/_/metrics.json` endpoint for slow queries
- **Log level**: Use `info` in production, `debug` only for troubleshooting
- **Backup config**: Keep config.yaml in version control

## Testing

The project includes comprehensive tests at multiple levels:

- **Unit tests** - Test individual functions and methods in isolation
- **Integration tests** - Test component interactions using `httptest` (in-process HTTP server)
- **End-to-end tests** - Start the actual binary as a subprocess and test via real HTTP

### Running Tests

```bash
# Run all tests (unit + integration)
make test

# Run by test type
make test-unit         # Unit tests only (internal packages)
make test-integration  # Integration tests (httptest-based)
make test-e2e          # All end-to-end test apps

# Run individual E2E apps
make test-e2e-taskapp  # Task management app
make test-e2e-crmapp   # CRM app (auth, roles, relationships)
make test-e2e-shopapp  # E-commerce app (state machines, inventory)
make test-e2e-blogapp  # Blog/CMS app (content, comments, search)

# Run by package
make test-db
make test-server
make test-config
make test-workflow

# Run with coverage (unit + e2e)
make test-cover

# Run benchmarks
make test-bench
```

### End-to-End Test Apps

The E2E test suite includes four demo applications that comprehensively exercise sql-proxy features:

| App | Config | Focus Areas |
|-----|--------|-------------|
| **taskapp** | `testdata/taskapp.yaml` | Basic CRUD, caching, rate limiting, batch operations |
| **crmapp** | `testdata/crmapp.yaml` | Auth, role-based access, relationships, multiple rate limits |
| **shopapp** | `testdata/shopapp.yaml` | State machines, order workflows, inventory, JSON columns |
| **blogapp** | `testdata/blogapp.yaml` | Content hierarchy, nested comments, search, pagination |

Each test app is a shell script with human-friendly colored output:

```bash
# Run individual apps
./e2e/taskapp_test.sh
./e2e/crmapp_test.sh
./e2e/shopapp_test.sh
./e2e/blogapp_test.sh

# Run with coverage
./e2e/taskapp_test.sh --cover

# Run with custom coverage directory
E2E_COVERAGE_DIR=my-coverage ./e2e/taskapp_test.sh --cover
```

The test apps share common infrastructure in `e2e/lib/`:
- `helpers.sh` - HTTP wrappers, assertions, colored output
- `runner.sh` - Server lifecycle, coverage collection, config templating

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
| End-to-end tests | `e2e/*_test.sh` | Shell scripts, start binary, make real HTTP requests |
| E2E configs | `testdata/*.yaml` | App configurations for E2E tests |
| E2E shared lib | `e2e/lib/` | Shared test infrastructure |
| Benchmarks | `internal/*/benchmark_test.go` | Performance tests |

All unit and integration tests use SQLite in-memory databases (`:memory:`) to avoid external dependencies.

## Roadmap

Planned features for future releases:

- [ ] **MySQL Support** - Add MySQL/MariaDB as a database backend option alongside SQL Server and SQLite.
- [ ] **PostgreSQL Support** - Add PostgreSQL as a database backend option.
- [ ] **TLS Support** - Native HTTPS termination without requiring a reverse proxy (Caddy/nginx). Will support configurable certificate paths and automatic Let's Encrypt integration.
- [x] **Rate Limiting** - Per-endpoint and per-client rate limiting to protect database resources from excessive requests.
- [x] **Workflows** - Multi-step query pipelines with conditional execution, iteration, and external API calls.
- [ ] **Authentication** - API key and/or JWT-based authentication for endpoint access control.
