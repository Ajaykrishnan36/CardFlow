import { useRef, useState, type ChangeEvent, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { BadgeCheck, Bookmark, ExternalLink, Eye, EyeOff, Globe, ImageOff, Mail, MapPin, Pencil, Phone, Store, Trash2, Upload, User, Users } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AppBusiness, BusinessListing, BusinessPatch, BusinessStatus, BusinessVerification } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, DetailItem, PageContainer } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { useWorkspace, workspaceBase } from '../workspace-context';
import { appAccess, appKeys, appUserPath, BusinessStatusBadge, formatDate, formatPhone, ListingBadge, verified, VerificationBadge } from './app-utils';
import { LeadStatusBadge, SaversDialog } from './business-savers';

const STATUSES: BusinessStatus[] = ['live', 'draft', 'pending_verification', 'under_review', 'suspended', 'removed'];
const VERIFICATIONS: BusinessVerification[] = ['gst', 'pan', 'tan', 'manual', 'pending', 'failed'];

/** /crm/w/:ws/businesses/:id — one listing: details, badge, visibility, edit and delete. */
export function BusinessDetailPage() {
  const { context } = useWorkspace();
  const { id = '' } = useParams();
  if (!appAccess(context, 'app_business').read) return <RecordNoAccess />;
  return <BusinessDetailView key={id} id={id} />;
}

