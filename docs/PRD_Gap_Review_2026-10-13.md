# Master PRD and competitor matrix — checked against the code

Checked on 13 Oct 2026 against the repository at commit `146b000` (local build and tests).
Sources: `CRM_Master_PRD_Bugs_Gaps_0_to_100000_2026-10-13.pdf` (130 pages) and
`crm_competitor_matrix.xlsx`.

**Status words.** *Built* = in the code and covered by a test or a screen check.
*Partly* = some of it works; the rest is listed. *Not built* = nothing in the code.
*Not code* = a setting, a provider account, a legal or business decision.

The PRD itself says it is a baseline, not a claim that things exist. It holds roughly
130 numbered requirements, 265 dealership cases, 192 insurance cases and two new packs
(salon, real estate). That is many months of work for a team; this review says where the
product really stands so the order of work can be chosen.

---

## 1. Section 18 — "what to fix first"

| Item | Status | Note |
|---|---|---|
| FIX-01 SSO → platform owner | Built (D-140) | No automated test yet for the SSO path itself. |
| FIX-02 OTP preview on live | Not code | Needs an SMS provider and the preview flag removed on Render. |
| FIX-02 MFA for privileged roles | Not built | Owner console MFA is off by decision D-133; business Super Admins are not forced either. |
| FIX-03 API key / webhook / invite limits | Built (D-140) | Tested. |
| FIX-04 Microsoft email trust | Built (D-140) | No automated test. |
| FIX-05 Staff defaults, built-in sets | Built (D-140) | Tested through the real invite flow. |
| FIX-06 Allocation, restore, conversion | Built (D-140) | Concurrent allocation is not tested. |
| FIX-07 Export, currency totals, formats | Built (D-140) | Locale part of BUG-037 is time zone only. |
| FIX-08 Lists, context, notices, token in URL | Built (D-140) | |
| MASTER-01 Super Admin MFA off | Not built | Same as FIX-02. |
| MASTER-02 Deal lines vs accepted quote | Built (D-139) | Deal items follow the accepted quote. |
| MASTER-03 Appointment exact local time | Partly | Time zone applied (D-140); the relative-only text on appointments was not re-checked. |
| MASTER-04 Contract value vs line total | Not built | Needs a decision: fixed value or line-priced. |
| MASTER-05 Payment on a Draft invoice | Not built | Needs a decision: block, or hold as an advance. |
| MASTER-06 Primary campaign on a deal | Not re-checked | D-135 added campaigns on deals; the "primary" pick was not re-tested. |
| MASTER-07 Salon and real-estate packs | Not built | |
| MASTER-08 Owner console, mobile, real SMS/email untested | Open | I cannot test on live or with real providers. |
| MASTER-10 No load / failover evidence | Open | |
| MASTER-11 Never-delete vs recycle bin vs erasure | Not built | Recycle bin exists; no erasure process. Needs the policy first. |
| MASTER-12 Income / Net income labels | Partly | Income counts payments once (D-135); labels do not state their basis. |

## 2. Shared CRM (sections 5–8)

