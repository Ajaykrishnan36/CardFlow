import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Code2, KeyRound, ShieldCheck } from 'lucide-react';
import { recordsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ObjectMeta, ProductDetail } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { objectIcon } from '@crm/features/records/use-object-meta';

/**
 * Product → API: the REST API a workspace on this product gets, built live from the
 * modules switched on in the draft and each object's fields (D-46). Field access set on
 * roles and permission sets decides which of these fields each user's API calls return.
 */
export function ProductApiTab({ product, modules }: { product: ProductDetail; modules: string[] }) {
  const { t } = useTranslation();
  const objects = useMemo(() => {
    const out: string[] = [];
    for (const m of product.moduleCatalog) {
      if (!modules.includes(m.key) || !m.available || m.hidden) continue;
      for (const o of m.objects ?? []) if (!out.includes(o)) out.push(o);
    }
    return out;
  }, [product.moduleCatalog, modules]);
  const [picked, setPicked] = useState<string | null>(null);
  const current = picked && objects.includes(picked) ? picked : objects[0];
  const apiOn = product.draftConfig.integrations.apiAccess;

  if (objects.length === 0) {
    return (
      <Card>
        <EmptyState icon={Code2} title={t('products.api.emptyTitle')} body={t('products.api.emptyBody')} />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <Alert tone={apiOn ? 'info' : 'warning'}>
        <span className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
          <span>{apiOn ? t('products.api.onBody') : t('products.api.offBody')}</span>
          <Link to={`/crm/owner/products/${product.id}?tab=setup&step=login`} className="shrink-0 font-medium text-primary hover:underline">
            {t('products.api.changeSetting')}
          </Link>
        </span>
      </Alert>
      <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
        <Card className="h-fit overflow-hidden">
          <CardHeader title={t('products.api.objects', { count: objects.length })} />
          <ul className="border-t py-1">
            {objects.map((o) => (
              <ObjectItem key={o} object={o} active={o === current} onClick={() => setPicked(o)} />
            ))}
          </ul>
        </Card>
        {current ? <ObjectApi key={current} object={current} /> : null}
      </div>
    </div>
  );
}

function ObjectItem({ object, active, onClick }: { object: string; active: boolean; onClick: () => void }) {
  const meta = useQuery({ queryKey: ['records', '/platform', object, 'meta'], queryFn: () => recordsApi.meta(object), staleTime: 5 * 60_000 });
  const Icon = objectIcon(object, meta.data?.icon);
  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        aria-current={active || undefined}
        className={cn('flex w-full items-center gap-2.5 px-4 py-2 text-left text-[13px] transition-colors', active ? 'bg-primary-soft font-medium text-primary' : 'hover:bg-muted')}
      >
        <Icon className="size-4 shrink-0" aria-hidden />
        <span className="min-w-0 flex-1 truncate">{meta.data?.labelPlural ?? object}</span>
        <span className="font-mono text-[11px] text-muted-foreground">{object}</span>
      </button>
    </li>
  );
}

