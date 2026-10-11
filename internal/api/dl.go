package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/blaxstar/citric/internal/brand"
)

// installScriptTemplate is served at GET /install.sh.
// %s is replaced with the server's own URL so the downloaded script
// points back to the same host it came from — works for both corp and public.
const installScriptTemplate = `#!/bin/sh
set -e

CITRIC_SERVER="${CITRIC_SERVER:-${CRIT_SERVER:-%s}}"

# ── --update / -u: reinstalling the binary on a machine that's already
# authenticated. Skips the onboarding steps at the end — there's no new
# token to provision, this just overwrites the binary in place. Pass it
# through when piping into sh: curl ... | sh -s -- --update
UPDATE=0
for arg in "$@"; do
  case "$arg" in
    --update|-u) UPDATE=1 ;;
  esac
done

# ── detect platform ───────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)        ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) printf 'unsupported architecture: %%s\n' "$ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) printf 'unsupported OS: %%s\n' "$OS" >&2; exit 1 ;;
esac

# Mirrors the CLI's own defaultClientConfigPath() (cmd/citric/client_config.go),
# including its fallback to the pre-rename crit config directory,
# so --update can tell whether this machine is actually already authenticated
# before it trusts --update to skip onboarding.
case "$OS" in
  linux)  CONFIG_BASE="${XDG_CONFIG_HOME:-$HOME/.config}" ;;
  darwin) CONFIG_BASE="$HOME/Library/Application Support" ;;
esac
CONFIG_FILE="$CONFIG_BASE/citric/config.toml"
[ -f "$CONFIG_FILE" ] || [ ! -f "$CONFIG_BASE/crit/config.toml" ] || CONFIG_FILE="$CONFIG_BASE/crit/config.toml"

if [ "$UPDATE" = "1" ] && [ ! -f "$CONFIG_FILE" ]; then
  printf '\033[33mwarning:\033[0m --update was passed but %%s not found —\n' "$CONFIG_FILE"
  printf 'this looks like a fresh machine, not an update. Continuing anyway,\n'
  printf 'but you will still need to run '\''citric auth login'\'' below.\n\n'
  UPDATE=0
fi

BINARY="citric-${OS}-${ARCH}"
URL="${CITRIC_SERVER}/dl/${BINARY}"

# ── pick install dir (no sudo by default) ────────────────────────────────────
if [ -n "$INSTALL_DIR" ]; then
  : # user override
elif [ -d "$HOME/.local/bin" ]; then
  INSTALL_DIR="$HOME/.local/bin"
elif [ -w /usr/local/bin ]; then
  INSTALL_DIR=/usr/local/bin
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

DEST="$INSTALL_DIR/citric"

# ── download ──────────────────────────────────────────────────────────────────
printf 'citric installer\n'
printf '  server : %%s\n' "$CITRIC_SERVER"
printf '  target : %%s/%%s\n' "$OS" "$ARCH"
printf '  dest   : %%s\n\n' "$DEST"

TMP=$(mktemp /tmp/citric-XXXXXX)
trap 'rm -f "$TMP"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL --progress-bar "$URL" -o "$TMP"
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress "$URL" -O "$TMP"
else
  printf 'error: curl or wget is required\n' >&2
  exit 1
fi

chmod +x "$TMP"
mv "$TMP" "$DEST"
trap - EXIT

# Upgrading from the pre-rename crit binary: keep the old command working by
# pointing it at citric (it answers to both names).
if [ -f "$INSTALL_DIR/crit" ] && [ ! -L "$INSTALL_DIR/crit" ]; then
  ln -sf citric "$INSTALL_DIR/crit"
  printf '  note   : %%s/crit now points at citric\n' "$INSTALL_DIR"
fi

# ── PATH hint ─────────────────────────────────────────────────────────────────
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf '\n\033[33mwarning:\033[0m %%s is not in PATH.\n' "$INSTALL_DIR"
    printf 'add this to your ~/.bashrc or ~/.zshrc:\n'
    printf '  export PATH="%%s:$PATH"\n' "$INSTALL_DIR"
    ;;
esac

if [ "$UPDATE" = "1" ]; then
  printf '\n\033[32m✓ citric updated\033[0m → %%s\n\n' "$DEST"
else
  printf '\n\033[32m✓ citric installed\033[0m → %%s\n\n' "$DEST"
  printf 'next steps:\n'
  printf '  1. open %%s, sign in, then: avatar menu → CLI access → generate a token\n' "$CITRIC_SERVER"
  printf '  2. citric auth login --server %%s --token <paste>\n' "$CITRIC_SERVER"
  printf '  3. cd <your-repo>\n'
  printf '  4. citric init                # remote + .citric.toml + config + commit-msg hook\n'
  printf '  5. citric commit -m "your change"\n'
  printf '  6. citric push\n'
  printf '\nalready authenticated on another machine? re-run this with --update to skip\n'
  printf 'straight to just updating the binary:\n'
  printf '  curl -fsSL %%s/install.sh | sh -s -- --update\n' "$CITRIC_SERVER"
fi
`

