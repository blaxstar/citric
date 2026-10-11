#!/bin/sh
# Stop the service on removal; data in /var/lib/citric is left in place.
systemctl disable --now citric >/dev/null 2>&1 || true
