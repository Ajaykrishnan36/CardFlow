# CRM Transformation Plan

One implementation plan for turning this codebase into a single, multi-tenant CRM platform with business-card scanning as a feature.

- **Specification:** `COMPLETE_UNIFIED_CRM_PRODUCT_SPEC_FOR_CLAUDE.md` (109 sections), referred to below as "the spec" with section numbers like §30.
- **Evidence:** `EXISTING_PROJECT_ARCHITECTURE.md` in this repo, plus the code checks named in each part. Repository state: `main` at `f0309e5`.
- **Status:** plan only. **No production code has been changed. Nothing here starts until it is approved.**
- **Not inspected:** the live database and live settings. Anything depending on them is marked as needing the inventory step in Phase 0.

---

## How to read this plan

1. Parts 1–6 say what exists. They are short because the detail is in `EXISTING_PROJECT_ARCHITECTURE.md`.
2. Part 7 is the target. Parts 8–20 say how each area gets there.
3. Parts 21–26 are the working lists: files, tests, reconciliation, rollback, risks.
4. Part 27 lists the decisions that are yours. Each has a recommendation, so approving the plan can be "accept all recommendations" or a list of changes.
5. The end of the document has the phase order and a check of every spec section against this plan.

**Guiding rule taken from the spec (§2, §91, §105):** the existing CRM is the core. Nothing in this plan creates a second lead, contact, account, role, workspace, user or permission system.

---

## 1. Existing architecture

One Go server (`backend/cmd/api/main.go`), one Postgres database, one webpack build, two products:

| | CardFlow app | CRM |
|---|---|---|
| Frontend | `frontend/src/` React Native Web, JS | `frontend/src/crm/` React DOM, TypeScript, Tailwind |
| API | `/api/v1/*` | `/api/crm/v1/*` |
| Schema | `public` | `crm` |
| Sign-in | phone + OTP → JWT in `localStorage` | email + password / email code / OAuth / SSO → server session cookie + CSRF |
| Mobile | Capacitor shell loads this bundle | not reachable from the native app |

`frontend/src/entry.js` picks the app from the URL path. Hosting: Render (server), Vercel (web), Neon (database).

## 2. Existing database

- `public` schema: `users`, `businesses` (+ phones, services, card images), `saved_cards` (+ phones, emails, addresses, images, tags), `categories`, `support_tickets`, `support_ticket_messages`, `devices`, `contact_backups`, `revenuecat_events`, and several unused tables.
- `crm` schema: identity (`identities`, `verified_identifiers`, `sessions`, `otp_challenges`…), tenancy (`workspaces`, `products`, `product_versions`, `workspace_products`), access (`memberships`, `roles`, `role_assignments`, `permission_sets`…), records (`leads`, `accounts`, `contacts`, `object_definitions`, `object_records`…), plus activity, automation, analytics and integration tables.
- Every CRM record has `workspace_id`, `owner_id`, `created_by`, `updated_by`, timestamps, `deleted_at`, `version` (spec §50 is already met).
- No row-level security. Isolation is in application code (`records/scope.go` `memberScope`).

## 3. Existing CRM functionality

Built and working on the web: leads, contacts, accounts, lead conversion (transactional, idempotent — `records/convert.go`), opportunities with pipeline, cases, tasks, calendar, notes, timeline, communications, catalog, email, campaigns, reports, dashboards, workflows, custom objects and fields, layouts, views, filters, bulk actions, CSV import/export, merge, recycle bin, REST + GraphQL API, API keys, webhooks, role tree, permission sets, teams, SSO, OAuth, MFA, audit log, duplicate detection inside a workspace (`records/listing.go`, by email, phone, name).

Not built: income, expenses, plans/entitlements, website lead form, any mobile screen.

## 4. Existing CardFlow functionality

Working: phone sign-in (insecure, see part 5), card capture, crop, browser OCR (Tesseract.js), review, save, card vault with tags, images, share link, vCard; own business listings; public directory search with distance; support ticket conversation; RevenueCat subscription (per user); themes; local notifications.

Placeholders that return fixed or empty data: server OCR, GST verification, business analytics, digital card, credits, enquiries, token refresh, logout-all, delete account, export data.

## 5. Existing authentication

| | App | CRM |
|---|---|---|
| Identifier | phone in `public.users` | email / phone / login ID in `crm.verified_identifiers` |
| Proof | OTP that is **returned in the response and shown on screen**; no SMS is sent (`auth/service.go` `SendOTP`) | Argon2id password, email code (email only, `identity/otp.go`), OAuth, SAML/OIDC, TOTP MFA |
| Session | HS256 JWT, no working refresh or revocation | row in `crm.sessions`, cookie `crm_sid`, CSRF token, idle and absolute expiry, revocable |
| Limits | none | lockout after 5 failures, per-IP and per-identifier limits |

Useful facts for the plan, confirmed in code:

- `crm.verified_identifiers.kind` already allows `phone`; `crm.otp_challenges.channel` already allows `sms`.
- `identity.accountFor(kind, value)` already looks identities up by any identifier kind.
- `identity.finishSignIn` refuses a person with no membership (`no_workspace_access`). This must change for self-serve.
- `identity.bearerToken` only accepts API keys (`crm_…`). There is no token session for a native app, and the cookie is `SameSite=Lax`, which a Capacitor shell on another origin cannot use.
- MFA is forced for privileged roles outside local (`shared.Config.MFAEnforced`, `access.IsPrivileged`). A self-serve Super Admin would be forced into authenticator-app setup.

## 6. Existing connector (spec §53, §101)

`backend/internal/crm/connectors/cardflow/`. It reads the app's tables directly and writes CRM records into **one** workspace, code `business-card-snap`.

| Question | Answer | Code |
|---|---|---|
| What it creates | Per app user: a lead (source `app_signup`) converted at once into a contact. Per owned business: a business account, contact linked to the first one. Per unclaimed card business: a lead (source `card_scan`). Per support ticket: a Case. App custom fields and layout sections. The workspace, its app setup and modules on first run | `cardflow.go` `bootstrap`, `upsertUser`; `card_leads.go` `upsertBusiness`, `createCardLead`; `cases.go` `syncTicketCases` |
| What it updates | Contact profile fields from the app (the app is the source of truth); account fields from the business; lead conversion on claim; case status ↔ ticket status; ticket reply written back to the app | same files, `convertCardLead`, `onCaseEvent`, `tickets.go` |
| External IDs | `crm.external_links` primary key `(system='cardflow', external_type, external_id)` with `external_type` `user` or `business`; cases carry `custom.app_ticket_id` | `store/migrations/0005_connectors.sql` |
| Duplicate handling | The primary key makes each app row map to one set of CRM records; rows are locked `FOR UPDATE`; case sync is keyed by ticket ID (`records.SyncRecord`) | `cardflow.go`, `cases.go` |
| Error handling | An error stops that pass, is stored and shown on the Integrations page; ticket → case failures are logged and skipped | `cardflow.go` `setError`, `Sync` |
| Retry | Watermarks in `crm.connector_state`; every pass re-reads from the watermark with a 2-second overlap, so a failed row is retried next pass | `loadState`, `saveState` |
| When it runs | Hourly, plus on demand when a CRM page showing app data opens (at most once a minute) | `defaultSyncInterval`, `SyncOnDemand` |
| Also serves | App users and businesses edited in place from the CRM (`/w/{code}/app/*`), ticket conversation | `appdata.go`, `tickets.go` |

