// Money (D-76): an amount can carry its own currency; without one it's in the
// product's currency (set from the workspace context).

export const CURRENCIES = ['INR', 'USD', 'EUR', 'GBP', 'AED', 'SAR', 'QAR', 'KWD', 'OMR', 'BHD', 'SGD', 'MYR', 'THB', 'IDR', 'PHP', 'VND', 'JPY', 'CNY', 'HKD', 'KRW', 'AUD', 'NZD', 'CAD', 'CHF', 'SEK', 'NOK', 'DKK', 'ZAR', 'NGN', 'KES', 'EGP', 'LKR', 'NPR', 'BDT', 'PKR', 'MXN', 'BRL'] as const;

let fallback = 'INR';

export function setDefaultCurrency(code: string | undefined) {
  if (code && /^[A-Z]{3}$/.test(code)) fallback = code;
}

export function defaultCurrency(): string {
  return fallback;
}

const cache = new Map<string, Intl.NumberFormat>();

export function formatMoney(n: number, code?: string | null): string {
  const c = code && /^[A-Z]{3}$/.test(code) ? code : fallback;
  let f = cache.get(c);
  if (!f) {
    f = new Intl.NumberFormat(c === 'INR' ? 'en-IN' : undefined, { style: 'currency', currency: c, maximumFractionDigits: 2 });
    cache.set(c, f);
  }
  return f.format(n);
}

export function currencySymbol(code: string): string {
  try {
    return (
      new Intl.NumberFormat(code === 'INR' ? 'en-IN' : undefined, { style: 'currency', currency: code, currencyDisplay: 'narrowSymbol' })
        .formatToParts(0)
        .find((p) => p.type === 'currency')?.value ?? code
    );
  } catch {
    return code;
  }
}

/** Calling codes for the phone country picker. */
export const DIAL_CODES: Array<{ code: string; country: string }> = [
  { code: '+91', country: 'IN' }, { code: '+1', country: 'US/CA' }, { code: '+44', country: 'GB' }, { code: '+971', country: 'AE' },
  { code: '+966', country: 'SA' }, { code: '+974', country: 'QA' }, { code: '+965', country: 'KW' }, { code: '+968', country: 'OM' },
  { code: '+973', country: 'BH' }, { code: '+65', country: 'SG' }, { code: '+60', country: 'MY' }, { code: '+66', country: 'TH' },
  { code: '+62', country: 'ID' }, { code: '+63', country: 'PH' }, { code: '+84', country: 'VN' }, { code: '+81', country: 'JP' },
  { code: '+86', country: 'CN' }, { code: '+852', country: 'HK' }, { code: '+82', country: 'KR' }, { code: '+61', country: 'AU' },
  { code: '+64', country: 'NZ' }, { code: '+49', country: 'DE' }, { code: '+33', country: 'FR' }, { code: '+39', country: 'IT' },
  { code: '+34', country: 'ES' }, { code: '+31', country: 'NL' }, { code: '+41', country: 'CH' }, { code: '+46', country: 'SE' },
  { code: '+47', country: 'NO' }, { code: '+45', country: 'DK' }, { code: '+27', country: 'ZA' }, { code: '+234', country: 'NG' },
  { code: '+254', country: 'KE' }, { code: '+20', country: 'EG' }, { code: '+94', country: 'LK' }, { code: '+977', country: 'NP' },
  { code: '+880', country: 'BD' }, { code: '+92', country: 'PK' }, { code: '+52', country: 'MX' }, { code: '+55', country: 'BR' }
];

/** "+91 98765 43210" → { dial: '+91', rest: '98765 43210' } (longest known code first). */
export function splitPhone(v: string, fallbackDial = '+91'): { dial: string; rest: string } {
  const s = v.trim();
  if (s.startsWith('+')) {
    const hit = [...DIAL_CODES].sort((a, b) => b.code.length - a.code.length).find((d) => s.startsWith(d.code));
    if (hit) return { dial: hit.code, rest: s.slice(hit.code.length).trim() };
  }
  return { dial: fallbackDial, rest: s };
}
