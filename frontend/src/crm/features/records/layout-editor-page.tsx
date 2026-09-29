import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Archive,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronUp,
  Columns2,
  Ellipsis,
  Eye,
  EyeOff,
  FolderInput,
  GripVertical,
  LayoutTemplate,
  Lock,
  Monitor,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Smartphone,
  Sparkles,
  Square,
  Star,
  Trash2,
  X
} from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, Layout, LayoutSection, ObjectKey, ObjectMeta } from '@crm/api/types';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger, Tooltip } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { FieldDialog, type FieldDialogState } from './field-dialog';
import { fieldIndex, newId, objectIcon, recordKeys, useObjectMeta } from './use-object-meta';
import { listHref, useRecordScope } from './record-scope';
import { isForbidden, RecordNoAccess } from './record-states';

const MAX_HIGHLIGHTS = 6;

// ---------------------------------------------------------------------------
// Pure layout operations
// ---------------------------------------------------------------------------

function insertAt<T>(arr: T[], item: T, index: number): T[] {
  const i = Math.max(0, Math.min(index, arr.length));
  return [...arr.slice(0, i), item, ...arr.slice(i)];
}

/** Drops unknown field keys and duplicates so the layout always validates server-side. */
function sanitize(layout: Layout, known: Set<string>): Layout {
  const seen = new Set<string>();
  return {
    highlights: [...new Set(layout.highlights)].filter((k) => known.has(k)).slice(0, MAX_HIGHLIGHTS),
    sections: layout.sections.map((s) => ({
      id: s.id,
      title: s.title,
      columns: s.columns === 1 ? 1 : 2,
      fields: s.fields.filter((k) => known.has(k) && !seen.has(k) && (seen.add(k), true))
    }))
  };
}

function placeInSection(l: Layout, key: string, sectionId: string, index: number): Layout {
  let idx = index;
  const stripped = l.sections.map((s) => {
    const pos = s.fields.indexOf(key);
    if (pos === -1) return s;
    if (s.id === sectionId && pos < idx) idx -= 1;
    return { ...s, fields: s.fields.filter((k) => k !== key) };
  });
  return { ...l, sections: stripped.map((s) => (s.id === sectionId ? { ...s, fields: insertAt(s.fields, key, idx) } : s)) };
}

function hideField(l: Layout, key: string): Layout {
  return { ...l, sections: l.sections.map((s) => (s.fields.includes(key) ? { ...s, fields: s.fields.filter((k) => k !== key) } : s)) };
}

function removeEverywhere(l: Layout, key: string): Layout {
  const h = hideField(l, key);
  return { ...h, highlights: h.highlights.filter((k) => k !== key) };
}

function placeInHighlights(l: Layout, key: string, index: number): Layout {
  const pos = l.highlights.indexOf(key);
  let idx = index;
  if (pos !== -1 && pos < idx) idx -= 1;
  return { ...l, highlights: insertAt(l.highlights.filter((k) => k !== key), key, idx) };
}

function moveSection(l: Layout, from: number, to: number): Layout {
  if (from === to || to < 0 || to >= l.sections.length) return l;
  const next = [...l.sections];
  const [s] = next.splice(from, 1);
  next.splice(to, 0, s!);
  return { ...l, sections: next };
}

// ---------------------------------------------------------------------------
// Drag & drop model (native HTML5 DnD; the payload also lives in state because
// dataTransfer can't be read during dragover)
// ---------------------------------------------------------------------------

type DragItem = { type: 'field'; key: string; from: 'palette' | 'section' | 'highlights' } | { type: 'section'; sectionId: string };
type DropTarget = { kind: 'section'; sectionId: string; index: number } | { kind: 'highlights'; index: number } | { kind: 'palette' } | { kind: 'sectionOrder'; index: number };

const sameTarget = (a: DropTarget | null, b: DropTarget | null) => JSON.stringify(a) === JSON.stringify(b);

export function LayoutEditorPage({ object }: { object: ObjectKey }) {
  const scope = useRecordScope();
  if (!scope.canCustomize) return <NoAccessPage />;
  return <LayoutEditorView object={object} />;
}

