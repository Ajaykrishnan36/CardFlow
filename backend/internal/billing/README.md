# CardFlow Premium — RevenueCat

CardFlow sells one subscription, **CardFlow Premium**, through RevenueCat on iOS (App Store), Android (Google Play) and Web (RevenueCat Web Billing). Access is decided by a single entitlement, `premium`. Product ids are never checked.

## Flow

1. The user signs in to CardFlow. The app calls `GET /api/v1/billing/status`, which returns `app_user_id` (the CardFlow user UUID), and logs the RevenueCat SDK in with that id (`src/services/subscription`).
2. The paywall (`SubscriptionScreen`) shows the packages of the **current offering** from the SDK. Prices and periods come from the store.
3. The purchase happens in the SDK. The app then calls `POST /api/v1/billing/sync`. The server re-reads the customer from RevenueCat with the secret key and stores the result.
4. RevenueCat also calls `POST /api/webhooks/revenuecat` for every lifecycle event. The server stores each event once (`revenuecat_events`) and updates `users`.
5. Premium APIs decide access from `users.is_subscribed` / `subscription_expires_at` (`domain.User.IsPremiumActive`). Nothing the client sends is trusted.
6. On logout the app calls RevenueCat `logOut`.

## States (`users.subscription_status`)

| State | Premium? | Meaning |
| --- | --- | --- |
| `FREE` | no | Never subscribed |
| `ACTIVE` | yes | Active, renews (or lifetime) |
| `CANCELLED` | yes, until expiry | Won't renew; access runs to the end of the paid period |
| `BILLING_ISSUE` | during the store grace period only | Payment failed; the user must update the payment method |
| `EXPIRED` | no | Ended, or refunded |

Access granted in the CRM (`subscription_source = crm_grant`) and access bought before the migration (`legacy`) are never revoked by RevenueCat reporting "no entitlement".

## Webhook guarantees

- **Authentication:** the `Authorization` header must match `REVENUECAT_WEBHOOK_AUTH`, compared in constant time. When the variable is unset, every webhook is rejected.
- **Idempotency:** the event id is the primary key, so a retried delivery returns `200 {"duplicate": true}` and is not applied twice.
- **Ordering:** an event older than the last one applied is ignored. When the secret key is set, the server re-reads RevenueCat on every event anyway.
- **Retries:** a processing failure returns 5xx, so RevenueCat retries it.

## Environment

Server (Render):

| Variable | Notes |
| --- | --- |
| `REVENUECAT_WEBHOOK_AUTH` | Required. The same value goes in RevenueCat → Integrations → Webhooks → Authorization header. |
| `REVENUECAT_SECRET_API_KEY` | Recommended. The secret key (`sk_...`), used for `/billing/sync` and the webhook re-read. |
| `REVENUECAT_ENTITLEMENT_ID` | Default `premium`. |

App (Vercel env, or a git-ignored `frontend/.env.local`): these are **public** SDK keys only, and the build fails if an `sk_` key is supplied.

| Variable | Store |
| --- | --- |
| `REVENUECAT_IOS_API_KEY` | App Store (`appl_...`) |
| `REVENUECAT_ANDROID_API_KEY` | Google Play (`goog_...`) |
| `REVENUECAT_WEB_API_KEY` | Web Billing (`rcb_...`) |

Until the stores are connected, put the **Test Store** key (`test_...`) in all three.

## RevenueCat dashboard checklist

1. Create the entitlement `premium`.
2. Create products (Test Store first; App Store, Play and Web Billing later) and attach them to `premium`.
3. Create an offering, mark it **Current**, and add packages (`$rc_monthly`, `$rc_annual`, and so on).
4. Add the webhook: URL `https://<api-host>/api/webhooks/revenuecat`, with the Authorization header set to `REVENUECAT_WEBHOOK_AUTH`.
5. iOS: enable the **In-App Purchase** capability for `app.cardflow.mobile` in Xcode / App Store Connect.
6. Android: upload a build to a Play testing track so the products resolve.
