# Quicktable MCP Server

Quicktable exposes its wide-column engine over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) using SSE transport. This lets AI agents read and write table data with structured tool calls.

## Transport

The server uses the **SSE + HTTP POST** transport pattern:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/mcp/sse` | GET | Open a persistent SSE stream; receive the message endpoint URL |
| `/mcp/message?sessionId=<id>` | POST | Send JSON-RPC tool calls; answered `202 Accepted` with no body, responses are delivered over SSE |

### Authentication

Both endpoints require the server's token (set with the server's `-t` flag) as a bearer token on every request, the SSE stream included:

```
Authorization: Bearer <token>
```

Without a valid token the server answers **401 Unauthorized** with `{"error":"missing or invalid bearer token"}`, and no session is created. The session ID in the endpoint URL is not a credential: every message POST must carry the token as well.

### Session handshake

1. Open a long-lived `GET /mcp/sse` connection (keep-alive), sending the `Authorization` header.
2. The server immediately sends an SSE event:
   ```
   event: endpoint
   data: /mcp/message?sessionId=<uuid>
   ```
3. Send all subsequent JSON-RPC requests to that URL, each with the `Authorization` header.
4. Responses arrive as SSE events on the stream:
   ```
   event: message
   data: {"jsonrpc":"2.0","id":1,"result":{...}}
   ```

---

## Tools

All tool results are JSON-encoded text. On error, the tool returns a result with `isError: true` and a plain-text error message — it does not raise a JSON-RPC error.

**Common parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `table` | string | Table name — non-empty, no whitespace |
| `rowKey` | string | 8-character hex row key (e.g. `"a3f10c2b"`), either case |
| `column` | string | Column name — non-empty, no whitespace |

---

### `qte_get_cell`

Get a single cell value.

**Parameters** — `table`, `rowKey`, `column` (all required)

**Result** — the JSON-serialised cell value

```json
"alice@example.com"
```

---

### `qte_set_cell`

Write a scalar value to a single cell. Creates the row or column if absent.

**Parameters** — `table`, `rowKey`, `column`, `value` (all required); `value` is a number, string or boolean

**Result**

```json
{"status":"ok"}
```

---

### `qte_delete_cell`

Remove a single column from a row. The row itself is kept.

**Parameters** — `table`, `rowKey`, `column` (all required)

**Result**

```json
{"status":"ok"}
```

---

### `qte_get_row`

Retrieve all columns for a row as a map.

**Parameters** — `table`, `rowKey` (both required)

**Result**

```json
{"email":"alice@example.com","age":30,"active":true}
```

---

### `qte_set_row`

Write multiple columns to a row (merge, not replace). Columns not mentioned are untouched.

**Parameters** — `table`, `rowKey` (both required), plus `columns` (object, required)

`columns` must be a JSON object of column-name → scalar-value pairs.

**Result**

```json
{"status":"ok"}
```

---

### `qte_insert_row`

Insert a new row with an auto-generated 8-digit hex key.

**Parameters** — `table` (required), plus optional `columns` object

With no columns a key is returned but nothing is stored, because a row exists only while it holds a cell.

**Result**

```json
{"rowKey":"d81c4f9a"}
```

---

### `qte_delete_row`

Delete an entire row and all its columns.

**Parameters** — `table`, `rowKey` (both required)

**Result**

```json
{"status":"ok"}
```

---

### `qte_query`

Scan, filter, and project rows. All parameters except `table` are optional.

**Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `table` | string | **Required.** Table to query |
| `prefix` | string | Filter rows whose key starts with this prefix |
| `startKey` | string | Inclusive lower bound on row key range |
| `endKey` | string | Inclusive upper bound on row key range |
| `orderDesc` | bool | Sort by key descending (default: ascending) |
| `offset` | number | Skip the first N matching rows |
| `limit` | number | Return at most N rows |
| `countOnly` | bool | Return only a count |
| `aggregate` | string | Return statistics over this numeric column instead of rows; cannot be combined with `countOnly` |
| `select` | string[] | Columns to return; all columns if omitted |
| `where` | Predicate[] | Predicates that must all hold |
| `or` | Predicate[][] | Alternative predicate groups: a row matches if all predicates of any group hold. Cannot be combined with `where`, and no group may be empty |

A predicate is `{"column": "age", "op": ">=", "value": 18}`. The operators are the same as in the REST API: `=`, `!=`, `>`, `>=`, `<`, `<=`, `CONTAINS`, `PREFIX`, `EXISTS` and `MISSING`; the last two take no `value`. Any other operator is an error. A value of a different kind than the cell never matches, except under `!=`.

**Example arguments**

```json
{
  "table": "users",
  "select": ["email"],
  "where": [
    {"column": "age", "op": ">=", "value": 18},
    {"column": "active", "op": "EXISTS"}
  ],
  "limit": 10
}
```

**Result** — array of rows

```json
[
  {"rowKey":"a3f10c2b","columns":{"email":"alice@example.com","age":30}},
  {"rowKey":"d81c4f9a","columns":{"email":"bob@example.com","age":25}}
]
```

**Example arguments** — `or`: active adults, or rows without an `age`

```json
{
  "table": "users",
  "or": [
    [{"column": "age", "op": ">=", "value": 18}, {"column": "active", "op": "=", "value": true}],
    [{"column": "age", "op": "MISSING"}]
  ]
}
```

**Result** — count only

```json
{"count":42}
```

**Result** — aggregate

`count` is the number of matching rows whose column holds a number; other rows are skipped. `offset` and `limit` are ignored. When no row has a number, every field is `0`.

```json
{"count":3,"sum":90,"min":25,"max":35,"avg":30}
```

---

### `qte_incr_cell`

Atomically increment a numeric cell. A cell that does not exist counts as zero.

**Parameters** — `table`, `rowKey`, `column` (all required), `delta` (number, required); a `delta` that is not a number is an error

**Result**

```json
{"value":105}
```

---

### `qte_append_cell`

Atomically append a string suffix to a string cell. The cell is created if it does not exist.

**Parameters** — `table`, `rowKey`, `column`, `suffix` (all required)

**Result**

```json
{"value":"entry1 entry2"}
```

---

### `qte_prepend_cell`

Atomically prepend a string to a string cell. The cell is created if it does not exist.

**Parameters** — `table`, `rowKey`, `column`, `prefix` (all required)

**Result**

```json
{"value":"prefix: entry1"}
```

---

### `qte_batch_get`

Retrieve multiple rows by their keys in one round-trip.

**Parameters** — `table`, `rowKeys` (string array) (both required)

**Result** — map of rowKey → columns; rows that do not exist are left out

```json
{
  "a3f10c2b": {"email":"alice@example.com"},
  "d81c4f9a": {"email":"bob@example.com"}
}
```

---

### `qte_cas`

Atomically check a condition on one column of a row and, if it holds, write columns. Omit `condition` to write unconditionally.

**Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `table` | string | **Required.** |
| `rowKey` | string | **Required.** |
| `condition` | object | `{"column": name, "op": op, "value": v}`; operators as in `qte_query` `where` |
| `setCols` | object | Column name → scalar value pairs to write |

Unknown arguments and unknown `condition` keys are rejected, so a misspelled `condition` cannot become an unconditional write.

**Example arguments**

```json
{
  "table": "posts",
  "rowKey": "a3f10c2b",
  "condition": {"column": "version", "op": "=", "value": 3},
  "setCols": {"version": 4, "status": "published"}
}
```

**Result**

```json
{"status":"ok"}
```

If the condition does not hold, the result is an error `check and put condition failed` and nothing is written.

---

### `qte_batch_write`

Apply several row mutations in one transaction: all succeed or none do.

**Parameters** — `table`, `mutations` (both required)

Each mutation is an object:

| Field | Type | Description |
|-------|------|-------------|
| `rowKey` | string | **Required.** Target row |
| `setCols` | object | Columns to set |
| `delCols` | string[] | Columns to delete; applied before `setCols` |
| `delete` | bool | If `true`, delete the entire row; `setCols` and `delCols` are ignored |

Unknown keys in a mutation are rejected.

**Example arguments**

```json
{
  "table": "users",
  "mutations": [
    {"rowKey": "a3f10c2b", "setCols": {"score": 100}},
    {"rowKey": "d81c4f9a", "delCols": ["temp_field"]},
    {"rowKey": "ff001122", "delete": true}
  ]
}
```

**Result**

```json
{"status":"ok"}
```

---

### `qte_list_tables`

List the names of all tables, sorted.

> **Note:** Tables are not created or stored separately. A table exists only while it holds at least one cell, so it appears here after its first write and disappears once its last row or cell is deleted.

**Parameters** — none

**Result**

```json
["orders","users"]
```

---

### `qte_drop_table`

Permanently delete a table and all of its rows. This cannot be undone.

> **Note:** Dropping a table that does not exist is not an error. It returns `{"status":"ok"}` and does nothing, so you can't use this tool to check whether a table existed.

**Parameters** — `table` (required)

**Result**

```json
{"status":"ok"}
```

---

### `qte_list_rows`

List the keys of all rows in a table, sorted. Use `qte_get_row` or `qte_query` to read their columns.

> **Note:** Listing a table that does not exist is not an error. It returns an empty list.

**Parameters** — `table` (required)

**Result**

```json
["0b7e19d4","a3f10c2b"]
```

---

### `qte_batch_insert`

Insert several rows in one transaction, each under a new auto-generated 8-digit hex key: all succeed or none do.

**Parameters** — `table`, `rows` (array of column objects) (both required)

An empty row `{}` gets a key but stores nothing, as with `qte_insert_row`.

**Example arguments**

```json
{
  "table": "users",
  "rows": [
    {"email": "alice@example.com", "score": 0},
    {"email": "bob@example.com"}
  ]
}
```

**Result** — keys in the order of `rows`

```json
{"rowKeys":["a3f10c2b","d81c4f9a"]}
```

---

### `qte_get_cells`

Retrieve selected columns of a row. Columns the row does not have are left out. Unlike `qte_get_row`, a row that does not exist gives an empty object, not an error.

**Parameters** — `table`, `rowKey`, `columns` (string array) (all required)

**Result**

```json
{"email":"alice@example.com","age":30}
```

---

### `qte_row_exists`

Check whether a row holds any cell, without reading its values.

**Parameters** — `table`, `rowKey` (both required)

**Result**

```json
{"exists":true}
```

---

### `qte_list_columns`

List the distinct column names used by any row of a table, sorted. This scans every key of the table.

> **Note:** Listing a table that does not exist is not an error. It returns an empty list.

**Parameters** — `table` (required)

**Result**

```json
["active","age","email"]
```

---

### `qte_cad`

Atomically check a condition on one column of a row and, if it holds, delete the columns in `delCols`, or the whole row if `delCols` is omitted. Omit `condition` to delete unconditionally.

**Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `table` | string | **Required.** |
| `rowKey` | string | **Required.** |
| `condition` | object | `{"column": name, "op": op, "value": v}`; operators as in `qte_query` `where` |
| `delCols` | string[] | Columns to delete; the whole row if omitted |

Unknown arguments and unknown `condition` keys are rejected, so a misspelled `delCols` cannot delete the whole row.

**Example arguments**

```json
{
  "table": "posts",
  "rowKey": "a3f10c2b",
  "condition": {"column": "status", "op": "=", "value": "draft"},
  "delCols": ["body", "status"]
}
```

**Result**

```json
{"status":"ok"}
```

If the condition does not hold, the result is an error `check and put condition failed` and nothing is deleted.

---

### `qte_delete_query`

Delete every row matching a filter in one transaction: all of them are deleted or none are. This cannot be undone.

**Parameters** — `table` (required), and the filter parameters of `qte_query`: `prefix`, `startKey`, `endKey`, `orderDesc`, `offset`, `limit`, `where` and `or`. `offset` and `limit` apply as in `qte_query`, so `limit` lets you delete in chunks. A very large delete can fail because it does not fit in one transaction; nothing is deleted then.

Unknown arguments are rejected, so a misspelled `where` cannot delete every row. With no filter at all, every row of the table is deleted.

**Example arguments**

```json
{"table": "users", "where": [{"column": "active", "op": "=", "value": false}]}
```

**Result**

```json
{"deleted":12}
```

---

## Tool summary

| Tool | Reads | Writes | Atomic |
|------|-------|--------|--------|
| `qte_get_cell` | ✓ | | |
| `qte_set_cell` | | ✓ | |
| `qte_delete_cell` | | ✓ | |
| `qte_get_row` | ✓ | | |
| `qte_set_row` | | ✓ | |
| `qte_insert_row` | | ✓ | |
| `qte_delete_row` | | ✓ | |
| `qte_query` | ✓ | | |
| `qte_incr_cell` | | ✓ | ✓ |
| `qte_append_cell` | | ✓ | ✓ |
| `qte_prepend_cell` | | ✓ | ✓ |
| `qte_batch_get` | ✓ | | |
| `qte_cas` | ✓ | ✓ | ✓ |
| `qte_batch_write` | | ✓ | ✓ |
| `qte_batch_insert` | | ✓ | ✓ |
| `qte_list_tables` | ✓ | | |
| `qte_drop_table` | | ✓ | ✓ |
| `qte_list_rows` | ✓ | | |
| `qte_get_cells` | ✓ | | |
| `qte_row_exists` | ✓ | | |
| `qte_list_columns` | ✓ | | |
| `qte_cad` | ✓ | ✓ | ✓ |
| `qte_delete_query` | ✓ | ✓ | ✓ |

---

## Data constraints

- **Table name:** non-empty, no whitespace
- **Row key:** exactly 8 hex characters (`[0-9a-fA-F]{8}`), stored and listed in lowercase
- **Column name:** non-empty, no whitespace
- **Cell value:** a JSON number, string or boolean; numbers are stored as 64-bit floats; `null`, arrays and objects are rejected

## Error handling

Tool errors are returned as MCP tool results with `isError: true`. The text content is a human-readable message matching the underlying engine error:

| Message | Meaning |
|---------|---------|
| `row not found` | The row key does not exist |
| `column not found` | The column does not exist on the row |
| `table name must be non-empty and contain no whitespace` | Bad table argument |
| `row key must be an 8-digit hexadecimal string` | Bad rowKey argument |
| `column name must be non-empty and contain no whitespace` | Bad column argument |
| `value must be a scalar (...)` | Unsupported value type |
| `cell value is not numeric` | Increment on a non-numeric cell |
| `delta must be numeric` | `qte_incr_cell` with a `delta` that is not a number |
| `cell value is not a string` | Append/prepend on wrong type |
| `unknown comparison operator: ...` | Bad `op` in a `where` or `or` predicate, or in a `qte_cas` / `qte_cad` condition |
| `unknown argument ...`, `unknown condition key ...`, `mutation N: ...` | Misspelled or malformed `qte_cas` / `qte_cad` / `qte_batch_write` / `qte_delete_query` input |
| `... must be an array of strings`, `... must be an object`, `... array required` | An argument of the wrong JSON type |
| `where and or cannot be combined`, `or group N is empty` | Malformed `qte_query` / `qte_delete_query` filter |
| `countOnly and aggregate cannot be combined` | Conflicting `qte_query` output options |
| `check and put condition failed` | CAS or CAD condition not satisfied |
