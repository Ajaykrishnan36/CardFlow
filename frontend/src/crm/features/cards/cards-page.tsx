import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Building2, CreditCard, ExternalLink, Link2, Mail, MapPin, Phone, Plus, ScanLine, Unlink, UserPlus } from 'lucide-react';
import { cardsApi, type CardFields, type CardMatches, type SaveCardBody, type SavedCard } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { EmptyState, ErrorState } from '@crm/components/states';
import { PageContainer, PageHeader, SearchInput } from '@crm/components/page';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';

const cardsKey = (code: string, ...rest: unknown[]) => ['workspace', code, 'cards', ...rest] as const;
const objectLabel: Record<string, string> = { leads: 'Lead', contacts: 'Contact', accounts: 'Account' };

/**
 * /crm/w/:ws/cards — business cards saved in this business (D-98). Cards scanned in the
 * mobile app land here; a card can also be typed in. Each card is checked against this
 * business's own leads, contacts and accounts before it becomes a new record.
 */
export function CardsPage() {
  const { code, context } = useWorkspace();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState('');
  const [adding, setAdding] = useState(false);
  const openId = params.get('card');
  useDocumentTitle(`Business cards · ${context.workspace.name}`);
  const q = useQuery({ queryKey: cardsKey(code, 'list', search), queryFn: () => cardsApi.list(code, { q: search || undefined }), staleTime: 15_000 });
  const cards = q.data?.items ?? [];

  return (
    <PageContainer>
      <PageHeader
        title="Business cards"
        description="Cards you saved in this business. Save a card as a lead or contact, or attach it to someone you already have."
        actions={
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus /> Add a card
          </Button>
        }
      />
      <div className="mb-4 max-w-sm">
        <SearchInput value={search} onChange={setSearch} placeholder="Search name, company, phone or email" />
      </div>
      {q.isError ? (
        <Card>
          <ErrorState title="Couldn't load the cards" message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        </Card>
      ) : q.isPending ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Card key={i} className="space-y-2 p-4">
              <Skeleton className="h-4 w-36" />
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-3 w-40" />
            </Card>
          ))}
        </div>
      ) : cards.length === 0 ? (
        <Card>
          <EmptyState
            icon={ScanLine}
            title={search ? 'No card matches that search' : 'No business cards yet'}
            body={search ? 'Try a name, a company or the last digits of a phone number.' : 'Scan a card in the mobile app, or add one here. It can become a lead or a contact in one step.'}
            action={
              !search ? (
                <Button size="sm" onClick={() => setAdding(true)}>
                  <Plus /> Add a card
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {cards.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => setParams({ card: c.id })}
                className="block w-full rounded-xl border bg-card p-4 text-left transition-colors hover:border-primary/50 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <div className="flex items-start gap-3">
                  <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
                    <CreditCard className="size-4" aria-hidden />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-semibold text-foreground">{c.personName || c.company || 'Business card'}</p>
                    <p className="truncate text-xs text-muted-foreground">{[c.designation, c.company].filter(Boolean).join(' · ') || '—'}</p>
                    <p className="mt-1.5 truncate text-xs text-muted-foreground">{c.phones[0] ?? c.emails[0] ?? ''}</p>
                  </div>
                </div>
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {c.links.length === 0 ? <Badge tone="neutral">Card only</Badge> : null}
                  {c.links.map((l) => (
                    <Badge key={l.id} tone={l.object === 'leads' ? 'warning' : 'success'}>
                      {objectLabel[l.object] ?? l.object}: {l.title}
                    </Badge>
                  ))}
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
      <AddCardDialog open={adding} onOpenChange={setAdding} code={code} />
      <CardViewer code={code} id={openId} onClose={() => setParams({}, { replace: true })} />
    </PageContainer>
  );
}

const emptyForm = { personName: '', designation: '', company: '', phone: '', phone2: '', email: '', website: '', address: '', gstin: '', note: '' };

function toCard(f: typeof emptyForm): CardFields {
  return {
    personName: f.personName.trim(),
    designation: f.designation.trim(),
    company: f.company.trim(),
    website: f.website.trim(),
    address: f.address.trim(),
    gstin: f.gstin.trim(),
    phones: [f.phone, f.phone2].map((p) => p.trim()).filter(Boolean),
    emails: [f.email.trim()].filter(Boolean)
  };
}

/** Type a card in, see who it matches in this business, then choose what to do with it. */
function AddCardDialog({ open, onOpenChange, code }: { open: boolean; onOpenChange: (open: boolean) => void; code: string }) {
  const [form, setForm] = useState(emptyForm);
  const [matches, setMatches] = useState<CardMatches | null>(null);
  const [error, setError] = useState<string | null>(null);
  const set = (k: keyof typeof emptyForm) => (e: { target: { value: string } }) => {
    setForm((f) => ({ ...f, [k]: e.target.value }));
    setMatches(null);
  };
  useEffect(() => {
    if (!open) {
      setForm(emptyForm);
      setMatches(null);
      setError(null);
    }
  }, [open]);

  const check = useMutation({
    mutationFn: () => cardsApi.match(code, toCard(form)),
    onSuccess: (m) => {
      setError(null);
      setMatches(m);
    },
    onError: (e) => setError(isApiError(e) ? e.message : 'Something went wrong. Please try again.')
  });
  const card = toCard(form);
  const hasSomething = Boolean(card.personName || card.company || card.phones?.length || card.emails?.length);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] max-w-2xl overflow-y-auto p-5">
        <DialogTitle className="pr-8 text-base font-semibold">Add a business card</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">We check this business for the same person before anything is created.</DialogDescription>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <Field label="Name">
            <Input value={form.personName} onChange={set('personName')} autoFocus placeholder="Ravi Kumar" />
          </Field>
          <Field label="Company">
            <Input value={form.company} onChange={set('company')} placeholder="Kumar Steels" />
          </Field>
          <Field label="Job title">
            <Input value={form.designation} onChange={set('designation')} />
          </Field>
          <Field label="Email">
            <Input value={form.email} onChange={set('email')} type="email" inputMode="email" />
          </Field>
          <Field label="Phone">
            <Input value={form.phone} onChange={set('phone')} type="tel" inputMode="tel" />
          </Field>
          <Field label="Second phone">
            <Input value={form.phone2} onChange={set('phone2')} type="tel" inputMode="tel" />
          </Field>
          <Field label="Website">
            <Input value={form.website} onChange={set('website')} />
          </Field>
          <Field label="GSTIN">
            <Input value={form.gstin} onChange={set('gstin')} maxLength={15} />
          </Field>
          <Field label="Address" className="sm:col-span-2">
            <Textarea value={form.address} onChange={set('address')} rows={2} />
          </Field>
        </div>
        {error ? (
          <Alert tone="danger" className="mt-4">
            {error}
          </Alert>
        ) : null}
        {matches ? (
          <SaveOptions
            code={code}
            card={card}
            matches={matches}
            onSaved={() => onOpenChange(false)}
            noteValue={form.note}
            onNote={(v) => setForm((f) => ({ ...f, note: v }))}
          />
        ) : (
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button onClick={() => check.mutate()} loading={check.isPending} disabled={!hasSomething}>
              Continue
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The decision step shared by "add a card" and "save this card as…": matches first, then the action. */
export function SaveOptions({
  code,
  card,
  cardId,
  matches,
  onSaved,
  noteValue,
  onNote
}: {
  code: string;
  card?: CardFields;
  cardId?: string;
  matches: CardMatches;
  onSaved: () => void;
  noteValue: string;
  onNote: (v: string) => void;
}) {
  const qc = useQueryClient();
  const base = workspaceBase(code);
  const people = matches.matches.filter((m) => m.object !== 'accounts');
  const [action, setAction] = useState<SaveCardBody['action']>(matches.suggested === 'attach' ? 'attach' : matches.suggested);
  const [targetId, setTargetId] = useState(matches.matches[0]?.id ?? '');
  const [createAccount, setCreateAccount] = useState(true);
  const [allowDuplicate, setAllowDuplicate] = useState(false);
  const [followUp, setFollowUp] = useState('');
  const [error, setError] = useState<string | null>(null);
  const target = matches.matches.find((m) => m.id === targetId);

  const save = useMutation({
    mutationFn: () =>
      cardsApi.save(code, {
        cardId,
        card,
        action,
        target: action === 'attach' && target ? { object: target.object, id: target.id } : undefined,
        createAccount: action === 'contact' ? createAccount : undefined,
        allowDuplicate: allowDuplicate || undefined,
        followUpAt: followUp ? new Date(followUp).toISOString() : undefined,
        note: noteValue.trim() || undefined
      }),
    onSuccess: async (res) => {
      await qc.invalidateQueries({ queryKey: ['workspace', code] });
      const what = res.record ? `${objectLabel[res.record.object] ?? 'Record'} ${res.created ? 'created' : 'updated'}: ${res.record.title}` : 'Card saved';
      toast.success(what, res.record ? { action: { label: 'Open', onClick: () => window.location.assign(`${base}/${res.record!.object}/${res.record!.recordId}`) } } : undefined);
      onSaved();
    },
    onError: (e) => {
      if (isApiError(e) && e.code === 'possible_duplicate') {
        setError('Someone with this phone number or email is already in this business. Attach the card to them, or tick “Create anyway”.');
        return;
      }
      setError(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : 'Something went wrong. Please try again.');
    }
  });

  const options: Array<{ key: SaveCardBody['action']; label: string; hint: string; show: boolean }> = [
    { key: 'attach', label: 'Attach to an existing record', hint: 'Keeps one record per person. Empty fields are filled from the card.', show: matches.matches.length > 0 },
    { key: 'lead', label: 'New lead', hint: 'Someone you may do business with.', show: Boolean(matches.can.lead) },
    { key: 'contact', label: 'New contact', hint: 'Someone you already work with.', show: Boolean(matches.can.contact) },
    { key: 'card_only', label: 'Keep the card only', hint: 'No CRM record for now.', show: true }
  ];

  return (
    <div className="mt-5 space-y-4 border-t pt-4">
      {matches.matches.length > 0 ? (
        <div>
          <p className="text-[13px] font-semibold">Already in this business</p>
          <ul className="mt-2 space-y-1.5">
            {matches.matches.map((m) => (
              <li key={m.object + m.id}>
                <label className="flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-sm hover:bg-muted/50">
                  <input
                    type="radio"
                    name="card-target"
                    checked={action === 'attach' && targetId === m.id}
                    onChange={() => {
                      setTargetId(m.id);
                      setAction('attach');
                    }}
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{m.title}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {objectLabel[m.object]} · {m.code}
                      {m.subtitle ? ` · ${m.subtitle}` : ''} · same {m.matchedOn.join(' and ')}
                    </span>
                  </span>
                  <Link to={`${base}/${m.object}/${m.id}`} target="_blank" className="text-muted-foreground hover:text-primary" aria-label={`Open ${m.title}`}>
                    <ExternalLink className="size-4" />
                  </Link>
                </label>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <Alert tone="info">Nobody in this business has this phone number or email yet.</Alert>
      )}
      {matches.hidden > 0 ? (
        <Alert tone="warning">
          {matches.hidden === 1 ? 'A record' : `${matches.hidden} records`} in this business that you can’t open already {matches.hidden === 1 ? 'has' : 'have'} this phone number or email. Ask the
          owner of that record before creating another.
        </Alert>
      ) : null}

      <fieldset>
        <legend className="text-[13px] font-semibold">What should we do with this card?</legend>
        <div className="mt-2 grid gap-2 sm:grid-cols-2">
          {options
            .filter((o) => o.show)
            .map((o) => (
              <label key={o.key} className={'flex cursor-pointer gap-2.5 rounded-lg border px-3 py-2 text-sm ' + (action === o.key ? 'border-primary bg-primary-soft/40' : 'hover:bg-muted/50')}>
                <input type="radio" name="card-action" className="mt-1" checked={action === o.key} onChange={() => setAction(o.key)} />
                <span>
                  <span className="block font-medium">{o.label}</span>
                  <span className="block text-xs text-muted-foreground">{o.hint}</span>
                </span>
              </label>
            ))}
        </div>
      </fieldset>

      {action === 'contact' && card?.company && matches.can.account ? (
        <Checkbox checked={createAccount} onCheckedChange={setCreateAccount} label={`Put the contact under the account “${card.company}”`} description="Uses the account if it exists, otherwise creates it." />
      ) : null}
      {(action === 'lead' || action === 'contact') && (people.length > 0 || matches.hidden > 0) ? (
        <Checkbox checked={allowDuplicate} onCheckedChange={setAllowDuplicate} label="Create anyway" description="A second record for the same phone number or email." />
      ) : null}
      {action !== 'card_only' ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Follow up on" hint={action === 'lead' || target?.object === 'leads' ? 'Shows on the lead and on your dashboard.' : 'Creates a task for you.'}>
            <Input type="datetime-local" value={followUp} onChange={(e) => setFollowUp(e.target.value)} />
          </Field>
          <Field label="Note">
            <Input value={noteValue} onChange={(e) => onNote(e.target.value)} placeholder="Where you met, what they need…" />
          </Field>
        </div>
      ) : null}
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div className="flex justify-end">
        <Button onClick={() => save.mutate()} loading={save.isPending} disabled={action === 'attach' && !target}>
          {action === 'lead' ? 'Create lead' : action === 'contact' ? 'Create contact' : action === 'attach' ? 'Attach card' : 'Save card'}
        </Button>
      </div>
    </div>
  );
}

/** One card: the scanned picture, its details, what it is linked to, and "save as…". */
export function CardViewer({ code, id, onClose }: { code: string; id: string | null; onClose: () => void }) {
  const qc = useQueryClient();
  const base = workspaceBase(code);
  const q = useQuery({ queryKey: cardsKey(code, 'one', id), queryFn: () => cardsApi.get(code, id!), enabled: Boolean(id) });
  const [matches, setMatches] = useState<CardMatches | null>(null);
  const [note, setNote] = useState('');
  const [side, setSide] = useState<'front' | 'back'>('front');
  useEffect(() => {
    setMatches(null);
    setNote('');
    setSide('front');
  }, [id]);
  const c: SavedCard | undefined = q.data;
  const fields = useMemo<CardFields | undefined>(
    () => (c ? { personName: c.personName, company: c.company, designation: c.designation, phones: c.phones, emails: c.emails, website: c.website, address: c.address, gstin: c.gstin } : undefined),
    [c]
  );
  const check = useMutation({ mutationFn: () => cardsApi.match(code, fields!), onSuccess: setMatches });
  const unlink = useMutation({
    mutationFn: (linkId: string) => cardsApi.unlink(code, id!, linkId),
    onSuccess: async () => {
      toast.success('Card removed from the record');
      await qc.invalidateQueries({ queryKey: ['workspace', code] });
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : 'Something went wrong.')
  });

  return (
    <Dialog open={Boolean(id)} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="max-h-[92vh] max-w-2xl overflow-y-auto p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{c ? c.personName || c.company || 'Business card' : 'Business card'}</DialogTitle>
        <DialogDescription className="mt-0.5 text-sm text-muted-foreground">
          {c ? [c.designation, c.company].filter(Boolean).join(' · ') || 'Saved card' : 'Loading…'}
        </DialogDescription>
        {q.isError ? (
          <ErrorState title="Couldn't open this card" message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        ) : !c ? (
          <div className="mt-4 space-y-2">
            <Skeleton className="h-40 w-full rounded-lg" />
            <Skeleton className="h-4 w-48" />
          </div>
        ) : (
          <div className="mt-4 space-y-4">
            {c.hasImage ? (
              <div>
                <img src={cardsApi.imageUrl(code, c.id, side)} alt="Scanned business card" className="max-h-72 w-full rounded-lg border bg-muted object-contain" />
                {c.hasBack ? (
                  <div className="mt-2 flex gap-2">
                    {(['front', 'back'] as const).map((s) => (
                      <Button key={s} size="sm" variant={side === s ? 'primary' : 'outline'} onClick={() => setSide(s)}>
                        {s === 'front' ? 'Front' : 'Back'}
                      </Button>
                    ))}
                  </div>
                ) : null}
              </div>
            ) : null}
            <dl className="grid gap-2 text-sm sm:grid-cols-2">
              {c.phones.map((p) => (
                <div key={p} className="flex items-center gap-2">
                  <Phone className="size-4 text-muted-foreground" aria-hidden />
                  <a href={`tel:${p}`} className="hover:underline">
                    {p}
                  </a>
                </div>
              ))}
              {c.emails.map((e) => (
                <div key={e} className="flex items-center gap-2">
                  <Mail className="size-4 text-muted-foreground" aria-hidden />
                  <a href={`mailto:${e}`} className="truncate hover:underline">
                    {e}
                  </a>
                </div>
              ))}
              {c.website ? (
                <div className="flex items-center gap-2">
                  <Link2 className="size-4 text-muted-foreground" aria-hidden />
                  <a href={c.website} target="_blank" rel="noreferrer" className="truncate hover:underline">
                    {c.website}
                  </a>
                </div>
              ) : null}
              {c.gstin ? (
                <div className="flex items-center gap-2">
                  <Building2 className="size-4 text-muted-foreground" aria-hidden /> GSTIN {c.gstin}
                </div>
              ) : null}
              {c.address ? (
                <div className="flex items-start gap-2 sm:col-span-2">
                  <MapPin className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden /> {c.address}
                </div>
              ) : null}
            </dl>
            {c.notes ? <p className="rounded-lg bg-muted/60 px-3 py-2 text-sm">{c.notes}</p> : null}
            <p className="text-xs text-muted-foreground">
              Saved {new Date(c.createdAt).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })}
              {c.savedBy ? ` by ${c.mine ? 'you' : c.savedBy}` : ''}
            </p>

            <div>
              <p className="text-[13px] font-semibold">In the CRM</p>
              {c.links.length === 0 ? (
                <p className="mt-1 text-sm text-muted-foreground">This card isn’t linked to a lead, contact or account in this business.</p>
              ) : (
                <ul className="mt-2 space-y-1.5">
                  {c.links.map((l) => (
                    <li key={l.id} className="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
                      <Badge tone={l.object === 'leads' ? 'warning' : 'success'}>{objectLabel[l.object] ?? l.object}</Badge>
                      <Link to={`${base}/${l.object}/${l.recordId}`} className="min-w-0 flex-1 truncate font-medium hover:text-primary hover:underline" onClick={onClose}>
                        {l.title} <span className="font-mono text-xs text-muted-foreground">{l.code}</span>
                      </Link>
                      <Button size="sm" variant="ghost" onClick={() => unlink.mutate(l.id)} loading={unlink.isPending && unlink.variables === l.id} aria-label="Remove the card from this record">
                        <Unlink />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {c.mine ? (
              matches ? (
                <SaveOptions code={code} cardId={c.id} card={fields} matches={matches} onSaved={() => setMatches(null)} noteValue={note} onNote={setNote} />
              ) : (
                <div className="flex justify-end border-t pt-4">
                  <Button variant="outline" onClick={() => check.mutate()} loading={check.isPending}>
                    <UserPlus /> {c.links.length ? 'Link to another record' : 'Save as lead or contact'}
                  </Button>
                </div>
              )
            ) : null}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
