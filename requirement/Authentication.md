# Authentication & sessions requirements

Login issues an **HTTP-only, server-side session cookie** (`odtbank_session`). Sessions have no rotation, device management, password reset, email verification, or rate limiting.

## Sign in — `POST /login`
- Body: `{email, password}`; verifies credentials and sets the session cookie.
- On failure returns `401` `invalid email or password`.
- The cookie may be marked secure when `COOKIE_SECURE=true` (serving over HTTPS).

## Identity & access
- `GET /me` returns the principal role and current onboarding status for the session.
- Banking endpoints (`/transfer`, `/deposit`, `/withdraw`, `/accounts`) require a logged-in, **approved** customer; waiting/rejected customers are rejected.
- Admin endpoints (`/admin/...`) require an admin principal; a customer principal is rejected with `403`.
- Account-scoped reads are owned per customer: a customer may read only their own account; reading another account is `403`. Admins may query any account.
- Logout invalidates the session and clears the cookie.

## Password storage
- Passwords are PBKDF2-SHA256, 600,000 iterations, random 16-byte salt, 32-byte key, encoded `pbkdf2_sha256$<iters>$<salt>$<hash>`.
- Verification uses a constant-time comparison.

## Admin provisioning
- In-memory demo: `ADMIN_EMAIL` / `ADMIN_PASSWORD` seed a dev admin at startup (must be set together).
- PostgreSQL: `cmd/admin` upserts an admin by email; two admins are needed to exercise dual-control adjustments.

## Limits / non-goals
- No session expiry rotation, device management, or rate limiting.
- Credentials are matched on normalized (lowercased) email.
