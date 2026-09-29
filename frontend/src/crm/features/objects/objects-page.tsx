import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Boxes, ChevronRight, Plus } from 'lucide-react';
import { objectsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ObjectDefinition } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NewObjectDialog, ObjectIcon, objectKeys } from './object-utils';

/** /crm/owner/objects — every object beyond leads/accounts/contacts: standard ones and yours. */
export function ObjectsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('objects.title'));
  const q = useQuery({ queryKey: objectKeys.all, queryFn: objectsApi.list });
  const [open, setOpen] = useState(false);
  const custom = q.data?.data.filter((d) => !d.standard) ?? [];
  const standard = q.data?.data.filter((d) => d.standard) ?? [];

  return (
    <PageContainer>
      <PageHeader
        title={t('objects.title')}
        description={t('objects.subtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <Boxes className="size-5" aria-hidden />
          </span>
        }
        actions={
          <Button onClick={() => setOpen(true)} disabled={!q.data}>
            <Plus /> {t('objects.new')}
          </Button>
        }
      />
      {q.isError ? (
        <ErrorState title={t('objects.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
      ) : !q.data ? (
        <Card className="space-y-3 p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </Card>
      ) : (
        <div className="space-y-5">
          <Card className="overflow-hidden">
            <CardHeader title={t('objects.yours')} description={t('objects.yoursBody')} />
            {custom.length === 0 ? (
              <EmptyState
                icon={Boxes}
                title={t('objects.noneTitle')}
                body={t('objects.noneBody')}
                action={
                  <Button size="sm" onClick={() => setOpen(true)}>
                    <Plus /> {t('objects.new')}
                  </Button>
                }
              />
            ) : (
              <ObjectRows list={custom} />
            )}
          </Card>
          <Card className="overflow-hidden">
            <CardHeader title={t('objects.standard')} description={t('objects.standardBody')} />
            <ObjectRows list={standard} />
          </Card>
        </div>
      )}
      <NewObjectDialog open={open} onOpenChange={setOpen} icons={q.data?.icons ?? ['box']} />
    </PageContainer>
  );
}

function ObjectRows({ list }: { list: ObjectDefinition[] }) {
  const { t } = useTranslation();
  return (
    <ul className="divide-y border-t">
      {list.map((d) => (
        <li key={d.key}>
          <Link to={`/crm/owner/objects/${encodeURIComponent(d.key)}`} className="group flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
            <ObjectIcon icon={d.icon} />
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 truncate text-[13px] font-semibold group-hover:text-primary">
                {d.plural}
                {d.status === 'archived' ? <Badge tone="neutral">{t('objects.archived')}</Badge> : null}
              </p>
              <p className="truncate text-xs text-muted-foreground">{d.description || t('objects.noDescription')}</p>
            </div>
            <div className="hidden text-right text-xs text-muted-foreground sm:block">
              <p className="font-mono">{d.key}</p>
              <p>
                {t('objects.fieldsCount', { count: d.fields.length + (d.statuses.length ? 2 : 1) })} · {t('objects.recordsCount', { count: d.records })}
              </p>
            </div>
            <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
          </Link>
        </li>
      ))}
    </ul>
  );
}
