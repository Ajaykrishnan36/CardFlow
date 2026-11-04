import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ClipboardList } from 'lucide-react';
import { businessApi } from '@crm/api/endpoints';
import { Badge, Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { workspaceBase } from './workspace-context';

// The top of Home (D-169): the work assigned to me. Paid access is on Billing and on
// Users & access, not on Home.

export function AtAGlance({ code }: { code: string }) {
  return (
    <section className="grid gap-3" aria-label="At a glance">
      <AssignedCard code={code} />
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
