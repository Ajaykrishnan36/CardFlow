import { useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import type { UserDetail } from '@crm/api/types';

/** Detail lives under its own key so list invalidation doesn't refetch the open record. */
export const userKey = (id: string) => ['platform', 'user', id] as const;

/** Store the server's fresh copy of the user and mark the list stale. */
export function useApplyUser(id: string) {
  const qc = useQueryClient();
  return useCallback(
    (user: UserDetail) => {
      qc.setQueryData(userKey(id), user);
      void qc.invalidateQueries({ queryKey: ['platform', 'users'] });
    },
    [qc, id]
  );
}
