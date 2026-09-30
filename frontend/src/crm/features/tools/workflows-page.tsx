import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Clock, FlaskConical, Hand, History, Play, Plus, Power, Rocket, Square, Trash2, Webhook, Workflow as WorkflowIcon, Zap } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FilterGroup, TriggerType, Workflow, WorkflowDef, WorkflowInput, WorkflowRun } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { useRecordScope } from '@crm/features/records/record-scope';
import { LookupCombobox } from '@crm/features/records/field-input';
import { FilterBuilder } from '@crm/features/records/list/filter-builder';
import { cleanFilter, countConditions, emptyGroup } from '@crm/features/records/list/filter-utils';
import { cn, relativeTime } from '@crm/lib/utils';
import { CopyField, errorText, formatDateTime, hasCap, JsonBlock, StatusBadge, toolKeys, useObjectMeta, useTools, useWorkspaceObjects } from './tool-utils';
import { StepList, stepIcons, useVariables } from './workflow-steps';

const TRIGGERS: Array<{ type: TriggerType; icon: typeof Zap }> = [
  { type: 'record.created', icon: Zap },
  { type: 'record.updated', icon: Zap },
  { type: 'record.upserted', icon: Zap },
  { type: 'record.deleted', icon: Zap },
  { type: 'manual', icon: Hand },
  { type: 'schedule', icon: Clock },
  { type: 'webhook', icon: Webhook }
];

function triggerSummary(t: (k: string, o?: Record<string, unknown>) => string, def: WorkflowDef, objectLabel: (k?: string) => string): string {
  const tr = def.trigger;
  if (tr.type.startsWith('record.')) return t(`tools.wf.summary.${tr.type}`, { object: objectLabel(tr.object) });
  if (tr.type === 'schedule') return tr.schedule?.cron ? t('tools.wf.summary.cron', { cron: tr.schedule.cron }) : t('tools.wf.summary.every', { n: tr.schedule?.every ?? 1, unit: t(`tools.wf.units.${tr.schedule?.unit ?? 'days'}`) });
  if (tr.type === 'manual') return tr.manual?.mode === 'global' || !tr.object ? t('tools.wf.summary.manualGlobal') : t('tools.wf.summary.manual', { object: objectLabel(tr.object) });
  return t('tools.wf.summary.webhook');
}

function useObjectLabel() {
  const objects = useWorkspaceObjects();
  return (k?: string) => objects.find((o) => o.key === k)?.label ?? k ?? '—';
}

