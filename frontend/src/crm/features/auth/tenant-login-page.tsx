import { useQuery } from '@tanstack/react-query';
import { Link, Navigate, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { Building2, ShieldCheck } from 'lucide-react';
import { onboardingApi } from '@crm/api/endpoints';
import { homeFor, isAuthStep, useMe } from '@crm/auth/session';
import { FullPageLoader } from '@crm/components/states';
import { safeReturnTo } from '@crm/lib/utils';
import { AuthHeading, AuthLayout } from './auth-layout';
import { LoginForm } from './login-form';
import { LoginPage, useDocumentTitle } from './login-pages';

/**
 * One business's own sign-in page (D-155): /<business-code>/login, or the root of the
 * business's own domain. It names the business, and after sign-in goes straight into it.
 * Which records anyone sees is still decided by the server, never by this page.
 */
export function TenantLoginPage({ code: fixed }: { code?: string }) {
  const params = useParams();
  const code = (fixed ?? params.tenant ?? '').toLowerCase();
  const { data: me, isPending } = useMe();
  const [search] = useSearchParams();
  const location = useLocation();
  const tenant = useQuery({ queryKey: ['tenant', code], queryFn: () => onboardingApi.tenant(code), retry: false, staleTime: 300_000, enabled: Boolean(code) });
  useDocumentTitle(tenant.data ? `Sign in · ${tenant.data.name}` : 'Sign in');

  if (isPending || tenant.isPending) return <FullPageLoader />;
  if (tenant.isError || !tenant.data) {
    return (
      <AuthLayout>
        <AuthHeading title="No business at this address" subtitle="Check the link you were given, or sign in from the main page." icon={<Building2 className="size-5" />} />
        <Link to="/crm/login" className="text-sm font-medium text-primary hover:underline">
          Go to the main sign-in
        </Link>
      </AuthLayout>
    );
  }
  const home = tenant.data.home;
  if (me) {
    if (isAuthStep(me.next)) return <Navigate to={`${me.next}?returnTo=${encodeURIComponent(home)}`} replace />;
    return <Navigate to={home} replace />;
  }
  // The sign-in forms return to the address in ?returnTo — this business's home.
  if (search.get('returnTo') !== home) return <Navigate to={`${location.pathname}?returnTo=${encodeURIComponent(home)}`} replace />;
  return (
    <AuthLayout
      footer={
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="size-3.5" aria-hidden /> Only people {tenant.data.name} has added can sign in.
        </span>
      }
    >
      <AuthHeading title={`Sign in to ${tenant.data.name}`} subtitle="Use your mobile number, or your email and password." icon={<Building2 className="size-5" />} />
      <LoginForm audience="workspace" />
    </AuthLayout>
  );
}

// Hosts that serve every business (the main site); any other host may be a business's own domain.
function sharedHost(host: string): boolean {
  return /^(localhost|127\.0\.0\.1|\[::1\])$/.test(host) || /\.(vercel\.app|onrender\.com|localhost)$/.test(host) || host === 'card-flow-kappa.vercel.app';
}

/** "/" — the main sign-in, or a business's own sign-in when the site is opened on its domain. */
export function RootLoginPage() {
  const host = window.location.hostname.toLowerCase();
  const own = !sharedHost(host);
  const byHost = useQuery({ queryKey: ['tenant-host', host], queryFn: () => onboardingApi.tenantByHost(host), retry: false, staleTime: 300_000, enabled: own });
  if (own && byHost.isPending) return <FullPageLoader />;
  if (own && byHost.data) return <TenantLoginPage code={byHost.data.code} />;
  return <SignedInOr />;
}

function SignedInOr() {
  const { data: me, isPending } = useMe();
  const [search] = useSearchParams();
  if (isPending) return <FullPageLoader />;
  if (me) {
    const back = safeReturnTo(search.get('returnTo'));
    if (isAuthStep(me.next)) return <Navigate to={back ? `${me.next}?returnTo=${encodeURIComponent(back)}` : me.next} replace />;
    return <Navigate to={back ?? homeFor(me)} replace />;
  }
  return <LoginPage />;
}