So today the whole card app is one tenant, and its users are that tenant's **contacts**. This plan keeps that workspace as the platform's own customer CRM (part 9) and stops using the connector as an identity bridge.

---

## 7. Target architecture

```text
                        ONE IDENTITY  (crm.identities)
       phone + OTP by SMS (customers)         email + password / Google (owner, staff)
                              │
        ┌─────────────────────┴──────────────────────┐
        ▼                                            ▼
 Mobile app (React Native Web, Capacitor)     Web CRM (React DOM) + Owner Console
 Home · My CRM · Scan · My Cards · Browse     full CRM workspace
        │  Bearer session token                      │  cookie session + CSRF
        └─────────────────────┬──────────────────────┘
                              ▼
                     /api/crm/v1  (one API, one authority)
        identity → membership → workspace → app/module → permission → record scope
                              │
        Business = crm.workspaces  (the tenant)
        ├── members, roles, permission sets, plan + entitlements
        ├── leads, contacts, accounts, opportunities, cases, tasks, events, notes
        ├── income, expenses                       (new standard objects)
        ├── cards scanned in this business + links to its CRM records
        └── reports, dashboards, workflows, email

        Public directory (public.businesses, categories)  ← stays separate, public data only
        Platform CRM + platform support                    ← the owner's own workspaces
```

What stays, what changes:

| Layer | Decision |
|---|---|
| CRM record engine, access, reports, workflows, email, API | Unchanged core. Extended, never duplicated |
| Web CRM UI | Stays the full CRM. Gains phone sign-in, business create/select, finance objects, dashboard date filter |
| Mobile UI | Stays React Native Web (spec §92). New CRM screens call `/api/crm/v1` |
| `/api/v1` | Kept working during the transition for published app versions; the app moves call by call to `/api/crm/v1` |
| `public.users` | Becomes an app profile row linked to an identity; no longer the login source |
| `public.businesses` | Stays the public directory model, optionally linked to a workspace |
| `public.saved_cards` | Stays the card vault, gains workspace and owner identity, linked to CRM records through a link table |
| Product / App (`crm.products`) | Internal configuration. Customers never see it |
| Connector | Reduced to the platform's own customer CRM feed and platform support |

---

## 8. Identity migration

**Decision (recommended): `crm.identities` is the single source of truth for who a person is.**

### 8.1 New sign-in for customers

| Step | Change | Where |
|---|---|---|
| Request code | New `POST /api/crm/v1/auth/phone/request`. Normalises the phone, applies per-IP and per-phone limits (reuse `otpLimiter`, `resetIPLimiter`), stores a hashed code in `crm.otp_challenges` with `channel='sms'`, 10-minute expiry, 5 attempts, single use. **Never returns the code.** In `local`/`dev` only, with no SMS provider configured, the code is written to the server log (same rule as the email code today, D-27/D-41) | `crm/identity/otp.go` (generalise `RequestOTP`), new `crm/sms` package |
| Send | New SMS sender behind an interface, provider chosen in part 27 | `crm/sms/sms.go` |
| Verify | New `POST /auth/phone/verify`. On success: find the identity by `(kind='phone', value)`. If none, create the identity and a verified phone identifier in the same transaction (sign-up and sign-in are one flow, as in the app today) | `crm/identity/otp.go`, `signup.go` |
| Session | `finishSignIn` gains method `phone`. A person with **no membership is allowed in**; the response says `next: "create_business"` instead of failing | `crm/identity/service.go` |
| Token for mobile | New bearer session: `Authorization: Bearer crms_<token>` resolves through the same `crm.sessions` row as the cookie. Bearer requests skip the CSRF check (no cookie, so no CSRF exposure). Web keeps cookies | `crm/identity/handlers.go` (`bearerToken`, `LoadSession`, `CSRF`) |
| Storage on device | Token in the platform's secure storage, not `localStorage` | new `frontend/src/services/session.js` |
| Logout / logout-all | Already real in the CRM (`identity.Logout`); the app starts calling them | app `AuthContext` |
| Sign-in method policy | `ProductConfig.LoginMethods` gains `phone`; the default customer setup turns it on | `crm/platform/products.go` |
| MFA | See part 20: phone OTP customers are not forced into authenticator setup | `access.IsPrivileged`, `shared.Config` |

### 8.2 Existing app users

Additive, no ID rewrites, no row deleted.

1. Migration adds `public.users.identity_id uuid NULL REFERENCES crm.identities` (unique).
2. Backfill script, idempotent, in batches. For each `public.users` row not yet linked:
   - Phone already a verified identifier of an identity → link to that identity.
   - Otherwise create an identity (`display_name` = name) and a `phone` identifier. It is marked verified only if the user has signed in at least once (`last_login_at IS NOT NULL`), because that proves the OTP was passed; otherwise unverified until first sign-in.
   - Email on the app profile is copied as an **unverified** email identifier only if no identity owns it. **Never auto-merge on email** without proof.
3. Lazy path: the new verify endpoint performs the same linking for any user the backfill has not reached.
4. `public.users.id` keeps being the foreign key for cards, businesses, tickets. Nothing downstream moves.

### 8.3 Transition for published app versions

- `backend/internal/middleware/auth.go` accepts **either** the legacy JWT **or** a CRM bearer session, and resolves both to the same `public.users` row through `identity_id`. Old app builds keep working.
- The legacy `/api/v1/auth/otp/*` endpoints are fixed in Phase 1 (no code in the response, limits, real SMS) and kept until old builds are retired, then removed.

### 8.4 Collisions and edge cases

| Case | Rule |
|---|---|
| Same phone on an app user and on an existing CRM identity (e.g. an invited Admin) | Link, do not create. One identity |
| Two app users whose phones normalise to the same number | Should be impossible (`users.phone` is unique); the backfill reports any and stops for a decision |
| Suspended or deleted app user | Identity created with the matching status; no session issued |
| Platform owner | Unchanged. Owner sign-in stays email + password / Google with MFA (spec §12) |

---

## 9. Business / workspace migration

**Decision (recommended): every customer business is exactly one `crm.workspaces` row** (spec §7, §103.4). The words "Product" and "App" leave the customer experience.

### 9.1 Self-serve creation

New endpoint `POST /api/crm/v1/businesses` (any signed-in identity, not owner-only):

1. Validates name; generates a unique `code` from the name (customers never type a code).
2. Calls the existing `platform.ProvisionTx` with the **platform default setup** as its one product, so system roles, role tree and default permission sets are created exactly as today.
3. Creates the membership as `active` and assigns `SUPER_ADMIN` to the creator in the same transaction (no invitation).
4. Starts the plan (free or trial, part 17) and writes the audit event `workspace.provisioned` with `selfServe: true`.
5. Idempotent through `Idempotency-Key` (same helper the owner flow uses).
6. Limits: a cap on businesses per identity and a per-identity rate limit, both platform settings.

Why a shared default setup: without product IDs, `ProvisionTx` calls `createProjectSetup` and makes **a new setup per workspace**. Thousands of self-serve businesses would create thousands of setups. One published "Standard CRM" setup, owned by the platform and referenced by a platform setting, avoids that and lets the owner upgrade every business by publishing a new version (already supported, D-45).

