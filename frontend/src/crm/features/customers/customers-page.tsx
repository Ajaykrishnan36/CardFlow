import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Building2, CalendarClock, Info, Mail, MessageSquare, Phone, Search, Users } from 'lucide-react';
import { businessApi, type AccountHealth } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Skeleton } from '@crm/components/ui/spinner';
import { AnimatedNumber, Heatmap, HealthRing, Sparkline, healthLabel, shortMoney } from '@crm/components/charts';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useRecordScope } from '@crm/features/records/record-scope';
import { recordKeys } from '@crm/features/records/use-object-meta';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { cn } from '@crm/lib/utils';

// Accounts & contacts (D-176): every account with its owner, open pipeline, a health score,
// twelve weeks of engagement and the last touch; pick one to see its activity day by day and
// the people there. The score is worked out from what happens on the account (see "How the
// score works"); nothing here is typed in.

type Tab = 'accounts' | 'contacts';
type Band = 'all' | 'healthy' | 'watch' | 'risk';

const band = (s: number): Band => (s >= 70 ? 'healthy' : s >= 50 ? 'watch' : 'risk');

export function CustomersPage() {
  const { code, context } = useWorkspace();
  const base = workspaceBase(code);
  useDocumentTitle(`Accounts & contacts · ${context.workspace.name}`);
  const [tab, setTab] = useState<Tab>('accounts');
  const [filter, setFilter] = useState<Band>('all');
  const [q, setQ] = useState('');
  const [picked, setPicked] = useState<string | null>(null);
  const list = useQuery({ queryKey: ['workspace', code, 'customer-health'], queryFn: () => businessApi.customerHealth(code), staleTime: 60_000 });
  const accounts = list.data?.data ?? [];
  const shown = useMemo(() => {
    const term = q.trim().toLowerCase();
    return accounts
      .filter((a) => filter === 'all' || band(a.score) === filter)
      .filter((a) => !term || `${a.name} ${a.industry ?? ''} ${a.owner}`.toLowerCase().includes(term))
      .sort((a, b) => b.openValue - a.openValue || b.score - a.score);
  }, [accounts, filter, q]);
  const sel = shown.find((a) => a.id === picked) ?? shown[0];
  const counts = { all: accounts.length, healthy: 0, watch: 0, risk: 0 } as Record<Band, number>;
  for (const a of accounts) counts[band(a.score)]++;
  const contactsTotal = useContactsTotal();

  return (
    <div className="mx-auto w-full max-w-[1440px] space-y-4 px-4 py-6 sm:px-6 lg:px-8">
      <div>
        <h1 className="text-[26px] font-bold tracking-tight text-foreground">Accounts & contacts</h1>
        <p className="text-[13px] text-muted-foreground">
          <span className="font-medium text-foreground">{accounts.length} accounts</span>{contactsTotal !== undefined ? ` · ${contactsTotal} contacts` : ''} · health worked out from activity
        </p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[1.75fr_1fr]">
        <Card className="crm-rise overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
            <div className="inline-flex rounded-full bg-muted p-0.5" role="tablist">
              {(['accounts', 'contacts'] as const).map((t) => (
                <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}
                  className={cn('rounded-full px-4 py-1 text-[13px] font-medium capitalize transition-colors', tab === t ? 'bg-card text-foreground shadow-card' : 'text-muted-foreground hover:text-foreground')}>
                  {t}
                </button>
              ))}
            </div>
            <Input value={q} onChange={(e) => setQ(e.target.value)} leading={<Search />} placeholder={tab === 'accounts' ? 'Filter accounts or industries' : 'Search contacts'} className="h-9 w-64 rounded-full" />
          </div>
          {tab === 'accounts' ? (
            <>
              <div className="flex flex-wrap gap-1.5 px-4 pt-3">
                {([['all', 'All', 'bg-foreground'], ['healthy', 'Healthy', 'bg-success'], ['watch', 'Watch', 'bg-warning'], ['risk', 'At risk', 'bg-danger']] as const).map(([k, label, dot]) => (
                  <button key={k} type="button" onClick={() => setFilter(k)}
                    className={cn('inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors', filter === k ? 'border-transparent bg-sidebar-active text-sidebar-active-foreground' : 'text-muted-foreground hover:text-foreground')}>
                    <span className={cn('size-1.5 rounded-full', filter === k ? 'bg-current' : dot)} /> {label} <span className="opacity-70">{counts[k]}</span>
                  </button>
                ))}
              </div>
              {list.isPending ? (
                <div className="space-y-2 p-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-12" />)}</div>
              ) : (
                <div className="crm-scroll overflow-x-auto">
                  <table className="w-full min-w-[760px] text-[13px]">
                    <thead className="text-left text-[11px] uppercase tracking-wider text-muted-foreground">
                      <tr>
                        <th className="px-4 py-2.5 font-medium">Account</th>
                        <th className="px-3 py-2.5 font-medium">Owner</th>
                        <th className="px-3 py-2.5 text-right font-medium">Pipeline</th>
                        <th className="px-3 py-2.5 font-medium">Health</th>
                        <th className="px-3 py-2.5 font-medium">Engagement · {list.data?.weeks ?? 12} wks</th>
                        <th className="px-4 py-2.5 text-right font-medium">Last touch</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y">
                      {shown.map((a, i) => {
                        const h = healthLabel(a.score);
                        const active = sel?.id === a.id;
                        return (
                          <tr key={a.id} onClick={() => setPicked(a.id)}
                            className={cn('crm-rise cursor-pointer transition-colors hover:bg-muted/40', active && 'bg-primary-soft/50 shadow-[inset_3px_0_0_hsl(var(--primary))]')}
                            style={{ animationDelay: `${Math.min(i, 14) * 25}ms` }}>
                            <td className="px-4 py-2.5">
                              <div className="flex items-center gap-2.5">
                                <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-primary-soft text-[11px] font-bold text-primary">{initials(a.name)}</span>
                                <div className="min-w-0">
                                  <p className="truncate font-semibold text-foreground">{a.name}</p>
                                  <p className="truncate text-[11px] text-muted-foreground">{a.industry ? humanize(a.industry) : a.kind === 'individual' ? 'Individual' : 'Business'}</p>
                                </div>
                              </div>
                            </td>
                            <td className="px-3 py-2.5 text-muted-foreground">{a.owner || '—'}</td>
                            <td className="px-3 py-2.5 text-right">
                              <p className="font-semibold tabular-nums">{shortMoney(a.openValue)}</p>
                              <p className="text-[11px] text-muted-foreground">{a.openDeals} open</p>
                            </td>
                            <td className="px-3 py-2.5">
                              <span className="flex items-center gap-2"><HealthRing score={a.score} size={34} /><span className={cn('text-xs font-medium', h.tone)}>{h.label}</span></span>
                            </td>
                            <td className="px-3 py-2.5">
                              <Sparkline points={a.engagement} width={120} height={28} color={a.score >= 70 ? 'hsl(var(--success))' : a.score >= 50 ? 'hsl(var(--warning))' : 'hsl(var(--danger))'} />
                            </td>
                            <td className="px-4 py-2.5 text-right text-xs text-muted-foreground">{a.lastTouch ? <span className="inline-flex items-center gap-1">{kindIcon(a.lastKind)} {ago(a.lastTouch)}</span> : 'Never'}</td>
                          </tr>
                        );
                      })}
                      {shown.length === 0 ? <tr><td colSpan={6} className="px-4 py-10 text-center text-muted-foreground">No accounts match.</td></tr> : null}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          ) : (
            <ContactsTab q={q} base={base} />
          )}
        </Card>
        {sel ? <AccountPanel code={code} a={sel} base={base} /> : list.isPending ? <Skeleton className="h-[520px] rounded-2xl" /> : null}
      </div>
    </div>
  );
}

