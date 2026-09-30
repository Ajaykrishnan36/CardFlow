import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Archive, ArrowDown, ArrowUp, Code2, Pencil, Plus, RotateCcw, Trash2, X } from 'lucide-react';
import { objectsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { FieldType, ObjectDefinition, ObjectDefinitionBody, ObjectFieldDef, StatusOption } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, PageContainer } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { FIELD_TYPES, UNIQUE_FIELD_TYPES, fieldErrorText, isLinkField, IconPicker, ObjectIcon, objectKeys } from './object-utils';

const TONES: StatusOption['tone'][] = ['neutral', 'primary', 'success', 'warning', 'danger'];
const toneDot: Record<StatusOption['tone'], string> = {
  neutral: 'bg-muted-foreground',
  primary: 'bg-primary',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger'
};

/** /crm/owner/objects/:key — build an object: labels, statuses, fields; see its API. */
export function ObjectDetailPage() {
  const { key = '' } = useParams();
  return <ObjectDetailView key={key} objectKey={key} />;
}

function ObjectDetailView({ objectKey }: { objectKey: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: objectKeys.one(objectKey), queryFn: () => objectsApi.get(objectKey) });
  const catalog = useQuery({ queryKey: objectKeys.all, queryFn: objectsApi.list });
  const d = q.data;
  useDocumentTitle(d ? d.plural : t('objects.title'));
  const [confirmArchive, setConfirmArchive] = useState(false);

  const save = useMutation({
    mutationFn: (body: ObjectDefinitionBody) => objectsApi.update(objectKey, body),
    onSuccess: (next) => {
      qc.setQueryData(objectKeys.one(objectKey), next);
      void qc.invalidateQueries({ queryKey: objectKeys.all });
      void qc.invalidateQueries({ queryKey: ['records'] });
      toast.success(t('objects.savedToast', { name: next.plural }));
    },
    onError: (e) => toast.error(fieldErrorText(e, t('common.genericError')))
  });
  const setStatus = useMutation({
    mutationFn: (archive: boolean) => (archive ? objectsApi.archive(objectKey) : objectsApi.restore(objectKey)),
    onSuccess: (next) => {
      qc.setQueryData(objectKeys.one(objectKey), next);
      void qc.invalidateQueries({ queryKey: objectKeys.all });
      toast.success(t(next.status === 'archived' ? 'objects.archivedToast' : 'objects.restoredToast', { name: next.plural }));
      setConfirmArchive(false);
    }
  });

  if (q.isError) {
    return (
      <PageContainer>
        <ErrorState
          title={isApiError(q.error) && q.error.status === 404 ? t('objects.notFound') : t('objects.loadError')}
          message={isApiError(q.error) ? q.error.message : undefined}
          onRetry={() => void q.refetch()}
        />
      </PageContainer>
    );
  }
  if (!d) {
    return (
      <PageContainer>
        <Skeleton className="h-4 w-40" />
        <Skeleton className="mt-3 h-28 w-full" />
        <Skeleton className="mt-4 h-64 w-full" />
      </PageContainer>
    );
  }

  return (
    <PageContainer>
      <Breadcrumbs items={[{ label: t('objects.title'), to: '/crm/owner/objects' }, { label: d.plural }]} />
      <Card className="mt-3 p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
          <ObjectIcon icon={d.icon} className="size-12" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-xl font-semibold tracking-tight">{d.plural}</h1>
              <Badge tone={d.standard ? 'neutral' : 'primary'}>{d.standard ? t('objects.standardBadge') : t('objects.customBadge')}</Badge>
              {d.status === 'archived' ? <Badge tone="warning">{t('objects.archived')}</Badge> : null}
            </div>
            <p className="mt-1 text-[13px] text-muted-foreground">{d.description || t('objects.noDescription')}</p>
            <p className="mt-1.5 text-xs text-muted-foreground">
              {t('objects.apiName')}: <span className="font-mono text-foreground">{d.key}</span> · {t('objects.prefix')}:{' '}
              <span className="font-mono text-foreground">{d.prefix}-000001</span> · {t('objects.recordsCount', { count: d.records })}
            </p>
          </div>
          {d.status === 'archived' ? (
            <Button variant="outline" onClick={() => setStatus.mutate(false)} loading={setStatus.isPending}>
              <RotateCcw /> {t('objects.restore')}
            </Button>
          ) : (
            <Button variant="outline" onClick={() => setConfirmArchive(true)}>
              <Archive /> {t('objects.archive')}
            </Button>
          )}
        </div>
      </Card>

      {d.status === 'archived' ? (
        <Alert tone="warning" className="mt-4">
          {t('objects.archivedBody')}
        </Alert>
      ) : null}

      <div className="mt-4 grid gap-4 lg:grid-cols-5">
        <div className="space-y-4 lg:col-span-3">
          <FieldsCard def={d} targets={catalog.data?.lookupTargets ?? []} saving={save.isPending} onSave={(fields) => save.mutate({ fields })} />
          <StatusesCard def={d} saving={save.isPending} onSave={(statuses, statusLabel) => save.mutate({ statuses, statusLabel })} />
        </div>
        <div className="space-y-4 lg:col-span-2">
          <DetailsCard def={d} icons={catalog.data?.icons ?? [d.icon]} saving={save.isPending} onSave={(b) => save.mutate(b)} />
          <ApiCard def={d} />
        </div>
      </div>

      <ConfirmDialog
        open={confirmArchive}
        onOpenChange={setConfirmArchive}
        title={t('objects.archiveTitle', { name: d.plural })}
        body={t('objects.archiveBody')}
        confirmLabel={t('objects.archive')}
        tone="danger"
        loading={setStatus.isPending}
        onConfirm={() => setStatus.mutate(true)}
      />
    </PageContainer>
  );
}

