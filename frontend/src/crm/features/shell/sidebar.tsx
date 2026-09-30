import { useTranslation } from 'react-i18next';
import { Link, NavLink, useNavigate } from 'react-router-dom';
import { Check, ChevronsLeft, ChevronsRight, ChevronsUpDown, Crown, LayoutGrid, Search } from 'lucide-react';
import type { Me, NavItem, WorkspaceContext } from '@crm/api/types';
import { isOwnerSession, workspaceHomePath } from '@crm/auth/session';
import { LogoMark } from '@crm/components/brand';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuTrigger, Tooltip } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn, initials } from '@crm/lib/utils';
import { useUI } from '@crm/lib/ui-store';
import { navIcon } from './nav-icons';
import { UserMenu } from './user-menu';
import { setSelectedApp } from '@crm/features/workspace/selected-app';
import { FavoritesNav } from './live';

function groupNav(items: NavItem[]) {
  const groups: Array<{ name: string | null; items: NavItem[] }> = [];
  for (const item of items) {
    const name = item.group ?? null;
    let g = groups.find((x) => x.name === name);
    if (!g) {
      g = { name, items: [] };
      groups.push(g);
    }
    g.items.push(item);
  }
  return groups;
}

interface SidebarProps {
  me: Me;
  /** Owner console: /capabilities navigation. Workspace app: context.navigation. */
  navigation?: NavItem[];
  loading?: boolean;
  /** Set inside /crm/w/:ws — drives the header (workspace + role) and the switcher. */
  workspace?: WorkspaceContext;
  workspaceCode?: string;
  collapsed: boolean;
  onNavigate?: () => void;
  className?: string;
  /** API prefix of the CRM being shown ('/platform' or '/w/<code>'), for favorites. */
  apiPrefix?: string | null;
}

export function SidebarContent({ me, navigation, loading, workspace, workspaceCode, collapsed, onNavigate, className, apiPrefix }: SidebarProps) {
  const { t } = useTranslation();
  const toggleSidebar = useUI((s) => s.toggleSidebar);
  const setCommandOpen = useUI((s) => s.setCommandOpen);
  const isOwner = isOwnerSession(me);
  const primaryMembership = me.memberships.find((m) => !m.isPlatformWorkspace) ?? me.memberships[0];
  const inWorkspace = Boolean(workspaceCode);

  return (
    <div className={cn('flex h-full flex-col bg-sidebar', className)}>
      {/* Context header */}
      {inWorkspace ? (
        <>
          <WorkspaceHeader workspace={workspace} code={workspaceCode!} collapsed={collapsed} onNavigate={onNavigate} />
          <AppSwitcher workspace={workspace} code={workspaceCode!} collapsed={collapsed} onNavigate={onNavigate} />
        </>
      ) : (
        <div className={cn('flex h-14 shrink-0 items-center gap-2.5 border-b px-3', collapsed && 'justify-center px-2')}>
          <div className="relative">
            <LogoMark className="size-8" />
            {isOwner ? (
              <span className="absolute -bottom-1 -right-1 grid size-4 place-items-center rounded-full border-2 border-sidebar bg-amber-400 text-amber-950">
                <Crown className="size-2.5" aria-hidden />
              </span>
            ) : null}
          </div>
          {!collapsed ? (
            <div className="min-w-0 leading-tight">
              <p className="truncate text-[13px] font-semibold text-foreground">{isOwner ? t('brand.name') : primaryMembership?.workspaceName ?? t('brand.name')}</p>
              <p className="truncate text-xs text-muted-foreground">{isOwner ? t('brand.ownerConsole') : primaryMembership?.roleName ?? t('shell.workspace')}</p>
            </div>
          ) : null}
        </div>
      )}

      {/* Search */}
      <div className={cn('px-3 pt-3', collapsed && 'px-2')}>
        {collapsed ? (
          <Tooltip content={t('shell.search')}>
            <button
              type="button"
              onClick={() => setCommandOpen(true)}
              className="grid h-9 w-full place-items-center rounded-md border bg-background text-muted-foreground hover:bg-muted"
              aria-label={t('shell.search')}
            >
              <Search className="size-4" />
            </button>
          </Tooltip>
        ) : (
          <button
            type="button"
            onClick={() => setCommandOpen(true)}
            className="flex h-9 w-full items-center gap-2 rounded-md border bg-background px-2.5 text-[13px] text-muted-foreground shadow-sm transition-colors hover:border-muted-foreground/30"
          >
            <Search className="size-4" aria-hidden />
            <span className="flex-1 text-left">{t('shell.search')}</span>
            <kbd className="hidden rounded border bg-muted px-1.5 font-sans text-[11px] font-medium md:inline">⌘K</kbd>
          </button>
        )}
      </div>

      {/* Navigation */}
      <nav className="crm-scroll flex-1 overflow-y-auto px-3 py-3" aria-label="Main">
        {apiPrefix ? <FavoritesNav prefix={apiPrefix} collapsed={collapsed} onNavigate={onNavigate} /> : null}
        {loading || !navigation ? (
          <div className="space-y-2 px-1">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-7" />
            ))}
          </div>
        ) : (
          groupNav(navigation).map((group) => (
            <div key={group.name ?? 'root'} className="mb-4 last:mb-0">
              {group.name && !collapsed ? (
                <p className="mb-1 px-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/80">{group.name}</p>
              ) : group.name && collapsed ? (
                <div className="mx-2 mb-2 h-px bg-border" />
              ) : null}
              <ul className="space-y-0.5">
                {group.items.map((item) => (
                  <li key={item.key}>
                    <SidebarLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
                  </li>
                ))}
              </ul>
            </div>
          ))
        )}
      </nav>

      {/* Footer */}
      <div className={cn('shrink-0 border-t p-2', collapsed && 'flex flex-col items-center gap-1')}>
        {workspace?.viewerIsOwner ? <OwnerConsoleLink workspaceId={workspace.workspace.id} collapsed={collapsed} onNavigate={onNavigate} /> : null}
        <UserMenu me={me} collapsed={collapsed} />
        <button
          type="button"
          onClick={toggleSidebar}
          className={cn(
            'mt-1 hidden h-8 items-center gap-2 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground md:flex',
            collapsed ? 'w-9 justify-center' : 'w-full'
          )}
          aria-label={collapsed ? t('shell.expand') : t('shell.collapse')}
        >
          {collapsed ? <ChevronsRight className="size-4" /> : <ChevronsLeft className="size-4" />}
          {!collapsed ? t('shell.collapse') : null}
        </button>
      </div>
    </div>
  );
}

