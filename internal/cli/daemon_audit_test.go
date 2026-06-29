package cli

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/audit"
)

// drainConn reads and discards everything written to one end of a net.Pipe so
// that ipc.SendError on the other end does not block.
func drainConn(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	return a, b
}

// TestSendAuditDisabled: when audit is disabled, auditable operations proceed
// without recording.
func TestSendAuditDisabled(t *testing.T) {
	conn, _ := drainConn(t)
	dc := &daemonContext{auditEnabled: false, auditLog: nil}
	if !dc.sendAudit(conn, audit.ExecEntry("ali", "uptime", audit.ResultSuccess, 0, 1, audit.SourceDaemon)) {
		t.Fatal("sendAudit returned false while audit disabled; should proceed")
	}
}

// TestSendAuditEnabledNilLoggerFailsClosed: C1 core invariant. Audit enabled
// but logger unavailable must NOT be treated as success.
func TestSendAuditEnabledNilLoggerFailsClosed(t *testing.T) {
	conn, _ := drainConn(t)
	dc := &daemonContext{auditEnabled: true, auditLog: nil}
	if dc.sendAudit(conn, audit.ExecEntry("ali", "uptime", audit.ResultSuccess, 0, 1, audit.SourceDaemon)) {
		t.Fatal("sendAudit returned true with audit enabled and nil logger; must fail closed")
	}
}

// TestReconcileAuditLifecycle exercises M4: enabling, disabling, and re-enabling
// audit while the daemon is running must create/close the logger accordingly.
func TestReconcileAuditLifecycle(t *testing.T) {
	dc := &daemonContext{}
	logPath := filepath.Join(t.TempDir(), "audit.log")

	enabledCfg := &appconfig.Config{Audit: appconfig.AuditConfig{Enabled: appconfig.Bool(true), Path: logPath}}
	if err := dc.reconcileAudit(enabledCfg); err != nil {
		t.Fatalf("enable: %v", err)
	}
	logger, enabled := dc.auditTarget()
	if !enabled || logger == nil {
		t.Fatalf("after enable: enabled=%v logger=%v, want enabled with logger", enabled, logger != nil)
	}

	disabledCfg := &appconfig.Config{Audit: appconfig.AuditConfig{Enabled: appconfig.Bool(false)}}
	if err := dc.reconcileAudit(disabledCfg); err != nil {
		t.Fatalf("disable: %v", err)
	}
	logger, enabled = dc.auditTarget()
	if enabled || logger != nil {
		t.Fatalf("after disable: enabled=%v logger=%v, want disabled with no logger", enabled, logger != nil)
	}

	if err := dc.reconcileAudit(enabledCfg); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	logger, enabled = dc.auditTarget()
	if !enabled || logger == nil {
		t.Fatalf("after re-enable: enabled=%v logger=%v, want enabled with logger", enabled, logger != nil)
	}
}

// TestReconcileAuditInvalidPathFailsClosed: M4 + C1. If the new audit config
// cannot open a logger, reconcile returns an error and leaves the daemon in a
// fail-closed state (enabled, no logger), so sendAudit rejects operations.
func TestReconcileAuditInvalidPathFailsClosed(t *testing.T) {
	dc := &daemonContext{}
	// A path whose parent is a regular file cannot be created as a directory.
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(parent, "audit.log")

	cfg := &appconfig.Config{Audit: appconfig.AuditConfig{Enabled: appconfig.Bool(true), Path: badPath}}
	if err := dc.reconcileAudit(cfg); err == nil {
		t.Fatal("reconcileAudit with invalid path returned nil; want error")
	}
	logger, enabled := dc.auditTarget()
	if !enabled || logger != nil {
		t.Fatalf("after invalid reconcile: enabled=%v logger=%v, want enabled with nil logger (fail closed)", enabled, logger != nil)
	}

	conn, _ := drainConn(t)
	if dc.sendAudit(conn, audit.ExecEntry("ali", "uptime", audit.ResultSuccess, 0, 1, audit.SourceDaemon)) {
		t.Fatal("sendAudit succeeded after fail-closed reconcile; must reject")
	}
}