function AccountPanel({ code, a, base }: { code: string; a: AccountHealth; base: string }) {
  const q = useQuery({ queryKey: ['workspace', code, 'customer-health', a.id], queryFn: () => businessApi.customerHealth(code, a.id), staleTime: 60_000 });
  const d = q.data?.detail;
  const h = healthLabel(a.score);
  const kinds = d?.kinds ?? {};
  const touches = Object.values(kinds).reduce((n, v) => n + v, 0);
  return (
    <div className="space-y-4">
      <div className="crm-hero crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift">
        <div className="crm-hero-stars pointer-events-none absolute inset-0 opacity-40" aria-hidden />
        <div className="relative flex items-start gap-3">
          <span className="grid size-12 shrink-0 place-items-center rounded-2xl bg-primary text-[14px] font-bold text-primary-foreground">{initials(a.name)}</span>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[17px] font-semibold">{a.name}</p>
            <p className="truncate text-xs text-white/65">{a.industry ? humanize(a.industry) : 'Account'}{a.owner ? ` · Owner ${a.owner}` : ''}</p>
          </div>
          <div className="text-center">
            <HealthRingLarge score={a.score} />
            <p className={cn('mt-0.5 text-[10px] font-semibold uppercase tracking-wider', h.tone)}>{h.label}</p>
          </div>
        </div>
        <div className="relative mt-4 grid grid-cols-3 gap-2">
          <PanelTile label="Open pipeline" value={shortMoney(a.openValue)} sub={`${a.openDeals} deals`} />
          <PanelTile label="Won" value={shortMoney(a.wonValue)} sub="all time" />
          <PanelTile label="Last touch" value={a.lastTouch ? ago(a.lastTouch) : '—'} sub={a.lastKind ? humanize(a.lastKind) : ''} />
        </div>
        <div className="relative mt-4">
          <div className="mb-2 flex items-end justify-between">
            <div>
              <p className="text-[13px] font-semibold">Engagement</p>
              <p className="text-[11px] text-white/55">Everything that happened on the account, last {d?.weeks ?? 18} weeks</p>
            </div>
            <p className="text-right"><span className="text-[22px] font-bold"><AnimatedNumber value={touches} /></span> <span className="text-[11px] text-white/60">touches</span></p>
          </div>
          {d ? <Heatmap cells={d.heatmap} rows={7} /> : <div className="h-28 animate-pulse rounded-lg bg-white/5" />}
          <div className="mt-3 grid grid-cols-4 gap-2 text-center text-[11px] text-white/70">
            {([['updates', 'Updates', MessageSquare], ['messages', 'Messages', Mail], ['calls', 'Calls', Phone], ['meetings', 'Meetings', CalendarClock]] as const).map(([k, label, Icon]) => (
              <div key={k} className="rounded-lg bg-white/5 py-1.5">
                <Icon className="mx-auto mb-0.5 size-3.5 text-mint" />
                <p className="text-[14px] font-semibold text-white">{kinds[k] ?? 0}</p>
                {label}
              </div>
            ))}
          </div>
        </div>
      </div>
      <Card className="crm-rise p-5">
        <div className="mb-2 flex items-center justify-between">
          <div>
            <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><Users className="size-4 text-primary" /> Key contacts</p>
            <p className="text-xs text-muted-foreground">{d?.contacts.length ?? 0} people at this account</p>
          </div>
          <Button asChild size="sm" variant="navy" className="rounded-full"><Link to={`${base}/accounts/${a.id}`}>Open account</Link></Button>
        </div>
        {!d ? (
          <Skeleton className="h-32" />
        ) : d.contacts.length === 0 ? (
          <p className="py-4 text-center text-[13px] text-muted-foreground">No contacts at this account yet.</p>
        ) : (
          <ul className="divide-y">
            {d.contacts.map((c) => (
              <li key={c.id}>
                <Link to={`${base}/contacts/${c.id}`} className="flex items-center gap-3 py-2 hover:text-primary">
                  <span className="grid size-9 shrink-0 place-items-center rounded-full bg-muted text-[11px] font-bold">{initials(c.name)}</span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-medium text-foreground">{c.name}</p>
                    <p className="truncate text-[11px] text-muted-foreground">{c.title || '—'}</p>
                  </div>
                  {c.role ? <span className="rounded-full bg-primary-soft px-2 py-0.5 text-[11px] font-medium text-primary">{c.role}</span> : null}
                  <span className="w-14 text-right text-[11px] text-muted-foreground">{c.lastTouch ? ago(c.lastTouch) : ''}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
        <details className="mt-3 rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          <summary className="flex cursor-pointer items-center gap-1.5 font-medium text-foreground"><Info className="size-3.5" /> How the score works</summary>
          <p className="mt-1.5 leading-relaxed">
            Up to 40 for how recently anyone touched the account (a week, a month, three months), up to 25 for how much happened in 12 weeks, up to 20 for deals won,
            plus 15 to start. Less 15 if a deal is past its close date, and 15 if an invoice is overdue. 70+ is healthy, 50–69 watch, under 50 at risk.
          </p>
        </details>
      </Card>
    </div>
  );
}

function ContactsTab({ q, base }: { q: string; base: string }) {
  const scope = useRecordScope();
  const [page, setPage] = useState(0);
  const per = 25;
  const params = { limit: per, offset: page * per, q: q.trim() || undefined };
  const list = useQuery({ queryKey: recordKeys.list(scope.prefix, 'contacts', params), queryFn: () => scope.api.list('contacts', params), placeholderData: (p) => p });
  const rows = list.data?.data ?? [];
  const total = list.data?.total ?? 0;
  return (
    <>
      <div className="crm-scroll overflow-x-auto">
        <table className="w-full min-w-[760px] text-[13px]">
          <thead className="text-left text-[11px] uppercase tracking-wider text-muted-foreground">
            <tr>
              <th className="px-4 py-2.5 font-medium">Contact</th>
              <th className="px-3 py-2.5 font-medium">Account</th>
              <th className="px-3 py-2.5 font-medium">Designation</th>
              <th className="px-3 py-2.5 font-medium">Email</th>
              <th className="px-3 py-2.5 font-medium">Phone</th>
              <th className="px-3 py-2.5 font-medium">Owner</th>
              <th className="px-4 py-2.5 text-right font-medium">Updated</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((r, i) => (
              <tr key={r.id} className="crm-rise hover:bg-muted/40" style={{ animationDelay: `${Math.min(i, 14) * 20}ms` }}>
                <td className="px-4 py-2.5">
                  <Link to={`${base}/contacts/${r.id}`} className="flex items-center gap-2.5 font-medium text-foreground hover:text-primary">
                    <span className="grid size-8 place-items-center rounded-full bg-muted text-[11px] font-bold">{initials(r.title)}</span>{r.title}
                  </Link>
                </td>
                <td className="px-3 py-2.5 text-muted-foreground">{r.lookups.accountId ? <Link to={`${base}/accounts/${r.lookups.accountId.id}`} className="inline-flex items-center gap-1 hover:text-primary"><Building2 className="size-3.5" />{r.lookups.accountId.label}</Link> : '—'}</td>
                <td className="px-3 py-2.5 text-muted-foreground">{String(r.values.title ?? '') || '—'}</td>
                <td className="px-3 py-2.5 text-muted-foreground">{String(r.values.email ?? '') || '—'}</td>
                <td className="px-3 py-2.5 text-muted-foreground">{String(r.values.mobile ?? r.values.phone ?? '') || '—'}</td>
                <td className="px-3 py-2.5 text-muted-foreground">{r.lookups.ownerId?.label ?? ''}</td>
                <td className="px-4 py-2.5 text-right text-xs text-muted-foreground">{ago(r.updatedAt)}</td>
              </tr>
            ))}
            {rows.length === 0 && !list.isPending ? <tr><td colSpan={7} className="px-4 py-10 text-center text-muted-foreground">No contacts match.</td></tr> : null}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
        <span>{total} contacts</span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)}>Previous</Button>
          <span>Page {page + 1} of {Math.max(1, Math.ceil(total / per))}</span>
          <Button variant="outline" size="sm" disabled={(page + 1) * per >= total} onClick={() => setPage((p) => p + 1)}>Next</Button>
        </div>
      </div>
    </>
  );
}

function useContactsTotal() {
  const scope = useRecordScope();
  const params = { limit: 1 };
  const q = useQuery({ queryKey: recordKeys.list(scope.prefix, 'contacts', params), queryFn: () => scope.api.list('contacts', params), staleTime: 60_000, enabled: scope.can('contacts', 'read') });
  return q.data?.total;
}

function HealthRingLarge({ score }: { score: number }) {
  return (
    <span className="inline-grid rounded-full bg-white/5 p-1">
      <HealthRing score={score} size={56} light />
    </span>
  );
}

function PanelTile({ label, value, sub }: { label: string; value: string; sub: string }) {
  return (
    <div className="rounded-xl border border-white/10 bg-white/5 px-3 py-2.5">
      <p className="text-[11px] text-white/60">{label}</p>
      <p className="truncate text-[17px] font-bold text-white">{value}</p>
      <p className="truncate text-[10px] text-white/45">{sub}</p>
    </div>
  );
}

function kindIcon(kind?: string) {
  if (kind === 'communications') return <Mail className="size-3.5" />;
  if (kind === 'call_logs') return <Phone className="size-3.5" />;
  if (kind === 'appointments' || kind === 'events') return <CalendarClock className="size-3.5" />;
  return <MessageSquare className="size-3.5" />;
}

function ago(iso: string) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

function initials(s: string) {
  return s.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join('') || '·';
}

function humanize(s: string) {
  const t = s.replace(/_/g, ' ');
  return t.charAt(0).toUpperCase() + t.slice(1);
}
