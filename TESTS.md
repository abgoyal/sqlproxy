# Test Documentation

This document is auto-generated from test source files. Run `make test-docs` to regenerate.

## Coverage Summary

Run `make test-cover` for current coverage statistics.


---

## Config

**Package**: `internal/config`

### config_test.go

- **TestLoad_ValidConfig**: TestLoad_ValidConfig verifies a complete valid YAML config loads with all fields correctly populated
- **TestLoad_EnvironmentVariables**: TestLoad_EnvironmentVariables verifies ${VAR} placeholders in config are expanded from environment
- **TestLoad_MissingServerHost**: TestLoad_MissingServerHost ensures config loading fails when server.host is omitted
- **TestLoad_InvalidPort**: TestLoad_InvalidPort validates server.port must be in range 1-65535
- **TestLoad_InvalidTimeout**: TestLoad_InvalidTimeout checks timeout validation: positive values, max >= default
- **TestLoad_NoDatabases**: TestLoad_NoDatabases ensures at least one database connection is required
- **TestLoad_DuplicateDatabaseNames**: TestLoad_DuplicateDatabaseNames ensures database names must be unique across connections
- **TestLoad_InvalidDatabaseType**: TestLoad_InvalidDatabaseType rejects unsupported database types like mysql
- **TestLoad_SQLiteMissingPath**: TestLoad_SQLiteMissingPath ensures SQLite databases require a path field
- **TestLoad_SQLServerMissingFields**: TestLoad_SQLServerMissingFields validates SQL Server requires host, port, user, password, database
- **TestLoad_InvalidLogLevel**: TestLoad_InvalidLogLevel rejects log levels other than debug/info/warn/error
- **TestLoad_QueryMissingName**: TestLoad_QueryMissingName ensures every query must have a name field
- **TestLoad_QueryUnknownDatabase**: TestLoad_QueryUnknownDatabase rejects queries referencing non-existent database connections
- **TestLoad_QueryInvalidMethod**: TestLoad_QueryInvalidMethod ensures query method must be GET or POST only
- **TestLoad_QueryNegativeTimeout**: TestLoad_QueryNegativeTimeout rejects negative timeout_sec values on queries
- **TestLoad_InvalidIsolationLevel**: TestLoad_InvalidIsolationLevel rejects invalid SQL Server isolation level names
- **TestLoad_ScheduleOnlyQuery**: TestLoad_ScheduleOnlyQuery verifies queries can have schedule without HTTP path
- **TestDatabaseConfig_IsReadOnly**: TestDatabaseConfig_IsReadOnly verifies readonly defaults to true when nil
- **TestDatabaseConfig_DefaultSessionConfig**: TestDatabaseConfig_DefaultSessionConfig checks implicit defaults based on readonly flag
- **TestResolveSessionConfig**: TestResolveSessionConfig validates priority: query overrides > db overrides > defaults
- **TestValidIsolationLevels**: TestValidIsolationLevels checks the ValidIsolationLevels map contains correct entries
- **TestValidDeadlockPriorities**: TestValidDeadlockPriorities checks the ValidDeadlockPriorities map for low/normal/high
- **TestValidJournalModes**: TestValidJournalModes checks ValidJournalModes for SQLite: wal/delete/truncate/memory/off
- **TestValidDatabaseTypes**: TestValidDatabaseTypes checks ValidDatabaseTypes contains sqlserver and sqlite only


---

## Database

**Package**: `internal/db`

### benchmark_test.go

