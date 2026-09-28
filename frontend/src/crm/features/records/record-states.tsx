import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Lock } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Card } from '@crm/components/ui/card';
import { PageContainer } from '@crm/components/page';
import { EmptyState } from '@crm/components/states';
import { useRecordScope } from './record-scope';

/** 403 from the record engine (action not allowed, or no longer a member). */
export function isForbidden(err: unknown): boolean {
  return isApiError(err) && err.status === 403;
}

/** Friendly in-page "you can't see this" state for record pages. */
export function RecordNoAccess({ bare }: { bare?: boolean }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const home = scope.audience === 'owner' ? '/crm/owner/dashboard' : `${scope.routeBase}/home`;
  const body = (
    <Card>
      <EmptyState
        icon={Lock}
        title={t('records.common.noAccessTitle')}
        body={t('records.common.noAccessBody')}
        action={
          <Button asChild variant="outline" size="sm">
            <Link to={home}>{t('records.common.noAccessHome')}</Link>
          </Button>
        }
      />
    </Card>
  );
  return bare ? body : <PageContainer wide>{body}</PageContainer>;
}
