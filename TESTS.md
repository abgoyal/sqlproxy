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
- **TestIsArrayType**: TestIsArrayType verifies IsArrayType correctly identifies array types
- **TestArrayBaseType**: TestArrayBaseType verifies ArrayBaseType extracts the base type from array types
- **TestValidParameterTypes**: TestValidParameterTypes verifies all expected parameter types are in ValidParameterTypes
- **TestQueryRateLimitConfig_IsPoolReference**: TestQueryRateLimitConfig_IsPoolReference verifies IsPoolReference returns true only when Pool is set
- **TestQueryRateLimitConfig_IsInline**: TestQueryRateLimitConfig_IsInline verifies IsInline returns true only when both RequestsPerSecond and Burst are positive


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
- **TestManager_ConcurrentReconnect**: TestManager_ConcurrentReconnect tests concurrent Reconnect calls to prevent race conditions
- **TestManager_ConcurrentReconnectAll**: TestManager_ConcurrentReconnectAll tests concurrent ReconnectAll calls
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

### sqlserver_test.go

- **TestIsolationToSQL**: TestIsolationToSQL tests conversion of config isolation strings to SQL Server syntax
- **TestDeadlockPriorityToSQL**: TestDeadlockPriorityToSQL tests conversion of config deadlock priority strings to SQL Server syntax
- **TestSQLServerDriver_BuildArgs**: TestSQLServerDriver_BuildArgs verifies parameter extraction from SQL
- **TestSQLServerDriver_BuildArgs_Values**: TestSQLServerDriver_BuildArgs_Values verifies parameter values are correctly assigned
- **TestSQLServerDriver_BuildArgs_NilValue**: TestSQLServerDriver_BuildArgs_NilValue verifies nil values are handled correctly


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
- **TestSanitizeHeaderValue**: TestSanitizeHeaderValue tests header value sanitization for security
- **TestGetOrGenerateRequestID_Sanitizes**: TestGetOrGenerateRequestID_Sanitizes validates that request IDs from headers are sanitized
- **TestHandler_ServeHTTP_JSONBody**: TestHandler_ServeHTTP_JSONBody tests JSON body parsing for POST endpoints
- **TestHandler_ServeHTTP_JSONBody_RejectsNestedForNonJSONType**: TestHandler_ServeHTTP_JSONBody_RejectsNestedForNonJSONType tests that nested JSON is rejected for non-json types
- **TestHandler_ServeHTTP_JSONTypeParam**: TestHandler_ServeHTTP_JSONTypeParam tests json type parameter accepts nested objects
- **TestHandler_ServeHTTP_ArrayTypeParam**: TestHandler_ServeHTTP_ArrayTypeParam tests array type parameters (int[], string[], etc.)
- **TestHandler_ServeHTTP_ArrayTypeParam_InvalidElement**: TestHandler_ServeHTTP_ArrayTypeParam_InvalidElement tests array type rejects wrong element types
- **TestHandler_ServeHTTP_StringArrayParam**: TestHandler_ServeHTTP_StringArrayParam tests string[] type parameter
- **TestConvertJSONValue**: TestConvertJSONValue tests JSON value type conversion
- **TestConvertJSONValue_JSONType**: TestConvertJSONValue_JSONType tests json type serializes to JSON string
- **TestConvertJSONValue_ArrayTypes**: TestConvertJSONValue_ArrayTypes tests array types serialize to JSON array string
- **TestConvertValue_JSONType**: TestConvertValue_JSONType tests json type from query string
- **TestConvertValue_ArrayTypes**: TestConvertValue_ArrayTypes tests array types from query string
- **TestValidateArrayElements**: TestValidateArrayElements tests array element validation
- **TestParseJSONColumns**: TestParseJSONColumns tests parsing JSON string columns into objects
- **TestHandler_ServeHTTP_JSONColumns**: TestHandler_ServeHTTP_JSONColumns tests json_columns config parses JSON in response
- **TestHandler_ServeHTTP_JSONColumns_WithoutConfig**: TestHandler_ServeHTTP_JSONColumns_WithoutConfig tests default behavior (no json_columns)


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
- **TestValidateLogging**: TestValidateLogging tests log level and rotation settings validation
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
- **TestRun_SQLServerUnresolvedEnvVar**: TestRun_SQLServerUnresolvedEnvVar tests that SQL Server with unresolved env vars is skipped during connection test
- **TestRun_SQLServerUnresolvedPassword**: TestRun_SQLServerUnresolvedPassword tests SQL Server with unresolved password env var is skipped
- **TestValidateQueries_ScheduleOnlyQuery**: TestValidateQueries_ScheduleOnlyQuery tests schedule-only queries (no HTTP path) are valid
- **TestValidateQueries_QueryWithTimeout**: TestValidateQueries_QueryWithTimeout tests query with custom timeout is validated
- **TestValidateQueries_AllWriteOperations**: TestValidateQueries_AllWriteOperations tests all write operations are detected
- **TestValidateWebhook**: TestValidateWebhook tests webhook configuration validation
- **TestValidateWebhookBody**: TestValidateWebhookBody tests webhook body configuration validation
- **TestValidateScheduleWithWebhook**: TestValidateScheduleWithWebhook tests schedule validation with webhook
- **TestValidateCache**: TestValidateCache tests cache configuration validation
- **TestValidateServerCache**: TestValidateServerCache tests server-level cache configuration validation
- **TestValidateQueries_WithCache**: TestValidateQueries_WithCache tests query-level cache validation integration
- **TestValidateTemplate**: TestValidateTemplate tests template syntax validation
- **TestValidateJSONColumns**: TestValidateJSONColumns tests json_columns validation
- **TestValidateQueries_JSONColumns**: TestValidateQueries_JSONColumns tests json_columns in full query validation
- **TestValidateQueries_JSONColumns_EmptyColumn**: TestValidateQueries_JSONColumns_EmptyColumn tests validation catches empty column name
- **TestValidateRateLimits**: TestValidateRateLimits tests server-level rate limit pool validation
- **TestValidateQueryRateLimits**: TestValidateQueryRateLimits tests per-query rate limit validation
- **TestValidateQueries_RateLimits**: TestValidateQueries_RateLimits tests rate limit validation in full query validation
- **TestValidateQueries_RateLimits_UnknownPool**: TestValidateQueries_RateLimits_UnknownPool tests that unknown pool reference is caught


