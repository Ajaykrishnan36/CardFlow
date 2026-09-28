import { useTranslation } from 'react-i18next';
import { Link, Navigate, useParams } from 'react-router-dom';
import { Building2, LogOut } from 'lucide-react';
import { homeWorkspace, isOwnerSession, useMe, useSignOut, workspaceHomePath } from '@crm/auth/session';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { EmptyState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';

/**
 * /crm/home and /crm/home/* (legacy entry): send members into their workspace app at
 * /crm/w/{code}/…, keeping any sub-path (e.g. /crm/home/leads → /crm/w/acme/leads).
 * Only people without an active workspace stay here.
 */
export function WorkspaceHomePage() {
  const { data: me } = useMe();
  const { '*': rest = '' } = useParams();
  if (!me) return null;
  if (isOwnerSession(me)) return <Navigate to="/crm/owner/dashboard" replace />;
  const ws = homeWorkspace(me);
  if (ws) {
    const home = workspaceHomePath(ws.workspaceCode);
    const sub = rest.replace(/^\/+|\/+$/g, '');
    return <Navigate to={sub ? home.replace(/\/home$/, `/${sub}`) : home} replace />;
  }
  return <NoWorkspaces />;
}

function NoWorkspaces() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const signOut = useSignOut();
  useDocumentTitle(t('shell.workspace'));
  const firstName = me?.identity.displayName.split(' ')[0] ?? '';
  return (
    <div className="mx-auto w-full max-w-2xl px-4 py-10 sm:px-6 lg:py-16">
      <h1 className="mb-5 text-center text-[22px] font-semibold tracking-tight sm:text-2xl">{t('workspace.home.title', { name: firstName })}</h1>
      <Card>
        <EmptyState
          icon={Building2}
          title={t('workspace.home.noWorkspaces')}
          body={t('workspaceApp.home.noWorkspacesBody')}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button asChild variant="outline" size="sm">
                <Link to="/crm/me">{t('workspaceApp.layout.profile')}</Link>
              </Button>
              <Button variant="ghost" size="sm" onClick={() => void signOut()}>
                <LogOut /> {t('common.signOut')}
              </Button>
            </div>
          }
        />
      </Card>
    </div>
  );
}
