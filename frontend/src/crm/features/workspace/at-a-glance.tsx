import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight, ClipboardList, CreditCard } from 'lucide-react';
import { businessApi } from '@crm/api/endpoints';
import { Badge, Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { PaidBadge, paidState, useBillingOverview } from '@crm/features/billing/billing-panel';
import { workspaceBase } from './workspace-context';

// The top of Home (D-169): the work assigned to me, and where paid access stands — mine,
// and for whoever pays for the team, who isn't paid or runs out within a week.

export function AtAGlance({ code }: { code: string }) {
  const billing = useBillingOverview(code);
  const charged = !!billing.data?.status.enforced && !billing.data.exempt;
  return (
    <section className={cn('grid gap-3', charged && 'lg:grid-cols-2')} aria-label="At a glance">
      <AssignedCard code={code} />
      {charged && billing.data ? <BillingCard code={code} data={billing.data} /> : null}
    </section>
  );
}

function AssignedCard({ code }: { code: string }) {
  const q = useQuery({
    queryKey: ['workspace', code, 'summary', 'mine'],
    queryFn: () => businessApi.summary(code, { range: 'today' }),
    staleTime: 30_000
  });
  const base = workspaceBase(code);
  const rows = q.data?.mine ?? [];
  const total = rows.reduce((n, r) => n + r.count, 0);
  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center justify-between gap-2">
        <p className="flex items-center gap-2 text-[14px] font-semibold text-foreground">
          <span className="grid size-7 place-items-center rounded-md bg-primary-soft text-primary">
            <ClipboardList className="size-4" aria-hidden />
          </span>
          Assigned to me
        </p>
        {q.data ? <Badge tone={total ? 'primary' : 'neutral'}>{total} open</Badge> : null}
      </div>
      {!q.data ? (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-14" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <p className="text-[13px] text-muted-foreground">Nothing is assigned to you.</p>
      ) : (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {rows.map((r) => (
            <Link
              key={r.object}
              to={`${base}/${r.object}`}
              className={cn(
                'rounded-lg border px-3 py-2 transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                r.count > 0 && 'border-primary/30 bg-primary-soft/30'
              )}
            >
              <span className={cn('block text-xl font-semibold tabular-nums', r.count ? 'text-foreground' : 'text-muted-foreground')}>{r.count}</span>
              <span className="block truncate text-xs text-muted-foreground">{r.label}</span>
            </Link>
          ))}
        </div>
      )}
    </Card>
  );
}

function BillingCard({ code, data }: { code: string; data: NonNullable<ReturnType<typeof useBillingOverview>['data']> }) {
  const to = `${workspaceBase(code)}/settings/billing`;
  const me = data.members.find((m) => m.me);
  const team = data.status.canPayOthers ? data.members.filter((m) => m.canPay) : [];
  const states = team.map((m) => ({ m, st: paidState(m.paidUntil) }));
  const unpaid = states.filter((x) => x.st.tone === 'danger' && (x.st.days === undefined || x.st.days <= 0));
  const soon = states.filter((x) => x.st.days !== undefined && x.st.days > 0 && x.st.days <= 7);
  const paid = states.filter((x) => x.st.tone === 'success');
  const attention = [...unpaid, ...soon].filter((x) => !x.m.me).slice(0, 4);
  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center justify-between gap-2">
        <p className="flex items-center gap-2 text-[14px] font-semibold text-foreground">
          <span className="grid size-7 place-items-center rounded-md bg-primary-soft text-primary">
            <CreditCard className="size-4" aria-hidden />
          </span>
          Billing
        </p>
        <Link to={to} className="inline-flex items-center gap-0.5 text-[13px] font-medium text-primary hover:underline">
          Open Billing <ChevronRight className="size-3.5" aria-hidden />
        </Link>
      </div>
      {me ? (
        <div className="mb-3 flex items-center justify-between gap-2 rounded-lg border px-3 py-2">
          <span className="text-[13px] text-foreground">Your access</span>
          <PaidBadge paidUntil={me.paidUntil} to={to} />
        </div>
      ) : null}
      {team.length ? (
        <>
          <div className="grid grid-cols-3 gap-2">
            <Tile to={to} n={unpaid.length} label="Not paid / expired" tone="danger" />
            <Tile to={to} n={soon.length} label="Expire in 7 days" tone="warning" />
            <Tile to={to} n={paid.length} label="Paid" tone="success" />
          </div>
          {attention.length ? (
            <ul className="mt-3 divide-y rounded-lg border">
              {attention.map(({ m }) => (
                <li key={m.membershipId} className="flex items-center justify-between gap-2 px-3 py-1.5">
                  <span className="min-w-0 truncate text-[13px] text-foreground">
                    {m.name}
                    {m.roleName ? <span className="text-muted-foreground"> · {m.roleName}</span> : null}
                  </span>
                  <PaidBadge paidUntil={m.paidUntil} to={to} />
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : null}
    </Card>
  );
}

function Tile({ to, n, label, tone }: { to: string; n: number; label: string; tone: 'danger' | 'warning' | 'success' }) {
  const color = { danger: 'text-danger', warning: 'text-warning', success: 'text-success' }[tone];
  return (
    <Link to={to} className="rounded-lg border px-3 py-2 transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <span className={cn('block text-xl font-semibold tabular-nums', n ? color : 'text-muted-foreground')}>{n}</span>
      <span className="block text-xs leading-tight text-muted-foreground">{label}</span>
    </Link>
  );
}
