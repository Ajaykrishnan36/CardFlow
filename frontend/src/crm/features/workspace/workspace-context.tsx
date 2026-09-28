import { createContext, useContext, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { workspaceApi } from '@crm/api/endpoints';
import type { ObjectKey, WorkspaceContext } from '@crm/api/types';
import { permissionObject } from '@crm/features/records/record-scope';

// GET /w/{code}/context, shared by the shell (navigation, switcher) and the
// workspace routes (record scope, dashboard). One query per workspace code.

export const workspaceContextKey = (code: string) => ['workspace', code, 'context'] as const;

export function useWorkspaceContextQuery(code: string | undefined) {
  return useQuery({
    queryKey: workspaceContextKey(code ?? ''),
    queryFn: () => workspaceApi.context(code!),
    enabled: Boolean(code),
    staleTime: 60_000
  });
}

interface WorkspaceValue {
  /** The workspace code from the URL (used for every route and API path). */
  code: string;
  context: WorkspaceContext;
}

const Ctx = createContext<WorkspaceValue | null>(null);

export function WorkspaceProvider({ value, children }: { value: WorkspaceValue; children: ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/** Only valid below WorkspaceLayout (/crm/w/:ws/*). */
export function useWorkspace(): WorkspaceValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('useWorkspace() must be used inside <WorkspaceLayout>');
  return v;
}

export function workspaceBase(code: string): string {
  return `/crm/w/${encodeURIComponent(code)}`;
}

export function isWorkspaceHome(path: string, code: string): boolean {
  return path === `${workspaceBase(code)}/home`;
}

/** dashboard.view: the server only lists the Home nav item when it is granted. */
export function hasDashboard(ctx: WorkspaceContext, code: string): boolean {
  return ctx.navigation.some((n) => isWorkspaceHome(n.path, code)) || ctx.effective.capabilities.some((c) => c.key === 'dashboard.view');
}

/** First page inside the workspace other than Home (used when the dashboard isn't granted). */
export function firstModulePath(ctx: WorkspaceContext, code: string): string | null {
  const base = workspaceBase(code) + '/';
  return ctx.navigation.find((n) => n.path.startsWith(base) && !isWorkspaceHome(n.path, code))?.path ?? null;
}

/** Support tickets (catalog object `ticket`, module `tickets`): read = see, update = reply / change status. */
export function ticketAccess(ctx: WorkspaceContext): { read: boolean; update: boolean } {
  if (ctx.viewerIsOwner) return { read: true, update: true };
  const a = ctx.effective.objects.ticket;
  const on = Boolean(a && a.moduleEnabled);
  return { read: on && a!.actions.includes('read'), update: on && a!.actions.includes('update') };
}

const OBJECTS: readonly ObjectKey[] = ['leads', 'accounts', 'contacts'];

/** Record objects this member can open. */
export function readableObjects(ctx: WorkspaceContext): ObjectKey[] {
  if (ctx.viewerIsOwner) return [...OBJECTS];
  return OBJECTS.filter((o) => {
    const a = ctx.effective.objects[permissionObject[o]];
    return Boolean(a && a.moduleEnabled && a.actions.includes('read'));
  });
}
