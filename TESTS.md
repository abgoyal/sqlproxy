# Test Documentation

This document is auto-generated from test source files. Run `make test-docs` to regenerate.

## Coverage Summary

Run `make test-cover` for current coverage statistics.


---

## Config

**Package**: `internal/config`

### config_test.go

- **TestLoad_ValidConfig**: TestLoad_ValidConfig verifies a complete valid YAML config loads with all fields correctly populated
- **TestLoad_EnvironmentVariables**: TestLoad_EnvironmentVariables verifies ${VAR} in variables.values is expanded from environment.
- **TestLoad_MissingServerHost**: TestLoad_MissingServerHost ensures config loading fails when server.host is omitted
- **TestLoad_InvalidPort**: TestLoad_InvalidPort validates server.port must be in range 1-65535
- **TestLoad_InvalidTimeout**: TestLoad_InvalidTimeout checks timeout validation: positive values, max >= default
- **TestLoad_NoDatabases**: TestLoad_NoDatabases ensures at least one database connection is required
- **TestLoad_DuplicateDatabaseNames**: TestLoad_DuplicateDatabaseNames ensures database names must be unique across connections
- **TestLoad_InvalidDatabaseType**: TestLoad_InvalidDatabaseType rejects unsupported database types like mysql
- **TestLoad_SQLiteMissingPath**: TestLoad_SQLiteMissingPath ensures SQLite databases require a path field
- **TestLoad_SQLServerMissingFields**: TestLoad_SQLServerMissingFields validates SQL Server requires host, port, user, password, database
- **TestLoad_InvalidLogLevel**: TestLoad_InvalidLogLevel rejects log levels other than debug/info/warn/error
- **TestLoad_InvalidIsolationLevel**: TestLoad_InvalidIsolationLevel rejects invalid SQL Server isolation level names
- **TestDatabaseConfig_IsReadOnly**: TestDatabaseConfig_IsReadOnly verifies readonly defaults to true when nil
- **TestDatabaseConfig_DefaultSessionConfig**: TestDatabaseConfig_DefaultSessionConfig checks implicit defaults based on readonly flag
- **TestValidIsolationLevels**: TestValidIsolationLevels checks the ValidIsolationLevels map contains correct entries
- **TestValidDeadlockPriorities**: TestValidDeadlockPriorities checks the ValidDeadlockPriorities map for low/normal/high
- **TestValidJournalModes**: TestValidJournalModes checks ValidJournalModes for SQLite: wal/delete/truncate/memory/off
- **TestValidDatabaseTypes**: TestValidDatabaseTypes checks ValidDatabaseTypes contains sqlserver and sqlite only
- **TestLoad_VariablesSection**: TestLoad_VariablesSection verifies the variables section with values
- **TestLoad_VariablesDefaultValues**: TestLoad_VariablesDefaultValues verifies ${VAR:default} syntax works correctly
- **TestLoad_VariablesEnvFileSupport**: TestLoad_VariablesEnvFileSupport verifies loading variables from env file
- **TestLoad_VariablesEnvOverridesFile**: TestLoad_VariablesEnvOverridesFile verifies actual env vars override env file values
- **TestLoad_UndefinedVariable**: TestLoad_UndefinedVariable verifies that referencing an undefined variable in templates causes an error
- **TestLoad_UndefinedVariableInNumericField**: TestLoad_UndefinedVariableInNumericField verifies undefined variable error in pre-rendered numeric fields
- **TestIsArrayType**: TestIsArrayType verifies IsArrayType correctly identifies array types
- **TestArrayBaseType**: TestArrayBaseType verifies ArrayBaseType extracts the base type from array types
- **TestValidParameterTypes**: TestValidParameterTypes verifies all expected parameter types are in ValidParameterTypes
- **TestRateLimitConfig_IsPoolReference**: TestRateLimitConfig_IsPoolReference verifies IsPoolReference returns true only when Pool is set
- **TestRateLimitConfig_IsInline**: TestRateLimitConfig_IsInline verifies IsInline returns true only when both RequestsPerSecond and Burst are positive


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
- **TestNewDriver_EmptyTypeReturnsError**: TestNewDriver_EmptyTypeReturnsError ensures empty type is rejected
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
- **TestIsWriteQuery**: TestIsWriteQuery tests the SQL statement type detection
- **TestSQLiteDriver_WriteOperations_RowsAffected**: TestSQLiteDriver_WriteOperations_RowsAffected tests that write operations return correct rows affected

### sqlserver_test.go

