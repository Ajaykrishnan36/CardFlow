import { useState } from "react";
import { ArrowLeft, Check, Phone } from "lucide-react";
import { registerApi } from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Button } from "@crm/components/ui/button";
import { Input } from "@crm/components/ui/input";
import { Field } from "@crm/components/ui/field";
import { Alert } from "@crm/components/ui/card";
import { OtpInput } from "@crm/components/ui/otp-input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@crm/components/ui/menu";

function message(e: unknown) {
  if (!isApiError(e)) return "Something went wrong. Please try again.";
  if (e.status === 429) return "Too many attempts. Try again in a few minutes.";
  return e.fieldErrors.phone ?? e.fieldErrors.code ?? e.message;
}

/**
 * "Forgot my CRM URL" (D-161): mobile number, code, then the URLs go out by SMS, WhatsApp
 * and email and the owner is told. The popup says the same whether or not the number has a
 * CRM, so it never reveals who has an account.
 */
export function RequestUrlDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [step, setStep] = useState<"phone" | "code" | "sent">("phone");
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [devCode, setDevCode] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setStep("phone");
    setCode("");
    setDevCode(undefined);
    setError(null);
  };
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) setTimeout(reset, 200);
  };

  const send = async () => {
    setError(null);
    if (phone.replace(/\D/g, "").length < 10) {
      setError("Enter your 10-digit mobile number.");
      return;
    }
    setBusy(true);
    try {
      const res = await registerApi.request(phone.trim());
      setDevCode(res.devCode);
      setCode("");
      setStep("code");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value = code) => {
    setError(null);
    if (value.length !== 6) {
      setError("Enter all 6 digits.");
      return;
    }
    setBusy(true);
    try {
      const check = await registerApi.verify({
        phone: phone.trim(),
        code: value,
      });
      await registerApi.requestUrl(check.registrationToken);
      setStep("sent");
    } catch (e) {
      setError(message(e));
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-md p-6">
        {step === "sent" ? (
          <div className="animate-pop-in">
            <span className="grid size-11 place-items-center rounded-full bg-success/15 text-success">
              <Check className="size-5" aria-hidden />
            </span>
            <DialogTitle className="mt-3 text-lg font-semibold">
              We’ve sent your CRM URL
            </DialogTitle>
            <DialogDescription className="mt-1 text-sm text-muted-foreground">
              If {phone.trim()} has a CRM account, its business URLs are on
              their way by SMS, WhatsApp and email. Sign in there with this
              mobile number.
            </DialogDescription>
            <div className="mt-6 flex justify-end">
              <Button onClick={() => close(false)}>Close</Button>
            </div>
          </div>
        ) : (
          <form
            noValidate
            onSubmit={(e) => {
              e.preventDefault();
              void (step === "phone" ? send() : verify());
            }}
          >
            <DialogTitle className="text-lg font-semibold">
              Forgot your CRM URL?
            </DialogTitle>
            <DialogDescription className="mt-1 text-sm text-muted-foreground">
              {step === "phone"
                ? "Enter your mobile number. After the code we send your business URLs to you."
                : `Enter the 6-digit code we sent to ${phone.trim()}.`}
            </DialogDescription>
            <div className="mt-5 space-y-4">
              {error ? <Alert tone="danger">{error}</Alert> : null}
              {step === "phone" ? (
                <Field label="Mobile number">
                  <Input
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                    type="tel"
                    inputMode="tel"
                    autoComplete="tel"
                    autoFocus
                    placeholder="98765 43210"
                    leading={<Phone />}
                  />
                </Field>
              ) : (
                <>
                  {devCode ? (
                    <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">
                      No SMS provider is set up on this server — your code is{" "}
                      {devCode}
                    </p>
                  ) : null}
                  <OtpInput
                    label="Code"
                    value={code}
                    onChange={setCode}
                    onComplete={(v) => void verify(v)}
                    invalid={Boolean(error)}
                    disabled={busy}
                    autoFocus
                  />
                </>
              )}
              <Button type="submit" className="w-full" loading={busy}>
                {step === "phone" ? "Text me a code" : "Send my URL"}
              </Button>
              {step === "code" ? (
                <button
                  type="button"
                  className="flex items-center gap-1 text-[13px] text-muted-foreground hover:text-foreground"
                  onClick={reset}
                >
                  <ArrowLeft className="size-3.5" aria-hidden /> Use a different
                  number
                </button>
              ) : null}
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
