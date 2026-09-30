import { useMemo } from 'react';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { NoAccessPage } from '@crm/features/system/pages';
import { ObjectsPage } from './objects-page';
import { ObjectDetailPage } from './object-detail-page';
import { ObjectsScopeProvider, productObjectsScope } from './object-utils';

// /crm/w/:ws/settings/objects — a product's own objects (D-79), with the same builder
// the owner uses. Needs "Customize page layouts & fields".

function useProductScope() {
  const { code } = useWorkspace();
  return useMemo(() => productObjectsScope(code), [code]);
}

export function ProductObjectsPage() {
  const { context } = useWorkspace();
  const scope = useProductScope();
  if (!context.canCustomize && !context.viewerIsOwner) return <NoAccessPage />;
  return (
    <ObjectsScopeProvider value={scope}>
      <ObjectsPage />
    </ObjectsScopeProvider>
  );
}

export function ProductObjectDetailPage() {
  const { context } = useWorkspace();
  const scope = useProductScope();
  if (!context.canCustomize && !context.viewerIsOwner) return <NoAccessPage />;
  return (
    <ObjectsScopeProvider value={scope}>
      <ObjectDetailPage />
    </ObjectsScopeProvider>
  );
}
