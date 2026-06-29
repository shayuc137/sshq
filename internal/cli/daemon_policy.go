package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
)

func (dc *daemonContext) checkDaemonCommand(conn net.Conn, alias, command string) bool {
	return dc.sendPolicyDecision(conn, dc.checker.CheckCommand(alias, command))
}

func (dc *daemonContext) checkDaemonTransfer(conn net.Conn, payload ipc.TransferPayload) bool {
	switch payload.Direction {
	case "upload":
		if !dc.sendPolicyDecision(conn, dc.checker.CheckLocalPath(payload.Alias, payload.LocalPath)) {
			return false
		}
		return dc.sendPolicyDecision(conn, dc.checker.CheckRemotePath(payload.Alias, payload.RemotePath))
	case "download":
		if !dc.sendPolicyDecision(conn, dc.checker.CheckRemotePath(payload.Alias, payload.RemotePath)) {
			return false
		}
		return dc.sendPolicyDecision(conn, dc.checker.CheckLocalPath(payload.Alias, payload.LocalPath))
	default:
		return true
	}
}

func (dc *daemonContext) checkDaemonRelay(conn net.Conn, payload ipc.RelayPayload) bool {
	if !dc.sendPolicyDecision(conn, dc.checker.CheckRemotePath(payload.SrcAlias, payload.SrcPath)) {
		return false
	}
	return dc.sendPolicyDecision(conn, dc.checker.CheckRemotePath(payload.DstAlias, payload.DstPath))
}

func (dc *daemonContext) checkDaemonCluster(conn net.Conn, aliases []string, command string) bool {
	var blocked []string
	for _, alias := range aliases {
		decision := dc.checker.CheckCommand(alias, command)
		if decision.Allowed {
			continue
		}
		blocked = append(blocked, fmt.Sprintf("%s(%s)", alias, decision.Reason))
	}
	if len(blocked) == 0 {
		return true
	}
	ipc.SendError(conn,
		"cluster command blocked by policy for aliases: "+strings.Join(blocked, ", "),
		"adjust config.toml or grant each blocked alias before retrying",
	)
	return false
}

func (dc *daemonContext) sendPolicyDecision(conn net.Conn, decision policy.Decision) bool {
	if decision.Allowed {
		return true
	}
	err := decisionToError(decision)
	if ce, ok := err.(*output.CmdError); ok {
		ipc.SendError(conn, ce.Hint, ce.Action)
		return false
	}
	ipc.SendError(conn, err.Error(), "fix config.toml, then retry")
	return false
}

func (dc *daemonContext) handlePolicyGrant(conn net.Conn, raw json.RawMessage) {
	var payload ipc.PolicyGrantPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid policy-grant payload: "+err.Error(), "")
		return
	}
	if payload.Kind == "" {
		payload.Kind = policy.KindCommand
	}
	if payload.Alias == "" {
		ipc.SendError(conn, "policy grant alias required", "usage: sshq policy grant <alias> <pattern> --ttl <duration>")
		return
	}
	if _, err := dc.store.Get(payload.Alias); err != nil {
		ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
		return
	}

	grant, err := dc.grants.Add(payload.Alias, payload.Kind, payload.Pattern, time.Duration(payload.TTLSeconds)*time.Second)
	if err != nil {
		ipc.SendError(conn, "invalid policy grant: "+err.Error(), "")
		return
	}

	frame, _ := ipc.MakeResultFrame(ipc.PolicyGrantResult{Grant: grantToIPC(grant)})
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handlePolicyRevoke(conn net.Conn, raw json.RawMessage) {
	var payload ipc.PolicyRevokePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid policy-revoke payload: "+err.Error(), "")
		return
	}
	if payload.ID == "" && payload.Alias == "" {
		ipc.SendError(conn, "policy revoke requires grant id or --alias", "usage: sshq policy revoke <grant-id> or sshq policy revoke --alias <alias>")
		return
	}

	removed := 0
	if payload.Alias != "" {
		removed = dc.grants.RevokeByAlias(payload.Alias)
	} else if dc.grants.Revoke(payload.ID) {
		removed = 1
	}

	frame, _ := ipc.MakeResultFrame(ipc.PolicyRevokeResult{Removed: removed})
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handlePolicyList(conn net.Conn, raw json.RawMessage) {
	var payload ipc.PolicyListPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			ipc.SendError(conn, "invalid policy-list payload: "+err.Error(), "")
			return
		}
	}
	if dc.appCfgErr != nil {
		ipc.SendError(conn, "app config invalid: "+dc.appCfgErr.Error(), "fix config.toml, then retry")
		return
	}
	if payload.Alias != "" {
		if _, err := dc.store.Get(payload.Alias); err != nil {
			ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
			return
		}
	}

	result := ipc.PolicyListResult{
		Alias:  payload.Alias,
		Policy: policyToIPC(dc.checker.EffectivePolicy(payload.Alias)),
		Grants: grantsToIPC(dc.grants.List(payload.Alias)),
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

func policyToIPC(p policy.EffectiveRuleSet) ipc.PolicyEffectiveResult {
	return ipc.PolicyEffectiveResult{
		Enabled:             p.Enabled,
		CommandWhitelist:    emptySlice(p.CommandWhitelist),
		CommandBlacklist:    emptySlice(p.CommandBlacklist),
		LocalPathWhitelist:  emptySlice(p.LocalPathWhitelist),
		RemotePathWhitelist: emptySlice(p.RemotePathWhitelist),
	}
}

func grantsToIPC(grants []*policy.Grant) []ipc.PolicyGrantInfo {
	out := make([]ipc.PolicyGrantInfo, 0, len(grants))
	for _, grant := range grants {
		out = append(out, grantToIPC(grant))
	}
	return out
}

func grantToIPC(grant *policy.Grant) ipc.PolicyGrantInfo {
	if grant == nil {
		return ipc.PolicyGrantInfo{}
	}
	return ipc.PolicyGrantInfo{
		ID:        grant.ID,
		Alias:     grant.Alias,
		Kind:      grant.Kind,
		Pattern:   grant.Pattern,
		CreatedAt: grant.CreatedAt.Unix(),
		ExpiresAt: grant.ExpiresAt.Unix(),
	}
}

func emptySlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
