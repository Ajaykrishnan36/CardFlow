import { useRef, useState, type FormEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, AtSign, Boxes, Check, CircleCheck, ShieldCheck } from 'lucide-react';
import { productsApi, workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ProductSummary, ProvisionBody, ProvisionResult } from '@crm/api/types';
import { Alert, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Checkbox, Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { DetailItem, DevLink, PageContainer, PageHeader } from '@crm/components/page';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { ProductIcon } from '@crm/features/products/product-icon';
import { CURRENCIES, InviteStatusBadge, LOCALES, slugCode, TIMEZONES, WORKSPACE_CODE_RE } from './workspace-ui';

const STEPS = ['details', 'products', 'admin', 'review'] as const;
type Step = (typeof STEPS)[number];

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function newKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
}

/** Which wizard step owns a server field error. */
function stepOfField(field: string): number {
  if (field === 'productIds' || field.startsWith('productIds')) return 1;
  if (field.startsWith('superAdmin')) return 2;
  return 0;
}

export function ProvisionWorkspacePage() {
  const { t } = useTranslation();
  useDocumentTitle(t('workspaces.provision.title'));
  // Remounting the wizard (via key) gives "Provision another" a fresh form and idempotency key.
  const [run, setRun] = useState(0);
  return (
    <PageContainer>
      <PageHeader
        crumbs={[{ label: t('workspaces.list.title'), to: '/crm/owner/workspaces' }, { label: t('workspaces.provision.title') }]}
        title={t('workspaces.provision.title')}
        description={t('workspaces.provision.description')}
      />
      <ProvisionWizard key={run} onAnother={() => setRun((r) => r + 1)} />
    </PageContainer>
  );
}

