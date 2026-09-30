import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { objectsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { FieldType, ObjectDefinition } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';
import { navIcon } from '@crm/features/shell/nav-icons';

export const objectKeys = {
  all: ['platform', 'objects'] as const,
  one: (key: string) => ['platform', 'objects', key] as const
};

export { CREATABLE_TYPES as FIELD_TYPES } from '@crm/features/records/field-dialog';

export const UNIQUE_FIELD_TYPES = new Set<FieldType>(['text', 'email', 'phone', 'url', 'number']);
export const isLinkField = (t: FieldType) => t === 'lookup' || t === 'relations';

/** English plural for a suggested label: Property → Properties, Class → Classes. */
export function pluralize(word: string): string {
  const w = word.trimEnd();
  if (!w) return '';
  if (/[^aeiou]y$/i.test(w)) return `${w.slice(0, -1)}ies`;
  if (/(s|x|z|ch|sh)$/i.test(w)) return `${w}es`;
  return `${w}s`;
}

export function ObjectIcon({ icon, className }: { icon: string; className?: string }) {
  const Icon = navIcon(icon);
  return (
    <span className={cn('grid size-10 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary', className)}>
      <Icon className="size-5" aria-hidden />
    </span>
  );
}

export function IconPicker({ icons, value, onChange }: { icons: string[]; value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation();
  return (
    <div role="radiogroup" aria-label={t('objects.icon')} className="grid grid-cols-8 gap-1.5 sm:grid-cols-10">
      {icons.map((key) => {
        const Icon = navIcon(key);
        return (
          <button
            key={key}
            type="button"
            role="radio"
            aria-checked={value === key}
            aria-label={key}
            title={key}
            onClick={() => onChange(key)}
            className={cn(
              'grid aspect-square place-items-center rounded-md border transition-colors',
              value === key ? 'border-primary bg-primary-soft text-primary ring-2 ring-primary/20' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            <Icon className="size-4" aria-hidden />
          </button>
        );
      })}
    </div>
  );
}

export function fieldErrorText(e: unknown, fallback: string) {
  if (!isApiError(e)) return fallback;
  return Object.values(e.fieldErrors)[0] ?? e.message;
}

/** Create an object: name, icon and description. Fields are added on its page right after. */
export function NewObjectDialog({
  open,
  onOpenChange,
  icons,
  onCreated,
  navigateOnCreate = true
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  icons: string[];
  onCreated?: (d: ObjectDefinition) => void;
  navigateOnCreate?: boolean;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [singular, setSingular] = useState('');
  const [plural, setPlural] = useState('');
  const [pluralTouched, setPluralTouched] = useState(false);
  const [description, setDescription] = useState('');
  const [icon, setIcon] = useState('box');
  const create = useMutation({
    mutationFn: () => objectsApi.create({ singular: singular.trim(), plural: plural.trim() || undefined, description: description.trim(), icon }),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: objectKeys.all });
      void qc.invalidateQueries({ queryKey: ['products'] });
      toast.success(t('objects.createdToast', { name: d.plural }));
      onOpenChange(false);
      setSingular('');
      setPlural('');
      setPluralTouched(false);
      setDescription('');
      setIcon('box');
      onCreated?.(d);
      if (navigateOnCreate) navigate(`/crm/owner/objects/${encodeURIComponent(d.key)}`);
    }
  });
  const fe = isApiError(create.error) ? create.error.fieldErrors : {};
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!singular.trim()) return;
    create.mutate();
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('objects.newTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('objects.newBody')}</DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('objects.singular')} error={fe.singular} hint={t('objects.singularHint')}>
              <Input
                value={singular}
                onChange={(e) => {
                  setSingular(e.target.value);
                  if (!pluralTouched) setPlural(pluralize(e.target.value));
                }}
                maxLength={60}
                autoFocus
                placeholder={t('objects.singularPlaceholder')}
              />
            </Field>
            <Field label={t('objects.plural')} error={fe.plural ?? fe.key}>
              <Input
                value={plural}
                onChange={(e) => {
                  setPlural(e.target.value);
                  setPluralTouched(true);
                }}
                maxLength={60}
                placeholder={t('objects.pluralPlaceholder')}
              />
            </Field>
          </div>
          <Field label={t('objects.description')} error={fe.description}>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} maxLength={300} />
          </Field>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">{t('objects.icon')}</p>
            <IconPicker icons={icons} value={icon} onChange={setIcon} />
          </div>
          {create.isError && Object.keys(fe).length === 0 ? <p className="text-sm text-danger">{fieldErrorText(create.error, t('common.genericError'))}</p> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={create.isPending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!singular.trim()}>
              {t('objects.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
