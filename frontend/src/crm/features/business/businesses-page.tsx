import { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowRight, Building2, LogOut, Plus, Users } from 'lucide-react';
import { businessApi } from '@crm/api/endpoints';
import { api } from '@crm/api/client';
import { isApiError } from '@crm/api/client';
import { isOwnerSession, meKey, useMe, useSignOut, workspaceHomePath } from '@crm/auth/session';
import { Button } from '@crm/components/ui/button';
import { Badge, Card, Alert } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';

export const businessesKey = ['businesses'] as const;

const industries = ['Retail', 'Wholesale & distribution', 'Manufacturing', 'Services', 'Real estate', 'Construction', 'Education', 'Healthcare', 'Hospitality', 'Technology', 'Finance', 'Other'];

/**
 * /crm/businesses — every business the person belongs to, and "create a business".
 * A business is one CRM: its own leads, contacts, accounts, team, roles and reports.
 * Nothing is shared between two businesses, even for the same person.
 */
export function BusinessesHomePage() {
  const { data: me } = useMe();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const [params, setParams] = useSearchParams();
  const q = useQuery({ queryKey: businessesKey, queryFn: businessApi.list, staleTime: 30_000 });
  const [creating, setCreating] = useState(false);
  useDocumentTitle('Your businesses');

  // First sign-in with no business: go straight to the form.
  useEffect(() => {
    if (params.get('new') && q.data?.canCreate) setCreating(true);
    else if (q.data && q.data.data.length === 0 && q.data.canCreate) setCreating(true);
  }, [params, q.data]);

  if (!me) return null;
  const firstName = me.identity.displayName.split(' ')[0] ?? '';
  const list = q.data?.data ?? [];

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8 sm:px-6 lg:py-12">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight sm:text-2xl">Your businesses</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {firstName && firstName !== 'New' ? `Hi ${firstName}. ` : ''}Each business has its own CRM. Records never mix between them.
          </p>
        </div>
        <div className="flex gap-2">
          {isOwnerSession(me) ? (
            <Button asChild variant="outline" size="sm">
              <Link to="/crm/owner/dashboard">Owner console</Link>
            </Button>
          ) : null}
          {q.data?.canCreate ? (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus /> New business
            </Button>
          ) : null}
        </div>
      </div>

      {q.isError ? (
        <Card>
          <ErrorState title="Couldn't load your businesses" message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        </Card>
      ) : q.isPending ? (
        <div className="space-y-3">
          {[0, 1].map((i) => (
            <Card key={i} className="p-4">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="mt-2 h-3 w-24" />
            </Card>
          ))}
        </div>
      ) : list.length === 0 ? (
        <Card className="p-8 text-center">
          <span className="mx-auto grid size-11 place-items-center rounded-full bg-primary-soft text-primary">
            <Building2 className="size-5" aria-hidden />
          </span>
          <h2 className="mt-3 text-base font-semibold">Create your first business</h2>
          <p className="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
            It takes a minute. You become its Super Admin and can invite your team afterwards.
          </p>
          {q.data?.canCreate ? (
            <Button className="mt-4" onClick={() => setCreating(true)}>
              <Plus /> Create a business
            </Button>
          ) : (
            <p className="mt-4 text-sm text-muted-foreground">Creating a business isn’t open right now. Ask your administrator for an invitation.</p>
          )}
        </Card>
      ) : (
        <ul className="space-y-3">
          {list.map((b) => (
            <li key={b.id}>
              <Link
                to={workspaceHomePath(b.code)}
                className="group block rounded-xl border bg-card p-4 transition-colors hover:border-primary/50 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <div className="flex items-center gap-3">
                  <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary-soft text-sm font-semibold text-primary">
                    {b.name.slice(0, 2).toUpperCase()}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[15px] font-semibold text-foreground">{b.isPlatform ? 'Platform CRM' : b.name}</p>
                    <p className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <Badge tone="primary">{b.roleName || 'Member'}</Badge>
                      <span className="flex items-center gap-1">
                        <Users className="size-3.5" aria-hidden /> {b.members} {b.members === 1 ? 'person' : 'people'}
                      </span>
                      {b.profile?.city ? <span>{b.profile.city}</span> : null}
                      <span>{b.currency}</span>
                    </p>
                  </div>
                  <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary" aria-hidden />
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {q.data && !q.data.canCreate && list.length > 0 ? (
        <p className="mt-4 text-xs text-muted-foreground">
          You’ve created {q.data.created} of {q.data.limit} businesses this account can create.
        </p>
      ) : null}

      <div className="mt-8 flex justify-center gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to="/crm/me">Profile &amp; security</Link>
        </Button>
        <Button variant="ghost" size="sm" onClick={() => void signOut()}>
          <LogOut /> Sign out
        </Button>
      </div>

      <CreateBusinessDialog
        open={creating}
        onOpenChange={(open) => {
          setCreating(open);
          if (!open && params.get('new')) setParams({}, { replace: true });
        }}
        onCreated={(code) => navigate(workspaceHomePath(code))}
      />
    </div>
  );
}

function CreateBusinessDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: (code: string) => void }) {
  const qc = useQueryClient();
  const { data: me } = useMe();
  // Someone who just signed up by phone has no name yet: ask for it here.
  const needsName = !me?.identity.displayName || me.identity.displayName === 'New user';
  const [yourName, setYourName] = useState('');
  const [form, setForm] = useState({ name: '', industry: '', phone: '', email: '', city: '' });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [alert, setAlert] = useState<string | null>(null);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const create = useMutation({
    mutationFn: async () => {
      if (needsName && yourName.trim()) await api('/me', { method: 'PATCH', body: { displayName: yourName.trim() } });
      return businessApi.create({ name: form.name.trim(), industry: form.industry, phone: form.phone.trim(), email: form.email.trim(), city: form.city.trim() });
    },
    onSuccess: async (res) => {
      toast.success(`${res.business.name} is ready`);
      await Promise.all([qc.invalidateQueries({ queryKey: businessesKey }), qc.invalidateQueries({ queryKey: meKey })]);
      qc.removeQueries({ queryKey: ['capabilities'] });
      onOpenChange(false);
      onCreated(res.business.code);
    },
    onError: (e) => {
      if (isApiError(e)) {
        setErrors(e.fieldErrors);
        setAlert(Object.keys(e.fieldErrors).length ? null : e.message);
      } else setAlert('Something went wrong. Please try again.');
    }
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">Create a business</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">
          You get a full CRM for it: leads, contacts, accounts, deals, tasks, cases, finance and reports.
        </DialogDescription>
        <form
          className="mt-4 space-y-4"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            setErrors({});
            setAlert(null);
            const fe: Record<string, string> = {};
            if (needsName && yourName.trim().length < 2) fe.displayName = 'Enter your name.';
            if (form.name.trim().length < 2) fe.name = 'Enter your business name.';
            if (Object.keys(fe).length) {
              setErrors(fe);
              return;
            }
            create.mutate();
          }}
        >
          {alert ? <Alert tone="danger">{alert}</Alert> : null}
          {needsName ? (
            <Field label="Your name" error={errors.displayName}>
              <Input value={yourName} onChange={(e) => setYourName(e.target.value)} autoFocus placeholder="Ajay Krishnan" maxLength={120} autoComplete="name" />
            </Field>
          ) : null}
          <Field label="Business name" error={errors.name}>
            <Input value={form.name} onChange={set('name')} autoFocus={!needsName} placeholder="Sri Lakshmi Traders" maxLength={120} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Industry" error={errors.industry}>
              <Select value={form.industry} onChange={set('industry')} placeholder="Choose…" options={industries.map((i) => ({ value: i, label: i }))} />
            </Field>
            <Field label="City" error={errors.city}>
              <Input value={form.city} onChange={set('city')} placeholder="Coimbatore" />
            </Field>
            <Field label="Business phone" error={errors.phone}>
              <Input value={form.phone} onChange={set('phone')} type="tel" inputMode="tel" placeholder="Optional" />
            </Field>
            <Field label="Business email" error={errors.email}>
              <Input value={form.email} onChange={set('email')} type="email" inputMode="email" placeholder="Optional" />
            </Field>
          </div>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={create.isPending}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending}>
              Create business
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
