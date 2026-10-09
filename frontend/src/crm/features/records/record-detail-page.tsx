import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Activity, ArrowRightLeft, ChevronDown, ChevronLeft, ChevronRight, Copy, Ellipsis, GitMerge, KeyRound, LayoutTemplate, Lock, Mail, Pencil, RefreshCw, Star, Trash2 } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta, RecordDetail, RecordRow, RelatedList } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, Tooltip } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, DevLink, PageContainer, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { plainKey, recordNeighbours } from '@crm/features/shell/shortcuts';
import { GiveLoginDialog } from '@crm/features/access/give-login-dialog';
import { TicketConversation } from './ticket-conversation';
import { ConvertLeadDialog } from './convert-dialog';
import { RecordTimeline } from './record-timeline';
import { RecordEmails } from './record-emails';
import { EmailDialog, MergeDialog, RecordFiles, useDuplicates } from './record-extras';
import { FieldEditor } from './field-input';
import { FieldLabel, FieldValue } from './field-value';
import { fieldIndex, guessTone, humanize, normalizeValue, objectIcon, recordKeys, sameValue, statusOption, useObjectMeta } from './use-object-meta';
import { layoutHref, listHref, recordHref, scopedLookupHref, useRecordScope } from './record-scope';
import { RunWorkflowButton } from './run-workflow';
import { isForbidden, RecordNoAccess } from './record-states';
import {
  AccountTerritoryCard, BookAppointmentButton, BundleCard, CaseWorkOrderButton, ContactRolesCard, CreditNoteCard, DocumentLinesCard, EntitlementUsageCard,
  PriceEntriesCard, RecordCampaignsCard, RecordTeamCard, TerritoryMembersCard, hasContactRoles, hasLines, hasRecordTeam
} from './record-commerce';
import { CaseSlaCard, InvoicePaymentsCard, PaymentAllocationsCard, RelationshipsCard, RenewContractButton } from './record-enterprise';

export function RecordDetailPage({ object }: { object: ObjectKey }) {
  const { id = '' } = useParams();
  const scope = useRecordScope();
  if (!scope.can(object, 'read')) return <RecordNoAccess />;
  // Keyed by id so edit state never leaks between records of the same object.
  return <RecordDetailView key={id} object={object} id={id} />;
}

type Tab = 'details' | 'activity' | 'emails' | 'related' | 'files';

