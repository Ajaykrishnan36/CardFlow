import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, CircleCheck, LoaderCircle } from 'lucide-react';
import { getIndustries, RequestError, sendRequest, type Industry, type WebsiteRequest } from '../../services/website-api';

type Kind = 'call' | 'enquiry';
type Values = Record<string, string>;

const teamSizes = ['Just me', '2-10', '11-50', '51-200', '201-500', '500+'];
const methods = [
  ['paper', 'Notebook / paper'],
  ['excel', 'Excel sheets'],
  ['whatsapp', 'WhatsApp'],
  ['memory', 'Memory'],
  ['other_crm', 'Another CRM'],
  ['other', 'Something else']
];
const slots = [
  ['morning', 'Morning (9 AM – 12 PM)'],
  ['afternoon', 'Afternoon (12 – 4 PM)'],
  ['evening', 'Evening (4 – 7 PM)'],
  ['any', 'Any time']
];
const categories = [
  ['demo', 'CRM demo'],
  ['pricing', 'Pricing'],
  ['product', 'Product information'],
  ['feature', 'Feature request'],
  ['customization', 'Customization'],
  ['technical', 'Technical question'],
  ['partnership', 'Partnership'],
  ['other', 'Other']
];
const codes = ['+91', '+971', '+65', '+60', '+1', '+44', '+61'];

const today = () => {
  const d = new Date();
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
};
const inMonths = (m: number) => {
  const d = new Date();
  d.setMonth(d.getMonth() + m);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
};

let industriesCache: Promise<Industry[]> | null = null;

/**
 * One form, two jobs (D-181): "Book a free call" and "Send an enquiry". Each saves a lead in
 * the CRM owner's Leads, marked with its source and type. Checked here and again on the server.
 */
