package main

// Listening and sandboxing, shared (as copies) by the Go daemons behind Caddy:
// filters, capi and wifibot. Keep the copies in sync.

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// listen opens the address Caddy proxies to. An absolute path is a unix socket, which
// only processes that can open its directory (Caddy, via a volume) can reach;
// anything else is a TCP address, for running the daemon by hand.
//
// The socket is mode 0660. Its directory is a setgid tmpfs shared only with Caddy
// (group root), so the socket belongs to that group and nobody else can connect.
func listen(addr string) (net.Listener, error) {
	if !strings.HasPrefix(addr, "/") {
		return net.Listen("tcp", addr)
	}
	if err := os.Remove(addr); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// No moment where the socket exists with wider permissions.
	old := syscall.Umask(0o117)
	l, err := net.Listen("unix", addr)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	// After sandbox() the socket can't be removed anyway: don't try on close.
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := os.Chmod(addr, 0o660); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// h2cOnly makes srv speak only HTTP/2 without TLS ("prior knowledge"), as Caddy's
// h2c transport does: one connection carries all concurrent requests.
func h2cOnly(srv *http.Server) {
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	srv.Protocols = &p
	if srv.HTTP2 == nil {
		srv.HTTP2 = &http.HTTP2Config{}
	}
	if srv.HTTP2.MaxConcurrentStreams == 0 {
		srv.HTTP2.MaxConcurrentStreams = 100
	}
}

// sandbox locks the process down with Landlock, for good: it can still read /etc
// (resolv.conf, hosts, the CA bundle) and the roDirs, write only the rwDirs, connect
// only to DNS and the given TCP ports, and no longer bind any port, signal other
// processes or use abstract unix sockets. Nothing may be created or removed next to
// the unix socket either, so a compromised daemon can't swap it for a symlink that
// sends Caddy's traffic somewhere else.
//
// Call it after listen and after reading everything else needed from the
// filesystem. It fails if the kernel doesn't support Landlock ABI 10 (Linux 7.0+).
func sandbox(roDirs, rwDirs []string, tcpPorts ...uint16) error {
	// Go loads the CA bundle on first use: make that happen now.
	if _, err := x509.SystemCertPool(); err != nil {
		return fmt.Errorf("CA bundle: %w", err)
	}
	rules := []landlock.Rule{
		landlock.RODirs("/etc"),
		// DNS through Docker's resolver: UDP, falling back to TCP for long answers.
		landlock.ConnectSendUDP(53),
		landlock.BindUDP(0),
		landlock.ConnectTCP(53),
	}
	if len(roDirs) > 0 {
		rules = append(rules, landlock.RODirs(roDirs...))
	}
	if len(rwDirs) > 0 {
		rules = append(rules, landlock.RWDirs(rwDirs...).WithRefer())
	}
	for _, p := range tcpPorts {
		rules = append(rules, landlock.ConnectTCP(p))
	}
	if err := landlock.V10.Restrict(rules...); err != nil {
		return fmt.Errorf("landlock: %w", err)
	}
	// Landlock's port rules don't cover Multipath TCP sockets: they must be unavailable
	// (sysctl net.mptcp.enabled=0 in the container's network namespace).
	if mptcpAvailable() && os.Getenv("ALLOW_MPTCP") != "true" {
		return errors.New("Multipath TCP is enabled, which would bypass the port rules: set sysctl net.mptcp.enabled=0 (or ALLOW_MPTCP=true when testing)")
	}
	return nil
}

// mptcpAvailable reports whether this process can open Multipath TCP sockets.
func mptcpAvailable() bool {
	const ipprotoMPTCP = 262
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, ipprotoMPTCP)
	if err != nil {
		return false
	}
	syscall.Close(fd)
	return true
}
