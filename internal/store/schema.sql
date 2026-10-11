CREATE TABLE IF NOT EXISTS users (
  username    TEXT PRIMARY KEY,
  email       TEXT UNIQUE NOT NULL,
  ssh_key     TEXT,
  role        TEXT CHECK(role IN ('admin','submitter','reviewer','readonly')) NOT NULL DEFAULT 'reviewer',
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cls (
  id            INTEGER PRIMARY KEY,
  change_id     TEXT UNIQUE NOT NULL,
  repo          TEXT NOT NULL,
  target_branch TEXT NOT NULL DEFAULT 'main',
  author        TEXT NOT NULL REFERENCES users(username),
  title         TEXT NOT NULL,
  description   TEXT,
  status        TEXT CHECK(status IN ('open','merged','abandoned')) NOT NULL DEFAULT 'open',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cls_status ON cls(status);
CREATE INDEX IF NOT EXISTS idx_cls_author ON cls(author);

CREATE TABLE IF NOT EXISTS patchsets (
  id          INTEGER PRIMARY KEY,
  cl_id       INTEGER NOT NULL REFERENCES cls(id),
  number      INTEGER NOT NULL,
  commit_sha  TEXT NOT NULL,
  base_sha    TEXT NOT NULL,
  ref         TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  UNIQUE(cl_id, number)
);

CREATE TABLE IF NOT EXISTS reviews (
  id         INTEGER PRIMARY KEY,
  cl_id      INTEGER NOT NULL REFERENCES cls(id),
  ps_id      INTEGER NOT NULL REFERENCES patchsets(id),
  reviewer   TEXT NOT NULL REFERENCES users(username),
  verdict    TEXT CHECK(verdict IN ('lgtm','needs_work','comment')) NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(cl_id, ps_id, reviewer)
);

CREATE TABLE IF NOT EXISTS comments (
  id          INTEGER PRIMARY KEY,
  cl_id       INTEGER NOT NULL REFERENCES cls(id),
  ps_id       INTEGER NOT NULL REFERENCES patchsets(id),
  author      TEXT NOT NULL REFERENCES users(username),
  file_path   TEXT NOT NULL,
  line        INTEGER,
  body        TEXT NOT NULL,
  parent_id   INTEGER REFERENCES comments(id),
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS builds (
  id          INTEGER PRIMARY KEY,
  cl_id       INTEGER NOT NULL REFERENCES cls(id),
  ps_id       INTEGER NOT NULL REFERENCES patchsets(id),
  tool_name   TEXT NOT NULL,
  status      TEXT CHECK(status IN ('queued','running','pass','fail')) NOT NULL DEFAULT 'queued',
  log_path    TEXT,
  started_at  INTEGER,
  ended_at    INTEGER
);

CREATE TABLE IF NOT EXISTS cl_reviewers (
  cl_id    INTEGER NOT NULL REFERENCES cls(id),
  username TEXT NOT NULL REFERENCES users(username),
  source   TEXT CHECK(source IN ('manual','auto_owners','auto_hierarchy')) NOT NULL,
  PRIMARY KEY (cl_id, username)
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Per-user, client-defined view preferences (sort order, filters, ...) as an
-- opaque JSON blob — the server doesn't interpret the shape, just stores and
-- returns it, so the frontend can evolve what it persists without a migration.
CREATE TABLE IF NOT EXISTS user_view_settings (
  username   TEXT PRIMARY KEY REFERENCES users(username),
  data       TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

-- Per-repo view-access control. A repo with no row (or restricted=0) is visible
-- to all authenticated users; restricted=1 limits it to admins + repo_acl.
CREATE TABLE IF NOT EXISTS repo_meta (
  repo       TEXT PRIMARY KEY,
  restricted INTEGER NOT NULL DEFAULT 0
);

-- Allow-list for restricted repos: principals may be usernames or Keycloak
-- realm roles ("teams").
CREATE TABLE IF NOT EXISTS repo_acl (
  repo      TEXT NOT NULL,
  principal TEXT NOT NULL,
  kind      TEXT CHECK(kind IN ('user','role')) NOT NULL,
  PRIMARY KEY (repo, principal, kind)
);

-- Effective Keycloak roles per user, refreshed at login and on sync. Lets the
-- git path (which only carries a username token) resolve role-based access.
CREATE TABLE IF NOT EXISTS user_roles (
  username TEXT NOT NULL,
  role     TEXT NOT NULL,
  PRIMARY KEY (username, role)
);

CREATE TABLE IF NOT EXISTS audit (
  id         INTEGER PRIMARY KEY,
  actor      TEXT NOT NULL,
  action     TEXT NOT NULL,
  target     TEXT NOT NULL,
  detail     TEXT,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS follows (
  follower   TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  followee   TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (follower, followee)
);
CREATE INDEX IF NOT EXISTS idx_follows_follower ON follows(follower);
CREATE INDEX IF NOT EXISTS idx_follows_followee ON follows(followee);

CREATE TABLE IF NOT EXISTS stars (
  username   TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  repo       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (username, repo)
);
CREATE INDEX IF NOT EXISTS idx_stars_username ON stars(username);
CREATE INDEX IF NOT EXISTS idx_stars_repo ON stars(repo);

-- Build nodes: machines that run build tools for this server (see docs/nodes.md).
-- credential_hash is the SHA-256 of the node's bearer credential.
CREATE TABLE IF NOT EXISTS nodes (
  id              INTEGER PRIMARY KEY,
  name            TEXT NOT NULL UNIQUE,
  credential_hash TEXT NOT NULL UNIQUE,
  status          TEXT CHECK(status IN ('active','draining')) NOT NULL DEFAULT 'active',
  os              TEXT NOT NULL DEFAULT '',
  arch            TEXT NOT NULL DEFAULT '',
  labels          TEXT NOT NULL DEFAULT '',
  version         TEXT NOT NULL DEFAULT '',
  max_concurrent  INTEGER NOT NULL DEFAULT 0,
  in_flight       INTEGER NOT NULL DEFAULT 0,
  last_seen       INTEGER,
  created_at      INTEGER NOT NULL
);

-- One-time tokens a new node exchanges for its credential.
CREATE TABLE IF NOT EXISTS node_join_tokens (
  token_hash TEXT PRIMARY KEY,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);
