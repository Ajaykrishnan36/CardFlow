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
- **D-46 Field access; product API view; owner console stays generic.** Roles and permission sets carry
  `fields: {object: {field: "read"|"hidden"}}`; grants add up, so a field is restricted only when every grant of the
  object restricts it (most open wins). Enforced in the record engine for pages and the REST API alike: hidden fields
  are left out of meta, lists, records, lookups and search; read-only ones reject writes. Required fields and the record
  ID can't be restricted. Delegated admins can't open a field beyond their own access (Exceeds). Owner and Super Admin
  are never restricted; the page-layout editor always sees every field (`?purpose=layout`). Editors load the fields
  from `/platform/workspaces/{id}/fields` and `/w/{code}/access/fields`. Each product has an API tab listing the
  endpoints and fields of every object its modules switch on. Connected-app pages (app users, businesses, support)
  are no longer in the owner sidebar; they live in that app's workspace, with tiles on its dashboard.
- **D-48 Roles are a hierarchy; permission sets grant.** Salesforce model: roles have a parent
  (`roles.parent_role_id`) and only widen record sharing — an "own records" scope covers the member and everyone in
  roles below theirs (`access.IdentitiesBelow`, applied to lists, records, lookups, related lists, dashboards,
  updates, deletes and lead conversion). Roles grant no permissions, except Super Admin (top of the tree, always
  full). Every workspace gets default permission sets "Admin access" and "Staff access" (`system_key`); inviting an
  Admin/Staff without sets gives them the default one. Start-up migration (once, `roles-to-permission-sets-v1`)
  moved each role's former permissions into a permission set assigned to the same members and pending invitations,
  and placed roles in the tree (Super Admin → Admin → Staff → End user; custom roles under Super Admin or Admin).
  Roles can't loop; deleting one moves its children up.
- **D-49 Each app is a project.** "Customer workspaces" are presented as Projects; the owner sidebar drops Products
  and Plans & Billing. A project created without picking a setup gets its own (a product used only by it, published
  v1 with the standard modules); its Setup tab opens that configuration (breadcrumbs lead back to the project).
  Products still exist underneath, so a setup can be shared by several projects. Team and customer logins stay.
- **D-50 Reports & dashboards.** Per project: `crm.reports` (object, filters, group-by with date buckets, measure
  count/sum/avg/min/max, chart bar/line/donut/number/table) and `crm.dashboards` (widgets = report + chart + size).
  A report always runs as the viewer — object permission, field access (D-46) and the role hierarchy (D-48) decide
  what it counts; a report or dashboard can be changed by its creator, the owner or anyone with access.manage.
  Charts are drawn in SVG (no chart library in the shared app bundle).
- **D-51 One business per GSTIN; card businesses are leads.** The first scan (or manual entry) of a card whose
  GSTIN isn't on the app creates an unclaimed business (`businesses.owner_user_id` NULL, `source = 'card'`, draft +
  unlisted, never in search). Later saves link to it: they only fill empty fields and missing card images; name,
  contact phone and GSTIN never change. Each saver keeps their own vault row (`saved_cards.linked_business_id`).
  The connector turns each card business into a Lead (`source card_scan`). When the person whose phone is on the
  card verifies it by OTP, the app assigns them the business (My Business, name pre-filled at onboarding) and the
  connector converts the lead into their account + contact (`lead_conversions.trigger = 'app_claim'`). An owner
  registering the same GSTIN also claims it. The Businesses page shows lead status, saved-by (with who) and card images.
