# SQL Proxy Configuration Examples

This directory contains example configurations demonstrating all features of SQL Proxy.
Each file focuses on a specific feature area with detailed comments explaining usage.

## Example Files

| File | Description |
|------|-------------|
| `01-basic.yaml` | Minimal setup, basic HTTP workflows |
| `02-parameters.yaml` | Parameter types, validation, defaults |
| `03-caching.yaml` | Trigger-level and step-level caching |
| `04-rate-limiting.yaml` | Named pools and inline rate limits |
| `05-scheduling.yaml` | Cron triggers, dynamic dates |
| `06-httpcall.yaml` | External API calls, webhooks, retries |
| `07-blocks.yaml` | Iteration over results, batch processing |
| `08-conditions.yaml` | Conditional step execution |
| `09-advanced.yaml` | Complex multi-feature workflows |
| `10-rest-crud.yaml` | RESTful CRUD patterns, path parameters, all HTTP methods |

## Quick Start

Copy one of these examples and modify for your needs:

```bash
cp examples/01-basic.yaml config.yaml
# Edit config.yaml with your database settings
./sql-proxy -config config.yaml
```

## Template Reference

Templates are used in SQL queries, cache keys, rate limit keys, and response bodies.
Different contexts provide different variables.

### SQL Query Templates

Available variables in SQL templates (using `@param` syntax for parameters):
- `@paramName` - Parameter value from trigger

### Response Templates

Available variables in response templates (Go text/template syntax):

**Query Step Results:**
- `.steps.<name>.data` - Query results (array of maps) - for SELECT queries
- `.steps.<name>.count` - Row count (length of data array)
- `.steps.<name>.rows_affected` - Rows affected (for INSERT/UPDATE/DELETE)
- `.steps.<name>.success` - Boolean success status
- `.steps.<name>.cache_hit` - Whether result came from cache
- `.steps.<name>.duration_ms` - Execution time

**Trigger Data:**
- `.trigger.params.<name>` - Parameter values
- `.trigger.headers.<name>` - HTTP headers (trigger.type == "http")
- `.trigger.client_ip` - Client IP address
- `.trigger.method` - HTTP method
- `.trigger.path` - Request path

**Workflow Metadata:**
- `.workflow.request_id` - Request ID
- `.workflow.name` - Workflow name
- `.workflow.start_time` - Workflow start time
- `.Param.<name>` - Shortcut for trigger.params

**Accessing Data:**
- Use `index` to access specific row/field: `{{index .steps.fetch.data 0 "name"}}`
- Use `json` to output entire result set: `{{json .steps.fetch.data}}`

### Cache Key Templates

Trigger-level cache keys:
- `.Param.<name>` - Parameter values

Step-level cache keys (can reference previous step results):
- `.Param.<name>` - Parameter values
- `.steps.<name>.data` - Previous step results

### Rate Limit Key Templates

- `.ClientIP` - Client IP address
- `.Query.<name>` - Query parameter
- `.Param.<name>` - Any parameter
- `.Header.<name>` - HTTP header value

### HTTPCall URL and Body Templates

Available variables in httpcall step url/body templates:
- `.trigger.params.<name>` - Trigger parameter values
- `.steps.<name>.data` - Previous step query results
- `.steps.<name>.status_code` - Previous httpcall status code
- `.workflow.request_id` - Request ID
- `.workflow.start_time` - Start time

### Block Iteration Context

Inside a block step (iterate over data), additional variables are available:
- `.<as>.<field>` - Current item field (where `<as>` is the iterate.as value)
- `._index` - Current iteration index (0-based)
- `._count` - Total number of items
- `.parent.steps.<name>` - Access parent context steps
- `.steps.<name>` - Steps executed within this iteration

### Template Functions

**JSON Functions**
| Function | Description | Example |
|----------|-------------|---------|
| `json` | Encode value as JSON | `{{json .steps.fetch.data}}` |
| `jsonIndent` | Encode as indented JSON | `{{jsonIndent .steps.fetch.data}}` |

