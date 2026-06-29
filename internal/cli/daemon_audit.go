package cli

import (
	"net"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/ipc"
)

func (dc *daemonContext) sendAudit(conn net.Conn, entry audit.Entry) bool {
	if dc == nil || dc.auditLog == nil {
		return true
	}
	if err := dc.auditLog.Record(entry); err != nil {
		ipc.SendError(conn, "audit log write failed: "+err.Error(), "check [audit] path and permissions")
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
