import { useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Crown, Ellipsis, GitBranch, Pencil, Plus, Trash2, UsersRound } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { WorkspaceRole } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';

export interface RoleNodeBody {
  name?: string;
  description?: string;
  parentRoleId?: string;
}

export interface RoleHierarchyApi {
  create: (body: RoleNodeBody) => Promise<unknown>;
  update: (id: string, body: RoleNodeBody) => Promise<unknown>;
  remove: (id: string) => Promise<unknown>;
}

type Node = WorkspaceRole & { children: Node[]; depth: number };

function buildTree(roles: WorkspaceRole[]): Node[] {
  const byId = new Map<string, Node>(roles.map((r) => [r.id, { ...r, children: [], depth: 0 }]));
  const roots: Node[] = [];
  for (const n of byId.values()) {
    const parent = n.parentRoleId ? byId.get(n.parentRoleId) : undefined;
    if (parent) parent.children.push(n);
    else roots.push(n);
  }
  const sort = (list: Node[], depth: number) => {
    list.sort((a, b) => b.rank - a.rank || a.name.localeCompare(b.name));
    for (const n of list) {
      n.depth = depth;
      sort(n.children, depth + 1);
    }
  };
  sort(roots, 0);
  return roots;
}

function flatten(nodes: Node[]): Node[] {
  return nodes.flatMap((n) => [n, ...flatten(n.children)]);
}

function descendants(n: Node): Set<string> {
  return new Set(flatten(n.children).map((c) => c.id));
}

/**
 * Role hierarchy (D-48): roles decide whose records people see — their own plus those of
 * everyone in roles below theirs. What people can do comes from permission sets.
 */
