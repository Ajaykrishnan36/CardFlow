import { useTranslation } from 'react-i18next';
import { CreditCard, HelpCircle, ScanLine, Store, type LucideIcon } from 'lucide-react';
import type { TicketStatus } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { humanize } from '@crm/features/records/use-object-meta';
import { cn } from '@crm/lib/utils';
import { workspaceBase } from '../workspace-context';

// Support tickets raised in a connected app (e.g. Business Card Snap), answered from the CRM.

export const supportKeys = {
  all: (code: string) => ['workspace', code, 'support'] as const,
  list: (code: string, params: { q?: string; status?: string }) => ['workspace', code, 'support', 'list', params] as const,
  ticket: (code: string, id: string) => ['workspace', code, 'support', 'ticket', id] as const
};

/** New app tickets should show up on their own. */
export const SUPPORT_REFRESH_MS = 15_000;

export const TICKET_STATUSES: TicketStatus[] = ['open', 'in_progress', 'resolved'];

/** App tickets are Cases (D-72): the case page, or the Cases list while it's being created. */
export function supportPath(code: string, _ticketId?: string, caseId?: string): string {
  return `${workspaceBase(code)}/cases${caseId ? `/${encodeURIComponent(caseId)}` : ''}`;
}

const statusTone: Record<TicketStatus, 'primary' | 'warning' | 'success'> = { open: 'primary', in_progress: 'warning', resolved: 'success' };

export function TicketStatusBadge({ status }: { status: TicketStatus }) {
  const { t } = useTranslation();
  return <Badge tone={statusTone[status] ?? 'neutral'}>{t(`workspaceApp.support.status.${status}`, { defaultValue: humanize(status) })}</Badge>;
}

const categoryIcons: Record<string, LucideIcon> = { billing: CreditCard, card_scan: ScanLine, business_listing: Store, general: HelpCircle };

export function CategoryChip({ category, className }: { category: string; className?: string }) {
  const { t } = useTranslation();
  const Icon = categoryIcons[category] ?? HelpCircle;
  return (
    <span className={cn('inline-flex items-center gap-1 whitespace-nowrap rounded-md border bg-muted/50 px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground', className)}>
      <Icon className="size-3" aria-hidden />
      {t(`workspaceApp.support.category.${category}`, { defaultValue: humanize(category) })}
    </span>
  );
}

/** "t-1a2b3c4d" → "#1a2b3c4d" */
export function ticketNumber(id: string): string {
  return `#${id.replace(/^t-/, '')}`;
}
