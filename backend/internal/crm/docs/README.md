# Ajay's CRM — developer notes

Lives inside the CardFlow repo without touching CardFlow code:

- Frontend: `frontend/src/crm/**` (React 18 + TypeScript + Tailwind 3), served at `/crm/*`.
- Backend: `backend/internal/crm/**` (Go, chi, pgx), API at `/api/crm/v1/*`, Postgres schema `crm`.
- Decisions, deviations from the PRD and milestone status: [`DECISIONS.md`](DECISIONS.md).

## Run locally

```bash
cd backend && go run ./cmd/api          # applies crm migrations + seed on start
cd frontend && npm run web -- --port 3001
```

Open http://localhost:3001/crm/login. Demo accounts (seeded only when `CRM_APP_ENV` is local/dev):

| Who | Email | Password | Lands on |
|-----|-------|----------|----------|
| Platform owner | ajay@gmail.com | Ajay1234 | /crm/owner/dashboard |
| Super Admin of `acme-demo` | superadmin@acme-demo.test | Demo1234 | /crm/home |

MFA is optional in `local` and required for privileged roles everywhere else. Password-reset and
new-device emails are printed to the API log (console mailer) unless `CRM_SMTP_HOST` is set.

Checks:

```bash
cd frontend && npm run crm:typecheck
cd backend && go test ./internal/crm/...
```

## Production (Render + Vercel)

Until these are set on Render, the CRM stays disabled (every `/api/crm` call returns
`503 crm_disabled`) and CardFlow is unaffected:

| Variable | Value |
|----------|-------|
| `CRM_ENCRYPTION_KEY_BASE64` | `openssl rand -base64 32` (keep it stable — it encrypts MFA secrets) |
| `CRM_OWNER_EMAIL` | owner's email |
| `CRM_OWNER_BOOTSTRAP_PASSWORD` | ≥ 12 chars, one-time; change it at first sign-in, then delete the variable |
| `CRM_BASE_URL` | web origin used in emails, e.g. `https://card-flow-kappa.vercel.app` |
| `CRM_SMTP_HOST` / `_PORT` / `_USER` / `_PASS` / `_FROM` | optional; without them emails only go to the log |

`CRM_APP_ENV` defaults to `production` when CardFlow's `ENV=production`. Never set
`CRM_SEED_DEMO=true` in production (the module refuses to start).

The Vercel frontend reaches the API through the `/api/crm/:path*` rewrite in
`frontend/vercel.json`, keeping the HttpOnly session cookie first-party.
