import { useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Check, Copy } from 'lucide-react';
import { workspaceToolsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ObjectKey } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Badge } from '@crm/components/ui/card';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { useRecordScope } from '@crm/features/records/record-scope';
import { LookupCombobox } from '@crm/features/records/field-input';
import { cn } from '@crm/lib/utils';

// Shared pieces of the workspace tool pages: Workflows, Campaigns, Email & calendar,
// Teams, Single sign-on and API & webhooks.

export function useTools() {
  const { code } = useWorkspace();
  return useMemo(() => workspaceToolsApi(code), [code]);
}

export const toolKeys = {
  all: (code: string) => ['workspace', code, 'tools'] as const,
  one: (code: string, ...parts: string[]) => ['workspace', code, 'tools', ...parts] as const
};

/** Objects this member can open, as the sidebar lists them (key = route segment). */
export function useWorkspaceObjects(): Array<{ key: ObjectKey; label: string }> {
  const { context } = useWorkspace();
  return useMemo(
    () =>
      context.navigation
        .filter((n) => n.group === 'CRM' && n.key !== 'support')
        .map((n) => ({ key: n.path.split('/').pop() as ObjectKey, label: n.label })),
    [context.navigation]
  );
}

/** Field metadata of an object in this workspace (for pickers). */
export function useObjectMeta(object: string | undefined) {
  const scope = useRecordScope();
  return useQuery({
    queryKey: ['records', scope.prefix, object, 'meta'],
    queryFn: () => scope.api.meta(object as ObjectKey),
    enabled: Boolean(object),
    staleTime: 60_000
  });
}

export function hasCap(key: string): (ctx: ReturnType<typeof useWorkspace>['context']) => boolean {
  return (ctx) => Boolean(ctx.viewerIsOwner) || ctx.effective.capabilities.some((c) => c.key === key);
}

export function errorText(e: unknown, fallback: string): string {
  if (!isApiError(e)) return fallback;
  const first = Object.values(e.fieldErrors)[0];
  return first ? `${e.message} ${first}` : e.message;
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      className={className}
      onClick={() => {
        void navigator.clipboard?.writeText(value).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        });
      }}
    >
      {copied ? <Check /> : <Copy />} {copied ? t('tools.common.copied') : (label ?? t('tools.common.copy'))}
    </Button>
  );
}

/** A value to copy, like a URL or an ID, on one line. */
export function CopyField({ label, value, hint }: { label: string; value: string; hint?: ReactNode }) {
  return (
    <div className="min-w-0">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <div className="mt-1 flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-2.5 py-1.5 font-mono text-xs text-foreground">{value}</code>
        <CopyButton value={value} />
      </div>
      {hint ? <p className="mt-1 text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** A secret shown once, right after it's created. */
export function SecretOnce({ title, secret, body }: { title: string; secret: string; body: string }) {
  return (
    <div className="rounded-lg border border-warning/40 bg-warning-soft px-3.5 py-3">
      <p className="text-[13px] font-semibold text-foreground">{title}</p>
      <p className="mt-0.5 text-xs text-muted-foreground">{body}</p>
      <div className="mt-2 flex items-center gap-2">
        <code className="min-w-0 flex-1 break-all rounded-md border bg-background px-2.5 py-1.5 font-mono text-xs">{secret}</code>
        <CopyButton value={secret} />
      </div>
    </div>
  );
}

export function JsonBlock({ value, className }: { value: unknown; className?: string }) {
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return <pre className={cn('max-h-80 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-[11px] leading-5 text-foreground', className)}>{text}</pre>;
}

const tones: Record<string, 'neutral' | 'primary' | 'success' | 'warning' | 'danger'> = {
  active: 'success',
  completed: 'success',
  delivered: 'success',
  sent: 'success',
  ok: 'success',
  draft: 'neutral',
  inactive: 'neutral',
  paused: 'neutral',
  skipped: 'neutral',
  cancelled: 'neutral',
  stopped: 'neutral',
  queued: 'primary',
  running: 'primary',
  sending: 'primary',
  scheduled: 'primary',
  pending: 'primary',
  waiting: 'warning',
  unsubscribed: 'warning',
  failed: 'danger',
  error: 'danger'
};

export function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  return <Badge tone={tones[status] ?? 'neutral'}>{t(`tools.status.${status}`, { defaultValue: status })}</Badge>;
}

export function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

/** Section row inside a settings card: label + help on the left, control on the right. */
export function SettingRow({ title, body, children }: { title: ReactNode; body?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0">
        <p className="text-[13px] font-medium text-foreground">{title}</p>
        {body ? <p className="text-xs text-muted-foreground">{body}</p> : null}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

/** Pick several workspace members (identity ids), shown as removable chips. */
export function PeoplePicker({
  value,
  onChange,
  placeholder
}: {
  value: Array<{ id: string; label: string }>;
  onChange: (v: Array<{ id: string; label: string }>) => void;
  placeholder?: string;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-2">
      {value.length ? (
        <ul className="flex flex-wrap gap-1.5">
          {value.map((p) => (
            <li key={p.id} className="inline-flex items-center gap-1 rounded-full border bg-muted/50 py-0.5 pl-2.5 pr-1 text-xs">
              {p.label}
              <button
                type="button"
                className="grid size-5 place-items-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground"
                onClick={() => onChange(value.filter((x) => x.id !== p.id))}
                aria-label={t('tools.common.removeName', { name: p.label })}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <LookupCombobox
        target="users"
        value={null}
        placeholder={placeholder ?? t('tools.common.addPerson')}
        onChange={(v) => v && !value.some((x) => x.id === v.id) && onChange([...value, { id: v.id, label: v.label.split(' · ')[0] }])}
      />
    </div>
  );
}
