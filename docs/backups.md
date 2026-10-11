# Backups

```sh
citric backup --out /backups/citric-$(date +%F).tar.gz
# Docker:
docker compose exec citric citric backup --out /var/lib/citric/backup.tar.gz
```

The archive holds a consistent snapshot of the database (taken with SQLite's
`VACUUM INTO`, safe while the server runs), every bare repository, and the
rest of `<data_dir>`: the JWT secret, hook token, mailmap and uploads. Build
logs, caches, build worktrees and `/dl` binaries are left out; they're
rebuilt.

A push landing during a backup may be missed. Run it again, or stop the server
for a guaranteed-quiet copy.

## Restore

Stop the server, then extract over (or next to) the state directory:

```sh
sudo systemctl stop citric
sudo tar -xzf citric-2026-10-10.tar.gz -C /var/lib/citric    # gives data/ and repos/
sudo chown -R citric:citric /var/lib/citric
sudo systemctl start citric
```

Point `data_dir` and `repos_dir` at the extracted `data/` and `repos/` if you
restore somewhere else.
