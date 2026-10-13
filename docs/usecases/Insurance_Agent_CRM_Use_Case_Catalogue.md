# Insurance Agent CRM: Use Case Catalogue (Service-Only Sales)

Oct 11, 2026 · @Gova

## Verdict

An insurance agent is an easier fit for this CRM than a dealership. There is no stock, no delivery and no workshop; the business is people, policies, dates and money. The existing lead, contact, contract, subscription, payment and workflow objects cover most of it, and a policy can be modelled as a contract that renews, which QA already proved (R1 #69).

**What is missing.** The policy itself (insurer, plan, sum assured, premium, term, nominee), a premium schedule, commission tracking against each insurer, and claims as a tracked process. Every reminder an agent lives by (premium due, renewal, grace period, maturity, birthday) depends on dates stored on the policy and on workflows that the QA reports have only partly tested.

**Fix first, from the earlier QA reports:**

1. Reminder workflow with no recipient publishes silently (R2 #97). A missed renewal is lost income.
2. False duplicate warning on a renewed contract (R1 #85), which would appear on every renewal.
3. Contract end date can precede start date (R1 #67).
4. Login code shown on the page of the test copy; confirm live behaviour, because policy files hold KYC and health details.
5. Dashboard "Income" ignores payments (R1 #84, R2 #104). Rework it for commission.

**Also fix, from the automotive QA of the same product (they affect this catalogue too):**

6. Won counts by expected close date, not by the date the policy was issued. This breaks targets (F9) and the dashboards.
7. Won value is the deal amount, not the accepted quote total, which distorts B12 and the pipeline.
8. Campaign membership is not copied to the contact when a lead converts, which affects A15 and campaign results (F4).
9. An account can be its own grandchild, which matters for household and employer accounts (A8, B18, H8).

Build order corrections: F7 (licence block) and F13 (role scoping and masking) move to Phase 1, F18 (secure sign-in) is Phase 0 only, and F15 is replaced by group H in Phase 1.

**How to read.** 192 use cases in groups A to J (A to G as first written; H and I added for the existing book, consent and privacy; J added for the new-agent primary flow), each marked with the same Fit column as the dealership catalogue.

**Primary use case.** For a new agent the core flow is: a new lead, a quote, the customer's premium and the agent's commission known before the sale, a policy with its years, a renewal date every year, a yearly project report with commission percentage by year, and a yearly plan such as 100 new customers with the commission planned from them. Group J ties groups A to G into that one flow and closes its gaps: fixed or variable premium, commission on the quote, a year-by-year policy table, and the annual plan. It is built first, ahead of the existing-book import in group H.

| Mark | Meaning |
| --- | --- |
| Ready | Passed in the QA reports |
| Partial | A base object exists; insurance fields or rules are missing |
| Untested | Present in the data model, never exercised in QA |
| Fix | QA found a defect that affects this case |
| New | Nothing in the data model covers it |

**Assumptions.** An individual or small-firm agent in India selling life, health, motor and general insurance on behalf of insurers, earning commission, who either lets the customer pay the insurer directly or collects the premium and remits it. "Invoice" therefore means two things: the agent's commission invoice to an insurer, and any fee the agent charges the customer. Tax and regulatory points (GST on commission, TDS, IRDAI rules, DPDP consent) are examples to confirm with a tax advisor and the insurer; build them as settings. Group G covers both collection modes; holding customer premium has conditions to confirm with the insurer. A brokerage would need further flows.

## Data map: insurance concept to CRM object

The cleanest model is one policy per contract. The contract holds the term and renews; a subscription holds the premium schedule; payments and invoices record money. Confirm with the team that new objects can be added without a schema migration.

| Insurance concept | Exists today | Build or extend |
| --- | --- | --- |
| Customer and household | contacts, accounts (Individual or Business), relationships, contact roles | Date of birth, PAN, occupation, income band, marital status, consent per channel; family members linked as relationships; household as an account |
| Insurer and plan | accounts typed Partner, catalog\_items, price books | Insurer as Partner with a role; plan catalogue with class (life, health, motor, general), sum-assured range, premium rules |
| Enquiry | leads | Insurance interest, family size, existing cover, budget, urgency, referrer |
| Needs analysis | opportunities, notes | Structured fact-find: dependants, liabilities, income, existing policies, protection gap |
| Quote comparison | quotes, line\_items | One quote per insurer; side-by-side view; chosen plan marked |
| Proposal and issuance | sales\_orders, tasks | Proposal number, underwriting status, medical or KYC requirements, issue date |
| Policy | contracts (start, end, renewal, contacts roles, renew action) | Policy number, insurer, class, sum assured, premium, mode, payment term, nominee, status, grace date, maturity date |
| Insured item | assets | Vehicle, property or equipment insured, with registration or address |
| Premium schedule | subscriptions | Instalments by mode (monthly, quarterly, half-yearly, yearly), due dates, paid or lapsed state |
| Premium payment | payments, payment\_allocations | Paid to the insurer directly, or to the agent who remits it; record reference, date, mode and receipt copy |
| Commission | invoices, credit\_notes, debit\_notes | New commission records per policy and year: expected, received, clawback; invoice to the insurer |
| Claim | cases, sla\_policies, entitlements | Claim type, intimation date, documents, insurer claim number, amount, status, settlement |
| Endorsement or service request | cases | Type (address, nominee, sum assured, port, revive), insurer reference, status |
| Reminder | workflows, tasks, campaigns, calendar events | Date-based triggers on policy fields; channel by consent |
| Agent licence and team | role\_assignments, teams, territories | Licence number, validity, insurer appointments, exam and renewal dates |

Notes: a policy bought from two insurers for one family is two contracts under one household account. Health and life data is sensitive; treat it with stricter masking than ordinary contact data.

## Actors and access

An agent usually works alone or with a small team, so the access model is simpler than a dealership's. The sensitive part is customer health and financial data.

| Role | Sees | Does | Must not |
| --- | --- | --- | --- |
| Agent (owner) | Everything | Sells, services, invoices insurers, runs reports | Nothing blocked; actions audited |
| Team member or POSP | Own customers and policies | Follows up, quotes, raises service requests | See other members' customers or commission |
| Back-office assistant | Policies, documents, service queue | Uploads documents, tracks proposals, logs claims | See commission or income reports |
| Telecaller | New leads, call tasks | Calls, qualifies, books meetings | See health or income details |
| Accountant | Invoices, commission, TDS | Raises insurer invoices, reconciles payouts | Edit policy details |
| Customer (portal or messages) | Own and family policies | Views policy, downloads documents, raises requests | See other customers |
| Family member or nominee | Limited view if the customer allows | Receives reminders; starts a claim | Edit the policy |
| Insurer or partner (optional) | Nothing by default | Shared leads or documents by explicit share only | Browse the customer base |

Rules to prove in QA: staff isolation holds for policies and claims (R2 proved it for leads and cases); health details are masked for every role except the agent and the assigned servicer; every view of a claim file is logged.

## A. Customer acquisition

An agent's pipeline is referrals, events and the existing book. Lead capture, duplicate checks and campaigns pass QA; consent capture and referral tracking need insurance-specific rules.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| A1 | Quick lead entry on a phone (agent) | Name, mobile, interest (life, health, motor, other) in under 30 seconds; duplicate mobile warns | Ready |
| A2 | Referral lead (agent) | Records the referrer; the referrer is thanked when the lead buys | Partial |
| A3 | Visiting card scan from a meeting or event (agent) | Photo creates a contact and a lead; source set to event | Untested |
| A4 | Lead from website, social ads, listing sites (assistant) | Import by form, CSV or webhook; source tagged; owner assigned | Untested |
| A5 | Missed call or WhatsApp enquiry (telecaller) | Logged against the lead; follow-up task created | Partial |
| A6 | Event or camp lead capture (agent) | Many people entered fast at one event; each tagged to the campaign and cost | Partial |
| A7 | Existing customer's family as new leads (agent) | A spouse, child or parent added as a linked lead from the customer's record | Partial |
| A8 | Employer or group lead (agent) | A company account with employees as contacts; group quote later | Ready |
| A9 | Lead temperature and priority (agent) | Hot when a need is stated and a meeting is booked; list sorted by it | Partial |
| A10 | Follow-up cadence (system) | Tasks at day 0, 2, 5, 10, 30; a reply or meeting stops the sequence | Untested |
| A11 | Book a meeting or call (agent) | Calendar slot, reminder to customer, outcome logged | Partial |
| A12 | Consent before marketing (agent) | Opt-in per channel with date and source; unsubscribe respected | Partial |
| A13 | Lost lead with reason (agent) | Reason mandatory: not interested, bought elsewhere, price, no response, health | Fix |
| A14 | Re-contact later (system) | A lead lost as "not now" reopens on a chosen date | Partial |
| A15 | Convert lead to customer (agent) | Contact, household and first opportunity created; lead keeps its link | Ready |
| A16 | Telephony (telecaller) | Click to call, call log and note attached to the record | New |

Privacy note: for any lead, store only what is needed to quote; collect health and income details at needs analysis, not at first contact.

## B. Needs analysis, quote, proposal and issuance

The opportunity, quote and order chain works. For insurance the quote becomes a comparison across insurers and the order becomes a proposal with requirements that must clear before a policy is issued.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| B1 | Fact-find (agent) | Dependants, income, liabilities, existing cover recorded on the household; missing items listed | New |
| B2 | Protection gap (agent) | Cover needed against cover held shows the shortfall for life and health | New |
| B3 | Opportunity per need (agent) | Separate deals for term life, health, motor, home; one household | Ready |
| B4 | Quote comparison (agent) | One quote per insurer for the same cover; premium, features and claim ratio side by side; one chosen; the agent also sees his commission per option (J6) | Partial |
| B5 | Revise a quote (agent) | New version when age, sum assured or add-ons change; old one expired; no false duplicate warning | Fix |
| B6 | Premium calculator (agent) | Premium by age, sum assured, term and mode, from a rate table or the insurer's quote | New |
| B7 | Share the quote (agent) | Sent by email or WhatsApp with a tracked link; opens appear on the deal | Partial |
| B8 | Discount or loading approval (principal) | A non-standard offer needs approval before it is shared | Ready |
| B9 | Proposal form (agent, assistant) | Applicant, life assured, nominee, medical and lifestyle answers; saved as a draft; completion checked | New |
| B10 | KYC collection (assistant) | PAN, address proof, photo, bank details; masked display; missing items listed for the customer | Untested |
| B11 | Medical or underwriting requirements (assistant) | Test booked, report received, insurer's decision logged: accepted, loaded, deferred, declined | New |
| B12 | Convert accepted quote to proposal (agent) | Plan, premium, premium type, term, commission rule and applicant carried over (J7); converts only once | Ready |
| B13 | Premium paid to the insurer (customer, assistant) | Customer pays the insurer directly; reference, date and mode recorded; proof attached | Partial |
| B14 | Policy issued (assistant) | Policy number, start and end dates, sum assured, premium, mode, nominee stored; the deal closes as won | Partial |
| B15 | Policy documents (assistant) | Policy copy, receipt and welcome kit stored and sent; the customer confirms receipt | Untested |
| B16 | Free-look period (system) | Reminder before the free-look window ends; cancellation recorded with refund if it happens | New |
| B17 | Multi-policy family sale (agent) | Several members insured on one proposal or several; household view shows all | Partial |
| B18 | Group or employer policy (agent) | Group quote, member list upload, certificate per employee, one invoice | New |
| B19 | Insured asset (assistant) | Vehicle or property linked to the motor or home policy; registration or address stored | Partial |
| B20 | Proposal rejected or withdrawn (agent) | Reason recorded; the lead returns to follow-up or closes as lost with reason | Fix |

## C. Invoices, commission, payments and subscriptions

The agent's own income arrives as commission, so "invoice" mostly means a commission invoice to an insurer. Premium goes to the insurer, directly or through the agent (group G); only the commission is the agent's income. Invoice, payment, allocation, credit note and subscription objects exist; commission logic is new.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| C1 | Premium schedule (system) | On issue, instalments are created by mode with due dates; each shows due, paid, overdue or lapsed | Untested |
| C2 | Record a premium payment (assistant) | Reference, date, amount and mode; the instalment clears; proof attached | Partial |
| C3 | Premium paid early or in advance (assistant) | Applied to the next due instalment; schedule shows credit | Untested |
| C4 | Failed payment or bounced mandate (assistant) | Instalment returns to due; customer gets a message; a task is created | New |
| C5 | Commission rules per plan (agent) | Rate by insurer, plan, policy year and payment term; first year, renewal and trail rates; stored as settings | New |
| C6 | Expected commission on issue (system) | Computed per policy and year from the rules; shown on the policy and in the pipeline | New |
| C7 | Insurer commission statement import (accountant) | Statement uploaded as CSV; lines matched to policies; unmatched lines listed | New |
| C8 | Reconcile expected against received (accountant) | Differences flagged by insurer and policy; short-paid items chased; ageing shown | New |
| C9 | Commission invoice to the insurer (accountant) | Invoice per insurer and period with tax and TDS as configured; no false duplicate warning | Partial |
| C10 | Record receipt of commission (accountant) | Payment allocated to one or many invoices; part payment leaves the right balance | Ready |
| C11 | Clawback on cancellation or lapse (accountant) | Free-look cancellation or early lapse creates a debit against future commission | New |
| C12 | Fee invoice to a customer (agent) | Optional fees, such as claim assistance or advisory; GST per settings; sent by link; payment by UPI recorded | Ready |
| C13 | Payment on a draft invoice (agent) | Recording a payment moves the invoice to Partially paid or Paid, not Draft | Fix |
| C14 | Invoice and order naming (agent) | Named by customer and insurer, not by the quote title | Fix |
| C15 | Tax and TDS summary (accountant) | Tax on commission invoiced and TDS deducted per period, ready for the return | New |
| C16 | Sub-agent or POSP payout (accountant) | Commission split by rule; payable to the team member; paid and balance tracked | New |
| C17 | Incentives and contest bonuses (accountant) | Insurer bonuses recorded against a period and policies | New |
| C18 | Service plan as a subscription (agent) | Optional paid plan, such as yearly policy review and claim support; renews, bills and reminds | Untested |
| C19 | Refunds and credit notes (accountant) | Refund of a fee or credit for an error tied to the original invoice | Untested |
| C20 | Income dashboard (agent) | Commission received, receivable and expected by month and insurer; payments counted | Fix |

Compliance note: where the agent collects premium (Mode 2 in group G), the money is held for the insurer and must be remitted within the insurer's time limit. Show it as a liability, never as income, and confirm the rules with each insurer.

## D. Renewals, reminders and lapse recovery

Renewals are the agent's recurring income, and every one starts as a date on a policy. Contract renewal works (R1 #69); the reminder workflows are the part to prove first.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| D1 | Renewal ladder for yearly policies (system) | Tasks and messages at 60, 30, 15, 7 and 1 days before expiry; stops when renewed | Untested |
| D2 | Premium due ladder for instalments (system) | Reminders before each due date; after the date, a grace-period countdown | Untested |
| D3 | Reminder with no recipient (agent) | A workflow cannot be published without a recipient; a test run shows who would receive it | Fix |
| D4 | Renew a policy (agent) | A new term starts the day after the old one ends; old one marked Renewed; links and nominee carried over. At issue the system already creates the renewal opportunity (Planned, Reminded, Quote shared, Renewed or Lapsed, never deleted) and each renewal creates the next one (H13) | Ready |
| D5 | Renewal quote (agent) | New premium compared with other insurers; customer chooses to renew or switch | Partial |
| D6 | Renewal with a changed profile (agent) | Age band, sum assured or add-on change recalculates premium; approval if the offer is non-standard | Partial |
| D7 | Contract dates validation (agent) | End date cannot precede start date; renewal start follows the end | Fix |
| D8 | No false duplicate on renewal (agent) | The earlier term of the same policy is never flagged as a duplicate | Fix |
| D9 | Motor policy renewal (agent) | Reminder with last year's premium, no-claim bonus and vehicle details; lapsed policies listed daily | New |
| D10 | Health portability and switch (agent) | Customer moves insurer; old policy closed, new one linked, waiting-period credit noted | New |
| D11 | Grace period and lapse (system) | Lapse date computed from the due date and grace days; policy marked Lapsed; an urgent task created | New |
| D12 | Revival of a lapsed policy (agent) | Revival offer, dues and health declaration tracked; policy returns to In force | New |
| D13 | Maturity and survival benefit (system) | Reminder before maturity; documents list; payout tracked; customer gets a review call | New |
| D14 | Policy anniversary and review call (system) | Yearly review task; changes in life events captured | Partial |
| D15 | Birthday and festival greetings (system) | Sent once, in the customer's language and channel, only with consent | Partial |
| D16 | Reminder to the right person (system) | Reminders go to the payer, not only the policyholder; a family member can be added as a recipient | Partial |
| D17 | Renewal pipeline report (agent) | Policies expiring in the next 90 days by month, insurer and premium; renewed against lapsed rate | New |
| D18 | Missed-renewal recovery (assistant) | Lapsed in the last 30 days list with a call script and outcome | Partial |

## E. Service requests, claims and grievances

The case object with SLA clocks passed QA (R1 #70 to #77, R2 #67 to #72). Insurance cases need a claim type, insurer references, a document checklist and a clock that tracks the insurer as well as the agent.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| E1 | Service request intake (customer, assistant) | Address, nominee, contact, duplicate policy copy, premium receipt; case linked to customer and policy | Partial |
| E2 | Endorsement tracking (assistant) | Request sent to the insurer; insurer reference, status and completion date stored | Partial |
| E3 | Claim intimation (customer, agent) | Claim type, date of event, hospital or garage, contact; case opens with a priority and a clock | Partial |
| E4 | Claim document checklist (assistant) | Checklist by claim type; each item received or missing; customer reminded | New |
| E5 | Cashless health admission support (agent) | Hospital, pre-authorisation request and status; urgent priority and a 2-hour response target | Partial |
| E6 | Motor claim support (assistant) | Surveyor, garage, estimate, approval, repair status, linked to the vehicle and policy | Partial |
| E7 | Death or maturity claim support (agent) | Nominee verified, documents listed, insurer claim number, settlement date | New |
| E8 | Claim status updates (assistant) | Each insurer response logged; customer notified at set stages | Partial |
| E9 | Rejected or short-settled claim (agent) | Reason recorded; appeal or grievance path started; outcome tracked | New |
| E10 | Escalation to the insurer's grievance channel (agent) | Date, reference, response due date, outcome; clock for the escalation level | New |
| E11 | Case SLA for the agent's own service (manager) | First response and resolution targets by case type; breaches reported | Ready |
| E12 | Resolution required (assistant) | A case cannot be resolved without a resolution note | Fix |
| E13 | Nominee and family contacts (agent) | Emergency contacts for health and life claims reachable from the policy | Partial |
| E14 | Claim history and ratio (agent) | Claims per customer and per insurer; used in the next quote comparison | New |
| E15 | Satisfaction after service (system) | Rating after each closed case; a low score creates a callback within 24 hours | Partial |

## F. Retention, cross-sell, compliance, reports and administration

A household that holds several policies renews more and refers more. Compliance matters more here than in vehicle sales: licence validity, consent and the privacy of health and income data.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| F1 | Household cover view (agent) | All policies of a family by person and class, with the gaps from B2 | Partial |
| F2 | Cross-sell by life stage (system) | Marriage, a child, a home loan, a new vehicle or retirement creates an opportunity and a task | New |
| F3 | Referral reward (agent) | Referrer recorded on the new policy; a thank-you or gift task on issue | Partial |
| F4 | Segments and campaigns (agent) | Filter by class, insurer, age, renewal month, premium; campaign results by policy issued, not clicks | Partial |
| F5 | Consent register (agent) | Opt-in by channel with date and source; withdrawal respected on the next send; export on request | Partial |
| F6 | Customer portal or messages (customer) | Own and family policies, due dates, documents, service requests | New |
| F7 | Agent licence tracking (agent) | Licence number, validity and renewal date for the agent and each team member; alert 60 days before expiry; sales blocked after expiry | New |
| F8 | Insurer appointments and training (agent) | Which insurers each member may sell for; product training dates; plans hidden if not appointed | New |
| F9 | Targets and scorecards (agent) | Replaced by J16 to J22 (yearly plan, customers and commission, back-calculation, actual against plan). The Ready mark no longer holds: won counted by expected close date breaks it until fixed | Ready |
| F10 | Persistency report (agent) | Share of policies renewed or paid on time by insurer, plan and month | New |
| F11 | Commission forecast (agent) | Expected commission by month from issued policies, renewals due and instalments due | New |
| F12 | Claims and service dashboard (agent) | Open claims by stage, ageing, insurer response time, satisfaction | Partial |
| F13 | Role scoping and masking (agent) | Team members see only their customers; PAN, health and income details masked except to the servicer | Partial |
| F14 | Audit trail (agent) | Changes to policy, nominee, commission and consent recorded with who and when | Untested |
| F15 | Import the existing book (assistant) | Superseded by group H (H1 to H24): full import flow, validation, matching, renewals built from the imported dates; built in Phase 1 | Untested |
| F16 | Document vault (assistant) | KYC, proposals and policies in restricted storage; retention period set; links expire | Untested |
| F17 | Data access and deletion requests (agent) | Superseded by group I (I8, I9): copy on request; erasure is Erased on request, never deletion; legal retention kept | New |
| F18 | Secure sign-in (agent) | Real SMS provider; no code shown on screen; MFA for the owner; session timeout | Fix |
| F19 | Phone-first use (agent) | Quick lead entry, today's reminders and a policy lookup work well at phone width | Untested |
| F20 | Languages and templates (agent) | Reminders and quotes in English and the local language from one template library | Partial |
| F21 | Integrations (agent) | WhatsApp Business, email, telephony, accounting, insurer portals where an interface exists | New |

Compliance note: insurers and the regulator set rules on disclosure, record keeping and customer communication. Confirm with each insurer before automating messages that could count as a sales pitch.

## G. Premium and commission entry, profit and project report

The agent enters the policy premium and his commission once. The CRM then shows the total to collect from the customer, the amount to pass to the insurer, and his profit, per policy, per customer, per month, and in a project report built from the same numbers.

**Two collection modes, chosen per policy.** Mode 1: the customer pays the insurer directly and the agent earns commission only. Mode 2: the customer pays the agent, who remits the premium to the insurer. Mode 2 puts the customer's money in the agent's hands for a while, so the CRM must show it as money owed to the insurer, never as income. Confirm the rules and time limits with each insurer before using Mode 2.

**In both modes the agent always sees both numbers.** Every policy carries the customer's premium and the agent's commission side by side, so he can read what the customer pays and what he earns without switching screens. Only the money flow differs: in Mode 1 nothing is collected or remitted by the agent, so the collect and remit lines show zero and profit is still calculated.

**The entry and the formulas**

| Field | How it is set |
| --- | --- |
| Premium before tax | Typed |
| Tax on premium | By setting for the plan class (may be nil) |
| Total to collect from the customer | Premium plus tax on premium, plus any fee the agent charges |
| Commission rate or amount | Filled from the commission rule, overridable with a reason |
| Gross commission | Rate times premium, or the typed amount |
| Amount to remit to the insurer (Mode 2) | Total to collect, less any commission the insurer lets the agent keep, less the agent's own fee |
| Direct costs | Sub-agent or POSP share, payment charges, courier, gifts |
| Profit | Gross commission, plus fee charged, less direct costs |
| Margin | Profit divided by total to collect, and profit divided by gross commission |

**Worked example (illustrative figures only)**

| Line | Amount in ₹ |
| --- | --- |
| Premium before tax | 20,000 |
| Tax on premium (assumed nil) | 0 |
| Total to collect from the customer | 20,000 |
| Commission at 15 percent | 3,000 |
| Sub-agent share at 30 percent of commission | 900 |
| Other direct costs | 100 |
| Profit | 2,000 |
| Profit as share of total collected | 10 percent |
| Profit as share of commission | 66.7 percent |

Tax on commission, and tax deducted at source on it, are tracked separately in C15 so they do not distort profit.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| G1 | Enter a policy sale (agent) | Five fields on a phone: customer, insurer and plan, premium, commission, collection mode; the screen shows total to collect and profit at once | New |
| G2 | Commission filled from the rule (agent) | Rate comes from C5 by insurer, plan and year; an override needs a reason and is logged | New |
| G3 | Choose the collection mode (agent) | Mode 1 or Mode 2 per policy; Mode 2 adds a remittance due date from the insurer's limit | New |
| G4 | Track the amount to collect (assistant) | Total, received, balance per customer and instalment; part payments allocate correctly | Partial |
| G5 | Track the amount to remit (assistant) | Due, remitted, reference, balance held; held money older than the limit alerts the agent | New |
| G6 | Profit per policy (agent) | Commission plus fees less direct costs, with both margins; matches the worked example to the rupee | New |
| G7 | Profit by customer, household, insurer, class and month (agent) | Filters and totals; the sum of policies equals the monthly total | New |
| G8 | Expected against received commission (accountant) | Variance per policy and insurer; short payments listed | New |
| G9 | Future commission on the policy (agent) | First-year and renewal commission for the next years shown from the rules, so lifetime value is visible | New |
| G10 | Change after entry (agent) | A new premium or endorsement recalculates all amounts; the old figures stay in the audit trail | New |
| G11 | Cancellation or free-look refund (agent) | Refund to the customer, premium recovered from the insurer, commission reversed; profit adjusts | New |
| G12 | Daily cash position (agent) | Collected today, remitted today, held for the insurer, own income earned | New |
| G13 | Monthly profit statement (agent) | Commission, fees, costs and net profit by insurer and class; opens to the policies behind each number | New |
| G14 | Reconcile with invoices and payments (accountant) | Totals agree with the commission invoices (C9), receipts (C10) and payments (C2); differences listed | New |
| G15 | Project report from actual data (agent) | One click makes the report from the book of policies, using the structure below; extended to yearly views and year-wise commission percentages in J14 and J15 | New |
| G16 | What-if projections (agent) | Change policies a month, average premium, commission rate, renewal rate and costs; the report updates | New |
| G17 | Export the report (agent) | PDF and spreadsheet for a bank, partner or tax filing, with every assumption listed | New |
| G18 | Report accuracy check (accountant) | Totals match G13 and G14; a report with unreconciled figures is marked draft | New |

**Project report structure**

| Part | Content | Source in the CRM |
| --- | --- | --- |
| Business summary | Agent profile, licence, insurers represented, team | Agent and licence records (F7, F8) |
| Assumptions | Policies a month by class, average premium, commission by class and year, renewal rate, ramp-up | Past policies; editable in G16 |
| Revenue | First-year commission, renewal commission, fees, incentives | G6, G9, C17 |
| Costs | Team and POSP share, rent, licence and training, marketing, software, travel | Direct costs on policies; overheads entered monthly |
| Profit and loss | Monthly for 12 months, yearly for 3 years | G13 |
| Cash flow | Collections, remittances, commission receipts, premium held for insurers | G12, C10 |
| Break-even and sensitivity | Policies a month to cover costs; effect of a 10 percent change in renewals or commission | G16 |
| Funding need | Working capital for premium float and early months, if any | Cash flow |

A bank may ask for its own format and for certified figures. Build the report so the numbers are exact and every assumption is visible; a chartered accountant can then review and certify it.

Acceptance test: enter ten policies in both modes with different commissions; the policy profits, the monthly statement, the cash position and the first-year section of the report all agree to the rupee with a hand calculation.

## H. Existing customers and policies: migration and bulk import

An agent who is already trading arrives with a book of customers and policies in spreadsheets, an old software package or insurer portal downloads. The new-lead path (groups A to D) covers a customer who buys from now on. This group covers the existing-book path: import customers, families and policies in bulk, validate them, match them against what the CRM already holds, and let renewals, premium reminders and commission start from the imported dates. It must be done before such an agent goes live, and it replaces F15. Scenario 1 below proves the whole flow.

**The flow**

1. Download the import template for customers, families and policies
2. Upload the file to a staging area; nothing reaches live data yet
3. Map columns, using a saved mapping if the same source was used before
4. Run a dry run that validates every row
5. Download the error and warning report, fix the rows, and re-upload only the failed ones
6. Review matches against customers and leads already in the CRM
7. Approve and import; large imports need a second approval
8. The CRM builds households and links each policy to its holder, insured persons and nominee
9. Premium schedules and a renewal opportunity for every policy are created from the imported dates
10. Policies already past due or lapsed are marked, and their call tasks are spread over several days
11. Imported customers are marked Consent pending and a consent request goes by WhatsApp or voice (I2, I3)
12. A reconciliation report ties the file to created, updated, held and rejected rows; a whole batch can be archived, never deleted

**Template columns**

| Group | Columns | Notes |
| --- | --- | --- |
| Customer | Name, mobile, alternate mobile, email, date of birth, address, city, pincode, language, occupation (optional) | Mobile is the main match key |
| Family | Family member name, relation, date of birth, mobile if any, household reference | Rows with the same household reference form one household |
| Policy | Insurer, plan, class, policy number, status (in force, lapsed, matured, surrendered, cancelled), start date, end or renewal date, sum assured, premium, mode, payment term, next due date, last premium paid date, grace days | Insurer plus policy number is the main policy key |
| People on the policy | Policyholder, insured persons, nominee name and relation | Every policy needs a holder |
| Insured item | Vehicle registration number and model, or property address | For motor and home policies |
| Money | Collection mode (1 or 2), commission rate or amount, sub-agent share | Blank uses the commission rule (C5) |
| Servicing | Agent of record (this agent, another agent, insurer direct), source of business, referrer | Anything other than this agent is a transfer (H16) |
| Consent | Marketing consent (yes, no, unknown), date, source | Unknown means Consent pending |
| Not accepted | Health or medical details, full Aadhaar, full PAN, bank, card or UPI numbers, document images | Stripped and reported; last four digits of Aadhaar or PAN optional |

**Validation rules**

| Check | Rule | Result |
| --- | --- | --- |
| Mobile | Ten digits, Indian format | Reject the row if invalid; warn if repeated in the file (a family may share one mobile) |
| Email | Valid format | Warn; row still imports |
| Policy number | Unique per insurer; spaces and case cleaned | Reject a duplicate in the file or in the CRM |
| Insurer and plan | Must exist in the catalogue | Hold the row with a map-or-create choice |
| Dates | End after start; next due date inside the term; last paid not in the future; date of birth not in the future | Reject impossible dates |
| Status against dates | In force with an end date already passed; Lapsed with a future end date and recent premiums | Hold for review |
| Premium and sum assured | Positive; premium not above sum assured | Reject non-positive values; warn otherwise |
| Mode and due date | Next due date fits the mode (monthly, quarterly, half-yearly, yearly) | Warn |
| Policyholder | Every policy has a holder row | Hold a policy with no holder |
| Nominee | Required for life policies | Warn if missing |
| Family | Relation valid; a child is younger than the parent | Warn |
| Motor | Registration number format; not already on an in-force motor policy | Warn on a duplicate |
| Commission | Rate within the limit set for the insurer and plan | Warn above the limit |
| Sensitive data | Health details, full Aadhaar, full PAN, bank, card or UPI numbers in any column | Strip, warn, report to the privacy owner |
| Consent | A yes needs a date and a source | Otherwise treated as unknown |

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| H1 | Template and guide (assistant) | Downloadable templates with example rows and allowed values; versioned | New |
| H2 | Upload to staging (assistant) | File held in staging; no live data touched; size and row limits; big files run in the background with progress | New |
| H3 | Column mapping (assistant) | Suggested matches; a saved mapping per source (old software, spreadsheet, insurer download) | New |
| H4 | Dry run (assistant) | Counts of good, warning, held and rejected rows; per-row messages; nothing written | New |
| H5 | Error and warning report (assistant) | Row, column, reason and suggested fix; downloadable; failed rows corrected and re-uploaded alone | New |
| H6 | Match customers (system) | By mobile, then email, then name and date of birth; result is new, matched or conflict | New |
| H7 | Merge rules on a match (system) | Empty fields filled; differing values listed for approval; a matched lead becomes a customer and keeps its history | New |
| H8 | Build households (system) | Family rows with the same household reference form one household account; relations stored; nobody becomes their own parent or grandchild | New |
| H9 | Match policies (system) | By insurer and policy number; a duplicate is rejected; a policy already held for another holder goes to review | New |
| H10 | Reference data check (system) | Unknown insurer, plan or class held, with a map-or-create choice | New |
| H11 | Status and date sanity (system) | Status checked against dates; in force with a past end date is held; lapsed and matured set correctly | New |
| H12 | Premium schedule from the import (system) | Instalments built from the next due date and mode; paid history optional; each shows due, paid, overdue or lapsed (C1) | New |
| H13 | Renewal opportunity for every policy (system) | Each in-force policy gets a renewal opportunity and the ladder (D1, D2) starting from its end or due date; each renewal then creates the next one | New |
| H14 | Overdue and lapsed backlog (assistant) | Past-due dates marked Overdue; past grace marked Lapsed with a revival list (D12); call tasks spread over a set number of days, not all on one day | New |
| H15 | Expected commission from the import (system) | Rate from the file or the rule; policy year worked out from the start date so first-year and renewal rates are right; shown on the policy (C6) | New |
| H16 | Agent-of-record transfer (agent) | A policy bought over from another agent, or held insurer-direct, is marked Transfer pending; no renewal ladder or expected commission until the insurer confirms; the confirmed date is stored and commission counts from then | New |
| H17 | Block sensitive data (system) | Health details, full Aadhaar or PAN, bank or card numbers stripped and reported; only the last four allowed | New |
| H18 | Consent status on import (system) | Customers without a valid consent source are Consent pending and marketing is blocked. A consent request goes by WhatsApp or voice (I2, I3) with attempts logged. Until they consent, only the consent request goes, at most twice; service reminders wait unless a lawyer confirms they may go | New |
| H19 | Reconciliation report (assistant) | Rows in the file against created, updated, held and rejected; customers against policies; premium and commission totals tie | New |
| H20 | Batch audit and archive (principal) | Who, file, time, mapping, counts and approvals recorded; a whole batch can be archived and hidden; nothing deleted | New |
| H21 | Re-import and update (assistant) | The same file twice creates no duplicates; a later file fills empty fields or applies approved changes | New |
| H22 | Import permissions and limits (principal) | Only the agent and a trusted assistant; daily row limit; second approval above a set size; commission columns visible only to the agent and the accountant | New |
| H23 | Existing customer calls for service (assistant) | Search by policy number, mobile or vehicle number finds the customer, household and policies; if not found, a quick entry is created and marked Unverified for later matching | Partial |
| H24 | Existing customer buys another policy (agent) | From the customer or household record start an opportunity with details prefilled; the old policy can be linked as replaced or ported (D10) | Partial |

Done when: every row in the file is either in the CRM correctly linked, held with a clear reason, or rejected with a clear reason; renewals and premium reminders start from the imported dates; and no health detail or full identity number reaches the database.

## I. Consent, privacy and audit

Health, income and nominee data make this catalogue more sensitive than the dealership one. The same rules apply: consent per purpose with notice shown first, no pre-ticked boxes, withdrawal that takes effect at once, an append-only audit trail, and a privacy owner who is not in sales. Leads and customers are never deleted and there is no recycle bin; they move to Deferred, Archived or Erased on request, and policies move to Lapsed, Cancelled, Surrendered or Matured. Notice wording, retention periods, breach deadlines and commencement dates are examples to confirm with a lawyer and each insurer.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| I1 | Consent per purpose (agent) | Separate consent for service messages, marketing, health data used for quotes, and sharing with an insurer; notice first; no pre-ticked box; each logged with channel, date, time and notice version | Partial |
| I2 | Consent request by WhatsApp (system) | An approved message with the notice link goes to the number on record; the customer replies or taps to give or refuse each purpose; sent, delivered, read and replied times logged; two attempts at most, then the status stays Consent pending. Check WhatsApp business messaging rules on opt-in and approved templates with the provider | New |
| I3 | Consent by voice call (agent or automated call) | Call from a registered number; the notice is read out in the customer's language; consent by key press or a spoken yes; call id, start and end time and outcome logged; a recording is made only if the notice says so, stored with its reference and kept for the retention period | New |
| I4 | Consent attempt log (privacy owner) | Every request logged with channel, number, attempt count, outcome (given, declined, no reply, wrong number) and date and time; a report lists customers still pending by age; a wrong number flags the mobile for review | New |
| I5 | Service and promotional messages kept apart (system) | Premium due, renewal notice, claim and service updates are service messages; cross-sell, greetings and campaigns are promotional and need consent; quiet hours and do-not-disturb respected; whether service messages may go before consent is for a lawyer and each insurer to confirm | New |
| I6 | Withdrawal (customer) | One reply, key press or link withdraws consent; applies on the next send; logged | Partial |
| I7 | Health and income data (agent) | Collected at needs analysis, not at first contact; masked for every role except the agent and the assigned servicer; every view logged | Partial |
| I8 | Copy of data on request (privacy owner) | Export of the customer's own data within a set time; requester verified first; logged | New |
| I9 | Erasure request (privacy owner) | Intake, verify the requester, preview every field with the legal reason to keep or erase, approval, erase or anonymise, instructions to third parties (insurer, messaging provider), confirmation; the record keeps its shape as Erased on request; replaces F17 | New |
| I10 | Retention by data class and legal hold (privacy owner) | Policy, claim, KYC, commission and tax records kept for the periods set; an open claim or dispute places a legal hold | New |
| I11 | Append-only audit (principal) | Consent, health and claim-file views, exports, edits (hashed), erasure steps, role changes, failed access and breaches recorded; no role can edit or delete an entry; an attempt to do so is itself logged; replaces F14 | Untested |
| I12 | Privacy owner and breach register (principal) | A privacy role separate from sales; a breach log with times and the notices sent | New |
| I13 | KYC and document images (principal) | Decision needed. Either the CRM keeps only a verified flag and a masked reference, with documents held on the insurer's portal, or documents sit in a vault (F16) with a retention period, expiring links and a view log. Aadhaar is last four digits only either way | New |
| I14 | Suitability record (agent) | The fact-find, gap, quotes compared and the reason for the chosen plan are saved with the proposal and locked after issue; the customer's declaration is recorded with date and time | New |
| I15 | No deletion (principal) | Leads and customers never deleted; statuses Deferred, Archived, Erased on request; policies move to Lapsed, Cancelled, Surrendered or Matured; a record that others point to cannot be removed | New |

To confirm before launch: the erasure rule (personal fields erased, record shape kept), notice wording and retention periods with a lawyer, whether service messages may go before consent, and the KYC image decision in I13.

## J. New-agent core flow: quote, premium, commission by policy year, report and annual plan

This is the primary flow for a new agent, and it is where the earlier groups left gaps. The catalogue handled policies, quotes and commission rules separately; it did not say how a premium that changes each year is treated, it did not show the agent's commission on the quote, it had no year-by-year view of a policy, its report stopped at 12 months and 3 years with no commission percentage by year, and its targets were monthly premium and policy counts, not a plan such as 100 new customers. Group J closes those gaps. Scenarios 14 to 17 prove it.

**The flow**

1. Set up the agent: licence, insurers, commission rate card, premium type per plan
2. Capture a lead and take it to a quote
3. The quote shows premium and the agent's commission for each insurer, for the first year and the renewal years
4. The customer sees premium and features only, never the commission
5. The chosen quote becomes a proposal and then a policy, carrying premium, premium type, term and commission rule
6. At issue the system creates a policy year table: each year's premium, commission percentage, expected commission and renewal date
7. Every year the renewal reminder ladder runs; at renewal the actual premium and commission replace the estimates
8. The yearly project report and commission percentage by year are built from the same table
9. The annual plan, for example 100 new customers, sets the targets and is compared with actual results every month

**Premium types**

| Type | Meaning | Typical plans | How the CRM treats it |
| --- | --- | --- | --- |
| Fixed | The same premium every year of the premium-paying term | Term life, endowment | Firm in every year; no estimate label |
| Stepped | A known schedule that changes in set years | Step-up plans | Firm; each year's premium taken from the schedule |
| Variable | Changes at renewal with age band, claims or no-claim bonus, medical inflation or an insurer revision | Health, motor and other general policies | Estimated until the insurer issues the renewal notice; labelled Estimated everywhere; replaced by the actual at renewal |

**The policy year table, created at issue**

| Column | Meaning |
| --- | --- |
| Policy year | Year 1, 2, 3 and so on, for the whole policy term |
| Start and end | The dates of that year; the end is followed by the renewal date |
| Premium due | Fixed, stepped or estimated; zero after the premium-paying term for limited-pay plans |
| Premium basis | Fixed, Stepped, Estimated or Actual |
| Commission percentage | From the rate card by insurer, plan, policy year and premium-paying term |
| Expected commission | Percentage times premium |
| Actual commission | Filled when the insurer pays; the difference from expected is shown |
| Renewal date | The date the next year's premium falls due, driving the reminder ladder |
| Status | Paid, Due, Overdue, Lapsed or Estimated |

**Worked example of a yearly plan (illustrative figures only)**

An agent plans 100 new customers. Commission rates here are examples; real rates come from each insurer's rate card and are subject to regulatory limits. Year 2 assumes 90 percent of customers renew; health premium is assumed to rise 8 percent at renewal and motor premium to stay flat. Every year 2 figure is Estimated.

| Product | Customers | Premium type | Year 1 premium (rupees) | Year 1 rate | Year 1 commission | Year 2 premium | Year 2 rate | Year 2 commission |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Term life | 40 | Fixed | 600,000 (40 x 15,000) | 30 percent | 180,000 | 540,000 | 7.5 percent | 40,500 |
| Health | 30 | Variable | 600,000 (30 x 20,000) | 15 percent | 90,000 | 583,200 | 15 percent | 87,480 |
| Motor | 30 | Variable | 240,000 (30 x 8,000) | 12 percent | 28,800 | 216,000 | 12 percent | 25,920 |
| Total | 100 |  | 1,440,000 | 20.8 percent effective | 298,800 | 1,339,200 | 11.5 percent effective | 153,900 |

Back-calculation from the same plan: if 25 percent of quotes become policies and 50 percent of leads reach a quote, 100 customers need 400 quotes and 800 leads in the year, about 67 leads, 33 quotes and 8 policies a month.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| J1 | New agent setup (agent) | A setup wizard: agent profile and licence, insurers appointed, classes sold, commission rate card (C5), tax settings, default collection mode, team; done when the first quote can be made | New |
| J2 | Policy information record (agent) | Insurer, plan, policy number, sum assured, premium, mode, policy term, premium-paying term, start date, renewal date, nominee, status all kept on the policy | Partial |
| J3 | Premium type (agent) | Fixed, Stepped or Variable set per plan and per policy; a Variable policy needs an estimate rule | New |
| J4 | Variable premium estimate (system) | Estimate by rule: yearly increase percentage, age-band table, or last year's premium; every forecast labelled Estimated; the actual premium at renewal replaces it and the variance is recorded | New |
| J5 | Premium calculator (agent) | Age, sum assured, term, premium-paying term, mode and add-ons give premium per insurer from a rate table or the insurer's quote (extends B6) | New |
| J6 | Commission on the quote (agent) | Each quote option shows premium, commission percentage and amount for year 1 and for the renewal years; visible only to roles allowed to see commission; the quote shared with the customer never shows it | New |
| J7 | Carry the quote into the policy (agent) | The chosen quote carries plan, premium, premium type, term, premium-paying term and commission rule to the proposal and the policy; a change after issue recalculates (G10) | Partial |
| J8 | Policy year table (system) | Created at issue for the whole term, with the columns above; policy term is kept apart from premium-paying term, so years after payment ends show zero premium and the commission the rule gives | New |
| J9 | Commission percentage by policy year (agent) | Rate from the rate card by insurer, plan, policy year and premium-paying term; an override needs a reason and is logged; shown on the policy and in reports | Partial |
| J10 | Renewal date every year (system) | Each policy year has its renewal date; the reminder ladder (D1) and a renewal opportunity (H13) start from it; lapse and grace rules apply | Partial |
| J11 | Renewal outcome and actual commission (accountant) | At renewal, the actual premium and the commission received are entered against that year's row; the variance from expected is shown; the effective percentage earned that year is stored | New |
| J12 | Renewal rate and persistency by year (agent) | Of the policies issued in a year, the share renewed in year 2, 3 and so on, by insurer and product (extends F10) | New |
| J13 | Policy and book forecast (agent) | Per policy and for the whole book, commission by year for the full term; Fixed and Stepped years shown as firm, Variable years as Estimated; totals by year (extends G9 and F11) | New |
| J14 | Yearly project report (agent) | Yearly view for up to five years or the policy terms: new policies, renewals, premium, commission percentage, commission, costs, profit; firm and Estimated split; plan and actual side by side (extends G15) | New |
| J15 | Commission percentage by year (agent) | A table by insurer and product: effective percentage in year 1, 2, 3 and so on, expected against actual | New |
| J16 | Annual plan, goal setting (agent) | Set the year's goal: number of new customers, product mix, average premium and commission by product, conversion rates and renewal rate | New |
| J17 | Planned commission (system) | From the plan: planned premium and commission for year 1, and year 2, 3 onwards from renewals, with Estimated labels | New |
| J18 | Back-calculation (system) | From customers and conversion rates: policies, quotes, meetings and leads needed per month, with the working shown | New |
| J19 | Split the plan (agent) | By month with seasonality, by team member and by product; the parts add up to the whole | New |
| J20 | Actual against plan (agent) | Customers issued, premium and commission against plan, counted by issue date; pipeline coverage from open quotes and conversion rates; an alert when behind by a set percentage | New |
| J21 | Re-plan with versions (agent) | Change the plan mid-year; every version kept; variance shown against the original and the latest | New |
| J22 | Plan feeds the report (agent) | The plan is the base case in the project report (G15, G16); the report shows plan and actual together | New |
| J23 | Funnel against assumptions (agent) | Lead to quote to proposal to issued, with actual rates against the plan's; suggested changes to assumptions are approved by the agent, never applied silently | New |
| J24 | Income goal (agent) | The agent enters a goal in rupees of commission instead of customers; the system works out customers needed from the average commission | New |
| J25 | Firm or Estimated labelling (accountant) | Every forecast, report and plan number is labelled firm or Estimated; plan figures are never mixed with actuals in totals | New |

Done when: a new agent can set up, quote with commission, issue a policy and see every year of it; the yearly report shows commission percentage by year with Estimated parts labelled; and a plan of 100 new customers shows its commission, the leads needed and the gap against actual every month.

## Build order and next QA round

Build in the order the agent earns money: capture the book, issue policies, never miss a renewal, then get paid commission correctly. Service and claims follow.

| Phase | Goal | Scope | Exit test |
| --- | --- | --- | --- |
| 0 Safe to start | Remove defects that would lose renewals or expose data | Real SMS sign-in; recipient check on reminders; renewal duplicate warning; contract date rule; mandatory lost reason; payment on draft invoice; income tile | Re-run the related failed checks; all pass |
| 1 Policies and renewals | A complete book with dates and reminders | Policy as contract, nominee, premium schedule; A1 to A15; B1 to B15; D1 to D8, D11, D14 to D16; H1 to H24 (existing book import); I1 to I15 (consent, privacy, audit); F7 licence block; F13 role scoping and masking; J1 to J11 (new agent setup, quote with commission, policy years, fixed or variable premium, yearly renewal) | Scenarios 1 (existing book import), 3 (renewal ladder), 12 (consent, erasure, audit), 14 (new agent setup) and 15 (quote with commission to policy years) pass; every due date produces the right reminder to the right person |
| 2 Money | Know what is owed and what was paid | C1 to C14, C19, C20; F11; G1 to G18; J12 to J25 (forecast, yearly project report, commission percentage by year, annual plan and targets) | A month of premiums and commission reconciled against two insurer statements with no unexplained difference; scenarios 16 (renewal and yearly report) and 17 (annual plan) pass |
| 3 Service and claims | Handle a claim end to end | E1 to E15; D9, D10, D12, D13 | A cashless health claim and a motor claim closed with documents, clocks and satisfaction scores |
| 4 Growth and scale | Team, cross-sell, portal | B16 to B20; C15 to C18; F1 to F6, F8, F10, F12, F16, F19 to F21 (F9 replaced by J16 to J22 and G15 to G18 moved to Phase 2; F7, F13 moved to Phase 1; F14, F15, F17 replaced by groups H and I; F18 is Phase 0) | Two team members with separate books, a household cross-sell campaign, licence alerts working |

**QA round 4: step-by-step scenarios**

Each scenario is run on a fresh test business and recorded as pass or fail with a screenshot; a failure becomes a defect. Scenarios 1 and 12 run before any real customer data is loaded.

**Scenario 1: Existing book import (run after group H is built)**

Prepare a file of 120 customers and 170 policies. Plant: 8 rows with no valid mobile, 6 duplicate policy numbers, 5 unknown insurers or plans, 5 end dates before start dates, 6 in-force policies with a past end date, 6 rows with health details or full Aadhaar-like numbers, 10 customers already in the CRM as leads, 8 lapsed policies, 12 renewals already past due, and 4 policies whose agent of record is someone else.

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Download the template | Columns, examples and allowed values present |
| 2 | Upload the file | Held in staging; live customer and policy counts unchanged |
| 3 | Map columns with a saved mapping | Suggested matches accepted; mapping saved for reuse |
| 4 | Run the dry run | Counts agree with the planted problems; nothing written |
| 5 | Download the error report; fix and re-upload the failed rows only | Each problem listed with row, column and reason; fixed rows pass |
| 6 | Review the 10 matches | Empty fields fill; conflicts listed for approval; leads become customers with history |
| 7 | Resolve the 5 unknown insurers or plans | Mapped to the catalogue or created |
| 8 | Check the sensitive-data rows | Stripped, reported to the privacy owner, never stored |
| 9 | Approve the import (large, so second approval) | Import runs in the background with progress |
| 10 | Open customers | Family rows form households; nobody is their own parent or grandchild |
| 11 | Open a policy | Holder, insured persons, nominee, dates and premium schedule are right |
| 12 | Open the renewal pipeline | Every in-force policy has a renewal opportunity with the ladder starting from its date |
| 13 | Check the 8 lapsed and 12 overdue policies | Lapsed with a revival list; Overdue marked; call tasks spread across the set number of days |
| 14 | Check expected commission | From the file or rule; policy year worked out from the start date |
| 15 | Check the 4 transfer policies | Transfer pending; no ladder or expected commission until confirmed |
| 16 | Check consent | Consent pending; marketing blocked; only the consent request is sent |
| 17 | Run the nightly job twice | Reminders only for open windows, once each, to the payer |
| 18 | Open the reconciliation report | File rows tie to created, updated, held and rejected counts; premium totals tie |
| 19 | Import the same file again | No duplicates; only empty fields fill |
| 20 | As assistant, search by policy number | Customer, household and history appear; a service request can start at once |
| 21 | Archive the batch | All its records hidden everywhere; nothing deleted |
| 22 | Open the audit trail | Batch, approvals and archive present with who and when |

**Scenario 2: Lead to issued term policy on a phone**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Quick lead entry | Saved in under 30 seconds; a duplicate mobile warns |
| 2 | Book a meeting | Slot set; customer reminder sent |
| 3 | Fact-find and protection gap | Shortfall shown; missing items listed |
| 4 | Quote three insurers | One quote per insurer, side by side |
| 5 | Revise a quote after the sum assured changes | New version; old one expired; no false duplicate warning |
| 6 | Share the chosen quote | Tracked link; opens appear on the deal |
| 7 | Convert to a proposal twice | Converts only once |
| 8 | Collect KYC items | Masked display; missing items listed |
| 9 | Issue the policy | Policy number, dates, premium schedule and renewal opportunity created; deal won on the accepted quote total |
| 10 | Send documents | Customer confirms receipt |

**Scenario 3: Yearly renewal ladder**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Create policies ending in 60, 30, 15, 7, 1 and 100 days | Tasks and messages fire at the right steps only |
| 2 | Open a reminder | It goes to the payer, not only the policyholder |
| 3 | Renew one policy | New term starts the next day; old one Renewed; nominee carried; next renewal opportunity created |
| 4 | Check the duplicate warning | None appears |
| 5 | Enter an end date before the start date | Refused |
| 6 | Let one policy expire unrenewed | Marked Lapsed; urgent task created |
| 7 | Run the nightly job twice | No duplicate tasks or messages |

**Scenario 4: Reminder workflow with and without a recipient**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Publish a reminder with no recipient | Refused with a clear message |
| 2 | Run a test with a recipient | Shows who would receive it |
| 3 | Remove the payer's mobile | Flagged; a task to fix it is created |
| 4 | Add a family member as a recipient | That person receives the reminder |

**Scenario 5: Monthly instalment policy**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Issue a monthly policy | Twelve instalments with due dates |
| 2 | Pay one on time | Clears; proof attached |
| 3 | Pay one late | Shows overdue, then paid |
| 4 | Bounce a mandate | Instalment returns to due; message and task created |
| 5 | Let one pass the grace period | Policy Lapsed; urgent task |
| 6 | Revive it | Dues and declaration tracked; back to In force |
| 7 | Pay one in advance | Credit applied to the next instalment |

**Scenario 6: Commission month**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Open expected commission for ten policies | Computed from the rules by year |
| 2 | Import an insurer statement | Lines matched; unmatched lines listed |
| 3 | Include one short-paid line | Flagged by insurer and policy |
| 4 | Cancel one policy in the free-look period | Clawback created against future commission |
| 5 | Raise the commission invoice to the insurer | Tax and TDS as configured; no false duplicate |
| 6 | Record a part receipt | Allocated; balance right |
| 7 | Open the income dashboard | Payments counted |

**Scenario 7: Health claim with rejection and appeal**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Log the claim intimation | Case opens with priority and a clock |
| 2 | Use the document checklist | Missing items shown; customer reminded |
| 3 | Request cashless pre-authorisation | Two-hour response target tracked |
| 4 | Log an insurer query | Customer notified |
| 5 | Record a rejection | Reason stored; grievance path started |
| 6 | Escalate to the insurer | Reference, response due date and outcome tracked |
| 7 | Try to resolve without a note | Refused |
| 8 | Give a low satisfaction score | Callback task due within 24 hours |

**Scenario 8: Team isolation and masking**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Run five users (agent, POSP, back-office, telecaller, accountant) against twelve actions: see own customers, see another member's customers, see commission, see health fields, open a claim file, export, edit a policy, edit commission, view audit, run an import, send a reminder, delete | Every cell matches the roles table; 60 cells checked |
| 2 | As POSP, search for another member's customer | Not found |
| 3 | Open health fields as back-office, then as the assigned servicer | Masked, then visible |
| 4 | Open a claim file | The view is logged with user and time |

**Scenario 9: Household cross-sell**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Create a household of four with three policies | Household view shows cover by person and class |
| 2 | Record a life event, such as a child born | Opportunity and task created |
| 3 | Run a campaign for that segment | Results counted by policies issued, not clicks |

**Scenario 10: Dashboard against hand counts**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Hand-count commission received, receivable and renewals due for one month | Written down first |
| 2 | Open the dashboard for that month | Figures agree; payments counted; won counted by issue date |

**Scenario 11: Live site (separate test business, owner approval)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Sign in | The code is not shown on screen |
| 2 | Receive the SMS and the email | Both arrive from the real providers |
| 3 | Sign in as owner | Second factor asked |
| 4 | Leave the session idle | It times out |

**Scenario 12: Consent, erasure and audit (run before real customer data)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Import a customer with no consent | Consent pending |
| 2 | Send a consent request by WhatsApp | Sent, delivered, read and replied times logged |
| 3 | Leave two requests unanswered | No third attempt; status stays pending |
| 4 | Take consent on a voice call by key press | Call id, times and outcome logged; recording stored only if the notice said so |
| 5 | Reply that it is a wrong number | Attempt closed; mobile flagged; no more requests |
| 6 | Send a marketing message to a pending customer | Blocked |
| 7 | Send a premium reminder to a pending customer | Follows the rule confirmed by the lawyer; waits by default |
| 8 | Withdraw consent | The next send is blocked |
| 9 | Raise a copy-of-data request | Requester verified; export produced and logged |
| 10 | Raise an erasure request for a customer with an active policy | Preview lists every field with its legal reason; policy and claim records kept; other personal fields erased |
| 11 | Approve the erasure | Record shows Erased on request; instructions to the insurer logged |
| 12 | Try to edit or delete an audit entry as every role | Refused; the attempt is logged |
| 13 | Open a claim file and a health field | Views logged with user and time |
| 14 | Type a 12-digit number in a note | Masked on save |

**Scenario 13: Existing customer service and agent-of-record transfer**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Search an imported policy by number | Customer, household, policies and history appear |
| 2 | Search by vehicle number | The motor policy appears |
| 3 | An unknown customer calls | Quick entry created and marked Unverified; matched later |
| 4 | Start a service request on an imported policy | Case linked to customer and policy |
| 5 | Start a new opportunity from the household record | Details prefilled; old policy can be marked replaced |
| 6 | Open a Transfer pending policy | No ladder or expected commission yet |
| 7 | Record the insurer's confirmation | Ladder starts; commission counts from the confirmed date |

**Scenario 14: New agent setup to first quote on a phone**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Create the business and sign in | Real SMS code; no code shown on screen |
| 2 | Enter the agent profile and licence | Validity and the 60-day alert date stored |
| 3 | Add insurers and appointments | Plans of insurers not appointed are hidden |
| 4 | Enter the commission rate card: first year, second year and renewals, by plan and premium-paying term | Saved as settings; rate shown per year |
| 5 | Mark each plan Fixed, Stepped or Variable, with the estimate rule for Variable | Saved |
| 6 | Add a lead and take it to a quote | Setup wizard shows complete |
| 7 | Make the first quote | Premium and commission shown for each insurer |

**Scenario 15: Quote with commission to policy years**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Quote three insurers for the same cover | Each shows premium, first-year commission and renewal-year commission |
| 2 | Share the quote with the customer | The customer view shows premium and features, no commission |
| 3 | Choose one and convert to a proposal | Plan, premium, premium type, term, premium-paying term and commission rule carried over |
| 4 | Issue the policy | The policy year table is created for the whole term |
| 5 | Check a Fixed term policy | Same premium each year; zero after the premium-paying term; commission percentage per year from the rate card |
| 6 | Check a Variable health policy | Years after the first labelled Estimated with the assumed increase; renewal date shown for each year |
| 7 | Change the sum assured after issue | The table recalculates; old figures stay in the audit trail |
| 8 | Check the expected commission total | Matches a hand calculation to the rupee |

**Scenario 16: Renewal, actual commission and yearly report**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Renew the health policy for year 2 with an actual premium different from the estimate | The estimate is replaced by the actual; the variance is shown |
| 2 | Receive year 2 commission at a different rate | The actual percentage for the year is stored; the gap from expected is flagged |
| 3 | Open commission percentage by year | Effective percentage for years 1, 2 and 3 by insurer and product matches hand figures |
| 4 | Open the persistency report | Share renewed by issue year, by insurer and product |
| 5 | Generate the yearly project report | Five yearly columns with new policies, renewals, premium, commission percentage, costs and profit; Estimated parts labelled |
| 6 | Add firm and Estimated figures | Totals agree with the policy year tables |

**Scenario 17: Annual plan for 100 new customers**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Create the plan: 100 new customers, 40 term, 30 health, 30 motor, average premiums, commission by year, conversion rates, renewal rate | Planned year 1 premium and commission and year 2 commission match the worked example; year 2 labelled Estimated |
| 2 | Open the back-calculation | 800 leads and 400 quotes for the year, with the working shown and monthly figures |
| 3 | Split the plan by month and by person | Parts add up to the total |
| 4 | Issue ten policies | Actual against plan counts by issue date, not expected close date |
| 5 | Fall behind plan by more than the set percentage | Shortfall alert; pipeline coverage shown |
| 6 | Re-plan mid-year | A new version is saved; variance shown against the original and the latest |
| 7 | Open the project report | The plan is the base case with actual alongside |
| 8 | Set an income goal instead of a customer count | Customers needed are worked out from the average commission |

**Scenario 18: Premium, commission and profit in both modes**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Enter ten policies in both modes with different commissions | Each shows total to collect, amount to remit and profit |
| 2 | Open a Mode 1 policy | Collect and remit lines show zero; commission and profit still shown |
| 3 | Leave a Mode 2 premium unremitted past its limit | Alert to the agent; shown as a liability, not income |
| 4 | Change a premium | All amounts recalculate; old figures stay in the audit trail |
| 5 | Compare policy profits, monthly statement, cash position and the first-year report section with a hand calculation | All agree to the rupee |
| 6 | Open the report with an unreconciled figure | Marked draft |

If you also sell general insurance for a company or hold a broker licence, tell me and I will add the extra flows (premium collection, quote for several clients, brokerage statements). I can also turn phase 1 into a detailed field-level spec.
