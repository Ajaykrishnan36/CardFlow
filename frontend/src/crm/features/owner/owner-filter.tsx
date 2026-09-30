import { useMemo, useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { platformApi } from '@crm/api/endpoints';
import { X } from 'lucide-react';
import { Button } from '@crm/components/ui/button';
import { Select } from '@crm/components/ui/form-controls';
import { cn } from '@crm/lib/utils';

// Owner console filter (D-74): a product, then one of its apps. "All" by default.
// The dashboard, Products, Apps and the record lists follow it. Kept in this browser.
// The picker sits in each page's header, not the sidebar (D-86).

export interface OwnerFilter {
  product: string; // workspace id, '' = all products
  productCode: string;
  app: string; // setup id, '' = all apps
}

const KEY = 'crm.ownerFilter';
const EMPTY: OwnerFilter = { product: '', productCode: '', app: '' };
const listeners = new Set<() => void>();
let current: OwnerFilter | null = null;

function load(): OwnerFilter {
  if (current) return current;
  try {
    const raw = localStorage.getItem(KEY);
    current = raw ? { ...EMPTY, ...(JSON.parse(raw) as Partial<OwnerFilter>) } : EMPTY;
  } catch {
    current = EMPTY;
  }
  return current;
}

export function setOwnerFilter(f: OwnerFilter) {
  current = f;
  try {
    localStorage.setItem(KEY, JSON.stringify(f));
  } catch {
    /* private mode */
  }
  listeners.forEach((l) => l());
}

export function useOwnerFilter(): OwnerFilter {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    load
  );
}

/** Query params for owner endpoints (?product=&app=). */
export function ownerFilterParams(f: OwnerFilter): { product?: string; app?: string } {
  // An app only counts inside its product (the App picker opens after a product is chosen).
  return { product: f.product || undefined, app: (f.product && f.app) || undefined };
}

/** Products (with their apps) the owner can filter by, from the apps list. */
export function useOwnerProducts(enabled = true) {
  const apps = useQuery({ queryKey: ['platform', 'apps', 'all'], queryFn: () => platformApi.apps(), staleTime: 60_000, enabled });
  const products = useMemo(() => {
    const seen = new Map<string, { id: string; code: string; name: string }>();
    for (const a of apps.data ?? []) seen.set(a.workspaceId, { id: a.workspaceId, code: a.workspaceCode, name: a.workspaceName });
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [apps.data]);
  return { apps: apps.data ?? [], products };
}

/** Sets the filter and refreshes everything that follows it. */
export function useChangeOwnerFilter() {
  const qc = useQueryClient();
  return (next: OwnerFilter) => {
    setOwnerFilter(next);
    void qc.invalidateQueries({ queryKey: ['platform'] });
    void qc.invalidateQueries({ queryKey: ['workspaces'] });
  };
}

/** Shown next to the Product picker while one product is chosen: back to all products. */
export function ClearProductButton({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation();
  return (
    <Button variant="ghost" size="sm" onClick={onClick}>
      <X /> {t('owner.filter.clear')}
    </Button>
  );
}

/**
 * In-page Product, then App picker (D-86). "All products" first; the App list opens up
 * only once a product is chosen, showing that product's apps.
 */
export function OwnerScopeBar({ showApp = true, className }: { showApp?: boolean; className?: string }) {
  const { t } = useTranslation();
  const f = useOwnerFilter();
  const change = useChangeOwnerFilter();
  const { apps, products } = useOwnerProducts();
  const appOptions = useMemo(() => {
    const seen = new Map<string, { id: string; name: string }>();
    for (const a of apps) if (a.workspaceId === f.product) seen.set(a.productId, { id: a.productId, name: a.name });
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [apps, f.product]);
  return (
    <div className={cn('flex w-full flex-wrap items-center gap-2 sm:w-auto', className)}>
      <Select
        className="h-9 w-full text-[13px] sm:w-52"
        aria-label={t('owner.filter.product')}
        value={f.product}
        onChange={(e) => {
          const p = products.find((x) => x.id === e.target.value);
          change({ product: p?.id ?? '', productCode: p?.code ?? '', app: '' });
        }}
        options={[{ value: '', label: t('owner.filter.allProducts') }, ...products.map((p) => ({ value: p.id, label: p.name }))]}
      />
      {showApp ? (
        <Select
          className="h-9 w-full text-[13px] sm:w-48"
          aria-label={t('owner.filter.app')}
          title={f.product ? undefined : t('owner.filter.pickProductFirst')}
          disabled={!f.product}
          value={f.product ? f.app : ''}
          onChange={(e) => change({ ...f, app: e.target.value })}
          options={[{ value: '', label: t('owner.filter.allApps') }, ...appOptions.map((a) => ({ value: a.id, label: a.name }))]}
        />
      ) : null}
      {f.product ? <ClearProductButton onClick={() => change(EMPTY)} /> : null}
    </div>
  );
}