function ObjectApi({ object }: { object: string }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ['records', '/platform', object, 'meta'], queryFn: () => recordsApi.meta(object), staleTime: 5 * 60_000 });
  if (q.isError) {
    return <ErrorState title={t('products.api.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />;
  }
  if (!q.data) return <Skeleton className="h-96 w-full" />;
  const m = q.data;
  const base = `/api/crm/v1/w/{workspace}/crm/${object}`;
  const endpoints: Array<[string, string, string]> = [
    ['GET', base, t('objects.api.list')],
    ['POST', base, t('objects.api.create')],
    ['GET', `${base}/{id}`, t('objects.api.get')],
    ['PATCH', `${base}/{id}`, t('objects.api.update')],
    ['DELETE', `${base}/{id}`, t('objects.api.delete')],
    ['GET', `/api/crm/v1/w/{workspace}/crm/meta/${object}`, t('objects.api.meta')]
  ];
  const writable = m.fields.filter((f) => !f.readOnly);
  const sample = Object.fromEntries(writable.slice(0, 4).map((f) => [f.key, sampleValue(f.type, f.options?.[0]?.value)]));
  return (
    <div className="min-w-0 space-y-4">
      <Card>
        <CardHeader
          title={
            <span className="flex items-center gap-2">
              <Code2 className="size-4 text-muted-foreground" aria-hidden /> {t('products.api.endpoints', { name: m.labelPlural })}
            </span>
          }
          description={t('objects.api.body')}
        />
        <ul className="space-y-2 px-4 pb-4">
          {endpoints.map(([method, path, what]) => (
            <li key={method + path} className="flex flex-col gap-0.5 text-xs sm:flex-row sm:items-center sm:gap-3">
              <span className="flex min-w-0 items-center gap-2">
                <MethodBadge method={method} />
                <span className="min-w-0 break-all font-mono text-foreground">{path}</span>
              </span>
              <span className="text-muted-foreground sm:ml-auto sm:text-right">{what}</span>
            </li>
          ))}
        </ul>
        <div className="border-t px-4 py-3">
          <p className="mb-1.5 text-xs font-medium text-muted-foreground">{t('products.api.example')}</p>
          <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-[11px] leading-5">
            {`POST ${base}\n${JSON.stringify({ values: sample }, null, 2)}`}
          </pre>
        </div>
      </Card>
      <Card className="overflow-hidden">
        <CardHeader
          title={t('products.api.fields', { count: m.fields.length })}
          description={
            <span className="flex items-start gap-1.5">
              <ShieldCheck className="mt-0.5 size-3.5 shrink-0" aria-hidden /> {t('products.api.fieldAccessNote')}
            </span>
          }
        />
        <div className="overflow-x-auto border-t">
          <table className="w-full text-left text-xs">
            <thead className="bg-muted/40 text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">{t('products.api.colField')}</th>
                <th className="px-4 py-2 font-medium">{t('products.api.colApiName')}</th>
                <th className="px-4 py-2 font-medium">{t('products.api.colType')}</th>
                <th className="px-4 py-2 font-medium">{t('products.api.colRules')}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {m.fields.map((f) => (
                <tr key={f.key}>
                  <td className="px-4 py-2 font-medium text-foreground">{f.label}</td>
                  <td className="px-4 py-2 font-mono">{f.key}</td>
                  <td className="px-4 py-2">
                    {t(`objects.types.${f.type}`, { defaultValue: f.type })}
                    {f.type === 'lookup' && f.lookup ? <span className="text-muted-foreground"> → {f.lookup}</span> : null}
                  </td>
                  <td className="px-4 py-2">
                    <span className="flex flex-wrap gap-1">
                      {f.required ? <Badge tone="danger">{t('products.api.required')}</Badge> : null}
                      {f.readOnly ? <Badge tone="neutral">{t('products.api.readOnly')}</Badge> : null}
                      {!f.standard ? <Badge tone="primary">{t('products.api.custom')}</Badge> : null}
                      {f.options?.length ? <span className="text-muted-foreground">{f.options.map((o) => o.value).join(' · ')}</span> : null}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="flex items-start gap-1.5 border-t px-4 py-3 text-xs text-muted-foreground">
          <KeyRound className="mt-0.5 size-3.5 shrink-0" aria-hidden /> {t('products.api.workspaceFields')}
        </p>
      </Card>
    </div>
  );
}

function MethodBadge({ method }: { method: string }) {
  const tone = { GET: 'bg-success-soft text-success', POST: 'bg-primary-soft text-primary', PATCH: 'bg-warning-soft text-warning', DELETE: 'bg-danger-soft text-danger' }[method];
  return <span className={cn('w-14 shrink-0 rounded px-1 py-0.5 text-center font-mono text-[10px] font-semibold', tone)}>{method}</span>;
}

function sampleValue(type: ObjectMeta['fields'][number]['type'], option?: string): unknown {
  switch (type) {
    case 'number':
    case 'currency':
    case 'percent':
      return 100;
    case 'boolean':
      return true;
    case 'date':
      return '2026-12-31';
    case 'datetime':
      return '2026-12-31T10:00:00Z';
    case 'select':
      return option ?? '…';
    case 'multiselect':
      return option ? [option] : [];
    case 'lookup':
      return '<record id>';
    case 'email':
      return 'name@example.com';
    default:
      return '…';
  }
}