- **BenchmarkSQLiteDriver_SimpleQuery**: BenchmarkSQLiteDriver_SimpleQuery measures minimal "SELECT 1" query performance
- **BenchmarkSQLiteDriver_SelectAll**: BenchmarkSQLiteDriver_SelectAll measures full table scan of 1000 rows
- **BenchmarkSQLiteDriver_SelectWithParam**: BenchmarkSQLiteDriver_SelectWithParam measures parameterized single-row lookup
- **BenchmarkSQLiteDriver_SelectWithMultipleParams**: BenchmarkSQLiteDriver_SelectWithMultipleParams measures query with 3 WHERE parameters
- **BenchmarkSQLiteDriver_Insert**: BenchmarkSQLiteDriver_Insert measures single row insert performance
- **BenchmarkSQLiteDriver_ConcurrentReads**: BenchmarkSQLiteDriver_ConcurrentReads measures parallel read operations on file-based db
- **BenchmarkSQLiteDriver_TranslateQuery**: BenchmarkSQLiteDriver_TranslateQuery measures @param to $param translation speed
- **BenchmarkManager_Get**: BenchmarkManager_Get measures driver lookup by name across 3 databases
- **BenchmarkManager_Get_Concurrent**: BenchmarkManager_Get_Concurrent measures parallel driver lookups across 3 databases
- **BenchmarkManager_PingAll**: BenchmarkManager_PingAll measures ping across all 3 databases sequentially
- **BenchmarkSQLiteDriver_LargeResult_100**: BenchmarkSQLiteDriver_LargeResult_100 measures fetching 100 row result set
- **BenchmarkSQLiteDriver_LargeResult_1000**: BenchmarkSQLiteDriver_LargeResult_1000 measures fetching 1000 row result set
- **BenchmarkSQLiteDriver_LargeResult_10000**: BenchmarkSQLiteDriver_LargeResult_10000 measures fetching 10000 row result set
- **BenchmarkSQLiteDriver_ConcurrentWrites**: BenchmarkSQLiteDriver_ConcurrentWrites measures serialized parallel writes with mutex

### driver_test.go

- **TestNewDriver_SQLite**: TestNewDriver_SQLite verifies factory creates SQLite driver with :memory: path
- **TestNewDriver_SQLiteExplicit**: TestNewDriver_SQLiteExplicit confirms returned driver is *SQLiteDriver type
- **TestNewDriver_EmptyTypeDefaultsToSQLServer**: TestNewDriver_EmptyTypeDefaultsToSQLServer ensures empty type falls back to sqlserver
- **TestNewDriver_MySQL_NotImplemented**: TestNewDriver_MySQL_NotImplemented confirms mysql type returns not-implemented error
- **TestNewDriver_Postgres_NotImplemented**: TestNewDriver_Postgres_NotImplemented confirms postgres type returns not-implemented error
- **TestNewDriver_UnknownType**: TestNewDriver_UnknownType rejects unrecognized database types like oracle
- **TestNewDriver_SQLiteInvalidPath**: TestNewDriver_SQLiteInvalidPath ensures SQLite driver requires non-empty path
- **TestDriverInterface_SQLite**: TestDriverInterface_SQLite validates SQLiteDriver implements all Driver interface methods
- **TestDriverInterface_Polymorphism**: TestDriverInterface_Polymorphism verifies multiple drivers work through interface
- **TestNewDriver_AllTypes**: TestNewDriver_AllTypes table-tests factory behavior for all database type values

### manager_test.go

- **TestNewManager_SingleDatabase**: TestNewManager_SingleDatabase verifies manager creation with one SQLite database
- **TestNewManager_MultipleDatabases**: TestNewManager_MultipleDatabases tests manager with three databases, validates Get by name
- **TestNewManager_EmptyConfig**: TestNewManager_EmptyConfig confirms manager handles zero databases gracefully
- **TestNewManager_InvalidConfig**: TestNewManager_InvalidConfig ensures manager rejects invalid database config
- **TestManager_Get**: TestManager_Get tests retrieving connections by name and error for unknown names
- **TestManager_IsReadOnly**: TestManager_IsReadOnly validates readonly status lookup for each connection
- **TestManager_Ping**: TestManager_Ping checks connectivity to all managed databases individually
- **TestManager_PingAll**: TestManager_PingAll verifies all connections are healthy in single call
- **TestManager_Reconnect**: TestManager_Reconnect tests single connection re-establishment by name
- **TestManager_ReconnectAll**: TestManager_ReconnectAll reconnects all databases and verifies connectivity
- **TestManager_Close**: TestManager_Close ensures all connections are released and count returns 0
- **TestManager_ConcurrentAccess**: TestManager_ConcurrentAccess runs 100 concurrent Get and Ping operations
- **TestManager_MixedDatabaseTypes**: TestManager_MixedDatabaseTypes manages SQLite connections with different readonly/settings

