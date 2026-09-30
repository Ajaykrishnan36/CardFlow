import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Play, Workflow as WorkflowIcon } from 'lucide-react';
import { workspaceToolsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ObjectKey } from '@crm/api/types';
import type { Workflow, WorkflowInput } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuLabel, MenuTrigger } from '@crm/components/ui/menu';
import { useRecordScope } from './record-scope';

// Manual workflows on records (D-78): a "Run workflow" button on the record page and
// in the list's bulk bar. Workflows with a form ask for their answers first.

export function useManualWorkflows(object: ObjectKey) {
  const scope = useRecordScope();
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const q = useQuery({ queryKey: ['workflows', code], queryFn: () => workspaceToolsApi(code).workflows(), enabled: Boolean(code), staleTime: 60_000 });
  const canManage = q.data?.canManage ?? false;
  const list = (q.data?.data ?? []).filter(
    (w) =>
      w.status === 'active' &&
      w.published?.trigger.type === 'manual' &&
      w.published.trigger.object === object &&
      w.published.trigger.manual?.mode !== 'global' &&
      (canManage || w.published.trigger.manual?.everyone)
  );
  return { code, list };
}

/** Runs a workflow on records, asking for its form first when it has one. */
export function useRunWorkflow(code: string, recordIds: () => string[], onDone?: () => void) {
  const { t } = useTranslation();
  const [asking, setAsking] = useState<Workflow | null>(null);
  const run = useMutation({
    mutationFn: ({ wf, input }: { wf: Workflow; input?: Record<string, unknown> }) => workspaceToolsApi(code).runWorkflow(wf.id, { recordIds: recordIds(), input }),
    onSuccess: (r) => {
      toast.success(t('lists.bulk.workflowStarted', { count: r.runs.length }));
      setAsking(null);
      onDone?.();
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const start = (wf: Workflow) => {
    if (wf.published?.trigger.manual?.form?.length) setAsking(wf);
    else run.mutate({ wf });
  };
  const dialog = <WorkflowFormDialog wf={asking} onClose={() => setAsking(null)} loading={run.isPending} onRun={(input) => asking && run.mutate({ wf: asking, input })} />;
  return { start, dialog, running: run.isPending };
}

/** Record page: "Run workflow" menu (hidden when none apply). */
export function RunWorkflowButton({ object, id, disabled }: { object: ObjectKey; id: string; disabled?: boolean }) {
  const { t } = useTranslation();
  const { code, list } = useManualWorkflows(object);
  const { start, dialog, running } = useRunWorkflow(code, () => [id]);
  if (!list.length) return null;
  return (
    <>
      <Menu>
        <MenuTrigger asChild>
          <Button variant="outline" size="sm" disabled={disabled || running} loading={running}>
            <Play /> {t('records.detail.runWorkflow')}
          </Button>
        </MenuTrigger>
        <MenuContent align="end" className="w-64">
          <MenuLabel>{t('records.detail.runWorkflowTitle')}</MenuLabel>
          {list.map((w) => (
            <MenuItem key={w.id} onSelect={() => start(w)}>
              <WorkflowIcon /> <span className="min-w-0 flex-1 truncate">{w.name}</span>
            </MenuItem>
          ))}
        </MenuContent>
      </Menu>
      {dialog}
    </>
  );
}

function WorkflowFormDialog({ wf, onClose, onRun, loading }: { wf: Workflow | null; onClose: () => void; onRun: (input: Record<string, unknown>) => void; loading: boolean }) {
  const { t } = useTranslation();
  const form: WorkflowInput[] = wf?.published?.trigger.manual?.form ?? [];
  const [values, setValues] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    const missing = form.find((f) => f.required && !String(values[f.key] ?? '').trim());
    if (missing) return setError(t('records.detail.workflowFieldRequired', { label: missing.label }));
    const input: Record<string, unknown> = {};
    for (const f of form) {
      const v = values[f.key];
      if (v === undefined || v === '') continue;
      input[f.key] = f.type === 'number' ? Number(v) : f.type === 'boolean' ? v === 'true' : v;
    }
    onRun(input);
  };
  return (
    <Dialog
      open={Boolean(wf)}
      onOpenChange={(o) => {
        if (!o) {
          setValues({});
          setError(null);
          onClose();
        }
      }}
    >
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{wf?.name}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{wf?.description || t('records.detail.workflowFormBody')}</DialogDescription>
        <div className="mt-4 space-y-3">
          {error ? <Alert tone="danger">{error}</Alert> : null}
          {form.map((f) => (
            <Field key={f.key} label={f.required ? `${f.label} *` : f.label}>
              {f.type === 'select' ? (
                <Select value={values[f.key] ?? ''} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })} placeholder={t('records.input.selectPlaceholder')} options={(f.options ?? []).map((o) => ({ value: o, label: o }))} />
              ) : f.type === 'boolean' ? (
                <Select value={values[f.key] ?? ''} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })} options={[{ value: '', label: '—' }, { value: 'true', label: t('tools.common.yes') }, { value: 'false', label: t('tools.common.no') }]} />
              ) : (
                <Input type={f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text'} value={values[f.key] ?? ''} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })} />
              )}
            </Field>
          ))}
          <div className="flex justify-end gap-2 pt-1">
            <Button variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button onClick={submit} loading={loading}>
              <Play /> {t('records.detail.runNow')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
