-- auth-svc: the one-time setup token for creating the first admin, in the auth_svc schema
-- Design: docs/design/identity-v0.md, "The first admin".
-- At most one row: singleton is always TRUE and is the primary key. The row
-- exists only while no admin does: auth-svc writes it at startup and deletes
-- it in the same transaction that creates the first admin.
-- token_sha256 is the hex SHA-256 of the token; the token itself is never
-- stored. A plain hash is enough: the token is 32 random bytes, not a
-- password someone chose.
CREATE TABLE IF NOT EXISTS auth_svc.setup_token (
    singleton    BOOLEAN     NOT NULL DEFAULT TRUE,
    token_sha256 TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT setup_token_pkey      PRIMARY KEY (singleton),
    CONSTRAINT setup_token_singleton CHECK (singleton),
    CONSTRAINT setup_token_sha256    CHECK (token_sha256 ~ '^[0-9a-f]{64}$')
);
