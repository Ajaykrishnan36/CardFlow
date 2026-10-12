import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { LayoutTemplate, Pencil, Plus, ShieldCheck, Trash2 } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta, ObjectRules, ValidationRule } from '@crm/api/types';
import type { FilterGroup } from '@crm/api/types-features';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Switch, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { FieldEditor } from './field-input';
import { FilterBuilder } from './list/filter-builder';
import { emptyGroup } from './list/filter-utils';
import { layoutHref, listHref, useRecordScope } from './record-scope';
import { isEmptyValue, recordKeys, useObjectMeta } from './use-object-meta';

const NO_DEFAULT = new Set(['files', 'relations', 'json']);

/** /setup/:object/rules — what a record must satisfy to be saved, and what new records start with. */
export function RulesPage({ object }: { object: ObjectKey }) {
  const scope = useRecordScope();
  if (!scope.canCustomize) return <NoAccessPage />;
  return <RulesView object={object} />;
}

function RulesView({ object }: { object: ObjectKey }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const metaQ = useObjectMeta(object, { forLayout: true });
  const meta = metaQ.data;
  const key = [...recordKeys.meta(scope.prefix, object), 'rules'];
  const rulesQ = useQuery({ queryKey: key, queryFn: () => scope.api.rules(object) });
  useDocumentTitle(meta ? t('records.rules.title', { object: meta.labelSingular }) : t('records.common.loading'));
  const [editing, setEditing] = useState<ValidationRule | 'new' | null>(null);
  const [removing, setRemoving] = useState<ValidationRule | null>(null);

  const adopt = (next: ObjectRules) => {
    qc.setQueryData(key, next);
    void qc.invalidateQueries({ queryKey: recordKeys.meta(scope.prefix, object) });
  };
  const toggle = useMutation({
    mutationFn: (r: ValidationRule) => scope.api.updateRule(object, r.id, { active: !r.active }),
    onSuccess: adopt
  });
  const remove = useMutation({
    mutationFn: (r: ValidationRule) => scope.api.deleteRule(object, r.id),
    onSuccess: (next) => {
      adopt(next);
      setRemoving(null);
      toast.success(t('records.rules.deleted'));
    }
  });

  if (metaQ.isError || rulesQ.isError) {
    return (
      <PageContainer>
        <ErrorState title={t('records.common.loadError')} onRetry={() => void (metaQ.refetch(), rulesQ.refetch())} />
      </PageContainer>
    );
  }
  if (!meta || !rulesQ.data) {
    return (
      <PageContainer>
        <Skeleton className="h-6 w-64" />
        <Skeleton className="mt-4 h-48 w-full" />
        <Skeleton className="mt-4 h-48 w-full" />
      </PageContainer>
    );
  }
  const rules = rulesQ.data.rules;
  const byKey = new Map(meta.fields.map((f) => [f.key, f]));

  return (
    <PageContainer>
      <PageHeader
        title={t('records.rules.title', { object: meta.labelSingular })}
        description={t('records.rules.subtitle', { objects: meta.labelPlural.toLowerCase() })}
        crumbs={[{ label: meta.labelPlural, to: listHref(scope, object) }, { label: t('records.rules.crumb') }]}
        actions={
          <Button variant="outline" asChild>
            <Link to={layoutHref(scope, object)}>
              <LayoutTemplate /> {t('records.common.editLayout')}
            </Link>
          </Button>
        }
      />

      <Card className="mt-4 overflow-hidden">
        <CardHeader
          title={t('records.rules.rulesTitle')}
          description={t('records.rules.rulesBody')}
          actions={
            <Button size="sm" onClick={() => setEditing('new')}>
              <Plus /> {t('records.rules.add')}
            </Button>
          }
        />
        {rules.length === 0 ? (
          <div className="px-5 py-8 text-center">
            <ShieldCheck className="mx-auto size-8 text-muted-foreground" aria-hidden />
            <p className="mt-2 text-sm font-medium">{t('records.rules.emptyTitle')}</p>
            <p className="mx-auto mt-1 max-w-md text-[13px] text-muted-foreground">{t('records.rules.emptyBody')}</p>
          </div>
        ) : (
          <ul className="divide-y">
            {rules.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center gap-3 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-medium">{r.name}</p>
                    {r.active ? null : <Badge tone="neutral">{t('records.rules.off')}</Badge>}
                    {r.field && byKey.get(r.field) ? <Badge tone="primary">{byKey.get(r.field)?.label}</Badge> : null}
                  </div>
                  <p className="mt-0.5 text-[13px] text-muted-foreground">{r.message}</p>
                </div>
                <Switch checked={r.active} onCheckedChange={() => toggle.mutate(r)} disabled={toggle.isPending} aria-label={t('records.rules.activeLabel', { name: r.name })} />
                <Button variant="subtle" size="icon-sm" onClick={() => setEditing(r)} aria-label={t('records.rules.edit', { name: r.name })}>
                  <Pencil />
                </Button>
                <Button variant="subtle" size="icon-sm" onClick={() => setRemoving(r)} aria-label={t('records.rules.delete', { name: r.name })}>
                  <Trash2 />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <DefaultsCard object={object} meta={meta} defaults={rulesQ.data.defaults} onSaved={adopt} />

      {editing ? <RuleDialog object={object} meta={meta} rule={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={adopt} /> : null}
      <ConfirmDialog
        open={Boolean(removing)}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={t('records.rules.deleteTitle', { name: removing?.name ?? '' })}
        body={t('records.rules.deleteBody')}
        confirmLabel={t('records.rules.confirmDelete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </PageContainer>
  );
}

function RuleDialog({
  object,
  meta,
  rule,
  onClose,
  onSaved
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  rule: ValidationRule | null;
  onClose: () => void;
  onSaved: (r: ObjectRules) => void;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const [name, setName] = useState(rule?.name ?? '');
  const [message, setMessage] = useState(rule?.message ?? '');
  const [field, setField] = useState(rule?.field ?? '');
  const [filter, setFilter] = useState<FilterGroup>(rule?.filter ?? emptyGroup('and'));
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), message: message.trim(), field, filter };
      return rule ? scope.api.updateRule(object, rule.id, body) : scope.api.createRule(object, body);
    },
    onSuccess: (next) => {
      onSaved(next);
      toast.success(t('records.rules.saved'));
      onClose();
    }
  });
  const fe = isApiError(save.error) ? save.error.fieldErrors : {};
  const fieldOptions = useMemo(
    () => [{ value: '', label: t('records.rules.topOfForm') }, ...[...meta.fields].sort((a, b) => a.label.localeCompare(b.label)).map((f) => ({ value: f.key, label: f.label }))],
    [meta.fields, t]
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="top-[4vh] max-h-[92vh] max-w-2xl overflow-y-auto p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{rule ? t('records.rules.editTitle') : t('records.rules.newTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('records.rules.dialogBody', { object: meta.labelSingular.toLowerCase() })}</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <Field label={t('records.rules.name')} error={fe.name}>
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus placeholder={t('records.rules.namePlaceholder')} />
          </Field>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">{t('records.rules.when', { object: meta.labelSingular.toLowerCase() })}</p>
            <div className="rounded-md border p-3">
              <FilterBuilder meta={meta} value={filter} onChange={setFilter} />
            </div>
            {fe.filter ? <p className="mt-1.5 text-xs text-danger">{fe.filter}</p> : null}
          </div>
          <Field label={t('records.rules.message')} error={fe.message} hint={t('records.rules.messageHint')}>
            <Textarea value={message} onChange={(e) => setMessage(e.target.value)} rows={2} maxLength={300} placeholder={t('records.rules.messagePlaceholder')} />
          </Field>
          <Field label={t('records.rules.showAt')} error={fe.field}>
            <Select value={field} onChange={(e) => setField(e.target.value)} options={fieldOptions} />
          </Field>
          {save.isError && Object.keys(fe).length === 0 ? <Alert tone="danger">{isApiError(save.error) ? save.error.message : t('common.genericError')}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={save.isPending}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DefaultsCard({ object, meta, defaults, onSaved }: { object: ObjectKey; meta: ObjectMeta; defaults: Record<string, unknown>; onSaved: (r: ObjectRules) => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const [values, setValues] = useState<Record<string, unknown>>(defaults);
  const [adding, setAdding] = useState('');
  useEffect(() => setValues(defaults), [defaults]);
  const save = useMutation({
    mutationFn: () => scope.api.saveDefaults(object, Object.fromEntries(Object.entries(values).filter(([, v]) => !isEmptyValue(v)))),
    onSuccess: (next) => {
      onSaved(next);
      toast.success(t('records.rules.defaultsSaved'));
    }
  });
  const fe = isApiError(save.error) ? save.error.fieldErrors : {};
  const usable = useMemo(
    () => meta.fields.filter((f: FieldDef) => !f.readOnly && !f.unique && !NO_DEFAULT.has(f.type) && f.key !== 'code').sort((a, b) => a.label.localeCompare(b.label)),
    [meta.fields]
  );
  const chosen = usable.filter((f) => f.key in values);
  const rest = usable.filter((f) => !(f.key in values));
  const dirty = JSON.stringify(values) !== JSON.stringify(defaults);
  return (
    <Card className="mt-4">
      <CardHeader title={t('records.rules.defaultsTitle')} description={t('records.rules.defaultsBody', { object: meta.labelSingular.toLowerCase() })} />
      <div className="space-y-4 p-5">
        {chosen.length === 0 ? <p className="text-[13px] text-muted-foreground">{t('records.rules.defaultsEmpty')}</p> : null}
        <div className="grid gap-4 sm:grid-cols-2">
          {chosen.map((f) => (
            <div key={f.key} className="flex items-start gap-2">
              <FieldEditor className="min-w-0 flex-1" field={{ ...f, required: false }} value={values[f.key]} onChange={(v) => setValues((p) => ({ ...p, [f.key]: v }))} error={fe[f.key]} />
              <Button
                variant="subtle"
                size="icon-sm"
                className="mt-6"
                onClick={() => setValues(({ [f.key]: _gone, ...p }) => p)}
                aria-label={t('records.rules.removeDefault', { field: f.label })}
              >
                <Trash2 />
              </Button>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2 border-t pt-4">
          <Select
            className="w-64"
            aria-label={t('records.rules.pickField')}
            value={adding}
            onChange={(e) => setAdding(e.target.value)}
            options={[{ value: '', label: t('records.rules.pickField') }, ...rest.map((f) => ({ value: f.key, label: f.label }))]}
          />
          <Button
            variant="outline"
            disabled={!adding}
            onClick={() => {
              setValues((p) => ({ ...p, [adding]: null }));
              setAdding('');
            }}
          >
            <Plus /> {t('records.rules.addDefault')}
          </Button>
          <Button className="ml-auto" onClick={() => save.mutate()} loading={save.isPending} disabled={!dirty}>
            {t('records.rules.saveDefaults')}
          </Button>
        </div>
        {save.isError && Object.keys(fe).length === 0 ? <Alert tone="danger">{isApiError(save.error) ? save.error.message : t('common.genericError')}</Alert> : null}
      </div>
    </Card>
  );
}
