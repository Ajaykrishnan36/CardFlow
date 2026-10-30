import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CreditCard, Lock, LogOut, ShieldCheck } from "lucide-react";
import {
  billingApi,
  type BillingMember,
  type BillingPeriod,
} from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { useSignOut } from "@crm/auth/session";
import { Alert, Badge, Card } from "@crm/components/ui/card";
import { Button } from "@crm/components/ui/button";
import { Checkbox } from "@crm/components/ui/form-controls";
import { Skeleton } from "@crm/components/ui/spinner";
import { PageContainer, PageHeader } from "@crm/components/page";
import { ErrorState } from "@crm/components/states";
import { useDocumentTitle } from "@crm/features/auth/login-pages";
import { cn } from "@crm/lib/utils";
import { PaymentCancelled, payWithRazorpay, rupees } from "@crm/lib/razorpay";
import {
  useWorkspace,
  workspaceContextKey,
} from "@crm/features/workspace/workspace-context";

// Paying for members with Razorpay (D-164). One panel serves the Billing page, the lock
// screen of someone not paid for, and "pay for the person you just added".

const billingKey = (code: string) => ["workspace", code, "billing"] as const;

function formatDate(iso?: string) {
  return iso
    ? new Date(iso).toLocaleDateString("en-IN", {
        day: "numeric",
        month: "short",
        year: "numeric",
      })
    : "";
}

/** Where a member's paid time stands: Not paid, Expired, "N days to expire" (a week or
 * less left) or Paid until a date. */
export function paidState(paidUntil?: string): {
  tone: "danger" | "warning" | "success";
  label: string;
  days?: number;
} {
  if (!paidUntil) return { tone: "danger", label: "Not paid" };
  const days = Math.ceil((new Date(paidUntil).getTime() - Date.now()) / 86_400_000);
  if (days <= 0) return { tone: "danger", label: `Expired ${formatDate(paidUntil)}`, days };
  if (days <= 7)
    return {
      tone: days <= 1 ? "danger" : "warning",
      label: days === 1 ? "1 day to expire" : `${days} days to expire`,
      days,
    };
  return { tone: "success", label: `Paid until ${formatDate(paidUntil)}`, days };
}

/** The paid-status badge; with `to` it is a link to the Billing page. */
export function PaidBadge({ paidUntil, to }: { paidUntil?: string; to?: string }) {
  const st = paidState(paidUntil);
  const badge = <Badge tone={st.tone}>{st.label}</Badge>;
  if (!to) return badge;
  return (
    <Link
      to={to}
      onClick={(e) => e.stopPropagation()}
      title={st.days !== undefined && st.days > 7 ? undefined : "Open Billing to pay"}
      className="inline-flex rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring hover:opacity-80"
    >
      {badge}
    </Link>
  );
}

function MemberStatus({ m }: { m: BillingMember }) {
  return <PaidBadge paidUntil={m.paidUntil} />;
}

/** The Billing overview of a business, shared by every screen that shows paid status. */
export function useBillingOverview(code: string, enabled = true) {
  return useQuery({
    queryKey: billingKey(code),
    queryFn: () => billingApi(code).overview(),
    enabled,
    staleTime: 60_000,
  });
}

const periodLabel: Record<BillingPeriod, string> = {
  "1m": "1 month",
  "3m": "3 months",
  "6m": "6 months",
  "12m": "12 months",
};

