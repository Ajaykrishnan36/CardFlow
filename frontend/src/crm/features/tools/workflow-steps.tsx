import { useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowDown,
  ArrowUp,
  Braces,
  Clock,
  Code2,
  FilePlus2,
  FileSearch,
  FileX2,
  GitBranch,
  Globe,
  Bell,
  Mail,
  PenSquare,
  Plus,
  Repeat,
  Shuffle,
  Square,
  Trash2,
  Replace
} from 'lucide-react';
import type { FieldDef } from '@crm/api/types';
import type { FilterGroup, StepType, WorkflowDef, WorkflowStep } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuTrigger } from '@crm/components/ui/menu';
import { Popover } from '@crm/components/ui/popover';
import { FilterBuilder } from '@crm/features/records/list/filter-builder';
import { emptyGroup } from '@crm/features/records/list/filter-utils';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { cn } from '@crm/lib/utils';
import { PeoplePicker, toolKeys, useObjectMeta, useTools, useWorkspaceObjects } from './tool-utils';

// The step list of the workflow builder: each step edits its own config; "if" and
// "loop" hold nested steps. Text inputs accept {{placeholders}} from the trigger and
// earlier steps — the "{ }" button inserts one.

export const STEP_TYPES: StepType[] = [
  'create_record',
  'update_record',
  'upsert_record',
  'delete_record',
  'find_records',
  'assign',
  'send_email',
  'notify',
  'http_request',
  'code',
  'delay',
  'if',
  'loop',
  'stop'
];

export const stepIcons: Record<StepType, typeof Plus> = {
  create_record: FilePlus2,
  update_record: PenSquare,
  upsert_record: Replace,
  delete_record: FileX2,
  find_records: FileSearch,
  assign: Shuffle,
  send_email: Mail,
  notify: Bell,
  http_request: Globe,
  code: Code2,
  delay: Clock,
  if: GitBranch,
  loop: Repeat,
  stop: Square
};

const STEP_GROUPS: Array<{ key: string; types: StepType[] }> = [
  { key: 'records', types: ['create_record', 'update_record', 'upsert_record', 'delete_record', 'find_records', 'assign'] },
  { key: 'messages', types: ['send_email', 'notify', 'http_request'] },
  { key: 'logic', types: ['if', 'loop', 'delay', 'code', 'stop'] }
];

export function newStep(type: StepType, triggerObject?: string): WorkflowStep {
  const id = `s${Math.random().toString(36).slice(2, 8)}`;
  const recordish = ['create_record', 'update_record', 'upsert_record', 'delete_record', 'find_records', 'assign'].includes(type);
  const config: Record<string, unknown> = {};
  if (recordish) config.object = type === 'create_record' ? 'tasks' : triggerObject ?? 'leads';
  if (type === 'create_record' || type === 'update_record' || type === 'upsert_record') config.values = {};
  if (type === 'assign') config.strategy = 'round_robin';
  if (type === 'delay') Object.assign(config, { amount: 1, unit: 'hours' });
  if (type === 'if') Object.assign(config, { match: 'all', conditions: [{ left: '', op: 'eq', right: '' }] });
  if (type === 'http_request') config.method = 'POST';
  if (type === 'notify') config.to = ['owner'];
  if (type === 'code') config.source = '// `input` has trigger and steps. Return an object.\nreturn { total: 1 };';
  const step: WorkflowStep = { id, type, config };
  if (type === 'if') Object.assign(step, { then: [], else: [] });
  if (type === 'loop') step.then = [];
  return step;
}

// ---- placeholders ----

export interface VarOption {
  path: string;
  label: string;
  group: string;
}

