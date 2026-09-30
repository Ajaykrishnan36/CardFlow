import { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { AlertTriangle, CheckCircle2, FileUp, Upload } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { ObjectKey, ObjectMeta } from '@crm/api/types';
import type { ImportResult } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Select } from '@crm/components/ui/form-controls';
import { Alert } from '@crm/components/ui/card';
import { recordKeys } from '../use-object-meta';
import { useRecordScope } from '../record-scope';
import { guessField, parseCsv } from './csv';

type Step = 'file' | 'map' | 'result';

/** Import a spreadsheet (CSV) into an object: map columns, check, then import. */
export function ImportDialog({ object, meta, open, onOpenChange }: { object: ObjectKey; meta: ObjectMeta; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [step, setStep] = useState<Step>('file');
  const [fileName, setFileName] = useState('');
  const [header, setHeader] = useState<string[]>([]);
  const [rows, setRows] = useState<string[][]>([]);
  const [mapping, setMapping] = useState<string[]>([]);
  const [mode, setMode] = useState<'create' | 'upsert' | 'update'>('create');
  const [matchField, setMatchField] = useState('');
  const [check, setCheck] = useState<ImportResult | null>(null);
  const [result, setResult] = useState<ImportResult | null>(null);
  const importable = useMemo(() => meta.fields.filter((f) => (!f.readOnly || f.key === 'code') && f.type !== 'files'), [meta.fields]);
  const reset = () => {
    setStep('file');
    setFileName('');
    setHeader([]);
    setRows([]);
    setMapping([]);
    setCheck(null);
    setResult(null);
    setMode('create');
  };
  const onFile = async (file: File) => {
    if (file.size > 12 * 1024 * 1024) return toast.error(t('lists.import.tooBig'));
    const all = parseCsv(await file.text());
    if (all.length < 2) return toast.error(t('lists.import.empty'));
    const [h, ...body] = all;
    if (body.length > 10000) return toast.error(t('lists.import.tooMany'));
    setFileName(file.name);
    setHeader(h!);
    setRows(body);
    const used = new Set<string>();
    setMapping(
      h!.map((col) => {
        const g = guessField(col, importable);
        if (!g || used.has(g)) return '';
        used.add(g);
        return g;
      })
    );
    setStep('map');
  };
  const body = (dryRun: boolean) => ({ rows, mapping, mode, matchField: mode === 'create' ? undefined : matchField, dryRun });
  const run = useMutation({
    mutationFn: (dryRun: boolean) => scope.api.importRows(object, body(dryRun)),
    onSuccess: (res) => {
      if (res.dryRun) setCheck(res);
      else {
        setResult(res);
        setStep('result');
        void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) });
      }
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const mapped = mapping.filter(Boolean);
  const fieldLabel = (k: string) => importable.find((f) => f.key === k)?.label ?? k;
  return (
    <Dialog open={open} onOpenChange={(o) => { onOpenChange(o); if (!o) reset(); }}>
      <DialogContent className="max-w-2xl p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">{t('lists.import.title', { objects: meta.labelPlural })}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('lists.import.subtitle')}</DialogDescription>
        </div>
        <div className="max-h-[65vh] space-y-4 overflow-y-auto px-5 py-4">
          {step === 'file' ? (
            <button
              type="button"
              onClick={() => inputRef.current?.click()}
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => {
                e.preventDefault();
                const f = e.dataTransfer.files[0];
                if (f) void onFile(f);
              }}
              className="flex w-full flex-col items-center gap-2 rounded-lg border-2 border-dashed px-6 py-10 text-center hover:border-primary/40 hover:bg-primary-soft/40"
            >
              <FileUp className="size-8 text-primary" aria-hidden />
              <span className="text-[13px] font-medium">{t('lists.import.choose')}</span>
              <span className="text-xs text-muted-foreground">{t('lists.import.chooseHint')}</span>
              <input ref={inputRef} type="file" accept=".csv,text/csv" className="hidden" onChange={(e) => e.target.files?.[0] && void onFile(e.target.files[0])} />
            </button>
          ) : null}
          {step === 'map' ? (
            <>
              <p className="text-[13px]">{t('lists.import.found', { file: fileName, count: rows.length })}</p>
              <div className="overflow-hidden rounded-lg border">
                <table className="w-full text-[13px]">
                  <thead className="bg-muted/50 text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-2 text-left font-medium">{t('lists.import.column')}</th>
                      <th className="px-3 py-2 text-left font-medium">{t('lists.import.example')}</th>
                      <th className="px-3 py-2 text-left font-medium">{t('lists.import.field')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {header.map((h, i) => (
                      <tr key={i} className="border-t">
                        <td className="px-3 py-1.5 font-medium">{h || t('lists.import.untitledColumn', { n: i + 1 })}</td>
                        <td className="max-w-[180px] truncate px-3 py-1.5 text-muted-foreground">{rows[0]?.[i] ?? ''}</td>
                        <td className="px-3 py-1.5">
                          <Select
                            aria-label={t('lists.import.fieldFor', { column: h })}
                            value={mapping[i] ?? ''}
                            onChange={(e) => {
                              const next = mapping.map((m, j) => (j === i ? e.target.value : m === e.target.value && e.target.value ? '' : m));
                              setMapping(next);
                              setCheck(null);
                            }}
                          >
                            <option value="">{t('lists.import.skip')}</option>
                            {importable.map((f) => (
                              <option key={f.key} value={f.key}>
                                {f.label}
                                {f.required ? ' *' : ''}
                              </option>
                            ))}
                          </Select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="space-y-1 text-[13px]">
                  <span className="font-medium">{t('lists.import.modeLabel')}</span>
                  <Select value={mode} onChange={(e) => { setMode(e.target.value as typeof mode); setCheck(null); }}
                    options={[
                      { value: 'create', label: t('lists.import.modeCreate') },
                      { value: 'upsert', label: t('lists.import.modeUpsert') },
                      { value: 'update', label: t('lists.import.modeUpdate') }
                    ]} />
                </label>
                {mode !== 'create' ? (
                  <label className="space-y-1 text-[13px]">
                    <span className="font-medium">{t('lists.import.matchLabel')}</span>
                    <Select value={matchField} onChange={(e) => { setMatchField(e.target.value); setCheck(null); }} placeholder={t('lists.import.matchPlaceholder')}
                      options={mapped.map((k) => ({ value: k, label: fieldLabel(k) }))} />
                  </label>
                ) : null}
              </div>
              {check ? (
                check.failed === 0 ? (
                  <Alert tone="success" title={t('lists.import.checkOk', { count: check.rows })}>
                    {t('lists.import.checkSummary', { created: check.created, updated: check.updated, skipped: check.skipped })}
                  </Alert>
                ) : (
                  <Alert tone="warning" title={t('lists.import.checkProblems', { count: check.failed })}>
                    <ul className="mt-1 max-h-40 list-disc space-y-0.5 overflow-auto pl-4">
                      {check.errors.slice(0, 50).map((e) => (
                        <li key={e.row}>
                          {t('lists.import.row', { n: e.row })}: {Object.entries(e.errors).map(([k, v]) => (k === '_' ? v : `${fieldLabel(k)} — ${v}`)).join('; ')}
                        </li>
                      ))}
                    </ul>
                    <p className="mt-1">{t('lists.import.problemsNote')}</p>
                  </Alert>
                )
              ) : null}
            </>
          ) : null}
          {step === 'result' && result ? (
            <div className="space-y-3 text-center">
              <CheckCircle2 className="mx-auto size-10 text-success" aria-hidden />
              <p className="text-[15px] font-semibold">{t('lists.import.done')}</p>
              <p className="text-[13px] text-muted-foreground">{t('lists.import.doneSummary', { created: result.created, updated: result.updated, skipped: result.skipped, failed: result.failed })}</p>
              {result.failed ? (
                <Alert tone="warning" title={t('lists.import.failedRows', { count: result.failed })}>
                  <ul className="max-h-40 list-disc overflow-auto pl-4 text-left">
                    {result.errors.slice(0, 50).map((e) => (
                      <li key={e.row}>
                        {t('lists.import.row', { n: e.row })}: {Object.values(e.errors).join('; ')}
                      </li>
                    ))}
                  </ul>
                </Alert>
              ) : null}
            </div>
          ) : null}
        </div>
        <div className="flex items-center justify-between gap-2 border-t px-5 py-3">
          {step === 'map' ? (
            <Button variant="ghost" onClick={reset}>
              {t('lists.import.chooseAnother')}
            </Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            {step === 'map' ? (
              <>
                <Button variant="outline" onClick={() => run.mutate(true)} loading={run.isPending && run.variables === true} disabled={!mapped.length || (mode !== 'create' && !matchField)}>
                  <AlertTriangle /> {t('lists.import.check')}
                </Button>
                <Button onClick={() => run.mutate(false)} loading={run.isPending && run.variables === false} disabled={!mapped.length || (mode !== 'create' && !matchField)}>
                  <Upload /> {t('lists.import.run', { count: rows.length })}
                </Button>
              </>
            ) : (
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t('lists.import.close')}
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
