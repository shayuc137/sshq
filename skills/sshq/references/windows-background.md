# Windows Background Task Recipes

Documentation version: `sshq v0.2.0`.

Durable Windows background work using Task Scheduler.

Auto-generated from `sshq docs --skill`. Do not edit manually.

---

## Prefer Task Scheduler

Use a scheduled task for work that must survive the SSH session. Start-Process can remain tied to the session/job object and provides weaker query and cleanup behavior. Task Scheduler gives explicit create, query, and delete operations.

Create a startup task that runs as SYSTEM:

~~~bash
sshq exec company-win 'schtasks /Create /TN "sshq-support" /SC ONSTART /RU SYSTEM /TR "C:\Program Files\Support Tool\support.exe" /F'
~~~

Query its definition and latest result:

~~~bash
sshq exec company-win 'schtasks /Query /TN "sshq-support" /V /FO LIST'
~~~

Delete it during cleanup:

~~~bash
sshq exec company-win 'schtasks /Delete /TN "sshq-support" /F'
~~~

For commands with PowerShell variables, multiple actions, or nested quoting, write a .ps1 file and execute it with --script-file rather than expanding the one-line command.