function LayoutEditorView({ object }: { object: ObjectKey }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const recordsApi = scope.api;
  const metaQ = useObjectMeta(object);
  const meta = metaQ.data;
  useDocumentTitle(meta ? t('records.layout.title', { object: meta.labelSingular }) : t('records.common.loading'));

  const [draft, setDraft] = useState<Layout | null>(null);
  const [baseline, setBaseline] = useState<Layout | null>(null);
  const [drag, setDrag] = useState<DragItem | null>(null);
  const [drop, setDrop] = useState<DropTarget | null>(null);
  const [mode, setMode] = useState<'build' | 'preview'>('build');
  const [device, setDevice] = useState<'desktop' | 'mobile'>('desktop');
  const [fieldDialog, setFieldDialog] = useState<FieldDialogState>(null);
  const [archiveField, setArchiveField] = useState<FieldDef | null>(null);
  const [confirmReset, setConfirmReset] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const sectionRefs = useRef(new Map<string, HTMLElement>());

  const byKey = useMemo(() => fieldIndex(meta), [meta]);

  // Initialise the draft once; later meta refreshes (new/edited fields) must not clobber edits.
  useEffect(() => {
    if (!meta || draft) return;
    const l = sanitize(meta.layout, new Set(meta.fields.map((f) => f.key)));
    setDraft(l);
    setBaseline(l);
  }, [meta, draft]);

  const dirty = Boolean(draft && baseline && JSON.stringify(draft) !== JSON.stringify(baseline));

  useEffect(() => {
    if (!dirty) return;
    const h = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', h);
    return () => window.removeEventListener('beforeunload', h);
  }, [dirty]);

  const update = (fn: (l: Layout) => Layout) => setDraft((l) => (l ? fn(l) : l));

  const adopt = (m: ObjectMeta) => qc.setQueryData(recordKeys.meta(scope.prefix, object), m);

  const save = useMutation({
    mutationFn: (l: Layout) => recordsApi.saveLayout(object, l),
    onSuccess: (m) => {
      adopt(m);
      const l = sanitize(m.layout, new Set(m.fields.map((f) => f.key)));
      setDraft(l);
      setBaseline(l);
      toast.success(t('records.layout.saved'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const reset = useMutation({
    mutationFn: () => recordsApi.resetLayout(object),
    onSuccess: (m) => {
      adopt(m);
      const l = sanitize(m.layout, new Set(m.fields.map((f) => f.key)));
      setDraft(l);
      setBaseline(l);
      setConfirmReset(false);
      toast.success(t('records.layout.resetDone'));
    },
    onError: (e) => {
      setConfirmReset(false);
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const archive = useMutation({
    mutationFn: (key: string) => recordsApi.deleteField(object, key),
    onSuccess: (m, key) => {
      adopt(m);
      setDraft((l) => (l ? removeEverywhere(l, key) : l));
      setBaseline((l) => (l ? removeEverywhere(l, key) : l));
      setArchiveField(null);
      toast.success(t('records.layout.archived'));
    },
    onError: (e) => {
      setArchiveField(null);
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const leave = () => {
    const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0;
    if (idx > 0) navigate(-1);
    else navigate(listHref(scope, object));
  };

  const onSave = () => {
    if (!draft) return;
    const untitled = t('records.layout.untitledSection');
    save.mutate({ ...draft, sections: draft.sections.map((s) => ({ ...s, title: s.title.trim() || untitled })) });
  };

  // ---- actions ----
  const addToHighlights = (key: string, index?: number) => {
    if (!draft) return;
    if (!draft.highlights.includes(key) && draft.highlights.length >= MAX_HIGHLIGHTS) {
      toast.error(t('records.layout.highlightsFull', { max: MAX_HIGHLIGHTS }));
      return;
    }
    update((l) => placeInHighlights(l, key, index ?? l.highlights.length));
  };

  const addSection = () => {
    const id = newId('sec');
    update((l) => ({ ...l, sections: [...l.sections, { id, title: t('records.layout.newSectionTitle'), columns: 2, fields: [] }] }));
    // Focus the new section's title once it renders.
    setTimeout(() => sectionRefs.current.get(id)?.querySelector<HTMLInputElement>('input')?.select(), 50);
  };

  const deleteSection = (sectionId: string) => {
    if (!draft) return;
    const before = draft;
    const s = draft.sections.find((x) => x.id === sectionId);
    update((l) => ({ ...l, sections: l.sections.filter((x) => x.id !== sectionId) }));
    toast(t('records.layout.sectionRemoved', { title: s?.title || t('records.layout.untitledSection'), count: s?.fields.length ?? 0 }), {
      action: { label: t('records.layout.undo'), onClick: () => setDraft(before) }
    });
  };

  const patchSection = (sectionId: string, patch: Partial<LayoutSection>) =>
    update((l) => ({ ...l, sections: l.sections.map((s) => (s.id === sectionId ? { ...s, ...patch } : s)) }));

  // ---- drag & drop ----
  const startDrag = (e: DragEvent, item: DragItem, image?: HTMLElement | null) => {
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', item.type === 'field' ? item.key : item.sectionId);
    if (image) e.dataTransfer.setDragImage(image, 16, 16);
    // Defer so the drag image is captured before styles change.
    setTimeout(() => setDrag(item), 0);
  };
  const endDrag = () => {
    setDrag(null);
    setDrop(null);
  };
  const over = (e: DragEvent, target: DropTarget) => {
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = 'move';
    setDrop((cur) => (sameTarget(cur, target) ? cur : target));
  };
  const commitDrop = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const item = drag;
    const target = drop;
    endDrag();
    if (!item || !target || !draft) return;
    if (item.type === 'section') {
      if (target.kind !== 'sectionOrder') return;
      const from = draft.sections.findIndex((s) => s.id === item.sectionId);
      const to = target.index > from ? target.index - 1 : target.index;
      update((l) => moveSection(l, from, to));
      return;
    }
    switch (target.kind) {
      case 'section':
        if (item.from === 'highlights') return;
        update((l) => placeInSection(l, item.key, target.sectionId, target.index));
        break;
      case 'highlights':
        addToHighlights(item.key, target.index);
        break;
      case 'palette':
        if (item.from === 'section') update((l) => hideField(l, item.key));
        else if (item.from === 'highlights') update((l) => ({ ...l, highlights: l.highlights.filter((k) => k !== item.key) }));
        break;
      default:
        break;
    }
  };
  /** before/after the hovered element, by pointer position. */
  const half = (e: DragEvent, horizontal: boolean) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    return horizontal ? e.clientX > r.left + r.width / 2 : e.clientY > r.top + r.height / 2;
  };

  // ---- render ----
  if (isForbidden(metaQ.error)) return <RecordNoAccess />;

  if (metaQ.isError) {
    return (
      <PageContainer wide>
        <Card>
          <ErrorState
            title={t('records.common.loadError')}
            message={isApiError(metaQ.error) ? metaQ.error.message : undefined}
            requestId={isApiError(metaQ.error) ? metaQ.error.requestId : undefined}
            onRetry={() => void metaQ.refetch()}
          />
        </Card>
      </PageContainer>
    );
  }

  if (!meta || !draft) return <EditorSkeleton />;

  const Icon = objectIcon(object, meta.icon);
  const placed = new Set(draft.sections.flatMap((s) => s.fields));
  const sectionOptions = draft.sections.map((s) => ({ id: s.id, title: s.title || t('records.layout.untitledSection') }));
  const persistedSectionIds = new Set((baseline?.sections ?? []).map((s) => s.id));
  const fieldDrag = drag?.type === 'field' ? drag : null;

  return (
    <PageContainer wide className={cn(drag && 'select-none')}>
      <div onDragOver={() => drag && setDrop(null)}>
        <PageHeader
          crumbs={[
            { label: t('records.layout.setup') },
            { label: meta.labelPlural, to: listHref(scope, object) },
            { label: t('records.layout.crumb') }
          ]}
          title={t('records.layout.title', { object: meta.labelSingular })}
          description={t('records.layout.subtitle')}
          icon={
            <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
              <Icon className="size-5" aria-hidden />
            </span>
          }
          badges={dirty ? <Badge tone="warning">{t('records.layout.unsaved')}</Badge> : null}
          actions={
            <>
              <Button variant="ghost" size="sm" onClick={() => setConfirmReset(true)}>
                <RotateCcw /> {t('records.layout.reset')}
              </Button>
              <Button variant="outline" size="sm" onClick={() => (dirty ? setConfirmDiscard(true) : leave())}>
                {t('common.cancel')}
              </Button>
              <Button size="sm" onClick={onSave} disabled={!dirty} loading={save.isPending}>
                {t('records.layout.save')}
              </Button>
            </>
          }
        />

        <div className="grid gap-6 lg:grid-cols-[280px_minmax(0,1fr)]">
          <FieldPalette
            meta={meta}
            placed={placed}
            highlights={draft.highlights}
            sections={sectionOptions}
            dropActive={drop?.kind === 'palette'}
            canDropHere={Boolean(fieldDrag && fieldDrag.from !== 'palette')}
            onDragStartField={(e, key) => startDrag(e, { type: 'field', key, from: 'palette' })}
            onDragEnd={endDrag}
            onDragOver={(e) => fieldDrag && fieldDrag.from !== 'palette' && over(e, { kind: 'palette' })}
            onDrop={commitDrop}
            onAddTo={(key, sectionId) => {
              const s = draft.sections.find((x) => x.id === sectionId);
              update((l) => placeInSection(l, key, sectionId, s?.fields.length ?? 0));
            }}
            onHide={(key) => update((l) => hideField(l, key))}
            onHighlight={(key) => addToHighlights(key)}
            onNewField={() => setFieldDialog({ mode: 'create', sectionId: draft.sections[0]?.id })}
            onEditField={(f) => setFieldDialog({ mode: 'edit', field: f })}
            onArchiveField={setArchiveField}
          />

          <div className="min-w-0 space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <SegmentedFilter
                value={mode}
                onChange={setMode}
                options={[
                  { value: 'build', label: t('records.layout.modeBuild') },
                  { value: 'preview', label: t('records.layout.modePreview') }
                ]}
              />
              {mode === 'preview' ? (
                <div className="inline-flex rounded-md border bg-muted/60 p-0.5" role="group" aria-label={t('records.layout.device')}>
                  {(['desktop', 'mobile'] as const).map((d) => {
                    const DIcon = d === 'desktop' ? Monitor : Smartphone;
                    return (
                      <button
                        key={d}
                        type="button"
                        aria-pressed={device === d}
                        onClick={() => setDevice(d)}
                        className={cn('flex items-center gap-1.5 rounded px-2.5 py-1 text-[13px] font-medium', device === d ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}
                      >
                        <DIcon className="size-3.5" aria-hidden /> {t(`records.layout.${d}`)}
                      </button>
                    );
                  })}
                </div>
              ) : (
                <p className="hidden text-xs text-muted-foreground md:block">{t('records.layout.dndHint')}</p>
              )}
            </div>

            {mode === 'preview' ? (
              <LayoutPreview meta={meta} layout={draft} device={device} />
            ) : (
              <>
                {/* Highlights strip */}
                <Card
                  className={cn('p-4 transition-colors', fieldDrag && fieldDrag.from !== 'highlights' && 'border-dashed border-primary/40', drop?.kind === 'highlights' && 'bg-primary-soft/40')}
                  onDragOver={(e) => fieldDrag && over(e, { kind: 'highlights', index: draft.highlights.length })}
                  onDrop={commitDrop}
                >
                  <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <h2 className="flex items-center gap-2 text-[13px] font-semibold text-foreground">
                        <Star className="size-4 text-warning" aria-hidden />
                        {t('records.layout.highlights')}
                        <span className="text-xs font-normal tabular-nums text-muted-foreground">
                          {draft.highlights.length}/{MAX_HIGHLIGHTS}
                        </span>
                      </h2>
                      <p className="text-xs text-muted-foreground">{t('records.layout.highlightsHint')}</p>
                    </div>
                  </div>
                  {draft.highlights.length === 0 ? (
                    <div className="rounded-md border border-dashed px-3 py-4 text-center text-[13px] text-muted-foreground">{t('records.layout.highlightsEmpty')}</div>
                  ) : (
                    <ul className="flex flex-wrap gap-2">
                      {draft.highlights.map((key, i) => {
                        const f = byKey.get(key);
                        if (!f) return null;
                        const ind = drop?.kind === 'highlights' ? (drop.index === i ? 'before' : drop.index === i + 1 && i === draft.highlights.length - 1 ? 'after' : null) : null;
                        return (
                          <li
                            key={key}
                            draggable
                            onDragStart={(e) => startDrag(e, { type: 'field', key, from: 'highlights' })}
                            onDragEnd={endDrag}
                            onDragOver={(e) => fieldDrag && over(e, { kind: 'highlights', index: half(e, true) ? i + 1 : i })}
                            onDrop={commitDrop}
                            className={cn(
                              'relative flex items-center gap-1 rounded-md border bg-background py-1 pl-1.5 pr-1 text-[13px] shadow-sm',
                              fieldDrag?.key === key && fieldDrag.from === 'highlights' && 'opacity-40'
                            )}
                          >
                            <DropBar position={ind} horizontal />
                            <GripVertical className="size-3.5 cursor-grab text-muted-foreground/60" aria-hidden />
                            <span className="font-medium text-foreground">{f.label}</span>
                            <Menu>
                              <MenuTrigger asChild>
                                <button type="button" className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground" aria-label={t('records.layout.chipActions', { label: f.label })}>
                                  <Ellipsis className="size-3.5" />
                                </button>
                              </MenuTrigger>
                              <MenuContent align="end" className="min-w-[10rem]">
                                <MenuItem disabled={i === 0} onSelect={() => update((l) => placeInHighlights(l, key, i - 1))}>
                                  <ArrowLeft /> {t('records.layout.moveLeft')}
                                </MenuItem>
                                <MenuItem disabled={i === draft.highlights.length - 1} onSelect={() => update((l) => placeInHighlights(l, key, i + 2))}>
                                  <ArrowRight /> {t('records.layout.moveRight')}
                                </MenuItem>
                              </MenuContent>
                            </Menu>
                            <button
                              type="button"
                              onClick={() => update((l) => ({ ...l, highlights: l.highlights.filter((k) => k !== key) }))}
                              className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-danger-soft hover:text-danger"
                              aria-label={t('records.layout.removeHighlight', { label: f.label })}
                            >
                              <X className="size-3.5" />
                            </button>
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </Card>

                {draft.sections.length === 0 ? (
                  <Card>
                    <EmptyState
                      icon={LayoutTemplate}
                      title={t('records.layout.noSectionsTitle')}
                      body={t('records.layout.noSectionsBody')}
                      action={
                        <Button onClick={addSection}>
                          <Plus /> {t('records.layout.addSection')}
                        </Button>
                      }
                    />
                  </Card>
                ) : null}

                {draft.sections.map((s, si) => {
                  const sectionInd = drop?.kind === 'sectionOrder' ? (drop.index === si ? 'before' : drop.index === si + 1 && si === draft.sections.length - 1 ? 'after' : null) : null;
                  const dropHere = drop?.kind === 'section' && drop.sectionId === s.id ? drop : null;
                  return (
                    <section
                      key={s.id}
                      ref={(el) => {
                        if (el) sectionRefs.current.set(s.id, el);
                        else sectionRefs.current.delete(s.id);
                      }}
                      aria-label={s.title || t('records.layout.untitledSection')}
                      onDragOver={(e) => {
                        if (drag?.type === 'section') over(e, { kind: 'sectionOrder', index: half(e, false) ? si + 1 : si });
                        else if (fieldDrag && fieldDrag.from !== 'highlights') over(e, { kind: 'section', sectionId: s.id, index: s.fields.length });
                      }}
                      onDrop={commitDrop}
                      className={cn(
                        'relative rounded-lg border bg-card shadow-card transition-[opacity,box-shadow]',
                        dropHere && 'ring-2 ring-primary/30',
                        drag?.type === 'section' && drag.sectionId === s.id && 'opacity-40'
                      )}
                    >
                      <DropBar position={sectionInd} />
                      <div className="flex flex-wrap items-center gap-2 border-b bg-muted/30 px-2 py-2 sm:px-3">
                        <span
                          draggable
                          onDragStart={(e) => startDrag(e, { type: 'section', sectionId: s.id }, sectionRefs.current.get(s.id))}
                          onDragEnd={endDrag}
                          className="grid size-7 cursor-grab place-items-center rounded text-muted-foreground/70 hover:bg-muted hover:text-foreground active:cursor-grabbing"
                          title={t('records.layout.dragSection')}
                          aria-hidden
                        >
                          <GripVertical className="size-4" />
                        </span>
                        <input
                          value={s.title}
                          onChange={(e) => patchSection(s.id, { title: e.target.value })}
                          placeholder={t('records.layout.untitledSection')}
                          aria-label={t('records.layout.sectionTitle')}
                          maxLength={80}
                          className="h-8 min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 text-[13px] font-semibold text-foreground outline-none transition-colors placeholder:font-normal placeholder:text-muted-foreground/70 hover:border-input focus:border-primary focus:bg-background focus:ring-[3px] focus:ring-primary/15"
                        />
                        <span className="text-xs tabular-nums text-muted-foreground">{t('records.layout.fieldCount', { count: s.fields.length })}</span>
                        <div className="inline-flex rounded-md border bg-background p-0.5" role="group" aria-label={t('records.layout.columns')}>
                          {([1, 2] as const).map((c) => {
                            const CIcon = c === 1 ? Square : Columns2;
                            return (
                              <Tooltip key={c} content={t('records.layout.columnsN', { count: c })} side="top">
                                <button
                                  type="button"
                                  aria-pressed={s.columns === c}
                                  aria-label={t('records.layout.columnsN', { count: c })}
                                  onClick={() => patchSection(s.id, { columns: c })}
                                  className={cn('grid size-6 place-items-center rounded', s.columns === c ? 'bg-primary-soft text-primary' : 'text-muted-foreground hover:text-foreground')}
                                >
                                  <CIcon className="size-3.5" />
                                </button>
                              </Tooltip>
                            );
                          })}
                        </div>
                        <div className="flex items-center">
                          <Button variant="subtle" size="icon-sm" disabled={si === 0} onClick={() => update((l) => moveSection(l, si, si - 1))} aria-label={t('records.layout.sectionUp')}>
                            <ChevronUp />
                          </Button>
                          <Button
                            variant="subtle"
                            size="icon-sm"
                            disabled={si === draft.sections.length - 1}
                            onClick={() => update((l) => moveSection(l, si, si + 1))}
                            aria-label={t('records.layout.sectionDown')}
                          >
                            <ChevronDown />
                          </Button>
                          <Tooltip content={t('records.layout.deleteSection')} side="top">
                            <Button variant="subtle" size="icon-sm" className="hover:bg-danger-soft hover:text-danger" onClick={() => deleteSection(s.id)} aria-label={t('records.layout.deleteSection')}>
                              <Trash2 />
                            </Button>
                          </Tooltip>
                        </div>
                      </div>
                      <div className="p-3">
                        {s.fields.length === 0 ? (
                          <div
                            className={cn(
                              'rounded-md border border-dashed px-3 py-6 text-center text-[13px] text-muted-foreground transition-colors',
                              dropHere && 'border-primary bg-primary-soft/50 text-primary'
                            )}
                          >
                            {t('records.layout.dropHere')}
                          </div>
                        ) : (
                          <ul className={cn('grid gap-2', s.columns === 2 && 'sm:grid-cols-2')}>
                            {s.fields.map((key, fi) => {
                              const f = byKey.get(key);
                              if (!f) return null;
                              const ind = dropHere ? (dropHere.index === fi ? 'before' : dropHere.index === fi + 1 && fi === s.fields.length - 1 ? 'after' : null) : null;
                              return (
                                <FieldChip
                                  key={key}
                                  field={f}
                                  indicator={ind}
                                  horizontal={s.columns === 2}
                                  dragging={fieldDrag?.key === key && fieldDrag.from !== 'highlights'}
                                  inHighlights={draft.highlights.includes(key)}
                                  sections={sectionOptions}
                                  currentSectionId={s.id}
                                  isFirst={fi === 0}
                                  isLast={fi === s.fields.length - 1}
                                  onDragStart={(e) => startDrag(e, { type: 'field', key, from: 'section' })}
                                  onDragEnd={endDrag}
                                  onDragOver={(e) => {
                                    if (!fieldDrag || fieldDrag.from === 'highlights') return;
                                    const after = half(e, s.columns === 2 && window.matchMedia('(min-width: 640px)').matches);
                                    over(e, { kind: 'section', sectionId: s.id, index: after ? fi + 1 : fi });
                                  }}
                                  onDrop={commitDrop}
                                  onMove={(delta) => update((l) => placeInSection(l, key, s.id, delta < 0 ? fi - 1 : fi + 2))}
                                  onMoveTo={(sectionId) => {
                                    const target = draft.sections.find((x) => x.id === sectionId);
                                    update((l) => placeInSection(l, key, sectionId, target?.fields.length ?? 0));
                                  }}
                                  onHighlight={() => addToHighlights(key)}
                                  onRemove={() => update((l) => hideField(l, key))}
                                  onEdit={!f.standard ? () => setFieldDialog({ mode: 'edit', field: f }) : undefined}
                                />
                              );
                            })}
                          </ul>
                        )}
                      </div>
                    </section>
                  );
                })}

                <button
                  type="button"
                  onClick={addSection}
                  className="flex w-full items-center justify-center gap-2 rounded-lg border border-dashed py-3 text-[13px] font-medium text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary-soft/40 hover:text-primary"
                >
                  <Plus className="size-4" aria-hidden /> {t('records.layout.addSection')}
                </button>
              </>
            )}
          </div>
        </div>

        {/* Long layouts scroll the header away; keep Save reachable while there are changes. */}
        {dirty ? (
          <div className="pointer-events-none sticky bottom-4 z-20 mt-6 flex justify-end">
            <div className="pointer-events-auto flex items-center gap-3 rounded-lg border bg-popover px-4 py-2.5 shadow-pop animate-slide-up">
              <span className="text-[13px] text-muted-foreground">{t('records.layout.unsaved')}</span>
              <Button variant="outline" size="sm" onClick={() => setConfirmDiscard(true)}>
                {t('common.cancel')}
              </Button>
              <Button size="sm" onClick={onSave} loading={save.isPending}>
                {t('records.layout.save')}
              </Button>
            </div>
          </div>
        ) : null}
      </div>

      <FieldDialog
        object={object}
        meta={meta}
        sections={sectionOptions}
        persistedSectionIds={persistedSectionIds}
        state={fieldDialog}
        onClose={() => setFieldDialog(null)}
        onUpdated={adopt}
        onCreated={(m, key, sectionId) => {
          adopt(m);
          const knownNow = new Set(m.fields.map((f) => f.key));
          setBaseline(sanitize(m.layout, knownNow));
          setDraft((l) => {
            if (!l) return l;
            if (l.sections.some((s) => s.fields.includes(key))) return l;
            const target = l.sections.find((s) => s.id === sectionId) ?? l.sections[0];
            if (!target) return { ...l, sections: [{ id: newId('sec'), title: t('records.layout.customSectionTitle'), columns: 2, fields: [key] }] };
            return placeInSection(l, key, target.id, target.fields.length);
          });
        }}
      />

      <ConfirmDialog
        open={Boolean(archiveField)}
        onOpenChange={(o) => !o && setArchiveField(null)}
        title={t('records.layout.archiveTitle', { label: archiveField?.label ?? '' })}
        body={t('records.layout.archiveBody')}
        confirmLabel={t('records.layout.archiveConfirm')}
        tone="danger"
        loading={archive.isPending}
        onConfirm={() => archiveField && archive.mutate(archiveField.key)}
      />
      <ConfirmDialog
        open={confirmReset}
        onOpenChange={setConfirmReset}
        title={t('records.layout.resetTitle')}
        body={t('records.layout.resetBody')}
        confirmLabel={t('records.layout.reset')}
        tone="danger"
        loading={reset.isPending}
        onConfirm={() => reset.mutate()}
      />
      <ConfirmDialog
        open={confirmDiscard}
        onOpenChange={setConfirmDiscard}
        title={t('records.layout.discardTitle')}
        body={t('records.layout.discardBody')}
        confirmLabel={t('records.layout.discard')}
        tone="danger"
        onConfirm={() => {
          setConfirmDiscard(false);
          setDraft(baseline);
          // Let the beforeunload guard detach before navigating.
          setTimeout(leave, 0);
        }}
      />
    </PageContainer>
  );
}

// ---------------------------------------------------------------------------

function DropBar({ position, horizontal }: { position: 'before' | 'after' | null; horizontal?: boolean }) {
  if (!position) return null;
  return (
    <span
      aria-hidden
      className={cn(
        'pointer-events-none absolute z-10 rounded-full bg-primary',
        horizontal
          ? cn('inset-y-0 w-0.5', position === 'before' ? '-left-[5px]' : '-right-[5px]')
          : cn('inset-x-0 h-0.5', position === 'before' ? '-top-[5px]' : '-bottom-[5px]')
      )}
    />
  );
}

function TypeHint({ field }: { field: FieldDef }) {
  const { t } = useTranslation();
  return (
    <span className="truncate text-[11px] text-muted-foreground">
      {t(`records.types.${field.type}`)}
      {field.type === 'lookup' && field.lookup ? ` · ${field.lookup}` : ''}
    </span>
  );
}

function FieldFlags({ field }: { field: FieldDef }) {
  const { t } = useTranslation();
  return (
    <>
      {field.required ? (
        <span className="text-danger" aria-label={t('records.layout.requiredFlag')}>
          *
        </span>
      ) : null}
      {field.readOnly ? (
        <Tooltip content={t('records.layout.readOnlyFlag')} side="top">
          <span className="inline-flex" aria-label={t('records.layout.readOnlyFlag')}>
            <Lock className="size-3 text-muted-foreground/70" aria-hidden />
          </span>
        </Tooltip>
      ) : null}
      {!field.standard ? (
        <Badge tone="primary" className="px-1.5 py-0 text-[10px]">
          {t('records.layout.custom')}
        </Badge>
      ) : null}
    </>
  );
}

function FieldChip({
  field,
  indicator,
  horizontal,
  dragging,
  inHighlights,
  sections,
  currentSectionId,
  isFirst,
  isLast,
  onDragStart,
  onDragEnd,
  onDragOver,
  onDrop,
  onMove,
  onMoveTo,
  onHighlight,
  onRemove,
  onEdit
}: {
  field: FieldDef;
  indicator: 'before' | 'after' | null;
  horizontal: boolean;
  dragging: boolean;
  inHighlights: boolean;
  sections: Array<{ id: string; title: string }>;
  currentSectionId: string;
  isFirst: boolean;
  isLast: boolean;
  onDragStart: (e: DragEvent) => void;
  onDragEnd: () => void;
  onDragOver: (e: DragEvent) => void;
  onDrop: (e: DragEvent) => void;
  onMove: (delta: -1 | 1) => void;
  onMoveTo: (sectionId: string) => void;
  onHighlight: () => void;
  onRemove: () => void;
  onEdit?: () => void;
}) {
  const { t } = useTranslation();
  const others = sections.filter((s) => s.id !== currentSectionId);
  return (
    <li
      draggable
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onDragOver={onDragOver}
      onDrop={onDrop}
      className={cn(
        'group relative flex min-w-0 items-center gap-1.5 rounded-md border bg-background py-1.5 pl-1 pr-1 shadow-sm transition-[opacity,border-color] hover:border-muted-foreground/40',
        dragging && 'opacity-40'
      )}
    >
      <DropBar position={indicator} horizontal={horizontal} />
      <GripVertical className="size-4 shrink-0 cursor-grab text-muted-foreground/50 group-hover:text-muted-foreground" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-1 text-[13px] font-medium text-foreground">
          <span className="truncate">{field.label}</span>
          <FieldFlags field={field} />
          {inHighlights ? <Star className="size-3 shrink-0 fill-warning text-warning" aria-label={t('records.layout.inHighlights')} /> : null}
        </p>
        <TypeHint field={field} />
      </div>
      <div className="flex shrink-0 items-center opacity-100 transition-opacity sm:opacity-0 sm:group-focus-within:opacity-100 sm:group-hover:opacity-100">
        <Menu>
          <MenuTrigger asChild>
            <button type="button" className="grid size-7 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground" aria-label={t('records.layout.chipActions', { label: field.label })}>
              <Ellipsis className="size-4" />
            </button>
          </MenuTrigger>
          <MenuContent align="end">
            <MenuItem disabled={isFirst} onSelect={() => onMove(-1)}>
              <ArrowUp /> {t('records.layout.moveUp')}
            </MenuItem>
            <MenuItem disabled={isLast} onSelect={() => onMove(1)}>
              <ArrowDown /> {t('records.layout.moveDown')}
            </MenuItem>
            {others.length ? (
              <>
                <MenuSeparator />
                <MenuLabel>{t('records.layout.moveTo')}</MenuLabel>
                {others.map((s) => (
                  <MenuItem key={s.id} onSelect={() => onMoveTo(s.id)}>
                    <FolderInput /> <span className="truncate">{s.title}</span>
                  </MenuItem>
                ))}
              </>
            ) : null}
            <MenuSeparator />
            <MenuItem disabled={inHighlights} onSelect={onHighlight}>
              <Star /> {t('records.layout.addToHighlights')}
            </MenuItem>
            {onEdit ? (
              <MenuItem onSelect={onEdit}>
                <Pencil /> {t('records.layout.editField')}
              </MenuItem>
            ) : null}
            <MenuItem onSelect={onRemove}>
              <EyeOff /> {t('records.layout.hideField')}
            </MenuItem>
          </MenuContent>
        </Menu>
        <button
          type="button"
          onClick={onRemove}
          className="grid size-7 place-items-center rounded text-muted-foreground hover:bg-danger-soft hover:text-danger"
          aria-label={t('records.layout.removeField', { label: field.label })}
        >
          <X className="size-3.5" />
        </button>
      </div>
    </li>
  );
}

type PaletteFilter = 'all' | 'hidden' | 'custom';

function FieldPalette({
  meta,
  placed,
  highlights,
  sections,
  dropActive,
  canDropHere,
  onDragStartField,
  onDragEnd,
  onDragOver,
  onDrop,
  onAddTo,
  onHide,
  onHighlight,
  onNewField,
  onEditField,
  onArchiveField
}: {
  meta: ObjectMeta;
  placed: Set<string>;
  highlights: string[];
  sections: Array<{ id: string; title: string }>;
  dropActive: boolean;
  canDropHere: boolean;
  onDragStartField: (e: DragEvent, key: string) => void;
  onDragEnd: () => void;
  onDragOver: (e: DragEvent) => void;
  onDrop: (e: DragEvent) => void;
  onAddTo: (key: string, sectionId: string) => void;
  onHide: (key: string) => void;
  onHighlight: (key: string) => void;
  onNewField: () => void;
  onEditField: (f: FieldDef) => void;
  onArchiveField: (f: FieldDef) => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState<PaletteFilter>('all');
  const hiddenCount = meta.fields.filter((f) => !placed.has(f.key)).length;
  const customCount = meta.fields.filter((f) => !f.standard).length;
  const needle = q.trim().toLowerCase();
  const fields = [...meta.fields]
    .filter((f) => (filter === 'hidden' ? !placed.has(f.key) : filter === 'custom' ? !f.standard : true))
    .filter((f) => !needle || f.label.toLowerCase().includes(needle) || f.key.toLowerCase().includes(needle))
    .sort((a, b) => Number(placed.has(a.key)) - Number(placed.has(b.key)) || a.label.localeCompare(b.label));

  return (
    <aside className="min-w-0 lg:sticky lg:top-4 lg:self-start">
      <Card
        className={cn('flex max-h-[420px] flex-col overflow-hidden transition-colors lg:max-h-[calc(100vh-2rem)]', canDropHere && 'border-dashed border-primary/40', dropActive && 'bg-primary-soft/40')}
        onDragOver={onDragOver}
        onDrop={onDrop}
      >
        <div className="space-y-2.5 border-b p-3">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-[13px] font-semibold text-foreground">{t('records.layout.fields')}</h2>
            <Button size="sm" variant="outline" onClick={onNewField}>
              <Sparkles /> {t('records.layout.newField')}
            </Button>
          </div>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('records.layout.searchFields')} aria-label={t('records.layout.searchFields')} leading={<Search />} />
          <SegmentedFilter
            value={filter}
            onChange={setFilter}
            options={[
              { value: 'all', label: t('records.layout.filterAll'), count: meta.fields.length },
              { value: 'hidden', label: t('records.layout.filterHidden'), count: hiddenCount },
              { value: 'custom', label: t('records.layout.filterCustom'), count: customCount }
            ]}
          />
        </div>
        {canDropHere ? <p className="border-b bg-primary-soft/60 px-3 py-1.5 text-center text-xs font-medium text-primary">{t('records.layout.dropToHide')}</p> : null}
        <ul className="min-h-0 flex-1 overflow-y-auto p-1.5" aria-label={t('records.layout.fields')}>
          {fields.length === 0 ? <li className="px-3 py-6 text-center text-[13px] text-muted-foreground">{t('records.layout.noFieldsMatch')}</li> : null}
          {fields.map((f) => {
            const isPlaced = placed.has(f.key);
            return (
              <li
                key={f.key}
                draggable
                onDragStart={(e) => onDragStartField(e, f.key)}
                onDragEnd={onDragEnd}
                className="group flex cursor-grab items-center gap-2 rounded-md px-1.5 py-1.5 hover:bg-muted active:cursor-grabbing"
              >
                <GripVertical className="size-3.5 shrink-0 text-muted-foreground/40 group-hover:text-muted-foreground" aria-hidden />
                <Tooltip content={isPlaced ? t('records.layout.placed') : t('records.layout.hidden')} side="top">
                  <span className={cn('grid size-5 shrink-0 place-items-center rounded', isPlaced ? 'bg-success-soft text-success' : 'bg-muted text-muted-foreground')} aria-label={isPlaced ? t('records.layout.placed') : t('records.layout.hidden')}>
                    {isPlaced ? <Check className="size-3" /> : <EyeOff className="size-3" />}
                  </span>
                </Tooltip>
                <div className="min-w-0 flex-1">
                  <p className={cn('flex min-w-0 items-center gap-1 text-[13px]', isPlaced ? 'text-muted-foreground' : 'font-medium text-foreground')}>
                    <span className="truncate">{f.label}</span>
                    <FieldFlags field={f} />
                  </p>
                  <TypeHint field={f} />
                </div>
                <Menu>
                  <MenuTrigger asChild>
                    <button
                      type="button"
                      className="grid size-7 shrink-0 place-items-center rounded text-muted-foreground opacity-100 hover:bg-background hover:text-foreground focus-visible:opacity-100 sm:opacity-0 sm:group-hover:opacity-100 data-[state=open]:opacity-100"
                      aria-label={t('records.layout.chipActions', { label: f.label })}
                    >
                      <Ellipsis className="size-4" />
                    </button>
                  </MenuTrigger>
                  <MenuContent align="end">
                    {sections.length ? <MenuLabel>{isPlaced ? t('records.layout.moveTo') : t('records.layout.addTo')}</MenuLabel> : null}
                    {sections.map((s) => (
                      <MenuItem key={s.id} onSelect={() => onAddTo(f.key, s.id)}>
                        <FolderInput /> <span className="truncate">{s.title}</span>
                      </MenuItem>
                    ))}
                    <MenuSeparator />
                    <MenuItem disabled={highlights.includes(f.key)} onSelect={() => onHighlight(f.key)}>
                      <Star /> {t('records.layout.addToHighlights')}
                    </MenuItem>
                    {isPlaced ? (
                      <MenuItem onSelect={() => onHide(f.key)}>
                        <EyeOff /> {t('records.layout.hideField')}
                      </MenuItem>
                    ) : null}
                    {!f.standard ? (
                      <>
                        <MenuSeparator />
                        <MenuItem onSelect={() => onEditField(f)}>
                          <Pencil /> {t('records.layout.editField')}
                        </MenuItem>
                        <MenuItem danger onSelect={() => onArchiveField(f)}>
                          <Archive /> {t('records.layout.archiveField')}
                        </MenuItem>
                      </>
                    ) : null}
                  </MenuContent>
                </Menu>
              </li>
            );
          })}
        </ul>
      </Card>
    </aside>
  );
}

function LayoutPreview({ meta, layout, device }: { meta: ObjectMeta; layout: Layout; device: 'desktop' | 'mobile' }) {
  const { t } = useTranslation();
  const byKey = fieldIndex(meta);
  const Icon = objectIcon(meta.object, meta.icon);
  const mobile = device === 'mobile';
  const empty = <span className="text-muted-foreground/60">—</span>;
  return (
    <div className={cn('mx-auto w-full rounded-xl border bg-background p-3 transition-[max-width] duration-300', mobile ? 'max-w-[390px]' : 'max-w-none')}>
      <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
        <Eye className="size-3.5" aria-hidden /> {t('records.layout.previewNote')}
      </div>
      <Card className="mb-3 overflow-hidden">
        <div className="flex items-center gap-3 p-4">
          <span className="grid size-10 place-items-center rounded-lg bg-primary text-primary-foreground">
            <Icon className="size-5" aria-hidden />
          </span>
          <div>
            <p className="text-xs text-muted-foreground">{meta.labelSingular}</p>
            <p className="text-base font-semibold text-foreground">{t('records.layout.sampleTitle', { object: meta.labelSingular })}</p>
            <p className="font-mono text-[11px] text-muted-foreground">{meta.codePrefix}-0001</p>
          </div>
        </div>
        {layout.highlights.length ? (
          <dl className={cn('grid gap-x-6 gap-y-3 border-t bg-muted/30 px-4 py-3', mobile ? 'grid-cols-2' : 'grid-cols-3 xl:grid-cols-6')}>
            {layout.highlights.map((k) => (
              <div key={k} className="min-w-0">
                <dt className="truncate text-xs text-muted-foreground">{byKey.get(k)?.label ?? k}</dt>
                <dd className="text-[13px]">{empty}</dd>
              </div>
            ))}
          </dl>
        ) : null}
      </Card>
      <div className="space-y-3">
        {layout.sections.map((s) => (
          <section key={s.id} className="rounded-lg border bg-card">
            <h3 className="flex items-center gap-2 rounded-t-lg bg-muted/40 px-3 py-2 text-[13px] font-semibold">
              <ChevronDown className="size-4 text-muted-foreground" aria-hidden />
              {s.title || t('records.layout.untitledSection')}
            </h3>
            <dl className={cn('grid gap-x-6 px-3 pb-2 pt-1', s.columns === 2 && !mobile && 'grid-cols-2')}>
              {s.fields.map((k) => {
                const f = byKey.get(k);
                if (!f) return null;
                return (
                  <div key={k} className="border-b border-dashed py-2 last:border-b-0">
                    <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                      {f.label}
                      {f.required ? <span className="text-danger">*</span> : null}
                    </dt>
                    <dd className="mt-0.5 text-[13px]">{empty}</dd>
                  </div>
                );
              })}
              {s.fields.length === 0 ? <p className="py-3 text-[13px] text-muted-foreground">{t('records.detail.emptySection')}</p> : null}
            </dl>
          </section>
        ))}
      </div>
    </div>
  );
}

function EditorSkeleton() {
  return (
    <PageContainer wide>
      <Skeleton className="mb-2 h-4 w-48" />
      <Skeleton className="mb-6 h-8 w-72" />
      <div className="grid gap-6 lg:grid-cols-[280px_minmax(0,1fr)]">
        <Card className="space-y-2 p-3">
          <Skeleton className="h-9" />
          {Array.from({ length: 10 }).map((_, i) => (
            <Skeleton key={i} className="h-8" />
          ))}
        </Card>
        <div className="space-y-4">
          <Card className="p-4">
            <Skeleton className="h-4 w-40" />
            <div className="mt-3 flex gap-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-28" />
              ))}
            </div>
          </Card>
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i} className="p-3">
              <Skeleton className="h-8 w-56" />
              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                {Array.from({ length: 6 }).map((__, j) => (
                  <Skeleton key={j} className="h-12" />
                ))}
              </div>
            </Card>
          ))}
        </div>
      </div>
    </PageContainer>
  );
}