/** Everything a step can reference: trigger data and the output of earlier steps. */
export function useVariables(def: WorkflowDef, triggerFields: FieldDef[] | undefined): VarOption[] {
  const { t } = useTranslation();
  return useMemo(() => {
    const out: VarOption[] = [];
    const tr = def.trigger;
    const g = t('tools.wf.vars.trigger');
    if (tr.object && tr.type !== 'webhook' && tr.type !== 'schedule' && !(tr.type === 'manual' && tr.manual?.mode === 'global')) {
      out.push({ path: 'trigger.recordId', label: t('tools.wf.vars.recordId'), group: g });
      out.push({ path: 'trigger.record.displayName', label: t('tools.wf.vars.recordName'), group: g });
      out.push({ path: 'trigger.record.code', label: t('tools.wf.vars.recordCode'), group: g });
      for (const f of triggerFields ?? []) out.push({ path: `trigger.record.${f.key}`, label: f.label, group: g });
      if (tr.type === 'record.updated') for (const f of triggerFields ?? []) out.push({ path: `trigger.previous.${f.key}`, label: t('tools.wf.vars.previous', { label: f.label }), group: g });
    }
    if (tr.type === 'manual') for (const f of tr.manual?.form ?? []) out.push({ path: `trigger.input.${f.key}`, label: f.label, group: t('tools.wf.vars.form') });
    if (tr.type === 'webhook') out.push({ path: 'trigger.body', label: t('tools.wf.vars.body'), group: g }, { path: 'trigger.query', label: t('tools.wf.vars.query'), group: g });
    out.push({ path: 'trigger.actorId', label: t('tools.wf.vars.actor'), group: g });
    const walk = (steps: WorkflowStep[], inLoop: boolean) => {
      for (const s of steps) {
        const name = s.name || t(`tools.wf.step.${s.type}`);
        const sg = t('tools.wf.vars.stepGroup', { name });
        const add = (k: string, label: string) => out.push({ path: `steps.${s.id}.${k}`, label, group: sg });
        if (['create_record', 'update_record', 'upsert_record'].includes(s.type)) {
          add('id', t('tools.wf.vars.newId'));
          add('record.displayName', t('tools.wf.vars.recordName'));
        }
        if (s.type === 'find_records') {
          add('count', t('tools.wf.vars.count'));
          add('first.id', t('tools.wf.vars.firstId'));
          add('records', t('tools.wf.vars.records'));
        }
        if (s.type === 'assign') add('ownerId', t('tools.wf.vars.assignee'));
        if (s.type === 'http_request') {
          add('status', t('tools.wf.vars.httpStatus'));
          add('body', t('tools.wf.vars.httpBody'));
        }
        if (s.type === 'code') out.push({ path: `steps.${s.id}`, label: t('tools.wf.vars.codeResult'), group: sg });
        if (s.type === 'loop') out.push({ path: 'loop.item', label: t('tools.wf.vars.loopItem'), group: sg }, { path: 'loop.index', label: t('tools.wf.vars.loopIndex'), group: sg });
        walk(s.then ?? [], inLoop || s.type === 'loop');
        walk(s.else ?? [], inLoop);
      }
    };
    walk(def.steps, false);
    return out;
  }, [def, triggerFields, t]);
}

function VarButton({ vars, onPick }: { vars: VarOption[]; onPick: (path: string) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const list = vars.filter((v) => !q || `${v.label} ${v.path}`.toLowerCase().includes(q.toLowerCase()));
  const groups = [...new Set(list.map((v) => v.group))];
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="end"
      width={320}
      className="p-2"
      trigger={(p) => (
        <Button type="button" size="icon-sm" variant="outline" ref={p.ref as never} onClick={p.onClick} aria-expanded={p['aria-expanded']} aria-label={t('tools.wf.insertValue')} title={t('tools.wf.insertValue')}>
          <Braces />
        </Button>
      )}
    >
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('tools.wf.searchValues')} autoFocus className="mb-2 h-8" />
      <div className="max-h-72 overflow-y-auto">
        {groups.map((g) => (
          <div key={g} className="mb-1.5">
            <p className="px-1.5 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{g}</p>
            {list
              .filter((v) => v.group === g)
              .map((v) => (
                <button
                  key={v.path}
                  type="button"
                  className="flex w-full items-center justify-between gap-2 rounded px-1.5 py-1 text-left text-[13px] hover:bg-muted"
                  onClick={() => {
                    onPick(v.path);
                    setOpen(false);
                  }}
                >
                  <span className="truncate">{v.label}</span>
                  <code className="shrink-0 truncate text-[10px] text-muted-foreground">{v.path}</code>
                </button>
              ))}
          </div>
        ))}
        {list.length === 0 ? <p className="px-1.5 py-3 text-center text-xs text-muted-foreground">{t('tools.wf.noValues')}</p> : null}
      </div>
    </Popover>
  );
}