export function BillingPanel({
  code,
  preselect,
  onPaid,
}: {
  code: string;
  preselect?: string[];
  onPaid?: () => void;
}) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: billingKey(code),
    queryFn: () => billingApi(code).overview(),
  });
  const [period, setPeriod] = useState<BillingPeriod>("1m");
  const [picked, setPicked] = useState<Set<string> | null>(null);

  // Start with: the people asked for; else everyone you can pay for who isn't paid up; else yourself.
  useEffect(() => {
    if (!q.data || picked) return;
    const payable = q.data.members.filter((m) => m.canPay);
    const start = preselect?.length
      ? payable.filter((m) => preselect.includes(m.membershipId))
      : payable.filter((m) => m.locked).length
        ? payable.filter((m) => m.locked)
        : payable.filter((m) => m.me);
    setPicked(new Set(start.map((m) => m.membershipId)));
  }, [q.data, picked, preselect]);

  const price = q.data?.prices.find((p) => p.key === period)?.price ?? 0;
  const count = picked?.size ?? 0;
  const total = price * count;

  const pay = useMutation({
    mutationFn: async () => {
      const order = await billingApi(code).order({
        period,
        membershipIds: [...(picked ?? [])],
      });
      const res = await payWithRazorpay(order);
      return billingApi(code).verify(res);
    },
    onSuccess: async (res) => {
      toast.success(
        `Paid for ${res.members} ${res.members === 1 ? "person" : "people"}`,
        { description: "Their access is open now." },
      );
      // Pick again only from the fresh list: the old one still shows them unpaid.
      await qc.invalidateQueries({ queryKey: billingKey(code) });
      setPicked(null);
      void qc.invalidateQueries({ queryKey: workspaceContextKey(code) });
      void qc.invalidateQueries({ queryKey: ["workspace", code] });
      onPaid?.();
    },
    onError: (e) => {
      if (e instanceof PaymentCancelled) return;
      toast.error(
        isApiError(e)
          ? e.message
          : e instanceof Error
            ? e.message
            : "The payment did not go through.",
      );
    },
  });

  const members = q.data?.members ?? [];
  const payable = useMemo(() => members.filter((m) => m.canPay), [members]);

  if (q.isError)
    return (
      <ErrorState
        title="Couldn't load billing"
        message={isApiError(q.error) ? q.error.message : undefined}
        onRetry={() => void q.refetch()}
      />
    );
  if (!q.data || !picked) return <Skeleton className="h-64 w-full" />;
  if (q.data.exempt)
    return <Alert tone="info">This business is never charged.</Alert>;
  if (!q.data.keyId)
    return (
      <Alert tone="warning">
        Payments aren't set up on this server yet. Ask the platform owner.
      </Alert>
    );

  const toggle = (id: string, on: boolean) => {
    const next = new Set(picked);
    if (on) next.add(id);
    else next.delete(id);
    setPicked(next);
  };
  const allPicked =
    payable.length > 0 && payable.every((m) => picked.has(m.membershipId));

  return (
    <div className="space-y-4">
      <div>
        <p className="mb-2 text-[13px] font-medium">How long</p>
        <div
          role="radiogroup"
          className="grid grid-cols-2 gap-2 sm:grid-cols-4"
        >
          {q.data.prices.map((p) => (
            <button
              key={p.key}
              type="button"
              role="radio"
              aria-checked={period === p.key}
              onClick={() => setPeriod(p.key)}
              className={cn(
                "rounded-lg border px-3 py-2.5 text-left transition-colors",
                period === p.key
                  ? "border-primary bg-primary-soft ring-2 ring-primary/20"
                  : "hover:bg-muted",
              )}
            >
              <p className="text-[13px] font-semibold">{periodLabel[p.key]}</p>
              <p className="text-xs text-muted-foreground">
                {rupees(p.price)} per person
              </p>
            </button>
          ))}
        </div>
      </div>

      <Card className="overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
          <p className="text-[13px] font-medium">Who to pay for</p>
          {payable.length > 1 ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={() =>
                setPicked(
                  allPicked
                    ? new Set()
                    : new Set(payable.map((m) => m.membershipId)),
                )
              }
            >
              {allPicked ? "Clear" : `Select all ${payable.length}`}
            </Button>
          ) : null}
        </div>
        <ul className="max-h-80 divide-y overflow-y-auto">
          {members.map((m) => (
            <li
              key={m.membershipId}
              className={cn(
                "flex items-center gap-3 px-4 py-2.5",
                !m.canPay && "opacity-60",
              )}
            >
              <Checkbox
                checked={picked.has(m.membershipId)}
                onCheckedChange={(on) => toggle(m.membershipId, on)}
                disabled={!m.canPay}
                aria-label={`Pay for ${m.name}`}
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-[13px] font-medium">
                  {m.name}
                  {m.me ? (
                    <span className="font-normal text-muted-foreground">
                      {" "}
                      (you)
                    </span>
                  ) : null}
                </p>
                <p className="truncate text-xs text-muted-foreground">
                  {[m.roleName, m.phone].filter(Boolean).join(" · ")}
                  {!m.canPay ? " · paid by someone above you" : ""}
                </p>
              </div>
              <MemberStatus m={m} />
            </li>
          ))}
        </ul>
      </Card>

      <div className="flex flex-col gap-3 rounded-lg border bg-muted/40 p-4 sm:flex-row sm:items-center">
        <div className="min-w-0 flex-1">
          <p className="text-[13px] text-muted-foreground">
            {count} {count === 1 ? "person" : "people"} × {rupees(price)} for{" "}
            {periodLabel[period]}
          </p>
          <p className="text-xl font-semibold tabular-nums">{rupees(total)}</p>
          <p className="text-xs text-muted-foreground">
            Paid time is added to what each person already has.
          </p>
        </div>
        <Button
          size="md"
          loading={pay.isPending}
          disabled={count === 0}
          onClick={() => pay.mutate()}
        >
          <CreditCard /> Pay {rupees(total)} with Razorpay
        </Button>
      </div>
    </div>
  );
}