### 9.2 Selecting and switching

- `GET /api/crm/v1/me` already returns `memberships`. Mobile and web show them as "My Businesses".
- The current business is the workspace code in every URL (`/w/{code}/…`). `memberScope` already answers **403 `no_workspace_access`** for a non-member (spec §99 is met; a test will pin it).
- On switch the client drops every cached query for the previous workspace (spec §35). On mobile the query cache is keyed by workspace code, so nothing stale can render.

### 9.3 Existing listings (`public.businesses`)

- Stays the public directory table (spec §37, §55, §103.3). No merge with `crm.accounts`.
- Migration adds `public.businesses.workspace_id uuid NULL`: "this listing is the public profile of that business".
- **Existing owners are not given workspaces automatically.** At first sign-in after the upgrade, a user who owns listings sees "Set up your CRM for *Kovai Precision Tools*" (one tap, creates the workspace and links the listing). A user with no listing sees "Create your business". Reason: the number of card-only users on live is unknown; auto-creating would fill the platform with empty tenants.
- The Business profile screen in a workspace edits the linked listing (spec §36). No duplicate business table.

### 9.4 The platform's own workspaces (spec §102)

| Workspace | Today | Target |
|---|---|---|
| `platform` (`is_platform`) | Owner's Platform CRM | Unchanged |
| `business-card-snap` | Mirror of the app's users, businesses, tickets | Becomes the platform's **customer base**: every person who signs up still appears here as a contact, every customer business as an account, every platform support request as a Case. It is where "Platform Support" lives (part 16). Renamed in the UI; data untouched |

---

## 10. CRM record migration

No CRM record is moved or rewritten.

- Leads, contacts, accounts and all object records already live in the right place with the right ownership columns.
- The connector-created records in `business-card-snap` stay: they are the platform's records about its customers, which is correct in the target (the platform is a business with customers).
- Person vs CRM record (spec §8, §9, §108): a lead or contact is already a **per-workspace relationship**. Business A deleting its lead cannot touch Business B's (different rows, different `workspace_id`). Deleting a CRM record never touches an identity.
- **Global person model (recommended): none for people who are not users.** See part 11.3. `leads.identity_id`, `contacts.identity_id`, `accounts.identity_id` already exist for the "this record also has a login" case and keep that meaning.

Additive migration for matching speed: indexes on normalised phone and lower-cased email for `crm.leads` and `crm.contacts` per workspace (expression indexes; no column or data change).

---

## 11. Card migration

**Decision (recommended): `public.saved_cards` remains the card vault and is not the CRM contact model** (spec §93).

### 11.1 Schema (additive)

| Change | Purpose |
|---|---|
| `saved_cards.workspace_id uuid NULL` | The business the card was scanned in. NULL for cards saved before the change (personal vault) |
| `saved_cards.identity_id uuid NULL` | The owner as an identity (filled from `users.identity_id`) |
| New `crm.card_links (id, workspace_id, card_id, object_key, record_id, created_by, created_at)`, unique on `(workspace_id, card_id, object_key, record_id)` | "This card belongs to that lead / contact / account in this business". One card can be linked in several businesses without any of them seeing the others' links |

Card images stay where they are. Long term they can also be listed under the linked record's Files (spec §72); not in the first phases.

### 11.2 The new scan flow (spec §30–§32, §100)

```text
capture → crop → OCR in the browser → review     (unchanged components)
   ↓
POST /w/{code}/cards/match   { phones, emails, name, company, gstin }
   ↓  searches ONLY this workspace: leads, contacts (phone, email), accounts (GSTIN, name)
   ↓  returns the matches the user may see
choose:  Save as Lead · Save as Contact · Attach to existing Contact · Attach to existing Account · Save card only
         optional: follow-up date, note, owner
   ↓
POST /w/{code}/cards         one database transaction:
                             save the card (+ images after) → create or update the CRM record
                             through the existing record service → write crm.card_links
   ↓
response states exactly what happened: card saved, record created / updated / linked
```

Both schemas are in one database, so "card saved but CRM record failed" cannot happen: it is one transaction (spec §100). Image upload stays a second call and is retried by the client; the UI shows it separately.

Record creation goes through `records` service code, so validation, history, audit, workflows ("lead created → assign owner") and webhooks all fire as for any other lead.

### 11.3 Person matching and privacy (spec §31, §75, §76, §94)

- Matching is **workspace-scoped**. A phone found in another business is invisible and irrelevant.
- No global registry of scanned people. Reasons: it is not needed for any rule in the spec, and a global index of phone numbers collected from scanned cards would let one business learn that another business knows the same person. The spec's examples (§75, §76) are fully satisfied by per-workspace records.
- If a platform-wide person index is wanted later (analytics, enrichment), it can be added as an internal table of hashed identifiers. That is a product and privacy decision, listed in part 27.
- Hidden matches: a Staff user with "own records" scope may scan someone who is already a colleague's lead. Recommended behaviour: the match response says "1 existing record you can't open" without details, so the user can ask instead of creating a duplicate. Listed in part 27.

### 11.4 Existing cards and behaviour kept

- Existing cards stay in the personal vault (`workspace_id` NULL) and get the new actions: Create Lead, Create Contact, Attach to Contact, Attach to Account, Open CRM record (spec §33).
- GSTIN linking to the public directory (`card/business_link.go`) is kept as is: it feeds Browse, not the CRM.
- Share link `/share/:id` stays. `card/service.go` `GetPublicCard` already returns only card fields; it will be pinned by a test to never include notes, tags, links or workspace (spec §73).

---

## 12. Database migrations

All additive. Nothing is dropped in this plan. CRM migrations continue the numbered files in `backend/internal/crm/store/migrations/`; app migrations continue in `backend/internal/database/migrations/`.

| # | Migration | Contents | Phase |
|---|---|---|---|
| CRM 0017 | phone sign-in | `sessions.transport` (`cookie`/`bearer`); nothing else needed (`otp_challenges`, `verified_identifiers` already fit) | 1 |
| App 016 | identity link | `users.identity_id` + unique index | 1 |
| CRM 0018 | self-serve | `platform_settings` (key/value: default setup, business cap, limits); `workspaces.created_by_identity`, `workspaces.origin` (`owner`/`self_serve`) | 2 |
| App 017 | business ↔ workspace | `businesses.workspace_id` + index | 2 |
| CRM 0019 | finance objects | seed `income` and `expenses` in `object_definitions` (module `finance`); expression indexes for date and amount | 3 |
| CRM 0020 | matching | expression indexes on normalised phone / lower email for leads and contacts | 4 |
| App 018 + CRM 0021 | cards in CRM | `saved_cards.workspace_id`, `saved_cards.identity_id`; `crm.card_links` | 4 |
| CRM 0022 | plans | `plans`, `workspace_subscriptions`, `subscription_events`, `usage_counters` | 6 |
| CRM 0023 | push | `push_devices` (identity, platform, token) | 7 |
| later | isolation | row-level security policies (part 20) | 8 |

Rules: each migration is tested on a copy first; each has a written "how to undo" (all are column/table additions, so undo is "stop using it"); the app migrations' re-run-on-every-start behaviour (`database/migrate.go`) is kept in mind by making every statement idempotent.