### sqlite_test.go

- **TestNewSQLiteDriver_InMemory**: TestNewSQLiteDriver_InMemory verifies in-memory SQLite driver creation with :memory: path
- **TestNewSQLiteDriver_ReadWrite**: TestNewSQLiteDriver_ReadWrite confirms explicit readonly=false enables write mode
- **TestNewSQLiteDriver_MissingPath**: TestNewSQLiteDriver_MissingPath ensures empty path is rejected with clear error
- **TestNewSQLiteDriver_CustomSettings**: TestNewSQLiteDriver_CustomSettings verifies busy_timeout and journal_mode PRAGMAs apply
- **TestSQLiteDriver_Query_Simple**: TestSQLiteDriver_Query_Simple executes basic SELECT and validates returned columns
- **TestSQLiteDriver_Query_WithParams**: TestSQLiteDriver_Query_WithParams verifies @param named parameters work correctly
- **TestSQLiteDriver_Query_NullParams**: TestSQLiteDriver_Query_NullParams tests NULL parameter handling for optional filters
- **TestSQLiteDriver_Query_EmptyResult**: TestSQLiteDriver_Query_EmptyResult confirms empty result set returns zero-length slice
- **TestSQLiteDriver_Query_DateTimeHandling**: TestSQLiteDriver_Query_DateTimeHandling tests time.Time parameter binding and retrieval
- **TestSQLiteDriver_Query_SpecialCharacters**: TestSQLiteDriver_Query_SpecialCharacters ensures SQL injection strings are safely escaped
- **TestSQLiteDriver_Query_Unicode**: TestSQLiteDriver_Query_Unicode validates CJK, Cyrillic, Arabic, and emoji preservation
- **TestSQLiteDriver_Query_LargeResult**: TestSQLiteDriver_Query_LargeResult tests handling of 10000 row result sets
- **TestSQLiteDriver_Query_Timeout**: TestSQLiteDriver_Query_Timeout verifies context deadline expiration stops query
- **TestSQLiteDriver_Query_Concurrent**: TestSQLiteDriver_Query_Concurrent runs 100 parallel queries with file-based SQLite
- **TestSQLiteDriver_Ping**: TestSQLiteDriver_Ping confirms Ping returns nil for healthy connection
- **TestSQLiteDriver_Reconnect**: TestSQLiteDriver_Reconnect tests connection re-establishment after close
- **TestSQLiteDriver_Config**: TestSQLiteDriver_Config verifies Config() returns original configuration
- **TestSQLiteDriver_TranslateQuery**: TestSQLiteDriver_TranslateQuery tests @param to sql.Named translation and deduplication


---

## Handler

**Package**: `internal/handler`

### benchmark_test.go

- **BenchmarkHandler_ServeHTTP_SimpleQuery**: BenchmarkHandler_ServeHTTP_SimpleQuery measures simple query handling throughput
- **BenchmarkHandler_ServeHTTP_WithParams**: BenchmarkHandler_ServeHTTP_WithParams measures parameterized query performance
- **BenchmarkHandler_ServeHTTP_Concurrent**: BenchmarkHandler_ServeHTTP_Concurrent measures parallel request handling
- **BenchmarkConvertValue_String**: BenchmarkConvertValue_String measures string type conversion speed
- **BenchmarkConvertValue_Int**: BenchmarkConvertValue_Int measures integer type conversion speed
- **BenchmarkConvertValue_Float**: BenchmarkConvertValue_Float measures float type conversion speed
- **BenchmarkConvertValue_Bool**: BenchmarkConvertValue_Bool measures boolean type conversion speed
- **BenchmarkConvertValue_DateTime**: BenchmarkConvertValue_DateTime measures datetime parsing performance
- **BenchmarkGenerateRequestID**: BenchmarkGenerateRequestID measures random ID generation throughput
- **BenchmarkGetOrGenerateRequestID_WithHeader**: BenchmarkGetOrGenerateRequestID_WithHeader measures header extraction speed
- **BenchmarkGetOrGenerateRequestID_NoHeader**: BenchmarkGetOrGenerateRequestID_NoHeader measures ID generation when no header
- **BenchmarkHandler_ParseParameters_NoParams**: BenchmarkHandler_ParseParameters_NoParams measures parsing overhead with zero params
- **BenchmarkHandler_ParseParameters_ManyParams**: BenchmarkHandler_ParseParameters_ManyParams measures parsing 5 params with type conversion
- **BenchmarkHandler_ResolveTimeout**: BenchmarkHandler_ResolveTimeout measures timeout resolution with query params

