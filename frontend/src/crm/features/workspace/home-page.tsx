import { Navigate, useParams } from 'react-router-dom';
import { homeWorkspace, isOwnerSession, useMe, workspaceHomePath } from '@crm/auth/session';

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
  // No business yet: create one (or see why not).
  return <Navigate to="/crm/businesses" replace />;
}
