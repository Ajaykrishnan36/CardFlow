# CardFlow + CRM — Handover

Everything needed to keep working on this project, including from a new Claude session.
**This repository is public.** Never put passwords, tokens, API keys or private emails in
this file or anywhere else in the repo. Secrets live only in Render/Vercel settings and in
your local `.env` files (which git ignores).

Tip for a new Claude session: start with *"Read HANDOVER.md and
backend/internal/crm/docs/DECISIONS.md first."*

---

## 1. What this is

- **CardFlow** is a business-card scanning and business-discovery app: a mobile app
  (Capacitor Android/iOS) plus a web app, both in `frontend/`, with a Go API in `backend/`.
- **The CRM** is a Salesforce/Twenty-style CRM built inside the same repo:
  - Backend: `backend/internal/crm`, served at `/api/crm/v1`.
  - Frontend: `frontend/src/crm`, served at `/crm`.
- **Owner console** (`/crm/owner/...`): the platform owner manages products (customer
  workspaces), apps, objects, users, integrations and the audit log.
- **Product CRM** (`/crm/w/<product-code>/...`): what each product's people use
  (Super Admin, Admin, Staff, End user).
- **Business Card Snap**: the CardFlow app connected to the CRM as a product. App users,
  cards and support tickets sync into it.

## 2. Live URLs

| What | URL |
|---|---|
| Web app + CRM (Vercel) | https://card-flow-kappa.vercel.app |
| Product sign-in | https://card-flow-kappa.vercel.app/crm/login |
| Owner console sign-in | https://card-flow-kappa.vercel.app/crm/owner/login |
| API + CRM (Render) | https://cardflow-api-fsij.onrender.com |
| API health check | https://cardflow-api-fsij.onrender.com/health |
| GitHub repo (live deploys from this one) | https://github.com/Ajaykrishnancreations/CardFlow (branch `main`) |
| GitHub copy | https://github.com/Ajaykrishnan36/CardFlow (a separate repo; pushing only here deploys nothing) |

How the pieces connect:
- Vercel serves the web app. `frontend/vercel.json` forwards `/api/crm/*` to Render.
- Render runs the Go server (`render.yaml`). It also serves a built copy of the web app
  from `backend/cmd/api/dist` (the "embedded bundle").
- The live database is Postgres on Neon. Render connects to it through `DATABASE_URL`.
- Render's free plan sleeps when idle, so the first request after a while takes about
  30–60 seconds.
- Pushing to `main` redeploys both Render and Vercel automatically.

## 3. Git: signing in and pushing

The old setup kept a GitHub token inside the remote URL. That token stopped working, and it
was shown on screen, so **revoke it**: GitHub → Settings → Developer settings → Personal
access tokens → delete any token you no longer use.

Set up clean access once, in your own Terminal (not inside a sandboxed tool):

```bash
brew install gh
```
```bash
gh auth login
```
Choose: GitHub.com → HTTPS → "Login with a web browser".
```bash
gh auth setup-git
```
Make sure the remote has no token in it:
```bash
git -C ~/Documents/CardFlow remote set-url origin https://github.com/Ajaykrishnan36/CardFlow.git
```

Then pushing is just:
```bash
git -C ~/Documents/CardFlow push origin main
```

If you use a personal access token instead of `gh`:
- Create a **fine-grained** token for this one repo, with *Contents: Read and write*.
- Paste it only when git asks for a password (the username is your GitHub username).
- Never paste it into the remote URL, a file or a chat.

Notes:
- If a push says *"Failed to connect to github.com port 443"* but github.com opens in your
  browser, the command ran inside a sandbox with no network. Run it in a normal Terminal.
- `Ajaykrishnancreations/CardFlow` and `Ajaykrishnan36/CardFlow` are two separate repos (not a rename). Render and
  Vercel build from `Ajaykrishnancreations/CardFlow`, so every change must reach that repo's `main`. This Mac pushes
  there over SSH (key `~/.ssh/id_ed25519_github_cloudbytelabs`, which signs in as Ajaykrishnancreations):
  `git -C ~/Documents/CardFlow push git@github.com:Ajaykrishnancreations/CardFlow.git main`.
  The saved HTTPS login (`Ajaykrishnan36`) has no write access to it.

## 4. Running it on your Mac

Prerequisites: Go, Node 20+, Postgres (local database `cardflow_db`), Redis (optional).

```bash
cd backend && go run ./cmd/api/
```
This starts the API on http://localhost:8080. It runs database migrations and, locally,
seeds demo data.

```bash
cd frontend && npm install && npm run web -- --port 3001
```
This starts the web app with hot reload on http://localhost:3001. It forwards `/api` to
port 8080.

- Local CRM: http://localhost:3001/crm/owner/login.
- The local demo owner account comes from the seed and is documented in DECISIONS.md
  (the "Seed" section). It only exists locally; production refuses to create it.
- To test emails without really sending them, start the backend with no mail settings.
  Emails are then printed in the log instead:

```bash
cd backend && CRM_SMTP_HOST= CRM_BREVO_API_KEY= go run ./cmd/api/
```