| Req | Status | What is missing |
|---|---|---|
| CRM-001 Lead capture | Built | Form, CSV, API, card scan, web-to-lead form with consent (D-146). No CAPTCHA. |
| CRM-002 Follow-ups and assignment | Partly | Tasks and reminders built; assignment only as a workflow step. No quiet hours, no assignment-rule screen, no fallback queue. |
| CRM-003 Conversion | Built | Repeat conversion returning the first result was not re-tested. |
| CRM-004 Customer 360 | Partly | No household, no hierarchy tree view, no merge undo. |
| CRM-005 Opportunities | Partly | No stage-required fields as such — validation rules (D-141) now cover this by hand. |
| CRM-006 Catalog and pricing | Built | |
| CRM-007 Quote → cash | Partly | Print / PDF for quotes, orders and invoices built (D-144). No e-signature, no HSN or CGST/SGST split. |
| CRM-008 Campaigns | Partly | No budget/cost, no approval, no bounce tracking. |
| CRM-009 Activities, calendar, search | Partly | No recurrence exceptions, no search index. |
| FIN-001/002 Money, receipts, refunds | Built | Concurrent allocation locking not proven by test. |
| FIN-003 Currency | Built | No dated rate tables. |
| FIN-004 Accounting export | Not built | |
| SRV-001 Cases and SLA | Partly | No email-to-case, no satisfaction rating. |
| SRV-002 Appointments and resources | Built | |
| SRV-003 Work orders | Partly | No checklist, no estimate approval by the customer. |
| SRV-004 Knowledge, portal | Partly | Knowledge object exists; no customer portal. |
| CFG-001 Fields and validation | Built (D-141) | No dependent picklists. Field access per permission set exists. |
| CFG-002 Formulas, roll-ups, generic approvals | Not built | Approvals exist for discounts/finance only. |
| CFG-003/004 Workflows and runs | Partly | No dry run. |
| CFG-005 Sandbox | Not built | |
| INT-001/002 API, keys, webhooks | Built | No idempotency keys, no bulk API. |
| INT-003 IndiaMART / JustDial / Meta feeds | Not built | Needs provider accounts. |
| INT-004 Telephony | Not built | Needs a provider. |
| INT-005 WhatsApp | Not built | PRD says later. |
| INT-006 Email | Partly | Sending and mailbox sync built; no one-to-one templates, no bounce handling. |
| MOB-001 Phone layout | Partly | Not re-tested this round. |
| MOB-002/003 Offline, field visits, GPS | Not built | |
| BUS-001…012 Business owner console | Partly | Users, roles, dashboards, team charts, audit built. No branches, no numbering series, no finance aging, no scheduled reports, no privacy console, no usage view. |
| OWN-001…016 Platform owner console | Partly | Tenant list, provisioning, plans/limits, audit built. No SaaS billing, no MRR, no support-access grants (owner still has full access to every business), no reliability or security dashboards. |
| ONB-001…009 Onboarding and migration | Partly | Checklist and CSV import with dry run built. No wizard, no background import, no batch rollback. |
| Login security | Partly (D-142) | Allowed networks and sign-in-again hours built. No MFA enforcement, no step-up. |
| Pickers and forms (your request) | Built (D-143) | Search by name / phone digits / number, second line, auto-fill, shorter New forms. Contact and Account now follow each other. Auto-fill is on the New form only, not yet when editing a record. |

## 3. Industry packs

**None of the five packs exists as a pack.** What works today is the general
lead → quote → order → invoice → payment → case flow, plus assets, appointments,
resources, work orders, contracts and entitlements, which the QA rounds used to play a
bike dealer by hand.

| Pack | Status | Biggest missing pieces |
|---|---|---|
| General business (GEN) | Mostly built | Refund-after-part-payment acceptance run. |
| Bike and car dealers (AUTO, 265 cases) | First slice built (D-145): stock by VIN, bookings with one vehicle each, test rides, delivery gate, registrations, service schedule and odometer. Still missing: | Vehicle stock by VIN / engine number, booking as its own record, allocation of one vehicle to one booking, test-ride record with licence check, on-road price breakdown, exchange vehicle, finance application per lender, PDI and handover checklists, registration tracker, **service plan created at delivery with odometer**, job card found by registration number, recall list. |
| Insurance agents (INS, 192 cases) | First slice built (D-148): policies, premium schedule, commission, renewal ladder, cancellation and clawback. Still missing: | Policies, policy years and instalments, commission expected / received / clawback, collection modes, renewal ladder, claims checklist, book import. |
| Beauty parlour and kits (BEA) | First slice built (D-149): stock, usage, kits, kit bookings, packages. Still missing: | Everything: stock by batch and expiry, kits, reservations, packages. |
| Real estate (RE) | First slice built (D-149): units, holds and bookings, site visits. Still missing: | Everything: projects and units, holds, bookings, milestones, channel partners. |

For the bike company that is waiting: the customer-facing basics work today (customer,
vehicle as an asset, service appointment with technician and bay, case, work order,
invoice, payment). The dealership-specific records above are the real gap.

## 4. Architecture, security, privacy, scale (sections 14–17)

| Area | Status |
|---|---|
| Tenant scoping on every request, same-tenant links (ARC-001, 006, 009) | Built (D-136, migration 0026). Row-level security in Postgres is not used. |
| Record version on edits (ARC-011) | Built. Transactional outbox: events table exists; no idempotent consumer framework. |
| Idempotency keys, cursor pagination (DEV-003) | Not built |
| SSRF guard on workflow HTTP steps (SEC-005) | Not re-checked |
| Consent records, notices, withdrawal (PRV-001) | Partly — consent log built (D-146); nothing yet refuses contact without consent |
| Erasure, retention, legal hold, breach register (PRV-002, 004) | Partly — privacy requests with erasure and legal hold built (D-149); retention schedules and breach register not |
| KYC by provider, Aadhaar last four only (PRV-003) | **Not built** |
| Load tests, failover, restore drills, monitoring, CI gates (NFR, OPS) | Not built / not evidenced |