function DetailsCard({ def, icons, saving, onSave }: { def: ObjectDefinition; icons: string[]; saving: boolean; onSave: (b: ObjectDefinitionBody) => void }) {
  const { t } = useTranslation();
  const initial = useMemo(
    () => ({ singular: def.singular, plural: def.plural, description: def.description, icon: def.icon, nameLabel: def.nameLabel }),
    [def]
  );
  const [form, setForm] = useState(initial);
  useEffect(() => setForm(initial), [initial]);
  const dirty = JSON.stringify(form) !== JSON.stringify(initial);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSave(form);
  };
  return (
    <Card>
      <CardHeader title={t('objects.details')} />
      <form onSubmit={submit} noValidate className="space-y-4 px-4 pb-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('objects.singular')}>
            <Input value={form.singular} onChange={(e) => setForm({ ...form, singular: e.target.value })} maxLength={60} />
          </Field>
          <Field label={t('objects.plural')}>
            <Input value={form.plural} onChange={(e) => setForm({ ...form, plural: e.target.value })} maxLength={60} />
          </Field>
        </div>
        <Field label={t('objects.nameLabel')} hint={t('objects.nameLabelHint')}>
          <Input value={form.nameLabel} onChange={(e) => setForm({ ...form, nameLabel: e.target.value })} maxLength={60} />
        </Field>
        <Field label={t('objects.description')}>
          <Textarea value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} rows={2} maxLength={300} />
        </Field>
        <div>
          <p className="mb-1.5 text-[13px] font-medium">{t('objects.icon')}</p>
          <IconPicker icons={icons} value={form.icon} onChange={(icon) => setForm({ ...form, icon })} />
        </div>
        <div className="flex justify-end">
          <Button type="submit" size="sm" disabled={!dirty} loading={saving && dirty}>
            {t('objects.saveDetails')}
          </Button>
        </div>
      </form>
    </Card>
  );
}

function StatusesCard({ def, saving, onSave }: { def: ObjectDefinition; saving: boolean; onSave: (s: StatusOption[], label: string) => void }) {
  const { t } = useTranslation();
  const [list, setList] = useState<StatusOption[]>(def.statuses);
  const [label, setLabel] = useState(def.statusLabel ?? '');
  useEffect(() => {
    setList(def.statuses);
    setLabel(def.statusLabel ?? '');
  }, [def]);
  const dirty = JSON.stringify(list) !== JSON.stringify(def.statuses) || label !== (def.statusLabel ?? '');
  const update = (i: number, patch: Partial<StatusOption>) => setList((l) => l.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  return (
    <Card>
      <CardHeader title={t('objects.statuses')} description={t('objects.statusesBody')} />
      <div className="space-y-3 px-4 pb-4">
        {list.length ? (
          <Field label={t('objects.statusLabel')}>
            <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t('objects.statusLabelPlaceholder')} maxLength={60} />
          </Field>
        ) : null}
        <ul className="space-y-2">
          {list.map((s, i) => (
            <li key={i} className="flex items-center gap-2">
              <span className={cn('size-2.5 shrink-0 rounded-full', toneDot[s.tone])} aria-hidden />
              <Input value={s.label} onChange={(e) => update(i, { label: e.target.value })} className="flex-1" maxLength={60} aria-label={t('objects.statusName')} />
              <Select
                value={s.tone}
                onChange={(e) => update(i, { tone: e.target.value as StatusOption['tone'] })}
                options={TONES.map((x) => ({ value: x, label: t(`objects.tones.${x}`) }))}
                className="w-32"
                aria-label={t('objects.statusColour')}
              />
              <Button type="button" variant="ghost" size="icon" aria-label={t('objects.remove')} onClick={() => setList((l) => l.filter((_, j) => j !== i))}>
                <X />
              </Button>
            </li>
          ))}
        </ul>
        <div className="flex items-center justify-between gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => setList((l) => [...l, { value: '', label: '', tone: 'neutral' }])}>
            <Plus /> {t('objects.addStatus')}
          </Button>
          <Button
            size="sm"
            disabled={!dirty || list.some((s) => !s.label.trim())}
            loading={saving && dirty}
            onClick={() => onSave(list.map((s) => ({ ...s, label: s.label.trim() })), label.trim())}
          >
            {t('objects.saveStatuses')}
          </Button>
        </div>
      </div>
    </Card>
  );
}

