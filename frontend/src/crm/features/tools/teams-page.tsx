import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Pencil, Plus, Trash2, Users } from 'lucide-react';
import type { Team } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { initials } from '@crm/lib/utils';
import { errorText, PeoplePicker, toolKeys, useTools } from './tool-utils';

/** /crm/w/:ws/settings/teams — groups of people for "my team" filters and round-robin assignment. */
export function TeamsPage() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  useDocumentTitle(t('tools.teams.title'));
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'teams');
  const q = useQuery({ queryKey: key, queryFn: () => api.teams() });
  const [editing, setEditing] = useState<Team | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Team | null>(null);
  const del = useMutation({
    mutationFn: (id: string) => api.deleteTeam(id),
    onSuccess: () => {
      toast.success(t('tools.teams.deleted'));
      setDeleting(null);
      void qc.invalidateQueries({ queryKey: key });
    }
  });
  const canManage = q.data?.canManage ?? false;
  return (
    <PageContainer>
      <PageHeader
        title={t('tools.teams.title')}
        description={t('tools.teams.subtitle')}
        actions={
          canManage ? (
            <Button onClick={() => setEditing('new')}>
              <Plus /> {t('tools.teams.new')}
            </Button>
          ) : null
        }
      />
      {q.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : q.isError ? (
        <Card>
          <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />
        </Card>
      ) : q.data.data.length === 0 ? (
        <Card>
          <EmptyState
            icon={Users}
            title={t('tools.teams.emptyTitle')}
            body={t('tools.teams.emptyBody')}
            action={
              canManage ? (
                <Button onClick={() => setEditing('new')}>
                  <Plus /> {t('tools.teams.new')}
                </Button>
              ) : null
            }
          />
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {q.data.data.map((team) => (
            <Card key={team.id} className="flex flex-col">
              <CardHeader
                title={team.name}
                description={team.description || t('tools.teams.members', { count: team.members.length })}
                actions={
                  canManage ? (
                    <>
                      <Button size="icon-sm" variant="subtle" onClick={() => setEditing(team)} aria-label={t('tools.common.edit')}>
                        <Pencil />
                      </Button>
                      <Button size="icon-sm" variant="subtle" onClick={() => setDeleting(team)} aria-label={t('tools.common.delete')}>
                        <Trash2 />
                      </Button>
                    </>
                  ) : null
                }
              />
              <ul className="flex-1 divide-y">
                {team.members.length === 0 ? <li className="px-5 py-4 text-[13px] text-muted-foreground">{t('tools.teams.noMembers')}</li> : null}
                {team.members.map((m) => (
                  <li key={m.identityId} className="flex items-center gap-2.5 px-5 py-2">
                    <span className="grid size-7 shrink-0 place-items-center rounded-full bg-primary-soft text-[11px] font-semibold text-primary">{initials(m.name)}</span>
                    <span className="min-w-0">
                      <span className="block truncate text-[13px] font-medium">{m.name}</span>
                      <span className="block truncate text-xs text-muted-foreground">{m.email}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </Card>
          ))}
        </div>
      )}
      <TeamDialog team={editing} onClose={() => setEditing(null)} onSaved={() => void qc.invalidateQueries({ queryKey: key })} />
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('tools.teams.deleteTitle', { name: deleting?.name })}
        body={t('tools.teams.deleteBody')}
        confirmLabel={t('tools.common.delete')}
        tone="danger"
        loading={del.isPending}
        onConfirm={() => deleting && del.mutate(deleting.id)}
      />
    </PageContainer>
  );
}

function TeamDialog({ team, onClose, onSaved }: { team: Team | 'new' | null; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const editing = team && team !== 'new' ? team : null;
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [people, setPeople] = useState<Array<{ id: string; label: string }>>([]);
  const [last, setLast] = useState<typeof team>(null);
  if (team !== last) {
    setLast(team);
    setName(editing?.name ?? '');
    setDescription(editing?.description ?? '');
    setPeople((editing?.members ?? []).map((m) => ({ id: m.identityId, label: m.name })));
  }
  const m = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description: description.trim(), members: people.map((p) => p.id) };
      return editing ? api.updateTeam(editing.id, body) : api.createTeam(body);
    },
    onSuccess: () => {
      toast.success(editing ? t('tools.teams.saved') : t('tools.teams.created'));
      onSaved();
      onClose();
    }
  });
  return (
    <Dialog open={team !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{editing ? t('tools.teams.editTitle') : t('tools.teams.new')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.teams.dialogBody')}</DialogDescription>
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) m.mutate();
          }}
        >
          {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
          <Field label={t('tools.teams.name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus placeholder={t('tools.teams.namePlaceholder')} />
          </Field>
          <Field label={t('tools.teams.description')}>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} maxLength={300} />
          </Field>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">{t('tools.teams.people')}</p>
            <PeoplePicker value={people} onChange={setPeople} />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={m.isPending} disabled={!name.trim()}>
              {editing ? t('tools.common.save') : t('tools.teams.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
