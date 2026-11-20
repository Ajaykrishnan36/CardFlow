import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Share2 } from 'lucide-react';
import { insuranceApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { RecordRow } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { useRecordScope } from '@crm/features/records/record-scope';

/** Share a quote with the customer by a link (catalogue B7): premium and features only, opens logged. */
export function ShareQuoteButton({ record, disabled }: { record: RecordRow; disabled?: boolean }) {
  const scope = useRecordScope();
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const share = useMutation({
    mutationFn: () => insuranceApi(code).shareQuote(record.id),
    onSuccess: async (r) => {
      const url = r.url.startsWith('http') ? r.url : `${window.location.origin}${r.url}`;
      try {
        await navigator.clipboard.writeText(url);
        toast.success('Link copied — send it to the customer. It works for 30 days; each open shows on this quote.');
      } catch {
        toast.success(`Share this link with the customer: ${url}`, { duration: 15000 });
      }
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : 'Couldn’t make the link.')
  });
  if (!code || !scope.can('quotes', 'update')) return null;
  return (
    <Button size="sm" variant="outline" onClick={() => share.mutate()} disabled={disabled || share.isPending}>
      <Share2 /> Share with customer
    </Button>
  );
}