/** /…/settings/billing */
export function BillingPage() {
  useDocumentTitle("Billing");
  const { code } = useWorkspace();
  return (
    <PageContainer>
      <PageHeader
        title="Billing"
        description="Every person in this business is paid for, for 1, 3, 6 or 12 months. You can pay for yourself and for the people below your role — several at once."
      />
      <BillingPanel code={code} />
    </PageContainer>
  );
}

/** Shown over the blurred business to someone not paid for (or whose time ran out). */
export function BillingLock() {
  const { code, context } = useWorkspace();
  const signOut = useSignOut();
  const ended = Boolean(context.billing?.paidUntil);
  return (
    <div className="relative min-h-[80vh]">
      <div aria-hidden className="pointer-events-none select-none p-6 blur-md">
        <div className="grid gap-4 md:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-28 rounded-xl border bg-card" />
          ))}
        </div>
        <div className="mt-4 h-72 rounded-xl border bg-card" />
      </div>
      <div className="fixed inset-0 z-50 overflow-y-auto bg-background/60 backdrop-blur-md">
        <div className="mx-auto flex min-h-full max-w-2xl flex-col justify-center px-4 py-10">
          <Card className="p-5 shadow-card sm:p-6">
            <div className="mb-4 flex items-start gap-3">
              <span className="grid size-10 shrink-0 place-items-center rounded-full bg-warning-soft text-warning">
                <Lock className="size-5" aria-hidden />
              </span>
              <div>
                <h1 className="text-lg font-semibold tracking-tight">
                  {ended
                    ? `Your access to ${context.workspace.name} has ended`
                    : `Pay to open ${context.workspace.name}`}
                </h1>
                <p className="mt-0.5 text-[13px] text-muted-foreground">
                  {ended
                    ? `It was paid until ${formatDate(context.billing?.paidUntil)}. Pay again to carry on where you left off — nothing is lost.`
                    : "Each person in this business is paid for. Choose how long, and who to pay for."}
                </p>
              </div>
            </div>
            <BillingPanel code={code} />
            <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t pt-4">
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <ShieldCheck className="size-3.5" aria-hidden /> Payments by
                Razorpay. Someone above you can also pay for you.
              </p>
              <div className="flex gap-2">
                <Button asChild variant="ghost" size="sm">
                  <Link to="/crm/me">Profile</Link>
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void signOut()}
                >
                  <LogOut /> Sign out
                </Button>
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}

/** After adding someone: pay for them now (they stay locked until someone does). */
export function PayForNewMember({
  code,
  membershipId,
  name,
}: {
  code: string;
  membershipId?: string;
  name: string;
}) {
  const { context } = useWorkspace();
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState(false);
  if (!membershipId || !context.billing?.enforced) return null;
  return (
    <div className="mx-5 mb-4 rounded-lg border p-3 text-left">
      {done ? (
        <p className="text-[13px] font-medium text-success">
          Paid — {name} can sign in and work now.
        </p>
      ) : open ? (
        <BillingPanel
          code={code}
          preselect={[membershipId]}
          onPaid={() => setDone(true)}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <div className="min-w-0 flex-1">
            <p className="text-[13px] font-medium">Pay for {name}</p>
            <p className="text-xs text-muted-foreground">
              They can't use the business until someone pays for them. You can
              also do it later under Billing.
            </p>
          </div>
          <Button size="sm" onClick={() => setOpen(true)}>
            <CreditCard /> Pay now
          </Button>
        </div>
      )}
    </div>
  );
}
