# telerpc

API for reporting Telegram MTProto RPC errors: to report a new error use the `code`, `method`
and `error` GET/POST parameters to the main endpoint @ `https://report-rpc-error.madelineproto.xyz`.

For the resulting JSON DB of localized descriptions, see [rpc-db](https://github.com/danog/rpc-db).

Ported from PHP to Go: one static binary, an embedded SQLite database instead of MariaDB,
Landlock after startup (`serve`: files under `DATA_DIR` only, no connections). Same API,
same answers, same rpc-db files: checked against the PHP version on a snapshot of the
production database (every answer to the same requests, the database afterwards, and every
file `maintain` writes, byte for byte).

## The API

- `?error=…&code=…&method=…` (query string, or a urlencoded or multipart body): records the
  error (its numbers sanitized: `FLOOD_WAIT_5` is `FLOOD_WAIT_%d`) and answers its
  description, `{"ok":false,"description":"No description"}` (404) if it has none. Bad reports
  are refused (MadelineProto's `RPCErrorException::isBad`). `AUTH_KEY_UNREGISTERED`,
  `BOT_METHOD_INVALID`, `USER_BOT_REQUIRED`, `USER_BOT_INVALID` and
  `BUSINESS_CONNECTION_INVALID` are only recorded with `auth=<TELERPC_AUTH_TOKEN>`; without
  it, they get their description.
- `?auth=…&setLayer=N`: the layer the errors are collected for (MadelineProto's CI).
- `?auth=…&cleanup`: removes the reports that need the token, before the CI collects them again.

Errors and methods compare without case, as in MariaDB's `utf8mb4_unicode_ci`.

## Commands

| | |
|---|---|
| `telerpc serve` (default) | the API on `LISTEN` |
| `telerpc maintain [dir]` | the PHP version's `php server.php`: cleans up the database, asks for the descriptions it lacks, writes rpc-db's files into `dir` (default `data`, the rpc-db submodule) |
| `telerpc export [dir]` | just writes rpc-db's files |
| `telerpc import-mysql` | one-time copy from MariaDB (`MYSQL_DSN`), see below |
| `telerpc verify` | each table's row count and SHA-256, as `mariadb -N -B` would print them |
| `telerpc init` | create an empty database |

`serve` never creates the database: a missing `telerpc.db` stops it, so a wrong mount can't
silently start collecting into a new, empty one.

Environment: `DATA_DIR` (`/data`; the database is `telerpc.db` in it), `LISTEN` (an address and
port, `0.0.0.0:1337` by default, HTTP/1.1 and h2c; or the path of a unix socket, h2c only, for
a reverse proxy on the same host), `TELERPC_AUTH_TOKEN` (`serve`), `TL_DIR` (`maintain`,
`export`: a directory with MadelineProto's `TL_telegram_v<layer>.tl` and `TL_mtproto_v1.tl`; if
unset they're downloaded from MadelineProto's `v8` branch, for the database's layer).

Build: `CGO_ENABLED=0 go build`. The Dockerfile is the one daniil-nas builds with (its
`go-tools-build` image); `go test` needs no database server.

`maintain` in the container, writing into a checkout of rpc-db:

```sh
docker run --rm -it --sysctl net.mptcp.enabled=0 --user 10110:10110 \
  -v /media/storage/persist/rpc:/data -v ~/rpc-db:/out \
  127.0.0.1:5000/rpc:latest maintain /out
```

## Changes from the PHP version

- MariaDB is replaced by SQLite: no database server, no `db.php`.
- No `git pull` and `composer update` at every start: the image is rebuilt instead.
- The TL schemas (for `maintain`'s list of methods and its layer check) come from
  MadelineProto's repository instead of the installed MadelineProto.
- Reports with invalid UTF-8 are refused (400); MariaDB refused them with a 500.
- A report too long for a column still gets a 500, with SQLite's message instead of MariaDB's.
- `maintain` reads the rows in key order (MariaDB returned them in its own); the files are
  the same.

## Migration from MariaDB

`import-mysql` reads every table inside one `START TRANSACTION WITH CONSISTENT SNAPSHOT, READ
ONLY` (MariaDB is never written to), refuses if the schema has any table or column it doesn't
know, writes `telerpc.db.import`, then reopens that file read-only and checks
`integrity_check` and every table's row count and hash against what MariaDB returned. Only
then is it linked to `telerpc.db`, which it never overwrites. Rows MariaDB considers distinct
but SQLite's `NOCASE` doesn't stop the import.

```sh
docker run --rm --network none --user 10110:10110 \
  -v homedaniilit_sock_mariadb:/run/mysqld:ro -v /media/storage/persist/rpc:/data \
  -e MYSQL_DSN='rpc:<password>@unix(/run/mysqld/mysqld.sock)/rpc' \
  127.0.0.1:5000/rpc:latest import-mysql
# Compare with MariaDB's own client (must equal the hashes import-mysql printed), e.g.:
docker exec homedaniilit-mariadb-1 mariadb -urpc -p<password> -N -B rpc -e \
  'SELECT error, method, code FROM errors ORDER BY error COLLATE utf8mb4_bin, method COLLATE utf8mb4_bin, code' | sha256sum
```