## 5. Competitor matrix — the "Your CRM" column corrected

The sheet's own Notes tab says this column was filled from intentions. Against the code:

| Feature | Sheet says | Actually |
|---|---|---|
| Lead capture (forms, email, API) | Yes | Partly — manual, CSV, API, card scan. No web form, no email capture. |
| Leads never deleted | Yes | **No** — leads can be deleted to a recycle bin. |
| Lead scoring | Verify | No — a manual number field only. |
| Auto assignment / round robin | Verify | Partly — a workflow step; no rule screen. |
| Deal pipeline | Yes | Yes |
| Business accounts + individual contacts | Yes | Yes |
| Business card scanner | Yes | Yes |
| Quotes | Verify | Yes, with PDF |
| Territory management | Verify | Yes |
| Goal / target tracking | Verify | Partly — forecast targets. |
| Invoicing | Yes | Yes, with PDF (no numbering series per branch) |
| Subscriptions | Yes | Partly — the record exists; no automatic recurring billing run was verified. |
| Payment tracking | Yes | Yes |
| Reminders and renewals | Yes | Partly — tasks, contract renewal; no renewal ladder. |
| Support ticketing | Verify | Yes |
| Claims management | Verify | No |
| Customer portal | Verify | No |
| JustDial / IndiaMART / TradeIndia | Verify | No |
| Cloud telephony | Verify | No |
| Field sales check-in / GPS | Verify | No |
| WhatsApp | Partial | **No** |
| WhatsApp auto lead | Verify | No |
| Facebook / LinkedIn lead ads | Verify | No |
| GST-ready invoicing | Verify | Partly — tax lines; no GSTIN/HSN fields or legal invoice format checked. |
| DPDP consent log + deletion audit | Yes | **No** |
| Online KYC (Aadhaar/PAN) | Yes | **No** |
| Audit trail | Yes | Yes |
| Roles and permissions | Verify | Yes |
| Workflow automation | Verify | Yes |
| Custom fields / objects | Verify | Yes |
| Reports and dashboards | Verify | Yes (single grouping, no scheduling) |
| Email integration and sequences | Verify | Partly — sending, sync, campaigns; no sequences. |
| Mobile app | Verify | Partly — phone layout and a Capacitor build; not re-tested. |
| API and webhooks | Verify | Yes |
| Marketplace | No | No |
| Open-source / self-hostable | Yes | Self-hostable; the repo is public. No licence decision was checked. |
| Automotive dealership flows | Yes | **No** — see section 3. |
| Insurance agent flows | Yes | **No** |
| Premium + commission recording | Yes | **No** |

Honest totals for "Your CRM" over the 39 rows: 15 Yes, 8 Partly, 16 No.

## 6. Suggested order for the bike dealer

1. ~~Quote and invoice PDF with the dealer's name and GSTIN~~ — built (D-144).
2. ~~Vehicle stock by VIN~~ — built (D-145).
3. ~~Booking with one vehicle per booking~~ — built (D-145).
4. ~~Delivery checks, vehicle becomes the customer's asset~~ — built (D-145).
5. ~~Service plan at delivery, odometer, next service~~ — built (D-145). Reminders to the customer still need an SMS/WhatsApp provider.
6. ~~Job card found by registration number~~ — built. Estimate approval by the customer is not.
7. ~~Web-to-lead form~~ — built (D-146). Assignment-rule screen is not.
8. ~~Consent record~~ — built (D-146). Enforcement comes with the first messaging provider.

## 7. Decisions only you can make (PRD section 22 and above)

- First paying pack and pilot customers (DEC-01).
- MFA for the owner console and business Super Admins — turn on or keep off.
- Payment on a Draft invoice: block, or keep as an advance (MASTER-05).
- Contract value: fixed, or the sum of its lines (MASTER-04).
- Leads: keep the recycle bin, or archive-only with a separate erasure process (DEC-07).
- Owner access to customer data: keep full access, or move to time-limited grants (DEC-04).
- Providers: SMS, telephony, lead portals, KYC, accounting, later WhatsApp (DEC-08).
- Sales staff recording payments — still restricted (D-96).
