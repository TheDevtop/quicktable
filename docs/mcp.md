# Quicktable MCP Server

Quicktable exposes its wide-column engine over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) using SSE transport. This lets AI agents read and write table data with structured tool calls.

## Transport

The server uses the **SSE + HTTP POST** transport pattern:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/mcp/sse` | GET | Open a persistent SSE stream; receive the message endpoint URL |
| `/mcp/message?sessionId=<id>` | POST | Send JSON-RPC tool calls; answered `202 Accepted` with no body, responses are delivered over SSE |

### Session handshake

1. Open a long-lived `GET /mcp/sse` connection (keep-alive).
2. The server immediately sends an SSE event:
   ```
   event: endpoint
   data: /mcp/message?sessionId=<uuid>
   ```
3. Send all subsequent JSON-RPC requests to that URL.
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
| `select` | string[] | Columns to return; all columns if omitted |
| `where` | Predicate[] | Predicates that must all hold |

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

**Result** — count only

```json
{"count":42}
```

---

### `qte_incr_cell`

Atomically increment an integer or float cell. The cell is initialised to zero if it does not exist. Float cells stay floats; integer cells stay integers unless `delta` has a fractional part.

**Parameters** — `table`, `rowKey`, `column` (all required), `delta` (number, required)

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

**Result** — map of rowKey → columns

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
| `qte_list_tables` | ✓ | | |
| `qte_drop_table` | | ✓ | ✓ |
| `qte_list_rows` | ✓ | | |

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
| `cell value is not a string or byte slice` | Append/prepend on wrong type |
| `unknown comparison operator: ...` | Bad `op` in a `where` predicate or `cas` condition |
| `unknown argument ...`, `unknown condition key ...`, `mutation N: ...` | Misspelled or malformed `qte_cas` / `qte_batch_write` input |
| `check and put condition failed` | CAS condition not satisfied |