- **D-52 Contact first; accounts are businesses (Salesforce model).** An app sign-up is a lead converted into a
  **contact only** (app profile fields live on the contact). An **account** (kind business) is created when that person
  has a business — registered in the app or claimed from a scanned card — and their contact is linked to it (first
  business = the contact's account). A card lead converts into that business account + the owner's contact. Records
  from before D-52 are kept: personal ("individual") accounts stay, contacts move to their business account.
  Provisioning step 2 is "Product setup": create a new product (opens its 6-step setup right after), use an existing
  one, or skip (standard setup). (Superseded by D-53.)
- **D-53 Products and setups.** In the UI a project (workspace) is now a **Product** — each app you run (e.g.
  Business Card Snap) — and what was called a product is its **Setup** (modules, roles, pipeline, login; shown under
  "Product setup"). "Add new product" is Details → Super Admin → Review and creates the product with no setup
  (`withoutSetup`); the setup is created from its Product setup tab ("New setup" / "Add existing setup") and linked when
  published. Code and API names (workspaces, products) are unchanged.
- **D-54 Filters and saved views.** One filter engine (`records/filter.go`) compiles AND/OR trees (depth ≤ 5, ≤ 50
  conditions) into SQL with bind parameters only; operators depend on the field type (text, number, date with
  relative ranges in the product's time zone, pick-lists "is / is not / is any of", "is me / my team"). Lists, boards,
  calendars, exports, bulk actions, reports, workflows and campaigns all use it. Saved views (table / board /
  calendar, personal or shared) remember filters, sorts, columns and layout; standard shared views are seeded once
  per product and object (e.g. Accounts "Business accounts" / "Personal accounts (older)", nothing deleted).
- **D-55 One write path and an event outbox.** Every record change goes through the service (validation, unique
  checks, history, audit) and writes an event to `crm.outbox_events` in the same transaction. An in-process worker is
  kicked on each change and otherwise sleeps until the next due item — it never polls, so the free Neon database can
  sleep. Events feed live pages (SSE), webhooks, workflows and notifications.
- **D-56 Bulk actions, recycle bin, merge.** Bulk update / delete / restore / permanent delete (≤ 10,000, one
  savepoint per record). Deleting moves records to the recycle bin (who and when); permanent delete clears
  references. Merge keeps one record, re-points links, then moves the duplicates to the recycle bin.
- **D-57 Richer field types.** Rating, address, full name, emails, phones, links, JSON, lookup (one record),
  relations (many records) and files, plus "unique values only" for text, email, phone, URL and number fields.
- **D-58 CSV import and export.** Import maps columns, checks the file first (dry run), then creates, upserts or
  updates, reading labels as people type them. Export writes what the list shows, with readable values.
- **D-59 Timeline and files.** A record's Activity tab merges field history, notes (with @mentions), tasks, events,
  emails and files. Files: 10 MB each, 250 MB per product, served with a sandboxing CSP.
- **D-60 Live updates and notifications.** One SSE stream per page refreshes what changed; mentions, assignments
  and workflow messages become notifications (bell). Favorites pin records and views in the sidebar.
- **D-61 API keys and the public API.** Keys (`crm_<prefix>_<secret>`, stored hashed) belong to one product and act
  with full access or one permission set, 100 requests a minute, only while the product setup has API access on.
  REST is the same API the web app uses; GraphQL and an OpenAPI file are generated from the product's objects.
- **D-62 Webhooks.** Signed (`X-CRM-Signature: sha256=` HMAC of `timestamp.body`), retried for 24 hours, only while
  the product setup has webhooks on; outbound calls refuse private addresses outside local development.
- **D-63 Workflows.** Trigger (record created / changed / deleted, run by hand, schedule, incoming webhook) and
  steps (create / update / upsert / delete / find records, assign by round robin / fewest records / random, email,
  notification, HTTP call, 1-second JavaScript sandbox, wait, if / otherwise, for each, stop). Drafts are published as
  versions; every run keeps a step log. A workflow never triggers itself and chains stop at depth 5. On Render's free
  plan the server sleeps when idle, so a schedule that falls due then runs as soon as it wakes.
- **D-64 Sign-in methods per product.** The product setup lists allowed methods: password, email code, Google,
  Microsoft, LinkedIn, SAML single sign-on. They are enforced at sign-in and when a session opens a product.
  Setups saved before enforcement are read as "password + email code" (the `enforced` marker), and sessions created
  before the method was recorded stay valid — nobody live is locked out. OAuth credentials come from
  `CRM_GOOGLE_*`, `CRM_MICROSOFT_*`, `CRM_LINKEDIN_*` (redirect `<CRM_BASE_URL>/api/crm/v1/oauth/<provider>/callback`).
- **D-65 Email & calendar.** People connect Gmail / Outlook (OAuth) or IMAP. Hourly or on-demand sync stores only
  emails with people already in the CRM (optionally creating contacts), with per-mailbox visibility and a
  blocklist; meetings become Events and events created here go to the calendar.
- **D-66 Email campaigns.** One email to a filtered group of contacts, leads or accounts, merge fields, test send,
  send now or later, unsubscribe link and header, at most 1,000 emails a day per product.
- **D-67 Teams.** Groups of members used by "is me or my team" filters and by workflow assignment.
- **D-68 Sales process drives the pick-lists.** The setup's lead statuses and opportunity stages (with win
  probability) are the Status / Stage options in the product; lead conversion follows the setup (contact only or with
  an account, optional opportunity, "only qualified leads").
- **D-69 Self sign-up.** When the setup allows it, `/crm/signup?product=<code>` creates an account after an email
  code and adds the person as an End user. Otherwise joining is by invitation only.
- **D-70 User types.** Inviting someone can name a user type from the setup; it limits the roles they can get and
  is stored on the membership (`memberships.user_type`).
- **D-71 Names (Salesforce style).** Owner console: *Products* (one per customer), each with a *Product setup*
  whose steps are Details & branding → Objects → Roles & user types → Sales process → Sign-in & integrations →
  Review & publish. The owner sidebar groups Products, Objects and Users & access under *Administration*; the
  dashboard's Products card counts the same rows as the Products page, and the Products list shows each product's
  setup by name and version (no separate setup count). Each product's accent colour is applied inside its workspace. Not built on purpose: extra UI
  languages / right-to-left, AI chat, AI agents, MCP server.
- **D-72 App tickets are Cases.** A connected app's support tickets (Business Card Snap) are mirrored into the
  standard Cases object (custom fields *App ticket ID*, *App category*, *Reply in app*; origin "App"), so every
  product uses the same Cases list, board, record page, reports and workflows. Changing a case's status or its
  "Reply in app" updates the ticket, so the person sees it in the app. The old Support page links now open the case.
  Business Card Snap also gets the standard objects once (opportunities, tasks, calendar, notes, communications,
  catalog, files); the owner can switch any off. The sidebar orders objects the same way in every product.
- **D-73 Apps inside a product (Salesforce App Launcher).** A product can have several apps (setups). The sidebar
  has an app switcher, "All apps" by default (the whole menu); picking one app narrows the menu, the dashboard cards and the accent colour, and the records
  are shared across the product's apps (nothing is split or moved). The choice is kept per product in the browser;
  the server falls back to the first app. Members see only the apps they are given. Owner console: *Administration →
  Apps* lists every app installed in every product, the dashboard's Apps card counts them, and the product page's
  tab is "Apps" (New app / Add existing app / Edit app setup).
- **D-74 Owner console filter.** The owner sidebar has two filters: Product (default "All products"), then App
  (default "All apps", listing only the selected product's apps). The Overview cards, Products, Apps and the
  Leads / Accounts / Contacts lists follow it; the setup checklist always covers the whole platform. Kept in the browser.
- **D-75 Rich text.** A `richtext` field type (TipTap editor: headings, bold/italic/strike, lists, checklists, quotes,
  code, links, images, @mentions). Notes, task comments and the descriptions of opportunities, events and cases use
  it (stored definitions are upgraded from long text on start; old plain values still show as they were). HTML is
  sanitized on the server (bluemonday, safe tags only) and again in the browser (DOMPurify). Images in notes are
  uploaded to the record's Files. Mentions notify the person with a plain-text preview.
- **D-76 Currency per amount; phone country codes.** A currency field keeps its number (so totals, filters and
  reports still work) and stores the amount's currency next to it (`<key>__currency` in the record, returned in
  `values`). The API also accepts `{"amount": 1200, "currency": "USD"}`. No code = the product's currency
  (workspaces.currency, INR by default); list totals and board sums use the product's currency. Phone fields get a
  country calling-code picker and are stored as "+91 98765 43210".
- **D-77 Grouped tables.** A table view can group rows by a pick-list, yes/no, lookup or rating field ("Group rows"),
  saved with the view like other settings. Each group shows its count and up to 50 rows, can be collapsed, and
  "Show all" narrows the list to that value.
- **D-78 Run workflow on a record.** Record pages have a "Run workflow" menu with the active manual workflows for that
  object (only those "everyone can run" for people who don't manage workflows). A workflow with a form asks for its
  answers first; the list's bulk run uses the same flow.
- **D-79 A product's own objects.** People with "Customize page layouts & fields" create objects in their product
  (Settings → Objects & fields), with the same builder the owner uses. Such an object belongs to that product
  (`object_definitions.workspace_id`): only that product's menu, permissions editor and API see it, it's on in every
  one of its apps, and it isn't offered as a module for other setups. Platform-wide objects stay the owner's. The
  owner console lists every object, marking the ones that belong to a product.
- **D-80 Email conversations with Reply.** A record's Emails tab groups its linked emails into conversations: messages
  join when they share a provider thread, when one answers another (In-Reply-To ↔ Message-ID), or when their subjects
  match once "Re:"/"Fwd:" are stripped. Reply (and Reply all) answers the latest incoming message: the email keeps the
  thread, gets "Re: subject", carries In-Reply-To/References, goes into the same Gmail thread or uses Outlook's reply
  when sent from the mailbox that received it. Mailbox sharing rules still apply (subject-only / metadata-only).
  Compose and reply use the rich-text editor; the HTML is sanitized on the server. Additive migration 0013.
- **D-81 Keyboard shortcuts.** "g" then a letter goes to a page (d Dashboard, l Leads, a Accounts, c Contacts,
  o Opportunities, t Tasks, e Calendar, n Notes, s Cases, r Reports, b Dashboards, w Workflows, m Campaigns, p Profile)
  when that page is in the person's menu; "/" opens search, "?" lists the shortcuts. On a record page j / k step to the
  next / previous record of the list the person came from (the list remembers its visible rows for the browser tab),
  with "3 of 25" and arrows beside the breadcrumbs; e starts editing. Shortcuts never fire while typing or in a dialog.
- **D-82 OpenID Connect single sign-on.** A product's SSO is SAML 2.0 or OpenID Connect (one provider per product).
  For OIDC the admin enters the issuer URL, client ID and secret (secret encrypted, never shown again); the CRM reads
  the issuer's discovery document, signs people in with the authorization-code flow plus PKCE, state and nonce (kept in
  a 10-minute encrypted cookie) and checks the ID token's signature against the provider's keys, issuer, audience,
  expiry and nonce; an unverified email is refused. Matching, just-in-time accounts and the role for new people work
  as for SAML. /auth/sso/<code>/start picks the protocol (the old SAML start link keeps working). Sessions now record
  the real sign-in method (it was always stored as "password", so SSO-only products would have refused them).
  Additive migration 0014.
- **D-83 Invite link for company domains.** In Users & access, people who manage access create one shareable link per
  product: the company email domains that may use it (personal services like gmail.com are refused) and the role
  joiners get (never Super admin). Opening /crm/join/<token> asks for a name and work email; an address off those
  domains is refused, and the email is proved with a one-time code before anything is created. A new person gets an
  account; someone who already has an account joins the product without their password changing; a removed or
  suspended membership isn't brought back. The link can be switched off or replaced (the old one stops at once), and
  counts its uses. The token is stored hashed for look-up and encrypted to show it again. Additive migration 0015.
- **D-84 Recycle bin retention (opt-in).** By default deleted records stay in the recycle bin until someone restores
  or deletes them, as before. People who manage access can choose, from the bin, to delete bin items for good 30, 60,
  90, 180 or 365 days after they were deleted (with a warning that it can't be undone). The worker checks at most
  hourly, removes up to 500 records a run through the same path as "Delete permanently" (links cleared, audit entry by
  the system), and skips records still in use. Nothing changes for products that don't opt in. Additive migration 0016.
- **D-85 Setup wizard: statuses/stages out, lead conversion into Objects.** The product setup wizard drops the
  separate "Sales process" step (now five steps: Details & branding → Objects → Roles & user types → Sign-in &
  integrations → Review & publish). Lead statuses and opportunity stages are no longer edited there; every product
  keeps the standard defaults (backend normalize() fills leadStatuses and pipelineStages whenever a config leaves them
  empty, so publish never blocks), and they are edited per object afterwards under Objects → Leads/Opportunities →
  Statuses, the way Twenty edits the Stage field's options. The "Lead conversion" toggles (create contact / create
  opportunity / only qualified) moved onto the Objects step, shown only when the Leads module is on, and appear under
  Objects in the review summary. Old `?step=pipeline` links fall back to the first step. Frontend only, plus the
  normalize() default-fill; no migration.
- **D-86 Owner filter moves from the sidebar into the page.** The owner console's Product and App dropdowns left the
  sidebar (it showed them alongside the Leads page's own product dropdown, which confused people). They now sit in the
  page header: Overview, Products and Apps show Product then App; the App dropdown stays disabled on "All products" and
  lists only the chosen product's apps. Leads, Accounts and Contacts show Product only (records belong to a product,
  not an app) and keep their "Platform CRM" option and per-product counts. The choice is shared: picking a product on
  one page carries to the others. The workspace app (Super Admin, Admin, Staff) is unchanged.
- **D-87 Overview cards open the same filtered list.** Every Overview card now opens a page that shows exactly the
  rows it counted, for the chosen product and app. The Users page follows the Product filter (people with a membership
  in the chosen products) and gains an "Invited" tab (people with a pending, unexpired invitation); the Invitations
  card opens that tab. The Users card counts everyone the Users page lists, the owner included. Checked for all seven
  cards with no filter, one product, a product plus app, and three different products.
- **D-88 One way back to all products.** On Leads, Accounts and Contacts the owner's Product picker moves out of the
  table toolbar to sit above the list, the same place and size as on Overview, Products and Apps. Whenever a product
  (or Platform CRM) is chosen, a "Show all products" button sits next to the picker on every owner page, so a filter
  carried over from another page can be cleared in one click.
- **D-89 App picker on the record lists too.** Leads, Accounts and Contacts now show Product then App, like Overview,
  Products and Apps (this revises D-86's "Product only"). The App list opens once a product is chosen and lists its
  apps. Records belong to the product, not the app, so an app narrows the list the way the Overview counts it: the
  product's records while that app is active in it, none otherwise (`?app=` on the owner list, export and bulk).
- **D-90 Businesses show on the account, not the contact.** A Business Card Snap person's contact shows their app
  profile and saved cards only. Each business they register or claim is its own business account (D-52), and that
  account shows its business listing and the owner's app profile; when the owner's contact sits under another of their
  businesses (a contact keeps one primary account), the account also shows a "Business owner" link to that contact.
  An older personal ("individual") account stands for the person: it shows all their businesses, saved cards and a
  "Contact" link. Its link row moved to the business account when D-52 ran, so the panels find the person through the
  account's stored App user ID (`custom.app_user_id`); contacts use the same fallback.
- **D-91 Support tickets are conversations.** App migration 015 adds `public.support_ticket_messages` (sender user or
  support, author name and role, body, time); the ticket's own message stays the first message, and a reply given
  before this becomes the first support message. In the app the ticket screen is a chat: the person's messages on the
  right as "You", support replies on the left with the replier's name and role, times and avatars, and a box to write
  again (a resolved ticket reopens). The case page shows the same conversation above the details, refreshed every 15
  seconds, with a reply box for anyone who may edit the case; each reply records the sender's name and their role in the
  product (Super Admin, Admin…, or Platform owner). The old "Reply in app" field still works and joins the conversation
  the same way. Opening the conversation asks the connector to sync, so the case follows new app messages.
- **D-92 The app has no admin console.** The in-app admin flow is removed from CardFlow (owner's instruction, 1 Oct
  2026): the seven admin screens, the admin top bar and tabs, the app's admin API calls, the server's `/api/v1/admin/*`
  routes, the `internal/admin` package and the admin-only middleware. The CRM is where the app is managed (app users,
  premium access, businesses, tickets as Cases). An account whose app role is still "admin" simply gets the normal
  user flow. No table or data is dropped; `users.role` keeps its values.
- **Known gap:** the app doesn't record logouts (logout is client-side only), so only sign-ins are logged.

## Unified CRM platform (from 5 Oct 2026)

The app and the CRM become one product: the CRM is the core, business-card scanning is a feature of it
(`CRM_TRANSFORMATION_PLAN.md`). Open decisions in that plan were settled with its recommendations; each is recorded here.

- **D-93 One identity; phone sign-in; sessions for the native app.** `crm.identities` is the only login record.
  Customers sign in with a mobile number and a 6-digit SMS code (`POST /auth/phone/request`, `/auth/phone/verify`):
  hashed in `crm.otp_challenges` (channel `sms`), 10 minutes, 5 attempts, single use; 3 requests per number per 10
  minutes, 10 per day, 20 per IP per hour. The first correct code for a number creates the identity, so sign-up and
  sign-in are one flow. The code is never logged and never returned, except by the *preview* sender, which exists only
  when asked for explicitly: `SMS_PROVIDER=preview` or `DEV_MOCK_SMS=true` outside production, or
  `OTP_PREVIEW_INSECURE=true` anywhere. Providers sit behind `crm/sms.Sender` (MSG91 and Twilio built in); with none
  configured, phone sign-in answers 503 `sms_unavailable` instead of pretending.
  A native app asks for a bearer session (`X-Session-Transport: bearer`): the same `crm.sessions` row as a cookie
  session, sent as `Authorization: Bearer crms_…`, revocable, 30 days idle / 90 days absolute, renewable
  (`POST /auth/session/renew` rotates the token). Bearer requests skip the CSRF check (no cookie is involved).
  A phone session isn't forced into authenticator-app MFA or the 30-minute privileged idle limit (the code proves
  possession); anyone who enrolled MFA is still challenged; the platform owner can't sign in by phone.
  The app's profile row is linked, not moved: `public.users.identity_id`. Existing app users get an identity at
  start-up (idempotent, `source = 'app_backfill'`; the phone counts as proved only if they had signed in before) and
  at sign-in. The app's old endpoints (`/api/v1/auth/otp/*`) now call the same service, so there is one code store and
  one set of limits; they still return the old JWT for app builds in use, plus the unified `session_token`. The app's
  API accepts either token. The old JWT refuses the built-in or a short key in production. Phone sign-in is a method
  in each app's setup (`loginMethods.phone`), on wherever a password or an email code is allowed.

- **D-94 Customers create their own business.** `POST /businesses` (any signed-in person) creates a workspace on the
  platform's one **Standard CRM** setup (`crm.products` key `standard_crm`, ensured at start-up) and makes the creator
  its active Super Admin in the same transaction; no owner involvement, no invitation. The person only types a name:
  the workspace code is derived from it. Limits are platform settings the owner can change
  (`/platform/settings/self-serve`: on/off, businesses per person, default 5), checked under a per-person lock.
  `GET /businesses` lists a person's businesses with their role. A person with no business may sign in (they are sent
  to create one); with self-serve off, joining is by invitation only as before. Workspaces record
  `created_by_identity`, `origin` (`owner` / `self_serve`) and a `profile` (industry, phone, address…); a directory
  listing can be linked as a business's public profile (`public.businesses.workspace_id`). Product and App stay as
  internal configuration; customers never see those words. Migration 0018.
