import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Check, Pencil, Send, TrendingUp, X } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { enterpriseApi, type Forecast, type ForecastNumbers, type ForecastRow } from '@crm/api/endpoints';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { formatMoney } from '@crm/lib/money';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';

// Forecasts (D-112): what the business expects to close in a month, quarter or financial
// year, worked out from its opportunities and set against targets. Nothing is stored
// twice: every number here is a sum over the opportunities the viewer may see.

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

function firstError(e: unknown): string {
  return isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : 'Something went wrong. Try again.';
}

export function ForecastPage() {
  useDocumentTitle('Forecasts');
  const { code, context } = useWorkspace();
  const api = enterpriseApi(code);
  const qc = useQueryClient();
  const [sp, setSp] = useSearchParams();
  const periodsQ = useQuery({ queryKey: ['forecast-periods', code], queryFn: () => api.forecastPeriods(), staleTime: 5 * 60_000 });
  const period = sp.get('period') || periodsQ.data?.current.quarter || '';
  const g = sp.get('groupBy');
  const groupBy = g === 'team' || g === 'territory' || g === 'role' ? g : 'owner';
  const forecastQ = useQuery({
    queryKey: ['forecast', code, period, groupBy],
    queryFn: () => api.forecast({ period, groupBy }),
    enabled: Boolean(period),
    placeholderData: (prev) => prev
  });
  const [quotaFor, setQuotaFor] = useState<{ scope: 'company' | 'team' | 'user'; id?: string; name: string; amount: number } | null>(null);
  const [submitOpen, setSubmitOpen] = useState(false);
  const [review, setReview] = useState<ForecastRow | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: ['forecast', code] });
  const fiscal = useMutation({
    mutationFn: (month: number) => api.setFiscalStart(month),
    onSuccess: () => {
      toast.success('Financial year updated');
      void qc.invalidateQueries({ queryKey: ['forecast-periods', code] });
      setSp({}, { replace: true });
      refresh();
    },
    onError: (e) => toast.error(firstError(e))
  });

  const patch = (next: Record<string, string>) => {
    const q = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(next)) q.set(k, v);
    setSp(q, { replace: true });
  };
  const f = forecastQ.data;
  const money = (n: number) => formatMoney(n, f?.currency ?? context.workspace.currency);
  const base = workspaceBase(code);
  const mine = f?.rows.find((r) => r.id === f.me);
  const periods = periodsQ.data?.data ?? [];

  return (
    <PageContainer wide>
      <PageHeader
        title="Forecasts"
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <TrendingUp className="size-5" aria-hidden />
          </span>
        }
        description={
          f
            ? `${f.period.label} · ${f.period.from} to ${f.period.to} · ${f.scope === 'company' ? 'every deal of the business' : 'the deals you can see'}`
            : 'What you expect to close, from your opportunities.'
        }
        actions={
          f && groupBy === 'owner' ? (
            <Button onClick={() => setSubmitOpen(true)}>
              <Send /> {mine?.submission ? 'Update my forecast' : 'Submit my forecast'}
            </Button>
          ) : null
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Select className="w-56" value={period} onChange={(e) => patch({ period: e.target.value })} aria-label="Period">
          {(['quarter', 'month', 'year'] as const).map((kind) => (
            <optgroup key={kind} label={kind === 'quarter' ? 'Quarters' : kind === 'month' ? 'Months' : 'Financial years'}>
              {periods
                .filter((p) => p.kind === kind)
                .map((p) => (
                  <option key={p.key} value={p.key}>
                    {p.label}
                  </option>
                ))}
            </optgroup>
          ))}
        </Select>
        <SegmentedFilter
          value={groupBy}
          onChange={(v) => patch({ groupBy: v })}
          options={[
            { value: 'owner', label: 'By person' },
            { value: 'team', label: 'By team' },
            { value: 'territory', label: 'By territory' },
            { value: 'role', label: 'By role' }
          ]}
        />
        {f?.canManage && periodsQ.data ? (
          <label className="ml-auto flex items-center gap-2 text-[13px] text-muted-foreground">
            Financial year starts in
            <Select className="w-36" value={String(periodsQ.data.fiscalStartMonth)} onChange={(e) => fiscal.mutate(Number(e.target.value))} disabled={fiscal.isPending}>
              {MONTHS.map((m, i) => (
                <option key={m} value={i + 1}>
                  {m}
                </option>
              ))}
            </Select>
          </label>
        ) : null}
      </div>

      {forecastQ.isError ? (
        <ErrorState title="Could not load the forecast" message={firstError(forecastQ.error)} onRetry={() => void forecastQ.refetch()} />
      ) : !f ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-20" />
          ))}
        </div>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Tile
              label="Target"
              value={f.totals.quota ? money(f.totals.quota) : 'Not set'}
              hint={f.totals.quota ? `${f.totals.attainment}% reached` : f.canManage ? 'Set a target to track progress' : undefined}
              action={
                f.canManage && f.scope === 'company' ? (
                  <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => setQuotaFor({ scope: 'company', name: 'the whole business', amount: f.totals.quota })}>
                    {f.totals.quota ? 'Change' : 'Set target'}
                  </button>
                ) : null
              }
            />
            <Tile label="Closed won" value={money(f.totals.closed)} hint={`${f.totals.wonDeals} ${f.totals.wonDeals === 1 ? 'deal' : 'deals'}`} tone="success" />
            <Tile label="Forecast" value={money(f.totals.forecast)} hint="Closed won + commit" tone="primary" />
            <Tile
              label={f.totals.quota ? 'Gap to target' : 'Open pipeline'}
              value={money(f.totals.quota ? f.totals.gap : f.totals.openPipeline)}
              hint={f.totals.quota ? (f.totals.gap > 0 ? `${f.totals.coverage}× pipeline cover` : 'Target covered') : `${f.totals.openDeals} open deals`}
              tone={f.totals.quota && f.totals.gap > 0 ? 'danger' : undefined}
            />
            <Tile label="Commit" value={money(f.totals.commit)} hint="Open deals you are sure of" />
            <Tile label="Best case" value={money(f.totals.bestCase)} hint={`${money(f.totals.bestCaseTotal)} if these close too`} />
            <Tile label="Pipeline" value={money(f.totals.pipeline)} hint="Other open deals" />
            <Tile label="Weighted" value={money(f.totals.weighted)} hint="Open amount × probability" />
          </div>

          <Card className="mt-4 overflow-hidden">
            {f.rows.length === 0 ? (
              <EmptyState
                icon={TrendingUp}
                title={groupBy === 'team' ? 'No teams yet' : groupBy === 'territory' ? 'No territories yet' : 'No deals in this period'}
                body={
                  groupBy === 'team'
                    ? 'Create teams in Settings → Teams to see the forecast rolled up by team.'
                    : 'Opportunities with a close date in this period show here, by the forecast category on each deal.'
                }
                action={
                  <Button asChild variant="outline">
                    <Link to={groupBy === 'team' ? `${base}/settings/teams` : `${base}/opportunities`}>{groupBy === 'team' ? 'Open teams' : 'Open opportunities'}</Link>
                  </Button>
                }
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[860px] text-[13px]">
                  <thead>
                    <tr className="border-b bg-muted/40 text-left text-xs font-medium text-muted-foreground">
                      <th className="px-4 py-2.5">{groupBy === 'team' ? 'Team' : groupBy === 'territory' ? 'Territory' : groupBy === 'role' ? 'Role' : 'Person'}</th>
                      <th className="px-3 py-2.5 text-right">Target</th>
                      <th className="px-3 py-2.5 text-right">Closed won</th>
                      <th className="px-3 py-2.5 text-right">Commit</th>
                      <th className="px-3 py-2.5 text-right">Best case</th>
                      <th className="px-3 py-2.5 text-right">Pipeline</th>
                      <th className="px-3 py-2.5 text-right">Forecast</th>
                      <th className="w-40 px-3 py-2.5">Reached</th>
                      {groupBy === 'owner' ? <th className="px-4 py-2.5">Submitted</th> : null}
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {f.rows.map((r) => (
                      <tr key={r.id || 'none'} className="hover:bg-muted/30">
                        <td className="px-4 py-2.5 font-medium">
                          {r.name}
                          {r.members ? <span className="ml-1.5 text-xs font-normal text-muted-foreground">{r.members} people</span> : null}
                          {r.id === f.me ? <span className="ml-1.5 text-xs font-normal text-muted-foreground">(you)</span> : null}
                        </td>
                        <td className="px-3 py-2.5 text-right tabular-nums">
                          {f.canManage && r.id && (groupBy === 'owner' || groupBy === 'team') ? (
                            <button
                              type="button"
                              className="group inline-flex items-center gap-1 hover:text-primary"
                              onClick={() => setQuotaFor({ scope: groupBy === 'team' ? 'team' : 'user', id: r.id, name: r.name, amount: r.quota })}
                            >
                              {r.quota ? money(r.quota) : <span className="text-muted-foreground">Set</span>}
                              <Pencil className="size-3 opacity-0 group-hover:opacity-100" aria-hidden />
                            </button>
                          ) : r.quota ? (
                            money(r.quota)
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                        <Num n={r.closed} money={money} />
                        <Num n={r.commit} money={money} />
                        <Num n={r.bestCase} money={money} />
                        <Num n={r.pipeline} money={money} />
                        <td className="px-3 py-2.5 text-right font-semibold tabular-nums">{money(r.forecast)}</td>
                        <td className="px-3 py-2.5">
                          {r.quota ? (
                            <div className="flex items-center gap-2">
                              <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                                <div className={cn('h-full rounded-full', r.attainment >= 100 ? 'bg-success' : 'bg-primary')} style={{ width: `${Math.min(100, r.attainment)}%` }} />
                              </div>
                              <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">{Math.round(r.attainment)}%</span>
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">No target</span>
                          )}
                        </td>
                        {groupBy === 'owner' ? (
                          <td className="px-4 py-2.5">
                            {r.submission ? (
                              <span className="flex flex-wrap items-center gap-2">
                                <Badge tone={r.submission.status === 'approved' ? 'success' : r.submission.status === 'rejected' ? 'danger' : 'warning'}>
                                  {r.submission.status === 'submitted' ? 'Waiting' : r.submission.status === 'approved' ? 'Approved' : 'Sent back'}
                                </Badge>
                                <span className="tabular-nums">{money(r.submission.overrideAmount ?? r.submission.forecastAmount)}</span>
                                {f.canManage ? (
                                  <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => setReview(r)}>
                                    Review
                                  </button>
                                ) : null}
                              </span>
                            ) : (
                              <span className="text-xs text-muted-foreground">Not yet</span>
                            )}
                          </td>
                        ) : null}
                      </tr>
                    ))}
                  </tbody>
                  <tfoot>
                    <tr className="border-t bg-muted/40 font-semibold">
                      <td className="px-4 py-2.5">Total</td>
                      <td className="px-3 py-2.5 text-right tabular-nums">{f.totals.quota ? money(f.totals.quota) : '—'}</td>
                      <Num n={f.totals.closed} money={money} />
                      <Num n={f.totals.commit} money={money} />
                      <Num n={f.totals.bestCase} money={money} />
                      <Num n={f.totals.pipeline} money={money} />
                      <td className="px-3 py-2.5 text-right tabular-nums">{money(f.totals.forecast)}</td>
                      <td className="px-3 py-2.5 text-xs font-normal text-muted-foreground" colSpan={groupBy === 'owner' ? 2 : 1}>
                        {groupBy === 'team' ? 'Someone in two teams shows in both; the total counts each deal once.' : groupBy === 'territory' ? 'A territory includes everything below it; the total counts each deal once.' : ''}
                      </td>
                    </tr>
                  </tfoot>
                </table>
              </div>
            )}
          </Card>
          <p className="mt-3 text-xs text-muted-foreground">
            Deals count in the period of their close date. Closed-lost ({money(f.totals.lost)}) and omitted ({money(f.totals.omitted)}) deals are left out. Change a deal's
            forecast category on the opportunity.
          </p>
        </>
      )}

      {quotaFor && f ? (
        <QuotaDialog
          target={quotaFor}
          periodLabel={f.period.label}
          onClose={() => setQuotaFor(null)}
          onSave={(amount) =>
            api.setQuota({
              periodKey: f.period.key,
              scope: quotaFor.scope,
              ownerId: quotaFor.scope === 'user' ? quotaFor.id : undefined,
              teamId: quotaFor.scope === 'team' ? quotaFor.id : undefined,
              amount
            })
          }
          onDone={refresh}
        />
      ) : null}
      {submitOpen && f ? <SubmitDialog forecast={f} mine={mine} money={money} onClose={() => setSubmitOpen(false)} onSave={(body) => api.submitForecast({ periodKey: f.period.key, ...body })} onDone={refresh} /> : null}
      {review && review.submission && f ? (
        <ReviewDialog row={review} money={money} onClose={() => setReview(null)} onSave={(body) => api.reviewForecast(review.submission!.id, body)} onDone={refresh} />
      ) : null}
    </PageContainer>
  );
}

