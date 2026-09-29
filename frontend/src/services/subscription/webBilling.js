// Browser billing via RevenueCat Web Billing (@revenuecat/purchases-js).
// Loaded lazily so native builds and non-paywall screens don't download it.

let modPromise = null;
function sdk() {
  if (!modPromise) modPromise = import('@revenuecat/purchases-js');
  return modPromise;
}

let instance = null;

export const webBilling = {
  async configure(apiKey, appUserId) {
    const { Purchases } = await sdk();
    if (instance && Purchases.isConfigured()) {
      if (instance.getAppUserId() !== appUserId) await instance.changeUser(appUserId);
      return;
    }
    instance = Purchases.configure({ apiKey, appUserId });
  },

  async logIn(appUserId) {
    if (!instance) throw new Error('RevenueCat is not configured');
    return instance.changeUser(appUserId);
  },

  async logOut() {
    // The web SDK has no anonymous logOut; drop the instance so the next
    // login configures a fresh one for the new user.
    if (instance) {
      try {
        instance.close();
      } catch (e) {}
    }
    instance = null;
  },

  async getOfferings() {
    const offerings = await instance.getOfferings();
    const pkgs = offerings?.current?.availablePackages || [];
    return pkgs.map((p) => {
      const product = p.webBillingProduct || p.rcBillingProduct || p.product || {};
      return {
        id: p.identifier,
        packageType: p.packageType,
        title: product.title || product.displayName || p.identifier,
        description: product.description || '',
        priceString: product.currentPrice?.formattedPrice || '',
        period: product.normalPeriodDuration || null, // ISO 8601, e.g. P1M
        productId: product.identifier || '',
        raw: p
      };
    });
  },

  async purchase(pkg) {
    const { customerInfo } = await instance.purchase({ rcPackage: pkg.raw });
    return customerInfo;
  },

  async restore() {
    // Web Billing purchases are tied to the App User ID, so "restore" is a
    // fresh read of the customer.
    return instance.getCustomerInfo();
  },

  async getCustomerInfo() {
    return instance.getCustomerInfo();
  },

  isCancelled(err) {
    return !!(err && err.errorCode === 1);
  }
};
