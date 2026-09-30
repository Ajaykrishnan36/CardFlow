import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Alert } from '@crm/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';
import { FieldEditor } from './field-input';
import { fieldIndex, isEmptyValue, objectIcon, recordKeys } from './use-object-meta';
import { recordHref, useRecordScope } from './record-scope';

interface FormSection {
  id: string;
  title: string;
  columns: 1 | 2;
  fields: FieldDef[];
}

function initialValues(object: ObjectKey, meta: ObjectMeta): Record<string, unknown> {
  const byKey = fieldIndex(meta);
  const v: Record<string, unknown> = {};
  if (object === 'leads') {
    if (byKey.has('status')) v.status = 'new';
    if (byKey.has('source')) v.source = 'manual';
  }
  return v;
}

export function NewRecordDialog({
  object,
  meta,
  open,
  onOpenChange,
  note,
  defaults
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Extra line under the subtitle (e.g. "Created in your Platform CRM"). */
  note?: string;
  /** Values to start with (a board column's stage, a calendar day…). */
  defaults?: Record<string, unknown>;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const Icon = objectIcon(object, meta?.icon);

  useEffect(() => {
    if (!open) return;
    setValues({ ...initialValues(object, meta), ...(defaults ?? {}) });
    setErrors({});
    setFormError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, object, meta]);

  const sections = useMemo<FormSection[]>(() => {
    const byKey = fieldIndex(meta);
    const used = new Set<string>();
    const out: FormSection[] = [];
    for (const s of meta.layout.sections) {
      const fields = s.fields.map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f && !f.readOnly));
      fields.forEach((f) => used.add(f.key));
      if (fields.length) out.push({ id: s.id, title: s.title, columns: s.columns, fields });
    }
    // A required field hidden from the layout would make create impossible — surface it anyway.
    const missing = meta.fields.filter((f) => f.required && !f.readOnly && !used.has(f.key));
    if (missing.length) out.push({ id: '__required', title: t('records.new.otherRequired'), columns: 2, fields: missing });
    return out;
  }, [meta, t]);

  const create = useMutation({
    mutationFn: (body: Record<string, unknown>) => scope.api.create(object, body),
    onSuccess: (row) => {
      toast.success(t('records.new.created', { object: meta.labelSingular, title: row.title }));
      void qc.invalidateQueries({ queryKey: recordKeys.lists(scope.prefix, object) });
      void qc.invalidateQueries({ queryKey: scope.dashboardKey });
      onOpenChange(false);
      if (scope.can(object, 'read')) navigate(recordHref(scope, object, row.id));
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        setErrors(e.fieldErrors);
        const shown = new Set(sections.flatMap((s) => s.fields.map((f) => f.key)));
        const other = Object.entries(e.fieldErrors).filter(([k]) => !shown.has(k));
        setFormError(other.length ? other.map(([k, m]) => `${fieldIndex(meta).get(k)?.label ?? k}: ${m}`).join(' · ') : t('records.new.fixErrors'));
      } else {
        setFormError(isApiError(e) ? e.message : t('common.genericError'));
      }
    }
  });

  const set = (key: string, v: unknown) => {
    setValues((prev) => ({ ...prev, [key]: v }));
    if (errors[key]) setErrors(({ [key]: _removed, ...rest }) => rest);
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const nextErrors: Record<string, string> = {};
    for (const s of sections) for (const f of s.fields) if (f.required && isEmptyValue(values[f.key])) nextErrors[f.key] = t('common.required');
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length) {
      setFormError(t('records.new.fixErrors'));
      return;
    }
    setFormError(null);
    const body: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(values)) if (!isEmptyValue(v)) body[k] = v;
    create.mutate(body);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !create.isPending && onOpenChange(o)}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-3xl flex-col p-0 sm:top-[6vh] sm:max-h-[88vh]">
        <div className="flex items-center gap-3 border-b px-5 py-4 pr-12">
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
            <Icon className="size-4" aria-hidden />
          </span>
          <div className="min-w-0">
            <DialogTitle className="text-base font-semibold">{t('records.new.title', { object: meta.labelSingular })}</DialogTitle>
            <DialogDescription className="text-[13px] text-muted-foreground">
              {t('records.new.subtitle')}
              {note ? <span className="mt-0.5 block font-medium text-foreground/80">{note}</span> : null}
            </DialogDescription>
          </div>
        </div>
        <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col" noValidate>
          <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-5">
            {formError ? <Alert tone="danger">{formError}</Alert> : null}
            {sections.length === 0 ? <p className="text-[13px] text-muted-foreground">{t('records.new.noFields')}</p> : null}
            {sections.map((s) => (
              <fieldset key={s.id} className="min-w-0">
                <legend className="mb-3 w-full border-b pb-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{s.title}</legend>
                <div className={cn('grid gap-x-5 gap-y-4', s.columns === 2 && 'sm:grid-cols-2')}>
                  {s.fields.map((f) => (
                    <FieldEditor key={f.key} field={f} value={values[f.key]} onChange={(v) => set(f.key, v)} error={errors[f.key]} disabled={create.isPending} />
                  ))}
                </div>
              </fieldset>
            ))}
            {/* Room for an open lookup list at the bottom of the scroll area. */}
            <div className="h-24" aria-hidden />
          </div>
          <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={create.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={create.isPending}>
              {t('records.new.submit', { object: meta.labelSingular })}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
