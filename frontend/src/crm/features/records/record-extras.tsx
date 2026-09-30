import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Download, FileText, GitMerge, ImageIcon, Mail, Trash2, Upload } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { workspaceToolsApi } from '@crm/api/endpoints';
import type { FieldDef, ObjectKey, ObjectMeta, RecordRow } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { RichTextEditor } from '@crm/components/rich-text';
import { relativeTime } from '@crm/lib/utils';
import { formatValueText } from './field-value';
import { recordKeys } from './use-object-meta';
import { useRecordScope } from './record-scope';

const kb = (n: number) => (n > 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

export function RecordFiles({ object, id, canWrite }: { object: ObjectKey; id: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const key = [...recordKeys.detail(scope.prefix, object, id), 'files'];
  const q = useQuery({ queryKey: key, queryFn: () => scope.api.files(object, id) });
  const upload = useMutation({
    mutationFn: (f: File) => scope.api.uploadFile(object, id, f),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: key });
      void qc.invalidateQueries({ queryKey: [...recordKeys.detail(scope.prefix, object, id), 'timeline'] });
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const remove = useMutation({
    mutationFn: (fid: string) => scope.api.deleteFile(fid),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key })
  });
  const onFiles = (list: FileList | null) => {
    for (const f of Array.from(list ?? []).slice(0, 10)) {
      if (f.size > 10 * 1024 * 1024) toast.error(t('files.tooBig', { name: f.name }));
      else upload.mutate(f);
    }
  };
  return (
    <div className="space-y-3 p-3 sm:p-4">
      {canWrite ? (
        <button
          type="button"
          onClick={() => input.current?.click()}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            onFiles(e.dataTransfer.files);
          }}
          className="flex w-full items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 py-5 text-[13px] text-muted-foreground hover:border-primary/40 hover:bg-primary-soft/40 hover:text-foreground"
        >
          <Upload className="size-4" aria-hidden /> {upload.isPending ? t('files.uploading') : t('files.drop')}
          <input ref={input} type="file" multiple className="hidden" onChange={(e) => onFiles(e.target.files)} />
        </button>
      ) : null}
      {q.isPending ? (
        <Skeleton className="h-16" />
      ) : !q.data?.length ? (
        <p className="py-6 text-center text-[13px] text-muted-foreground">{t('files.empty')}</p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {q.data.map((f) => {
            const image = f.contentType.startsWith('image/') && f.contentType !== 'image/svg+xml';
            return (
              <li key={f.id} className="flex items-center gap-3 px-3 py-2">
                {image ? (
                  <img src={scope.api.fileUrl(f.id, true)} alt="" className="size-10 shrink-0 rounded border object-cover" />
                ) : (
                  <span className="grid size-10 shrink-0 place-items-center rounded border bg-muted text-muted-foreground">
                    {f.contentType.startsWith('image/') ? <ImageIcon className="size-4" /> : <FileText className="size-4" />}
                  </span>
                )}
                <div className="min-w-0 flex-1">
                  <a href={scope.api.fileUrl(f.id, image || f.contentType === 'application/pdf')} target="_blank" rel="noreferrer" className="block truncate text-[13px] font-medium text-primary hover:underline">
                    {f.name}
                  </a>
                  <p className="text-xs text-muted-foreground">
                    {kb(f.size)} · {f.createdBy ? `${f.createdBy} · ` : ''}
                    {relativeTime(f.createdAt)}
                  </p>
                </div>
                <Button variant="subtle" size="icon-sm" asChild>
                  <a href={scope.api.fileUrl(f.id)} aria-label={t('files.download', { name: f.name })}>
                    <Download />
                  </a>
                </Button>
                {canWrite ? (
                  <Button variant="subtle" size="icon-sm" onClick={() => remove.mutate(f.id)} aria-label={t('files.delete', { name: f.name })}>
                    <Trash2 />
                  </Button>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

/** Send an email from a record — from your connected mailbox or the CRM's address. */
export function EmailDialog({ object, record, open, onOpenChange, defaultTo }: { object: ObjectKey; record: RecordRow; open: boolean; onOpenChange: (o: boolean) => void; defaultTo: string }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const boxes = useQuery({ queryKey: ['mailboxes', code], queryFn: () => workspaceToolsApi(code).mailboxes(), enabled: open && Boolean(code) });
  const [to, setTo] = useState(defaultTo);
  const [cc, setCc] = useState('');
  const [subject, setSubject] = useState('');
  const [html, setHtml] = useState('');
  const [mailboxId, setMailboxId] = useState('');
  const [error, setError] = useState<string | null>(null);
  const send = useMutation({
    mutationFn: () => scope.api.sendEmail(object, record.id, { to: [to], cc: cc ? [cc] : [], subject, html, mailboxId: mailboxId || undefined }),
    onSuccess: () => {
      toast.success(t('email.sent'));
      onOpenChange(false);
      setSubject('');
      setHtml('');
      void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, object, record.id) });
    },
    onError: (e) => setError(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const active = (boxes.data?.data ?? []).filter((b) => b.status !== 'paused');
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Mail className="size-4 text-primary" /> {t('email.title', { name: record.title })}
          </DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('email.subtitle')}</DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            send.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label={t('email.from')}>
            <Select value={mailboxId} onChange={(e) => setMailboxId(e.target.value)}>
              <option value="">{t('email.fromCrm')}</option>
              {active.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.email}
                </option>
              ))}
            </Select>
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('email.to')}>
              <Input type="email" required value={to} onChange={(e) => setTo(e.target.value)} />
            </Field>
            <Field label={t('email.cc')}>
              <Input type="email" value={cc} onChange={(e) => setCc(e.target.value)} />
            </Field>
          </div>
          <Field label={t('email.subject')}>
            <Input required maxLength={250} value={subject} onChange={(e) => setSubject(e.target.value)} />
          </Field>
          <Field label={t('email.message')}>
            <RichTextEditor value={html} onChange={setHtml} minHeight={160} ariaLabel={t('email.message')} />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={send.isPending} disabled={!to || !subject}>
              <Mail /> {t('email.send')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Possible duplicates of a record, and merging them into it. */
export function useDuplicates(object: ObjectKey, id: string, enabled: boolean) {
  const scope = useRecordScope();
  return useQuery({ queryKey: [...recordKeys.detail(scope.prefix, object, id), 'duplicates'], queryFn: () => scope.api.duplicates(object, id), enabled, staleTime: 60_000 });
}

export function MergeDialog({ object, meta, record, open, onOpenChange }: { object: ObjectKey; meta: ObjectMeta; record: RecordRow; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const dups = useDuplicates(object, record.id, open);
  const [chosen, setChosen] = useState<string | null>(null);
  const other = useQuery({ queryKey: recordKeys.detail(scope.prefix, object, chosen ?? '-'), queryFn: () => scope.api.get(object, chosen!), enabled: Boolean(chosen) });
  const [pick, setPick] = useState<Record<string, 'this' | 'other'>>({});
  const fields = meta.fields.filter((f) => !f.readOnly && f.type !== 'files');
  const o = other.data?.record;
  const differs = (f: FieldDef) => o && JSON.stringify(o.values[f.key] ?? null) !== JSON.stringify(record.values[f.key] ?? null) && o.values[f.key] != null;
  const merge = useMutation({
    mutationFn: (values: Record<string, unknown>) => scope.api.merge(object, { primaryId: record.id, duplicateIds: [chosen!], values }),
    onSuccess: () => {
      toast.success(t('merge.done'));
      onOpenChange(false);
      setChosen(null);
      void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) });
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <GitMerge className="size-4 text-primary" /> {t('merge.title', { name: record.title })}
          </DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('merge.subtitle')}</DialogDescription>
        </div>
        <div className="max-h-[60vh] space-y-3 overflow-y-auto px-5 py-4">
          {!chosen ? (
            dups.isPending ? (
              <Skeleton className="h-16" />
            ) : !dups.data?.length ? (
              <p className="py-6 text-center text-[13px] text-muted-foreground">{t('merge.none')}</p>
            ) : (
              <ul className="divide-y rounded-lg border">
                {dups.data.map((d) => (
                  <li key={d.id} className="flex items-center justify-between gap-3 px-3 py-2">
                    <div>
                      <p className="text-[13px] font-medium">{d.title}</p>
                      <p className="font-mono text-[11px] text-muted-foreground">{d.code}</p>
                    </div>
                    <Button size="sm" variant="outline" onClick={() => setChosen(d.id)}>
                      {t('merge.compare')}
                    </Button>
                  </li>
                ))}
              </ul>
            )
          ) : !o ? (
            <Skeleton className="h-40" />
          ) : (
            <>
              <p className="text-[13px] text-muted-foreground">{t('merge.pickValues')}</p>
              <table className="w-full text-[13px]">
                <thead className="text-xs text-muted-foreground">
                  <tr>
                    <th className="py-1 text-left font-medium">{t('merge.field')}</th>
                    <th className="py-1 text-left font-medium">{t('merge.keep')} ({record.code})</th>
                    <th className="py-1 text-left font-medium">{t('merge.other')} ({o.code})</th>
                  </tr>
                </thead>
                <tbody>
                  {fields.filter((f) => differs(f)).map((f) => (
                    <tr key={f.key} className="border-t">
                      <td className="py-1.5 pr-2 font-medium">{f.label}</td>
                      {(['this', 'other'] as const).map((side) => {
                        const rec = side === 'this' ? record : o;
                        const on = (pick[f.key] ?? (record.values[f.key] == null ? 'other' : 'this')) === side;
                        return (
                          <td key={side} className="py-1.5 pr-2">
                            <label className="flex cursor-pointer items-center gap-2">
                              <input type="radio" name={`m-${f.key}`} checked={on} onChange={() => setPick((p) => ({ ...p, [f.key]: side }))} />
                              <span className="truncate">{formatValueText(f, rec.values[f.key], rec.lookups[f.key])}</span>
                            </label>
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
              {!fields.some((f) => differs(f)) ? <p className="text-[13px] text-muted-foreground">{t('merge.same')}</p> : null}
              <Alert tone="info">{t('merge.what', { code: o.code })}</Alert>
            </>
          )}
        </div>
        <div className="flex justify-between gap-2 border-t px-5 py-3">
          {chosen ? (
            <Button variant="ghost" onClick={() => setChosen(null)}>
              {t('merge.back')}
            </Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            {chosen && o ? (
              <Button loading={merge.isPending} onClick={() => {
                // A field empty here takes the other record's value unless you chose otherwise.
                const values: Record<string, unknown> = {};
                for (const f of fields) {
                  const side = pick[f.key] ?? (record.values[f.key] == null ? 'other' : 'this');
                  if (side === 'other' && o.values[f.key] != null) values[f.key] = o.values[f.key];
                }
                merge.mutate(values);
              }}>
                <GitMerge /> {t('merge.merge')}
              </Button>
            ) : null}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
