import { useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Plus, Trash2 } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldCreateBody, FieldDef, FieldType, ObjectKey, ObjectMeta, Option } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Alert } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { FIELD_KEY_RE, toFieldKey } from './use-object-meta';
import { useRecordScope } from './record-scope';

type CreatableType = FieldType;

/** Field types a customizer can add, grouped the way the type picker shows them. */
export const CREATABLE_TYPES: CreatableType[] = [
  'text',
  'textarea',
  'richtext',
  'fullName',
  'email',
  'emails',
  'phone',
  'phones',
  'url',
  'links',
  'address',
  'number',
  'currency',
  'percent',
  'rating',
  'date',
  'datetime',
  'select',
  'multiselect',
  'boolean',
  'lookup',
  'relations',
  'files',
  'json'
];

const UNIQUE_TYPES = new Set<FieldType>(['text', 'email', 'phone', 'url', 'number']);
const LINK_TYPES = new Set<FieldType>(['lookup', 'relations']);

interface OptionRow {
  value: string;
  label: string;
  valueTouched: boolean;
  /** Existing option of a saved field: its value is stored on records, so it can't change. */
  locked?: boolean;
}

export type FieldDialogState = { mode: 'create'; sectionId?: string } | { mode: 'edit'; field: FieldDef } | null;

