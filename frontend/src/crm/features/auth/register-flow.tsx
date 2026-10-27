import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  ArrowLeft,
  ArrowRight,
  Clock,
  Hourglass,
  MessageSquareText,
  Phone,
  Plus,
  RotateCw,
} from "lucide-react";
import { registerApi, type RegisterCheck } from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Button } from "@crm/components/ui/button";
import { Input } from "@crm/components/ui/input";
import { Field } from "@crm/components/ui/field";
import { Alert } from "@crm/components/ui/card";
import { OtpInput } from "@crm/components/ui/otp-input";
import { CreateBusinessDialog } from "@crm/features/business/businesses-page";
import { cn } from "@crm/lib/utils";
import { useShake } from "./use-shake";

const RESEND_SECONDS = 60;
const mmss = (s: number) =>
  `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;

function useCountdown(until: number | null) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!until) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [until]);
  return until ? Math.max(0, Math.ceil((until - now) / 1000)) : 0;
}

function explain(e: unknown): { message: string; field: boolean } {
  if (!isApiError(e))
    return { message: "Something went wrong. Please try again.", field: false };
  if (e.status === 429) {
    const minutes = Math.max(1, Math.ceil((e.retryAfter ?? 60) / 60));
    return {
      message: `Too many attempts. Try again in ${minutes} minute${minutes === 1 ? "" : "s"}.`,
      field: false,
    };
  }
  if (e.fieldErrors.phone) return { message: e.fieldErrors.phone, field: true };
  return { message: e.fieldErrors.code ?? e.message, field: false };
}

/**
 * "Create your own CRM" (D-160, D-161). It registers and never signs in: send the code, check
 * it, send the business form. Everyone then sees "we will review"; the owner approves new
 * businesses, and someone who already has a CRM is messaged their URLs instead. Signing in
 * happens only at a business's own URL.
 */
export function RegisterFlow() {
  const navigate = useNavigate();
  const [phone, setPhone] = useState("");
  const [step, setStep] = useState<"phone" | "code" | "done">("phone");
  const [code, setCode] = useState("");
  const [devCode, setDevCode] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [phoneShakeRef, shakePhone] = useShake();
  const [codeShakeRef, shakeCode] = useShake();
  const [resendAt, setResendAt] = useState<number | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const resendIn = useCountdown(resendAt);
  const expiresIn = useCountdown(expiresAt);
  const expired = expiresAt !== null && expiresIn === 0;
  const [check, setCheck] = useState<RegisterCheck | null>(null);
  const [creating, setCreating] = useState(false);
  const [reviewed, setReviewed] = useState(false);

  const fail = (message: string, onField: boolean) => {
    setFieldError(onField ? message : null);
    setError(onField ? null : message);
    requestAnimationFrame(onField ? shakePhone : shakeCode);
  };

  const send = async () => {
    setError(null);
    setFieldError(null);
    const value = phone.trim();
    if (value.replace(/\D/g, "").length < 10) {
      fail("Enter your 10-digit mobile number.", true);
      return;
    }
    setBusy(true);
    try {
      const res = await registerApi.request(value);
      setDevCode(res.devCode);
      setStep("code");
      setCode("");
      setResendAt(Date.now() + RESEND_SECONDS * 1000);
      setExpiresAt(Date.now() + res.expiresIn * 1000);
      toast.success("Code sent");
    } catch (e) {
      const { message, field } = explain(e);
      if (step === "code" && field) setStep("phone");
      fail(message, field);
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value = code) => {
    setError(null);
    if (value.length !== 6) {
      fail("Enter all 6 digits.", false);
      return;
    }
    setBusy(true);
    try {
      const res = await registerApi.verify({
        phone: phone.trim(),
        code: value,
      });
      setCheck(res);
      setStep("done");
      if (res.canCreate) setCreating(true);
    } catch (e) {
      fail(explain(e).message, false);
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const startOver = () => {
    setStep("phone");
    setCheck(null);
    setCreating(false);
    setReviewed(false);
    setError(null);
    setExpiresAt(null);
  };

  if (step === "done" && check) {
    // Everyone gets the same steps after the code, new or not (D-161): the business form,
    // then "we will review". Whether the number already has a CRM is checked on the
    // server, which then messages them instead; nothing about anyone is shown here.
    if (reviewed) {
      return (
        <div className="space-y-4 animate-pop-in">
          <Alert tone="success" title="Thank you — we’ve received your details">
            <span className="flex items-start gap-2">
              <Hourglass className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span>
                We will review your application and contact you. Your business
                URL comes by SMS, WhatsApp and email once it’s approved.
              </span>
            </span>
          </Alert>
          <BackLink onClick={startOver} />
        </div>
      );
    }
    return (
      <div className="space-y-4 animate-pop-in">
        <Alert tone="success" title="Number confirmed">
          Tell us about your business. We review it and send you its own URL.
        </Alert>
        {check.canCreate ? (
          <Button
            size="lg"
            className="w-full"
            onClick={() => setCreating(true)}
          >
            <Plus /> Set up your business
          </Button>
        ) : (
          <Alert tone="warning">
            Creating a business isn’t open right now. Please try again later.
          </Alert>
        )}
        <BackLink onClick={startOver} />
        <CreateBusinessDialog
          open={creating}
          mode="first"
          onOpenChange={setCreating}
          onCreated={(slug) => navigate(`/${slug}/login`)}
          onReviewed={() => setReviewed(true)}
          register={{
            token: check.registrationToken,
            person: { name: "", email: "", phone: phone.trim() },
          }}
        />
      </div>
    );
  }

  if (step === "phone") {
    return (
      <form
        key="phone"
        noValidate
        className="space-y-5 animate-pop-in"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <div ref={phoneShakeRef}>
          <Field
            label="Mobile number"
            error={fieldError ?? undefined}
            hint="We’ll text you a 6-digit code to confirm the number."
          >
            <Input
              value={phone}
              onChange={(e) => {
                setPhone(e.target.value);
                if (fieldError) setFieldError(null);
              }}
              inputSize="lg"
              type="tel"
              inputMode="tel"
              autoComplete="tel"
              autoFocus
              placeholder="98765 43210"
              leading={<Phone />}
            />
          </Field>
        </div>
        <Button type="submit" size="lg" className="group w-full" loading={busy}>
          {busy ? "Sending…" : "Text me a code"}
          {!busy ? (
            <ArrowRight className="transition-transform group-hover:translate-x-0.5" />
          ) : null}
        </Button>
      </form>
    );
  }

  return (
    <form
      key="code"
      noValidate
      className="space-y-5 animate-pop-in"
      onSubmit={(e) => {
        e.preventDefault();
        void verify();
      }}
    >
      <Alert tone="success" title="Check your messages">
        <span className="flex items-start gap-2">
          <MessageSquareText
            className="mt-0.5 size-4 shrink-0 text-success"
            aria-hidden
          />
          We sent a 6-digit code to {phone.trim()}.
        </span>
      </Alert>
      {devCode ? (
        <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">
          No SMS provider is set up on this server — your code is {devCode}
        </p>
      ) : null}
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div ref={codeShakeRef}>
        <OtpInput
          label="Code"
          value={code}
          onChange={setCode}
          onComplete={(v) => void verify(v)}
          invalid={Boolean(error)}
          disabled={busy || expired}
          autoFocus
        />
      </div>
      {expiresAt ? (
        <p
          className={cn(
            "flex items-center gap-1.5 text-[13px]",
            expired ? "text-danger" : "text-muted-foreground",
          )}
          aria-live="polite"
        >
          <Clock className="size-3.5" aria-hidden />
          {expired
            ? "This code has expired. Send a new one."
            : `Code expires in ${mmss(expiresIn)}`}
        </p>
      ) : null}
      <Button
        type="submit"
        size="lg"
        className="w-full"
        loading={busy}
        disabled={expired}
      >
        {busy ? "Checking…" : "Continue"}
      </Button>
      <div className="flex items-center justify-between text-[13px]">
        <button
          type="button"
          className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
          onClick={startOver}
        >
          <ArrowLeft className="size-3.5" aria-hidden /> Use a different number
        </button>
        <button
          type="button"
          className="flex items-center gap-1.5 font-medium text-primary hover:underline disabled:cursor-not-allowed disabled:text-muted-foreground disabled:no-underline"
          disabled={resendIn > 0 || busy}
          onClick={() => void send()}
        >
          <RotateCw
            className={cn("size-3.5", busy && "animate-spin")}
            aria-hidden
          />
          {resendIn > 0 ? `Resend in ${mmss(resendIn)}` : "Resend code"}
        </button>
      </div>
    </form>
  );
}

function BackLink({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      className="flex items-center gap-1 text-[13px] text-muted-foreground hover:text-foreground"
      onClick={onClick}
    >
      <ArrowLeft className="size-3.5" aria-hidden /> Use a different number
    </button>
  );
}
