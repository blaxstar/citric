#!/bin/sh
# Create the service account and state directories. Never starts the service:
# edit /etc/citric/citric.toml (or citric.env) first, then
#   systemctl enable --now citric
set -e
if ! getent passwd citric >/dev/null; then
  useradd --system --home-dir /var/lib/citric --shell /usr/sbin/nologin citric
fi
install -d -o citric -g citric -m 0750 /var/lib/citric /var/lib/citric/data /var/lib/citric/repos /var/cache/citric
install -d -m 0755 /etc/citric
if [ ! -f /etc/citric/citric.toml ]; then
  cp /usr/share/citric/citric.toml.example /etc/citric/citric.toml
  chmod 0640 /etc/citric/citric.toml
  chown root:citric /etc/citric/citric.toml
fi
systemctl daemon-reload >/dev/null 2>&1 || true
echo "citric installed. Edit /etc/citric/citric.toml, then: systemctl enable --now citric"
