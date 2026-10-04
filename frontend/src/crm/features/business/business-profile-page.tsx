import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { businessApi, type BusinessProfile } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Textarea } from '@crm/components/ui/form-controls';
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
  { key: 'gstin', label: 'GSTIN' },
  { key: 'pan', label: 'PAN' },
  { key: 'address', label: 'Address', wide: true },
  { key: 'city', label: 'City' },
  { key: 'state', label: 'State' },
  { key: 'postalCode', label: 'Postal code' },
  { key: 'country', label: 'Country' },
  { key: 'about', label: 'About the business', wide: true, long: true }
];

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