- **TestIsolationToSQL**: TestIsolationToSQL tests conversion of config isolation strings to SQL Server syntax
- **TestDeadlockPriorityToSQL**: TestDeadlockPriorityToSQL tests conversion of config deadlock priority strings to SQL Server syntax
- **TestSQLServerDriver_BuildArgs**: TestSQLServerDriver_BuildArgs verifies parameter extraction from SQL
- **TestSQLServerDriver_BuildArgs_Values**: TestSQLServerDriver_BuildArgs_Values verifies parameter values are correctly assigned
- **TestSQLServerDriver_BuildArgs_NilValue**: TestSQLServerDriver_BuildArgs_NilValue verifies nil values are handled correctly


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
- **TestValidateDebug**: TestValidateDebug tests debug config validation rules
- **TestRun_ValidConfig**: TestRun_ValidConfig tests complete valid configuration passes all checks
- **TestRun_InvalidConfig**: TestRun_InvalidConfig tests configuration with invalid port fails validation
- **TestRun_DBConnectionTest**: TestRun_DBConnectionTest verifies SQLite :memory: connection succeeds
- **TestRun_DBConnectionFail**: TestRun_DBConnectionFail verifies invalid SQLite path fails connection test
- **TestRun_SQLServerUnresolvedEnvVar**: TestRun_SQLServerUnresolvedEnvVar tests that SQL Server with unresolved env vars is skipped during connection test
- **TestRun_SQLServerUnresolvedPassword**: TestRun_SQLServerUnresolvedPassword tests SQL Server with unresolved password env var is skipped
- **TestValidateServerCache**: TestValidateServerCache tests server-level cache configuration validation
- **TestValidateRateLimits**: TestValidateRateLimits tests server-level rate limit pool validation
- **TestRun_NoWorkflowsWarning**: TestRun_NoWorkflowsWarning tests that empty workflows list generates a warning
- **TestValidatePublicIDs**: TestValidatePublicIDs tests public ID configuration validation
- **TestValidatePublicIDFunctionUsageWithoutConfig**: ValidatePublicIDFunctionUsageWithoutConfig


---

## Server

**Package**: `internal/server`

### server_test.go

