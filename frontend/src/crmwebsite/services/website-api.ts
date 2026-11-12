// The website talks only to the CRM's public endpoints: no sign-in, nothing private.
import { API_ORIGIN } from '@crm/api/client';

const BASE = `${API_ORIGIN}/api/crm/v1`;

export interface Plan {
  key: string;
  months: number;
  price: number;
}
export interface Industry {
  key: string;
  label: string;
}

export interface WebsiteRequest {
  kind: 'call' | 'enquiry';
  name: string;
  business: string;
  phone: string;
  email: string;
  industry: string;
  teamSize?: string;
  currentMethod?: string;
  challenge?: string;
  preferredDate?: string;
  preferredTime?: string;
  subject?: string;
  category?: string;
  message: string;
  /** Hidden from people; a robot that fills it is ignored. */
  website: string;
}

export class RequestError extends Error {
  constructor(message: string, public fields: Record<string, string> = {}, public status = 0) {
    super(message);
  }
}

export async function sendRequest(body: WebsiteRequest): Promise<{ reference?: string }> {
  let res: Response;
  try {
    res = await fetch(`${BASE}/public/enquiries`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  } catch {
    throw new RequestError('You seem to be offline. Please check your connection and try again.');
  }
  const data = await res.json().catch(() => ({}));
  if (res.ok) return data as { reference?: string };
  if (res.status === 429) throw new RequestError('We are receiving many requests right now. Please try again in a few minutes.', {}, 429);
  throw new RequestError(data?.message || 'Sorry, that did not go through. Please try again.', data?.fieldErrors ?? {}, res.status);
}

export async function getPlans(): Promise<Plan[]> {
  const res = await fetch(`${BASE}/public/billing/plans`);
  if (!res.ok) return [];
  const d = await res.json();
  return d?.prices ?? [];
}

export async function getIndustries(): Promise<Industry[]> {
  const res = await fetch(`${BASE}/public/industries`);
  if (!res.ok) return [];
  const d = await res.json();
  return (d?.data ?? []).map((x: Industry) => ({ key: x.key, label: x.label }));
}
