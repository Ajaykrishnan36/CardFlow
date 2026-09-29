import { PREMIUM_ENTITLEMENT, billingPlatform, isPublicKey, revenueCatApiKey } from './revenueCatConfig';
import { nativeBilling } from './nativeBilling';
import { webBilling } from './webBilling';

// One billing API for iOS, Android and Web. Screens never talk to a
// RevenueCat SDK directly — they use this module (through AuthContext).
//
// The App User ID is always the authenticated CardFlow user id (from the
// server), so purchases follow the account across devices and platforms.
// Premium access shown in the UI comes from the "premium" entitlement; the
// server makes its own decision from RevenueCat webhooks/REST and never trusts
// what the client says.

export class BillingError extends Error {
  constructor(code, message, cause) {
    super(message);
    this.code = code; // 'cancelled' | 'unavailable' | 'not_ready' | 'network' | 'failed'
    this.cause = cause;
  }
}

const platform = billingPlatform();
const impl = platform === 'web' ? webBilling : nativeBilling;

let currentUserId = null;
let readyPromise = null;

function toBillingError(err, fallback) {
  if (err instanceof BillingError) return err;
  if (impl.isCancelled(err)) return new BillingError('cancelled', 'Purchase cancelled.', err);
  const msg = String(err?.message || '');
  if (/network|offline|internet|timed? ?out|fetch/i.test(msg)) {
    return new BillingError('network', 'No internet connection. Please check your connection and try again.', err);
  }
  // SDK messages are developer-facing ("credentials issue…"); log them and
  // show the user a plain message instead.
  console.warn('RevenueCat error', err);
  return new BillingError('failed', fallback, err);
}

export function isBillingConfigured() {
  return isPublicKey(revenueCatApiKey());
}

/** Log the SDK in as this CardFlow user (configures it on first use). */
export function identify(appUserId, onCustomerInfo) {
  if (!appUserId) return Promise.resolve(false);
  const key = revenueCatApiKey();
  if (!isPublicKey(key)) {
    if (key) console.error('RevenueCat: a secret key was provided to the client — refusing to use it.');
    return Promise.resolve(false);
  }
  if (currentUserId === appUserId && readyPromise) return readyPromise;
  currentUserId = appUserId;
  readyPromise = impl
    .configure(key, appUserId, onCustomerInfo)
    .then(() => true)
    .catch((e) => {
      console.warn('RevenueCat configure failed', e);
      readyPromise = null;
      currentUserId = null;
      return false;
    });
  return readyPromise;
}

/** Log out of RevenueCat (call on CardFlow logout). */
export async function reset() {
  const had = currentUserId;
  currentUserId = null;
  readyPromise = null;
  if (!had) return;
  try {
    await impl.logOut();
  } catch (e) {
    console.warn('RevenueCat logOut failed', e);
  }
}

async function ready() {
  if (!isBillingConfigured()) {
    throw new BillingError('unavailable', 'Subscriptions are not available on this build yet.');
  }
  const ok = readyPromise ? await readyPromise : false;
  if (!ok) throw new BillingError('not_ready', 'Subscriptions are still loading. Please try again in a moment.');
}

/** Packages of the current offering, normalised for the paywall. */
export async function getOfferings() {
  await ready();
  try {
    return await impl.getOfferings();
  } catch (e) {
    throw toBillingError(e, "Plans couldn't be loaded right now. Please try again.");
  }
}

export async function purchaseSubscription(pkg) {
  await ready();
  try {
    return await impl.purchase(pkg);
  } catch (e) {
    throw toBillingError(e, 'The purchase could not be completed. You have not been charged.');
  }
}

export async function restorePurchases() {
  await ready();
  try {
    return await impl.restore();
  } catch (e) {
    throw toBillingError(e, 'Could not restore purchases.');
  }
}

export async function getCustomerInfo() {
  await ready();
  try {
    return await impl.getCustomerInfo();
  } catch (e) {
    throw toBillingError(e, 'Could not load your subscription.');
  }
}

function toDate(v) {
  if (!v) return null;
  const d = v instanceof Date ? v : new Date(v);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** True when the customer has the premium entitlement right now. */
export function hasPremiumAccess(customerInfo) {
  return !!customerInfo?.entitlements?.active?.[PREMIUM_ENTITLEMENT];
}

/**
 * FREE | ACTIVE | CANCELLED | EXPIRED | BILLING_ISSUE from the SDK's customer
 * info (same states the server stores).
 */
export function stateFromCustomerInfo(customerInfo) {
  const ent = customerInfo?.entitlements?.all?.[PREMIUM_ENTITLEMENT] || null;
  const active = customerInfo?.entitlements?.active?.[PREMIUM_ENTITLEMENT] || null;
  const e = active || ent;
  const base = {
    isPremium: !!active,
    expiresAt: toDate(e?.expirationDate),
    willRenew: !!(active && active.willRenew),
    productId: e?.productIdentifier || null,
    managementURL: customerInfo?.managementURL || null
  };
  if (!e) return { ...base, status: 'FREE' };
  const unsub = toDate(e.unsubscribeDetectedAt);
  const billing = toDate(e.billingIssueDetectedAt);
  if (billing && (!unsub || billing > unsub)) return { ...base, status: 'BILLING_ISSUE' };
  if (!active) return { ...base, status: 'EXPIRED' };
  if (!e.willRenew && base.expiresAt) return { ...base, status: 'CANCELLED' };
  return { ...base, status: 'ACTIVE' };
}

const STORE_MANAGE_URLS = {
  ios: 'https://apps.apple.com/account/subscriptions',
  android: 'https://play.google.com/store/account/subscriptions?package=app.cardflow.mobile'
};

/**
 * Opens where the customer manages or cancels the subscription (the store's
 * subscription page, or RevenueCat's Web Billing portal).
 */
export function openCustomerCenter(managementURL) {
  const url = managementURL || STORE_MANAGE_URLS[platform];
  if (!url) return false;
  window.open(url, '_blank', 'noopener');
  return true;
}

export { platform as billingPlatformName };
