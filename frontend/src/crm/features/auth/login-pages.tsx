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
 * "/" — the site's own page (D-158): start a CRM with your mobile number. Each business
 * signs its people in at its own URL; this page also opens that URL from a code, and
 * someone who already has an account is shown their business URLs after the code.
 */
export function LoginPage() {
  const { t } = useTranslation();
  setSignInTenant(""); // the main page signs anyone in
  useDocumentTitle("Create your CRM");
  const [emailWay, setEmailWay] = useState(false);
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
        title={emailWay ? "Sign in with email" : "Create your own CRM"}
        subtitle={
          emailWay
            ? "For people added by email. Next time, use your business URL."
            : "Enter your mobile number. After the code you set up your business and get its own URL."
        }
      />
      <LoginForm audience="workspace" phoneOnly={!emailWay} />
      <button
        type="button"
        className="mt-3 text-[13px] font-medium text-primary hover:underline"
        onClick={() => setEmailWay(!emailWay)}
      >
        {emailWay
          ? "Create a CRM with your mobile number instead"
          : "Added to a business by email? Sign in with email"}
      </button>
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
      <div className="mt-8 flex flex-col items-start gap-1.5 rounded-lg border border-dashed bg-muted/40 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
        <div className="flex items-center gap-2.5 text-[13px] text-muted-foreground">
          <Crown className="size-4 text-primary" aria-hidden />
          {t("auth.login.ownerPrompt")}
        </div>
        <Link
          to="/crm/owner/login"
          className="group inline-flex items-center gap-1 text-[13px] font-medium text-primary hover:underline"
        >
          {t("auth.login.ownerLink")}
          <ArrowRight
            className="size-3.5 transition-transform group-hover:translate-x-0.5"
            aria-hidden
          />
        </Link>
      </div>
    </AuthLayout>
  );
}

/** "Already have a business?" — paste its URL or type its code to open its sign-in page. */
function OpenBusinessUrl() {
  const navigate = useNavigate();
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
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
      {error ? <p className="mt-1.5 text-[13px] text-danger">{error}</p> : null}
      <p className="mt-2 text-xs text-muted-foreground">
        Don’t have it? Enter your mobile number above — after the code we show
        your business URLs.
      </p>
    </form>
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
