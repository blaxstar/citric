# Windows

CitriC runs natively on Windows as a server, a build node, or just the CLI.

## Install

- **MSI:** `citric_<version>_windows_amd64.msi` (or `arm64`) from
  [GitHub Releases](https://github.com/blaxstar/citric/releases). Installs to
  `C:\Program Files\citric` and adds it to `PATH`.
- **winget:** `winget install BLAXSTAR.citric`
- **Scoop:** `scoop install https://raw.githubusercontent.com/blaxstar/citric/main/packaging/scoop/citric.json`
- **From a CitriC server:** `irm https://citric.example.com/install.ps1 | iex`

**Prerequisite:** [Git for Windows](https://git-scm.com/download/win). CitriC
needs `git`, and the server's repository hooks run on its bundled `sh`.

## Server

From an Administrator PowerShell:

```powershell
citric service install            # registers and starts the "citric" Windows service
citric admin create alice         # first admin, prints a CLI token
```

Data lives in `%ProgramData%\citric\` (`data`, `repos`). The config file is
`%ProgramData%\citric\citric.toml` if you pass `--config` to
`service install`; otherwise configure through `CITRIC_*` system environment
variables. `citric service uninstall` removes the service and keeps the data.

## Builds on Windows

Build commands run in PowerShell by default (`pwsh` when installed, else
Windows PowerShell 5). Pick another shell per tool with
`shell = "cmd"` (or `"bash"` for Git Bash) in `.citric.toml`. Cancelling or
timing out a build ends its whole process tree.

To build some tools on Windows and the rest on Linux, add a Windows
[build node](../nodes.md) and set `runs_on = ["windows"]` on those tools.