/** Text input that takes {{placeholders}}. */
export function VarInput({ value, onChange, vars, placeholder, multiline, mono, ariaLabel }: { value: string; onChange: (v: string) => void; vars: VarOption[]; placeholder?: string; multiline?: boolean; mono?: boolean; ariaLabel?: string }) {
  const add = (p: string) => onChange(`${value}{{${p}}}`);
  return (
    <div className="flex items-start gap-1.5">
      {multiline ? (
        <Textarea value={value} onChange={(e) => onChange(e.target.value)} rows={4} placeholder={placeholder} className={cn('flex-1', mono && 'font-mono text-xs')} aria-label={ariaLabel} spellCheck={!mono} />
      ) : (
        <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className={cn('flex-1', mono && 'font-mono text-xs')} aria-label={ariaLabel} />
      )}
      <VarButton vars={vars} onPick={add} />
    </div>
  );
}

// ---- step list ----

interface ListProps {
  steps: WorkflowStep[];
  onChange: (steps: WorkflowStep[]) => void;
  vars: VarOption[];
  triggerObject?: string;
  errors: Record<string, string>;
  path: string;
  depth?: number;
}

export function StepList({ steps, onChange, vars, triggerObject, errors, path, depth = 0 }: ListProps) {
  const { t } = useTranslation();
  const set = (i: number, s: WorkflowStep | null) => onChange(s ? steps.map((x, j) => (j === i ? s : x)) : steps.filter((_, j) => j !== i));
  const move = (i: number, d: -1 | 1) => {
    const n = [...steps];
    const [s] = n.splice(i, 1);
    n.splice(i + d, 0, s!);
    onChange(n);
  };
  return (
    <div className={cn('space-y-2', depth > 0 && 'border-l-2 border-dashed border-border pl-3')}>
      {steps.map((s, i) => (
        <StepCard
          key={s.id}
          step={s}
          index={i}
          count={steps.length}
          onChange={(n) => set(i, n)}
          onRemove={() => set(i, null)}
          onMove={(d) => move(i, d)}
          vars={vars}
          triggerObject={triggerObject}
          errors={errors}
          path={`${path}.${i}`}
          depth={depth}
        />
      ))}
      <AddStep onAdd={(type) => onChange([...steps, newStep(type, triggerObject)])} label={steps.length ? t('tools.wf.addStep') : depth ? t('tools.wf.addStepHere') : t('tools.wf.addFirstStep')} />
    </div>
  );
}

function AddStep({ onAdd, label }: { onAdd: (t: StepType) => void; label: string }) {
  const { t } = useTranslation();
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button type="button" variant="ghost" size="sm" className="text-primary">
          <Plus /> {label}
        </Button>
      </MenuTrigger>
      <MenuContent align="start" className="w-64">
        {STEP_GROUPS.map((g) => (
          <div key={g.key}>
            <MenuLabel>{t(`tools.wf.stepGroups.${g.key}`)}</MenuLabel>
            {g.types.map((type) => {
              const Icon = stepIcons[type];
              return (
                <MenuItem key={type} onSelect={() => onAdd(type)}>
                  <Icon /> {t(`tools.wf.step.${type}`)}
                </MenuItem>
              );
            })}
          </div>
        ))}
      </MenuContent>
    </Menu>
  );
}