function SidebarLink({ item, collapsed, onNavigate }: { item: NavItem; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const Icon = navIcon(item.icon);
  const link = (
    <NavLink
      to={item.path}
      onClick={onNavigate}
      end={item.path.split('/').length <= 3}
      className={({ isActive }) =>
        cn(
          'group flex h-8 items-center gap-2.5 rounded-md px-2 text-[13px] font-medium transition-colors',
          collapsed && 'mx-auto w-9 justify-center px-0',
          isActive
            ? 'bg-primary-soft text-primary'
            : item.available
              ? 'text-foreground/80 hover:bg-muted hover:text-foreground'
              : 'text-muted-foreground/70 hover:bg-muted hover:text-muted-foreground'
        )
      }
    >
      <Icon className="size-4 shrink-0" aria-hidden />
      {!collapsed ? (
        <>
          <span className="flex-1 truncate">{item.label}</span>
          {!item.available ? (
            <span className="rounded border border-dashed px-1 text-[10px] font-medium uppercase leading-4 tracking-wide text-muted-foreground/80">
              {t('common.soon')}
            </span>
          ) : null}
        </>
      ) : (
        <span className="sr-only">{item.label}</span>
      )}
    </NavLink>
  );
  return collapsed ? <Tooltip content={item.available ? item.label : `${item.label} · ${t('common.soon')}`}>{link}</Tooltip> : link;
}

function WorkspaceAvatar({ name, className }: { name: string; className?: string }) {
  return (
    <span className={cn('grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-[12px] font-semibold text-primary-foreground shadow-sm shadow-primary/20', className)} aria-hidden>
      {initials(name) || '·'}
    </span>
  );
}

/** Workspace name + role; a switcher menu when the member belongs to more than one workspace. */
function WorkspaceHeader({ workspace, code, collapsed, onNavigate }: { workspace?: WorkspaceContext; code: string; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const name = workspace?.workspace.name ?? '';
  const role = workspace?.viewerIsOwner ? t('workspaceApp.owner.role') : (workspace?.role?.name ?? t('workspaceApp.shell.member'));
  // `workspaces` may or may not include the current one; keep one entry per code, current first.
  const options = workspace
    ? [{ code, name }, ...workspace.workspaces.filter((w) => w.code !== code && w.code !== workspace.workspace.code)]
    : [];
  const switchable = options.length > 1;

  const body = (
    <>
      {workspace ? <WorkspaceAvatar name={name} /> : <Skeleton className="size-8 rounded-lg" />}
      {!collapsed ? (
        <span className="min-w-0 flex-1 text-left leading-tight">
          {workspace ? (
            <>
              <span className="block truncate text-[13px] font-semibold text-foreground">{name}</span>
              <span className="block truncate text-xs text-muted-foreground">{role}</span>
            </>
          ) : (
            <>
              <Skeleton className="h-3.5 w-28" />
              <Skeleton className="mt-1 h-3 w-16" />
            </>
          )}
        </span>
      ) : null}
      {switchable && !collapsed ? <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" aria-hidden /> : null}
    </>
  );

  const shell = cn('flex h-14 shrink-0 items-center border-b px-3', collapsed && 'justify-center px-2');
  if (!switchable) {
    return (
      <div className={shell}>
        <div className={cn('flex min-w-0 flex-1 items-center gap-2.5', collapsed && 'flex-none')} title={collapsed ? `${name} · ${role}` : undefined}>
          {body}
        </div>
      </div>
    );
  }
  return (
    <div className={shell}>
      <Menu>
        <MenuTrigger asChild>
          <button
            type="button"
            className={cn(
              'flex min-w-0 items-center gap-2.5 rounded-md p-1 transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring data-[state=open]:bg-muted',
              collapsed ? 'flex-none' : '-mx-1 flex-1'
            )}
            aria-label={t('workspaceApp.shell.workspaceMenu', { name })}
          >
            {body}
          </button>
        </MenuTrigger>
        <MenuContent align="start" className="w-64">
          <MenuLabel>{t('workspaceApp.shell.switchWorkspace')}</MenuLabel>
          {options.map((w) => {
            const current = w.code === code;
            return (
              <MenuItem
                key={w.code}
                onSelect={() => {
                  if (current) return;
                  onNavigate?.();
                  navigate(workspaceHomePath(w.code));
                }}
                aria-current={current ? 'true' : undefined}
              >
                <WorkspaceAvatar name={w.name} className="size-6 rounded-md text-[10px]" />
                <span className="min-w-0 flex-1 truncate">{w.name}</span>
                {current ? <Check className="!text-primary" aria-label={t('workspaceApp.shell.currentWorkspace')} /> : null}
              </MenuItem>
            );
          })}
        </MenuContent>
      </Menu>
    </div>
  );
}

/** Owner viewing a customer workspace: one click back to the console. */
function OwnerConsoleLink({ workspaceId, collapsed, onNavigate }: { workspaceId: string; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const link = (
    <Link
      to={`/crm/owner/workspaces/${encodeURIComponent(workspaceId)}`}
      onClick={onNavigate}
      className={cn(
        'mb-1 flex h-8 items-center gap-2 rounded-md px-2 text-[13px] font-medium text-amber-900 transition-colors hover:bg-amber-100 dark:text-amber-200 dark:hover:bg-amber-400/10',
        collapsed ? 'w-9 justify-center px-0' : 'w-full'
      )}
    >
      <Crown className="size-4 shrink-0" aria-hidden />
      {collapsed ? <span className="sr-only">{t('workspaceApp.owner.console')}</span> : <span className="truncate">{t('workspaceApp.owner.console')}</span>}
    </Link>
  );
  return collapsed ? <Tooltip content={t('workspaceApp.owner.console')}>{link}</Tooltip> : link;
}

/** Apps of a product (D-73), like Salesforce's App Launcher: switching changes the menu, records are shared. */
function AppSwitcher({ workspace, code, collapsed, onNavigate }: { workspace?: WorkspaceContext; code: string; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const apps = workspace?.apps ?? [];
  if (apps.length < 2) return null;
  const current = apps.find((a) => a.key === workspace?.app);
  const currentName = current?.name ?? t('shell.apps.all');
  const pick = (key: string) => {
    if (key === (current?.key ?? 'all')) return;
    setSelectedApp(code, key);
    onNavigate?.();
    navigate(workspaceHomePath(code));
  };
  const trigger = (
    <button
      type="button"
      className={cn(
        'flex w-full items-center gap-2 rounded-md border bg-background px-2 py-1.5 text-left shadow-sm transition-colors hover:bg-muted data-[state=open]:bg-muted',
        collapsed && 'justify-center px-0'
      )}
      aria-label={t('shell.apps.switch', { name: currentName })}
    >
      <LayoutGrid className="size-4 shrink-0 text-primary" aria-hidden />
      {!collapsed ? (
        <>
          <span className="min-w-0 flex-1">
            <span className="block text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{t('shell.apps.label')}</span>
            <span className="block truncate text-[13px] font-medium text-foreground">{currentName}</span>
          </span>
          <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        </>
      ) : null}
    </button>
  );
  return (
    <div className={cn('px-3 pt-3', collapsed && 'px-2')}>
      <Menu>
        <MenuTrigger asChild>{trigger}</MenuTrigger>
        <MenuContent align="start" className="w-64">
          <MenuLabel>{t('shell.apps.title')}</MenuLabel>
          <MenuItem aria-current={!current ? 'true' : undefined} onSelect={() => pick('all')}>
            <span className="grid size-6 place-items-center rounded-md bg-muted text-muted-foreground">
              <LayoutGrid className="size-3.5" aria-hidden />
            </span>
            <span className="min-w-0 flex-1 truncate">{t('shell.apps.all')}</span>
            {!current ? <Check className="!text-primary" aria-label={t('shell.apps.current')} /> : null}
          </MenuItem>
          {apps.map((a) => {
            const on = a.key === current?.key;
            return (
              <MenuItem key={a.key} aria-current={on ? 'true' : undefined} onSelect={() => pick(a.key)}>
                <span className="grid size-6 place-items-center rounded-md text-[10px] font-semibold text-white" style={{ background: a.accentColor || 'hsl(var(--primary))' }}>
                  {initials(a.name)}
                </span>
                <span className="min-w-0 flex-1 truncate">{a.name}</span>
                {on ? <Check className="!text-primary" aria-label={t('shell.apps.current')} /> : null}
              </MenuItem>
            );
          })}
          <p className="px-2 pb-1.5 pt-1 text-[11px] text-muted-foreground">{t('shell.apps.hint')}</p>
        </MenuContent>
      </Menu>
    </div>
  );
}
