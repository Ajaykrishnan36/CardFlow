import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Minus } from 'lucide-react';
import type { AccessCatalog, ObjectAction, RowScope } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { cn } from '@crm/lib/utils';

export interface MatrixRow {
  key: string;
  label: string;
  module: string;
  supported: ObjectAction[];
  /** Greyed out (e.g. module not in the user's products). */
  muted?: boolean;
  /** Control before the label (row select-all). */
  lead?: ReactNode;
  /** Extra content under the label (badges, sources). */
  aside?: ReactNode;
  /** "Records" column: scope select or badge. */
  scope: ReactNode;
}

export type CellVariant = 'table' | 'card';

/**
 * Objects × actions grid shared by the editor and the read-only views.
 * Table with a sticky header from 640px; one card per object below.
 */
export function AccessMatrix({
  catalog,
  rows,
  renderCell,
  label,
  dense,
  className
}: {
  catalog: AccessCatalog;
  rows: MatrixRow[];
  /** Narrow columns for read-only marks. */
  dense?: boolean;
  renderCell: (row: MatrixRow, action: ObjectAction, variant: CellVariant) => ReactNode;
  /** Accessible name of the table. */
  label: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const actions = catalog.actions;
  return (
    <div className={className}>
      <table className="hidden w-full table-fixed border-separate border-spacing-0 rounded-lg border text-[13px] sm:table" aria-label={label}>
        <colgroup>
          <col />
          {actions.map((a) => (
            <col key={a.key} className={dense ? 'w-[44px]' : 'w-[56px]'} />
          ))}
          <col className={dense ? 'w-[124px]' : 'w-[156px]'} />
        </colgroup>
        <thead>
          <tr className="text-[11px] font-medium text-muted-foreground">
            <th scope="col" className="sticky top-0 z-[1] rounded-tl-lg border-b bg-muted px-3 py-2 text-left font-medium">
              {t('access.matrix.object')}
            </th>
            {actions.map((a) => (
              <th key={a.key} scope="col" className="sticky top-0 z-[1] border-b bg-muted px-1 py-2 text-center font-medium">
                <span className="block truncate" title={a.label}>
                  {a.label}
                </span>
              </th>
            ))}
            <th scope="col" className="sticky top-0 z-[1] rounded-tr-lg border-b bg-muted px-3 py-2 text-left font-medium">
              {t('access.matrix.records')}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => {
            const last = i === rows.length - 1;
            const cell = cn('px-1 py-2 text-center align-middle', !last && 'border-b');
            return (
              <tr key={row.key} className={cn(row.muted && 'bg-muted/30')}>
                <th scope="row" className={cn('px-3 py-2 text-left align-middle font-normal', !last && 'border-b')}>
                  <div className="flex items-start gap-2.5">
                    {row.lead ? <span className="mt-0.5">{row.lead}</span> : null}
                    <div className="min-w-0">
                      <p className={cn('truncate font-medium', row.muted ? 'text-muted-foreground' : 'text-foreground')}>{row.label}</p>
                      <p className="truncate text-[11px] text-muted-foreground">{t('access.matrix.module', { module: row.module })}</p>
                      {row.aside ? <div className="mt-1">{row.aside}</div> : null}
                    </div>
                  </div>
                </th>
                {actions.map((a) => (
                  <td key={a.key} className={cell}>
                    {row.supported.includes(a.key) ? (
                      renderCell(row, a.key, 'table')
                    ) : (
                      <span className="text-muted-foreground/50" title={t('access.matrix.notSupported')}>
                        —<span className="sr-only">{t('access.matrix.notSupported')}</span>
                      </span>
                    )}
                  </td>
                ))}
                <td className={cn('px-3 py-2 align-middle', !last && 'border-b')}>{row.scope}</td>
              </tr>
            );
          })}
        </tbody>
      </table>

      <ul className="space-y-2 sm:hidden" aria-label={label}>
        {rows.map((row) => (
          <li key={row.key} className={cn('rounded-lg border p-3', row.muted && 'bg-muted/30')}>
            <div className="flex items-start gap-2.5">
              {row.lead ? <span className="mt-0.5">{row.lead}</span> : null}
              <div className="min-w-0 flex-1">
                <p className={cn('text-[13px] font-medium', row.muted ? 'text-muted-foreground' : 'text-foreground')}>{row.label}</p>
                <p className="text-[11px] text-muted-foreground">{t('access.matrix.module', { module: row.module })}</p>
                {row.aside ? <div className="mt-1">{row.aside}</div> : null}
              </div>
            </div>
            <div className="mt-2.5 grid grid-cols-2 gap-x-3 gap-y-2 min-[400px]:grid-cols-3">
              {actions
                .filter((a) => row.supported.includes(a.key))
                .map((a) => (
                  <div key={a.key}>{renderCell(row, a.key, 'card')}</div>
                ))}
            </div>
            <div className="mt-2.5 border-t pt-2.5">{row.scope}</div>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** ✓ / — for read-only matrices. */
export function GrantMark({ on, label, variant, muted }: { on: boolean; label: string; variant: CellVariant; muted?: boolean }) {
  const { t } = useTranslation();
  const icon = on ? (
    <Check className={cn('size-4', muted ? 'text-muted-foreground' : 'text-success')} strokeWidth={2.5} aria-hidden />
  ) : (
    <Minus className="size-3.5 text-muted-foreground/50" aria-hidden />
  );
  if (variant === 'card') {
    return (
      <span className={cn('flex items-center gap-1.5 text-[13px]', on && !muted ? 'text-foreground' : 'text-muted-foreground')}>
        {icon}
        {label}
        <span className="sr-only">: {on ? t('access.matrix.granted') : t('access.matrix.notGranted')}</span>
      </span>
    );
  }
  return (
    <span className="inline-grid place-items-center" title={`${label}: ${on ? t('access.matrix.granted') : t('access.matrix.notGranted')}`}>
      {icon}
      <span className="sr-only">
        {label}: {on ? t('access.matrix.granted') : t('access.matrix.notGranted')}
      </span>
    </span>
  );
}

export function ScopeBadge({ scope, none }: { scope: RowScope; none?: boolean }) {
  const { t } = useTranslation();
  if (none) return <Badge>{t('access.matrix.scope.none')}</Badge>;
  return <Badge tone={scope === 'workspace' ? 'primary' : 'neutral'}>{t(`access.matrix.scope.${scope}`)}</Badge>;
}

/** Checkbox with an indeterminate state (row "select all"). Styled like the form Checkbox. */
export function TriCheckbox({
  checked,
  indeterminate,
  onCheckedChange,
  disabled,
  'aria-label': ariaLabel
}: {
  checked: boolean;
  indeterminate?: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  'aria-label': string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = Boolean(indeterminate) && !checked;
  }, [indeterminate, checked]);
  const mixed = Boolean(indeterminate) && !checked;
  return (
    <span className="relative grid size-4 shrink-0 place-items-center">
      <input
        ref={ref}
        type="checkbox"
        aria-label={ariaLabel}
        checked={checked}
        disabled={disabled}
        onChange={(e) => onCheckedChange(mixed ? true : e.target.checked)}
        className={cn(
          'peer absolute inset-0 cursor-pointer appearance-none rounded border border-input bg-background shadow-sm transition-colors checked:border-primary checked:bg-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 disabled:cursor-not-allowed disabled:opacity-60',
          mixed && 'border-primary bg-primary'
        )}
      />
      {mixed ? (
        <Minus className="pointer-events-none relative size-3 text-primary-foreground" strokeWidth={3} aria-hidden />
      ) : (
        <Check className="pointer-events-none relative size-3 text-primary-foreground opacity-0 peer-checked:opacity-100" strokeWidth={3} aria-hidden />
      )}
    </span>
  );
}
