package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// mysqlColumns is the MariaDB schema the importer knows how to copy, as
// table.column:type:nullable. Anything else (a new table or column, a changed type) stops the
// import, so nothing can be left behind unnoticed.
var mysqlColumns = []string{
	"bot_method_invalid.method:varchar:NO",
	"code_errors.code:int:NO",
	"error_descriptions.description:varchar:YES",
	"error_descriptions.error:varchar:NO",
	"errors.code:int:NO",
	"errors.error:varchar:NO",
	"errors.method:varchar:NO",
	"settings.key:varchar:NO",
	"settings.value:int:NO",
}

// importMySQL copies the MariaDB database at dsn into a new SQLite database at dbPath:
// every row of every table, read in one consistent snapshot (MariaDB isn't written to). The
// copy is written next to dbPath, then read back from disk and compared with what MariaDB
// returned (row counts and hashes); only then is it linked to dbPath. An existing dbPath is
// never touched.
func importMySQL(ctx context.Context, dsn, dbPath string) ([]TableSum, error) {
	if _, err := os.Lstat(dbPath); !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s already exists: not overwriting it", dbPath)
	}
	tmp := dbPath + ".import"
	for _, p := range []string{tmp, tmp + "-wal", tmp + "-shm", tmp + "-journal"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("MYSQL_DSN: %w", err)
	}
	cfg.ParseTime = false
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	my := sql.OpenDB(connector)
	defer my.Close()
	conn, err := my.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("MariaDB: %w", err)
	}
	defer conn.Close()
	for _, q := range []string{
		"SET NAMES utf8mb4",
		"SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ",
		"START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY",
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("MariaDB: %s: %w", q, err)
		}
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")

	if err := checkMySQLSchema(ctx, conn); err != nil {
		return nil, err
	}

	st, err := CreateStore(tmp)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sums []TableSum
	for _, t := range tables {
		var count int64
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t.name).Scan(&count); err != nil {
			return nil, fmt.Errorf("MariaDB: count %s: %w", t.name, err)
		}
		// A plain INSERT: two rows MariaDB considers distinct but SQLite's NOCASE doesn't
		// (another accent, trailing spaces) stop the import instead of one replacing the other.
		insert, err := tx.PrepareContext(ctx, "INSERT INTO "+t.name+" ("+strings.Join(t.cols, ", ")+") VALUES (?"+strings.Repeat(", ?", len(t.cols)-1)+")")
		if err != nil {
			return nil, err
		}
		sum, err := sumTable(ctx, conn, t, true, func(vals []any) error {
			_, err := insert.ExecContext(ctx, vals...)
			return err
		})
		insert.Close()
		if err != nil {
			return nil, fmt.Errorf("copy: %w", err)
		}
		if sum.Rows != count {
			return nil, fmt.Errorf("%s: read %d rows, MariaDB counts %d", t.name, sum.Rows, count)
		}
		log.Printf("copied %s", sum)
		sums = append(sums, sum)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// A single file, no WAL next to it, before checking and linking.
	if _, err := st.db.ExecContext(ctx, "PRAGMA journal_mode=DELETE"); err != nil {
		return nil, err
	}
	if err := st.Close(); err != nil {
		return nil, err
	}

	if err := checkCopy(ctx, tmp, sums); err != nil {
		return nil, fmt.Errorf("check of the copy failed (%s kept for inspection): %w", tmp, err)
	}
	if err := syncPath(tmp); err != nil {
		return nil, err
	}
	if err := os.Link(tmp, dbPath); err != nil { // fails if dbPath appeared meanwhile
		return nil, err
	}
	if err := os.Remove(tmp); err != nil {
		return nil, err
	}
	if err := syncPath(filepath.Dir(dbPath)); err != nil {
		return nil, err
	}
	return sums, nil
}

func checkMySQLSchema(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT t.table_name, c.column_name, c.data_type, c.is_nullable
		FROM information_schema.tables t
		LEFT JOIN information_schema.columns c ON c.table_schema = t.table_schema AND c.table_name = t.table_name
		WHERE t.table_schema = DATABASE()`)
	if err != nil {
		return fmt.Errorf("MariaDB schema: %w", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var tbl string
		var col, typ, null sql.NullString
		if err := rows.Scan(&tbl, &col, &typ, &null); err != nil {
			return err
		}
		got = append(got, tbl+"."+col.String+":"+typ.String+":"+null.String)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// information_schema sorts in its own collation: compare as sets.
	slices.Sort(got)
	if !slices.Equal(got, mysqlColumns) {
		return fmt.Errorf("MariaDB schema is not the expected one, refusing to import:\n  got  %v\n  want %v", got, mysqlColumns)
	}
	return nil
}

// checkCopy opens the copy afresh and compares it with what was read from MariaDB.
func checkCopy(ctx context.Context, path string, want []TableSum) error {
	st, err := OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer st.Close()
	var mode string
	if err := st.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if mode != "delete" {
		return fmt.Errorf("journal_mode %s, want delete", mode)
	}
	var integrity string
	if err := st.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity_check: %s", integrity)
	}
	got, err := st.Sums(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("copy differs from MariaDB:\n  got  %v\n  want %v", got, want)
	}
	return nil
}

func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