- **D-95 One place builds the server; end-to-end tests.** Route wiring moved from `cmd/api/main.go` to
  `internal/server`, so `internal/e2e` can run the real server against a throwaway database (created and dropped per
  run) and drive it over HTTP. CORS allows only `ALLOWED_ORIGINS` and the native shells in production (any origin
  elsewhere); credentials stay off because the app uses bearer tokens.

- **D-96 Finance on the record engine; dashboard summary.** Income and Expenses are standard objects (module
  `finance`): amount, date, category, payment method, account / contact / opportunity, reference, repeats, notes. They
  get lists, filters, views, import/export, reports and permissions like any object; no separate finance tables.
  The default Staff permission set doesn't include them (Admin and Super Admin do). `GET /w/{code}/dashboard/summary`
  (`range=today|week|month|year|all|custom`, in the business's time zone) returns, for what the caller may read and
  within their record scope: active / new / converted / lost leads, contacts, accounts, open opportunities, pipeline
  and weighted pipeline value, won and lost deals, win rate, tasks due today / overdue / upcoming, upcoming meetings,
  open cases, income, expenses and net income (cancelled entries excluded; totals in the business currency, no
  conversion), plus short lists (follow-ups, recent leads, tasks, meetings, opportunities, contacts). Computed in SQL.
- **D-97 Commerce and knowledge objects.** Standard objects added: price books, quotes, sales orders, invoices,
  purchase orders, line items (one object used by quotes, orders, invoices, opportunities and price books) and
  knowledge articles (`solutions`). Modules `sales_docs`, `knowledge`, `finance`, `cards`. Existing standard objects
  gain fields once (marker `objects:standard-fields-v2`): opportunity pipeline, forecast category, lost reason, price
  book; task repeats and reminder; event attendees, reminder, type; case category, respond-by, closed-on, article;
  communication duration; catalog cost. Calls are Communications with channel Call; meetings are Calendar events.

### D-98 — Business cards are part of the CRM

A scanned card is saved by a member inside one business. `POST /w/{code}/cards/match` looks for the
person among **that business's** leads, contacts and accounts only (last 10 digits of a phone number,
lower-cased email, exact company name for accounts), respects read permission and "own records"
scope, and reports records the caller may not open only as a count (`hidden`). `POST /w/{code}/cards`
saves in one transaction: the card (or an existing `cardId` from the caller's vault), a new lead or
contact (optionally with its account, reusing an account of the same name), or an attachment to an
existing record that only fills empty fields; plus the `crm.card_links` row, a "Business card
scanned" timeline entry, an optional note and follow-up (a lead's next follow-up, otherwise a task).
Creating a lead or contact for a phone/email already in the business answers `409 possible_duplicate`
with the matches unless `allowDuplicate` is sent. `Idempotency-Key` is honoured.

The vault stays in `public.saved_cards` (no second card store); it gains `identity_id` and
`workspace_id`, and `user_id` becomes optional so CRM-only people can keep cards. There is no
separate permission object for cards: a card is visible to the person who saved it and to anyone who
can read a record it is linked to; creating/attaching needs the lead/contact/account permission.
Lead, contact and account pages show a "Business cards" related list.

### D-99 — Web CRM: phone sign-in, "Your businesses", business profile, summary, cards

`/crm/login` opens on a **Mobile** tab (the same number as the app; a new number creates the account
and lands on "Create a business"). `/crm/businesses` lists every business the person belongs to and
creates new ones; the sidebar's business menu always links to it. `GET/PATCH /w/{code}/business` is
the business profile (name, currency, time zone, contact and tax details; editing needs
`access.manage` or `members.manage`). The dashboard shows the D-96 summary with a date range. The
sidebar groups objects by module: CRM, Service, Sales, Finance. `/crm/w/{code}/cards` lists, adds and
converts business cards (D-98). Fast2SMS joins MSG91 and Twilio as an SMS provider
(`SMS_PROVIDER=fast2sms`, `SMS_AUTH_KEY`).

### D-100 — The mobile app is the CRM on a phone

Tabs: **Home · My CRM · Scan · My Cards · Browse**. The app keeps one session token (the unified
`crms_` bearer session, D-93) for both APIs; a token is never invented on the device, sign-out
revokes the session on the server, and a session from before the change is asked to sign in again
before the CRM opens. `CrmContext` holds the person's businesses and the open one (remembered per
device); switching it resets every CRM screen. Home is `/dashboard/summary` with a date range, quick
add and what's due. My CRM lists the modules the member's role allows (the same navigation the web
sidebar uses) with one search across leads, contacts, accounts, deals, tasks and cases. List, record
and form are **one generic implementation driven by `/crm/meta/{object}`** — fields, types, options,
lookups, required and statuses come from the server, so a custom field or object added on the web
appears in the app without a release. Lead conversion, status change, notes, call log, tasks and
meetings for a record are on the record screen. After a scan the app offers "Save to CRM"
(D-98), and a saved card shows what it is linked to. Record creation honours `Idempotency-Key`.
`DEV_MOCK_SMS=true` on a non-production server always previews codes (never calls a provider).

