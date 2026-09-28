import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, ArrowRight, Check, CircleCheck, Save } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { ProductConfig, ProductDetail } from '@crm/api/types';
import { Alert, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { cn } from '@crm/lib/utils';
import { nextVersion, SETUP_STEPS, stepForField, type SetupStep } from './product-utils';
import type { ProductDraft } from './use-product-draft';
import { GeneralStep, LoginStep, ModulesStep, PipelineStep, ReviewStep, RolesStep } from './setup-steps';

type SaveResult = { ok: true } | { ok: false; error: unknown };

interface WizardProps {
  product: ProductDetail;
  draft: ProductDraft;
  setDraft: Dispatch<SetStateAction<ProductDraft>>;
  updateConfig: (patch: Partial<ProductConfig>) => void;
  dirty: boolean;
  step: SetupStep;
  onStepChange: (step: SetupStep) => void;
  onSave: () => Promise<SaveResult>;
  saving: boolean;
  publishErrors: Record<string, string> | null;
  publishable: boolean;
  publishing: boolean;
  onPublish: () => void;
}

/** Whether a step has the minimum a publish needs (drives the stepper check marks). */
function stepComplete(step: SetupStep, d: ProductDraft): boolean {
  switch (step) {
    case 'general':
      return d.name.trim().length > 0;
    case 'modules':
      return d.config.modules.length > 0;
    case 'roles':
      return d.config.userTypes.length > 0 && d.config.roles.some((r) => r.enabled);
    case 'pipeline':
      return d.config.leadStatuses.length > 0 && d.config.pipelineStages.length > 0;
    case 'login':
      return d.config.loginMethods.password;
    default:
      return false;
  }
}

/** PRD product setup: General → Modules & data → Roles & user types → Pipeline & conversion → Login & integrations → Review & publish. */
export function ProductSetupWizard(props: WizardProps) {
  const { product, draft, setDraft, updateConfig, dirty, step, onStepChange, onSave, saving, publishErrors, publishable, publishing, onPublish } = props;
  const { t } = useTranslation();
  const index = SETUP_STEPS.indexOf(step);
  const [saveError, setSaveError] = useState<{ message: string; fields: Record<string, string> } | null>(null);
  const [nameError, setNameError] = useState<string | undefined>();
  const topRef = useRef<HTMLDivElement>(null);

  const errorSteps = new Set(Object.keys(publishErrors ?? {}).map(stepForField));

  // Moving between steps: clear stale save errors and bring the step header into view.
  useEffect(() => {
    setSaveError(null);
    topRef.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [step]);

  const saveAndContinue = async () => {
    if (!draft.name.trim()) {
      setNameError(t('products.create.nameRequired'));
      onStepChange('general');
      return;
    }
    setNameError(undefined);
    setSaveError(null);
    // Nothing changed since the last save: just move on.
    const res: SaveResult = dirty ? await onSave() : { ok: true };
    if (res.ok) {
      const next = SETUP_STEPS[index + 1];
      if (next) onStepChange(next);
      return;
    }
    const e = res.error;
    if (isApiError(e)) {
      if (e.fieldErrors.name) setNameError(e.fieldErrors.name);
      setSaveError({ message: e.message, fields: e.fieldErrors });
    } else {
      setSaveError({ message: t('common.genericError'), fields: {} });
    }
  };

  const title = t(`products.setup.steps.${step}.title`);
  const description = t(`products.setup.steps.${step}.description`);

  return (
    <div ref={topRef} className="grid scroll-mt-4 gap-4 lg:grid-cols-[232px_minmax(0,1fr)] lg:gap-6">
      {/* Stepper: horizontal scroller below lg, vertical rail at lg+. */}
      <nav aria-label={t('products.setup.stepsLabel')} className="min-w-0 lg:sticky lg:top-4 lg:self-start">
        <ol className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 sm:mx-0 sm:px-0 lg:flex-col lg:gap-0.5 lg:overflow-visible">
          {SETUP_STEPS.map((s, i) => {
            const active = s === step;
            const complete = s !== 'review' && stepComplete(s, draft);
            const hasError = errorSteps.has(s);
            return (
              <li key={s} className="shrink-0">
                <button
                  type="button"
                  onClick={() => onStepChange(s)}
                  aria-current={active ? 'step' : undefined}
                  className={cn(
                    'flex w-full items-center gap-2.5 whitespace-nowrap rounded-md px-2.5 py-2 text-left text-[13px] transition-colors',
                    active ? 'bg-primary-soft font-medium text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  )}
                >
                  <span
                    className={cn(
                      'grid size-6 shrink-0 place-items-center rounded-full border text-[11px] font-semibold tabular-nums',
                      hasError
                        ? 'border-danger bg-danger-soft text-danger'
                        : active
                          ? 'border-primary bg-primary text-primary-foreground'
                          : complete
                            ? 'border-success/40 bg-success-soft text-success'
                            : 'bg-card'
                    )}
                  >
                    {complete && !active && !hasError ? <Check className="size-3.5" strokeWidth={3} aria-hidden /> : i + 1}
                  </span>
                  <span>{t(`products.setup.steps.${s}.title`)}</span>
                </button>
              </li>
            );
          })}
        </ol>
      </nav>

      <Card className="min-w-0">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4">
          <div className="min-w-0">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
              {t('products.setup.stepOf', { current: index + 1, total: SETUP_STEPS.length })}
            </p>
            <h2 className="mt-0.5 text-[15px] font-semibold text-foreground">{title}</h2>
            <p className="mt-0.5 text-[13px] text-muted-foreground">{description}</p>
          </div>
          <DirtyIndicator dirty={dirty} />
        </div>

        <div className="px-5 py-5">
          {saveError ? (
            <Alert tone="danger" className="mb-5" title={saveError.message}>
              {Object.keys(saveError.fields).length > 0 ? (
                <ul className="mt-1 list-disc space-y-0.5 pl-4">
                  {Object.entries(saveError.fields).map(([k, v]) => (
                    <li key={k}>{v}</li>
                  ))}
                </ul>
              ) : null}
            </Alert>
          ) : null}

          {step === 'general' ? <GeneralStep draft={draft} setDraft={setDraft} nameError={nameError} /> : null}
          {step === 'modules' ? <ModulesStep catalog={product.moduleCatalog} modules={draft.config.modules} updateConfig={updateConfig} /> : null}
          {step === 'roles' ? <RolesStep config={draft.config} updateConfig={updateConfig} /> : null}
          {step === 'pipeline' ? <PipelineStep config={draft.config} updateConfig={updateConfig} /> : null}
          {step === 'login' ? <LoginStep config={draft.config} updateConfig={updateConfig} /> : null}
          {step === 'review' ? (
            <ReviewStep
              product={product}
              draft={draft}
              publishErrors={publishErrors}
              goTo={onStepChange}
              publishable={publishable}
              publishing={publishing}
              onPublish={onPublish}
              version={nextVersion(product)}
            />
          ) : null}
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 rounded-b-lg border-t bg-muted/30 px-5 py-3">
          <Button type="button" variant="outline" onClick={() => onStepChange(SETUP_STEPS[index - 1])} disabled={index === 0 || saving}>
            <ArrowLeft /> {t('common.back')}
          </Button>
          {step === 'review' ? (
            dirty ? (
              <Button type="button" variant="outline" onClick={() => void saveAndContinue()} loading={saving}>
                {!saving ? <Save /> : null} {t('products.setup.saveDraft')}
              </Button>
            ) : null
          ) : (
            <Button type="button" onClick={() => void saveAndContinue()} loading={saving}>
              {saving ? t('products.setup.saving') : t('products.setup.saveContinue')} {!saving ? <ArrowRight /> : null}
            </Button>
          )}
        </div>
      </Card>
    </div>
  );
}

function DirtyIndicator({ dirty }: { dirty: boolean }) {
  const { t } = useTranslation();
  return dirty ? (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-warning-soft px-2 py-0.5 text-[11px] font-semibold text-warning" role="status">
      <span className="size-1.5 rounded-full bg-warning" aria-hidden />
      {t('products.setup.unsaved')}
    </span>
  ) : (
    <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground" role="status">
      <CircleCheck className="size-3.5 text-success" aria-hidden />
      {t('products.setup.allSaved')}
    </span>
  );
}
