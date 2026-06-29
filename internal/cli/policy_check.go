package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
	"github.com/shayuc137/sshq/internal/transfer"
)

func checkPolicyCommand(ctx context.Context, alias, command string) error {
	return checkPolicyCommandWithAudit(ctx, alias, command, audit.OperationExec, audit.ExecSummary(command), audit.SourceDirect)
}

func checkPolicyCommandWithAudit(ctx context.Context, alias, command, operation, summary, source string) error {
	checker, err := checkerForPolicyCheck(ctx)
	if err != nil {
		return err
	}
	if checker == nil {
		return nil
	}
	decision := checker.CheckCommand(alias, command)
	if decision.Allowed {
		return nil
	}
	if err := recordBlockedDecision(ctx, decision, operation, summary, source, nil); err != nil {
		return err
	}
	return decisionToError(decision)
}

func checkPolicyTransfer(ctx context.Context, parsed transfer.ParsedArgs) error {
	checker, err := checkerForPolicyCheck(ctx)
	if err != nil {
		return err
	}
	if checker == nil {
		return nil
	}

	summary := transferAuditSummary(parsed)
	switch parsed.Direction {
	case transfer.Upload:
		decision := checker.CheckLocalPath(parsed.Dst.Alias, parsed.Src.Path)
		if err := auditBlockedTransfer(ctx, decision, summary); err != nil {
			return err
		}
		decision = checker.CheckRemotePath(parsed.Dst.Alias, parsed.Dst.Path)
		return auditBlockedTransfer(ctx, decision, summary)
	case transfer.Download:
		decision := checker.CheckRemotePath(parsed.Src.Alias, parsed.Src.Path)
		if err := auditBlockedTransfer(ctx, decision, summary); err != nil {
			return err
		}
		decision = checker.CheckLocalPath(parsed.Src.Alias, parsed.Dst.Path)
		return auditBlockedTransfer(ctx, decision, summary)
	case transfer.Relay:
		decision := checker.CheckRemotePath(parsed.Src.Alias, parsed.Src.Path)
		if err := auditBlockedTransfer(ctx, decision, summary); err != nil {
			return err
		}
		decision = checker.CheckRemotePath(parsed.Dst.Alias, parsed.Dst.Path)
		return auditBlockedTransfer(ctx, decision, summary)
	}
	return nil
}

func checkPolicyClusterCommand(ctx context.Context, aliases []string, command string) error {
	checker, err := checkerForPolicyCheck(ctx)
	if err != nil {
		return err
	}
	if checker == nil {
		return nil
	}

	var blocked []string
	for _, alias := range aliases {
		decision := checker.CheckCommand(alias, command)
		if decision.Allowed {
			continue
		}
		if err := recordBlockedDecision(ctx, decision, audit.OperationClusterExec, audit.ExecSummary(command), audit.SourceDirect, aliases); err != nil {
			return err
		}
		blocked = append(blocked, fmt.Sprintf("%s(%s)", alias, decision.Reason))
	}
	if len(blocked) == 0 {
		return nil
	}
	return output.Errorf(
		"cluster command blocked by policy for aliases: "+strings.Join(blocked, ", "),
		"adjust config.toml or grant each blocked alias before retrying",
	)
}

func checkerForPolicyCheck(ctx context.Context) (*policy.Checker, error) {
	if err := appConfigErrorFrom(ctx); err != nil {
		return nil, output.Errorf("app config invalid: "+err.Error(), "fix config.toml")
	}
	cfg := appConfigFrom(ctx)
	if cfg == nil {
		return policyCheckerFrom(ctx), nil
	}
	grants, err := daemonGrantsForPolicyCheck()
	if err != nil {
		return nil, err
	}
	return policy.NewChecker(cfg, grants), nil
}

func decisionToError(decision policy.Decision) error {
	if decision.Allowed {
		return nil
	}
	return (&policy.BlockedError{
		Alias:   decision.Alias,
		Kind:    decision.Kind,
		Reason:  decision.Reason,
		Pattern: decision.Pattern,
		Input:   decision.Input,
	}).ToOutputError()
}

func auditBlockedTransfer(ctx context.Context, decision policy.Decision, summary string) error {
	if decision.Allowed {
		return nil
	}
	if err := recordBlockedDecision(ctx, decision, audit.OperationCP, summary, audit.SourceDirect, nil); err != nil {
		return err
	}
	return decisionToError(decision)
}

func recordBlockedDecision(ctx context.Context, decision policy.Decision, operation, summary, source string, aliases []string) error {
	entry := audit.BlockedEntry(decision.Alias, operation, summary, decision.Reason, decision.Pattern, source)
	if len(aliases) > 0 {
		entry.Aliases = append([]string(nil), aliases...)
	}
	return recordAudit(ctx, entry)
}

func transferAuditSummary(parsed transfer.ParsedArgs) string {
	switch parsed.Direction {
	case transfer.Upload, transfer.Download:
		_, direction, localPath, remotePath := transferAuditParts(parsed)
		return audit.TransferSummary(direction, localPath, remotePath)
	case transfer.Relay:
		return audit.RelaySummary(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path)
	default:
		return parsed.Src.String() + " -> " + parsed.Dst.String()
	}
}

func transferAuditParts(parsed transfer.ParsedArgs) (alias, direction, localPath, remotePath string) {
	switch parsed.Direction {
	case transfer.Upload:
		return parsed.Dst.Alias, "upload", parsed.Src.Path, parsed.Dst.Path
	case transfer.Download:
		return parsed.Src.Alias, "download", parsed.Dst.Path, parsed.Src.Path
	default:
		return "", parsed.Direction.String(), parsed.Src.Path, parsed.Dst.Path
	}
}

func daemonGrantsForPolicyCheck() (*policy.GrantManager, error) {
	if !ipc.IsRunning() {
		return nil, nil
	}
	conn, err := ipc.Connect()
	if err != nil {
		return nil, nil
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("policy-list", ipc.PolicyListPayload{})
	if err := ipc.Send(conn, env); err != nil {
		return nil, nil
	}

	var result ipc.PolicyListResult
	if err := recvPolicyResult(conn, &result); err != nil {
		if ce, ok := err.(*output.CmdError); ok && strings.Contains(ce.Hint, "protocol version mismatch") {
			return nil, output.Errorf(ce.Hint, "restart daemon so CLI and daemon use the same sshq version")
		}
		return nil, nil
	}

	grants := policy.NewGrantManager()
	for _, info := range result.Grants {
		ttl := time.Until(time.Unix(info.ExpiresAt, 0))
		if ttl <= 0 {
			continue
		}
		if ttl > policy.MaxGrantTTL {
			ttl = policy.MaxGrantTTL
		}
		_, _ = grants.Add(info.Alias, info.Kind, info.Pattern, ttl)
	}
	return grants, nil
}

func recvPolicyResult(conn net.Conn, out any) error {
	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon connection lost", "retry after restarting daemon")
		}
		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "")
		}
		switch frame.Type {
		case "result":
			if err := json.Unmarshal(frame.Payload, out); err != nil {
				return output.Errorf("invalid daemon result", "")
			}
			return nil
		case "error":
			if strings.Contains(frame.Hint, "protocol version mismatch") {
				return output.Errorf(frame.Hint, "restart daemon so CLI and daemon use the same sshq version")
			}
			return output.Errorf(frame.Hint, frame.Action)
		}
	}
}