### D-101 — Plans belong to a business; limits are enforced on the server

`crm.plans` (Free · Pro · Business, editable by the owner), `crm.workspace_subscriptions` (one row
per business: plan, status, period end, source), `crm.subscription_events` and
`crm.usage_counters`. A business's plan is resolved in this order: its own live subscription →
a business the owner provisioned (no limits) → the creator holds the app's premium subscription
(Pro for the businesses they created — the app-store subscription stays per account, so nothing
changes in the stores) → the default plan. A lapsed subscription falls back to the default plan and
reports `expired`.

Limits (`members`, `records`, `cardScansPerMonth`; 0 = unlimited) are checked where the thing is
created — `createRecord`, every path that adds a membership (`platform.SeatCheck`), and a new card
scan — and answer `402 plan_limit` with the limit in `details`. Work the system does on its own
(workflows, the app connector) is never blocked. `GET /w/{code}/plan` shows any member the plan,
real usage and the plans on offer; only the platform owner changes plans
(`PUT /platform/plans/{key}`) or a business's subscription (`PUT /platform/subscriptions/{id}`),
both audited.

### D-102 — Add a teammate by mobile number

`POST /w/{code}/admin/members` with a phone and no email adds the person at once: the identity is
found or created by that number, the membership is active, and they get the role's default
permission set (roles are hierarchy positions, D-48). They sign in with a code sent to the number —
no email, password or invitation link. The number counts as verified only after that first sign-in.