function FieldsCard({
  def,
  targets,
  saving,
  onSave
}: {
  def: ObjectDefinition;
  targets: Array<{ key: string; label: string }>;
  saving: boolean;
  onSave: (f: ObjectFieldDef[]) => void;
}) {
  const { t } = useTranslation();
  const [list, setList] = useState<ObjectFieldDef[]>(def.fields);
  const [editing, setEditing] = useState<number | 'new' | null>(null);
  useEffect(() => setList(def.fields), [def]);
  const dirty = JSON.stringify(list) !== JSON.stringify(def.fields);
  const move = (i: number, by: number) =>
    setList((l) => {
      const n = [...l];
      const j = i + by;
      if (j < 0 || j >= n.length) return l;
      [n[i], n[j]] = [n[j]!, n[i]!];
      return n;
    });
  const targetLabel = (k?: string) => targets.find((x) => x.key === k)?.label ?? k;
  return (
    <Card className="overflow-hidden">
      <CardHeader
        title={t('objects.fields')}
        description={t('objects.fieldsBody')}
        actions={
          <Button size="sm" variant="outline" onClick={() => setEditing('new')}>
            <Plus /> {t('objects.addField')}
          </Button>
        }
      />
      <ul className="divide-y border-t">
        <li className="flex items-center gap-3 bg-muted/30 px-4 py-2.5 text-[13px]">
          <span className="min-w-0 flex-1 font-medium">{def.nameLabel}</span>
          <Badge tone="neutral">{t('records.types.text')}</Badge>
          <span className="w-24 text-right text-xs text-muted-foreground">{t('objects.requiredAlways')}</span>
        </li>
        {list.map((f, i) => (
          <li key={f.key || i} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">
                {f.label}
                {f.required ? <span className="text-danger"> *</span> : null}
              </p>
              <p className="truncate font-mono text-[11px] text-muted-foreground">{f.key || t('objects.newField')}</p>
            </div>
            <Badge tone={f.type === 'lookup' ? 'primary' : 'neutral'}>
              {isLinkField(f.type) ? t('objects.linksTo', { name: targetLabel(f.lookup) }) : t(`records.types.${f.type}`)}
            </Badge>
            <div className="flex shrink-0 items-center">
              <Button variant="ghost" size="icon" aria-label={t('objects.moveUp')} disabled={i === 0} onClick={() => move(i, -1)}>
                <ArrowUp />
              </Button>
              <Button variant="ghost" size="icon" aria-label={t('objects.moveDown')} disabled={i === list.length - 1} onClick={() => move(i, 1)}>
                <ArrowDown />
              </Button>
              <Button variant="ghost" size="icon" aria-label={t('objects.edit')} onClick={() => setEditing(i)}>
                <Pencil />
              </Button>
              <Button variant="ghost" size="icon" aria-label={t('objects.remove')} onClick={() => setList((l) => l.filter((_, j) => j !== i))}>
                <Trash2 />
              </Button>
            </div>
          </li>
        ))}
        {list.length === 0 ? <li className="px-4 py-6 text-center text-[13px] text-muted-foreground">{t('objects.noFields')}</li> : null}
      </ul>
      {dirty ? (
        <div className="flex items-center justify-between gap-3 border-t bg-warning-soft/40 px-4 py-2.5">
          <p className="text-xs text-muted-foreground">{t('objects.unsavedFields')}</p>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setList(def.fields)} disabled={saving}>
              {t('objects.discard')}
            </Button>
            <Button size="sm" loading={saving} onClick={() => onSave(list)}>
              {t('objects.saveFields')}
            </Button>
          </div>
        </div>
      ) : null}
      <FieldDialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        field={typeof editing === 'number' ? list[editing] : undefined}
        targets={targets.filter((x) => x.key !== def.key).concat([{ key: def.key, label: def.plural }])}
        onSubmit={(f) => {
          setList((l) => (typeof editing === 'number' ? l.map((x, j) => (j === editing ? f : x)) : [...l, f]));
          setEditing(null);
        }}
      />
    </Card>
  );
}