- **TestServer_New**: TestServer_New verifies server initialization creates dbManager and httpServer
- **TestServer_HealthHandler**: TestServer_HealthHandler tests /health returns status and database connections
- **TestServer_MetricsHandler_Disabled**: TestServer_MetricsHandler_Disabled tests /_/metrics.json returns not-enabled message when disabled
- **TestServer_MetricsJSONHandler_Enabled**: TestServer_MetricsJSONHandler_Enabled tests /_/metrics.json returns valid JSON metrics
- **TestServer_MetricsPrometheusHandler_Enabled**: TestServer_MetricsPrometheusHandler_Enabled tests /_/metrics returns Prometheus format
- **TestServer_MetricsPrometheusHandler_Disabled**: TestServer_MetricsPrometheusHandler_Disabled tests /_/metrics returns error when disabled
- **TestServer_LogLevelHandler**: TestServer_LogLevelHandler tests log level GET retrieval and POST update operations
- **TestServer_ListEndpointsHandler**: TestServer_ListEndpointsHandler tests root path returns service info and workflow listing
- **TestServer_ListEndpointsHandler_NotFound**: TestServer_ListEndpointsHandler_NotFound tests unknown paths return 404
- **TestServer_OpenAPIHandler**: TestServer_OpenAPIHandler tests /openapi.json returns valid spec with CORS headers
- **TestServer_RecoveryMiddleware**: TestServer_RecoveryMiddleware tests panic recovery returns 500 without server crash
- **TestServer_GzipMiddleware**: TestServer_GzipMiddleware tests gzip compression when Accept-Encoding header set
- **TestServer_GzipMiddleware_NoGzip**: TestServer_GzipMiddleware_NoGzip tests no compression without Accept-Encoding header
- **TestServer_StartShutdown**: TestServer_StartShutdown tests server start and graceful shutdown sequence
- **TestServer_Integration_WorkflowEndpoint**: TestServer_Integration_WorkflowEndpoint tests workflow execution via httptest server
- **TestServer_Integration_ParameterizedWorkflow**: TestServer_Integration_ParameterizedWorkflow tests parameterized workflow with required and optional params
- **TestServer_Integration_WithGzip**: TestServer_Integration_WithGzip tests HTTP request/response cycle with gzip encoding
- **TestServer_HealthHandler_Degraded**: TestServer_HealthHandler_Degraded tests /health returns degraded status when database is unreachable
- **TestServer_HealthHandler_DatabaseDown**: TestServer_HealthHandler_DatabaseDown tests /_/health shows database as disconnected when ping fails
- **TestServer_HealthHandler_MultipleDatabases**: TestServer_HealthHandler_MultipleDatabases tests /_/health with multiple database connections
- **TestServer_DBHealthHandler**: TestServer_DBHealthHandler tests /_/health/{dbname} endpoint
- **TestServer_DBHealthHandler_Disconnected**: TestServer_DBHealthHandler_Disconnected tests /_/health/{dbname} when db is down
- **TestServer_CacheClearHandler**: TestServer_CacheClearHandler tests /_/cache/clear endpoint
- **TestServer_CacheClearHandler_NoCacheConfigured**: TestServer_CacheClearHandler_NoCacheConfigured tests cache clear when cache disabled
- **TestServer_RateLimitsHandler**: TestServer_RateLimitsHandler tests the /_/ratelimits endpoint
- **TestServer_RateLimitsHandler_NotConfigured**: TestServer_RateLimitsHandler_NotConfigured tests the endpoint when rate limiting is disabled
- **TestServer_RateLimitsResetHandler**: TestServer_RateLimitsResetHandler tests the /_/ratelimits/reset endpoint
- **TestServer_RateLimitResponse**: TestServer_RateLimitResponse tests that 429 response includes retry_after_sec
- **TestServer_CronWorkflowSetup**: TestServer_CronWorkflowSetup verifies cron workflow jobs are registered correctly
- **TestServer_CronWorkflowExecution**: TestServer_CronWorkflowExecution verifies cron workflow execution path works
- **TestServer_NoCronWorkflow**: TestServer_NoCronWorkflow verifies server works without cron triggers


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
- **TestSpec_WorkflowEndpoints**: TestSpec_WorkflowEndpoints tests workflow config generates correct path operations
- **TestSpec_SkipsCronOnlyWorkflows**: TestSpec_SkipsCronOnlyWorkflows verifies cron-only workflows are excluded from paths
- **TestBuildWorkflowPath_GET**: TestBuildWorkflowPath_GET tests GET path generation with parameters and tags
- **TestBuildWorkflowPath_POST**: TestBuildWorkflowPath_POST tests POST method creates post operation, not get
- **TestBuildWorkflowPath_Responses**: TestBuildWorkflowPath_Responses verifies 200, 400, 500, 504 response codes present
- **TestBuildParamDescription**: TestBuildParamDescription tests parameter description includes type and default
- **TestParamTypeToSchema**: TestParamTypeToSchema tests parameter type to JSON Schema conversion
- **TestBuildComponents**: TestBuildComponents verifies required schema definitions are present
- **TestSpec_ValidJSON**: TestSpec_ValidJSON verifies spec serializes to valid JSON and back
- **TestSpec_TimeoutParameter**: TestSpec_TimeoutParameter tests _timeout param has correct default and maximum
- **TestSpec_WorkflowDescription**: TestSpec_WorkflowDescription tests custom timeout info in spec
- **TestParamTypeToSchema_ArrayTypes**: TestParamTypeToSchema_ArrayTypes tests array type schema generation
- **TestParamTypeToSchema_JSONType**: TestParamTypeToSchema_JSONType tests json type schema generation
- **TestBuildWorkflowPath_DefaultTimeout**: TestBuildWorkflowPath_DefaultTimeout tests server default timeout used when workflow has none


---

## Service

**Package**: `internal/service`

### service_test.go

- **TestDefaultServiceName**: TestDefaultServiceName verifies the default service name constant
- **TestJoinErrors**: TestJoinErrors verifies error joining function


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

