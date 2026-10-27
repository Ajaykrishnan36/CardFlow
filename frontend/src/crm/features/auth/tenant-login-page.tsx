import { useQuery } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  useLocation,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { Building2, ShieldCheck } from "lucide-react";
import { onboardingApi } from "@crm/api/endpoints";
import { homeFor, isAuthStep, isOwnerSession, useMe } from "@crm/auth/session";
import { FullPageLoader } from "@crm/components/states";
import { safeReturnTo } from "@crm/lib/utils";
import { setSignInTenant } from "@crm/lib/tenant";
import { AuthHeading, AuthLayout } from "./auth-layout";
import { LoginForm } from "./login-form";
import { LoginPage, useDocumentTitle } from "./login-pages";

/**
 * One business's own sign-in page (D-155, D-158): /<business-code>/login, or the root of the
 * business's own domain. It names the business, and after sign-in goes straight into it.
 * Which records anyone sees is still decided by the server, never by this page.
 */
export function TenantLoginPage({ code: fixed }: { code?: string }) {
  const params = useParams();
  const code = (fixed ?? params.tenant ?? "").toLowerCase();
  const { data: me, isPending } = useMe();
  const [search] = useSearchParams();
  const location = useLocation();
  const tenant = useQuery({
    queryKey: ["tenant", code],
    queryFn: () => onboardingApi.tenant(code),
    retry: false,
    staleTime: 300_000,
    enabled: Boolean(code),
  });
  useDocumentTitle(tenant.data ? `Sign in · ${tenant.data.name}` : "Sign in");
  setSignInTenant(tenant.data?.slug ?? code);

  if (isPending || tenant.isPending) return <FullPageLoader />;
  if (tenant.isError || !tenant.data) {
    return (
      <AuthLayout>
        <AuthHeading
          title="Business not found"
          subtitle="This business URL is invalid or unavailable. Please check the URL or contact support."
          icon={<Building2 className="size-5" />}
        />
        <Link
          to="/"
          className="text-sm font-medium text-primary hover:underline"
        >
          Go to the main sign-in
        </Link>
      </AuthLayout>
    );
  }
  // A grouped or capitalised code, or an address from before D-158, opens the business's own address.
  if (!fixed && tenant.data.slug !== code) {
    return (
      <Navigate to={`/${tenant.data.slug}/login${location.search}`} replace />
    );
  }
  const home = tenant.data.home;
  if (me) {
    if (isAuthStep(me.next))
      return (
        <Navigate
          to={`${me.next}?returnTo=${encodeURIComponent(home)}`}
          replace
        />
      );
    return <Navigate to={home} replace />;
  }
  // The sign-in forms return to the address in ?returnTo — this business's home.
  // Keep ?signedOut=1 / ?expired=1 and a deeper page the person was on (/psg/leads/…).
  const back = safeReturnTo(search.get("returnTo"));
  if (!back || back.split("/")[1] !== tenant.data.slug) {
    const q = new URLSearchParams(search);
    q.set("returnTo", home);
    return <Navigate to={`${location.pathname}?${q.toString()}`} replace />;
  }
  return (
    <AuthLayout
      footer={
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="size-3.5" aria-hidden /> Only people added to{" "}
          {tenant.data.name} can sign in here.
        </span>
      }
    >
      <AuthHeading
        title={`Sign in to ${tenant.data.name}`}
        subtitle="Use your mobile number, or your email and password."
        icon={<Building2 className="size-5" />}
      />
      <LoginForm audience="workspace" />
    </AuthLayout>
  );
}

// Hosts that serve every business (the main site); any other host may be a business's own domain.
function sharedHost(host: string): boolean {
  return (
    /^(localhost|127\.0\.0\.1|\[::1\])$/.test(host) ||
    /\.(vercel\.app|onrender\.com|localhost)$/.test(host) ||
    host === "card-flow-kappa.vercel.app"
  );
}

/** "/" — the main sign-in, or a business's own sign-in when the site is opened on its domain. */
export function RootLoginPage() {
  const host = window.location.hostname.toLowerCase();
  const own = !sharedHost(host);
  const byHost = useQuery({
    queryKey: ["tenant-host", host],
    queryFn: () => onboardingApi.tenantByHost(host),
    retry: false,
    staleTime: 300_000,
    enabled: own,
  });
  if (own && byHost.isPending) return <FullPageLoader />;
  if (own && byHost.data) return <TenantLoginPage code={byHost.data.code} />;
  return <SignedInOr />;
}

function SignedInOr() {
  const { data: me, isPending } = useMe();
  setSignInTenant("");
  const [search] = useSearchParams();
  if (isPending) return <FullPageLoader />;
  if (me) {
    const back = safeReturnTo(search.get("returnTo"));
    if (isAuthStep(me.next))
      return (
        <Navigate
          to={
            back ? `${me.next}?returnTo=${encodeURIComponent(back)}` : me.next
          }
          replace
        />
      );
    return (
      <Navigate
        to={back ?? (isOwnerSession(me) ? homeFor(me) : "/crm/businesses")}
        replace
      />
    );
  }
  return <LoginPage />;
}
