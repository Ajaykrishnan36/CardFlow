# Ajay's CRM — Decisions & Status

Source of truth: "Ajay's CRM Core — End-to-End PRD (React + Go + PostgreSQL)" (58 pages, 25 Sep 2026).
This file records every place the implementation deviates from, or fills a gap in, the PRD
(PRD §0: "choose the simplest secure option … write it down in docs/DECISIONS.md, and continue").

## Milestone status

| Milestone | Status | Notes |
|-----------|--------|-------|
| M0 Foundation | done (adapted) | Module + schema + migrations + router mount; React shell, design system, router, API client, i18n. Not done: docker-compose/Makefile/CI/OpenAPI codegen (see D-03/D-04 — this repo's existing tooling is used instead) |
| M1 Identity | done | Password login (owner + workspace audience), sessions (idle/absolute, rotation), CSRF, lockout, TOTP MFA + recovery codes (replay-protected), forgot/reset/change password, new-device email, /me, /me/sessions, /capabilities, seed, production guard. Verified in browser + curl + Go unit tests |
| M2 Platform | done (except /select-workspace) | Products (list, 6-step wizard with draft save per step, versions, publish/archive/restore, impact note), customer workspaces (provision wizard, idempotent — OWN-02; suspend; product assignment + version upgrade; invite/resend/revoke), owner dashboard with 6 clickable KPIs, Users & Access (list, detail, edit, reset link, sign-out-all, MFA reset, membership role/status), audit log |
| M3 CRM (platform slice) | started | Platform CRM for the owner: leads, accounts, contacts on a metadata-driven record engine (list/detail/inline edit/optimistic concurrency), Salesforce-style default layouts, page layout editor (hide/show, sections, highlights, drag & drop), custom fields, lead → account + contact conversion with optional workspace + Super Admin invitation, /accept-invite. Not yet: CRM modules inside customer workspaces, policy engine, RLS, opportunities |
| M3 Access | started | Editable + custom roles, delegated admin within own access (PERM-03), permission sets, effective access with sources, product gating, member workspace app at /crm/w/:ws with enforcement, give-login (invite or temporary password). Not yet: field-level security, deny rules, RLS, workspace Super Admins managing their own users |
| M4–M10 | not started | |

### Verified on 2026-09-25
- Desktop (1440×900), tablet (768), mobile (375), light + dark, owner console.
- Deep link → login → MFA → back to the deep link; wrong password; empty-field validation;
  lockout at 5; forgot → console email → reset → login banner; Super Admin gets 403 on owner API
  and "no access" in the UI; unbuilt nav items show a "coming soon" page.
- CardFlow at `/` unaffected: no CRM CSS/root/viewport change; CRM initial JS ≈ 193 KB gzip.

## Hosting constraints (why this differs from the PRD's greenfield monorepo)

The CRM is built **inside the existing CardFlow repository**, which is live (Vercel web, Render Go API,
Neon Postgres, Capacitor iOS/Android). Hard rule from the owner: do not touch CardFlow code.

- **D-01 Folder layout.** Frontend: `frontend/src/crm/**`. Backend: `backend/internal/crm/**`.
  Shared files are only touched additively: `frontend/webpack.config.js`, `frontend/babel.config.js`,
  `frontend/package.json`, `frontend/vercel.json`, `backend/cmd/api/main.go` (one `Mount` call),
  `backend/go.mod`. No CardFlow component, screen, handler or table is modified.
- **D-02 Routes.** SPA routes live under `/crm/*` (entry: `/crm/login`). API lives under
  `/api/crm/v1/*` instead of the PRD's `/api/v1/*`, because CardFlow already owns `/api/v1`.
- **D-03 Frontend stack.** CardFlow is React Native Web + webpack (JS). The CRM keeps the same
  webpack build but adds TypeScript (babel `preset-typescript`, `tsc --noEmit` for types), Tailwind 3
  (postcss-loader, config scoped to `src/crm`), React Router 6, TanStack Query 5. A tiny new entry
  (`src/entry.js`) lazy-loads either the untouched CardFlow `src/index.js` or the CRM, so neither
  app downloads the other's code. PRD asks for Vite + pnpm workspaces; switching CardFlow's build
  tool would violate D-01, so webpack stays.
- **D-04 Database.** Same Postgres database as CardFlow, but every CRM object lives in schema `crm`
  so no CardFlow table can collide or be touched. Migrations are embedded SQL files run in order at
  API start-up and tracked in `crm.schema_migrations` (goose-compatible numbering, no new CLI tool on
  Render's free tier).
- **D-05 Same-origin cookies.** Session and CSRF cookies are named `crm_sid` / `crm_csrf` (PRD:
  `sid` / `csrf`) to avoid clashing with anything else on the shared origin. Web on Vercel reaches
  the API through a Vercel rewrite of `/api/crm/*` to Render, so cookies stay first-party.
  Local dev: webpack-dev-server proxies `/api/crm` to `:8080`.
- **D-06 Failure isolation.** If CRM configuration is unsafe in production (demo seed enabled,
  demo credential present, or no encryption key), the CRM module does **not** crash the process
  (that would take CardFlow down); it mounts a stub that answers every `/api/crm/*` call with
  `503 crm_disabled` and logs the reason. PRD says "refuses to start" — scoped to the CRM module.

## Security / identity

- **D-10 Environment.** `CRM_APP_ENV` (local|dev|staging|production). Default: `production` when
  CardFlow's `ENV=production`, otherwise `local`.
- **D-11 MFA enforcement.** Required for privileged identities (platform owner, Super Admin, Admin)
  in dev/staging/production; optional in `local` (PRD §13.5: "MFA optional only in local").
  Anyone who has enrolled MFA is always challenged.
- **D-12 Unified login.** `/crm/login` accepts both workspace members and the platform owner (the
  server resolves the role; no role selector). `/crm/owner/login` is the dedicated owner console
  (audience=owner rejects non-owners with the same generic error).
- **D-13 Two-step session.** Password success creates a session with `mfa_required=true,
  mfa_passed=false`; it can only call `/me`, MFA endpoints and logout until MFA passes, then the
  token rotates. Column `sessions.mfa_required` is an addition to PRD §13.2.
- **D-14 Enumeration-safe lockout.** 5 failures lock for 15 min per identity (DB) *and* per
  normalised identifier string (memory), so unknown and known identifiers behave identically.
  Locked / rate-limited responses are `429 too_many_attempts` for both.
- **D-15 CSRF on login too.** Every non-GET under `/api/crm/v1` needs `X-CSRF-Token` equal to the
  `crm_csrf` cookie; the cookie is issued on any response that lacks it (client calls
  `GET /auth/csrf` first).
- **D-16 Mail.** `console` mailer (logs the full message, including reset links) unless
  `CRM_SMTP_HOST` is set. PRD uses Mailpit locally; not installed here.
- **D-17 Production owner bootstrap.** Render free tier has no shell, so `cmd/seed --owner` is
  replaced by env: `CRM_OWNER_EMAIL` + `CRM_OWNER_BOOTSTRAP_PASSWORD` create the owner once with
  `must_change=true`; the password must be changed and MFA enrolled at first login.
- **D-18 Encryption key.** `CRM_ENCRYPTION_KEY_BASE64` (32 bytes, AES-256-GCM) encrypts TOTP
  secrets. Local falls back to a fixed, documented dev key; production without it → D-06.
- **D-19 RLS timing.** Identity/platform tables are global (PRD). Row-level security arrives in M3
  together with the tenant tables and the tenant transaction helper. Note: locally the app connects
  as a Postgres superuser, which bypasses RLS — M3 must add a non-superuser test role for the
  isolation suite. Neon cannot grant `BYPASSRLS`, so owner operations will use a transaction-local
  `app.owner_ops` flag checked by the policies instead of a separate DB role.
- **D-20 Async work.** No River worker yet (nothing needs it in M0–M2); `outbox_events` exists and
  is written in the same transaction as audits from M3 on.

## Platform CRM (M2/M3 slice, 25 Sep 2026)

- **D-21 Platform CRM first.** The owner asked for Salesforce-like leads → convert → account → login before
  the M3 policy engine exists. Records live in the *platform workspace* and are owner-only
  (`/api/crm/v1/platform/crm/*`). The engine (`internal/crm/records`) is written against a workspace id, so
  customer workspaces reuse it once M3 adds the policy engine, RLS and workspace-scoped routes.
- **D-22 Lead product.** PRD `leads.product_id` references `workspace_products`; platform leads are about a
  global product the prospect is interested in, so `product_id` references `products` and is nullable.
- **D-23 Contact → account.** `contacts.account_id` holds the primary account instead of the PRD's
  `account_contacts` join table (added when multi-account contacts are needed).
- **D-24 Standard vs custom fields.** Standard fields are typed columns declared once in `records/spec.go`
  (key, column, type, options); custom fields are rows in `crm.field_definitions` with values in the record's
  `custom` jsonb. Archiving a custom field keeps its values, so reviving the key restores them. Instead of the
  PRD's `object_definitions` table, objects are keyed by `object_key` until custom objects arrive (M5).
- **D-25 Layouts.** One published layout per object (`crm.layouts`), `{highlights (≤6), sections[{id,title,
  columns,fields}]}`. A field not placed in a section is hidden. Defaults place the maximum set of standard
  fields (Salesforce "Lead/Account/Contact information, Address, Description, System" sections); "Reset to
  default" deletes the row. Role-specific layouts come with the policy engine.
- **D-26 Conversion + access.** Convert creates the account (`A-000001`), optional contact (`C-000001`), marks
  the lead converted (kept, read-only status) and writes `lead_conversions` — in one transaction, idempotent
  via `Idempotency-Key`. "Give a login" additionally provisions a customer workspace and invites the lead as
  its Super Admin; access only starts when the invitation is accepted (PRD CRM-01/02). Custom values carry
  over when the target object has a custom field with the same key.
- **D-27 Local invitation/reset links.** Like CardFlow's on-screen OTP in development, `devAcceptUrl` /
  `devResetUrl` are returned only when `CRM_APP_ENV` is local/dev; production only emails them.
- **D-28 Accept-invite errors.** A wrong existing password on `/invitations/accept` is `422 fieldErrors.password`
  (not 401), so the SPA doesn't treat an anonymous form error as an expired session.

## Access control (M3 slice, 25 Sep 2026)

- **D-29 Permission sets per workspace.** `crm.permission_sets` (+ description, timestamps) belong to one
  workspace; the owner manages them for every workspace, including their own platform team. Rules shape:
  `{objects: {lead|account|contact: [read,create,update,delete,convert,export]}, rows: {obj: {scope: own|workspace}},
  capabilities: [dashboard.view, metadata.manage]}`. Any action implies read; unknown keys are dropped (`access.Normalize`).
- **D-30 Effective access = role ∪ permission sets, gated by products.** System-role rules live in code
  (`access.SystemRoles`), not in `roles.base_rules`, so fixes reach existing workspaces. END_USER grants nothing,
  so "End user + permission sets" gives exactly the chosen access. Broadest row scope wins. An object is usable
  only if one of the member's products (pinned product version → `modules`) enables it; the platform workspace
  has no products and all CRM modules. No deny rules yet.
- **D-31 Enforcement.** The same record engine is mounted at `/platform/…` (owner, everything) and `/w/{code}/…`
  (members). Every request recomputes access from the DB (no caching), checks the action, and applies own-scope
  filters to lists, details, updates, deletes, related lists, dashboard and lookups. Members can't give logins or
  provision workspaces. `/capabilities` and `/w/{code}/context` build the sidebar from the same result.
- **D-32 Giving a login.** From a lead/account/contact, the Users page, or the convert dialog: pick workspace
  (platform team / existing / new), role, products, permission sets, and either an invitation (72 h) or a
  temporary password (`must_change=true`, active immediately). Existing passwords are never overwritten;
  permission sets on an invitation are applied when it is accepted.
- **D-33 No secrets in custom fields.** Custom fields whose label/key looks like a password/PIN/OTP/secret are
  rejected; values would be readable by anyone who can open the record.

- **D-34 Editable and custom roles.** Roles are per workspace. Built-in roles keep following the code defaults
  until edited (`roles.customized`), then use their stored rules; "Reset to default" clears that. Built-in roles
  can't be renamed or deleted. Custom roles get an upper-snake key from their name and rank 60 (80 — privileged,
  MFA outside local — when they carry an admin capability). Deleting a role in use → 409 `role_in_use`.
- **D-35 Delegated administration (PERM-03).** Capabilities `access.manage` (roles & permission sets) and
  `members.manage` (users) let a member administer their own workspace at `/w/{code}/admin/*`. Every grant —
  role rules, permission sets, row scope, capabilities, products — must be within the caller's own effective access
  (`access.Exceeds`), else 403 `exceeds_your_access` naming each offending grant. They can't edit themselves, the
  platform owner, anyone whose access exceeds theirs, or a role/set that already exceeds theirs. Built-in roles don't
  include these capabilities by default; the owner grants them (owner's instruction, 28 Sep 2026).

## Connected app: Business Card Snap / CardFlow (28 Sep 2026)

- **D-36 Connector, not code hooks.** `internal/crm/connectors/cardflow` reads the app's tables every 5 s
  (`public.users`, `public.support_tickets`) with a watermark in `crm.connector_state`. On first start it creates the
  product `business_card_snap` (v1), the customer workspace `business-card-snap`, app custom fields (accounts:
  plan, subscribed, last app sign-in, saved cards…) and an "App profile" layout section. Each app user becomes a lead
  (source `app_signup`) converted at once into an individual account + contact, linked in `crm.external_links`; the
  app is the source of truth for the synced profile fields. Sign-ins (`users.last_login_at` moving) and tickets become
  `crm.activities` (deduplicated). App admins (`role = admin`, with an email) get a Super Admin invitation; that
  workspace's Super Admin role includes all capabilities ("give all access"). Disable with `CRM_CARDFLOW_SYNC=false`.
- **D-37 Support tickets persisted in the app.** They were in-memory in `internal/support` (lost on restart). The one
  change to CardFlow code: migration `012_support_tickets.sql` + the handler now reads/writes that table (same API and
  JSON; in-memory fallback only without a database; the two hard-coded sample tickets are no longer shown). The CRM
  answers tickets by writing `admin_reply`/`status` back, so the app user sees the reply immediately. New catalog object
  `ticket` (read, update = reply); the Support nav item appears only in workspaces with a connected source.
- **D-38 Owner enters workspaces.** The platform owner can open any `/w/{code}` with full access; each context load is
  audited (`workspace.entered`), and the UI shows an owner banner.
- **D-39 Owner lists span workspaces.** `/platform/crm/{object}` defaults to every workspace (`?workspace=all|platform|<code>`),
  returns per-workspace counts, and tags each row with its workspace; customer-workspace rows open in `/crm/w/{code}` with that
  workspace's layout and fields. Dashboard lead/account totals count all workspaces. The owner's "Support" opens the connected
  app's tickets.
- **D-40 Email.** SMTP via `CRM_SMTP_*` (Gmail app password works; spaces are stripped). Emails are multipart text + HTML
  from one table-based, inline-styled template; animation and dark mode are progressive enhancements (Apple Mail/iOS play
  them; Gmail/Outlook show the static version). Links always use `CRM_BASE_URL` (live default
  `https://card-flow-kappa.vercel.app`). Owner can check the setup at `GET /platform/email` and `POST /platform/email/test`.
- **D-41 Email sign-in codes + magic link (AUTH-02).** `POST /auth/otp/request` emails a 6-digit code (10 min, 5 attempts,
  single use, stored hashed in `crm.otp_challenges`) with a one-click link to `/crm/login?method=code&email=…&code=…`;
  `POST /auth/otp/verify` then runs the same post-first-factor checks as a password sign-in (`finishSignIn`: status,
  workspace access, MFA, device alert, audit). The dev code is returned only in local/dev when email goes to the log.
- **D-42 Explicit "no account found" (owner decision).** Code requests and password resets now say plainly when no active
  account uses the address (422 `account_not_found` on `identifier`; 403 `account_suspended`), replacing the generic
  "if an account exists" reply. This trades account-enumeration resistance for clarity; the per-IP and per-address rate
  limits stay. The UI shows the error under the field with a shake and a toast, a success toast naming the address, a
  10-minute expiry countdown and a 2-minute resend timer.
- **D-43 Email over HTTPS (Brevo).** Render's free plan blocks outbound SMTP (25/465/587), so Gmail SMTP times out
  there. `CRM_BREVO_API_KEY` switches the mailer to Brevo's HTTPS API (sender = `CRM_SMTP_FROM`, verified in Brevo);
  otherwise SMTP, otherwise the console. Startup logs an error when the SMTP server can't be reached, and
  `GET /platform/email` reports `reachable`. Sign-in codes and password resets are sent before responding, so a
  failed send returns 502 `email_failed` instead of a false "sent".
- **D-44 The connected app's data, edited in place.** Business Card Snap users, business listings and saved cards are
  read from and written to the app's own tables (no copy): `/w/{code}/app/users|businesses|categories`. Catalog objects
  `app_user` (read, update = edit profile / grant or revoke premium / app role / status, delete) and `app_business`
  (read, update = listing, badge, visibility, delete), in modules `subscriptions` and `directory`, shown only in the
  connected workspace. Grant access writes the same columns as the app's billing (`is_subscribed`,
  `subscription_plan_id`, `subscription_expires_at`; plans 3m/6m/12m/lifetime), so the app unlocks premium at once.
  Deletes are soft, like the app's admin console (admins can't be deleted). Account and contact pages list the person's
  app profile, businesses and saved cards. The app's own admin "Listings Management" is sample data held in memory —
  the CRM shows the real listings. Startup adds the modules to the product and the objects to the workspace's
  customized Super Admin role once (`cardflow:app-objects-v1`); Staff get read only.
