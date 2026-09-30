/** Parses CSV text (RFC 4180: quotes, escaped quotes, newlines inside quotes; , ; or tab). */
export function parseCsv(text: string): string[][] {
  const src = text.replace(/^﻿/, '');
  const firstLine = src.slice(0, src.indexOf('\n') > 0 ? src.indexOf('\n') : src.length);
  const counts = [',', ';', '\t'].map((d) => firstLine.split(d).length);
  const delim = [',', ';', '\t'][counts.indexOf(Math.max(...counts))]!;
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  for (let i = 0; i < src.length; i++) {
    const c = src[i]!;
    if (quoted) {
      if (c === '"') {
        if (src[i + 1] === '"') {
          cell += '"';
          i++;
        } else quoted = false;
      } else cell += c;
      continue;
    }
    if (c === '"' && cell === '') quoted = true;
    else if (c === delim) {
      row.push(cell);
      cell = '';
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && src[i + 1] === '\n') i++;
      row.push(cell);
      rows.push(row);
      row = [];
      cell = '';
    } else cell += c;
  }
  if (cell !== '' || row.length) {
    row.push(cell);
    rows.push(row);
  }
  return rows.filter((r) => r.some((x) => x.trim() !== ''));
}

const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '');

/** Guesses which field a column heading means ("E-mail" → email, "Company" → organization). */
export function guessField(heading: string, fields: Array<{ key: string; label: string }>): string {
  const h = norm(heading);
  if (!h) return '';
  const aliases: Record<string, string[]> = {
    email: ['email', 'emailaddress', 'mail'],
    phone: ['phone', 'phonenumber', 'telephone', 'tel'],
    mobile: ['mobile', 'cell', 'mobilenumber', 'whatsapp'],
    organization: ['company', 'organisation', 'organization', 'business', 'companyname'],
    firstName: ['firstname', 'givenname', 'first'],
    lastName: ['lastname', 'surname', 'familyname', 'last'],
    name: ['name', 'fullname', 'accountname', 'title'],
    code: ['recordid', 'id', 'code'],
    website: ['website', 'web', 'url', 'site']
  };
  for (const f of fields) if (norm(f.label) === h || norm(f.key) === h) return f.key;
  for (const [key, list] of Object.entries(aliases)) if (list.includes(h) && fields.some((f) => f.key === key)) return key;
  for (const f of fields) if (norm(f.label).includes(h) || h.includes(norm(f.label))) return f.key;
  return '';
}
