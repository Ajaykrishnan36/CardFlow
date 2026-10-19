import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { businessApi, type BusinessProfile, type PostalAddress } from '@crm/api/endpoints';
import { isApiError, IS_NATIVE, sessionToken } from '@crm/api/client';
import { Copy, ImagePlus } from 'lucide-react';
import { AddressFields, addressFilled, emptyAddress } from './address-fields';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Switch, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { PageContainer, PageHeader } from '@crm/components/page';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';

const fields: Array<{ key: string; label: string; wide?: boolean; type?: string; long?: boolean }> = [
  { key: 'industry', label: 'Industry' },
  { key: 'phone', label: 'Phone', type: 'tel' },
  { key: 'email', label: 'Email', type: 'email' },
  { key: 'website', label: 'Website' },
  { key: 'whatsapp', label: 'WhatsApp', type: 'tel' },
  { key: 'gstin', label: 'GSTIN' },
  { key: 'pan', label: 'PAN' },
  { key: 'about', label: 'About the business', wide: true, long: true }
];

const addressKeys = ['addressLine', 'landmark', 'area', 'city', 'district', 'state', 'postalCode', 'country'] as const;

function addressOf(p: Record<string, string>): PostalAddress {
  const a = emptyAddress();
  for (const k of addressKeys) a[k] = p[k] ?? (k === 'country' ? 'India' : '');
  if (!p.addressLine && p.address) a.addressLine = p.address; // older profiles kept one free-text line
  a.latitude = p.latitude ? Number(p.latitude) : null;
  a.longitude = p.longitude ? Number(p.longitude) : null;
  return a;
}

function oneLine(a: PostalAddress): string {
  return [a.addressLine, a.area, a.landmark ? `Near ${a.landmark}` : '', a.city, a.state, a.postalCode, a.country].filter(Boolean).join(', ');
}

