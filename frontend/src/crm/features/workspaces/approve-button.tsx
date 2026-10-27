import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { BadgeCheck } from "lucide-react";
import { platformApi } from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import { Button } from "@crm/components/ui/button";

const channelNames = {
  email: "email",
  sms: "SMS",
  whatsapp: "WhatsApp",
} as const;

/** What happened on each channel, honestly: "Sent by email. SMS isn't set up on this server." */
export function describeNotified(
  sent: Record<"email" | "sms" | "whatsapp", string>,
) {
  const by = (status: string) =>
    (Object.keys(channelNames) as Array<keyof typeof channelNames>)
      .filter((k) => sent[k] === status)
      .map((k) => channelNames[k]);
  const parts: string[] = [];
  if (by("sent").length) parts.push(`URL sent by ${by("sent").join(", ")}.`);
  if (by("failed").length) parts.push(`${by("failed").join(", ")} failed.`);
  if (by("not_set_up").length)
    parts.push(`${by("not_set_up").join(", ")} not set up on this server.`);
  if (by("no_address").length)
    parts.push(`No ${by("no_address").join(", ")} address on file.`);
  return parts.join(" ");
}

/**
 * Approve a business that waits for the owner (D-161): its URL opens and is sent to the
 * applicant by email, SMS and WhatsApp.
 */
export function ApproveButton({
  workspaceId,
  name,
  size = "sm",
}: {
  workspaceId: string;
  name: string;
  size?: "sm" | "md";
}) {
  const qc = useQueryClient();
  const approve = useMutation({
    mutationFn: () => platformApi.approve(workspaceId),
    onSuccess: (res) => {
      toast.success(`${name} is approved`, {
        description: describeNotified(res.notified),
      });
      void qc.invalidateQueries({ queryKey: ["workspaces"] });
      void qc.invalidateQueries({ queryKey: ["workspace", workspaceId] });
      void qc.invalidateQueries({ queryKey: ["platform"] });
    },
    onError: (e) =>
      toast.error(
        isApiError(e) ? e.message : "Could not approve. Please try again.",
      ),
  });
  return (
    <Button
      size={size}
      loading={approve.isPending}
      onClick={(e) => {
        e.stopPropagation();
        e.preventDefault();
        approve.mutate();
      }}
    >
      <BadgeCheck /> Approve
    </Button>
  );
}