---

## Server

**Package**: `internal/server`

### server_test.go

- **TestServer_New**: TestServer_New verifies server initialization creates dbManager and httpServer
- **TestServer_HealthHandler**: TestServer_HealthHandler tests /health returns status and database connections
- **TestServer_MetricsHandler_Disabled**: TestServer_MetricsHandler_Disabled tests /_/metrics returns not-enabled message when disabled
- **TestServer_LogLevelHandler**: TestServer_LogLevelHandler tests log level GET retrieval and POST update operations
- **TestServer_ListEndpointsHandler**: TestServer_ListEndpointsHandler tests root path returns service info and endpoint listing
- **TestServer_ListEndpointsHandler_NotFound**: TestServer_ListEndpointsHandler_NotFound tests unknown paths return 404
- **TestServer_OpenAPIHandler**: TestServer_OpenAPIHandler tests /openapi.json returns valid spec with CORS headers
- **TestServer_RecoveryMiddleware**: TestServer_RecoveryMiddleware tests panic recovery returns 500 without server crash
- **TestServer_GzipMiddleware**: TestServer_GzipMiddleware tests gzip compression when Accept-Encoding header set
- **TestServer_GzipMiddleware_NoGzip**: TestServer_GzipMiddleware_NoGzip tests no compression without Accept-Encoding header
- **TestServer_StartShutdown**: TestServer_StartShutdown tests server start and graceful shutdown sequence
- **TestServer_Integration_QueryEndpoint**: TestServer_Integration_QueryEndpoint tests query execution via httptest server
- **TestServer_Integration_ParameterizedQuery**: TestServer_Integration_ParameterizedQuery tests parameterized query with required and optional params
- **TestServer_Integration_WithGzip**: TestServer_Integration_WithGzip tests HTTP request/response cycle with gzip encoding
- **TestServer_HealthHandler_Degraded**: TestServer_HealthHandler_Degraded tests /health returns degraded status when database is unreachable
- **TestServer_HealthHandler_DatabaseDown**: TestServer_HealthHandler_DatabaseDown tests /_/health shows database as disconnected when ping fails
- **TestServer_HealthHandler_MultipleDatabases**: TestServer_HealthHandler_MultipleDatabases tests /_/health with multiple database connections
- **TestServer_DBHealthHandler**: TestServer_DBHealthHandler tests /_/health/{dbname} endpoint
- **TestServer_DBHealthHandler_Disconnected**: TestServer_DBHealthHandler_Disconnected tests /_/health/{dbname} when db is down
- **TestServer_Integration_WithCache**: TestServer_Integration_WithCache tests cache hit/miss behavior and headers
- **TestServer_Integration_CacheMetrics**: TestServer_Integration_CacheMetrics tests cache stats appear in metrics snapshot
- **TestServer_CacheClearHandler**: TestServer_CacheClearHandler tests /_/cache/clear endpoint
- **TestServer_CacheClearHandler_NoCacheConfigured**: TestServer_CacheClearHandler_NoCacheConfigured tests cache clear when cache disabled
- **TestServer_RateLimitsHandler**: TestServer_RateLimitsHandler tests the /_/ratelimits endpoint
- **TestServer_RateLimitsHandler_NotConfigured**: TestServer_RateLimitsHandler_NotConfigured tests the endpoint when rate limiting is disabled
- **TestServer_RateLimitResponse**: TestServer_RateLimitResponse tests that 429 response includes retry_after_sec


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
- **TestEndpointStats_Fields**: TestEndpointStats_Fields verifies all endpoint stat fields are populated
- **TestSnapshot_Timestamp**: TestSnapshot_Timestamp verifies snapshot timestamp is set correctly
- **TestSnapshot_Version**: TestSnapshot_Version verifies version and buildTime are included in snapshot
- **TestSnapshot_EmptyVersion**: TestSnapshot_EmptyVersion verifies empty version/buildTime are handled correctly
- **TestReset**: TestReset verifies metrics are cleared while preserving configuration
- **TestReset_NoCollector**: TestReset_NoCollector verifies Reset handles nil collector
- **TestSetRateLimitSnapshotProvider**: TestSetRateLimitSnapshotProvider verifies rate limit metrics are included in snapshot
- **TestSetRateLimitSnapshotProvider_NoCollector**: TestSetRateLimitSnapshotProvider_NoCollector verifies nil collector handling
- **TestSnapshot_BothCacheAndRateLimits**: TestSnapshot_BothCacheAndRateLimits verifies both cache and rate limit metrics work together


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
- **TestParamTypeToSchema_ArrayTypes**: TestParamTypeToSchema_ArrayTypes tests array type schema generation
- **TestParamTypeToSchema_JSONType**: TestParamTypeToSchema_JSONType tests json type schema generation
- **TestBuildQueryPath_DefaultTimeout**: TestBuildQueryPath_DefaultTimeout tests server default timeout used when query has none


