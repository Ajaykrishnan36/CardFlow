import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { FieldValues, Path, UseFormSetError } from 'react-hook-form';
import { isApiError } from '@crm/api/client';

export interface FormAlert {
  message: string;
  requestId?: string;
}

/**
 * Maps API errors onto a form: 422 → per-field messages, everything else → one alert.
 * Never shows success before the server confirms (PRD FE-02).
 */
export function useFormError<T extends FieldValues>(setError: UseFormSetError<T>, fields: Array<Path<T>>) {
  const { t } = useTranslation();
  const [alert, setAlert] = useState<FormAlert | null>(null);

  const handle = (e: unknown) => {
    if (!isApiError(e)) {
      setAlert({ message: t('common.genericError') });
      return;
    }
    if (e.status === 422 && Object.keys(e.fieldErrors).length > 0) {
      let unmatched = '';
      for (const [key, msg] of Object.entries(e.fieldErrors)) {
        if ((fields as string[]).includes(key)) setError(key as Path<T>, { type: 'server', message: msg });
        else unmatched = msg;
      }
      setAlert(unmatched ? { message: unmatched } : null);
      return;
    }
    if (e.status === 429 && e.retryAfter) {
      setAlert({ message: t('common.tooManyAttempts', { count: Math.max(1, Math.ceil(e.retryAfter / 60)) }) });
      return;
    }
    setAlert({ message: e.message, requestId: e.status >= 500 ? e.requestId : undefined });
  };

  return { alert, setAlert, handle };
}
