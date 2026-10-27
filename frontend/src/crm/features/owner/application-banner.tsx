import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Building2, ExternalLink, UserRound } from "lucide-react";
import { platformApi } from "@crm/api/endpoints";
import { Alert, Badge, Card } from "@crm/components/ui/card";
import { ApproveButton } from "@crm/features/workspaces/approve-button";
import { AppsTable } from "./customers-page";

/**
 * On an owner-console lead, contact or account (D-161, D-162): the person behind it and their
 * businesses — the ones they created (with Approve while one waits) and the ones they belong
 * to. Converting a lead into a contact stays on the page as usual.
 */
export function ApplicationBanner({
  object,
  id,
}: {
  object: "leads" | "contacts" | "accounts";
  id: string;
}) {
  const q = useQuery({
    queryKey: ["platform", "record-businesses", object, id],
    queryFn: () => platformApi.recordBusinesses(object, id),
  });
  const data = q.data;
  if (!data?.person) return null;
  const waiting = data.apps.filter((a) => a.status === "pending");
  return (
    <div className="mb-4 space-y-3">
      {waiting.map((a) => (
        <Alert
          key={a.workspaceId}
          tone="warning"
          title={`${a.name} is waiting for your approval`}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p>
              Approving opens its URL and sends it to the applicant by email,
              SMS and WhatsApp.
            </p>
            <ApproveButton
              workspaceId={a.workspaceId}
              name={a.name}
              size="md"
            />
          </div>
        </Alert>
      ))}
      <Card className="p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <p className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <UserRound className="size-4 text-muted-foreground" aria-hidden />
            {data.person.name || "Unnamed"}
            <span className="font-normal text-muted-foreground">
              {[data.person.phone, data.person.email]
                .filter(Boolean)
                .join(" · ")}
            </span>
          </p>
          <Link
            to={`/crm/owner/customers/${encodeURIComponent(data.person.id)}`}
            className="text-xs font-medium text-primary hover:underline"
          >
            Open product
          </Link>
        </div>
        <p className="mb-2 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Building2 className="size-3.5" aria-hidden /> Businesses (
          {data.apps.length})
        </p>
        <AppsTable apps={data.apps} />
        {data.memberOf.length ? (
          <>
            <p className="mb-2 mt-4 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Also a member of
            </p>
            <ul className="divide-y rounded-lg border">
              {data.memberOf.map((b) => (
                <li
                  key={b.slug}
                  className="flex flex-wrap items-center gap-3 px-4 py-2.5 text-[13px]"
                >
                  <span className="min-w-0 flex-1 font-medium text-foreground">
                    {b.name}
                  </span>
                  <Badge tone="primary">{b.role || "Member"}</Badge>
                  <span className="text-xs text-muted-foreground">
                    {b.members} people
                  </span>
                  <a
                    href={b.loginUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="flex items-center gap-1 text-xs font-medium text-primary hover:underline"
                  >
                    <ExternalLink className="size-3.5" aria-hidden /> URL
                  </a>
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </Card>
    </div>
  );
}
