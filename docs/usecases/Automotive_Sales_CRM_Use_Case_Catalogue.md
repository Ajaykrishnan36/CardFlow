# Automotive Sales CRM — Use Case Catalogue (Bike & Car Dealerships)

Oct 11, 2026 · @Gova

## Verdict

The CRM can already carry a dealership from enquiry to paid invoice. It cannot yet run one: the vehicle itself (stock, VIN, registration, exchange, finance, insurance, delivery) is not modelled, and four dashboard and workflow defects would mislead a dealer on day one.

**What the evidence says.** Across the three QA rounds, 286 of 303 checks passed, 11 failed and 6 were gaps or notes. The bike-dealer round (report 2) proved individual versus business customers, staff privacy, discount approval, a mechanic on leave being skipped in booking, and a workflow email on case creation.

**Three things to settle before any dealer goes live:**

1. Sign-in codes appear on the login page of the test copy. The tester did not check the live site, so confirm what live does. If it shows the code, anyone who knows a staff number can sign in as that person.
2. Dashboard "Won" and "Income" read ₹0 on the day a bike is sold and paid. A dealer reads these every evening.
3. The Appointments list does not show the mechanic or the time, so the workshop has no usable job board.

**What this document gives you.** 265 use cases in eleven groups (A to K) for bike and car dealerships, each marked with how close the CRM is today, then a phased build order and a test pack for the next QA round.

**Fit column, used in every table**

| Mark | Meaning |
| --- | --- |
| Ready | Passed in the QA reports |
| Partial | A base object exists; automotive fields or rules are missing |
| Untested | Present in the data model, never exercised in QA |
| Fix | QA found a defect that affects this case |
| New | Nothing in the data model covers it |

**The journey the catalogue follows**

1. Enquiry arrives (walk-in, call, portal, card scan)
2. Follow-up, test ride or drive, quotation
3. Booking with advance, exchange valuation, finance and insurance
4. Stock allocation, pre-delivery inspection, invoice, registration
5. Delivery and handover
6. Service, warranty, accessories, insurance renewal
7. Referral, upgrade, and the next purchase

**Assumptions.** India, INR, GST and RTO rules, because the tested business is a Hero dealer in Tamil Nadu. Tax rates and legal limits below are examples to confirm with a tax advisor; build them as settings, never as fixed numbers. Say so if you want another market.

## QA review: what holds, what breaks, what is missing

The three reports agree on the core. They log 11 failed checks plus several notes; the table lists those that matter to a dealership. Reports are R1 (lead to case), R2 (Hero bike dealer) and R3 (Sales Overview diagram), with the step number from each log.

**Proven and reusable for automotive**

