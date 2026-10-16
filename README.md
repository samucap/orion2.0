#### TODO

    - Ensure topnav returns current top categories

### Refresh cookie and CSRF

The frontend proxies `/api/*` through its own domain, so the browser treats the API as same-origin and the refresh cookie as first-party.

- **Cookie:** `refreshToken`, Path `/api/auth`, HttpOnly, Secure, `SameSite=Strict`. Browsers do not attach it to requests started by other sites.
- **Content-Type:** every `/api` request with a body must be `application/json` (415 otherwise). Cross-site HTML forms cannot send JSON, and a cross-site `fetch` with JSON triggers a CORS preflight that the allow-list rejects.
- **Origin check:** `POST /api/auth/refresh-token` (cookie-only auth) returns 403 when `Origin` is present and does not match `CLIENT_URL`, or when `Origin` is missing and `Sec-Fetch-Site` is cross-site. Requests with neither header (curl, server-to-server) pass because they carry no browser cookies.
- **`CLIENT_URL`** must be the public frontend origin, for example `https://app.example.com` (scheme, no path). Empty allows `http://localhost:*` for local development.

Calling the API directly from another site (no proxy) would require `SameSite=None` plus CSRF tokens, and Safari would still block the cookie as third-party.

### Local Docker

`.env` is excluded from the image build (see `.dockerignore`). Compose injects it at **run** time.

```bash
cp .env.example .env   # set JWT_SECRET (required); Postgres vars optional
docker compose up --build
curl -sS http://127.0.0.1:8080/health
```

One-off without Compose:

```bash
docker build -t orion2.0:local .
docker run --rm -p 8080:8080 --env-file .env \
  --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  orion2.0:local
```