- **D-45 Objects as data; the workspace owner has everything.** Beyond leads/accounts/contacts, every object is a
  definition in `crm.object_definitions` (standard ones seeded behind modules: opportunities, tasks, calendar events,
  notes, communications, subscriptions, catalog, support cases; plus the owner's custom objects). Records live in
  `crm.object_records`; each object gets a view `crm.obj_<key>`, so the record engine (list/detail/create/edit, page
  layouts, workspace custom fields, lookups, related lists, REST API `/w/{code}/crm/{key}`) serves them unchanged.
  Definition fields are stored in `custom` jsonb; lookups create related lists on the target record. Owner API:
  `/platform/objects` (create, edit fields/statuses/labels/icon, archive, restore); custom objects are modules of
  their own in Setup → Modules & data. Modules without objects yet (files, forms, submissions, workflows, reports) are
  shown as planned. Super Admin always has every object and capability (users, roles, customize) — its permissions
  can't be edited, and it covers every product of the workspace, including later ones. The owner inside a workspace
  sees that workspace's modules. Publishing a product version upgrades every workspace on it (opt out with
  `upgradeWorkspaces: false`). A product can be created from a workspace and is assigned to it on first publish.
- **Known gap:** the app doesn't record logouts (logout is client-side only), so only sign-ins are logged.

## Seed

- Local/dev: platform workspace `platform`, system roles, owner `ajay@gmail.com` / `Ajay1234`.
  Idempotent. Refused (D-06) when `CRM_APP_ENV=production`.