---

## Service

**Package**: `internal/service`

### service_test.go

- **TestDefaultServiceName**: TestDefaultServiceName verifies the default service name constant
- **TestJoinErrors**: TestJoinErrors verifies error joining function


---

## Webhook

**Package**: `internal/webhook`

### benchmark_test.go

- **BenchmarkExecuteTemplate**: BenchmarkExecuteTemplate benchmarks template execution with various complexity
- **BenchmarkBuildBody**: BenchmarkBuildBody benchmarks body building for webhook payloads
- **BenchmarkExecute**: BenchmarkExecute benchmarks end-to-end webhook execution
- **BenchmarkExecute_WithBodyTemplate**: BenchmarkExecute_WithBodyTemplate benchmarks execution with body templates
- **BenchmarkResolveRetryConfig**: BenchmarkResolveRetryConfig benchmarks retry config resolution
- **BenchmarkBuildBody_Parallel**: BenchmarkBuildBody_Parallel benchmarks concurrent body building

### webhook_test.go

- **TestExecuteTemplate_Basic**: TestExecuteTemplate_Basic tests basic template execution
- **TestExecuteTemplate_Functions**: TestExecuteTemplate_Functions tests custom template functions
- **TestExecuteItemTemplate**: TestExecuteItemTemplate tests item template with row data
- **TestBuildBody_RawJSON**: TestBuildBody_RawJSON tests raw JSON output when no body config
- **TestBuildBody_HeaderItemFooter**: TestBuildBody_HeaderItemFooter tests templated body building
- **TestBuildBody_EmptyTemplate**: TestBuildBody_EmptyTemplate tests alternate empty template
- **TestBuildBody_DefaultSeparator**: TestBuildBody_DefaultSeparator tests default comma separator when not specified
- **TestBuildBody_NewlineSeparator**: TestBuildBody_NewlineSeparator tests newline separator for list format
- **TestBuildBody_ParamsAccess**: TestBuildBody_ParamsAccess tests access to params in templates
- **TestExecute_RawPayload**: TestExecute_RawPayload tests webhook execution with raw JSON
- **TestExecute_TemplatedURL**: TestExecute_TemplatedURL tests URL template execution
- **TestExecute_SkipOnEmpty**: TestExecute_SkipOnEmpty tests on_empty: skip behavior
- **TestExecute_SendOnEmpty**: TestExecute_SendOnEmpty tests on_empty: send (default) behavior
- **TestExecute_HTTPError**: TestExecute_HTTPError tests error handling for non-2xx responses
- **TestExecute_Timeout**: TestExecute_Timeout tests context timeout
- **TestExecute_HTTPMethods**: TestExecute_HTTPMethods tests default POST and explicit GET methods
- **TestBuildBody_SlackFormat**: TestBuildBody_SlackFormat tests building Slack-style webhook body
- **TestExecuteTemplate_InvalidTemplate**: TestExecuteTemplate_InvalidTemplate tests error handling for invalid templates
- **TestExecute_URLTemplateError**: TestExecute_URLTemplateError tests error when URL template is invalid
- **TestExecute_InvalidURL**: TestExecute_InvalidURL tests error when URL is malformed
- **TestBuildBody_TemplateErrors**: TestBuildBody_TemplateErrors tests error messages identify which template failed
- **TestExecuteTemplate_ExecutionError**: TestExecuteTemplate_ExecutionError tests template execution error (not parse error)
- **TestJsonFunction_Error**: TestJsonFunction_Error tests json function with unmarshalable value
- **TestJsonFunctions_Error**: TestJsonFunctions_Error tests json/jsonIndent functions with unmarshalable values
- **TestExecute_ConnectionError**: TestExecute_ConnectionError tests error when server is unreachable
- **TestResolveRetryConfig**: TestResolveRetryConfig tests retry configuration resolution
- **TestExecute_RetryDisabled**: TestExecute_RetryDisabled tests that retries are skipped when disabled
- **TestExecute_CustomRetryConfig**: TestExecute_CustomRetryConfig tests custom retry settings
- **TestExecute_BackoffTiming**: TestExecute_BackoffTiming tests exponential backoff with cap
- **TestExecute_BackoffCapping**: TestExecute_BackoffCapping tests that backoff is capped at MaxBackoff
- **TestExecutionContext_Version**: TestExecutionContext_Version tests that version is included in ExecutionContext