- **BenchmarkEngine_CacheKey_Simple**: BenchmarkEngine_CacheKey_Simple benchmarks simple cache key like "items:{{.trigger.params.status}}"
- **BenchmarkEngine_CacheKey_MultiParam**: BenchmarkEngine_CacheKey_MultiParam benchmarks cache key with multiple params
- **BenchmarkEngine_CacheKey_WithDefault**: BenchmarkEngine_CacheKey_WithDefault benchmarks cache key with default fallback
- **BenchmarkEngine_RateLimit_ClientIP**: BenchmarkEngine_RateLimit_ClientIP benchmarks simple rate limit key
- **BenchmarkEngine_RateLimit_Composite**: BenchmarkEngine_RateLimit_Composite benchmarks composite rate limit key
- **BenchmarkEngine_RateLimit_HeaderRequired**: BenchmarkEngine_RateLimit_HeaderRequired benchmarks rate limit with required header
- **BenchmarkEngine_ExecuteInline**: BenchmarkEngine_ExecuteInline benchmarks inline (non-cached) template execution
- **BenchmarkEngine_Register**: BenchmarkEngine_Register benchmarks template registration/compilation
- **BenchmarkEngine_Validate**: BenchmarkEngine_Validate benchmarks template validation
- **BenchmarkEngine_ValidateWithParams**: BenchmarkEngine_ValidateWithParams benchmarks template validation with param checking
- **BenchmarkContextBuilder_Simple**: BenchmarkContextBuilder_Simple benchmarks basic context building
- **BenchmarkContextBuilder_WithHeaders**: BenchmarkContextBuilder_WithHeaders benchmarks context with many headers
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
- **TestContext_ToMap**: TestContext_ToMap tests context conversion to map
- **TestExtractParamRefs**: TestExtractParamRefs tests param reference extraction
- **TestExtractHeaderRefs**: TestExtractHeaderRefs tests header reference extraction
- **TestExtractQueryRefs**: TestExtractQueryRefs tests query reference extraction
- **TestContext_Integration**: TestContext_Integration tests full context usage with engine
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
- **TestDefaultFuncInTemplates**: TestDefaultFuncInTemplates tests both direct and piped forms of default
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
- **TestEngine_ConcurrentAccess**: TestEngine_ConcurrentAccess tests thread safety
- **TestSampleContextMap**: TestSampleContextMap tests sample context generation
- **TestJSONFunc_Error**: TestJSONFunc_Error tests JSON serialization error handling
- **TestJSONIndentFunc_Error**: TestJSONIndentFunc_Error tests indented JSON serialization error handling
- **TestEngine_ExecuteInline_EmptyResult**: TestEngine_ExecuteInline_EmptyResult tests that empty results are rejected
- **TestEngine_Execute_TemplateError**: TestEngine_Execute_TemplateError tests template execution error handling
- **TestEngine_Validate_StructuralError**: TestEngine_Validate_StructuralError tests validation with structural template errors
- **TestToNumber**: TestToNumber tests the numeric type conversion helper
- **TestEngine_MathFunctions_Float**: TestEngine_MathFunctions_Float tests math functions with float values.
- **TestEngine_MathFunctions_Extended**: TestEngine_MathFunctions_Extended tests extended math functions
- **TestEngine_NumericFormatFunctions**: TestEngine_NumericFormatFunctions tests numeric formatting functions
- **TestHeaderFunc**: TestHeaderFunc tests header access with canonical form handling
- **TestCookieFunc**: TestCookieFunc tests cookie access with default value
- **TestArrayHelpers**: TestArrayHelpers tests first, last, len, pluck, isEmpty functions
- **TestTypeConversions**: TestTypeConversions tests float, string, bool functions
- **TestEngine_NewFunctionsInTemplates**: TestEngine_NewFunctionsInTemplates tests new functions work in templates
- **TestIPNetworkFunc**: TestIPNetworkFunc tests the ipNetwork template function
- **TestIPPrefixFunc**: TestIPPrefixFunc tests the ipPrefix template function
- **TestNormalizeIPFunc**: TestNormalizeIPFunc tests the normalizeIP template function
- **TestIPFunctionsInTemplates**: TestIPFunctionsInTemplates tests that IP functions work in actual templates
- **TestUUIDFunc**: TestUUIDFunc tests UUID generation through template engine
- **TestUUIDShortFunc**: TestUUIDShortFunc tests UUID without hyphens
- **TestShortIDFunc**: TestShortIDFunc tests short ID generation
- **TestNanoidFunc**: TestNanoidFunc tests NanoID generation
- **TestIDFunctionsInTemplates**: TestIDFunctionsInTemplates tests UUID/ID functions in actual templates
- **TestPublicIDFunc**: TestPublicIDFunc tests the publicID template function
- **TestPrivateIDFunc**: TestPrivateIDFunc tests the privateID template function
- **TestPublicPrivateIDRoundTrip**: TestPublicPrivateIDRoundTrip tests encoding and decoding produces original value
- **TestPublicIDInTemplates**: TestPublicIDInTemplates tests publicID/privateID functions in templates
- **TestValidationHelpers**: ValidationHelpers
- **TestEncodingHashingFuncs**: EncodingHashingFuncs
- **TestStringHelpers**: StringHelpers
- **TestDateTimeFuncs**: DateTimeFuncs
- **TestJSONHelpers**: JSONHelpers
- **TestConditionalHelpers**: ConditionalHelpers
- **TestDigFunc**: DigFunc
- **TestDebugHelpers**: DebugHelpers
- **TestNumericFormatting**: NumericFormatting
- **TestParseTimeFunc**: ParseTimeFunc
- **TestMergeFunc**: MergeFunc
- **TestValuesFunc**: ValuesFunc
- **TestFormatTimeEdgeCases**: FormatTimeEdgeCases
- **TestDigFuncEdgeCases**: DigFuncEdgeCases
- **TestTypeOfNil**: TypeOfNil
- **TestUrlDecodeError**: UrlDecodeError
- **TestBase64DecodeError**: Base64DecodeError
- **TestMatchesInvalidRegex**: MatchesInvalidRegex
- **TestSubstrEdgeCases**: SubstrEdgeCases
- **TestTruncateEdgeCases**: TruncateEdgeCases
- **TestJoinNonSlice**: JoinNonSlice
- **TestKeysNonMap**: KeysNonMap
- **TestToIntConversions**: ToIntConversions
- **TestAndFunc**: AndFunc
- **TestOrFunc**: OrFunc
- **TestBooleanOperatorsInTemplates**: BooleanOperatorsInTemplates
- **TestIsEmailFunc**: IsEmailFunc
- **TestIsUUIDFunc**: IsUUIDFunc
- **TestIsURLFunc**: IsURLFunc
- **TestIsIPFunc**: IsIPFunc
- **TestIsIPv4Func**: IsIPv4Func
- **TestIsIPv6Func**: IsIPv6Func
- **TestIsNumericFunc**: IsNumericFunc
- **TestMatchesFunc**: MatchesFunc
- **TestUrlEncodeFunc**: UrlEncodeFunc
- **TestUrlDecodeFunc**: UrlDecodeFunc
- **TestUrlDecodeOrFunc**: UrlDecodeOrFunc
- **TestBase64EncodeFunc**: Base64EncodeFunc
- **TestBase64DecodeFunc**: Base64DecodeFunc
- **TestBase64DecodeOrFunc**: Base64DecodeOrFunc
- **TestSHA256Func**: SHA256Func
- **TestMD5Func**: MD5Func
- **TestHmacSHA256Func**: HmacSHA256Func
- **TestIPNetworkFuncEdgeCases**: IPNetworkFuncEdgeCases
- **TestIPPrefixFuncEdgeCases**: IPPrefixFuncEdgeCases
- **TestShortIDFuncCharacterSet**: ShortIDFuncCharacterSet
- **TestNanoidFuncCharacterSet**: NanoidFuncCharacterSet
- **TestNanoidFuncEdgeCases**: NanoidFuncEdgeCases
- **TestExprFuncs**: TestExprFuncs verifies ExprFuncs returns all expected functions
- **TestExprFuncs_DivOr**: TestExprFuncs_DivOr tests the divOr function from ExprFuncs
- **TestExprFuncs_ModOr**: TestExprFuncs_ModOr tests the modOr function from ExprFuncs


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
- **TestReset**: Reset


