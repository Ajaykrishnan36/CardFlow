import { useEffect, useRef, useState } from 'react';
import { XCircle } from 'lucide-react';
import type { ObjectKey } from '@crm/api/types';
import { setLostReasonPrompt } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';

// The "Why was it lost?" popup (D-177). Mounted once in the app; any save that closes a lead
// or a deal as lost waits for it. Cancel leaves the record as it was.

const QUICK: Record<string, string[]> = {
  leads: ['Bought from a competitor', 'Price too high', 'Not interested any more', 'No response after follow-ups', 'Budget not approved', 'Postponed the purchase', 'Wrong or duplicate contact'],
  opportunities: ['Lost to a competitor', 'Price too high', 'Budget not approved', 'Went silent', 'Requirement changed', 'Postponed', 'Product did not fit']
};

export function LostReasonHost() {
  const [ask, setAsk] = useState<{ object: ObjectKey; done: (v: string | null) => void } | null>(null);
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');
  const ref = useRef<HTMLTextAreaElement>(null);

  // One question at a time: asked again while it is open (a second save of the same change),
  // the same answer goes to both.
  const pending = useRef<{ object: ObjectKey; promise: Promise<string | null> } | null>(null);
  useEffect(() => {
    setLostReasonPrompt((object) => {
      if (pending.current && pending.current.object === object) return pending.current.promise;
      const promise = new Promise<string | null>((resolve) => {
        setReason('');
        setError('');
        setAsk({
          object,
          done: (v) => {
            pending.current = null;
            resolve(v);
          }
        });
      });
      pending.current = { object, promise };
      return promise;
    });
    return () => setLostReasonPrompt(null);
  }, []);

  const close = (v: string | null) => {
    ask?.done(v);
    setAsk(null);
  };
  const save = () => {
    const r = reason.trim();
    if (r.length < 3) {
      setError('Write the reason in a few words.');
      ref.current?.focus();
      return;
    }
    close(r);
  };
  const what = ask?.object === 'opportunities' ? 'deal' : 'lead';

  return (
    <Dialog open={Boolean(ask)} onOpenChange={(o) => (!o ? close(null) : undefined)}>
      <DialogContent className="max-w-md">
        <div className="flex items-start gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-danger-soft text-danger"><XCircle className="size-5" /></span>
          <div>
            <DialogTitle className="text-[17px] font-semibold">Why was this {what} lost?</DialogTitle>
            <DialogDescription className="text-[13px] text-muted-foreground">The reason is saved on the {what} and shows in reports. It is needed to close it as lost.</DialogDescription>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-1.5">
          {(QUICK[ask?.object ?? 'leads'] ?? QUICK.leads!).map((q) => (
            <button key={q} type="button" onClick={() => { setReason(q); setError(''); }}
              className={cn('rounded-full border px-3 py-1 text-xs font-medium transition-colors', reason === q ? 'border-primary bg-primary-soft text-primary' : 'text-muted-foreground hover:border-primary/40 hover:text-foreground')}>
              {q}
            </button>
          ))}
        </div>
        <Field label="Reason *" error={error} className="mt-3">
          <Textarea ref={ref} value={reason} onChange={(e) => { setReason(e.target.value); setError(''); }} rows={3} maxLength={500} autoFocus
            placeholder="For example: bought a Honda from the next showroom for ₹3,000 less" />
        </Field>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" onClick={() => close(null)}>Cancel</Button>
          <Button variant="danger" onClick={save}>Close as lost</Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
