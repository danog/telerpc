// telerpc is the API that collects the RPC errors Telegram returns to MadelineProto
// (report-rpc-error.madelineproto.xyz), and builds rpc-db's JSON files from them.
//
//	telerpc serve          the API on LISTEN (unix socket: Caddy, h2c; TCP: HTTP/1.1 and h2c)
//	telerpc maintain [dir] clean up the database, ask for missing descriptions, write rpc-db's
//	                       files into dir (./data, the rpc-db submodule)
//	telerpc export [dir]   just write rpc-db's files
//	telerpc import-mysql   one-time copy of the MariaDB database (MYSQL_DSN) to DATA_DIR
//	telerpc verify         each table's row count and hash, as `mariadb -N -B ... | sha256sum`
//	telerpc init           create an empty database
//
// Environment: DATA_DIR (/data; the database is telerpc.db in it), LISTEN (0.0.0.0:1337),
// TELERPC_AUTH_TOKEN (serve), TL_DIR (maintain, export: MadelineProto's src directory, for
// its TL schemas; downloaded from GitHub if not set).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("%s is not set", key)
	}
	return v
}

func main() {
	log.SetFlags(0)
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	dataDir := env("DATA_DIR", "/data")
	dbPath := filepath.Join(dataDir, "telerpc.db")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, dataDir, dbPath)
	case "maintain", "export":
		dir := "data"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		err = runExport(ctx, cmd == "maintain", dataDir, dbPath, dir)
	case "import-mysql":
		var sums []TableSum
		sums, err = importMySQL(ctx, required("MYSQL_DSN"), dbPath)
		if err == nil {
			log.Printf("imported into %s, checked against MariaDB:", dbPath)
			for _, s := range sums {
				log.Print(s)
			}
		}
	case "verify":
		err = verify(ctx, dataDir, dbPath)
	case "init":
		var st *Store
		if st, err = CreateStore(dbPath); err == nil {
			err = st.Close()
			log.Printf("created %s", dbPath)
		}
	default:
		err = fmt.Errorf("unknown command %q (serve, maintain, export, import-mysql, verify, init)", cmd)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, dataDir, dbPath string) error {
	auth := required("TELERPC_AUTH_TOKEN")
	st, err := OpenStore(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	addr := env("LISTEN", "0.0.0.0:1337")
	l, err := listen(addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// From here on: files only under the data directory, no connections but DNS.
	if err := sandbox(nil, []string{dataDir}); err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           &Handler{store: st, auth: auth},
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	if strings.HasPrefix(addr, "/") {
		// Only Caddy connects to the socket, with h2c.
		h2cOnly(srv)
	} else {
		var p http.Protocols
		p.SetHTTP1(true)
		p.SetUnencryptedHTTP2(true)
		srv.Protocols = &p
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	log.Printf("serving on %s", addr)
	select {
	case err = <-errc:
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = srv.Shutdown(shutdown)
		cancel()
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// runExport runs maintain (interactive) or export, writing into dir.
func runExport(ctx context.Context, interactive bool, dataDir, dbPath, dir string) error {
	st, err := OpenStore(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s: not a directory (rpc-db's checkout)", dir)
	}
	layer, err := st.Layer(ctx)
	if err != nil {
		return err
	}
	schemas, err := loadSchemas(ctx, os.Getenv("TL_DIR"), layer)
	if err != nil {
		return err
	}
	// From here on: files only under the data and output directories, no connections.
	if err := sandbox(nil, []string{dataDir, dir}); err != nil {
		return err
	}
	if interactive {
		return maintain(ctx, st, schemas, dir, os.Stdin, os.Stdout)
	}
	return export(ctx, st, schemas, dir, os.Stdout)
}

func verify(ctx context.Context, dataDir, dbPath string) error {
	st, err := OpenReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	// Read-write: reading a WAL database updates its -shm file.
	if err := sandbox(nil, []string{dataDir}); err != nil {
		return err
	}
	sums, err := st.Sums(ctx)
	if err != nil {
		return err
	}
	for _, s := range sums {
		fmt.Println(s)
	}
	return nil
}