---

## Types

**Package**: `internal/types`

### params_test.go

- **TestIsArrayType**: IsArrayType
- **TestArrayBaseType**: ArrayBaseType
- **TestConvertValue**: ConvertValue
- **TestConvertJSONValue**: ConvertJSONValue
- **TestValidateArrayElements**: ValidateArrayElements
- **TestValidParamTypes**: ValidParamTypes


---

## Workflow

**Package**: `internal/workflow`

### compile_test.go

- **TestCompile_BasicWorkflow**: Compile BasicWorkflow
- **TestCompile_ConditionAliases**: Compile ConditionAliases
- **TestCompile_HTTPCallStep**: Compile HTTPCallStep
- **TestCompile_BlockWithIteration**: Compile BlockWithIteration
- **TestCompile_CacheKeyTemplate**: Compile CacheKeyTemplate
- **TestCompile_InvalidTemplateSyntax**: Compile InvalidTemplateSyntax
- **TestAliasExpansion**: TestAliasExpansion tests alias expansion via AST patching.
- **TestAliasChaining**: TestAliasChaining tests that aliases can reference other aliases.
- **TestCircularDependencyDetection**: TestCircularDependencyDetection verifies that circular alias dependencies are detected.
- **TestAliasInStringLiteral**: TestAliasInStringLiteral verifies aliases are NOT expanded inside string literals.
- **TestAliasNotMatchPropertyPath**: TestAliasNotMatchPropertyPath verifies aliases don't match property paths.
- **TestEmptyAliases**: TestEmptyAliases verifies compilation works with no aliases.
- **TestEvalCondition**: EvalCondition
- **TestEvalExpression**: EvalExpression
- **TestTemplateFuncs**: TestTemplateFuncs tests all template functions available in workflow templates.
- **TestTemplateFuncs_InWorkflowContext**: TestTemplateFuncs_InWorkflowContext tests template functions with realistic workflow data.
- **TestExprFunc_isValidPublicID**: TestExprFunc_isValidPublicID tests the isValidPublicID expr function.
- **TestExprFuncs_InConditions**: TestExprFuncs_InConditions tests that common functions from tmpl.ExprFuncs
- **TestValidateDivisions**: TestValidateDivisions tests static validation of division operations
- **TestExtractStepRefs**: TestExtractStepRefs tests step reference extraction from expressions

