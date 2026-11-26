# Quicktable REST API

All endpoints except [`/health`](#health) are under `/api/tables`. Every response body is JSON with `Content-Type: application/json`, except for [`HEAD`](#check-a-row-exists), which has no body.

**Authentication:** every request except [`GET /health`](#health) must carry the server's token (set with the server's `-t` flag) as a bearer token:

```
Authorization: Bearer <token>
```

A request with a missing, malformed or wrong token is rejected with **401 Unauthorized** and a `WWW-Authenticate: Bearer` header, before anything else is checked. The error never repeats the token that was sent.

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
| 400 Bad Request | Invalid table name, row key, column name or operator, unsupported value type, wrong cell type for `incr`/`append`/`prepend`, conflicting query fields, or malformed JSON |
| 401 Unauthorized | Missing or invalid bearer token: `{ "error": "missing or invalid bearer token" }` |
| 404 Not Found | Row or column does not exist |
| 409 Conflict | A `cas` or `cad` condition check failed |
| 500 Internal Server Error | Unexpected storage error |

---

## Health

```
GET /health
```

Returns `200` whenever the server is accepting requests. Use it for liveness and readiness probes. This is the only endpoint that needs no token.

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

### List columns

```
GET /api/tables/{table}/columns
```

Returns the distinct column names used by any row of the table, sorted. This scans every key of the table, so it gets slower as the table grows.

> **Note:** Listing a table that does not exist is not an error. It returns `200` with an empty list.

**Response 200**

```json
{ "columns": ["active", "age", "email"] }
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

Returns all columns of the row as a key/value map, or **404** if the row does not exist.

To read only some columns, repeat the `column` query parameter:

```
GET /api/tables/{table}/rows/{rowKey}?column=email&column=age
```

Columns the row does not have are left out of `columns`. Unlike a full read, this returns `200` with empty `columns` when the row does not exist.

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

### Check a row exists

```
HEAD /api/tables/{table}/rows/{rowKey}
```

Checks whether the row holds any cell, without reading its values. There is no response body.

| Status | Meaning |
|--------|---------|
| 200 OK | The row exists |
| 404 Not Found | The row does not exist |
| 400 Bad Request | Invalid table name or row key |

---

### Set a row

```
PUT /api/tables/{table}/rows/{rowKey}
```

Writes the given columns to the row (merge, not replace), creating the row if it does not exist. Columns not mentioned are left untouched. An empty object `{}` writes nothing.

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

Scan, filter, and project rows. Send an empty body to return all rows. Unknown fields are ignored.

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
| `where` | Predicate[] | Predicates that must all hold (see below) |
| `or` | Predicate[][] | Alternative predicate groups: a row matches if all predicates of any group hold. Cannot be combined with `where`, and no group may be empty |
| `countOnly` | bool | Return only a count instead of row data |
| `aggregate` | string | Return statistics over this numeric column instead of row data. Cannot be combined with `countOnly` |

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

**Example request** — `or`

Rows that are active adults, or that have no `age` at all:

```json
{
  "or": [
    [
      { "column": "age", "op": ">=", "value": 18 },
      { "column": "active", "op": "=", "value": true }
    ],
    [
      { "column": "age", "op": "MISSING" }
    ]
  ]
}
```

**Response 200** — count only

```json
{ "count": 42 }
```

**Response 200** — aggregate

`count` is the number of matching rows whose column holds a number. Rows where the column is missing or is not a number are skipped. `offset` and `limit` are ignored, as with `countOnly`. When no row has a number, every field is `0`.

```json
{
  "table": "users",
  "column": "age",
  "stats": { "count": 3, "sum": 90, "min": 25, "max": 35, "avg": 30 }
}
```

Each item in `rows` has the shape:

```json
{ "rowKey": "a3f10c2b", "columns": { "col": "val" } }
```

---

### Delete by query

```
POST /api/tables/{table}/query/delete
```

Deletes every row that matches the filter in one transaction: either all of them are deleted or none are. This cannot be undone.

The body takes the filter fields of [Query](#query): `prefix`, `startKey`, `endKey`, `orderDesc`, `offset`, `limit`, `where` and `or`. `offset` and `limit` apply as in a query, so `limit` lets you delete in chunks. A very large delete can fail with **500** because it does not fit in one transaction; nothing is deleted then, and you can retry with a `limit`.

A body is required, and unknown fields are rejected with **400**, so a misspelled `where` cannot delete every row. Send `{}` to delete every row of the table.

**Example request**

```json
{ "where": [ { "column": "active", "op": "=", "value": false } ] }
```

**Response 200**

```json
{ "table": "users", "deleted": 12 }
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

Retrieve multiple rows by their keys. Rows that do not exist are left out of `rows`.

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

Delete multiple rows in one transaction. Keys of rows that do not exist are not an error.

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

Increment, append and prepend return the new value of the cell. Check-and-set and check-and-delete return `{"status":"ok"}`.

### Increment

```
POST /api/tables/{table}/rows/{rowKey}/incr
```

Atomically adds `delta` to a numeric cell. A cell that does not exist counts as zero. A `delta` that is not a JSON number is rejected with **400**.

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

---

### Check-and-delete (CAD)

```
POST /api/tables/{table}/rows/{rowKey}/cad
```

Atomically checks a condition on the row and, only if it holds, deletes the columns in `delCols`. Without `delCols`, it deletes the whole row. Returns **409 Conflict** if the check fails.

Omit `condition` to delete unconditionally. Unknown fields in the body are rejected with **400**, so a misspelled `delCols` cannot delete the whole row.

**Body**

```json
{
  "condition": {
    "column": "status",
    "op": "=",
    "value": "draft"
  },
  "delCols": ["body", "status"]
}
```

`condition` has the same fields as in [CAS](#check-and-set-cas).

**Response 200**

```json
{ "status": "ok" }
```

**Response 409** — condition failed

```json
{ "error": "check and put condition failed" }
```