function RecordDetailView({ object, id }: { object: ObjectKey; id: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const { prefix } = scope;
  const isOwner = scope.audience === 'owner';
  const canEdit = scope.can(object, 'update');
  const canDelete = scope.can(object, 'delete');
  const metaQ = useObjectMeta(object);
  const detailQ = useQuery({ queryKey: recordKeys.detail(prefix, object, id), queryFn: () => scope.api.get(object, id) });
  const meta = metaQ.data;
  const detail = detailQ.data;
  const record = detail?.record;
  useDocumentTitle(record?.title ?? meta?.labelSingular ?? t('records.common.loading'));

  const [tab, setTab] = useState<Tab>(() => (new URLSearchParams(window.location.search).get('tab') as Tab) || 'details');
  const [emailOpen, setEmailOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const favQ = useQuery({ queryKey: ['favorites', prefix], queryFn: () => scope.api.favorites(), staleTime: 60_000 });
  const isFav = (favQ.data ?? []).some((f) => f.kind === 'record' && f.targetId === id);
  const toggleFav = useMutation({
    mutationFn: () => (isFav ? scope.api.removeFavorite(id) : scope.api.addFavorite({ kind: 'record', object, targetId: id })),
    onSuccess: (list) => {
      qc.setQueryData(['favorites', prefix], list);
      toast.success(isFav ? t('records.detail.unfavorited') : t('records.detail.favorited'));
    }
  });
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, unknown>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [convertOpen, setConvertOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const mainRef = useRef<HTMLDivElement>(null);

  const byKey = useMemo(() => fieldIndex(meta), [meta]);

  const changes = useMemo(() => {
    if (!editing || !record) return {};
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(draft)) {
      const f = byKey.get(k);
      if (!f || f.readOnly) continue;
      if (!sameValue(v, record.values[k])) out[k] = normalizeValue(v);
    }
    return out;
  }, [draft, editing, record, byKey]);
  const changeCount = Object.keys(changes).length;

  const startEdit = useCallback(
    (key?: string) => {
      if (!record || !canEdit) return;
      if (!editing) {
        setDraft({ ...record.values });
        setErrors({});
        setFormError(null);
        setConflict(false);
        setEditing(true);
      }
      setTab('details');
      setFocusKey(key ?? null);
      if (key) setCollapsed((c) => new Set([...c].filter((sid) => !meta?.layout.sections.find((s) => s.id === sid)?.fields.includes(key))));
    },
    [record, editing, meta, canEdit]
  );

  const cancelEdit = useCallback(() => {
    setEditing(false);
    setDraft({});
    setErrors({});
    setFormError(null);
    setConflict(false);
  }, []);

  // Move focus to the field that was double-clicked / pencilled.
  useEffect(() => {
    if (!editing) return;
    const root = mainRef.current;
    if (!root) return;
    const scope = focusKey ? root.querySelector(`[data-field-key="${CSS.escape(focusKey)}"]`) : root;
    const el = scope?.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled]), button[role="switch"]:not([disabled])');
    el?.focus();
  }, [editing, focusKey]);

  // Warn before leaving with unsaved edits.
  useEffect(() => {
    if (!editing || changeCount === 0) return;
    const h = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', h);
    return () => window.removeEventListener('beforeunload', h);
  }, [editing, changeCount]);

  const save = useMutation({
    mutationFn: (vars: { values: Record<string, unknown>; version: number }) => scope.api.update(object, id, vars.values, vars.version),
    onSuccess: (row) => {
      qc.setQueryData<RecordDetail>(recordKeys.detail(prefix, object, id), (prev) => (prev ? { ...prev, record: row } : prev));
      void qc.invalidateQueries({ queryKey: recordKeys.detail(prefix, object, id) });
      void qc.invalidateQueries({ queryKey: recordKeys.lists(prefix, object) });
      toast.success(t('records.detail.saved'));
      cancelEdit();
    },
    onError: (e) => {
      if (isApiError(e) && (e.code === 'version_conflict' || e.status === 409)) {
        setConflict(true);
        return;
      }
      if (isForbidden(e)) {
        setFormError(t('records.detail.forbiddenAction'));
        return;
      }
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        setErrors(e.fieldErrors);
        const unknown = Object.entries(e.fieldErrors).filter(([k]) => !byKey.has(k));
        setFormError(unknown.length ? unknown.map(([, m]) => m).join(' · ') : t('records.detail.fixErrors'));
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const onSave = useCallback(() => {
    if (!record) return;
    if (changeCount === 0) return cancelEdit();
    const next: Record<string, string> = {};
    for (const [k, v] of Object.entries(changes)) if (byKey.get(k)?.required && v === null) next[k] = t('common.required');
    setErrors(next);
    if (Object.keys(next).length) {
      setFormError(t('records.detail.fixErrors'));
      return;
    }
    setFormError(null);
    save.mutate({ values: changes, version: record.version });
  }, [record, changeCount, changes, byKey, save, cancelEdit, t]);

  // j / k step to the next / previous record of the list you came from; e edits (D-81).
  const neighbours = useMemo(() => recordNeighbours(listHref(scope, object), id), [scope, object, id]);
  useEffect(() => {
    if (editing) return;
    const h = (e: KeyboardEvent) => {
      if (!plainKey(e)) return;
      const to = e.key === 'j' ? neighbours.next : e.key === 'k' ? neighbours.prev : undefined;
      if (to) {
        e.preventDefault();
        navigate(recordHref(scope, object, to) + (tab !== 'details' ? `?tab=${tab}` : ''));
      } else if (e.key === 'e' && canEdit) {
        e.preventDefault();
        startEdit();
      }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [editing, neighbours, navigate, scope, object, tab, canEdit, startEdit]);

  // ⌘/Ctrl+S saves, Esc cancels while editing.
  useEffect(() => {
    if (!editing) return;
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        onSave();
      } else if (e.key === 'Escape' && !save.isPending && !document.querySelector('[role="dialog"]')) {
        if (changeCount === 0) cancelEdit();
      }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [editing, onSave, cancelEdit, changeCount, save.isPending]);

  const remove = useMutation({
    mutationFn: () => scope.api.remove(object, id),
    onSuccess: () => {
      toast.success(t('records.detail.deleted', { object: meta?.labelSingular ?? '' }));
      qc.removeQueries({ queryKey: recordKeys.detail(prefix, object, id) });
      void qc.invalidateQueries({ queryKey: recordKeys.lists(prefix, object) });
      void qc.invalidateQueries({ queryKey: scope.dashboardKey });
      navigate(listHref(scope, object));
    },
    onError: (e) => {
      setConfirmDelete(false);
      toast.error(isForbidden(e) ? t('records.detail.forbiddenAction') : isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const reload = () => {
    cancelEdit();
    void detailQ.refetch();
  };

  const listPath = listHref(scope, object);

  if (isForbidden(metaQ.error) || isForbidden(detailQ.error)) return <RecordNoAccess />;

  if (metaQ.isError || detailQ.isError) {
    const err = metaQ.error ?? detailQ.error;
    const notFound = isApiError(err) && err.status === 404;
    return (
      <PageContainer wide>
        <Card>
          {notFound ? (
            <EmptyState
              icon={objectIcon(object, meta?.icon)}
              title={t('records.detail.notFoundTitle')}
              body={t('records.detail.notFoundBody')}
              action={
                <Button asChild variant="outline">
                  <Link to={listPath}>{t('records.detail.backToList', { objects: meta?.labelPlural ?? '' })}</Link>
                </Button>
              }
            />
          ) : (
            <ErrorState
              title={t('records.common.loadError')}
              message={isApiError(err) ? err.message : undefined}
              requestId={isApiError(err) ? err.requestId : undefined}
              onRetry={() => {
                void metaQ.refetch();
                void detailQ.refetch();
              }}
            />
          )}
        </Card>
      </PageContainer>
    );
  }

  if (!meta || !detail || !record) return <DetailSkeleton />;

  const Icon = objectIcon(object, meta.icon);
  const status = meta.statusField ? statusOption(meta, record.values[meta.statusField]) : undefined;
  // A case mirrored from a Business Card Snap ticket shows the ticket's conversation (D-91).
  const workspaceCode = scope.audience === 'member' ? decodeURIComponent(scope.prefix.replace(/^\/w\//, '')) : '';
  const ticketId = object === 'cases' && workspaceCode && typeof record.values.app_ticket_id === 'string' ? record.values.app_ticket_id : '';
  const isLead = object === 'leads';
  const converted = isLead && Boolean(detail.conversion || record.values.convertedAt || record.values.status === 'converted');
  const canConvert = isLead && !converted && scope.can('leads', 'convert');
  // Owner only: give the person behind this record a login.
  const email = typeof record.values.email === 'string' ? record.values.email.trim() : '';
  const identityId = typeof record.values.identityId === 'string' && record.values.identityId ? record.values.identityId : null;
  const loginLabel = record.lookups.identityId?.label ?? email;
  const workspaceNameFor = (): string => {
    const org = typeof record.values.organization === 'string' ? record.values.organization.trim() : '';
    if (object === 'leads') return org || record.title;
    if (object === 'contacts') return record.lookups.accountId?.label ?? record.title;
    return record.title;
  };
  const highlights = meta.layout.highlights.map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f));
  // The activity list lives in the Activity tab now (timeline); the rest stays under Related.
  // Links made in the Relationships panel have their own card (add / remove) on workspace pages.
  const inWorkspace = scope.audience === 'member';
  const relatedLists = detail.related.filter(
    (r) => r.object !== 'activities' && !(inWorkspace && (r.key.startsWith('rel:') || (object === 'invoices' && r.object === 'payments') || (hasLines(object) && r.object === 'line_items')))
  );
  const relatedCount = relatedLists.reduce((n, r) => n + r.rows.length, 0);
  const canEmail = Boolean(email) && scope.hasCapability('email.send');

  return (
    <PageContainer wide className={cn(editing && 'pb-0 lg:pb-0')}>
      <div className="flex items-start justify-between gap-2">
        <Breadcrumbs items={[{ label: meta.labelPlural, to: neighbours.listUrl ?? listPath }, { label: record.title || record.code }]} />
        {neighbours.index >= 0 && neighbours.total > 1 ? (
          <div className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
            <span className="tabular-nums">{t('records.detail.position', { n: neighbours.index + 1, total: neighbours.total })}</span>
            <Button variant="ghost" size="icon-sm" disabled={!neighbours.prev || editing} aria-label={t('records.detail.previous')} title={t('records.detail.previous') + ' (k)'}
              onClick={() => neighbours.prev && navigate(recordHref(scope, object, neighbours.prev))}>
              <ChevronLeft />
            </Button>
            <Button variant="ghost" size="icon-sm" disabled={!neighbours.next || editing} aria-label={t('records.detail.next')} title={t('records.detail.next') + ' (j)'}
              onClick={() => neighbours.next && navigate(recordHref(scope, object, neighbours.next))}>
              <ChevronRight />
            </Button>
          </div>
        ) : null}
      </div>

      {/* Header / highlights panel */}
      <Card className="mb-4 overflow-hidden">
        <div className="flex flex-wrap items-start gap-4 px-4 py-4 sm:px-5">
          <span className="grid size-11 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground shadow-sm shadow-primary/20">
            <Icon className="size-5" aria-hidden />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-xs font-medium text-muted-foreground">{meta.labelSingular}</p>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="min-w-0 break-words text-xl font-semibold tracking-tight text-foreground sm:text-[22px]">{record.title || t('records.common.untitled')}</h1>
              {status ? <Badge tone={status.tone}>{status.label}</Badge> : null}
              {isOwner && identityId ? (
                <Link
                  to={`/crm/owner/users/${encodeURIComponent(identityId)}`}
                  className="inline-flex max-w-full items-center gap-1 rounded-full border border-success/25 bg-success-soft px-2 py-0.5 text-[11px] font-semibold leading-4 text-success hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  title={t('records.detail.openUser')}
                >
                  <KeyRound className="size-3 shrink-0" aria-hidden />
                  <span className="truncate">{t('records.detail.hasLogin', { label: loginLabel })}</span>
                </Link>
              ) : null}
            </div>
            <div className="mt-0.5 flex items-center gap-1">
              <Tooltip content={isFav ? t('records.detail.unfavorite') : t('records.detail.favorite')} side="top">
                <button type="button" onClick={() => toggleFav.mutate()} aria-pressed={isFav}
                  className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground"
                  aria-label={isFav ? t('records.detail.unfavorite') : t('records.detail.favorite')}>
                  <Star className={cn('size-3.5', isFav && 'fill-amber-400 text-amber-500')} />
                </button>
              </Tooltip>
              <span className="font-mono text-xs text-muted-foreground">{record.code}</span>
              <Tooltip content={t('records.detail.copyCode')} side="top">
                <button
                  type="button"
                  className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground"
                  aria-label={t('records.detail.copyCode')}
                  onClick={() => {
                    void navigator.clipboard?.writeText(record.code).then(
                      () => toast.success(t('records.detail.codeCopied', { code: record.code })),
                      () => toast.error(t('common.genericError'))
                    );
                  }}
                >
                  <Copy className="size-3.5" />
                </button>
              </Tooltip>
            </div>
          </div>
          <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            {!editing && canEdit ? (
              <Button variant="outline" size="sm" onClick={() => startEdit()}>
                <Pencil /> {t('records.detail.edit')}
              </Button>
            ) : null}
            {isOwner && !identityId ? (
              email ? (
                <Button variant="outline" size="sm" className="border-primary/40 text-primary hover:bg-primary-soft" onClick={() => setLoginOpen(true)} disabled={editing}>
                  <KeyRound /> {t('records.detail.giveLogin')}
                </Button>
              ) : (
                <Tooltip content={t('records.detail.giveLoginNoEmail')} side="bottom">
                  {/* Disabled buttons swallow pointer events; the wrapper keeps the tooltip reachable. */}
                  <span
                    tabIndex={0}
                    role="button"
                    aria-disabled="true"
                    aria-label={`${t('records.detail.giveLogin')} — ${t('records.detail.giveLoginNoEmail')}`}
                    className="inline-flex rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <Button variant="outline" size="sm" disabled aria-hidden tabIndex={-1}>
                      <KeyRound /> {t('records.detail.giveLogin')}
                    </Button>
                  </span>
                </Tooltip>
              )
            ) : null}
            {canEmail ? (
              <Button variant="outline" size="sm" onClick={() => setEmailOpen(true)} disabled={editing}>
                <Mail /> {t('records.detail.sendEmail')}
              </Button>
            ) : null}
            <RunWorkflowButton object={object} id={id} disabled={editing} />
            {object === 'contracts' ? <RenewContractButton record={record} disabled={editing} /> : null}
            {inWorkspace && object === 'cases' ? <CaseWorkOrderButton record={record} disabled={editing} /> : null}
            {inWorkspace && (object === 'work_orders' || object === 'appointments' || object === 'cases') ? <BookAppointmentButton record={record} object={object as 'work_orders' | 'appointments' | 'cases'} disabled={editing} /> : null}
            {canConvert ? (
              <Button size="sm" onClick={() => setConvertOpen(true)} disabled={editing}>
                <ArrowRightLeft /> {t('records.detail.convert')}
              </Button>
            ) : null}
            <Menu>
              <MenuTrigger asChild>
                <Button variant="outline" size="icon-sm" aria-label={t('records.common.moreActions')}>
                  <Ellipsis />
                </Button>
              </MenuTrigger>
              <MenuContent align="end">
                {scope.canCustomize ? (
                  <MenuItem onSelect={() => navigate(layoutHref(scope, object))}>
                    <LayoutTemplate /> {t('records.common.editLayout')}
                  </MenuItem>
                ) : null}
                <MenuItem onSelect={() => void detailQ.refetch()}>
                  <RefreshCw /> {t('records.detail.refresh')}
                </MenuItem>
                {canEdit && canDelete ? (
                  <MenuItem onSelect={() => setMergeOpen(true)}>
                    <GitMerge /> {t('records.detail.merge')}
                  </MenuItem>
                ) : null}
                {canDelete ? (
                  <>
                    <MenuSeparator />
                    <MenuItem danger onSelect={() => setConfirmDelete(true)}>
                      <Trash2 /> {t('records.detail.delete')}
                    </MenuItem>
                  </>
                ) : null}
              </MenuContent>
            </Menu>
          </div>
        </div>
        {highlights.length ? (
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 border-t bg-muted/30 px-4 py-3 sm:grid-cols-3 sm:px-5 lg:flex lg:flex-wrap lg:gap-x-10">
            {highlights.map((f) => (
              <div key={f.key} className="min-w-0 lg:max-w-[240px]">
                <dt className="truncate text-xs text-muted-foreground">{f.label}</dt>
                <dd className="mt-0.5 flex min-w-0 text-[13px] font-medium text-foreground">
                  <FieldValue field={f} record={record} meta={meta} compact className="truncate" />
                </dd>
              </div>
            ))}
          </dl>
        ) : null}
      </Card>

      {detail.conversion ? <ConversionBanner conversion={detail.conversion} /> : null}
      <DuplicatesBanner object={object} id={id} enabled={canEdit && canDelete} onReview={() => setMergeOpen(true)} />

      <div className="grid gap-4 lg:grid-cols-3">
        <div ref={mainRef} className="min-w-0 lg:col-span-2">
          {inWorkspace && hasLines(object) ? <DocumentLinesCard object={object} record={record} /> : null}
          {ticketId ? <TicketConversation code={workspaceCode} ticketId={ticketId} canReply={canEdit} /> : null}
          <Card className="overflow-hidden">
            <Tabs
              className="px-3"
              value={tab}
              onChange={setTab}
              items={[
                { value: 'details', label: t('records.detail.tabDetails') },
                { value: 'activity', label: t('records.detail.tabActivity') },
                ...(email ? [{ value: 'emails' as Tab, label: t('records.detail.tabEmails') }] : []),
                {
                  value: 'related',
                  label: (
                    <span className="inline-flex items-center gap-1.5">
                      {t('records.detail.tabRelated')}
                      {relatedCount ? <span className="rounded-full bg-muted px-1.5 text-[11px] tabular-nums text-muted-foreground">{relatedCount}</span> : null}
                    </span>
                  )
                },
                { value: 'files', label: t('records.detail.tabFiles') }
              ]}
            />
            {tab === 'details' ? (
              <div className="space-y-3 p-3 sm:p-4">
                {formError && editing ? <Alert tone="danger">{formError}</Alert> : null}
                {meta.layout.sections.length === 0 ? (
                  <EmptyState
                    icon={LayoutTemplate}
                    title={t('records.detail.noSectionsTitle')}
                    body={t('records.detail.noSectionsBody')}
                    action={
                      scope.canCustomize ? (
                        <Button asChild variant="outline" size="sm">
                          <Link to={layoutHref(scope, object)}>{t('records.common.editLayout')}</Link>
                        </Button>
                      ) : undefined
                    }
                  />
                ) : (
                  meta.layout.sections.map((s) => {
                    const fields = s.fields.map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f));
                    const open = !collapsed.has(s.id);
                    const bodyId = `section-${s.id}`;
                    return (
                      <section key={s.id} className="rounded-lg border">
                        <h2>
                          <button
                            type="button"
                            aria-expanded={open}
                            aria-controls={bodyId}
                            onClick={() =>
                              setCollapsed((c) => {
                                const n = new Set(c);
                                if (n.has(s.id)) n.delete(s.id);
                                else n.add(s.id);
                                return n;
                              })
                            }
                            className={cn('flex w-full items-center gap-2 rounded-t-lg bg-muted/40 px-3 py-2 text-left text-[13px] font-semibold text-foreground hover:bg-muted/70', !open && 'rounded-b-lg')}
                          >
                            <ChevronDown className={cn('size-4 text-muted-foreground transition-transform', !open && '-rotate-90')} aria-hidden />
                            {s.title}
                          </button>
                        </h2>
                        {open ? (
                          <dl id={bodyId} className={cn('grid gap-x-6 px-3 pb-2 pt-1', s.columns === 2 && 'sm:grid-cols-2')}>
                            {fields.map((f) => (
                              <FieldCell
                                key={f.key}
                                field={f}
                                record={record}
                                meta={meta}
                                editing={editing}
                                canEdit={canEdit}
                                value={draft[f.key]}
                                error={errors[f.key]}
                                disabled={save.isPending}
                                onChange={(v) => {
                                  setDraft((d) => ({ ...d, [f.key]: v }));
                                  if (errors[f.key]) setErrors(({ [f.key]: _gone, ...rest }) => rest);
                                }}
                                onStartEdit={() => startEdit(f.key)}
                              />
                            ))}
                            {fields.length === 0 ? <p className="col-span-full py-3 text-[13px] text-muted-foreground">{t('records.detail.emptySection')}</p> : null}
                          </dl>
                        ) : null}
                      </section>
                    );
                  })
                )}
              </div>
            ) : tab === 'activity' ? (
              <RecordTimeline object={object} id={id} canWrite={scope.can(object, 'read')} />
            ) : tab === 'emails' ? (
              <RecordEmails object={object} id={id} canSend={canEmail} onNewEmail={() => setEmailOpen(true)} />
            ) : tab === 'files' ? (
              <RecordFiles object={object} id={id} canWrite={canEdit} />
            ) : (
              <div className="space-y-3 p-3 sm:p-4">
                {relatedLists.length === 0 ? (
                  <EmptyState icon={Icon} title={t('records.detail.noRelatedTitle')} body={t('records.detail.noRelatedBody')} />
                ) : (
                  relatedLists.map((r) => <RelatedCard key={r.key} list={r} />)
                )}
              </div>
            )}
          </Card>
        </div>

        <aside className="min-w-0 space-y-4">
          {inWorkspace && object === 'cases' ? <CaseSlaCard record={record} /> : null}
          {inWorkspace && object === 'invoices' ? <InvoicePaymentsCard record={record} /> : null}
          {inWorkspace && object === 'payments' ? <PaymentAllocationsCard record={record} canEdit={canEdit} /> : null}
          {inWorkspace && object === 'credit_notes' ? <CreditNoteCard record={record} /> : null}
          {inWorkspace && object === 'entitlements' ? <EntitlementUsageCard record={record} /> : null}
          {inWorkspace && object === 'price_books' ? <PriceEntriesCard mode="book" record={record} /> : null}
          {inWorkspace && object === 'catalog_items' ? <PriceEntriesCard mode="item" record={record} /> : null}
          {inWorkspace && object === 'catalog_items' ? <BundleCard record={record} /> : null}
          {inWorkspace && object === 'territories' ? <TerritoryMembersCard record={record} /> : null}
          {inWorkspace && hasContactRoles(object) ? <ContactRolesCard object={object} record={record} /> : null}
          {inWorkspace && hasRecordTeam(object) ? <RecordTeamCard object={object} record={record} /> : null}
          {inWorkspace && object === 'accounts' ? <AccountTerritoryCard record={record} /> : null}
          {inWorkspace && (object === 'leads' || object === 'contacts' || object === 'opportunities') ? <RecordCampaignsCard object={object} record={record} /> : null}
          {inWorkspace ? <RelationshipsCard object={object} record={record} canEdit={canEdit} /> : null}
          {relatedLists.map((r) => (
            <RelatedCard key={r.key} list={r} compact onViewAll={() => setTab('related')} />
          ))}
          <RecordInfoCard record={record} meta={meta} />
        </aside>
      </div>

      {editing ? (
        <div className="sticky bottom-0 z-20 -mx-4 mt-4 border-t bg-card/95 px-4 py-3 shadow-[0_-4px_12px_-6px_rgb(15_23_42/0.12)] backdrop-blur sm:-mx-6 sm:px-6 lg:-mx-8 lg:px-8">
          {conflict ? (
            <div role="alert" className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-[13px]">
              <span className="text-foreground">{t('records.detail.conflict')}</span>
              <Button size="sm" variant="outline" onClick={reload}>
                <RefreshCw /> {t('records.detail.reload')}
              </Button>
            </div>
          ) : null}
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-[13px] text-muted-foreground" aria-live="polite">
              {changeCount ? t('records.detail.changes', { count: changeCount }) : t('records.detail.noChanges')}
              <span className="ml-2 hidden text-xs text-muted-foreground/70 md:inline">{t('records.detail.saveShortcut')}</span>
            </p>
            <div className="grid grid-cols-2 gap-2 sm:flex">
              <Button variant="outline" onClick={cancelEdit} disabled={save.isPending}>
                {t('common.cancel')}
              </Button>
              <Button onClick={onSave} loading={save.isPending} disabled={conflict}>
                {t('records.detail.save')}
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('records.detail.deleteTitle', { object: meta.labelSingular.toLowerCase() })}
        body={t('records.detail.deleteBody', { title: record.title || record.code })}
        confirmLabel={t('records.detail.delete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
      {canEmail ? <EmailDialog object={object} record={record} open={emailOpen} onOpenChange={setEmailOpen} defaultTo={email} /> : null}
      {canEdit && canDelete ? <MergeDialog object={object} meta={meta} record={record} open={mergeOpen} onOpenChange={setMergeOpen} /> : null}
      {/* Stays mounted after conversion so its success view survives the lead refetch. */}
      {isLead && scope.can('leads', 'convert') ? <ConvertLeadDialog lead={record} open={convertOpen} onOpenChange={setConvertOpen} /> : null}
      {isOwner ? (
        <GiveLoginDialog
          open={loginOpen}
          onOpenChange={setLoginOpen}
          object={object}
          recordId={record.id}
          personName={record.title}
          email={email || undefined}
          defaultWorkspaceName={workspaceNameFor()}
          onDone={() => {
            void qc.invalidateQueries({ queryKey: recordKeys.detail(prefix, object, id) });
            void qc.invalidateQueries({ queryKey: recordKeys.lists(prefix, object) });
            void qc.invalidateQueries({ queryKey: ['users'] });
          }}
        />
      ) : null}
    </PageContainer>
  );
}

function FieldCell({
  field,
  record,
  meta,
  editing,
  canEdit,
  value,
  error,
  disabled,
  onChange,
  onStartEdit
}: {
  field: FieldDef;
  record: RecordRow;
  meta: ObjectMeta;
  editing: boolean;
  canEdit: boolean;
  value: unknown;
  error?: string;
  disabled: boolean;
  onChange: (v: unknown) => void;
  onStartEdit: () => void;
}) {
  const { t } = useTranslation();
  if (editing && canEdit && !field.readOnly) {
    return (
      <div data-field-key={field.key} className="py-2">
        <FieldEditor field={field} value={value} onChange={onChange} error={error} disabled={disabled} lookupLabel={record.lookups[field.key]?.label} links={record.links?.[field.key]} currencyHint={record.values[`${field.key}__currency`] as string | undefined} />
      </div>
    );
  }
  const editable = canEdit && !field.readOnly;
  return (
    <div
      data-field-key={field.key}
      className={cn('group relative min-w-0 border-b border-dashed py-2 last:border-b-0', editable && !editing && 'cursor-text')}
      onDoubleClick={editable && !editing ? onStartEdit : undefined}
    >
      <dt className="flex items-center gap-1 text-xs text-muted-foreground">
        <FieldLabel field={field} />
        {editing && field.readOnly ? (
          <Tooltip content={t('records.detail.readOnly')} side="top">
            <span className="inline-flex" aria-label={t('records.detail.readOnly')}>
              <Lock className="size-3 text-muted-foreground/70" aria-hidden />
            </span>
          </Tooltip>
        ) : null}
      </dt>
      <dd className="mt-0.5 flex min-h-5 min-w-0 items-start justify-between gap-2 text-[13px] text-foreground">
        <span className="min-w-0 break-words">
          <FieldValue field={field} record={record} meta={meta} />
        </span>
        {editable && !editing ? (
          <button
            type="button"
            onClick={onStartEdit}
            className="grid size-6 shrink-0 place-items-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-60"
            aria-label={t('records.detail.editField', { field: field.label })}
          >
            <Pencil className="size-3.5" />
          </button>
        ) : null}
      </dd>
    </div>
  );
}

function RelatedCard({ list, compact, onViewAll }: { list: RelatedList; compact?: boolean; onViewAll?: () => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const rows = compact ? list.rows.slice(0, 5) : list.rows;
  const isTimeline = list.object === 'activities';
  // Tickets open the workspace support page; there is none in the owner console.
  // The connected app's pages (profile, businesses, saved cards) live in the workspace.
  const appHref = (id: string) => {
    if (scope.audience !== 'member') return null;
    if (list.object === 'cards') return `${scope.routeBase}/cards?card=${encodeURIComponent(id)}`;
    if (list.object === 'app-users') return `${scope.routeBase}/app-users/${encodeURIComponent(id)}`;
    if (list.object === 'app-businesses') return `${scope.routeBase}/businesses/${encodeURIComponent(id)}`;
    const [userId] = id.split('#');
    return `${scope.routeBase}/app-users/${encodeURIComponent(userId ?? '')}?tab=cards`;
  };
  const hrefFor = (id: string) =>
    list.object === 'tickets'
      ? scope.audience === 'member'
        ? `${scope.routeBase}/support/${encodeURIComponent(id)}`
        : null
      : list.object.startsWith('app-') || list.object === 'cards'
        ? appHref(id)
        : isTimeline
          ? null
          : scopedLookupHref(scope, list.object, id);
  return (
    <Card className="overflow-hidden">
      <CardHeader
        className={cn('px-4 py-3', compact && 'py-2.5')}
        title={
          <span className="flex items-center gap-2 text-[13px]">
            {list.label}
            <span className="rounded-full bg-muted px-1.5 text-[11px] font-medium tabular-nums text-muted-foreground">{list.rows.length}</span>
          </span>
        }
      />
      {list.rows.length === 0 ? (
        <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('records.detail.relatedEmpty', { label: list.label.toLowerCase() })}</p>
      ) : isTimeline ? (
        <ActivityTimeline rows={rows} />
      ) : (
        <ul className="divide-y">
          {rows.map((r) => {
            const href = hrefFor(r.id);
            const content = (
              <>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-medium text-foreground group-hover:text-primary">{r.title}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {r.code ? <span className="font-mono">{r.code}</span> : null}
                    {r.code && r.subtitle ? ' · ' : null}
                    {r.subtitle}
                  </p>
                </div>
                {r.status ? <Badge tone={guessTone(r.status)}>{humanize(r.status)}</Badge> : null}
              </>
            );
            return (
              <li key={r.id}>
                {href ? (
                  <Link to={href} className="group flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50">
                    {content}
                  </Link>
                ) : (
                  <div className="flex items-center gap-3 px-4 py-2.5">{content}</div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {compact && list.rows.length > rows.length && onViewAll ? (
        <button type="button" onClick={onViewAll} className="w-full border-t px-4 py-2 text-center text-[13px] font-medium text-primary hover:bg-muted/50">
          {t('records.detail.viewAll', { count: list.rows.length })}
        </button>
      ) : null}
    </Card>
  );
}

/** Activities (e.g. "Signed in to Business Card Snap"): icon + title + time; the subtitle is an ISO timestamp. */
function ActivityTimeline({ rows }: { rows: RelatedList['rows'] }) {
  return (
    <ol className="relative px-4 py-3">
      {rows.map((r, i) => {
        const d = r.subtitle ? new Date(r.subtitle) : null;
        const valid = d && !Number.isNaN(d.getTime());
        return (
          <li key={r.id} className="relative flex gap-3 pb-3 last:pb-0">
            {i < rows.length - 1 ? <span className="absolute left-[11px] top-6 h-[calc(100%-1.25rem)] w-px bg-border" aria-hidden /> : null}
            <span className="relative grid size-6 shrink-0 place-items-center rounded-full border bg-card text-muted-foreground">
              <Activity className="size-3" aria-hidden />
            </span>
            <div className="min-w-0 flex-1 pt-0.5">
              <p className="text-[13px] leading-5 text-foreground">{r.title}</p>
              {r.subtitle ? (
                valid ? (
                  <time dateTime={r.subtitle} title={d!.toLocaleString('en-IN')} className="text-xs text-muted-foreground">
                    {relativeTime(r.subtitle)}
                  </time>
                ) : (
                  <p className="text-xs text-muted-foreground">{r.subtitle}</p>
                )
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function RecordInfoCard({ record, meta }: { record: RecordRow; meta: ObjectMeta }) {
  const { t } = useTranslation();
  const byKey = fieldIndex(meta);
  const who = (key: string) => {
    const f = byKey.get(key);
    const v = record.values[key];
    if (v === null || v === undefined || v === '') return null;
    return f ? <FieldValue field={f} record={record} meta={meta} /> : <span>{record.lookups[key]?.label ?? String(v)}</span>;
  };
  const owner = byKey.get('ownerId');
  const rows: Array<{ label: string; time: string; by: ReturnType<typeof who> }> = [
    { label: t('records.detail.created'), time: record.createdAt, by: who('createdBy') },
    { label: t('records.detail.updated'), time: record.updatedAt, by: who('updatedBy') }
  ];
  return (
    <Card>
      <CardHeader className="px-4 py-3" title={<span className="text-[13px]">{t('records.detail.recordInfo')}</span>} />
      <dl className="space-y-3 px-4 py-3 text-[13px]">
        {owner && record.values.ownerId ? (
          <div>
            <dt className="text-xs text-muted-foreground">{owner.label}</dt>
            <dd className="mt-0.5">
              <FieldValue field={owner} record={record} meta={meta} />
            </dd>
          </div>
        ) : null}
        {rows.map((r) => (
          <div key={r.label}>
            <dt className="text-xs text-muted-foreground">{r.label}</dt>
            <dd className="mt-0.5 text-foreground">
              <time dateTime={r.time} title={new Date(r.time).toLocaleString('en-IN')}>
                {relativeTime(r.time)}
              </time>
              {r.by ? (
                <span className="text-muted-foreground">
                  {' '}
                  {t('records.detail.by')} {r.by}
                </span>
              ) : null}
            </dd>
          </div>
        ))}
        <div>
          <dt className="text-xs text-muted-foreground">{t('records.detail.version')}</dt>
          <dd className="mt-0.5 tabular-nums text-foreground">{record.version}</dd>
        </div>
      </dl>
    </Card>
  );
}

function ConversionBanner({ conversion }: { conversion: NonNullable<RecordDetail['conversion']> }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const link = (label: string, to: string | null, name?: string) =>
    name ? (
      <span className="whitespace-nowrap">
        {label}{' '}
        {to ? (
          <Link to={to} className="hover:underline">
            {name}
          </Link>
        ) : (
          <span className="font-medium text-foreground">{name}</span>
        )}
      </span>
    ) : null;
  const parts = [
    link(t('records.convert.account'), scopedLookupHref(scope, 'accounts', conversion.account?.id), conversion.account?.label),
    link(t('records.convert.contact'), scopedLookupHref(scope, 'contacts', conversion.contact?.id), conversion.contact?.label),
    link(t('records.convert.workspace'), scopedLookupHref(scope, 'workspaces', conversion.workspace?.id), conversion.workspace?.label)
  ].filter(Boolean);
  const inv = conversion.invitation;
  return (
    <div className="mb-4 space-y-2">
      <Alert tone="success" title={t('records.detail.convertedTitle', { time: relativeTime(conversion.convertedAt) })}>
        <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
          <span aria-hidden>→</span>
          {parts.map((p, i) => (
            <span key={i} className="inline-flex items-center gap-1.5">
              {i > 0 ? <span aria-hidden>·</span> : null}
              {p}
            </span>
          ))}
        </span>
        {inv ? (
          <span className="mt-1 flex flex-wrap items-center gap-1.5">
            {t('records.detail.invitation', { email: inv.email ?? inv.displayName })}
            <Badge tone={guessTone(inv.status)}>{t(`records.invitationStatus.${inv.status}`, { defaultValue: humanize(inv.status) })}</Badge>
            {inv.status === 'pending' || inv.status === 'delivered' ? (
              <span className="text-xs">{t('records.detail.invitationExpires', { time: relativeTime(inv.expiresAt) })}</span>
            ) : null}
          </span>
        ) : null}
      </Alert>
      {scope.audience === 'owner' && inv?.devAcceptUrl && (inv.status === 'pending' || inv.status === 'delivered') ? <DevLink url={inv.devAcceptUrl} label={t('records.convert.devLink')} /> : null}
    </div>
  );
}

function DetailSkeleton() {
  return (
    <PageContainer wide>
      <Skeleton className="mb-3 h-4 w-40" />
      <Card className="mb-4 p-5">
        <div className="flex items-start gap-4">
          <Skeleton className="size-11 rounded-lg" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-6 w-64" />
            <Skeleton className="h-3 w-24" />
          </div>
        </div>
        <div className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="space-y-1.5">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-4 w-28" />
            </div>
          ))}
        </div>
      </Card>
      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="space-y-4 p-4 lg:col-span-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="h-8" />
              <div className="grid gap-4 sm:grid-cols-2">
                {Array.from({ length: 4 }).map((__, j) => (
                  <div key={j} className="space-y-1.5">
                    <Skeleton className="h-3 w-20" />
                    <Skeleton className="h-4 w-40" />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </Card>
        <div className="space-y-4">
          <Card className="space-y-3 p-4">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-9" />
            <Skeleton className="h-9" />
          </Card>
          <Card className="space-y-3 p-4">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-9" />
          </Card>
        </div>
      </div>
    </PageContainer>
  );
}

function DuplicatesBanner({ object, id, enabled, onReview }: { object: ObjectKey; id: string; enabled: boolean; onReview: () => void }) {
  const { t } = useTranslation();
  const q = useDuplicates(object, id, enabled);
  if (!q.data?.length) return null;
  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-warning/30 bg-warning-soft px-4 py-2.5 text-[13px]">
      <span>{t('records.detail.duplicates', { count: q.data.length })}</span>
      <Button size="sm" variant="outline" onClick={onReview}>
        <GitMerge /> {t('records.detail.reviewDuplicates')}
      </Button>
    </div>
  );
}