/** /crm/w/:ws/settings/business — the business's own details (name, money, time zone, contact and tax details). */
export function BusinessProfilePage() {
  const { code, context } = useWorkspace();
  const qc = useQueryClient();
  useDocumentTitle(`Business profile · ${context.workspace.name}`);
  const q = useQuery({ queryKey: ['workspace', code, 'business'], queryFn: () => businessApi.profile(code) });
  const [form, setForm] = useState<{ name: string; currency: string; timezone: string; profile: Record<string, string> } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [alert, setAlert] = useState<string | null>(null);

  const fill = (b: BusinessProfile) => setForm({ name: b.name, currency: b.currency, timezone: b.timezone, profile: { ...b.profile } });
  useEffect(() => {
    if (q.data && !form) fill(q.data);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data]);

  const save = useMutation({
    mutationFn: () => {
      const profile: Record<string, string | null> = {};
      for (const f of fields) profile[f.key] = form!.profile[f.key]?.trim() || null;
      profile.packs = form!.profile.packs?.trim() || null;
      const a = addressOf(form!.profile);
      for (const k of addressKeys) profile[k] = a[k]?.trim() || null;
      profile.latitude = a.latitude != null ? String(a.latitude) : null;
      profile.longitude = a.longitude != null ? String(a.longitude) : null;
      profile.address = addressFilled(a) ? oneLine(a) : null;
      profile.customDomain = form!.profile.customDomain?.trim() || null;
      return businessApi.update(code, { name: form!.name.trim(), currency: form!.currency.trim().toUpperCase(), timezone: form!.timezone.trim(), profile });
    },
    onSuccess: async (b) => {
      toast.success('Business profile saved');
      setErrors({});
      setAlert(null);
      fill(b);
      qc.setQueryData(['workspace', code, 'business'], b);
      await qc.invalidateQueries({ queryKey: ['workspace', code, 'context'] });
      await qc.invalidateQueries({ queryKey: ['businesses'] });
    },
    onError: (e) => {
      if (isApiError(e)) {
        setErrors(e.fieldErrors);
        setAlert(Object.keys(e.fieldErrors).length ? null : e.message);
      } else setAlert('Something went wrong. Please try again.');
    }
  });

  const canEdit = Boolean(q.data?.canEdit);
  return (
    <PageContainer>
      <PageHeader
        title="Business profile"
        description="Details of this business. They appear on your quotes and invoices and help your team recognise the business."
        badges={q.data ? <Badge tone="neutral">{q.data.members} {q.data.members === 1 ? 'member' : 'members'}</Badge> : null}
      />
      {q.isError ? (
        <Card>
          <ErrorState title="Couldn't load the business profile" message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        </Card>
      ) : !form ? (
        <Card className="space-y-3 p-5">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-2/3" />
        </Card>
      ) : (
        <form
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
          className="space-y-5"
        >
          {!canEdit ? <Alert tone="info">Only people who manage this business can change these details.</Alert> : null}
          {alert ? <Alert tone="danger">{alert}</Alert> : null}
          <Card>
            <CardHeader title="Basics" description="The name your team and customers see, and how money and dates are shown." />
            <div className="grid gap-4 p-5 sm:grid-cols-2">
              <Field label="Business name" error={errors.name} className="sm:col-span-2">
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} disabled={!canEdit} maxLength={120} />
              </Field>
              <Field label="Currency" error={errors.currency} hint="3-letter code, e.g. INR, USD, AED.">
                <Input value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value })} disabled={!canEdit} maxLength={3} />
              </Field>
              <Field label="Time zone" error={errors.timezone} hint="e.g. Asia/Kolkata">
                <Input value={form.timezone} onChange={(e) => setForm({ ...form, timezone: e.target.value })} disabled={!canEdit} />
              </Field>
            </div>
          </Card>
          <Card>
            <CardHeader title="Contact and tax details" />
            <div className="grid gap-4 p-5 sm:grid-cols-2">
              {fields.map((f) => (
                <Field key={f.key} label={f.label} error={errors[`profile.${f.key}`]} className={f.wide ? 'sm:col-span-2' : undefined}>
                  {f.long ? (
                    <Textarea
                      rows={3}
                      value={form.profile[f.key] ?? ''}
                      onChange={(e) => setForm({ ...form, profile: { ...form.profile, [f.key]: e.target.value } })}
                      disabled={!canEdit}
                    />
                  ) : (
                    <Input
                      type={f.type ?? 'text'}
                      value={form.profile[f.key] ?? ''}
                      onChange={(e) => setForm({ ...form, profile: { ...form.profile, [f.key]: e.target.value } })}
                      disabled={!canEdit}
                    />
                  )}
                </Field>
              ))}
            </div>
          </Card>
          <Card>
            <CardHeader title="Address" description="Enter the pincode or use the current location; the rest fills in." />
            <div className="p-5">
              <AddressFields
                value={addressOf(form.profile)}
                disabled={!canEdit}
                errors={errors}
                prefix="profile."
                onChange={(a) =>
                  setForm({
                    ...form,
                    profile: {
                      ...form.profile,
                      ...Object.fromEntries(addressKeys.map((k) => [k, a[k] ?? ''])),
                      latitude: a.latitude != null ? String(a.latitude) : '',
                      longitude: a.longitude != null ? String(a.longitude) : ''
                    }
                  })
                }
              />
            </div>
          </Card>
          <LoginAddressCard
            loginPath={q.data?.loginPath ?? `/${code}/login`}
            domain={form.profile.customDomain ?? ''}
            onDomain={(d) => setForm({ ...form, profile: { ...form.profile, customDomain: d } })}
            error={errors['profile.customDomain']}
            disabled={!canEdit}
          />
          {q.data ? <CardImagesCard code={code} sides={q.data.cardImages ?? { front: false, back: false }} canEdit={canEdit} /> : null}
          <Card>
            <CardHeader title="Industry packs" description="Extra menus and records for one trade. Switch on what your business needs; nothing is lost when a pack is switched off." />
            <div className="p-5">
              {[
                { key: 'dealership', trade: /automotive/i, industry: 'Automotive', label: 'Dealership (bikes and cars)', what: 'Vehicle stock by VIN, bookings, test rides, exchange vehicles, loan applications, registrations and the service schedule.' },
                { key: 'insurance', trade: /insurance/i, industry: 'Insurance', label: 'Insurance agency', what: 'Policies, the premium schedule, renewals, claims and commission expected against received.' },
                { key: 'salon', trade: /beauty|salon/i, industry: 'Beauty & salon', label: 'Salon and makeup kits', what: 'Product stock with batch and expiry, kits and their bookings, client packages.' },
                { key: 'real_estate', trade: /real estate/i, industry: 'Real estate', label: 'Real estate', what: 'Units and listings, holds and bookings, site visits.' }
              ].map((pack) => {
                const byTrade = pack.trade.test(form.profile.industry ?? '');
                const packs = (form.profile.packs ?? '').split(',').map((x) => x.trim()).filter(Boolean);
                return (
                  <Switch
                    key={pack.key}
                    className="py-1.5"
                    checked={byTrade || packs.includes(pack.key)}
                    disabled={!canEdit || byTrade}
                    onCheckedChange={(c) =>
                      setForm({ ...form, profile: { ...form.profile, packs: (c ? [...packs, pack.key] : packs.filter((x) => x !== pack.key)).join(',') } })
                    }
                    label={pack.label}
                    description={byTrade ? `On because your industry is ${pack.industry}. ${pack.what}` : pack.what}
                  />
                );
              })}
            </div>
          </Card>
          {canEdit ? (
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => q.data && fill(q.data)} disabled={save.isPending}>
                Reset
              </Button>
              <Button type="submit" loading={save.isPending}>
                Save changes
              </Button>
            </div>
          ) : null}
        </form>
      )}
    </PageContainer>
  );
}

