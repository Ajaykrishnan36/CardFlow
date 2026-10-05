import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, CheckCircle2, Circle, Sparkles, X } from 'lucide-react';
import { api } from '@crm/api/client';
import { Card } from '@crm/components/ui/card';
import { cn } from '@crm/lib/utils';
import { workspaceBase } from './workspace-context';

interface Step {
  key: string;
  title: string;
  body: string;
  done: boolean;
  go: 'lead' | 'scan' | 'task' | 'business' | 'team' | 'account';
}

function remembered(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
}

/**
 * "Getting started" on the dashboard of a new business (D-109): the first things to do,
 * ticked from the business's real data. Gone once everything is done or it is hidden.
 */
export function GettingStartedPanel({ code }: { code: string }) {
  const hideKey = `cf_start_hidden_${code}`;
  const [hidden, setHidden] = useState(() => remembered(hideKey));
  const q = useQuery({
    queryKey: ['workspace', code, 'getting-started'],
    queryFn: () => api<{ steps: Step[]; done: number; total: number }>(`/w/${encodeURIComponent(code)}/getting-started`),
    staleTime: 30_000,
    enabled: !hidden
  });
  const d = q.data;
  if (hidden || !d || d.total === 0 || d.done >= d.total) return null;
  const base = workspaceBase(code);
  const href: Record<Step['go'], string> = {
    lead: `${base}/leads?new=1`,
    scan: `${base}/cards`,
    task: `${base}/tasks?new=1`,
    business: `${base}/settings/business`,
    team: `${base}/settings/access`,
    account: '/crm/me'
  };
  return (
    <Card className="border-primary/40 p-5" aria-label="Getting started">
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
          <Sparkles className="size-4" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[15px] font-semibold text-foreground">Getting started</h2>
          <p className="text-[13px] text-muted-foreground">
            {d.done} of {d.total} done. A few first steps to get value from your CRM.
          </p>
        </div>
        <button
          type="button"
          className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          aria-label="Hide getting started"
          onClick={() => {
            try {
              window.localStorage.setItem(hideKey, '1');
            } catch {
              // not remembered on this device
            }
            setHidden(true);
          }}
        >
          <X className="size-4" />
        </button>
      </div>
      <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${Math.round((d.done / d.total) * 100)}%` }} />
      </div>
      <ul className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {d.steps.map((s) => (
          <li key={s.key}>
            {s.done ? (
              <div className="flex h-full items-start gap-2.5 rounded-lg border border-dashed px-3 py-2.5 text-muted-foreground">
                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
                <span className="text-[13px] font-medium line-through">{s.title}</span>
              </div>
            ) : (
              <Link
                to={href[s.go]}
                className={cn(
                  'group flex h-full items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:border-primary/50 hover:bg-muted/40',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                )}
              >
                <Circle className="mt-0.5 size-4 shrink-0 text-muted-foreground/60" aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className="block text-[13px] font-semibold text-foreground">{s.title}</span>
                  <span className="mt-0.5 block text-xs leading-relaxed text-muted-foreground">{s.body}</span>
                </span>
                <ArrowRight className="mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary" aria-hidden />
              </Link>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}
