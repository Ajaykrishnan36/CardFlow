import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Clock } from 'lucide-react';
import { workspaceToolsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { useRecordScope } from '../record-scope';

const CHOICES = [30, 60, 90, 180, 365];

/** The recycle bin's note: how long records stay, and (for access managers) the setting (D-84). */
export function BinRetentionNote() {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const key = ['workspace', code, 'recycle-bin'];
  const q = useQuery({ queryKey: key, queryFn: () => workspaceToolsApi(code).binSettings(), enabled: Boolean(code), staleTime: 60_000 });
  const canChange = Boolean(code) && scope.hasCapability('access.manage');
  const [open, setOpen] = useState(false);
  const days = q.data?.retentionDays ?? null;
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-2 text-xs text-muted-foreground">
      <span className="inline-flex items-center gap-1.5">
        <Clock className="size-3.5" aria-hidden /> {days ? t('lists.bin.hintRetention', { count: days }) : t('lists.bin.hint')}
      </span>
      {canChange ? (
        <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setOpen(true)} disabled={!q.data}>
          {t('lists.bin.change')}
        </Button>
      ) : null}
      {open ? <RetentionDialog code={code} current={days} onClose={() => setOpen(false)} queryKey={key} /> : null}
    </div>
  );
}

function RetentionDialog({ code, current, onClose, queryKey }: { code: string; current: number | null; onClose: () => void; queryKey: unknown[] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [value, setValue] = useState(current ? String(current) : '');
  const save = useMutation({
    mutationFn: () => workspaceToolsApi(code).saveBinSettings({ retentionDays: value ? Number(value) : null }),
    onSuccess: (r) => {
      qc.setQueryData(queryKey, r);
      toast.success(t('lists.bin.retentionSaved'));
      onClose();
    }
  });
  const turningOn = Boolean(value) && (!current || Number(value) < current);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">{t('lists.bin.retentionTitle')}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('lists.bin.retentionBody')}</DialogDescription>
        </div>
        <div className="space-y-3 px-5 py-4">
          {save.isError ? <Alert tone="danger">{isApiError(save.error) ? Object.values(save.error.fieldErrors)[0] ?? save.error.message : t('common.genericError')}</Alert> : null}
          <Select value={value} onChange={(e) => setValue(e.target.value)} aria-label={t('lists.bin.retentionTitle')}>
            <option value="">{t('lists.bin.keepForever')}</option>
            {CHOICES.map((d) => (
              <option key={d} value={d}>
                {t('lists.bin.deleteAfter', { count: d })}
              </option>
            ))}
          </Select>
          {turningOn ? <Alert tone="warning">{t('lists.bin.retentionWarning', { count: Number(value) })}</Alert> : null}
          <div className="flex justify-end gap-2 pt-1">
            <Button variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button variant={turningOn ? 'danger' : 'primary'} loading={save.isPending} disabled={(current ? String(current) : '') === value} onClick={() => save.mutate()}>
              {t('common.save')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
