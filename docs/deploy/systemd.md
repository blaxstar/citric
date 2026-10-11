# Linux packages and systemd

## Packages

Download the `.deb` or `.rpm` for your architecture from
[GitHub Releases](https://github.com/blaxstar/citric/releases):

```sh
sudo apt install ./citric_<version>_linux_amd64.deb      # Debian, Ubuntu
sudo dnf install ./citric_<version>_linux_amd64.rpm      # Fedora, RHEL
```

The package creates a `citric` system user, `/var/lib/citric`, and
`/etc/citric/citric.toml` (from the example), and installs
`citric.service`. It doesn't start anything. Edit the config, then:

```sh
sudo systemctl enable --now citric
journalctl -u citric -f
sudo -u citric citric admin create alice      # first admin, prints a CLI token
```

Settings can also go in `/etc/citric/citric.env` as `CITRIC_*=` lines.

Build toolchains must be on `build_path` (default
`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`) and usable
without a home directory: builds run with a throwaway `HOME`, so install
toolchains system-wide rather than per-user (rustup, nvm).

## Tarball

```sh
tar -xzf citric_<version>_linux_amd64.tar.gz
sudo install citric /usr/local/bin/
sudo citric service install --config /etc/citric/citric.toml   # writes and starts a systemd unit
```

`citric service install` also works on macOS (launchd) and Windows. The
release checksums are in `SHA256SUMS`, signed with cosign:

```sh
cosign verify-blob --certificate SHA256SUMS.pem --signature SHA256SUMS.sig \
  --certificate-identity-regexp 'https://github.com/blaxstar/citric/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com SHA256SUMS
```

## Clients

The server hands out CLI binaries at `/dl` and installers at `/install.sh`
and `/install.ps1`, so client and server versions match. Packages ship them
in `/usr/share/citric/dist`.
