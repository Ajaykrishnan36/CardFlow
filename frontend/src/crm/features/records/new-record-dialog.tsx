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
  // A record starts at the first step of its life (Scheduled, New, Draft…), not at "None".
  if (meta.statusField && byKey.has(meta.statusField) && v[meta.statusField] === undefined && meta.statuses?.[0]) v[meta.statusField] = meta.statuses[0].value;
  // What the business set under Rules & defaults wins over the built-in starting values.
  for (const [k, d] of Object.entries(meta.defaults ?? {})) if (byKey.has(k)) v[k] = d;
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
  // Labels of lookups the form filled in by itself (the picker only knows what was picked in it).
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const Icon = objectIcon(object, meta?.icon);

  useEffect(() => {
    if (!open) return;
    setValues({ ...initialValues(object, meta), ...(defaults ?? {}) });
    setLabels({});
    setErrors({});
    setFormError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, object, meta]);

  const sections = useMemo<FormSection[]>(() => {
    const byKey = fieldIndex(meta);
    const used = new Set<string>();
    const out: FormSection[] = [];
    for (const s of meta.layout.sections) {
      const later = new Set(meta.createHidden ?? []);
      const fields = s.fields.map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f && !f.readOnly && (!later.has(f.key) || (f.required && f.key !== 'name'))));
      fields.forEach((f) => used.add(f.key));
      if (fields.length) out.push({ id: s.id, title: s.title, columns: s.columns, fields });
    }
    // A required field hidden from the layout would make create impossible — surface it anyway.
    const selfNamed = (meta.createHidden ?? []).includes('name');
    const missing = meta.fields.filter((f) => f.required && !f.readOnly && !used.has(f.key) && !(selfNamed && f.key === 'name'));
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
    // A hand-picked value carries its own label; forget the one the form filled in.
    if (labels[key] !== undefined) setLabels(({ [key]: _old, ...rest }) => rest);
    if (errors[key]) setErrors(({ [key]: _removed, ...rest }) => rest);
  };

  // Choosing a customer, a vehicle or any other record fills the empty fields it answers:
  // a contact brings its account, an asset its owner, product and contract.
  const fillFrom = async (field: FieldDef, picked: { id: string; label: string } | null) => {
    if (!picked || !field.lookup) return;
    try {
      const related = await scope.api.lookupRelated(field.lookup, picked.id);
      const byKey = fieldIndex(meta);
      const usable = (k: string, v: { object?: string }) => {
        const f = byKey.get(k);
        return Boolean(f && f.key !== field.key && !f.readOnly && f.type === 'lookup' && f.lookup === v.object);
      };
      // Who the record is for must agree: the customer follows the last thing picked.
      // Picking a contact (or a vehicle, a case…) sets its account; other fields are
      // only filled when empty.
      const person = (k: string) => k === 'accountId' || k === 'contactId';
      const current = values;
      const set: Record<string, { id: string; label: string } | null> = {};
      for (const [k, v] of Object.entries(related)) {
        if (!usable(k, v)) continue;
        if (isEmptyValue(current[k]) || (person(k) && current[k] !== v.id)) set[k] = v;
      }
      // An account was picked and the contact on the form belongs elsewhere: drop it
      // (or replace it, when the account has exactly one contact — handled above).
      if (field.key === 'accountId' && !set.contactId && !isEmptyValue(current.contactId) && byKey.get('contactId')?.lookup === 'contacts') {
        const theirs = await scope.api.lookupRelated('contacts', String(current.contactId));
        if (theirs.accountId?.id !== picked.id) set.contactId = null;
      }
      const keys = Object.keys(set);
      if (!keys.length) return;
      setValues((prev) => ({ ...prev, ...Object.fromEntries(keys.map((k) => [k, set[k]?.id ?? null])) }));
      setLabels((prev) => ({ ...prev, ...Object.fromEntries(keys.map((k) => [k, set[k]?.label ?? ''])) }));
      const filled = keys.filter((k) => set[k]);
      const cleared = keys.filter((k) => !set[k]);
      if (filled.length) toast.message(t('records.input.filledFrom', { name: picked.label, fields: filled.map((k) => byKey.get(k)?.label ?? k).join(', ') }));
      if (cleared.length) toast.message(t('records.input.clearedFor', { name: picked.label, fields: cleared.map((k) => byKey.get(k)?.label ?? k).join(', ') }));
    } catch {
      // Filling is a convenience: the person can still pick the other fields by hand.
    }
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
                    <FieldEditor
                      key={f.key}
                      field={f}
                      value={values[f.key]}
                      onChange={(v) => set(f.key, v)}
                      error={errors[f.key]}
                      disabled={create.isPending}
                      lookupLabel={labels[f.key]}
                      lookupContext={{ accountId: values.accountId as string | undefined, contactId: values.contactId as string | undefined }}
                      onPick={(v) => void fillFrom(f, v)}
                    />
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