function BusinessDetailView({ id }: { id: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { code, context } = useWorkspace();
  const can = appAccess(context, 'app_business');
  const canUsers = appAccess(context, 'app_user').read;
  const q = useQuery({ queryKey: appKeys.business(code, id), queryFn: () => workspaceAppApi(code).business(id) });
  const b = q.data;
  useDocumentTitle(b ? b.name : t('workspaceApp.app.bizTitle'));
  const [editOpen, setEditOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [saversOpen, setSaversOpen] = useState(false);

  const update = useMutation({
    mutationFn: (body: BusinessPatch) => workspaceAppApi(code).updateBusiness(id, body),
    onSuccess: (next, body) => {
      qc.setQueryData<AppBusiness>(appKeys.business(code, id), next);
      void qc.invalidateQueries({ queryKey: appKeys.all(code) });
      if (body.listing && Object.keys(body).length === 1) toast.success(t(body.listing === 'listed' ? 'workspaceApp.app.listedToast' : 'workspaceApp.app.hiddenToast', { name: next.name }));
      else toast.success(t('workspaceApp.app.bizSavedToast', { name: next.name }));
      setEditOpen(false);
    }
  });
  const remove = useMutation({
    mutationFn: () => workspaceAppApi(code).deleteBusiness(id),
    onSuccess: () => {
      toast.success(t('workspaceApp.app.bizDeletedToast', { name: b?.name }));
      void qc.invalidateQueries({ queryKey: appKeys.all(code) });
      navigate(`${workspaceBase(code)}/businesses`, { replace: true });
    },
    onError: (e) => {
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
      setConfirmDelete(false);
    }
  });

  if (isApiError(q.error) && q.error.status === 403) return <RecordNoAccess />;
  if (q.isError) {
    return (
      <PageContainer>
        <ErrorState
          title={isApiError(q.error) && q.error.status === 404 ? t('workspaceApp.app.bizNotFound') : t('workspaceApp.app.loadError')}
          message={isApiError(q.error) ? q.error.message : undefined}
          onRetry={() => void q.refetch()}
        />
      </PageContainer>
    );
  }
  if (!b) {
    return (
      <PageContainer wide>
        <Skeleton className="h-4 w-40" />
        <Skeleton className="mt-3 h-40 w-full" />
        <Skeleton className="mt-4 h-64 w-full" />
      </PageContainer>
    );
  }

  const quick = (body: BusinessPatch) =>
    update.mutate(body, { onError: (e) => toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError')) });
  const address = [b.addressLine1, b.addressLine2, b.locality, b.city, b.district, b.state].filter(Boolean).join(', ');

  return (
    <PageContainer wide>
      <Breadcrumbs items={[{ label: t('workspaceApp.app.bizTitle'), to: `${workspaceBase(code)}/businesses` }, { label: b.name }]} />
      <Card className="mt-3 p-5">
        <div className="flex flex-col gap-4 md:flex-row md:items-start">
          <span className="grid size-14 shrink-0 place-items-center rounded-xl bg-primary-soft text-primary">
            <Store className="size-7" aria-hidden />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-xl font-semibold tracking-tight">{b.name}</h1>
              <LeadStatusBadge biz={b} />
              <VerificationBadge value={b.verification} />
              <ListingBadge value={b.listing} />
              <BusinessStatusBadge value={b.status} />
            </div>
            <p className="mt-1 text-[13px] uppercase tracking-wide text-muted-foreground">
              {[b.category, b.city && `${b.city} (${b.pincode})`].filter(Boolean).join(' · ')}
            </p>
            {b.owner.id ? (
              <p className="mt-1.5 flex items-center gap-1.5 text-[13px] text-muted-foreground">
                <User className="size-3.5" aria-hidden />
                {t('workspaceApp.app.owner')}:{' '}
                {canUsers ? (
                  <Link to={appUserPath(code, b.owner.id)} className="font-medium text-primary hover:underline">
                    {b.owner.name || formatPhone(b.owner.phone)}
                  </Link>
                ) : (
                  <span className="font-medium text-foreground">{b.owner.name}</span>
                )}
                <span className="tabular-nums">({formatPhone(b.owner.phone)})</span>
              </p>
            ) : (
              <p className="mt-1.5 flex flex-wrap items-center gap-1.5 text-[13px] text-muted-foreground">
                <User className="size-3.5" aria-hidden />
                {t('workspaceApp.app.notOnApp')} — {t('workspaceApp.app.leadStatusHint.lead', { phone: formatPhone(b.contactPhone) || '—' })}
              </p>
            )}
            {b.leadId ? (
              <Link to={`${workspaceBase(code)}/leads/${b.leadId}`} className="mt-1.5 inline-flex items-center gap-1 text-[13px] font-medium text-primary hover:underline">
                <ExternalLink className="size-3.5" aria-hidden /> {t('workspaceApp.app.openLead')}
              </Link>
            ) : null}
          </div>
          {can.update || can.delete ? (
            <div className="flex shrink-0 flex-wrap gap-2">
              {can.update ? (
                <>
                  <Button variant="outline" onClick={() => quick({ listing: b.listing === 'listed' ? 'unlisted' : 'listed' })} loading={update.isPending && !editOpen}>
                    {b.listing === 'listed' ? <EyeOff /> : <Eye />} {b.listing === 'listed' ? t('workspaceApp.app.hideFromSearch') : t('workspaceApp.app.showInSearch')}
                  </Button>
                  <Button onClick={() => setEditOpen(true)}>
                    <Pencil /> {t('workspaceApp.app.edit')}
                  </Button>
                </>
              ) : null}
              {can.delete ? (
                <Button variant="outline" className="text-danger hover:bg-danger-soft" onClick={() => setConfirmDelete(true)}>
                  <Trash2 /> {t('workspaceApp.app.delete')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </Card>

      <CardImagesCard biz={b} code={code} canUpdate={can.update} />

      <div className="mt-4 grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title={t('workspaceApp.app.listingDetails')} />
          <dl className="grid gap-4 px-4 pb-4 sm:grid-cols-2">
            <DetailItem label={t('workspaceApp.app.bizName')}>{b.name}</DetailItem>
            <DetailItem label={t('workspaceApp.app.category')}>{b.category || null}</DetailItem>
            <DetailItem label={t('workspaceApp.app.address')} className="sm:col-span-2">
              {address ? (
                <span className="inline-flex items-start gap-1.5">
                  <MapPin className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden /> {address} – {b.pincode}
                </span>
              ) : null}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.gstin')}>{b.gstin ? <span className="font-mono">{b.gstin}</span> : null}</DetailItem>
            {b.source === 'card' ? (
              <DetailItem label={t('workspaceApp.app.cardContact')}>
                {b.contactName || b.contactPhone ? (
                  <span>
                    {[b.contactName, b.contactDesignation].filter(Boolean).join(' · ')}
                    {b.contactPhone ? <span className="block tabular-nums text-muted-foreground">{formatPhone(b.contactPhone)}</span> : null}
                  </span>
                ) : null}
              </DetailItem>
            ) : null}
            <DetailItem label={t('workspaceApp.app.phones')}>
              {b.phones.length ? (
                <span className="space-y-0.5">
                  {b.phones.map((p) => (
                    <span key={p} className="flex items-center gap-1.5 tabular-nums">
                      <Phone className="size-3.5 text-muted-foreground" aria-hidden /> {formatPhone(p)}
                    </span>
                  ))}
                </span>
              ) : null}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.email')}>
              {b.email ? (
                <span className="inline-flex items-center gap-1.5">
                  <Mail className="size-3.5 text-muted-foreground" aria-hidden /> {b.email}
                </span>
              ) : null}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.website')}>
              {b.website ? (
                <a href={/^https?:\/\//.test(b.website) ? b.website : `https://${b.website}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-primary hover:underline">
                  <Globe className="size-3.5" aria-hidden /> {b.website}
                </a>
              ) : null}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.description')} className="sm:col-span-2">
              {b.description ? <span className="whitespace-pre-line">{b.description}</span> : null}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.services')} className="sm:col-span-2">
              {b.services.length ? (
                <span className="flex flex-wrap gap-1.5">
                  {b.services.map((s) => (
                    <Badge key={s} tone="primary">
                      {s}
                    </Badge>
                  ))}
                </span>
              ) : null}
            </DetailItem>
          </dl>
        </Card>
        <Card>
          <CardHeader title={t('workspaceApp.app.directory')} />
          <dl className="grid gap-4 px-4 pb-4">
            <DetailItem label={t('workspaceApp.app.verificationLabel')}>
              {can.update ? (
                <Select
                  value={b.verification}
                  onChange={(e) => quick({ verification: e.target.value as BusinessVerification })}
                  options={VERIFICATIONS.map((v) => ({ value: v, label: t(`workspaceApp.app.verification.${v}`) }))}
                  aria-label={t('workspaceApp.app.verificationLabel')}
                />
              ) : (
                <VerificationBadge value={b.verification} />
              )}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.statusLabel')}>
              {can.update ? (
                <Select
                  value={b.status}
                  onChange={(e) => quick({ status: e.target.value as BusinessStatus })}
                  options={STATUSES.map((v) => ({ value: v, label: t(`workspaceApp.app.bizStatus.${v}`) }))}
                  aria-label={t('workspaceApp.app.statusLabel')}
                />
              ) : (
                t(`workspaceApp.app.bizStatus.${b.status}`)
              )}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.visibility')}>
              <ListingBadge value={b.listing} />
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.savedByLabel')}>
              {b.savedBy ? (
                <button
                  type="button"
                  onClick={() => setSaversOpen(true)}
                  className="inline-flex items-center gap-1.5 font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <Users className="size-3.5" aria-hidden /> {t('workspaceApp.app.savedBy', { count: b.savedBy })}
                </button>
              ) : (
                <span className="inline-flex items-center gap-1.5">
                  <Bookmark className="size-3.5 text-muted-foreground" aria-hidden /> {t('workspaceApp.app.savedBy', { count: 0 })}
                </span>
              )}
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.completeness')}>
              <span className="flex items-center gap-2">
                <span className="h-1.5 w-24 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-primary" style={{ width: `${Math.min(100, b.completeness)}%` }} />
                </span>
                <span className="tabular-nums">{b.completeness}%</span>
              </span>
            </DetailItem>
            <DetailItem label={t('workspaceApp.app.updated')}>{formatDate(b.updatedAt)}</DetailItem>
          </dl>
        </Card>
      </div>

      <SaversDialog biz={b} code={code} canUsers={canUsers} open={saversOpen} onOpenChange={setSaversOpen} />

      {can.update ? (
        <EditBusinessDialog
          open={editOpen}
          onOpenChange={setEditOpen}
          biz={b}
          loading={update.isPending}
          error={update.error}
          onSave={(body) => update.mutate(body)}
        />
      ) : null}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('workspaceApp.app.confirmDeleteBiz.title', { name: b.name })}
        body={t('workspaceApp.app.confirmDeleteBiz.body')}
        confirmLabel={t('workspaceApp.app.confirmDeleteBiz.action')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
    </PageContainer>
  );
}

type Form = {
  name: string;
  categoryId: string;
  addressLine1: string;
  locality: string;
  city: string;
  state: string;
  pincode: string;
  gstin: string;
  email: string;
  website: string;
  description: string;
  services: string;
  ownerPhone: string;
  listing: BusinessListing;
  verification: BusinessVerification;
};

function toForm(b: AppBusiness): Form {
  return {
    name: b.name,
    categoryId: b.categoryId,
    addressLine1: b.addressLine1,
    locality: b.locality,
    city: b.city,
    state: b.state,
    pincode: b.pincode,
    gstin: b.gstin,
    email: b.email,
    website: b.website,
    description: b.description,
    services: b.services.join(', '),
    ownerPhone: b.owner.phone,
    listing: b.listing,
    verification: b.verification
  };
}

function EditBusinessDialog({
  open,
  onOpenChange,
  biz,
  loading,
  error,
  onSave
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  biz: AppBusiness;
  loading: boolean;
  error: unknown;
  onSave: (body: BusinessPatch) => void;
}) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const cats = useQuery({ queryKey: appKeys.categories(code), queryFn: () => workspaceAppApi(code).categories(), enabled: open, staleTime: 300_000 });
  const [form, setForm] = useState<Form>(() => toForm(biz));
  const fe = isApiError(error) ? error.fieldErrors : {};
  const set = (k: keyof Form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const orig = toForm(biz);
    const body: BusinessPatch = {};
    for (const k of Object.keys(form) as Array<keyof Form>) {
      if (form[k] === orig[k]) continue;
      if (k === 'services') body.services = form.services.split(',').map((s) => s.trim()).filter(Boolean);
      else if (k === 'ownerPhone') body.ownerPhone = form.ownerPhone.trim();
      else (body as Record<string, string>)[k] = (form[k] as string).trim();
    }
    if (Object.keys(body).length === 0) {
      onOpenChange(false);
      return;
    }
    onSave(body);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) setForm(toForm(biz));
        onOpenChange(o);
      }}
    >
      <DialogContent className="top-[5vh] max-h-[90vh] max-w-2xl overflow-y-auto p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('workspaceApp.app.editBizTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('workspaceApp.app.editBizBody')}</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 grid gap-4 sm:grid-cols-2">
          <Field label={t('workspaceApp.app.bizName')} error={fe.name} className="sm:col-span-2">
            <Input value={form.name} onChange={set('name')} maxLength={200} autoFocus />
          </Field>
          <Field label={t('workspaceApp.app.category')} error={fe.categoryId}>
            <Select value={form.categoryId} onChange={set('categoryId')} options={(cats.data ?? [{ id: biz.categoryId, name: biz.category }]).map((c) => ({ value: c.id, label: c.name }))} />
          </Field>
          <Field label={t('workspaceApp.app.gstin')} error={fe.gstin}>
            <Input value={form.gstin} onChange={(e) => setForm((f) => ({ ...f, gstin: e.target.value.toUpperCase() }))} maxLength={15} className="font-mono" placeholder="33ABCDE1234F1Z5" />
          </Field>
          <Field label={t('workspaceApp.app.addressLine1')} error={fe.addressLine1} className="sm:col-span-2">
            <Input value={form.addressLine1} onChange={set('addressLine1')} maxLength={255} />
          </Field>
          <Field label={t('workspaceApp.app.locality')} error={fe.locality}>
            <Input value={form.locality} onChange={set('locality')} maxLength={150} />
          </Field>
          <Field label={t('workspaceApp.app.city')} error={fe.city}>
            <Input value={form.city} onChange={set('city')} maxLength={100} />
          </Field>
          <Field label={t('workspaceApp.app.state')} error={fe.state}>
            <Input value={form.state} onChange={set('state')} maxLength={100} />
          </Field>
          <Field label={t('workspaceApp.app.pincode')} error={fe.pincode}>
            <Input value={form.pincode} onChange={set('pincode')} inputMode="numeric" maxLength={6} />
          </Field>
          <Field label={t('workspaceApp.app.email')} error={fe.email}>
            <Input type="email" value={form.email} onChange={set('email')} maxLength={255} />
          </Field>
          <Field label={t('workspaceApp.app.website')} error={fe.website}>
            <Input value={form.website} onChange={set('website')} maxLength={255} />
          </Field>
          <Field label={t('workspaceApp.app.ownerPhone')} error={fe.ownerPhone} hint={t('workspaceApp.app.ownerPhoneHint')}>
            <Input value={form.ownerPhone} onChange={set('ownerPhone')} inputMode="tel" />
          </Field>
          <Field label={t('workspaceApp.app.verificationLabel')} error={fe.verification}>
            <Select
              value={form.verification}
              onChange={set('verification')}
              options={VERIFICATIONS.map((v) => ({ value: v, label: t(`workspaceApp.app.verification.${v}`) }))}
            />
          </Field>
          <Field label={t('workspaceApp.app.services')} error={fe.services} hint={t('workspaceApp.app.servicesHint')} className="sm:col-span-2">
            <Input value={form.services} onChange={set('services')} />
          </Field>
          <Field label={t('workspaceApp.app.description')} error={fe.description} className="sm:col-span-2">
            <Textarea value={form.description} onChange={set('description')} rows={3} maxLength={4000} />
          </Field>
          <div className="sm:col-span-2">
            <p className="mb-1.5 text-[13px] font-medium">{t('workspaceApp.app.visibility')}</p>
            <div className="grid grid-cols-2 gap-2">
              {(['listed', 'unlisted'] as BusinessListing[]).map((v) => (
                <button
                  key={v}
                  type="button"
                  aria-pressed={form.listing === v}
                  onClick={() => setForm((f) => ({ ...f, listing: v }))}
                  className={cn(
                    'flex items-center justify-center gap-1.5 rounded-lg border py-2 text-[13px] font-medium transition-colors',
                    form.listing === v ? 'border-primary bg-primary-soft text-primary' : 'text-muted-foreground hover:bg-muted'
                  )}
                >
                  {v === 'listed' ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
                  {v === 'listed' ? t('workspaceApp.app.listed') : t('workspaceApp.app.hidden')}
                </button>
              ))}
            </div>
            {verified(form.verification) ? (
              <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                <BadgeCheck className="size-3.5 text-amber-600" aria-hidden /> {t('workspaceApp.app.badgeShown')}
              </p>
            ) : null}
          </div>
          {isApiError(error) && Object.keys(fe).length === 0 ? <p className="text-sm text-danger sm:col-span-2">{error.message}</p> : null}
          <div className="flex justify-end gap-2 sm:col-span-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={loading}>
              {t('workspaceApp.app.saveChanges')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const MAX_IMAGE_BYTES = 8 * 1024 * 1024;

/** Front/back of the original business card, with upload / replace. */
function CardImagesCard({ biz, code, canUpdate }: { biz: AppBusiness; code: string; canUpdate: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const side = useRef<'front' | 'back'>('front');
  const upload = useMutation({
    mutationFn: ({ s, data }: { s: 'front' | 'back'; data: string }) => workspaceAppApi(code).uploadCardImage(biz.id, s, data),
    onSuccess: (next, vars) => {
      qc.setQueryData<AppBusiness>(appKeys.business(code, biz.id), next);
      void qc.invalidateQueries({ queryKey: appKeys.all(code) });
      toast.success(t('workspaceApp.app.imageUploaded', { side: t(vars.s === 'back' ? 'workspaceApp.app.cardBack' : 'workspaceApp.app.cardFront') }));
    },
    onError: (e) => toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError'))
  });
  const pick = (s: 'front' | 'back') => {
    side.current = s;
    if (input.current) {
      input.current.value = '';
      input.current.click();
    }
  };
  const onFile = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (!file.type.startsWith('image/') || file.size > MAX_IMAGE_BYTES) {
      toast.error(t('workspaceApp.app.imageTooBig'));
      return;
    }
    const reader = new FileReader();
    reader.onload = () => upload.mutate({ s: side.current, data: String(reader.result) });
    reader.readAsDataURL(file);
  };
  const version = biz.updatedAt;
  return (
    <Card className="mt-4">
      <CardHeader title={t('workspaceApp.app.cardImages')} />
      <input ref={input} type="file" accept="image/*" className="hidden" onChange={onFile} />
      <div className="grid gap-4 px-4 pb-4 sm:grid-cols-2">
        {(['front', 'back'] as const).map((s) => {
          const has = s === 'front' ? biz.hasFrontImage : biz.hasBackImage;
          const label = t(s === 'front' ? 'workspaceApp.app.cardFront' : 'workspaceApp.app.cardBack');
          return (
            <figure key={s} className="min-w-0">
              <figcaption className="mb-1.5 flex items-center justify-between text-xs font-medium text-muted-foreground">
                {label}
                {canUpdate ? (
                  <Button size="sm" variant="outline" onClick={() => pick(s)} loading={upload.isPending && upload.variables?.s === s}>
                    <Upload /> {has ? t('workspaceApp.app.replaceImage') : t('workspaceApp.app.uploadImage')}
                  </Button>
                ) : null}
              </figcaption>
              {has ? (
                <a href={workspaceAppApi(code).cardImageUrl(biz.id, s, version)} target="_blank" rel="noreferrer" className="block">
                  <img
                    src={workspaceAppApi(code).cardImageUrl(biz.id, s, version)}
                    alt={`${biz.name} — ${label}`}
                    className="aspect-[1.75] w-full rounded-lg border bg-muted object-contain"
                  />
                </a>
              ) : (
                <div className="grid aspect-[1.75] w-full place-items-center rounded-lg border border-dashed bg-muted/40 text-xs text-muted-foreground">
                  <span className="flex flex-col items-center gap-1.5">
                    <ImageOff className="size-5" aria-hidden />
                    {t('workspaceApp.app.noCardImage')}
                  </span>
                </div>
              )}
            </figure>
          );
        })}
      </div>
    </Card>
  );
}
