# Quicktable MCP Server

Quicktable exposes its wide-column engine over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) using SSE transport. This lets AI agents read and write table data with structured tool calls.

## Transport

The server uses the **SSE + HTTP POST** transport pattern:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/mcp/sse` | GET | Open a persistent SSE stream; receive the message endpoint URL |
| `/mcp/message?sessionId=<id>` | POST | Send JSON-RPC tool calls; responses are delivered over SSE |

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
| `rowKey` | string | 8-character hex row key (e.g. `"a3f10c2b"`) |
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

**Parameters** — `table`, `rowKey`, `column` (all required), plus `value` (any scalar)

> **Note:** `value` is passed as a plain argument, not a declared MCP parameter. Pass it alongside the other arguments in the `arguments` object.

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
| `endKey` | string | Exclusive upper bound on row key range |
| `orderDesc` | bool | Sort by key descending (default: ascending) |
| `offset` | number | Skip the first N matching rows |
| `limit` | number | Return at most N rows |
| `countOnly` | bool | Return only a count |

> **Note:** Column selection (`select`) and `where` predicates are not yet exposed as MCP parameters; use the REST API for predicate-filtered queries.

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

Atomically increment an integer or float cell. The cell is initialised to zero if it does not exist. Pass a whole number for integer arithmetic, a decimal for float arithmetic.

**Parameters** — `table`, `rowKey`, `column` (all required), `delta` (number, required)

**Result**

```json
{"value":105}
```

---

### `qte_append_cell`

Atomically append a string suffix to a cell value.

**Parameters** — `table`, `rowKey`, `column`, `suffix` (all required)

**Result**

```json
{"value":"entry1 entry2"}
```

---

### `qte_batch_get`

Retrieve multiple rows by their keys in one round-trip.

**Parameters** — `table` (required), plus `rowKeys` (string array, required)

`rowKeys` is passed as a plain argument alongside `table`.

**Result** — map of rowKey → columns

```json
{
  "a3f10c2b": {"email":"alice@example.com"},
  "d81c4f9a": {"email":"bob@example.com"}
}
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
| `qte_batch_get` | ✓ | | |
| `qte_list_tables` | ✓ | | |
| `qte_drop_table` | | ✓ | ✓ |
| `qte_list_rows` | ✓ | | |

---

## Data constraints

- **Table name:** non-empty, no whitespace
- **Row key:** exactly 8 lowercase hex characters (`[0-9a-f]{8}`)
- **Column name:** non-empty, no whitespace
- **Cell value:** scalar only — `int`, `int64`, `uint64`, `float64`, `bool`, `string`, or `[]byte`; `null` is rejected

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
| `check and put condition failed` | CAS condition not satisfied |
