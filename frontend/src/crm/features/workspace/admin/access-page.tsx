import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ShieldCheck, UserRound } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { EffectiveAccessMatrix } from '@crm/features/access/effective-access';
import { useWorkspace } from '../workspace-context';
import { canAdminister, useAdminOptions } from './admin-utils';
import { MembersTab } from './members-tab';
import { RulesTab } from './rules-tabs';

type Tab = 'users' | 'roles' | 'sets';

/** /crm/w/:ws/settings/access — delegated administration (members.manage / access.manage). */
export function WorkspaceAccessPage() {
  const { context } = useWorkspace();
  if (!canAdminister(context)) return <NoAccessPage />;
  return <AccessView />;
}

function AccessView() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('workspaceApp.admin.title'));
  const q = useAdminOptions(code);
  const [sp, setSp] = useSearchParams();
  const [mineOpen, setMineOpen] = useState(false);

  if (isApiError(q.error) && q.error.status === 403) return <NoAccessPage />;

  const options = q.data;
  const tabs: Array<{ value: Tab; label: string }> = [];
  if (options?.canManageMembers) tabs.push({ value: 'users', label: t('workspaceApp.admin.tabs.users') });
  if (options?.canManageAccess) {
    tabs.push({ value: 'roles', label: t('workspaceApp.admin.tabs.roles') });
    tabs.push({ value: 'sets', label: t('workspaceApp.admin.tabs.sets') });
  }
  if (options && tabs.length === 0) return <NoAccessPage />;
  const requested = sp.get('tab') as Tab | null;
  const tab: Tab | undefined = tabs.find((x) => x.value === requested)?.value ?? tabs[0]?.value;

  return (
    <PageContainer wide>
      <PageHeader
        title={t('workspaceApp.admin.title')}
        description={t('workspaceApp.admin.subtitle', { workspace: context.workspace.name })}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <ShieldCheck className="size-5" aria-hidden />
          </span>
        }
        actions={
          <button
            type="button"
            onClick={() => setMineOpen(true)}
            className="inline-flex items-center gap-1.5 rounded-full border bg-background px-3 py-1 text-[13px] font-medium text-foreground shadow-sm transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <UserRound className="size-3.5 text-muted-foreground" aria-hidden />
            {t('workspaceApp.admin.yourAccess')}
            {context.role ? <span className="text-muted-foreground">· {context.role.name}</span> : null}
          </button>
        }
      />

      {q.isError ? (
        <Card>
          <ErrorState
            title={t('workspaceApp.admin.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : !options || !tab ? (
        <div className="space-y-4" aria-busy="true">
          <Skeleton className="h-9 w-72" />
          <Card className="space-y-3 p-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10" />
            ))}
          </Card>
        </div>
      ) : (
        <>
          {tabs.length > 1 ? (
            <Tabs
              className="mb-4"
              value={tab}
              onChange={(v) =>
                setSp(
                  (prev) => {
                    const n = new URLSearchParams(prev);
                    n.set('tab', v);
                    return n;
                  },
                  { replace: true }
                )
              }
              items={tabs}
            />
          ) : null}
          {tab === 'users' ? <MembersTab code={code} options={options} /> : null}
          {tab === 'roles' ? <RulesTab key="roles" kind="role" code={code} options={options} /> : null}
          {tab === 'sets' ? <RulesTab key="sets" kind="set" code={code} options={options} /> : null}
        </>
      )}

      <Dialog open={mineOpen} onOpenChange={setMineOpen}>
        <DialogContent className="top-[4vh] max-h-[92vh] max-w-3xl overflow-y-auto p-5 sm:top-[8vh]">
          <DialogTitle className="pr-8 text-base font-semibold">{t('workspaceApp.admin.yourAccessTitle')}</DialogTitle>
          <DialogDescription className="mb-4 text-[13px] text-muted-foreground">{t('workspaceApp.admin.yourAccessBody')}</DialogDescription>
          <EffectiveAccessMatrix effective={context.effective} catalog={options?.catalog} isPlatform={context.workspace.isPlatform} />
        </DialogContent>
      </Dialog>
    </PageContainer>
  );
}
