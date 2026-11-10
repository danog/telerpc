package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// The same five tables as the MariaDB "rpc" database, with the same columns, keys and rows.
// Text keys compare without case, as MariaDB's utf8mb4_unicode_ci did (FLOOD_WAIT_%d and
// flood_wait_%d are one error), and have its lengths (varchar(255), description 1024).
//
// errors: every (error, method, code) reported; error_descriptions: their human-readable
// descriptions; bot_method_invalid: the methods bots can't call; code_errors: the codes
// reported as errors; settings: the layer the errors were collected for.
const schema = `
CREATE TABLE errors (
	error TEXT NOT NULL COLLATE NOCASE CHECK (length(error) <= 255),
	method TEXT NOT NULL COLLATE NOCASE CHECK (length(method) <= 255),
	code INTEGER NOT NULL CHECK (code BETWEEN -2147483648 AND 2147483647),
	PRIMARY KEY (error, method, code)
) STRICT;
CREATE TABLE error_descriptions (
	error TEXT NOT NULL PRIMARY KEY COLLATE NOCASE CHECK (length(error) <= 255),
	description TEXT CHECK (length(description) <= 1024)
) STRICT;
CREATE TABLE bot_method_invalid (
	method TEXT NOT NULL PRIMARY KEY COLLATE NOCASE CHECK (length(method) <= 255)
) STRICT;
CREATE TABLE code_errors (
	code INTEGER NOT NULL PRIMARY KEY CHECK (code BETWEEN -2147483648 AND 2147483647)
) STRICT;
CREATE TABLE settings (
	key TEXT NOT NULL PRIMARY KEY COLLATE NOCASE CHECK (length(key) <= 255),
	value INTEGER NOT NULL CHECK (value BETWEEN -2147483648 AND 2147483647)
) STRICT;
`

// column kinds, for listing rows as the mariadb client prints them.
const (
	colInt = iota
	colText
	colNullText
)

// table describes how to list a table's rows, in the same order and text format as
// `mariadb -N -B -e 'SELECT <cols> FROM <name> ORDER BY <myOrder>'`, so the two can be
// compared by hash. Text sorts by code point on both sides (utf8mb4_bin, SQLite's BINARY).
type table struct {
	name    string
	cols    []string
	kinds   []int
	order   string // SQLite
	myOrder string // MariaDB
}

var tables = []table{
	{name: "bot_method_invalid", cols: []string{"method"}, kinds: []int{colText},
		order: "method COLLATE BINARY", myOrder: "method COLLATE utf8mb4_bin"},
	{name: "code_errors", cols: []string{"code"}, kinds: []int{colInt},
		order: "code", myOrder: "code"},
	{name: "error_descriptions", cols: []string{"error", "description"}, kinds: []int{colText, colNullText},
		order: "error COLLATE BINARY", myOrder: "error COLLATE utf8mb4_bin"},
	{name: "errors", cols: []string{"error", "method", "code"}, kinds: []int{colText, colText, colInt},
		order: "error COLLATE BINARY, method COLLATE BINARY, code", myOrder: "error COLLATE utf8mb4_bin, method COLLATE utf8mb4_bin, code"},
	{name: "settings", cols: []string{"`key`", "value"}, kinds: []int{colText, colInt},
		order: "`key` COLLATE BINARY", myOrder: "`key` COLLATE utf8mb4_bin"},
}

// Store is the database: one SQLite file.
type Store struct {
	db *sql.DB
}

func dsn(path string) string {
	// WAL: reports are written while exports read. FULL sync: a report is on disk before it's
	// answered. IMMEDIATE transactions take the write lock up front, so concurrent writers
	// wait (busy_timeout) instead of failing.
	return "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)" +
		"&_pragma=busy_timeout(60000)&_pragma=temp_store(MEMORY)&_txlock=immediate"
}

// OpenStore opens an existing database. It never creates one: a missing file means a wrong
// mount or a migration not done yet, and recording into a fresh empty file would hide that.
func OpenStore(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s does not exist: import it first (import-mysql) or create it (init)", path)
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name IN ('errors', 'error_descriptions', 'bot_method_invalid', 'code_errors', 'settings')").Scan(&n); err != nil {
		db.Close()
		return nil, err
	}
	if n != 5 {
		db.Close()
		return nil, fmt.Errorf("%s: not a telerpc database", path)
	}
	return &Store{db: db}, nil
}

// OpenReadOnly opens an existing database for reading only (for checks: it can't change it).
func OpenReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(60000)&_pragma=temp_store(MEMORY)")
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// CreateStore creates a new, empty database. It fails if the file already exists.
func CreateStore(path string) (*Store, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Description returns an error's description: found is false if it has none recorded,
// desc.Valid is false if it's recorded as NULL.
func (s *Store) Description(ctx context.Context, err string) (desc sql.NullString, found bool, e error) {
	e = s.db.QueryRowContext(ctx, "SELECT description FROM error_descriptions WHERE error = ?", err).Scan(&desc)
	if errors.Is(e, sql.ErrNoRows) {
		return desc, false, nil
	}
	return desc, e == nil, e
}

// Record adds an error of a method.
func (s *Store) Record(ctx context.Context, err, method string, code int64) error {
	_, e := s.db.ExecContext(ctx, "REPLACE INTO errors (error, method, code) VALUES (?, ?, ?)", err, method, code)
	return e
}

// RecordCode adds a code reported as an error.
func (s *Store) RecordCode(ctx context.Context, code int64) error {
	_, e := s.db.ExecContext(ctx, "REPLACE INTO code_errors (code) VALUES (?)", code)
	return e
}

// RecordBotMethodInvalid adds a method bots can't call.
func (s *Store) RecordBotMethodInvalid(ctx context.Context, method string) error {
	_, e := s.db.ExecContext(ctx, "REPLACE INTO bot_method_invalid (method) VALUES (?)", method)
	return e
}

// Layer is the layer the errors were collected for (set by MadelineProto's CI).
func (s *Store) Layer(ctx context.Context) (int64, error) {
	var layer int64
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE `key` = 'layer'").Scan(&layer)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("Layer is not configured")
	}
	return layer, err
}

