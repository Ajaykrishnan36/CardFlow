import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Activity, ArrowRight, KeyRound, LogIn, LogOut, Pencil, ShieldCheck, TriangleAlert, UserRoundX, type LucideIcon } from 'lucide-react';
import type { AuditEntry, UserDetail } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Card, CardHeader } from '@crm/components/ui/card';
import { Tooltip } from '@crm/components/ui/menu';
import { EmptyState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { absoluteTime, actionTone, humanizeAction, type ActionTone } from '@crm/features/audit/audit-format';

const toneClass: Record<ActionTone, string> = {
  success: 'bg-success-soft text-success',
  danger: 'bg-danger-soft text-danger',
  warning: 'bg-warning-soft text-warning',
  primary: 'bg-primary-soft text-primary',
  neutral: 'bg-muted text-muted-foreground'
};

function actionIcon(action: string): LucideIcon {
  if (/failed|locked/.test(action)) return TriangleAlert;
  if (/^auth\.login/.test(action)) return LogIn;
  if (/logout|sessions?_revoked|session\.revoked/.test(action)) return LogOut;
  if (/mfa/.test(action)) return ShieldCheck;
  if (/password/.test(action)) return KeyRound;
  if (/suspend/.test(action)) return UserRoundX;
  if (/updated|changed/.test(action)) return Pencil;
  return Activity;
}

export function UserActivityTab({ user }: { user: UserDetail }) {
  const { t } = useTranslation();
  const entries = user.recentActivity;
  return (
    <Card className="min-w-0">
      <CardHeader
        title={t('users.activity.title')}
        description={t('users.activity.description')}
        actions={
          <Button asChild variant="ghost" size="sm">
            <Link to={`/crm/owner/audit?entityId=${encodeURIComponent(user.id)}`}>
              {t('users.activity.fullLog')} <ArrowRight />
            </Link>
          </Button>
        }
      />
      {entries.length === 0 ? (
        <EmptyState icon={Activity} title={t('users.activity.empty')} className="py-10" />
      ) : (
        <ol className="px-5 py-4">
          {entries.map((e, i) => (
            <TimelineItem key={e.id} entry={e} last={i === entries.length - 1} subjectName={user.displayName} />
          ))}
        </ol>
      )}
    </Card>
  );
}

function TimelineItem({ entry: e, last, subjectName }: { entry: AuditEntry; last: boolean; subjectName: string }) {
  const { t } = useTranslation();
  const Icon = actionIcon(e.action);
  const byOther = e.actorName && e.actorName !== subjectName;
  const meta = [e.workspaceName, e.ip].filter(Boolean);
  return (
    <li className={cn('relative flex gap-3', !last && 'pb-5')}>
      {!last ? <span className="absolute bottom-0 left-[13px] top-8 w-px bg-border" aria-hidden /> : null}
      <span className={cn('relative grid size-7 shrink-0 place-items-center rounded-full ring-4 ring-card', toneClass[actionTone(e.action)])}>
        <Icon className="size-3.5" aria-hidden />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <p className="text-[13px] leading-5 text-foreground">
          <span className="font-medium">{humanizeAction(t, e.action)}</span>
          {byOther ? <span className="text-muted-foreground"> {t('users.activity.by', { actor: e.actorName })}</span> : null}
        </p>
        <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
          <Tooltip content={absoluteTime(e.createdAt)} side="top">
            <time dateTime={e.createdAt} tabIndex={0} className="rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              {relativeTime(e.createdAt)}
            </time>
          </Tooltip>
          {meta.map((m) => (
            <span key={m} className="flex items-center gap-1.5">
              <span aria-hidden>·</span>
              <span className={m === e.ip ? 'font-mono text-[11px]' : undefined}>{m}</span>
            </span>
          ))}
        </p>
      </div>
    </li>
  );
}