**String Functions**
| Function | Description | Example |
|----------|-------------|---------|
| `upper` | Convert to uppercase | `{{upper .Param.name}}` |
| `lower` | Convert to lowercase | `{{lower .Param.name}}` |
| `trim` | Trim whitespace | `{{trim .Param.value}}` |
| `replace` | Replace all occurrences | `{{replace .Param.text "old" "new"}}` |
| `contains` | Check substring | `{{if contains .Param.text "search"}}` |
| `hasPrefix` | Check prefix | `{{if hasPrefix .Param.path "/"}}` |
| `hasSuffix` | Check suffix | `{{if hasSuffix .Param.file ".json"}}` |

**Default Value Functions**
| Function | Description | Example |
|----------|-------------|---------|
| `default` | Default if empty | `{{.Param.status \| default "active"}}` |
| `coalesce` | First non-empty value | `{{coalesce .Param.a .Param.b "default"}}` |
| `getOr` | Map access with fallback | `{{getOr .Header "X-Custom" "default"}}` |

**Map/Array Access Functions**
| Function | Description | Example |
|----------|-------------|---------|
| `index` | Access array/map element | `{{index .steps.fetch.data 0 "name"}}` |
| `len` | Length of array/map | `{{len .steps.fetch.data}}` |
| `require` | Error if key missing | `{{require .Header "Authorization"}}` |
| `has` | Check if key exists | `{{if has .Header "X-Custom"}}` |

**Math Functions**
| Function | Description | Example |
|----------|-------------|---------|
| `add` | Addition | `{{add .Param.offset 10}}` |
| `sub` | Subtraction | `{{sub .Param.total 1}}` |
| `mul` | Multiplication | `{{mul .Param.quantity .Param.price}}` |
| `div` | Integer division | `{{div .Param.total .Param.count}}` |
| `mod` | Modulo | `{{mod .Param.index 2}}` |

## Path Parameters

Path parameters capture values from the URL path using `{paramName}` syntax:

```yaml
triggers:
  - type: http
    path: "/api/items/{id}"
    method: GET
    parameters:
      - name: "id"           # Must match the path parameter name
        type: "int"
        required: true       # Path parameters must be required
```

**Rules:**
- Path parameters use `{paramName}` syntax (Go 1.22+ native routing)
- Each path parameter must have a matching entry in `parameters`
- Path parameters must be `required: true`
- Path parameters take precedence over query parameters with the same name

**Examples:**
```bash
# Single path parameter
curl http://localhost:8080/api/items/42

# Multiple path parameters
curl http://localhost:8080/api/items/42/reviews/7
```

## HTTP Methods

HTTP triggers support all standard methods:

| Method | Typical Use |
|--------|-------------|
| `GET` | Retrieve resources |
| `POST` | Create resources |
| `PUT` | Replace resources |
| `DELETE` | Remove resources |
| `PATCH` | Partial updates |
| `HEAD` | Check existence (no body) |
| `OPTIONS` | CORS preflight / list methods |

**RESTful Pattern:** Multiple workflows can share the same path with different methods:

```yaml
workflows:
  - name: "list_items"
    triggers:
      - type: http
        path: "/api/items"
        method: GET
    # ...

  - name: "create_item"
    triggers:
      - type: http
        path: "/api/items"
        method: POST
    # ...
```

## Parameter Types

| Type | Description | Example Values |
|------|-------------|----------------|
| `string` | Text value | `"hello"` |
| `int` / `integer` | Whole number | `42` |
| `float` / `double` | Decimal number | `3.14` |
| `bool` / `boolean` | True/false | `true`, `false`, `1`, `0` |
| `datetime` | ISO 8601 timestamp | `2024-01-15T10:30:00Z` |
| `date` | Date only | `2024-01-15` |
| `json` | JSON object/array | `{"key": "value"}` |
| `int[]` | Integer array | `[1, 2, 3]` |
| `string[]` | String array | `["a", "b", "c"]` |
| `float[]` | Float array | `[1.1, 2.2]` |
| `bool[]` | Boolean array | `[true, false]` |

## Cron Expressions

Standard 5-field cron format: `minute hour day-of-month month day-of-week`

| Expression | Description |
|------------|-------------|
| `* * * * *` | Every minute |
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour |
| `0 0 * * *` | Daily at midnight |
| `0 8 * * 1-5` | Weekdays at 8am |
| `0 0 1 * *` | First of each month |

## Dynamic Date Values (Cron Params)

| Value | Description |
|-------|-------------|
| `now` | Current timestamp |
| `today` | Today at midnight |
| `yesterday` | Yesterday at midnight |
| `tomorrow` | Tomorrow at midnight |
