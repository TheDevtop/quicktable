# Quicktable REST API

The server stores cells addressed by **table**, **row** and **column**, and offers six operations. Each one is served at `POST /api/<name>` with a JSON body, and as an [MCP tool](mcp.md) with the same arguments and results:

| Endpoint | Purpose |
|----------|---------|
| [`POST /api/scan`](#scan) | List tables, the rows of a table, or the columns of a table |
| [`POST /api/get`](#get) | Read a table, a row, a column or one value |
| [`POST /api/values`](#values) | Read many cells from one snapshot |
| [`POST /api/query`](#query) | Filter rows, or count, sum, average, min or max them |
| [`POST /api/batch`](#batch) | Write, delete and check cells atomically |
| [`POST /api/incr`](#incr) | Atomically add to a number and return the result |

[`GET /health`](#health) is the only other endpoint. Every response body is JSON.

**Authentication:** every request except `GET /health` must carry the server's token (set with the server's `-t` flag) as a bearer token:

```
Authorization: Bearer <token>
```

A request with a missing, malformed or wrong token is rejected with **401 Unauthorized** and a `WWW-Authenticate: Bearer` header before anything else is checked. The error never repeats the token that was sent.

**Request bodies:** unknown fields are rejected with **400**, so a misspelled `row` cannot turn a row delete into a table drop. An empty body counts as `{}`.

**Names:** table names, row keys and column names are any non-empty string without whitespace or NUL bytes. There is no fixed key format, and the server does not generate keys.

**Values:** a cell holds a JSON number, string or boolean. Numbers are stored as 64-bit floats, so integers above 2^53 lose precision. `null`, arrays and objects are rejected with **400**. There is no time type: store times as strings in fixed-width UTC, such as `2026-11-26T10:00:00Z`, and they sort in time order. Keep the width fixed: zone offsets, or fractional seconds whose trailing zeros are dropped, break the order.

A table, row or column is not stored on its own. It exists only while it holds at least one cell, so deleting its last cell removes it.

---

## Errors

Errors return a JSON body with a single `"error"` key:

```json
{ "error": "mustang: not found" }
```

| HTTP status | Cause |
|-------------|-------|
| 400 Bad Request | Malformed JSON, unknown field, invalid name, unsupported value type, unknown `cmp`, `op` or `agg`, or a cell of the wrong type for `incr`, `sum` or `avg` |
| 401 Unauthorized | Missing or invalid bearer token |
| 404 Not Found | The table, row, column or cell being read does not exist |
| 409 Conflict | A batch `if` check failed, or a concurrent write to the same cells conflicted. Nothing was written. A conflict can be retried |
| 413 Request Entity Too Large | The batch does not fit in one transaction. Nothing was written |
| 500 Internal Server Error | Unexpected storage error |

Any other method on an `/api/` path returns **405 Method Not Allowed**.

---

## Health

```
GET /health
```

Returns `200` whenever the server is accepting requests. Use it for liveness and readiness probes. It needs no token.

```json
{ "status": "ok" }
```

---

## Scan

```
POST /api/scan
```

Lists names. The fields given decide what is listed:

| Body | Lists |
|------|-------|
| `{}` | All table names, sorted |
| `{"table": "users"}` | The row keys of `users`, sorted |
| `{"table": "users", "columns": true}` | The column names used in `users` |

Column names come in the order they are first seen when walking the table by row key, not sorted. Listing columns reads every key of the table, so it gets slower as the table grows. `columns` without `table` is rejected with **400**.

A table that does not exist gives an empty list, not an error.

**Response 200**

```json
{ "names": ["alice", "bob"] }
```

---

## Get

```
POST /api/get
```

Reads part of one table. Which of `row` and `column` are given decides the scope, and the key of the response says what came back:

| Body | Response |
|------|----------|
| `{"table": "users"}` | `{"table": {rowKey: {column: value}}}`, the whole table |
| `{"table": "users", "row": "alice"}` | `{"row": {column: value}}`, one row |
| `{"table": "users", "column": "age"}` | `{"column": {rowKey: value}}`, one column across the rows that have it |
| `{"table": "users", "row": "alice", "column": "age"}` | `{"value": value}`, one cell |

If nothing is found, the response is **404**.

**Examples**

```json
{ "table": "users", "row": "alice" }
```

```json
{ "row": { "age": 30, "email": "alice@example.com" } }
```

```json
{ "table": "users", "column": "age" }
```

```json
{ "column": { "alice": 30, "bob": 25 } }
```

---

## Values

```
POST /api/values
```

Reads many cells, possibly from different tables, from one consistent snapshot. Each cell needs `table`, `row` and `column`. The values come back in the order of `cells`, with `null` for a missing cell.

**Body**

```json
{
  "cells": [
    { "table": "users", "row": "alice", "column": "email" },
    { "table": "users", "row": "bob", "column": "email" }
  ]
}
```

**Response 200**

```json
{ "values": ["alice@example.com", null] }
```

---

## Query

```
POST /api/query
```

Scans the rows of a table in ascending row key order, or descending with `reverse`, and returns those that match. With `agg`, it returns one value computed over the matching rows instead.

**Body fields**

| Field | Type | Description |
|-------|------|-------------|
| `table` | string | **Required.** Table to scan |
| `prefix` | string | Only row keys that start with this prefix |
| `from` | string | Only row keys `>=` this key |
| `to` | string | Only row keys `<` this key |
| `where` | Predicate[] | Predicates that must all hold |
| `select` | string[] | Columns to return. All columns if omitted |
| `limit` | int | Stop after this many rows. `0` means no limit |
| `reverse` | bool | Scan in descending row key order, so `limit` keeps the last rows |
| `agg` | string | Return `count`, `sum`, `avg`, `min` or `max` instead of rows |
| `column` | string | Column for `sum`, `avg`, `min` and `max` |

Row keys compare byte by byte. As in Bigtable, `select` filters cells, and a row left with no cells is not returned: a row that has none of the selected columns is dropped. `limit` counts only the rows that are returned. To find rows that lack a column, use a `missing` predicate.

For time ranges, put the time last in the row key, as in `sensor1/2026-11-26T10:00:00Z`. A shortened time is a valid bound: `"from": "sensor1/2026-11-26", "to": "sensor1/2026-11-27"` selects one day, and `"prefix": "sensor1/", "reverse": true, "limit": 1` reads the latest row without scanning the rest.

**Predicate**

```json
{ "column": "age", "cmp": ">=", "value": 18 }
```

| `cmp` | Holds when |
|-------|------------|
| `=` `!=` `<` `<=` `>` `>=` | The cell compares to `value` as stated |
| `prefix` | The string cell starts with `value` |
| `contains` | The string cell contains `value` |
| `exists` | The cell is present. `value` is not used |
| `missing` | The cell is absent. `value` is not used |

Any other `cmp` is rejected with **400**, as is a `prefix` or `contains` with a value that is not a string.

Numbers compare numerically, strings byte by byte, booleans with `false` before `true`. Only `missing` matches an absent cell. A cell of a different type than `value` never matches, **including under `!=`**. For example, `{"column": "age", "cmp": "!=", "value": 30}` skips rows without `age` and rows where `age` is a string.

**Example request**

```json
{
  "table": "users",
  "where": [ { "column": "age", "cmp": ">=", "value": 18 } ],
  "select": ["email"],
  "limit": 10
}
```

**Response 200**

`rows` is in row key order, descending with `reverse`. Each item holds the row's `key` and its selected columns in `row`.

```json
{
  "rows": [
    { "key": "alice", "row": { "email": "alice@example.com" } }
  ]
}
```

### Aggregates

With `agg`, `select` is ignored and the response is a single value:

```json
{ "table": "users", "agg": "avg", "column": "age" }
```

```json
{ "value": 27.5 }
```

| `agg` | Result |
|-------|--------|
| `count` | Number of matching rows. `column` is not used |
| `sum` | Sum of `column` over the matching rows that have it. `0` if none do |
| `avg` | `sum` divided by the number of matching rows that have `column`. **404** if none do |
| `min`, `max` | Smallest or largest value of `column`, of any type. **404** if no matching row has it |

`limit` applies to aggregates as well. For `count` it counts every matching row. For the others it counts only matching rows that have `column`, so `{"agg": "sum", "column": "age", "limit": 10}` sums the first ten `age` values. `sum` and `avg` reject a non-number value with **400**. `min` and `max` reject a column that mixes types with **400**.

---

## Batch

```
POST /api/batch
```

Applies a list of operations atomically and in order, in one transaction. Either all operations take effect or none do. Later operations see the effects of earlier ones, so an `if` can check a cell that an earlier `set` wrote.

**Body**

```json
{
  "ops": [
    { "op": "if", "table": "posts", "row": "p1", "column": "version", "cmp": "=", "value": 3 },
    { "op": "set", "table": "posts", "row": "p1", "column": "version", "value": 4 },
    { "op": "set", "table": "posts", "row": "p1", "column": "status", "value": "published" },
    { "op": "delete", "table": "posts", "row": "p1", "column": "draft" },
    { "op": "incr", "table": "stats", "row": "posts", "column": "published", "delta": 1 }
  ]
}
```

**Operations**

| `op` | Fields | Effect |
|------|--------|--------|
| `set` | `table`, `row`, `column`, `value` | Write a cell, creating it if needed |
| `delete` | `table`, and optionally `row` and `column` | Delete a cell, row, column or table (see below) |
| `if` | `table`, `row`, `column`, `cmp`, `value` | Check a cell with a [predicate](#query). If it fails, the batch is aborted with **409** and nothing is written |
| `incr` | `table`, `row`, `column`, `delta` | Add `delta` to a number, a missing cell counting as 0 |

Fields that an operation does not use are ignored.

`delete` picks its scope from the fields given, the same way as [Get](#get):

| Fields | Deletes |
|--------|---------|
| `row` and `column` | One cell |
| `row` only | The whole row |
| `column` only | That column in every row of the table |
| neither | **The whole table.** This cannot be undone |

Deleting something that does not exist is not an error.

A batch that does not fit in one transaction fails with **413**. The error names the cell of the failing `if` as `table/row/column`. If another writer changes a cell this batch read before it commits, the batch fails with **409** and can be retried. An empty `ops` list does nothing.

**Response 200**

```json
{ "status": "ok" }
```

**Response 409**, when an `if` failed:

```json
{ "error": "mustang: condition failed: posts/p1/version" }
```

---

## Incr

```
POST /api/incr
```

Atomically adds `delta` to a numeric cell and returns the new value. A missing cell counts as 0. A cell that holds something other than a number is rejected with **400**, as is a `delta` that is not a JSON number. A `delta` that is left out counts as 0.

If concurrent writes conflict, the server retries them itself, so a `409` is rare.

**Body**

```json
{ "table": "users", "row": "alice", "column": "score", "delta": 5 }
```

**Response 200**

```json
{ "value": 105 }
```