func (s *Store) SetLayer(ctx context.Context, layer int64) error {
	_, err := s.db.ExecContext(ctx, "REPLACE INTO settings (`key`, value) VALUES ('layer', ?)", layer)
	return err
}

// Cleanup removes the reports that need the auth token, before MadelineProto's CI collects
// them again.
func (s *Store) Cleanup(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range errorsRequireAuth {
		if _, err := tx.ExecContext(ctx, "DELETE FROM errors WHERE error = ?", e); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM bot_method_invalid"); err != nil {
		return err
	}
	return tx.Commit()
}

// errorRow is a row of errors.
type errorRow struct {
	Error, Method string
	Code          int64
}

// Errors lists the errors table, in key order.
func (s *Store) Errors(ctx context.Context) ([]errorRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT error, method, code FROM errors ORDER BY error, method, code")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []errorRow
	for rows.Next() {
		var r errorRow
		if err := rows.Scan(&r.Error, &r.Method, &r.Code); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// descRow is a row of error_descriptions.
type descRow struct {
	Error       string
	Description sql.NullString
}

// Descriptions lists error_descriptions, in key order.
func (s *Store) Descriptions(ctx context.Context) ([]descRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT error, description FROM error_descriptions ORDER BY error")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []descRow
	for rows.Next() {
		var r descRow
		if err := rows.Scan(&r.Error, &r.Description); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BotMethodsInvalid lists bot_method_invalid, in key order.
func (s *Store) BotMethodsInvalid(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT method FROM bot_method_invalid ORDER BY method")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) exec(ctx context.Context, q string, args ...any) error {
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// TableSum is a table's row count and the SHA-256 of its rows as `mariadb -N -B` prints them.
type TableSum struct {
	Table string
	Rows  int64
	Hash  string
}

func (t TableSum) String() string {
	return fmt.Sprintf("%-20s %9d rows  sha256 %s", t.Table, t.Rows, t.Hash)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// sumTable lists t's rows from db (SQLite, or MariaDB when mysql is set) and returns their
// count and hash. With each, it also calls row, if not nil, with the row's values.
func sumTable(ctx context.Context, db querier, t table, mysql bool, row func([]any) error) (TableSum, error) {
	order := t.order
	if mysql {
		order = t.myOrder
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(t.cols, ", ")+" FROM "+t.name+" ORDER BY "+order)
	if err != nil {
		return TableSum{}, fmt.Errorf("%s: %w", t.name, err)
	}
	defer rows.Close()
	h := sha256.New()
	sum := TableSum{Table: t.name}
	ints := make([]int64, len(t.cols))
	strs := make([]sql.NullString, len(t.cols))
	dest := make([]any, len(t.cols))
	for i, k := range t.kinds {
		if k == colInt {
			dest[i] = &ints[i]
		} else {
			dest[i] = &strs[i]
		}
	}
	vals := make([]any, len(t.cols))
	var line []byte
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return TableSum{}, fmt.Errorf("%s: %w", t.name, err)
		}
		line = line[:0]
		for i, k := range t.kinds {
			if i > 0 {
				line = append(line, '\t')
			}
			switch {
			case k == colInt:
				line = strconv.AppendInt(line, ints[i], 10)
				vals[i] = ints[i]
			case !strs[i].Valid:
				if k != colNullText {
					return TableSum{}, fmt.Errorf("%s.%s: NULL in a NOT NULL column", t.name, t.cols[i])
				}
				line = append(line, "NULL"...)
				vals[i] = nil
			default:
				line = appendEscaped(line, strs[i].String)
				vals[i] = strs[i].String
			}
		}
		line = append(line, '\n')
		h.Write(line)
		sum.Rows++
		if row != nil {
			if err := row(vals); err != nil {
				return TableSum{}, fmt.Errorf("%s: %w", t.name, err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return TableSum{}, fmt.Errorf("%s: %w", t.name, err)
	}
	sum.Hash = hex.EncodeToString(h.Sum(nil))
	return sum, nil
}

// appendEscaped escapes a value as the mariadb client's batch mode does.
func appendEscaped(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b = append(b, `\\`...)
		case '\t':
			b = append(b, `\t`...)
		case '\n':
			b = append(b, `\n`...)
		case 0:
			b = append(b, `\0`...)
		default:
			b = append(b, c)
		}
	}
	return b
}

// Sums returns every table's count and hash, read in one transaction (a consistent view).
func (s *Store) Sums(ctx context.Context) ([]TableSum, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var out []TableSum
	for _, t := range tables {
		sum, err := sumTable(ctx, tx, t, false, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}
