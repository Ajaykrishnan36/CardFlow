import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Copy, ExternalLink, Pencil, Plus, Trash2 } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { WebForm, WebFormList } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Switch, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { LookupCombobox } from '@crm/features/records/field-input';
import { errorText, hasCap, toolKeys, useTools } from './tool-utils';

const copy = (text: string, what: string) =>
  navigator.clipboard.writeText(text).then(
    () => toast.success(`${what} copied`),
    () => toast.error("Couldn't copy. Select the text and copy it by hand.")
  );

/** /crm/w/:ws/settings/web-forms — forms for the business's website that become leads. */
export function WebFormsPage() {
  const { code, context } = useWorkspace();
  useDocumentTitle('Web forms');
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'web-forms');
  const allowed = hasCap('metadata.manage')(context) || Boolean(context.canCustomize);
  const q = useQuery({ queryKey: key, queryFn: () => api.webForms(), enabled: allowed });
  const [editing, setEditing] = useState<WebForm | 'new' | null>(null);
  const [removing, setRemoving] = useState<WebForm | null>(null);
  const adopt = (next: WebFormList) => qc.setQueryData(key, next);
  const toggle = useMutation({ mutationFn: (f: WebForm) => api.saveWebForm(f.id, { active: !f.active }), onSuccess: adopt, onError: (e) => toast.error(errorText(e, 'Something went wrong.')) });
  const remove = useMutation({
    mutationFn: (f: WebForm) => api.deleteWebForm(f.id),
    onSuccess: (next) => {
      adopt(next);
      setRemoving(null);
      toast.success('Form deleted');
    }
  });
  if (!allowed) return <NoAccessPage />;
  return (
    <PageContainer>
      <PageHeader
        title="Web forms"
        description="Put a form on your website or share its link. Everything sent in becomes a lead for the person you choose — the same mobile number twice is added to the lead already there."
        actions={
          <Button onClick={() => setEditing('new')}>
            <Plus /> New form
          </Button>
        }
      />
      {q.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : q.isError ? (
        <Card>
          <ErrorState title="Couldn't load the forms" onRetry={() => void q.refetch()} />
        </Card>
      ) : q.data.data.length === 0 ? (
        <Card className="px-5 py-10 text-center">
          <p className="text-sm font-medium">No forms yet</p>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-muted-foreground">Make one for your website's "Contact us" or "Book a test ride" page. You get a link and a snippet to paste.</p>
          <Button className="mt-4" onClick={() => setEditing('new')}>
            <Plus /> New form
          </Button>
        </Card>
      ) : (
        <div className="space-y-4">
          {q.data.data.map((f) => (
            <Card key={f.id}>
              <CardHeader
                title={
                  <span className="flex flex-wrap items-center gap-2">
                    {f.name}
                    {f.active ? <Badge tone="success">Taking submissions</Badge> : <Badge tone="neutral">Off</Badge>}
                  </span>
                }
                description={`${f.submissions} ${f.submissions === 1 ? 'submission' : 'submissions'} · leads go to ${f.ownerName || 'someone who has left'}${f.consentText ? ' · asks for consent' : ''}`}
                actions={
                  <span className="flex items-center gap-1.5">
                    <Switch checked={f.active} onCheckedChange={() => toggle.mutate(f)} disabled={toggle.isPending} aria-label={`${f.name} is on`} />
                    <Button variant="subtle" size="icon-sm" onClick={() => setEditing(f)} aria-label={`Edit ${f.name}`}>
                      <Pencil />
                    </Button>
                    <Button variant="subtle" size="icon-sm" onClick={() => setRemoving(f)} aria-label={`Delete ${f.name}`}>
                      <Trash2 />
                    </Button>
                  </span>
                }
              />
              <div className="space-y-3 p-5">
                <Field label="Link to share">
                  <div className="flex gap-2">
                    <Input readOnly value={f.url} onFocus={(e) => e.target.select()} className="font-mono text-xs" />
                    <Button type="button" variant="outline" onClick={() => void copy(f.url, 'Link')}>
                      <Copy /> Copy
                    </Button>
                    <Button type="button" variant="outline" asChild>
                      <a href={f.url} target="_blank" rel="noreferrer">
                        <ExternalLink /> Open
                      </a>
                    </Button>
                  </div>
                </Field>
                <Field label="Paste this into your website" hint="Your web designer puts this where the form should appear.">
                  <div className="flex gap-2">
                    <Input readOnly value={f.embed} onFocus={(e) => e.target.select()} className="font-mono text-xs" />
                    <Button type="button" variant="outline" onClick={() => void copy(f.embed, 'Snippet')}>
                      <Copy /> Copy
                    </Button>
                  </div>
                </Field>
              </div>
            </Card>
          ))}
        </div>
      )}
      {editing ? <FormDialog form={editing === 'new' ? null : editing} offered={q.data?.fields ?? []} onClose={() => setEditing(null)} onSaved={adopt} /> : null}
      <ConfirmDialog
        open={Boolean(removing)}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Delete “${removing?.name ?? ''}”?`}
        body="Its link stops working at once. Leads it already brought in stay."
        confirmLabel="Delete form"
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </PageContainer>
  );
}

function FormDialog({ form, offered, onClose, onSaved }: { form: WebForm | null; offered: WebFormList['fields']; onClose: () => void; onSaved: (l: WebFormList) => void }) {
  const api = useTools();
  const [name, setName] = useState(form?.name ?? '');
  const [heading, setHeading] = useState(form?.heading ?? '');
  const [intro, setIntro] = useState(form?.intro ?? '');
  const [fields, setFields] = useState<string[]>(form?.fields ?? ['email', 'message']);
  const [owner, setOwner] = useState<string | null>(form?.ownerId ?? null);
  const [consent, setConsent] = useState(form ? form.consentText : 'I agree to be contacted about my enquiry.');
  const [thanks, setThanks] = useState(form?.thankYou ?? '');
  const save = useMutation({
    mutationFn: () => api.saveWebForm(form?.id ?? null, { name: name.trim(), heading, intro, fields, ...(owner ? { ownerId: owner } : {}), consentText: consent, thankYou: thanks }),
    onSuccess: (next) => {
      onSaved(next);
      toast.success('Form saved');
      onClose();
    }
  });
  const fe = isApiError(save.error) ? save.error.fieldErrors : {};
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="top-[4vh] max-h-[92vh] max-w-xl overflow-y-auto p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{form ? 'Edit web form' : 'New web form'}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">Name and mobile number are always asked. Tick what else you want to know.</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <Field label="Form name" hint="Only your team sees this." error={fe.name}>
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus placeholder="Website enquiry" />
          </Field>
          <Field label="Heading on the form" error={fe.heading}>
            <Input value={heading} onChange={(e) => setHeading(e.target.value)} maxLength={120} placeholder="Book a test ride" />
          </Field>
          <Field label="Line under the heading">
            <Input value={intro} onChange={(e) => setIntro(e.target.value)} maxLength={500} placeholder="Leave your number and we'll call you back today." />
          </Field>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">Also ask for</p>
            <div className="grid gap-2 sm:grid-cols-2">
              {offered.map((f) => (
                <Checkbox key={f.Key} checked={fields.includes(f.Key)} onCheckedChange={(c) => setFields((p) => (c ? [...p, f.Key] : p.filter((k) => k !== f.Key)))} label={f.Label} />
              ))}
            </div>
            {fe.fields ? <p className="mt-1 text-xs text-danger">{fe.fields}</p> : null}
          </div>
          <Field label="Who gets the leads" error={fe.ownerId} hint={form ? undefined : 'Leave empty to get them yourself.'}>
            <LookupCombobox target="users" value={owner} label={form?.ownerName} onChange={(v) => setOwner(v?.id ?? null)} />
          </Field>
          <Field label="Consent line" hint="Shown with a tick box the visitor must tick. Each tick is saved under Consents. Leave empty to ask for none.">
            <Textarea value={consent} onChange={(e) => setConsent(e.target.value)} rows={2} maxLength={500} />
          </Field>
          <Field label="Thank-you message">
            <Input value={thanks} onChange={(e) => setThanks(e.target.value)} maxLength={300} placeholder="Thank you. We'll get in touch soon." />
          </Field>
          {save.isError && Object.keys(fe).length === 0 ? <Alert tone="danger">{errorText(save.error, 'Something went wrong.')}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              Save form
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
