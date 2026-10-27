import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Building2, ExternalLink, UserRound } from "lucide-react";
import { platformApi, type Application } from "@crm/api/endpoints";
import { Card } from "@crm/components/ui/card";
import { Skeleton } from "@crm/components/ui/spinner";
import { PageContainer, PageHeader, SearchInput } from "@crm/components/page";
import { EmptyState, ErrorState } from "@crm/components/states";
import { useDocumentTitle } from "@crm/features/auth/login-pages";
import { relativeTime } from "@crm/lib/utils";
import { ApproveButton } from "@crm/features/workspaces/approve-button";
import { WorkspaceStatusBadge } from "@crm/features/workspaces/workspace-ui";
import type { WorkspaceStatus } from "@crm/api/types";

/**
 * /crm/owner/customers — Products (D-161): one per person, one mobile number, who created a
 * CRM on "Create your own CRM", with how many apps (businesses) they run.
 */
export function CustomersPage() {
  useDocumentTitle("Products");
  const q = useQuery({
    queryKey: ["platform", "customers"],
    queryFn: platformApi.customers,
  });
  const [search, setSearch] = useState("");
  const rows = useMemo(() => {
    const s = search.trim().toLowerCase();
    return (q.data ?? []).filter(
      (c) => !s || `${c.name} ${c.phone} ${c.email}`.toLowerCase().includes(s),
    );
  }, [q.data, search]);
  const apps = (q.data ?? []).reduce((n, c) => n + c.apps, 0);
  return (
    <PageContainer>
      <PageHeader
        title="Products"
        description="One product per account: the person (mobile number) who created a CRM, and how many apps they run."
      />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder="Search by name, mobile or email…"
          className="sm:w-72"
        />
        {q.data ? (
          <p className="text-[13px] text-muted-foreground">
            {q.data.length} {q.data.length === 1 ? "product" : "products"} ·{" "}
            {apps} {apps === 1 ? "app" : "apps"}
          </p>
        ) : null}
      </div>
      <Card className="overflow-hidden">
        {q.isPending ? (
          <Skeleton className="m-5 h-40" />
        ) : q.isError ? (
          <ErrorState
            title="Couldn't load products"
            onRetry={() => void q.refetch()}
          />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={UserRound}
            title="No products yet"
            body="When someone creates their CRM on the sign-up page, their account shows here."
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[13px]">
              <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 font-medium">Account</th>
                  <th className="px-4 py-2.5 font-medium">Mobile</th>
                  <th className="px-4 py-2.5 text-right font-medium">Apps</th>
                  <th className="px-4 py-2.5 text-right font-medium">
                    To approve
                  </th>
                  <th className="px-4 py-2.5 text-right font-medium">People</th>
                  <th className="px-4 py-2.5 text-right font-medium">Since</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((c) => (
                  <tr key={c.id} className="hover:bg-muted/40">
                    <td className="px-4 py-3">
                      <Link
                        to={`/crm/owner/customers/${encodeURIComponent(c.id)}`}
                        className="font-medium text-primary hover:underline"
                      >
                        {c.name || "Unnamed"}
                      </Link>
                      {c.email ? (
                        <p className="text-xs text-muted-foreground">
                          {c.email}
                        </p>
                      ) : null}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs">
                      {c.phone || "—"}
                    </td>
                    <td className="px-4 py-3 text-right font-semibold tabular-nums">
                      {c.apps}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums">
                      {c.pending ? (
                        <span className="font-medium text-warning">
                          {c.pending}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums">
                      {c.members}
                    </td>
                    <td className="px-4 py-3 text-right text-muted-foreground">
                      {relativeTime(c.createdAt)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </PageContainer>
  );
}

/** /crm/owner/customers/:id — one account and every app it runs. */
export function CustomerDetailPage() {
  const { id = "" } = useParams();
  const q = useQuery({
    queryKey: ["platform", "customer", id],
    queryFn: () => platformApi.customer(id),
    enabled: Boolean(id),
  });
  useDocumentTitle(q.data?.customer.name || "Product");
  const crumbs = [{ label: "Products", to: "/crm/owner/customers" }];
  if (q.isPending)
    return (
      <PageContainer>
        <Skeleton className="h-60" />
      </PageContainer>
    );
  if (q.isError || !q.data) {
    return (
      <PageContainer>
        <PageHeader title="Product" crumbs={crumbs} />
        <ErrorState
          title="Couldn't load this product"
          onRetry={() => void q.refetch()}
        />
      </PageContainer>
    );
  }
  const { customer: c, apps } = q.data;
  return (
    <PageContainer>
      <PageHeader
        crumbs={[...crumbs, { label: c.name || c.phone }]}
        title={c.name || "Unnamed"}
        description={[c.phone, c.email].filter(Boolean).join(" · ")}
      />
      <p className="mb-3 text-[13px] text-muted-foreground">
        {c.apps} {c.apps === 1 ? "app" : "apps"} · {c.active} live
        {c.pending ? ` · ${c.pending} waiting for approval` : ""} · {c.members}{" "}
        people
      </p>
      <AppsTable apps={apps} />
    </PageContainer>
  );
}

export function AppsTable({ apps }: { apps: Application[] }) {
  if (apps.length === 0)
    return (
      <Card>
        <EmptyState
          icon={Building2}
          title="No apps"
          body="This account hasn't created a business."
        />
      </Card>
    );
  return (
    <Card className="overflow-hidden">
      <ul className="divide-y">
        {apps.map((a) => (
          <li
            key={a.workspaceId}
            className="flex flex-wrap items-center gap-3 px-4 py-3"
          >
            <div className="min-w-0 flex-1">
              <Link
                to={`/crm/owner/workspaces/${encodeURIComponent(a.workspaceId)}`}
                className="font-medium text-foreground hover:text-primary hover:underline"
              >
                {a.name}
              </Link>
              <p className="mt-0.5 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                <span className="font-mono">{a.display}</span>
                {a.industry ? <span>{a.industry}</span> : null}
                {a.teamSize ? <span>Team {a.teamSize}</span> : null}
                <span>
                  {a.members} {a.members === 1 ? "person" : "people"}
                </span>
                {a.taxId ? <span>ID {a.taxId}</span> : null}
              </p>
            </div>
            <WorkspaceStatusBadge status={a.status as WorkspaceStatus} />
            {a.status === "pending" ? (
              <ApproveButton workspaceId={a.workspaceId} name={a.name} />
            ) : a.status === "active" ? (
              <a
                href={a.loginUrl}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-1 text-xs font-medium text-primary hover:underline"
              >
                <ExternalLink className="size-3.5" aria-hidden /> URL
              </a>
            ) : null}
          </li>
        ))}
      </ul>
    </Card>
  );
}
