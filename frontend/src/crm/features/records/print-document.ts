import type { BusinessProfile, PriceResult } from '@crm/api/endpoints';
import type { RecordRow } from '@crm/api/types';
import { formatMoney } from '@crm/lib/money';

// A quote, order or invoice as a document the customer can be given (D-144). It is laid
// out as a plain page in its own window and handed to the browser's print dialog, where
// "Save as PDF" makes the file: real text, the business's name, address and GSTIN at the
// top, the customer's below, every line with its tax, and the totals.

export type PrintableObject = 'quotes' | 'sales_orders' | 'invoices';

const esc = (v: unknown) =>
  String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string);

const day = (v: unknown) => {
  const s = String(v ?? '');
  if (!s) return '';
  const d = new Date(s.length <= 10 ? `${s}T00:00:00` : s);
  return Number.isNaN(d.getTime()) ? s : d.toLocaleDateString('en-IN', { day: '2-digit', month: 'short', year: 'numeric' });
};

const text = (html: unknown) => {
  const el = document.createElement('div');
  el.innerHTML = String(html ?? '');
  return (el.textContent ?? '').trim();
};

const lines = (...parts: unknown[]) => parts.map((p) => String(p ?? '').trim()).filter(Boolean);

export interface PrintInput {
  object: PrintableObject;
  record: RecordRow;
  pricing: PriceResult;
  business: BusinessProfile;
  /** The customer's account, when the viewer may read it (for the address and tax number). */
  account?: RecordRow | null;
}

