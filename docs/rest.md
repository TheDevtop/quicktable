# Quicktable REST API

The Quicktable server offers six operations over HTTP:
`scan`, `get`, `values`, `query`, `batch` and `incr`.
Each one is a `POST` to `/api/<name>` with a JSON body,
and answers with a JSON object.
The same operations are offered as MCP tools; see [mcp.md](./mcp.md).

## Contents

- [Conventions](#conventions)
- [Data model](#data-model)
- [Errors](#errors)
- [GET /health](#get-health)
- [POST /api/scan](#post-apiscan)
- [POST /api/get](#post-apiget)
- [POST /api/values](#post-apivalues)
- [POST /api/query](#post-apiquery)
- [POST /api/batch](#post-apibatch)
- [POST /api/incr](#post-apiincr)

## Conventions

| | |
|-|-|
| Base URL | `http://<host>:16000` (set with `-a`) |
| Method | `POST` for every `/api/*` route; `GET` for `/health` |
| Request body | JSON object; an empty body is the same as `{}` |
| Response body | JSON object, `Content-Type: application/json; charset=utf-8` |
| Authentication | `Authorization: Bearer <token>` on every route except `/health` |

**Authentication.**
The server compares the bearer token with the one given by `-t`.
A missing or wrong token gets `401` and
`{"error":"missing or invalid bearer token"}`.

**Unknown fields are rejected.**
A request field the operation does not know is a `400`,
so a misspelled field fails rather than being ignored.
This matters most for deletes,
where a dropped `row` or `column` would widen the delete to a whole table.

**Timeouts.**
The server allows 30 seconds to read a request and 60 seconds to write a response.

## Data model

A store holds tables, a table holds rows named by a row key,
and a row holds cells named by a column.
Rows need not share columns.
A table or row exists only while it holds at least one cell:
there is no create, and deleting the last cell removes the row,
and the last row removes the table.

| Term | Meaning |
|------|---------|
| table | Named set of rows |
| row key | Name of a row within a table; rows are kept sorted by row key |
| column | Name of a cell within a row |
| cell | One value, addressed by table, row key and column |

**Names.**
Table names, row keys and columns must be non-empty
and must not contain whitespace or the NUL byte.
Any other UTF-8 is allowed.
A bad name is a `400` (`mustang: invalid name: ...`).

**Values.**
A cell holds one of three types:

| Type | JSON | Notes |
|------|------|-------|
| number | `31`, `2.5`, `-1e3` | 64-bit float; integers beyond 2^53 lose precision; `NaN` is rejected |
| string | `"Ann"` | |
| boolean | `true`, `false` | |

`null`, arrays and objects cannot be stored (`400`).

**Ordering.**
Row keys sort byte-wise.
Values compare only with values of the same type:
numbers numerically, strings byte-wise, and `false` before `true`.

**Times.**
There is no time type.
Store times as fixed-width UTC strings, such as `2026-11-26T10:00:00Z`;
these sort like the instants they name,
so row key ranges and string comparisons select time ranges.

## Errors

A failed request answers with a non-`2xx` status and one field:

```json
{"error": "mustang: not found"}
```

| Status | Cause | Typical message |
|--------|-------|-----------------|
| `400` | Malformed JSON, unknown field, unknown `cmp`, `agg` or `op` | `bad request: json: unknown field "rows"` |
| `400` | Invalid name | `mustang: invalid name: "a b"` |
| `400` | Unsupported or mismatched value type | `mustang: unsupported value type: age mixes types` |
| `401` | Missing or wrong bearer token | `missing or invalid bearer token` |
| `404` | Table, row, column or cell does not exist | `mustang: not found` |
| `405` | Wrong HTTP method (plain text, not JSON) | `Method Not Allowed` |
| `409` | An `if` in a batch failed | `mustang: condition failed: users/ann/age` |
| `409` | A cell read by an `if` changed concurrently; retry the batch | `Transaction Conflict. Please retry` |
| `413` | Batch too large for one transaction; split it | `Txn is too big to fit into one request` |
| `500` | Storage failure or corrupt data | |

Error messages are for people; branch on the status code.

## GET /health

Liveness probe. Needs no token.

```
GET /health
```

```json
{"status": "ok"}
```

## POST /api/scan

List names: all tables, the row keys of a table, or the columns of a table.
Read only.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `table` | string | no | Table to list; omit to list all tables |
| `columns` | boolean | no | List the table's columns instead of its row keys; needs `table` |

| Request | Lists |
|---------|-------|
| `{}` | Table names, sorted |
| `{"table": T}` | Row keys of `T`, sorted |
| `{"table": T, "columns": true}` | Column names used anywhere in `T`, not guaranteed sorted |

Response:

```json
{"names": ["ann", "bob"]}
```

A table that does not exist gives `{"names": []}`, not `404`.
Listing columns reads every cell of the table, as there is no column index.

## POST /api/get

Read a whole table, a row, a column or one value.
Which of `row` and `column` are given picks the scope.
Read only.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `table` | string | yes | Table name |
| `row` | string | no | Row key |
| `column` | string | no | Column name |

| Given | Response |
|-------|----------|
| `table` | `{"table": {"<row key>": {"<column>": <value>, ...}, ...}}` |
| `table`, `row` | `{"row": {"<column>": <value>, ...}}` |
| `table`, `column` | `{"column": {"<row key>": <value>, ...}}`, rows without the column left out |
| `table`, `row`, `column` | `{"value": <value>}` |

Anything absent is `404`.

Example:

```
POST /api/get
{"table": "users", "row": "ann"}
```

```json
{"row": {"age": 31, "name": "Ann"}}
```

## POST /api/values

Read many cells from one consistent snapshot.
Read only.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `cells` | array | yes | Cells to read, each `{"table", "row", "column"}`, all three required |

Response: `values`, in the same order as `cells`;
a missing cell is `null` rather than an error.

```
POST /api/values
{"cells": [
	{"table": "users", "row": "ann", "column": "age"},
	{"table": "users", "row": "zoe", "column": "age"}
]}
```

```json
{"values": [31, null]}
```

## POST /api/query

Scan a table's rows in row key order,
filtered by row key range and predicates on cells,
and return them, or fold them into one value.
Read only.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `table` | string | yes | Table name |
| `prefix` | string | no | Only row keys starting with this |
| `from` | string | no | Only row keys `>= from` |
| `to` | string | no | Only row keys `< to` |
| `where` | array | no | Predicates, all of which must hold; see below |
| `select` | array of strings | no | Columns to return; all if omitted |
| `limit` | integer | no | Stop after this many matching rows; `0` or omitted is no limit |
| `reverse` | boolean | no | Scan in descending row key order, so `limit` keeps the last rows |
| `agg` | string | no | `count`, `sum`, `avg`, `min` or `max`; omit to return rows |
| `column` | string | for `sum`, `avg`, `min`, `max` | Column to fold |

### Predicates

Each element of `where` tests one column of a row:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `column` | string | yes | Column to test |
| `cmp` | string | yes | Comparison, below |
| `value` | number, string or boolean | except for `exists` and `missing` | Operand |

| `cmp` | Holds when the cell |
|-------|---------------------|
| `=` `!=` `<` `<=` `>` `>=` | Is present, has the operand's type, and compares so |
| `prefix` | Is a string starting with the operand (a string) |
| `contains` | Is a string containing the operand (a string) |
| `exists` | Is present |
| `missing` | Is absent |

A cell of another type than the operand never matches, not even `!=`,
and an absent cell matches only `missing`.
So `{"column": "age", "cmp": "!=", "value": 5}` skips rows with no `age`
and rows whose `age` is a string.

### Rows

Without `agg`, the response lists matching rows in scan order:

```json
{"rows": [{"key": "ann", "row": {"age": 31, "name": "Ann"}}]}
```

With `select`, each row holds only the selected columns,
and a row with none of them is left out.
Predicates may test columns that are not selected.
No matching rows gives `{"rows": []}`.

### Aggregates

With `agg`, the response is one value:

```json
{"value": 2}
```

| `agg` | Result | No rows to fold |
|-------|--------|-----------------|
| `count` | Number of matching rows | `0` |
| `sum` | Sum of `column` over matching rows that have it | `0` |
| `avg` | `sum` divided by the number of rows that have `column` | `404` |
| `min` | Smallest value of `column` | `404` |
| `max` | Largest value of `column` | `404` |

`sum` and `avg` need numbers; any other type in `column` is `400`.
`min` and `max` work on any one type; mixed types are `400`.
`limit` and `reverse` apply before folding, so
`{"agg": "sum", "limit": 10, "reverse": true}` sums the last ten rows.
`select` is ignored by aggregates.

### Examples

Adults whose name starts with "A", names only:

```
POST /api/query
{"table": "users",
 "where": [{"column": "age", "cmp": ">=", "value": 18},
           {"column": "name", "cmp": "prefix", "value": "A"}],
 "select": ["name"]}
```

The ten latest events of one day, with row keys that are timestamps:

```
POST /api/query
{"table": "events", "from": "2026-11-26T00:00:00Z", "to": "2026-11-27T00:00:00Z",
 "reverse": true, "limit": 10}
```

Average order total:

```
POST /api/query
{"table": "orders", "agg": "avg", "column": "total"}
```

## POST /api/batch

Apply operations atomically and in order, in one transaction.
Either every operation takes effect or none does.
Later operations see the writes of earlier ones.
Writes.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `ops` | array | yes | Operations, applied in order |

Each operation:

| Field | Type | Used by | Meaning |
|-------|------|---------|---------|
| `op` | string | all | `set`, `delete`, `if` or `incr` |
| `table` | string | all | Table name |
| `row` | string | all but some deletes | Row key |
| `column` | string | all but some deletes | Column name |
| `value` | number, string or boolean | `set`, `if` | Value to store, or operand to compare with |
| `cmp` | string | `if` | Comparison, as in [predicates](#predicates) |
| `delta` | number | `incr` | Amount to add |

| `op` | Effect |
|------|--------|
| `set` | Store `value` in the cell, replacing what was there |
| `delete` | Delete; the scope depends on `row` and `column`, below |
| `if` | Abort the batch with `409` unless the cell satisfies `cmp value` |
| `incr` | Add `delta` to the number in the cell; a missing cell counts as `0` |

| `delete` with | Deletes |
|---------------|---------|
| `row` and `column` | One cell |
| `row` only | The whole row |
| `column` only | That column in every row |
| neither | **The whole table** |

Deleting something that does not exist is not an error.

Response:

```json
{"status": "ok"}
```

An invalid name, value or `op` anywhere in the batch is `400`
and nothing is written.
A failed `if` is `409` and nothing is written.
A `409` conflict, from another writer changing a cell this batch read
before it committed, is safe to retry.
A batch too large for one transaction is `413`; split it.

### Examples

Create a row:

```
POST /api/batch
{"ops": [
	{"op": "set", "table": "users", "row": "ann", "column": "name", "value": "Ann"},
	{"op": "set", "table": "users", "row": "ann", "column": "age", "value": 31}
]}
```

Compare and set: change the status only if it is still `pending`:

```
POST /api/batch
{"ops": [
	{"op": "if", "table": "orders", "row": "o17", "column": "status", "cmp": "=", "value": "pending"},
	{"op": "set", "table": "orders", "row": "o17", "column": "status", "value": "paid"}
]}
```

Insert only if absent:

```
POST /api/batch
{"ops": [
	{"op": "if", "table": "users", "row": "bob", "column": "name", "cmp": "missing"},
	{"op": "set", "table": "users", "row": "bob", "column": "name", "value": "Bob"}
]}
```

Move stock between two rows:

```
POST /api/batch
{"ops": [
	{"op": "if", "table": "stock", "row": "a", "column": "n", "cmp": ">=", "value": 5},
	{"op": "incr", "table": "stock", "row": "a", "column": "n", "delta": -5},
	{"op": "incr", "table": "stock", "row": "b", "column": "n", "delta": 5}
]}
```

## POST /api/incr

Atomically add `delta` to a numeric cell and return the new value.
A missing cell counts as `0`.
Concurrent increments of the same cell are retried by the server,
so none are lost.
Writes.

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `table` | string | yes | Table name |
| `row` | string | yes | Row key |
| `column` | string | yes | Column name |
| `delta` | number | yes | Amount to add; may be negative or fractional |

```
POST /api/incr
{"table": "counters", "row": "home", "column": "hits", "delta": 1}
```

```json
{"value": 42}
```

A cell that holds a string or boolean is `400`.
Use `incr` inside a [batch](#post-apibatch) to combine it with conditions or other writes.