function FieldDialog({
  open,
  onOpenChange,
  field,
  targets,
  onSubmit
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  field?: ObjectFieldDef;
  targets: Array<{ key: string; label: string }>;
  onSubmit: (f: ObjectFieldDef) => void;
}) {
  const { t } = useTranslation();
  const blank: ObjectFieldDef = { key: '', label: '', type: 'text' };
  const [f, setF] = useState<ObjectFieldDef>(field ?? blank);
  const [options, setOptions] = useState('');
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (open) {
      setF(field ?? blank);
      setOptions((field?.options ?? []).map((o) => o.label).join('\n'));
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, field]);
  const choice = f.type === 'select' || f.type === 'multiselect';
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!f.label.trim()) return setError(t('common.required'));
    if (isLinkField(f.type) && !f.lookup) return setError(t('objects.pickTarget'));
    const opts = options
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    if (choice && opts.length === 0) return setError(t('objects.addOptions'));
    const prev = new Map((field?.options ?? []).map((o) => [o.label, o.value]));
    onSubmit({
      ...f,
      label: f.label.trim(),
      options: choice ? opts.map((label) => ({ label, value: prev.get(label) ?? '' })) : undefined,
      lookup: isLinkField(f.type) ? f.lookup : undefined,
      unique: UNIQUE_FIELD_TYPES.has(f.type) ? f.unique : undefined
    });
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{field ? t('objects.editField') : t('objects.addField')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('objects.fieldDialogBody')}</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <Field label={t('objects.fieldLabel')} error={error ?? undefined}>
            <Input value={f.label} onChange={(e) => setF({ ...f, label: e.target.value })} maxLength={80} autoFocus placeholder={t('objects.fieldLabelPlaceholder')} />
          </Field>
          <Field label={t('objects.fieldType')} hint={field?.key ? t('objects.typeChangeHint') : t(`records.typeHints.${f.type}`)}>
            <Select value={f.type} onChange={(e) => setF({ ...f, type: e.target.value as FieldType })} options={FIELD_TYPES.map((x) => ({ value: x, label: t(`records.types.${x}`) }))} />
          </Field>
          {isLinkField(f.type) ? (
            <Field label={t('objects.linksToLabel')}>
              <Select
                value={f.lookup ?? ''}
                onChange={(e) => setF({ ...f, lookup: e.target.value })}
                placeholder={t('objects.pickTarget')}
                options={targets.map((x) => ({ value: x.key, label: x.label }))}
              />
            </Field>
          ) : null}
          {choice ? (
            <Field label={t('objects.options')} hint={t('objects.optionsHint')}>
              <Textarea value={options} onChange={(e) => setOptions(e.target.value)} rows={4} />
            </Field>
          ) : null}
          <Field label={t('objects.helpText')}>
            <Input value={f.helpText ?? ''} onChange={(e) => setF({ ...f, helpText: e.target.value })} maxLength={300} />
          </Field>
          <Checkbox checked={Boolean(f.required)} onCheckedChange={(v) => setF({ ...f, required: v })} label={t('objects.required')} />
          {UNIQUE_FIELD_TYPES.has(f.type) ? (
            <Checkbox checked={Boolean(f.unique)} onCheckedChange={(v) => setF({ ...f, unique: v })} label={t('records.fieldDialog.unique')} description={t('objects.uniqueHint')} />
          ) : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit">{field ? t('objects.applyField') : t('objects.addField')}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ApiCard({ def }: { def: ObjectDefinition }) {
  const { t } = useTranslation();
  const base = `/api/crm/v1/w/{workspace}/crm/${def.key}`;
  const rows: Array<[string, string, string]> = [
    ['GET', base, t('objects.api.list')],
    ['POST', base, t('objects.api.create')],
    ['GET', `${base}/{id}`, t('objects.api.get')],
    ['PATCH', `${base}/{id}`, t('objects.api.update')],
    ['DELETE', `${base}/{id}`, t('objects.api.delete')],
    ['GET', `/api/crm/v1/w/{workspace}/crm/meta/${def.key}`, t('objects.api.meta')]
  ];
  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <Code2 className="size-4 text-muted-foreground" aria-hidden /> {t('objects.api.title')}
          </span>
        }
        description={t('objects.api.body')}
      />
      <ul className="space-y-1.5 px-4 pb-4">
        {rows.map(([m, path, what]) => (
          <li key={m + path} className="text-xs">
            <span className="flex items-start gap-2">
              <span className="w-12 shrink-0 rounded bg-muted px-1 py-0.5 text-center font-mono text-[10px] font-semibold">{m}</span>
              <span className="min-w-0 break-all font-mono text-foreground">{path}</span>
            </span>
            <span className="ml-14 block text-muted-foreground">{what}</span>
          </li>
        ))}
      </ul>
      <p className="border-t px-4 py-3 text-xs text-muted-foreground">{t('objects.api.enable', { module: def.module })}</p>
    </Card>
  );
}
