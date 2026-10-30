-- This file is the representation of the SQLite schema of Headscale.
-- It is the "source of truth" and is used to validate any migrations
-- that are run against the database to ensure it ends in the expected state.

CREATE TABLE migrations(id text,PRIMARY KEY(id));

CREATE TABLE users(
  id integer PRIMARY KEY AUTOINCREMENT,
  name text,
  display_name text,
  email text,
  provider_identifier text,
  provider text,
  profile_pic_url text,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime
);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);


-- The following three UNIQUE indexes work together to enforce the user identity model:
--
-- 1. Users can be either local (provider_identifier is NULL) or from external providers (provider_identifier set)
-- 2. Each external provider identifier must be unique across the system
-- 3. Local usernames must be unique among local users
-- 4. The same username can exist across different providers with different identifiers
--
-- Examples:
-- - Can create local user "alice" (provider_identifier=NULL)
-- - Can create external user "alice" with GitHub (name="alice", provider_identifier="alice_github")
-- - Can create external user "alice" with Google (name="alice", provider_identifier="alice_google")
-- - Cannot create another local user "alice" (blocked by idx_name_no_provider_identifier)
-- - Cannot create another user with provider_identifier="alice_github" (blocked by idx_provider_identifier)
-- - Cannot create user "bob" with provider_identifier="alice_github" (blocked by idx_name_provider_identifier)
CREATE UNIQUE INDEX idx_provider_identifier ON users(provider_identifier) WHERE provider_identifier IS NOT NULL;
CREATE UNIQUE INDEX idx_name_provider_identifier ON users(name, provider_identifier);
CREATE UNIQUE INDEX idx_name_no_provider_identifier ON users(name) WHERE provider_identifier IS NULL;

-- Unified store for every authenticatable secret (API keys, pre-auth keys,
-- OAuth clients and access tokens), discriminated by kind. Only a hash of the
-- secret is stored (SHA-256; legacy bcrypt/Argon2id until next use); identifier
-- is the public lookup value, unique within a kind. Per-kind columns are sparse
-- by design.
CREATE TABLE credentials(
  id integer PRIMARY KEY AUTOINCREMENT,
  kind text NOT NULL CHECK(kind IN ('api','authkey','oauth_client','oauth_token')),
  identifier text,
  hash blob,
  user_id integer,
  description text,
  scopes text,
  tags text,
  reusable numeric,
  ephemeral numeric DEFAULT false,
  used numeric DEFAULT false,
  last_seen datetime,
  client_id text,
  created_at datetime,
  expiration datetime,
  revoked datetime,

  CONSTRAINT fk_credentials_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT chk_credentials_hash CHECK(hash IS NOT NULL OR revoked IS NOT NULL),
  CONSTRAINT chk_credentials_hash_format CHECK(hash IS NULL OR substr(CAST(hash AS TEXT), 1, 1) = '$')
);
CREATE UNIQUE INDEX idx_credentials_identifier ON credentials(kind, identifier);
CREATE INDEX idx_credentials_user_id ON credentials(user_id);

CREATE TABLE nodes(
  id integer PRIMARY KEY AUTOINCREMENT,
  machine_key text,
  node_key text,
  disco_key text,

  endpoints text,
  host_info text,
  ipv4 text,
  ipv6 text,
  hostname text,
  given_name varchar(63),
  -- user_id is NULL for tagged nodes (owned by tags, not a user).
  -- Only set for user-owned nodes (no tags).
  user_id integer,
  register_method text,
  tags text,
  auth_key_id integer,
  last_seen datetime,
  expiry datetime,
  approved_routes text,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime,

  CONSTRAINT fk_nodes_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_nodes_auth_key FOREIGN KEY(auth_key_id) REFERENCES credentials(id)
);
CREATE INDEX idx_nodes_auth_key_id ON nodes(auth_key_id);

CREATE TABLE policies(
  id integer PRIMARY KEY AUTOINCREMENT,
  data text,

  created_at datetime,
  updated_at datetime,
  deleted_at datetime
);
CREATE INDEX idx_policies_deleted_at ON policies(deleted_at);

CREATE TABLE web_admins(
  id integer PRIMARY KEY AUTOINCREMENT,
  username text NOT NULL,
  password_hash blob NOT NULL,
  created_at datetime,
  updated_at datetime,
  last_login_at datetime
);
CREATE UNIQUE INDEX idx_web_admins_username ON web_admins(username);

CREATE TABLE database_versions(
  id integer PRIMARY KEY,
  version text NOT NULL,
  updated_at datetime
);

CREATE TABLE user_accounts (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 login_email text UNIQUE, legacy_login text UNIQUE,
 is_admin boolean NOT NULL DEFAULT false,
 password_hash blob, must_change_password boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL DEFAULT 1, last_login_at datetime
);

CREATE TABLE user_securities (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 encrypted_totp_secret blob,
 last_totp_step bigint NOT NULL DEFAULT -1,
 recovery_hashes text NOT NULL DEFAULT '[]'
);
CREATE TABLE user_passkeys (
 id text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name text NOT NULL,
 credential text NOT NULL,
 created_at datetime NOT NULL
);
CREATE INDEX idx_user_passkeys_user_id ON user_passkeys(user_id);

CREATE TABLE user_auth_policies (
 email_change_blocked boolean NOT NULL DEFAULT false,
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 require_strong_auth boolean NOT NULL DEFAULT false,
 totp_suspended_until datetime,
 passkeys_suspended_until datetime,
 updated_by bigint NOT NULL DEFAULT 0,
 updated_at datetime,
 reason text NOT NULL DEFAULT ''
);

CREATE TABLE user_email_changes (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 email text NOT NULL,
 token_hash text NOT NULL,
 account_revision bigint NOT NULL,
 requested_at datetime NOT NULL,
 expires_at datetime NOT NULL
);

CREATE TABLE user_invitations (
 id text PRIMARY KEY,
 email text NOT NULL UNIQUE,
 mode text NOT NULL CHECK (mode IN ('local','oidc')),
 token_hash text NOT NULL UNIQUE,
 created_by bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at datetime NOT NULL,
 expires_at datetime NOT NULL
);