// installPS1Template is served at GET /install.ps1, the Windows counterpart
// of install.sh. %s is the server's own URL.
//
//	irm https://citric.example.com/install.ps1 | iex
const installPS1Template = `$ErrorActionPreference = 'Stop'
$Server = if ($env:CITRIC_SERVER) { $env:CITRIC_SERVER } else { '%s' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$dir = if ($env:CITRIC_INSTALL_DIR) { $env:CITRIC_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'citric\bin' }
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir 'citric.exe'
$url = "$Server/dl/citric-windows-$arch.exe"

Write-Host 'citric installer'
Write-Host "  server : $Server"
Write-Host "  target : windows/$arch"
Write-Host "  dest   : $dest"
Write-Host ''

$tmp = "$dest.download"
Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
Move-Item -Force $tmp $dest

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not ($userPath -split ';' | Where-Object { $_ -eq $dir })) {
  [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir), 'User')
  Write-Host "added $dir to your user PATH (open a new terminal to pick it up)"
}

Write-Host ''
Write-Host "citric installed -> $dest" -ForegroundColor Green
Write-Host 'next steps:'
Write-Host "  1. open $Server, sign in, then: avatar menu -> CLI access -> generate a token"
Write-Host "  2. citric auth login --server $Server --token <paste>"
Write-Host '  3. cd <your-repo>; citric init'
Write-Host '  4. citric commit -m "your change"; citric push'
`

// handleInstallPS1 serves install.ps1 with this server's URL embedded.
func (s *Server) handleInstallPS1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="install.ps1"`)
	fmt.Fprintf(w, installPS1Template, s.publicBaseURL(r))
}

// dlFilenameRE only allows <name>-<os>-<arch> patterns to prevent path
// traversal. crit-* is the pre-rename name that older clients still request
// when they self-update.
var dlFilenameRE = regexp.MustCompile(`^(citric|crit)-(linux|darwin|windows)-(amd64|arm64)(\.exe)?$`)

// handleInstallScript serves a dynamically generated install.sh with the
// server's own URL embedded so it works from any domain without config.
func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	serverURL := s.publicBaseURL(r)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="install.sh"`)
	fmt.Fprintf(w, installScriptTemplate, serverURL)
}

// handleDownload serves pre-built client binaries from data_dir/dist/.
// No auth required — binaries are public so new users can install without a token.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// chi wildcard gives us the filename after /dl/
	filename := strings.TrimPrefix(r.URL.Path, "/dl/")
	filename = filepath.Base(filename) // strip any path components

	if !dlFilenameRE.MatchString(filename) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	f, err := s.openDist(filename)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("binary %q not yet available — check back after the next release", filename), http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, filename, stat.ModTime(), f)
}

// openDist opens a client binary from data_dir/dist, then dist_dir. A request
// for the legacy crit-<os>-<arch> name is served the citric build when one
// exists, so old clients that self-update move onto the renamed binary.
func (s *Server) openDist(filename string) (*os.File, error) {
	names := []string{filename}
	if strings.HasPrefix(filename, brand.LegacyName+"-") {
		names = append([]string{brand.Name + strings.TrimPrefix(filename, brand.LegacyName)}, names...)
	}
	dirs := []string{filepath.Join(s.cfg.Server.DataDir, "dist")}
	if s.cfg.Server.DistDir != "" {
		dirs = append(dirs, s.cfg.Server.DistDir)
	}
	for _, dir := range dirs {
		for _, name := range names {
			if f, err := os.Open(filepath.Join(dir, name)); err == nil {
				return f, nil
			}
		}
	}
	return nil, os.ErrNotExist
}
