import { useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Ellipsis, KeyRound, Layers, Lock, Pencil, Plus, ShieldCheck, Trash2 } from 'lucide-react';
import { workspaceAdminApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessRules, PermissionSet, WorkspaceAdminOptions, WorkspaceRole } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, Tooltip } from '@crm/components/ui/menu';
import { ConfirmDialog } from '@crm/components/page';
import { EmptyState } from '@crm/components/states';
import { emptyRules, normalizeRules, rulesEqual, summarizeRules } from '@crm/features/access/use-access';
import { adminErrorMessage, exceedingGrants, useInvalidateAdmin } from './admin-utils';
import { GrantableMatrix } from './grantable-matrix';

type Kind = 'role' | 'set';
type Item = WorkspaceRole | PermissionSet;

const isRole = (i: Item): i is WorkspaceRole => 'isSystem' in i;

/** Roles or permission sets of this workspace: list + New / Edit / Delete, all limited to your own access. */
export function RulesTab({ kind, code, options }: { kind: Kind; code: string; options: WorkspaceAdminOptions }) {
  const { t } = useTranslation();
  const invalidate = useInvalidateAdmin(code);
  const items: Item[] = kind === 'role' ? options.roles : options.permissionSets;
  const [editing, setEditing] = useState<Item | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Item | null>(null);
  const k = kind === 'role' ? 'roles' : 'sets';
  const allRecords = t('access.matrix.scope.workspace').toLowerCase();

  const remove = useMutation({
    mutationFn: (item: Item) => (kind === 'role' ? workspaceAdminApi(code).deleteRole(item.id) : workspaceAdminApi(code).deletePermissionSet(item.id)),
    onSuccess: (_r, item) => {
      toast.success(t(`workspaceApp.admin.${k}.deleted`, { name: item.name }));
      setDeleting(null);
      invalidate();
    },
    onError: (e) => {
      setDeleting(null);
      toast.error(isApiError(e) && e.code === 'role_in_use' ? e.message || t('workspaceApp.admin.roles.inUse') : adminErrorMessage(e, t));
    }
  });

  return (
    <Card className="overflow-hidden">
      <CardHeader
        title={t(`workspaceApp.admin.${k}.title`)}
        description={t(`workspaceApp.admin.${k}.description`)}
        actions={
          <Button size="sm" onClick={() => setEditing('new')}>
            <Plus /> {t(`workspaceApp.admin.${k}.new`)}
          </Button>
        }
      />
      {items.length === 0 ? (
        <EmptyState
          icon={kind === 'role' ? ShieldCheck : Layers}
          title={t(`workspaceApp.admin.${k}.emptyTitle`)}
          body={t(`workspaceApp.admin.${k}.emptyBody`)}
          action={
            <Button size="sm" variant="outline" onClick={() => setEditing('new')}>
              <Plus /> {t(`workspaceApp.admin.${k}.new`)}
            </Button>
          }
        />
      ) : (
        <ul className="divide-y">
          {items.map((item) => {
            const beyond = exceedingGrants(item.rules, options.grantable, options.catalog, allRecords);
            const summary = summarizeRules(item.rules, options.catalog);
            const system = isRole(item) && item.isSystem;
            return (
              <li key={item.id} className="flex items-start gap-3 px-4 py-3 sm:px-5">
                <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
                  {kind === 'role' ? <KeyRound className="size-4" aria-hidden /> : <Layers className="size-4" aria-hidden />}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <button type="button" onClick={() => setEditing(item)} className="truncate text-left text-[13px] font-medium text-foreground hover:text-primary hover:underline">
                      {item.name}
                    </button>
                    {system ? <Badge>{t('workspaceApp.admin.roles.builtIn')}</Badge> : null}
                    {isRole(item) && item.customized ? <Badge tone="warning">{t('workspaceApp.admin.roles.customized')}</Badge> : null}
                    {beyond.length ? (
                      <Tooltip content={<span className="block max-w-[260px]">{t('workspaceApp.admin.beyondList', { list: beyond.join(', ') })}</span>} side="top">
                        <span tabIndex={0} className="inline-flex focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-full">
                          <Badge tone="warning">
                            <Lock className="size-3" aria-hidden /> {t('workspaceApp.admin.moreThanYours')}
                          </Badge>
                        </span>
                      </Tooltip>
                    ) : null}
                  </div>
                  {item.description ? <p className="truncate text-xs text-muted-foreground">{item.description}</p> : null}
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{summary || t('workspaceApp.admin.noGrants')}</p>
                </div>
                <span className="hidden shrink-0 text-xs tabular-nums text-muted-foreground sm:block">{t('workspaceApp.admin.assigned', { count: item.assignedCount })}</span>
                <Menu>
                  <MenuTrigger asChild>
                    <Button variant="ghost" size="icon-sm" aria-label={t('workspaceApp.admin.actionsFor', { name: item.name })}>
                      <Ellipsis />
                    </Button>
                  </MenuTrigger>
                  <MenuContent align="end">
                    <MenuItem onSelect={() => setEditing(item)}>
                      <Pencil /> {system ? t('workspaceApp.admin.roles.editPermissions') : t('workspaceApp.admin.edit')}
                    </MenuItem>
                    {!system ? (
                      <>
                        <MenuSeparator />
                        <MenuItem danger onSelect={() => setDeleting(item)}>
                          <Trash2 /> {t('workspaceApp.admin.delete')}
                        </MenuItem>
                      </>
                    ) : null}
                  </MenuContent>
                </Menu>
              </li>
            );
          })}
        </ul>
      )}

      <RulesDialog kind={kind} code={code} options={options} item={editing} onClose={() => setEditing(null)} />
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t(`workspaceApp.admin.${k}.deleteTitle`, { name: deleting?.name ?? '' })}
        body={
          deleting && deleting.assignedCount > 0
            ? t(`workspaceApp.admin.${k}.deleteInUse`, { count: deleting.assignedCount })
            : t(`workspaceApp.admin.${k}.deleteBody`)
        }
        confirmLabel={t('workspaceApp.admin.delete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting)}
      />
    </Card>
  );
}

function RulesDialog({ kind, code, options, item, onClose }: { kind: Kind; code: string; options: WorkspaceAdminOptions; item: Item | 'new' | null; onClose: () => void }) {
  const { t } = useTranslation();
  const invalidate = useInvalidateAdmin(code);
  const open = item !== null;
  const existing = item && item !== 'new' ? item : null;
  const system = Boolean(existing && isRole(existing) && existing.isSystem);
  const k = kind === 'role' ? 'roles' : 'sets';
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [rules, setRules] = useState<AccessRules>(emptyRules());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(existing?.name ?? '');
    setDescription(existing?.description ?? '');
    setRules(normalizeRules(existing?.rules, options.catalog));
    setErrors({});
    setFormError(null);
    // Reset only when the dialog opens for another item.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, existing?.id]);

  const beyond = exceedingGrants(rules, options.grantable, options.catalog, t('access.matrix.scope.workspace').toLowerCase());
  const unchanged = existing ? existing.name === name.trim() && (existing.description ?? '') === description.trim() && rulesEqual(existing.rules, rules, options.catalog) : false;

  const save = useMutation({
    mutationFn: async () => {
      const api = workspaceAdminApi(code);
      const body = { name: name.trim(), description: description.trim() || undefined, rules: normalizeRules(rules, options.catalog) };
      if (kind === 'role') {
        // Built-in roles: permissions only (no rename).
        if (existing) return api.updateRole(existing.id, system ? { rules: body.rules } : body);
        return api.createRole(body);
      }
      if (existing) return api.updatePermissionSet(existing.id, body);
      return api.createPermissionSet(body);
    },
    onSuccess: () => {
      toast.success(t(existing ? `workspaceApp.admin.${k}.saved` : `workspaceApp.admin.${k}.created`, { name: name.trim() }));
      invalidate();
      onClose();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        setErrors(e.fieldErrors);
        setFormError(e.fieldErrors.rules ?? t('workspaceApp.admin.fixErrors'));
        return;
      }
      setFormError(adminErrorMessage(e, t));
    }
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!system && !name.trim()) {
      setErrors({ name: t('common.required') });
      return;
    }
    setErrors({});
    setFormError(null);
    save.mutate();
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="top-[3vh] flex max-h-[94vh] max-w-3xl flex-col p-0 sm:top-[5vh] sm:max-h-[90vh]">
        <div className="border-b px-5 py-4 pr-12">
          <DialogTitle className="text-base font-semibold">
            {existing ? t(`workspaceApp.admin.${k}.editTitle`, { name: existing.name }) : t(`workspaceApp.admin.${k}.newTitle`)}
          </DialogTitle>
          <DialogDescription className="text-[13px] text-muted-foreground">{t('workspaceApp.admin.limitNote')}</DialogDescription>
        </div>
        <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-5">
            {formError ? <Alert tone="danger">{formError}</Alert> : null}
            {system ? <Alert tone="info">{t('workspaceApp.admin.roles.builtInNote')}</Alert> : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t('workspaceApp.admin.name')} error={errors.name}>
                <Input value={name} onChange={(e) => setName(e.target.value)} disabled={system || save.isPending} maxLength={80} />
              </Field>
              <Field label={t('workspaceApp.admin.descriptionLabel')} error={errors.description}>
                <Textarea rows={1} value={description} onChange={(e) => setDescription(e.target.value)} disabled={system || save.isPending} maxLength={240} />
              </Field>
            </div>
            {beyond.length ? (
              <Alert tone="warning" title={t('workspaceApp.admin.beyondTitle')}>
                {t('workspaceApp.admin.beyondBody', { list: beyond.join(', ') })}
              </Alert>
            ) : null}
            <GrantableMatrix value={rules} onChange={setRules} catalog={options.catalog} grantable={options.grantable} disabled={save.isPending} />
          </div>
          <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={save.isPending} disabled={unchanged}>
              {existing ? t('workspaceApp.admin.save') : t(`workspaceApp.admin.${k}.create`)}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
