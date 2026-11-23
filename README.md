# Cogniz CRM

CardFlow is an integrated platform combining verified local business discovery with AI-powered physical business card digitization and lifelong digital card storage.

---

## 🚀 Quick Start — Running in Google Chrome

To launch and test the application in Google Chrome:

```bash
# 1. Navigate to the frontend directory
cd frontend

# 2. Install dependencies (if not already installed)
npm install

# 3. Start the React Native Web development server
npm run web
```

Once running, open your web browser at:
**`http://localhost:3000`**

---

## 🔑 Development Test Accounts (DEV Environment Only)

For instant local testing, three fixed accounts are pre-configured:

| User Role | Mobile Number | Fixed DEV OTP | Purpose |
|---|---|---|---|
| **Normal User** | `1234567890` | `123456` | Search directory, scan cards, manage Card Vault, send enquiries |
| **Business Owner** | `9876543210` | `123456` | Manage multiple businesses, QR counter display, enquiries, analytics |
| **Admin** | `9999988888` | `123456` | In-app KYC review queue, moderation, user management, audit logs |

> **⚠️ Security Guard:** These fixed test credentials are strictly disabled when `ENV=production`. In production, OTPs are dispatched via an SMS gateway.

---

## 📁 Repository Structure

```
CardFlow/
├── frontend/               # React Native + React Native Web application (JavaScript)
│   ├── public/             # HTML templates and static assets
│   ├── src/
│   │   ├── components/     # UI primitives (Button, Input, Card, Badge, Header, TabBar, Layout)
│   │   ├── context/        # AuthContext with role handling & dev test accounts
│   │   ├── data/           # Coimbatore seed categories and verified business mock data
│   │   ├── navigation/     # AppNavigator with role-based screen routing
│   │   ├── screens/
│   │   │   ├── auth/       # Splash, Phone Login, OTP Verification
│   │   │   ├── user/       # Home, Search, Business Details, Card Vault, Scanner, Profile
│   │   │   ├── owner/      # Multi-Business Dashboard, My Businesses, QR, Share, Enquiries, Analytics
│   │   │   └── admin/      # In-App Admin Dashboard, Users, Listings, KYC Review Queue, Settings
│   │   └── theme/          # Typography, color tokens, spacing, radii
│   ├── package.json
│   ├── babel.config.js
│   └── webpack.config.js
├── backend/                # Go REST API backend (Phase 2+)
├── docs/                   # Complete architectural & technical specifications
│   ├── ARCHITECTURE.md
│   ├── DATABASE.md
│   ├── API.md
│   ├── SECURITY.md
│   └── IMPLEMENTATION_PLAN.md
├── .env.example            # Environment configuration template
├── README.md               # Overview & quick start guide
└── SETUP.md                # Detailed step-by-step setup manual
```

---

## 🛠️ Technology Stack

* **Mobile & Web App:** React Native + React Native Web (JavaScript)
* **Backend:** Go (Modular Clean Architecture)
* **Database:** PostgreSQL 16 + PostGIS + pg_trgm + Full-Text Search
* **Cache & OTP:** Redis 7
* **Object Storage:** S3-Compatible Object Store (Presigned direct uploads)
* **AI Extraction:** Google Vertex AI / Gemini 3.x Flash-Lite
* **Maps:** Google Maps Platform
* **Authentication:** Phone Number + OTP with Ed25519-signed JWTs
* **Admin:** Integrated directly inside the same React Native mobile app
