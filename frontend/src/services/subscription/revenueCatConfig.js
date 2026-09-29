import { Capacitor } from '@capacitor/core';

// RevenueCat configuration. Only PUBLIC SDK keys belong here — they are
// injected at build time by webpack's DefinePlugin from the environment
// (REVENUECAT_IOS_API_KEY / REVENUECAT_ANDROID_API_KEY / REVENUECAT_WEB_API_KEY).
// The secret key (sk_...) and the webhook secret live on the Go server only.
//
// While the store accounts aren't set up, put the RevenueCat Test Store key
// (test_...) in these variables; swap in the real appl_ / goog_ / rcb_ keys later.

// CardFlow Premium is one entitlement. Never check product ids for access.
export const PREMIUM_ENTITLEMENT = 'premium';

const KEYS = {
  ios: process.env.REVENUECAT_IOS_API_KEY || '',
  android: process.env.REVENUECAT_ANDROID_API_KEY || '',
  web: process.env.REVENUECAT_WEB_API_KEY || ''
};

/** 'ios' | 'android' | 'web' */
export function billingPlatform() {
  try {
    const p = Capacitor.getPlatform();
    if (p === 'ios' || p === 'android') return p;
  } catch (e) {}
  return 'web';
}

export function isNativeBilling() {
  return billingPlatform() !== 'web';
}

export function revenueCatApiKey(platform = billingPlatform()) {
  return KEYS[platform] || '';
}

// Guard against a secret key being pasted into a client variable by mistake.
export function isPublicKey(key) {
  return !!key && !/^sk_/.test(key);
}
