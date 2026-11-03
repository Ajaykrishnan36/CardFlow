import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Building2 } from 'lucide-react';
import { businessApi, type OfficeRow } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { formatMoney } from '@crm/lib/money';
import { toTenantPath } from '@crm/lib/tenant';
import { cn } from '@crm/lib/utils';

// The owner's view of the office (D-170): this month's numbers, today's work, test ride →
// purchase by model and stock by model — all counted from the records the flow makes.

const value = (r: OfficeRow) => (r.money ? formatMoney(r.value) : String(r.value));

export function OfficeCard({ code }: { code: string }) {
  const q = useQuery({ queryKey: ['workspace', code, 'office'], queryFn: () => businessApi.office(code), staleTime: 30_000 });
  if (q.isError) return null;
  const d = q.data;
  const top = d ? Math.max(1, ...d.funnel.map((f) => f.value)) : 1;
  return (
    <section className="space-y-3" aria-label="What is happening">
      <div className="flex items-center gap-2">
        <span className="grid size-7 place-items-center rounded-md bg-primary-soft text-primary">
          <Building2 className="size-4" aria-hidden />
        </span>
        <h2 className="text-[15px] font-semibold text-foreground">What is happening · {d?.period ?? 'this month'}</h2>
      </div>
      {!d ? (
        <Skeleton className="h-40" />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
            {d.tiles.map((r) => (
              <Tile key={r.key} r={r} />
            ))}
            {d.testRideToPurchase !== undefined ? (
              <Tile r={{ key: 'ratio', label: `${d.rideWord ?? 'Test ride'} → purchase`, value: Math.round(d.testRideToPurchase) }} suffix="%" />
            ) : null}
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            <Card className="p-4">
              {d.funnel.length ? (
                <>
                  <p className="mb-2 text-[13px] font-semibold text-foreground">Sales funnel this month</p>
                  <ul className="space-y-1.5">
                    {d.funnel.map((f, i) => (
                      <li key={f.key} className="flex items-center gap-2 text-[13px]">
                        <span className="w-32 shrink-0 text-muted-foreground">{f.label}</span>
                        <span className="h-5 flex-1 rounded bg-muted">
                          <span
                            className={cn('block h-5 rounded', i === d.funnel.length - 1 ? 'bg-success/50' : 'bg-primary/35')}
                            style={{ width: `${Math.max(2, (f.value / top) * 100)}%` }}
                          />
                        </span>
                        <span className="w-10 text-right font-medium tabular-nums">{f.value}</span>
                      </li>
                    ))}
                  </ul>
                </>
              ) : null}
              {d.testRidesByModel.length ? (
                <>
                  <p className="mb-1.5 mt-4 text-[13px] font-semibold text-foreground">{d.rideWord ?? 'Test ride'} → purchase, by model</p>
                  <table className="w-full text-[13px]">
                    <thead className="text-xs text-muted-foreground">
                      <tr>
                        <th className="py-1 text-left font-medium">Model</th>
                        <th className="py-1 text-right font-medium">Done</th>
                        <th className="py-1 text-right font-medium">Bought</th>
                        <th className="py-1 text-right font-medium">Conv.</th>
                      </tr>
                    </thead>
                    <tbody>
                      {d.testRidesByModel.map((m) => (
                        <tr key={m.model} className="border-t">
                          <td className="py-1">{m.model}</td>
                          <td className="py-1 text-right tabular-nums">{m.rides}</td>
                          <td className="py-1 text-right tabular-nums">{m.bought}</td>
                          <td className="py-1 text-right tabular-nums">{m.rides ? Math.round((m.bought * 100) / m.rides) : 0}%</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              ) : null}
              {d.stockByModel.length ? (
                <>
                  <p className="mb-1.5 mt-4 text-[13px] font-semibold text-foreground">Stock by model</p>
                  <table className="w-full text-[13px]">
                    <thead className="text-xs text-muted-foreground">
                      <tr>
                        <th className="py-1 text-left font-medium">Model</th>
                        <th className="py-1 text-right font-medium">Free</th>
                        <th className="py-1 text-right font-medium">Booked</th>
                      </tr>
                    </thead>
                    <tbody>
                      {d.stockByModel.map((m) => (
                        <tr key={m.model} className="border-t">
                          <td className="py-1">
                            {m.model} {m.low ? <span className="ml-1 rounded-full bg-danger-soft px-1.5 text-[11px] font-medium text-danger">low</span> : null}
                          </td>
                          <td className={cn('py-1 text-right tabular-nums', m.low && 'font-semibold text-danger')}>{m.inStock}</td>
                          <td className="py-1 text-right tabular-nums">{m.allocated}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              ) : null}
            </Card>
            <Card className="p-4">
              <p className="mb-2 text-[13px] font-semibold text-foreground">Today in the office</p>
              <ul className="divide-y">
                {d.today.map((r) => {
                  const body = (
                    <>
                      <span className="text-[13px] text-foreground">{r.label}</span>
                      <span className={cn('text-[13px] font-semibold tabular-nums', r.alert ? 'text-danger' : 'text-foreground')}>{r.note ?? value(r)}</span>
                    </>
                  );
                  return (
                    <li key={r.key}>
                      {r.path ? (
                        <Link to={toTenantPath(r.path)} className="flex items-center justify-between gap-3 py-1.5 hover:text-primary">
                          {body}
                        </Link>
                      ) : (
                        <div className="flex items-center justify-between gap-3 py-1.5">{body}</div>
                      )}
                    </li>
                  );
                })}
              </ul>
            </Card>
          </div>
        </>
      )}
    </section>
  );
}

function Tile({ r, suffix }: { r: OfficeRow; suffix?: string }) {
  const inner = (
    <>
      <span className={cn('block text-xl font-semibold tabular-nums', r.alert ? 'text-danger' : 'text-foreground')}>
        {value(r)}
        {suffix}
      </span>
      <span className="block text-xs leading-tight text-muted-foreground">{r.label}</span>
    </>
  );
  const cls = cn('rounded-lg border bg-card px-3 py-2', r.alert && 'border-danger/40 bg-danger-soft/30');
  return r.path ? (
    <Link to={toTenantPath(r.path)} className={cn(cls, 'transition-colors hover:bg-muted/60')}>
      {inner}
    </Link>
  ) : (
    <div className={cls}>{inner}</div>
  );
}