### config_test.go

- **TestStepConfig_StepType**: StepConfig StepType
- **TestStepConfig_IsBlock**: StepConfig IsBlock
- **TestStepConfig_IsQuery**: StepConfig IsQuery
- **TestStepConfig_IsHTTPCall**: StepConfig IsHTTPCall
- **TestStepConfig_IsResponse**: StepConfig IsResponse
- **TestRateLimitRefConfig**: RateLimitRefConfig

### context_test.go

- **TestNewContext**: NewContext
- **TestContext_Context**: Context Context
- **TestContext_SetGetStepResult**: Context SetGetStepResult
- **TestContext_BuildExprEnv_HTTPTrigger**: Context BuildExprEnv HTTPTrigger
- **TestContext_BuildExprEnv_CronTrigger**: Context BuildExprEnv CronTrigger
- **TestContext_BuildExprEnv_WithVariables**: Context BuildExprEnv WithVariables
- **TestContext_BuildExprEnv_NilVariables**: Context BuildExprEnv NilVariables
- **TestContext_BuildExprEnv_VarsInExpr**: Context BuildExprEnv VarsInExpr
- **TestContext_BuildTemplateData**: Context BuildTemplateData
- **TestStepResultToMap**: StepResultToMap
- **TestHeaderToMap**: HeaderToMap
- **TestBlockContext**: BlockContext
- **TestBlockContext_SetGetStepResult**: BlockContext SetGetStepResult
- **TestBlockContext_BuildExprEnv**: BlockContext BuildExprEnv
- **TestBlockContext_BuildTemplateData**: BlockContext BuildTemplateData
- **TestContext_BuildExprEnv_ContainsExprFuncs**: Context BuildExprEnv ContainsExprFuncs
- **TestContext_BuildExprEnv_CookieAccess**: Context BuildExprEnv CookieAccess
- **TestContext_BuildExprEnv_CookiesInExpr**: Context BuildExprEnv CookiesInExpr

### executor_test.go

- **TestNewExecutor**: NewExecutor
- **TestExecutor_Execute_SimpleQuery**: Executor Execute SimpleQuery
- **TestExecutor_Execute_DisabledStep**: Executor Execute DisabledStep
- **TestExecutor_Execute_ConditionalStep**: Executor Execute ConditionalStep
- **TestExecutor_Execute_StepFailure_Abort**: Executor Execute StepFailure Abort
- **TestExecutor_Execute_StepFailure_Continue**: Executor Execute StepFailure Continue
- **TestExecutor_Execute_ResponseStep**: Executor Execute ResponseStep
- **TestExecutor_Execute_HTTPCallStep**: Executor Execute HTTPCallStep
- **TestExecutor_Execute_ContextCancellation**: Executor Execute ContextCancellation
- **TestExecutor_Execute_WorkflowTimeout**: Executor Execute WorkflowTimeout
- **TestExecutor_Execute_HTTPTriggerWithoutResponse**: Executor Execute HTTPTriggerWithoutResponse
- **TestExecutor_Execute_UnknownStepType**: Executor Execute UnknownStepType
- **TestExecutor_Execute_BlockStep**: Executor Execute BlockStep
- **TestExecutor_Execute_BlockStep_IterationError_Abort**: Executor Execute BlockStep IterationError Abort
- **TestExecutor_Execute_BlockStep_WithoutIteration**: Executor Execute BlockStep WithoutIteration
- **TestExecutor_Execute_StepNames_Auto**: Executor Execute StepNames Auto
- **TestExecutor_Execute_LoggingCalls**: Executor Execute LoggingCalls
- **TestExecutor_StepCache_Hit**: Executor StepCache Hit
- **TestExecutor_StepCache_Miss**: Executor StepCache Miss
- **TestExecutor_StepCache_NilCache**: Executor StepCache NilCache
- **TestExecutor_ConditionalResponse_NegatedAlias**: TestExecutor_ConditionalResponse_NegatedAlias tests that negated condition aliases work correctly
- **TestExecutor_ConditionalResponse_FromConfig**: TestExecutor_ConditionalResponse_FromConfig tests conditional responses compiled from config (like E2E).
- **TestEvaluateStepParams_Integer**: EvaluateStepParams Integer
- **TestEvaluateStepParams_String**: EvaluateStepParams String
- **TestEvaluateStepParams_TemplateError**: EvaluateStepParams TemplateError
- **TestEvaluateStepParams_MultipleParams**: EvaluateStepParams MultipleParams
- **TestEvaluateStepParams_UsesExistingParamsMap**: EvaluateStepParams UsesExistingParamsMap
- **TestEvaluateStepParams_TemplateWithData**: EvaluateStepParams TemplateWithData
- **TestEvaluateStepParams_EmptyParams**: EvaluateStepParams EmptyParams
- **TestCompileAndEvaluate_ConditionAliases**: TestCompileAndEvaluate_ConditionAliases tests that condition aliases and negated aliases are properly compiled and evaluated.
- **TestParseInt64**: ParseInt64

