import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Briefcase, Contact, Crown, CreditCard, Gift, LifeBuoy, Mail, MapPin, MoreHorizontal, Pencil, Phone, ShieldCheck, Smartphone, Store, Trash2, UserX } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AppPlanId, AppUserDetail, AppUserPatch, SavedCard } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, DetailItem, PageContainer, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';
import { scopedLookupHref, useRecordScope } from '@crm/features/records/record-scope';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { ticketAccess, useWorkspace, workspaceBase } from '../workspace-context';
import { supportPath, TicketStatusBadge } from '../support/support-utils';
import {
  AccessBadge,
  appAccess,
  appKeys,
  businessPath,
  formatDate,
  formatPhone,
  ListingBadge,
  PLANS,
  RoleBadge,
  UserStatusBadge,
  VerificationBadge
} from './app-utils';

type Tab = 'overview' | 'businesses' | 'cards' | 'tickets' | 'payments';
const TABS: Tab[] = ['overview', 'businesses', 'cards', 'tickets', 'payments'];

/** /crm/w/:ws/app-users/:id — one app user's profile, access and everything they own in the app. */
export function AppUserDetailPage() {
  const { context } = useWorkspace();
  const { id = '' } = useParams();
  if (!appAccess(context, 'app_user').read) return <RecordNoAccess />;
  return <AppUserDetailView key={id} id={id} />;
}

function errorText(e: unknown, fallback: string) {
  if (!isApiError(e)) return fallback;
  const first = Object.values(e.fieldErrors)[0];
  return first ?? e.message;
}

