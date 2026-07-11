# Windows Path Recipes

Documentation version: `sshq v0.4.0`.

Canonical Windows path forms for remote execution and file transfer.

Auto-generated from `sshq docs --skill`. Do not edit manually.

---

## Canonical path form

Use forward slashes in Windows remote paths. This keeps the alias:path boundary unambiguous and works with the SFTP path model:

~~~text
company-win:C:/Users/support/Desktop/report.txt
company-win:C:/Program Files/Support Tool/config.json
~~~

Quote the complete endpoint when a path contains spaces. Add --mkdirs when the remote parent directory may not exist:

~~~bash
sshq cp --mkdirs ./support.exe 'company-win:C:/Program Files/Support Tool/support.exe'
sshq cp 'company-win:C:/Program Files/Support Tool/support.log' './support logs/'
~~~

A local Windows drive path such as C:/Temp/input.txt remains local; a remote Windows path always includes the alias prefix:

~~~bash
sshq cp 'C:/Temp/input.txt' 'company-win:C:/Users/support/Desktop/input.txt'
~~~

For complex PowerShell expressions or paths containing PowerShell variables, put the script in a local .ps1 file:

~~~bash
sshq exec company-win --script-file ./inspect-paths.ps1 --shell powershell
~~~
