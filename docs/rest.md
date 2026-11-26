# Quicktable REST API

All endpoints except [`/health`](#health) are under `/api/tables`. Every response body is JSON with `Content-Type: application/json`.

**Row keys** are 8-character hexadecimal strings (e.g. `"a3f10c2b"`). Either case is accepted; keys are stored and listed in lowercase. The engine auto-generates one when you insert a row.

**Cell values** must be a JSON number, string or boolean. Numbers are stored as 64-bit floats, so integers above 2^53 lose precision. `null`, arrays and objects are rejected.

---

## Error responses

All errors return a JSON body with a single `"error"` key.

```json
{ "error": "row not found" }
```

| HTTP status | Cause |
|-------------|-------|
| 400 Bad Request | Invalid table name, row key, column name or operator, unsupported value type, wrong cell type for `incr`/`append`/`prepend`, or malformed JSON |
| 404 Not Found | Row or column does not exist |
| 409 Conflict | A `cas` condition check failed |
| 500 Internal Server Error | Unexpected storage error |

---

## Health

```
GET /health
```

Returns `200` whenever the server is accepting requests. Use it for liveness and readiness probes.

**Response 200**

```json
{ "status": "ok" }
```

---

## Table operations

### List tables

```
GET /api/tables
```

Returns the names of all tables, sorted.

> **Note:** Tables are not created or stored separately. A table exists only while it holds at least one cell, so it appears here after its first write and disappears once its last row or cell is deleted.

**Response 200**

```json
{ "tables": ["orders", "users"] }
```

---

### Drop a table

```
DELETE /api/tables/{table}
```

Permanently removes the table and all of its rows. This cannot be undone.

> **Note:** Dropping a table that does not exist is not an error. It returns `200` with `{"status":"ok"}` and does nothing, so you can't use this endpoint to check whether a table existed.

**Response 200**

```json
{ "status": "ok" }
```

---

## Cell operations

### Get a cell

```
GET /api/tables/{table}/rows/{rowKey}/columns/{column}
```

Returns the value stored at a single column.

**Response 200**

```json
{
  "table": "users",
  "rowKey": "a3f10c2b",
  "column": "email",
  "value": "alice@example.com"
}
```

---

### Set a cell

```
PUT /api/tables/{table}/rows/{rowKey}/columns/{column}
```

**Body**

```json
{ "value": "alice@example.com" }
```

The `value` field accepts any scalar. Creates the row or column if it does not exist.

**Response 200**

```json
{ "status": "ok" }
```

---

### Delete a cell

```
DELETE /api/tables/{table}/rows/{rowKey}/columns/{column}
```

Removes a single column from the row. The row itself is kept.

**Response 200**

```json
{ "status": "ok" }
```

---

## Row operations

### List rows

```
GET /api/tables/{table}/rows
```

Returns the keys of all rows in the table, sorted. Use [Get a row](#get-a-row) or [Query](#query) to read their columns.

> **Note:** Listing a table that does not exist is not an error. It returns `200` with an empty list.

**Response 200**

```json
{ "rows": ["0b7e19d4", "a3f10c2b"] }
```

---

### Get a row

```
GET /api/tables/{table}/rows/{rowKey}
```

Returns all columns of the row as a key/value map.

**Response 200**

```json
{
  "table": "users",
  "rowKey": "a3f10c2b",
  "columns": {
    "email": "alice@example.com",
    "age": 30,
    "active": true
  }
}
```

---

### Set a row

```
PUT /api/tables/{table}/rows/{rowKey}
```

Writes the given columns to an existing row (merge, not replace). Columns not mentioned are left untouched.

**Body** — a plain JSON object of column → value pairs:

```json
{
  "email": "alice@example.com",
  "age": 30
}
```

**Response 200**

```json
{ "status": "ok" }
```

---

### Insert a row

```
POST /api/tables/{table}/rows
```

Creates a new row with an auto-generated key that is not yet used in the table. The body is a JSON object of initial column values. With an empty body `{}` a key is returned but nothing is stored, because a row exists only while it holds a cell.

**Body**

```json
{
  "email": "bob@example.com",
  "score": 0
}
```

**Response 201**

```json
{
  "table": "users",
  "rowKey": "d81c4f9a"
}
```

---

### Delete a row

```
DELETE /api/tables/{table}/rows/{rowKey}
```

Removes the row and all its columns.

**Response 200**

```json
{ "status": "ok" }
```

---

## Query

```
POST /api/tables/{table}/query
```

Scan, filter, and project rows. Send an empty body or omit `Content-Length` to return all rows.

**Body fields** (all optional)

| Field | Type | Description |
|-------|------|-------------|
| `prefix` | string | Only rows whose key starts with this prefix |
| `startKey` | string | Inclusive lower bound on the row key range |
| `endKey` | string | Inclusive upper bound on the row key range |
| `select` | string[] | Columns to include; all columns returned if omitted |
| `orderDesc` | bool | Sort rows by key in descending order (default: ascending) |
| `offset` | int | Skip the first N matching rows |
| `limit` | int | Return at most N rows |
| `where` | Predicate[] | Column filter predicates (see below) |
| `countOnly` | bool | Return only a count instead of row data |

**Predicate object**

```json
{ "column": "age", "op": ">=", "value": 18 }
```

| `op` value | Meaning |
|-----------|---------|
| `"="` | Equal |
| `"!="` | Not equal |
| `">"` | Greater than |
| `">="` | Greater than or equal |
| `"<"` | Less than |
| `"<="` | Less than or equal |
| `"CONTAINS"` | String contains substring |
| `"PREFIX"` | String starts with prefix |
| `"EXISTS"` | Column is present (no `value` needed) |
| `"MISSING"` | Column is absent (no `value` needed) |

Any other `op` is rejected with **400**. Numbers compare with numbers, strings with strings and booleans with booleans; a value of a different kind never matches, except under `"!="`.

**Example request**

```json
{
  "prefix": "a3",
  "select": ["email", "age"],
  "where": [
    { "column": "age", "op": ">=", "value": 18 },
    { "column": "active", "op": "EXISTS" }
  ],
  "limit": 50
}
```

**Response 200** — rows

```json
{
  "table": "users",
  "rows": [
    { "rowKey": "a3f10c2b", "columns": { "email": "alice@example.com", "age": 30 } }
  ],
  "count": 1
}
```

**Response 200** — count only

```json
{ "count": 42 }
```

Each item in `rows` has the shape:

```json
{ "rowKey": "a3f10c2b", "columns": { "col": "val" } }
```

---

## Batch operations

```
POST /api/tables/{table}/batch
```

Multi-row read, write, delete, or insert in a single request. Unknown fields in the body or in a mutation are rejected with **400**, so a misspelled `setCols` is not silently dropped.

**Body**

```json
{
  "action": "write" | "get" | "delete" | "insert",
  "rowKeys": ["..."],
  "mutations": [...],
  "rows": [...]
}
```

---

### Batch get

Retrieve multiple rows by their keys.

```json
{
  "action": "get",
  "rowKeys": ["a3f10c2b", "d81c4f9a"]
}
```

**Response 200**

```json
{
  "table": "users",
  "rows": {
    "a3f10c2b": { "email": "alice@example.com" },
    "d81c4f9a": { "email": "bob@example.com" }
  }
}
```

---

### Batch write

Apply multiple row mutations atomically in one transaction.

```json
{
  "action": "write",
  "mutations": [
    {
      "rowKey": "a3f10c2b",
      "setCols": { "score": 100 }
    },
    {
      "rowKey": "d81c4f9a",
      "delCols": ["temp_field"]
    },
    {
      "rowKey": "ff001122",
      "delete": true
    }
  ]
}
```

**Mutation fields**

| Field | Type | Description |
|-------|------|-------------|
| `rowKey` | string | Target row (required) |
| `setCols` | object | Columns to set (key/value pairs) |
| `delCols` | string[] | Column names to delete |
| `delete` | bool | If `true`, delete the entire row |

**Response 200**

```json
{ "status": "ok" }
```

---

### Batch delete

Delete multiple rows.

```json
{
  "action": "delete",
  "rowKeys": ["a3f10c2b", "d81c4f9a"]
}
```

**Response 200**

```json
{ "status": "ok" }
```

---

### Batch insert

Insert multiple rows atomically, each under a new auto-generated key that is not yet used in the table. The keys are returned in the order of `rows`. An empty row `{}` gets a key but stores nothing, as with [Insert a row](#insert-a-row).

```json
{
  "action": "insert",
  "rows": [
    { "email": "alice@example.com", "score": 0 },
    { "email": "bob@example.com" }
  ]
}
```

**Response 201**

```json
{
  "table": "users",
  "rowKeys": ["a3f10c2b", "d81c4f9a"]
}
```

---

## Atomic cell operations

All atomic operations return the new value after the operation.

### Increment

```
POST /api/tables/{table}/rows/{rowKey}/incr
```

Atomically adds `delta` to a numeric cell. The cell is created at zero if it does not exist.

**Body**

```json
{ "column": "score", "delta": 5 }
```

**Response 200**

```json
{ "value": 105 }
```

---

### Append

```
POST /api/tables/{table}/rows/{rowKey}/append
```

Atomically appends a string to a string cell. The cell is created if it does not exist.

**Body**

```json
{ "column": "log", "value": " entry2" }
```

**Response 200**

```json
{ "value": "entry1 entry2" }
```

---

### Prepend

```
POST /api/tables/{table}/rows/{rowKey}/prepend
```

Atomically prepends a string to a string cell. The cell is created if it does not exist.

**Body**

```json
{ "column": "log", "value": "prefix: " }
```

**Response 200**

```json
{ "value": "prefix: entry1" }
```

---

### Check-and-set (CAS)

```
POST /api/tables/{table}/rows/{rowKey}/cas
```

Atomically checks a condition on the row and writes columns only if the condition holds. Returns **409 Conflict** if the check fails.

Omit `condition` to write unconditionally. Unknown fields in the body are rejected with **400**, so a misspelled `condition` cannot become an unconditional write.

**Body**

```json
{
  "condition": {
    "column": "version",
    "op": "=",
    "value": 3
  },
  "setCols": {
    "version": 4,
    "status": "published"
  }
}
```

| `condition` field | Type | Description |
|-------------------|------|-------------|
| `column` | string | Column to check |
| `op` | CompareOp | Any operator from the query table above |
| `value` | any | Value to compare against |

**Response 200**

```json
{ "status": "ok" }
```

**Response 409** — condition failed

```json
{ "error": "check and put condition failed" }
```