function Num({ n, money }: { n: number; money: (n: number) => string }) {
  return <td className={cn('px-3 py-2.5 text-right tabular-nums', !n && 'text-muted-foreground')}>{n ? money(n) : '—'}</td>;
}

function Tile({ label, value, hint, tone, action }: { label: string; value: string; hint?: string; tone?: 'success' | 'primary' | 'danger'; action?: React.ReactNode }) {
  return (
    <Card className="px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        {action}
      </div>
      <p className={cn('mt-1 text-xl font-semibold tabular-nums tracking-tight', tone === 'success' && 'text-success', tone === 'primary' && 'text-primary', tone === 'danger' && 'text-danger')}>{value}</p>
      {hint ? <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p> : null}
    </Card>
  );
}

function QuotaDialog({
  target,
  periodLabel,
  onClose,
  onSave,
  onDone
}: {
  target: { scope: string; name: string; amount: number };
  periodLabel: string;
  onClose: () => void;
  onSave: (amount: number) => Promise<unknown>;
  onDone: () => void;
}) {
  const [amount, setAmount] = useState(target.amount ? String(target.amount) : '');
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () => onSave(Number(amount) || 0),
    onSuccess: () => {
      toast.success('Target saved');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-sm p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">Target for {target.name}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{periodLabel}. Leave empty or 0 to remove the target.</DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label="Target amount">
            <Input type="number" inputMode="decimal" min="0" step="1" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              Save target
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function SubmitDialog({
  forecast,
  mine,
  money,
  onClose,
  onSave,
  onDone
}: {
  forecast: Forecast;
  mine?: ForecastNumbers & { submission?: ForecastRow['submission'] };
  money: (n: number) => string;
  onClose: () => void;
  onSave: (body: { forecastAmount?: number; comment?: string }) => Promise<unknown>;
  onDone: () => void;
}) {
  const worked = mine?.forecast ?? 0;
  const [amount, setAmount] = useState(String(mine?.submission?.forecastAmount ?? worked));
  const [comment, setComment] = useState(mine?.submission?.comment ?? '');
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () => onSave({ forecastAmount: amount === '' ? undefined : Number(amount), comment: comment.trim() || undefined }),
    onSuccess: () => {
      toast.success('Forecast submitted');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">My forecast for {forecast.period.label}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            From your deals: {money(mine?.closed ?? 0)} closed won + {money(mine?.commit ?? 0)} commit = {money(worked)}. Send that, or the number you stand behind.
          </DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          {mine?.submission?.managerComment ? <Alert tone="info" title="Your manager wrote">{mine.submission.managerComment}</Alert> : null}
          <Field label="I expect to close">
            <Input type="number" inputMode="decimal" min="0" step="1" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />
          </Field>
          <Field label="Comment (optional)">
            <Textarea value={comment} onChange={(e) => setComment(e.target.value)} rows={2} maxLength={1000} placeholder="What this depends on" />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              <Send /> Submit
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ReviewDialog({
  row,
  money,
  onClose,
  onSave,
  onDone
}: {
  row: ForecastRow;
  money: (n: number) => string;
  onClose: () => void;
  onSave: (body: { status: 'approved' | 'rejected'; overrideAmount?: number; comment?: string }) => Promise<unknown>;
  onDone: () => void;
}) {
  const s = row.submission!;
  const [override, setOverride] = useState(s.overrideAmount != null ? String(s.overrideAmount) : '');
  const [comment, setComment] = useState(s.managerComment ?? '');
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (status: 'approved' | 'rejected') => onSave({ status, overrideAmount: status === 'approved' && override !== '' ? Number(override) : undefined, comment: comment.trim() || undefined }),
    onSuccess: (_d, status) => {
      toast.success(status === 'approved' ? 'Forecast approved' : 'Sent back');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">{row.name}'s forecast</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            Submitted {money(s.forecastAmount)}. Their deals today: {money(row.closed)} closed won, {money(row.commit)} commit, {money(row.bestCase)} best case.
          </DialogDescription>
        </div>
        <div className="space-y-3 px-5 py-4">
          {error ? <Alert tone="danger">{error}</Alert> : null}
          {s.comment ? <Alert tone="info" title="Their comment">{s.comment}</Alert> : null}
          <Field label="Your number (optional)" hint="Fill this in to approve with a different amount than they submitted.">
            <Input type="number" inputMode="decimal" min="0" step="1" value={override} onChange={(e) => setOverride(e.target.value)} placeholder={String(s.forecastAmount)} />
          </Field>
          <Field label="Comment (optional)">
            <Textarea value={comment} onChange={(e) => setComment(e.target.value)} rows={2} maxLength={1000} />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="danger-outline" loading={save.isPending && save.variables === 'rejected'} onClick={() => save.mutate('rejected')}>
              <X /> Send back
            </Button>
            <Button type="button" loading={save.isPending && save.variables === 'approved'} onClick={() => save.mutate('approved')}>
              <Check /> Approve
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
