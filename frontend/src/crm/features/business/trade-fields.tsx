import { useQuery } from '@tanstack/react-query';
import { Sparkles } from 'lucide-react';
import { registerApi, type IndustryOption } from '@crm/api/endpoints';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { SearchSelect } from '@crm/components/ui/search-select';

export interface Trade {
  industry: string;
  industryOther: string;
  businessType: string;
  businessTypeOther: string;
}

export const emptyTrade: Trade = { industry: '', industryOther: '', businessType: '', businessTypeOther: '' };

export function useIndustries() {
  return useQuery({ queryKey: ['public', 'industries'], queryFn: registerApi.industries, staleTime: 30 * 60_000 });
}

export function findIndustry(list: IndustryOption[] | undefined, label: string) {
  const v = label.trim().toLowerCase();
  return list?.find((i) => i.label.toLowerCase() === v || i.key === v);
}

/** Problems with the trade before it is sent (the server checks the same). */
export function tradeErrors(t: Trade, list: IndustryOption[] | undefined): Record<string, string> {
  const e: Record<string, string> = {};
  const ind = findIndustry(list, t.industry);
  if (!t.industry) e.industry = 'Choose your industry.';
  else if (ind?.key === 'other') {
    if (t.industryOther.trim().length < 2) e.industryOther = 'Enter your business industry.';
  } else if (ind && ind.types.length > 0) {
    if (!t.businessType) e.businessType = `Choose the ${ind.typeLabel.toLowerCase()}.`;
    else if (t.businessType === 'Other' && t.businessTypeOther.trim().length < 2) e.businessTypeOther = 'Enter your type of business.';
  }
  return e;
}

/** What the CRM switches on for the trade, in one line under the boxes. */
function whatYouGet(ind: IndustryOption | undefined, type: string): string {
  const car = /car|both|commercial|electric/i.test(type);
  const bike = !/^car$/i.test(type);
  switch (ind?.packs?.[0]) {
    case 'dealership':
      return `Vehicle stock by VIN, bookings, ${car && !bike ? 'test drives' : car ? 'test rides & drives' : 'test rides'}, registrations, delivery and the service schedule${
        car && bike ? ' — bikes and cars side by side' : car ? ' — with body type, fuel and transmission' : ' — with bike categories'
      }.`;
    case 'insurance':
      return 'Policies, the premium schedule, renewal reminders, claims and commissions.';
    case 'legal':
      return 'Client matters with court and case number, hearings with the next date, consultations and engagement agreements.';
    case 'real_estate':
      return 'Units & listings, holds & bookings and site visits.';
    case 'salon':
      return 'Product stock with expiry, kits and their bookings, and client packages.';
  }
  return 'The common CRM: leads, contacts, opportunities, quotes, orders, invoices, payments and cases.';
}

/**
 * Industry type, then the business types of that industry (D-174): both searchable, one
 * choice each. "Other" asks for the trade in the person's own words.
 */
export function TradeFields({
  value,
  onChange,
  errors,
  requiredMark = true,
  disabled,
}: {
  value: Trade;
  onChange: (t: Trade) => void;
  errors: Record<string, string | undefined>;
  requiredMark?: boolean;
  disabled?: boolean;
}) {
  const industries = useIndustries();
  const list = industries.data?.data;
  const ind = findIndustry(list, value.industry);
  const star = requiredMark ? ' *' : '';
  const isOther = ind?.key === 'other';

  return (
    <>
      <Field label={`Industry type${star}`} error={errors.industry}>
        <SearchSelect
          value={ind?.label ?? value.industry}
          onChange={(v) => onChange({ industry: v, industryOther: '', businessType: '', businessTypeOther: '' })}
          options={(list ?? []).map((i) => ({ value: i.label, label: i.label, keywords: i.keywords }))}
          placeholder={industries.isPending ? 'Loading…' : 'Search: bike, lawyer, insurance…'}
          invalid={!!errors.industry}
          disabled={disabled}
        />
      </Field>
      {isOther ? (
        <>
          <Field label={`Enter your business industry${star}`} error={errors.industryOther}>
            <Input
              value={value.industryOther}
              onChange={(e) => onChange({ ...value, industryOther: e.target.value })}
              placeholder="Pet grooming"
              maxLength={80}
              invalid={!!errors.industryOther}
              disabled={disabled}
            />
          </Field>
          <Field label="Type of business" error={errors.businessType}>
            <Input
              value={value.businessType}
              onChange={(e) => onChange({ ...value, businessType: e.target.value })}
              placeholder="Optional"
              maxLength={80}
              disabled={disabled}
            />
          </Field>
        </>
      ) : ind && ind.types.length > 0 ? (
        <>
          <Field label={`${ind.typeLabel}${star}`} error={errors.businessType}>
            <SearchSelect
              value={value.businessType}
              onChange={(v) => onChange({ ...value, businessType: v, businessTypeOther: '' })}
              options={ind.types.map((t) => ({ value: t.label, label: t.label }))}
              placeholder="Search or choose…"
              invalid={!!errors.businessType}
              disabled={disabled}
            />
          </Field>
          {value.businessType === 'Other' ? (
            <Field label={`Enter your type of business${star}`} error={errors.businessTypeOther}>
              <Input
                value={value.businessTypeOther}
                onChange={(e) => onChange({ ...value, businessTypeOther: e.target.value })}
                maxLength={80}
                invalid={!!errors.businessTypeOther}
                disabled={disabled}
              />
            </Field>
          ) : null}
        </>
      ) : null}
      {ind ? (
        <p className="flex items-start gap-1.5 text-xs text-muted-foreground sm:col-span-2">
          <Sparkles className="mt-0.5 size-3.5 shrink-0 text-primary" aria-hidden />
          <span>
            <span className="font-medium text-foreground">Your CRM gets: </span>
            {whatYouGet(ind, value.businessType)}
          </span>
        </p>
      ) : null}
    </>
  );
}
