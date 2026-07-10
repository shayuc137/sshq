# Remote Windows Support Recipe

Documentation version: `sshq v0.2.0`.

A complete temporary-support flow from WireGuard reachability through cleanup.

Auto-generated from `sshq docs --skill`. Do not edit manually.

---

## 1. Add temporary WireGuard reachability

Allocate one peer address and route only that address. The hub-side shape is:

~~~text
[Peer]
PublicKey = <windows-peer-public-key>
AllowedIPs = 10.0.0.4/32
~~~

Install the matching peer configuration on Windows, bring up the tunnel, and confirm the support machine can reach the assigned address before changing SSH configuration.

## 2. Enable OpenSSH Server on Windows

Run these commands in an elevated PowerShell console on the Windows machine:

~~~powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshq-temp-sshd -DisplayName 'sshq temporary OpenSSH' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 -RemoteAddress 10.0.0.0/24
~~~

## 3. Install a temporary support key

Create a dedicated key locally, then append its public key to the target user's %USERPROFILE%/.ssh/authorized_keys. Restrict the .ssh directory and file to that user and SYSTEM; Windows OpenSSH rejects overly broad ACLs.

## 4. Register and diagnose the host

~~~bash
sshq config add support-win --hostname 10.0.0.4 --user support --identity ~/.ssh/support-win
sshq doctor support-win
~~~

Read the resolved alias, hostname, user, port, identity file, and ProxyJump values from config add; continue only after doctor passes the connection checks.

## 5. Execute and transfer

~~~bash
sshq exec support-win --script-file ./diagnose.ps1 --shell powershell
sshq cp --mkdirs ./support.exe 'support-win:C:/Program Files/Support Tool/support.exe'
sshq cp 'support-win:C:/ProgramData/Support Tool/diagnostics.zip' ./diagnostics.zip
~~~

## 6. Clean up

Remove temporary artifacts and scheduled tasks while the connection still works, then remove the sshq alias, the authorized key line, the temporary firewall rule, and the WireGuard peer:

~~~bash
sshq exec support-win 'schtasks /Delete /TN "sshq-support" /F'
sshq exec support-win 'powershell -NoProfile -Command "Remove-Item -LiteralPath ''C:/Program Files/Support Tool'' -Recurse -Force"'
sshq config remove support-win
~~~

Finally remove sshq-temp-sshd if it was created only for the support window, and delete the peer from both WireGuard endpoints.