/** The business's own sign-in address, and its own domain (D-155). */
function LoginAddressCard({ loginPath, domain, onDomain, error, disabled }: { loginPath: string; domain: string; onDomain: (d: string) => void; error?: string; disabled: boolean }) {
  const url = `${window.location.origin}${loginPath}`;
  return (
    <Card>
      <CardHeader title="Login address" description="Your team signs in to this business here. Share this link, or use your own domain." />
      <div className="space-y-4 p-5">
        <div className="flex items-center gap-2 rounded-lg border bg-muted/40 p-2 pl-3">
          <code className="min-w-0 flex-1 truncate text-sm">{url}</code>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => {
              void navigator.clipboard?.writeText(url);
              toast.success('Login address copied');
            }}
          >
            <Copy /> Copy
          </Button>
        </div>
        <Field
          label="Your own domain"
          error={error}
          hint="For example crm.yourbusiness.com. Point it (CNAME) at this site and ask us to connect it; then opening it shows your business's sign-in."
        >
          <Input value={domain} onChange={(e) => onDomain(e.target.value)} placeholder="crm.yourbusiness.com" disabled={disabled} />
        </Field>
      </div>
    </Card>
  );
}

/** The image behind an API address, fetched with the session (works in the app shell too). */
function useApiImage(url: string, version: number, enabled: boolean): string {
  const [src, setSrc] = useState('');
  useEffect(() => {
    if (!enabled) {
      setSrc('');
      return;
    }
    let live = true;
    let made = '';
    const headers: Record<string, string> = IS_NATIVE ? { 'X-Session-Transport': 'bearer', Authorization: `Bearer ${sessionToken()}` } : {};
    fetch(url, { credentials: IS_NATIVE ? 'omit' : 'same-origin', headers })
      .then((r) => (r.ok ? r.blob() : null))
      .then((b) => {
        if (b && live) {
          made = URL.createObjectURL(b);
          setSrc(made);
        }
      })
      .catch(() => undefined);
    return () => {
      live = false;
      if (made) URL.revokeObjectURL(made);
    };
  }, [url, version, enabled]);
  return src;
}

function CardImagesCard({ code, sides, canEdit }: { code: string; sides: { front: boolean; back: boolean }; canEdit: boolean }) {
  const qc = useQueryClient();
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState<string | null>(null);
  const run = async (side: 'front' | 'back', job: () => Promise<BusinessProfile>) => {
    setBusy(side);
    try {
      const b = await job();
      qc.setQueryData(['workspace', code, 'business'], b);
      setVersion((v) => v + 1);
    } catch (e) {
      toast.error(isApiError(e) ? e.fieldErrors.file ?? e.message : 'Upload failed.');
    } finally {
      setBusy(null);
    }
  };
  return (
    <Card>
      <CardHeader title="Business card" description="Photos of the front and back of your card." />
      <div className="grid gap-4 p-5 sm:grid-cols-2">
        {(['front', 'back'] as const).map((side) => (
          <CardSide
            key={side}
            label={side === 'front' ? 'Front' : 'Back'}
            src={useApiImage(businessApi.cardImageUrl(code, side), version, sides[side])}
            has={sides[side]}
            busy={busy === side}
            canEdit={canEdit}
            onPick={(f) => void run(side, () => businessApi.uploadCardImage(code, side, f))}
            onRemove={() => void run(side, () => businessApi.removeCardImage(code, side))}
          />
        ))}
      </div>
    </Card>
  );
}

function CardSide({ label, src, has, busy, canEdit, onPick, onRemove }: { label: string; src: string; has: boolean; busy: boolean; canEdit: boolean; onPick: (f: File) => void; onRemove: () => void }) {
  return (
    <div>
      <p className="mb-1.5 text-[13px] font-medium">{label}</p>
      <div className="flex h-36 items-center justify-center overflow-hidden rounded-lg border bg-muted/30">
        {has && src ? <img src={src} alt={`Business card ${label.toLowerCase()}`} className="h-full w-full object-contain" /> : <span className="text-sm text-muted-foreground">{has ? 'Loading…' : 'No photo yet'}</span>}
      </div>
      {canEdit ? (
        <div className="mt-2 flex gap-2">
          <label className={`inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md border px-3 text-sm hover:bg-muted ${busy ? 'pointer-events-none opacity-60' : ''}`}>
            <ImagePlus className="size-4" aria-hidden /> {has ? 'Replace' : 'Upload'}
            <input
              type="file"
              accept="image/jpeg,image/png,image/webp"
              className="sr-only"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) onPick(f);
                e.target.value = '';
              }}
            />
          </label>
          {has ? (
            <Button type="button" size="sm" variant="ghost" onClick={onRemove} disabled={busy}>
              Remove
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