### handler_test.go

- **TestHandler_ServeHTTP_SimpleQuery**: TestHandler_ServeHTTP_SimpleQuery validates basic GET query returns JSON with success and data
- **TestHandler_ServeHTTP_WithParameters**: TestHandler_ServeHTTP_WithParameters tests query string parameters are bound to SQL
- **TestHandler_ServeHTTP_MissingRequiredParam**: TestHandler_ServeHTTP_MissingRequiredParam returns 400 when required parameter missing
- **TestHandler_ServeHTTP_DefaultParameter**: TestHandler_ServeHTTP_DefaultParameter uses default value when optional param omitted
- **TestHandler_ServeHTTP_WrongMethod**: TestHandler_ServeHTTP_WrongMethod returns 405 when HTTP method doesn't match config
- **TestHandler_ServeHTTP_InvalidParamType**: TestHandler_ServeHTTP_InvalidParamType returns 400 when int param gets non-numeric value
- **TestHandler_ServeHTTP_POSTMethod**: TestHandler_ServeHTTP_POSTMethod tests form-encoded POST parameters are parsed
- **TestHandler_ServeHTTP_CustomRequestID**: TestHandler_ServeHTTP_CustomRequestID echoes X-Request-ID or X-Correlation-ID headers
- **TestHandler_ResolveTimeout**: TestHandler_ResolveTimeout validates timeout priority: _timeout param > query > default, capped by max
- **TestConvertValue**: TestConvertValue tests type conversion for string/int/bool/float/datetime parameters
- **TestHandler_ParseParameters**: TestHandler_ParseParameters validates required/optional/default parameter handling
- **TestHandler_ServeHTTP_EmptyResult**: TestHandler_ServeHTTP_EmptyResult returns success with count=0 for no matching rows
- **TestHandler_ServeHTTP_SQLError**: TestHandler_ServeHTTP_SQLError returns 500 for queries against non-existent tables
- **TestHandler_ServeHTTP_DateTimeParam**: TestHandler_ServeHTTP_DateTimeParam tests datetime parameter parsing and SQL binding
- **TestGenerateRequestID**: TestGenerateRequestID validates unique 16-char hex IDs are generated
- **TestGetOrGenerateRequestID**: TestGetOrGenerateRequestID checks header extraction priority and fallback generation


---

## Scheduler

**Package**: `internal/scheduler`

### scheduler_test.go