---

## Cache

**Package**: `internal/cache`

### cache_test.go

- **TestNew**: TestNew verifies cache creation with different configurations
- **TestCache_GetSet**: TestCache_GetSet tests basic cache operations
- **TestCache_Delete**: TestCache_Delete tests cache entry deletion
- **TestCache_Clear**: TestCache_Clear tests clearing all entries for an endpoint
- **TestCache_ClearAll**: TestCache_ClearAll tests clearing entire cache
- **TestCache_TTL**: TestCache_TTL tests TTL expiration
- **TestCache_GetSnapshot**: TestCache_GetSnapshot tests metrics snapshot
- **TestCache_GetTTLRemaining**: TestCache_GetTTLRemaining tests remaining TTL calculation
- **TestBuildKey**: TestBuildKey tests cache key template execution
- **TestCache_NilSafe**: TestCache_NilSafe tests that nil cache is handled safely
- **TestCache_MultipleEndpoints**: TestCache_MultipleEndpoints tests independent tracking per endpoint
- **TestCache_PerEndpointSizeLimit**: TestCache_PerEndpointSizeLimit tests per-endpoint size limits trigger eviction
- **TestRegisterEndpoint_CronEviction**: TestRegisterEndpoint_CronEviction tests cron-based eviction setup
- **TestRegisterEndpoint_NilConfig**: TestRegisterEndpoint_NilConfig tests registering with nil config
- **TestCache_UpdateExistingKey**: TestCache_UpdateExistingKey tests updating an existing cached entry
- **TestCache_DefaultTTL**: TestCache_DefaultTTL tests that TTL=0 uses server default TTL
- **TestCache_UnregisteredEndpoint**: TestCache_UnregisteredEndpoint tests operations on endpoints not explicitly registered
- **TestCalculateSize**: TestCalculateSize tests size calculation for cache entries
- **TestGetOrCompute**: TestGetOrCompute tests the GetOrCompute method
- **TestGetOrCompute_Error**: TestGetOrCompute_Error tests error handling in GetOrCompute
- **TestGetOrCompute_NilCache**: TestGetOrCompute_NilCache tests GetOrCompute with nil cache
- **TestGetOrCompute_Singleflight**: TestGetOrCompute_Singleflight tests that singleflight prevents stampedes
- **TestCache_EvictFromEndpoint**: TestCache_EvictFromEndpoint tests LRU eviction when per-endpoint size is exceeded
- **TestCache_EvictionMetrics**: TestCache_EvictionMetrics tests that eviction metrics are properly tracked
- **TestCache_CronEvictionExecution**: TestCache_CronEvictionExecution tests that cron eviction runs and clears cache
- **TestCache_ClearTriggersEvictionMetric**: TestCache_ClearTriggersEvictionMetric tests that Clear increments eviction count


