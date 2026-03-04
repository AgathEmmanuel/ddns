package dns

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/agath/ddns/internal/health"
	"github.com/miekg/dns"
)

// Server wraps a miekg/dns server with the split-horizon handler.
type Server struct {
	addr     string
	srv      *dns.Server
	handler  *Handler
	dataDir  string
	resolvBk string // path to resolv.conf backup
}

// NewServer creates a DNS resolver server.
func NewServer(addr string, monitor *health.Monitor, upstreamAddr string, resolver Resolver, dataDir string) *Server {
	fwd := newUpstreamForwarder(upstreamAddr)
	handler := NewHandler(monitor, fwd, resolver)
	mux := dns.NewServeMux()
	mux.HandleFunc(".", handler.ServeDNS)
	return &Server{
		addr:    addr,
		handler: handler,
		dataDir: dataDir,
		srv: &dns.Server{
			Addr:    addr,
			Net:     "udp",
			Handler: mux,
		},
	}
}

// Start launches the DNS server and redirects /etc/resolv.conf to point at us.
func (s *Server) Start() error {
	if err := s.backupAndSetResolv(); err != nil {
		slog.Warn("dns: could not redirect /etc/resolv.conf (may need root)", "err", err)
	}
	go func() {
		if err := s.srv.ListenAndServe(); err != nil {
			slog.Error("dns: server stopped", "err", err)
		}
	}()
	slog.Info("dns: resolver started", "addr", s.addr)
	return nil
}

// Stop shuts down the server and restores /etc/resolv.conf.
func (s *Server) Stop() {
	s.srv.Shutdown()
	s.restoreResolv()
}

// backupAndSetResolv saves the current resolv.conf and writes ours.
func (s *Server) backupAndSetResolv() error {
	if err := os.MkdirAll(s.dataDir, 0700); err != nil {
		return err
	}
	// Read current resolv.conf.
	orig, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("read resolv.conf: %w", err)
	}
	// Save backup.
	s.resolvBk = filepath.Join(s.dataDir, "resolv.conf.bak")
	if err := os.WriteFile(s.resolvBk, orig, 0644); err != nil {
		return fmt.Errorf("backup resolv.conf: %w", err)
	}
	// Extract host from DNS addr (strip port if present).
	host := s.addr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		host = "127.0.0.1"
	}
	newResolv := fmt.Sprintf("# Managed by ddns — original backed up to %s\nnameserver %s\n", s.resolvBk, host)
	if err := os.WriteFile("/etc/resolv.conf", []byte(newResolv), 0644); err != nil {
		return fmt.Errorf("write resolv.conf: %w", err)
	}
	slog.Info("dns: redirected /etc/resolv.conf", "nameserver", host, "backup", s.resolvBk)
	return nil
}

// restoreResolv restores the original /etc/resolv.conf from backup.
func (s *Server) restoreResolv() {
	if s.resolvBk == "" {
		return
	}
	orig, err := os.ReadFile(s.resolvBk)
	if err != nil {
		slog.Warn("dns: could not read resolv.conf backup", "path", s.resolvBk, "err", err)
		return
	}
	if err := os.WriteFile("/etc/resolv.conf", orig, 0644); err != nil {
		slog.Warn("dns: could not restore resolv.conf", "err", err)
		return
	}
	slog.Info("dns: restored /etc/resolv.conf")
}

// RestoreResolvFromBackup restores resolv.conf from backup on next startup
// (crash recovery — should be called before Start).
func RestoreResolvFromBackup(dataDir string) {
	bkPath := filepath.Join(dataDir, "resolv.conf.bak")
	orig, err := os.ReadFile(bkPath)
	if err != nil {
		return // No backup, nothing to restore.
	}
	current, _ := os.ReadFile("/etc/resolv.conf")
	if strings.Contains(string(current), "Managed by ddns") {
		if err := os.WriteFile("/etc/resolv.conf", orig, 0644); err != nil {
			slog.Warn("dns: crash recovery: could not restore resolv.conf", "err", err)
			return
		}
		slog.Info("dns: crash recovery: restored /etc/resolv.conf from backup")
	}
}
