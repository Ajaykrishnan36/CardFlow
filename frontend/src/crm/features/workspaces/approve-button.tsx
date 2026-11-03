import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { BadgeCheck } from "lucide-react";
import { ownerBillingApi, platformApi, type ApproveBody, type BillingPeriod } from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Alert } from "@crm/components/ui/card";
import { Button } from "@crm/components/ui/button";
import { Field } from "@crm/components/ui/field";
import { Input } from "@crm/components/ui/input";
import { Select } from "@crm/components/ui/form-controls";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@crm/components/ui/menu";
import { cn } from "@crm/lib/utils";

const channelNames = {
  email: "email",
  sms: "SMS",
  whatsapp: "WhatsApp",
} as const;

/** What happened on each channel, honestly: "Sent by email. SMS isn't set up on this server." */
export function describeNotified(
  sent: Record<"email" | "sms" | "whatsapp", string>,
) {
  const by = (status: string) =>
    (Object.keys(channelNames) as Array<keyof typeof channelNames>)
      .filter((k) => sent[k] === status)
      .map((k) => channelNames[k]);
  const parts: string[] = [];
  if (by("sent").length) parts.push(`URL sent by ${by("sent").join(", ")}.`);
  if (by("failed").length) parts.push(`${by("failed").join(", ")} failed.`);
  if (by("not_set_up").length)
    parts.push(`${by("not_set_up").join(", ")} not set up on this server.`);
  if (by("no_address").length)
    parts.push(`No ${by("no_address").join(", ")} address on file.`);
  return parts.join(" ");
}

const planName = (key: string) =>
  ({ "1m": "1 month", "3m": "3 months", "6m": "6 months", "12m": "12 months" } as Record<string, string>)[key] ?? key;

/**
 * Approve a business that waits for the owner (D-161): its URL opens and is sent to the
 * applicant by email, SMS and WhatsApp. On a plan (D-172): paid to the owner outside the CRM —
 * the applicant starts straight away — or paid online by them when they first sign in.
 */
export function ApproveButton({
  workspaceId,
  name,
  size = "sm",
}: {
  workspaceId: string;
  name: string;
  size?: "sm" | "md";
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const apps = useQuery({
    queryKey: ["platform", "applications", "pending"],
    queryFn: () => platformApi.applications({ status: "pending" }),
    enabled: open,
  });
  const prices = useQuery({ queryKey: ["platform", "billing"], queryFn: ownerBillingApi.get, enabled: open });
  const app = apps.data?.find((a) => a.workspaceId === workspaceId);
  const [plan, setPlan] = useState<BillingPeriod | "">("");
  const [payment, setPayment] = useState<"offline" | "online">("offline");
  const [method, setMethod] = useState<NonNullable<ApproveBody["method"]>>("upi");
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [error, setError] = useState<string | null>(null);
  const price = prices.data?.prices.find((p) => p.key === plan)?.price;
  useEffect(() => {
    if (open && app && !plan) setPlan((app.plan as BillingPeriod) || "1m");
  }, [open, app, plan]);
  useEffect(() => {
    if (price !== undefined) setAmount(String(price));
  }, [price]);
  const approve = useMutation({
    mutationFn: () =>
      platformApi.approve(
        workspaceId,
        plan
          ? payment === "offline"
            ? { plan, payment, method, amount: Number(amount) || undefined, reference: reference.trim() || undefined }
            : { plan, payment }
          : {},
      ),
    onSuccess: (res) => {
      toast.success(`${name} is approved`, {
        description:
          (payment === "offline" && plan ? `Paid for ${planName(plan)}. ` : plan ? `They pay online for ${planName(plan)}. ` : "") +
          describeNotified(res.notified),
      });
      setOpen(false);
      void qc.invalidateQueries({ queryKey: ["workspaces"] });
      void qc.invalidateQueries({ queryKey: ["workspace", workspaceId] });
      void qc.invalidateQueries({ queryKey: ["platform"] });
    },
    onError: (e) => setError(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : "Could not approve. Please try again."),
  });
  return (
    <>
      <Button
        size={size}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          setError(null);
          setOpen(true);
        }}
      >
        <BadgeCheck /> Approve
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg" onClick={(e) => e.stopPropagation()}>
          <DialogTitle className="text-base font-semibold">Approve {name}</DialogTitle>
          <DialogDescription className="text-[13px] text-muted-foreground">
            {app
              ? `Asked on ${new Date(app.createdAt).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" })}` +
                (app.plan ? ` · interested in ${planName(app.plan)}` : " · no plan chosen") +
                (app.ownerName ? ` · ${app.ownerName}${app.ownerPhone ? ` (${app.ownerPhone})` : ""}` : "")
              : "Its URL opens and is sent to the applicant."}
          </DialogDescription>
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <div className="mt-3 space-y-3">
            <div>
              <p className="mb-1.5 text-[13px] font-medium">Plan</p>
              <div className="grid grid-cols-4 gap-2">
                {(prices.data?.prices ?? []).map((p) => (
                  <button
                    key={p.key}
                    type="button"
                    onClick={() => setPlan(p.key)}
                    aria-pressed={plan === p.key}
                    className={cn(
                      "rounded-lg border px-2 py-1.5 text-left text-xs hover:bg-muted/60",
                      plan === p.key && "border-primary bg-primary-soft/50 ring-1 ring-primary/30",
                    )}
                  >
                    <span className="block text-[13px] font-semibold">{planName(p.key)}</span>₹{p.price.toLocaleString("en-IN")}
                    {app?.plan === p.key ? <span className="block text-[11px] text-primary">asked for</span> : null}
                  </button>
                ))}
              </div>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {(["offline", "online"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setPayment(m)}
                  aria-pressed={payment === m}
                  className={cn(
                    "rounded-lg border px-3 py-2 text-left hover:bg-muted/60",
                    payment === m && "border-primary bg-primary-soft/50 ring-1 ring-primary/30",
                  )}
                >
                  <span className="block text-[13px] font-semibold">{m === "offline" ? "Paid to us offline" : "They pay online"}</span>
                  <span className="block text-xs text-muted-foreground">
                    {m === "offline" ? "Cash, UPI or bank transfer received. They start at once." : "They pay with Razorpay when they first sign in."}
                  </span>
                </button>
              ))}
            </div>
            {payment === "offline" ? (
              <div className="grid grid-cols-3 gap-2">
                <Field label="How">
                  <Select
                    value={method}
                    onChange={(e) => setMethod(e.target.value as typeof method)}
                    options={[
                      { value: "upi", label: "UPI" },
                      { value: "cash", label: "Cash" },
                      { value: "bank", label: "Bank transfer" },
                      { value: "cheque", label: "Cheque" },
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
            <Button variant="outline" onClick={() => setOpen(false)}>Cancel</Button>
            <Button loading={approve.isPending} disabled={!plan} onClick={() => approve.mutate()}>
              <BadgeCheck /> {payment === "offline" ? "Mark paid and approve" : "Approve"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