- **TestNew**: TestNew verifies scheduler creation only registers queries with schedule config
- **TestScheduler_StartStop**: TestScheduler_StartStop confirms scheduler starts and stops gracefully
- **TestScheduler_RunQuery**: TestScheduler_RunQuery tests direct query execution returns correct count
- **TestScheduler_RunQueryWithParams**: TestScheduler_RunQueryWithParams tests scheduled query with bound parameter values
- **TestScheduler_RunQueryError**: TestScheduler_RunQueryError verifies error handling for queries against non-existent tables
- **TestScheduler_BuildParams**: TestScheduler_BuildParams tests parameter resolution using defaults and schedule overrides
- **TestScheduler_ResolveValue_DynamicDates**: TestScheduler_ResolveValue_DynamicDates tests dynamic date keywords: now, today, yesterday, tomorrow
- **TestScheduler_ResolveValue_Types**: TestScheduler_ResolveValue_Types tests type conversion for string, int, and bool parameters
- **TestScheduler_ResolveValue_DateFormats**: TestScheduler_ResolveValue_DateFormats tests datetime parsing with various input formats
- **TestParseInt**: TestParseInt tests integer parsing accepts positives, rejects negatives and invalid input
- **TestHasScheduledQueries**: TestHasScheduledQueries tests detection of scheduled queries in config list
- **TestScheduler_InvalidCron**: TestScheduler_InvalidCron verifies invalid cron expressions are rejected without panic
- **TestScheduler_UnknownDatabase**: TestScheduler_UnknownDatabase tests error for queries referencing non-existent database
- **TestScheduler_CustomTimeout**: TestScheduler_CustomTimeout tests query-specific timeout configuration is applied
- **TestScheduler_ExecuteJob**: TestScheduler_ExecuteJob tests job execution wrapper runs query and logs results
- **TestScheduler_ExecuteJob_WithFailure**: TestScheduler_ExecuteJob_WithFailure tests job execution handles query failures without panic


---

## Validation

**Package**: `internal/validate`

### validate_test.go

- **TestResult_AddError**: TestResult_AddError verifies error accumulation marks result as invalid
- **TestResult_AddWarning**: TestResult_AddWarning confirms warnings don't affect valid flag
- **TestValidateServer**: TestValidateServer tests server port and timeout validation rules
- **TestValidateDatabase_Empty**: TestValidateDatabase_Empty ensures empty database list is rejected
- **TestValidateDatabase_Duplicate**: TestValidateDatabase_Duplicate ensures duplicate database names are rejected
- **TestValidateDatabase_InvalidType**: TestValidateDatabase_InvalidType ensures unsupported database types are rejected
- **TestValidateDatabase_SQLite**: TestValidateDatabase_SQLite tests SQLite-specific validation: path, journal mode, timeout
- **TestValidateDatabase_SQLServer**: TestValidateDatabase_SQLServer tests SQL Server validation: host, port, isolation, timeout
- **TestValidateDatabase_EnvVarWarning**: TestValidateDatabase_EnvVarWarning tests unresolved env vars generate warnings
- **TestValidateLogging**: TestValidateLogging tests log level validation accepts only debug/info/warn/error
- **TestValidateQueries_NoQueries**: TestValidateQueries_NoQueries tests empty queries list generates warning
- **TestValidateQueries_DuplicateName**: TestValidateQueries_DuplicateName ensures duplicate query names are rejected
- **TestValidateQueries_DuplicatePath**: TestValidateQueries_DuplicatePath ensures duplicate endpoint paths are rejected
- **TestValidateQueries_InvalidPath**: TestValidateQueries_InvalidPath ensures path must start with leading /
- **TestValidateQueries_InvalidMethod**: TestValidateQueries_InvalidMethod ensures only GET/POST methods are allowed
- **TestValidateQueries_UnknownDatabase**: TestValidateQueries_UnknownDatabase ensures query must reference existing database
- **TestValidateQueries_WriteOnReadOnly**: TestValidateQueries_WriteOnReadOnly ensures write SQL rejected on read-only database
- **TestValidateQueries_WriteOnReadWrite**: TestValidateQueries_WriteOnReadWrite confirms write SQL allowed on write-enabled database
- **TestValidateQueries_UnusedDatabase**: TestValidateQueries_UnusedDatabase tests unused database generates warning
- **TestValidateParams**: TestValidateParams tests SQL/parameter cross-validation for mismatches and reserved names
- **TestValidateSchedule**: TestValidateSchedule tests cron expression and required parameter validation
- **TestRun_ValidConfig**: TestRun_ValidConfig tests complete valid configuration passes all checks
- **TestRun_InvalidConfig**: TestRun_InvalidConfig tests configuration with invalid port fails validation
- **TestRun_DBConnectionTest**: TestRun_DBConnectionTest verifies SQLite :memory: connection succeeds
- **TestRun_DBConnectionFail**: TestRun_DBConnectionFail verifies invalid SQLite path fails connection test


