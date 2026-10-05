import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { apiClient, currentSessionToken } from '../services/api';
import { crmApi } from '../services/crmApi';
import { syncAuthNotifications } from '../utils/pushNotifications';
import * as subscriptionService from '../services/subscription/subscriptionService';

const AuthContext = createContext(null);

// The signed-in person's app profile (cards, listings, premium) for the phone layout.
// Signing in and out happens once for the whole site (D-104): this provider starts from
// the session that already exists and asks the site to end it.
//   onUnavailable — there is no app profile for this session (the phone layout can't run)
//   onSignOut     — end the session and go to the sign-in page
export function AuthProvider({ children, onUnavailable, onSignOut }) {
  const [user, setUser] = useState(null);
  const [role, setRoleState] = useState(null); // 'user' | 'owner'
  // The app has no admin console (the CRM manages the app); an account still marked
  // "admin" on the server gets the normal user flow.
  const setRole = (r) => setRoleState(r === 'admin' ? 'user' : r);
  const [token, setToken] = useState(null);
  const [isLoading, setIsLoading] = useState(false);
  const [savedCards, setSavedCards] = useState([]);
  const [myBusinesses, setMyBusinesses] = useState([]);
  // Global paywall overlay — any screen can call openSubscription() to show
  // the full plan-chooser without needing its own navigation route.
  const [subscriptionOverlayOpen, setSubscriptionOverlayOpen] = useState(false);
  const openSubscription = useCallback(() => setSubscriptionOverlayOpen(true), []);
  const closeSubscription = useCallback(() => setSubscriptionOverlayOpen(false), []);

  // Load user's saved card vault
  const loadUserVault = useCallback(async (authToken) => {
    const currentToken = authToken || token;
    if (!currentToken) {
      setSavedCards([]);
      return;
    }
    try {
      const cards = await apiClient.getCards(currentToken);
      if (cards && Array.isArray(cards)) {
        setSavedCards(cards);
      }
    } catch (e) {
      console.warn('Could not load saved cards from database', e);
      setSavedCards([]);
    }
  }, [token]);

  // The person's public listings (one per business). The server is the only source.
  const loadMyBusinesses = useCallback(async (authToken, currentUser) => {
    const currentToken = authToken || token;
    const u = currentUser || user;
    if (!currentToken || !u) {
      setMyBusinesses([]);
      return;
    }
    try {
      const list = await apiClient.getMyBusinesses(currentToken);
      setMyBusinesses(Array.isArray(list) ? list : []);
    } catch (e) {
      console.warn('Could not load businesses from API', e);
      setMyBusinesses([]);
    }
  }, [token]);

  const addMyBusiness = useCallback(async (bizData) => {
    const created = await apiClient.createMyBusiness(bizData, token);
    const newBiz = {
      ...created,
      id: created?.id || created?.ID,
      name: created?.name || bizData.business_name,
      business_name: created?.name || bizData.business_name,
      category: bizData.category,
      city: created?.city || bizData.city,
      district: bizData.district,
      state: created?.state || bizData.state,
      address: created?.address_line1 || bizData.address,
      phone: bizData.phone,
      whatsapp: bizData.whatsapp,
      email: bizData.email,
      website: bizData.website,
      gstin: bizData.gstin,
      verification: created?.verification || 'pending',
      status: created?.status || 'live',
      card_image_url: bizData.front_image_data || '',
      card_back_image_url: bizData.back_image_data || ''
    };
    setMyBusinesses((prev) => [newBiz, ...prev]);
    return newBiz;
  }, [token, user]);

  const updateMyBusiness = useCallback(async (bizId, bizData) => {
    const updated = await apiClient.updateMyBusiness(bizId, bizData, token);
    const next = {
      id: bizId,
      ...updated,
      name: updated?.name || bizData.business_name || bizData.name,
      business_name: updated?.name || bizData.business_name || bizData.name,
      category: bizData.category,
      city: updated?.city || bizData.city,
      state: updated?.state || bizData.state,
      address: updated?.address_line1 || bizData.address,
      phone: bizData.phone,
      whatsapp: bizData.whatsapp,
      email: bizData.email,
      website: bizData.website,
      gstin: bizData.gstin,
      description: bizData.description,
      services: Array.isArray(bizData.services) ? bizData.services : (bizData.services || '').split(',').map((s) => s.trim()).filter(Boolean),
      verification: updated?.verification || 'pending',
      card_image_url: bizData.front_image_data
        ? `/api/v1/owner/businesses/${bizId}/card-image?side=front`
        : undefined,
      card_back_image_url: bizData.back_image_data
        ? `/api/v1/owner/businesses/${bizId}/card-image?side=back`
        : undefined
    };
    setMyBusinesses((prev) => prev.map((b) => (String(b.id) === String(bizId) ? { ...b, ...next } : b)));
    return next;
  }, [token]);

  const sessionRestoredRef = useRef(false);
  const [authReady, setAuthReady] = useState(false);

  // Start from the site's session: ask the server who this is. Nothing about the session
  // is kept in the browser's storage.
  useEffect(() => {
    if (sessionRestoredRef.current) return;
    sessionRestoredRef.current = true;
    const sessionToken = currentSessionToken();
    (async () => {
      try {
        const [profile, me] = await Promise.all([apiClient.getMe(sessionToken), crmApi.me().catch(() => null)]);
        const account = {
          id: profile.id || null,
          phone: String(profile.phone || '').replace('+91', ''),
          role: 'user',
          name: me?.identity?.displayName || profile.name || '',
          email: me?.identity?.email || '',
          emailVerified: Boolean(me?.identity?.emailVerified),
          phoneVerified: Boolean(me?.identity?.phoneVerified),
          hasPassword: Boolean(me?.identity?.hasPassword),
          city: profile.city || '',
          state: profile.state || '',
          plan: profile.plan || 'free',
          freeScansRemaining: profile.free_scans_remaining != null ? profile.free_scans_remaining : 30,
          credits: profile.credit_balance != null ? profile.credit_balance : 0,
          isIdVerified: profile.is_id_verified || false,
          isSubscribed: profile.is_subscribed || false,
          subscriptionPlanId: profile.subscription_plan_id || null,
          subscriptionExpiresAt: profile.subscription_expires_at || null
        };
        setUser(account);
        setRole('user');
        setToken(sessionToken);
        loadUserVault(sessionToken);
        loadMyBusinesses(sessionToken, account);
      } catch (e) {
        // Signed out, or signed in without an app profile (an email-only account).
        if (onUnavailable) onUnavailable();
      } finally {
        setAuthReady(true);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Re-read name, email and what is verified after the person changes them.
  const refreshAccount = useCallback(async () => {
    const me = await crmApi.me();
    setUser((u) => (u ? {
      ...u,
      name: me.identity.displayName || u.name,
      email: me.identity.email || '',
      phone: String(me.identity.phone || u.phone || '').replace('+91', ''),
      emailVerified: Boolean(me.identity.emailVerified),
      phoneVerified: Boolean(me.identity.phoneVerified),
      hasPassword: Boolean(me.identity.hasPassword)
    } : u));
    return me;
  }, []);

  const [lastSentOtp, setLastSentOtp] = useState('');

  // A code for changing the mobile number on the account (sign-in codes are the sign-in page's job).
  const sendOtp = async (phone) => {
    setIsLoading(true);
    try {
      const res = await crmApi.requestPhoneChange(phone);
      setLastSentOtp(res?.devCode || '');
      return { success: true, message: 'OTP sent successfully', otp: res?.devCode || '' };
    } catch (e) {
      return { success: false, error: (e.fieldErrors && Object.values(e.fieldErrors)[0]) || e.message || "Couldn't send OTP. Please try again." };
    } finally {
      setIsLoading(false);
    }
  };

  const updateProfile = useCallback(async (fields) => {
    const payload = { name: fields.name, city: fields.city, state: fields.state };
    let updated = null;
    if (token) {
      updated = await apiClient.updateProfile(payload, token);
    }
    const merged = {
      ...user,
      name: updated?.name ?? fields.name ?? user?.name,
      city: updated?.city ?? fields.city ?? user?.city,
      state: updated?.state ?? fields.state ?? user?.state
    };
    setUser(merged);
    return merged;
  }, [user, token]);

  // Updates the logged-in user's own mobile number after OTP verification —
  // distinct from sendOtp/verifyOtp (login), this never changes the session.
  const changePhone = useCallback(async (newPhone, otpCode) => {
    const updated = await apiClient.changePhone(newPhone, otpCode, token);
    const merged = { ...user, phone: String(updated?.phone ?? user?.phone ?? '').replace('+91', '') };
    setUser(merged);
    return merged;
  }, [user, token]);

  // ---------------------------------------------------------------------
  // Pro (RevenueCat)
  // The RevenueCat App User ID is the signed-in CardFlow user id (returned by
  // the server). Premium access comes from the server's stored state, which
  // it builds from RevenueCat webhooks / REST — the client never grants it.
  // ---------------------------------------------------------------------
  const [subscription, setSubscription] = useState(null); // server /billing/status
  const [isPurchasing, setIsPurchasing] = useState(false);
  const purchasingRef = useRef(false);
  const tokenRef = useRef(token);
  tokenRef.current = token;

  const applyServerStatus = useCallback((st) => {
    if (!st) return;
    setSubscription(st);
    setUser((prev) => {
      if (!prev) return prev;
      const merged = {
        ...prev,
        id: st.app_user_id || prev.id,
        isSubscribed: !!st.is_premium,
        subscriptionStatus: st.status,
        subscriptionPlanId: st.product_id || null,
        subscriptionExpiresAt: st.expires_at || null
      };
      return merged;
    });
  }, []);

  const refreshSubscription = useCallback(async ({ sync = false } = {}) => {
    const t = tokenRef.current;
    if (!t) return null;
    const st = sync ? await apiClient.syncBilling(t) : await apiClient.getBillingStatus(t);
    applyServerStatus(st);
    return st;
  }, [applyServerStatus]);

  // Log RevenueCat in whenever a session starts (login or restore), out on logout.
  useEffect(() => {
    if (!token || token.startsWith('cf_token_')) {
      setSubscription(null);
      subscriptionService.reset();
      return undefined;
    }
    let alive = true;
    let syncTimer = null;
    apiClient
      .getBillingStatus(token)
      .then((st) => {
        if (!alive) return;
        applyServerStatus(st);
        // The SDK tells us when the store reports a change (renewal,
        // purchase on another device…) — ask the server to re-sync.
        return subscriptionService.identify(st.app_user_id, () => {
          clearTimeout(syncTimer);
          syncTimer = setTimeout(() => {
            if (alive && !purchasingRef.current) refreshSubscription({ sync: true }).catch(() => {});
          }, 1500);
        });
      })
      .catch((e) => console.warn('Could not load subscription status', e));
    return () => {
      alive = false;
      clearTimeout(syncTimer);
    };
  }, [token, applyServerStatus, refreshSubscription]);

  // After a purchase/restore the server may learn about it a moment later
  // (webhook). Re-sync until it confirms, for up to ~20 seconds.
  const waitForServerPremium = useCallback(async () => {
    for (let i = 0; i < 8; i += 1) {
      try {
        const st = await refreshSubscription({ sync: true });
        if (st?.is_premium) return st;
      } catch (e) {}
      await new Promise((r) => setTimeout(r, 2500));
    }
    return null;
  }, [refreshSubscription]);

  const loadOfferings = useCallback(() => subscriptionService.getOfferings(), []);

  // Buys a RevenueCat package. Resolves { status: 'active' | 'pending' }.
  // Throws BillingError (code 'cancelled' when the user backs out).
  const purchasePackage = useCallback(async (pkg) => {
    if (!tokenRef.current) throw new subscriptionService.BillingError('failed', 'Please sign in first.');
    if (purchasingRef.current) throw new subscriptionService.BillingError('busy', 'A purchase is already in progress.');
    purchasingRef.current = true;
    setIsPurchasing(true);
    try {
      const info = await subscriptionService.purchaseSubscription(pkg);
      if (!subscriptionService.hasPremiumAccess(info)) {
        // e.g. an Ask-to-Buy / pending payment — nothing granted yet.
        return { status: 'pending' };
      }
      const st = await waitForServerPremium();
      return { status: st?.is_premium ? 'active' : 'pending' };
    } finally {
      purchasingRef.current = false;
      setIsPurchasing(false);
    }
  }, [waitForServerPremium]);

  // Resolves { restored: boolean }.
  const restorePurchases = useCallback(async () => {
    if (purchasingRef.current) throw new subscriptionService.BillingError('busy', 'A purchase is already in progress.');
    purchasingRef.current = true;
    setIsPurchasing(true);
    try {
      const info = await subscriptionService.restorePurchases();
      if (!subscriptionService.hasPremiumAccess(info)) {
        await refreshSubscription({ sync: true }).catch(() => {});
        return { restored: false };
      }
      const st = await waitForServerPremium();
      return { restored: !!st?.is_premium };
    } finally {
      purchasingRef.current = false;
      setIsPurchasing(false);
    }
  }, [refreshSubscription, waitForServerPremium]);

  // Store subscription page / Web Billing portal (cancel, change plan, update payment).
  const manageSubscription = useCallback(async () => {
    let url = subscription?.management_url || null;
    try {
      const info = await subscriptionService.getCustomerInfo();
      url = info?.managementURL || url;
    } catch (e) {}
    return subscriptionService.openCustomerCenter(url);
  }, [subscription]);

  // Server state wins; the cached user flags only cover the moment before it loads.
  const isPremiumActive = subscription
    ? !!subscription.is_premium
    : !!(
        user?.isSubscribed &&
        (!user?.subscriptionExpiresAt || new Date(user.subscriptionExpiresAt) > new Date())
      );

  // Re-arms the local push-notification queue whenever the caller's auth
  // state changes: logged out -> "please log in", logged in free -> "go
  // premium", logged in premium -> "back up your contacts". Waits for the
  // session-restore effect above so it never briefly fires "logged out"
  // for a user who was actually still signed in.
  useEffect(() => {
    if (!authReady) return;
    const authState = !user || !token ? 'logged_out' : isPremiumActive ? 'premium' : 'free';
    syncAuthNotifications(authState);
  }, [authReady, user, token, isPremiumActive]);

  // Helper to check if a business is already saved in this user's vault.
  // GSTIN is checked first since it's the one field that's actually unique
  // per business — the name/phone fuzzy-match is only a fallback for
  // businesses that have no GSTIN on file yet.
  const isBusinessSaved = useCallback((biz) => {
    if (!biz || !savedCards || savedCards.length === 0) return false;
    const bGstin = (biz.gstin || '').toUpperCase().trim();
    if (bGstin) {
      const gstinMatch = savedCards.some((card) => (card.gstin || '').toUpperCase().trim() === bGstin);
      if (gstinMatch) return true;
    }
    const bName = (biz.name || '').toLowerCase().trim();
    const bPhone = (biz.phone || '').replace(/\D/g, '');

    return savedCards.some((card) => {
      const cCompany = (card.company || card.person_name || '').toLowerCase().trim();
      const cPhone = (card.phones?.[0]?.raw || card.phones?.[0]?.e164 || '').replace(/\D/g, '');
      if (bName && cCompany && (cCompany.includes(bName) || bName.includes(cCompany))) return true;
      if (bPhone && cPhone && (cPhone.includes(bPhone) || bPhone.includes(cPhone))) return true;
      return false;
    });
  }, [savedCards]);

  // Finds the saved-card entry that corresponds to a given business, using
  // the same GSTIN-first / fuzzy-fallback matching as isBusinessSaved.
  const findSavedCardForBusiness = useCallback((biz) => {
    if (!biz || !savedCards || savedCards.length === 0) return null;
    const bGstin = (biz.gstin || '').toUpperCase().trim();
    if (bGstin) {
      const byGstin = savedCards.find((card) => (card.gstin || '').toUpperCase().trim() === bGstin);
      if (byGstin) return byGstin;
    }
    const bName = (biz.name || '').toLowerCase().trim();
    const bPhone = (biz.phone || '').replace(/\D/g, '');
    return savedCards.find((card) => {
      const cCompany = (card.company || card.person_name || '').toLowerCase().trim();
      const cPhone = (card.phones?.[0]?.raw || card.phones?.[0]?.e164 || '').replace(/\D/g, '');
      if (bName && cCompany && (cCompany.includes(bName) || bName.includes(cCompany))) return true;
      if (bPhone && cPhone && (cPhone.includes(bPhone) || bPhone.includes(cPhone))) return true;
      return false;
    }) || null;
  }, [savedCards]);

  // Save a business card directly from discovery into user vault
  const saveBusinessToVault = async (biz) => {
    if (!biz || !token) return;
    const payload = {
      person_name: biz.name || 'Business Contact',
      designation: 'Owner / Partner',
      company: biz.name || 'Business Enterprise',
      website: `https://cardflow.app/b/${biz.slug || ''}`,
      notes: `Saved from Discover Businesses (${biz.category || ''})`,
      met_context: 'Discover Directory',
      source: 'BUSINESS_PROFILE',
      gstin: biz.gstin || '',
      phones: biz.phone ? [{ raw: biz.phone, e164: biz.phone.replace(/[^0-9+]/g, ''), type: 'work', is_whatsapp: true }] : [],
      emails: biz.email ? [biz.email] : [],
      raw_address: biz.address || 'Coimbatore, Tamil Nadu',
      tags: [biz.category || 'Verified Business', 'Directory Lead']
    };

    const saved = await apiClient.saveCard(payload, token);
    await loadUserVault(token);
    return saved;
  };

  // Un-saves a previously-saved business (toggling the Save button back off).
  const unsaveBusinessFromVault = async (biz) => {
    if (!biz || !token) return;
    const existing = findSavedCardForBusiness(biz);
    if (!existing?.id) return;
    await apiClient.deleteCard(existing.id, token);
    await loadUserVault(token);
  };

  // Whether a shared card (opened via a "/share/{id}" link) is already in
  // this user's vault — GSTIN-first, same priority as isBusinessSaved.
  const isSharedCardSaved = useCallback((sharedCard) => {
    if (!sharedCard || !savedCards || savedCards.length === 0) return false;
    const sGstin = (sharedCard.gstin || '').toUpperCase().trim();
    if (sGstin) {
      if (savedCards.some((card) => (card.gstin || '').toUpperCase().trim() === sGstin)) return true;
    }
    const sName = (sharedCard.person_name || sharedCard.company || '').toLowerCase().trim();
    const sPhone = (sharedCard.phones?.[0]?.raw || sharedCard.phones?.[0]?.e164 || '').replace(/\D/g, '');
    return savedCards.some((card) => {
      const cCompany = (card.company || card.person_name || '').toLowerCase().trim();
      const cPhone = (card.phones?.[0]?.raw || card.phones?.[0]?.e164 || '').replace(/\D/g, '');
      if (sName && cCompany && (cCompany.includes(sName) || sName.includes(cCompany))) return true;
      if (sPhone && cPhone && (cPhone.includes(sPhone) || sPhone.includes(cPhone))) return true;
      return false;
    });
  }, [savedCards]);

  // Saves a card someone shared via a "/share/{id}" link into this user's
  // own vault. The backend's GSTIN dedup (in CreateSavedCard) still applies,
  // so this links to the same business record if one already matches.
  const saveSharedCardToVault = async (sharedCard) => {
    if (!sharedCard || !token) return;
    const payload = {
      person_name: sharedCard.person_name || '',
      designation: sharedCard.designation || '',
      company: sharedCard.company || '',
      website: sharedCard.website || '',
      notes: '',
      met_context: 'Received via shared card link',
      source: 'SCANNED',
      gstin: sharedCard.gstin || '',
      phones: sharedCard.phones || [],
      emails: sharedCard.emails || [],
      raw_address: sharedCard.raw_address || ''
    };
    const saved = await apiClient.saveCard(payload, token);
    await loadUserVault(token);
    return saved;
  };

  const logout = () => {
    subscriptionService.reset();
    setSubscription(null);
    setUser(null);
    setRole(null);
    setToken(null);
    setSavedCards([]);
    setMyBusinesses([]);
    // The site ends the session on the server and shows the sign-in page.
    if (onSignOut) onSignOut();
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        role,
        token,
        isAuthenticated: !!user,
        authReady,
        refreshAccount,
        isLoading,
        lastSentOtp,
        savedCards,
        myBusinesses,
        isBusinessSaved,
        saveBusinessToVault,
        unsaveBusinessFromVault,
        isSharedCardSaved,
        saveSharedCardToVault,
        loadUserVault,
        loadMyBusinesses,
        addMyBusiness,
        updateMyBusiness,
        sendOtp,
        updateProfile,
        changePhone,
        isPremiumActive,
        subscription,
        isPurchasing,
        loadOfferings,
        purchasePackage,
        restorePurchases,
        refreshSubscription,
        manageSubscription,
        subscriptionOverlayOpen,
        openSubscription,
        closeSubscription,
        logout,
        setUser
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