### D-103 — Email and password on a phone account

`POST /me/email/request` sends a 6-digit code to an address the signed-in person wants on their
account; `POST /me/email/verify` puts it there once the code is typed back (an address counts only
when verified; it replaces an older address of the same account; one address belongs to one account).
`POST /me/password` sets a password — directly when the account never had one, with the current
password otherwise — and signs other devices out. `/me` reports `emailVerified`, `phoneVerified` and
`hasPassword`. The same account then signs in by mobile code, email code, or email + password.

### D-104 — One sign-in for the whole site

In a browser the session is the CRM's HttpOnly cookie, and the app API (`/api/v1`) accepts it too:
no token is handed to JavaScript. A request that changes data must carry the CSRF token, exactly as
CRM requests do. Native apps keep using the bearer session. The front end has one login
(`/` and `/crm/login` are the same screen: Mobile · Password · Email code); only the owner console has
its own (`/crm/owner/login`).

### D-105 — A business has one public listing

Creating a business also creates its directory listing (`public.businesses`, linked by
`workspace_id`) with its digital card and QR, so it appears in Browse and in the app's "My business"
screens. The directory never shows sample businesses: with no listings, search returns an empty list.
Search works with and without PostGIS (latitude/longitude columns and a great-circle distance).

### D-106 — Fresh start

`CRM_FRESH_START=erase-everything-<label>` on the host erases every customer's data in one
transaction and creates the baseline (see `internal/crm/freshstart.go`). Each value runs once and is
recorded; no API can trigger it. Kept: the platform owner's login, the platform and app-connector
workspaces with their setup, setups, plans, settings, categories. The baseline is one person
(+91 98765 43211) as Super Admin of "Ajay tech" and "Ajay finace", each with its own sample records
and public listing. After a fresh start demo data is never seeded again. Where `CRM_OWNER_EMAIL` is
set and the single owner has another address, the owner's address becomes that one.

### D-107 — One app shell, two layouts, one set of URLs

`src/entry.js` boots one app for every URL. Signed out, `/` and `/crm/login` are the same sign-in.
Signed in, `Shell` (`src/crm/app.tsx`) shows the phone layout (React Native Web, `src/MobileRoot.js`,
mounted in a portal on `<body>` so neither layout's CSS reaches the other) when the screen is under
768px or this is the native app **and** the phone layout has a screen for the address
(`src/navigation/routes.js`); otherwise the desktop CRM, which is responsive. The address bar is the
phone layout's navigation state: `/crm/w/{code}/home`, `/menu`, `/{object}`, `/{object}/{id}`,
`?new=1`, `?edit=1`, `/cards`, `?card=`, `?scan=1`, `/listing`, `/crm/browse`, `/crm/me`. The desktop
routes the same addresses (phone-only ones redirect to the nearest desktop page).

The phone layout keeps no session of its own: in a browser both layouts use the HttpOnly cookie
(D-104), in the native shell both use the bearer token kept by `crm/api/client.ts`. Its old sign-in,
splash and onboarding screens, the mock-driven owner screens and the sample directory data are
removed. An account without an app profile (signed up by email) gets the desktop layout on a phone.

### D-108 — Fresh start keeps one account (replaces the sample baseline of D-106)

A fresh start no longer seeds sample businesses. It keeps the platform owner, one person by mobile
number (default +91 98765 43211) and that person's business by name (default "Ajay traders") with
its records, setup and listing; everything else is erased, including that person's other businesses.
Records in the kept business that named an erased teammate pass to the business's owner. The kept
person and owner stay signed in. Whatever of the kept account is missing is created, empty. The
app's start-up seed is reference data only (directory categories).

### D-109 — First-time guidance

`GET /w/{code}/getting-started` lists the first steps for a business (first lead, scan a card, plan
a follow-up, complete the business profile, add a teammate, add your email) with `done` worked out
from the business's real data and the caller's permissions. The phone Home and the desktop dashboard
show it as a card until everything is done or it is hidden (per business, per device). The phone
layout also shows a four-step tour once per person per device. Browse lists other people's
businesses only; a person's own are under Profile → My Businesses (open the CRM, the business card
and listing, or add a business).

### D-110 — "New lead" on a directory listing

Someone else's listing (Browse) has a **New lead** action: it adds that business to the open
business's CRM as a lead (company, phones, email, website, address, source `directory` — a new
lead-source option). The sheet first runs the D-98 match against the viewer's own records and shows
anything already there (open it, or create another); a follow-up date and a note are optional. The
listing's owner and their CRM are not touched. Shown only to members who may create leads, never on
your own listing.

### D-111 — Relationships between any two records

