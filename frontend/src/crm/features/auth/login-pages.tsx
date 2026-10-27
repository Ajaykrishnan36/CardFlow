import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import {
  ArrowRight,
  Boxes,
  Crown,
  ScanLine,
  ShieldCheck,
  Users,
} from "lucide-react";
import { LogoMark } from "@crm/components/brand";
import { AuthHeading, AuthLayout } from "./auth-layout";
import { LoginForm } from "./login-form";
import { RegisterFlow } from "./register-flow";
import { RequestUrlDialog } from "./request-url-dialog";
import { normalizeCode, setSignInTenant } from "@crm/lib/tenant";
import { Button } from "@crm/components/ui/button";
import { Input } from "@crm/components/ui/input";

export function useDocumentTitle(title: string) {
  const { t } = useTranslation();
  useEffect(() => {
    document.title = `${title} · ${t("brand.name")}`;
  }, [title, t]);
}

/**
 * "/" — the site's own page (D-158, D-160): "Create your own CRM". It only registers: no
 * one signs in here, not staff, a Super Admin or the platform owner. It makes no call until
 * the person sends their number (send code, check code). Each business signs its people in
 * at its own URL; this page also opens that URL from a code.
 */
export function CreateYourCrmPage() {
  const { t } = useTranslation();
  setSignInTenant("");
  useDocumentTitle("Create your CRM");
  return (
    <AuthLayout
      footer={
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="size-3.5" aria-hidden />
          Every business gets its own private URL. Only the people it adds can
          sign in there.
        </span>
      }
    >
      <AuthHeading
        title="Create your own CRM"
        subtitle="Enter your mobile number and tell us about your business. We review it and send you its own URL."
      />
      <RegisterFlow />
      <OpenBusinessUrl />
      {/* On a phone the brand panel is hidden: say what this is under the form. */}
      <ul className="mt-8 space-y-3.5 lg:hidden">
        {[
          {
            icon: Boxes,
            title: t("auth.brand.f1Title"),
            body: t("auth.brand.f1Body"),
          },
          {
            icon: ScanLine,
            title: t("auth.brand.f2Title"),
            body: t("auth.brand.f2Body"),
          },
          {
            icon: Users,
            title: t("auth.brand.f3Title"),
            body: t("auth.brand.f3Body"),
          },
        ].map(({ icon: Icon, title, body }) => (
          <li key={title} className="flex gap-3">
            <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
              <Icon className="size-4" aria-hidden />
            </span>
            <div className="min-w-0">
              <p className="text-[13px] font-semibold text-foreground">
                {title}
              </p>
              <p className="mt-0.5 text-[13px] leading-relaxed text-muted-foreground">
                {body}
              </p>
            </div>
          </li>
        ))}
      </ul>
    </AuthLayout>
  );
}

/** "Already have a business?" — paste its URL or type its code to open its sign-in page. */
function OpenBusinessUrl() {
  const navigate = useNavigate();
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [requesting, setRequesting] = useState(false);
  const go = (e: FormEvent) => {
    e.preventDefault();
    let raw = value.trim();
    // A pasted URL: take the first part of its path (https://site/<code>/login → <code>).
    const m = /^(?:https?:\/\/)?[^/]+\/([^/?#]+)/i.exec(raw);
    if (m && /[./]/.test(raw)) raw = decodeURIComponent(m[1]);
    const code = normalizeCode(raw);
    if (!/^[0-9a-z-]{3,40}$/.test(code)) {
      setError(
        "Enter your business URL or its code, like 271026-44-234F-000501.",
      );
      return;
    }
    navigate(`/${code}/login`);
  };
  return (
    <>
      <form onSubmit={go} noValidate className="mt-6 rounded-lg border p-4">
        <p className="text-sm font-semibold text-foreground">
          Already have a business?
        </p>
        <p className="mt-0.5 text-[13px] text-muted-foreground">
          Sign in at its own URL. Paste it, or type the business code.
        </p>
        <div className="mt-3 flex gap-2">
          <Input
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              setError("");
            }}
            placeholder="271026-44-234F-000501"
            aria-label="Business URL or code"
            className="font-mono"
            autoCapitalize="characters"
            spellCheck={false}
          />
          <Button type="submit" variant="outline">
            Go <ArrowRight />
          </Button>
        </div>
        {error ? (
          <p className="mt-1.5 text-[13px] text-danger">{error}</p>
        ) : null}
        <p className="mt-2 text-xs text-muted-foreground">
          Forgot your CRM URL?{" "}
          <button
            type="button"
            className="font-medium text-primary hover:underline"
            onClick={() => setRequesting(true)}
          >
            Click here to request it
          </button>
          , or ask your Super Admin.
        </p>
      </form>
      {/* Outside the form: its own form's submit must not reach this one. */}
      <RequestUrlDialog open={requesting} onOpenChange={setRequesting} />
    </>
  );
}

/** /crm/owner/login — dedicated owner console (PRD §10.3): centred card, dark by design. */
export function OwnerLoginPage() {
  const { t } = useTranslation();
  setSignInTenant("");
  useDocumentTitle(t("auth.owner.title"));
  return (
    <div className="dark flex min-h-full flex-col">
      <div className="relative flex flex-1 flex-col overflow-hidden bg-[hsl(228_30%_5%)] text-foreground">
        <div
          className="pointer-events-none absolute left-1/2 top-[-18rem] size-[46rem] -translate-x-1/2 rounded-full bg-indigo-600/25 blur-3xl"
          aria-hidden
        />
        <div
          className="crm-grid-bg pointer-events-none absolute inset-0 opacity-60"
          aria-hidden
        />

        <main className="relative flex flex-1 items-center justify-center px-5 py-12">
          <div className="w-full max-w-[420px] animate-slide-up">
            <div className="mb-8 flex flex-col items-center text-center">
              <div className="relative">
                <LogoMark className="size-12 shadow-glow" />
                <span className="absolute -bottom-1.5 -right-1.5 grid size-6 place-items-center rounded-full border-2 border-[hsl(228_30%_5%)] bg-amber-400 text-amber-950">
                  <Crown className="size-3" aria-hidden />
                </span>
              </div>
              <p className="mt-5 text-xs font-semibold uppercase tracking-[0.2em] text-indigo-300">
                {t("brand.name")}
              </p>
              <h1 className="mt-2 text-[26px] font-semibold tracking-tight">
                {t("auth.owner.title")}
              </h1>
              <p className="mt-2 text-sm text-muted-foreground">
                {t("auth.owner.subtitle")}
              </p>
            </div>

            <div className="rounded-2xl border bg-card/80 p-7 shadow-2xl backdrop-blur-xl sm:p-8">
              <LoginForm audience="owner" />
            </div>

            <div className="mt-6 flex flex-col items-center gap-3 text-center">
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <ShieldCheck className="size-3.5" aria-hidden />
                {t("auth.owner.restricted")}
              </p>
              <Link
                to="/crm/login"
                className="text-[13px] font-medium text-indigo-300 hover:underline"
              >
                ← {t("auth.owner.workspaceLink")}
              </Link>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