`.claude/launch.json` has these as ready-made preview configs: `cardflow-backend`,
`cardflow-backend-console-mail` and `cardflow-frontend`.

## 5. Settings (environment variables)

Names only. Set the values in the Render dashboard (live) or `backend/.env` (local).
Never commit the values.

CRM settings:
- **Core:**
  - `CRM_APP_ENV`: `local` or `production`.
  - `CRM_BASE_URL`: the public URL, used in links and sign-in callbacks.
  - `CRM_APP_NAME`
  - `CRM_ENCRYPTION_KEY_BASE64`: encrypts stored secrets. Keep it the same forever;
    changing it makes saved mailbox and SSO secrets unreadable.
  - `CRM_COOKIE_SECURE`
- **Owner bootstrap:** `CRM_OWNER_EMAIL`, `CRM_OWNER_BOOTSTRAP_PASSWORD`.
- **Email:** `CRM_BREVO_API_KEY`, or SMTP with `CRM_SMTP_HOST`, `CRM_SMTP_PORT`,
  `CRM_SMTP_USER`, `CRM_SMTP_PASS` and `CRM_SMTP_FROM`.
- **Sign in with Google / Microsoft / LinkedIn, and mailbox connect:**
  `CRM_GOOGLE_CLIENT_ID`/`_SECRET`, `CRM_MICROSOFT_CLIENT_ID`/`_SECRET`/`_TENANT`,
  `CRM_LINKEDIN_CLIENT_ID`/`_SECRET`.
- **Business Card Snap sync:** `CRM_CARDFLOW_SYNC`, `CRM_SYNC_INTERVAL`.
- **Demo data:** `CRM_SEED_DEMO` (local only; never seeded again after a fresh start).
- **Sign-in codes by SMS:** `SMS_PROVIDER` (`msg91`, `twilio` or `fast2sms`) with
  `SMS_AUTH_KEY` (MSG91 also needs `SMS_OTP_TEMPLATE_ID`; Twilio uses
  `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM`). `DEV_MOCK_SMS=true` on a
  server whose `ENV` isn't `production` shows the code on screen instead of sending it.
  `OTP_PREVIEW_INSECURE=true` does the same anywhere — never leave it on for real users.
- **Fresh start:** `CRM_FRESH_START` — see "Fresh start" below.

### Fresh start (erase all customer data)

Setting `CRM_FRESH_START=erase-everything-<label>` (for example
`erase-everything-2026-10-05`) and restarting the server **permanently erases every
customer's data** except one account.

Kept:
- the platform owner's login;
- the person with mobile 98765 43211 (their profile and their own saved cards);
- that person's business named "Ajay traders", with its records and public listing;
- setups, plans, settings and directory categories.

Erased: every other person, business (including that person's other businesses), record,
card, listing, ticket, session, file, message and audit entry. If the kept person or
business doesn't exist yet, it is created (empty). No sample records are added.

- Take a database backup first. There is no undo.
- Each value runs once. Leaving the setting in place, or restarting, does not erase again.
  To erase again later, change the label.
- It all happens in one transaction: if anything fails, nothing is erased (the log says why).
- Check the server log for `fresh start complete`.
- Nothing in the app or the API can trigger it; only this setting can.
- To keep a different number or business name: `CRM_FRESH_START_PHONE`,
  `CRM_FRESH_START_BUSINESS`.
- The "Business Card Snap" row in the owner console is the system workspace that receives
  the app's support tickets and sign-ups. It is emptied, not removed (the server needs it).

App settings: see `.env.example`. It covers the database, Redis, S3, JWT, Gemini, SMS,
KYC, RevenueCat, maps and so on.

- The RevenueCat **secret** key (`sk_…`) belongs only in Render.
- Only the public RevenueCat keys may go into the app.

Still to do on Render, when you want those features live:
- Add the Google, Microsoft and LinkedIn client IDs and secrets.
- Register this callback URL in each provider's console:
  `<CRM_BASE_URL>/api/crm/v1/oauth/<provider>/callback`, where `<provider>` is `google`,
  `microsoft` or `linkedin`. Use exactly the URL `CRM_BASE_URL` is set to on Render.

## 6. Code map

```
backend/
  cmd/api/                 server entry point; dist/ = embedded web bundle (built, committed)
  internal/crm/
    crm.go                 wires the CRM module, routes, policies
    access/                roles, permission sets, role hierarchy, navigation
    identity/              sign-in: password, email code, OAuth, MFA, sessions, sign-up, invite links
    platform/              owner console: products, apps, dashboard, users, audit
    records/               the CRM engine: objects, fields, records, views, filters, timeline,
                           email, workflows, reports, dashboards, API keys, webhooks, SSO, recycle bin
    connectors/cardflow/   Business Card Snap sync (app users, cards, tickets ↔ Cases)
    mail/, oauth/, shared/ email senders, OAuth providers, helpers
    store/migrations/      CRM database migrations (0001 … 0016), applied at startup
    docs/DECISIONS.md      every design decision, D-01 … D-87 — read this first
frontend/
  src/crm/
    app.tsx                routes
    api/                   API client, endpoints, types
    features/              pages: records, owner, products, users, tools, shell, auth…
    components/            UI kit, rich-text editor
    i18n/                  all CRM wording (English)
  src/ …                   the CardFlow app itself
```

## 7. Release routine (what to do for every change)

Run everything from the repo root.

1. **Check:**
```bash
cd backend && gofmt -l ./internal/crm && go vet ./internal/crm/... && go test ./internal/crm/...
```
```bash
cd frontend && npx tsc -p src/crm
```
2. **Build the web bundle.** Move `frontend/.env.local` aside while building so local
   settings don't get baked into the live bundle:
```bash
cd frontend && mv .env.local /tmp/env.local.bak && npm run build; mv /tmp/env.local.bak .env.local
```
3. **Copy it into the backend:**
```bash
rm -rf backend/cmd/api/dist && cp -R frontend/dist backend/cmd/api/dist && (cd backend && go build ./...)
```
4. **Commit and push.** The convention is one commit for the change, then one called
   `build: refresh embedded web bundle`.
5. **Check it's live.** The bundle file name in the live page should match the local
   `backend/cmd/api/dist/index.html`:
```bash
curl -s https://cardflow-api-fsij.onrender.com/crm/login | grep -o 'bundle\.[a-f0-9]*\.js' | sort
```
6. Add a short `D-xx` entry to `backend/internal/crm/docs/DECISIONS.md` for anything
   non-trivial.

## 8. Rules this project follows

- Never commit `.env`, `.env.local`, keys or tokens. The repo is public.
- Database migrations only **add** things. Never delete live business data.
- Don't test against the live database, and don't sign in to live with real accounts
  for testing. Test on your Mac.
- Test emails locally with the "console mail" backend so nothing is really sent.
- Don't copy code from Twenty (it's AGPL). Match features, write our own code.
- Features ship as working slices. No "coming soon" placeholders.