---

## Template Engine

**Package**: `internal/tmpl`

### benchmark_test.go

- **BenchmarkEngine_CacheKey_Simple**: BenchmarkEngine_CacheKey_Simple benchmarks simple cache key like "items:{{.Param.status}}"
- **BenchmarkEngine_CacheKey_MultiParam**: BenchmarkEngine_CacheKey_MultiParam benchmarks cache key with multiple params
- **BenchmarkEngine_CacheKey_WithDefault**: BenchmarkEngine_CacheKey_WithDefault benchmarks cache key with default fallback
- **BenchmarkEngine_RateLimit_ClientIP**: BenchmarkEngine_RateLimit_ClientIP benchmarks simple rate limit key
- **BenchmarkEngine_RateLimit_Composite**: BenchmarkEngine_RateLimit_Composite benchmarks composite rate limit key
- **BenchmarkEngine_RateLimit_HeaderRequired**: BenchmarkEngine_RateLimit_HeaderRequired benchmarks rate limit with required header
- **BenchmarkEngine_Webhook_SimpleJSON**: BenchmarkEngine_Webhook_SimpleJSON benchmarks simple webhook JSON body
- **BenchmarkEngine_Webhook_SlackFormat**: BenchmarkEngine_Webhook_SlackFormat benchmarks Slack webhook body
- **BenchmarkEngine_Webhook_WithConditional**: BenchmarkEngine_Webhook_WithConditional benchmarks webhook with conditional logic
- **BenchmarkEngine_ExecuteInline**: BenchmarkEngine_ExecuteInline benchmarks inline (non-cached) template execution
- **BenchmarkEngine_Register**: BenchmarkEngine_Register benchmarks template registration/compilation
- **BenchmarkEngine_Validate**: BenchmarkEngine_Validate benchmarks template validation
- **BenchmarkEngine_ValidateWithParams**: BenchmarkEngine_ValidateWithParams benchmarks template validation with param checking
- **BenchmarkContextBuilder_Simple**: BenchmarkContextBuilder_Simple benchmarks basic context building
- **BenchmarkContextBuilder_WithHeaders**: BenchmarkContextBuilder_WithHeaders benchmarks context with many headers
- **BenchmarkContextBuilder_WithResult**: BenchmarkContextBuilder_WithResult benchmarks context with query result
- **BenchmarkExtractParamRefs_Simple**: BenchmarkExtractParamRefs_Simple benchmarks simple param extraction
- **BenchmarkExtractParamRefs_Complex**: BenchmarkExtractParamRefs_Complex benchmarks complex param extraction
- **BenchmarkExtractHeaderRefs**: BenchmarkExtractHeaderRefs benchmarks header reference extraction
- **BenchmarkFunc_RequireFunc**: BenchmarkFunc_RequireFunc benchmarks require function
- **BenchmarkFunc_GetOrFunc**: BenchmarkFunc_GetOrFunc benchmarks getOr function
- **BenchmarkFunc_HasFunc**: BenchmarkFunc_HasFunc benchmarks has function
- **BenchmarkFunc_JSONFunc**: BenchmarkFunc_JSONFunc benchmarks JSON serialization
- **BenchmarkFunc_CoalesceFunc**: BenchmarkFunc_CoalesceFunc benchmarks coalesce function
- **BenchmarkEngine_Concurrent_SameTemplate**: BenchmarkEngine_Concurrent_SameTemplate benchmarks concurrent access to same template
- **BenchmarkEngine_Concurrent_DifferentTemplates**: BenchmarkEngine_Concurrent_DifferentTemplates benchmarks concurrent access to different templates
- **BenchmarkContextBuilder_Concurrent**: BenchmarkContextBuilder_Concurrent benchmarks concurrent context building

