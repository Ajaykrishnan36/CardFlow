import { useTranslation } from 'react-i18next';
import { Share2 } from 'lucide-react';
import { PageContainer, PageHeader } from '@crm/components/page';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { ApiSharing } from '@crm/features/access/api-sharing';
import { useWorkspace } from '../workspace-context';

/** /crm/w/:ws/settings/api — the workspace's APIs and which roles see each key (Super Admin, or roles with Manage roles). */
export function ApiSharingPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('access.sharing.title'));
  const allowed = context.viewerIsOwner || context.effective.capabilities.some((c) => c.key === 'access.manage');
  if (!allowed) return <RecordNoAccess />;
  return (
    <PageContainer wide>
      <PageHeader
        title={t('access.sharing.title')}
        description={t('access.sharing.subtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <Share2 className="size-5" aria-hidden />
          </span>
        }
      />
      <ApiSharing code={code} source={{ kind: 'member' }} />
    </PageContainer>
  );
}