export function RequestForm({ kind, idPrefix, onDone }: { kind: Kind; idPrefix: string; onDone?: () => void }) {
  const [v, setV] = useState<Values>({ cc: '+91', name: '', business: '', phone: '', email: '', industry: '', teamSize: '', currentMethod: '', challenge: '', preferredDate: '', preferredTime: '', subject: '', category: '', message: '', website: '' });
  const [errors, setErrors] = useState<Values>({});
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const [failure, setFailure] = useState('');
  const [industries, setIndustries] = useState<Industry[]>([]);
  useEffect(() => {
    industriesCache ??= getIndustries().catch(() => []);
    void industriesCache.then(setIndustries);
  }, []);
  const set = (k: string) => (e: { target: { value: string } }) => {
    setV((o) => ({ ...o, [k]: e.target.value }));
    setErrors((o) => ({ ...o, [k]: '' }));
    setFailure('');
  };
  const id = (k: string) => `${idPrefix}-${k}`;

  const validate = (): Values => {
    const fe: Values = {};
    if (v.name.trim().length < 2) fe.name = 'Enter your name.';
    if (v.business.trim().length < 2) fe.business = 'Enter your business name.';
    const digits = v.phone.replace(/\D/g, '');
    if (digits.length < 7 || digits.length > 12 || (v.cc === '+91' && digits.length !== 10)) fe.phone = 'Enter a valid mobile number.';
    if (v.email.trim() && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v.email.trim())) fe.email = 'Enter a valid email address.';
    if (kind === 'call') {
      if (!v.email.trim()) fe.email = 'Enter your email address.';
      if (!v.industry) fe.industry = 'Choose your industry.';
      if (!v.teamSize) fe.teamSize = 'Choose the size of your team.';
      if (!v.preferredDate) fe.preferredDate = 'Choose a date for the call.';
      else if (v.preferredDate < today() || v.preferredDate > inMonths(3)) fe.preferredDate = 'Choose a date from today up to three months ahead.';
      if (!v.preferredTime) fe.preferredTime = 'Choose a time for the call.';
    } else {
      if (v.subject.trim().length < 3) fe.subject = 'Enter a subject.';
      if (!v.category) fe.category = 'Choose what your enquiry is about.';
      if (v.message.trim().length < 5) fe.message = 'Write your message.';
    }
    return fe;
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const fe = validate();
    setErrors(fe);
    if (Object.keys(fe).length) {
      document.getElementById(id(Object.keys(fe)[0]!))?.focus();
      return;
    }
    setBusy(true);
    setFailure('');
    const body: WebsiteRequest = {
      kind,
      name: v.name.trim(),
      business: v.business.trim(),
      phone: v.cc + v.phone.replace(/\D/g, ''),
      email: v.email.trim(),
      industry: v.industry,
      message: v.message.trim(),
      website: v.website,
      ...(kind === 'call'
        ? { teamSize: v.teamSize, currentMethod: v.currentMethod, challenge: v.challenge.trim(), preferredDate: v.preferredDate, preferredTime: v.preferredTime }
        : { subject: v.subject.trim(), category: v.category })
    };
    try {
      const res = await sendRequest(body);
      setDone(res.reference ?? '');
      onDone?.();
    } catch (err) {
      const re = err instanceof RequestError ? err : new RequestError('Sorry, that did not go through. Please try again.');
      const map: Values = {};
      for (const [k, msg] of Object.entries(re.fields)) map[k] = msg;
      setErrors(map);
      setFailure(Object.keys(map).length ? 'Please check the highlighted fields.' : re.message);
    } finally {
      setBusy(false);
    }
  };

  const slotName = useMemo(() => Object.fromEntries(slots), []);
  if (done !== null) {
    return (
      <div className="cw-form-done" role="status">
        <span className="cw-done-ring">
          <CircleCheck aria-hidden />
        </span>
        <h3>{kind === 'call' ? 'Your call request is in' : 'Your enquiry is in'}</h3>
        {kind === 'call' ? (
          <p>
            We have your request for <b>{new Date(v.preferredDate + 'T00:00').toLocaleDateString('en-IN', { weekday: 'short', day: 'numeric', month: 'short' })}</b>,{' '}
            {slotName[v.preferredTime]?.toLowerCase()}. Our team will call or message you to <b>confirm the time</b>; the call is not booked until then.
          </p>
        ) : (
          <p>Thank you. Our team will read your message and get back to you on the number or email you gave.</p>
        )}
        {done ? <small>Your reference: {done}</small> : null}
        <Link className="cw-btn cw-btn-outline" to="/signup">
          Create your CRM now <ArrowRight aria-hidden />
        </Link>
      </div>
    );
  }

  const field = (k: string, label: string, control: ReactNode, opts: { required?: boolean; hint?: string; wide?: boolean } = {}) => (
    <div className={`cw-field${opts.wide ? ' is-wide' : ''}${errors[k] ? ' has-error' : ''}`}>
      <label htmlFor={id(k)}>
        {label} {opts.required ? <i aria-hidden>*</i> : <em>(optional)</em>}
      </label>
      {control}
      {errors[k] ? (
        <p className="cw-field-error" id={id(k) + '-err'}>
          {errors[k]}
        </p>
      ) : opts.hint ? (
        <p className="cw-field-hint">{opts.hint}</p>
      ) : null}
    </div>
  );
  const aria = (k: string, required = false) => ({ id: id(k), name: k, 'aria-invalid': Boolean(errors[k]) || undefined, 'aria-describedby': errors[k] ? id(k) + '-err' : undefined, required });
  const select = (k: string, options: (string | string[])[], placeholder: string, required = false) => (
    <select {...aria(k, required)} value={v[k]} onChange={set(k)}>
      <option value="">{placeholder}</option>
      {options.map((o) => (Array.isArray(o) ? <option key={o[0]} value={o[0]}>{o[1]}</option> : <option key={o}>{o}</option>))}
    </select>
  );

  return (
    <form className="cw-form" onSubmit={submit} noValidate>
      <div className="cw-form-grid">
        {field('name', 'Full name', <input {...aria('name', true)} autoComplete="name" placeholder="Your name" value={v.name} onChange={set('name')} />, { required: true })}
        {field('business', 'Business name', <input {...aria('business', true)} autoComplete="organization" placeholder="Your business" value={v.business} onChange={set('business')} />, { required: true })}
        {field(
          'phone',
          'Phone number',
          <span className="cw-phone">
            <select aria-label="Country code" value={v.cc} onChange={set('cc')}>
              {codes.map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
            <input {...aria('phone', true)} type="tel" inputMode="tel" autoComplete="tel-national" placeholder="98765 43210" value={v.phone} onChange={set('phone')} />
          </span>,
          { required: true }
        )}
        {field('email', 'Email address', <input {...aria('email', kind === 'call')} type="email" autoComplete="email" placeholder="you@business.com" value={v.email} onChange={set('email')} />, { required: kind === 'call' })}
        {field('industry', 'Industry', select('industry', industries.map((i) => [i.label, i.label]), 'Choose your industry', kind === 'call'), { required: kind === 'call' })}
        {kind === 'call' ? (
          <>
            {field('teamSize', 'Team / sales team size', select('teamSize', teamSizes, 'Choose', true), { required: true })}
            {field('currentMethod', 'How do you manage leads today?', select('currentMethod', methods, 'Choose one'))}
            {field('preferredDate', 'Preferred date', <input {...aria('preferredDate', true)} type="date" min={today()} max={inMonths(3)} value={v.preferredDate} onChange={set('preferredDate')} />, { required: true })}
            {field('preferredTime', 'Preferred time', select('preferredTime', slots, 'Choose a time', true), { required: true, hint: 'Indian Standard Time. We confirm before calling.' })}
            {field('challenge', 'Main business challenge', <input {...aria('challenge')} placeholder="e.g. we forget follow-ups" value={v.challenge} onChange={set('challenge')} />)}
            {field('message', 'Anything else?', <textarea {...aria('message')} rows={3} placeholder="Tell us a little about how you sell today" value={v.message} onChange={set('message')} />, { wide: true })}
          </>
        ) : (
          <>
            {field('category', 'Enquiry category', select('category', categories, 'Choose one', true), { required: true })}
            {field('subject', 'Subject', <input {...aria('subject', true)} placeholder="What is it about?" value={v.subject} onChange={set('subject')} />, { required: true })}
            {field('message', 'Message', <textarea {...aria('message', true)} rows={4} placeholder="Write your question or request" value={v.message} onChange={set('message')} />, { required: true, wide: true })}
          </>
        )}
      </div>
      {/* People never see this box; robots fill it in. */}
      <input className="cw-trap" tabIndex={-1} autoComplete="off" aria-hidden name="website" value={v.website} onChange={set('website')} />
      {failure ? (
        <p className="cw-form-error" role="alert">
          {failure}
        </p>
      ) : null}
      <div className="cw-form-foot">
        <small>We use your details only to reply to this request.</small>
        <button className="cw-btn cw-btn-primary cw-btn-lg" type="submit" disabled={busy}>
          {busy ? <LoaderCircle className="cw-spin" aria-hidden /> : null}
          {busy ? 'Sending…' : kind === 'call' ? 'Request my free call' : 'Send enquiry'}
          {busy ? null : <ArrowRight aria-hidden />}
        </button>
      </div>
    </form>
  );
}