---

## 13. API changes

Nothing existing is removed in this plan. New endpoints, all under `/api/crm/v1`:

| Area | Endpoint | Auth |
|---|---|---|
| Sign-in | `POST /auth/phone/request`, `POST /auth/phone/verify` | public, rate-limited |
| Session | existing `/me`, `/auth/logout`, `/auth/logout-all`, `/me/sessions` now also by bearer token | session |
| Businesses | `GET /businesses` (my memberships with plan and role), `POST /businesses` (create), `GET/PATCH /w/{code}/business` (profile, linked listing) | session / member with `business.manage` |
| Dashboard | `GET /w/{code}/dashboard/summary?from=&to=` (part 14.2) | member with dashboard capability |
| Cards | `POST /w/{code}/cards/match`, `POST /w/{code}/cards`, `GET /w/{code}/cards`, `POST /w/{code}/cards/{id}/links`, `DELETE …/links/{linkId}`; personal vault `GET /cards` | member / session |
| Records | **no new endpoints**: mobile uses `/w/{code}/crm/{object}`, `/crm/meta/{object}`, `/timeline`, `/notes`, `/convert`, `/search`, `/lookup` as the web does | member |
| Finance | **no new endpoints**: `income` and `expenses` are objects served by the record routes | member |
| Plans | `GET /w/{code}/subscription`, `POST /w/{code}/subscription/sync`; owner: `/platform/plans`, `/platform/subscriptions` | member with `billing.manage` / owner |
| Account | `POST /me/export`, `DELETE /me` (real implementations, part 20) | session |
| Push | `POST /me/devices`, `DELETE /me/devices/{id}` | session |

`/api/v1` during the transition:

| Group | Fate |
|---|---|
| `/auth/otp/*` | Fixed in Phase 1, kept for old app builds, removed in Phase 8 |
| `/cards*`, `/owner/businesses*`, `/categories`, `/businesses/search`, `/public/cards/*`, `/support/*`, `/billing/*` | Keep working through the dual-auth middleware; the new app moves to the CRM equivalents phase by phase |
| Placeholders (`/auth/refresh`, `/auth/logout-all`, `DELETE /users/me`, `/users/me/export`, `/owner/businesses/{id}/analytics`, `/verify/gst`, `/card`, `/enquiries`, `/billing/credits`, public `/cards/scan`) | Removed in Phase 8, after confirming no shipped build calls them |

The long-term names in spec §59 (`/leads`, `/contacts`…) map onto the existing `/w/{code}/crm/{object}` routes. No renaming is proposed: the workspace code in the path is what makes every request tenant-checked.

---

## 14. Mobile screen changes

Stack stays React Native Web inside Capacitor. Three structural changes come first because every CRM screen depends on them:

1. **API client**: new `frontend/src/services/crmApi.js` with bearer session, real error handling (today's client turns failures into empty lists), and 401 → sign-in.
2. **State**: split `AuthContext.js` (761 lines) into session, current business, and the existing vault/billing pieces. Server data moves to TanStack Query (already in the bundle), keyed by workspace code.
3. **Navigation**: the hand-written state machine in `AppNavigator.js` cannot hold list → detail → related → edit stacks. Replace it with a small stack navigator (recommended: `react-router` in memory/hash mode, already a dependency; it also gives the Android back button and web URLs).

### 14.1 Screens

| Screen | Status | Notes |
|---|---|---|
| Splash, Login, OTP | Modify | Same look. OTP screen stops showing the code. Calls the unified sign-in |
| Onboarding → **Business setup** | Modify | Name, then "Create your business" or pick one. Offers existing listings (9.3) |
| **Home** (`DashboardScreen.js`) | Rebuild content | Greeting, business switcher, quick actions (Scan Card, Add Lead, Add Contact), date range, metric cards, today's follow-ups, upcoming tasks, recent leads, opportunities, activities (spec §17) |
| **My CRM** (new tab) | New | Entry list: Leads, Contacts, Accounts, Opportunities, Cases, Tasks, Calendar, Activities, Notes, Reports. Items appear only if the backend says the user may read them |
| Record list (one generic screen) | New | Search, filter, sort, saved views, paging. Driven by `/crm/meta/{object}` so the same screen serves every object, including custom ones |
| Record detail (one generic screen) | New | Highlights, fields from the layout, related lists, timeline, actions: Call, Email, Message, Edit, Add Note, Create Task, Schedule Follow-up, Convert (leads) |
| Record form (one generic screen) | New | Field inputs by type from metadata; server validation shown per field |
| Opportunities pipeline | New | Stage columns as a swipeable list, move stage, mark won/lost |
| Tasks / Calendar | New | Today, upcoming, overdue; complete, reschedule, assign |
| Lead convert | New | Uses the existing convert endpoint and its options |
| Scan | Modify | Capture and OCR unchanged; new match and "save as" steps (11.2) |
| My Cards, Card detail | Modify | Kept; CRM actions added (11.4) |
| My Business | Replace | Becomes business switcher + Business profile (in Profile). Tab is replaced by My CRM |
| Browse, Business detail, Shared card | Keep | "Save to cards" kept; "Add as Account" added |
| Profile, Theme, Notifications, Privacy, Terms | Keep | Add business list, team (invite members), sessions |
| Subscription, Transactions | Modify | Per business (part 17) |
| Support (hub, new request, tickets, chat) | Keep | Platform support (part 16) |
| Basic reports | New (later) | Read-only run of saved reports; the builder stays on the web (spec §69) |

Bottom tabs: **Home · My CRM · Scan · My Cards · Browse** (`components/TabBar.js`).

### 14.2 Home numbers (spec §17, §18, §82)

One call, `GET /w/{code}/dashboard/summary?from=&to=`, computed in SQL with the caller's permissions and record scope:

| Metric | Definition (to confirm in part 27) | Date filter applies |
|---|---|---|
| Active accounts | accounts not deleted, lifecycle/type not inactive | no (current state) |
| Active contacts | contacts not deleted | no |
| Active leads | leads not deleted, status not converted and not lost | no |
| Closed leads | leads converted or lost in the period | yes |
| Total income / expenses / net | sum of `income` / `expenses` records dated in the period, in the business currency | yes |
| Today's follow-ups | leads with `next_follow_up_at` today + tasks due today | fixed to today |
| Upcoming tasks, recent leads, opportunities, activities | top 5 each | no |

Ranges: Today, This week, This month, This year, All time, Custom, in the business time zone. Objects the user cannot read are omitted, not zeroed. No sample data anywhere.

---

## 15. Desktop / web screen changes

The web CRM stays the full interface (spec §39). Changes:

| Area | Change | Files |
|---|---|---|
| Sign-in | Phone tab on `/crm/login` next to Password and Email code | `features/auth/login-pages.tsx`, new `phone-code-form.tsx` |
| After sign-in | `/crm/home` becomes "Your businesses": list, create, open. It already exists as the page for people with no workspace | `features/workspace/home-page.tsx` |
| Business switcher | In the workspace header | `features/shell/app-shell.tsx` |
| Sidebar | Dashboard, Leads, Contacts, Accounts, Opportunities, Cases, Tasks, Calendar, Activities, Reports, Automation, Settings (spec §40). Server-built already (`access/nav.go`); reorder and add Finance (Income, Expenses) | `backend/internal/crm/access/nav.go` |
| Dashboard | Date-range control and the summary metrics from 14.2 | `features/workspace/dashboard-page.tsx` |
| Business profile | New settings page editing the linked directory listing | new `features/workspace/business-profile.tsx` |
| Cards | "Cards" related list on leads, contacts, accounts; card image viewer | `features/records/record-detail-page.tsx` |
| Subscription | Settings → Plan & billing | new page |
| Wording | "Product" → "Business" in customer-facing text (`i18n/*`). Internal code names unchanged | `frontend/src/crm/i18n/` |
| Root URL | `/` for a signed-in web customer goes to their CRM; the card app at `/` remains the mobile experience on phones | `frontend/src/entry.js` (decision in part 27) |

Nothing in the web CRM is rebuilt in React Native, and nothing in the mobile app is rebuilt in Tailwind.

---

## 16. Owner Console changes

Reused and simplified (spec §12, §41). The sidebar is built in `backend/internal/crm/access/capabilities.go`.

| Target item | Today | Change |
|---|---|---|
| Dashboard | Overview | Add counts for self-serve sign-ups, active subscriptions, open support cases |
| CRM: Leads, Contacts, Accounts, Opportunities, Cases | Leads, Accounts, Contacts | Add Opportunities and Cases to the owner lists (the record engine already serves them) |
| Businesses | "Products" (`/crm/owner/workspaces`) | Rename. Show origin (self-serve / owner-created), plan, owner, last activity. "Add new product" stays as "Add business" for owner-created ones |
| Users & access | exists | Add identity search by phone |
| Plans & Subscriptions | missing | New: plan catalog, limits, per-business subscription, manual grant/extend (replaces "grant access" on app users) |
| Support | Cases in `business-card-snap` | A sidebar entry that opens the platform support queue with the conversation (already built, D-91) |
| Integrations | exists | Add SMS provider status and push provider status |
| Audit logs | exists | Unchanged |
| Settings | missing | Platform settings: default setup, business cap per person, sign-in policy. "Apps" (setups) and "Objects" move under here as platform configuration |

**Platform support vs customer cases (spec §26):** platform support requests are Cases in the platform's customer workspace, created from the app's Support screen by any customer. A customer's own Cases are records in their own workspace. Different workspace, different permissions, same engine; they cannot mix because every Case row carries its `workspace_id`.

---

## 17. Subscription changes

**Decision (recommended): the plan belongs to the workspace** (spec §48, §95).

```text
crm.plans                      key, name, limits (jsonb), modules, store product ids, active
crm.workspace_subscriptions    workspace_id, plan_id, status, period start/end, will_renew,
                               source (store / web / manual), payer_identity_id
crm.subscription_events        idempotent provider events (same pattern as revenuecat_events)
crm.usage_counters             workspace_id, metric, value   (users, records, storage)
```

- **Entitlements are enforced on the server.** `memberScope` loads the workspace's plan into the request scope; create paths check limits (members, records, storage, custom objects); modules not in the plan are off, exactly as modules not in the app setup are off today. A blocked action returns `402` with a machine-readable reason; clients only display it (spec §48 "do not hardcode limits in mobile").
- **RevenueCat:** the customer ID becomes the workspace (`ws:<id>`), not the user. The webhook (`billing/handler.go`) resolves it to a workspace and updates `workspace_subscriptions`. Existing per-user logic stays until old builds retire.
- **Existing premium users:** their entitlement is carried to the first business they create (or the one linked to their listing), until its current expiry. Nobody loses what they paid for. Card-only users keep the current free limit rule until they create a business.
- **Owner Console** can grant or extend a plan manually, audited.

Known constraint, listed as a risk: Apple and Google tie a subscription to the store account, and one store account can hold one active subscription per subscription group. A person paying for **several businesses** from one phone needs either separate products per plan tier with quantity handled by web billing, or web billing for business plans. This needs a decision before Phase 6 (part 27).

---

## 18. Income / expense implementation

**Decision (recommended): use the generic object engine**, not new tables (spec §45, §46).

Why it fits, checked in `records/objects.go`: object definitions already support `currency`, `date`, `select`, `lookup`, `textarea`, `percent`; records get lists, filters by date range, saved views, import/export, permissions, field access, timeline, reports with sum by date bucket, and the public API with no extra code. Opportunities and subscriptions are already built this way with `amount` as `currency`.

| Object | Module | Fields |
|---|---|---|
| `income` (prefix `INC`) | `finance` | amount (currency, required), date (required), category (select: Sales, Service, Subscription, Other, editable per business), description, account (lookup), contact (lookup), opportunity (lookup), reference, payment method |
| `expenses` (prefix `EXP`) | `finance` | amount (currency, required), date (required), category (select: Rent, Salary, Travel, Marketing, Supplies, Other), description, vendor (text), account (lookup), reference, payment method |

- Totals: `SUM((custom->>'amount')::numeric)` filtered by `workspace_id`, `object_key`, date; expression indexes added in migration 0019. Amounts in another currency are stored with their code (D-76) and, as list totals already do, summed in the business currency without conversion; multi-currency conversion is out of scope and noted in part 27.
- Net income = income − expenses, computed in the summary endpoint (spec §47).
- Permissions: two new catalog objects. Default sets: Super Admin and "Admin access" full; "Staff access" none. Adjustable per business.
- Optional link: marking an opportunity "won" can offer "Record income" pre-filled. Not automatic.
- When a dedicated table would be right: if invoicing, tax or a ledger is added later. Not now.

---

## 19. Permission changes

The model is unchanged (roles widen visibility, permission sets grant actions, backend decides). Additions only:

| Addition | Where |
|---|---|
| Objects `income`, `expense`, `card` in the access catalog | `access/roles.go` |
| Capabilities `business.manage` (profile, listing) and `billing.manage` (plan) | `access/roles.go` |
| Self-serve creator gets `SUPER_ADMIN` | part 9.1 |
| Default sets updated: "Admin access" gains finance and cards; "Staff access" gains cards (own) | `platform/hierarchy.go`, one-time migration of existing workspaces' default sets only if untouched |
| Plan entitlements gate modules and limits | part 17 |
| Mobile | renders from `GET /w/{code}/context` and `/crm/meta/{object}`; contains **no** permission logic (spec §14, §60) |

Removed: `public.users.role = 'admin'` loses all meaning (already inert in the app); the frontend "owner mode" (`AuthContext.switchToOwnerMode`) is deleted in Phase 8.

---

## 20. Security changes

| # | Change | Phase |
|---|---|---|
| 1 | OTP never in a response, the UI or production logs; hashed at rest; expiry; attempt cap; per-phone and per-IP limits | 1 |
| 2 | Real SMS delivery; failure returns an error instead of pretending | 1 |
| 3 | Bearer sessions for mobile backed by `crm.sessions`: revocable, idle and absolute expiry, logout and logout-all real | 1 |
| 4 | Token in secure device storage | 1 |
| 5 | Legacy JWT: long random secret required at start-up in production (refuse the built-in default), shortened lifetime, removed in Phase 8 | 1 / 8 |
| 6 | MFA policy: owner always; password-based privileged users as today; a session started by phone OTP is treated as possession-verified and is not forced into authenticator setup, with optional TOTP available. Sensitive actions (delete business, export all data, change plan, transfer ownership) require a fresh OTP | 1–2 |
| 7 | CORS: replace `*` with the configured origin list (`ALLOWED_ORIGINS` exists but is ignored) | 1 |
| 8 | Tenant isolation test suite across every record route, search, lookup, dashboard, export, files, timeline, cards (part 23) | 0, then every phase |
| 9 | Self-serve abuse controls: business cap, creation rate limit, SMS spend limits, audit | 2 |
| 10 | Real account deletion and data export (replacing the placeholders), with a grace period and audit | 7 |
| 11 | Public surfaces reviewed: `/share/:id`, `/b/{slug}`, Browse return public fields only, pinned by tests | 4 |
| 12 | Row-level security as defense in depth: policies on `workspace_id` using a per-transaction setting, with the application checks unchanged. Needs every record query inside a tenant transaction helper, so it comes last and only after the test suite is in place | 8 |
| 13 | `ENV=production` on the live server; remove unused secrets from config | 0 |
| 14 | Audit events added for: business creation, business switch by the owner, subscription change, data export, account deletion, card → CRM link (spec §65) | each phase |

---

## 21. Files to modify

Backend:

| File | Change |
|---|---|
| `crm/identity/otp.go`, `service.go`, `handlers.go`, `sessions.go`, `signup.go`, `normalize.go` | phone OTP, bearer sessions, sign-in without membership, MFA policy |
| new `crm/sms/` | SMS sender interface + provider |
| `crm/platform/workspaces.go`, new `crm/platform/selfserve.go`, new `crm/platform/settings.go` | `POST /businesses`, default setup, limits |
| `crm/platform/products.go` | `LoginMethods.Phone`; default "Standard CRM" setup |
| `crm/access/roles.go`, `capabilities.go`, `nav.go`, `effective.go` | new objects, capabilities, sidebars, entitlements |
| `crm/platform/hierarchy.go` | default permission sets |
| `crm/records/objects.go` | `income`, `expenses` standard objects |
| `crm/records/scope.go` | summary endpoint, plan in scope |
| new `crm/records/cards.go` (or `crm/cards/`) | match, save, links |
| new `crm/billing/` | plans, subscriptions, entitlements |
| `crm/connectors/cardflow/*` | platform customer feed only; stop identity bridging |
| `crm/store/migrations/0017…0023` | part 12 |
| `internal/middleware/auth.go` | dual auth (legacy JWT or CRM session) |
| `internal/auth/service.go`, `handler.go` | secure the legacy OTP until removal |
| `internal/card/*`, `internal/business/*` | accept identity + workspace; shared with the CRM card endpoints |
| `internal/billing/handler.go` | workspace customer IDs |
| `internal/database/migrations/016…018` | part 12 |
| `cmd/api/main.go` | CORS list, route wiring |

Frontend (mobile app):

| File | Change |
|---|---|
| new `services/crmApi.js`, `services/session.js` | API client, secure token |
| `context/AuthContext.js` → `context/SessionContext.js`, `BusinessContext.js`, existing vault/billing | split |
| `navigation/AppNavigator.js`, `components/TabBar.js` | stack navigation, new tabs |
| `screens/auth/*` | unified sign-in, business setup |
| `screens/user/DashboardScreen.js` | CRM Home |
| new `screens/crm/*` | My CRM, record list / detail / form, pipeline, tasks, calendar, convert |
| `screens/user/ScanCardScreen.js`, `SavedCardsScreen.js`, `SavedCardDetailScreen.js` | match and save-as flow, CRM actions |
| `screens/user/MyBusinessHubScreen.js`, `BusinessDetailsScreen.js`, `ProfileScreen.js` | switcher, business profile, team |
| `components/SubscriptionScreen.js`, `services/subscription/*` | per-business plan |

Frontend (web CRM): the files named in part 15 and part 16.

Docs: `HANDOVER.md`, `backend/internal/crm/docs/DECISIONS.md` (one entry per decision as it is implemented), replace the stale `docs/`.

## 22. Files to remove later

Only in Phase 8, each after an import check and a release where the replacement has been live (spec §56, §90).

| Item | Condition for removal |
|---|---|
| `frontend/src/screens/user/HomeScreen.js`, `screens/owner/*`, `components/Header.js`, `ScreenHeader.js`, `ConfirmModal.js`, `ContactsBackupModal.js`, `context/NavigationContext.js` | Still unimported after the new navigation ships |
| `frontend/src/data/mockData.js` and its fallbacks in `AuthContext` and `SearchScreen` | New API client in use |
| `AuthContext.switchToOwnerMode` / "owner" role | New business switcher in use |
| `backend/internal/extractor/` and public `/cards/scan` | Confirmed unused by shipped builds |
| Placeholder handlers (part 13) | Confirmed unused by shipped builds |
| `backend/internal/auth` legacy OTP + JWT, `/api/v1/auth/*` | Old app builds retired (store analytics show none in use) |
| `backend/internal/enquiry/` | Decision: build properly or drop |
| Connector identity bridging code | Part 24 reconciliation passed |
| `backend/migrations/` (duplicate), `docs/` (stale) | Any time; no runtime effect |
| Unused tables | **Not dropped in this plan.** Archived only after a separate, explicit approval following spec §79 |

---

## 23. Tests

Automated, in the repo, run before every release. They follow spec §81.

| Suite | Cases |
|---|---|
| Identity | new phone; existing phone; correct code; wrong code; expired code; reused code; attempt cap; per-phone and per-IP limits; code never present in any response body; logout; logout-all revokes bearer and cookie sessions; idle and absolute expiry; legacy JWT and bearer resolve to the same user |
| Business | create; idempotent retry creates one; cap per identity; creator is Super Admin; several businesses; switch; invite, remove, change role; suspended business → 403 |
| **Tenant isolation** | For every record route, search, lookup, timeline, files, export, dashboard summary, cards and card links: a member of A calling B's code gets 403; a record ID from B requested through A's code gets 404; list, search and dashboard of A never contain B's rows. Run as a table-driven test over the route list so a new route cannot be forgotten |
| CRM | create lead, contact, account, opportunity, case, task; convert lead once and twice (one result); notes; timeline; reports sum |
| Finance | income and expense create; totals by each date range in the business time zone; permissions hide totals |
| Card scanner | match existing lead; match existing contact; no match; save as lead; save as contact; attach; card only; failure inside the transaction leaves no card and no record; rescanning the same person updates, not duplicates |
| Multi-business person (spec §81) | phone `6382124970`: Business A lead, Business B contact, Business C lead; delete A's; B and C unchanged; A's notes and deal value never appear to B or C |
| Subscriptions | limit reached → 402; plan change; webhook idempotent; grandfathered premium |
| Migration | backfill is idempotent; counts reconcile (part 24); collisions reported, not merged |
| Frontend | type-check; component tests for the generic list, detail, form; manual script per release on Android, iOS and web |

A non-superuser database role is used for the isolation suite, as DECISIONS D-19 already notes is needed.

## 24. Data reconciliation

Run before and after each data step, on a copy first, then live. Results are saved with the release.

| Check | Expectation |
|---|---|
| `count(public.users where deleted_at is null)` vs users with `identity_id` | equal after backfill |
| identities created by backfill vs phone identifiers created | equal |
| duplicate phone identifiers | zero |
| app users vs `crm.external_links(type user)` | unchanged by the migration |
| `count(saved_cards)` and per-user counts before/after | identical |
| card images count and total bytes | identical |
| `count(businesses)` owned / unclaimed; `workspace_id` set only where the owner created a workspace | as expected |
| leads, contacts, accounts, object records per workspace before/after | identical |
| support tickets vs cases with `app_ticket_id`; messages per ticket | identical |
| old → new mapping tables: `users.id → identity_id`, `businesses.id → workspace_id`, `saved_cards.id → card_links` | exported and kept |
| premium users before vs workspace subscriptions granted | every paying user accounted for |

**Phase 0 produces the baseline:** a read-only SQL script (counts, foreign-key check, orphan check, connector link check) that you run or approve against live. I do not query the live database myself under the project's rules.

## 25. Rollback strategy

| Layer | How to go back |
|---|---|
| Schema | Every migration is additive. Rolling back code leaves unused columns and tables, which is harmless. Nothing is dropped, so nothing needs restoring |
| Behaviour | Feature switches (platform settings / env): `unified_auth`, `self_serve_business`, `mobile_crm`, `card_crm`, `workspace_billing`. Each phase can be turned off without a deploy |
| Sign-in | Legacy `/api/v1/auth` and JWT keep working through the dual-auth middleware until Phase 8. If unified sign-in misbehaves, the app is pointed back at the legacy endpoints by the switch |
| Data | Backfills only add links (`identity_id`, `workspace_id`). Undo is `SET … = NULL` for rows created by a tagged run; identities created by a run are tagged with the run ID |
| Deploy | Render "Rollback" to the previous deploy; Vercel "Promote previous" |
| Backups | Before any data step: a full dump stored outside Neon, plus a Neon branch. **Neon's free plan keeps only 6 hours of history**, so point-in-time restore is not a safety net here |
| Mobile | Old builds keep working against `/api/v1`; a bad release is fixed by server switch first, store update second |

## 26. Risks

| # | Risk | Likelihood | Effect | Mitigation |
|---|---|---|---|---|
| 1 | SMS in India needs DLT registration (entity, sender ID, template approval), which takes days and blocks phone sign-in going live | High | Phase 1 date | Start registration when the plan is approved; build against a test sender meanwhile |
| 2 | Per-business subscriptions through app stores (one active subscription per store account per group) | High | Phase 6 design | Decide model before Phase 6 (part 27, D6) |
| 3 | Two UI stacks: every CRM screen exists twice (web, mobile) | Certain | Ongoing cost | Metadata-driven generic mobile screens; mobile covers daily work, web covers configuration |
| 4 | Identity merge mistakes | Medium | Wrong person sees data | Link only on OTP-proven phone; never on email; collisions reported; isolation tests |
| 5 | A tenant leak in a new endpoint | Medium | Severe | Table-driven isolation suite as a release gate; later row-level security |
| 6 | Old app builds in the field | Certain | Breakage if `/api/v1` changes | Dual auth; no removal until usage is zero |
| 7 | Free hosting: Render sleeps (slow first OTP, delayed workflows), Neon free limits and 6-hour history | High | Poor first impression; weak backups | Move to paid tiers before public launch; external dumps |
| 8 | OTP cost and abuse (SMS pumping) | Medium | Money | Per-phone/IP limits, daily cap, country allow-list |
| 9 | Self-serve creates many empty tenants | Medium | Clutter, cost | Caps; owner view of inactive businesses; no auto-creation for existing users |
| 10 | Scope: 109 spec sections | Certain | Long delivery | Phases below; each ends in something usable |
| 11 | Personal data of third parties from scanned cards (DPDP Act) | Medium | Legal | Workspace-scoped storage, no global person index, export/delete, retention decision |
| 12 | Live deploy repo confusion (two GitHub repos) | Medium | Changes not going live | Fix remotes in Phase 0 |
| 13 | Storing card images and CRM files in Postgres | Medium | Database growth | Turn on S3-compatible storage before growth |

---

## 27. Open decisions

Each is yours. My recommendation is given so the plan can be approved as a whole.

| # | Decision (spec §103) | Recommendation |
|---|---|---|
| D1 | Identity source of truth | `crm.identities` |
| D2 | Migrate `public.users` into identities? | Link, don't move: add `users.identity_id`; keep `users` as the app profile row |
| D3 | Does `public.businesses` remain the directory model? | Yes; add optional `workspace_id` |
| D4 | Customer business = `crm.workspaces`? | Yes, one to one |
| D5 | Product/App internal? | Yes; one platform "Standard CRM" setup for all self-serve businesses |
| D6 | Subscription/entitlement schema and purchase route | Workspace-level tables (part 17). **Needs your choice:** store subscriptions tied to one business per store account, or web billing for business plans |
| D7 | Income/expense schema | Generic objects `income`, `expenses` |
| D8 | Global person model | None for non-users; workspace-scoped matching |
| D9 | Card-to-CRM model | Card vault + `crm.card_links` per workspace |
| D10 | SMS provider | **Needs your choice.** For India with DLT: MSG91 or similar; the config already has auth key, sender ID and template ID fields |
| D11 | Push provider | Firebase Cloud Messaging (covers Android and iOS through Capacitor) |
| D12 | Data retention | **Needs your choice:** recycle-bin retention default, how long after account deletion data is purged, OTP log retention |
| D13 | Privacy for globally matched people | No cross-business sharing of any CRM data; no global person index |

Additional decisions found while planning:

| # | Decision | Recommendation |
|---|---|---|
| A1 | Do existing listing owners get a workspace automatically? | No; one-tap offer at next sign-in |
| A2 | Are cards personal or shared with the business? | Owned by the person who scanned; visible to others in the business only through the CRM record they are linked to |
| A3 | Hidden duplicate when another rep owns the record | Tell the user a record exists without showing it |
| A4 | Definition of "Closed leads" and "Active accounts" | Closed = converted or lost in the period; Active = not deleted and not marked inactive |
| A5 | MFA for phone-OTP Super Admins | Not forced; fresh OTP for sensitive actions |
| A6 | Default finance access | Super Admin and Admin yes; Staff no |
| A7 | Business cap per person and free-plan limits | **Needs your numbers** |
| A8 | What `/` shows on desktop web for a signed-in customer | The web CRM; the card app layout stays the phone experience |
| A9 | Mobile navigation library | `react-router` (already bundled) |
| A10 | Enquiries feature | Drop, or rebuild as leads into the listed business's CRM (a good lead source) — your call |
| A11 | Multi-currency totals | Not converted in v1 |
| A12 | Which GitHub repo is primary | `Ajaykrishnancreations/CardFlow` (the one that deploys) |
| A13 | Phone-contacts backup, KYC | Keep backup as is; KYC out of scope |

---

## Phases

Each phase ends with something working and is released on its own. Sizes are relative (S, M, L).

| Phase | Outcome | Main work | Size |
|---|---|---|---|
| **0. Safety baseline** | Nothing visible changes | Live inventory script; backups; isolation test suite for existing routes; feature switches; deploy-repo fix; `ENV=production`; start SMS registration | S |
| **1. One secure sign-in** | Customers sign in by phone with a real SMS; one identity; app and CRM share sessions | Part 8, security items 1–7, migrations 0017 and 016, backfill | L |
| **2. Create and switch business** | A new customer creates a business and lands in a working CRM with no owner involvement | Part 9, web `/crm/home`, mobile business setup and switcher, migrations 0018 and 017 | M |
| **3. Mobile CRM core + Home** | Home dashboard with real numbers; Leads, Contacts, Accounts, Tasks on the phone; income and expenses on web and mobile | Part 14 foundations and generic screens, part 18, summary endpoint, migration 0019 | L |
| **4. Cards inside the CRM** | Scan → match → save as lead/contact; My Cards actions | Part 11, migrations 0020, 018, 0021 | M |
| **5. Rest of mobile CRM** | Opportunities pipeline, Cases, Calendar, Activities, Notes, lead conversion, basic reports | Part 14 remaining | M |
| **6. Plans per business** | Business-level subscription and enforced limits | Part 17, migration 0022 | M |
| **7. Owner Console and platform support** | Renamed and simplified console, Plans & Subscriptions, Support queue, push notifications, real export/delete | Part 16, security item 10, migration 0023 | M |
| **8. Cleanup and hardening** | One auth system, dead code gone, connector reduced, row-level security | Parts 22, 20.12 | M |

Order rationale: sign-in first because it is unsafe today and everything else depends on identity; business creation before mobile CRM because every CRM call needs a workspace; cards after the CRM screens exist so "open the lead" has somewhere to go; cleanup last so nothing is removed before its replacement has run in production.

---

## Spec coverage check

Every spec section against today's code and this plan.

| Spec § | Topic | Today | Plan |
|---|---|---|---|
| 1 | One CRM platform | Two products | Parts 7–16 |
| 2 | Don't rebuild the CRM | CRM exists | Rule throughout; no second engine |
| 3 | Current architecture | As described | Part 1 |
| 4, 5 | Two auth systems → one identity | Two | Part 8 |
| 6 | Phone + OTP, secure | Insecure | Part 8.1, part 20 |
| 7 | Business = workspace | Exists, owner-created only | Part 9 |
| 8, 9 | Independent relationships per business | Already true for CRM records | Part 10; tests in part 23 |
| 10 | Self-serve onboarding | Missing | Part 9.1 |
| 11 | Product/App internal | Customer-visible to owner only | Part 9.1, D5 |
| 12 | Owner Console | Exists | Part 16 |
| 13, 14, 15 | Super Admin, roles, permissions | Exists | Part 19 (additions only) |
| 16, 19 | Mobile role and tabs | Card app | Part 14 |
| 17, 18 | Home dashboard, date filter | Missing | Part 14.2 |
| 20–24 | My CRM, Leads, Contacts, Accounts, Opportunities | Web only | Part 14.1 |
| 25, 26 | Cases vs platform support | Mixed in one workspace | Part 16 |
| 27, 28 | Tasks, calendar, timeline | Web only | Part 14.1 |
| 29 | Scanner | Works | Kept |
| 30, 31, 32 | Scan flow, matching, actions | GSTIN → business only | Part 11.2, 11.3 |
| 33 | My Cards + CRM actions | Vault only | Part 11.4 |
| 34, 35 | Business switching | Listings, not tenants | Part 9.2 |
| 36 | Business profile | `public.businesses` | Part 9.3 |
| 37, 38, 55 | Directory separate from accounts | Separate | Kept separate |
| 39, 40 | Web CRM stays; customer flow | Exists; no create/select | Part 15 |
| 41, 42 | Owner Console UI; owner flow not required | Required today | Parts 9.1, 16 |
| 43 | Core objects | Exist | Kept; Files already exist |
| 44 | Lead conversion | Transactional, idempotent | Kept; tested |
| 45, 46, 47 | Income, expenses, net | Missing | Part 18 |
| 48, 95 | Business-level subscription | Per user | Part 17 |
| 49, 50 | Database, ownership columns | Present | Part 2 |
| 51, 79, 80 | Migration safety, reconciliation | n/a | Parts 12, 24, 25 |
| 52 | Review every CardFlow table | Reviewed in the architecture doc §13.1 | Keep all; no drops (part 22) |
| 53, 101 | Connector | Documented | Parts 6, 9.4 |
| 54 | Person matching | Missing | Part 11.3 |
| 56, 90 | Dead files | Listed | Part 22 |
| 57 | Screen targets | n/a | Part 14.1 |
| 58 | Profile, theme | Exist | Kept |
| 59 | API strategy | Two APIs | Part 13 |
| 60, 98 | Backend is the authority | True for CRM | Kept; mobile has no permission logic |
| 61 | Tenant security, RLS | App-level only | Part 20.8, 20.12 |
| 62 | Search scoped | Scoped (`/w/{code}/search`); owner search privileged | Kept; tested |
| 63 | Duplicates workspace-scoped | Exists | Kept; reused by card match |
| 64 | Soft delete | Recycle bin exists | Kept; identity deletion separate (part 20.10) |
| 65 | Audit | Exists | Part 20.14 additions |
| 66 | Notifications | Local only | Phase 7, D11 |
| 67 | Integrations | Exist | Kept |
| 68 | Support | Exists | Part 16 |
| 69, 70, 71 | Reports, workflows, import/export | Exist on web | Kept; mobile shows results |
| 72 | Files | Two stores | Card images stay for now; long-term under record Files |
| 73, 74 | Public share and business page | Exist | Field review and tests (part 20.11) |
| 75, 76, 94 | Privacy examples | n/a | Part 11.3; tests |
| 77 | Remove from customer experience | Present | Parts 9, 14, 22 |
| 78 | Must not be removed | Present | Nothing in this plan removes them |
| 81 | Testing | Partial | Part 23 |
| 82 | Performance | Paged APIs exist | Mobile uses paging; summary in SQL |
| 83, 84, 85 | Mobile, desktop UX, design language | n/a | Parts 14, 15; visual language kept |
| 86, 87, 88 | Keep / new lists | n/a | Matches parts 7–18 |
| 89 | File-level inspection | Done | Architecture doc §23; part 21 |
| 91, 92 | Reuse CRM objects; mobile-specific UI | n/a | Followed |
| 93 | Card ≠ contact model | Card is the only model | Part 11.1 |
| 96, 97 | Self-service; owner vs Super Admin | Owner-driven | Part 9; roles unchanged |
| 99 | Switching returns 403 | Already 403 (`memberScope`) | Pinned by test |
| 100 | Atomic card + CRM | n/a | Part 11.2 (one transaction) |
| 102 | Platform CRM workspace | Exists | Kept (part 9.4) |
| 103 | Open decisions | n/a | Part 27 |
| 104 | This plan | n/a | This document |
| 105 | Implementation rules | n/a | Adopted as the rules for every phase |
| 106, 107, 108 | Definition of done, final model, data rule | n/a | Target in part 7; done when every row above is met |
| 109 | Inspect first, plan, wait | Done | **Waiting for approval** |

---

## What I need from you to start

1. Approve the plan, or list changes.
2. Answer the four decisions that have no safe default: **D6** (how businesses pay), **D10** (SMS provider), **D12** (retention), **A7** (limits). The rest can go ahead on the recommendations.
3. Run or approve the Phase 0 inventory script against the live database, so migration sizes are known.

Nothing in the codebase changes until then.