/** /crm/w/:ws/workflows — automations (workflows.manage). */
export function WorkflowsPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('tools.wf.title'));
  const api = useTools();
  const navigate = useNavigate();
  const objectLabel = useObjectLabel();
  const allowed = hasCap('workflows.manage')(context);
  const q = useQuery({ queryKey: toolKeys.one(code, 'workflows'), queryFn: () => api.workflows(), enabled: allowed });
  const [open, setOpen] = useState(false);
  if (!allowed) return <NoAccessPage />;
  return (
    <PageContainer>
      <PageHeader
        title={t('tools.wf.title')}
        description={t('tools.wf.subtitle')}
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus /> {t('tools.wf.new')}
          </Button>
        }
      />
      <Card>
        {q.isPending ? (
          <Skeleton className="m-5 h-40" />
        ) : q.isError ? (
          <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />
        ) : q.data.data.length === 0 ? (
          <EmptyState
            icon={WorkflowIcon}
            title={t('tools.wf.emptyTitle')}
            body={t('tools.wf.emptyBody')}
            action={
              <Button onClick={() => setOpen(true)}>
                <Plus /> {t('tools.wf.new')}
              </Button>
            }
          />
        ) : (
          <ul className="divide-y">
            {q.data.data.map((w) => (
              <li key={w.id}>
                <Link to={`${workspaceBase(code)}/workflows/${w.id}`} className="flex flex-col gap-1 px-5 py-3.5 hover:bg-muted/40 sm:flex-row sm:items-center sm:gap-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-foreground">{w.name}</span>
                      <StatusBadge status={w.status} />
                      {w.hasChanges && w.version ? <Badge tone="warning">{t('tools.wf.unpublished')}</Badge> : null}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{triggerSummary(t, w.published ?? w.draft, objectLabel)}{w.description ? ` · ${w.description}` : ''}</p>
                  </div>
                  <div className="flex shrink-0 gap-4 text-xs text-muted-foreground">
                    <span>{w.version ? t('tools.wf.versionN', { n: w.version }) : t('tools.wf.neverPublished')}</span>
                    <span>{t('tools.wf.runsN', { count: w.runs.total })}</span>
                    {w.runs.failed ? <span className="text-danger">{t('tools.wf.failedN', { count: w.runs.failed })}</span> : null}
                    <span>{w.lastRunAt ? t('tools.wf.lastRun', { when: relativeTime(w.lastRunAt) }) : t('tools.wf.noRuns')}</span>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <NewWorkflowDialog open={open} onOpenChange={setOpen} onCreated={(w) => navigate(`${workspaceBase(code)}/workflows/${w.id}`)} />
    </PageContainer>
  );
}

function NewWorkflowDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; onCreated: (w: Workflow) => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const objects = useWorkspaceObjects();
  const [name, setName] = useState('');
  const [type, setType] = useState<TriggerType>('record.created');
  const [object, setObject] = useState('leads');
  const m = useMutation({
    mutationFn: () => {
      const trigger: WorkflowDef['trigger'] = { type };
      if (type.startsWith('record.')) trigger.object = object;
      if (type === 'manual') Object.assign(trigger, { object, manual: { mode: 'single' } });
      if (type === 'schedule') trigger.schedule = { every: 1, unit: 'days' };
      return api.createWorkflow({ name: name.trim(), draft: { trigger, steps: [] } });
    },
    onSuccess: (w) => {
      onOpenChange(false);
      setName('');
      onCreated(w);
    }
  });
  const needsObject = type.startsWith('record.') || type === 'manual';
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('tools.wf.new')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.wf.newBody')}</DialogDescription>
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) m.mutate();
          }}
        >
          {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
          <Field label={t('tools.wf.name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={120} placeholder={t('tools.wf.namePlaceholder')} />
          </Field>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">{t('tools.wf.startsWhen')}</legend>
            <div className="grid gap-1.5 sm:grid-cols-2">
              {TRIGGERS.map((tr) => (
                <button
                  key={tr.type}
                  type="button"
                  onClick={() => setType(tr.type)}
                  className={cn('flex items-start gap-2 rounded-md border px-2.5 py-2 text-left text-[13px]', type === tr.type ? 'border-primary bg-primary-soft' : 'hover:bg-muted')}
                  aria-pressed={type === tr.type}
                >
                  <tr.icon className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
                  <span>
                    <span className="block font-medium">{t(`tools.wf.trigger.${tr.type}`)}</span>
                    <span className="block text-xs text-muted-foreground">{t(`tools.wf.triggerHint.${tr.type}`)}</span>
                  </span>
                </button>
              ))}
            </div>
          </fieldset>
          {needsObject ? (
            <Field label={t('tools.wf.object')}>
              <Select value={object} onChange={(e) => setObject(e.target.value)} options={objects.map((o) => ({ value: o.key, label: o.label }))} />
            </Field>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={m.isPending} disabled={!name.trim()}>
              {t('tools.wf.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** /crm/w/:ws/workflows/:id — the builder, runs and versions. */
export function WorkflowEditorPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  const { id = '' } = useParams();
  const api = useTools();
  const allowed = hasCap('workflows.manage')(context);
  const q = useQuery({ queryKey: toolKeys.one(code, 'workflow', id), queryFn: () => api.workflow(id), enabled: allowed });
  useDocumentTitle(q.data?.name ?? t('tools.wf.title'));
  if (!allowed) return <NoAccessPage />;
  if (q.isPending)
    return (
      <PageContainer>
        <Skeleton className="h-96 w-full" />
      </PageContainer>
    );
  if (q.isError)
    return (
      <PageContainer>
        <Card>
          <ErrorState title={t('tools.common.loadError')} message={errorText(q.error, t('common.genericError'))} onRetry={() => void q.refetch()} />
        </Card>
      </PageContainer>
    );
  return <WorkflowEditor w={q.data} />;
}

function WorkflowEditor({ w }: { w: Workflow }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const key = toolKeys.one(code, 'workflow', w.id);
  const [tab, setTab] = useState<'build' | 'runs' | 'versions'>('build');
  const [name, setName] = useState(w.name);
  const [description, setDescription] = useState(w.description);
  const [def, setDef] = useState<WorkflowDef>(w.draft);
  const [dirty, setDirty] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [testOpen, setTestOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const meta = useObjectMeta(def.trigger.object);
  const vars = useVariables(def, meta.data?.fields);

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  const change = (d: WorkflowDef) => {
    setDef(d);
    setDirty(true);
  };
  const byKey = useMemo(() => new Map((meta.data?.fields ?? []).map((f) => [f.key, f])), [meta.data]);
  const cleanDef = (): WorkflowDef => ({ ...def, trigger: { ...def.trigger, filter: cleanFilter(def.trigger.filter, byKey) } });
  const onError = (e: unknown) => {
    if (isApiError(e) && Object.keys(e.fieldErrors).length) setErrors(e.fieldErrors);
    toast.error(errorText(e, t('common.genericError')));
  };
  const accept = (n: Workflow) => {
    qc.setQueryData(key, n);
    void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'workflows') });
  };
  const save = useMutation({
    mutationFn: () => api.updateWorkflow(w.id, { name: name.trim(), description: description.trim(), draft: cleanDef() }),
    onSuccess: (n) => {
      accept(n);
      setDirty(false);
      setErrors({});
      toast.success(t('tools.wf.saved'));
    },
    onError
  });
  const publish = useMutation({
    mutationFn: async () => {
      if (dirty || name !== w.name) await api.updateWorkflow(w.id, { name: name.trim(), description: description.trim(), draft: cleanDef() });
      return api.publishWorkflow(w.id);
    },
    onSuccess: (n) => {
      accept(n);
      setDirty(false);
      setErrors({});
      toast.success(t('tools.wf.published', { n: n.version }));
    },
    onError
  });
  const status = useMutation({
    mutationFn: (s: 'active' | 'inactive') => api.setWorkflowStatus(w.id, s),
    onSuccess: (n) => {
      accept(n);
      toast.success(n.status === 'active' ? t('tools.wf.turnedOn') : t('tools.wf.turnedOff'));
    },
    onError
  });
  const del = useMutation({
    mutationFn: () => api.deleteWorkflow(w.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'workflows') });
      navigate(`${workspaceBase(code)}/workflows`);
    }
  });

  return (
    <PageContainer wide>
      <PageHeader
        crumbs={[{ label: t('tools.wf.title'), to: `${workspaceBase(code)}/workflows` }, { label: w.name }]}
        title={w.name}
        badges={
          <>
            <StatusBadge status={w.status} />
            {w.version ? <Badge tone="neutral">{t('tools.wf.versionN', { n: w.version })}</Badge> : null}
            {w.hasChanges || dirty ? <Badge tone="warning">{t('tools.wf.unpublished')}</Badge> : null}
          </>
        }
        actions={
          <>
            <Button variant="subtle" onClick={() => setDeleting(true)} aria-label={t('tools.common.delete')}>
              <Trash2 />
            </Button>
            <Button variant="outline" onClick={() => setTestOpen(true)}>
              <FlaskConical /> {t('tools.wf.test')}
            </Button>
            <Button variant="outline" onClick={() => save.mutate()} loading={save.isPending} disabled={!dirty && name === w.name && description === w.description}>
              {t('tools.wf.saveDraft')}
            </Button>
            {w.version ? (
              <Button variant="outline" onClick={() => status.mutate(w.status === 'active' ? 'inactive' : 'active')} loading={status.isPending}>
                <Power /> {w.status === 'active' ? t('tools.wf.turnOff') : t('tools.wf.turnOn')}
              </Button>
            ) : null}
            <Button onClick={() => publish.mutate()} loading={publish.isPending} disabled={!dirty && !w.hasChanges && w.version > 0}>
              <Rocket /> {w.version ? t('tools.wf.publishChanges') : t('tools.wf.publish')}
            </Button>
          </>
        }
      />
      <Tabs
        className="mb-4"
        value={tab}
        onChange={setTab}
        items={[
          { value: 'build', label: t('tools.wf.tabs.build') },
          { value: 'runs', label: t('tools.wf.tabs.runs', { count: w.runs.total }) },
          { value: 'versions', label: t('tools.wf.tabs.versions') }
        ]}
      />
      {tab === 'runs' ? (
        <RunsTab w={w} />
      ) : tab === 'versions' ? (
        <VersionsTab
          w={w}
          onRestored={(n) => {
            accept(n);
            setDef(n.draft);
            setDirty(false);
            setTab('build');
          }}
        />
      ) : (
        <div className="mx-auto max-w-5xl space-y-4">
          <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
            <Card className="h-fit">
              <CardHeader title={t('tools.wf.about')} />
              <div className="space-y-3 px-5 py-4">
                <Field label={t('tools.wf.name')}>
                  <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={120} />
                </Field>
                <Field label={t('tools.wf.description')}>
                  <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} maxLength={500} />
                </Field>
              </div>
            </Card>
            <TriggerCard w={w} def={def} onChange={change} errors={errors} />
          </div>
          <div>
            <p className="mb-2 text-[13px] font-semibold text-foreground">{t('tools.wf.then')}</p>
            {errors.steps ? <Alert tone="danger" className="mb-2">{errors.steps}</Alert> : null}
            <StepList steps={def.steps} onChange={(steps) => change({ ...def, steps })} vars={vars} triggerObject={def.trigger.object} errors={errors} path="steps" />
          </div>
        </div>
      )}
      <TestDialog open={testOpen} onOpenChange={setTestOpen} w={w} def={def} dirty={dirty} onBeforeRun={async () => {
        if (dirty) {
          const n = await api.updateWorkflow(w.id, { name: name.trim(), description: description.trim(), draft: cleanDef() });
          accept(n);
          setDirty(false);
        }
      }} onStarted={() => setTab('runs')} />
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={t('tools.wf.deleteTitle', { name: w.name })}
        body={t('tools.wf.deleteBody')}
        confirmLabel={t('tools.common.delete')}
        tone="danger"
        loading={del.isPending}
        onConfirm={() => del.mutate()}
      />
    </PageContainer>
  );
}

