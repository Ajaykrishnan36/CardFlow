import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { mockBusinesses } from '../data/mockData';
import { apiClient } from '../services/api';
import { syncAuthNotifications } from '../utils/pushNotifications';
import * as subscriptionService from '../services/subscription/subscriptionService';

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [role, setRoleState] = useState(null); // 'user' | 'owner'
  // The app has no admin console (the CRM manages the app); an account still marked
  // "admin" on the server gets the normal user flow.
  const setRole = (r) => setRoleState(r === 'admin' ? 'user' : r);
  const [token, setToken] = useState(null);
  const [activeBusinessId, setActiveBusinessId] = useState('biz-1');
  const [isLoading, setIsLoading] = useState(false);
  const [pendingPhone, setPendingPhone] = useState('');
  const [isNewUser, setIsNewUser] = useState(false);
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

  // Load user's owned businesses (multiple per user supported)
  const loadMyBusinesses = useCallback(async (authToken, currentUser) => {
    const currentToken = authToken || token;
    const u = currentUser || user;
    if (!currentToken || !u) {
      setMyBusinesses([]);
      return;
    }
    try {
      const list = await apiClient.getMyBusinesses(currentToken);
      if (list && Array.isArray(list) && list.length > 0) {
        setMyBusinesses(list);
        try {
          localStorage.setItem(`cf_biz_${u.phone}`, JSON.stringify(list));
        } catch (e) {}
        return;
      }
    } catch (e) {
      console.warn('Could not load businesses from API', e);
    }
    // Fallback: dev owner accounts or localStorage
    try {
      const cached = localStorage.getItem(`cf_biz_${u.phone}`);
      if (cached) {
        setMyBusinesses(JSON.parse(cached));
        return;
      }
    } catch (e) {}
    // Seed businesses for dev owner test accounts
    if (u.ownedBusinessIds?.length) {
      const seeded = mockBusinesses.filter(
        (b) => u.ownedBusinessIds.includes(b.id) || b.ownerPhone === u.phone || b.ownerPhone === `+91${u.phone}`
      );
      setMyBusinesses(seeded);
    } else {
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
    setMyBusinesses((prev) => {
      const updated = [newBiz, ...prev];
      try {
        localStorage.setItem(`cf_biz_${user?.phone}`, JSON.stringify(updated.map((b) => ({
          ...b,
          card_image_url: b.card_image_url ? '[stored]' : '',
          card_back_image_url: b.card_back_image_url ? '[stored]' : '',
          front_image_data: undefined,
          back_image_data: undefined
        }))));
      } catch (e) {}
      return updated;
    });
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

  // Restore session once on app startup — must NOT depend on loadUserVault/loadMyBusinesses
  // (those callbacks change when token/user updates, which caused an infinite API loop)
  useEffect(() => {
    if (sessionRestoredRef.current) return;
    sessionRestoredRef.current = true;

    try {
      const savedUser = localStorage.getItem('cf_user');
      const savedToken = localStorage.getItem('cf_token');
      if (savedUser && savedToken) {
        const parsed = JSON.parse(savedUser);
        setUser(parsed);
        setRole(parsed.role);
        setToken(savedToken);
        if (parsed.role === 'owner' && parsed.ownedBusinessIds?.length) {
          setActiveBusinessId(parsed.ownedBusinessIds[0]);
        }
        // Pass token/user explicitly — do not rely on stale closure state
        apiClient.getCards(savedToken).then((cards) => {
          if (cards && Array.isArray(cards)) setSavedCards(cards);
        });
        apiClient.getMyBusinesses(savedToken).then((list) => {
          if (list && Array.isArray(list) && list.length > 0) {
            setMyBusinesses(list);
            return;
          }
          try {
            const cached = localStorage.getItem(`cf_biz_${parsed.phone}`);
            if (cached) {
              setMyBusinesses(JSON.parse(cached));
              return;
            }
          } catch (e) {}
          if (parsed.ownedBusinessIds?.length) {
            setMyBusinesses(
              mockBusinesses.filter(
                (b) => parsed.ownedBusinessIds.includes(b.id) || b.ownerPhone === parsed.phone || b.ownerPhone === `+91${parsed.phone}`
              )
            );
          }
        }).catch(() => {});
      }
    } catch (e) {
      console.warn('Could not read session storage', e);
    } finally {
      setAuthReady(true);
    }
  }, []);

  const [lastSentOtp, setLastSentOtp] = useState('');

  const sendOtp = async (phone) => {
    setIsLoading(true);
    setPendingPhone(phone);
    try {
      const res = await apiClient.sendOtp(phone);
      if (res?.status === 'error' || res?.error) {
        setIsLoading(false);
        return {
          success: false,
          error: res?.error?.message || res?.error || "Couldn't send OTP. Please try again."
        };
      }
      const code = res?.data?.otp_preview || res?.otp_preview || '';
      setLastSentOtp(code);
      setIsLoading(false);
      return { success: true, message: 'OTP sent successfully', otp: code };
    } catch (e) {
      setIsLoading(false);
      return { success: false, error: "Couldn't send OTP. Please try again." };
    }
  };

  const verifyOtp = async (phone, enteredOtp, options = {}) => {
    setIsLoading(true);
    try {
      const apiRes = await apiClient.verifyOtp(phone, enteredOtp);
      const errMsg = apiRes?.error?.message || apiRes?.error || null;
      const hasToken = !!(apiRes?.data?.access_token || apiRes?.access_token || apiRes?.data?.user || apiRes?.user);
      if (!apiRes || apiRes.status === 'error' || (errMsg && !hasToken)) {
        setIsLoading(false);
        return {
          success: false,
          error: typeof errMsg === 'string' ? errMsg : 'Invalid OTP. Please check the code and try again.'
        };
      }

      // Build session only from API / database user payload
      let matchedAccount = null;
      let isBrandNew = false;

      const apiUser = apiRes?.data?.user || apiRes?.user;
      if (apiUser) {
        matchedAccount = {
          id: apiUser.id || null,
          phone: (apiUser.phone || phone).replace('+91', ''),
          role: apiUser.role || 'user',
          name: apiUser.name || 'CardFlow User',
          city: apiUser.city || 'Coimbatore',
          state: apiUser.state || 'Tamil Nadu',
          plan: apiUser.plan || 'free',
          freeScansRemaining: apiUser.free_scans_remaining != null ? apiUser.free_scans_remaining : 30,
          credits: apiUser.credit_balance != null ? apiUser.credit_balance : 10,
          isIdVerified: apiUser.is_id_verified || false,
          isSubscribed: apiUser.is_subscribed || false,
          subscriptionPlanId: apiUser.subscription_plan_id || null,
          subscriptionExpiresAt: apiUser.subscription_expires_at || null,
          isNewUser: apiRes?.data?.is_new_user || apiRes?.is_new_user || false,
          // A business created from someone scanning this person's card is now theirs.
          suggestedName: apiRes?.data?.suggested_name || '',
          claimedBusinesses: apiRes?.data?.claimed_businesses || []
        };
        isBrandNew = matchedAccount.isNewUser;
      }

      if (!matchedAccount) {
        isBrandNew = !!(apiRes?.data?.is_new_user || apiRes?.is_new_user);
        matchedAccount = {
          phone,
          role: 'user',
          name: 'CardFlow User',
          city: 'Coimbatore',
          state: 'Tamil Nadu',
          plan: 'free',
          freeScansRemaining: 30,
          credits: 10,
          isIdVerified: false,
          isSubscribed: false,
          subscriptionPlanId: null,
          subscriptionExpiresAt: null,
          isNewUser: isBrandNew
        };
      }

      if (typeof options.beforeCommit === 'function') {
        await options.beforeCommit();
      }

      const liveJwt = apiRes?.data?.access_token || apiRes?.access_token || `cf_token_${matchedAccount.phone}`;
      setUser(matchedAccount);
      setRole(matchedAccount.role);
      setToken(liveJwt);
      setIsNewUser(isBrandNew);

      if (matchedAccount.role === 'owner' && matchedAccount.ownedBusinessIds?.length) {
        setActiveBusinessId(matchedAccount.ownedBusinessIds[0]);
      }

      try {
        localStorage.setItem('cf_user', JSON.stringify(matchedAccount));
        localStorage.setItem('cf_token', liveJwt);
      } catch (e) {}

      loadUserVault(liveJwt);
      loadMyBusinesses(liveJwt, matchedAccount);

      setIsLoading(false);
      return { success: true, user: matchedAccount, isNewUser: isBrandNew };
    } catch (e) {
      setIsLoading(false);
      return { success: false, error: 'Something went wrong. Please try again.' };
    }
  };

  const completeOnboarding = async (profileData) => {
    const updatedUser = {
      ...user,
      name: profileData.name || user?.name || 'CardFlow User',
      role: user?.role || 'user',
      isNewUser: false
    };

    setUser(updatedUser);
    setRole(updatedUser.role);
    setIsNewUser(false);

    try {
      localStorage.setItem('cf_user', JSON.stringify(updatedUser));
    } catch (e) {}

    if (token) {
      try {
        await apiClient.updateProfile({ name: updatedUser.name }, token);
      } catch (e) {
        console.warn('Could not persist onboarding name', e);
      }
    }
  };

  const updateProfile = useCallback(async (fields) => {
    const payload = {
      name: fields.name,
      email: fields.email || null,
      city: fields.city,
      state: fields.state
    };
    let updated = null;
    if (token) {
      updated = await apiClient.updateProfile(payload, token);
    }
    const merged = {
      ...user,
      name: updated?.name ?? fields.name ?? user?.name,
      email: updated?.email ?? fields.email ?? user?.email,
      city: updated?.city ?? fields.city ?? user?.city,
      state: updated?.state ?? fields.state ?? user?.state
    };
    setUser(merged);
    try {
      localStorage.setItem('cf_user', JSON.stringify(merged));
    } catch (e) {}
    return merged;
  }, [user, token]);

  // Updates the logged-in user's own mobile number after OTP verification —
  // distinct from sendOtp/verifyOtp (login), this never changes the session.
  const changePhone = useCallback(async (newPhone, otpCode) => {
    const updated = await apiClient.changePhone(newPhone, otpCode, token);
    const merged = { ...user, phone: updated?.phone ?? user?.phone };
    setUser(merged);
    try {
      localStorage.setItem('cf_user', JSON.stringify(merged));
    } catch (e) {}
    return merged;
  }, [user, token]);

  // ---------------------------------------------------------------------
  // CardFlow Premium (RevenueCat)
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
      try {
        localStorage.setItem('cf_user', JSON.stringify(merged));
      } catch (e) {}
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
    setPendingPhone('');
    setIsNewUser(false);
    setSavedCards([]);
    setMyBusinesses([]);
    try {
      localStorage.removeItem('cf_user');
      localStorage.removeItem('cf_token');
    } catch (e) {}
  };

  const switchActiveBusiness = (bizId) => {
    setActiveBusinessId(bizId);
  };

  const switchToOwnerMode = (newBizData = null) => {
    const updated = {
      ...user,
      role: 'owner',
      plan: user?.plan === 'free' ? 'plus' : user?.plan || 'plus',
      ownedBusinessIds: user?.ownedBusinessIds && user.ownedBusinessIds.length > 0
        ? user.ownedBusinessIds
        : ['biz-1', 'biz-3']
    };
    if (newBizData && newBizData.businessName) {
      updated.businessName = newBizData.businessName;
    }
    setUser(updated);
    setRole('owner');
    try {
      localStorage.setItem('cf_user', JSON.stringify(updated));
    } catch (e) {}
  };

  const switchToUserMode = () => {
    const updated = {
      ...user,
      role: 'user'
    };
    setUser(updated);
    setRole('user');
    try {
      localStorage.setItem('cf_user', JSON.stringify(updated));
    } catch (e) {}
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        role,
        token,
        isAuthenticated: !!user,
        isNewUser,
        isLoading,
        pendingPhone,
        lastSentOtp,
        activeBusinessId,
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
        verifyOtp,
        completeOnboarding,
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
        switchActiveBusiness,
        switchToOwnerMode,
        switchToUserMode,
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
