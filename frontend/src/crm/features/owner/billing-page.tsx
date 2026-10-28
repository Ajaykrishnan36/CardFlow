import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CreditCard } from "lucide-react";
import {
  ownerBillingApi,
  type BillingPeriod,
  type OwnerBilling,
} from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Alert, Badge, Card, CardHeader } from "@crm/components/ui/card";
import { Button } from "@crm/components/ui/button";
import { Field } from "@crm/components/ui/field";
import { Input } from "@crm/components/ui/input";
import { Switch } from "@crm/components/ui/form-controls";
import { Skeleton } from "@crm/components/ui/spinner";
import { PageContainer, PageHeader } from "@crm/components/page";
import { EmptyState, ErrorState } from "@crm/components/states";
import { useDocumentTitle } from "@crm/features/auth/login-pages";
import { relativeTime } from "@crm/lib/utils";
import { rupees } from "@crm/lib/razorpay";

const periodLabel: Record<BillingPeriod, string> = {
  "1m": "1 month",
  "3m": "3 months",
  "6m": "6 months",
  "12m": "12 months",
};

/** /crm/owner/billing — prices per member and every payment (D-164). */
export function OwnerBillingPage() {
  useDocumentTitle("Billing");
  const q = useQuery({
    queryKey: ["platform", "billing"],
    queryFn: ownerBillingApi.get,
  });
  return (
    <PageContainer wide>
      <PageHeader
        title="Billing"
        description="Every person in a customer's business is paid for with Razorpay, for 1, 3, 6 or 12 months. Set the price per person here."
      />
      {q.isError ? (
        <Card>
          <ErrorState
            title="Couldn't load billing"
            message={isApiError(q.error) ? q.error.message : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : !q.data ? (
        <Skeleton className="h-80" />
      ) : (
        <div className="space-y-5">
          <Settings data={q.data} />
          <Payments data={q.data} />
        </div>
      )}
    </PageContainer>
  );
}

function Settings({ data }: { data: OwnerBilling }) {
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(data.enabled);
  const [prices, setPrices] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  useEffect(() => {
    setEnabled(data.enabled);
    setPrices(
      Object.fromEntries(data.prices.map((p) => [p.key, String(p.price)])),
    );
  }, [data]);
  const save = useMutation({
    mutationFn: () =>
      ownerBillingApi.save({
        enabled,
        prices: Object.fromEntries(
          data.prices.map((p) => [p.key, Number(prices[p.key])]),
        ) as Record<BillingPeriod, number>,
      }),
    onSuccess: (next) => {
      qc.setQueryData(["platform", "billing"], next);
      setErrors({});
      toast.success("Billing saved");
    },
    onError: (e) => {
      if (isApiError(e)) setErrors(e.fieldErrors);
      toast.error(isApiError(e) ? e.message : "Could not save.");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Card>
      <CardHeader
        title="Prices and charging"
        actions={
          data.ready ? (
            <Badge tone={data.testMode ? "warning" : "success"}>
              {data.testMode ? "Razorpay test mode" : "Razorpay live"}
            </Badge>
          ) : (
            <Badge tone="danger">Razorpay not set up</Badge>
          )
        }
      />
      <form onSubmit={submit} className="space-y-4 px-4 pb-4">
        {!data.ready ? (
          <Alert tone="warning">
            Set RAZORPAY_KEY_ID and RAZORPAY_KEY_SECRET on the server. Until
            then nobody is charged or locked.
          </Alert>
        ) : !data.webhookReady ? (
          <Alert tone="info">
            Optional: add a Razorpay webhook to
            /api/crm/v1/public/billing/razorpay with RAZORPAY_WEBHOOK_SECRET, so
            a payment still counts if the person closes the page before it
            confirms.
          </Alert>
        ) : null}
        <Switch
          checked={enabled}
          onCheckedChange={setEnabled}
          label="Charge members"
          description="Off: nobody is locked and nothing is asked for. On: people who aren't paid for see the payment screen."
        />
        <div className="grid gap-3 sm:grid-cols-4">
          {data.prices.map((p) => (
            <Field
              key={p.key}
              label={`${periodLabel[p.key]} (₹ per person)`}
              error={errors[`prices.${p.key}`]}
            >
              <Input
                inputMode="numeric"
                value={prices[p.key] ?? ""}
                onChange={(e) =>
                  setPrices({
                    ...prices,
                    [p.key]: e.target.value.replace(/\D/g, ""),
                  })
                }
              />
            </Field>
          ))}
        </div>
        <div className="flex justify-end">
          <Button type="submit" loading={save.isPending}>
            Save
          </Button>
        </div>
      </form>
    </Card>
  );
}

function Payments({ data }: { data: OwnerBilling }) {
  const paid = data.orders.filter((o) => o.status === "paid");
  return (
    <Card className="overflow-hidden">
      <CardHeader
        title="Payments"
        description={`${paid.length} paid · ${rupees(data.collectedPaise / 100)} collected`}
      />
      {data.orders.length === 0 ? (
        <EmptyState
          icon={CreditCard}
          title="No payments yet"
          body="Payments show here as businesses pay for their people."
        />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-[13px]">
            <thead className="border-y bg-muted/40 text-left text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 font-medium">Business</th>
                <th className="px-4 py-2.5 font-medium">Paid by</th>
                <th className="px-4 py-2.5 font-medium">For</th>
                <th className="px-4 py-2.5 text-right font-medium">Amount</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Razorpay</th>
                <th className="px-4 py-2.5 text-right font-medium">When</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {data.orders.map((o) => (
                <tr key={o.id}>
                  <td className="px-4 py-2.5 font-medium">{o.business}</td>
                  <td className="px-4 py-2.5">{o.paidBy || "—"}</td>
                  <td className="px-4 py-2.5">
                    {o.members} {o.members === 1 ? "person" : "people"} ·{" "}
                    {periodLabel[o.period]}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {rupees(o.amountPaise / 100)}
                  </td>
                  <td className="px-4 py-2.5">
                    <Badge
                      tone={
                        o.status === "paid"
                          ? "success"
                          : o.status === "failed"
                            ? "danger"
                            : "neutral"
                      }
                    >
                      {o.status === "paid"
                        ? "Paid"
                        : o.status === "failed"
                          ? "Failed"
                          : "Not completed"}
                    </Badge>
                  </td>
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {o.paymentId || o.orderId}
                  </td>
                  <td className="px-4 py-2.5 text-right text-muted-foreground">
                    {relativeTime(o.paidAt ?? o.createdAt)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}
