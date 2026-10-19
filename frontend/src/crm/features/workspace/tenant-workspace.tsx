import { Navigate, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { Building2, LogOut } from 'lucide-react';
import { isOwnerSession, useMe, useSignOut } from '@crm/auth/session';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { PageContainer } from '@crm/components/page';
import { currentBusinessFor, memberTenantOf, rememberBusiness } from '@crm/lib/tenant';
import { WorkspaceLayout } from './workspace-layout';

/**
 * /<tenant>/* (D-156): the tenant's permanent address. The business inside it is the one
 * asked for (?b=<code>, then forgotten from the address), else the last one used here, else
 * the first — so PSG School and PSG College share /psg/home, /psg/leads … Who may open what
 * is still decided by the server for every request.
 */
export function TenantWorkspace() {
  const { tenant = '' } = useParams();
  const slug = tenant.toLowerCase();
  const { data: me } = useMe();
  const [search] = useSearchParams();
  const location = useLocation();
  const signOut = useSignOut();
  const mine = (me?.memberships ?? []).filter((m) => m.status === 'active' && m.tenantSlug === slug);
  const asked = search.get('b');
  if (asked !== null) {
    if (mine.some((m) => m.workspaceCode === asked)) rememberBusiness(slug, asked);
    const rest = new URLSearchParams(search);
    rest.delete('b');
    const q = rest.toString();
    return <Navigate to={`${location.pathname}${q ? `?${q}` : ''}${location.hash}`} replace />;
  }
  if (mine.length === 0) {
    return (
      <PageContainer>
        <Card className="mx-auto mt-10 max-w-md p-8 text-center">
          <span className="mx-auto grid size-11 place-items-center rounded-full bg-muted text-muted-foreground">
            <Building2 className="size-5" aria-hidden />
          </span>
          <h1 className="mt-3 text-base font-semibold">Account not found</h1>
          <p className="mt-1 text-sm text-muted-foreground">You aren’t one of the people of this business. Sign in with the account they added, or ask them to add you.</p>
          <Button className="mt-5" variant="outline" onClick={() => void signOut({ to: `/${slug}/login?signedOut=1` })}>
            <LogOut /> Sign in with another account
          </Button>
        </Card>
      </PageContainer>
    );
  }
  const current = currentBusinessFor(slug) ?? mine[0].workspaceCode;
  return <WorkspaceLayout key={current} code={current} />;
}

/** /crm/w/<code>/x — older and server-made links: members go to the tenant's /<slug>/x. */
export function LegacyWorkspaceRoute() {
  const { ws = '' } = useParams();
  const { data: me } = useMe();
  const location = useLocation();
  const slug = memberTenantOf(ws);
  if (!me || isOwnerSession(me) || !slug) return <WorkspaceLayout />; // the platform owner's support visits keep /crm/w
  const rest = location.pathname.replace(/^\/crm\/w\/[^/]+/, '');
  const q = new URLSearchParams(location.search);
  q.set('b', ws);
  return <Navigate to={`/${slug}${rest || '/home'}?${q.toString()}${location.hash}`} replace />;
}