function StepCard({
  step,
  index,
  count,
  onChange,
  onRemove,
  onMove,
  vars,
  triggerObject,
  errors,
  path,
  depth
}: {
  step: WorkflowStep;
  index: number;
  count: number;
  onChange: (s: WorkflowStep) => void;
  onRemove: () => void;
  onMove: (d: -1 | 1) => void;
  vars: VarOption[];
  triggerObject?: string;
  errors: Record<string, string>;
  path: string;
  depth: number;
}) {
  const { t } = useTranslation();
  const Icon = stepIcons[step.type];
  const error = errors[path];
  const setConfig = (patch: Record<string, unknown>) => onChange({ ...step, config: { ...(step.config ?? {}), ...patch } });
  return (
    <Card className={cn('overflow-visible', error && 'border-danger/50')}>
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <span className="grid size-7 shrink-0 place-items-center rounded-md bg-primary-soft text-primary">
          <Icon className="size-4" aria-hidden />
        </span>
        <span className="shrink-0 text-xs font-semibold text-muted-foreground">{index + 1}.</span>
        <Input
          value={step.name ?? ''}
          onChange={(e) => onChange({ ...step, name: e.target.value })}
          placeholder={t(`tools.wf.step.${step.type}`)}
          className="h-8 min-w-0 flex-1 border-transparent bg-transparent px-1.5 text-[13px] font-medium shadow-none hover:border-input focus:border-input"
          aria-label={t('tools.wf.stepName')}
        />
        <code className="hidden text-[10px] text-muted-foreground sm:block" title={t('tools.wf.stepIdHint', { ph: `{{steps.${step.id}.id}}` })}>
          {step.id}
        </code>
        <Button type="button" size="icon-sm" variant="subtle" disabled={index === 0} onClick={() => onMove(-1)} aria-label={t('tools.wf.moveUp')}>
          <ArrowUp />
        </Button>
        <Button type="button" size="icon-sm" variant="subtle" disabled={index === count - 1} onClick={() => onMove(1)} aria-label={t('tools.wf.moveDown')}>
          <ArrowDown />
        </Button>
        <Button type="button" size="icon-sm" variant="subtle" onClick={onRemove} aria-label={t('tools.wf.removeStep')}>
          <Trash2 />
        </Button>
      </div>
      <div className="space-y-3 px-3 py-3">
        {error ? <p className="text-[13px] text-danger">{error}</p> : null}
        <StepConfig step={step} setConfig={setConfig} vars={vars} triggerObject={triggerObject} />
        {step.type === 'if' ? (
          <div className="grid gap-3 2xl:grid-cols-2">
            <Branch title={t('tools.wf.ifTrue')}>
              <StepList steps={step.then ?? []} onChange={(then) => onChange({ ...step, then })} vars={vars} triggerObject={triggerObject} errors={errors} path={`${path}.then`} depth={depth + 1} />
            </Branch>
            <Branch title={t('tools.wf.otherwise')}>
              <StepList steps={step.else ?? []} onChange={(els) => onChange({ ...step, else: els })} vars={vars} triggerObject={triggerObject} errors={errors} path={`${path}.else`} depth={depth + 1} />
            </Branch>
          </div>
        ) : null}
        {step.type === 'loop' ? (
          <Branch title={t('tools.wf.forEach')}>
            <StepList steps={step.then ?? []} onChange={(then) => onChange({ ...step, then })} vars={vars} triggerObject={triggerObject} errors={errors} path={`${path}.then`} depth={depth + 1} />
          </Branch>
        ) : null}
      </div>
    </Card>
  );
}

function Branch({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-lg bg-muted/30 p-2.5">
      <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</p>
      {children}
    </div>
  );
}

const str = (v: unknown) => (typeof v === 'string' ? v : v == null ? '' : String(v));

function ObjectSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation();
  const objects = useWorkspaceObjects();
  return (
    <Field label={t('tools.wf.object')}>
      <Select value={value} onChange={(e) => onChange(e.target.value)} placeholder={t('tools.wf.pickObject')} options={objects.map((o) => ({ value: o.key, label: o.label }))} />
    </Field>
  );
}