- Individuals as contact-only or "Individual" accounts, dealers and fleets as Business accounts (R2 #29 to #41)
- Price book to quote with discount to order to invoice to part and full payment, totals right to the paisa (R1 #53 to #64, R2 #42 to #49)
- Tiered discount approval that blocks the order until approved (R3 #88 to #93)
- Sold bike registered as an asset with chassis number and warranty (R2 #50)
- Staff isolation: own leads only, no payments, no user admin, no other staff's cases (R2 #56 to #59, #74)
- Service chain: SLA by priority, work order, appointment that skips a mechanic on leave, double-booking refused, invoice blocked until work is complete (R2 #61 to #87)
- Workflow email on case creation, campaign with unsubscribe, territories, forecast quotas, campaign ROI (R2 #88 to #101, R3 #51 to #86)
- Another business cannot see or link to a business's records (R3 #109, #110)

**Defects that matter to a dealership**

| # | Defect (source) | Why a dealer cares | Fix |
| --- | --- | --- | --- |
| 1 | Login code shown on the page (all three, test copy only) | Staff accounts hold customer PAN, licences and loan papers | Connect an SMS provider; confirm live behaviour; add an owner MFA option |
| 2 | "Won" counts by expected close date (R1 #83) | Today's deliveries show ₹0 in this month | Count on the won or delivered date |
| 3 | "Income" ignores payments (R1 #84, R2 #104) | Daily collections are the dealer's cash view | Include received payments, or rename the tile |
| 4 | Won value is the deal amount, not the accepted quote (R2 note) | Overstates sales by the discount (₹99,028.68 against ₹97,075.25 invoiced) | Roll the accepted quote total into the deal |
| 5 | Appointments list hides mechanic and time (R2 note) | No workshop board | Add mechanic, bay and start time columns |
| 6 | Notify step with no recipient publishes and reports "ok" (R2 #97) | A service reminder could silently reach nobody | Refuse to publish without a recipient |
| 7 | Deal can be Closed lost with no reason (R1 #51) | Lost-sale reasons are the best sales insight | Make reason and competitor model mandatory |
| 8 | Payment on a draft invoice leaves it Draft (R1 note) | Booking advances are taken before invoicing | Move the invoice to Partially paid on payment |
| 9 | False duplicate warning on a revised quote or renewed contract (R1 #85) | Noise on every re-quote and AMC renewal | Ignore a record's own earlier version |
| 10 | Campaign membership not copied to the contact on lead conversion (R3 #38) | Festival campaign attribution breaks at conversion | Carry membership across |
| 11 | Contract can end before it starts (R1 #67); account can be its own grandchild (R3 #7) | AMC dates and dealer-group trees | Add both validations |

Minor: no Automotive in the Create-a-business industry list (R2 #105), order and invoice named "Quote - ..." (R1 #86), phone-added users show "-" under name (R2 note).

**Gaps against the Sales Overview diagram.** There is no contact "reports to" link, no primary-campaign field or campaign list on a deal, no partner roles, and no dedicated partner-on-a-deal link (R3 #16, #41, #63, #65, #112). Partner roles matter here: banks, insurers, the OEM and sub-dealers are all partners. R3's header counts five gaps while its diagram table shows four; two of the five checks cover the same box.

**Never tested.** The live site, real SMS and email, phone layout, business-card scanning, file uploads, CSV import and the owner console. Showroom staff work on phones and walk-ins arrive as cards, so phone layout and card scanning should lead the next round.

**One design risk from the ER diagrams.** Links between objects, such as a quote to its account or an invoice to its order, are lookup fields checked by the server and are not database foreign keys (131 of them, Appendix B). A vehicle linked to a customer, booking and registration will rely entirely on those checks. Test deleting and merging records that others point to.

## Data map: dealership concept to CRM object

Most dealership records can follow the pattern the CRM already uses for its 31 objects: a definition plus rows in crm.object\_records, with a lookup field to each related record. The ER diagrams show no stock or inventory table, so vehicle stock is the main new build; parts stock is deferred for now. Confirm with the team that new objects can be added without a schema migration.

| Dealership concept | Exists today | Build or extend |
| --- | --- | --- |
| Customer (person, company, fleet) | accounts (kind Individual or Business), contacts, relationships | PAN, GSTIN, date of birth, preferred language, family members, consent flags per channel |
| Model, variant, colour | catalog\_items, price\_books, price\_book\_entries | Segment, fuel or EV, engine cc, battery kWh, model to variant to colour tree, tax class per model |
| Vehicle in dealer stock | Nothing | New object vehicle\_stock: VIN, engine number, colour, key number, location, status, age, landed cost |
| Vehicle owned by a customer | assets (serial number, parent asset, contract, subscription) | Registration number, VIN link, purchase date, odometer, insurance and PUC expiry, next service due, owner history |
| Enquiry | leads (rating, source, lost reason) | Model interest, budget, purchase timeline, finance and exchange flags, current vehicle |
| Deal | opportunities, stages, forecast category | Automotive stages, promised delivery date, booking reference, competitor model |
| Test ride or drive | appointments, service\_resources | Demo vehicle as a resource, licence check, indemnity consent, odometer, feedback |
| On-road quotation | quotes, line\_items, pricing\_rules | Fixed price components: ex-showroom, road tax, insurance, handling, HSRP, TCS, accessories, exchange |
| Booking advance | payments, payment\_allocations, refund\_allocations | New object booking: receipt, variant and colour, allocated VIN, cancellation rule |
| Exchange vehicle | Nothing | New objects exchange\_valuation and used stock link |
| Finance application | Nothing; partner accounts exist | New object finance\_application per lender; lender as a Partner account with a role |
| Insurance policy | Nothing | New object insurance\_policy: insurer, premium, IDV, cover note, expiry, renewal state |
| Registration and RC | Nothing | New object registration: stage, RTO, application and HSRP numbers, RC dispatch |
| Pre-delivery inspection and handover | tasks, work\_orders | Checklist template, delivery note, customer sign-off, photos |
| Service job | cases, work\_orders, appointments, sla\_policies | Job card fields, estimate approval, bay, technician time, warranty or paid split |
| Warranty, AMC, service pack | entitlements, entitlement\_usage, contracts, subscriptions | Rules by years and kilometres; pack redemption per visit |
| Parts | catalog\_items | Deferred for now: stock levels, bin, reorder point, issue and return against job cards |
| Dealer network | accounts typed Reseller or Partner, territories, territory\_assignments | Sub-dealer targets, stock transfer, lead sharing |
| Schemes and discounts | approval\_rules, pricing\_rules | Scheme validity, stacking rules, claim tracking against the OEM |
| Branch | role\_assignments carries branch\_ids and product\_ids | Confirm where branches are stored; no branch table appears in the ER diagrams |

## Actors and access

Only two access levels were tested (Super Admin and Staff). A dealership needs about sixteen roles, scoped by branch, and the team-lead and branch-manager views are untested.

| Role | Sees | Does | Must not |
| --- | --- | --- | --- |
| Dealer principal | Everything, all branches | Approves large discounts, cancellations, write-offs | Nothing blocked; every action audited |
| Branch or sales manager | Own branch, team pipeline | Assigns leads, approves discounts to a limit, reviews forecast | See other branches' customers |
| Sales executive or consultant | Own leads, deals, bookings | Follows up, quotes, books, hands off to F&I | Open a colleague's lead; record payments |
| Lead desk or telecaller | New leads, call tasks | Calls, qualifies, books test rides | Edit prices or discounts |
| Receptionist | Walk-in form, today's appointments | Registers visitors, books test rides and service | See deal values or finance papers |
| F&I executive | Deals with finance or insurance | Runs lender applications, issues insurance, tracks payout | Change vehicle price |
| Used-vehicle evaluator | Exchange valuations | Inspects, grades, offers a value | See the customer's finance file |
| Stock and yard controller | Vehicle stock, allocations | Receives, moves, allocates vehicles | See customer PAN or income documents |
| Registration executive | Registration queue | Files, tracks and hands over RC | Edit invoices |
| Cashier or accounts | Invoices, payments, refunds | Records receipts, issues credit notes | Alter a posted invoice |
| Service advisor | Service customers, job cards | Books jobs, estimates, closes and invoices | Approve warranty claims alone |
| Technician | Own assigned jobs | Clocks time, updates status, requests parts | See prices or customer phone numbers |
| Parts counter | Parts stock, issues | Sells and issues parts | Edit vehicle sales records |
| OEM area manager | Read-only dealer reports | Views retail and stock numbers | Open any customer record |
| Sub-dealer | Own leads, quotes, stock requests | Enters leads, requests vehicles | See the main dealer's pipeline |
| Customer | Own vehicles, bookings, service history | Books service, downloads invoices, tracks delivery | See anyone else's data |
| Team lead | Own and team leads, team pipeline | Reassigns leads, approves discounts to a limit, reviews follow-ups | See other teams or branches |
| Privacy owner | Consent register, requests, grievances, audit trail, masked data only | Runs erasure, access and correction requests, breach register, legal holds | Edit sales records or prices; edit or delete an audit entry |

Rules to prove in QA: record ownership by person, team and branch; a manager sees a team, not the company; masked PAN and licence numbers for every role except F&I and the principal; every discount, cancellation and refund recorded in the audit log.

## A. Enquiry to booking

The lead, quote and approval engine works; the gaps are automotive capture fields, test rides, an on-road price structure and a booking that is not an order.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| A1 | Walk-in enquiry at reception (receptionist) | Form under 30 seconds: name, mobile, model of interest. Same mobile again warns and links to the earlier lead | Partial |
| A2 | Visiting card scan for a fleet or dealer prospect (sales executive) | Photo creates a business account and contact; lead created from the same screen | Untested |
| A3 | Portal and digital leads: OEM site, own website, social lead ads, listing sites (lead desk) | Import by CSV, webhook or API with source tagged; duplicates merged; owner assigned within 1 minute | Untested |
| A4 | Phone, missed-call and WhatsApp enquiry (lead desk) | Each call logs as a communication against the lead; a missed call opens a lead | Partial |
| A5 | Returning enquirer (lead desk) | A mobile seen 40 days ago attaches to the old record, marks it returning, tells the old owner | Partial |
| A6 | Assign by rule (manager) | Round-robin, or by model, language, branch or territory; a lead untouched for 15 minutes goes to the next person | Untested |
| A7 | First-response clock on leads (manager) | Target 5 minutes for digital leads; breach alerts the manager. SLA timers exist only for cases today | New |
| A8 | Capture needs (sales executive) | Usage, budget, timeline, current vehicle, finance yes or no, exchange yes or no, decision maker, colour | New |
| A9 | Lead temperature (system) | Hot when budget confirmed, timeline under 15 days and test ride done; the list sorts by it | Partial |
| A10 | Follow-up cadence (system) | Tasks on day 0, 1, 3, 7, 15 and 30; a customer reply or booking stops the rest; overdue list per manager | Untested |
| A11 | Book a test ride or drive (receptionist) | Slot offered only when a demo vehicle and a person are free; a vehicle in service or a person on leave is skipped | Partial |
| A12 | Pre-checks for a test drive (executive) | Valid licence photo and expiry, age, signed indemnity; expired licence blocks the drive | New |
| A13 | Run the test ride (executive) | Start and end odometer, route, rating, comments; a follow-up task within 2 hours | New |
| A14 | Doorstep demo for a bike or EV (executive) | Address, route and executive booked; same checks as A12 | Partial |
| A15 | Share brochure and comparison (executive) | Sent by email or WhatsApp with a tracked link; opens appear on the lead | Partial |
| A16 | On-road quotation (executive) | Lines for ex-showroom, road tax, insurance, handling, HSRP, TCS, accessories and extended warranty; tax by model class; total correct to the paisa | Partial |
| A17 | Compare variants in one quote (executive) | Up to three variants side by side; customer chooses one and the quote converts with that choice | New |
| A18 | Revise a quote (executive) | New version, old one marked expired, no false duplicate warning | Fix |
| A19 | Discount approval (manager, principal) | Up to 2 percent executive, up to 4 percent manager, above that principal (limits are settings); order blocked until approved | Partial |
| A20 | Schemes and offers (manager) | OEM, corporate, loyalty and festival offers with start and end dates; stacking rules; scheme visible on the quote | Untested |
| A21 | Take a booking advance (cashier) | Receipt number, mode, amount; payment allowed before invoice and the invoice then shows Partially paid, not Draft | Fix |
| A22 | Booking with colour, variant and promised date (executive) | A booking is its own record, separate from the sales order; the promised date drives reminders | New |
| A23 | Waiting list when no stock (stock controller) | Booking queues by date; allocation follows the queue; customer sees the estimated week | New |
| A24 | Amend a booking (executive) | Change variant or colour; price difference recalculated; approval if the discount changes | New |
| A25 | Cancel a booking and refund (cashier, principal) | Rule such as non-refundable fee after 7 days; refund approved and tied to the original receipt | Untested |
| A26 | Lost sale with reason (executive) | Reason, competitor make and model, and price gap are mandatory; lost-to-competitor report works | Fix |
| A27 | Win back a lost lead (system) | After 60 days a workflow offers the current scheme and reopens the lead | Partial |
| A28 | Hand a deal to F&I and the evaluator (executive) | One click adds them to the deal team with the right access; they see only their part | Partial |
| A29 | Group or family purchase (executive) | Two vehicles on one deal, one account, one payment plan | Partial |
| A30 | Customer with several vehicles (executive) | A contact sees every vehicle owned; the quote knows what they already have | Partial |
| A31 | Same mobile enquires at two branches (lead desk) | One company-wide customer record; the second branch sees an existing-customer flag and the owner, not the first branch's notes or quotes; a lead owner rule decides who keeps it | Partial |
| A32 | Tasks created by stage (system) | Moving a lead or deal to a stage (enquiry, test ride, decision, booking, delivery) creates that stage's tasks and checklist automatically, with owners and due dates | Partial |
| A33 | Test ride or drive checklist (executive) | Before: slot confirmed, licence seen, indemnity signed, vehicle fuelled and cleaned. During: odometer, route. After: rating, objections, next step agreed. A required item left open blocks closing the test ride | New |
| A34 | Decision-making checklist (executive) | Budget confirmed, family consulted, variant and colour fixed, finance route chosen, exchange valued, competitor compared, delivery date agreed; each item has an owner and date; open items show as blockers on the deal | New |
| A35 | Follow-up outcome log (executive) | Every call or visit records an outcome (connected, not reachable, callback asked, visited, promised date) and the next follow-up date is mandatory before saving | Partial |
| A36 | Overdue and stuck-deal alerts (manager) | A task overdue by a day, or a deal with no activity for 7 days, alerts the executive and then the manager; the manager's list shows who and how long | Partial |
| A37 | Checklist templates (manager) | Manager edits templates per stage and per vehicle type (bike, scooter, EV, car, SUV) without a developer; a change applies to new leads only | New |

Bike note: A1, A11 and A21 run dozens of times a day, so the screens must work on a phone with two taps per field.

Tax note: GST on vehicles changed in September 2025 (two-wheelers up to 350cc and small cars moved to 18 percent; larger vehicles to 40 percent, with the compensation cess removed). The R2 test used 28 percent. Keep rates per model and date, and confirm current rates before launch.

## B. Exchange, finance and insurance

None of this exists in the data model, and it is where a dealership earns most of its margin beyond the vehicle. Lenders and insurers are partners, so the missing partner roles from R3 block it too.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| B1 | Declare an exchange vehicle (executive) | Make, model, year, registration, kilometres, owners, expected price; flag shows on the deal | New |
| B2 | Evaluate the exchange vehicle (evaluator) | Checklist, up to 20 photos, RC and insurance copies, grade; offer valid for 7 days | New |
| B3 | Exchange value in the quote (executive) | Appears as a negative line; a bonus above the evaluator's value needs manager approval; invoice nets it off | New |
| B4 | Competing bids (evaluator) | Customer's outside bid logged; one re-bid allowed; outcome recorded | New |
| B5 | Take in the exchange vehicle (stock controller) | Transfer forms, RC, lender no-objection if hypothecated; vehicle enters used stock with cost | New |
| B6 | Loan pre-check (F&I) | Income band, employment, consent to a credit check; no full credit report stored | New |
| B7 | Send one application to several lenders (F&I) | Per lender: documents, status, rate, tenure, EMI, fee; one lender accepted, others closed | New |
| B8 | EMI calculator in the quote (executive) | Down payment, rate, tenure give EMI; three scenarios side by side | New |
| B9 | Collect KYC and income documents (F&I) | Checklist per lender; upload from phone; missing items create a task for the customer | Untested |
| B10 | Sanction, down payment, delivery order and disbursal (F&I) | Sanction letter stored; vehicle cannot be delivered until the delivery order or down payment is recorded | New |
| B11 | Finance rejected (F&I) | Try another lender, reduce the loan, or close as lost with reason "finance rejected" | New |
| B12 | Lender payout tracking (accounts) | Expected against received commission per lender; unpaid items aged | New |
| B13 | Sell insurance at billing (F&I) | Insurer, premium, IDV, owner-driver cover, add-ons; premium appears as a quote line; cover note issued | New |
| B14 | Insurance renewal pipeline (system) | Tasks at 90, 60, 30, 15 and 7 days before expiry; lapsed policies listed; policies bought elsewhere recorded | New |
| B15 | Insurance claim assist (service advisor) | Case with surveyor, estimate, cashless or reimbursement, linked to the work order | Partial |
| B16 | Extended warranty, service pack, roadside plan at billing (F&I) | Sold as items; each creates an entitlement with start and end by years and kilometres | Partial |
| B17 | Insurer payout tracking (accounts) | Same as B12 for insurers | New |
| B18 | Hypothecation endorsement (registration executive) | Lender name entered at registration; endorsement tracked to RC | New |

Compliance note: keep identity and income documents in restricted storage with masked display and a retention period; treat them as personal data under India's DPDP Act.

## C. Stock, delivery, registration and accessories

Invoice, payment and allocation tables already exist, so billing is close. Stock, inspection, registration and handover are new.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| C1 | Order from the OEM and track dispatch (stock controller) | Ordered, dispatched, in transit, arrived; expected date per vehicle | New |
| C2 | Receive a vehicle (stock controller) | VIN and engine number scanned; damage noted with photos; location set; a duplicate VIN is refused | New |
| C3 | Stock view and ageing (manager) | By model, variant, colour and days in stock; over 60 days flagged; free against allocated | New |
| C4 | Allocate a VIN to a booking (stock controller) | First booking first; override needs approval; one vehicle cannot be allocated twice | New |
| C5 | Transfer between branches or dealers (stock controller) | Delivery challan or e-way bill, receiving branch confirms, status follows the vehicle | New |
| C6 | Demand against supply (manager) | Open bookings per variant against stock and inbound; shortfall feeds the next OEM order | New |
| C7 | Pre-delivery inspection (technician) | Checklist per model; a failed item blocks delivery; passed date stored | Partial |
| C8 | Final invoice (cashier) | Built from the order; booking advance applied through payment allocation; GST per model; discounts shown | Partial |
| C9 | Collect the balance (cashier) | Split across cash, UPI, card, lender disbursal and exchange; invoice turns Paid only when the sum matches | Partial |
| C10 | Schedule delivery (delivery coordinator) | Slot, bay and executive; customer gets a reminder; calendar event created | Partial |
| C11 | Handover checklist (delivery coordinator) | Keys, manual, tool kit, helmets or accessories bundled, warranty card, first-service date explained | New |
| C12 | Customer sign-off (executive) | Signature or code on the delivery note, one photo; delivery stage set to Delivered | New |
| C13 | Late-delivery alert (system) | Booking past its promised date triggers a message to the customer and an alert to the manager | Untested |
| C14 | Registration tracking (registration executive) | Stages from temporary registration to RTO filing, tax paid, HSRP, RC issued; a clock per stage | New |
| C15 | RC handover (registration executive) | RC given or posted with proof; registration number written back to the vehicle and the customer's record | New |
| C16 | Documents pending (registration executive) | Missing address proof or signature holds registration and creates a task for the customer | New |
| C17 | Accessories on a deal (executive) | Chosen at quote, fitted through a work order before delivery, billed on the invoice; discount cap by approval | Partial |
| C18 | Accessory-only sale (parts counter) | Counter sale with invoice and GST; links to the customer's vehicle if known | Partial |
| C19 | Cancel after allocation (principal) | Vehicle returns to stock, refund processed, lost reason recorded, allocation history kept | New |
| C20 | Wrong vehicle or colour delivered (service advisor) | Case with owner, resolution and cost; blocks the delivery stage until closed | Partial |
| C21 | Fleet or bulk delivery (delivery coordinator) | One invoice, many VINs, a delivery note per vehicle, registrations tracked individually | New |

Cash note: Indian tax law limits cash receipts (Section 269ST, ₹2 lakh) and adds TCS on high-value vehicles; build both as checks in C9 and confirm thresholds before launch.

## D. Service, workshop, warranty and recalls

Service is the strongest existing area: SLA clocks, work orders, appointments with leave, and invoice gating all passed. The gaps are job-card detail, estimates, parts and OEM claims.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| D1 | Book a service slot (receptionist, customer) | Slot offered only when a technician and bay are free; leave respected; double booking refused | Ready |
| D2 | Free-service schedule (system) | First, second and third free services set by OEM days and kilometres on the vehicle record; reminder before each | Partial |
| D3 | Service reminder campaign (system) | Due in 7 days, overdue at 30 and 60; a reminder with no recipient cannot be published | Fix |
| D4 | Walk-in job card (service advisor) | Registration number finds customer and vehicle; complaints, odometer, fuel level, photos, valuables noted | Partial |
| D5 | Estimate and customer approval (service advisor) | Service cost, labour cost and tax listed; approval by link or code; extra work needs fresh approval before it starts | Partial |
| D6 | Job allocation and workshop board (supervisor) | Jobs by technician and bay on a timeline; list shows mechanic and start time | Fix |
| D7 | Technician time (technician) | Clock on and off per job; productive hours against billed hours | New |
| D8 | Parts against a job card (technician, parts counter) \[deferred: no parts stock for now\] | Request, issue, return; stock reduces; a job cannot close with parts unissued | New |
| D9 | Final check and ready message (supervisor) | Quality check passed, customer told the vehicle is ready, pickup slot offered | Partial |
| D10 | Invoice a job (service advisor) | Blocked until work is complete; split between customer, warranty and goodwill | Partial |
| D11 | Warranty claim to the OEM (service advisor) | Claim lines, failed part held, claim status, amount received; rejected claims reviewed | New |
| D12 | Check warranty on arrival (service advisor) | Valid by date and kilometres, shown at job card creation, extended warranty included | Partial |
| D13 | Service pack or AMC sale and use (service advisor) | Prepaid pack of visits; each visit consumes one; balance visible; expiry reminders | Untested |
| D14 | Breakdown or roadside call (service advisor) | Critical priority with a 10-minute response clock; tow dispatch; case closes with cost | Ready |
| D15 | Pickup and drop (service advisor) | Driver assigned, two appointments, vehicle condition photos at both ends | New |
| D16 | Recall or service campaign (manager) | OEM bulletin lists affected VINs; owners contacted; completion tracked per vehicle | New |
| D17 | Repeat repair and complaint (service advisor) | Same issue within 30 days flagged; escalated with an SLA; resolution text required before closing | Partial |
| D18 | Service feedback (system) | Rating after delivery; a low score creates a callback task within 24 hours | Partial |
| D19 | Upsell at the job card (service advisor) | Tyres, battery, coating, wheel balance offered by vehicle age and kilometres | Partial |
| D20 | Vehicle history (service advisor) | One timeline of jobs, parts, claims, owner changes and insurance | Partial |
| D21 | Change of ownership (service advisor) | Vehicle moves to the new owner's account, history stays, old owner's reminders stop | New |
| D22 | Service camp or mobile van (manager) | Camp event with expected vehicles; walk-ups become jobs and leads | Partial |
| D23 | EV service (technician) | Battery health reading, software update note, charger issues on the job card | New |
| D24 | Fleet service contract (manager) | Contract with a visit schedule, priority slots and one monthly invoice; renewal without false duplicate warning | Fix |

## E. Parts, used vehicles, dealer network and fleet

Territories, partner accounts and contracts already pass QA, so the network and fleet cases are mostly rules on existing objects. Parts and used vehicles both need stock.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| E1 | Parts catalogue by model (parts counter) \[deferred\] | Part number, fitment by model and year, supersession, price and tax class | Partial |
| E2 | Parts stock and reorder (parts counter) \[deferred\] | Quantity by bin; reorder point raises a purchase order; receipt updates stock | New |
| E3 | Counter sale of parts (parts counter) \[deferred\] | Invoice, payment, optional link to the customer's vehicle | Partial |
| E4 | Parts return and credit note (parts counter) \[deferred\] | Return within a set period; credit note against the invoice; stock restored | Untested |
| E5 | Buy a used vehicle outright (evaluator) | Seller identity, RC, valuation, payment, ownership transfer papers | New |
| E6 | Refurbish a used vehicle (supervisor) | Job card with cost added to stock value; ready-for-sale status | New |
| E7 | Price and list a used vehicle (manager) | Cost, refurbishment, margin and asking price; photos; listed on own site and listing sites | New |
| E8 | Match used stock to enquiries (lead desk) | New or old lead wanting a model and budget gets a suggestion | New |
| E9 | Sell a used vehicle (executive) | Quote, booking, warranty, transfer paperwork, GST on margin where applicable | New |
| E10 | Certified pre-owned programme (manager) | Inspection report, certificate, warranty entitlement | New |
| E11 | Sub-dealer or satellite showroom leads (manager) | Territory rules route leads; targets per sub-dealer; commission on delivery | Ready |
| E12 | Supply vehicles to a sub-dealer (stock controller) | Invoice at dealer price, stock moves, outstanding tracked | New |
| E13 | Lead transfer to another dealer (executive) | Customer lives elsewhere; lead sent with consent; referral fee recorded | New |
| E14 | OEM monthly reporting (principal) | Retail, wholesale, stock and service numbers exported in the OEM format | New |
| E15 | Several branches and a dealer group (principal) | Branch data separated, group totals on one view; parent and child accounts without loops | Fix |
| E16 | Fleet enquiry of 10 or more vehicles (executive) | Several contacts with roles, multiple quote versions, one deal | Ready |
| E17 | Corporate tie-up (manager) | Company discount programme, employee list, offer visible to listed employees only | Partial |
| E18 | Government or institutional tender (manager) | Tender number, validity, deposit, document set, one approved price | New |
| E19 | Delivery-partner and taxi fleets, including EV (executive) | Bulk finance, charger advice, subsidy or incentive handling where applicable | New |
| E20 | Rental and leasing customers (executive) | Contract with term, monthly invoice, vehicle swap on breakdown | Partial |

## F. Retention, marketing and referrals

Campaigns, unsubscribe handling, workflows and campaign ROI work. What a dealer needs on top is a vehicle-driven calendar: reminders keyed to purchase date, service due, insurance expiry and loan end.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| F1 | Thank-you and first feedback (system) | Message on delivery day; rating request at day 3 and day 30; a score of 6 or below creates a callback within 24 hours | Partial |
| F2 | Referral programme (executive) | Referred lead records who referred; reward released on the referred customer's delivery | Partial |
| F3 | Birthday and vehicle anniversary greetings (system) | Dates from the contact and the delivery record; sent once, in the customer's language | Partial |
| F4 | Festival campaigns (marketing) | Regional festival calendar, offer, audience by model owned; results by delivery, not clicks | Partial |
| F5 | Upgrade or exchange campaign (marketing) | Owners with a vehicle of a set age or kilometres get a trade-in estimate and a test ride offer | Partial |
| F6 | Insurance renewal campaign (system) | Same as B14 with a price quote in the message | New |
| F7 | Accessory upsell by model (marketing) | Owners of a model offered its accessory list with a tracked link | Partial |
| F8 | Loan-ending campaign (marketing) | Customers whose loan ends within 90 days get an upgrade offer | New |
| F9 | Win back lapsed service customers (system) | No visit in 12 months starts a sequence with a service offer | Partial |
| F10 | Loyalty points (cashier) | Points on service spend and referrals; redeem on parts and accessories; balance on the customer record | New |
| F11 | Consent and channels (marketing) | Opt-in per channel (call, SMS, WhatsApp, email), recorded with date and source; unsubscribe respected; regulatory sender registration for SMS | Partial |
| F12 | Launch or test-ride event (marketing) | Invitations, RSVP, attendance, on-the-spot leads, cost against the campaign | Partial |
| F13 | Review request (system) | After a high score, one request for a public review; never after a low score | New |
| F14 | Complaint recovery (manager) | Low-score customer gets a manager call, an action and a closing note within 48 hours | Partial |
| F15 | Customer app or portal (customer) | Vehicles, due dates, invoices, delivery status, service booking | New |
| F16 | Source and campaign attribution (marketing) | Deliveries credited to first, last and shared touch; membership survives lead conversion | Fix |
| F17 | Segments and lists (marketing) | Filter by model, area, vehicle age, finance state, last service; saved for reuse | Partial |
| F18 | Telephony (lead desk) | Click to call, call log and recording attached to the record, missed calls become tasks | New |

## G. Payments, reports, security and administration

Payment allocation, approvals, forecast quotas and audit tables exist. The dealer-facing dashboards and the security basics are what need work.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| G1 | Receipts in every mode (cashier) | Cash, UPI, card, cheque, transfer, lender disbursal, exchange adjustment; one invoice, many payments; allocation to invoices | Ready |
| G2 | Cash and PAN checks (cashier) | Warning above the legal cash limit; PAN or Form 60 asked where the rules require it | New |
| G3 | TCS on high-value vehicles (cashier) | Calculated on the invoice when the threshold applies; shown as its own line | New |
| G4 | Credit and debit notes (accounts) | Scheme claims, price corrections, returns; each tied to an invoice and reflected in the balance | Untested |
| G5 | Invoice numbering (accounts) | Series per branch and financial year, no gaps, no edits after posting | Untested |
| G6 | Accounting export (accounts) | Invoices, receipts and credit notes to the accounting package; GST return data | New |
| G7 | Receivables (accounts) | Customer balance, lender disbursals due, OEM claims due, insurer payouts due, aged | Partial |
| G8 | Refunds (cashier, principal) | Approved, paid by mode, tied to the original receipt, shown on the account statement | Untested |
| G9 | Daily sales report (manager) | Enquiries, test rides, bookings, deliveries and cancellations by branch and person; counts on the delivery date | Fix |
| G10 | Funnel by source and model (manager) | Enquiry to test ride to booking to delivery, with drop-off at each step | New |
| G11 | Lost-sale analysis (manager) | By reason, competitor and price gap; trend by month | Fix |
| G12 | Stock and delivery pending (manager) | Ageing, allocated but undelivered, bookings past promise date | New |
| G13 | Finance and insurance penetration (principal) | Percent of deliveries financed and insured in-house; payout per vehicle | New |
| G14 | Exchange and used-vehicle margin (principal) | Valuations against conversions; margin per used vehicle | New |
| G15 | Workshop dashboard (manager) | Jobs per day, bay use, repeat repair rate, SLA met, revenue per job | Partial |
| G16 | Targets and scorecards (manager) | Monthly target by person and model; attainment and gap | Ready |
| G17 | Role and branch scoping (principal) | Executive sees own, manager sees team or branch, principal sees all; proven by test users in each role | Partial |
| G18 | Protect sensitive data (principal) | PAN, licence and income files masked and role-limited; access logged; deletion on request | New |
| G19 | Secure sign-in (principal) | SMS code by a real provider, never shown on screen; MFA for owner and principal; session timeout | Fix |
| G20 | Audit trail (principal) | Price, discount, cancellation, refund and deletion events with who and when | Untested |
| G21 | Import existing data (manager) | Customers, vehicles with chassis and registration numbers, service history by CSV; a bad row is reported, not dropped | Untested |
| G22 | OEM and third-party links (principal) | OEM portal or DMS sync, WhatsApp Business, telephony, accounting | New |
| G23 | Phone-first and weak network (executive) | Core screens usable at phone width; a slow connection saves a draft and retries | Untested |
| G24 | Language and templates (marketing) | Messages and quotes in English and the local language, with one template library | Partial |
| G25 | No deletion, reopen instead (manager) | No recycle bin and no delete. Leads, customers, vehicles and bookings move to a status such as Deferred, Cancelled or Archived and can be reopened with their history; records that others point to cannot be removed | Partial |
| G26 | Merge duplicate customers (manager) | Records, vehicles and bookings move to the surviving customer; every link is re-pointed; a merge log is kept; undo allowed for a set period | New |
| G27 | Block deletes that would orphan records (manager) | A customer, vehicle or booking that other records point to cannot be deleted; the message lists what points to it; archive is offered instead | New |
| G28 | Export control (principal) | Export of customer, vehicle and finance data allowed only by role and branch; every export logged with who, when and how many rows | New |

## H. KYC and document verification (digital)

Collect once, verify online with the customer's consent, and store the verification result with a masked reference, not the document number or an image of it. This removes manual keying, cuts errors, and leaves far less sensitive data to protect.

**What to verify and what the CRM keeps**

| Item | Used for | How it is verified | What the CRM stores |
| --- | --- | --- | --- |
| Mobile number | Every lead, booking | One-time code at booking | Verified flag and date |
| Email | Invoices, documents | Link click | Verified flag and date |
| Identity and address | Booking, finance, registration | Customer consents and shares through DigiLocker, or a licensed KYC provider's offline Aadhaar e-KYC | Verified status, name, last 4 digits only, provider reference, date; never the full number or an image |
| PAN | Cash over the limit, TCS, finance, invoice | Online PAN check with name match | Verified status, name match result, PAN encrypted and masked on screen |
| Driving licence | Test ride | DigiLocker or a licence lookup through a provider | Verified, class, expiry date |
| Old vehicle RC | Exchange, insurance, registration | Vehicle registry lookup through a licensed provider | Make, model, owner name match, hypothecation, insurance and fitness dates |
| Bank account | Refunds, loan disbursal | Small test credit or reverse test credit | Verified, name match, masked account number |
| GSTIN | Company and fleet customers | GST registry lookup | Legal name, status, state |
| Signature | Booking form, test ride indemnity, delivery note | Electronic signature through a licensed provider | Signed document reference |
| Credit check | Finance | Done by the lender with the customer's consent | Outcome only |

Compliance notes, to confirm with the provider and a lawyer: private businesses are restricted in collecting and verifying Aadhaar numbers, so use DigiLocker or a licensed provider's offline e-KYC and keep only the last four digits. Under the DPDP Act, collect only what the purpose needs, record consent with purpose and time, and keep evidence only for the retention period. Group I sets the strict rules. When a customer has no PAN, capture Form 60.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| H1 | Consent before any check (customer, executive) | Screen states the purpose; consent given by code or tap; logged with time; a refusal opens the manual path | New |
| H2 | Verify mobile by code (executive) | A booking cannot be saved until the mobile is verified | New |
| H3 | Fetch identity and address through DigiLocker (executive) | Customer approves on their phone; name, date of birth and address fill the record; no document copy kept | New |
| H4 | Verify PAN (cashier, F&I) | Name match against the customer; a mismatch goes to review | New |
| H5 | Check the driving licence (executive) | Class and expiry read; an expired licence blocks the test ride | New |
| H6 | Look up the exchange vehicle (evaluator) | Registration number fills make, model, owner and loan status; owner must match the customer | New |
| H7 | Verify a bank account (cashier) | Done before any refund or loan disbursal; name must match | New |
| H8 | Verify a company or fleet customer (executive) | GSTIN lookup, authorised signatory identified, signatory's identity verified | New |
| H9 | Sign electronically (customer) | Booking form, indemnity and delivery note signed on a phone; signed copy stored and linked | New |
| H10 | Store the result, not the document (system) | Masked values and a provider reference only; no full Aadhaar number, no image | New |
| H11 | Name consistency (system) | Names across identity, PAN, licence and bank compared; differences flagged; manager override needs a reason | New |
| H12 | Provider down or check fails (executive) | Manual upload with a second person's approval; record marked manually verified | New |
| H13 | Reuse verified results (system) | A customer verified once is not charged again within the validity period; a changed detail triggers a new check | New |
| H14 | Verification gates by stage (system) | Booking needs mobile and identity; invoice over the cash limit needs PAN; delivery needs signature; the stage waits until done | New |
| H15 | Document checklist by deal type (executive) | Cash, loan, exchange, company and fleet each show the items needed and progress | New |
| H16 | Cost and audit of checks (principal) | Every call logged with provider reference, who asked, cost; monthly cost report | New |
| H17 | Retention (system) | Evidence expires after the retention period while the verified status stays | New |
| H18 | Who may see what (principal) | Masked values only, for F&I, registration and the principal; unmasked never shown | New |
| H19 | Hand verified data on (F&I, registration) | Lender and registration forms pre-filled from verified fields, with no retyping | New |
| H20 | Fleet and company onboarding (executive) | One company check plus one for each signatory; contact roles filled from the results | New |

**Test flow (scenario 11, run after FF20)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Executive starts a booking and asks the customer for consent | Consent logged with purpose and time |
| 2 | Mobile verified by code | Flag set; booking can continue |
| 3 | Customer approves identity share on their phone | Name and address fill; only the last 4 digits and a reference are stored |
| 4 | PAN checked | Name match passes; PAN shown masked |
| 5 | A second customer with a different name on PAN and ID | Mismatch flagged; manager override needs a reason |
| 6 | Provider turned off | Manual upload path opens; second approval required; marked manually verified |
| 7 | Licence of an expired holder at test ride | Ride blocked |
| 8 | Exchange vehicle number entered | Make, model, owner and loan status fill; owner mismatch flagged |
| 9 | Delivery note signed on the phone | Signed copy stored, stage moves to Delivered |
| 10 | Open the customer record as executive and as principal | Only masked values appear; no document number or image anywhere |
| 11 | Check the cost report | One line per check, with provider reference |

Done when: no document number or image is held anywhere in the CRM, every verification has a reference and a consent record, and a failed provider never stops a sale.

## I. Consent, privacy and audit (DPDP Act)

These rules are strict by design: no consent, no processing; Aadhaar kept as the last four digits only; every consent, check, view, export and erasure logged in a record nobody can edit. This is a product specification, not legal advice. Have a lawyer confirm the current DPDP Act and Rules, including commencement dates, notice wording, retention periods and breach deadlines, before launch.

**Strict rules**

| # | Rule | What the CRM enforces |
| --- | --- | --- |
| 1 | Consent per purpose | Separate consent for identity checks, marketing by each channel, sharing with a lender, and service reminders; nothing bundled or pre-ticked |
| 2 | Notice first | The customer sees what is collected, why, who receives it, how to withdraw and how to complain, in their language, before consenting |
| 3 | No consent, no processing | A check, message or lender share without a valid consent for that purpose is blocked, not warned |
| 4 | Aadhaar last four digits only | The field accepts four digits; any 12-digit number typed in a note, message, file name or upload is detected and masked; no Aadhaar image or e-KYC file is kept |
| 5 | Collect the minimum | Fields not needed for the purpose are not shown; each field is tied to a purpose |
| 6 | Withdrawal as easy as consent | A link in every message and a one-step action for staff; it takes effect at once |
| 7 | Retention by data class | Each class has a retention period; when it ends the data is erased automatically unless a legal hold applies |
| 8 | Erasure on request | Personal fields are erased or anonymised on a verified request; records the law requires are kept, with only the needed fields |
| 9 | Minors | A date of birth under 18 blocks collection until verified guardian consent is recorded |
| 10 | Encryption and masking | Sensitive fields encrypted at rest and masked on screen; unmasked values never shown to any role |
| 11 | Append-only audit | Nobody, including the owner, can edit or delete an audit entry |
| 12 | Named privacy owner | One role handles requests, grievances and breaches, separate from sales |

**How this fits the rule "leads are never deleted".** A lead or customer record is never removed. On a verified erasure request its personal fields are erased, the record stays with the status Erased on request, and the business facts that do not identify anyone (counts, dates, amounts, model) remain for reporting. Invoices, tax and registration records that the law requires stay, limited to the fields required.

**Consent record (one row each time consent is given, changed or withdrawn)**

| Field | Content |
| --- | --- |
| Consent number | Unique, never reused |
| Customer | Link to the record |
| Purpose | Identity check, marketing by channel, lender sharing, reminders |
| Data items covered | The fields the purpose needs |
| Notice version and language | The exact notice shown |
| How captured | In person, phone, link; method: code, tap or e-sign; staff who captured it |
| Time given, valid until | Date and time; expiry where set |
| Status | Given, withdrawn, expired |
| Withdrawal | Time, channel, who recorded it |
| Evidence | Reference to the code, tap or signed form |

**Erasure request flow**

1. Request arrives by link, at the counter or by email; it is logged with a number, time and channel
2. Requester proved to be the customer (code to the registered mobile)
3. Privacy owner reviews scope and checks legal retention for each record type
4. A preview lists every record and field to be erased or kept, with the legal reason for each kept
5. Privacy owner approves; the system erases or anonymises the personal fields across the customer, leads, notes, files, messages and linked vehicles and bookings
6. Lenders and providers that received the data are sent an erasure instruction, and their replies are logged
7. The record shows Erased on request; the customer gets a confirmation inside the allowed time
8. A completion entry is written to the audit trail with counts, never the erased values

**Audit trail: events always recorded**

| Event | Recorded |
| --- | --- |
| Consent given, changed, withdrawn, expired | Consent number, customer id, purpose, who, time |
| Notice shown | Notice version and language |
| Verification call | Check type, provider reference, result, cost, who asked |
| View of a sensitive field | User, record id, field, time |
| Export or download | User, what, how many rows, time |
| Edit of a sensitive field | User, field name, time; old and new values as protected hashes, not text |
| Erasure request, each step, completion | Request number, step, user, time, counts |
| Access, correction and grievance requests | Number, type, status, time to close |
| Role or permission change | Who changed what for whom |
| Failed sign-in or denied access | User, record, time |
| Breach or incident | Time found, scope, people informed, time informed |

Audit entries hold ids and counts, not personal data, so erasing a customer never conflicts with keeping the log. Keep logs for the period the Rules require, which is at least one year; confirm the current figure.

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| I1 | Show notice and take consent (executive, customer) | Notice in the customer's language, consent per purpose, no pre-ticked box; a consent row written | New |
| I2 | Consent on a phone or by link (customer) | Customer gives consent on their own phone by code or tap, by WhatsApp reply, or on a voice call; works in the showroom and remotely | New |
| I3 | Block processing without consent (system) | An identity check, message or lender share is refused when no valid consent covers that purpose | New |
| I4 | Withdraw consent (customer, executive) | One tap in a message or one action for staff; effect immediate; confirmation sent | Partial (unsubscribe passed, R2 #99 to #101) |
| I5 | Consent expiry and renewal (system) | Expired consent stops processing and prompts a renewal request | New |
| I6 | Aadhaar last four only (system) | Field takes four digits; no image or e-KYC file stored; a full number anywhere is masked on save | New |
| I7 | Detect stray identity numbers (system) | Notes, messages, file names and uploads scanned for 12-digit and PAN patterns; matches masked and reported to the privacy owner | New |
| I8 | Minimum collection by purpose (manager) | Each field linked to a purpose; fields without one cannot be added to a form | New |
| I9 | Encrypt and mask (system) | Sensitive fields encrypted at rest and masked everywhere on screen | New |
| I10 | Retention schedule (privacy owner) | Period set per data class; automatic erasure at the end unless a legal hold is set | New |
| I11 | Legal hold (privacy owner) | A hold with a reason stops erasure of the named records and is logged | New |
| I12 | Erasure request intake (customer, executive) | Logged with number, channel and time; requester verified by code | New |
| I13 | Erasure execution (privacy owner) | Preview, approval, erase or anonymise across all linked records, status Erased on request, confirmation sent | New |
| I14 | Erasure to third parties (privacy owner) | Instruction sent to each lender and provider that received the data; replies logged; open ones chased | New |
| I15 | Access and correction requests (privacy owner) | Customer receives a summary of data held and who received it; corrections applied and logged | New |
| I16 | Grievance register (privacy owner) | Number, type, owner, due date, outcome; ageing report | New |
| I17 | Breach register and notices (privacy owner) | Incident logged, scope listed, notices to the regulator and affected customers tracked against the legal deadline | New |
| I18 | Minors (executive) | Date of birth under 18 blocks collection until guardian consent is verified and recorded | New |
| I19 | Append-only audit (principal) | All events in the table above recorded; no role can edit or delete an entry; an attempt is itself logged | Untested (audit events exist in the data model) |
| I20 | Privacy reports (privacy owner) | Consent register, open requests by age, erasures done, views of sensitive fields by user, exports by user | New |
| I21 | Consent request by WhatsApp (system) | An approved message with the notice link goes to the number on record; the customer replies or taps to give or refuse each purpose; logged with message id and the sent, delivered, read and replied times; two attempts at most, then the status stays Consent pending. Check WhatsApp's business messaging rules on opt-in and approved templates, and telecom rules, with the provider before launch | New |
| I22 | Consent by voice call (agent or automated call) | Call from a registered number; the notice is read out in the customer's language; consent given by key press or a spoken yes; call id, start and end time, outcome logged; a recording is made only if the notice says so, stored with its reference and kept for the retention period | New |
| I23 | Consent attempt log (privacy owner) | Every request is logged with channel, number, attempt count, outcome (given, declined, no reply, wrong number) and date and time; a report lists customers still pending by age; a wrong number flags the mobile for review | New |

**Test flow (scenario 12, run after FF21)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Start a booking; try an identity check with no consent | Blocked with a clear message |
| 2 | Show the notice and take consent for identity and reminders only | Two consent rows written with notice version and language |
| 3 | Send a marketing message to that customer | Blocked: no marketing consent |
| 4 | Type a 12-digit number in a note and in a file name | Masked on save; privacy owner alerted |
| 5 | Try to store a full Aadhaar number in any field | Refused; only four digits accepted |
| 6 | Withdraw consent from the link in a message | Effect at once; later checks and messages blocked; withdrawal row written |
| 7 | Customer under 18 entered | Collection blocked until guardian consent is recorded |
| 8 | Customer asks for erasure at the counter | Request logged; requester verified by code |
| 9 | Privacy owner opens the preview | Every record and field to erase or keep listed, with legal reasons |
| 10 | Approve and run | Personal fields gone across customer, lead, notes, files, messages; status Erased on request; invoice kept with only required fields |
| 11 | Lender and provider erasure instructions | Sent, replies logged |
| 12 | Open the audit trail for this customer | Consent, checks, views, erasure steps all present with who and when; no personal values in it |
| 13 | Owner tries to edit or delete an audit entry | Refused and logged |
| 14 | Run the privacy reports | Counts agree with the steps above |
| 15 | Send a consent request by WhatsApp to an imported customer | Message with notice link sent; sent, delivered and read times logged; a yes reply writes a consent row with the message id |
| 16 | Customer does not reply to two requests | No third attempt; status stays Consent pending; marketing and reminders blocked |
| 17 | Take consent on a voice call by key press | Call id, start and end time and outcome logged; recording reference stored only if the notice said so; consent row written |
| 18 | Reply says wrong number | Attempt closed as wrong number; the mobile is flagged for review; no further requests to it |

Done when: nothing is processed without a matching consent, no full Aadhaar number exists anywhere (including notes and files), every erasure leaves a complete audit record, and the audit trail cannot be changed by anyone.

## J. Post-sales service cycle, created automatically

Everything the service team needs is punched once during the sale. When the vehicle is delivered, the CRM builds the vehicle's service plan and creates the first service opportunity, and each service closure creates the next. Each one then moves by itself from reminder to booking, pickup, service, delivery and invoice. Service cases, work orders, appointments and the invoice gate passed QA (R2 #61 to #87); what is new is the automatic creation, the pickup and drop leg, and the invoice step.

**Punched during the sale (nobody types it again)**

| Captured at | Information | Used for |
| --- | --- | --- |
| Needs capture (A8) | Expected daily distance, usage (city, highway, delivery), preferred service day | Estimating when the kilometre limit will be reached |
| Booking (FF3) | Model, variant, colour, accessories, extended warranty or service pack bought | Service pack redemption, warranty rules |
| Delivery (FF6) | Delivery date, odometer, VIN, registration number once issued | Start of warranty and the service clock |
| Customer record | Contact person, alternate number, language, address for pickup, preferred channel | Reminders and the pickup leg |
| Consent (group I) | Consent for service reminders by channel | Whether and how a reminder may be sent |
| Insurance and compliance | Insurance expiry, pollution certificate expiry | Renewal service opportunities |
| Finance | Lender and loan end date | Upgrade opportunity later (F8) |

**What the system creates at delivery**

1. The customer's vehicle record, linked to the booking, invoice and VIN
2. The service plan: free services, annual services, warranty end, insurance expiry, each with a due date window by days and by kilometres
3. The first service opportunity, in stage Planned, with an expected value from the service pack or price list; each service closure creates the next
4. Entitlements for free services and any pack bought, so a visit can consume one
5. The reminder ladder for each service, held until its window opens and only if consent exists
6. A thank-you and feedback task on day 3 (F1)

**Service opportunity stages**

| Stage | What happens | Automatic actions | Moves on when |
| --- | --- | --- | --- |
| Planned | Window not yet open | Due date kept up to date from the last odometer reading | Window opens |
| Reminded | Window open | Reminders at 15, 7 and 2 days before due, by consented channels; call task if no reply | Customer accepts or asks for a slot |
| Booked | Slot, bay and technician confirmed | Service request (case) created with vehicle, service type, complaints; appointment created; confirmation sent | Vehicle arrives or is picked up |
| Vehicle received | Arrival or pickup done | Job card (work order) created; condition photos and odometer saved | Estimate approved |
| In service | Work under way | Status updates to customer; parts are not tracked in the CRM for now | Work complete |
| Ready | Quality check passed | Ready message; drop or collection slot offered | Vehicle handed back |
| Delivered | Vehicle handed back | Delivery sign-off stored; invoice created (J12) | Invoice paid |
| Closed won | Paid | Next service date and kilometres set at closure (J24); next opportunity created; feedback task | None |
| Missed | Window closed with no visit | Escalation task; reopens as Deferred for a new date if the customer asks | Never deleted |

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| J1 | Build the service plan at delivery (system) | On the Delivered stage, plan lines appear for every service by model template; none is created if the vehicle has no delivery date | New |
| J2 | Service plan templates by model (manager) | Free and paid services, days and kilometre limits, packs and prices set per model and variant without a developer | New |
| J3 | Create service opportunities (system) | For the next service only, with an expected value; each closure creates the following one; they show in the service pipeline and forecast | New |
| J4 | Keep due dates current (system) | After every odometer reading the next due date is recomputed from days and from estimated kilometres | New |
| J5 | Reminder ladder (system) | 15, 7 and 2 days before due, only on consented channels, to the right contact, in their language; stops on booking | Fix (no-recipient defect, R2 #97) |
| J6 | Customer accepts, asks for a slot or calls (customer, advisor) | One step turns the opportunity into a booked appointment and a service request | Partial |
| J7 | Service request created automatically (system) | Case opens with vehicle, service type, known complaints, entitlement and SLA clock | Partial |
| J8 | Book a slot and a technician (advisor) | Slot offered only when bay and technician are free; leave respected | Ready |
| J9 | Pickup and drop (advisor, driver) | Driver assigned, pickup appointment, photos, odometer, valuables list and signature at pickup; same at drop | New |
| J10 | Vehicle received, job card made (advisor) | Job card created from the request; complaints, photos, odometer; free service entitlement applied if due | Partial |
| J11 | Estimate and approval (advisor, customer) | Service cost, labour cost and tax listed; approval by link or code; extra work needs fresh approval | Partial |
| J12 | Invoice created from the completed job (system) | On completion and customer approval a draft invoice is made from the work order with service cost, labour cost and tax; blocked before completion; detailed billing stays outside the CRM | Partial (invoice from work order passed, R2 #83 to #85; automatic creation new) |
| J13 | Free service and warranty billing (advisor) | A free service closes with a zero-value record that consumes the entitlement; a warranty repair splits into customer share and OEM claim | New |
| J14 | Payment and receipt at handover (cashier) | Part and split payments; invoice Paid only when settled; receipt numbered | Partial |
| J15 | Quality check and ready message (supervisor) | Check passed before the ready message; failed check returns the job to In service | Partial |
| J16 | Deliver the vehicle back (advisor) | Handover checklist and customer sign-off; stage Delivered | New |
| J17 | Follow-up after service (system) | Rating at day 2; a low score creates a callback within 24 hours; a repeat repair within 30 days is flagged | Partial |
| J18 | Missed service (manager) | Window closed with no visit triggers a call task, then a manager alert; the opportunity moves to Missed or Deferred, never deleted | New |
| J19 | Next cycle (system) | After Closed won the next planned service gets its new due date and the ladder restarts | New |
| J20 | Service pipeline and revenue report (manager) | Expected service revenue by month from open opportunities, booked against missed, revenue per job | New |
| J21 | Link to the outside bill (cashier) | The detailed bill is made outside the CRM; its number, date and total are recorded on the CRM invoice; a difference between the two totals is flagged for review | New |

**Invoice rules for the service step**

- The invoice is a summary created as a draft from the completed work order: service cost, labour cost and tax payable. Parts are not itemised and detailed billing is done outside the CRM
- It cannot be created before the work order is Completed (proven, R2 #83)
- A posted invoice cannot be edited; a mistake is corrected by a credit note
- Invoice numbers follow a series per branch and financial year
- A payment recorded on a draft invoice must move it to Partially paid or Paid (open defect, R1 note)
- A free service produces a zero-value record; a warranty repair produces an invoice for the customer's share and a claim for the rest
- The invoice and its payments appear on the vehicle's history

**Test flow (scenario 13, run after FF22)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Deliver a vehicle with odometer 12 and an estimated 25 km a day | Vehicle record, service plan and the first service opportunity appear automatically |
| 2 | Open the first free service opportunity | Due date shown from days and from kilometres; stage Planned |
| 3 | Move the clock to the reminder window | Reminders go out only on consented channels; stage Reminded |
| 4 | Customer replies yes | Stage Booked; appointment and service request created with the right vehicle |
| 5 | Choose pickup | Driver assigned; pickup with photos, odometer and signature; stage Vehicle received |
| 6 | Job card opens | Free service entitlement applied |
| 7 | Add an extra repair | Estimate sent; work waits for approval |
| 8 | Complete the work and pass the quality check | Stage Ready; ready message sent |
| 9 | Try to make the invoice before completion | Refused |
| 10 | Complete the work order | Draft invoice created with service cost, labour cost and tax; the free service counts at zero |
| 11 | Customer pays in two parts | Invoice Paid only after the second; receipts numbered |
| 12 | Drop the vehicle back | Sign-off stored; stage Delivered, then Closed won |
| 13 | Check the next service | New due date from the latest odometer; next opportunity updated |
| 14 | Customer ignores a later reminder | Window closes; stage Missed; call task and manager alert; nothing deleted |

Done when: from the delivery click onward, every service step is created without retyping vehicle or customer details, the invoice follows the completed job, and the next service is already planned when this one closes.

### Next service: how the date and kilometres are set

At every service closure the team confirms the next service. The CRM proposes both the kilometres and the date, the advisor can correct them, and a nightly job turns the result into the service-due record, reminders and call task. Nobody creates the due record by hand.

**Rules (all numbers are settings)**

1. Every odometer reading is stored with its date and source: delivery, arrival or pickup, service closure, or customer report
2. A reading lower than the previous one is refused or sent for review
3. Average daily kilometres is kept on the vehicle record: latest odometer minus the reading about 90 to 180 days earlier, divided by the days between
4. With less than 30 days or 100 km of history, use the estimate captured at sale; show a low-confidence mark
5. A reading interval above three times the average is flagged as an outlier and left out until reviewed
6. Next service by kilometres: last service odometer plus the model's interval, converted to a date by dividing by the average daily kilometres
7. Next service by days: last service date plus the model's day interval
8. The due date is the earlier of the two; the record shows which one set it (Kilometres or Days)
9. The advisor may overwrite either value with a reason; an overwrite becomes the fixed value until the next closure
10. The nightly job creates the service-due record, reminders and call task when the window opens, once only

**Worked example (illustrative figures)**

| Item | Value |
| --- | --- |
| Last service | 10 Jan 2027 at 4,000 km |
| Model rule | Every 3,000 km or 180 days |
| Average daily kilometres from history | 25 km a day |
| By kilometres | 3,000 divided by 25 is 120 days, so 10 May 2027 at 7,000 km |
| By days | 180 days, so 9 Jul 2027 |
| Due | 10 May 2027 at 7,000 km; basis Kilometres |

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| J22 | Odometer history (system) | Each reading kept with date and source; a lower reading is refused or flagged | New |
| J23 | Average daily kilometres on the vehicle (system) | Computed from history as in rule 3; shows basis, date computed and confidence | New |
| J24 | Set the next service at closure (advisor) | Closing odometer entered; next kilometres and date proposed; advisor confirms or edits with a reason | New |
| J25 | Date or kilometres, whichever first (system) | Both predicted dates shown; due is the earlier; the basis is stored; if one is missing the other is used | New |
| J26 | Next service opportunity at closure (system) | Created or updated with due date, kilometres and expected value; never two open for the same vehicle | New |
| J27 | Nightly due job (system) | Finds vehicles whose window opens in the set number of days; creates the service-due record, reminders and call task once; safe to run twice | New |
| J28 | Exact date set by the service team (advisor) | When the date or kilometres do not match reality, the advisor sets the exact date with a reason; the opportunity, reminders and due record update at once; old reminders cancelled | New |
| J29 | Mismatch check at arrival (advisor) | Arrival odometer compared with the predicted value for that day; a gap over the tolerance is flagged and the average recomputed | New |
| J30 | Early and late visits (system) | An early visit restarts the next interval from the actual reading; a late one is marked overdue, escalated, and the next interval counts from the actual reading; free-service limits checked | New |
| J31 | Customer-reported odometer (customer) | Sent by link or message; stored with source Customer and lower trust; a staff reading overrides it | New |
| J32 | Vehicle with no history (system) | First interval uses the sale-time estimate or the model default, with a low-confidence mark | New |
| J33 | Data quality list (manager) | Vehicles with no reading for 120 days, outlier averages, or no next service set | New |

**Test flow (scenario 14, run after FF23)**

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Close service 1: odometer 4,000 on 10 Jan | Closing screen proposes 7,000 km and 10 May, basis Kilometres; advisor confirms |
| 2 | Open the vehicle record | Odometer history shows delivery and closure readings; average shown with basis and date computed |
| 3 | Open the service pipeline | One open next-service opportunity with date, kilometres and expected value; no duplicate |
| 4 | Run the nightly job 15 days before due, then run it again | Service-due record, reminders and call task created once; the second run adds nothing |
| 5 | Customer says they are travelling until June; advisor sets 15 Jun with a reason | May reminders cancelled; new ladder for June; opportunity and due record move to 15 Jun |
| 6 | Vehicle arrives with an odometer far from the prediction | Mismatch flagged; average recomputed |
| 7 | Enter an odometer lower than the last reading | Refused or sent for review |
| 8 | Customer reports an odometer by link | Stored as a customer reading with lower trust |
| 9 | A vehicle with only the delivery reading | Estimate used; low confidence shown |
| 10 | Vehicle comes 40 days after due | Marked overdue and escalated; next interval counts from the actual odometer; free-service limits checked |
| 11 | Close service 2 | Next kilometres and date proposed from the new average; the cycle repeats without hand-made records |
| 12 | Open the data quality list | Vehicles with no recent reading or odd averages appear |

Done when: the next service is always set at closure, the nightly job creates each due record exactly once, and a manual date or kilometre correction by the service team flows through to the reminders and the next job without further work.

## K. Existing customers: migration and bulk import

A dealership that is already trading arrives with customers, vehicles and service history in spreadsheets or an old system. The new-customer path (lead to contact, sale, service) is covered above. This group covers the existing-customer path: import customers and vehicles in bulk, validate them, match them against what the CRM already holds, and let the service cycle (group J) start from the imported data. It must be done before such a dealership goes live. Scenario 8 proves the import mechanics; scenario 15 below proves the whole existing-customer flow.

**The flow**

1. Download the import template for customers, vehicles and service history
2. Upload the file to a staging area; nothing reaches live data yet
3. Map columns, using a saved mapping if the same source was used before
4. Run a dry run that validates every row
5. Download the error and warning report, fix the rows, and re-upload only the failed ones
6. Review matches against customers and leads already in the CRM
7. Approve and import; large imports need a second approval
8. The CRM links each vehicle to its owner and stores the service history as odometer readings
9. The service cycle builds itself: next service date and kilometres, next service opportunity, nightly due job
10. Imported customers are marked Consent pending and a one-time notice goes out
11. A reconciliation report ties the file to what was created, updated, held and rejected
12. A whole batch can be archived if it was wrong; nothing is deleted

**Template columns**

| Group | Columns | Notes |
| --- | --- | --- |
| Customer | Name, type (individual or business), mobile, alternate mobile, email, address, city, pincode, language, GSTIN for businesses, contact person for businesses | Mobile is the main match key |
| Vehicle | Registration number, chassis number, engine number, model, variant, colour, delivery or purchase date, selling dealer (own or other) | Chassis is the main vehicle key |
| Service history | Last service date, type and odometer; free services used; further rows allowed for earlier readings | Each row becomes an odometer reading with source Import |
| Dates | Warranty end, extended warranty end, insurance expiry | Stored as entitlements and expiry dates |
| Consent | Marketing consent (yes, no, unknown), date, source | Unknown means Consent pending |
| Not accepted | Full Aadhaar number, card or bank numbers, identity images | Stripped and reported; last four digits of Aadhaar optional |

**Validation rules**

| Check | Rule | Result |
| --- | --- | --- |
| Mobile | Ten digits, Indian format, stored with country code | Reject the row if invalid; warn if repeated in the file |
| Email | Valid format | Warn; row still imports |
| Registration number | Cleaned of spaces and case; state pattern checked; unique | Reject a duplicate; allow Not yet registered for new vehicles |
| Chassis number | Expected length for the make; unique | Reject a duplicate; warn on an odd length (older vehicles) |
| Engine number | Unique where given | Warn on a duplicate |
| Model, variant, colour | Must exist in the catalogue | Hold the row with an option to map or create |
| Dates | Not in the future; delivery before last service; odometer date after delivery | Reject impossible dates |
| Odometer | Not negative; never lower than an earlier reading for the vehicle | Hold the row for review |
| Implausible usage | Average daily kilometres above a set limit | Warn |
| Owner | Every vehicle has an owner row | Hold a vehicle with no owner |
| Business customer | A contact person is given | Warn if missing |
| Sensitive numbers | Full Aadhaar or other identity numbers in any column | Strip, warn, report to the privacy owner |
| GSTIN | Format and check digit | Warn; verified later by lookup (group H) |

| ID | Use case and actor | Rules and acceptance check | Fit |
| --- | --- | --- | --- |
| K1 | Template and guide (manager) | Downloadable templates with example rows and allowed values; versioned | New |
| K2 | Upload to staging (manager) | File held in staging; no live data touched; size and row limits; big files run in the background with progress | New |
| K3 | Column mapping (manager) | Suggested matches; a saved mapping per source system | New |
| K4 | Dry run (manager) | Counts of good, warning, held and rejected rows; per-row messages; nothing written | New |
| K5 | Error and warning report (manager) | Row, column, reason and suggested fix; downloadable; failed rows can be corrected and re-uploaded alone | New |
| K6 | Match customers (system) | By mobile, then email, then name and pincode; result is new, matched or conflict | New |
| K7 | Merge rules on a match (system) | Empty fields are filled; differing values listed for approval; a matched lead becomes a contact and keeps its history | New |
| K8 | Match vehicles (system) | By chassis, then registration; duplicates in the file or the CRM are rejected; a second owner creates an owner-change history | New |
| K9 | Reference data check (system) | Unknown model, variant or colour held, with map-or-create choice | New |
| K10 | Link vehicle to owner (system) | Each vehicle linked to its customer; business customers get contact roles | New |
| K11 | Import service history (manager) | Last service and earlier rows become odometer readings with source Import and dates | New |
| K12 | Warranty, extended warranty and insurance dates (system) | Stored as entitlements and expiry dates; validity checked | New |
| K13 | Block sensitive data (system) | Full Aadhaar and other identity or bank numbers stripped and reported; only last four allowed | New |
| K14 | Consent status on import (system) | Customers without a valid consent source are Consent pending and marketing is blocked. A consent request with the notice goes by WhatsApp message or voice call (I21, I22), and the reply or key press is logged with date and time. Until they consent, only the consent request is sent, at most twice; service reminders wait unless a lawyer confirms they may go | New |
| K15 | Build the service cycle from the import (system) | Average kilometres from history or the model default; next service date and kilometres; next service opportunity; nightly job picks it up (J22 to J27) | New |
| K16 | Overdue backlog (manager) | Vehicles already past due are marked Overdue; call tasks spread over a set number of days, not one day | New |
| K17 | Reconciliation report (manager) | Rows in the file against created, updated, held and rejected; customers against vehicles; totals tie | New |
| K18 | Batch audit (principal) | Who, file, time, mapping, counts and approvals recorded; every row's result kept | Untested |
| K19 | Archive a batch (principal) | All records the batch created are archived and hidden; nothing deleted; logged | New |
| K20 | Re-import and update (manager) | The same file twice creates no duplicates; a later file fills empty fields or applies approved changes | New |
| K21 | Import permissions and limits (principal) | Only manager roles; daily row limit; second approval above a set size | New |
| K22 | Existing customer walks in for service (advisor) | Search by registration or mobile finds the vehicle and history; if not found, a quick entry is created and marked Unverified for later matching | Partial |
| K23 | Existing customer buys another vehicle (executive) | From the customer or vehicle record start an enquiry with details prefilled; the old vehicle can go into an exchange (B1) | Partial |

**Test flow (scenario 15, run after FF24)**

Prepare a file of 300 customers and 380 vehicles. Plant these: 10 rows with no valid mobile, 8 duplicate chassis numbers, 6 unknown models, 5 future dates, 4 odometer readings that go down, 6 rows with full Aadhaar-like numbers, 12 customers already in the CRM as leads or contacts, and 5 vehicles already past their service due date.

| # | Actor and action | Expected result |
| --- | --- | --- |
| 1 | Download the template | Columns, examples and allowed values present |
| 2 | Upload the file | Held in staging; live customer and vehicle counts unchanged |
| 3 | Map columns with a saved mapping | Suggested matches accepted; mapping saved for reuse |
| 4 | Run the dry run | Counts agree with the planted problems; nothing written |
| 5 | Download the error report; fix and re-upload the failed rows only | Each problem listed with row, column and reason; fixed rows pass |
| 6 | Review the 12 matches | Empty fields fill; conflicts listed for approval; leads become contacts with history |
| 7 | Resolve the 6 unknown models | Mapped to catalogue models or created |
| 8 | Check the Aadhaar-like numbers | Stripped, reported to the privacy owner, never stored |
| 9 | Approve the import (large, so second approval) | Import runs in the background with progress |
| 10 | Open customers and vehicles | Every vehicle linked to its owner; business customers have contact roles |
| 11 | Open a vehicle | Odometer history shows imported readings with source Import |
| 12 | Open the service pipeline | A next-service opportunity exists for each vehicle, with date and kilometres; low confidence where history is thin |
| 13 | Check the 5 overdue vehicles | Marked Overdue; call tasks spread across the set number of days |
| 14 | Check consent | Imported customers show Consent pending; marketing blocked; notice request sent |
| 15 | Run the nightly due job | Service-due records created only for vehicles whose window is open, once each |
| 16 | Open the reconciliation report | File rows tie to created, updated, held and rejected counts |
| 17 | Import the same file again | No duplicates; only empty fields fill |
| 18 | As service advisor, search by registration number for an imported vehicle | Customer and full history appear; a service request can start at once |
| 19 | Archive the batch | All its records hidden everywhere; nothing deleted; audit entry written |
| 20 | Open the audit trail | Import batch, approvals and archive all present with who and when |

Done when: every row in the file is either in the CRM correctly linked, held with a clear reason, or rejected with a clear reason; the service cycle starts from the imported data; and no sensitive identity number reaches the database.

## Bike versus car, build order and next QA round

Build the bike dealership first. It reuses the most existing CRM, has the highest volume to prove speed, and its QA round (R2) is already done. Car-specific depth (exchange, finance, used vehicles) follows in phase 2 and 4.

**How the two businesses differ**

| Area | Bike, scooter, two-wheeler EV | Car, SUV |
| --- | --- | --- |
| Volume and ticket | Many walk-ins and bookings a day, small ticket | Fewer deals, larger ticket, longer cycle |
| Entry speed | Two-tap walk-in capture matters most | Full needs capture and scheduled visits |
| Finance | Common, small loans, fast approval | Common, larger loans, more documents, several lenders |
| Exchange | Occasional | Frequent, with a proper evaluation |
| Test ride or drive | Quick, often on the spot | Booked, licence checked, demo car availability |
| Accessories | Helmets, guards, covers | Larger packages, coating, fastag, extended warranty |
| Service | High frequency, short jobs, walk-in, same-day | Appointments, longer jobs, estimates, pickup and drop |
| Used vehicles | Minor | A second profit line |
| Staffing | More telecallers and counter staff | More consultants, a dedicated F&I desk |

**Build order**

| Phase | Goal | Scope | Exit test |
| --- | --- | --- | --- |
| 0 Safe to go live | Remove the defects that would mislead or expose a dealer | Real SMS sign-in; Won and Income fixes; Appointments columns; Notify validation; mandatory lost reason; draft-invoice payment; Automotive industry; duplicate-warning fix | Re-run the 11 failed checks; all pass |
| 1 Sell and deliver a bike | Enquiry to delivery with the vehicle modelled | A1 to A30 except finance links; C1 to C16; G1, G9, G16 to G19; H1 to H14; I1 to I14 | A bike dealer sells and delivers 20 vehicles from three staff phones with correct numbers |
| 2 Exchange, finance, insurance | Capture the margin beyond the vehicle | B1 to B18; F6, F8; G2, G3, G7 | A deal with exchange, loan and insurance closes with one invoice and tracked payouts |
| 3 Service depth | A workshop that runs from the CRM | D1 to D24 except D8; E1 to E4 deferred; F1, F9, F14; G15 | One day of 40 jobs on a board with estimates and claims |
| 4 Cars, used vehicles, network, portal | Car dealer and group scale | E5 to E20; F10 to F18; G6, G14, G21, G22 | A car dealer with two branches, a used-vehicle sale and a sub-dealer reports to the OEM |

**Next QA round (report 4)**

1. Bike dealer, walk-in to RC handover on a phone: capture, test ride, quote, booking, VIN allocation, inspection, invoice, split payment, registration, delivery
2. Booking cancelled after allocation: refund, VIN back to stock, lost reason recorded
3. Same mobile enquires at two branches: duplicate handling and branch privacy
4. Role matrix with five test users: executive, team lead, branch manager, accountant, technician; each tries to read, edit and export across the boundary
5. Delete or merge a customer, vehicle or booking that other records point to: nothing is orphaned
6. Card scan of a fleet owner into a lead, then a 12-vehicle fleet deal
7. Workflow tests: service reminder with and without a recipient, late-delivery alert, insurance expiry ladder
8. CSV import of 500 legacy customers with chassis and registration numbers, including 20 bad rows
9. Dashboard figures against hand-counted deliveries and collections for one day
10. On the live site: confirm the sign-in code is not shown, and check a real SMS and email arrive

Scenarios 11 (digital KYC), 12 (consent, erasure and audit) 13 (automatic service cycle), 14 (next service from odometer history) and 15 (existing customer migration) were added later and are written in full in groups H, I, J and K. Run them after FF21 and FF20, and before scenario 1 goes live with real customer data.

**Which use cases each scenario proves**

| Scenario | Use cases covered |
| --- | --- |
| 1 Walk-in to RC handover on a phone | A1, A11 to A13, A16, A22, A21, C4, C7 to C9, C12, C14, C15, G23 |
| 2 Booking cancelled after allocation | A25, C19, A26 |
| 3 Same mobile at two branches | A5, A31, G17 |
| 4 Role matrix with five users | G17, G18, G28 |
| 5 Delete or merge a referenced record | G25, G26, G27 |
| 6 Card scan and 12-vehicle fleet deal | A2, E16, C21 |
| 7 Workflow tests | D3, C13, B14 |
| 8 CSV import of 500 customers with 20 bad rows | G21 |
| 9 Dashboard against hand counts | G1, G9, G16 |
| 10 Live site sign-in, SMS and email | G19 |
| 11 Digital KYC and verification | H1 to H20 |
| 12 Consent, erasure and audit | I1 to I20 |
| 13 Automatic service cycle | J1 to J20 |
| 14 Next service from odometer history | J22 to J33 |
| 15 Existing customer migration and bulk import | K1 to K23, G21 |

Want me to turn phase 1 into a detailed spec (objects, fields, screens, validation rules) the team can build from? That is the natural next document.

## Report 4 flows: step by step

Each scenario below is a numbered flow. The Status column says what to expect when you run it: **Works** (passed in reports 1 to 3), **Check** (built but never tested), or **Gap** (no feature yet; the FF number points to the feature flow to build in the gap register at the end). Where a scenario fails at a Gap step, build that flow, then rerun from that step.

### Scenario 1. Bike dealer, walk-in to RC handover, on a phone

Run on a phone-width screen with three users: receptionist, sales executive, technician, plus the cashier and a registration executive.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Executive signs in with a mobile number and a code sent by SMS | Lands in the dealership as Staff; code not shown on screen | Check (FF17) |
| 2 | Receptionist adds a walk-in: name, mobile, model of interest | Lead saved with a number; the same mobile warns of a duplicate | Works; phone layout Check (FF18) |
| 3 | System assigns the lead to the next executive | Owner set, executive notified | Check (A6) |
| 4 | Executive records needs: budget, timeline, finance yes or no, exchange yes or no | Fields saved on the lead | Gap (A8) |
| 5 | Executive books a test ride | Slot offered only when a demo bike and the executive are free | Gap (FF5) |
| 6 | Pre-checks: licence photo and expiry, indemnity signed, start odometer | An expired licence blocks the ride | Gap (FF5) |
| 7 | After the ride: end odometer, rating, comments | Saved; a follow-up task due in 2 hours | Gap (FF5) |
| 8 | Executive converts the lead | Contact and opportunity created, lead keeps its link | Works |
| 9 | On-road quote: ex-showroom, road tax, insurance, handling, HSRP, accessories, discount | Each line shown; total correct to the paisa | Gap (FF4); totals Works |
| 10 | Discount above the executive's limit | Order blocked until the manager approves | Works |
| 11 | Cashier takes a booking advance with colour, variant and promised date | Booking record and numbered receipt; invoice status not Draft | Gap (FF3) |
| 12 | Stock controller allocates a VIN | One VIN to one booking; stock shows Allocated | Gap (FF2) |
| 13 | Technician completes the pre-delivery inspection | A failed item blocks delivery | Gap (FF6) |
| 14 | Cashier makes the invoice from the order | Same lines and total; advance applied; balance shown | Check (FF8) |
| 15 | Balance paid in parts: cash, UPI, loan disbursal | Invoice Paid only when the parts add up; cash limit warning | Check; limit Gap (G2) |
| 16 | Registration executive tracks temporary registration, RTO filing, tax, HSRP, RC | Each stage dated with a clock | Gap (FF7) |
| 17 | Delivery: handover checklist, customer signature, photo | Stage Delivered; delivery note stored | Gap (FF6) |
| 18 | RC handed over | Registration number written to the vehicle and customer record | Gap (FF7) |
| 19 | Vehicle registered as the customer's asset with chassis and warranty | Asset shows chassis, warranty; first service date set | Works; service date Gap (D2) |
| 20 | Day 3 thank-you and feedback request | Message sent once; a low score creates a callback | Check (F1) |

Done when: steps 1 to 20 run on a phone with no workaround, and the totals at steps 9, 14 and 15 match a hand calculation.

### Scenario 2. Booking cancelled after allocation

Start with a booking that has an advance receipt and an allocated VIN.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Customer asks to cancel; executive opens the booking and chooses Cancel with a reason | Reason list: price, delivery wait, finance rejected, bought elsewhere, changed mind | Gap (FF3, FF9) |
| 2 | System applies the cancellation rule | Shows refundable amount and any non-refundable fee | Gap (FF9) |
| 3 | Manager approves the refund | Approval recorded with name and time | Works (approval engine) |
| 4 | Cashier records the refund by mode | Refund tied to the original receipt; account statement shows it | Check (refund allocation exists) |
| 5 | System releases the VIN | Stock status returns to Free; allocation history kept | Gap (FF2, FF9) |
| 6 | Deal closes as Lost | Reason and competitor stored; lost report counts it | Fix (R1 #51), then Gap (FF9) |
| 7 | Next booking on the waiting list | Offered the freed VIN in booking order | Gap (A23) |
| 8 | Customer record | Keeps the full history; marked as a cancelled booking | Gap (FF9) |

Done when: refund, stock and lost-sale numbers all agree and the cancelled VIN can be sold to someone else.

### Scenario 3. Same mobile enquires at two branches

Two branches, A and B, each with a receptionist and an executive.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Branch A creates a lead for a mobile number | Lead owned by an A executive | Works |
| 2 | Branch B receptionist enters the same number | Match found across the whole dealership | Check (match works inside one business, R1 #23) |
| 3 | Branch B sees the match | Shows existing customer, branch and owner; no notes, quotes or finance details | Gap (FF10) |
| 4 | Ownership rule runs | The lead stays with A if active in the last 30 days; otherwise B may take it | Gap (FF10) |
| 5 | B executive requests a transfer | A's manager approves or refuses; both are notified | Gap (FF10) |
| 6 | B staff open A's lead by direct link | Refused | Check (own-lead isolation Works, R2 #56, #74) |
| 7 | Audit log | Shows the match, the request and the decision | Check (G20) |

Done when: one customer record exists, no branch sees another's commercial details, and the owner rule decides the winner.

### Scenario 4. Role matrix with five test users

Create five users in one branch, plus one lead owned by another branch: executive, team lead, branch manager, accountant, technician. Each user tries every row below. A wrong answer in any cell is a failed check.

| Action | Executive | Team lead | Branch manager | Accountant | Technician |
| --- | --- | --- | --- | --- | --- |
| Read own lead | Allow | Allow | Allow | Deny | Deny |
| Read a teammate's lead | Deny | Allow | Allow | Deny | Deny |
| Read another branch's lead | Deny | Deny | Deny | Deny | Deny |
| Edit a teammate's lead | Deny | Allow | Allow | Deny | Deny |
| Approve a discount | Deny | Up to limit | Up to higher limit | Deny | Deny |
| Record a payment | Deny | Deny | Deny | Allow | Deny |
| Change a posted invoice | Deny | Deny | Deny | Credit note only | Deny |
| See PAN and licence numbers unmasked | Deny | Deny | Deny | Deny | Deny |
| Open own job card | Deny | Deny | Allow | Deny | Allow |
| Export leads | Own only | Team | Branch | Deny | Deny |
| Export invoices | Deny | Deny | Branch | Allow | Deny |
| Invite or change users | Deny | Deny | Deny | Deny | Deny |

For every Deny, the user must see a clear refusal, not an empty page, and the audit log must record the attempt. Status: only Super Admin and Staff were tested (R2 #56 to #59); team lead, branch scope, accountant, technician and export limits are all Gap (FF11).

Done when: all 60 cells give the expected answer and every export is logged with who, when and how many rows.

### Scenario 5. Delete or merge a record that others point to

Create customer C with one vehicle, one booking, one invoice, one payment, one case and one lead conversion. Create a duplicate customer D.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Manager deletes customer C | Refused; the message lists the vehicle, booking, invoice, payment and case that point to C | Gap (FF12) |
| 2 | Manager deletes the vehicle | Refused while the booking points to it | Gap (FF12) |
| 3 | Manager archives C instead | C hidden from lists, still resolves on every linked record | Gap (FF12) |
| 4 | Manager merges D into C | A preview shows every record that will move; the manager confirms | Gap (FF12) |
| 5 | Merge completes | D's records now point to C; counts of vehicles, invoices and payments equal the sum of both | Gap (FF12) |
| 6 | Check every link | Quotes, bookings, invoices, payments, cases and contact roles all show C; no record shows D | Gap (FF12) |
| 7 | Open the merge log | Who, when, which fields won, record counts moved | Gap (FF12) |
| 8 | Undo the merge within the allowed period | D is restored with its original records | Gap (FF12) |
| 9 | Try to delete a lead, then set it to Deferred | No role is offered delete; Deferred removes the lead from active lists, keeps every link and note, and it returns on the chosen date or when reopened | Gap (FF12) |
| 10 | Run an orphan scan | Zero lookup fields point to a missing record | Gap (FF12) |

Why this matters: the 131 lookup fields are not database foreign keys (Appendix B of the ER document), so only server checks protect them.

Done when: the orphan scan returns zero after delete, archive, merge and undo.

### Scenario 6. Card scan of a fleet owner, then a 12-vehicle deal

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Executive scans the fleet owner's visiting card on a phone | A business account, a contact and a lead are created; fields read correctly; source set to card | Check (A2) |
| 2 | Executive converts the lead | Account, contact and opportunity created | Works |
| 3 | Add contact roles: owner as decision maker, accounts head, fleet manager | Roles saved and visible from the account | Works |
| 4 | Quote 12 vehicles from the fleet price book | Lines and total correct; fleet discount above the limit goes to approval | Works |
| 5 | Take one advance for all 12 | One booking with 12 lines, or 12 linked bookings; one receipt | Gap (FF3, FF13) |
| 6 | Allocate 12 VINs | Each VIN to one line; shortage shown if stock is short | Gap (FF2, FF13) |
| 7 | Make one order and one invoice of 12 lines | Tax and total right; invoice names the account, not the quote | Works; naming Fix (R1 #86) |
| 8 | Deliver in two batches of 6 | One delivery note per vehicle, two handover dates | Gap (FF6, FF13) |
| 9 | Track registration per vehicle | Twelve separate registration records with their own stages | Gap (FF7, FF13) |
| 10 | Record one fleet loan across the deal | One finance application, 12 vehicles listed | Gap (B7) |
| 11 | Set up a fleet service contract | Visit schedule and priority slots; renewal without a false duplicate warning | Fix (D24) |
| 12 | Open the account statement | Invoiced, paid and outstanding amounts agree | Works |

Done when: one deal, one invoice, twelve vehicles each with their own VIN, delivery and registration.

### Scenario 7. Workflow tests

**7a. Service reminder with and without a recipient**

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Build a workflow: 7 days before the vehicle's next service date, send a message | Date-based trigger can be chosen | Check (only a record-created trigger was tested, R2 #90) |
| 2 | Add a Notify step with no recipient and try to publish | Refused, or a clear warning | Fix (FF14, R2 #97) |
| 3 | Set the recipient to the vehicle owner and publish | Publishes; a test run lists who would receive it | Gap (FF14) |
| 4 | Set a vehicle's service date 7 days ahead | The workflow runs on the right day, once | Check (FF14) |
| 5 | A customer who declined WhatsApp | Skipped; the run log says why | Gap (F11) |
| 6 | Open the run log | Each run shows who got what and when | Works (R2 #94) |

**7b. Late-delivery alert**

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | A booking with a promised date of yesterday and no delivery | Customer message sent; manager alerted | Gap (FF14, C13) |
| 2 | Delivery recorded | No further alerts for that booking | Gap (FF14) |

**7c. Insurance expiry ladder**

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Insurance expiry stored on the vehicle | Tasks created at 90, 60, 30, 15 and 7 days | Gap (B14, FF14) |
| 2 | Policy renewed | Ladder stops; new expiry stored | Gap (B14, FF14) |
| 3 | Policy bought elsewhere | Recorded, so reminders stop | Gap (B14) |

Done when: no workflow can publish without a recipient, every run is logged, and each ladder fires on the right days and stops when the event is done.

### Scenario 8. CSV import of 500 legacy customers, 20 bad rows

Prepare a file of 500 rows: customer name, mobile, email, city, chassis number, registration number, model, purchase date, last service date. Plant 20 bad rows: 5 with no mobile, 4 with a chassis number of the wrong length, 4 with a duplicate registration number, 4 with an impossible date, 3 with a duplicate mobile.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Manager downloads the import template | Columns named, one example row, allowed values listed | Gap (FF15) |
| 2 | Uploads the file and maps columns | Mapping screen suggests matches; saved for reuse | Check (CSV import never tested) |
| 3 | Runs a dry run | Counts: 480 good, 20 bad; nothing written yet | Gap (FF15) |
| 4 | Opens the error report | One line per bad row: row number, column, reason; downloadable | Gap (FF15) |
| 5 | Imports the 480 good rows | 480 customers and vehicles created, each vehicle linked to its customer | Check |
| 6 | Duplicate mobiles | Merged into one customer with two vehicles, or listed for review | Gap (FF15, FF12) |
| 7 | Fixes the 20 rows and imports them again | 20 added; nothing doubled | Gap (FF15) |
| 8 | Checks service due dates | Calculated from purchase date and last service | Gap (D2) |
| 9 | Archives the whole import batch | All records of that batch hidden everywhere, nothing deleted, records created later untouched | Gap (FF15) |
| 10 | Orphan scan | Zero broken links | Gap (FF12) |

Done when: 500 rows end as 500 correct records, with every bad row reported and none lost silently.

### Scenario 9. Dashboard against hand counts for one day

Pick one working day. Enter exactly 10 enquiries, 6 test rides, 4 bookings, 2 deliveries, 1 cancellation, and 5 receipts totalling a known amount. Count them by hand before opening any report.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Open the dashboard for Today | Enquiries 10, test rides 6, bookings 4, deliveries 2, cancellations 1 | Gap (FF16) |
| 2 | Check Won or Delivered for Today | Counts 2 on the delivery date, not on the expected close date | Fix (R1 #83) |
| 3 | Check Income or Collections for Today | Equals the 5 receipts to the rupee | Fix (R1 #84, R2 #104) |
| 4 | Check the won value | Equals the invoiced totals after discount, not the deal amounts | Fix (R2 note) |
| 5 | Filter by branch and by person | Each total adds up to the dealership total | Gap (FF16) |
| 6 | Entries made after 11:30 pm and before 12:30 am | Land on the right date in Indian time | Check (FF16) |
| 7 | Change a delivery date to yesterday | Both days' numbers update correctly | Check (FF16) |

Done when: every tile equals the hand count for three different days.

### Scenario 10. Live site: sign-in code, SMS and email

The earlier reports say the project's own rule is not to test against the live database. Do this scenario in a separate test business created for the purpose, with the owner's approval, and delete it afterwards. Use only a team member's own phone number and email address.

| # | Actor and action | Expected result | Status |
| --- | --- | --- | --- |
| 1 | Open the live login page and ask for a code | The page does not show the code | Check (FF17); the test copy showed it |
| 2 | Wait for the SMS | Arrives within 60 seconds from a registered sender | Check (FF17) |
| 3 | Enter the code | Signs in; the code works once and expires after a few minutes | Check (FF17) |
| 4 | Enter a wrong code five times | Sign-in is locked for a while; the attempts are logged | Check (FF17) |
| 5 | Invite a teammate by email | The email arrives, from the right sender, not in spam | Check (FF17) |
| 6 | Send a test campaign to the team's own address | Arrives with the unsubscribe line; unsubscribing stops the next send | Works on the test copy (R2 #99 to #101); live Check |
| 7 | Sign in as owner | A second factor is offered | Gap (G19) |
| 8 | Delete the test business | Its records are gone, other businesses untouched | Check (FF12) |

Done when: the code never appears on screen, messages arrive on real phones and inboxes, and the test business leaves nothing behind.

## Gap register: feature flows to build

Every Gap or Fix above points to one of these flows. Each lists the steps the finished feature must follow, in order, and the test that proves it. Build them in the order shown; a flow can start once the ones it depends on are done.

| ID | Feature flow | Steps in order | Depends on | Done when |
| --- | --- | --- | --- | --- |
| FF17 | Secure sign-in | 1 Connect an SMS provider. 2 Remove the code from the page. 3 Limit attempts and expire codes. 4 Offer a second factor to owners | None | Scenario 10 steps 1 to 4 and 7 pass |
| FF16 | Correct dashboard numbers | 1 Count Won on the won or delivered date. 2 Add a Collections tile from payments. 3 Roll the invoice total into the won value. 4 Add daily counts by branch and person. 5 Use Indian time throughout | None | Scenario 9 matches hand counts on three days |
| FF14 | Workflow checks and date triggers | 1 Refuse publish when a Notify step has no recipient. 2 Add triggers on a date field, before or after. 3 Add a test run that lists recipients. 4 Skip people without consent and log why. 5 Stop a ladder when its event happens | None | Scenario 7 passes |
| FF1 | Vehicle catalogue | 1 Define model, variant and colour. 2 Add segment, fuel or EV, engine size, tax class. 3 Link to price books | None | A model with three variants and four colours prices correctly in a quote |
| FF2 | Vehicle stock and allocation | 1 Create the stock object with VIN, engine number, colour, location, status. 2 Receive a vehicle and refuse duplicate VINs. 3 Allocate to a booking, one to one. 4 Release on cancellation. 5 Show ageing and free against allocated | FF1 | Scenario 1 step 12 and scenario 2 step 5 pass; stock counts equal a physical count |
| FF3 | Booking | 1 Create the booking object: customer, variant, colour, promised date. 2 Take advance receipts against it. 3 Amend with a price difference. 4 Cancel with a rule and approval. 5 Convert to the sales order on allocation | FF1 | Scenario 1 step 11 and scenario 2 steps 1 to 2 pass |
| FF4 | On-road quote | 1 Add fixed components: ex-showroom, road tax, insurance, handling, HSRP, TCS, accessories, extended warranty. 2 Set tax by model and date. 3 Show up to three variants side by side | FF1 | Totals match a hand calculation for a bike and a car |
| FF8 | Invoice with advance and split payments | 1 Apply the booking advance by allocation. 2 Accept several payment modes on one invoice. 3 Warn on cash over the legal limit. 4 Turn Paid only when the parts add up | FF3 | Scenario 1 steps 14 and 15 pass |
| FF5 | Test ride | 1 Make demo vehicles bookable resources. 2 Check licence expiry and indemnity. 3 Log odometer, rating, comments. 4 Create a follow-up task | FF1 | Scenario 1 steps 5 to 7 pass |
| FF6 | Inspection, handover and delivery note | 1 Checklist template per model. 2 Block delivery on a failed item. 3 Handover checklist. 4 Customer signature and photo. 5 Set stage Delivered | FF2 | Scenario 1 steps 13 and 17 pass; fleet delivery notes per VIN |
| FF7 | Registration tracker | 1 Create a record at invoice. 2 Stages: temporary registration, RTO filing, tax, HSRP, RC. 3 Clock per stage. 4 Hold on missing documents. 5 Write the registration number back to the vehicle | FF2 | Scenario 1 steps 16 and 18 pass |
| FF9 | Cancellation and refund | 1 Cancel action with reason list. 2 Apply the refund rule. 3 Route to approval. 4 Record the refund against the receipt. 5 Release the VIN. 6 Close the deal as Lost with reason | FF2, FF3 | Scenario 2 passes end to end |
| FF10 | One customer across branches | 1 Match on mobile across the dealership. 2 Show branch and owner only. 3 Apply an ownership rule by activity. 4 Transfer by request and approval. 5 Log every step | None | Scenario 3 passes |
| FF11 | Roles, branch scope and export control | 1 Add branch and team to users. 2 Define the five roles and the matrix. 3 Mask PAN and licence numbers. 4 Limit exports by role and branch. 5 Log every export and every denied attempt | FF10 | All 60 cells of scenario 4 correct |
| FF12 | Merge, delete protection and orphan scan | 1 Remove delete for leads and add Deferred and Archived statuses; block deletes of other records that leave links. 2 Offer archive. 3 Preview, run and log a merge. 4 Undo within a set period. 5 Run an orphan scan on demand | None | Scenario 5 passes; scan returns zero |
| FF13 | Fleet and bulk deals | 1 One booking, order and invoice with many vehicles. 2 Allocate and deliver in batches. 3 One delivery note and one registration per vehicle. 4 One finance application across the vehicles | FF2, FF3, FF6, FF7 | Scenario 6 steps 5 to 9 pass |
| FF15 | Import with validation | 1 Template and column mapping. 2 Dry run with counts. 3 Error report by row. 4 Merge duplicates. 5 Re-import fixes without doubling. 6 Archive a whole batch (nothing deleted) | FF12 | Scenario 8 passes |
| FF18 | Phone-first screens | 1 Walk-in capture, test ride log, booking and delivery sign-off usable at phone width. 2 Save a draft when the network drops and retry | None | Scenario 1 runs entirely on a phone |
| FF19 | Stage checklists and follow-up tasks | 1 Manager defines templates per stage and vehicle type. 2 A stage change creates the tasks and checklist with owners and due dates. 3 Required items block the next stage. 4 Each follow-up logs an outcome and a mandatory next date. 5 Overdue tasks and stuck deals alert the executive, then the manager | FF5 for the test ride items | A lead taken from enquiry to booking in scenario 1 creates the right tasks at each stage, cannot advance with a required item open, and shows overdue work on the manager's list |
| FF20 | Digital KYC and verification | 1 Choose and contract licensed providers for identity, PAN, licence, vehicle lookup, bank check and e-sign. 2 Capture consent. 3 Call the provider and store only the result, masked reference and provider reference. 4 Compare names. 5 Gate stages on verification. 6 Manual fallback with second approval. 7 Log cost and retention | FF11 for masking and roles | Scenario 11 passes and no document number or image is stored |
| FF21 | Privacy, consent and audit | 1 Consent object and notice versions. 2 Enforce consent before checks, messages and sharing. 3 Last-four Aadhaar field and stray-number scanner. 4 Retention schedule and legal hold. 5 Erasure request, preview, execution and third-party instructions. 6 Access, correction, grievance and breach registers. 7 Append-only audit trail and privacy reports. 8 Privacy owner role | FF11 for roles and masking | Scenario 12 passes; build before FF20 so no verification runs without consent |
| FF22 | Automatic service cycle | 1 Service plan templates by model. 2 On Delivered, build the plan and the service opportunities. 3 Recompute due dates from days and kilometres. 4 Reminder ladder tied to consent. 5 Opportunity to booking, appointment and service request. 6 Pickup and drop with photos and signatures. 7 Job card, estimate approval, quality check. 8 Draft invoice from the completed work order, free and warranty billing. 9 Next-cycle update and missed-service handling | FF6, FF14, FF21 | Scenario 13 passes |
| FF23 | Odometer history and next-service engine | 1 Odometer readings with date and source. 2 Average daily kilometres on the vehicle. 3 Next service proposal at closure by kilometres and by days. 4 Opportunity created or updated, never duplicated. 5 Nightly job creating the service-due record once. 6 Exact-date override with reason and re-planning. 7 Mismatch, early, late and no-history rules. 8 Data quality list | FF22 | Scenario 14 passes |
| FF24 | Existing customer import and migration | 1 Templates and staging area. 2 Column mapping and saved profiles. 3 Dry run and error report by row. 4 Matching and merge rules for customers and vehicles. 5 Reference data check. 6 Owner linking and service history as odometer readings. 7 Sensitive-number block and consent-pending status. 8 Post-import service build and overdue backlog spreading. 9 Reconciliation report, batch audit, archive and safe re-import. 10 Permissions, limits and second approval | FF15, FF1, FF21, FF23 | Scenario 15 passes |

**Suggested build order:** FF17, FF16, FF14 (safe to go live), then FF1 to FF4, FF8, FF19, FF21, FF20 (sell a bike), then FF5 to FF7, FF9, FF10 (deliver and control), then FF22 and FF23 (service cycle); FF24 (existing customer import) comes before go-live for any dealership that already has customers, then FF11 to FF13, FF15, FF18 (scale and protect). This matches phases 0 and 1 in the build order above.

**How to use this section.** Run each scenario in order. At the first Gap or Fix step, stop, build the named flow, and rerun the scenario from that step. Record the result in report 4 against the step number so the next round starts from a known state.