export function RoleHierarchy({
  roles,
  loading,
  error,
  onRetry,
  canEdit,
  api,
  onChanged,
  context
}: {
  roles?: WorkspaceRole[];
  loading?: boolean;
  error?: unknown;
  onRetry?: () => void;
  canEdit: boolean;
  api: RoleHierarchyApi;
  onChanged: () => void;
  /** e.g. the workspace name, shown in the dialog. */
  context?: string;
}) {
  const { t } = useTranslation();
  const tree = useMemo(() => buildTree(roles ?? []), [roles]);
  const all = useMemo(() => flatten(tree), [tree]);
  const [editing, setEditing] = useState<{ node?: Node; parentId?: string } | null>(null);
  const [toDelete, setToDelete] = useState<Node | null>(null);

  const remove = useMutation({
    mutationFn: (n: Node) => api.remove(n.id),
    onSuccess: (_r, n) => {
      toast.success(t('access.hierarchy.deleted', { name: n.name }));
      setToDelete(null);
      onChanged();
    },
    onError: (e) => {
      setToDelete(null);
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  let body;
  if (error) {
    body = <ErrorState title={t('access.roles.errorTitle')} message={isApiError(error) ? error.message : undefined} onRetry={onRetry} />;
  } else if (loading || !roles) {
    body = (
      <div className="space-y-2 p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} style={{ marginLeft: i * 24 }}>
            <Skeleton className="h-10" />
          </div>
        ))}
      </div>
    );
  } else {
    body = (
      <ul className="py-2" role="tree" aria-label={t('access.hierarchy.title')}>
        {all.map((n) => (
          <li key={n.id} role="treeitem" aria-level={n.depth + 1} aria-selected={false} className="group flex items-start gap-2 px-3 py-1.5 sm:px-5">
            <div className="flex min-w-0 flex-1 items-start gap-2" style={{ paddingLeft: n.depth * 28 }}>
              {n.depth > 0 ? <span className="mt-3 h-px w-4 shrink-0 bg-border" aria-hidden /> : null}
              <span className={`mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg ${n.key === 'SUPER_ADMIN' ? 'bg-warning-soft text-warning' : 'bg-primary-soft text-primary'}`}>
                {n.key === 'SUPER_ADMIN' ? <Crown className="size-4" aria-hidden /> : <GitBranch className="size-4" aria-hidden />}
              </span>
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-1.5">
                  <span className="text-[13px] font-semibold text-foreground">{n.name}</span>
                  {n.isSystem ? <Badge tone="neutral">{t('access.roles.badgeBuiltIn')}</Badge> : <Badge tone="primary">{t('access.roles.badgeCustom')}</Badge>}
                  <Badge tone={n.assignedCount ? 'success' : 'neutral'} className="gap-1">
                    <UsersRound className="size-3" aria-hidden />
                    {t('access.roles.users', { count: n.assignedCount })}
                  </Badge>
                  {n.key === 'SUPER_ADMIN' ? <Badge tone="warning">{t('access.hierarchy.fullAccess')}</Badge> : null}
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {n.description ||
                    (n.children.length
                      ? t('access.hierarchy.seesBelow', { count: flatten(n.children).length })
                      : t('access.hierarchy.seesOwn'))}
                </p>
              </div>
            </div>
            {canEdit ? (
              <Menu modal={false}>
                <MenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label={t('access.roles.actionsFor', { name: n.name })} className="opacity-70 group-hover:opacity-100">
                    <Ellipsis />
                  </Button>
                </MenuTrigger>
                <MenuContent align="end" className="min-w-[13rem]">
                  <MenuItem onSelect={() => setEditing({ parentId: n.id })}>
                    <Plus /> {t('access.hierarchy.addBelow')}
                  </MenuItem>
                  {n.key !== 'SUPER_ADMIN' ? (
                    <MenuItem onSelect={() => setEditing({ node: n })}>
                      <Pencil /> {n.isSystem ? t('access.hierarchy.move') : t('access.hierarchy.edit')}
                    </MenuItem>
                  ) : null}
                  {!n.isSystem ? (
                    <>
                      <MenuSeparator />
                      <MenuItem danger onSelect={() => setToDelete(n)}>
                        <Trash2 /> {t('access.roles.delete')}
                      </MenuItem>
                    </>
                  ) : null}
                </MenuContent>
              </Menu>
            ) : null}
          </li>
        ))}
      </ul>
    );
  }

  return (
    <Card className="min-w-0">
      <CardHeader
        title={t('access.hierarchy.title')}
        description={t('access.hierarchy.description')}
        actions={
          canEdit && roles ? (
            <Button size="sm" onClick={() => setEditing({ parentId: tree[0]?.id })}>
              <Plus /> {t('access.roles.new')}
            </Button>
          ) : null
        }
      />
      <div className="border-t">{body}</div>
      <RoleNodeDialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        node={editing?.node}
        defaultParent={editing?.parentId}
        roles={all}
        api={api}
        context={context}
        onSaved={() => {
          setEditing(null);
          onChanged();
        }}
      />
      <ConfirmDialog
        open={toDelete !== null}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={t('access.hierarchy.deleteTitle', { name: toDelete?.name ?? '' })}
        body={t('access.hierarchy.deleteBody')}
        confirmLabel={t('access.roles.delete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => toDelete && remove.mutate(toDelete)}
      />
    </Card>
  );
}

function RoleNodeDialog({
  open,
  onOpenChange,
  node,
  defaultParent,
  roles,
  api,
  context,
  onSaved
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  node?: Node;
  defaultParent?: string;
  roles: Node[];
  api: RoleHierarchyApi;
  context?: string;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [parent, setParent] = useState('');
  const [error, setError] = useState<Record<string, string>>({});
  const blocked = node ? descendants(node) : new Set<string>();
  const parents = roles.filter((r) => r.id !== node?.id && !blocked.has(r.id));

  const save = useMutation({
    mutationFn: () => {
      const body: RoleNodeBody = { parentRoleId: parent || undefined };
      if (!node || !node.isSystem) {
        body.name = name.trim();
        body.description = description.trim();
      }
      return node ? api.update(node.id, body) : api.create(body);
    },
    onSuccess: () => {
      toast.success(node ? t('access.hierarchy.saved', { name: name || node.name }) : t('access.hierarchy.created', { name }));
      onSaved();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) setError(e.fieldErrors);
      else toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if ((!node || !node.isSystem) && !name.trim()) return setError({ name: t('common.required') });
    setError({});
    save.mutate();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setName(node?.name ?? '');
          setDescription(node?.description ?? '');
          setParent(node?.parentRoleId ?? defaultParent ?? '');
          setError({});
        }
        onOpenChange(o);
      }}
    >
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{node ? t('access.hierarchy.editTitle', { name: node.name }) : t('access.hierarchy.newTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">
          {context ? `${context} · ` : ''}
          {t('access.hierarchy.dialogBody')}
        </DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          {!node || !node.isSystem ? (
            <>
              <Field label={t('access.roles.name')} error={error.name}>
                <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} autoFocus placeholder={t('access.hierarchy.namePlaceholder')} />
              </Field>
              <Field label={t('access.roles.descriptionLabel')} error={error.description}>
                <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} maxLength={300} />
              </Field>
            </>
          ) : null}
          <Field label={t('access.hierarchy.reportsTo')} error={error.parentRoleId} hint={t('access.hierarchy.reportsToHint')}>
            <Select
              value={parent}
              onChange={(e) => setParent(e.target.value)}
              options={parents.map((r) => ({ value: r.id, label: `${'— '.repeat(r.depth)}${r.name}` }))}
            />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={save.isPending}>
              {node ? t('common.save') : t('access.hierarchy.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