Lookup fields say "this record's account"; they can't say "this contact also works for that
account", "she is the decision maker on this deal" or "this contract covers these assets".
`crm.record_relationships` holds such links between any two records of one business, typed by
`crm.relationship_types` (13 built in with `workspace_id` null; a business may add its own with
the metadata capability). A type can fix the object at each end and a cardinality
(one-to-one … many-to-many), enforced when a link is made. Both ends must be in the business of
the URL and readable by the caller: a record of another business is 404 even for someone who is
a member of both — there is no cross-business link and no shared person registry. A link shows
on both records with the inverse wording, as related lists (`rel:<type>:<dir>:<object>`) and in
the Relationships panel (add / remove). Adding and removing write the timeline and the audit
log. API: `GET/POST /w/{code}/crm/{object}/{id}/relationships`, `DELETE /w/{code}/relationships/{id}`,
`GET/POST /w/{code}/relationship-types`, `DELETE /w/{code}/relationship-types/{key}`.
Campaigns are not record objects, so lead ↔ campaign is not in this engine.

### D-112 — Forecasts are sums, not stored numbers

A forecast for a period is computed from opportunities whose `closeDate` is inside it: closed
won, commit / best case / pipeline by `forecastCategory` (empty = pipeline), omitted and lost
shown apart; forecast = closed won + commit; weighted = open amount × probability. Periods are
keys — `YYYY-MM`, `YYYY-Qn`, `FYYYYY` — on the business's financial year
(`workspaces.profile.fiscalStartMonth`, default April). One aggregate query groups by owner; team
rows add up their members, and the totals come from the deals themselves, so a person in two
teams is not counted twice. Row scope applies: "own" sees their own deals and those below them.
Stored: targets (`crm.forecast_quotas`: company, team or person per period) and submissions
(`crm.forecast_submissions`: a snapshot, the amount the person stands behind, and the manager's
approve / reject with an optional override). Targets and approval need the new capability
`forecast.manage`. API under `/w/{code}/forecast…`; page `/crm/w/{code}/forecasts`.

### D-113 — Payments are records; an invoice's paid amount is derived

New object `payments` and table `crm.payment_allocations` (payment, invoice, amount
`numeric(16,2)`, unique per pair). Many payments per invoice, many invoices per payment, the
unapplied rest stays on the payment. Only payments whose money has arrived (Paid, Partially
refunded) count, less the refunded share. Every change to a payment, an allocation or an
invoice total recomputes `amountPaid`, `balanceDue` and the invoice status in the same
transaction (`records/hooks.go` → `afterSave`); a typed-in paid amount doesn't survive. All sums
are SQL numeric. Refunds are recorded (`POST /payments/{id}/refund`), not sent to a gateway.
An invoice that carried a typed `amountPaid` from before gets one opening payment for that
amount on upgrade (marker `payments:opening-balances-v1`); nothing else is invented.
Payments are in the Finance module, so staff need Finance access to see or record them.

### D-114 — Vendors and services reuse what exists

A vendor is an account of type Vendor or Supplier (account types now also include Partner,
Reseller, Distributor, Competitor); purchase orders and expenses already point at accounts.
A service is a catalog item with `itemType` = service, plus `billingFrequency` and
`durationMinutes`. No new tables. The Vendors and Services menu entries are the same lists
through a fixed filter carried in the address (`?type=vendor`, `?itemType=service`), shared by
the desktop list and the phone list (`frontend/src/navigation/presets.js`).

### D-115 — SLA: policies, clocks, working hours

`sla_policies` records give first-response and resolution targets per priority (or any), with
optional working hours, work days and holidays. A case picks its policy: on the case →
its entitlement → an active entitlement of its contract or account → priority → default. Two
clocks per case live in `crm.sla_timers`. Leaving New answers the first response; Pending
pauses (resume shifts the due time by the working time waited); Resolved/Closed completes;
reopening restarts resolution. A new case always gets status New. The sweep
(`Handler.Sweep`, advisory lock, sleeps until the next due time, at most an hour) warns at 80%,
marks breaches once (case flag, timeline, notification) and escalates when the policy says so.
`CRM_SWEEPS=off` disables the background loop (tests call `Module.Sweep` directly).
Cases gained statuses Assigned and Pending, priority Critical, and links to asset, contract,
entitlement and product.

### D-116 — Contracts, assets, entitlements, appointments

Four standard objects in the generic engine (no new tables): `contracts` (term, value,
renewal notice; `POST /contracts/{id}/renew` creates the next term, marks the old one Renewed,
links them with `renewal_of` and copies what it covers), `assets` (serial, warranty, location,
parent asset), `entitlements` (account, contract, SLA policy, level, dates) and `appointments`
(distinct from calendar events: own statuses, service, assigned person, reminder, cancellation
reason). The sweep marks contracts Expiring inside their notice period (one renewal task for
the owner) and Expired after the end date, and unpaid invoices past due as Overdue.

### D-117 — Menu by area

