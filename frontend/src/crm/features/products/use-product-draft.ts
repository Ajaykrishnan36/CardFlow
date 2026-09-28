import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ProductConfig, ProductDetail } from '@crm/api/types';
import { normalizeConfig } from './product-utils';

/** The editable part of a product: General fields + the draft configuration. */
export interface ProductDraft {
  name: string;
  description: string;
  icon: string;
  config: ProductConfig;
}

export function draftFromProduct(p: ProductDetail): ProductDraft {
  return { name: p.name, description: p.description ?? '', icon: p.icon || 'boxes', config: normalizeConfig(p.draftConfig) };
}

const same = (a: ProductDraft, b: ProductDraft) => JSON.stringify(a) === JSON.stringify(b);

/**
 * Local working copy of a product's draft. It lives on the detail page (not inside the
 * wizard) so switching tabs never throws away edits. Server updates flow in only while
 * there are no local edits, so a background refetch can't clobber unsaved work.
 */
export function useProductDraft(product: ProductDetail) {
  const serverDraft = useMemo(() => draftFromProduct(product), [product]);
  const [draft, setDraft] = useState<ProductDraft>(serverDraft);
  const prev = useRef(serverDraft);

  useEffect(() => {
    const before = prev.current;
    prev.current = serverDraft;
    setDraft((d) => (same(d, before) ? serverDraft : d));
  }, [serverDraft]);

  const dirty = !same(draft, serverDraft);

  // Warn before closing the tab with unsaved setup changes.
  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [dirty]);

  const updateConfig = useCallback((patch: Partial<ProductConfig>) => setDraft((d) => ({ ...d, config: { ...d.config, ...patch } })), []);
  const reset = useCallback(() => setDraft(serverDraft), [serverDraft]);

  return { draft, setDraft, updateConfig, dirty, reset };
}
