import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  ArrowLeft,
  ArrowRight,
  Clock,
  Hourglass,
  LogIn,
  Mail,
  MessageSquareText,
  Phone,
  Plus,
  RotateCw,
  Send,
  UserRound,
  Users,
} from "lucide-react";
import { registerApi, type RegisterCheck } from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Button } from "@crm/components/ui/button";
import { Input } from "@crm/components/ui/input";
import { Field } from "@crm/components/ui/field";
import { Alert, Badge } from "@crm/components/ui/card";
import { OtpInput } from "@crm/components/ui/otp-input";
import {
  BusinessUrl,
  CreateBusinessDialog,
} from "@crm/features/business/businesses-page";
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
 * "Create your own CRM" (D-160, D-161). It registers and never signs in: the page makes two
 * calls, send the code and check it. A first business waits for the owner's approval. Someone who already has an account is then shown who they are
 * and their business URLs, to sign in there; anyone may still create a business, linked to
 * them. Signing in happens only at a business's own URL.
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
      // A new number goes straight to the form; an existing account sees the alert first,
      // and someone whose application is in review sees that.
      if (!res.hasAccount && res.canCreate && res.pending.length === 0)
        setCreating(true);
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
    setError(null);
    setExpiresAt(null);
  };

  if (step === "done" && check) {
    const dialog = (
      <CreateBusinessDialog
        open={creating}
        mode={check.hasAccount ? "add" : "first"}
        onOpenChange={setCreating}
        onCreated={(slug) => navigate(`/${slug}/login`)}
        register={{ token: check.registrationToken, person: check.person }}
      />
    );
    if (!check.hasAccount) {
      const waiting = check.pending.length > 0;
      return (
        <div className="space-y-4 animate-pop-in">
          {waiting ? (
            <PendingNotice check={check} />
          ) : (
            <Alert tone="success" title="Number confirmed">
              Set up your business. After we review it, you get its own URL and
              sign in there.
            </Alert>
          )}
          {waiting ? null : check.canCreate ? (
            <Button
              size="lg"
              className="w-full"
              onClick={() => setCreating(true)}
            >
              <Plus /> Set up your business
            </Button>
          ) : (
            <Alert tone="warning">
              Creating a business isn’t open right now. Ask your administrator
              for an invitation.
            </Alert>
          )}
          <BackLink onClick={startOver} />
          {dialog}
        </div>
      );
    }
    return (
      <div className="space-y-4 animate-pop-in">
        <AccountFound check={check} />
        {check.pending.length > 0 ? <PendingNotice check={check} /> : null}
        {check.canCreate ? (
          <Button
            variant="outline"
            className="w-full"
            onClick={() => setCreating(true)}
          >
            <Plus /> Create another business
          </Button>
        ) : (
          <p className="text-xs text-muted-foreground">
            You’ve created {check.created} of {check.limit} businesses this
            account can create.
          </p>
        )}
        <BackLink onClick={startOver} />
        {dialog}
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

/** "You already have a CRM account": who they are, and each business's own URL to sign in at. */
function AccountFound({ check }: { check: RegisterCheck }) {
  const [sending, setSending] = useState(false);
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [sendError, setSendError] = useState<string | null>(null);
  const { person, businesses } = check;
  const first = person.name.split(" ")[0];
  // Sent on their own when the code is checked (D-161): say where, only for real channels.
  const notified = check.notify
    ? sentList(check.notify.email, check.notify.sms, check.notify.whatsapp)
    : "";

  const requestUrls = async () => {
    setSending(true);
    setSendError(null);
    try {
      const res = await registerApi.sendUrls(check.registrationToken);
      setSentTo(
        sentList(
          res.channels.email === "sent" ? res.email : "",
          res.channels.sms === "sent",
          res.channels.whatsapp === "sent",
        ),
      );
    } catch (e) {
      setSendError(
        isApiError(e) ? e.message : "Something went wrong. Please try again.",
      );
    } finally {
      setSending(false);
    }
  };

  return (
    <>
      <Alert
        tone="info"
        title={`${first ? `Hi ${first}, you` : "You"} already have a CRM account`}
      >
        Please sign in at your business URL below. Each business signs its
        people in at its own URL only.
      </Alert>
      <div className="rounded-lg border p-3 text-[13px]">
        <p className="flex items-center gap-2 font-medium text-foreground">
          <UserRound className="size-4 text-muted-foreground" aria-hidden />{" "}
          {person.name || "Your account"}
        </p>
        <p className="mt-1 flex flex-wrap gap-x-4 gap-y-0.5 pl-6 text-muted-foreground">
          {person.email ? <span>{person.email}</span> : null}
          {person.phone ? <span>{person.phone}</span> : null}
        </p>
      </div>
      <ul className="space-y-3">
        {businesses.map((b) => (
          <li key={b.slug} className="rounded-lg border bg-card">
            <div className="flex items-center gap-3 p-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-xs font-semibold text-primary">
                {b.name.slice(0, 2).toUpperCase()}
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-semibold text-foreground">
                  {b.name}
                </p>
                <p className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <Badge tone="primary">{b.role || "Member"}</Badge>
                  <span className="flex items-center gap-1">
                    <Users className="size-3.5" aria-hidden /> {b.members}{" "}
                    {b.members === 1 ? "person" : "people"}
                  </span>
                  {b.city ? <span>{b.city}</span> : null}
                </p>
              </div>
              <Button asChild size="sm">
                <Link to={b.loginPath}>
                  <LogIn /> Sign in
                </Link>
              </Button>
            </div>
            <div className="border-t px-3 py-2">
              <BusinessUrl slug={b.slug} />
            </div>
          </li>
        ))}
      </ul>
      {sentTo ? (
        <Alert tone="success">We sent your business URLs again {sentTo}.</Alert>
      ) : (
        <div>
          {notified ? (
            <p className="mb-1 flex items-start gap-1.5 text-[13px] text-muted-foreground">
              <Send className="mt-0.5 size-3.5 shrink-0" aria-hidden />
              We’re also sending these URLs {notified}.
            </p>
          ) : null}
          <Button
            variant="ghost"
            size="sm"
            loading={sending}
            onClick={() => void requestUrls()}
          >
            <Mail /> Send my URLs again
          </Button>
          {sendError ? (
            <p className="mt-1 text-[13px] text-danger">{sendError}</p>
          ) : null}
        </div>
      )}
    </>
  );
}

/** "to a•••@x.com, by SMS and on WhatsApp" — only the channels that are set up. */
function sentList(email: string, sms: boolean, whatsapp: boolean) {
  const parts = [
    email ? `to ${email}` : "",
    sms ? "by SMS" : "",
    whatsapp ? "on WhatsApp" : "",
  ].filter(Boolean);
  if (parts.length === 0) return "";
  return parts.length === 1
    ? parts[0]
    : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

/** Their business is waiting for the owner's approval (D-161). */
function PendingNotice({ check }: { check: RegisterCheck }) {
  return (
    <Alert tone="info" title="Your application is being reviewed">
      <span className="flex items-start gap-2">
        <Hourglass className="mt-0.5 size-4 shrink-0" aria-hidden />
        <span>
          {check.pending.map((p) => p.name).join(", ")}{" "}
          {check.pending.length === 1 ? "is" : "are"} waiting for approval. We
          will contact you and send the business URL by SMS, WhatsApp and email
          once it’s approved.
        </span>
      </span>
    </Alert>
  );
}
