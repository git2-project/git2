-- auth-svc: user accounts in the auth_svc schema
-- Not "auth": Supabase reserves that schema for its own Auth product.
CREATE SCHEMA IF NOT EXISTS auth_svc;

CREATE TABLE IF NOT EXISTS auth_svc.users (
    id            UUID         PRIMARY KEY,
    email         VARCHAR(254) NOT NULL,
    username      VARCHAR(39)  NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key    UNIQUE (email),
    CONSTRAINT users_username_key UNIQUE (username)
);
