import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowRight, Building2, Check, Copy, ImagePlus, LogOut, Plus, Users } from 'lucide-react';
import { businessApi, onboardingApi, type PostalAddress } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { isOwnerSession, meKey, useMe, useSignOut, workspaceHomePath } from '@crm/auth/session';
import { Button } from '@crm/components/ui/button';
import { Badge, Card, Alert } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select } from '@crm/components/ui/form-controls';
import { AddressFields, addressFilled, emptyAddress } from './address-fields';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';

export const businessesKey = ['businesses'] as const;

const industries = ['Retail', 'Wholesale & distribution', 'Automotive', 'Manufacturing', 'Agriculture', 'Transport & logistics', 'Services', 'Real estate', 'Construction', 'Education', 'Healthcare', 'Hospitality', 'Technology', 'Finance', 'Insurance', 'Beauty & salon', 'Other'];

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

type Side = 'front' | 'back';

/**
 * Create a business (D-155), in sections split by a line: you (name, date of birth,
 * personal email, the mobile you signed in with, the business / brand name), your address,
 * the business (industry, phone and WhatsApp — same as yours by default — email, website,
 * GSTIN) and its address, and the business card photos. Your own details are remembered
 * and filled in again for your next business. At the end: the business's own login address.
 */
function CreateBusinessDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: (code: string) => void }) {
  const qc = useQueryClient();
  const details = useQuery({ queryKey: ['me', 'details'], queryFn: onboardingApi.details, enabled: open, staleTime: 60_000 });
  const [you, setYou] = useState({ displayName: '', dateOfBirth: '', personalEmail: '' });
  const [personal, setPersonal] = useState<PostalAddress>(emptyAddress());
  const [biz, setBiz] = useState({ name: '', industry: '', phone: '', whatsapp: '', email: '', website: '', gstin: '' });
  const [samePhone, setSamePhone] = useState(true);
  const [sameWhatsapp, setSameWhatsapp] = useState(true);
  const [sameAddress, setSameAddress] = useState(false);
  const [address, setAddress] = useState<PostalAddress>(emptyAddress());
  const [cards, setCards] = useState<Record<Side, File | null>>({ front: null, back: null });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [alert, setAlert] = useState<string | null>(null);
  const [done, setDone] = useState<{ code: string; name: string } | null>(null);
  const myPhone = details.data?.phone ?? '';

  // Fill in what we already know about the person, once per opening.
  const filled = useRef(false);
  useEffect(() => {
    if (!open) {
      filled.current = false;
      return;
    }
    const d = details.data;
    if (!d || filled.current) return;
    filled.current = true;
    setYou({ displayName: d.displayName, dateOfBirth: d.dateOfBirth, personalEmail: d.personalEmail });
    if (addressFilled(d.address)) setPersonal({ ...emptyAddress(), ...d.address, country: d.address.country || 'India' });
  }, [open, details.data]);

  const create = useMutation({
    mutationFn: async () => {
      const phone = samePhone ? myPhone : biz.phone.trim();
      const res = await businessApi.create({
        name: biz.name.trim(),
        industry: biz.industry,
        phone,
        whatsapp: sameWhatsapp ? phone : biz.whatsapp.trim(),
        email: biz.email.trim(),
        website: biz.website.trim(),
        gstin: biz.gstin.trim(),
        displayName: you.displayName.trim(),
        dateOfBirth: you.dateOfBirth,
        personalEmail: you.personalEmail.trim(),
        personalAddress: addressFilled(personal) ? personal : undefined,
        address: sameAddress ? (addressFilled(personal) ? personal : undefined) : addressFilled(address) ? address : undefined
      });
      // The card photos are optional: a failed upload never undoes the business.
      for (const side of ['front', 'back'] as Side[]) {
        const f = cards[side];
        if (f) await businessApi.uploadCardImage(res.business.code, side, f).catch(() => toast.error(`The ${side} of the card wasn’t saved. Add it later in Business profile.`));
      }
      return res;
    },
    onSuccess: async (res) => {
      await Promise.all([qc.invalidateQueries({ queryKey: businessesKey }), qc.invalidateQueries({ queryKey: meKey }), qc.invalidateQueries({ queryKey: ['me', 'details'] })]);
      qc.removeQueries({ queryKey: ['capabilities'] });
      setDone({ code: res.business.code, name: res.business.name });
    },
    onError: (e) => {
      if (isApiError(e)) {
        setErrors(e.fieldErrors);
        setAlert(Object.keys(e.fieldErrors).length ? 'Please fix the highlighted fields.' : e.message);
      } else setAlert('Something went wrong. Please try again.');
    }
  });

  const close = (o: boolean) => {
    if (!o && done) {
      const code = done.code;
      setDone(null);
      onOpenChange(false);
      onCreated(code);
      return;
    }
    onOpenChange(o);
  };

  const pickCard = (side: Side) => (e: { target: HTMLInputElement }) => {
    const f = e.target.files?.[0] ?? null;
    if (f && f.size > 5 * 1024 * 1024) {
      setErrors((x) => ({ ...x, [`card.${side}`]: 'Use a photo up to 5 MB.' }));
      return;
    }
    setErrors((x) => ({ ...x, [`card.${side}`]: '' }));
    setCards((c) => ({ ...c, [side]: f }));
  };

  const previews = useMemo(() => ({ front: cards.front ? URL.createObjectURL(cards.front) : '', back: cards.back ? URL.createObjectURL(cards.back) : '' }), [cards]);
  useEffect(() => () => Object.values(previews).forEach((u) => u && URL.revokeObjectURL(u)), [previews]);
  const loginUrl = done ? `${window.location.origin}/${done.code}/login` : '';

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="top-[3vh] flex max-h-[94vh] max-w-2xl flex-col p-0 sm:top-[5vh] sm:max-h-[90vh]">
        {done ? (
          <div className="p-6">
            <span className="grid size-11 place-items-center rounded-full bg-success/15 text-success">
              <Check className="size-5" aria-hidden />
            </span>
            <DialogTitle className="mt-3 text-lg font-semibold">{done.name} is ready</DialogTitle>
            <DialogDescription className="mt-1 text-sm text-muted-foreground">
              This is your business’s own login address. Share it with your team, or point your own domain at it later from Business profile.
            </DialogDescription>
            <div className="mt-4 flex items-center gap-2 rounded-lg border bg-muted/40 p-2 pl-3">
              <code className="min-w-0 flex-1 truncate text-sm">{loginUrl}</code>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => {
                  void navigator.clipboard?.writeText(loginUrl);
                  toast.success('Login address copied');
                }}
              >
                <Copy /> Copy
              </Button>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">Business code: {done.code}</p>
            <div className="mt-6 flex justify-end">
              <Button onClick={() => close(false)}>
                Open {done.name} <ArrowRight />
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="border-b px-5 py-4 pr-12">
              <DialogTitle className="text-base font-semibold">Create a business</DialogTitle>
              <DialogDescription className="mt-1 text-sm text-muted-foreground">
                You get a full CRM for it: leads, contacts, accounts, deals, tasks, cases, finance and reports. Only the starred fields are needed now.
              </DialogDescription>
            </div>
            <form
              className="flex min-h-0 flex-1 flex-col"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                setErrors({});
                setAlert(null);
                const fe: Record<string, string> = {};
                if (you.displayName.trim().length < 2) fe.displayName = 'Enter your name.';
                if (biz.name.trim().length < 2) fe.name = 'Enter your business or brand name.';
                if (!samePhone && !biz.phone.trim()) fe.phone = 'Enter the business phone, or tick “same as my mobile”.';
                if (Object.keys(fe).length) {
                  setErrors(fe);
                  setAlert('Please fix the highlighted fields.');
                  return;
                }
                create.mutate();
              }}
            >
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
                {alert ? <Alert tone="danger">{alert}</Alert> : null}

                <Section title="About you">
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="Your name *" error={errors.displayName}>
                      <Input value={you.displayName} onChange={(e) => setYou({ ...you, displayName: e.target.value })} placeholder="Ajay Krishnan" maxLength={120} autoComplete="name" autoFocus />
                    </Field>
                    <Field label="Date of birth" error={errors.dateOfBirth}>
                      <Input type="date" value={you.dateOfBirth} onChange={(e) => setYou({ ...you, dateOfBirth: e.target.value })} max={new Date().toISOString().slice(0, 10)} autoComplete="bday" />
                    </Field>
                    <Field label="Personal email" error={errors.personalEmail}>
                      <Input type="email" inputMode="email" value={you.personalEmail} onChange={(e) => setYou({ ...you, personalEmail: e.target.value })} placeholder="you@gmail.com" autoComplete="email" />
                    </Field>
                    <Field label="Mobile number" hint="The number you signed in with.">
                      <Input value={myPhone} readOnly disabled placeholder="—" />
                    </Field>
                    <Field label="Business / brand name *" error={errors.name} className="sm:col-span-2">
                      <Input value={biz.name} onChange={(e) => setBiz({ ...biz, name: e.target.value })} placeholder="Sri Lakshmi Traders" maxLength={120} />
                    </Field>
                  </div>
                </Section>

                <hr className="border-border" />
                <Section title="Your address" hint="Enter the pincode or use your current location; we fill in the rest.">
                  <AddressFields value={personal} onChange={setPersonal} errors={errors} prefix="personal." />
                </Section>

                <hr className="border-border" />
                <Section title="Your business">
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="Industry" error={errors.industry}>
                      <Select value={biz.industry} onChange={(e) => setBiz({ ...biz, industry: e.target.value })} placeholder="Choose…" options={industries.map((i) => ({ value: i, label: i }))} />
                    </Field>
                    <Field label="Website" error={errors.website}>
                      <Input value={biz.website} onChange={(e) => setBiz({ ...biz, website: e.target.value })} placeholder="www.yourbusiness.com" inputMode="url" />
                    </Field>
                    <div>
                      <Field label="Business phone" error={errors.phone}>
                        <Input value={samePhone ? myPhone : biz.phone} onChange={(e) => setBiz({ ...biz, phone: e.target.value })} type="tel" inputMode="tel" disabled={samePhone} placeholder="+91 98765 43210" />
                      </Field>
                      <Checkbox className="mt-2" checked={samePhone} onCheckedChange={setSamePhone} label="Same as my mobile" />
                    </div>
                    <div>
                      <Field label="WhatsApp number" error={errors.whatsapp}>
                        <Input
                          value={sameWhatsapp ? (samePhone ? myPhone : biz.phone) : biz.whatsapp}
                          onChange={(e) => setBiz({ ...biz, whatsapp: e.target.value })}
                          type="tel"
                          inputMode="tel"
                          disabled={sameWhatsapp}
                          placeholder="+91 98765 43210"
                        />
                      </Field>
                      <Checkbox className="mt-2" checked={sameWhatsapp} onCheckedChange={setSameWhatsapp} label="Same as business phone" />
                    </div>
                    <Field label="Business email" error={errors.email}>
                      <Input value={biz.email} onChange={(e) => setBiz({ ...biz, email: e.target.value })} type="email" inputMode="email" placeholder="sales@yourbusiness.com" />
                    </Field>
                    <Field label="GSTIN" error={errors.gstin} hint="15 characters. Checked against the business’s state.">
                      <Input value={biz.gstin} onChange={(e) => setBiz({ ...biz, gstin: e.target.value.toUpperCase() })} placeholder="33ABCDE1234F1Z5" maxLength={15} className="font-mono" />
                    </Field>
                  </div>
                </Section>

                <Section title="Business address">
                  <Checkbox className="mb-3" checked={sameAddress} onCheckedChange={setSameAddress} label="Same as my address" />
                  {sameAddress ? null : <AddressFields value={address} onChange={setAddress} errors={errors} prefix="address." />}
                </Section>

                <hr className="border-border" />
                <Section title="Business card" hint="Optional. A photo of each side, up to 5 MB.">
                  <div className="grid gap-4 sm:grid-cols-2">
                    {(['front', 'back'] as Side[]).map((side) => (
                      <div key={side}>
                        <p className="mb-1.5 text-[13px] font-medium text-foreground">{side === 'front' ? 'Front' : 'Back'}</p>
                        <label className="flex h-28 cursor-pointer items-center justify-center overflow-hidden rounded-lg border border-dashed bg-muted/30 text-sm text-muted-foreground hover:border-primary/50">
                          {cards[side] ? (
                            <img src={previews[side]} alt={`Business card ${side}`} className="h-full w-full object-contain" />
                          ) : (
                            <span className="flex items-center gap-2">
                              <ImagePlus className="size-4" aria-hidden /> Add {side} photo
                            </span>
                          )}
                          <input type="file" accept="image/jpeg,image/png,image/webp" capture="environment" className="sr-only" onChange={pickCard(side)} />
                        </label>
                        {errors[`card.${side}`] ? <p className="mt-1 text-[13px] text-danger">{errors[`card.${side}`]}</p> : null}
                        {cards[side] ? (
                          <button type="button" className="mt-1 text-xs text-muted-foreground hover:text-danger" onClick={() => setCards((c) => ({ ...c, [side]: null }))}>
                            Remove
                          </button>
                        ) : null}
                      </div>
                    ))}
                  </div>
                </Section>
              </div>
              <div className="flex justify-end gap-2 border-t bg-muted/30 px-5 py-3">
                <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={create.isPending}>
                  Cancel
                </Button>
                <Button type="submit" loading={create.isPending}>
                  Create business
                </Button>
              </div>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      {hint ? <p className="mb-3 mt-0.5 text-xs text-muted-foreground">{hint}</p> : <div className="mb-3" />}
      {children}
    </section>
  );
}