function AppUserDetailView({ id }: { id: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { code, context } = useWorkspace();
  const can = appAccess(context, 'app_user');
  const canTickets = ticketAccess(context).read;
  const canBiz = appAccess(context, 'app_business').read;
  const scope = useRecordScope();
  const [sp, setSp] = useSearchParams();
  const rawTab = sp.get('tab') as Tab | null;
  const tab: Tab = rawTab && TABS.includes(rawTab) ? rawTab : 'overview';

  const q = useQuery({ queryKey: appKeys.user(code, id), queryFn: () => workspaceAppApi(code).user(id) });
  const u = q.data;
  useDocumentTitle(u ? u.name || u.phone : t('workspaceApp.app.usersTitle'));

  const [grantOpen, setGrantOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [confirm, setConfirm] = useState<null | 'revoke' | 'admin' | 'user' | 'suspend' | 'activate' | 'delete'>(null);

  const update = useMutation({
    mutationFn: (body: AppUserPatch) => workspaceAppApi(code).updateUser(id, body),
    onSuccess: (next, body) => {
      qc.setQueryData<AppUserDetail>(appKeys.user(code, id), next);
      void qc.invalidateQueries({ queryKey: appKeys.all(code) });
      void qc.invalidateQueries({ queryKey: ['workspace', code, 'records'] });
      const name = next.name || formatPhone(next.phone);
      if (body.access?.action === 'grant') toast.success(t('workspaceApp.app.grantedToast', { name, plan: next.planName }));
      else if (body.access?.action === 'revoke') toast.success(t('workspaceApp.app.revokedToast', { name }));
      else if (body.role) toast.success(t('workspaceApp.app.roleToast', { name, role: t(body.role === 'admin' ? 'workspaceApp.app.roleAdmin' : 'workspaceApp.app.roleUser') }));
      else if (body.status) toast.success(t(body.status === 'suspended' ? 'workspaceApp.app.suspendedToast' : 'workspaceApp.app.activatedToast', { name }));
      else toast.success(t('workspaceApp.app.savedToast'));
      setGrantOpen(false);
      setEditOpen(false);
      setConfirm(null);
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const remove = useMutation({
    mutationFn: () => workspaceAppApi(code).deleteUser(id),
    onSuccess: () => {
      toast.success(t('workspaceApp.app.deletedToast', { name: u?.name || u?.phone }));
      void qc.invalidateQueries({ queryKey: appKeys.all(code) });
      navigate(`${workspaceBase(code)}/app-users`, { replace: true });
    },
    onError: (e) => {
      toast.error(errorText(e, t('common.genericError')));
      setConfirm(null);
    }
  });

  if (isApiError(q.error) && q.error.status === 403) return <RecordNoAccess />;
  if (q.isError) {
    return (
      <PageContainer>
        <ErrorState
          title={isApiError(q.error) && q.error.status === 404 ? t('workspaceApp.app.userNotFound') : t('workspaceApp.app.loadError')}
          message={isApiError(q.error) ? q.error.message : undefined}
          onRetry={() => void q.refetch()}
        />
      </PageContainer>
    );
  }
  if (!u) return <DetailSkeleton />;

  const name = u.name || t('workspaceApp.app.unnamed');
  const accountHref = u.account ? scopedLookupHref(scope, 'accounts', u.account.id) : null;
  const contactHref = u.contact ? scopedLookupHref(scope, 'contacts', u.contact.id) : null;
  const setTab = (v: Tab) =>
    setSp(
      (prev) => {
        const n = new URLSearchParams(prev);
        if (v === 'overview') n.delete('tab');
        else n.set('tab', v);
        return n;
      },
      { replace: true }
    );

  const tabs = [
    { value: 'overview' as Tab, label: t('workspaceApp.app.tabOverview') },
    ...(canBiz ? [{ value: 'businesses' as Tab, label: <TabLabel text={t('workspaceApp.app.tabBusinesses')} n={u.businessList.length} /> }] : []),
    { value: 'cards' as Tab, label: <TabLabel text={t('workspaceApp.app.tabCards')} n={u.cardList.length} /> },
    ...(canTickets ? [{ value: 'tickets' as Tab, label: <TabLabel text={t('workspaceApp.app.tabTickets')} n={u.ticketList.length} /> }] : []),
    { value: 'payments' as Tab, label: <TabLabel text={t('workspaceApp.app.tabPayments')} n={u.payments.length} /> }
  ];

  return (
    <PageContainer wide>
      <Breadcrumbs items={[{ label: t('workspaceApp.app.usersTitle'), to: `${workspaceBase(code)}/app-users` }, { label: name }]} />

      <Card className="mt-3 overflow-hidden">
        <div className="flex flex-col gap-4 p-5 md:flex-row md:items-start">
          <Avatar name={u.name || u.phone} className="size-14 text-lg" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-xl font-semibold tracking-tight">{name}</h1>
              <RoleBadge role={u.role} />
              <AccessBadge user={u} />
              <UserStatusBadge status={u.status} />
            </div>
            <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[13px] text-muted-foreground">
              <span className="inline-flex items-center gap-1 tabular-nums">
                <Phone className="size-3.5" aria-hidden />
                {formatPhone(u.phone)}
              </span>
              {u.email ? (
                <span className="inline-flex items-center gap-1">
                  <Mail className="size-3.5" aria-hidden />
                  {u.email}
                </span>
              ) : null}
              {u.city ? (
                <span className="inline-flex items-center gap-1">
                  <MapPin className="size-3.5" aria-hidden />
                  {[u.city, u.state].filter(Boolean).join(', ')}
                </span>
              ) : null}
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              {accountHref ? (
                <Link to={accountHref} className="inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium hover:bg-muted">
                  <Briefcase className="size-3.5 text-muted-foreground" aria-hidden />
                  {u.account!.label}
                </Link>
              ) : null}
              {contactHref ? (
                <Link to={contactHref} className="inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium hover:bg-muted">
                  <Contact className="size-3.5 text-muted-foreground" aria-hidden />
                  {u.contact!.label}
                </Link>
              ) : null}
            </div>
          </div>
          {can.update || can.delete ? (
            <div className="flex shrink-0 items-center gap-2">
              {can.update ? (
                u.premium ? (
                  <Button variant="outline" onClick={() => setGrantOpen(true)}>
                    <Gift /> {t('workspaceApp.app.changeAccess')}
                  </Button>
                ) : (
                  <Button onClick={() => setGrantOpen(true)}>
                    <Gift /> {t('workspaceApp.app.grantAccess')}
                  </Button>
                )
              ) : null}
              <Menu>
                <MenuTrigger asChild>
                  <Button variant="outline" size="icon" aria-label={t('workspaceApp.app.moreActions')}>
                    <MoreHorizontal />
                  </Button>
                </MenuTrigger>
                <MenuContent align="end">
                  {can.update ? (
                    <>
                      <MenuItem onSelect={() => setEditOpen(true)}>
                        <Pencil /> {t('workspaceApp.app.editProfile')}
                      </MenuItem>
                      {u.premium ? (
                        <MenuItem onSelect={() => setConfirm('revoke')}>
                          <Crown /> {t('workspaceApp.app.revokeAccess')}
                        </MenuItem>
                      ) : null}
                      <MenuItem onSelect={() => setConfirm(u.role === 'admin' ? 'user' : 'admin')}>
                        <ShieldCheck /> {u.role === 'admin' ? t('workspaceApp.app.makeUser') : t('workspaceApp.app.makeAdmin')}
                      </MenuItem>
                      <MenuItem onSelect={() => setConfirm(u.status === 'suspended' ? 'activate' : 'suspend')}>
                        <UserX /> {u.status === 'suspended' ? t('workspaceApp.app.activate') : t('workspaceApp.app.suspend')}
                      </MenuItem>
                    </>
                  ) : null}
                  {can.delete && u.role !== 'admin' ? (
                    <>
                      <MenuSeparator />
                      <MenuItem danger onSelect={() => setConfirm('delete')}>
                        <Trash2 /> {t('workspaceApp.app.deleteUser')}
                      </MenuItem>
                    </>
                  ) : null}
                </MenuContent>
              </Menu>
            </div>
          ) : null}
        </div>
        <div className="grid grid-cols-2 border-t sm:grid-cols-4">
          <Stat icon={Store} label={t('workspaceApp.app.tabBusinesses')} value={u.businessList.length} />
          <Stat icon={CreditCard} label={t('workspaceApp.app.tabCards')} value={u.cardList.length} />
          <Stat icon={LifeBuoy} label={t('workspaceApp.app.openTicketsLabel')} value={u.openTickets} />
          <Stat icon={Smartphone} label={t('workspaceApp.app.lastSignIn')} value={u.lastLoginAt ? relativeTime(u.lastLoginAt) : t('workspaceApp.app.never')} />
        </div>
      </Card>

      <Tabs value={tab} onChange={setTab} items={tabs} className="mt-5" />

      <div className="mt-4">
        {tab === 'overview' ? (
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader title={t('workspaceApp.app.profile')} />
              <dl className="grid gap-4 px-4 pb-4 sm:grid-cols-2">
                <DetailItem label={t('workspaceApp.app.name')}>{u.name || null}</DetailItem>
                <DetailItem label={t('workspaceApp.app.phone')}>{formatPhone(u.phone)}</DetailItem>
                <DetailItem label={t('workspaceApp.app.email')}>{u.email || null}</DetailItem>
                <DetailItem label={t('workspaceApp.app.city')}>{[u.city, u.state].filter(Boolean).join(', ') || null}</DetailItem>
                <DetailItem label={t('workspaceApp.app.joined')}>{formatDate(u.createdAt)}</DetailItem>
                <DetailItem label={t('workspaceApp.app.lastSignIn')}>{u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString('en-IN') : null}</DetailItem>
              </dl>
            </Card>
            <Card>
              <CardHeader
                title={t('workspaceApp.app.access')}
                actions={
                  can.update ? (
                    <Button size="sm" variant={u.premium ? 'outline' : 'primary'} onClick={() => setGrantOpen(true)}>
                      <Gift /> {u.premium ? t('workspaceApp.app.changeAccess') : t('workspaceApp.app.grantAccess')}
                    </Button>
                  ) : null
                }
              />
              <dl className="grid gap-4 px-4 pb-4 sm:grid-cols-2">
                <DetailItem label={t('workspaceApp.app.plan')}>
                  <AccessBadge user={u} />
                </DetailItem>
                <DetailItem label={t('workspaceApp.app.expires')}>
                  {u.premium ? (u.expiresAt ? formatDate(u.expiresAt) : t('workspaceApp.app.neverExpires')) : null}
                </DetailItem>
                <DetailItem label={t('workspaceApp.app.appRole')}>
                  <RoleBadge role={u.role} />
                </DetailItem>
                <DetailItem label={t('workspaceApp.app.freeScans')}>{u.premium ? t('workspaceApp.app.unlimited') : u.freeScansLeft}</DetailItem>
              </dl>
            </Card>
          </div>
        ) : tab === 'businesses' ? (
          <BusinessList user={u} code={code} />
        ) : tab === 'cards' ? (
          <CardList cards={u.cardList} code={code} canBiz={canBiz} />
        ) : tab === 'tickets' ? (
          <Card className="overflow-hidden">
            {u.ticketList.length === 0 ? (
              <EmptyState icon={LifeBuoy} title={t('workspaceApp.app.noTickets')} />
            ) : (
              <ul className="divide-y">
                {u.ticketList.map((tk) => (
                  <li key={tk.id}>
                    <Link to={supportPath(code, tk.id)} className="group flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-[13px] font-medium group-hover:text-primary">{tk.subject || t('workspaceApp.support.noSubject')}</p>
                        <p className="truncate text-xs text-muted-foreground">{tk.reply ? tk.reply : t('workspaceApp.support.noReply')}</p>
                      </div>
                      <TicketStatusBadge status={tk.status} />
                      <span className="w-20 text-right text-xs text-muted-foreground">{relativeTime(tk.createdAt)}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        ) : (
          <Card className="overflow-hidden">
            {u.payments.length === 0 ? (
              <EmptyState icon={CreditCard} title={t('workspaceApp.app.noPayments')} />
            ) : (
              <ul className="divide-y">
                {u.payments.map((p, i) => (
                  <li key={i} className="flex items-center gap-3 px-4 py-3 text-[13px]">
                    <div className="min-w-0 flex-1">
                      <p className="font-medium">{p.planName}</p>
                      <p className="text-xs text-muted-foreground">{new Date(p.paidAt ?? p.createdAt).toLocaleString('en-IN')}</p>
                    </div>
                    <Badge tone={p.status === 'paid' ? 'success' : p.status === 'failed' ? 'danger' : 'neutral'}>
                      {t(`workspaceApp.app.payment.${p.status}`, { defaultValue: p.status })}
                    </Badge>
                    <span className="w-24 text-right font-medium tabular-nums">
                      {p.currency && p.currency !== 'INR' ? `${p.currency} ` : '₹'}
                      {p.amountInr.toLocaleString('en-IN')}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        )}
      </div>

      <GrantDialog open={grantOpen} onOpenChange={setGrantOpen} user={u} loading={update.isPending} onGrant={(planId) => update.mutate({ access: { action: 'grant', planId } })} />
      <EditProfileDialog open={editOpen} onOpenChange={setEditOpen} user={u} loading={update.isPending} onSave={(body) => update.mutate(body)} />
      <ConfirmDialog
        open={confirm !== null && confirm !== 'delete'}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={confirm ? t(`workspaceApp.app.confirm.${confirm}.title`, { name }) : ''}
        body={confirm ? t(`workspaceApp.app.confirm.${confirm}.body`, { name }) : ''}
        confirmLabel={confirm ? t(`workspaceApp.app.confirm.${confirm}.action`) : ''}
        tone={confirm === 'suspend' || confirm === 'revoke' ? 'danger' : 'primary'}
        loading={update.isPending}
        onConfirm={() => {
          if (confirm === 'revoke') update.mutate({ access: { action: 'revoke' } });
          else if (confirm === 'admin' || confirm === 'user') update.mutate({ role: confirm });
          else if (confirm === 'suspend') update.mutate({ status: 'suspended' });
          else if (confirm === 'activate') update.mutate({ status: 'active' });
        }}
      />
      <ConfirmDialog
        open={confirm === 'delete'}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t('workspaceApp.app.confirm.delete.title', { name })}
        body={t('workspaceApp.app.confirm.delete.body', { name })}
        confirmLabel={t('workspaceApp.app.confirm.delete.action')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
    </PageContainer>
  );
}

function TabLabel({ text, n }: { text: string; n: number }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {text}
      {n ? <span className="rounded-full bg-muted px-1.5 text-[11px] tabular-nums text-muted-foreground">{n}</span> : null}
    </span>
  );
}

function Stat({ icon: Icon, label, value }: { icon: typeof Store; label: string; value: number | string }) {
  return (
    <div className="flex items-center gap-3 border-r px-5 py-3 last:border-r-0 [&:nth-child(2)]:border-r-0 sm:[&:nth-child(2)]:border-r">
      <Icon className="size-4 text-muted-foreground" aria-hidden />
      <div className="min-w-0">
        <p className="truncate text-xs text-muted-foreground">{label}</p>
        <p className="text-sm font-semibold tabular-nums">{value}</p>
      </div>
    </div>
  );
}

function BusinessList({ user: u, code }: { user: AppUserDetail; code: string }) {
  const { t } = useTranslation();
  if (u.businessList.length === 0) return <EmptyState icon={Store} title={t('workspaceApp.app.noBusinessesForUser')} />;
  return (
    <Card className="overflow-hidden">
      <ul className="divide-y">
        {u.businessList.map((b) => (
          <li key={b.id}>
            <Link to={businessPath(code, b.id)} className="group flex flex-col gap-2 px-4 py-3 hover:bg-muted/50 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <p className="truncate text-[13px] font-medium group-hover:text-primary">{b.name}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {[b.category, b.city && `${b.city} (${b.pincode})`].filter(Boolean).join(' · ')}
                </p>
              </div>
              <div className="flex flex-wrap gap-1.5">
                <VerificationBadge value={b.verification} />
                <ListingBadge value={b.listing} />
              </div>
            </Link>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function CardList({ cards, code, canBiz }: { cards: SavedCard[]; code: string; canBiz: boolean }) {
  const { t } = useTranslation();
  if (cards.length === 0) return <EmptyState icon={CreditCard} title={t('workspaceApp.app.noCards')} />;
  return (
    <div className="grid gap-3 md:grid-cols-2">
      {cards.map((c) => (
        <Card key={c.id} id={`card-${c.id}`} className="p-4">
          <div className="flex items-start gap-3">
            <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
              <CreditCard className="size-5" aria-hidden />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-semibold">{c.personName || c.company || t('workspaceApp.app.unnamedCard')}</p>
              <p className="truncate text-xs text-muted-foreground">{[c.designation, c.personName ? c.company : ''].filter(Boolean).join(' · ') || '—'}</p>
              <div className="mt-2 space-y-0.5 text-xs text-muted-foreground">
                {c.phones.map((p) => (
                  <p key={p} className="flex items-center gap-1.5 tabular-nums">
                    <Phone className="size-3" aria-hidden /> {formatPhone(p)}
                  </p>
                ))}
                {c.emails.map((e) => (
                  <p key={e} className="flex items-center gap-1.5">
                    <Mail className="size-3" aria-hidden /> {e}
                  </p>
                ))}
                {c.address ? (
                  <p className="flex items-start gap-1.5">
                    <MapPin className="mt-0.5 size-3 shrink-0" aria-hidden /> <span className="line-clamp-2">{c.address}</span>
                  </p>
                ) : null}
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                {c.gstin ? <Badge tone="warning">GST {c.gstin}</Badge> : null}
                <Badge tone="neutral">{t(`workspaceApp.app.cardType.${c.type}`, { defaultValue: c.type })}</Badge>
                {c.business ? (
                  canBiz ? (
                    <Link to={businessPath(code, c.business.id)} className="text-xs font-medium text-primary hover:underline">
                      {t('workspaceApp.app.linkedBusiness', { name: c.business.name })}
                    </Link>
                  ) : (
                    <span className="text-xs text-muted-foreground">{t('workspaceApp.app.linkedBusiness', { name: c.business.name })}</span>
                  )
                ) : null}
                <span className="ml-auto text-[11px] text-muted-foreground">{t('workspaceApp.app.savedOn', { date: formatDate(c.createdAt) })}</span>
              </div>
            </div>
          </div>
        </Card>
      ))}
    </div>
  );
}

function GrantDialog({
  open,
  onOpenChange,
  user,
  loading,
  onGrant
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  user: AppUserDetail;
  loading: boolean;
  onGrant: (plan: AppPlanId) => void;
}) {
  const { t } = useTranslation();
  const [plan, setPlan] = useState<AppPlanId>('12m');
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('workspaceApp.app.grantTitle', { name: user.name || formatPhone(user.phone) })}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('workspaceApp.app.grantBody')}</DialogDescription>
        {user.premium ? (
          <Alert tone="info" className="mt-3">
            {t('workspaceApp.app.grantCurrent', { plan: user.planName, date: user.expiresAt ? formatDate(user.expiresAt) : t('workspaceApp.app.neverExpires') })}
          </Alert>
        ) : null}
        <div role="radiogroup" className="mt-4 grid grid-cols-2 gap-2">
          {PLANS.map((p) => (
            <button
              key={p}
              type="button"
              role="radio"
              aria-checked={plan === p}
              onClick={() => setPlan(p)}
              className={cn(
                'rounded-lg border px-3 py-2.5 text-left transition-colors',
                plan === p ? 'border-primary bg-primary-soft ring-2 ring-primary/20' : 'hover:bg-muted'
              )}
            >
              <p className="text-[13px] font-semibold">{t(`workspaceApp.app.plans.${p}`)}</p>
              <p className="text-xs text-muted-foreground">
                {p === 'lifetime'
                  ? t('workspaceApp.app.neverExpires')
                  : t('workspaceApp.app.untilDate', { date: formatDate(addMonths(new Date(), { '3m': 3, '6m': 6, '12m': 12 }[p]).toISOString()) })}
              </p>
            </button>
          ))}
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button loading={loading} onClick={() => onGrant(plan)}>
            <Gift /> {t('workspaceApp.app.grantConfirm')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function addMonths(d: Date, n: number) {
  const x = new Date(d);
  x.setMonth(x.getMonth() + n);
  return x;
}

function EditProfileDialog({
  open,
  onOpenChange,
  user,
  loading,
  onSave
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  user: AppUserDetail;
  loading: boolean;
  onSave: (body: AppUserPatch) => void;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState({ name: user.name, email: user.email, city: user.city });
  const [error, setError] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!form.name.trim()) {
      setError(t('common.required'));
      return;
    }
    setError(null);
    const body: AppUserPatch = {};
    if (form.name.trim() !== user.name) body.name = form.name.trim();
    if (form.email.trim() !== user.email) body.email = form.email.trim();
    if (form.city.trim() !== user.city) body.city = form.city.trim();
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
        if (o) setForm({ name: user.name, email: user.email, city: user.city });
        onOpenChange(o);
      }}
    >
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('workspaceApp.app.editProfile')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('workspaceApp.app.editProfileBody')}</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <Field label={t('workspaceApp.app.name')} error={error ?? undefined}>
            <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} maxLength={100} autoFocus />
          </Field>
          <Field label={t('workspaceApp.app.email')}>
            <Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} maxLength={255} />
          </Field>
          <Field label={t('workspaceApp.app.city')}>
            <Input value={form.city} onChange={(e) => setForm({ ...form, city: e.target.value })} maxLength={100} />
          </Field>
          <p className="text-xs text-muted-foreground">{t('workspaceApp.app.phoneLocked')}</p>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={loading}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DetailSkeleton() {
  return (
    <PageContainer wide>
      <Skeleton className="h-4 w-40" />
      <Card className="mt-3 p-5">
        <div className="flex gap-4">
          <Skeleton className="size-14 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-6 w-56" />
            <Skeleton className="h-4 w-80" />
          </div>
        </div>
      </Card>
      <Skeleton className="mt-5 h-9 w-96" />
      <Skeleton className="mt-4 h-48 w-full" />
    </PageContainer>
  );
}
