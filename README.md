#### TODO

    - Ensure topnav returns current top categories

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