### handler_test.go

- **TestNewHTTPHandler**: NewHTTPHandler
- **TestHTTPHandler_ServeHTTP_MethodNotAllowed**: HTTPHandler ServeHTTP MethodNotAllowed
- **TestHTTPHandler_ServeHTTP_Success**: HTTPHandler ServeHTTP Success
- **TestHTTPHandler_ServeHTTP_VersionWithBuildTime**: HTTPHandler ServeHTTP VersionWithBuildTime
- **TestHTTPHandler_ServeHTTP_RequestID_FromHeader**: HTTPHandler ServeHTTP RequestID FromHeader
- **TestHTTPHandler_ServeHTTP_CorrelationID**: HTTPHandler ServeHTTP CorrelationID
- **TestHTTPHandler_ParseParameters_QueryString**: HTTPHandler ParseParameters QueryString
- **TestHTTPHandler_ParseParameters_MissingRequired**: HTTPHandler ParseParameters MissingRequired
- **TestHTTPHandler_ParseParameters_JSONBody**: HTTPHandler ParseParameters JSONBody
- **TestHTTPHandler_ParseParameters_InvalidJSON**: HTTPHandler ParseParameters InvalidJSON
- **TestHTTPHandler_ParseParameters_TypeConversion**: HTTPHandler ParseParameters TypeConversion
- **TestHTTPHandler_WorkflowError_DefaultResponse**: HTTPHandler WorkflowError DefaultResponse
- **TestHTTPHandler_NoResponse_EmptySuccess**: HTTPHandler NoResponse EmptySuccess
- **TestGetOrGenerateRequestID**: GetOrGenerateRequestID
- **TestGenerateRequestID**: GenerateRequestID
- **TestSanitizeHeaderValue**: SanitizeHeaderValue
- **TestResolveClientIP**: ResolveClientIP
- **TestDBManagerAdapter**: DBManagerAdapter
- **TestLoggerAdapter**: LoggerAdapter
- **TestHTTPHandler_ParseParameters_NestedObject**: HTTPHandler ParseParameters NestedObject
- **TestHTTPHandler_ParseParameters_JSONType**: HTTPHandler ParseParameters JSONType
- **TestHTTPHandler_ParseParameters_ArrayType**: HTTPHandler ParseParameters ArrayType
- **TestHTTPHandler_TriggerCache_Hit**: HTTPHandler TriggerCache Hit
- **TestHTTPHandler_TriggerCache_Miss**: HTTPHandler TriggerCache Miss
- **TestHTTPHandler_TriggerCache_NilCache**: HTTPHandler TriggerCache NilCache
- **TestFlattenHeaders**: FlattenHeaders
- **TestFlattenQuery**: FlattenQuery
- **TestEvaluateCacheKey_ExpandedContext**: EvaluateCacheKey ExpandedContext
- **TestParseCookies**: ParseCookies

### step_test.go

- **TestQueryStep**: TestQueryStep tests the QueryStep implementation.
- **TestExtractSQLParams**: TestExtractSQLParams tests the extractSQLParams function.
- **TestHTTPCallStep**: TestHTTPCallStep tests the HTTPCallStep implementation.
- **TestNormalizeJSONResponse**: TestNormalizeJSONResponse tests the normalizeJSONResponse function.
- **TestResponseStep**: TestResponseStep tests the ResponseStep implementation.
- **TestResponseStepWriteError**: TestResponseStepWriteError tests the ResponseStep with a failing ResponseWriter.
- **TestHTTPCallRetry**: TestHTTPCallRetry tests the retry logic in HTTPCallStep.
- **TestHTTPCallWithBody**: TestHTTPCallWithBody tests POST requests with body.
- **TestHTTPCallWithHeaders**: TestHTTPCallWithHeaders tests requests with custom headers.

### validate_test.go

