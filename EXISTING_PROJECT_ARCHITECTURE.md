# Existing Project Architecture

Analysis of the repository as it stands on `main` at commit `f0309e5` (5 Oct 2026).
Written before the CRM-first transformation. **No production code was changed to produce this document.**

Rules used while writing it:

- Every conclusion cites the file (and function, component or table) that proves it.
- Anything that could not be determined from the repository is marked `UNKNOWN — requires clarification`.
- The live database and live accounts were not inspected. Statements about "live" come from config files in the repo only.
- This repository is public. No secret values appear here, only setting names.

---

## 1. Executive Summary

The repository holds **two products that share one server, one database and one web build**:

| | **CardFlow app** ("Business Card Snap") | **Ajay's CRM** |
|---|---|---|
| What it is | Consumer app: scan business cards, keep a card vault, list your own business, browse businesses | Multi-tenant CRM platform with an Owner Console |
| Frontend | React Native Web, plain JavaScript, `frontend/src/` (≈15,000 lines) | React DOM + TypeScript + Tailwind, `frontend/src/crm/` (≈38,400 lines) |
| Backend | Go packages in `backend/internal/{auth,card,business,discovery,billing,support,contacts,enquiry}` | Go module `backend/internal/crm/` (≈31,500 lines) |
| API | `/api/v1/*` | `/api/crm/v1/*` |
| Database | Postgres, `public` schema, 30+ tables | Same Postgres, `crm` schema, 50+ tables |
| Sign-in | Mobile number + OTP → JWT in `localStorage` | Email + password (or email code, Google, Microsoft, LinkedIn, SAML, OIDC) → server session cookie, MFA |
| Mobile | Yes, Capacitor shells for Android and iOS | No. Responsive web only; the native app cannot reach it |

The five findings that matter most for the transformation:

1. **A full CRM already exists.** Leads, accounts, contacts, opportunities, tasks, calendar events, notes, cases, reports, dashboards, workflows, email, campaigns, roles, permission sets and custom objects are implemented and multi-tenant (`backend/internal/crm/records`, `backend/internal/crm/access`). The transformation is mostly about **making the mobile app a client of this CRM**, not about building a CRM.
2. **The tenant already exists, under a different name.** What the UI calls a **Product** is the table `crm.workspaces`: one per customer business, with its own users, roles and records. What the UI calls an **App** is `crm.products` (a setup: modules, roles, sign-in methods) linked through `crm.workspace_products`. "Business A / Business B / Business C" in the target design maps directly onto `crm.workspaces`.
3. **The two products have two separate user systems** that are only joined by a background sync (`backend/internal/crm/connectors/cardflow`). App users live in `public.users` (phone). CRM users live in `crm.identities` (email). Unifying them is the central piece of work.
4. **The app's sign-in is not safe for production today.** The OTP is returned in the API response and no SMS is ever sent (`backend/internal/auth/service.go`, `SendOTP`). Refresh, logout-all, account delete and data export are placeholders.
5. **Several app features are placeholders**: server-side card OCR, GST verification, business analytics, digital card, enquiries. Card reading actually happens in the browser with Tesseract.js.

Income and expenses do not exist anywhere in the codebase.

---

