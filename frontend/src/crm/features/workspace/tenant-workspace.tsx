import { useQuery } from "@tanstack/react-query";
import {
  Navigate,
  useLocation,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { onboardingApi } from "@crm/api/endpoints";
import { Building2, LogOut } from "lucide-react";
import { isOwnerSession, useMe, useSignOut } from "@crm/auth/session";
import { Button } from "@crm/components/ui/button";
import { Card } from "@crm/components/ui/card";
import { PageContainer } from "@crm/components/page";
import {
  currentBusinessFor,
  memberTenantOf,
  rememberBusiness,
} from "@crm/lib/tenant";
import { WorkspaceLayout } from "./workspace-layout";

/**
 * /<code>/* (D-156, D-158): a business's own address. Since D-158 each address has one
 * business; ?b=<code> (older links) is still honoured and then dropped from the address.
 * An address typed grouped or in capitals, or one from before D-158, moves to the
 * business's own. Who may open what is still decided by the server for every request.
 */
export function TenantWorkspace() {
  const { tenant = "" } = useParams();
  const slug = tenant.toLowerCase();
  const { data: me } = useMe();
  const [search] = useSearchParams();
  const location = useLocation();
  const signOut = useSignOut();
  const mine = (me?.memberships ?? []).filter(
    (m) => m.status === "active" && m.tenantSlug === slug,
  );
  const asked = search.get("b");
  const lookup = useQuery({
    queryKey: ["tenant", slug],
    queryFn: () => onboardingApi.tenant(slug),
    retry: false,
    staleTime: 300_000,
    enabled: Boolean(me) && mine.length === 0,
  });
  if (asked !== null) {
    if (mine.some((m) => m.workspaceCode === asked))
      rememberBusiness(slug, asked);
    const rest = new URLSearchParams(search);
    rest.delete("b");
    const q = rest.toString();
    return (
      <Navigate
        to={`${location.pathname}${q ? `?${q}` : ""}${location.hash}`}
        replace
      />
    );
  }
  if (mine.length === 0 && lookup.isPending) return null;
  if (mine.length === 0 && lookup.data && lookup.data.slug !== slug) {
    const rest = location.pathname.replace(/^\/[^/]+/, "");
    return (
      <Navigate
        to={`/${lookup.data.slug}${rest}${location.search}${location.hash}`}
        replace
      />
    );
  }
  if (mine.length === 0) {
    return (
      <PageContainer>
        <Card className="mx-auto mt-10 max-w-md p-8 text-center">
          <span className="mx-auto grid size-11 place-items-center rounded-full bg-muted text-muted-foreground">
            <Building2 className="size-5" aria-hidden />
          </span>
          <h1 className="mt-3 text-base font-semibold">
            {lookup.isError ? "Business not found" : "No access"}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {lookup.isError
              ? "This business URL is invalid or unavailable. Please check the URL or contact support."
              : "You don’t have access to this business. Please contact your Super Admin."}
          </p>
          <Button
            className="mt-5"
            variant="outline"
            onClick={() => void signOut({ to: `/${slug}/login?signedOut=1` })}
          >
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
  const { ws = "" } = useParams();
  const { data: me } = useMe();
  const location = useLocation();
  const slug = memberTenantOf(ws);
  if (!me || isOwnerSession(me) || !slug) return <WorkspaceLayout />; // the platform owner's support visits keep /crm/w
  const rest = location.pathname.replace(/^\/crm\/w\/[^/]+/, "");
  const q = new URLSearchParams(location.search);
  q.set("b", ws);
  return (
    <Navigate
      to={`/${slug}${rest || "/home"}?${q.toString()}${location.hash}`}
      replace
    />
  );
}
