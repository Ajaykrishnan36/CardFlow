// iOS / Android billing via the RevenueCat Capacitor SDK (App Store / Play Store).
// Loaded lazily so the web bundle never pulls in native plugin code paths.

let modPromise = null;
function sdk() {
  if (!modPromise) modPromise = import('@revenuecat/purchases-capacitor');
  return modPromise;
}

let listenerId = null;

export const nativeBilling = {
  async configure(apiKey, appUserId, onCustomerInfo) {
    const { Purchases, LOG_LEVEL } = await sdk();
    const { isConfigured } = await Purchases.isConfigured();
    if (!isConfigured) {
      if (process.env.NODE_ENV !== 'production') {
        await Purchases.setLogLevel({ level: LOG_LEVEL.DEBUG }).catch(() => {});
      }
      await Purchases.configure({ apiKey, appUserID: appUserId });
    } else {
      await Purchases.logIn({ appUserID: appUserId });
    }
    if (onCustomerInfo && listenerId == null) {
      listenerId = await Purchases.addCustomerInfoUpdateListener((info) => onCustomerInfo(info));
    }
  },

  async logIn(appUserId) {
    const { Purchases } = await sdk();
    const { customerInfo } = await Purchases.logIn({ appUserID: appUserId });
    return customerInfo;
  },

  async logOut() {
    const { Purchases } = await sdk();
    const { isConfigured } = await Purchases.isConfigured();
    if (!isConfigured) return;
    const { isAnonymous } = await Purchases.isAnonymous();
    // logOut on an anonymous user throws; nothing to do then.
    if (!isAnonymous) await Purchases.logOut();
  },

  async getOfferings() {
    const { Purchases } = await sdk();
    const offerings = await Purchases.getOfferings();
    const pkgs = offerings?.current?.availablePackages || [];
    return pkgs.map((p) => ({
      id: p.identifier,
      packageType: p.packageType,
      title: p.product?.title || p.identifier,
      description: p.product?.description || '',
      priceString: p.product?.priceString || '',
      period: p.product?.subscriptionPeriod || null, // ISO 8601, e.g. P1M
      productId: p.product?.identifier || '',
      raw: p
    }));
  },

  async purchase(pkg) {
    const { Purchases } = await sdk();
    const { customerInfo } = await Purchases.purchasePackage({ aPackage: pkg.raw });
    return customerInfo;
  },

  async restore() {
    const { Purchases } = await sdk();
    const { customerInfo } = await Purchases.restorePurchases();
    return customerInfo;
  },

  async getCustomerInfo() {
    const { Purchases } = await sdk();
    const { customerInfo } = await Purchases.getCustomerInfo();
    return customerInfo;
  },

  isCancelled(err) {
    return !!(err && (err.userCancelled || err.code === '1' || err.code === 1));
  }
};
