import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Download, Printer } from 'lucide-react';
import { insuranceApi, type InsuranceReport } from '@crm/api/endpoints';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { formatMoney } from '@crm/lib/money';

const inr = (n: number) => formatMoney(Math.round(n), 'INR').replace(/\.00$/, '');

function csv(r: InsuranceReport): string {
  const rows: string[][] = [['Section', 'Item', 'Commission', 'Fees', 'Costs', 'Profit', 'Policies']];
  for (const m of [...r.months, r.monthsTotal]) rows.push(['Month', m.month, ...[m.commission, m.fees, m.costs, m.profit, m.policies].map(String)]);
  rows.push([]);
  rows.push(['Year', 'Label', 'New policies', 'Renewals', 'Premium (firm)', 'Premium (Estimated)', 'Commission (firm)', 'Commission (Estimated)', 'Commission received', 'Commission %', 'Plan commission']);
  for (const y of r.years)
    rows.push(['Year', y.label, ...[y.newPolicies, y.renewals, y.premiumFirm, y.premiumEstimated, y.commissionFirm, y.commissionEstimated, y.commissionActual, y.commissionPercent, y.planCommission ?? ''].map(String)]);
  rows.push([]);
  rows.push(['Assumptions']);
  for (const a of r.assumptions) rows.push(['', a]);
  return rows.map((row) => row.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(',')).join('\n');
}

/** /insurance/report — cash today, the year by month and five years by policy year (catalogue G12–G18, J14–J15, J22). */
export function InsuranceReportPage() {
  const { tenant = '', ws = '' } = useParams();
  const code = ws || tenant;
  const q = useQuery({ queryKey: ['insurance-report', code], queryFn: () => insuranceApi(code).report() });
  useDocumentTitle('Insurance report');
  if (q.isError) return <PageContainer><ErrorState title="Couldn’t load the report" onRetry={() => void q.refetch()} /></PageContainer>;
  if (!q.data) return <PageContainer><Skeleton className="h-6 w-64" /><Skeleton className="mt-4 h-72 w-full" /></PageContainer>;
  const r = q.data;
  const download = () => {
    const blob = new Blob(['﻿' + csv(r)], { type: 'text/csv;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `insurance-report-${r.financialYear}${r.draft ? '-DRAFT' : ''}.csv`;
    a.click();
  };
  return (
    <PageContainer>
      <PageHeader
        title={<span className="flex items-center gap-2">Insurance report {r.draft ? <Badge tone="warning">Draft</Badge> : null}</span>}
        description={`Financial year ${r.financialYear}: commission, fees, costs and profit, and the next five years from your policies.`}
        actions={
          <div className="flex gap-2 print:hidden">
            <Button variant="outline" size="sm" onClick={download}><Download /> Spreadsheet</Button>
            <Button variant="outline" size="sm" onClick={() => window.print()}><Printer /> Print / PDF</Button>
          </div>
        }
      />
      {r.draft ? (
        <Alert tone="warning" className="mt-4">
          Draft: {r.unreconciled.shortPaidCommissions} short-paid commission(s) and {r.unreconciled.unmatchedStatementLines} unmatched statement line(s) are not reconciled yet.
        </Alert>
      ) : null}
      <div className="mt-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          ['Collected today (for insurers)', r.cash.collectedToday],
          ['Remitted today', r.cash.remittedToday],
          ['Held for insurers (owed, not income)', r.cash.heldForInsurers],
          ['Your income today', r.cash.incomeToday]
        ].map(([label, v]) => (
          <Card key={label as string} className="p-4">
            <p className="text-[13px] text-muted-foreground">{label}</p>
            <p className="mt-1 text-xl font-semibold tabular-nums">{inr(v as number)}</p>
          </Card>
        ))}
      </div>
      <Card className="mt-4 overflow-hidden">
        <CardHeader title="Month by month" description="Commission in the month it arrived; fees and direct costs in the month the policy started." />
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr>{['Month', 'Policies', 'Commission', 'Fees', 'Direct costs', 'Profit'].map((h) => <th key={h} className="px-3 py-2 text-right font-medium first:text-left">{h}</th>)}</tr>
            </thead>
            <tbody className="divide-y">
              {[...r.months, r.monthsTotal].map((m) => (
                <tr key={m.month} className={m.month === 'Total' ? 'bg-muted/30 font-semibold' : undefined}>
                  <td className="px-3 py-1.5">{m.month}</td>
                  {[m.policies, m.commission, m.fees, m.costs, m.profit].map((v, i) => (
                    <td key={i} className="px-3 py-1.5 text-right tabular-nums">{i === 0 ? v : inr(v)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Card className="mt-4 overflow-hidden">
        <CardHeader title="Five years from your policies" description="Firm years and Estimated years are kept apart; the plan is shown beside, never added in." />
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr>{['Year', 'New', 'Renewals', 'Premium (firm)', 'Premium (Estimated)', 'Commission (firm)', 'Commission (Estimated)', 'Received', 'Commission %', 'Plan'].map((h) => <th key={h} className="px-3 py-2 text-right font-medium first:text-left">{h}</th>)}</tr>
            </thead>
            <tbody className="divide-y">
              {r.years.map((y) => (
                <tr key={y.year}>
                  <td className="px-3 py-1.5">{y.label}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{y.newPolicies}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{y.renewals}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{inr(y.premiumFirm)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-warning">{inr(y.premiumEstimated)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{inr(y.commissionFirm)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-warning">{inr(y.commissionEstimated)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{inr(y.commissionActual)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{y.commissionPercent}%</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-muted-foreground">{y.planCommission ? inr(y.planCommission) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Card className="mt-4 overflow-hidden">
        <CardHeader title="Commission % by policy year" description="Expected from the rate card against what insurers actually paid." />
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr>{['Insurer', 'Policy year', 'Expected %', 'Actual %'].map((h) => <th key={h} className="px-3 py-2 text-right font-medium first:text-left">{h}</th>)}</tr>
            </thead>
            <tbody className="divide-y">
              {r.commissionByYear.map((p, i) => (
                <tr key={i}>
                  <td className="px-3 py-1.5">{p.insurer}</td>
                  <td className="px-3 py-1.5 text-right">{p.policyYear}{p.estimated ? ' (Estimated)' : ''}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{p.expectedPercent}%</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{p.actualPercent != null ? `${p.actualPercent}%` : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Card className="mt-4 p-4 text-xs text-muted-foreground">
        <p className="font-medium text-foreground">Assumptions</p>
        <ul className="mt-1 list-disc pl-5">{r.assumptions.map((a) => <li key={a}>{a}</li>)}</ul>
      </Card>
    </PageContainer>
  );
}
