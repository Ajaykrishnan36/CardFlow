import { useMemo, useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { platformApi } from '@crm/api/endpoints';
import { Select } from '@crm/components/ui/form-controls';

// Owner console filter (D-74): a product, then one of its apps. "All" by default.
// The dashboard, Products, Apps and the record lists follow it. Kept in this browser.

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
  return { product: f.product || undefined, app: f.app || undefined };
}

/** Sidebar: Product, then App (its apps only). */
export function OwnerFilterPicker({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const f = useOwnerFilter();
  const apps = useQuery({ queryKey: ['platform', 'apps', 'all'], queryFn: () => platformApi.apps(), staleTime: 60_000 });
  const products = useMemo(() => {
    const seen = new Map<string, { id: string; code: string; name: string }>();
    for (const a of apps.data ?? []) seen.set(a.workspaceId, { id: a.workspaceId, code: a.workspaceCode, name: a.workspaceName });
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [apps.data]);
  const appOptions = useMemo(() => {
    const seen = new Map<string, { id: string; name: string }>();
    for (const a of apps.data ?? []) if (!f.product || a.workspaceId === f.product) seen.set(a.productId, { id: a.productId, name: a.name });
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [apps.data, f.product]);
  if (collapsed) return null;
  const change = (next: OwnerFilter) => {
    setOwnerFilter(next);
    void qc.invalidateQueries({ queryKey: ['platform'] });
    void qc.invalidateQueries({ queryKey: ['workspaces'] });
  };
  return (
    <div className="space-y-2 px-3 pt-3">
      <label className="block">
        <span className="mb-1 block text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{t('owner.filter.product')}</span>
        <Select
          className="h-9 text-[13px]"
          value={f.product}
          onChange={(e) => {
            const p = products.find((x) => x.id === e.target.value);
            const keepApp = f.app && (apps.data ?? []).some((a) => a.productId === f.app && (!p || a.workspaceId === p.id));
            change({ product: p?.id ?? '', productCode: p?.code ?? '', app: keepApp ? f.app : '' });
          }}
          options={[{ value: '', label: t('owner.filter.allProducts') }, ...products.map((p) => ({ value: p.id, label: p.name }))]}
        />
      </label>
      <label className="block">
        <span className="mb-1 block text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{t('owner.filter.app')}</span>
        <Select
          className="h-9 text-[13px]"
          value={f.app}
          onChange={(e) => change({ ...f, app: e.target.value })}
          options={[{ value: '', label: t('owner.filter.allApps') }, ...appOptions.map((a) => ({ value: a.id, label: a.name }))]}
        />
      </label>
    </div>
  );
}
