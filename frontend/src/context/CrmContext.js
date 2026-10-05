import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { useAuth } from './AuthContext';
import { crmApi, newIdempotencyKey } from '../services/crmApi';

// The CRM side of the app: which businesses the person belongs to, which one is open,
// and what they may do in it. A business is a CRM workspace; switching it switches every
// list, number and search on the CRM screens. Nothing is shared between two businesses.

const CrmContext = createContext(null);
const ACTIVE_KEY = 'cf_active_business';

function readActive() {
  try {
    return localStorage.getItem(ACTIVE_KEY) || '';
  } catch (e) {
    return '';
  }
}

export function CrmProvider({ children }) {
  const { isAuthenticated } = useAuth();
  const [businesses, setBusinesses] = useState([]);
  const [canCreate, setCanCreate] = useState(false);
  const [activeCode, setActiveCode] = useState(readActive);
  const [context, setContext] = useState(null);
  // loading | ready | error
  const [status, setStatus] = useState('loading');
  const [error, setError] = useState('');
  const load = useCallback(async () => {
    if (!isAuthenticated) return;
    setStatus((s) => (s === 'ready' ? s : 'loading'));
    try {
      const res = await crmApi.businesses();
      const list = (res.data || []).filter((b) => !b.isPlatform);
      setBusinesses(list);
      setCanCreate(Boolean(res.canCreate));
      setActiveCode((cur) => (list.some((b) => b.code === cur) ? cur : list[0]?.code || ''));
      setError('');
      setStatus('ready');
    } catch (e) {
      if (e.status === 401) return;
      setError(e.message || 'Could not load your businesses.');
      setStatus('error');
    }
  }, [isAuthenticated]);

  useEffect(() => {
    if (!isAuthenticated) {
      setBusinesses([]);
      setContext(null);
      setStatus('loading');
      return;
    }
    load();
  }, [isAuthenticated, load]);

  // What the person may do in the open business (permissions, menu, enabled modules).
  useEffect(() => {
    let cancelled = false;
    setContext(null);
    if (!activeCode || status !== 'ready') return undefined;
    try {
      localStorage.setItem(ACTIVE_KEY, activeCode);
    } catch (e) {}
    crmApi
      .context(activeCode)
      .then((c) => {
        if (!cancelled) setContext(c);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [activeCode, status]);

  const switchBusiness = useCallback((code) => setActiveCode(code), []);

  const createBusiness = useCallback(
    async (form) => {
      const res = await crmApi.createBusiness(form, newIdempotencyKey());
      await load();
      setActiveCode(res.business.code);
      return res.business;
    },
    [load]
  );

  const value = useMemo(() => {
    const active = businesses.find((b) => b.code === activeCode) || null;
    const objects = context?.effective?.objects || {};
    const singular = { leads: 'lead', accounts: 'account', contacts: 'contact' };
    const navKeys = new Set((context?.navigation || []).map((n) => n.path.split('/').pop()));
    return {
      status,
      error,
      businesses,
      canCreate,
      active,
      activeCode,
      context,
      navigation: context?.navigation || [],
      currency: active?.currency || 'INR',
      /** Whether an object is in this person's menu (enabled module + read permission). */
      has: (object) => navKeys.has(object),
      can: (object, action) => {
        const p = objects[singular[object] || object];
        if (!p) return false;
        return p.moduleEnabled !== false && Array.isArray(p.actions) && p.actions.includes(action);
      },
      reload: load,
      switchBusiness,
      createBusiness
    };
  }, [status, error, businesses, canCreate, activeCode, context, load, switchBusiness, createBusiness]);

  return <CrmContext.Provider value={value}>{children}</CrmContext.Provider>;
}

export function useCrm() {
  const ctx = useContext(CrmContext);
  if (!ctx) throw new Error('useCrm must be used within a CrmProvider');
  return ctx;
}
