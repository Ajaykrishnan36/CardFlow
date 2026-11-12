import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { BadgeCheck } from 'lucide-react';
import { ownerBillingApi, platformApi, type ApproveBody, type BillingPeriod } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';
import { describeNotified } from '@crm/features/workspaces/approve-button';

const planName = (key: string) => ({ '1m': '1 month', '3m': '3 months', '6m': '6 months', '12m': '12 months' } as Record<string, string>)[key] ?? key;

/** Field names the server may point at, as the lead form calls them. */
const fieldNames: Record<string, string> = {
  mobile: 'Mobile',
  organization: 'Business / brand name',
  industry: 'Industry',
  taxId: 'GSTIN, PAN or TAN',
  businessType: 'Business type',
  plan: 'Plan'
};

/**
 * Converting one of the owner's leads is approving it (D-180): pick the plan (required) and
 * how it is paid. The business is made from the lead if it has none yet, opens, its URL is
 * sent, and the lead becomes a contact.
 */
export function LeadApproveDialog({
  open,
  onOpenChange,
  leadId,
  name,
  askedPlan
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  leadId: string;
  name: string;
  askedPlan?: string;
}) {
  const qc = useQueryClient();
  const prices = useQuery({ queryKey: ['platform', 'billing'], queryFn: ownerBillingApi.get, enabled: open });
  const [plan, setPlan] = useState<BillingPeriod | ''>('');
  const [payment, setPayment] = useState<'offline' | 'online'>('offline');
  const [method, setMethod] = useState<NonNullable<ApproveBody['method']>>('upi');
  const [amount, setAmount] = useState('');
  const [reference, setReference] = useState('');
  const [error, setError] = useState<string | null>(null);
  const price = prices.data?.prices.find((p) => p.key === plan)?.price;
  useEffect(() => {
    if (open) {
      setError(null);
      setPlan((askedPlan as BillingPeriod) || '');
    }
  }, [open, askedPlan]);
  useEffect(() => {
    if (price !== undefined) setAmount(String(price));
  }, [price]);

  const approve = useMutation({
    mutationFn: () =>
      platformApi.approveLead(
        leadId,
        payment === 'offline'
          ? { plan: plan as BillingPeriod, payment, method, amount: Number(amount) || undefined, reference: reference.trim() || undefined }
          : { plan: plan as BillingPeriod, payment }
      ),
    onSuccess: (res) => {
      toast.success(`${name} is approved`, {
        description: (payment === 'offline' ? `Paid for ${planName(plan)}. ` : `They pay online for ${planName(plan)}. `) + describeNotified(res.notified)
      });
      onOpenChange(false);
      void qc.invalidateQueries();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        const [k, v] = Object.entries(e.fieldErrors)[0]!;
        setError(fieldNames[k] && !v.includes(fieldNames[k]) ? `${fieldNames[k]}: ${v}` : v);
      } else setError(isApiError(e) ? e.message : 'Could not approve. Please try again.');
    }
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle className="text-base font-semibold">Approve {name}</DialogTitle>
        <DialogDescription className="text-[13px] text-muted-foreground">
          Their business opens with them as Super Admin, its URL is sent to their mobile and email, and this lead becomes a contact.
        </DialogDescription>
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <div className="mt-3 space-y-3">
          <div>
            <p className="mb-1.5 text-[13px] font-medium">
              Plan <span className="text-danger">*</span>
            </p>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {(prices.data?.prices ?? []).map((p) => (
                <button
                  key={p.key}
                  type="button"
                  onClick={() => setPlan(p.key)}
                  aria-pressed={plan === p.key}
                  className={cn('rounded-lg border px-2 py-1.5 text-left text-xs hover:bg-muted/60', plan === p.key && 'border-primary bg-primary-soft/50 ring-1 ring-primary/30')}
                >
                  <span className="block text-[13px] font-semibold">{planName(p.key)}</span>₹{p.price.toLocaleString('en-IN')} per person
                  {askedPlan === p.key ? <span className="block text-[11px] text-primary">asked for</span> : null}
                </button>
              ))}
            </div>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {(['offline', 'online'] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => setPayment(m)}
                aria-pressed={payment === m}
                className={cn('rounded-lg border px-3 py-2 text-left hover:bg-muted/60', payment === m && 'border-primary bg-primary-soft/50 ring-1 ring-primary/30')}
              >
                <span className="block text-[13px] font-semibold">{m === 'offline' ? 'Paid to us in hand' : 'They pay online'}</span>
                <span className="block text-xs text-muted-foreground">
                  {m === 'offline' ? 'Cash, UPI, bank transfer or cheque received. They start at once.' : 'They pay when they first sign in.'}
                </span>
              </button>
            ))}
          </div>
          {payment === 'offline' ? (
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
              <Field label="Amount (₹)">
                <Input type="number" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
              </Field>
              <Field label="Reference">
                <Input value={reference} onChange={(e) => setReference(e.target.value)} placeholder="UTR / receipt no." />
              </Field>
            </div>
          ) : null}
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={approve.isPending} disabled={!plan} onClick={() => approve.mutate()}>
            <BadgeCheck /> {payment === 'offline' ? 'Mark paid and approve' : 'Approve'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
