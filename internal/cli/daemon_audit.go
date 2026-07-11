package cli

import (
	"net"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
)

// auditTarget snapshots the current logger and enabled flag under the lock so a
// concurrent reloadAppConfig swap cannot tear the pair.
func (dc *daemonContext) auditTarget() (*audit.Logger, bool) {
	if dc == nil {
		return nil, false
	}
	dc.auditMu.Lock()
	defer dc.auditMu.Unlock()
	return dc.auditLog, dc.auditEnabled
}

// sendAudit records an auditable operation. It distinguishes three states:
//   - audit disabled: nothing to record, succeed.
//   - audit enabled with a working logger: record; on write error fail closed.
//   - audit enabled but logger unavailable: fail closed, so an operation never
//     runs unaudited when the operator asked for auditing.
//
// Returns true when the caller may proceed, false when it must abort (an IPC
// error has already been sent).
func (dc *daemonContext) sendAudit(conn net.Conn, entry audit.Entry) bool {
	logger, enabled := dc.auditTarget()
	if !enabled {
		return true
	}
	if logger == nil {
		ipc.SendError(conn, output.CodeAuditWriteFailed, "audit log unavailable", "fix [audit] path or disable audit.enabled, then restart daemon")
		return false
	}
	if err := logger.Record(entry); err != nil {
		ipc.SendError(conn, output.CodeAuditWriteFailed, "audit log write failed: "+err.Error(), "check [audit] path and permissions")
		return false
	}
	return true
}

func (dc *daemonContext) sendAuditError(conn net.Conn, entry audit.Entry, err error) bool {
	if err != nil {
		entry.ErrorHint = audit.RedactSummary(err.Error())
	}
	return dc.sendAudit(conn, entry)
}

func transferPayloadAuditParts(payload ipc.TransferPayload) (alias, direction, localPath, remotePath string) {
	return payload.Alias, payload.Direction, payload.LocalPath, payload.RemotePath
}

func transferPayloadAuditSummary(payload ipc.TransferPayload) string {
	_, direction, localPath, remotePath := transferPayloadAuditParts(payload)
	return audit.TransferSummary(direction, localPath, remotePath)
}
