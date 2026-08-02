# Windows Non-ASCII Output Recipes

Documentation version: `sshq v0.4.1`.

Reliable round-tripping of non-ASCII (e.g. Chinese) text with Windows hosts.

Auto-generated from `sshq docs --skill`. Do not edit manually.

---

## When you need this

On some Windows hosts (observed: Chinese Windows + PowerShell 5.1), remote output containing non-ASCII text arrives garbled: UTF-8 bytes are decoded as GBK somewhere in the sshd forwarding chain, and illegal sequences are replaced with U+FFFD. Once that replacement happens the original bytes are gone — no client-side decoding can recover them, and setting [Console]::OutputEncoding on the remote side does not help.

Pure-ASCII output is unaffected. Skip these recipes when the command output contains no non-ASCII text.

The reliable workaround keeps every byte on the wire ASCII: encode with base64 on the remote side, decode locally. This survives any code page.

## Receiving non-ASCII output

In the remote script, serialize to JSON, encode the UTF-8 bytes as base64, and emit fixed-width lines:

~~~powershell
$data  = Get-ChildItem 'C:/Users/support/Desktop' | Select-Object Name, LastWriteTime
$json  = $data | ConvertTo-Json -Compress
$b64   = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
for ($i = 0; $i -lt $b64.Length; $i += 120) {
    $b64.Substring($i, [Math]::Min(120, $b64.Length - $i))
}
~~~

Decode locally by joining the lines:

~~~bash
sshq exec company-win --script-file ./list-desktop.ps1 --shell powershell \
  | jq -r '.data.stdout' | tr -d '\n' | base64 -d
~~~

## Sending non-ASCII arguments

Non-ASCII literals written directly into a script (paths, filenames) are damaged in transit the same way. Encode them locally and decode inside the script:

~~~bash
B64=$(printf '%s' "AI业务操作台" | base64 -w0)
cat > open-dir.ps1 <<EOF
\$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$B64'))
Get-ChildItem -LiteralPath "C:/Users/support/Desktop/\$name"
EOF
sshq exec company-win --script-file ./open-dir.ps1 --shell powershell
~~~

## Status

This is a workaround, not a fix. The layer performing the lossy GBK decode is unconfirmed (suspected: Windows OpenSSH sshd console code-page conversion), so an in-band fix such as an encoding flag cannot work yet. The recipes above were validated in production use — six calls, zero corruption.