## 9. Where things stand (Oct 2026)

**The app and the CRM are one product now** (D-93 to D-107):
- **One sign-in** for everyone at `/` and `/crm/login`: mobile number + code, email +
  password, or email code. Only the owner console has its own (`/crm/owner/login`).
- **One set of addresses.** After sign-in, a phone-sized screen (and the native app) gets
  the phone layout, a computer gets the desktop CRM — same URL, same session, same data.
- **A customer creates their own business** (a CRM workspace) and can have several; each
  keeps its own records. Each business gets a public listing with its digital card.
- **Business cards** are scanned into the open business and saved as a lead or contact.
- **Plans** (Free / Pro / Business) belong to a business and are enforced on the server.
- **Profile:** verified mobile, add and verify an email, set a password.

Not built yet from the unified plan: owner-console screens for plans and subscriptions
(the API exists: `/platform/plans`, `/platform/subscriptions`), a plan page on the desktop
CRM, push notifications, account export/delete, a separate "platform support" queue.

Earlier work (see DECISIONS.md for details):
- **Records:** rich text, currency and phone codes, grouped tables, running workflows from
  a record, objects owned by a product.
- **Email:** conversations with Reply (D-80).
- **Navigation:** keyboard shortcuts (D-81).
- **Sign-in:** OpenID Connect SSO (D-82), invite links limited to company domains (D-83).
- **Recycle bin:** optional automatic emptying, off by default (D-84).
- **Setup wizard:** statuses and stages are edited per object; lead conversion moved to
  the Objects step (D-85).
- **Owner console:** the Product/App picker moved from the sidebar into each page (D-86).
  Overview cards open lists filtered like their counts (D-87).

Gaps compared with Twenty (not built yet, roughly by priority):
1. Default values for fields (dropdown default, checkbox default, currency and phone
   default per field).
2. True many-to-many links, showing on both records.
3. Customisable sidebar: reorder, folders, hide items, custom links.
4. Record page built from tabs and movable widgets (charts, embeds…).
5. Dashboards with tabs, a drag grid, pie charts, text and embed widgets.
6. Excel (XLSX) import, a template download, and fixing bad rows on screen.
7. Resizable table columns (widths are already saved; there's no drag handle yet).
8. Workflow: a form step in the middle of a run, and a "Test" run.
9. Medium priority:
   - email folder choice and company auto-create from the email domain;
   - CalDAV, and pushing edited CRM events back to Google or Outlook;
   - bounce and spam tracking for campaigns;
   - "view as user";
   - product logo upload;
   - a proper search index.

Deliberately left out: AI chat and agents, an MCP server, other languages, right-to-left
layout.

## 10. Checklist for you

- [ ] Revoke the old GitHub token and set up `gh auth login` (section 3).
- [ ] Add the Google, Microsoft and LinkedIn keys on Render if you want those sign-ins
      live (section 5).
- [ ] Keep `CRM_ENCRYPTION_KEY_BASE64` backed up somewhere safe (a password manager).
- [ ] Get SMS sending working (Fast2SMS refused requests: "IP is blacklisted from Dev API
      section" — ask Fast2SMS to allow the server, or switch to MSG91). Then set
      `DEV_MOCK_SMS=false` and `ENV=production` on Render so codes are sent, not shown.
- [ ] Vercel: the project must serve `/api/*` from Render (it's in `frontend/vercel.json`).
