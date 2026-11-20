import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { sharedDocApi } from '@crm/api/endpoints';
import { formatMoney } from '@crm/lib/money';

/** /q/:token — the customer's copy of a quote: the plan, premium and totals; never the agent's commission. */
export function SharedQuotePage() {
  const { token = '' } = useParams();
  const q = useQuery({ queryKey: ['shared-quote', token], queryFn: () => sharedDocApi.quote(token), retry: false });
  const money = (n?: number) => formatMoney(Number(n ?? 0), q.data?.currency || 'INR');
  return (
    <div className="min-h-screen bg-muted/40 px-4 py-10">
      <div className="mx-auto max-w-2xl rounded-2xl border bg-card p-6 shadow-card sm:p-8">
        {q.isLoading ? (
          <p className="text-sm text-muted-foreground">Loading the quotation…</p>
        ) : q.isError || !q.data ? (
          <div>
            <h1 className="text-lg font-semibold">This link has expired</h1>
            <p className="mt-2 text-sm text-muted-foreground">Ask your advisor to send the quotation again.</p>
          </div>
        ) : (
          <>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{q.data.business}</p>
            <h1 className="mt-1 text-xl font-semibold">{q.data.name}</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {q.data.quoteDate ? `Dated ${q.data.quoteDate}` : null}
              {q.data.validUntil ? ` · valid until ${q.data.validUntil}` : null}
            </p>
            <div className="mt-6 overflow-x-auto rounded-lg border">
              <table className="w-full text-left text-sm">
                <thead className="bg-muted/50 text-xs text-muted-foreground">
                  <tr>
                    <th className="px-3 py-2 font-medium">Plan</th>
                    <th className="px-3 py-2 text-right font-medium">Premium</th>
                    <th className="px-3 py-2 text-right font-medium">Tax</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {q.data.lines.map((l, i) => (
                    <tr key={i}>
                      <td className="px-3 py-2">{l.name}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{money(l.total)}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{money(l.taxAmount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <dl className="mt-4 space-y-1 text-sm">
              <div className="flex justify-between"><dt className="text-muted-foreground">Premium</dt><dd className="tabular-nums">{money(q.data.subtotal)}</dd></div>
              {q.data.discount ? <div className="flex justify-between"><dt className="text-muted-foreground">Discount</dt><dd className="tabular-nums">−{money(q.data.discount)}</dd></div> : null}
              <div className="flex justify-between"><dt className="text-muted-foreground">Tax</dt><dd className="tabular-nums">{money(q.data.tax)}</dd></div>
              <div className="flex justify-between border-t pt-2 text-base font-semibold"><dt>You pay</dt><dd className="tabular-nums">{money(q.data.total)}</dd></div>
            </dl>
            {q.data.terms ? <p className="mt-6 whitespace-pre-line text-xs text-muted-foreground">{q.data.terms}</p> : null}
          </>
        )}
      </div>
    </div>
  );
}