The sidebar and the phone menu group objects as Sales, Products, Sales operations, Service,
Purchasing and Finance (a business's own objects stay under CRM / "More"). Forecasts is a
desktop page; on a phone its menu entry opens the same address in the desktop layout.

### D-118 — Multi-currency with the rate fixed on the record

`crm.currencies` (shared list) and `crm.exchange_rates` (per business, effective-dated; a new
rate ends the previous one, nothing is overwritten). Opportunities, quotes, orders, invoices,
payments, income, expenses and contracts carry `currency`, `exchangeRate` and the amount in
the business's base currency. The rate is looked up once, for the record's own date, in the
save hook (`records/currency.go`) and kept: later rates never touch existing records. A rate
typed on the record wins. A foreign currency with no rate is refused (422) rather than
guessed. Forecasts and attribution add up base amounts. Money arithmetic in Go is in whole
hundredths (`Cents`), never floats.

### D-119 — Price book entries

A price is a row in `crm.price_book_entries` (book, item, currency, quantity break, start
date → list price, selling price, cost, largest discount). The catalog price is only the
fallback when no entry exists and the document is in the base currency. Entries are
soft-deleted and versioned; every change is audited with old and new values. Managing prices
needs the capability `pricing.manage`.

### D-120 — CPQ on the existing catalog and line items

One engine (`records/pricing.go`) prices every document that has lines (quotes, sales orders,
invoices, credit notes, contracts, work orders, opportunities): price book → entry → price
rule → discount rule → line discount → tax, deterministic and in integers. Bundles
(`crm.product_bundle_items`) expand into component lines; `crm.pricing_rules` also holds
configuration (requires / excludes) and eligibility (customer type, territory) rules. Results
are stored on `line_items` as a snapshot; conversion quote → order → invoice copies lines
without re-pricing; lines of a document that left draft are locked (409) in both APIs.
`PUT /{object}/{id}/lines` replaces lines under a row lock on the document. Quotes can be
revised (`quoteVersion`, `previousQuoteId`). Someone without `pricing.manage` can't override
a price that the engine worked out.

### D-121 — Approvals by limit and role

`crm.approval_rules` (kind, limit, approver role) and `crm.approval_requests`. Kinds:
discount (percent off list on a quote), refund, credit note, debit note, write-off (amounts).
A record over a limit waits; a member in the approver role, a role above it in the hierarchy,
or with `approvals.manage` decides. The requester can't decide their own request unless they
hold `approvals.manage` (so a one-person business isn't stuck). An approval already given for
at least the same value isn't asked for again.

### D-122 — Contact roles are relationships with meaning

Not a new table: `crm.record_relationships` gained `role`, `is_primary`, `is_active`,
`start_date`, `end_date`, and a system type `contact_role` (contact → any record). One row per
contact per record; one primary contact per record (partial unique index). Own API
(`…/contact-roles`), hidden from the generic relationship list. Asset relationship types
(installed on, component of, replaced by, upgraded to, depends on) were added as system types.

### D-123 — Record teams give access

`crm.record_team_members` (record, person, team role, access level). The list query and the
single-record query accept a record whose team includes the viewer or someone in a role below
them; updates need `write`, deletes `full` (checked in `updateValues` / `deleteRecord`, so
every caller is covered). Managing a team: the owner or someone above them, a `full` member,
or the capability `record_teams.manage`. Reports, global search and dashboards still use
ownership only — a team member finds the record in its list and by link. An account's team
does not open the account's deals: each record has its own team.

### D-124 — Territories

The territory is an object (`territories`, tree by `parentTerritoryId`, cycle-checked) so it
gets lists, forms, lookups and reports for free; `crm.territory_assignments` is a table
because it needs dates, foreign keys to accounts, identities and teams, and a uniqueness
rule. A deal's `territoryId` resolves account → owner → owner's team at creation; open deals
follow their account when it is reassigned, closed deals keep theirs. The free-text
`territory` on deals is kept; a one-time backfill turns each distinct text into a territory
record and links the deals. Territory structure is readable by everyone who can read
territories; assigning needs `territory.manage`.

### D-125 — Campaign members; attribution is computed

`crm.campaign_members` (lead xor contact, status, responded, first/last touch); everyone an
email campaign is sent to becomes a member, and the migration turned existing recipients
into members. Cost columns were added to `crm.campaigns`. Attribution (first touch, last
touch, even) is worked out on request from members, opportunity contacts, contact roles and
lead conversions; the shares of a deal sum to exactly its amount. Nothing is stored, so
nothing can drift.

### D-126 — Refunds, credit notes, debit notes, adjustments

Four objects in the generic engine plus two allocation tables (`crm.refund_allocations`,
`crm.credit_allocations`). An invoice's balance is derived:
total + debit notes − payments (less refunds) − credit applied − write-offs, never negative.
The old `POST /payments/{id}/refund` now creates a refund record; a payment's
`refundedAmount` is derived from its succeeded refunds. Refunds come out of the unapplied
part of a payment first, then off its invoices, newest allocation first. A one-time backfill
creates a refund record for payments that already carried a refunded amount.

### D-127 — Work orders, resources, scheduling, entitlement usage, more SLA milestones

`work_orders`, `service_resources`, `resource_absences` are objects. Appointments gained
`resourceId`, `workOrderId`, `durationMinutes`. Saving an appointment locks its resource and
refuses a booking outside working hours, during an absence, without a needed skill, or over
capacity (409 / 422); `GET /scheduling/slots` offers only times that would be accepted.
Rescheduling creates a new appointment and marks the old one. A completed work order can be
invoiced (lines copied). `crm.entitlement_usage` counts cases and work-order hours;
`overagePolicy` blocks or flags. SLA policies gained assignment and customer-update
milestones, a warning percentage and an escalation contact. No GPS, routing or travel time.

### D-128 — Removing the connector workspace; sample data on request

The "Business Card Snap" workspace belongs to the app connector (D-36), which recreates it
at every start and files every app user in it as a lead. Since the app and the CRM are one
product (D-93) a business no longer needs it. With `CRM_CARDFLOW_SYNC=false` the connector
doesn't start, and a fresh start (D-108) then removes that workspace and its setup too; with
the connector on, the workspace is kept (emptied) as before. Turning the connector off also
removes the App users / Businesses pages and the app-ticket → Case mirror that lived in that
workspace; app support tickets themselves are unaffected.

`CRM_SAMPLE_DATA=<business code>` creates about 25 connected example records in one business,
once (marker `sample-data:<code>`): two leads, a customer and a vendor with contacts, a product
and a service in a default price book, a territory, two deals, a priced quote, an invoice
with a part payment, a contract with entitlement and SLA policy, an asset, a case, a
technician, a purchase order, an expense, a task and an event. They are made through the
normal record code path, are owned by the business's first member, and say "Sample record"
in their description. This replaces the "no sample data" rule of D-108 for the business the
owner names — nothing is seeded anywhere unless asked.

### D-129 — One-time removal of the test businesses on live (owner's instruction, 5 Oct 2026)

The owner asked for "Gova test", "surya work space" and "Business Card Snap" to be removed
from live with everything in them, without setting anything on the host. `crm/retire.go` does
it at start-up, once (marker `cleanup:test-workspaces-2026-10-05`), and only in a database
that has `gova-test` or `surya-work-space` — so it is a no-op locally and in other databases.
It is narrower than a fresh start: only rows of those three workspaces go (every `crm` table
with a `workspace_id`), their public listings are hidden, setups no business uses are
removed, and the app connector is retired (marker `connector:cardflow-retired`;
`CRM_CARDFLOW_SYNC=force` brings it back) so its workspace isn't recreated. People, saved
cards and other businesses are not touched. Because no backup can be taken from here, every
row is first copied to `crm.deleted_workspace_archive` (migration 0023) in the same
transaction. Afterwards "Ajay traders" gets the sample records of D-128.

### D-130 — Dashboard and menu layouts, per screen size

A business — and the owner console — can arrange its dashboard and its side menu: order
and show/hide, saved for everyone there, separately for desktop and for phones
(`crm.ui_layouts`: workspace, surface `dashboard` | `nav`, device `desktop` | `mobile`,
`{order, hidden}`; the owner console uses the platform workspace). Only the arrangement is
stored: permissions still decide what a person sees, and an item added to the product later
appears at the end. The Dashboard entry, Users & access and Profile & security can't be
hidden. Editing needs "Customize page layouts & fields" (Super Admin by default) through
"Edit menu" in the sidebar and "Edit layout" on the dashboard; one dialog has a Desktop and
a Mobile tab and saves both. The phone layout reads the mobile arrangement; it is edited
from the desktop dialog. Menu entries move within their section; sections keep their order.
The "Add existing app" button was removed from a product's Apps tab in the owner console
(a new app is still created there).

## D-131 — Phone number on the owner's "Invite admin"

The owner console's invite dialog takes an optional phone number next to name and email
(`POST /platform/workspaces/{id}/invitations`, field `phone`).

- A new person gets the number as a phone identifier, unverified: it becomes theirs when they
  sign in with a code sent to it, the same rule as adding a member by phone.
- A number that already belongs to somebody else is refused with a field error; nothing is saved.
- Someone who already has a login keeps their own number. The invitation never changes it,
  because a number is a way to sign in and only its holder should add one to a live account.
- The invitation itself is still delivered by email.

## D-132 — Signing in by phone accepts an open invitation

An invited person whose invitation carries a phone number (D-131) can sign in with a code
sent to that number and lands in the business: the sign-in accepts their open, unexpired
invitations and gives them the invited role, setups and permission sets. The inviter put
the number on the invitation, so the code proves they are the invited person, as the
emailed link does. The link still works and is how they set a password; the email address
stays unverified until then. Someone invited by email only is not affected.

## D-133 — The owner console signs in without two-step verification

At Ajay's request (2026-10-09) the platform owner is no longer asked for an authenticator
code when signing in to the owner console, even if one was set up, and is not made to set
one up. The owner's Profile page no longer shows the two-step section. Everyone else is
unchanged: admins on the live site must still use two-step verification (D-11), and anyone
who set it up is still asked for the code. The code is kept: deleting the `row.isOwner`
block in `identity/service.go` (and its twin in `identity/invitations.go`) switches it back
on. Trade-off: the owner account now relies on its password or email code alone.

## D-134 — Fixes and gaps from the October 2026 test round

Three end-to-end rounds (a normal business, a two-wheeler dealer, and the Salesforce
"Sales Overview" diagram) found these. Migration 0025 only adds.

- **Rules.** A contract can't end before it starts. A deal marked Closed lost needs a lost
  reason, as leads do. An account can't be placed under its own child, and a contact can't
  report to someone who reports to them (the check territories already had).
- **Closing a deal.** When a deal becomes won or lost and its close date is still in the
  future, the close date becomes today, so it counts in the period it was closed in. A past
  date is kept (imported history). A won deal turns a prospect account into an active customer.
- **Dashboard income** adds payments received (paid or partly refunded, less refunds) to the
  Income records. Someone who logs an Income record for the same money would count it twice.
- **Duplicates.** Documents and activities are no longer matched by name: a quote and its
  next version, or a contract and its renewal, are not duplicates.
- **Names.** An order made from "Quote — X" is "Order — X"; its invoice is "Invoice — X".
- **Workflows.** A Notify step with nobody to notify can't be published.
- **Campaigns.** A contact shows the campaigns of the lead it was converted from. A deal
  shows the campaigns that reached its people (the same people the results page credits);
  there is still no hand-picked "primary campaign" field.
- **Contacts** have "Reports to".
- **Partners.** New built-in relationship types: Partner / Reseller / Distributor /
  Implementation partner on a deal, and Reseller of / Distributor of between accounts.
- **Lists.** Appointments list who is coming, the service and the assigned resource.
  "Create a business" offers Automotive, Agriculture and Transport & logistics.

## D-135 — The remaining points from the test round

- A case can't be marked Resolved or Closed without a resolution.
- When a quote is accepted (turned into an order), its deal's amount becomes the quote's total.
- Service resources, territories, price books, products and SLA policies saved with no
  status start Active.
- Users & access shows a person's mobile number when they have no email.
- Sign-in codes on the page are a server setting, not code: they show only while
  `OTP_PREVIEW_INSECURE=true` (or a non-production preview) is set. Real use needs
  `SMS_PROVIDER` (msg91, twilio or fast2sms) with its keys, and that flag removed.

## D-136 — Integrity hardening after the schema review

An outside review of the ER diagrams listed nine structural concerns. Each was checked
against the real database and code. Migration 0026 only adds; every constraint is created
NOT VALID and then validated where the data allows, so a stray old row can't stop a deploy.

- **Same tenant (review #3).** For every foreign key between two tenant tables there is now a
  second key on `(workspace_id, column) → (workspace_id, id)`: a row can only reference a row
  of its own workspace. This includes the keys into `crm.object_records`.
- **Typed keys (#2).** `crm.check_record_type()` on the allocation, price-book, bundle, SLA,
  territory and entitlement tables: an `invoice_id` must be an invoice, not any record.
- **Money links (#1).** Invoices, payments and line items stay in `crm.object_records`, but
  `crm.check_money_links()` makes the database check their links itself (exists, same
  workspace, right kind). Moving them to their own tables is a larger change, not done here.
- **Tenant keys (#7).** The 18 tables that carried `workspace_id` without a key now have one.
- **Custom object keys (#4).** Keys stay unique across the platform, but a business is no
  longer blocked by another business's object: it gets a free variant (`site_visits_2`).
- **Deletes (#5).** Invoices, payments, refunds, credit notes, debit notes and adjustments can
  go to the recycle bin but can't be deleted for good, so allocations never cascade away.
- **Orphans (#8).** Deleting a record for good also clears its approval requests and card
  links (files, timeline, relationships, team members and favourites were already cleared).
- **Indexes (#9).** The generic table already had a GIN index and per-object expression
  indexes; account, contact and opportunity lookups are added.
- **Card links.** `card_links.card_id` now references `public.saved_cards`.
- **Not a defect (#6).** Person lookups (`assignedTo`, `managerId`, …) point at
  `crm.identities` and are checked for active membership on save; the diagram's "users"
  label was misleading.
- **Not done.** Row-level security in PostgreSQL; checking in the database that an owner is
  a member; real tables for financial documents; queues and email templates.

## D-137 — Who added what, and team performance

For a business with staff (a dealer with several salespeople), the Super Admin needs to see
who brought in each lead and how each person is doing.

- **Added by.** The "Created by" field is labelled "Added by" and is a default column of the
  leads, accounts and contacts lists (next to Owner). Export carries it.
- **Team performance** on the dashboard (`GET /w/{code}/dashboard/team`, section
  `section:team`): per person, leads added, leads converted, conversion %, contacts and
  accounts added, deals won and their value; a bar chart of leads per day (per month for
  long periods) and a line per person. Download as CSV. Only for people who see everyone's
  leads; staff who see only their own get 403 and the section is hidden.
- **Reports** can be downloaded as CSV from the report builder.
- **Bulk import** of leads already exists (Import from CSV). It needs the Import permission,
  which Staff don't have by default; the Super Admin can grant it in the permission set.

## D-138 — An Admin can add staff, and the users list says who added whom

- The Admin role (and its "Admin access" permission set) includes "Manage users": an Admin
  can add and manage staff, never with more access than their own (so not a Super Admin).
  Migration 0027 gives this once to the Admin set of businesses that already exist.
- Users & access shows an **Added by** column (the member who gave the person access).

## D-139 — Follow-ups from the dealer round

- **A deal's items follow the accepted quote.** When a quote is accepted, the deal's own
  line items are replaced by the quote's lines, so Amount and the Items total agree.
- **A reassigned case stays visible.** When a case's owner changes, the previous owner and
  the person who logged it are put on the case team with read access.
- **Staff can import.** The Staff role (and its "Staff access" set) includes Import on
  every object they can create; migration 0028 adds it once to existing businesses.
- **Staff still can't record payments.** Money in and out stays with Admins unless a
  permission set grants it (D-96); this was left as it is on purpose.

## Seed

- Local/dev: platform workspace `platform`, system roles, owner `ajay@gmail.com` / `Ajay1234`.
  Idempotent. Refused (D-06) when `CRM_APP_ENV=production`.
