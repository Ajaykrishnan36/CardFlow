import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { productsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Textarea } from '@crm/components/ui/form-controls';
import { Alert } from '@crm/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { PRODUCT_KEY_RE, slugKey } from './product-utils';

interface Errors {
  name?: string;
  key?: string;
  description?: string;
  form?: string;
}

export function CreateProductDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-0">
        {/* Remount on every open so the form starts clean. */}
        {open ? <CreateProductForm onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function CreateProductForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [name, setName] = useState('');
  const [key, setKey] = useState('');
  const [keyEdited, setKeyEdited] = useState(false);
  const [description, setDescription] = useState('');
  const [errors, setErrors] = useState<Errors>({});

  const create = useMutation({
    mutationFn: productsApi.create,
    onSuccess: (p) => {
      qc.setQueryData(['product', p.id], p);
      void qc.invalidateQueries({ queryKey: ['products'] });
      toast.success(t('products.create.created'));
      onDone();
      navigate(`/crm/owner/products/${p.id}?tab=setup`);
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        const fe = e.fieldErrors;
        const known = fe.name || fe.key || fe.description;
        setErrors({ name: fe.name, key: fe.key, description: fe.description, form: known ? undefined : e.message });
        return;
      }
      if (isApiError(e) && e.code === 'key_taken') {
        setErrors({ key: e.message });
        return;
      }
      setErrors({ form: isApiError(e) ? e.message : t('common.genericError') });
    }
  });

  const onNameChange = (v: string) => {
    setName(v);
    if (!keyEdited) setKey(slugKey(v));
    if (errors.name) setErrors((er) => ({ ...er, name: undefined }));
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next: Errors = {};
    if (!name.trim()) next.name = t('products.create.nameRequired');
    if (!PRODUCT_KEY_RE.test(key)) next.key = t('products.create.keyInvalid');
    setErrors(next);
    if (next.name || next.key) return;
    create.mutate({ name: name.trim(), key, description: description.trim() || undefined, icon: 'boxes' });
  };

  return (
    <form onSubmit={onSubmit} noValidate>
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{t('products.create.title')}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('products.create.subtitle')}</DialogDescription>
      </div>
      <div className="space-y-4 px-5 py-5">
        {errors.form ? <Alert tone="danger">{errors.form}</Alert> : null}
        <Field label={t('products.create.name')} error={errors.name}>
          <Input value={name} onChange={(e) => onNameChange(e.target.value)} placeholder={t('products.create.namePlaceholder')} autoFocus maxLength={80} />
        </Field>
        <Field label={t('products.create.key')} error={errors.key} hint={t('products.create.keyHint')}>
          <Input
            value={key}
            onChange={(e) => {
              setKeyEdited(true);
              setKey(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ''));
              if (errors.key) setErrors((er) => ({ ...er, key: undefined }));
            }}
            className="font-mono"
            placeholder="real_estate_crm"
            maxLength={41}
            autoCapitalize="none"
            spellCheck={false}
          />
        </Field>
        <Field label={t('products.create.descriptionLabel')} error={errors.description}>
          <Textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder={t('products.create.descriptionPlaceholder')} rows={3} />
        </Field>
      </div>
      <div className="flex justify-end gap-2 border-t px-5 py-3.5">
        <Button type="button" variant="outline" onClick={onDone} disabled={create.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={create.isPending}>
          {create.isPending ? t('products.create.submitting') : t('products.create.submit')}
        </Button>
      </div>
    </form>
  );
}
