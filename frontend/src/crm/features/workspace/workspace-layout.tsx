import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, Navigate, Outlet, useParams } from 'react-router-dom';
import { Building2, Compass, KeyRound, Lock, LogOut } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { useMe, useSignOut, workspaceHomePath } from '@crm/auth/session';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { Spinner } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { PageContainer } from '@crm/components/page';
import { RecordScopeProvider, scopeFromContext } from '@crm/features/records/record-scope';
import { hexToHsl } from '@crm/lib/utils';
import { setDefaultCurrency } from '@crm/lib/money';
import { useWorkspaceContextQuery, WorkspaceProvider } from './workspace-context';

/**
 * Route element for /crm/w/:ws/*. Loads the member's workspace context and provides
 * the record scope (API prefix, routes, permissions) to every page below it.
 */
export function WorkspaceLayout() {
  const { t } = useTranslation();
  const { ws = '' } = useParams();
  const q = useWorkspaceContextQuery(ws);
  const value = useMemo(() => (q.data ? { code: ws, context: q.data } : null), [q.data, ws]);
  const scope = useMemo(() => (q.data ? scopeFromContext(q.data, ws) : null), [q.data, ws]);
  useProductAccent(q.data?.setup?.accentColor);
  setDefaultCurrency(q.data?.workspace.currency);

  if (q.isPending) {
    return (
      <div className="grid min-h-[60vh] place-items-center">
        <Spinner className="size-5 text-muted-foreground" label={t('common.loading')} />
      </div>
    );
  }
  if (q.isError) {
    if (isApiError(q.error) && q.error.code === 'sign_in_method_not_allowed') return <WrongSignInMethod code={ws} message={q.error.message} />;
    if (isApiError(q.error) && q.error.status === 403) return <WorkspaceUnavailable code={ws} reason="forbidden" />;
    if (isApiError(q.error) && q.error.status === 404) return <WorkspaceUnavailable code={ws} reason="notFound" />;
    return (
      <PageContainer>
        <Card>
          <ErrorState
            title={t('workspaceApp.layout.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      </PageContainer>
    );
  }
  if (!value || !scope) return null;

  return (
    <WorkspaceProvider value={value}>
      <RecordScopeProvider value={scope}>
        <Outlet />
      </RecordScopeProvider>
    </WorkspaceProvider>
  );
}

/** The product setup's accent colour (Details & branding) becomes the workspace's primary colour. */
function useProductAccent(hex: string | undefined) {
  useEffect(() => {
    const hsl = hex ? hexToHsl(hex) : null;
    if (!hsl) return;
    const [h, s, l] = hsl;
    const el = document.createElement('style');
    el.dataset.productAccent = '';
    const fg = l > 65 ? '0 0% 9%' : '0 0% 100%';
    const darkL = Math.min(72, Math.max(l, 58));
    el.textContent =
      `:root{--primary:${h} ${s}% ${l}%;--ring:${h} ${s}% ${l}%;--primary-foreground:${fg};--primary-soft:${h} 100% 97%}` +
      `.dark{--primary:${h} ${Math.min(s, 85)}% ${darkL}%;--ring:${h} ${Math.min(s, 85)}% ${darkL}%;--primary-foreground:${darkL > 65 ? '0 0% 9%' : '0 0% 100%'};--primary-soft:${h} 40% 19%}`;
    document.head.appendChild(el);
    return () => el.remove();
  }, [hex]);
}

/** /crm/w/:ws → /crm/w/:ws/home */
export function WorkspaceIndexRedirect() {
  const { ws = '' } = useParams();
  return <Navigate to={workspaceHomePath(ws)} replace />;
}

/** The product only accepts other sign-in methods (e.g. SSO) than the one this session used (D-64). */
function WrongSignInMethod({ code, message }: { code: string; message: string }) {
  const { t } = useTranslation();
  const signOut = useSignOut();
  return (
    <div className="grid min-h-full place-items-center px-6 py-16">
      <div className="flex w-full max-w-md flex-col items-center text-center animate-slide-up">
        <div className="mb-5 grid size-14 place-items-center rounded-2xl border bg-card text-primary shadow-card">
          <KeyRound className="size-7" aria-hidden />
        </div>
        <h1 className="text-xl font-semibold tracking-tight text-foreground">{t('workspaceApp.layout.methodTitle')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{message}</p>
        <Button className="mt-6" onClick={() => void signOut({ to: `/crm/login?product=${encodeURIComponent(code)}` })}>
          {t('workspaceApp.layout.methodAction')}
        </Button>
      </div>
    </div>
  );
}

/** Not a member (403 no_workspace_access) or unknown code (404): offer the user's other workspaces. */
function WorkspaceUnavailable({ code, reason }: { code: string; reason: 'forbidden' | 'notFound' }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const signOut = useSignOut();
  const others = (me?.memberships ?? []).filter((m) => m.status === 'active' && m.workspaceCode !== code);
  const Icon = reason === 'forbidden' ? Lock : Compass;
  return (
    <div className="grid min-h-full place-items-center px-6 py-16">
      <div className="flex w-full max-w-md flex-col items-center text-center animate-slide-up">
        <div className="mb-5 grid size-14 place-items-center rounded-2xl border bg-card text-primary shadow-card">
          <Icon className="size-7" aria-hidden />
        </div>
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          {reason === 'forbidden' ? t('workspaceApp.layout.forbiddenTitle') : t('workspaceApp.layout.notFoundTitle')}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {reason === 'forbidden' ? t('workspaceApp.layout.forbiddenBody') : t('workspaceApp.layout.notFoundBody', { code })}
        </p>
        {others.length ? (
          <Card className="mt-6 w-full overflow-hidden text-left">
            <p className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">{t('workspaceApp.layout.yourWorkspaces')}</p>
            <ul className="divide-y">
              {others.map((m) => (
                <li key={m.id}>
                  <Link to={workspaceHomePath(m.workspaceCode)} className="flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none">
                    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary-soft text-primary">
                      <Building2 className="size-4" aria-hidden />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px] font-medium text-foreground">{m.workspaceName}</span>
                      {m.roleName ? <span className="block truncate text-xs text-muted-foreground">{m.roleName}</span> : null}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </Card>
        ) : null}
        <div className="mt-6 flex flex-wrap justify-center gap-2">
          <Button asChild variant="outline">
            <Link to="/crm/me">{t('workspaceApp.layout.profile')}</Link>
          </Button>
          <Button variant="ghost" onClick={() => void signOut()}>
            <LogOut /> {t('common.signOut')}
          </Button>
        </div>
      </div>
    </div>
  );
}
