# Quicktable

A wide-column database written in Go, inspired by Bigtable.

A store holds tables, a table holds rows named by a key,
and a row holds cells named by a column.
Rows need not share columns,
and a table or row exists only while it holds a cell.
A cell holds a number, string or boolean.
Times are strings in fixed-width UTC, such as `2026-11-26T10:00:00Z`;
these sort like the instants they name,
so row key and value ranges select time ranges.
Data is kept on disk by the Mustang engine, built on Badger,
or in memory.

## Build

	go build -o quicktable ./cmd/server
	go build -o qt ./cmd/client

A container image is described in [build/Containerfile](./build/Containerfile),
and deployments for [Compose](./deployments/compose/compose.yaml)
and [Kubernetes](./deployments/kubernetes/quicktable.yaml)
live in [deployments](./deployments).

## Server

The server offers the same six operations over REST and MCP:
scan, get, values, query, batch and incr.

	quicktable -t secret

| Flag | Default | Meaning |
|------|---------|---------|
| `-a` | `:16000` | Listen address |
| `-d` | `./data` | Storage directory |
| `-m` | | Keep the store in memory only |
| `-t` | | Bearer token required on every request |
| `-v` | | Debug logging |

	curl -H 'Authorization: Bearer secret' \
		-d '{"table":"users","row":"ann"}' \
		localhost:16000/api/get

## Shell

`qt` opens a store directly and reads commands
from a terminal or a script.

	$ qt -m
	: set users ann name "Ann A" age 31
	: count users where age >= 18
	1

A store cannot be opened by the server and `qt` at the same time.

## Documentation

- [REST API](./docs/rest.md)
- [MCP](./docs/mcp.md)
- [qt(1)](./docs/qt.1), read with `man ./docs/qt.1`

## License

See [LICENSE](./LICENSE).
