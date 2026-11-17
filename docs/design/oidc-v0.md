# OIDC single sign-on v0

Owner: Beste. Scope: WBS-4.1, Requirements Specification FR-21, FR-22, UC-05 and UC-06. The wire shapes are in [`contracts/auth-svc/openapi.yaml`](../../contracts/auth-svc/openapi.yaml) (sign-in) and [`contracts/admin-api/openapi.yaml`](../../contracts/admin-api/openapi.yaml) (provider settings). LDAP (WBS-4.2) comes next week and reuses the account linking below.

## What the admin configures (FR-21)

An admin adds a provider in the Admin plane: a short **slug** (used in URLs), a **display name** for the sign-in button, the **issuer URL**, the **client ID** and the **client secret**.

- Before saving, git2 fetches `<issuer>/.well-known/openid-configuration` and checks that it answers, that its `issuer` is exactly the one entered, and that it lists an authorization and a token endpoint. Otherwise nothing is saved (400, with the reason).
- The provider's answer shows the **redirect URI** to register at the provider: `GIT2_EXTERNAL_URL/api/auth/oidc/<slug>/callback`.
- The client secret is **write-only**: it is never returned, only whether one is set.
- A provider can be **disabled** (UC-05 A2): it disappears from the sign-in page and its sign-ins are refused; accounts created through it stay.
- Deleting a provider deletes its identity links, never the accounts.

The client secret is stored encrypted with AES-256-GCM. The key comes from `GIT2_SECRET_KEY` (32 bytes, base64), never from the database, so a database dump or backup alone does not reveal it. Without the key git2 refuses to save a provider (503) and OIDC sign-in is off.

## How a user signs in (FR-22)

```
browser            git2 (auth-svc)                     provider
   │ GET /api/auth/oidc/<slug>/start?return_to=/x │
   │──────────────────────────▶│ state, nonce, PKCE verifier → Redis (10 min)
   │◀── 302 to provider ───────│
   │────────────────────────────────────────────────────▶│ user signs in
   │◀──────────────── 302 /api/auth/oidc/<slug>/callback?code&state
   │──────────────────────────▶│ state from Redis (used once)
   │                           │ code + verifier ─────────▶│
   │                           │◀──────────── ID token ────│
   │                           │ check signature, issuer, audience, nonce
   │                           │ find, link or create the account
   │                           │ one-time handoff code → Redis (60 s)
   │◀── 302 /sign-in/callback?code=<handoff> (or /admin/sign-in/callback)
   │ POST /api/auth/oidc/exchange {code}
   │──────────────────────────▶│ handoff code used once
   │◀── 200, tokens in X-Access-Token / X-Refresh-Token
```

**Why the handoff code.** Tokens travel only in response headers. A browser redirect cannot give headers to the web app, and putting tokens in a URL would leak them into history and logs. So the callback redirects with a one-time code that lives 60 seconds, and the web app trades it for tokens with a normal API call, exactly like a password sign-in.

**`return_to`** is kept with the state, and only a path on this instance is accepted (it starts with one `/`), so a sign-in link cannot send users to another site. It also picks the plane: a path under `/admin/` comes back through `/admin/sign-in/callback`, anything else through Dash's `/sign-in/callback`. Without it the user lands on `/`.

**Errors** come back to the sign-in page as `?sso_error=<reason>`, never as a provider's raw message: `cancelled` (the user cancelled at the provider), `provider_unavailable` (UC-06 E2), `account_disabled` (UC-06 E3), `email_in_use` (see below), `failed` (anything else, logged on the server). Password sign-in keeps working when a provider is down.

## Which account the identity belongs to

An identity is the pair (provider, `sub` claim). In order:

1. **Known identity** → its account.
2. **Verified email that matches an account** (`email_verified` is true and the address equals an account's email) → that identity is linked to the account, and the user is signed in (FR-22).
3. **No account with that email** → a new account is created with the `user` role and no password (it signs in only through the provider). FR-20 allows this as the only self-service way to get an account.
4. **An account has that email, but the provider does not say it is verified** → refused with `email_in_use`. Linking on an unverified address would let anyone who can set that address at the provider take the account over.

A disabled account is refused in every case (UC-06 E3), even after the provider accepted the user.

**Username of a new account:** the `preferred_username` claim if it is a valid, free, not reserved username; otherwise the part of the email before `@`, cleaned to the username rules; if that is taken, `-2`, `-3` and so on are added. The user cannot change it in v0 (usernames are part of repository URLs).

## Storage

Migration `007_auth_oidc.sql` (after ci-svc's `006`):

- `auth_svc.oidc_providers`: id, slug (unique, same rule as team names), display name, issuer, client ID, encrypted client secret, enabled, timestamps.
- `auth_svc.user_identities`: provider, subject, user, the email the provider gave, created and last sign-in time. Primary key (provider, subject); one identity per user and provider. Deleting a provider deletes its rows; a user with identities cannot be deleted (users are only disabled anyway).

Redis keeps only the short-lived `oidc-state:<state>` (10 minutes) and `oidc-handoff:<code>` (60 seconds), each read and deleted in one step so it works once. Losing Redis only cancels sign-ins in progress (NFR-21).

## Tests

The handler tests run a fake OIDC provider inside the test (`httptest`): discovery document, signing keys, authorization and token endpoints, signed ID tokens. They need no network and cover the whole flow, every error and the linking rules. An end-to-end test against Keycloak can join CI later (FR-21/FR-22 acceptance).

## Open for the web side (Bilge)

- The sign-in pages show one button per entry of `GET /api/auth/oidc/providers` and link it to `/api/auth/oidc/<slug>/start?return_to=…`.
- New routes `/sign-in/callback` and `/admin/sign-in/callback`: post `code` to `/api/auth/oidc/exchange`, then go to `return_to`; on `sso_error` show the message on the sign-in page.
