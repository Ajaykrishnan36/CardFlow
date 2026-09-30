import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Download, Pencil, RotateCcw, Trash2, X, Zap } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { BulkQuery } from '@crm/api/endpoints';
import type { FieldDef, ObjectKey, ObjectMeta } from '@crm/api/types';
import type { BulkResult } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuTrigger } from '@crm/components/ui/menu';
import { Select } from '@crm/components/ui/form-controls';
import { FieldInput } from '../field-input';
import { recordKeys } from '../use-object-meta';
import { useRecordScope } from '../record-scope';

export function BulkBar({
  object,
  meta,
  count,
  allMatching,
  totalMatching,
  query,
  onSelectAll,
  onClear,
  binMode,
  exportHref,
  workflows,
  onRunWorkflow
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  count: number;
  allMatching: boolean;
  totalMatching: number;
  query: BulkQuery;
  onSelectAll?: () => void;
  onClear: () => void;
  binMode?: boolean;
  exportHref?: string;
  workflows?: Array<{ id: string; name: string }>;
  onRunWorkflow?: (id: string) => void;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [confirm, setConfirm] = useState<'delete' | 'destroy' | null>(null);
  const run = useMutation({
    mutationFn: (body: { action: 'update' | 'delete' | 'restore' | 'destroy'; values?: Record<string, unknown> }) => scope.api.bulk(object, { ...body, query }),
    onSuccess: (res: BulkResult, body) => {
      void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) });
      const verb = t(`lists.bulk.done.${body.action}`, { count: res.processed });
      if (res.failed) toast.warning(`${verb} ${t('lists.bulk.failedN', { count: res.failed })} ${res.failures[0]?.error ?? ''}`);
      else toast.success(verb);
      setEditOpen(false);
      setConfirm(null);
      onClear();
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const n = allMatching ? totalMatching : count;
  return (
    <div className="sticky top-0 z-20 flex flex-wrap items-center gap-2 border-b bg-primary-soft px-4 py-2 text-[13px]" role="toolbar" aria-label={t('lists.bulk.toolbar')}>
      <span className="font-semibold text-primary">{t('lists.bulk.selected', { count: n })}</span>
      {!allMatching && onSelectAll && totalMatching > count ? (
        <Button variant="link" size="sm" onClick={onSelectAll}>
          {t('lists.bulk.selectAll', { count: totalMatching })}
        </Button>
      ) : null}
      <div className="ml-auto flex flex-wrap items-center gap-1.5">
        {binMode ? (
          <>
            <Button size="sm" variant="outline" onClick={() => run.mutate({ action: 'restore' })} loading={run.isPending && run.variables?.action === 'restore'} disabled={!scope.can(object, 'delete')}>
              <RotateCcw /> {t('lists.bulk.restore')}
            </Button>
            {scope.can(object, 'destroy') ? (
              <Button size="sm" variant="danger-outline" onClick={() => setConfirm('destroy')}>
                <Trash2 /> {t('lists.bulk.destroy')}
              </Button>
            ) : null}
          </>
        ) : (
          <>
            {scope.can(object, 'update') ? (
              <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
                <Pencil /> {t('lists.bulk.edit')}
              </Button>
            ) : null}
            {workflows?.length && onRunWorkflow ? (
              <Menu>
                <MenuTrigger asChild>
                  <Button size="sm" variant="outline">
                    <Zap /> {t('lists.bulk.runWorkflow')}
                  </Button>
                </MenuTrigger>
                <MenuContent>
                  {workflows.map((w) => (
                    <MenuItem key={w.id} onSelect={() => onRunWorkflow(w.id)}>
                      {w.name}
                    </MenuItem>
                  ))}
                </MenuContent>
              </Menu>
            ) : null}
            {exportHref && scope.can(object, 'export') ? (
              <Button size="sm" variant="outline" asChild>
                <a href={exportHref}>
                  <Download /> {t('lists.bulk.export')}
                </a>
              </Button>
            ) : null}
            {scope.can(object, 'delete') ? (
              <Button size="sm" variant="danger-outline" onClick={() => setConfirm('delete')}>
                <Trash2 /> {t('lists.bulk.delete')}
              </Button>
            ) : null}
          </>
        )}
        <Button size="icon-sm" variant="subtle" onClick={onClear} aria-label={t('lists.bulk.clear')}>
          <X />
        </Button>
      </div>
      <BulkEditDialog meta={meta} open={editOpen} onOpenChange={setEditOpen} count={n} busy={run.isPending} onApply={(values) => run.mutate({ action: 'update', values })} />
      <Dialog open={confirm !== null} onOpenChange={(o) => !o && setConfirm(null)}>
        <DialogContent className="max-w-md p-5">
          <DialogTitle className="text-base font-semibold">{confirm === 'destroy' ? t('lists.bulk.destroyTitle', { count: n }) : t('lists.bulk.deleteTitle', { count: n })}</DialogTitle>
          <DialogDescription className="mt-2 text-[13px] text-muted-foreground">{confirm === 'destroy' ? t('lists.bulk.destroyBody') : t('lists.bulk.deleteBody')}</DialogDescription>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirm(null)}>
              {t('lists.bulk.cancel')}
            </Button>
            <Button variant="danger" loading={run.isPending} onClick={() => confirm && run.mutate({ action: confirm })}>
              {confirm === 'destroy' ? t('lists.bulk.destroyConfirm', { count: n }) : t('lists.bulk.deleteConfirm', { count: n })}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function BulkEditDialog({ meta, open, onOpenChange, count, busy, onApply }: { meta: ObjectMeta; open: boolean; onOpenChange: (o: boolean) => void; count: number; busy: boolean; onApply: (values: Record<string, unknown>) => void }) {
  const { t } = useTranslation();
  const editable = meta.fields.filter((f) => !f.readOnly && f.type !== 'files').sort((a, b) => a.label.localeCompare(b.label));
  const [key, setKey] = useState(editable[0]?.key ?? '');
  const [value, setValue] = useState<unknown>(null);
  const field: FieldDef | undefined = editable.find((f) => f.key === key);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="text-base font-semibold">{t('lists.bulk.editTitle', { count })}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('lists.bulk.editBody')}</DialogDescription>
        <div className="mt-4 space-y-3">
          <label className="block space-y-1 text-[13px]">
            <span className="font-medium">{t('lists.bulk.field')}</span>
            <Select value={key} onChange={(e) => { setKey(e.target.value); setValue(null); }} options={editable.map((f) => ({ value: f.key, label: f.label }))} />
          </label>
          {field ? (
            <div className="space-y-1 text-[13px]">
              <span className="font-medium">{t('lists.bulk.newValue')}</span>
              <FieldInput field={field} value={value} onChange={setValue} />
              <p className="text-xs text-muted-foreground">{t('lists.bulk.emptyClears')}</p>
            </div>
          ) : null}
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('lists.bulk.cancel')}
          </Button>
          <Button loading={busy} onClick={() => onApply({ [key]: value === '' ? null : value })} disabled={!field}>
            {t('lists.bulk.apply', { count })}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
