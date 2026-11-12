import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CalendarClock } from 'lucide-react';
import { ownerBillingApi, type BillingPeriod } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { WorkspaceMember } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';

const planName = (key: string) => ({ '1m': '1 month', '3m': '3 months', '6m': '6 months', '12m': '12 months' } as Record<string, string>)[key] ?? key;
const monthsOf = (key: string) => ({ '1m': 1, '3m': 3, '6m': 6, '12m': 12 } as Record<string, number>)[key] ?? 0;

const methodName = { cash: 'cash', upi: 'UPI', bank: 'bank transfer', cheque: 'cheque' } as const;

export const fmtDate = (iso: string) => new Date(iso).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });

/** Days left on a paid date: negative once it has passed. */
export const daysLeft = (iso: string) => Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);

/** The new end date: the plan added from today, or from the date they already have if later. */
function nextDate(current: string | undefined, months: number) {
  const now = new Date();
  const from = current && new Date(current) > now ? new Date(current) : now;
  const d = new Date(from);
  d.setMonth(d.getMonth() + months);
  return d.toISOString();
}

/**
 * The owner renews one or more members of a business with a payment taken in hand — cash,
 * UPI, bank transfer or cheque. No Razorpay: the owner records what was received.
 */
export function RenewDialog({
  open,
  onOpenChange,
  workspaceId,
  businessName,
  members,
  onDone
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  workspaceId: string;
  businessName: string;
  members: WorkspaceMember[];
  onDone?: () => void;
}) {
  const qc = useQueryClient();
  const prices = useQuery({ queryKey: ['platform', 'billing'], queryFn: ownerBillingApi.get, enabled: open });
  const [plan, setPlan] = useState<BillingPeriod>('1m');
  const [method, setMethod] = useState<'cash' | 'upi' | 'bank' | 'cheque'>('upi');
  const [amount, setAmount] = useState('');
  const [reference, setReference] = useState('');
  const [error, setError] = useState<string | null>(null);
  const unit = prices.data?.prices.find((p) => p.key === plan)?.price ?? 0;
  const total = unit * members.length;
  useEffect(() => {
    if (open) {
      setError(null);
      setReference('');
    }
  }, [open]);
  useEffect(() => {
    setAmount(total ? String(total) : '');
  }, [total]);

  const renew = useMutation({
    mutationFn: () =>
      ownerBillingApi.renew(workspaceId, {
        plan,
        method,
        amount: Number(amount) || undefined,
        reference: reference.trim() || undefined,
        membershipIds: members.map((m) => m.membershipId)
      }),
    onSuccess: (res) => {
      const until = res.members[0]?.paidUntil;
      toast.success(members.length === 1 ? `${members[0]!.displayName} is renewed` : `${members.length} members renewed`, {
        description: `${planName(plan)} paid by ${methodName[method]}` + (until && members.length === 1 ? ` · active until ${fmtDate(until)}` : '')
      });
      void qc.invalidateQueries({ queryKey: ['workspace', workspaceId] });
      void qc.invalidateQueries({ queryKey: ['platform', 'billing'] });
      onOpenChange(false);
      onDone?.();
    },
    onError: (e) => setError(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : 'Could not renew. Please try again.')
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle className="text-base font-semibold">
          Renew {members.length === 1 ? members[0]!.displayName : `${members.length} members`}
        </DialogTitle>
        <DialogDescription className="text-[13px] text-muted-foreground">
          {businessName} · payment received by you in hand (cash, UPI, bank or cheque). Nobody is asked to pay online.
        </DialogDescription>
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <div className="mt-3 space-y-4">
          <div>
            <p className="mb-1.5 text-[13px] font-medium">Plan (per member)</p>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {(prices.data?.prices ?? []).map((p) => (
                <button
                  key={p.key}
                  type="button"
                  onClick={() => setPlan(p.key)}
                  aria-pressed={plan === p.key}
                  className={cn(
                    'rounded-lg border px-2 py-1.5 text-left text-xs hover:bg-muted/60',
                    plan === p.key && 'border-primary bg-primary-soft/50 ring-1 ring-primary/30'
                  )}
                >
                  <span className="block text-[13px] font-semibold">{planName(p.key)}</span>₹{p.price.toLocaleString('en-IN')}
                </button>
              ))}
            </div>
          </div>
          <div className="rounded-lg border">
            <p className="border-b bg-muted/40 px-3 py-1.5 text-xs font-medium text-muted-foreground">New active-until date</p>
            <ul className="max-h-40 divide-y overflow-y-auto">
              {members.map((m) => (
                <li key={m.membershipId} className="flex items-center justify-between gap-3 px-3 py-1.5 text-[13px]">
                  <span className="min-w-0 truncate">{m.displayName}</span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {m.paidUntil ? fmtDate(m.paidUntil) : 'Not paid'} → <span className="font-medium text-foreground">{fmtDate(nextDate(m.paidUntil, monthsOf(plan)))}</span>
                  </span>
                </li>
              ))}
            </ul>
          </div>
          <div className="grid gap-2 sm:grid-cols-3">
            <Field label="Paid by">
              <Select
                value={method}
                onChange={(e) => setMethod(e.target.value as typeof method)}
                options={[
                  { value: 'upi', label: 'UPI' },
                  { value: 'cash', label: 'Cash' },
                  { value: 'bank', label: 'Bank transfer' },
                  { value: 'cheque', label: 'Cheque' }
                ]}
              />
            </Field>
            <Field label="Amount received (₹)">
              <Input type="number" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
            </Field>
            <Field label="Reference">
              <Input value={reference} onChange={(e) => setReference(e.target.value)} placeholder="UTR / receipt no." />
            </Field>
          </div>
          {members.length > 1 && unit ? (
            <p className="text-xs text-muted-foreground">
              ₹{unit.toLocaleString('en-IN')} × {members.length} members = ₹{total.toLocaleString('en-IN')}
            </p>
          ) : null}
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={renew.isPending} disabled={!members.length} onClick={() => renew.mutate()}>
            <CalendarClock /> Mark paid and renew
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
