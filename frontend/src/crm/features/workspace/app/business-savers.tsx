import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Bookmark, ChevronRight, Users } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AppBusiness } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { appKeys, appUserPath, formatDate, formatPhone, initials } from './app-utils';

/** Lead status of a business: card-created (lead → converted) or owner-registered. */
export function LeadStatusBadge({ biz }: { biz: Pick<AppBusiness, 'leadStatus' | 'contactPhone'> }) {
  const { t } = useTranslation();
  const tone = biz.leadStatus === 'lead' ? 'primary' : biz.leadStatus === 'converted' ? 'success' : 'neutral';
  return (
    <Badge tone={tone} title={t(`workspaceApp.app.leadStatusHint.${biz.leadStatus}`, { phone: formatPhone(biz.contactPhone) || '—' })}>
      {t(`workspaceApp.app.leadStatus.${biz.leadStatus}`)}
    </Badge>
  );
}

/** "Saved by N" — opens the list of people who saved the card. */
export function SavedByButton({ biz, code, canUsers, className }: { biz: AppBusiness; code: string; canUsers: boolean; className?: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  if (!biz.savedBy) return null;
  return (
    <>
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen(true);
        }}
        className={cn(
          'relative z-10 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          className
        )}
        aria-label={t('workspaceApp.app.savedByButton', { count: biz.savedBy })}
        title={t('workspaceApp.app.savedByButton', { count: biz.savedBy })}
      >
        <Bookmark className="size-3.5" aria-hidden /> <span className="tabular-nums">{biz.savedBy}</span>
      </button>
      <SaversDialog biz={biz} code={code} canUsers={canUsers} open={open} onOpenChange={setOpen} />
    </>
  );
}

export function SaversDialog({
  biz,
  code,
  canUsers,
  open,
  onOpenChange
}: {
  biz: AppBusiness;
  code: string;
  canUsers: boolean;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: [...appKeys.business(code, biz.id), 'savers'],
    queryFn: () => workspaceAppApi(code).savers(biz.id),
    enabled: open
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 pr-8 text-base font-semibold">
            <Users className="size-4 text-primary" aria-hidden />
            {t('workspaceApp.app.saversTitle', { name: biz.name })}
          </DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('workspaceApp.app.saversDescription')}</DialogDescription>
        </div>
        <div className="max-h-[60vh] overflow-y-auto">
          {q.isError ? (
            <ErrorState title={t('workspaceApp.app.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
          ) : !q.data ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: Math.min(biz.savedBy, 4) }).map((_, i) => (
                <Skeleton key={i} className="h-11" />
              ))}
            </div>
          ) : q.data.length === 0 ? (
            <p className="px-5 py-8 text-center text-[13px] text-muted-foreground">{t('workspaceApp.app.saversEmpty')}</p>
          ) : (
            <ul className="divide-y">
              {q.data.map((s) => {
                const label = s.name || formatPhone(s.phone);
                const body = (
                  <>
                    <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary-soft text-[11px] font-semibold text-primary">
                      {s.name ? initials(s.name) : '#'}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px] font-medium text-foreground">{label}</span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {[s.name ? formatPhone(s.phone) : '', s.city, t('workspaceApp.app.savedOn', { date: formatDate(s.savedAt) })]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </span>
                  </>
                );
                return (
                  <li key={s.userId}>
                    {canUsers ? (
                      <Link
                        to={appUserPath(code, s.userId)}
                        onClick={() => onOpenChange(false)}
                        className="group flex items-center gap-3 px-5 py-2.5 hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
                      >
                        {body}
                        <ChevronRight className="size-4 text-muted-foreground group-hover:text-foreground" aria-hidden />
                      </Link>
                    ) : (
                      <div className="flex items-center gap-3 px-5 py-2.5">{body}</div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