### context_test.go

- **TestNewContextBuilder**: TestNewContextBuilder tests builder creation
- **TestContextBuilder_Build**: TestContextBuilder_Build tests context creation from HTTP request
- **TestContextBuilder_Build_NilParams**: TestContextBuilder_Build_NilParams tests build with nil params
- **TestContextBuilder_ResolveClientIP_NoProxy**: TestContextBuilder_ResolveClientIP_NoProxy tests IP resolution without proxy headers
- **TestContextBuilder_ResolveClientIP_WithProxy**: TestContextBuilder_ResolveClientIP_WithProxy tests IP resolution with proxy headers
- **TestContextBuilder_GetRequestID**: TestContextBuilder_GetRequestID tests request ID extraction
- **TestContext_WithResult**: TestContext_WithResult tests adding result to context
- **TestContext_ToMap**: TestContext_ToMap tests context conversion to map
- **TestExtractParamRefs**: TestExtractParamRefs tests param reference extraction
- **TestExtractHeaderRefs**: TestExtractHeaderRefs tests header reference extraction
- **TestExtractQueryRefs**: TestExtractQueryRefs tests query reference extraction
- **TestContext_Integration**: TestContext_Integration tests full context usage with engine
- **TestContext_PostQuery_Integration**: TestContext_PostQuery_Integration tests post-query context with webhooks
- **BenchmarkContextBuilder_Build**: BenchmarkContextBuilder_Build benchmarks context creation
- **BenchmarkExtractParamRefs**: BenchmarkExtractParamRefs benchmarks param extraction

### engine_test.go

- **TestNew**: TestNew verifies engine creation with all functions
- **TestRequireFunc**: TestRequireFunc tests the require helper function
- **TestGetOrFunc**: TestGetOrFunc tests the getOr helper function
- **TestHasFunc**: TestHasFunc tests the has helper function
- **TestJSONFunc**: TestJSONFunc tests JSON serialization
- **TestJSONIndentFunc**: TestJSONIndentFunc tests indented JSON serialization
- **TestDefaultFunc**: TestDefaultFunc tests the default helper function
- **TestCoalesceFunc**: TestCoalesceFunc tests the coalesce function
- **TestEngine_Register**: TestEngine_Register tests template registration
- **TestEngine_Execute**: TestEngine_Execute tests template execution
- **TestEngine_Execute_NotRegistered**: TestEngine_Execute_NotRegistered tests executing unregistered template
- **TestEngine_Execute_EmptyResult**: TestEngine_Execute_EmptyResult tests that empty results are rejected
- **TestEngine_ExecuteInline**: TestEngine_ExecuteInline tests inline template execution
- **TestEngine_Validate**: TestEngine_Validate tests template validation
- **TestEngine_ValidateWithParams**: TestEngine_ValidateWithParams tests template validation with param checking
- **TestEngine_MathFunctions**: TestEngine_MathFunctions tests math helper functions in templates
- **TestEngine_StringFunctions**: TestEngine_StringFunctions tests string helper functions in templates
- **TestEngine_ContextFunctions**: TestEngine_ContextFunctions tests context-based helper functions
- **TestEngine_RequireFuncError**: TestEngine_RequireFuncError tests require function error case
- **TestEngine_PostQueryContext**: TestEngine_PostQueryContext tests post-query context with Result
- **TestEngine_ConcurrentAccess**: TestEngine_ConcurrentAccess tests thread safety
- **TestSampleContextMap**: TestSampleContextMap tests sample context generation
- **TestJSONFunc_Error**: TestJSONFunc_Error tests JSON serialization error handling
- **TestJSONIndentFunc_Error**: TestJSONIndentFunc_Error tests indented JSON serialization error handling
- **TestEngine_ExecuteInline_EmptyResult**: TestEngine_ExecuteInline_EmptyResult tests that empty results are rejected
- **TestEngine_Execute_TemplateError**: TestEngine_Execute_TemplateError tests template execution error handling
- **TestEngine_Validate_StructuralError**: TestEngine_Validate_StructuralError tests validation with structural template errors


