# Cogniz CRM website (`crmwebsite/`)

The public marketing website, shown at **`/`** (D-181). It is part of the same web app as the
CRM: same build, same address, same brand. The CRM's sign-up / sign-in page moved to **`/signup`**.

On a business's own domain, and inside the phone app, `/` still opens that business's sign-in.

## Layout

| Folder | What is in it |
|---|---|
| `index.tsx` | `HomePage` (route `/`): the website, or the sign-in on a business domain / the app |
| `components/layout` | Header (sticky, mobile menu) and Footer |
| `components/sections` | Hero, Problem (paper → CRM), Journey (13 steps), Features (+ quotation stage), Team, Industries, Benefits, Showcase, Pricing, Contact, FAQ, Final CTA |
| `components/forms` | `RequestForm`: "Book a free call" and "Send an enquiry" |
| `components/common` | Logo, section headings, screen frame, counters |
| `data/content.ts` | Every word on the site, the industries, FAQ and which screen goes where |
| `hooks` | Scroll reveal, in-view, count-up |
| `services/website-api.ts` | The only API calls: `POST /public/enquiries`, `GET /public/billing/plans`, `GET /public/industries` |
| `assets/images` | Real CRM screens (demonstration business, sample data) and the sample quotation / invoice |
| `assets/videos` | Hero video (`hero.mp4`, `hero.webm`, `hero-poster.webp`) |
| `seo` | `robots.txt`, `sitemap.xml` (copied to the site root by webpack) |
| `styles/website.css` | All styles, scoped under `.cw` so the CRM screens are untouched |

## Forms → CRM

Both forms post to `POST /api/crm/v1/public/enquiries` (no sign-in). The server
(`backend/internal/crm/platform/enquiry.go`):

* checks every field again, drops robots (hidden `website` box), and limits each address to
  5 requests and the site to 120 every 10 minutes;
* files a **lead in the owner's Platform CRM** with source `website_call`
  ("CRM Website - Book Free Call") or `website_enquiry` ("CRM Website - General Enquiry"),
  status *New*, and `requestType` `call` / `enquiry`;
* a visitor with an open lead (same mobile) is **not filed twice**: the new request is added to
  that lead (a call request upgrades an enquiry) and noted on its timeline;
* tells the owner (bell notification).

The owner sees them under **Call requests** and **Website enquiries** (menu, dashboard cards with
new / pending / completed counts, and lists with their own columns). Assign, add notes, schedule
follow-ups and approve them like any other owner lead.

## Changing things

* Words: `data/content.ts`.
* Screens: replace a file in `assets/images` (1440 × 900, WebP).
* Hero video: `assets/videos/hero.mp4`, `hero.webm`, `hero-poster.webp` — our own 45-second
  story: a quotation written by hand is photographed and sent, the customer can't read it and
  blocks him; a colleague brings a laptop with Cogniz CRM; the lead is added and a clear quotation (GST worked out) is sent,
  and the customer confirms; the owner relaxes, adds a team member and checks leads, quotations
  and follow-ups in one click each. Replace those three files to change it.