## 2. Current Architecture

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ CLIENTS                                                                      │
│                                                                              │
│  Android / iOS app (Capacitor shell, appId app.cardflow.mobile)              │
│  └─ loads the same web bundle; path is "/", so it always runs CardFlow       │
│                                                                              │
│  Browser                                                                     │
│  ├─ /            → CardFlow app      (React Native Web, JS)                  │
│  ├─ /share/:id   → shared card view  (CardFlow, no login needed)             │
│  └─ /crm/*       → Ajay's CRM        (React DOM, TypeScript, Tailwind)       │
│                                                                              │
│  frontend/src/entry.js picks which app to load from the URL path             │
└───────────────┬───────────────────────────────────┬──────────────────────────┘
                │ Bearer JWT (localStorage)         │ Cookie session crm_sid + CSRF
                ▼                                   ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ ONE GO SERVER  backend/cmd/api/main.go  (chi router, "modular monolith")     │
│                                                                              │
│  /api/v1/*                         /api/crm/v1/*                             │
│  auth · cards · businesses ·       identity · platform (owner console) ·     │
│  discovery · billing · support ·   records engine · access · connectors ·    │
│  contacts · enquiries              mail · oauth · saml                       │
│                                                                              │
│  /api/webhooks/revenuecat          /b/{slug} public business page            │
│  /*  → embedded web bundle (backend/cmd/api/dist, committed to git)          │
│                                                                              │
│  Background: CardFlow→CRM connector sync, CRM event worker                   │
└───────────────┬──────────────────────────────────────────────────────────────┘
                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ ONE POSTGRES DATABASE (Neon in production)                                   │
│   schema public  → CardFlow tables (users, businesses, saved_cards, …)       │
│   schema crm     → CRM tables (identities, workspaces, leads, accounts, …)   │
│ Optional: Redis (OTP store), S3 (card images; otherwise stored in Postgres)  │
└───────────────┬──────────────────────────────────────────────────────────────┘
                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ EXTERNAL SERVICES ACTUALLY CALLED                                            │
│  RevenueCat (subscriptions) · Brevo or SMTP (CRM email) ·                    │
│  Google / Microsoft / LinkedIn OAuth (CRM sign-in, mailboxes, calendar) ·    │
│  IMAP (CRM mailboxes) · customer SAML / OIDC identity providers ·            │
│  Tesseract.js (OCR, runs in the browser, not a server call)                  │
│                                                                              │
│ CONFIGURED BUT NOT CALLED BY ANY CODE                                        │
│  SMS provider · Gemini / Vertex AI · KYC provider · FCM push ·               │
│  Google Play / Apple billing keys · Google Maps keys · Sentry                │
└──────────────────────────────────────────────────────────────────────────────┘
```

Hosting (from `render.yaml`, `frontend/vercel.json`, `HANDOVER.md`):

- **Render** runs the Go server (`buildCommand: go build -o server ./cmd/api`). It also serves the embedded web bundle.
- **Vercel** serves the web build and forwards `/api/crm/*` to Render (`frontend/vercel.json` rewrites). CardFlow's `/api/v1` is called directly on the Render host (`frontend/src/services/api.js`, `getBaseUrl()`).
- **Neon** hosts Postgres. Render reaches it through `DATABASE_URL`.

How data flows today:

| Data | Flow |
|---|---|
| Sign-in (app) | phone → `POST /api/v1/auth/otp/send` → `POST /auth/otp/verify` → JWT → `localStorage` |
| Cards | camera → Tesseract.js in the browser → review form → `POST /api/v1/cards` → `public.saved_cards` |
| Businesses | app form → `POST /api/v1/owner/businesses` → `public.businesses` |
| App user → CRM | connector reads `public.users` → creates a CRM lead, converts it to a contact (`connectors/cardflow/cardflow.go`, `upsertUser`) |
| Business → CRM | connector reads `public.businesses` → CRM account (owned) or lead (unclaimed card business) (`connectors/cardflow/card_leads.go`, `upsertBusiness`) |
| Support ticket | app → `public.support_tickets` (+ `support_ticket_messages`) → mirrored as a CRM Case (`connectors/cardflow/cases.go`) |
| CRM records | browser → `/api/crm/v1/w/{code}/crm/{object}` → `crm.*` tables, scoped by `workspace_id` |

---

## 3. Repository Structure

```text
CardFlow/
├── backend/
│   ├── cmd/api/main.go            server entry: wiring, every /api/v1 route, embedded bundle
│   ├── cmd/api/dist/              built web bundle, committed (served by the Go server)
│   ├── internal/
│   │   ├── auth/                  app OTP + JWT (service.go, jwt.go, handler.go, profile.go)
│   │   ├── middleware/auth.go     app Bearer-token check (Authenticate)
│   │   ├── domain/models.go       app structs: User, Business, SavedCard, Enquiry, …
│   │   ├── card/                  card vault, one-business-per-GSTIN linking
│   │   ├── business/              a user's own businesses
│   │   ├── discovery/             categories, business search, public profile page
│   │   ├── billing/               RevenueCat status, sync, webhook
│   │   ├── support/               tickets and ticket conversation
│   │   ├── contacts/              phone-contacts backup (native app only)
│   │   ├── enquiry/               enquiries (placeholder, nothing saved)
│   │   ├── extractor/gemini.go    server OCR (placeholder, returns empty data)
│   │   ├── storage/s3.go          optional S3 for images
│   │   ├── config/config.go       every app environment variable
│   │   ├── database/              pool, Redis client, migrations 001–015
│   │   ├── kyc/, notification/    EMPTY directories
│   │   └── crm/                   the whole CRM module
│   │       ├── crm.go             module wiring and route mounting
│   │       ├── identity/          sign-in, sessions, MFA, OTP by email, sign-up, invitations
│   │       ├── platform/          owner console: products (apps), workspaces, users, roles, audit
│   │       ├── access/            roles, permission sets, effective access, navigation
│   │       ├── records/           record engine, filters, views, reports, workflows, mail, API keys…
│   │       ├── connectors/cardflow/  sync between the app and the CRM
│   │       ├── mail/, oauth/, saml.go, shared/, seed/
│   │       ├── store/migrations/  CRM migrations 0001–0016
│   │       └── docs/DECISIONS.md  design decisions D-01 … D-92
│   ├── migrations/                duplicate copy of app migrations 001–002 (not used by the code)
│   ├── dist/                      an older bundle copy on this machine (git-ignored, not the one embedded)
│   └── pkg/{response,validator}/  JSON envelope, phone normalisation
├── frontend/
│   ├── src/entry.js               chooses CardFlow or CRM by URL path
│   ├── src/index.js, App.js       CardFlow bootstrap
│   ├── src/navigation/AppNavigator.js   CardFlow navigation (state machine, no router)
│   ├── src/context/AuthContext.js       CardFlow global state (761 lines)
│   ├── src/services/api.js              CardFlow API client
│   ├── src/services/subscription/       RevenueCat (native + web)
│   ├── src/screens/{auth,user,owner}/   CardFlow screens
│   ├── src/components/, utils/, theme/, data/mockData.js
│   ├── src/crm/                   the CRM frontend (own tsconfig, Tailwind, i18n)
│   ├── android/, ios/             Capacitor native projects
│   ├── capacitor.config.json, webpack.config.js, vercel.json
├── docs/                          API.md, ARCHITECTURE.md, DATABASE.md… written 31 Aug 2026, BEFORE the CRM existed
├── CardFlow-PRD-v1.6-Final.md     the original CardFlow PRD
├── HANDOVER.md                    run, release and deploy notes
├── render.yaml                    Render blueprint
└── CardFlow-debug.apk             a debug Android build on this machine (git-ignored, not in the repo)
```

Size and age: 145 commits, first commit 31 Aug 2026.

---

## 4. Authentication

There are **two unrelated authentication systems**.

### 4.1 CardFlow app: mobile number + OTP

| Step | What happens | Code |
|---|---|---|
| Enter number | Login screen calls `sendOtp(phone)` | `frontend/src/screens/auth/LoginScreen.js` → `AuthContext.sendOtp` → `apiClient.sendOtp` (`frontend/src/services/api.js`) |
| OTP generated | 6 digits from `crypto/rand` | `backend/internal/auth/service.go`, `generate6DigitCode()` |
| OTP stored | In an in-memory map `otpStore[phone]`; also in Redis (`otp:<phone>`, 5-minute expiry, stores both a hash **and the plain code**) when Redis is configured | `service.go`, `RequestOTP()` |
| OTP "sent" | **It is not sent.** The code is written to the server log and returned to the caller as `otp_preview` | `service.go`, `RequestOTP()` (`slog.Info("🚀 [OTP DISPATCHED]"…)`) and `SendOTP()` |
| OTP shown | The app prints `OTP: <code>` on the OTP screen | `frontend/src/screens/auth/OtpScreen.js` (`styles.otpPreview`, `lastSentOtp`) |
| OTP verified | Compared with the in-memory map, then Redis; deleted on success | `service.go`, `validateOTP()` |
| User found or created | `SELECT … FROM users WHERE phone = $1`; if missing, `INSERT INTO users` with name "CardFlow User", city Coimbatore, 30 free scans, then a 10-credit `credit_ledger` row | `service.go`, `resolveUser()` |
| Card businesses claimed | Unclaimed businesses whose `contact_phone` equals this phone are given to the user | `service.go`, `claimCardBusinesses()` |
| Token issued | HS256 JWT signed with `JWT_PRIVATE_KEY` (a shared string, despite the name). Claims: `sub`, `phone`, `role`, `plan`. Lifetime `JWT_ACCESS_EXPIRY_MINUTES` (default 15) | `backend/internal/auth/jwt.go`, `GenerateTokenPair()` |
| Token stored | `localStorage` keys `cf_token` and `cf_user` | `frontend/src/context/AuthContext.js`, `verifyOtp()` |
| Each request | `Authorization: Bearer <jwt>`; the middleware validates it and reloads role, plan and subscription from the database | `backend/internal/middleware/auth.go`, `Authenticate()` |
| First-time sign-in | `is_new_user` is true → the Onboarding screen asks for a name | `AppNavigator.js` (`isNewUser` → `OnboardingScreen`), `AuthContext.completeOnboarding` |

Things that do **not** work the way their names suggest:

| Feature | Reality | Code |
|---|---|---|
| Refresh token | The token returned at sign-in is a random string that is never stored. `POST /auth/refresh` returns made-up strings that are not valid JWTs. The app never calls it. | `jwt.go` (`cf_refr_…`), `handler.go` `RefreshToken()` |
| Token expiry | When the JWT expires every call returns 401 and the app keeps showing the signed-in UI. Nothing signs the user out or refreshes. | No 401 handling in `frontend/src/services/api.js` |
| Logout | Client only: clears `localStorage`. The JWT stays valid until it expires. | `AuthContext.logout()` |
| Logout all | Returns a success message and does nothing. | `handler.go` `LogoutAll()` |
| Delete account | Returns a success message and does nothing. | `handler.go` `DeleteMe()` |
| Export my data | Returns a hard-coded shell with empty lists and a fixed date. | `handler.go` `ExportMe()` |
| Rate limiting | None on OTP send or verify. No attempt counter is enforced. | `service.go` |
| SMS settings | `SMS_PROVIDER`, `SMS_AUTH_KEY`, `DEV_MOCK_SMS` are read into config and used by no code. | `backend/internal/config/config.go` |

**Google login in the app:** does not exist.
**Admin login in the app:** there is none. The in-app admin console was removed on 1 Oct 2026 (commit `7413a52`). An account with `users.role = 'admin'` gets the normal user flow (`AuthContext.setRole`).

### 4.2 CRM: Owner Console and product users

| Topic | How it works | Code |
|---|---|---|
| Identifiers | Email, phone or login ID in `crm.verified_identifiers` (`kind IN ('email','phone','login_id')`) | `store/migrations/0001_foundation.sql`, `identity/normalize.go` |
| Password | Argon2id hash in `crm.password_credentials`; 5 failures lock for 15 minutes | `identity/password.go`, `identity/service.go` `Login()` |
| Owner sign-in | `/crm/owner/login`, audience `owner`; only `identities.is_platform_owner` passes | `identity/service.go` `finishSignIn()`, `identity.RequireOwner` |
| Product sign-in | `/crm/login`; the server resolves which product(s) the person belongs to | `identity/handlers.go` `handleLogin` |
| Email code | 6-digit code by email, 10 minutes, 5 attempts, stored hashed in `crm.otp_challenges`. **Email only**: `RequestOTP` rejects anything that isn't an email | `identity/otp.go` |
| Google / Microsoft / LinkedIn | OAuth, enabled per app setup | `oauth/oauth.go`, `crm.go` `oauthCallback` |
| SAML / OpenID Connect | One provider per product | `saml.go`, `records/sso.go`, `records/oidc.go` |
| MFA | TOTP plus recovery codes; required for owner, Super Admin and Admin outside local | `identity/service.go` `VerifyMFA`, `EnrollMFA` |
| Session | Server-side row in `crm.sessions`; cookie `crm_sid` (HTTP-only) plus `crm_csrf` double-submit token. Idle 30 minutes (privileged) or 7 days; absolute 30 days | `identity/sessions.go` |
| Sign-up | Self sign-up per product if the setup allows it; invitations (72 h); invite links limited to company email domains | `identity/signup.go`, `identity/invitations.go`, `records/invite_link.go` |
| Logout | Revokes the session row; "logout all" revokes every session | `identity/service.go` `Logout()` |
| Owner bootstrap | `CRM_OWNER_EMAIL` + `CRM_OWNER_BOOTSTRAP_PASSWORD` create the owner once | `seed/seed.go`, DECISIONS D-17 |

**Super Admin login:** a Super Admin is a normal CRM identity with the `SUPER_ADMIN` role in one workspace. They sign in at `/crm/login` and land in `/crm/w/<code>`.

---

## 5. Users & Roles

### 5.1 App user model

Table `public.users` (`backend/internal/database/migrations/001_initial_schema.sql`, plus 008 and 013). Go struct `domain.User` (`backend/internal/domain/models.go`).

| Field | Type | Notes |
|---|---|---|
| `id` | UUID | Primary key |
| `phone` | varchar(20), unique | The login ID, E.164 |
| `name`, `email`, `photo_url` | text | Email is optional and never verified |
| `city`, `state`, `country` | text | Defaults: Coimbatore, Tamil Nadu, IN |
| `role` | enum `user_role` (`user`, `admin`) | "owner" is **not** a database role |
| `plan` | enum (`free`, `plus`, `premium`) | Legacy; premium is decided by the fields below |
| `status` | enum (`pending_profile`, `active`, `suspended`, `deleted`) | |
| `free_scans_remaining`, `free_scans_reset_at` | | Stored, but no code decrements them |
| `is_subscribed`, `subscription_plan_id`, `subscription_expires_at`, `subscription_status`, `subscription_source`, `subscription_store`, `subscription_will_renew`, `subscription_management_url`, `subscription_event_at`, `subscription_updated_at` | | Written by RevenueCat sync (`billing/handler.go` `applyState`) and by the CRM's "grant access" |
| `created_at`, `updated_at`, `last_login_at`, `deleted_at` | timestamps | Soft delete |

Relationships of an app user:

```text
public.users
 ├── businesses.owner_user_id        the businesses this person owns (0..n)
 ├── businesses.created_by_user_id   card businesses this person's scan created
 ├── saved_cards.user_id             their private card vault
 ├── support_tickets.user_id         their tickets
 ├── devices.user_id                 device rows written at sign-in
 ├── user_kyc.user_id                KYC status (table exists; nothing writes it)
 ├── credit_ledger / purchases / subscriptions / subscription_payments   (legacy billing tables)
 ├── contact_backups.user_id         phone contacts backup
 └── revenuecat_events.user_id       billing events
```

An app user has **no** product, app, team or role relationship. Those concepts only exist in the CRM.

### 5.2 App roles

| Role | Where it exists | What it does |
|---|---|---|
| `user` | `users.role` | Everything in the app |
| `admin` | `users.role` | Nothing in the app any more. In the CRM connector, an app admin with an email gets a Super Admin invitation (`connectors/cardflow/cardflow.go`, `upsertUser`, `u.Role == "admin"`) |
| "owner" | Frontend only (`AuthContext.switchToOwnerMode`) | A leftover UI mode. No backend meaning. Any user can own businesses |

### 5.3 CRM user model

```text
crm.identities                 a person (display_name, is_platform_owner, status)
 ├── crm.verified_identifiers  their email / phone / login id
 ├── crm.password_credentials  password hash, lockout
 ├── crm.mfa_methods           TOTP
 ├── crm.sso_links             Google / Microsoft / SAML subject
 ├── crm.sessions              live sessions
 └── crm.memberships           one per workspace they belong to (status, user_type)
      ├── crm.role_assignments          → crm.roles (one role), product_ids[] = which apps they may use
      └── crm.membership_permission_sets → crm.permission_sets
```

CRM roles (`backend/internal/crm/access/roles.go`, `SystemRoles`): `SUPER_ADMIN` (rank 100), `ADMIN` (80), `STAFF` (50), `END_USER` (10), plus custom roles per workspace. Roles form a tree (`roles.parent_role_id`, migration 0007) and only widen **which records** a person sees. **Permission sets** grant what they can do. Super Admin always has full access. See section 16.

### 5.4 How the two user systems are joined today

They are not the same record. `crm.external_links` (`system='cardflow'`, `external_type='user'`, `external_id = users.id`) points an app user at the CRM lead and contact created for them. There is no shared login: an app user cannot sign in to the CRM with their phone, and a CRM user cannot sign in to the app with their email.

---

## 6. Owner Console

Route prefix `/crm/owner/*`. Every API it calls sits behind `identity.RequireOwner` (`backend/internal/crm/platform/platform.go`, `Routes`). Only the single identity with `is_platform_owner = true` can enter. Sidebar items come from the server (`backend/internal/crm/access/capabilities.go`).

| Module | Route | What it does | APIs | Tables read / written | Frontend files |
|---|---|---|---|---|---|
| Login | `/crm/owner/login` | Email + password, then MFA | `POST /auth/login`, `/auth/mfa/*` | `identities`, `password_credentials`, `sessions`, `mfa_methods` | `features/auth/login-pages.tsx`, `mfa-pages.tsx` |
| Overview | `/crm/owner/dashboard` | Count cards (products, apps, leads, accounts, contacts, users, invitations), setup checklist, recent products. Follows the Product / App filter | `GET /platform/dashboard?product=&app=` | Reads counts across `workspaces`, `workspace_products`, `leads`, `accounts`, `contacts`, `memberships`, `invitations` (`platform/dashboard.go`) | `features/owner/dashboard-page.tsx`, `owner-filter.tsx` |
| Platform CRM: Leads, Accounts, Contacts | `/crm/owner/leads` etc. | Record lists across every product, with Product and App pickers; the owner's own "Platform CRM" list | `GET/POST /platform/crm/{object}`, `…/{id}`, convert, bulk, import, export | `crm.leads`, `crm.accounts`, `crm.contacts`, `lead_conversions` | `features/records/record-list-page.tsx`, `record-detail-page.tsx` |
| Products | `/crm/owner/workspaces` | One row per customer business. Create (Details → Super Admin → Review), suspend, open, manage its apps and people | `GET/POST /platform/workspaces`, `PATCH …/{id}`, `POST …/{id}/products`, `…/invitations` | `crm.workspaces`, `workspace_products`, `memberships`, `invitations` (`platform/workspaces.go`, `invitations.go`) | `features/workspaces/*` |
| Apps | `/crm/owner/apps` | Every app installed in every product | `GET /platform/apps` | `workspace_products` ⨝ `products` ⨝ `workspaces` (`platform/dashboard.go` `handleListApps`) | `features/owner/apps-page.tsx` |
| App setup | `/crm/owner/products/:id` | The 5-step setup wizard: details, objects (modules), roles & user types, sign-in & integrations, review & publish. Publishing creates a version and upgrades products using it | `GET/POST/PATCH /platform/products`, `…/publish`, `…/archive` | `crm.products` (`draft_config`), `product_versions` (`platform/products.go`) | `features/products/product-setup-wizard.tsx`, `setup-steps.tsx` |
| Objects | `/crm/owner/objects` | Standard and custom object definitions, fields, statuses | `GET/POST/PATCH /platform/objects` | `crm.object_definitions`, `field_definitions` (`records/objects.go`) | `features/objects/*` |
| Users & access | `/crm/owner/users` | Everyone with a login; edit, reset password, reset MFA, sign out everywhere, change role, permission sets, role tree | `GET/POST/PATCH /platform/users…`, `/platform/workspaces/{id}/roles`, `…/permission-sets` | `identities`, `memberships`, `role_assignments`, `roles`, `permission_sets` (`platform/users.go`, `access.go`, `hierarchy.go`) | `features/users/*`, `features/access/*` |
| Integrations | `/crm/owner/integrations` | Status of the Business Card Snap connector and of Google / Microsoft / LinkedIn sign-in providers; "sync now" | `GET /platform/integrations`, `POST /platform/integrations/cardflow/sync`, `GET /oauth/providers` | `crm.connector_state` (`connectors/cardflow/api.go`) | `features/integrations/*` |
| Audit log | `/crm/owner/audit` | Every privileged action | `GET /platform/audit` | `crm.audit_events` | `features/audit/audit-page.tsx` |
| Profile & security | `/crm/me` | Name, password, MFA, sessions | `/me`, `/me/sessions`, `/auth/password/change` | `identities`, `sessions` | `features/me/profile-page.tsx` |
| Entering a product | `/crm/w/<code>` | The owner can open any product with full access; each entry is audited | `GET /w/{code}/context` | writes `audit_events` (`workspace.entered`) (`records/scope.go` `memberScope`) | `features/workspace/*` |

Any other `/crm/owner/*` path shows a "coming soon" page (`frontend/src/crm/app.tsx`, `NavComingSoon`).

---

## 7. Products & Apps

The names in the UI and the names in the code are different. This is the single most confusing part of the codebase.

| UI word | Code / table | Meaning |
|---|---|---|
| **Product** | `crm.workspaces` (Go: `platform/workspaces.go`; routes say `workspaces`) | **A tenant.** One customer or business: its users, roles, records. Has `code` (used in URLs), currency, time zone, status |
| **App** | `crm.products` + `crm.product_versions` (Go: `platform/products.go`) | **A setup.** Which modules/objects are on, role templates, user types, sign-in methods, API/webhook switches, lead-conversion rules. Versioned |
| App installed in a product | `crm.workspace_products` (`workspace_id`, `product_id`, `config_version`, `status`) | The link. A product can have many apps; an app (setup) can be reused by many products |
| **Super Admin** | `crm.memberships` + `role_assignments` → role `SUPER_ADMIN` in that workspace | The top person inside one product |
| Platform CRM | the workspace with `is_platform = true` | The owner's own records |

The actual relationship:

```text
Platform owner  (crm.identities.is_platform_owner)
   │ creates
   ▼
Product  = crm.workspaces            ← tenant boundary: every record carries workspace_id
   │ installs 1..n                   (crm.workspace_products)
   ▼
App      = crm.products @ version    ← decides menu, objects, roles template, sign-in methods
   │
   ▼
Super Admin = membership(role SUPER_ADMIN)   invited when the product is created
   │ invites / manages (within own access)
   ▼
Admin, Staff, End user, custom roles = memberships
```

Answers to the specific questions:

- **How are Products created?** Owner only. `POST /platform/workspaces` (`platform/workspaces.go`, `handleProvisionWorkspace`, idempotent via `Idempotency-Key`). UI: `features/workspaces/provision-page.tsx`, steps Details → Super Admin → Review. It inserts `crm.workspaces`, system roles, default permission sets ("Admin access", "Staff access") and, unless "invite later" is ticked, a membership plus invitation for the Super Admin.
- **How are Apps created?** Owner only. `POST /platform/products` then the wizard saves `draft_config` (`PATCH`) and `POST …/publish` writes a `product_versions` row. From a product's **Apps** tab: New app, Add existing app, Edit app setup (`features/workspaces/workspace-products-tab.tsx`).
- **How are Apps connected to Products?** `POST /platform/workspaces/{id}/products` inserts `workspace_products`. Publishing a new app version upgrades every product on it unless `upgradeWorkspaces: false` (DECISIONS D-45).
- **How are Apps assigned to users?** `crm.role_assignments.product_ids[]` lists the apps a member may use. A member's objects are usable only if one of their apps enables that module (`access/effective.go`).
- **How does the Super Admin see them?** `GET /w/{code}/context` returns the apps and navigation (`records/scope.go`, `handleContext`, field `Apps`). The sidebar has an app switcher; "All apps" by default (DECISIONS D-73). Records are shared across a product's apps; an app only narrows the menu.
- **Can a customer create their own product or app?** No. Both are behind `identity.RequireOwner`. A product's people with the "customize" capability can create their own *objects* (`/w/{code}/objects`, D-79), not their own workspace.

---

## 8. Businesses / Organizations

There are **three different things** that could be called "a business". They are separate tables.

| Concept | Table | Belongs to | Purpose |
|---|---|---|---|
| App business listing | `public.businesses` | `owner_user_id` → `public.users` (nullable since migration 014) | A directory listing a user created, or an unclaimed business created from a scanned card |
| CRM tenant | `crm.workspaces` | the platform | A customer's CRM space |
| CRM account | `crm.accounts` (`kind` = `business` or `individual`) | `workspace_id` | A company record inside one tenant's CRM |

### 8.1 App business (`public.businesses`)

Fields (`001_initial_schema.sql`, `014_card_businesses.sql`, struct `domain.Business`): name, slug, description, primary category, address, city, state, pincode, `location` (PostGIS point), hours, GSTIN, PAN, TAN, legal/trade name, `status` (`draft` … `live`), `verification`, `listing` (`unlisted`/`listed`), completeness, `source` (`owner` or `card`), `contact_name`, `contact_designation`, `contact_phone`, `created_by_user_id`, `claimed_at`.

Child tables: `business_categories`, `business_services`, `business_phones`, `business_media`, `business_verifications`, `business_card_images`, `digital_cards`.

- **Ownership:** `owner_user_id`. Checked on the server by `business/service.go`, `VerifyOwnerAccess()`.
- **Multiple businesses per user:** supported (`GET /owner/businesses`, `business/service.go` `GetOwnerBusinesses`; UI `MyBusinessHubScreen.js`).
- **One business per GSTIN:** unique index on `upper(gstin)`; enforced in `card/business_link.go`, `linkCardBusiness()`.
- **Team members of a business:** do not exist. A business has exactly one owner and no other users.

### 8.2 How data is tied to a business today

- In the **app**, data belongs to a **user**, not a business. `saved_cards.user_id` is the owner of a card; `saved_cards.linked_business_id` only points at the business the card is about.
- In the **CRM**, data belongs to a **workspace**. Every record table has `workspace_id`; every request recomputes access and filters by it (`records/scope.go`, `memberScope`; `records/listing.go`, `paramsFrom`).
- Isolation is enforced **in application code only**. There is no Postgres row-level security anywhere in `backend/internal/crm/store/migrations` (0 occurrences of `ROW LEVEL SECURITY`); DECISIONS D-19 records it as not done.

---

## 9. Business Card / Business Snap

### 9.1 What actually happens when a card is scanned

The flow in the brief (scan → extract → find person → map or create) is **partly right**. The matching is by **business (GSTIN)**, not by person, and there is no person entity at all.

```text
Scan tab (ScanCardScreen.js)
  │ camera via navigator.mediaDevices.getUserMedia, or pick a file
  ▼
Corner adjust + perspective crop      components/CardCornerAdjuster.js, utils/perspectiveCrop.js
  ▼
OCR IN THE BROWSER                    utils/ocrParser.js  extractCardWithTesseract()  (tesseract.js)
  │ front and back are read separately, then mergeExtractions()
  ▼
Regex parsing                         parseBusinessCardText(): name, designation, company,
  │                                   phones, emails, website, GSTIN, pincode, address
  ▼
Review form (user edits the fields)
  ▼
POST /api/v1/cards                    card/handler.go CreateCard()
  │ free plan: at most 5 saved cards (FreeSavedCardLimit) → 402 UPGRADE_REQUIRED
  ▼
card/service.go CreateSavedCard()
  ├─ has a valid GSTIN?  ── no ──► card saved in the user's vault only. Nothing else is created.
  │        yes
  ▼
card/business_link.go linkCardBusiness()
  ├─ a business with this GSTIN exists ─► link the card to it (linked_business_id);
  │                                       fill only its empty fields and missing card photos
  └─ none exists ─► createCardBusiness(): unclaimed business
                    (owner_user_id NULL, source 'card', draft, unlisted, never in search)
  ▼
saved_cards (+ saved_card_phones, saved_card_emails, saved_card_addresses, saved_card_images)
  ▼
POST /api/v1/cards/{id}/original-image  (front, back) → Postgres BYTEA, or S3 when configured
```

Later:

- **Claim:** when someone signs in with the phone printed on a card, the unclaimed business becomes theirs (`auth/service.go`, `claimCardBusinesses()`).
- **CRM:** the connector turns each unclaimed card business into a CRM **Lead** (`source card_scan`), and converts it into an Account + Contact when claimed (`connectors/cardflow/card_leads.go`).

### 9.2 Component inventory

| Part | State | Code |
|---|---|---|
| Camera capture | Working (web APIs) | `ScanCardScreen.js` |
| Crop / perspective | Working | `CardCornerAdjuster.js`, `perspectiveCrop.js` |
| OCR | Working, in the browser, Tesseract.js | `utils/ocrParser.js` |
| Field extraction | Regex and heuristics; English-oriented | `ocrParser.js` (`extractCompany`, `extractPersonName`, …) |
| Server OCR | **Placeholder.** `ExtractCardFromImage` sleeps 50 ms and returns empty fields. `POST /cards/scan` is not used by the Scan screen | `extractor/gemini.go`, `card/handler.go` `ScanCard` |
| Person / contact creation | **Not implemented.** The card row *is* the contact; there is no person table | `saved_cards` |
| Existing-person matching | **Not implemented** | |
| Business matching | By GSTIN only | `card/business_link.go` |
| Duplicate detection | Client-side "already saved?" check by GSTIN, then name or phone | `AuthContext.js` `isBusinessSaved`, `findSavedCardForBusiness` |
| Saved cards / My Cards | Working: list, search, detail, edit, delete, notes, share link, vCard and image download | `SavedCardsScreen.js`, `SavedCardDetailScreen.js`, `utils/vcard.js`, `cardDownload.js` |
| Share link | `/share/{id}` opens a card without login. The card is public to anyone who has the link (UUID) | `SharedCardScreen.js`, `card/service.go` `GetPublicCard` |
| Browse / discovery | Working: categories, text + distance search (PostGIS), business detail, save to vault. Falls back to hard-coded sample businesses when the database or PostGIS query fails | `SearchScreen.js`, `discovery/service.go` (`getFallbackBusinesses`) |
| Free-plan limits | 5 saved cards, enforced on server and client. `free_scans_remaining` is stored but never decremented | `card/handler.go`, `ScanCardScreen.js` |
| Card tags | Working: saved with the card, read back in the vault | `card/service.go` (`tags`, `saved_card_tags`) |
| favorites, scan_records, reports, analytics_events tables | Tables exist; no code reads or writes them | `001_initial_schema.sql` |

### 9.3 Reuse in the future CRM

| Reuse as is | Reuse with changes | Do not carry over |
|---|---|---|
| Camera, crop, Tesseract OCR and the regex parser | Save target: write a CRM **Lead/Contact** in the user's business instead of (or as well as) `saved_cards` | Server "Gemini" stub |
| Card image storage | Matching: add person matching by phone/email against CRM contacts; today only GSTIN → business | Hard-coded fallback businesses |
| vCard / image export, share link | Vault ownership: today per user; in a CRM it should be per business with an owner | |

---

## 10. Mobile Application

The mobile app is the CardFlow web bundle inside a Capacitor shell (`frontend/capacitor.config.json`, `appId app.cardflow.mobile`; `frontend/android`, `frontend/ios`; Android `versionName 1.0`). Native plugins: contacts, local notifications, status bar, RevenueCat. Navigation is a hand-written state machine in `AppNavigator.js` (no router library). All state lives in `AuthContext.js`.

Bottom tabs (`components/TabBar.js`): **Home · My Cards · Scan · My Business · Browse**.

### 10.1 Screens in use

| Screen | File | Purpose | API | Data model | Reuse? |
|---|---|---|---|---|---|
| Splash | `screens/auth/SplashScreen.js` | Intro, "Get started" | none | none | Keep, rebrand |
| Login | `screens/auth/LoginScreen.js` | Enter mobile number | `POST /auth/otp/send` | `users` | Keep (target sign-in is phone + OTP) |
| OTP | `screens/auth/OtpScreen.js` | Enter code; **shows the code on screen** | `POST /auth/otp/verify` | `users` | Keep; remove the on-screen code |
| Onboarding | `screens/auth/OnboardingScreen.js` | First-time name entry | `PATCH /users/me` | `users` | Modify: add business selection/creation |
| Home | `screens/user/DashboardScreen.js` | Greeting, big Scan button, recent cards, browse link | none (uses context) | `saved_cards`, `businesses` | Modify heavily: becomes the CRM dashboard |
| My Cards | `screens/user/SavedCardsScreen.js` | Vault list, search, grid/list toggle | `GET /cards` | `saved_cards` | Keep |
| Card detail | `screens/user/SavedCardDetailScreen.js` | View/edit card, notes, images, share, export, delete | `PATCH/DELETE /cards/{id}`, `POST …/original-image` | `saved_cards` + children | Keep; add "create lead/contact" |
| Scan | `screens/user/ScanCardScreen.js` | Capture, OCR, review, save | `POST /cards`, `POST /cards/{id}/original-image` | `saved_cards` | Keep |
| My Business | `screens/user/MyBusinessHubScreen.js` | List and add my businesses | `GET/POST /owner/businesses` | `businesses` | Modify: becomes business (tenant) selection |
| Business detail | `screens/user/BusinessDetailsScreen.js` | View/edit a business, card images, save to vault | `PATCH /owner/businesses/{id}`, `POST …/card-image` | `businesses` | Modify |
| Browse | `screens/user/SearchScreen.js` | Categories and business search | `GET /categories`, `GET /businesses/search` | `businesses`, `categories` | Keep |
| Shared card | `screens/user/SharedCardScreen.js` | Public card from `/share/{id}` | `GET /public/cards/{id}` | `saved_cards` | Keep |
| Profile | `screens/user/ProfileScreen.js` | Name, change number, subscription, support, theme, notifications, privacy, terms, logout | `PATCH /users/me`, `/users/me/phone` | `users` | Keep |
| Change number | `components/ChangePhoneModal.js` | OTP to a new number | `PATCH /users/me/phone` | `users` | Keep |
| Subscription | `components/SubscriptionScreen.js` | Plans, purchase, restore, manage | RevenueCat SDK, `GET /billing/status`, `POST /billing/sync` | `users.subscription_*`, `revenuecat_events` | Modify: plans move to per business |
| Transactions | `components/TransactionHistoryScreen.js` | Payment history | `GET /billing/transactions` | `revenuecat_events` | Keep |
| Theme | `components/ThemeSettings.js`, `CardStyleModal.js` | Colours and card style (some premium-only) | none (`localStorage`) | none | Keep |
| Notifications | `components/NotificationSettings.js`, `utils/pushNotifications.js` | **Local** reminders only; no server push | none | none | Keep; add real push later |
| Support hub | `screens/user/SupportHubScreen.js` | Entry to support | `GET /support/tickets/my` | `support_tickets` | Keep |
| New request | `screens/user/SupportRequestScreen.js` | Create a ticket | `POST /support/tickets` | `support_tickets` | Keep |
| My tickets | `screens/user/SupportTicketsScreen.js` | Ticket list | `GET /support/tickets/my` | `support_tickets` | Keep |
| Ticket chat | `screens/user/SupportTicketDetailScreen.js` | Conversation with support | `GET /support/tickets/my/{id}`, `POST …/messages` | `support_ticket_messages` | Keep |
| Contacts backup | `utils/contactsSync.js` | Phone contacts backup (native only) | `/contacts/backup*` | `contact_backups` | Keep or drop: UNKNOWN — requires clarification |

There are **no CRM screens in the mobile app**: no leads, contacts, accounts, cases, tasks or dashboard.

### 10.2 Unused / dead files in the app

Nothing imports these (checked by searching every import in `frontend/src` outside `crm/`):

| File | Was |
|---|---|
| `screens/user/HomeScreen.js` (536 lines) | An older Home; replaced by `DashboardScreen.js` |
| `screens/owner/OwnerDashboardScreen.js`, `MyBusinessesScreen.js`, `AnalyticsScreen.js`, `EnquiriesScreen.js`, `QRCodeScreen.js`, `ShareCardScreen.js` | The old "owner mode"; they run on `data/mockData.js` |
| `components/Header.js`, `ScreenHeader.js`, `ConfirmModal.js`, `ContactsBackupModal.js` | Unused components |
| `context/NavigationContext.js` | Unused |

---

## 11. Web / Desktop Application

One webpack build (`frontend/webpack.config.js`, entry `src/entry.js`) produces both apps as separate lazy chunks.

| | CardFlow on the web | CRM on the web |
|---|---|---|
| URL | `/`, `/share/:id` | `/crm/*` |
| UI stack | React Native Web components, `StyleSheet`, a phone-width layout centred on desktop (`components/Layout.js`) | React DOM, Tailwind, Radix UI, TanStack Query, React Router, react-hook-form + zod, i18next, TipTap |
| State | One React context (`AuthContext.js`) + `localStorage` | Server state in TanStack Query; small UI store with zustand (`crm/lib/ui-store.ts`) |
| Shared code between the two | **None.** Different components, API clients, auth and styling | |

CRM web areas (`frontend/src/crm/app.tsx`):

- **Sign-in and account:** `/crm/login`, `/crm/signup`, `/crm/join/:token`, `/crm/owner/login`, forgot / reset / change password, accept invitation, MFA setup and verify, `/crm/me`.
- **Owner Console:** `/crm/owner/*`. See section 6.
- **Product workspace:** `/crm/w/:ws/*`, used by Super Admin, Admin, Staff and End user:
  - Home dashboard, record lists and record pages for every enabled object, page-layout editor.
  - Reports, dashboards, workflows, email campaigns.
  - Settings: users & access, email & calendar, teams, single sign-on, objects & fields, API & webhooks.
  - Business Card Snap only: App users, Businesses, ticket conversation on Cases.

There is no separate "Admin Console". Admin work inside a product is the same workspace UI with more permissions.

---

## 12. Existing CRM Features

All of this is in the CRM module and reachable only through the web at `/crm`. Status reflects code that exists and is wired end to end.

| Feature | Status | Where |
|---|---|---|
| Leads | Fully implemented | `crm.leads`, `records/spec.go`, list/detail/convert |
| Contacts | Fully implemented | `crm.contacts` |
| Accounts | Fully implemented (`kind` business / individual) | `crm.accounts` |
| Customers | No separate object. An account's `type`/`lifecycle` marks a customer | `records/spec.go` |
| Lead conversion | Fully implemented (contact, optional account and opportunity, idempotent) | `records/convert.go`, `crm.lead_conversions` |
| Opportunities / Deals | Fully implemented, pipeline stages with win probability, board view | object `opportunities` in `crm.object_records` |
| Pipeline (kanban) | Fully implemented | `features/records/list/kanban-board.tsx` |
| Cases | Fully implemented; app tickets mirrored into them with a chat | object `cases`, `connectors/cardflow/cases.go`, `tickets.go` |
| Tasks | Fully implemented | object `tasks` |
| Calendar events | Fully implemented, calendar view, Google/Outlook sync | object `events`, `records/mail.go` |
| Notes | Fully implemented, rich text, @mentions | object `notes`, `records/timeline.go` |
| Activities / timeline | Fully implemented (field history, notes, tasks, events, emails, files) | `crm.activities`, `records/timeline.go` |
| Follow-ups | Partial: `leads.next_follow_up_at` and tasks exist; no dedicated follow-up reminder flow | `0002_records.sql` |
| Communications | Fully implemented (log + send) | object `communications` |
| Products / catalog | Fully implemented as `catalog_items`; no price books, quotes or line items | object `catalog_items` |
| Subscriptions | Object exists (records only; not a billing engine) | object `subscriptions` |
| Email (mailboxes, threads, reply) | Fully implemented | `records/mail.go`, `threads.go` |
| Email campaigns | Fully implemented, 1,000 a day per product | `records/campaigns.go` |
| Reports | Fully implemented (group, count/sum/avg/min/max, bar/line/donut/number/table) | `records/reports.go` |
| Dashboards | Fully implemented; no tabs, drag grid or pie yet | `records/reports.go`, `features/reports/*` |
| Workflows / automation | Fully implemented (triggers, steps, versions, run log) | `records/workflows.go`, `workflow_api.go` |
| Custom objects and fields, page layouts | Fully implemented | `records/objects.go`, `meta.go` |
| Saved views, filters, bulk actions, import/export CSV, merge, recycle bin | Fully implemented | `records/views.go`, `filter.go`, `csvio.go`, `listing.go` |
| Public REST + GraphQL API, API keys, webhooks | Fully implemented | `records/publicapi.go`, `apikeys.go`, `webhooks.go` |
| Sales / Income | **Not implemented** | no code |
| Expenses | **Not implemented** | no code |
| Quotes, invoices, payments on deals | **Not implemented** | no code |
| Website lead form | **Not implemented** (forms module is listed as planned) | `platform/products.go` `ModuleInfo` |
| CRM on mobile | **Not implemented** | no screens in `frontend/src/screens` |

In the **CardFlow app** the only CRM-like items are the placeholder Enquiries (not saved) and support tickets.

---

## 13. Database Models

One Postgres database, two schemas. App migrations: `backend/internal/database/migrations/001…015` (embedded and re-applied at every start, `database/migrate.go` `RunMigrations`). CRM migrations: `backend/internal/crm/store/migrations/0001…0016` (applied once each, tracked in `crm.schema_migrations`).

### 13.1 `public` schema (CardFlow)

| Table | Purpose | Key fields | References | Used by code? |
|---|---|---|---|---|
| `users` | App accounts | `id`, `phone` unique, `role`, `plan`, subscription fields | none | Yes |
| `user_kyc` | Aadhaar/PAN status | `user_id` PK | `users` | Read once at sign-in; nothing writes it |
| `devices` | Device per user | `user_id`, `device_id`, `push_token` | `users` | Written at sign-in |
| `categories` | Business categories (tree) | `parent_id`, `slug` | self | Yes |
| `businesses` | Listings and unclaimed card businesses | `owner_user_id` (nullable), `gstin`, `source`, `contact_phone`, `location` | `users`, `categories` | Yes |
| `business_categories`, `business_services`, `business_phones`, `business_media`, `business_verifications`, `digital_cards` | Business details | `business_id` | `businesses` | services and phones yes; a `digital_cards` row is inserted when a business is created (`business/service.go`) but never read; the rest are not written |
| `business_card_images` | Card photo per business and side (BYTEA) | `business_id`, `side` | `businesses` | Yes |
| `saved_cards` | A user's card vault | `user_id`, `person_name`, `company`, `gstin`, `linked_business_id`, `source` | `users`, `businesses` | Yes |
| `saved_card_phones`, `saved_card_emails`, `saved_card_addresses`, `saved_card_images` | Card details and images | `saved_card_id` | `saved_cards` | Yes |
| `tags`, `saved_card_tags` | Tags on saved cards | `user_id`, `saved_card_id` | `users`, `saved_cards` | Yes (`card/service.go`) |
| `favorites`, `scan_records`, `reports`, `analytics_events` | Planned features | | `users` | **No code uses them** |
| `enquiries` | Enquiry to a business | `business_id`, `user_id` | | **No code reads or writes it** |
| `subscriptions`, `purchases`, `credit_ledger` | Original billing design | `user_id` | `users` | Only `credit_ledger` signup bonus is written |
| `subscription_payments` | Razorpay payments (legacy) | `razorpay_order_id` | `users` | Read only, for old payment history (`billing/handler.go` `GetTransactions`, CRM `appdata.go`). Razorpay itself was replaced by RevenueCat |
| `revenuecat_events` | Billing webhook events | `event_id` PK, `user_id` | `users` | Yes |
| `contact_backups` | Phone contacts backup | `user_id`, `phones`, `emails` (JSON) | `users` | Yes (native only) |
| `support_tickets` | Tickets | `id` (text), `user_id`, `status`, `admin_reply` | `users` | Yes |
| `support_ticket_messages` | Ticket conversation | `ticket_id`, `sender`, `author_name`, `author_role` | `support_tickets` | Yes |
| `audit_logs` | Old app admin audit | `admin_id` | `users` | No (admin console removed) |

### 13.2 `crm` schema

| Group | Tables | Tenant key |
|---|---|---|
| Identity | `identities`, `verified_identifiers`, `password_credentials`, `mfa_methods`, `sso_links`, `sessions`, `otp_challenges`, `password_resets` | global (a person can belong to several workspaces) |
| Tenancy | `workspaces`, `products`, `product_versions`, `workspace_products` | `workspaces.id` |
| Access | `memberships`, `roles`, `role_assignments`, `permission_sets`, `membership_permission_sets`, `teams`, `team_members`, `branches`, `invitations`, `invite_links`, `sso_providers`, `api_keys` | `workspace_id` |
| Core records | `leads`, `accounts`, `contacts`, `lead_conversions`, `code_counters` | `workspace_id` |
| Generic records | `object_definitions` (optional `workspace_id` for a product's own objects), `object_records`, `field_definitions`, `layouts`, `views`, `favorites`, `files` | `workspace_id` |
| Activity | `activities`, `notifications`, `messages`, `message_links`, `mail_accounts`, `mail_blocklist` | `workspace_id` |
| Automation | `workflows`, `workflow_versions`, `workflow_runs`, `assignment_state`, `webhooks`, `webhook_deliveries`, `campaigns`, `campaign_recipients`, `unsubscribes` | `workspace_id` |
| Analytics | `reports`, `dashboards` | `workspace_id` |
| Integration | `external_links`, `connector_state` | `workspace_id` |
| Platform | `audit_events`, `outbox_events`, `idempotency_keys`, `schema_migrations` | mixed |

Common columns on record tables: `id` UUID, `workspace_id`, `code` (human ID like `L-000123`), `owner_id`, `custom` JSONB (custom-field values), `version` (optimistic concurrency), `deleted_at` (recycle bin), `created_by`, `updated_by`, `created_at`, `updated_at`.

Standard objects stored in `object_records` (`records/objects.go`): `opportunities`, `tasks`, `events`, `notes`, `communications`, `subscriptions`, `catalog_items`, `cases`.

---

## 14. Entity Relationships

Generated from the migrations, not assumed.

### 14.1 CardFlow (`public`)

```text
users ──────────────┬──< saved_cards ──< saved_card_phones / _emails / _addresses / _images
  │                 │        │
  │                 │        └── linked_business_id ──► businesses   (same GSTIN)
  │                 │
  ├──< businesses (owner_user_id, may be NULL = unclaimed card business)
  │        ├──< business_phones, business_services, business_card_images
  │        └── primary_category_id ──► categories
  │
  ├──< support_tickets ──< support_ticket_messages
  ├──< devices
  ├──< contact_backups
  ├──< revenuecat_events
  └──1 user_kyc
```

### 14.2 CRM (`crm`)

```text
identities ──< verified_identifiers
    │      ──< sessions
    │
    └──< memberships >── workspaces  ("Product" = the tenant)
              │               │
              │               ├──< workspace_products >── products ("App") ──< product_versions
              │               ├──< roles (tree via parent_role_id)
              │               ├──< permission_sets
              │               │
              │               ├──< leads ──(convert)──► contacts ──► accounts
              │               │      └──< lead_conversions
              │               ├──< object_records  (opportunities, tasks, events, notes,
              │               │                     communications, cases, catalog_items,
              │               │                     subscriptions, custom objects)
              │               ├──< activities, files, views, reports, dashboards
              │               └──< workflows, webhooks, campaigns, mail_accounts, messages
              │
              ├──< role_assignments ──► roles          (+ product_ids[] = apps they may use)
              └──< membership_permission_sets ──► permission_sets
```

### 14.3 The bridge between the two

```text
public.users.id ───────────► crm.external_links (external_type 'user')     ──► lead, contact, [account]
public.businesses.id ──────► crm.external_links (external_type 'business') ──► lead (unclaimed) or account + contact
public.support_tickets.id ─► crm.object_records (cases, custom.app_ticket_id)
```

All of it lives in one workspace, code `business-card-snap`, created by `connectors/cardflow/cardflow.go` `bootstrap()`. In other words, **today the whole CardFlow app is one tenant of the CRM**, and its users are that tenant's *contacts*, not its *users*.

---

## 15. API Inventory

Response envelope for `/api/v1`: `{ "status": "success", "data": … }` or `{ "status": "error", "error": { "code", "message" } }` (`backend/pkg/response`). The CRM returns plain JSON and its own error shape (`crm/shared/errors.go`).

### 15.1 CardFlow `/api/v1` (complete list, from `backend/cmd/api/main.go`)

| Method | Endpoint | Purpose | Auth | Role | Request | Response |
|---|---|---|---|---|---|---|
| **Authentication** | | | | | | |
| POST | `/auth/otp/send` | Create an OTP | none | any | `phone`, `device_id`, `platform` | `success`, `otp_preview` (the code) |
| POST | `/auth/otp/verify` | Sign in / sign up | none | any | `phone`, `otp_code`, `device_id`, `platform`, `push_token` | `access_token`, `refresh_token`, `user`, `is_new_user`, `claimed_businesses`, `suggested_name` |
| POST | `/auth/refresh` | Placeholder | none | any | `refresh_token` | fake token strings |
| POST | `/auth/logout-all` | Placeholder | none | any | none | message |
| **Users** | | | | | | |
| GET | `/users/me` | Current user | JWT | user | none | `User` |
| PATCH | `/users/me` | Update profile | JWT | user | name, email, city, state | `User` |
| PATCH | `/users/me/phone` | Change number | JWT | user | `phone`, `otp_code` | `User` |
| DELETE | `/users/me` | Placeholder | JWT | user | none | message |
| GET | `/users/me/export` | Placeholder | JWT | user | none | fixed shell |
| **Cards** | | | | | | |
| GET | `/cards` | My vault | JWT | user | none | `SavedCard[]` |
| POST | `/cards` | Save a card (links/creates business by GSTIN) | JWT | user | `SavedCard` | `SavedCard`; 402 over the free limit |
| PATCH | `/cards/{id}` | Edit a card | JWT | owner of card | partial `SavedCard` | `SavedCard` |
| DELETE | `/cards/{id}` | Delete a card | JWT | owner of card | none | message |
| GET / POST | `/cards/{id}/original-image` | Read / upload card photo | JWT | owner of card | `image_data` (data URL), `side` | image bytes / message |
| POST | `/cards/upload-url` | S3 presigned upload | JWT | user | kind, ext | URL (only when S3 is on) |
| POST | `/cards/scan` | Server OCR **placeholder** (registered both public and protected) | none / JWT | any | `image_object_key` | empty extraction |
| GET | `/public/cards/{id}` | Shared card | none | any | none | limited `SavedCard` |
| GET | `/public/cards/{id}/original-image` | Shared card photo | none | any | none | image |
| **Businesses** | | | | | | |
| GET | `/categories` | Category list | none | any | none | `Category[]` |
| GET | `/businesses/search` | Search by text, category, distance | none | any | `q`, `category_id`, `lat`, `lng`, `radius_km`, `limit`, `offset` | `Business[]` |
| GET | `/businesses/{id}`, `/businesses/slug/{slug}` | One business | none | any | none | `Business` |
| GET | `/businesses/{id}/card-image` | Business card photo | JWT | user | `side` | image |
| GET | `/owner/businesses` | My businesses | JWT | user | none | `Business[]` |
| POST | `/owner/businesses` | Create a business | JWT | user | business fields | `Business` |
| PATCH | `/owner/businesses/{id}` | Edit | JWT | owner of business | business fields | `Business` |
| POST / GET | `/owner/businesses/{id}/card-image` | Card photo | JWT | owner | `side`, data URL | message / image |
| GET | `/owner/businesses/{id}/analytics` | **Hard-coded numbers** | JWT | owner | none | fixed sample data |
| POST | `/owner/businesses/{id}/verify/gst` | **Always "verified"** | JWT | user | `gstin` | fixed response |
| GET | `/owner/businesses/{id}/card` | **Hard-coded** digital card | JWT | user | none | fixed response |
| GET | `/owner/businesses/{id}/enquiries` | Placeholder | JWT | owner | none | not from the database |
| **Support** | | | | | | |
| POST | `/support/tickets` | Create a ticket | JWT | user | `category`, `subject`, `message` | `ticket` with `messages` |
| GET | `/support/tickets/my` | My tickets | JWT | user | none | `tickets[]` |
| GET | `/support/tickets/my/{id}` | One ticket with conversation | JWT | ticket owner | none | `ticket` |
| POST | `/support/tickets/my/{id}/messages` | Write again (reopens) | JWT | ticket owner | `message` | `ticket` |
| **Billing** | | | | | | |
| GET | `/billing/status` | Premium status | JWT | user | none | status |
| POST | `/billing/sync` | Re-read RevenueCat | JWT | user | none | status |
| GET | `/billing/transactions` | History | JWT | user | none | list |
| GET | `/billing/credits` | **Hard-coded** credits | JWT | user | none | fixed sample balance and history |
| POST | `/api/webhooks/revenuecat` | RevenueCat events | shared secret header | RevenueCat | event | 200 |
| **Other** | | | | | | |
| POST | `/enquiries` | **Not saved** | JWT | user | `business_id`, `message` | echo |
| POST / GET | `/contacts/backup`, `/contacts/backup/status` | Phone contacts backup | JWT | user | contacts | status / list |
| GET | `/b/{slug}` | Public HTML business page | none | any | none | HTML |
| GET | `/health` | Health | none | any | none | status |

`/api/v1/admin/*` no longer exists (removed 1 Oct 2026).

### 15.2 CRM `/api/crm/v1` (grouped; all routes are in the files named)

Auth for every non-GET: session cookie **and** `X-CSRF-Token`. API keys (`Authorization: Bearer crm_…`) work on `/w/{code}/crm/*` when the app setup allows API access.

| Group | Endpoints | Role | Defined in |
|---|---|---|---|
| Authentication | `/auth/csrf`, `/auth/login`, `/auth/methods`, `/auth/otp/request`, `/auth/otp/verify`, `/auth/oauth/{provider}/start`, `/auth/password/{forgot,reset,change}`, `/auth/signup/{request,verify}`, `/auth/join/{token}`, `/auth/mfa/{verify,enroll,confirm}`, `/auth/logout`, `/auth/logout-all`, `/auth/saml/{code}/*`, `/auth/sso/{code}/start`, `/auth/oidc/{code}/callback`, `/invitations/{preview,accept}`, `/me`, `/me/sessions`, `/capabilities` | public or session | `identity/handlers.go`, `saml.go` |
| Products (tenants) | `/platform/workspaces` (list, create, get, patch), `…/{id}/products`, `…/{id}/invitations` (+ resend, revoke) | owner | `platform/platform.go` |
| Apps (setups) | `/platform/products` (list, create, get, patch, publish, archive, restore), `/platform/apps` | owner | `platform/platform.go` |
| Users | `/platform/users` (list, get, create, patch), `…/password-reset`, `…/revoke-sessions`, `…/reset-mfa`, `…/memberships` | owner | `platform/platform.go` |
| Access | `/platform/access/{catalog,workspaces}`, `/platform/workspaces/{id}/{roles,permission-sets,fields}`, `/platform/roles/{id}`, `/platform/permission-sets/{id}` | owner | `platform/platform.go`, `records/objects.go` |
| Delegated admin | `/w/{code}/admin/{options,members,roles,permission-sets}` | member with `members.manage` / `access.manage` | `platform/admin.go` |
| Leads, Contacts, Accounts, every object | `/{prefix}/crm/{object}` list/create, `/{id}` get/patch/delete/restore, `/groups`, `/bulk`, `/export`, `/import`, `/merge`, `/views`, `/{id}/{timeline,notes,files,duplicates,email,emails}`, `/crm/leads/{id}/convert`, `/crm/meta/{object}` (layout, fields). `{prefix}` is `/platform` (owner) or `/w/{code}` (member) | owner or member with the permission | `records/handlers.go` |
| Objects | `/platform/objects`, `/w/{code}/objects` | owner / member with customize | `records/objects.go` |
| Cases / Support | Cases use the generic record routes; `/w/{code}/app/tickets/{id}` and `…/messages` (conversation); `/w/{code}/support/tickets*` (older) | member with `cases` | `connectors/cardflow/tickets.go`, `api.go` |
| Connected app data | `/w/{code}/app/{users,businesses,categories}` | member with `app_user` / `app_business` | `connectors/cardflow/appdata.go` |
| Reports | `/w/{code}/reports*`, `/dashboards*`, `/w/{code}/dashboard` | member | `records/reports.go` |
| Automation | `/w/{code}/workflows*`, `/campaigns*`, `/hooks/workflows/{token}`, `/public/unsubscribe/{token}` | member with the capability / public | `records/handlers.go` |
| Email | `/w/{code}/mailboxes*` | member | `records/handlers.go` |
| Developer | `/w/{code}/developer/{api-keys,webhooks,openapi.json}`, `/w/{code}/graphql` | member with developer capability | `records/handlers.go` |
| Settings | `/w/{code}/{sso,invite-link,recycle-bin,teams}`, `/context`, `/search`, `/lookup/{target}`, `/favorites`, `/notifications`, `/stream` (SSE) | member | `records/handlers.go` |
| Integrations | `/platform/integrations`, `/platform/integrations/cardflow/sync`, `/oauth/providers`, `/oauth/{provider}/callback` | owner / public callback | `connectors/cardflow/api.go`, `crm.go` |
| Platform | `/platform/dashboard`, `/platform/audit`, `/platform/email`, `/platform/email/test`, `/healthz` | owner | `platform/platform.go` |

---

## 16. Permissions

### 16.1 CardFlow app

| Check | Where | Notes |
|---|---|---|
| Signed in | Backend, `middleware/auth.go` `Authenticate` | Bearer JWT |
| "This is my card" | Backend, every card query filters `user_id` (`card/service.go`, `CardBelongsToUser`) | |
| "This is my business" | Backend, `business/service.go` `VerifyOwnerAccess` | |
| "This is my ticket" | Backend, `support/handler.go` `ownTicket` | |
| Premium features | Backend for the 5-card limit (`card/handler.go`); frontend only for themes | |
| Roles | **None enforced.** `user` and `admin` behave the same | |

Authorization is on the backend and is ownership-based. There is no role or permission system in the app.

### 16.2 CRM

Both backend and frontend, with the backend as the authority. Every request recomputes access from the database (no caching, DECISIONS D-31).

| Role | Can do |
|---|---|
| **Platform owner** | Everything in the Owner Console; can enter any product with full access (audited) |
| **Super Admin** | Everything inside their product, every app in it; permissions cannot be edited |
| **Admin** | Whatever their permission sets grant (default set "Admin access"); sees own records plus roles below |
| **Staff** | Whatever their permission sets grant (default set "Staff access") |
| **End user** | Nothing by default; exactly what added permission sets grant |
| **Custom roles** | A place in the role tree; permissions come from permission sets |

A permission set (`access/roles.go`, `Rules`) holds: objects × actions (`read`, `create`, `update`, `delete`, `convert`, `import`, `export`, `destroy`), row scope per object (`own` or `workspace`), field access (read-only or hidden), and capabilities (dashboard, customize, manage users, manage roles, workflows, developer, send email, campaigns).

Order of checks for one request (`records/scope.go`, `access/effective.go`): session and MFA → active membership in the workspace → allowed sign-in method → an app the member has switches the object on → a permission set allows the action → the row is in scope (own + roles below, or whole workspace) → field access.

Delegated admins can never grant more than they hold (`access.Exceeds`, D-35).

**Weak point:** tenant isolation depends on every query including `workspace_id`. There is no database-level row security.

---

## 17. Integrations

| Integration | Used for | Credentials (setting names) | Files | Really called? | Reuse for CRM |
|---|---|---|---|---|---|
| OTP by SMS | App sign-in | `SMS_PROVIDER`, `SMS_AUTH_KEY`, `SMS_SENDER_ID`, `SMS_OTP_TEMPLATE_ID` | `config/config.go` only | **No.** No SMS code exists | Must be built; required for phone login |
| Email (Brevo HTTPS or SMTP) | CRM invitations, codes, resets, record email, campaigns | `CRM_BREVO_API_KEY` or `CRM_SMTP_*` | `crm/mail/mail.go` | Yes | Yes |
| Google OAuth | CRM sign-in; Gmail and Calendar sync | `CRM_GOOGLE_CLIENT_ID/_SECRET` | `crm/oauth/oauth.go`, `records/mail.go` | Code is complete; whether keys are set on live is UNKNOWN — requires clarification | Yes |
| Microsoft OAuth | CRM sign-in; Outlook mail and calendar | `CRM_MICROSOFT_CLIENT_ID/_SECRET/_TENANT` | same | same | Yes |
| LinkedIn OAuth | CRM sign-in | `CRM_LINKEDIN_CLIENT_ID/_SECRET` | `crm/oauth/oauth.go` | same | Yes |
| SAML / OIDC | Per-product SSO | stored encrypted per product (`crm.sso_providers`) | `crm/saml.go`, `records/sso.go`, `records/oidc.go` | Yes | Yes |
| IMAP | Mailbox sync | per mailbox, encrypted | `records/mail.go` | Yes | Yes |
| RevenueCat | App subscriptions (iOS, Android, web) | server: `REVENUECAT_SECRET_API_KEY`, `REVENUECAT_WEBHOOK_AUTH`, `REVENUECAT_ENTITLEMENT_ID`; client: `REVENUECAT_{IOS,ANDROID,WEB}_API_KEY` | `backend/internal/billing/*`, `frontend/src/services/subscription/*` | Yes | Yes, but entitlement is per user today |
| OCR | Card reading | none | `frontend/src/utils/ocrParser.js` (tesseract.js) | Yes, in the browser | Yes |
| Gemini / Vertex AI | Intended server OCR | `GCP_PROJECT_ID`, `GCP_LOCATION`, `GCP_SERVICE_ACCOUNT_JSON`, `GEMINI_MODEL_ID`, `DEV_MOCK_GEMINI` | `extractor/gemini.go` | **No.** Stub | Optional upgrade |
| S3-compatible storage | Card images | `S3_*` | `storage/s3.go` | Only if `S3_ENDPOINT` is not localhost; otherwise images are stored in Postgres | Yes |
| CRM files | Record attachments | none | `records/timeline.go` (10 MB each, 250 MB per product, in Postgres) | Yes | Yes |
| Redis | OTP store | `REDIS_URL` or `REDIS_*` | `auth/service.go`, `database` | Optional | Yes |
| Maps | "Open in maps" links | none | `SearchScreen.js`, `BusinessDetailsScreen.js`, `SavedCardDetailScreen.js` (plain `maps.google.com` links) | Links only; `MOBILE_GOOGLE_MAPS_API_KEY_*` unused | n/a |
| PostGIS | Distance search | database extension | `discovery/service.go` | Yes, with a non-PostGIS fallback (`006/007` bootstrap migrations) | Yes |
| Push notifications | Reminders | `FCM_SERVER_KEY` (unused) | `frontend/src/utils/pushNotifications.js` (Capacitor **local** notifications) | Local only; no server push | Must be built |
| KYC | Aadhaar / PAN / GST checks | `KYC_*`, `DEV_MOCK_KYC` | `config/config.go` only; `internal/kyc/` is empty | **No** | Build if needed |
| Phone contacts | Backup | none | `utils/contactsSync.js`, `contacts/handler.go` | Yes (native) | Optional |
| Google Play / Apple keys, Sentry | not used | `GOOGLE_PLAY_*`, `APPLE_*`, `MOBILE_SENTRY_DSN` | `config/config.go`, `.env.example` | **No** | n/a |
| Razorpay | Old payments | none left | table `subscription_payments` only | Removed | Drop |

---

## 18. Current User Flows

### 18.1 App user (as built)

```text
Splash → enter mobile number → OTP (shown on screen) → [first time: enter name]
   ↓
Home ── Scan ──► capture → OCR in browser → review → save ──► My Cards (vault, private to the user)
   │                                           │
   │                                           └─ card has a GSTIN → linked to / creates a business record
   ├── My Cards ──► card detail → edit, share link, export, delete
   ├── My Business ──► add / edit my businesses (listing)
   ├── Browse ──► categories, search → business detail → save to My Cards
   └── Profile ──► subscription (RevenueCat), support (ticket chat), theme, logout
```

### 18.2 What the CRM sees of that (automatic, in the background)

```text
App sign-up ─────────► Lead (App sign-up) → converted at once → Contact
User adds a business ► Account (business) linked to that contact
Card with new GSTIN ─► unclaimed business → Lead (card scan), not converted
Card person signs in ► business claimed → lead converted → Account + Contact
Support ticket ──────► Case, with a two-way conversation
```

### 18.3 Owner and product users

```text
Owner signs in (/crm/owner/login, MFA)
   ↓ Add new product (Details → Super Admin → Review)
   ↓ Product → Apps tab → New app → setup wizard → Publish
Super Admin accepts the invitation → /crm/w/<code>
   ↓ invites Admin / Staff / End users; assigns role + permission sets + apps
People work leads → convert → contacts / accounts / opportunities, cases, tasks, reports, workflows
```

---

## 19. Technical Debt

Severity: **High** = blocks a safe production launch or the transformation; **Medium** = should be fixed during it; **Low** = cleanup.

### Security

| # | Issue | Severity | Evidence |
|---|---|---|---|
| S1 | OTP is returned to the caller and shown on screen; no SMS is sent. Anyone can sign in as any phone number | High | `auth/service.go` `SendOTP` (`otp_preview`), `OtpScreen.js` |
| S2 | No rate limit or attempt limit on OTP send/verify; in-memory codes never expire | High | `auth/service.go` `RequestOTP`, `validateOTP` |
| S3 | OTP stored in Redis in plain text next to its hash; hash uses a fixed salt | Medium | `auth/service.go` (`"code": otpCode`, `cf_salt_2026`) |
| S4 | JWT signed with a shared string (HS256); a default value is in the source, so the live value must be long and random (the live value is not readable from the repo) | High | `config/config.go` `JWT_PRIVATE_KEY` default, `auth/jwt.go` |
| S5 | No working refresh or revocation; logout is client-only | Medium | `auth/handler.go` `RefreshToken`, `LogoutAll` |
| S6 | CORS allows every origin; the `ALLOWED_ORIGINS` setting is ignored | Medium | `cmd/api/main.go` (`AllowedOrigins: []string{"*"}`) |
| S7 | Token and user kept in `localStorage` | Medium | `AuthContext.js` |
| S8 | `ENV` is `development` on the live server per its settings (`render.yaml` sets production, the dashboard value differs) | Medium | `render.yaml`; live value reported 30 Sep 2026 |
| S9 | Tenant isolation relies on application code only; no row-level security | Medium | `crm/store/migrations/*` (none), D-19 |
| S10 | A shared card link exposes a saved card to anyone with the ID | Low (by design) | `card/service.go` `GetPublicCard` |
| S11 | `POST /cards/scan` is public | Low | `cmd/api/main.go` |

### Placeholders presented as features

| # | Issue | Evidence |
|---|---|---|
| P1 | Server OCR returns empty data | `extractor/gemini.go` |
| P2 | GST verification always returns "verified" | `business/handler.go` `VerifyGST` |
| P3 | Business analytics are fixed numbers | `business/handler.go` `GetBusinessAnalytics` |
| P4 | Digital card is a fixed response | `business/handler.go` `GetDigitalCard` |
| P5 | Enquiries are never saved; the app never calls them | `enquiry/handler.go` (no SQL) |
| P6 | Delete account and export data do nothing | `auth/handler.go` |
| P7 | Search silently falls back to hard-coded sample businesses | `discovery/service.go` `getFallbackBusinesses` |
| P8 | Credits balance and history are fixed sample values | `billing/handler.go` `GetCredits` |

### Structure and duplication

| # | Issue | Evidence |
|---|---|---|
| D1 | Two user systems, two auth systems, two API styles, two UI stacks in one repo | sections 4, 5, 11 |
| D2 | Naming: UI "Product" = code `workspace`; UI "App" = code `product` | section 7 |
| D3 | `AuthContext.js` (761 lines) holds auth, vault, businesses, billing and UI state together | `frontend/src/context/AuthContext.js` |
| D4 | The app has no router; navigation is nested state in one component | `navigation/AppNavigator.js` |
| D5 | The app's API client swallows errors and returns `null` or `[]`, so failures look like empty data | `frontend/src/services/api.js` |
| D6 | Dead screens and components (section 10.2) and `data/mockData.js` still feeding live code paths (`AuthContext.loadMyBusinesses` fallback, `SearchScreen` fallback categories) | section 10.2 |
| D7 | The built web bundle is committed to git (`backend/cmd/api/dist`, allowed by an exception in `.gitignore`); every change needs a second "refresh bundle" commit | `.gitignore`, `HANDOVER.md` section 7 |
| D8 | Duplicate migrations folder `backend/migrations` not used by the code | repo tree |
| D9 | App migrations re-run in full on every start and log failures as warnings | `database/migrate.go` |
| D10 | Unused tables (favorites, scan_records, reports, analytics_events, enquiries, subscriptions, purchases, audit_logs, business_media, business_verifications) | section 13.1 |
| D11 | Unused config (SMS, KYC, FCM, Gemini, Apple, Google Play, `DATA_ENCRYPTION_KEY`, `JWT_PUBLIC_KEY`, `DEV_MOCK_*`) | section 17 |
| D12 | Services insert a fake user with phone `+910000000000` "to satisfy the foreign key" | `card/service.go`, `business/service.go` |
| D13 | Hard-coded defaults: city Coimbatore, state Tamil Nadu, name "CardFlow User" | `auth/service.go` `resolveUser`, `AuthContext.js` |
| D14 | `docs/*.md` describe the pre-CRM design and are out of date | `docs/` (31 Aug 2026) |
| D16 | Two GitHub repos; only one deploys | `HANDOVER.md` section 3 |
| D17 | The CRM connector reads the app's tables directly and syncs on demand, so the CRM copy can lag until someone opens a CRM page | `connectors/cardflow/cardflow.go` `SyncOnDemand` |

### Worth preserving

The CRM record engine, access model, identity/session code, workflow engine, mail, and the card capture + OCR pipeline are well structured and tested (`go test ./internal/crm/...`, `crm/access/access_test.go`, `records/features_test.go`).

---

## 20. Keep / Modify / Remove / New

### KEEP

| What | Why |
|---|---|
| CRM record engine, objects, layouts, views, filters (`crm/records`) | Already a working multi-tenant CRM core |
| CRM access model: role tree + permission sets (`crm/access`) | Matches the Salesforce-style target |
| CRM identity: sessions, MFA, OAuth, SAML/OIDC, invitations (`crm/identity`) | Owner Console sign-in stays email + password or Google |
| Reports, dashboards, workflows, email, campaigns, API keys, webhooks | Already built |
| Owner Console screens | Becomes the platform administration |
| Card capture, crop, OCR and parser (`ScanCardScreen.js`, `ocrParser.js`) | Card scanning becomes a CRM feature |
| My Cards and Browse | Stay as tabs in the target navigation |
| RevenueCat billing | Subscriptions continue |
| Support ticket conversation ↔ Cases | Already CRM-shaped |
| App UI kit (`components/Button, Card, Input, Badge, TabBar, Layout`, `theme/`) | Mobile look and feel |

### MODIFY

| What | Change needed |
|---|---|
| App sign-in | Real SMS; no code in responses; limits; proper refresh and revocation; issue a CRM identity/session instead of a separate JWT |
| User model | Merge `public.users` into `crm.identities` (phone as a `verified_identifier`). One person, one login, across app and CRM |
| "My Business" | From "a directory listing I own" to "the CRM business (tenant) I work in", with selection and creation after login |
| Workspace creation | Today owner-only (`handleProvisionWorkspace`). Customers must be able to create their own business, pick a plan, and become its Super Admin |
| Product/App assignment | Today the owner installs apps. Target: a default CRM setup applied automatically on self-serve creation; owner manages plans |
| Card save | Write a Lead/Contact in the user's business (and keep the vault) |
| Home screen | Add quick actions (Scan a card, Add a lead, Add a contact) and CRM counts |
| Billing | From per-user premium to per-business plan |
| Session transport for mobile | The CRM uses cookies + CSRF, which is awkward in a native shell. Add a token session for the mobile client |
| CRM email code | `identity/otp.go` accepts email only; extend to phone (`otp_challenges.channel` already allows `sms`) |
| Naming | Align UI and code words: Business (tenant), Plan/Setup, App |
| Tenant isolation | Add row-level security or a mandatory tenant transaction helper |
| CardFlow → CRM connector | Becomes unnecessary once the app writes CRM records directly; keep only as a one-time migration |

### REMOVE

| What | Why |
|---|---|
| OTP preview in API and UI | Security |
| Placeholder endpoints: GST verify, analytics, digital card, credits, enquiries, `/auth/refresh`, `/auth/logout-all`, `DELETE /users/me`, `/users/me/export` (or implement them) | They report success without doing anything |
| Server "Gemini" stub and `/cards/scan` | Not used |
| Dead screens (`screens/owner/*`, `HomeScreen.js`), unused components, `mockData.js` fallbacks | Dead code |
| Unused tables and config (section 19, D10–D11) | Noise |
| `users.role = 'admin'` and frontend "owner mode" | Replaced by CRM roles |
| `backend/migrations` (duplicate, tracked in git) and the stale `docs/` | Clutter |
| Hard-coded fallback businesses and Coimbatore defaults | Wrong data in production |

### NEW

| What | Notes |
|---|---|
| CRM screens in the mobile app: leads, contacts, accounts, cases, tasks, follow-ups, opportunity pipeline, dashboard | None exist. The CRM UI is React DOM; the app is React Native Web |
| Self-serve business creation and business switcher | New API and screens |
| SMS OTP delivery | No provider integration exists |
| Income and expenses | No code exists. Can be standard objects (`object_definitions`) plus report measures |
| Customers as a first-class view | Today only an account type |
| Plans and subscription per business | New |
| Server push notifications | Only local notifications exist |
| Person matching when a card is scanned | Only GSTIN → business exists |
| Website lead capture form | Not built |
| Data migration from `public.*` to `crm.*` | One-time |

---

## 21. Recommended CRM Transformation Architecture

A recommendation, not a decision. It follows from what already exists.

```text
                         ONE IDENTITY  (crm.identities)
        phone + OTP (customers)      email + password / Google (owner, staff)
                                   │
                    ┌──────────────┴───────────────┐
                    ▼                              ▼
        Customer app (mobile + web)         Owner Console (web)
        Home · My CRM · My Cards · Browse   platform administration
                    │                              │
                    └──────────────┬───────────────┘
                                   ▼
                       /api/crm/v1  (one API)
                                   │
              Business = crm.workspaces  (tenant, complete isolation)
              ├── members, roles, permission sets
              ├── leads, contacts, accounts, opportunities, cases, tasks
              ├── income, expenses            (new objects)
              ├── saved cards                 (moved in, owned by the business + a person)
              └── reports and dashboards
```

Suggested order:

1. **Fix app sign-in first** (S1–S5). It is unsafe today regardless of the transformation.
2. **Unify identity.** Add phone + OTP to the CRM identity service (`identity/otp.go`, `verified_identifiers.kind='phone'` already exists). Give the mobile client a token session. Migrate `public.users` to `crm.identities`.
3. **Self-serve business.** A signed-in person with no membership creates a business: a new `crm.workspaces` row, a default app setup, themselves as Super Admin. Reuse the logic in `platform/workspaces.go` behind a new customer-facing endpoint with limits.
4. **Business selection.** After login: choose or create a business (`sessions.selected_membership_id` already exists for this).
5. **Mobile "My CRM".** Build lead, contact, account, case and task screens on the existing `/w/{code}/crm/{object}` API, which is already metadata-driven (`/crm/meta/{object}` returns fields and layouts).
6. **Cards as a CRM feature.** Save a scan as a Lead or Contact in the active business; keep `My Cards` as the personal vault view.
7. **Income and expenses** as standard objects with report measures; add them to the Home dashboard.
8. **Retire the bridge.** Move Business Card Snap's own users from "contacts of one tenant" to real identities; keep `connectors/cardflow` only for the migration.
9. **Harden isolation** with row-level security before many businesses share the database.
10. **Owner Console**: add Plans and Subscriptions; keep Products (renamed Businesses), Apps, Users & access, Integrations.

Biggest structural decision: the customer app is React Native Web and the CRM is React DOM + Tailwind. Either build the CRM screens again in the app's stack, or make the responsive CRM web UI the customer app inside the Capacitor shell and move card scanning into it. This needs a decision before step 5 (see section 22).

---

## 22. Open Questions / Unknowns

| # | Question | Why it matters |
|---|---|---|
| 1 | Which UI stack does the customer app use going forward (React Native Web, or the CRM's React DOM + Tailwind in the Capacitor shell)? | Decides how much of the CRM UI is rebuilt |
| 2 | Which SMS provider and sender ID? `UNKNOWN — requires clarification` | Phone login cannot go live without it |
| 3 | Is a user allowed to belong to several businesses, and to own more than one? | Data model and business switcher |
| 4 | What happens to existing app users, their saved cards and their businesses at migration? Does each existing business listing become a CRM tenant, or stay a directory listing? | Migration design |
| 5 | Is Browse (public business directory) staying a public, cross-tenant feature? | It is the one thing that deliberately crosses tenants |
| 6 | Are saved cards private to a person or shared with the whole business? | Ownership and permissions |
| 7 | Plans: what is free, what is paid, per user or per business? | Billing redesign |
| 8 | What do "Income" and "Expenses" need (categories, tax, attachments, link to deals)? | New objects |
| 9 | Are "Customers" a separate object or accounts/contacts with a customer status? | Data model |
| 10 | Which GitHub repo is the main one? Live deploys come from `Ajaykrishnancreations/CardFlow` only | Process |
| 11 | Live configuration: are Redis, S3, Google/Microsoft/LinkedIn keys and the RevenueCat public keys set on Render/Vercel? `UNKNOWN — requires clarification` (not readable from the repo) | Which integrations actually work on live |
| 12 | Are the Android and iOS apps published in the stores, and at what version? `UNKNOWN — requires clarification` | Release and migration timing |
| 13 | Should phone-contacts backup stay? | Scope |
| 14 | Is KYC (Aadhaar, PAN, GST verification) still wanted? | It is entirely unbuilt |
| 15 | Row counts and data quality in the live database. `UNKNOWN — requires clarification` (not inspected) | Migration effort |
| 16 | The CRM PRD ("Ajay's CRM Core", 58 pages) referenced by `DECISIONS.md` is not in the repo | Source of truth for CRM scope |

---

## 23. Files and Components Relevant to the Migration

### Sign-in and identity

| File | Relevant items |
|---|---|
| `backend/internal/auth/service.go` | `RequestOTP`, `SendOTP`, `VerifyOTP`, `validateOTP`, `resolveUser`, `claimCardBusinesses` |
| `backend/internal/auth/jwt.go` | `GenerateTokenPair`, `ValidateAccessToken`, `Claims` |
| `backend/internal/auth/handler.go` | `RefreshToken`, `LogoutAll`, `DeleteMe`, `ExportMe` (placeholders) |
| `backend/internal/middleware/auth.go` | `Authenticate` |
| `backend/internal/crm/identity/service.go` | `Login`, `finishSignIn`, `VerifyMFA`, `Logout` |
| `backend/internal/crm/identity/otp.go` | `RequestOTP`, `VerifyOTP` (email only today) |
| `backend/internal/crm/identity/sessions.go` | `createSession`, `lookupSession`, cookie names and lifetimes |
| `backend/internal/crm/identity/signup.go` | `RequestSignup`, `CompleteSignup`, `joinByLink` |
| `backend/internal/crm/identity/normalize.go` | email / phone / login-id identifiers |
| `frontend/src/context/AuthContext.js` | `sendOtp`, `verifyOtp`, `logout`, `setRole`, vault and business state |
| `frontend/src/services/api.js` | `getBaseUrl`, every app API call |
| `frontend/src/screens/auth/*` | Splash, Login, OTP, Onboarding |

### Tenancy, products, apps, access

| File | Relevant items |
|---|---|
| `backend/internal/crm/platform/workspaces.go` | `handleProvisionWorkspace`, `handleAssignProduct` |
| `backend/internal/crm/platform/products.go` | `ProductConfig`, publish, `WorkspaceSetup` |
| `backend/internal/crm/platform/invitations.go`, `users.go`, `admin.go`, `access.go`, `hierarchy.go` | invitations, users, delegated admin, roles, permission sets |
| `backend/internal/crm/access/roles.go`, `effective.go`, `capabilities.go`, `nav.go` | `SystemRoles`, `Rules`, `Effective`, navigation |
| `backend/internal/crm/records/scope.go` | `memberScope`, `ownerScope`, `handleContext` |
| `backend/internal/crm/store/migrations/0001_foundation.sql` | identities, workspaces, products, memberships, roles |
| `frontend/src/crm/features/workspaces/*`, `products/*`, `users/*`, `access/*`, `owner/*` | Owner Console screens |

### Records

| File | Relevant items |
|---|---|
| `backend/internal/crm/records/spec.go`, `objects.go`, `meta.go` | object and field definitions |
| `backend/internal/crm/records/records.go`, `service.go`, `listing.go`, `filter.go`, `convert.go` | CRUD, lists, filters, lead conversion |
| `backend/internal/crm/records/reports.go`, `workflows.go`, `mail.go`, `campaigns.go` | analytics, automation, email |
| `backend/internal/crm/store/migrations/0002_records.sql`, `0006_objects.sql`, `0010_workspace_features.sql` | record tables |
| `frontend/src/crm/features/records/*` | list, detail, layout editor, conversion |

### Cards and businesses

| File | Relevant items |
|---|---|
| `frontend/src/screens/user/ScanCardScreen.js` | capture, OCR, review, save |
| `frontend/src/utils/ocrParser.js`, `perspectiveCrop.js`, `components/CardCornerAdjuster.js` | OCR and image handling |
| `backend/internal/card/service.go`, `handler.go` | `CreateSavedCard`, `GetSavedCards`, `GetPublicCard`, free-plan limit |
| `backend/internal/card/business_link.go` | `linkCardBusiness`, `createCardBusiness`, `NormalizeGSTIN` |
| `backend/internal/business/service.go` | `CreateBusiness`, `GetOwnerBusinesses`, `VerifyOwnerAccess` |
| `backend/internal/discovery/service.go` | `SearchBusinesses`, fallback data |
| `backend/internal/database/migrations/001_initial_schema.sql`, `014_card_businesses.sql` | app schema |

### The bridge

| File | Relevant items |
|---|---|
| `backend/internal/crm/connectors/cardflow/cardflow.go` | `bootstrap`, `Sync`, `upsertUser`, `SyncOnDemand` |
| `backend/internal/crm/connectors/cardflow/card_leads.go` | `syncBusinesses`, `upsertBusiness`, `createCardLead`, `convertCardLead` |
| `backend/internal/crm/connectors/cardflow/cases.go`, `tickets.go` | tickets ↔ cases, conversation |
| `backend/internal/crm/connectors/cardflow/appdata.go` | app users, businesses, related lists |
| `backend/internal/crm/store/migrations/0005_connectors.sql` | `external_links`, `connector_state` |

### Billing, config, deploy

| File | Relevant items |
|---|---|
| `backend/internal/billing/handler.go`, `revenuecat.go` | status, sync, webhook, `applyState` |
| `frontend/src/services/subscription/*` | RevenueCat native and web |
| `backend/internal/config/config.go`, `backend/internal/crm/shared/config.go`, `.env.example` | every setting name |
| `backend/cmd/api/main.go` | all app routes, CORS, embedded bundle |
| `frontend/src/entry.js`, `frontend/webpack.config.js`, `frontend/capacitor.config.json`, `frontend/vercel.json`, `render.yaml` | build and hosting |
| `backend/internal/crm/docs/DECISIONS.md`, `HANDOVER.md` | decisions D-01…D-92, run and release routine |
