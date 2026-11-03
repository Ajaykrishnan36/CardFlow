import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { formatMoney } from '@crm/lib/money';
import { commerceApi } from '@crm/api/endpoints';
import { codeOf } from './record-commerce';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Building2, CircleCheck, Contact, Handshake, KeyRound, Info, Plus, X } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { ConvertBody, ConvertResult, GiveLoginBody, GiveLoginResult, LookupValue, RecordRow } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Alert } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { DevLink } from '@crm/components/page';
import { cn } from '@crm/lib/utils';
import { GiveLoginSection } from '@crm/features/access/give-login-dialog';
import { LookupCombobox } from './field-input';
import { recordKeys } from './use-object-meta';
import { scopedLookupHref, useRecordScope } from './record-scope';
import { isForbidden } from './record-states';

const s = (v: unknown) => (typeof v === 'string' ? v.trim() : '');

function uuid(): string {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/** The convert response may also say the person already had a login (same meaning as GiveLoginResult). */
type ConvertOutcome = ConvertResult & Partial<Pick<GiveLoginResult, 'existingLogin'>>;

const ACCESS_PREFIX = 'access.';

interface Draft {
  mode: 'new' | 'existing' | 'none';
  accountName: string;
  kind: 'business' | 'individual';
  existing: LookupValue | null;
  createContact: boolean;
  /** Owner only: give the converted person a login (null = no login). */
  access: GiveLoginBody | null;
  createOpportunity: boolean;
  oppName: string;
  oppAmount: string;
  oppCloseDate: string;
  /** What they want to buy: product + quantity, priced from the price book (D-170). */
  items: Array<{ key: string; item: LookupValue | null; quantity: string }>;
}

function draftFor(lead: RecordRow, createOpportunity: boolean): Draft {
  const org = s(lead.values.organization);
  const fullName = [s(lead.values.firstName), s(lead.values.lastName)].filter(Boolean).join(' ') || lead.title;
  const close = new Date(Date.now() + 30 * 86400_000);
  return {
    // Contact first: a person without a company becomes just a contact.
    mode: org ? 'new' : 'none',
    accountName: org || fullName,
    kind: 'business',
    existing: null,
    createContact: true,
    access: null,
    createOpportunity,
    oppName: `${org || fullName} — new deal`,
    oppAmount: '',
    oppCloseDate: `${close.getFullYear()}-${String(close.getMonth() + 1).padStart(2, '0')}-${String(close.getDate()).padStart(2, '0')}`,
    items: [{ key: uuid(), item: null, quantity: '1' }]
  };
}

export function ConvertLeadDialog({ lead, open, onOpenChange }: { lead: RecordRow; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const scope = useRecordScope();
  // Tenants don't create tenants: logins are only given from the owner console.
  const canGiveLogin = scope.audience === 'owner';
  const canOpportunity = scope.can('opportunities', 'create');
  const [d, setD] = useState<Draft>(() => draftFor(lead, canOpportunity));
  const notQualified = scope.setup.requireQualified && lead.values.status !== 'qualified';
  const [idemKey, setIdemKey] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [accessErrors, setAccessErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [result, setResult] = useState<ConvertOutcome | null>(null);
  const [sentAccess, setSentAccess] = useState<GiveLoginBody | null>(null);

  const firstName = s(lead.values.firstName) || lead.title.split(' ')[0] || lead.title;
  const fullName = [s(lead.values.firstName), s(lead.values.lastName)].filter(Boolean).join(' ') || lead.title;
  const email = s(lead.values.email);

  // Fresh form + idempotency key each time the dialog opens: retries of one attempt reuse the key.
  useEffect(() => {
    if (!open) return;
    setD(draftFor(lead, canOpportunity));
    setIdemKey(uuid());
    setErrors({});
    setAccessErrors({});
    setFormError(null);
    setResult(null);
    setSentAccess(null);
    // Only on open — not when the lead refetches underneath.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const patch = (p: Partial<Draft>) => setD((prev) => ({ ...prev, ...p }));
  const picked = d.items.filter((x) => x.item);
  const code = codeOf(scope.prefix);
  const priceKey = picked.map((x) => `${x.item!.id}:${Number(x.quantity) || 1}`).join(',');
  // Prices come from the price book, never typed in: preview what the opportunity will hold.
  const price = useQuery({
    queryKey: ['convert-price', code, priceKey],
    queryFn: () => commerceApi(code).preview({ lines: picked.map((x) => ({ itemId: x.item!.id, quantity: Number(x.quantity) || 1 })) }),
    enabled: Boolean(code && open && picked.length && d.createOpportunity),
    staleTime: 30_000
  });
  const setItem = (key: string, p: Partial<Draft['items'][number]>) =>
    setD((prev) => ({ ...prev, items: prev.items.map((x) => (x.key === key ? { ...x, ...p } : x)) }));

  const convert = useMutation({
    mutationFn: (body: ConvertBody) => scope.api.convertLead(lead.id, body, idemKey),
    onSuccess: (res, body) => {
      setSentAccess(body.access ?? null);
      setResult(res);
      void qc.invalidateQueries({ queryKey: recordKeys.prefix(scope.prefix) });
      void qc.invalidateQueries({ queryKey: scope.dashboardKey });
      if (res.workspaceId && canGiveLogin) void qc.invalidateQueries({ queryKey: ['workspaces'] });
      if (res.identityId && canGiveLogin) void qc.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (e) => {
      if (!isApiError(e)) return setFormError(t('common.genericError'));
      if (e.code === 'already_converted') return setFormError(t('records.convert.alreadyConverted'));
      if (isForbidden(e)) return setFormError(t('records.convert.forbidden'));
      const own: Record<string, string> = {};
      const access: Record<string, string> = {};
      for (const [k, m] of Object.entries(e.fieldErrors)) {
        if (k.startsWith(ACCESS_PREFIX)) access[k.slice(ACCESS_PREFIX.length)] = m;
        else if (k === 'access') access._ = m;
        else own[k] = m;
      }
      setErrors(own);
      setAccessErrors(access);
      setFormError(Object.keys(e.fieldErrors).length ? (access._ ?? t('records.convert.fixErrors')) : e.message);
    }
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next: Record<string, string> = {};
    if (d.mode === 'new' && !d.accountName.trim()) next.accountName = t('common.required');
    if (d.mode === 'existing' && !d.existing) next.accountId = t('records.convert.chooseAccount');
    setErrors(next);
    setAccessErrors({});
    if (Object.keys(next).length) {
      setFormError(null);
      return;
    }
    setFormError(null);
    if (d.mode === 'none' && !d.createContact) {
      setFormError(t('records.convert.noneNeedsContact'));
      return;
    }
    if (d.createOpportunity && canOpportunity && !picked.length) {
      setErrors({ items: 'Pick the product they want to buy.' });
      return;
    }
    const body: ConvertBody = {
      account: d.mode === 'new' ? { mode: 'new', name: d.accountName.trim(), kind: d.kind } : d.mode === 'existing' ? { mode: 'existing', accountId: d.existing!.id } : { mode: 'none' },
      createContact: d.createContact,
      opportunity: canOpportunity
        ? {
            create: d.createOpportunity,
            closeDate: d.oppCloseDate || undefined,
            items: picked.map((x) => ({ itemId: x.item!.id, quantity: Number(x.quantity) || 1 }))
          }
        : undefined,
      access: canGiveLogin && email && d.access ? d.access : undefined
    };
    convert.mutate(body);
  };

  const err = (...keys: string[]) => keys.map((k) => errors[k]).find(Boolean);
  const accountLabel = d.mode === 'new' ? d.accountName : d.mode === 'existing' ? d.existing?.label ?? '' : '';
  const accountHref = result?.accountId ? scopedLookupHref(scope, 'accounts', result.accountId) : null;
  const opportunityHref = result?.opportunityId ? scopedLookupHref(scope, 'opportunities', result.opportunityId) : null;
  const contactHref = result?.contactId ? scopedLookupHref(scope, 'contacts', result.contactId) : null;
  const newWorkspaceName = sentAccess?.workspace.mode === 'new' ? sentAccess.workspace.name : null;
  const showWorkspace = Boolean(result?.workspaceId && sentAccess && sentAccess.workspace.mode !== 'platform');

  return (
    <Dialog open={open} onOpenChange={(o) => !convert.isPending && onOpenChange(o)}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-2xl flex-col p-0 sm:top-[6vh] sm:max-h-[88vh]">
        <div className="border-b px-5 py-4 pr-12">
          <DialogTitle className="text-base font-semibold">{result ? t('records.convert.doneTitle') : t('records.convert.title')}</DialogTitle>
          <DialogDescription className="text-[13px] text-muted-foreground">
            {result ? t('records.convert.doneBody', { name: fullName }) : t('records.convert.subtitle', { name: lead.title, code: lead.code })}
          </DialogDescription>
        </div>

        {result ? (
          <>
            <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-5">
              <ul className="divide-y rounded-lg border">
                {result.accountId ? <ResultRow icon={Building2} label={t('records.convert.account')} name={accountLabel} to={accountHref} /> : null}
                {result.contactId ? <ResultRow icon={Contact} label={t('records.convert.contact')} name={fullName} to={contactHref} /> : null}
                {result.opportunityId ? <ResultRow icon={Handshake} label={t('records.convert.opportunity')} name={picked.length ? `${fullName} — ${picked[0].item!.label.split(' · ')[0]}${picked.length > 1 ? ` + ${picked.length - 1} more` : ''}` : d.oppName} to={opportunityHref} /> : null}
                {showWorkspace && result.workspaceId ? (
                  <ResultRow
                    icon={Building2}
                    label={t('records.convert.workspace')}
                    name={newWorkspaceName ?? t('records.convert.openWorkspace')}
                    to={`/crm/owner/workspaces/${encodeURIComponent(result.workspaceId)}`}
                  />
                ) : null}
                {result.identityId && canGiveLogin ? (
                  <ResultRow icon={KeyRound} label={t('records.convert.login')} name={email || fullName} to={`/crm/owner/users/${encodeURIComponent(result.identityId)}`} />
                ) : null}
              </ul>
              {sentAccess && result.existingLogin ? (
                <Alert tone="info" title={t('records.convert.existingLoginTitle')}>
                  {t('records.convert.existingLoginBody', { name: firstName, email })}
                </Alert>
              ) : result.invitation ? (
                <>
                  <Alert tone="info" title={t('records.convert.invitedTitle')}>
                    {t('records.convert.invitedBody', { email: result.invitation.email ?? email, name: firstName })}
                  </Alert>
                  {result.invitation.devAcceptUrl ? <DevLink url={result.invitation.devAcceptUrl} label={t('records.convert.devLink')} /> : null}
                </>
              ) : sentAccess?.method === 'password' && result.identityId ? (
                <Alert tone="success" title={t('records.convert.passwordTitle')}>
                  {t('records.convert.passwordBody', { name: firstName, email })}
                </Alert>
              ) : null}
            </div>
            <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t('records.convert.stayOnLead')}
              </Button>
              {accountHref || contactHref ? (
                <Button
                  onClick={() => {
                    onOpenChange(false);
                    navigate((accountHref || contactHref)!);
                  }}
                >
                  {accountHref ? t('records.convert.goToAccount') : t('records.convert.goToContact')} <ArrowRight />
                </Button>
              ) : null}
            </div>
          </>
        ) : (
          <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-5">
              {formError ? <Alert tone="danger">{formError}</Alert> : null}
              {notQualified ? <Alert tone="warning" title={t('records.convert.needsQualifiedTitle')}>{t('records.convert.needsQualified')}</Alert> : null}

              <Section icon={Contact} title="Who is buying?">
                <div className="grid gap-3 sm:grid-cols-3">
                  <RadioCard
                    checked={d.mode === 'none'}
                    onSelect={() => patch({ mode: 'none', createContact: true })}
                    name="account-mode"
                    label="A person"
                  >
                    <p className="mt-1.5 text-xs text-muted-foreground">{fullName} becomes a contact. Most people buying for themselves.</p>
                  </RadioCard>
                  <RadioCard checked={d.mode === 'new'} onSelect={() => patch({ mode: 'new', kind: 'business', createContact: true })} name="account-mode" label="A business">
                    {d.mode === 'new' ? (
                      <div className="mt-2" onClick={(e) => e.stopPropagation()}>
                        <Field label="Business name" error={err('accountName', 'account.name', 'name')}>
                          <Input value={d.accountName} onChange={(e) => patch({ accountName: e.target.value })} />
                        </Field>
                        <p className="mt-1.5 text-xs text-muted-foreground">A fleet or company: an account, with {firstName} as its contact.</p>
                      </div>
                    ) : (
                      <p className="mt-1.5 text-xs text-muted-foreground">A fleet or company buying.</p>
                    )}
                  </RadioCard>
                  <RadioCard checked={d.mode === 'existing'} onSelect={() => patch({ mode: 'existing', createContact: true })} name="account-mode" label="A business we know">
                    {d.mode === 'existing' ? (
                      <div className="mt-2" onClick={(e) => e.stopPropagation()}>
                        <Field label={t('records.convert.accountSearch')} error={err('accountId', 'account.accountId', 'account')}>
                          <LookupCombobox target="accounts" value={d.existing?.id ?? null} label={d.existing?.label} onChange={(v) => patch({ existing: v })} />
                        </Field>
                      </div>
                    ) : (
                      <p className="mt-1.5 text-xs text-muted-foreground">Add {firstName} to an account you already have.</p>
                    )}
                  </RadioCard>
                </div>
              </Section>

              {canOpportunity ? (
                <Section icon={Handshake} title="What do they want to buy?">
                  <Checkbox
                    checked={d.createOpportunity}
                    onCheckedChange={(c) => patch({ createOpportunity: c })}
                    label="Start an opportunity with these products"
                    description="Prices come from your price book. You can add more products to the opportunity at each follow-up, then make the quote from it."
                  />
                  {d.createOpportunity ? (
                    <div className="mt-3 space-y-2">
                      {d.items.map((x, i) => {
                        const line = price.data?.lines.find((l) => l.itemId === x.item?.id);
                        return (
                          <div key={x.key} className="grid grid-cols-[1fr_88px_auto] items-end gap-2">
                            <Field label={i === 0 ? 'Product' : undefined} error={i === 0 ? err('items', 'opportunity.items.lines', 'opportunity.items') : undefined}>
                              <LookupCombobox target="catalog_items" value={x.item?.id ?? null} label={x.item?.label} onChange={(v) => setItem(x.key, { item: v })} placeholder="Pick a product" />
                            </Field>
                            <Field label={i === 0 ? 'Qty' : undefined}>
                              <Input type="number" inputMode="numeric" min={1} value={x.quantity} onChange={(e) => setItem(x.key, { quantity: e.target.value })} />
                            </Field>
                            <div className="flex h-9 items-center gap-2">
                              <span className="w-28 text-right text-[13px] tabular-nums text-foreground">{line ? formatMoney(line.total, price.data?.currency) : '—'}</span>
                              {d.items.length > 1 ? (
                                <Button type="button" variant="ghost" size="icon-sm" aria-label="Remove product" onClick={() => patch({ items: d.items.filter((y) => y.key !== x.key) })}>
                                  <X />
                                </Button>
                              ) : null}
                            </div>
                          </div>
                        );
                      })}
                      <Button type="button" variant="outline" size="sm" onClick={() => patch({ items: [...d.items, { key: uuid(), item: null, quantity: '1' }] })}>
                        <Plus /> Add another product
                      </Button>
                      {price.data && picked.length ? (
                        <dl className="mt-2 space-y-1 rounded-lg border bg-muted/30 px-3 py-2 text-[13px]">
                          {price.data.priceBook ? (
                            <div className="flex justify-between text-muted-foreground">
                              <dt>Price book</dt>
                              <dd>{price.data.priceBook}</dd>
                            </div>
                          ) : null}
                          <div className="flex justify-between text-muted-foreground">
                            <dt>Before tax</dt>
                            <dd className="tabular-nums">{formatMoney(price.data.subtotal - price.data.discount, price.data.currency)}</dd>
                          </div>
                          <div className="flex justify-between text-muted-foreground">
                            <dt>GST</dt>
                            <dd className="tabular-nums">{formatMoney(price.data.tax, price.data.currency)}</dd>
                          </div>
                          <div className="flex justify-between font-semibold text-foreground">
                            <dt>Opportunity amount</dt>
                            <dd className="tabular-nums">{formatMoney(price.data.total, price.data.currency)}</dd>
                          </div>
                        </dl>
                      ) : null}
                      <Field label={t('records.convert.oppClose')} error={err('opportunity.closeDate')} className="max-w-[220px]">
                        <Input type="date" value={d.oppCloseDate} onChange={(e) => patch({ oppCloseDate: e.target.value })} />
                      </Field>
                      <p className="text-xs text-muted-foreground">No quote or invoice is made now. The quote comes from the opportunity; the invoice only once it is paid.</p>
                    </div>
                  ) : null}
                </Section>
              ) : null}

              {canGiveLogin ? (
                <Section icon={KeyRound} title={t('records.convert.access')}>
                  {email ? (
                    <GiveLoginSection
                      value={d.access}
                      onChange={(v) => {
                        patch({ access: v });
                        if (Object.keys(accessErrors).length) setAccessErrors({});
                      }}
                      personName={fullName}
                      email={email}
                      defaultWorkspaceName={accountLabel || fullName}
                      errors={accessErrors}
                      disabled={convert.isPending}
                    />
                  ) : (
                    <p className="flex items-start gap-2 text-[13px] text-muted-foreground">
                      <Info className="mt-0.5 size-4 shrink-0" aria-hidden />
                      {t('records.convert.noEmail')}
                    </p>
                  )}
                </Section>
              ) : null}
              <div className="h-16" aria-hidden />
            </div>
            <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={convert.isPending}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" loading={convert.isPending} disabled={notQualified}>
                {t('records.convert.submit')}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Section({ icon: Icon, title, children }: { icon: typeof Building2; title: string; children: ReactNode }) {
  return (
    <section className="rounded-lg border bg-card">
      <h3 className="flex items-center gap-2 border-b bg-muted/40 px-4 py-2 text-[13px] font-semibold text-foreground">
        <Icon className="size-4 text-muted-foreground" aria-hidden />
        {title}
      </h3>
      <div className="p-4">{children}</div>
    </section>
  );
}

function RadioCard({ checked, onSelect, name, label, children }: { checked: boolean; onSelect: () => void; name: string; label: string; children?: ReactNode }) {
  return (
    <div
      className={cn('rounded-md border p-3 transition-colors', checked ? 'border-primary bg-primary-soft/40 ring-1 ring-primary/20' : 'hover:bg-muted/50')}
      onClick={onSelect}
    >
      <label className="flex cursor-pointer items-center gap-2 text-[13px] font-medium text-foreground">
        <input type="radio" name={name} checked={checked} onChange={onSelect} className="size-4 accent-[hsl(var(--primary))]" />
        {label}
      </label>
      {children}
    </div>
  );
}

function ResultRow({ icon: Icon, label, name, to }: { icon: typeof Building2; label: string; name: string; to: string | null }) {
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <CircleCheck className="size-5 shrink-0 text-success" aria-hidden />
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
        <Icon className="size-4" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-xs text-muted-foreground">{label}</p>
        {to ? (
          <Link to={to} className="block truncate text-[13px] font-medium text-primary hover:underline">
            {name || label}
          </Link>
        ) : (
          <p className="truncate text-[13px] font-medium text-foreground">{name || label}</p>
        )}
      </div>
    </li>
  );
}
