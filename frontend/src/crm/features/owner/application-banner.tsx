import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { platformApi } from "@crm/api/endpoints";
import { Alert } from "@crm/components/ui/card";
import { ApproveButton } from "@crm/features/workspaces/approve-button";

/**
 * On an owner-console lead made by "Create your own CRM" (D-161): the business it applied for,
 * and Approve while it waits. Converting the lead into a contact stays on the page as usual.
 */
export function ApplicationBanner({ leadId }: { leadId: string }) {
  const q = useQuery({
    queryKey: ["platform", "applications", "lead", leadId],
    queryFn: () => platformApi.applications({ leadId }),
  });
  const app = q.data?.[0];
  if (!app) return null;
  const pending = app.status === "pending";
  return (
    <Alert
      tone={pending ? "warning" : "success"}
      title={
        pending
          ? `${app.name} is waiting for your approval`
          : `${app.name} is approved`
      }
      className="mb-4"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p>
          {pending
            ? "Approving opens its URL and sends it to the applicant by email, SMS and WhatsApp."
            : `Its URL is live. ${app.members} ${app.members === 1 ? "person" : "people"} so far.`}{" "}
          <Link
            to={`/crm/owner/workspaces/${encodeURIComponent(app.workspaceId)}`}
            className="font-mono text-xs font-medium underline"
          >
            {app.display}
          </Link>
        </p>
        {pending ? (
          <ApproveButton
            workspaceId={app.workspaceId}
            name={app.name}
            size="md"
          />
        ) : null}
      </div>
    </Alert>
  );
}
