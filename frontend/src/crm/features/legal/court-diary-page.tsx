import { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Printer } from 'lucide-react';
import { legalApi, type DiaryRow } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';

const istDay = (offset = 0) => new Date(Date.now() + 5.5 * 3600000 + offset * 86400000).toISOString().slice(0, 10);
const when = (s: string) => new Intl.DateTimeFormat('en-IN', { timeZone: 'Asia/Kolkata', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' }).format(new Date(s));
const monthEnd = () => { const d = new Date(istDay()); return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).toISOString().slice(0, 10); };
const label = (s: string) => s.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());
const selectClass = 'h-9 rounded-md border border-border bg-background px-2 text-sm';

/** /legal/diary: an advocate's day. Everything here respects who may see each matter. */
export function CourtDiaryPage() {
  const { tenant = '', ws = '' } = useParams();
  const code = ws || tenant;
  const base = ws ? `/crm/w/${encodeURIComponent(ws)}` : `/${encodeURIComponent(tenant)}`;
  useDocumentTitle('Court diary');
  const [from, setFrom] = useState(istDay());
  const [to, setTo] = useState(istDay(7));
  const [notUpdated, setNotUpdated] = useState(false);
  const [advocate, setAdvocate] = useState('');
  const [court, setCourt] = useState('');
  const [caseType, setCaseType] = useState('');
  const [status, setStatus] = useState('');
  const params = { from, to, advocate, court, caseType, status, notUpdated: notUpdated ? '1' : '' };
  const q = useQuery({ queryKey: ['legal-diary', code, params], queryFn: () => legalApi(code).diary(params) });
  const notify = useMutation({
    mutationFn: (id: string) => legalApi(code).notify(id),
    onSuccess: () => toast.success('Advocate notified in the CRM.'),
    onError: (e) => toast.error(isApiError(e) ? e.message : 'Couldn’t notify the advocate.')
  });
  const rows = q.data?.rows ?? [];
  const choices = useMemo(() => ({
    advocates: [...new Map(rows.filter((r) => r.advocateId).map((r) => [r.advocateId, r.advocate])).entries()],
    courts: [...new Set(rows.map((r) => r.court).filter(Boolean))]
  }), [rows]);
  const range = (a: string, b: string) => { setNotUpdated(false); setFrom(a); setTo(b); };
  const printTomorrow = () => { range(istDay(1), istDay(1)); setTimeout(() => window.print(), 600); };
  if (q.isError) return <PageContainer><ErrorState title="Couldn’t load the diary" onRetry={() => void q.refetch()} /></PageContainer>;
  const t = q.data?.tiles;
  const tiles: Array<[string, number | undefined, () => void]> = [
    ['Today', t?.today, () => range(istDay(), istDay())],
    ['Next 7 days', t?.next7, () => range(istDay(), istDay(7))],
    ['Hearings not updated', t?.notUpdated, () => setNotUpdated(true)],
    ['Overdue follow-ups', t?.overdue, () => undefined],
    ['Matters missing documents', t?.missingDocs, () => undefined],
    ['Deadlines in 30 days', t?.deadlines, () => undefined],
    ...(t?.unassigned === undefined ? [] : [['Unassigned leads', t.unassigned, () => undefined] as [string, number, () => void]])
  ];
  const links: Record<string, string> = { 'Overdue follow-ups': `${base}/tasks`, 'Matters missing documents': `${base}/client_requirements`, 'Deadlines in 30 days': `${base}/legal_deadlines`, 'Unassigned leads': `${base}/leads?queue=unassigned` };
  return (
    <PageContainer>
      <PageHeader title="Court diary" description="Hearings, follow-ups and deadlines. Times are in IST."
        actions={<div className="flex gap-2 print:hidden"><Button size="sm" variant="outline" onClick={printTomorrow}><Printer /> Print tomorrow’s diary</Button></div>} />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-7 print:hidden">
        {tiles.map(([name, n, go]) => {
          const body = <Card className="h-full p-3 text-left"><div className="text-xs text-muted-foreground">{name}</div><div className="mt-1 text-2xl font-semibold">{n ?? '–'}</div></Card>;
          return links[name] ? <Link key={name} to={links[name]}>{body}</Link> : <button key={name} type="button" onClick={go}>{body}</button>;
        })}
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-2 print:hidden">
        <Button size="sm" variant="outline" onClick={() => range(istDay(), istDay())}>Today</Button>
        <Button size="sm" variant="outline" onClick={() => range(istDay(1), istDay(1))}>Tomorrow</Button>
        <Button size="sm" variant="outline" onClick={() => range(istDay(), istDay(7))}>7 days</Button>
        <Button size="sm" variant="outline" onClick={() => range(istDay(), monthEnd())}>This month</Button>
        <input aria-label="From" type="date" className={selectClass} value={from} onChange={(e) => range(e.target.value, to)} />
        <input aria-label="To" type="date" className={selectClass} value={to} onChange={(e) => range(from, e.target.value)} />
        <select aria-label="Advocate" className={selectClass} value={advocate} onChange={(e) => setAdvocate(e.target.value)}><option value="">All advocates</option>{choices.advocates.map(([id, n]) => <option key={id} value={id}>{n}</option>)}</select>
        <select aria-label="Court" className={selectClass} value={court} onChange={(e) => setCourt(e.target.value)}><option value="">All courts</option>{choices.courts.map((c) => <option key={c}>{c}</option>)}</select>
        <select aria-label="Case type" className={selectClass} value={caseType} onChange={(e) => setCaseType(e.target.value)}><option value="">All case types</option>{['accident', 'property', 'family', 'criminal', 'contract', 'other'].map((c) => <option key={c} value={c}>{label(c)}</option>)}</select>
        <select aria-label="Status" className={selectClass} value={status} onChange={(e) => setStatus(e.target.value)}><option value="">All statuses</option>{['scheduled', 'attended', 'adjourned', 'not_reached', 'disposed', 'cancelled'].map((c) => <option key={c} value={c}>{label(c)}</option>)}</select>
      </div>
      <Card className="mt-4 overflow-x-auto">
        {!q.data ? <Skeleton className="m-4 h-40" /> : rows.length === 0 ? <p className="p-6 text-sm text-muted-foreground">No hearings in this period.</p> : (
          <table className="w-full min-w-[1100px] text-sm">
            <thead className="text-left text-xs text-muted-foreground"><tr>{['Time', 'Court & hall', 'Matter', 'Client', 'Case type', 'Purpose', 'Advocate', 'Status', 'Last outcome'].map((h) => <th key={h} className="px-3 py-2">{h}</th>)}<th className="px-3 py-2 print:hidden">Actions</th></tr></thead>
            <tbody>{rows.map((r: DiaryRow) => (
              <tr key={r.id} className="border-t border-border align-top">
                <td className="whitespace-nowrap px-3 py-2">{when(r.at)}</td>
                <td className="px-3 py-2">{[r.court, r.hall].filter(Boolean).join(', ')}</td>
                <td className="px-3 py-2"><Link className="text-primary hover:underline" to={`${base}/legal_matters/${r.matterId}`}>{r.matterCode}</Link><div className="text-xs text-muted-foreground">{r.caseNumber || r.matter}</div></td>
                <td className="px-3 py-2">{r.client}</td><td className="px-3 py-2">{label(r.caseType)}</td><td className="px-3 py-2">{label(r.purpose)}</td><td className="px-3 py-2">{r.advocate}</td>
                <td className="px-3 py-2">{r.notUpdated ? <Badge tone="danger">Not updated</Badge> : <Badge>{label(r.status)}</Badge>}</td>
                <td className="max-w-56 px-3 py-2">{r.lastOutcome}</td>
                <td className="px-3 py-2 print:hidden"><div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-primary">
                  <Link to={`${base}/legal_matters/${r.matterId}`}>Open matter</Link><Link to={`${base}/legal_documents?matterId=${r.matterId}&matterName=${encodeURIComponent(r.matter)}`}>Documents</Link><Link to={`${base}/court_hearings/${r.id}`}>Update hearing</Link>
                  <Link to={`${base}/legal_documents?new=1&matterId=${r.matterId}&matterName=${encodeURIComponent(r.matter)}&category=court_order`}>Upload order</Link><Link to={`${base}/tasks?new=1&matterId=${r.matterId}&matterName=${encodeURIComponent(r.matter)}`}>Add task</Link><button type="button" onClick={() => notify.mutate(r.id)}>Notify advocate</button>
                </div></td>
              </tr>))}</tbody>
          </table>)}
      </Card>
    </PageContainer>
  );
}