---

## Server

**Package**: `internal/server`

### server_test.go

- **TestServer_New**: TestServer_New verifies server initialization creates dbManager and httpServer
- **TestServer_HealthHandler**: TestServer_HealthHandler tests /health returns status and database connections
- **TestServer_MetricsHandler_Disabled**: TestServer_MetricsHandler_Disabled tests /metrics returns not-enabled message when disabled
- **TestServer_LogLevelHandler**: TestServer_LogLevelHandler tests log level GET retrieval and POST update operations
- **TestServer_ListEndpointsHandler**: TestServer_ListEndpointsHandler tests root path returns service info and endpoint listing
- **TestServer_ListEndpointsHandler_NotFound**: TestServer_ListEndpointsHandler_NotFound tests unknown paths return 404
- **TestServer_OpenAPIHandler**: TestServer_OpenAPIHandler tests /openapi.json returns valid spec with CORS headers
- **TestServer_RecoveryMiddleware**: TestServer_RecoveryMiddleware tests panic recovery returns 500 without server crash
- **TestServer_GzipMiddleware**: TestServer_GzipMiddleware tests gzip compression when Accept-Encoding header set
- **TestServer_GzipMiddleware_NoGzip**: TestServer_GzipMiddleware_NoGzip tests no compression without Accept-Encoding header
- **TestServer_StartShutdown**: TestServer_StartShutdown tests server start and graceful shutdown sequence
- **TestServer_E2E_QueryEndpoint**: TestServer_E2E_QueryEndpoint tests end-to-end query execution via HTTP test server
- **TestServer_E2E_ParameterizedQuery**: TestServer_E2E_ParameterizedQuery tests parameterized query with required and optional params
- **TestServer_E2E_WithGzip**: TestServer_E2E_WithGzip tests full HTTP request/response cycle with gzip encoding


---

## Logging

**Package**: `internal/logging`

### logging_test.go

- **TestInit_Stdout**: TestInit_Stdout verifies logger initialization to stdout sets correct level
- **TestInit_FileOutput**: TestInit_FileOutput tests logger initialization creates log directory and file
- **TestSetLevel**: TestSetLevel tests dynamic log level changes including case handling
- **TestGetLevel**: TestGetLevel verifies GetLevel returns correct string for each slog level
- **TestParseLevel**: TestParseLevel tests string to slog.Level parsing with case insensitivity
- **TestMapToAttrs**: TestMapToAttrs tests map to slog attribute slice conversion
- **TestLogFunctions**: TestLogFunctions tests Debug, Info, Warn, Error output to buffer
- **TestLogFunctions_NilFields**: TestLogFunctions_NilFields verifies log functions handle nil field maps without panic
- **TestClose_NoFile**: TestClose_NoFile tests Close handles nil file closer gracefully
- **TestClose_WithFile**: TestClose_WithFile tests Close properly closes log file handle
- **TestInit_InvalidDirectory**: TestInit_InvalidDirectory tests Init handles permission-denied paths without panic


---

## Metrics

**Package**: `internal/metrics`

### benchmark_test.go

- **BenchmarkRecord**: BenchmarkRecord measures single metric recording throughput
- **BenchmarkRecord_Concurrent**: BenchmarkRecord_Concurrent measures parallel metric recording with RunParallel
- **BenchmarkRecord_MultipleEndpoints**: BenchmarkRecord_MultipleEndpoints measures recording across 5 different endpoints
- **BenchmarkGetSnapshot**: BenchmarkGetSnapshot measures snapshot retrieval with 10 pre-populated endpoints
- **BenchmarkGetSnapshot_Concurrent**: BenchmarkGetSnapshot_Concurrent measures parallel snapshot reads under load
- **BenchmarkRecord_WithError**: BenchmarkRecord_WithError measures error metric recording with status 500
- **BenchmarkRecord_WithTimeout**: BenchmarkRecord_WithTimeout measures timeout metric recording with status 504
- **BenchmarkInit**: BenchmarkInit measures collector initialization throughput
- **BenchmarkMixedWorkload**: BenchmarkMixedWorkload simulates real-world usage: 90% success, 10% error, 1% snapshots