function TriggerCard({ w, def, onChange, errors }: { w: Workflow; def: WorkflowDef; onChange: (d: WorkflowDef) => void; errors: Record<string, string> }) {
  const { t } = useTranslation();
  const objects = useWorkspaceObjects();
  const tr = def.trigger;
  const meta = useObjectMeta(tr.object);
  const set = (patch: Partial<WorkflowDef['trigger']>) => onChange({ ...def, trigger: { ...tr, ...patch } });
  const recordTrigger = tr.type.startsWith('record.');
  const manual = tr.type === 'manual';
  const mode = tr.manual?.mode ?? 'global';
  const err = errors['trigger.type'] ?? errors['trigger.object'] ?? errors['trigger.schedule'] ?? errors['trigger.filter'] ?? Object.entries(errors).find(([k]) => k.startsWith('trigger.'))?.[1];
  return (
    <Card className="overflow-visible">
      <CardHeader title={t('tools.wf.startsWhen')} />
      <div className="space-y-3 px-5 py-4">
        {err ? <p className="text-[13px] text-danger">{err}</p> : null}
        <Field label={t('tools.wf.triggerType')}>
          <Select
            value={tr.type}
            onChange={(e) => {
              const type = e.target.value as TriggerType;
              const n: WorkflowDef['trigger'] = { type, object: type.startsWith('record.') || type === 'manual' ? tr.object ?? 'leads' : undefined };
              if (type === 'manual') n.manual = { mode: 'single' };
              if (type === 'schedule') n.schedule = { every: 1, unit: 'days' };
              onChange({ ...def, trigger: n });
            }}
            options={TRIGGERS.map((x) => ({ value: x.type, label: t(`tools.wf.trigger.${x.type}`) }))}
          />
        </Field>
        <p className="text-xs text-muted-foreground">{t(`tools.wf.triggerHint.${tr.type}`)}</p>
        {manual ? (
          <Field label={t('tools.wf.manualMode')}>
            <Select
              value={mode}
              onChange={(e) => set({ manual: { ...tr.manual, mode: e.target.value as 'single' | 'bulk' | 'global' }, object: e.target.value === 'global' ? undefined : tr.object ?? 'leads' })}
              options={(['single', 'bulk', 'global'] as const).map((m) => ({ value: m, label: t(`tools.wf.modes.${m}`) }))}
            />
          </Field>
        ) : null}
        {recordTrigger || (manual && mode !== 'global') ? (
          <Field label={t('tools.wf.object')}>
            <Select value={tr.object ?? ''} onChange={(e) => set({ object: e.target.value, fields: undefined, filter: undefined })} options={objects.map((o) => ({ value: o.key, label: o.label }))} />
          </Field>
        ) : null}
        {tr.type === 'record.updated' && meta.data ? (
          <Field label={t('tools.wf.watchFields')} hint={t('tools.wf.watchFieldsHint')}>
            <Select
              value=""
              onChange={(e) => e.target.value && set({ fields: [...(tr.fields ?? []), e.target.value] })}
              placeholder={tr.fields?.length ? t('tools.wf.addAnother') : t('tools.wf.anyField')}
              options={meta.data.fields.filter((f) => !(tr.fields ?? []).includes(f.key)).map((f) => ({ value: f.key, label: f.label }))}
            />
          </Field>
        ) : null}
        {tr.fields?.length ? (
          <ul className="flex flex-wrap gap-1.5">
            {tr.fields.map((k) => (
              <li key={k} className="inline-flex items-center gap-1 rounded-full border bg-muted/50 py-0.5 pl-2.5 pr-1 text-xs">
                {meta.data?.fields.find((f) => f.key === k)?.label ?? k}
                <button type="button" className="grid size-5 place-items-center rounded-full hover:bg-muted" onClick={() => set({ fields: tr.fields!.filter((x) => x !== k) })} aria-label={t('tools.common.removeName', { name: k })}>
                  ×
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        {recordTrigger && meta.data ? (
          <div>
            <p className="mb-1.5 text-[13px] font-medium">
              {t('tools.wf.onlyWhen')} {countConditions(tr.filter) ? null : <span className="font-normal text-muted-foreground">{t('tools.wf.onlyWhenAll')}</span>}
            </p>
            <FilterBuilder meta={meta.data} value={(tr.filter as FilterGroup) ?? emptyGroup()} onChange={(g) => set({ filter: g })} />
          </div>
        ) : null}
        {tr.type === 'schedule' ? <ScheduleEditor def={def} onChange={onChange} /> : null}
        {manual ? (
          <>
            <Checkbox checked={Boolean(tr.manual?.everyone)} onCheckedChange={(v) => set({ manual: { ...tr.manual, everyone: v } })} label={t('tools.wf.everyoneRuns')} description={t('tools.wf.everyoneRunsHint')} />
            <FormEditor form={tr.manual?.form ?? []} onChange={(form) => set({ manual: { ...tr.manual, form } })} />
          </>
        ) : null}
        {tr.type === 'webhook' ? (
          w.webhookUrl ? (
            <CopyField label={t('tools.wf.webhookUrl')} value={w.webhookUrl} hint={t('tools.wf.webhookUrlHint', { ph: '{{trigger.body}}' })} />
          ) : (
            <p className="text-xs text-muted-foreground">{t('tools.wf.webhookAfterSave')}</p>
          )
        ) : null}
        {w.nextRunAt && tr.type === 'schedule' ? <p className="text-xs text-muted-foreground">{t('tools.wf.nextRun', { when: formatDateTime(w.nextRunAt) })}</p> : null}
      </div>
    </Card>
  );
}

function ScheduleEditor({ def, onChange }: { def: WorkflowDef; onChange: (d: WorkflowDef) => void }) {
  const { t } = useTranslation();
  const s = def.trigger.schedule ?? { every: 1, unit: 'days' as const };
  const useCron = Boolean(s.cron !== undefined && s.cron !== null && s.cron !== '' ) || false;
  const [cronMode, setCronMode] = useState(useCron);
  const set = (patch: Partial<NonNullable<WorkflowDef['trigger']['schedule']>>) => onChange({ ...def, trigger: { ...def.trigger, schedule: { ...s, ...patch } } });
  return (
    <div className="space-y-2">
      <Tabs
        value={cronMode ? 'cron' : 'every'}
        onChange={(v) => {
          setCronMode(v === 'cron');
          set(v === 'cron' ? { cron: s.cron || '0 9 * * 1-5' } : { cron: '' });
        }}
        items={[
          { value: 'every', label: t('tools.wf.repeatEvery') },
          { value: 'cron', label: t('tools.wf.cron') }
        ]}
      />
      {cronMode ? (
        <Field label={t('tools.wf.cronExpr')} hint={t('tools.wf.cronHint')}>
          <Input value={s.cron ?? ''} onChange={(e) => set({ cron: e.target.value })} className="font-mono" placeholder="0 9 * * 1-5" />
        </Field>
      ) : (
        <div className="grid grid-cols-[96px_minmax(0,1fr)] gap-2">
          <Input type="number" min={1} value={s.every ?? 1} onChange={(e) => set({ every: Number(e.target.value) })} aria-label={t('tools.wf.every')} />
          <Select value={s.unit ?? 'days'} onChange={(e) => set({ unit: e.target.value as 'minutes' })} options={(['minutes', 'hours', 'days', 'weeks'] as const).map((u) => ({ value: u, label: t(`tools.wf.units.${u}`) }))} />
        </div>
      )}
      <p className="text-xs text-muted-foreground">{t('tools.wf.scheduleNote')}</p>
    </div>
  );
}

function FormEditor({ form, onChange }: { form: WorkflowInput[]; onChange: (f: WorkflowInput[]) => void }) {
  const { t } = useTranslation();
  const set = (i: number, p: Partial<WorkflowInput> | null) => onChange(p ? form.map((x, j) => (j === i ? { ...x, ...p } : x)) : form.filter((_, j) => j !== i));
  return (
    <div>
      <p className="mb-1 text-[13px] font-medium">{t('tools.wf.askFor')}</p>
      <p className="mb-2 text-xs text-muted-foreground">{t('tools.wf.askForHint', { ph: '{{trigger.input.key}}' })}</p>
      <div className="space-y-2">
        {form.map((f, i) => (
          <div key={i} className="grid grid-cols-[minmax(0,1fr)_110px_2rem] items-center gap-1.5">
            <Input
              value={f.label}
              onChange={(e) => set(i, { label: e.target.value, key: f.key && f.key !== autoKey(f.label) ? f.key : autoKey(e.target.value) })}
              placeholder={t('tools.wf.question')}
              aria-label={t('tools.wf.question')}
            />
            <Select value={f.type} onChange={(e) => set(i, { type: e.target.value as WorkflowInput['type'] })} options={(['text', 'number', 'date', 'boolean', 'select'] as const).map((x) => ({ value: x, label: t(`tools.wf.inputTypes.${x}`) }))} />
            <Button type="button" size="icon-sm" variant="subtle" onClick={() => set(i, null)} aria-label={t('tools.common.removeName', { name: f.label })}>
              <Trash2 />
            </Button>
            {f.type === 'select' ? (
              <Input className="col-span-3" value={(f.options ?? []).join(', ')} onChange={(e) => set(i, { options: e.target.value.split(',').map((x) => x.trim()).filter(Boolean) })} placeholder={t('tools.wf.choices')} />
            ) : null}
          </div>
        ))}
        <Button type="button" size="sm" variant="ghost" onClick={() => onChange([...form, { key: `q${form.length + 1}`, label: '', type: 'text' }])}>
          <Plus /> {t('tools.wf.addQuestion')}
        </Button>
      </div>
    </div>
  );
}

function autoKey(label: string) {
  const k = label
    .trim()
    .replace(/[^a-zA-Z0-9]+(.)?/g, (_, c: string | undefined) => (c ? c.toUpperCase() : ''))
    .replace(/^[^a-zA-Z]+/, '');
  return k ? k[0]!.toLowerCase() + k.slice(1, 40) : '';
}

function TestDialog({ open, onOpenChange, w, def, dirty, onBeforeRun, onStarted }: { open: boolean; onOpenChange: (o: boolean) => void; w: Workflow; def: WorkflowDef; dirty: boolean; onBeforeRun: () => Promise<void>; onStarted: () => void }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const scope = useRecordScope();
  const api = useTools();
  const qc = useQueryClient();
  const [record, setRecord] = useState<{ id: string; label: string } | null>(null);
  const [input, setInput] = useState<Record<string, string>>({});
  const tr = def.trigger;
  const needsRecord = Boolean(tr.object) && (tr.type.startsWith('record.') || (tr.type === 'manual' && tr.manual?.mode !== 'global'));
  const m = useMutation({
    mutationFn: async () => {
      await onBeforeRun();
      let rec: Record<string, unknown> | undefined;
      if (needsRecord && record && tr.object) {
        const d = await scope.api.get(tr.object, record.id);
        rec = { ...d.record.values, id: d.record.id, code: d.record.code, displayName: d.record.title, version: d.record.version };
      }
      return api.runWorkflow(w.id, { test: true, record: rec, input });
    },
    onSuccess: () => {
      toast.success(t('tools.wf.testStarted'));
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'runs', w.id) });
      onOpenChange(false);
      onStarted();
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('tools.wf.testTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.wf.testBody')}</DialogDescription>
        <div className="mt-4 space-y-4">
          {dirty ? <Alert tone="info">{t('tools.wf.testSavesDraft')}</Alert> : null}
          {needsRecord && tr.object ? (
            <Field label={t('tools.wf.testRecord')} hint={t('tools.wf.testRecordHint')}>
              <LookupCombobox target={tr.object} value={record?.id ?? null} label={record?.label} onChange={(v) => setRecord(v ? { id: v.id, label: v.label } : null)} />
            </Field>
          ) : null}
          {(tr.manual?.form ?? []).map((f) => (
            <Field key={f.key} label={f.label || f.key}>
              <Input value={input[f.key] ?? ''} onChange={(e) => setInput({ ...input, [f.key]: e.target.value })} type={f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text'} />
            </Field>
          ))}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={() => m.mutate()} loading={m.isPending} disabled={needsRecord && !record}>
              <Play /> {t('tools.wf.runTest')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function RunsTab({ w }: { w: Workflow }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const [status, setStatus] = useState('');
  const [openId, setOpenId] = useState<string | null>(null);
  const key = toolKeys.one(code, 'runs', w.id, status);
  const q = useQuery({
    queryKey: key,
    queryFn: () => api.runs(w.id, status || undefined),
    refetchInterval: (query) => ((query.state.data ?? []).some((r) => ['queued', 'running'].includes(r.status)) ? 3000 : 30_000)
  });
  const act = useMutation({
    mutationFn: ({ run, action }: { run: WorkflowRun; action: 'stop' | 'retry' }) => (action === 'stop' ? api.stopRun(w.id, run.id) : api.retryRun(w.id, run.id)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'runs', w.id) }),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  return (
    <Card>
      <CardHeader
        title={t('tools.wf.runsTitle')}
        description={t('tools.wf.runsBody')}
        actions={
          <Select
            className="h-8 w-40 text-xs"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            options={[{ value: '', label: t('tools.wf.allRuns') }, ...(['running', 'waiting', 'completed', 'failed', 'stopped'] as const).map((s) => ({ value: s, label: t(`tools.status.${s}`) }))]}
          />
        }
      />
      {q.isPending ? (
        <Skeleton className="m-5 h-32" />
      ) : (q.data ?? []).length === 0 ? (
        <EmptyState icon={History} title={t('tools.wf.noRunsTitle')} body={t('tools.wf.noRunsBody')} />
      ) : (
        <ul className="divide-y">
          {(q.data ?? []).map((r) => {
            const trig = r.trigger as { type?: string; record?: { displayName?: string } };
            return (
              <li key={r.id} className="px-5 py-2.5">
                <div className="flex flex-wrap items-center gap-2">
                  <button type="button" onClick={() => setOpenId(openId === r.id ? null : r.id)} className="min-w-0 flex-1 text-left text-[13px]">
                    <span className="font-medium">{formatDateTime(r.startedAt)}</span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {r.version ? t('tools.wf.versionN', { n: r.version }) : t('tools.wf.testRun')}
                      {trig.record?.displayName ? ` · ${trig.record.displayName}` : ''}
                      {r.startedBy ? ` · ${r.startedBy}` : ''}
                      {r.finishedAt ? ` · ${Math.max(0, Math.round((new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime()) / 100) / 10)}s` : ''}
                      {r.resumeAt && r.status === 'waiting' ? ` · ${t('tools.wf.resumes', { when: formatDateTime(r.resumeAt) })}` : ''}
                    </span>
                  </button>
                  <StatusBadge status={r.status} />
                  {['queued', 'running', 'waiting'].includes(r.status) ? (
                    <Button size="sm" variant="outline" onClick={() => act.mutate({ run: r, action: 'stop' })}>
                      <Square /> {t('tools.wf.stop')}
                    </Button>
                  ) : null}
                  {r.status === 'failed' ? (
                    <Button size="sm" variant="outline" onClick={() => act.mutate({ run: r, action: 'retry' })}>
                      {t('tools.wf.retry')}
                    </Button>
                  ) : null}
                </div>
                {r.error ? <p className="mt-1 text-xs text-danger">{r.error}</p> : null}
                {openId === r.id ? (
                  <ol className="mt-2 space-y-1.5 border-l-2 pl-3">
                    {r.steps.map((s, i) => {
                      const Icon = stepIcons[s.type] ?? Zap;
                      return (
                        <li key={i} className="text-[13px]">
                          <div className="flex items-center gap-2">
                            <Icon className="size-3.5 text-muted-foreground" aria-hidden />
                            <span className="font-medium">{s.name || t(`tools.wf.step.${s.type}`)}</span>
                            <StatusBadge status={s.status} />
                            <span className="text-xs text-muted-foreground">{s.durationMs} ms</span>
                          </div>
                          {s.error ? <p className="ml-5 text-xs text-danger">{s.error}</p> : null}
                          {s.output !== undefined && s.output !== null ? <JsonBlock value={s.output} className="ml-5 mt-1 max-h-48" /> : null}
                        </li>
                      );
                    })}
                    {r.steps.length === 0 ? <li className="text-xs text-muted-foreground">{t('tools.wf.noSteps')}</li> : null}
                    <li>
                      <details className="text-xs text-muted-foreground">
                        <summary className="cursor-pointer">{t('tools.wf.triggerData')}</summary>
                        <JsonBlock value={r.trigger} className="mt-1 max-h-48" />
                      </details>
                    </li>
                  </ol>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

function VersionsTab({ w, onRestored }: { w: Workflow; onRestored: (w: Workflow) => void }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const q = useQuery({ queryKey: toolKeys.one(code, 'versions', w.id, String(w.version)), queryFn: () => api.workflowVersions(w.id) });
  const restore = useMutation({
    mutationFn: (v: number) => api.restoreWorkflowVersion(w.id, v),
    onSuccess: (n) => {
      toast.success(t('tools.wf.restored'));
      onRestored(n);
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  return (
    <Card>
      <CardHeader title={t('tools.wf.versionsTitle')} description={t('tools.wf.versionsBody')} />
      {q.isPending ? (
        <Skeleton className="m-5 h-24" />
      ) : (q.data ?? []).length === 0 ? (
        <p className="px-5 py-10 text-center text-[13px] text-muted-foreground">{t('tools.wf.noVersions')}</p>
      ) : (
        <ul className="divide-y">
          {(q.data ?? []).map((v) => (
            <li key={v.version} className="flex flex-wrap items-center gap-3 px-5 py-3 text-[13px]">
              <span className="font-medium">{t('tools.wf.versionN', { n: v.version })}</span>
              {v.version === w.version ? <Badge tone="success">{t('tools.wf.live')}</Badge> : null}
              <span className="flex-1 text-xs text-muted-foreground">
                {t('tools.wf.publishedBy', { name: v.publishedBy || '—', when: formatDateTime(v.publishedAt) })} · {t('tools.wf.stepsN', { count: countSteps(v.definition.steps) })}
              </span>
              <Button size="sm" variant="outline" onClick={() => restore.mutate(v.version)} loading={restore.isPending && restore.variables === v.version}>
                {t('tools.wf.restore')}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function countSteps(list: WorkflowDef['steps']): number {
  return list.reduce((n, s) => n + 1 + countSteps(s.then ?? []) + countSteps(s.else ?? []), 0);
}