function RecordIdInput({ value, onChange, vars }: { value: string; onChange: (v: string) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  return (
    <Field label={t('tools.wf.whichRecord')} hint={t('tools.wf.whichRecordHint')}>
      <VarInput value={value} onChange={onChange} vars={vars} placeholder="{{trigger.recordId}}" mono />
    </Field>
  );
}

/** field → value rows for create / update / upsert. */
function ValuesEditor({ object, values, onChange, vars }: { object: string; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  const meta = useObjectMeta(object);
  const fields = (meta.data?.fields ?? []).filter((f) => !f.readOnly && f.type !== 'files');
  const entries = Object.entries(values);
  const unused = fields.filter((f) => !(f.key in values));
  return (
    <div>
      <p className="mb-1.5 text-[13px] font-medium">{t('tools.wf.setFields')}</p>
      <div className="space-y-2">
        {entries.map(([k, v]) => {
          const f = fields.find((x) => x.key === k);
          const opts = f?.options;
          return (
            <div key={k} className="grid gap-1.5 sm:grid-cols-[180px_minmax(0,1fr)_2rem] sm:items-start">
              <p className="truncate pt-2 text-[13px] text-muted-foreground">{f?.label ?? k}</p>
              {opts && opts.length ? (
                <Select value={str(v)} onChange={(e) => onChange({ ...values, [k]: e.target.value })} options={opts.map((o) => ({ value: o.value, label: o.label }))} placeholder={t('tools.wf.pickValue')} />
              ) : f?.type === 'boolean' ? (
                <Select value={str(v)} onChange={(e) => onChange({ ...values, [k]: e.target.value === 'true' })} options={[{ value: 'true', label: t('tools.common.yes') }, { value: 'false', label: t('tools.common.no') }]} />
              ) : (
                <VarInput value={str(v)} onChange={(nv) => onChange({ ...values, [k]: nv })} vars={vars} placeholder={f?.type === 'date' ? 'YYYY-MM-DD' : undefined} multiline={f?.type === 'textarea'} ariaLabel={f?.label ?? k} />
              )}
              <Button
                type="button"
                size="icon-sm"
                variant="subtle"
                onClick={() => {
                  const n = { ...values };
                  delete n[k];
                  onChange(n);
                }}
                aria-label={t('tools.common.removeName', { name: f?.label ?? k })}
              >
                <Trash2 />
              </Button>
            </div>
          );
        })}
        {unused.length ? (
          <Select
            className="h-8 w-60 text-xs"
            value=""
            onChange={(e) => e.target.value && onChange({ ...values, [e.target.value]: '' })}
            placeholder={t('tools.wf.addField')}
            options={unused.map((f) => ({ value: f.key, label: f.label }))}
          />
        ) : null}
      </div>
    </div>
  );
}

function StepConfig({ step, setConfig, vars, triggerObject }: { step: WorkflowStep; setConfig: (p: Record<string, unknown>) => void; vars: VarOption[]; triggerObject?: string }) {
  const { t } = useTranslation();
  const c = step.config ?? {};
  const object = str(c.object);
  switch (step.type) {
    case 'create_record':
      return (
        <>
          <ObjectSelect value={object} onChange={(v) => setConfig({ object: v, values: {} })} />
          {object ? <ValuesEditor object={object} values={(c.values as Record<string, unknown>) ?? {}} onChange={(values) => setConfig({ values })} vars={vars} /> : null}
        </>
      );
    case 'update_record':
      return (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            <ObjectSelect value={object} onChange={(v) => setConfig({ object: v, values: {} })} />
            <RecordIdInput value={str(c.recordId)} onChange={(v) => setConfig({ recordId: v })} vars={vars} />
          </div>
          {object ? <ValuesEditor object={object} values={(c.values as Record<string, unknown>) ?? {}} onChange={(values) => setConfig({ values })} vars={vars} /> : null}
        </>
      );
    case 'upsert_record':
      return <UpsertConfig c={c} setConfig={setConfig} vars={vars} />;
    case 'delete_record':
      return (
        <div className="grid gap-3 sm:grid-cols-2">
          <ObjectSelect value={object} onChange={(v) => setConfig({ object: v })} />
          <RecordIdInput value={str(c.recordId)} onChange={(v) => setConfig({ recordId: v })} vars={vars} />
        </div>
      );
    case 'find_records':
      return <FindConfig c={c} setConfig={setConfig} />;
    case 'assign':
      return <AssignConfig c={c} setConfig={setConfig} vars={vars} />;
    case 'send_email':
      return (
        <>
          <Field label={t('tools.wf.emailTo')} hint={t('tools.wf.emailToHint')}>
            <VarInput value={str(c.to)} onChange={(v) => setConfig({ to: v })} vars={vars} placeholder="{{trigger.record.email}}" />
          </Field>
          <Field label={t('tools.wf.subject')}>
            <VarInput value={str(c.subject)} onChange={(v) => setConfig({ subject: v })} vars={vars} />
          </Field>
          <Field label={t('tools.wf.message')}>
            <VarInput value={str(c.body)} onChange={(v) => setConfig({ body: v })} vars={vars} multiline />
          </Field>
          {triggerObject ? (
            <p className="text-xs text-muted-foreground">
              <label className="inline-flex items-center gap-1.5">
                <input
                  type="checkbox"
                  checked={Boolean(c.linkObject)}
                  onChange={(e) => setConfig(e.target.checked ? { linkObject: triggerObject, linkRecordId: '{{trigger.recordId}}' } : { linkObject: '', linkRecordId: '' })}
                />
                {t('tools.wf.logOnRecord')}
              </label>
            </p>
          ) : null}
        </>
      );
    case 'notify':
      return <NotifyConfig c={c} setConfig={setConfig} vars={vars} />;
    case 'http_request':
      return (
        <>
          <div className="grid gap-3 sm:grid-cols-[120px_minmax(0,1fr)]">
            <Field label={t('tools.wf.method')}>
              <Select value={str(c.method) || 'POST'} onChange={(e) => setConfig({ method: e.target.value })} options={['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((m) => ({ value: m, label: m }))} />
            </Field>
            <Field label={t('tools.wf.url')}>
              <VarInput value={str(c.url)} onChange={(v) => setConfig({ url: v })} vars={vars} placeholder="https://api.example.com/…" mono />
            </Field>
          </div>
          <JsonConfig label={t('tools.wf.headers')} hint={t('tools.wf.headersHint')} value={c.headers} onChange={(v) => setConfig({ headers: v })} />
          {str(c.method) !== 'GET' ? (
            <Field label={t('tools.wf.requestBody')} hint={t('tools.wf.requestBodyHint')}>
              <VarInput value={typeof c.body === 'string' ? c.body : c.body ? JSON.stringify(c.body, null, 2) : ''} onChange={(v) => setConfig({ body: v })} vars={vars} multiline mono placeholder={'{"email": "{{trigger.record.email}}"}'} />
            </Field>
          ) : null}
        </>
      );
    case 'code':
      return (
        <Field label={t('tools.wf.code')} hint={t('tools.wf.codeHint')}>
          <Textarea value={str(c.source)} onChange={(e) => setConfig({ source: e.target.value })} rows={8} className="font-mono text-xs" spellCheck={false} />
        </Field>
      );
    case 'delay':
      return (
        <div className="grid gap-3 sm:grid-cols-[120px_160px_minmax(0,1fr)]">
          <Field label={t('tools.wf.wait')}>
            <Input type="number" min={1} value={str(c.amount)} onChange={(e) => setConfig({ amount: Number(e.target.value), until: '' })} />
          </Field>
          <Field label={t('tools.wf.unit')}>
            <Select value={str(c.unit) || 'hours'} onChange={(e) => setConfig({ unit: e.target.value })} options={(['minutes', 'hours', 'days'] as const).map((u) => ({ value: u, label: t(`tools.wf.units.${u}`) }))} />
          </Field>
          <Field label={t('tools.wf.orUntil')} hint={t('tools.wf.orUntilHint')}>
            <VarInput value={str(c.until)} onChange={(v) => setConfig({ until: v })} vars={vars} placeholder="{{trigger.record.closeDate}}" />
          </Field>
        </div>
      );
    case 'if':
      return <ConditionsConfig c={c} setConfig={setConfig} vars={vars} />;
    case 'loop':
      return (
        <Field label={t('tools.wf.items')} hint={t('tools.wf.itemsHint')}>
          <VarInput value={str(c.items)} onChange={(v) => setConfig({ items: v })} vars={vars} placeholder="{{steps.s1.records}}" mono />
        </Field>
      );
    case 'stop':
      return <p className="text-[13px] text-muted-foreground">{t('tools.wf.stopHint')}</p>;
  }
}

function UpsertConfig({ c, setConfig, vars }: { c: Record<string, unknown>; setConfig: (p: Record<string, unknown>) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  const object = str(c.object);
  const meta = useObjectMeta(object);
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-3">
        <ObjectSelect value={object} onChange={(v) => setConfig({ object: v, values: {}, matchField: '' })} />
        <Field label={t('tools.wf.matchBy')}>
          <Select value={str(c.matchField)} onChange={(e) => setConfig({ matchField: e.target.value })} placeholder={t('tools.wf.pickField')} options={(meta.data?.fields ?? []).filter((f) => ['text', 'email', 'phone', 'url', 'number'].includes(f.type) || f.key === 'code').map((f) => ({ value: f.key, label: f.label }))} />
        </Field>
        <Field label={t('tools.wf.matchValue')}>
          <VarInput value={str(c.matchValue)} onChange={(v) => setConfig({ matchValue: v })} vars={vars} />
        </Field>
      </div>
      {object ? <ValuesEditor object={object} values={(c.values as Record<string, unknown>) ?? {}} onChange={(values) => setConfig({ values })} vars={vars} /> : null}
    </>
  );
}

function FindConfig({ c, setConfig }: { c: Record<string, unknown>; setConfig: (p: Record<string, unknown>) => void }) {
  const { t } = useTranslation();
  const object = str(c.object);
  const meta = useObjectMeta(object);
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_140px]">
        <ObjectSelect value={object} onChange={(v) => setConfig({ object: v, filter: undefined })} />
        <Field label={t('tools.wf.limit')}>
          <Input type="number" min={1} max={200} value={str(c.limit) || '50'} onChange={(e) => setConfig({ limit: Number(e.target.value) })} />
        </Field>
      </div>
      {meta.data ? (
        <div>
          <p className="mb-1.5 text-[13px] font-medium">{t('tools.wf.where')}</p>
          <FilterBuilder meta={meta.data} value={(c.filter as FilterGroup) ?? emptyGroup()} onChange={(g) => setConfig({ filter: g })} />
        </div>
      ) : null}
    </>
  );
}

function AssignConfig({ c, setConfig, vars }: { c: Record<string, unknown>; setConfig: (p: Record<string, unknown>) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const teams = useQuery({ queryKey: toolKeys.one(code, 'teams'), queryFn: () => api.teams(), staleTime: 60_000 });
  const pool = c.teamId ? 'team' : 'people';
  const people = (c.memberLabels as Array<{ id: string; label: string }>) ?? ((c.members as string[]) ?? []).map((id) => ({ id, label: id.slice(0, 8) }));
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2">
        <ObjectSelect value={str(c.object)} onChange={(v) => setConfig({ object: v })} />
        <RecordIdInput value={str(c.recordId)} onChange={(v) => setConfig({ recordId: v })} vars={vars} />
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('tools.wf.strategy')}>
          <Select value={str(c.strategy) || 'round_robin'} onChange={(e) => setConfig({ strategy: e.target.value })} options={(['round_robin', 'least_loaded', 'random'] as const).map((s) => ({ value: s, label: t(`tools.wf.strategies.${s}`) }))} />
        </Field>
        <Field label={t('tools.wf.pool')}>
          <Select
            value={pool}
            onChange={(e) => setConfig(e.target.value === 'team' ? { teamId: teams.data?.data[0]?.id ?? '', members: [] } : { teamId: '', members: [] })}
            options={[
              { value: 'team', label: t('tools.wf.poolTeam') },
              { value: 'people', label: t('tools.wf.poolPeople') }
            ]}
          />
        </Field>
      </div>
      {pool === 'team' ? (
        <Field label={t('tools.wf.team')} hint={teams.data && !teams.data.data.length ? t('tools.wf.noTeams') : undefined}>
          <Select value={str(c.teamId)} onChange={(e) => setConfig({ teamId: e.target.value })} options={(teams.data?.data ?? []).map((tm) => ({ value: tm.id, label: `${tm.name} (${tm.members.length})` }))} />
        </Field>
      ) : (
        <div>
          <p className="mb-1.5 text-[13px] font-medium">{t('tools.wf.people')}</p>
          <PeoplePicker value={people} onChange={(v) => setConfig({ members: v.map((p) => p.id), memberLabels: v })} />
        </div>
      )}
    </>
  );
}

function NotifyConfig({ c, setConfig, vars }: { c: Record<string, unknown>; setConfig: (p: Record<string, unknown>) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  const to = (c.to as string[]) ?? [];
  const people = (c.toLabels as Array<{ id: string; label: string }>) ?? to.filter((x) => x !== 'owner' && x !== 'creator').map((id) => ({ id, label: id.slice(0, 8) }));
  const toggle = (k: string) => setConfig({ to: to.includes(k) ? to.filter((x) => x !== k) : [...to, k] });
  return (
    <>
      <div>
        <p className="mb-1.5 text-[13px] font-medium">{t('tools.wf.notifyWho')}</p>
        <div className="mb-2 flex flex-wrap gap-4 text-[13px]">
          <label className="inline-flex items-center gap-1.5">
            <input type="checkbox" checked={to.includes('owner')} onChange={() => toggle('owner')} /> {t('tools.wf.recordOwner')}
          </label>
          <label className="inline-flex items-center gap-1.5">
            <input type="checkbox" checked={to.includes('creator')} onChange={() => toggle('creator')} /> {t('tools.wf.workflowCreator')}
          </label>
        </div>
        <PeoplePicker
          value={people}
          onChange={(v) => setConfig({ to: [...to.filter((x) => x === 'owner' || x === 'creator'), ...v.map((p) => p.id)], toLabels: v })}
          placeholder={t('tools.wf.alsoNotify')}
        />
      </div>
      <Field label={t('tools.wf.notifyTitle')}>
        <VarInput value={str(c.title)} onChange={(v) => setConfig({ title: v })} vars={vars} placeholder={t('tools.wf.notifyTitlePlaceholder', { ph: '{{trigger.record.displayName}}' })} />
      </Field>
      <Field label={t('tools.wf.message')}>
        <VarInput value={str(c.body)} onChange={(v) => setConfig({ body: v })} vars={vars} />
      </Field>
    </>
  );
}

function ConditionsConfig({ c, setConfig, vars }: { c: Record<string, unknown>; setConfig: (p: Record<string, unknown>) => void; vars: VarOption[] }) {
  const { t } = useTranslation();
  const conds = (c.conditions as Array<{ left: string; op: string; right: string }>) ?? [];
  const set = (i: number, patch: Partial<{ left: string; op: string; right: string }> | null) =>
    setConfig({ conditions: patch ? conds.map((x, j) => (j === i ? { ...x, ...patch } : x)) : conds.filter((_, j) => j !== i) });
  const OPS = ['eq', 'neq', 'contains', 'notContains', 'startsWith', 'in', 'gt', 'gte', 'lt', 'lte', 'empty', 'notEmpty', 'isTrue', 'isFalse'];
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-[13px]">
        {t('tools.wf.continueWhen')}
        <Select className="h-8 w-24 text-xs" value={str(c.match) || 'all'} onChange={(e) => setConfig({ match: e.target.value })} options={[{ value: 'all', label: t('lists.filter.all') }, { value: 'any', label: t('lists.filter.any') }]} />
        {t('tools.wf.ofTheseAreTrue')}
      </div>
      {conds.map((x, i) => (
        <div key={i} className="grid gap-1.5 sm:grid-cols-[minmax(0,1fr)_150px_minmax(0,1fr)_2rem] sm:items-start">
          <VarInput value={x.left} onChange={(v) => set(i, { left: v })} vars={vars} placeholder="{{trigger.record.status}}" ariaLabel={t('tools.wf.leftValue')} />
          <Select value={x.op} onChange={(e) => set(i, { op: e.target.value })} options={OPS.map((o) => ({ value: o, label: t(`tools.wf.ops.${o}`) }))} />
          {['empty', 'notEmpty', 'isTrue', 'isFalse'].includes(x.op) ? <span /> : <VarInput value={x.right} onChange={(v) => set(i, { right: v })} vars={vars} ariaLabel={t('tools.wf.rightValue')} />}
          <Button type="button" size="icon-sm" variant="subtle" onClick={() => set(i, null)} aria-label={t('lists.filter.remove')}>
            <Trash2 />
          </Button>
        </div>
      ))}
      <Button type="button" size="sm" variant="ghost" onClick={() => setConfig({ conditions: [...conds, { left: '', op: 'eq', right: '' }] })}>
        <Plus /> {t('lists.filter.addCondition')}
      </Button>
    </div>
  );
}

function JsonConfig({ label, hint, value, onChange }: { label: string; hint?: string; value: unknown; onChange: (v: unknown) => void }) {
  const { t } = useTranslation();
  const [text, setText] = useState(value ? JSON.stringify(value, null, 2) : '');
  const [bad, setBad] = useState(false);
  return (
    <Field label={label} hint={bad ? undefined : hint} error={bad ? t('tools.wf.badJson') : undefined}>
      <Textarea
        value={text}
        rows={3}
        className="font-mono text-xs"
        placeholder='{"Authorization": "Bearer …"}'
        spellCheck={false}
        onChange={(e) => {
          setText(e.target.value);
          if (!e.target.value.trim()) {
            setBad(false);
            onChange(undefined);
            return;
          }
          try {
            onChange(JSON.parse(e.target.value));
            setBad(false);
          } catch {
            setBad(true);
          }
        }}
      />
    </Field>
  );
}
