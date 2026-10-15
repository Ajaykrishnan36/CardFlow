import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CheckSquare } from 'lucide-react';
import { workspaceToolsApi, type ChecklistStep } from '@crm/api/endpoints';
import type { ObjectKey, RecordRow } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Checkbox } from '@crm/components/ui/form-controls';
import { errorText } from '@crm/features/tools/tool-utils';
import { useRecordScope } from './record-scope';

/** The checklists of a record (D-152): steps that must be done before it reaches a stage. */
export function ChecklistCard({ object, record }: { object: ObjectKey; record: RecordRow }) {
  const scope = useRecordScope();
  const qc = useQueryClient();
  const code = decodeURIComponent(scope.prefix.replace(/^\/w\//, ''));
  const key = ['checklists', code, object, record.id];
  const q = useQuery({ queryKey: key, queryFn: () => workspaceToolsApi(code).recordChecklists(object, record.id), enabled: Boolean(code), staleTime: 10_000 });
  const tick = useMutation({
    mutationFn: (s: ChecklistStep) => scope.api.update('checklist_items', s.id, { done: !s.done }, s.version),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
    onError: (e) => toast.error(errorText(e, "Couldn't save the step."))
  });
  const lists = q.data?.data ?? [];
  if (!lists.length) return null;
  return (
    <>
      {lists.map((l) => {
        const done = l.steps.filter((s) => s.done).length;
        return (
          <Card key={l.id} className="mb-4 overflow-hidden">
            <CardHeader
              className="px-4 py-3"
              title={
                <span className="flex flex-wrap items-center gap-2 text-[13px]">
                  <CheckSquare className="size-4 text-muted-foreground" aria-hidden /> {l.name}
                  <Badge tone={done === l.steps.length ? 'success' : 'warning'}>
                    {done}/{l.steps.length} done
                  </Badge>
                </span>
              }
              description={`Must be done before “${l.stage}”.`}
            />
            <ul className="divide-y">
              {l.steps.map((s) => (
                <li key={s.id} className="px-4 py-2">
                  <Checkbox
                    checked={s.done}
                    disabled={tick.isPending}
                    onCheckedChange={() => tick.mutate(s)}
                    label={
                      <span className={s.done ? 'text-muted-foreground line-through' : ''}>
                        {s.name}
                        {s.required ? null : <span className="ml-1 text-xs text-muted-foreground">(optional)</span>}
                      </span>
                    }
                    description={s.done && s.doneBy ? `${s.doneBy}${s.doneAt ? ` · ${new Date(s.doneAt).toLocaleString()}` : ''}` : undefined}
                  />
                </li>
              ))}
            </ul>
          </Card>
        );
      })}
    </>
  );
}
