import { useState, type ReactNode } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { BadgeCheck, Check, ChevronDown, ChevronRight, Coins, Megaphone, Plus, SlidersHorizontal, Trash2, X } from 'lucide-react';
import { commerceApi, type ApprovalRequest, type ApprovalRule, type CampaignAttribution, type PricingRule } from '@crm/api/endpoints';
import type { LookupValue } from '@crm/api/types';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SegmentedFilter, Tabs } from '@crm/components/page';
import { EmptyState } from '@crm/components/states';
import { formatMoney } from '@crm/lib/money';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { RecordPicker, firstError } from '@crm/features/records/record-commerce';
import { humanize } from '@crm/features/records/use-object-meta';

// Pages of the commercial model (D-118…D-125): the approvals inbox, pricing setup
// (rules, approval limits, currencies and exchange rates) and campaign results.

const icon = (node: ReactNode) => <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary [&>svg]:size-5">{node}</span>;

function Modal({ title, description, onClose, children }: { title: string; description?: string; onClose: () => void; children: ReactNode }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
          {description ? <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{description}</DialogDescription> : null}
        </div>
        <div className="max-h-[70vh] space-y-3 overflow-y-auto px-5 py-4">{children}</div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- approvals inbox

const kindUnit = (kind: string, value: number, currency?: string) => (kind === 'discount' ? `${value}%` : formatMoney(value, currency));
const statusTone = { pending: 'warning', approved: 'success', rejected: 'danger', cancelled: 'neutral' } as const;

export function ApprovalsPage() {
  useDocumentTitle('Approvals');
  const { code, context } = useWorkspace();
  const api = commerceApi(code);
  const qc = useQueryClient();
  const [status, setStatus] = useState<'pending' | 'all'>('pending');
  const listQ = useQuery({ queryKey: ['approvals', code, status], queryFn: () => api.approvals(status) });
  const [open, setOpen] = useState<ApprovalRequest | null>(null);
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | null>(null);
  const decide = useMutation({
    mutationFn: (v: 'approved' | 'rejected') => api.decide(open!.id, v, note.trim() || undefined),
    onSuccess: (_d, v) => {
      toast.success(v === 'approved' ? 'Approved' : 'Rejected');
      setOpen(null);
      setNote('');
      void qc.invalidateQueries({ queryKey: ['approvals', code] });
    },
    onError: (e) => setError(firstError(e))
  });
  const rows = listQ.data?.data ?? [];
  const kinds = listQ.data?.kinds ?? {};
  const base = workspaceBase(code);
  return (
    <PageContainer>
      <PageHeader title="Approvals" icon={icon(<BadgeCheck />)} description="Discounts, refunds, credit notes, debit notes and write-offs above your business's limits wait here for a decision." />
      <div className="mb-4">
        <SegmentedFilter value={status} onChange={setStatus} options={[{ value: 'pending', label: 'Waiting' }, { value: 'all', label: 'All' }]} />
      </div>
      <Card className="overflow-hidden">
        {listQ.isLoading ? (
          <div className="space-y-2 p-4"><Skeleton className="h-10" /><Skeleton className="h-10" /></div>
        ) : rows.length === 0 ? (
          <EmptyState icon={BadgeCheck} title={status === 'pending' ? 'Nothing is waiting' : 'No requests yet'} body="Requests you made, and requests you can decide, show here. Limits are set under Settings → Pricing & approvals." />
        ) : (
          <ul className="divide-y">
            {rows.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-[13px]">
                <div className="min-w-0 flex-1">
                  <Link to={`${base}/${r.object}/${r.recordId}`} className="font-medium text-primary hover:underline">{r.record || humanize(r.object)}</Link>
                  <p className="text-xs text-muted-foreground">
                    {(kinds[r.kind] ?? r.kind).replace(/ \(.*\)/, '')} · asked by {r.requestedBy || 'someone'} · {new Date(r.createdAt).toLocaleDateString()}
                    {r.decidedBy ? ` · ${r.status} by ${r.decidedBy}` : ''}{r.note ? ` — “${r.note}”` : ''}
                  </p>
                </div>
                <span className="font-semibold tabular-nums">{kindUnit(r.kind, r.value, context.workspace.currency)}</span>
                <Badge tone={statusTone[r.status]}>{r.status === 'pending' ? 'Waiting' : humanize(r.status)}</Badge>
                {r.canDecide ? <Button size="sm" onClick={() => { setError(null); setOpen(r); }}>Decide</Button> : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {open ? (
        <Modal title={open.record || 'Request'} description={`${(kinds[open.kind] ?? open.kind).replace(/ \(.*\)/, '')}: ${kindUnit(open.kind, open.value, context.workspace.currency)}. ${open.reason}`} onClose={() => setOpen(null)}>
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label="Note (optional)"><Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} /></Field>
          <div className="flex justify-end gap-2">
            <Button variant="danger-outline" loading={decide.isPending && decide.variables === 'rejected'} onClick={() => decide.mutate('rejected')}><X /> Reject</Button>
            <Button loading={decide.isPending && decide.variables === 'approved'} onClick={() => decide.mutate('approved')}><Check /> Approve</Button>
          </div>
        </Modal>
      ) : null}
    </PageContainer>
  );
}

// ---------------------------------------------------------------- pricing setup

type SetupTab = 'rules' | 'approvals' | 'currencies';

export function PricingSetupPage() {
  useDocumentTitle('Pricing & approvals');
  const [sp, setSp] = useSearchParams();
  const tab = (sp.get('tab') as SetupTab) || 'rules';
  return (
    <PageContainer>
      <PageHeader title="Pricing & approvals" icon={icon(<SlidersHorizontal />)}
        description="Rules that change a price or decide what can be sold together, the limits above which a discount or a write-off needs approval, and exchange rates. Prices themselves live in price books." />
      <Card className="overflow-hidden">
        <Tabs className="px-3" value={tab} onChange={(v) => setSp({ tab: v }, { replace: true })}
          items={[{ value: 'rules', label: 'Pricing rules' }, { value: 'approvals', label: 'Approval limits' }, { value: 'currencies', label: 'Currencies' }]} />
        {tab === 'rules' ? <RulesTab /> : tab === 'approvals' ? <ApprovalLimitsTab /> : <CurrenciesTab />}
      </Card>
    </PageContainer>
  );
}

const ruleKinds: Record<PricingRule['kind'], string> = { discount: 'Discount', price: 'Special price', configuration: 'Sold together', eligibility: 'Who can buy' };

function describeRule(r: PricingRule): string {
  const c = r.condition;
  const when: string[] = [];
  if (c.itemIds?.length) when.push(`${c.itemIds.length} item${c.itemIds.length > 1 ? 's' : ''}`);
  if (c.family) when.push(`family “${c.family}”`);
  if (c.itemType) when.push(`${c.itemType}s`);
  if (c.minQuantity != null) when.push(`quantity ≥ ${c.minQuantity}`);
  if (c.accountTypes?.length) when.push(`customer type ${c.accountTypes.join(' / ')}`);
  if (c.accountIds?.length) when.push('chosen customers');
  if (c.withItemIds?.length) when.push('bought together');
  const a = r.action;
  const then =
    r.kind === 'discount' ? (a.type === 'percent' ? `${a.value}% off` : `${a.value} off per unit`)
    : r.kind === 'price' ? (a.type === 'fixed' ? `price ${a.value}` : `${a.value}% of list price`)
    : r.kind === 'configuration' ? [a.requires?.length ? `needs ${a.requires.length} other item(s)` : '', a.excludes?.length ? `not with ${a.excludes.length} item(s)` : ''].filter(Boolean).join(', ')
    : `only for ${[...(a.accountTypes ?? []), a.territoryIds?.length ? 'chosen territories' : ''].filter(Boolean).join(' / ')}`;
  return `${when.length ? when.join(', ') : 'Every item'} → ${then}`;
}

function RulesTab() {
  const { code } = useWorkspace();
  const api = commerceApi(code);
  const qc = useQueryClient();
  const listQ = useQuery({ queryKey: ['pricing-rules', code], queryFn: () => api.rules() });
  const [adding, setAdding] = useState(false);
  const refresh = () => void qc.invalidateQueries({ queryKey: ['pricing-rules', code] });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteRule(id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  const toggle = useMutation({ mutationFn: (r: PricingRule) => api.saveRule({ ...r, isActive: !r.isActive }, r.id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  const rows = listQ.data?.data ?? [];
  const canManage = listQ.data?.canManage ?? false;
  return (
    <div>
      <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
        <p className="text-[13px] text-muted-foreground">Checked in order of priority (lowest number first). The first price rule and the first discount rule that match a line apply.</p>
        {canManage ? <Button size="sm" onClick={() => setAdding(true)}><Plus /> Rule</Button> : null}
      </div>
      {listQ.isLoading ? <div className="p-4"><Skeleton className="h-10" /></div> : rows.length === 0 ? (
        <EmptyState icon={SlidersHorizontal} title="No pricing rules" body="Quotes use the price book as it is. Add a rule for volume discounts, customer prices, bundle discounts, or items that must (or can't) be sold together." />
      ) : (
        <ul className="divide-y">
          {rows.map((r) => (
            <li key={r.id} className={cn('flex flex-wrap items-center gap-3 px-4 py-3 text-[13px]', !r.isActive && 'opacity-60')}>
              <span className="w-8 text-xs tabular-nums text-muted-foreground">{r.priority}</span>
              <div className="min-w-0 flex-1">
                <span className="font-medium">{r.name}</span>
                <p className="text-xs text-muted-foreground">{describeRule(r)}{r.effectiveFrom || r.effectiveTo ? ` · ${r.effectiveFrom ?? '…'} to ${r.effectiveTo ?? '…'}` : ''}</p>
              </div>
              <Badge tone="neutral">{ruleKinds[r.kind]}</Badge>
              {canManage ? (
                <>
                  <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => toggle.mutate(r)}>{r.isActive ? 'Turn off' : 'Turn on'}</button>
                  <Button variant="ghost" size="icon-sm" aria-label={`Delete ${r.name}`} onClick={() => remove.mutate(r.id)}><Trash2 /></Button>
                </>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? <RuleDialog onClose={() => setAdding(false)} onDone={refresh} /> : null}
    </div>
  );
}

function ItemList({ label, hint, value, onChange }: { label: string; hint?: string; value: LookupValue[]; onChange: (v: LookupValue[]) => void }) {
  return (
    <Field label={label} hint={hint}>
      <div>
      {value.length ? (
        <div className="mb-1.5 flex flex-wrap gap-1.5">
          {value.map((v) => (
            <span key={v.id} className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs">
              {v.label}
              <button type="button" aria-label={`Remove ${v.label}`} onClick={() => onChange(value.filter((x) => x.id !== v.id))}><X className="size-3" /></button>
            </span>
          ))}
        </div>
      ) : null}
      <RecordPicker target="catalog_items" value={null} onPick={(v) => v && onChange([...value, v])} exclude={value.map((v) => v.id)} placeholder="Search the catalog…" />
      </div>
    </Field>
  );
}

function RuleDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { code } = useWorkspace();
  const api = commerceApi(code);
  const [kind, setKind] = useState<PricingRule['kind']>('discount');
  const [name, setName] = useState('');
  const [priority, setPriority] = useState('100');
  const [items, setItems] = useState<LookupValue[]>([]);
  const [others, setOthers] = useState<LookupValue[]>([]);
  const [together, setTogether] = useState(false);
  const [minQty, setMinQty] = useState('');
  const [accountTypes, setAccountTypes] = useState('');
  const [actionType, setActionType] = useState('percent');
  const [value, setValue] = useState('');
  const [relation, setRelation] = useState<'requires' | 'excludes'>('requires');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [error, setError] = useState<string | null>(null);
  const types = accountTypes.split(',').map((s) => s.trim().toLowerCase()).filter(Boolean);
  const save = useMutation({
    mutationFn: () => {
      const ids = items.map((i) => i.id);
      const condition: PricingRule['condition'] = { itemIds: ids.length ? ids : undefined, minQuantity: minQty === '' ? undefined : Number(minQty) };
      let action: PricingRule['action'] = {};
      if (kind === 'discount' || kind === 'price') {
        if (together) condition.withItemIds = ids;
        if (types.length) condition.accountTypes = types;
        action = { type: kind === 'price' && actionType === 'percent' ? 'percent_of_list' : kind === 'price' ? 'fixed' : actionType, value: Number(value) };
      } else if (kind === 'configuration') {
        action = { [relation]: others.map((o) => o.id) };
      } else {
        action = { accountTypes: types };
      }
      return api.saveRule({ name, kind, priority: Number(priority) || 100, condition, action, effectiveFrom: from || undefined, effectiveTo: to || undefined, isActive: true });
    },
    onSuccess: () => { toast.success('Rule saved'); onDone(); onClose(); },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Modal title="New pricing rule" onClose={onClose}>
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <Field label="What it does">
        <Select value={kind} onChange={(e) => { setKind(e.target.value as PricingRule['kind']); setActionType(e.target.value === 'price' ? 'fixed' : 'percent'); }}>
          <option value="discount">Give a discount</option>
          <option value="price">Set a special price</option>
          <option value="configuration">Items that must, or can't, be sold together</option>
          <option value="eligibility">Limit who can buy an item</option>
        </Select>
      </Field>
      <div className="grid grid-cols-3 gap-3">
        <div className="col-span-2"><Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. 10% off from 100 units" /></Field></div>
        <Field label="Priority"><Input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} /></Field>
      </div>
      <ItemList label={kind === 'discount' || kind === 'price' ? 'For these items' : 'These items'} hint={kind === 'discount' || kind === 'price' ? 'Empty = every item.' : undefined} value={items} onChange={setItems} />
      {kind === 'discount' || kind === 'price' ? (
        <>
          {items.length > 1 ? (
            <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={together} onChange={(e) => setTogether(e.target.checked)} /> Only when all of them are on the same document (bundle price)</label>
          ) : null}
          <div className="grid grid-cols-2 gap-3">
            <Field label="From quantity" hint="Optional."><Input type="number" min="0" value={minQty} onChange={(e) => setMinQty(e.target.value)} /></Field>
            <Field label="Customer types" hint="Optional. e.g. partner, reseller"><Input value={accountTypes} onChange={(e) => setAccountTypes(e.target.value)} /></Field>
            <Field label={kind === 'discount' ? 'Discount' : 'Price'}>
              <Select value={actionType} onChange={(e) => setActionType(e.target.value)}>
                {kind === 'discount' ? (<><option value="percent">Percent off</option><option value="amount">Amount off per unit</option></>) : (<><option value="fixed">Fixed price</option><option value="percent">Percent of list price</option></>)}
              </Select>
            </Field>
            <Field label="Value"><Input type="number" min="0" step="0.01" value={value} onChange={(e) => setValue(e.target.value)} /></Field>
          </div>
        </>
      ) : kind === 'configuration' ? (
        <>
          <Field label="Rule">
            <Select value={relation} onChange={(e) => setRelation(e.target.value as 'requires' | 'excludes')}>
              <option value="requires">…need these on the same document</option>
              <option value="excludes">…can't be sold with these</option>
            </Select>
          </Field>
          <ItemList label="Other items" value={others} onChange={setOthers} />
        </>
      ) : (
        <Field label="Only for these customer types" hint="Separate with commas, e.g. partner, reseller"><Input value={accountTypes} onChange={(e) => setAccountTypes(e.target.value)} /></Field>
      )}
      <div className="grid grid-cols-2 gap-3">
        <Field label="From (optional)"><Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} /></Field>
        <Field label="Until (optional)"><Input type="date" value={to} onChange={(e) => setTo(e.target.value)} /></Field>
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={onClose}>Cancel</Button>
        <Button loading={save.isPending} disabled={!name.trim()} onClick={() => { setError(null); save.mutate(); }}>Save rule</Button>
      </div>
    </Modal>
  );
}

function ApprovalLimitsTab() {
  const { code, context } = useWorkspace();
  const api = commerceApi(code);
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['approval-rules', code], queryFn: () => api.approvalRules() });
  const [kind, setKind] = useState('discount');
  const [min, setMin] = useState('');
  const [role, setRole] = useState('');
  const save = useMutation({
    mutationFn: (rules: ApprovalRule[]) => api.saveApprovalRules(kind, rules),
    onSuccess: () => { setMin(''); void qc.invalidateQueries({ queryKey: ['approval-rules', code] }); toast.success('Limits saved'); },
    onError: (e) => toast.error(firstError(e))
  });
  if (q.isLoading || !q.data) return <div className="p-4"><Skeleton className="h-10" /></div>;
  const { data, kinds, roles, canManage } = q.data;
  const mine = data.filter((r) => r.kind === kind).sort((a, b) => a.minValue - b.minValue);
  const roleName = (k: string) => roles.find((r) => r.key === k)?.name ?? k;
  const unit = kind === 'discount' ? '%' : ` ${context.workspace.currency ?? ''}`;
  return (
    <div className="space-y-4 p-4">
      <Field label="Limits for">
        <Select className="max-w-sm" value={kind} onChange={(e) => setKind(e.target.value)}>
          {Object.entries(kinds).map(([k, label]) => <option key={k} value={k}>{label}</option>)}
        </Select>
      </Field>
      {mine.length === 0 ? (
        <p className="text-[13px] text-muted-foreground">No limit: nothing of this kind needs approval.</p>
      ) : (
        <ul className="divide-y rounded-md border text-[13px]">
          {mine.map((r) => (
            <li key={r.minValue} className="flex items-center gap-3 px-3 py-2">
              <span className="flex-1">From <strong className="tabular-nums">{r.minValue}{unit}</strong> — needs <strong>{roleName(r.approverRole)}</strong> (or a role above it)</span>
              {canManage ? <Button variant="ghost" size="icon-sm" aria-label="Remove this limit" onClick={() => save.mutate(mine.filter((x) => x.minValue !== r.minValue))}><Trash2 /></Button> : null}
            </li>
          ))}
        </ul>
      )}
      {canManage ? (
        <div className="flex flex-wrap items-end gap-3">
          <Field label={kind === 'discount' ? 'From discount (%)' : 'From amount'}><Input className="w-36" type="number" min="0" step="0.01" value={min} onChange={(e) => setMin(e.target.value)} /></Field>
          <Field label="Approved by">
            <Select className="w-52" value={role || roles[0]?.key || ''} onChange={(e) => setRole(e.target.value)}>
              {roles.map((r) => <option key={r.key} value={r.key}>{r.name}</option>)}
            </Select>
          </Field>
          <Button loading={save.isPending} disabled={min === ''} onClick={() => save.mutate([...mine.filter((x) => x.minValue !== Number(min)), { kind, minValue: Number(min), approverRole: role || roles[0]?.key || '', label: '' }])}>
            <Plus /> Add limit
          </Button>
        </div>
      ) : null}
      <p className="text-xs text-muted-foreground">Below the lowest limit nothing waits. A quote can't be sent, and a refund, credit note or write-off doesn't count, until its request is approved (Approvals in the menu).</p>
    </div>
  );
}

function CurrenciesTab() {
  const { code } = useWorkspace();
  const api = commerceApi(code);
  const qc = useQueryClient();
  const curQ = useQuery({ queryKey: ['currencies', code], queryFn: () => api.currencies() });
  const rateQ = useQuery({ queryKey: ['exchange-rates', code], queryFn: () => api.rates() });
  const [from, setFrom] = useState('USD');
  const [rate, setRate] = useState('');
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10));
  const refresh = () => void qc.invalidateQueries({ queryKey: ['exchange-rates', code] });
  const save = useMutation({ mutationFn: () => api.saveRate({ fromCurrency: from, rate: Number(rate), effectiveFrom: date }), onSuccess: () => { setRate(''); refresh(); toast.success('Rate saved'); }, onError: (e) => toast.error(firstError(e)) });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteRate(id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  if (!curQ.data || !rateQ.data) return <div className="p-4"><Skeleton className="h-10" /></div>;
  const base = curQ.data.base;
  const canManage = rateQ.data.canManage;
  return (
    <div className="space-y-4 p-4">
      <p className="text-[13px] text-muted-foreground">
        Your books are in <strong>{base}</strong> (Settings → Business profile). A deal, quote, invoice or payment in another currency takes the rate in force on its date and keeps it: changing a rate later never changes records that already exist.
      </p>
      {rateQ.data.data.length === 0 ? <p className="text-[13px] text-muted-foreground">No exchange rates yet. Add one to use another currency on records.</p> : (
        <ul className="divide-y rounded-md border text-[13px]">
          {rateQ.data.data.map((r) => (
            <li key={r.id} className={cn('flex items-center gap-3 px-3 py-2', r.effectiveTo && 'opacity-60')}>
              <span className="flex-1 tabular-nums">1 {r.fromCurrency} = <strong>{r.rate}</strong> {r.toCurrency}</span>
              <span className="text-xs text-muted-foreground">from {r.effectiveFrom}{r.effectiveTo ? ` to ${r.effectiveTo}` : ''}</span>
              {canManage ? <Button variant="ghost" size="icon-sm" aria-label="Delete this rate" onClick={() => remove.mutate(r.id)}><Trash2 /></Button> : null}
            </li>
          ))}
        </ul>
      )}
      {canManage ? (
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Currency">
            <Select className="w-56" value={from} onChange={(e) => setFrom(e.target.value)}>
              {curQ.data.data.filter((c) => c.code !== base).map((c) => <option key={c.code} value={c.code}>{c.code} — {c.name}</option>)}
            </Select>
          </Field>
          <Field label={`1 ${from} in ${base}`}><Input className="w-32" type="number" min="0" step="any" value={rate} onChange={(e) => setRate(e.target.value)} /></Field>
          <Field label="From"><Input type="date" value={date} onChange={(e) => setDate(e.target.value)} /></Field>
          <Button loading={save.isPending} disabled={!(Number(rate) > 0)} onClick={() => save.mutate()}><Coins /> Save rate</Button>
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------- campaign results

export function CampaignResultsPage() {
  useDocumentTitle('Campaign results');
  const { code } = useWorkspace();
  const api = commerceApi(code);
  const [model, setModel] = useState('first');
  const q = useQuery({ queryKey: ['attribution', code, model], queryFn: () => api.attribution(model), placeholderData: (p) => p });
  const [open, setOpen] = useState<string | null>(null);
  const d = q.data;
  const money = (n?: number) => (n == null ? '—' : formatMoney(n, d?.currency));
  return (
    <PageContainer wide>
      <PageHeader title="Campaign results" icon={icon(<Megaphone />)}
        description="Who each campaign reached, and the deals that came from those people. A deal touched by several campaigns is shared between them — never counted twice." />
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <SegmentedFilter value={model} onChange={setModel} options={(d?.models ?? [{ key: 'first', label: 'First touch' }]).map((m) => ({ value: m.key, label: m.label }))} />
        {d ? <span className="text-[13px] text-muted-foreground">{d.totals.deals} deals touched · {money(d.totals.wonRevenue)} won · {money(d.totals.pipeline)} open</span> : null}
      </div>
      <Card className="overflow-hidden">
        {!d ? <div className="p-4"><Skeleton className="h-10" /></div> : d.data.length === 0 ? (
          <EmptyState icon={Megaphone} title="No campaigns yet" body="Create a campaign under Email campaigns. Everyone it is sent to becomes a member; you can also add members by hand for events and call lists." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[900px] text-[13px]">
              <thead>
                <tr className="border-b bg-muted/40 text-left text-xs font-medium text-muted-foreground">
                  <th className="px-4 py-2.5">Campaign</th>
                  <th className="px-3 py-2.5 text-right">Members</th>
                  <th className="px-3 py-2.5 text-right">Responded</th>
                  <th className="px-3 py-2.5 text-right">Leads → converted</th>
                  <th className="px-3 py-2.5 text-right">Deals</th>
                  <th className="px-3 py-2.5 text-right">Open pipeline</th>
                  <th className="px-3 py-2.5 text-right">Won revenue</th>
                  <th className="px-3 py-2.5 text-right">Cost</th>
                  <th className="px-4 py-2.5 text-right">ROI</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {d.data.map((c) => (
                  <CampaignRow key={c.id} c={c} money={money} open={open === c.id} onToggle={() => setOpen(open === c.id ? null : c.id)} canManage={d.canManage} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </PageContainer>
  );
}

function CampaignRow({ c, money, open, onToggle, canManage }: { c: CampaignAttribution; money: (n?: number) => string; open: boolean; onToggle: () => void; canManage: boolean }) {
  return (
    <>
      <tr className="hover:bg-muted/30">
        <td className="px-4 py-2.5">
          <button type="button" className="flex items-center gap-1.5 font-medium" onClick={onToggle} aria-expanded={open}>
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />} {c.name}
          </button>
          <span className="ml-5 text-xs text-muted-foreground">{humanize(c.type)} · {humanize(c.status)}</span>
        </td>
        <td className="px-3 py-2.5 text-right tabular-nums">{c.members}</td>
        <td className="px-3 py-2.5 text-right tabular-nums">{c.responded}</td>
        <td className="px-3 py-2.5 text-right tabular-nums">{c.leads} → {c.convertedLeads}{c.leads ? ` (${c.conversionRate}%)` : ''}</td>
        <td className="px-3 py-2.5 text-right tabular-nums">{c.opportunities}</td>
        <td className="px-3 py-2.5 text-right tabular-nums">{money(c.pipeline)}</td>
        <td className="px-3 py-2.5 text-right font-semibold tabular-nums">{money(c.wonRevenue)}</td>
        <td className="px-3 py-2.5 text-right tabular-nums">{money(c.actualCost)}</td>
        <td className={cn('px-4 py-2.5 text-right font-medium tabular-nums', c.roiPercent != null && (c.roiPercent >= 0 ? 'text-success' : 'text-danger'))}>{c.roiPercent != null ? `${c.roiPercent}%` : '—'}</td>
      </tr>
      {open ? (
        <tr>
          <td colSpan={9} className="bg-muted/20 px-4 py-3"><CampaignMembers campaign={c} canManage={canManage} /></td>
        </tr>
      ) : null}
    </>
  );
}

function CampaignMembers({ campaign, canManage }: { campaign: CampaignAttribution; canManage: boolean }) {
  const { code } = useWorkspace();
  const api = commerceApi(code);
  const qc = useQueryClient();
  const base = workspaceBase(code);
  const q = useQuery({ queryKey: ['campaign-members', code, campaign.id], queryFn: () => api.campaignMembers(campaign.id), enabled: canManage });
  const [kind, setKind] = useState<'contacts' | 'leads'>('contacts');
  const [cost, setCost] = useState(campaign.actualCost != null ? String(campaign.actualCost) : '');
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['campaign-members', code, campaign.id] });
    void qc.invalidateQueries({ queryKey: ['attribution', code] });
  };
  const add = useMutation({ mutationFn: (v: LookupValue) => api.addCampaignMember(campaign.id, kind === 'contacts' ? { contactId: v.id } : { leadId: v.id }), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  const update = useMutation({ mutationFn: (v: { id: string; status: string }) => api.updateCampaignMember(v.id, { status: v.status }), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  const remove = useMutation({ mutationFn: (id: string) => api.removeCampaignMember(id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  const saveCost = useMutation({ mutationFn: () => api.campaignCosts(campaign.id, { actualCost: Number(cost) || 0 }), onSuccess: () => { toast.success('Cost saved'); refresh(); }, onError: (e) => toast.error(firstError(e)) });
  if (!canManage) return <p className="text-[13px] text-muted-foreground">You need the “Manage email campaigns” permission to see and change members.</p>;
  const rows = q.data?.data ?? [];
  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <div className="lg:col-span-2">
        <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Members</p>
        {q.isLoading ? <Skeleton className="h-8" /> : rows.length === 0 ? <p className="text-[13px] text-muted-foreground">No members yet.</p> : (
          <ul className="max-h-64 divide-y overflow-y-auto rounded-md border bg-card text-[13px]">
            {rows.map((m) => (
              <li key={m.id} className="flex items-center gap-2 px-3 py-1.5">
                <Link to={`${base}/${m.object}/${m.recordId}`} className="min-w-0 flex-1 truncate font-medium text-primary hover:underline">{m.name || m.email}</Link>
                <span className="text-xs text-muted-foreground">{m.object === 'leads' ? 'Lead' : 'Contact'}</span>
                <Select className="h-7 w-32 text-xs" value={m.status} onChange={(e) => update.mutate({ id: m.id, status: e.target.value })} aria-label={`Status of ${m.name}`}>
                  {(q.data?.statuses ?? []).map((s) => <option key={s} value={s}>{humanize(s)}</option>)}
                </Select>
                <Button variant="ghost" size="icon-sm" aria-label={`Remove ${m.name}`} onClick={() => remove.mutate(m.id)}><X /></Button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="space-y-3">
        <Field label="Add a member">
          <div>
          <Select className="mb-1.5" value={kind} onChange={(e) => setKind(e.target.value as 'contacts' | 'leads')}>
            <option value="contacts">A contact</option>
            <option value="leads">A lead</option>
          </Select>
          <RecordPicker key={kind} target={kind} value={null} onPick={(v) => v && add.mutate(v)} exclude={rows.filter((r) => r.object === kind).map((r) => r.recordId)} />
          </div>
        </Field>
        <Field label="What the campaign cost" hint="Used for ROI and cost per lead.">
          <div className="flex gap-2">
            <Input type="number" min="0" step="0.01" value={cost} onChange={(e) => setCost(e.target.value)} />
            <Button variant="outline" loading={saveCost.isPending} onClick={() => saveCost.mutate()}>Save</Button>
          </div>
        </Field>
      </div>
    </div>
  );
}
