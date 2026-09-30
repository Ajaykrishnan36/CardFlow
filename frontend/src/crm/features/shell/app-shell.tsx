import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, Outlet, useLocation, useMatch } from 'react-router-dom';
import { ArrowLeft, Crown, Menu as MenuIcon, Search } from 'lucide-react';
import { isOwnerSession, useCapabilities, useMe } from '@crm/auth/session';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Button } from '@crm/components/ui/button';
import { FullPageLoader } from '@crm/components/states';
import { useUI } from '@crm/lib/ui-store';
import { cn } from '@crm/lib/utils';
import type { NavItem } from '@crm/api/types';
import { useWorkspaceContextQuery } from '@crm/features/workspace/workspace-context';
import { SidebarContent } from './sidebar';
import { CommandMenu } from './command-menu';
import { ThemeToggle } from './theme-toggle';
import { NotificationsBell, useLiveUpdates } from './live';

function currentNav(items: NavItem[] | undefined, pathname: string): NavItem | undefined {
  return items
    ?.filter((i) => pathname === i.path || pathname.startsWith(i.path + '/'))
    .sort((a, b) => b.path.length - a.path.length)[0];
}

/** Persistent left sidebar + top bar + content (PRD §10.1, Twenty-style). */
export function AppShell() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  // Inside the member workspace app the nav comes from the workspace context, not /capabilities.
  const wsMatch = useMatch({ path: '/crm/w/:ws', end: false });
  const wsCode = wsMatch?.params.ws;
  const { data: caps } = useCapabilities(Boolean(me) && !wsCode);
  const wsQ = useWorkspaceContextQuery(me ? wsCode : undefined);
  const workspace = wsCode ? wsQ.data : undefined;
  const navigation = wsCode ? wsQ.data?.navigation : caps?.navigation;
  // Distinguish "still loading" (skeleton) from "loaded but failed" (empty nav) in the sidebar.
  const navLoading = wsCode ? wsQ.isPending : !caps;
  const collapsed = useUI((s) => s.sidebarCollapsed);
  const mobileNavOpen = useUI((s) => s.mobileNavOpen);
  const setMobileNavOpen = useUI((s) => s.setMobileNavOpen);
  const setCommandOpen = useUI((s) => s.setCommandOpen);
  const location = useLocation();

  useEffect(() => setMobileNavOpen(false), [location.pathname, setMobileNavOpen]);
  const ownerSession = me ? isOwnerSession(me) : false;
  const apiPrefix = wsCode ? `/w/${encodeURIComponent(wsCode)}` : ownerSession ? '/platform' : null;
  const routeBase = wsCode ? `/crm/w/${encodeURIComponent(wsCode)}` : '/crm/owner';
  useLiveUpdates(me ? apiPrefix : null);

  if (!me) return <FullPageLoader />;
  const active = currentNav(navigation, location.pathname);
  const isOwner = isOwnerSession(me);
  const contextCrumb = workspace?.workspace.name ?? (isOwner ? t('brand.ownerConsole') : me.memberships[0]?.workspaceName ?? t('shell.workspace'));

  return (
    <div className="flex h-full overflow-hidden bg-surface">
      <aside
        className={cn(
          'hidden shrink-0 border-r transition-[width] duration-200 ease-out md:block',
          collapsed ? 'w-[64px]' : 'w-[248px]'
        )}
      >
        <SidebarContent me={me} navigation={navigation} loading={navLoading} workspace={workspace} workspaceCode={wsCode} collapsed={collapsed} apiPrefix={apiPrefix} />
      </aside>

      <Dialog open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <DialogContent
          hideClose
          className="left-0 top-0 h-full w-[84vw] max-w-[300px] translate-x-0 rounded-none border-y-0 border-l-0 p-0 animate-slide-in-left"
        >
          <DialogTitle className="sr-only">{t('shell.openMenu')}</DialogTitle>
          <DialogDescription className="sr-only">{t('brand.name')}</DialogDescription>
          <SidebarContent
            me={me}
            navigation={navigation}
            loading={navLoading}
            workspace={workspace}
            workspaceCode={wsCode}
            collapsed={false}
            apiPrefix={apiPrefix}
            onNavigate={() => setMobileNavOpen(false)}
          />
        </DialogContent>
      </Dialog>

      <div className="crm-scroll flex min-w-0 flex-1 flex-col overflow-y-auto">
        {workspace?.viewerIsOwner ? (
          // The platform owner is inside a customer's workspace: always make that obvious.
          <div role="status" className="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-b border-amber-300/60 bg-amber-50 px-3 py-1.5 text-xs text-amber-950 dark:border-amber-400/25 dark:bg-amber-400/10 dark:text-amber-100 sm:px-5">
            <Crown className="size-3.5 shrink-0" aria-hidden />
            <span className="min-w-0 flex-1">{t('workspaceApp.owner.banner', { workspace: workspace.workspace.name })}</span>
            <Link
              to={`/crm/owner/workspaces/${encodeURIComponent(workspace.workspace.id)}`}
              className="inline-flex items-center gap-1 rounded font-semibold underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <ArrowLeft className="size-3.5" aria-hidden />
              {t('workspaceApp.owner.back')}
            </Link>
          </div>
        ) : null}
        <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur-md sm:px-5">
          <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={() => setMobileNavOpen(true)} aria-label={t('shell.openMenu')}>
            <MenuIcon />
          </Button>
          <nav aria-label="Breadcrumb" className="min-w-0 flex-1">
            <ol className="flex items-center gap-1.5 text-[13px]">
              <li className="hidden max-w-[16rem] truncate text-muted-foreground sm:block">{contextCrumb}</li>
              {active ? (
                <>
                  <li className="hidden text-muted-foreground/50 sm:block" aria-hidden>
                    /
                  </li>
                  <li className="truncate font-medium text-foreground" aria-current="page">
                    {active.label}
                  </li>
                </>
              ) : null}
            </ol>
          </nav>
          <Button variant="subtle" size="icon-sm" className="md:hidden" onClick={() => setCommandOpen(true)} aria-label={t('shell.search')}>
            <Search />
          </Button>
          {apiPrefix ? <NotificationsBell prefix={apiPrefix} /> : null}
          <ThemeToggle />
        </header>
        <main className="flex-1">
          <Outlet />
        </main>
      </div>

      <CommandMenu navigation={navigation ?? []} workspaces={workspace?.workspaces} currentWorkspace={wsCode} apiPrefix={apiPrefix} routeBase={routeBase} />
    </div>
  );
}
