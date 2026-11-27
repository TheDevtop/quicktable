# Quicktable MCP API

The Quicktable server is an MCP server that offers six tools:
`qt_scan`, `qt_get`, `qt_values`, `qt_query`, `qt_batch` and `qt_incr`.
They are the same operations as the REST API,
with the same arguments and the same results;
[rest.md](./rest.md) covers each one in more depth.

## Contents

- [Connecting](#connecting)
- [Data model](#data-model)
- [Results and errors](#results-and-errors)
- [Tools](#tools)
  - [qt_scan](#qt_scan)
  - [qt_get](#qt_get)
  - [qt_values](#qt_values)
  - [qt_query](#qt_query)
  - [qt_batch](#qt_batch)
  - [qt_incr](#qt_incr)
- [Recipes](#recipes)

## Connecting

| | |
|-|-|
| Transport | Streamable HTTP |
| Endpoint | `http://<host>:16000/mcp` |
| Server name | `quicktable-mcp` |
| Capabilities | Tools only: no resources, prompts or notifications |
| Sessions | None; the server is stateless and every call is a plain request and response |
| Streaming | Disabled; a `GET` on `/mcp` answers `405` |
| Authentication | `Authorization: Bearer <token>`, the token given to the server with `-t` |

A request without a valid token gets HTTP `401`.

A client configuration, such as for Claude Code (`.mcp.json`):

```json
{
  "mcpServers": {
    "quicktable": {
      "type": "http",
      "url": "http://localhost:16000/mcp",
      "headers": {"Authorization": "Bearer secret"}
    }
  }
}
```

Or from the command line:

```
claude mcp add --transport http quicktable http://localhost:16000/mcp \
	--header 'Authorization: Bearer secret'
```

## Data model

A store holds tables, a table holds rows named by a row key,
and a row holds cells named by a column.
Rows need not share columns.
A table or row exists only while it holds a cell:
there is no create step, writing a cell creates its row and table,
and deleting the last cell removes them.

- **Names.** Tables, row keys and columns are non-empty strings without whitespace or NUL.
- **Values.** A cell holds a number (64-bit float), a string or a boolean. `null`, arrays and objects cannot be stored.
- **Ordering.** Rows are kept sorted by row key, byte-wise. Values compare only with values of the same type.
- **Times.** There is no time type. Store times as fixed-width UTC strings such as `2026-11-26T10:00:00Z`; they sort in time order, so row key ranges and string comparisons select time ranges.

## Results and errors

A successful call returns one text content item holding a JSON object,
the same object the REST API returns:

```json
{"content": [{"type": "text", "text": "{\"row\":{\"age\":31,\"name\":\"Ann\"}}"}]}
```

A failed call returns a tool result with `isError: true`
and the error message as text:

```json
{"content": [{"type": "text", "text": "mustang: not found"}], "isError": true}
```

| Message starts with | Meaning | What to do |
|---------------------|---------|------------|
| `mustang: not found` | Table, row, column or cell does not exist | Check names with `qt_scan` |
| `bad request:` | Unknown argument, or unknown `cmp`, `agg` or `op` | Fix the arguments |
| `op N: bad request:` | Batch operation `N` (from 0) is invalid | Fix that operation |
| `mustang: invalid name` | Empty name, or name with whitespace | Fix the name |
| `mustang: unsupported value type` | Wrong value type, such as `incr` on a string or `sum` over strings | Fix the value or column |
| `mustang: condition failed: t/r/c` | An `if` in a batch did not hold; nothing was written | Re-read and decide |
| `Transaction Conflict. Please retry` | Another writer changed a cell the batch checked | Retry the batch |
| `Txn is too big to fit into one request` | Batch too large | Split the batch |

Arguments a tool does not know are rejected, not ignored.
This guards deletes: a misspelled `row` would otherwise widen a row delete to a table delete.

## Tools

Every tool carries `openWorldHint: false`.
Read tools carry `readOnlyHint: true`;
write tools carry `destructiveHint: true`.

| Tool | Reads or writes | Purpose |
|------|-----------------|---------|
| `qt_scan` | reads | List tables, row keys or columns |
| `qt_get` | reads | Read a table, row, column or value |
| `qt_values` | reads | Read many cells at once |
| `qt_query` | reads | Filter rows, or count, sum, average, min or max them |
| `qt_batch` | writes | Set, delete, check and increment atomically |
| `qt_incr` | writes | Add to a number and return the result |

### qt_scan

List table names, the row keys of a table, or the column names of a table.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `table` | string | no | Table to list; omit to list all tables |
| `columns` | boolean | no | List columns instead of row keys; needs `table` |

Result: `{"names": [...]}`.
Tables and row keys come sorted; columns do not.
A table that does not exist gives an empty list, not an error.
Listing row keys or columns of a large table returns all of them;
use `qt_query` with `prefix`, `from`, `to` or `limit` to page through rows.

### qt_get

Read a whole table, a row, a column or one value.
Which of `row` and `column` are given picks the scope.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `table` | string | yes | Table name |
| `row` | string | no | Row key |
| `column` | string | no | Column name |

| Given | Result |
|-------|--------|
| `table` | `{"table": {"<row key>": {"<column>": <value>}}}` |
| `table`, `row` | `{"row": {"<column>": <value>}}` |
| `table`, `column` | `{"column": {"<row key>": <value>}}` |
| `table`, `row`, `column` | `{"value": <value>}` |

Anything absent is `mustang: not found`.
Reading a whole table returns every cell; prefer `qt_query` with `limit` on large tables.

### qt_values

Read many cells from one consistent snapshot.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `cells` | array of `{table, row, column}` | yes | Cells to read; all three fields required |

Result: `{"values": [...]}`, in the order of `cells`.
A missing cell is `null`, not an error.

### qt_query

Scan a table's rows in row key order, filtered by row key range and predicates,
and return them, or fold them into one value.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `table` | string | yes | Table name |
| `prefix` | string | no | Only row keys starting with this |
| `from` | string | no | Only row keys `>= from` |
| `to` | string | no | Only row keys `< to` |
| `where` | array of predicates | no | All must hold |
| `select` | array of strings | no | Columns to return; all if omitted |
| `limit` | integer | no | Stop after this many matching rows; `0` is no limit |
| `reverse` | boolean | no | Scan in descending row key order, so `limit` keeps the last rows |
| `agg` | string | no | `count`, `sum`, `avg`, `min` or `max` |
| `column` | string | with `sum`, `avg`, `min`, `max` | Column to fold |

A predicate is `{"column": ..., "cmp": ..., "value": ...}`:

| `cmp` | Holds when the cell |
|-------|---------------------|
| `=` `!=` `<` `<=` `>` `>=` | Is present, has the type of `value`, and compares so |
| `prefix` | Is a string starting with `value` |
| `contains` | Is a string containing `value` |
| `exists` | Is present; `value` is not used |
| `missing` | Is absent; `value` is not used |

A cell of another type than `value` never matches, not even `!=`,
and an absent cell matches only `missing`.

Without `agg`, the result is `{"rows": [{"key": "<row key>", "row": {...}}, ...]}`
in scan order.
With `select`, each row holds only those columns,
and rows with none of them are left out.

With `agg`, the result is `{"value": <value>}`:

| `agg` | Result | No rows to fold |
|-------|--------|-----------------|
| `count` | Number of matching rows | `0` |
| `sum` | Sum of `column` over rows that have it | `0` |
| `avg` | Mean of `column` over rows that have it | `mustang: not found` |
| `min`, `max` | Smallest or largest value of `column` | `mustang: not found` |

`sum` and `avg` need numbers. `min` and `max` work on any one type, not mixed types.
`limit` and `reverse` apply before folding.

### qt_batch

Apply operations atomically and in order.
Either all take effect or none do,
and later operations see the writes of earlier ones.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `ops` | array of operations | yes | Applied in order |

| Operation field | Type | Meaning |
|-----------------|------|---------|
| `op` | string | `set`, `delete`, `if` or `incr`; required |
| `table` | string | Table name; required |
| `row` | string | Row key |
| `column` | string | Column name |
| `value` | number, string or boolean | Value for `set`; operand for `if` |
| `cmp` | string | Comparison for `if`, as in `qt_query` predicates |
| `delta` | number | Amount for `incr` |

| `op` | Needs | Effect |
|------|-------|--------|
| `set` | `row`, `column`, `value` | Store the value, replacing what was there |
| `delete` | see below | Delete cells |
| `if` | `row`, `column`, `cmp`, `value` (not for `exists`/`missing`) | Abort the batch unless the cell satisfies the comparison |
| `incr` | `row`, `column`, `delta` | Add to the number in the cell; a missing cell counts as `0` |

| `delete` with | Deletes |
|---------------|---------|
| `row` and `column` | One cell |
| `row` only | The whole row |
| `column` only | That column in every row |
| neither | **The whole table** |

Result: `{"status": "ok"}`.
Deleting something that does not exist is not an error.

### qt_incr

Atomically add `delta` to a numeric cell and return the new value.
A missing cell counts as `0`.
Concurrent increments are retried by the server, so none are lost.

| Argument | Type | Required | Meaning |
|----------|------|----------|---------|
| `table` | string | yes | Table name |
| `row` | string | yes | Row key |
| `column` | string | yes | Column name |
| `delta` | number | yes | Amount to add; may be negative |

Result: `{"value": <new value>}`.
A cell holding a string or boolean is an error.

## Recipes

Explore an unknown store:

```
qt_scan {}
qt_scan {"table": "users"}
qt_scan {"table": "users", "columns": true}
qt_query {"table": "users", "limit": 5}
```

Write a row:

```
qt_batch {"ops": [
  {"op": "set", "table": "users", "row": "ann", "column": "name", "value": "Ann"},
  {"op": "set", "table": "users", "row": "ann", "column": "age", "value": 31}
]}
```

Change a value only if it has not changed since it was read:

```
qt_batch {"ops": [
  {"op": "if", "table": "orders", "row": "o17", "column": "status", "cmp": "=", "value": "pending"},
  {"op": "set", "table": "orders", "row": "o17", "column": "status", "value": "paid"}
]}
```

Insert only if absent:

```
qt_batch {"ops": [
  {"op": "if", "table": "users", "row": "bob", "column": "name", "cmp": "missing"},
  {"op": "set", "table": "users", "row": "bob", "column": "name", "value": "Bob"}
]}
```

Count adults, and find the oldest age:

```
qt_query {"table": "users", "where": [{"column": "age", "cmp": ">=", "value": 18}], "agg": "count"}
qt_query {"table": "users", "agg": "max", "column": "age"}
```

The ten latest events of a day, with timestamp row keys:

```
qt_query {"table": "events", "from": "2026-11-26T00:00:00Z", "to": "2026-11-27T00:00:00Z",
          "reverse": true, "limit": 10}
```

Delete one row; always give `row`, as leaving out both `row` and `column` deletes the table:

```
qt_batch {"ops": [{"op": "delete", "table": "users", "row": "bob"}]}
```