- **TestValidate_BasicWorkflow**: Validate BasicWorkflow
- **TestValidate_MissingName**: Validate MissingName
- **TestValidate_MissingTriggers**: Validate MissingTriggers
- **TestValidate_MissingSteps**: Validate MissingSteps
- **TestValidate_HTTPTrigger**: Validate HTTPTrigger
- **TestValidate_CronTrigger**: Validate CronTrigger
- **TestValidate_QueryStep**: Validate QueryStep
- **TestValidate_HTTPCallStep**: Validate HTTPCallStep
- **TestValidate_ResponseStep**: Validate ResponseStep
- **TestValidate_BlockStep**: Validate BlockStep
- **TestValidate_ConditionAliases**: Validate ConditionAliases
- **TestValidate_Warnings**: Validate Warnings
- **TestValidate_DuplicateStepNames**: Validate DuplicateStepNames
- **TestValidate_MultiStepRequiresNames**: Validate MultiStepRequiresNames
- **TestValidate_PathParameters**: Validate PathParameters
- **TestExtractPathParams**: ExtractPathParams
- **TestValidate_SQLTemplateInjection**: Validate SQLTemplateInjection
- **TestContainsTemplateInterpolation**: ContainsTemplateInterpolation
- **TestValidate_RateLimitPool**: TestValidate_RateLimitPool verifies rate limit validation accepts valid pool references
- **TestValidate_RateLimitInline**: TestValidate_RateLimitInline verifies rate limit validation accepts valid inline config
- **TestValidate_RateLimitErrors**: TestValidate_RateLimitErrors verifies rate limit validation catches invalid configurations
- **TestValidate_HTTPCallRetry**: TestValidate_HTTPCallRetry verifies httpcall retry configuration validation
- **TestValidate_HTTPCallRetryValid**: TestValidate_HTTPCallRetryValid verifies valid httpcall retry configuration passes
- **TestValidate_DivisionSafety**: TestValidate_DivisionSafety tests that unsafe divisions are caught during validation
- **TestValidate_StepReferences**: TestValidate_StepReferences tests that step references are validated


---

## Workflow Steps

**Package**: `internal/workflow/step`

### step_test.go

- **TestQueryStep**: TestQueryStep tests the QueryStep implementation.
- **TestExtractSQLParams**: TestExtractSQLParams tests the extractSQLParams function.
- **TestHTTPCallStep**: TestHTTPCallStep tests the HTTPCallStep implementation.
- **TestNormalizeJSONResponse**: TestNormalizeJSONResponse tests the normalizeJSONResponse function.
- **TestResponseStep**: TestResponseStep tests the ResponseStep implementation.
- **TestResponseStepWriteError**: TestResponseStepWriteError tests the ResponseStep with a failing ResponseWriter.
- **TestHTTPCallRetry**: TestHTTPCallRetry tests the retry logic in HTTPCallStep.
- **TestHTTPCallWithBody**: TestHTTPCallWithBody tests POST requests with body.
- **TestHTTPCallWithHeaders**: TestHTTPCallWithHeaders tests requests with custom headers.


---

## End-to-End

**Package**: `e2e`

### e2e_test.go

- **TestE2E_ServerStartupAndShutdown**: TestE2E_ServerStartupAndShutdown tests the server starts and stops cleanly
- **TestE2E_HealthEndpoint**: TestE2E_HealthEndpoint tests /health returns database status
- **TestE2E_MetricsEndpoint**: TestE2E_MetricsEndpoint tests /_/metrics.json returns runtime stats
- **TestE2E_OpenAPIEndpoint**: TestE2E_OpenAPIEndpoint tests /_/openapi.json returns valid spec
- **TestE2E_RootEndpoint**: TestE2E_RootEndpoint tests / returns endpoint listing
- **TestE2E_WorkflowEndpoint**: TestE2E_WorkflowEndpoint tests workflow execution returns data
- **TestE2E_ErrorHandling_MissingRequiredParameter**: TestE2E_ErrorHandling_MissingRequiredParameter tests 400 response for missing required parameters
- **TestE2E_ErrorHandling_InvalidParameterType**: TestE2E_ErrorHandling_InvalidParameterType tests 400 response for wrong parameter types
- **TestE2E_ErrorHandling_DatabaseError**: TestE2E_ErrorHandling_DatabaseError tests 500 response for database errors
- **TestE2E_ErrorHandling_NotFound**: TestE2E_ErrorHandling_NotFound tests 404 response for non-existent endpoints
- **TestE2E_ErrorHandling_MethodNotAllowed**: TestE2E_ErrorHandling_MethodNotAllowed tests 405 response for wrong HTTP methods
- **TestE2E_LogLevelEndpoint**: TestE2E_LogLevelEndpoint tests runtime log level changes
- **TestE2E_GzipCompression**: TestE2E_GzipCompression tests response compression
- **TestE2E_RequestID**: TestE2E_RequestID tests request ID propagation
- **TestE2E_NotFound**: TestE2E_NotFound tests 404 for unknown paths
- **TestE2E_GracefulShutdown**: TestE2E_GracefulShutdown tests server handles SIGTERM gracefully
- **TestE2E_ErrorHandling_RateLimited**: TestE2E_ErrorHandling_RateLimited tests 429 response when rate limit is exceeded
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
