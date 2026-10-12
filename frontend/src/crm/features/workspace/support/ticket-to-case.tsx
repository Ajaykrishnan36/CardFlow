import { toast } from 'sonner';
import { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { workspaceSupportApi } from '@crm/api/endpoints';
import { Spinner } from '@crm/components/ui/spinner';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { supportPath } from './support-utils';

/** /crm/w/:ws/support/:id — an old ticket link: open the Case that mirrors it (D-72). */
export function TicketToCase() {
  const { id = '' } = useParams();
  const { code } = useWorkspace();
  const navigate = useNavigate();
  useEffect(() => {
    let live = true;
    workspaceSupportApi(code)
      .get(id)
      .then((tk) => live && navigate(supportPath(code, id, tk.caseId), { replace: true }))
      .catch(() => {
        if (!live) return;
        toast.error("That support ticket wasn't found. It may have been removed, or the link is wrong.");
        navigate(supportPath(code), { replace: true });
      });
    return () => {
      live = false;
    };
  }, [code, id, navigate]);
  return (
    <div className="grid min-h-[40vh] place-items-center">
      <Spinner className="size-5 text-muted-foreground" />
    </div>
  );
}