### metrics_test.go

- **TestInit**: TestInit verifies metrics collector initialization with health checker
- **TestRecord_NoCollector**: TestRecord_NoCollector verifies Record handles nil collector without panic
- **TestRecord**: TestRecord tests request metric recording and snapshot retrieval
- **TestRecord_Error**: TestRecord_Error tests error counter increment on 500 status
- **TestRecord_Timeout**: TestRecord_Timeout tests timeout counter increment on 504 status
- **TestRecord_MinMaxDuration**: TestRecord_MinMaxDuration tests min/max duration tracking across requests
- **TestRecord_Averages**: TestRecord_Averages tests average duration calculation for total and query times
- **TestGetSnapshot_NoCollector**: TestGetSnapshot_NoCollector verifies nil return when collector not initialized
- **TestGetSnapshot_RuntimeStats**: TestGetSnapshot_RuntimeStats verifies Go runtime stats in snapshot
- **TestGetSnapshot_Uptime**: TestGetSnapshot_Uptime tests uptime calculation in snapshot
- **TestGetSnapshot_DBHealth**: TestGetSnapshot_DBHealth tests database health status via checker function
- **TestRecord_Concurrent**: TestRecord_Concurrent tests thread-safe metric recording with 100 goroutines
- **TestRecord_MultipleEndpoints**: TestRecord_MultipleEndpoints tests separate stats tracking per endpoint
- **TestClose**: TestClose verifies Close is a no-op and doesn't panic
- **TestEndpointStats_Fields**: TestEndpointStats_Fields verifies all endpoint stat fields are populated
- **TestSnapshot_Timestamp**: TestSnapshot_Timestamp verifies snapshot timestamp is set correctly


---

## OpenAPI

**Package**: `internal/openapi`

### openapi_test.go

- **TestSpec_BasicStructure**: TestSpec_BasicStructure verifies OpenAPI spec has required root elements
- **TestSpec_BuiltInPaths**: TestSpec_BuiltInPaths verifies /health, /metrics, /config/loglevel paths are present
- **TestSpec_QueryEndpoints**: TestSpec_QueryEndpoints tests query config generates correct path operations
- **TestSpec_SkipsScheduleOnlyQueries**: TestSpec_SkipsScheduleOnlyQueries verifies schedule-only queries are excluded from paths
- **TestBuildQueryPath_GET**: TestBuildQueryPath_GET tests GET path generation with parameters and tags
- **TestBuildQueryPath_POST**: TestBuildQueryPath_POST tests POST method creates post operation, not get
- **TestBuildQueryPath_Responses**: TestBuildQueryPath_Responses verifies 200, 400, 500, 504 response codes present
- **TestBuildParamDescription**: TestBuildParamDescription tests parameter description includes type and default
- **TestParamTypeToSchema**: TestParamTypeToSchema tests parameter type to JSON Schema conversion
- **TestBuildComponents**: TestBuildComponents verifies required schema definitions are present
- **TestSpec_ValidJSON**: TestSpec_ValidJSON verifies spec serializes to valid JSON and back
- **TestSpec_TimeoutParameter**: TestSpec_TimeoutParameter tests _timeout param has correct default and maximum
- **TestSpec_QueryDescription**: TestSpec_QueryDescription tests custom description and timeout info in spec
- **TestBuildQueryPath_DefaultTimeout**: TestBuildQueryPath_DefaultTimeout tests server default timeout used when query has none


---

## Running Tests

```bash
# Run all tests
make test

# Run tests for a specific package
make test-db
make test-handler
# etc.

# Run with coverage
make test-cover
make test-cover-html

# Run benchmarks
make test-bench
```

## Test Organization

- **Unit tests**: Test individual functions and methods
- **Integration tests**: Test component interactions (e.g., handler + db)
- **E2E tests**: Test full request/response cycles
- **Benchmarks**: Performance tests (prefixed with `Benchmark`)

All tests use SQLite in-memory databases to avoid external dependencies.
