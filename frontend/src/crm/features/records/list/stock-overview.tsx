import { useQuery } from '@tanstack/react-query';
import { ArrowDownToLine, ArrowUpFromLine, Boxes, Car, PackageCheck, TriangleAlert } from 'lucide-react';
import { businessApi } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { AnimatedNumber, ProgressBar, shortMoney, useEntered, useReducedMotion } from '@crm/components/charts';
import { cn } from '@crm/lib/utils';
import { useRecordScope } from '../record-scope';

// Above the stock lists (D-178): eight weeks of stock in against out, what is on hand per
// product (bikes counted with the accessories), and the totals — from the movements.

export function StockOverview() {
  const scope = useRecordScope();
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const q = useQuery({ queryKey: ['workspace', code, 'stock-summary'], queryFn: () => businessApi.stockSummary(code), enabled: Boolean(code), staleTime: 30_000 });
  const on = useEntered(80);
  const reduced = useReducedMotion();
  const d = q.data;
  if (!code || q.isError) return null;
  if (!d) return <div className="mb-4 h-[260px] animate-pulse rounded-2xl bg-muted" />;
  const max = Math.max(1, ...d.weeks.flatMap((w) => [w.in, w.out]));
  const topItem = Math.max(1, ...d.items.map((i) => i.onHand));
  return (
    <div className="mb-4 space-y-3">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Tile icon={Boxes} label="On hand" value={d.onHand} sub={`${d.lines} stock lines`} />
        <Tile icon={PackageCheck} label="Promised" value={d.reserved} sub="reserved for customers" />
        <Tile icon={TriangleAlert} label="Running low" value={d.low} sub="products to reorder" warn={d.low > 0} />
        <Tile icon={ArrowDownToLine} label="Stock value" value={d.value} sub={`in ${d.inThisWeek} · out ${d.outThisWeek} this week`} money />
      </div>
      <div className="grid gap-3 xl:grid-cols-[1.4fr_1fr]">
        <Card className="crm-rise p-5">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div>
              <p className="text-[15px] font-semibold text-foreground">Stock in and out</p>
              <p className="text-xs text-muted-foreground">Every movement, by week — received and returned against issued, delivered and damaged</p>
            </div>
            <div className="flex items-center gap-3 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5"><ArrowDownToLine className="size-3.5 text-chart-1" /> In</span>
              <span className="flex items-center gap-1.5"><ArrowUpFromLine className="size-3.5 text-chart-2" /> Out</span>
            </div>
          </div>
          <div className="flex h-[170px] items-end gap-3">
            {d.weeks.map((w, i) => (
              <div key={w.label} className="group flex min-w-0 flex-1 flex-col items-center gap-1">
                <span className="text-[10px] tabular-nums text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100">+{w.in} / −{w.out}</span>
                <div className="flex w-full flex-1 items-end justify-center gap-1">
                  {[['in', w.in, 'bg-chart-1'], ['out', w.out, 'bg-chart-2']].map(([k, v, c]) => (
                    <span key={k as string} className={cn('w-1/3 max-w-[18px] rounded-t-md', c as string)}
                      style={{ height: `${((v as number) / max) * 100}%`, minHeight: (v as number) > 0 ? 3 : 0, transformOrigin: 'bottom',
                        transform: on || reduced ? 'scaleY(1)' : 'scaleY(0)', transition: `transform 900ms cubic-bezier(0.16,1,0.3,1) ${i * 60}ms` }} />
                  ))}
                </div>
                <span className="text-[10px] text-muted-foreground">{w.label}</span>
              </div>
            ))}
          </div>
        </Card>
        <Card className="crm-rise p-5">
          <p className="mb-3 text-[15px] font-semibold text-foreground">On hand by product</p>
          {d.items.length === 0 ? (
            <p className="py-8 text-center text-[13px] text-muted-foreground">No stock lines yet.</p>
          ) : (
            <ul className="space-y-2.5">
              {d.items.slice(0, 7).map((i) => (
                <li key={i.name}>
                  <div className="mb-1 flex items-center justify-between gap-2 text-[13px]">
                    <span className="flex min-w-0 items-center gap-1.5 truncate font-medium text-foreground">
                      {i.vehicles ? <Car className="size-3.5 shrink-0 text-primary" /> : null}{i.name}
                    </span>
                    <span className={cn('shrink-0 tabular-nums', i.low ? 'font-semibold text-warning' : 'text-muted-foreground')}>
                      {i.onHand}{i.reserved ? ` · ${i.reserved} promised` : ''}{i.low ? ' · low' : ''}
                    </span>
                  </div>
                  <ProgressBar value={i.onHand} max={topItem} color={i.low ? 'hsl(var(--warning))' : 'hsl(var(--chart-1))'} />
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}

function Tile({ icon: Icon, label, value, sub, warn, money }: { icon: typeof Boxes; label: string; value: number; sub: string; warn?: boolean; money?: boolean }) {
  return (
    <Card className="crm-rise p-4">
      <span className={cn('grid size-8 place-items-center rounded-lg', warn ? 'bg-warning-soft text-warning' : 'bg-primary-soft text-primary')}><Icon className="size-4" /></span>
      <p className="mt-2 text-xs text-muted-foreground">{label}</p>
      <p className={cn('text-[22px] font-bold leading-tight', warn ? 'text-warning' : 'text-foreground')}>
        <AnimatedNumber value={value} format={(n) => (money ? shortMoney(n) : Math.round(n).toLocaleString('en-IN'))} />
      </p>
      <p className="truncate text-[11px] text-muted-foreground">{sub}</p>
    </Card>
  );
}