function ProvisionWizard({ onAnother }: { onAnother: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  // PRD OWN-02: one key per wizard, reused on every retry so a double submit or a
  // retried timeout can never create a second workspace.
  const idempotencyKey = useRef(newKey());

  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const [codeEdited, setCodeEdited] = useState(false);
  const [timezone, setTimezone] = useState('Asia/Kolkata');
  const [locale, setLocale] = useState('en');
  const [currency, setCurrency] = useState('INR');
  const [productIds, setProductIds] = useState<string[]>([]);
  const [adminName, setAdminName] = useState('');
  const [adminEmail, setAdminEmail] = useState('');
  const [inviteLater, setInviteLater] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<{ message: string; requestId?: string } | null>(null);
  const [result, setResult] = useState<ProvisionResult | null>(null);

  const products = useQuery({ queryKey: ['products', { q: '', status: 'active' }], queryFn: () => productsApi.list({ status: 'active' }) });
  const activeProducts = products.data?.data ?? [];
  const selectedProducts = activeProducts.filter((p) => productIds.includes(p.id));

  const clearError = (key: string) => setErrors((e) => (e[key] ? { ...e, [key]: '' } : e));

  const validate = (s: number): Record<string, string> => {
    const e: Record<string, string> = {};
    if (s === 0) {
      if (!name.trim()) e.name = t('workspaces.provision.nameRequired');
      if (!WORKSPACE_CODE_RE.test(code)) e.code = t('workspaces.provision.codeInvalid');
    }
    if (s === 1 && productIds.length === 0) e.productIds = t('workspaces.provision.productsRequired');
    if (s === 2 && !inviteLater) {
      if (!adminName.trim()) e['superAdmin.name'] = t('workspaces.provision.adminNameRequired');
      if (!EMAIL_RE.test(adminEmail.trim())) e['superAdmin.email'] = t('workspaces.provision.adminEmailInvalid');
    }
    return e;
  };

  const goTo = (s: number) => {
    setStep(s);
    setReached((r) => Math.max(r, s));
    setFormError(null);
  };

  const provision = useMutation({
    mutationFn: (body: ProvisionBody) => workspacesApi.provision(body, idempotencyKey.current),
    onSuccess: (res) => {
      qc.setQueryData(['workspace', res.workspace.id], res.workspace);
      void qc.invalidateQueries({ queryKey: ['workspaces'] });
      void qc.invalidateQueries({ queryKey: ['products'] });
      void qc.invalidateQueries({ queryKey: ['product'] });
      void qc.invalidateQueries({ queryKey: ['platform', 'dashboard'] });
      setResult(res);
    },
    onError: (e) => {
      if (isApiError(e) && e.status === 422 && Object.keys(e.fieldErrors).length > 0) {
        // Nothing was created, so a corrected resubmit gets a fresh key.
        idempotencyKey.current = newKey();
        setErrors(e.fieldErrors);
        const first = Math.min(...Object.keys(e.fieldErrors).map(stepOfField));
        setStep(first);
        setFormError({ message: e.message });
        return;
      }
      setFormError({ message: isApiError(e) ? e.message : t('common.genericError'), requestId: isApiError(e) && e.status >= 500 ? e.requestId : undefined });
    }
  });

  const onContinue = (ev: FormEvent) => {
    ev.preventDefault();
    const e = validate(step);
    if (Object.keys(e).length > 0) {
      setErrors((prev) => ({ ...prev, ...e }));
      return;
    }
    if (step < STEPS.length - 1) {
      goTo(step + 1);
      return;
    }
    // Final check across every step before submitting.
    for (let s = 0; s < STEPS.length - 1; s++) {
      const se = validate(s);
      if (Object.keys(se).length > 0) {
        setErrors((prev) => ({ ...prev, ...se }));
        setStep(s);
        return;
      }
    }
    setFormError(null);
    provision.mutate({
      name: name.trim(),
      code,
      timezone,
      locale,
      currency,
      productIds,
      superAdmin: inviteLater ? undefined : { name: adminName.trim(), email: adminEmail.trim() }
    });
  };

  if (result) return <SuccessCard result={result} onAnother={onAnother} />;

  const current: Step = STEPS[step];
  const adminLabel = inviteLater ? t('workspaces.provision.nobodyYet') : adminName.trim() || adminEmail.trim();

  return (
    <div className="mx-auto w-full max-w-3xl">
      <Stepper step={step} reached={reached} onSelect={(s) => goTo(s)} />

      <Card>
        <form onSubmit={onContinue} noValidate>
          <div className="border-b px-5 py-4 sm:px-6">
            <h2 className="text-[15px] font-semibold text-foreground">{t(`workspaces.provision.steps.${current}.title`)}</h2>
            <p className="mt-0.5 text-[13px] text-muted-foreground">{t(`workspaces.provision.steps.${current}.description`)}</p>
          </div>

          <div className="space-y-5 px-5 py-5 sm:px-6">
            {formError ? (
              <Alert tone="danger">
                {formError.message}
                {formError.requestId ? <span className="mt-1 block font-mono text-[11px]">{t('common.requestId', { id: formError.requestId })}</span> : null}
              </Alert>
            ) : null}

            {current === 'details' ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={t('workspaces.provision.name')} error={errors.name} className="sm:col-span-2">
                  <Input
                    value={name}
                    autoFocus
                    maxLength={120}
                    placeholder={t('workspaces.provision.namePlaceholder')}
                    onChange={(e) => {
                      setName(e.target.value);
                      if (!codeEdited) setCode(slugCode(e.target.value));
                      clearError('name');
                    }}
                  />
                </Field>
                <Field label={t('workspaces.provision.code')} error={errors.code} hint={t('workspaces.provision.codeHint')} className="sm:col-span-2">
                  <Input
                    value={code}
                    className="font-mono"
                    maxLength={40}
                    spellCheck={false}
                    autoCapitalize="none"
                    placeholder="acme-realty"
                    onChange={(e) => {
                      setCodeEdited(true);
                      setCode(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''));
                      clearError('code');
                    }}
                  />
                </Field>
                <Field label={t('workspaces.fields.timezone')} error={errors.timezone} className="sm:col-span-2">
                  <Select value={timezone} onChange={(e) => setTimezone(e.target.value)} options={TIMEZONES.map((z) => ({ value: z, label: z.replace(/_/g, ' ') }))} />
                </Field>
                <Field label={t('workspaces.fields.locale')} error={errors.locale}>
                  <Select value={locale} onChange={(e) => setLocale(e.target.value)} options={LOCALES.map((l) => ({ value: l, label: t(`workspaces.locales.${l}`, { defaultValue: l }) }))} />
                </Field>
                <Field label={t('workspaces.fields.currency')} error={errors.currency}>
                  <Select
                    value={currency}
                    onChange={(e) => setCurrency(e.target.value)}
                    options={CURRENCIES.map((c) => ({ value: c, label: `${c} — ${t(`workspaces.currencies.${c}`, { defaultValue: c })}` }))}
                  />
                </Field>
              </div>
            ) : null}

            {current === 'products' ? (
              <ProductPicker
                query={products}
                items={activeProducts}
                selected={productIds}
                error={errors.productIds}
                onToggle={(id, on) => {
                  setProductIds((ids) => (on ? [...ids, id] : ids.filter((x) => x !== id)));
                  clearError('productIds');
                }}
              />
            ) : null}

            {current === 'admin' ? (
              <div className="space-y-4">
                <div className={cn('grid gap-4 sm:grid-cols-2', inviteLater && 'pointer-events-none opacity-50')} aria-disabled={inviteLater || undefined}>
                  <Field label={t('workspaces.provision.adminName')} error={inviteLater ? undefined : errors['superAdmin.name']}>
                    <Input
                      value={adminName}
                      disabled={inviteLater}
                      autoComplete="off"
                      onChange={(e) => {
                        setAdminName(e.target.value);
                        clearError('superAdmin.name');
                      }}
                    />
                  </Field>
                  <Field label={t('workspaces.provision.adminEmail')} error={inviteLater ? undefined : errors['superAdmin.email']}>
                    <Input
                      type="email"
                      value={adminEmail}
                      disabled={inviteLater}
                      autoComplete="off"
                      autoCapitalize="none"
                      leading={<AtSign />}
                      placeholder="admin@customer.com"
                      onChange={(e) => {
                        setAdminEmail(e.target.value);
                        clearError('superAdmin.email');
                      }}
                    />
                  </Field>
                </div>
                <div className="rounded-lg border bg-muted/30 p-3">
                  <Checkbox
                    checked={inviteLater}
                    onCheckedChange={setInviteLater}
                    label={t('workspaces.provision.inviteLater')}
                    description={t('workspaces.provision.inviteLaterHint')}
                  />
                </div>
                {!inviteLater ? <p className="text-xs text-muted-foreground">{t('workspaces.provision.adminNote')}</p> : null}
              </div>
            ) : null}

            {current === 'review' ? (
              <div className="space-y-4">
                <ReviewSection title={t('workspaces.provision.steps.details.title')} onEdit={() => goTo(0)}>
                  <dl className="grid gap-4 sm:grid-cols-2">
                    <DetailItem label={t('workspaces.provision.name')}>{name}</DetailItem>
                    <DetailItem label={t('workspaces.provision.code')}>
                      <span className="font-mono text-[13px]">{code}</span>
                    </DetailItem>
                    <DetailItem label={t('workspaces.fields.timezone')}>{timezone.replace(/_/g, ' ')}</DetailItem>
                    <DetailItem label={t('workspaces.fields.locale')}>{t(`workspaces.locales.${locale}`, { defaultValue: locale })}</DetailItem>
                    <DetailItem label={t('workspaces.fields.currency')}>{currency}</DetailItem>
                  </dl>
                </ReviewSection>
                <ReviewSection title={t('workspaces.provision.steps.products.title')} onEdit={() => goTo(1)}>
                  <ul className="flex flex-wrap gap-1.5">
                    {selectedProducts.map((p) => (
                      <li key={p.id} className="rounded-md border bg-muted/50 px-2 py-0.5 text-xs font-medium">
                        {p.name} {p.currentVersion ? <span className="font-mono text-muted-foreground">v{p.currentVersion}</span> : null}
                      </li>
                    ))}
                  </ul>
                </ReviewSection>
                <ReviewSection title={t('workspaces.provision.steps.admin.title')} onEdit={() => goTo(2)}>
                  {inviteLater ? (
                    <p className="text-[13px] text-muted-foreground">{t('workspaces.provision.inviteLaterReview')}</p>
                  ) : (
                    <p className="text-[13px]">
                      <span className="font-medium">{adminName}</span> <span className="text-muted-foreground">· {adminEmail}</span>
                    </p>
                  )}
                </ReviewSection>
                <div className="flex gap-3 rounded-lg border border-primary/20 bg-primary-soft/60 p-4 text-[13px]">
                  <ShieldCheck className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
                  <div>
                    <p className="font-semibold text-foreground">{t('workspaces.provision.previewTitle')}</p>
                    <p className="mt-0.5 text-muted-foreground">
                      {inviteLater
                        ? t('workspaces.provision.previewLater', { workspace: name, products: selectedProducts.map((p) => p.name).join(', ') })
                        : t('workspaces.provision.preview', { name: adminLabel, workspace: name, products: selectedProducts.map((p) => p.name).join(', ') })}
                    </p>
                  </div>
                </div>
              </div>
            ) : null}
          </div>

          <div className="flex items-center justify-between gap-2 rounded-b-lg border-t bg-muted/30 px-5 py-3 sm:px-6">
            {step > 0 ? (
              <Button type="button" variant="outline" onClick={() => goTo(step - 1)} disabled={provision.isPending}>
                <ArrowLeft /> {t('common.back')}
              </Button>
            ) : (
              <Button asChild variant="ghost">
                <Link to="/crm/owner/workspaces">{t('common.cancel')}</Link>
              </Button>
            )}
            <Button type="submit" loading={provision.isPending} disabled={current === 'products' && products.isPending}>
              {current === 'review'
                ? provision.isPending
                  ? t('workspaces.provision.submitting')
                  : t('workspaces.provision.submit')
                : t('common.continue')}
              {current !== 'review' ? <ArrowRight /> : null}
            </Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

function Stepper({ step, reached, onSelect }: { step: number; reached: number; onSelect: (s: number) => void }) {
  const { t } = useTranslation();
  return (
    <nav aria-label={t('workspaces.provision.stepsLabel')} className="mb-4">
      <ol className="flex items-center">
        {STEPS.map((s, i) => {
          const done = i < step;
          const active = i === step;
          const clickable = i <= reached && !active;
          return (
            <li key={s} className={cn('flex items-center', i < STEPS.length - 1 && 'flex-1')}>
              <button
                type="button"
                disabled={!clickable}
                onClick={() => onSelect(i)}
                aria-current={active ? 'step' : undefined}
                className="flex items-center gap-2 rounded-md py-1 text-[13px] disabled:cursor-default"
              >
                <span
                  className={cn(
                    'grid size-7 shrink-0 place-items-center rounded-full border text-xs font-semibold tabular-nums transition-colors',
                    active ? 'border-primary bg-primary text-primary-foreground' : done ? 'border-primary/40 bg-primary-soft text-primary' : 'bg-card text-muted-foreground'
                  )}
                >
                  {done ? <Check className="size-3.5" strokeWidth={3} aria-hidden /> : i + 1}
                </span>
                <span className={cn('whitespace-nowrap font-medium', active ? 'text-foreground' : 'hidden text-muted-foreground sm:inline')}>
                  {t(`workspaces.provision.steps.${s}.short`)}
                </span>
              </button>
              {i < STEPS.length - 1 ? <span className={cn('mx-2 h-px flex-1', i < step ? 'bg-primary/40' : 'bg-border')} aria-hidden /> : null}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function ProductPicker({
  query,
  items,
  selected,
  error,
  onToggle
}: {
  query: { isPending: boolean; isError: boolean; error: unknown; refetch: () => unknown };
  items: ProductSummary[];
  selected: string[];
  error?: string;
  onToggle: (id: string, on: boolean) => void;
}) {
  const { t } = useTranslation();
  if (query.isError) {
    return (
      <ErrorState
        title={t('workspaces.provision.productsError')}
        message={isApiError(query.error) ? query.error.message : undefined}
        onRetry={() => void query.refetch()}
      />
    );
  }
  if (query.isPending) {
    return (
      <div className="grid gap-2.5 sm:grid-cols-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[66px] rounded-lg" />
        ))}
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <EmptyState
        icon={Boxes}
        className="py-8"
        title={t('workspaces.provision.noProductsTitle')}
        body={t('workspaces.provision.noProductsBody')}
        action={
          <Button asChild variant="outline">
            <Link to="/crm/owner/products">{t('workspaces.provision.goToProducts')}</Link>
          </Button>
        }
      />
    );
  }
  return (
    <div className="space-y-3">
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div className="grid gap-2.5 sm:grid-cols-2" role="group" aria-label={t('workspaces.provision.steps.products.title')}>
        {items.map((p) => {
          const on = selected.includes(p.id);
          return (
            <label
              key={p.id}
              className={cn(
                'flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors focus-within:ring-2 focus-within:ring-ring',
                on ? 'border-primary/50 bg-primary-soft/60' : 'hover:bg-muted/60'
              )}
            >
              <input type="checkbox" className="sr-only" checked={on} onChange={(e) => onToggle(p.id, e.target.checked)} />
              <ProductIcon icon={p.icon} size="sm" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] font-medium text-foreground">{p.name}</span>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {p.key}
                  {p.currentVersion ? ` · v${p.currentVersion}` : ''}
                </span>
              </span>
              <span
                className={cn(
                  'grid size-5 shrink-0 place-items-center rounded-full border transition-colors',
                  on ? 'border-primary bg-primary text-primary-foreground' : 'border-input bg-background'
                )}
                aria-hidden
              >
                {on ? <Check className="size-3" strokeWidth={3} /> : null}
              </span>
            </label>
          );
        })}
      </div>
      <p className="text-xs text-muted-foreground">{t('workspaces.provision.selectedCount', { count: selected.length })}</p>
    </div>
  );
}

function ReviewSection({ title, onEdit, children }: { title: string; onEdit: () => void; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
        <h3 className="text-[13px] font-semibold text-foreground">{title}</h3>
        <Button type="button" variant="link" size="sm" className="text-xs" onClick={onEdit}>
          {t('workspaces.provision.edit')}
        </Button>
      </div>
      <div className="px-3 py-3">{children}</div>
    </div>
  );
}

function SuccessCard({ result, onAnother }: { result: ProvisionResult; onAnother: () => void }) {
  const { t } = useTranslation();
  const { workspace, invitation } = result;
  return (
    <Card className="mx-auto w-full max-w-3xl animate-slide-up">
      <div className="flex flex-col items-center px-6 pb-6 pt-10 text-center">
        <span className="grid size-12 place-items-center rounded-full bg-success-soft text-success">
          <CircleCheck className="size-6" aria-hidden />
        </span>
        <h2 className="mt-4 text-lg font-semibold text-foreground">{t('workspaces.provision.successTitle', { name: workspace.name })}</h2>
        <p className="mt-1 font-mono text-xs text-muted-foreground">{workspace.code}</p>
      </div>
      <div className="space-y-4 border-t px-5 py-5 sm:px-6">
        {invitation ? (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2.5 text-[13px]">
            <span className="min-w-0">
              {t('workspaces.provision.invitedAs', { email: invitation.email ?? invitation.displayName, role: invitation.roleName })}
            </span>
            <InviteStatusBadge status={invitation.status} />
          </div>
        ) : (
          <Alert tone="info">{t('workspaces.provision.noInvitation')}</Alert>
        )}
        {invitation?.status === 'delivery_failed' ? <Alert tone="warning">{t('workspaces.provision.deliveryFailed')}</Alert> : null}
        {invitation?.devAcceptUrl ? <DevLink url={invitation.devAcceptUrl} label={t('workspaces.devInviteLink')} /> : null}
      </div>
      <div className="flex flex-col-reverse gap-2 rounded-b-lg border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end sm:px-6">
        <Button variant="outline" onClick={onAnother}>
          {t('workspaces.provision.another')}
        </Button>
        <Button asChild>
          <Link to={`/crm/owner/workspaces/${workspace.id}`}>
            {t('workspaces.provision.open')} <ArrowRight />
          </Link>
        </Button>
      </div>
    </Card>
  );
}
