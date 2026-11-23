import { useMutation, useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { FileQuestion } from 'lucide-react';
import { legalApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { RecordRow } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { formatMoney } from '@crm/lib/money';
import { useRecordScope } from '@crm/features/records/record-scope';

const useCode = () => { const s = useRecordScope(); return s.prefix.startsWith('/w/') ? decodeURIComponent(s.prefix.slice(3)) : ''; };

/** Marks the missing documents as requested and copies the message; nothing is sent automatically. */
export function RequestDocumentsButton({ record, disabled }: { record: RecordRow; disabled?: boolean }) {
  const code = useCode();
  const ask = useMutation({
    mutationFn: () => legalApi(code).requestDocuments(record.id),
    onSuccess: async (r) => {
      if (!r.message) { toast.success('Nothing is missing on this matter.'); return; }
      try { await navigator.clipboard.writeText(r.message); toast.success(`${r.requested.length} documents marked requested. Message copied, send it to the client.`); }
      catch { toast.success(r.message, { duration: 15000 }); }
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : 'Couldn’t request the documents.')
  });
  if (!code) return null;
  return <Button size="sm" variant="outline" onClick={() => ask.mutate()} disabled={disabled || ask.isPending}><FileQuestion /> Request missing documents</Button>;
}

/** On a case intake: what the chosen service covers, needs and costs. */
export function ServicePanel({ record }: { record: RecordRow }) {
  const code = useCode();
  const id = String(record.values?.serviceId ?? '');
  const q = useQuery({ queryKey: ['legal-service', code, id], queryFn: () => legalApi(code).service(id), enabled: Boolean(code && id) });
  if (!id || !q.data) return null;
  const s = q.data;
  return (
    <Card className="p-4">
      <div className="flex items-baseline justify-between gap-3"><h3 className="font-semibold">{s.name}</h3><span className="font-semibold">{s.price == null ? 'No price set' : formatMoney(s.price, 'INR').replace(/\.00$/, '')}</span></div>
      <div className="mt-3 grid gap-4 md:grid-cols-2 text-sm">
        <div><div className="mb-1 text-xs text-muted-foreground">Scope</div><ul className="list-disc pl-4">{s.scope.map((x) => <li key={x.name}>{x.name} ({x.kind})</li>)}</ul></div>
        <div><div className="mb-1 text-xs text-muted-foreground">Documents from client</div><ul className="list-disc pl-4">{s.documents.map((x) => <li key={x.name}>{x.name} ({x.kind.replace(/_/g, ' ')})</li>)}</ul></div>
      </div>
    </Card>
  );
}
