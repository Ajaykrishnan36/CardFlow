import { useQuery } from '@tanstack/react-query';
import { integrationsApi } from '@crm/api/endpoints';

export const integrationsKey = ['platform', 'integrations'] as const;

/** Connected apps (owner). Polls so sync counts stay fresh while the page is open. */
export function useIntegrations(options: { poll?: boolean } = {}) {
  return useQuery({
    queryKey: integrationsKey,
    queryFn: integrationsApi.list,
    refetchInterval: options.poll ? 10_000 : false,
    staleTime: 10_000
  });
}
