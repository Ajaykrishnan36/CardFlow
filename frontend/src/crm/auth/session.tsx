import { useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Navigate, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { authApi, meApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { Me, Membership } from '@crm/api/types';
import { safeReturnTo } from '@crm/lib/utils';
import { FullPageLoader } from '@crm/components/states';
import { UnavailablePage } from '@crm/features/system/pages';

export const meKey = ['me'] as const;

/** GET /me — null when signed out (401). Never retried: a 401 is an answer, not a failure. */
export function useMe() {
  return useQuery<Me | null>({
    queryKey: meKey,
    queryFn: async () => {
      try {
        return await meApi.me();
      } catch (e) {
        if (isApiError(e) && e.status === 401) return null;
        throw e;
      }
    },
    retry: false,
    staleTime: 30_000
  });
}

export function useCapabilities(enabled: boolean) {
  return useQuery({ queryKey: ['capabilities'], queryFn: meApi.capabilities, enabled, staleTime: 60_000 });
}

function isAuthStep(path: string) {
  return path === '/crm/mfa/setup' || path === '/crm/mfa/verify' || path === '/crm/change-password';
}

export function isOwnerSession(me: Me): boolean {
  return me.identity.isPlatformOwner && me.session.audience === 'owner';
}

/**
 * The workspace a member lands in: their first active customer workspace, else the
 * platform workspace (the owner's own staff), else none.
 */
export function homeWorkspace(me: Me): Membership | undefined {
  const active = me.memberships.filter((m) => m.status === 'active');
  return active.find((m) => !m.isPlatformWorkspace) ?? active.find((m) => m.isPlatformWorkspace);
}

export function workspaceHomePath(code: string): string {
  return `/crm/w/${encodeURIComponent(code)}/home`;
}

/** Where a fully signed-in user belongs if they hit an auth page. */
export function homeFor(me: Me): string {
  if (isOwnerSession(me)) return '/crm/owner/dashboard';
  const ws = homeWorkspace(me);
  return ws ? workspaceHomePath(ws.workspaceCode) : '/crm/businesses';
}

/**
 * Guards authenticated routes. `step` pages (MFA, forced password change) accept a
 * session that is still mid-sign-in; every other page sends it to the server-decided
 * next step first (PRD §10.4: expired session → login → back to an allow-listed URL).
 */
export function RequireSession({ step = false }: { step?: boolean }) {
  const { data: me, isPending, error, refetch } = useMe();
  const location = useLocation();

  if (isPending) return <FullPageLoader />;
  if (error) {
    if (isApiError(error) && error.status === 503) return <UnavailablePage />;
    return <UnavailablePage onRetry={() => void refetch()} />;
  }
  if (!me) {
    // The owner console has its own sign-in; everything else shares one (D-104).
    const login = location.pathname.startsWith('/crm/owner') ? '/crm/owner/login' : '/crm/login';
    const returnTo = safeReturnTo(location.pathname + location.search);
    return <Navigate to={`${login}${returnTo ? `?returnTo=${encodeURIComponent(returnTo)}` : ''}`} replace />;
  }
  const pendingStep = isAuthStep(me.next);
  if (!step && pendingStep) {
    const returnTo = safeReturnTo(location.pathname + location.search);
    return <Navigate to={returnTo ? `${me.next}?returnTo=${encodeURIComponent(returnTo)}` : me.next} replace />;
  }
  if (step && pendingStep && location.pathname !== me.next) return <Navigate to={me.next + location.search} replace />;
  // MFA setup and password change can also be opened voluntarily from the profile.
  if (step && !pendingStep && location.pathname === '/crm/mfa/verify') {
    const returnTo = safeReturnTo(new URLSearchParams(location.search).get('returnTo'));
    return <Navigate to={returnTo ?? homeFor(me)} replace />;
  }
  return <Outlet />;
}

export function RequireOwner() {
  const { data: me } = useMe();
  const location = useLocation();
  // Anyone who isn't signed in to the owner console is asked to (it has its own sign-in).
  if (!me?.identity.isPlatformOwner || me.session.audience !== 'owner') {
    const returnTo = safeReturnTo(location.pathname + location.search);
    return <Navigate to={`/crm/owner/login${returnTo ? `?returnTo=${encodeURIComponent(returnTo)}` : ''}`} replace />;
  }
  return <Outlet />;
}

/** /crm/owner/login: shown to everyone except a signed-in owner (a signed-in customer may still sign in as owner). */
export function OwnerLoginGate() {
  const { data: me, isPending } = useMe();
  const [params] = useSearchParams();
  if (isPending) return <FullPageLoader />;
  if (me && isOwnerSession(me) && !isAuthStep(me.next)) {
    return <Navigate to={safeReturnTo(params.get('returnTo')) ?? '/crm/owner/dashboard'} replace />;
  }
  return <Outlet />;
}

/** Public auth pages: bounce an already signed-in user to where they belong. */
export function RedirectIfSignedIn() {
  const { data: me, isPending } = useMe();
  const location = useLocation();
  if (isPending) return <FullPageLoader />;
  if (me) {
    const returnTo = safeReturnTo(new URLSearchParams(location.search).get('returnTo'));
    if (isAuthStep(me.next)) {
      return <Navigate to={returnTo ? `${me.next}?returnTo=${encodeURIComponent(returnTo)}` : me.next} replace />;
    }
    return <Navigate to={returnTo ?? homeFor(me)} replace />;
  }
  return <Outlet />;
}

export function useSignOut() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return useCallback(
    async (opts: { all?: boolean; to?: string } = {}) => {
      try {
        await (opts.all ? authApi.logoutAll() : authApi.logout());
      } catch {
        /* the session may already be gone — sign out locally regardless */
      }
      qc.clear();
      qc.setQueryData(meKey, null);
      navigate(opts.to ?? '/crm/login?signedOut=1', { replace: true });
    },
    [qc, navigate]
  );
}

/** After a successful auth step: refresh /me, then go to the server's next step. */
export function useAfterAuth() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return useCallback(
    async (next: string, returnTo?: string | null) => {
      qc.removeQueries({ queryKey: ['capabilities'] });
      await qc.fetchQuery({ queryKey: meKey, queryFn: meApi.me, staleTime: 0 });
      if (isAuthStep(next)) {
        // Carry the original destination through MFA / password-change steps.
        navigate(returnTo ? `${next}?returnTo=${encodeURIComponent(returnTo)}` : next, { replace: true });
        return;
      }
      navigate(returnTo ?? next, { replace: true });
    },
    [qc, navigate]
  );
}