export function documentHtml({ object, record, pricing, business, account }: PrintInput): string {
  const v = record.values;
  const bp = business.profile;
  const money = (n: number) => formatMoney(n, pricing.currency);
  const title = object === 'quotes' ? 'Quotation' : object === 'sales_orders' ? 'Sales Order' : bp.gstin ? 'Tax Invoice' : 'Invoice';
  const meta: Array<[string, string]> = [[`${title} no.`, record.code]];
  if (object === 'quotes') meta.push(['Date', day(v.quoteDate ?? record.createdAt)], ['Valid until', day(v.validUntil)]);
  if (object === 'sales_orders') meta.push(['Date', day(v.orderDate ?? record.createdAt)]);
  if (object === 'invoices') meta.push(['Date', day(v.invoiceDate ?? record.createdAt)], ['Due date', day(v.dueDate)]);
  const from = lines(bp.address, lines(bp.city, bp.state, bp.postalCode).join(', '), bp.country);
  const contact = lines(bp.phone, bp.email, bp.website).join(' · ');
  const a = account?.values ?? {};
  const customer = record.lookups.accountId?.label?.replace(/ · [A-Z]+-\d+$/, '') ?? '';
  const person = record.lookups.contactId?.label?.replace(/ · [A-Z]+-\d+$/, '') ?? '';
  const to = lines(a.billingStreet, lines(a.billingCity, a.billingState, a.billingPostalCode).join(', '), a.billingCountry);
  const anyDiscount = pricing.lines.some((l) => l.discountAmount > 0);
  const rows = pricing.lines
    .map(
      (l, i) => `<tr${l.bundleOf != null ? ' class="part"' : ''}>
        <td class="n">${l.bundleOf != null ? '' : i + 1}</td>
        <td>${esc(l.name)}${l.sku ? `<div class="sub">${esc(l.sku)}</div>` : ''}</td>
        <td class="r">${esc(l.quantity)}</td>
        <td class="r">${esc(money(l.unitPrice))}</td>
        ${anyDiscount ? `<td class="r">${l.discountAmount ? esc(money(l.discountAmount)) : ''}</td>` : ''}
        <td class="r">${l.taxRate ? `${esc(l.taxRate)}%` : ''}</td>
        <td class="r">${l.taxAmount ? esc(money(l.taxAmount)) : ''}</td>
        <td class="r b">${esc(money(l.total))}</td>
      </tr>`
    )
    .join('');
  const totals: Array<[string, string, boolean?]> = [['Subtotal', money(pricing.subtotal)]];
  if (pricing.discount > 0) totals.push(['Discount', `− ${money(pricing.discount)}`]);
  if (pricing.tax > 0) totals.push(['Tax', money(pricing.tax)]);
  totals.push(['Total', money(pricing.total), true]);
  if (object === 'invoices') {
    const num = (k: string) => Number(v[k] ?? 0) || 0;
    if (num('amountPaid') > 0) totals.push(['Paid', `− ${money(num('amountPaid'))}`]);
    if (num('creditedAmount') > 0) totals.push(['Credit notes', `− ${money(num('creditedAmount'))}`]);
    if (v.balanceDue !== undefined && v.balanceDue !== null) totals.push(['Balance due', money(num('balanceDue')), true]);
  }
  const terms = text(v.terms);
  const notes = text(v.description);
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>${esc(`${title} ${record.code}`)}</title><style>
    @page { size: A4; margin: 16mm 14mm; }
    * { box-sizing: border-box; }
    body { font: 12px/1.45 -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #111; margin: 0; }
    .page { max-width: 780px; margin: 0 auto; padding: 24px; }
    header { display: flex; justify-content: space-between; gap: 24px; border-bottom: 2px solid #111; padding-bottom: 14px; }
    h1 { font-size: 20px; margin: 0 0 4px; }
    h2 { font-size: 18px; margin: 0 0 8px; text-align: right; letter-spacing: .04em; text-transform: uppercase; }
    .muted, .sub { color: #555; }
    .sub { font-size: 11px; }
    .meta td { padding: 1px 0 1px 14px; text-align: right; }
    .meta td:first-child { color: #555; }
    .parties { display: flex; gap: 24px; margin: 16px 0; }
    .parties > div { flex: 1; }
    .label { font-size: 10px; text-transform: uppercase; letter-spacing: .06em; color: #555; margin-bottom: 3px; }
    table.items { width: 100%; border-collapse: collapse; margin-top: 6px; }
    .items th { text-align: left; font-size: 10px; text-transform: uppercase; letter-spacing: .05em; color: #555; border-bottom: 1px solid #111; padding: 6px 6px; }
    .items td { padding: 7px 6px; border-bottom: 1px solid #ddd; vertical-align: top; }
    .items tr.part td { color: #555; padding-left: 18px; }
    .r { text-align: right !important; white-space: nowrap; font-variant-numeric: tabular-nums; }
    .n { width: 26px; color: #555; }
    .b { font-weight: 600; }
    .totals { margin: 12px 0 0 auto; width: 280px; border-collapse: collapse; }
    .totals td { padding: 4px 6px; }
    .totals td:last-child { text-align: right; font-variant-numeric: tabular-nums; }
    .totals tr.strong td { font-weight: 700; font-size: 13px; border-top: 1px solid #111; }
    .block { margin-top: 18px; white-space: pre-wrap; }
    footer { margin-top: 36px; display: flex; justify-content: space-between; align-items: flex-end; color: #555; font-size: 11px; }
    .sign { border-top: 1px solid #111; padding-top: 4px; width: 200px; text-align: center; color: #111; }
    tr { page-break-inside: avoid; }
    @media print { .page { padding: 0; } }
  </style></head><body><div class="page">
    <header>
      <div>
        <h1>${esc(business.name)}</h1>
        ${from.map((l) => `<div>${esc(l)}</div>`).join('')}
        ${contact ? `<div class="muted">${esc(contact)}</div>` : ''}
        ${bp.gstin ? `<div>GSTIN: <b>${esc(bp.gstin)}</b></div>` : ''}
        ${bp.pan ? `<div>PAN: ${esc(bp.pan)}</div>` : ''}
      </div>
      <div>
        <h2>${esc(title)}</h2>
        <table class="meta">${meta.filter(([, val]) => val).map(([k, val]) => `<tr><td>${esc(k)}</td><td><b>${esc(val)}</b></td></tr>`).join('')}</table>
      </div>
    </header>
    <div class="parties">
      <div>
        <div class="label">${object === 'quotes' ? 'Quotation for' : 'Bill to'}</div>
        <div class="b">${esc(customer || person || record.title)}</div>
        ${customer && person ? `<div>Attn: ${esc(person)}</div>` : ''}
        ${to.map((l) => `<div>${esc(l)}</div>`).join('')}
        ${lines(a.phone, a.email).length ? `<div class="muted">${esc(lines(a.phone, a.email).join(' · '))}</div>` : ''}
      </div>
      <div>
        <div class="label">Subject</div>
        <div>${esc(record.title)}</div>
      </div>
    </div>
    <table class="items">
      <thead><tr><th>#</th><th>Item</th><th class="r">Qty</th><th class="r">Rate</th>${anyDiscount ? '<th class="r">Discount</th>' : ''}<th class="r">Tax %</th><th class="r">Tax</th><th class="r">Amount</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>
    <table class="totals">${totals.map(([k, val, strong]) => `<tr${strong ? ' class="strong"' : ''}><td>${esc(k)}</td><td>${esc(val)}</td></tr>`).join('')}</table>
    ${terms ? `<div class="block"><div class="label">Terms</div>${esc(terms)}</div>` : ''}
    ${notes ? `<div class="block"><div class="label">Notes</div>${esc(notes)}</div>` : ''}
    <footer>
      <div>${esc(title)} ${esc(record.code)} · ${esc(business.name)}</div>
      <div class="sign">For ${esc(business.name)}</div>
    </footer>
  </div></body></html>`;
}

/**
 * Opens the document in its own window and starts the print dialog. `win` must have
 * been opened in the click handler (browsers block windows opened later).
 */
export function printInto(win: Window, html: string) {
  win.document.open();
  win.document.write(html);
  win.document.close();
  win.focus();
  // Give the new document a moment to lay out before the dialog snapshots it.
  setTimeout(() => win.print(), 250);
}
