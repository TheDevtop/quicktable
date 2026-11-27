# Quicktable MCP Server

Quicktable exposes its operations over the [Model Context Protocol](https://modelcontextprotocol.io/) (MCP), so AI agents can read and write table data with tool calls. Each tool is one [REST endpoint](rest.md) under another name: `qt_get` takes the same arguments as the body of `POST /api/get` and returns the same JSON.

## Transport

The server uses the **Streamable HTTP** transport at a single endpoint:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/mcp` | POST | Send a JSON-RPC request. The response comes back in the HTTP response body |

The server is stateless. It hands out no session ID and keeps no state between requests, so every `POST` stands alone. Streaming is off: `GET /mcp` returns **405**, and every response is plain JSON rather than an SSE stream. Each request is limited by the server's HTTP timeouts.

Send `Content-Type: application/json` and `Accept: application/json, text/event-stream`, as the transport requires.

If the server listens on a loopback address, requests whose `Host` header is not a loopback name are rejected with **403**, as a guard against DNS rebinding.

### Authentication

Every request to `/mcp` must carry the server's token (set with the server's `-t` flag) as a bearer token:

```
Authorization: Bearer <token>
```

Without a valid token the server answers **401 Unauthorized** with `{"error":"missing or invalid bearer token"}` before reading the JSON-RPC request.

### Example

```
POST /mcp
Authorization: Bearer <token>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":2,"method":"tools/call",
 "params":{"name":"qt_get","arguments":{"table":"users","row":"alice"}}}
```

```json
{"jsonrpc":"2.0","id":2,"result":{"content":[
  {"type":"text","text":"{\"row\":{\"age\":30,\"email\":\"alice@example.com\"}}"}
]}}
```

The server reports itself as `quicktable-mcp` and offers tools only.

---

## Tools

| Tool | Same as | Reads only | Purpose |
|------|---------|------------|---------|
| `qt_scan` | [`POST /api/scan`](rest.md#scan) | ✓ | List tables, the row keys of a table, or its column names |
| `qt_get` | [`POST /api/get`](rest.md#get) | ✓ | Read a table, a row, a column or one value |
| `qt_values` | [`POST /api/values`](rest.md#values) | ✓ | Read many cells from one snapshot |
| `qt_query` | [`POST /api/query`](rest.md#query) | ✓ | Filter rows by key range and predicates, or aggregate them |
| `qt_batch` | [`POST /api/batch`](rest.md#batch) | | Set, delete, check and increment cells atomically |
| `qt_incr` | [`POST /api/incr`](rest.md#incr) | | Atomically add to a number and return the result |

The read-only tools are annotated `readOnlyHint: true`. `qt_batch` and `qt_incr` are annotated `destructiveHint: true`. All tools have `openWorldHint: false`. Each tool's input schema is listed by `tools/list` and rejects unknown arguments.

The REST reference covers each tool's arguments, scopes and results. The points an agent most needs:

- **Names** are any non-empty string without whitespace. Row keys have no fixed format and are never generated: the caller picks them.
- **Values** are numbers, strings or booleans. `null`, arrays and objects are rejected.
- **`qt_get` and `qt_batch` delete pick their scope from the arguments given.** Leaving out `row` and `column` in a `delete` operation drops the whole table.
- **Predicates** use `cmp`, one of `=`, `!=`, `<`, `<=`, `>`, `>=`, `prefix`, `contains`, `exists` and `missing`. Only `missing` matches an absent cell, and a cell of another type than `value` never matches, not even under `!=`.
- **`qt_query` ranges** are `from` (inclusive) and `to` (exclusive), plus `prefix`. Rows come back in ascending row key order, or descending with `reverse`; `reverse` with `limit` reads the last rows.
- **`qt_query` `select`** drops rows that have none of the selected columns, as in Bigtable, and `limit` counts only the rows returned.
- **`qt_batch`** applies its `ops` in order in one transaction. A failing `if` aborts the batch and nothing is written.

### Example arguments

`qt_query`: adults' email addresses, at most 10 rows

```json
{
  "table": "users",
  "where": [{"column": "age", "cmp": ">=", "value": 18}],
  "select": ["email"],
  "limit": 10
}
```

```json
{"rows":[{"key":"alice","row":{"email":"alice@example.com"}}]}
```

`qt_batch`: publish a post only if it is still at version 3

```json
{
  "ops": [
    {"op": "if", "table": "posts", "row": "p1", "column": "version", "cmp": "=", "value": 3},
    {"op": "set", "table": "posts", "row": "p1", "column": "version", "value": 4},
    {"op": "set", "table": "posts", "row": "p1", "column": "status", "value": "published"}
  ]
}
```

```json
{"status":"ok"}
```

---

## Errors

A failed tool call is answered with a normal result that has `isError: true` and a plain-text message. It does not raise a JSON-RPC error:

```json
{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"mustang: not found"}],"isError":true}}
```

The messages are the same as the REST API's `error` field:

| Message starts with | Meaning |
|---------------------|---------|
| `mustang: not found` | The table, row, column or cell being read does not exist, or `avg`, `min` or `max` found no value |
| `mustang: invalid name` | An empty name, or one with whitespace |
| `mustang: unsupported value type` | A value that is not a number, string or boolean; a non-string operand for `prefix` or `contains`; `incr`, `sum` or `avg` on a cell that is not a number; or `min` or `max` over mixed types |
| `mustang: condition failed: table/row/column` | The `qt_batch` `if` on that cell failed. Nothing was written |
| `bad request: json: ...` | An unknown argument, or an argument of the wrong JSON type |
| `bad request: unknown cmp`, `unknown op`, `unknown agg` | An unknown predicate, batch operation or aggregate |
| `bad request: columns needs a table` | `qt_scan` with `columns` but no `table` |
| `op N: ...` | `qt_batch` operation N (counting from 0) has an unknown `op` or `cmp` |
| `Transaction Conflict` | A concurrent write to the same cells. Nothing was written, and the call can be retried |
| `Txn is too big` | The batch does not fit in one transaction. Nothing was written |