export function FieldDialog({
  object,
  meta,
  sections,
  persistedSectionIds,
  state,
  onClose,
  onCreated,
  onUpdated
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  /** Sections of the editor's current (possibly unsaved) draft. */
  sections: Array<{ id: string; title: string }>;
  /** Sections that exist server-side; a new field can only be placed there by the API. */
  persistedSectionIds: Set<string>;
  state: FieldDialogState;
  onClose: () => void;
  onCreated: (meta: ObjectMeta, key: string, sectionId: string | undefined) => void;
  onUpdated: (meta: ObjectMeta) => void;
}) {
  const { t } = useTranslation();
  const { api: recordsApi } = useRecordScope();
  const open = state !== null;
  const editing = state?.mode === 'edit' ? state.field : null;

  const [label, setLabel] = useState('');
  const [key, setKey] = useState('');
  const [keyTouched, setKeyTouched] = useState(false);
  const [type, setType] = useState<CreatableType>('text');
  const [options, setOptions] = useState<OptionRow[]>([]);
  const [required, setRequired] = useState(false);
  const [unique, setUnique] = useState(false);
  const [lookup, setLookup] = useState('');
  const [helpText, setHelpText] = useState('');
  const [sectionId, setSectionId] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!state) return;
    setErrors({});
    setFormError(null);
    if (state.mode === 'edit') {
      const f = state.field;
      setLabel(f.label);
      setKey(f.key);
      setKeyTouched(true);
      setType(f.type);
      setOptions((f.options ?? []).map((o) => ({ ...o, valueTouched: true, locked: true })));
      setRequired(f.required);
      setUnique(Boolean(f.unique));
      setLookup(f.lookup ?? '');
      setHelpText(f.helpText ?? '');
      setSectionId('');
    } else {
      setLabel('');
      setKey('');
      setKeyTouched(false);
      setType('text');
      setOptions([]);
      setRequired(false);
      setUnique(false);
      setLookup('');
      setHelpText('');
      setSectionId(state.sectionId ?? sections[0]?.id ?? '');
    }
    // Reset only when the dialog (re)opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state]);

  const hasOptions = type === 'select' || type === 'multiselect';
  const isLink = LINK_TYPES.has(type);
  const canBeUnique = UNIQUE_TYPES.has(type);
  const targets = (meta.lookupTargets ?? []).filter((x) => x.key !== 'products' && x.key !== 'workspaces');

  useEffect(() => {
    if (hasOptions && options.length === 0) setOptions([{ value: '', label: '', valueTouched: false }]);
  }, [hasOptions, options.length]);

  const mutation = useMutation({
    mutationFn: async (): Promise<{ meta: ObjectMeta; key: string }> => {
      const opts: Option[] | undefined = hasOptions ? options.filter((o) => o.label.trim()).map((o) => ({ value: o.value.trim(), label: o.label.trim() })) : undefined;
      if (editing) {
        const m = await recordsApi.updateField(object, editing.key, { label: label.trim(), required, unique: canBeUnique ? unique : undefined, options: opts, helpText: helpText.trim() || undefined });
        return { meta: m, key: editing.key };
      }
      const body: FieldCreateBody = {
        label: label.trim(),
        key,
        type,
        required,
        unique: canBeUnique && unique ? true : undefined,
        lookup: isLink ? lookup : undefined,
        options: opts,
        helpText: helpText.trim() || undefined,
        sectionId: persistedSectionIds.has(sectionId) ? sectionId : undefined
      };
      const m = await recordsApi.createField(object, body);
      const known = new Set(meta.fields.map((f) => f.key));
      const created = m.fields.find((f) => f.key === key) ?? m.fields.find((f) => !known.has(f.key));
      return { meta: m, key: created?.key ?? key };
    },
    onSuccess: ({ meta: m, key: k }) => {
      if (editing) {
        toast.success(t('records.fieldDialog.updated', { label: label.trim() }));
        onUpdated(m);
      } else {
        toast.success(t('records.fieldDialog.created', { label: label.trim() }));
        onCreated(m, k, sectionId || undefined);
      }
      onClose();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        setErrors(e.fieldErrors);
        setFormError(e.message);
      } else setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next: Record<string, string> = {};
    if (!label.trim()) next.label = t('common.required');
    if (!editing) {
      if (!FIELD_KEY_RE.test(key)) next.key = t('records.fieldDialog.keyInvalid');
      else if (meta.fields.some((f) => f.key === key)) next.key = t('records.fieldDialog.keyTaken');
    }
    if (!editing && isLink && !lookup) next.lookup = t('records.fieldDialog.lookupRequired');
    if (hasOptions) {
      const filled = options.filter((o) => o.label.trim());
      if (filled.length === 0) next.options = t('records.fieldDialog.optionsRequired');
      else if (filled.some((o) => !o.value.trim())) next.options = t('records.fieldDialog.optionValueRequired');
      else if (new Set(filled.map((o) => o.value.trim())).size !== filled.length) next.options = t('records.fieldDialog.optionsDuplicate');
    }
    setErrors(next);
    if (Object.keys(next).length) return;
    setFormError(null);
    mutation.mutate();
  };

  const updateOption = (i: number, patch: Partial<OptionRow>) =>
    setOptions((rows) =>
      rows.map((r, j) => {
        if (j !== i) return r;
        const n = { ...r, ...patch };
        if (patch.label !== undefined && !n.valueTouched) n.value = toFieldKey(patch.label);
        return n;
      })
    );

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !mutation.isPending && onClose()}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-xl flex-col p-0 sm:top-[8vh] sm:max-h-[84vh]">
        <div className="border-b px-5 py-4 pr-12">
          <DialogTitle className="text-base font-semibold">{editing ? t('records.fieldDialog.editTitle', { label: editing.label }) : t('records.fieldDialog.newTitle')}</DialogTitle>
          <DialogDescription className="text-[13px] text-muted-foreground">
            {editing ? t('records.fieldDialog.editBody') : t('records.fieldDialog.newBody', { object: meta.labelSingular.toLowerCase() })}
          </DialogDescription>
        </div>
        <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-5">
            {formError ? <Alert tone="danger">{formError}</Alert> : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t('records.fieldDialog.label')} error={errors.label}>
                <Input
                  value={label}
                  autoFocus
                  maxLength={80}
                  onChange={(e) => {
                    setLabel(e.target.value);
                    if (!keyTouched) setKey(toFieldKey(e.target.value));
                  }}
                />
              </Field>
              <Field label={t('records.fieldDialog.key')} error={errors.key} hint={editing ? t('records.fieldDialog.keyLocked') : t('records.fieldDialog.keyHint')}>
                <Input
                  className="font-mono"
                  value={key}
                  disabled={Boolean(editing)}
                  maxLength={40}
                  onChange={(e) => {
                    setKey(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ''));
                    setKeyTouched(true);
                  }}
                />
              </Field>
              <Field label={t('records.fieldDialog.type')} error={errors.type} hint={editing ? t('records.fieldDialog.typeLocked') : t(`records.typeHints.${type}`)}>
                <Select
                  value={type}
                  disabled={Boolean(editing)}
                  onChange={(e) => setType(e.target.value as CreatableType)}
                  options={CREATABLE_TYPES.map((ty) => ({ value: ty, label: t(`records.types.${ty}`) }))}
                />
              </Field>
              {isLink ? (
                <Field label={t('records.fieldDialog.lookup')} error={errors.lookup} hint={editing ? t('records.fieldDialog.lookupLocked') : t('records.fieldDialog.lookupHint')}>
                  <Select
                    value={lookup}
                    disabled={Boolean(editing)}
                    onChange={(e) => setLookup(e.target.value)}
                    placeholder={t('records.fieldDialog.lookupPlaceholder')}
                    options={targets.map((x) => ({ value: x.key, label: x.label }))}
                  />
                </Field>
              ) : null}
              {!editing ? (
                <Field label={t('records.fieldDialog.section')} error={errors.sectionId}>
                  <Select
                    value={sectionId}
                    onChange={(e) => setSectionId(e.target.value)}
                    options={sections.map((s) => ({ value: s.id, label: s.title || t('records.layout.untitledSection') }))}
                    placeholder={sections.length ? undefined : t('records.fieldDialog.noSection')}
                  />
                </Field>
              ) : null}
            </div>

            {hasOptions ? (
              <fieldset>
                <legend className="mb-2 text-[13px] font-medium text-foreground">{t('records.fieldDialog.options')}</legend>
                <div className="space-y-2">
                  <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2 text-xs text-muted-foreground">
                    <span>{t('records.fieldDialog.optionLabel')}</span>
                    <span>{t('records.fieldDialog.optionValue')}</span>
                  </div>
                  {options.map((o, i) => (
                    <div key={i} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] items-center gap-2">
                      <Input value={o.label} onChange={(e) => updateOption(i, { label: e.target.value })} aria-label={t('records.fieldDialog.optionLabelN', { n: i + 1 })} />
                      <Input
                        className="font-mono"
                        value={o.value}
                        disabled={o.locked}
                        onChange={(e) => updateOption(i, { value: e.target.value, valueTouched: true })}
                        aria-label={t('records.fieldDialog.optionValueN', { n: i + 1 })}
                      />
                      <Button
                        type="button"
                        variant="subtle"
                        size="icon-sm"
                        onClick={() => setOptions((rows) => rows.filter((_, j) => j !== i))}
                        aria-label={t('records.fieldDialog.removeOption', { n: i + 1 })}
                        disabled={options.length <= 1}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  ))}
                  <Button type="button" variant="ghost" size="sm" onClick={() => setOptions((rows) => [...rows, { value: '', label: '', valueTouched: false }])}>
                    <Plus /> {t('records.fieldDialog.addOption')}
                  </Button>
                  {errors.options ? <p className="text-[13px] text-danger">{errors.options}</p> : null}
                </div>
              </fieldset>
            ) : null}

            <Field label={t('records.fieldDialog.helpText')} error={errors.helpText} hint={t('records.fieldDialog.helpTextHint')}>
              <Textarea rows={2} maxLength={300} value={helpText} onChange={(e) => setHelpText(e.target.value)} />
            </Field>
            <Checkbox checked={required} onCheckedChange={setRequired} label={t('records.fieldDialog.required')} description={t('records.fieldDialog.requiredHint')} />
            {canBeUnique ? (
              <div>
                <Checkbox checked={unique} onCheckedChange={setUnique} label={t('records.fieldDialog.unique')} description={t('records.fieldDialog.uniqueHint', { objects: meta.labelPlural.toLowerCase() })} />
                {errors.unique ? <p className="mt-1 text-[13px] text-danger">{errors.unique}</p> : null}
              </div>
            ) : null}
          </div>
          <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="outline" onClick={onClose} disabled={mutation.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={mutation.isPending}>
              {editing ? t('records.fieldDialog.saveField') : t('records.fieldDialog.createField')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