---

## Rate Limiting

**Package**: `internal/ratelimit`

### ratelimit_test.go

- **TestNew**: New
- **TestAllow_NoLimits**: Allow NoLimits
- **TestAllow_NamedPool**: Allow NamedPool
- **TestAllow_InlineConfig**: Allow InlineConfig
- **TestAllow_MultiplePools**: Allow MultiplePools
- **TestAllow_DifferentClients**: Allow DifferentClients
- **TestAllow_HeaderBasedKey**: Allow HeaderBasedKey
- **TestAllow_MissingTemplateData**: Allow MissingTemplateData
- **TestAllow_NonexistentPool**: Allow NonexistentPool
- **TestMetrics**: Metrics
- **TestMetrics_InlinePool**: TestMetrics_InlinePool tests that inline rate limits also track metrics
- **TestPoolNames**: PoolNames
- **TestGetPool**: GetPool
- **TestBucketCleanup**: BucketCleanup


---

## End-to-End

**Package**: `e2e`

### e2e_test.go

- **TestE2E_ServerStartupAndShutdown**: TestE2E_ServerStartupAndShutdown tests the server starts and stops cleanly
- **TestE2E_HealthEndpoint**: TestE2E_HealthEndpoint tests /health returns database status
- **TestE2E_MetricsEndpoint**: TestE2E_MetricsEndpoint tests /_/metrics returns runtime stats
- **TestE2E_OpenAPIEndpoint**: TestE2E_OpenAPIEndpoint tests /_/openapi.json returns valid spec
- **TestE2E_RootEndpoint**: TestE2E_RootEndpoint tests / returns endpoint listing
- **TestE2E_QueryEndpoint**: TestE2E_QueryEndpoint tests query execution returns data
- **TestE2E_QueryWithParameters**: TestE2E_QueryWithParameters tests parameterized query execution
- **TestE2E_LogLevelEndpoint**: TestE2E_LogLevelEndpoint tests runtime log level changes
- **TestE2E_GzipCompression**: TestE2E_GzipCompression tests response compression
- **TestE2E_RequestID**: TestE2E_RequestID tests request ID propagation
- **TestE2E_NotFound**: TestE2E_NotFound tests 404 for unknown paths
- **TestE2E_GracefulShutdown**: TestE2E_GracefulShutdown tests server handles SIGTERM gracefully
- **TestE2E_ConfigValidation**: TestE2E_ConfigValidation tests -validate flag
- **TestE2E_InvalidConfig**: TestE2E_InvalidConfig tests server rejects invalid config


---

## Running Tests

```bash
# Run all tests
make test

# Run by test type
make test-unit         # Unit tests (internal packages)
make test-integration  # Integration tests (httptest-based)
make test-e2e          # End-to-end tests (starts actual binary)

# Run by package
make test-db
make test-handler
make test-tmpl
make test-ratelimit
# etc.

# Run with coverage
make test-cover
make test-cover-html

# Run benchmarks
make test-bench
```

## Test Organization

| Type | Location | Description |
|------|----------|-------------|
| Unit tests | `internal/*/` | Test individual functions and methods |
| Integration tests | `internal/server/` | Test component interactions via `httptest` |
| End-to-end tests | `e2e/` | Start binary, make real HTTP requests |
| Benchmarks | `internal/*/benchmark_test.go` | Performance tests |

All unit and integration tests use SQLite in-memory databases to avoid external dependencies.
