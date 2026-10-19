import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { LocateFixed } from 'lucide-react';
import { onboardingApi, type PostalAddress } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';

export const emptyAddress = (): PostalAddress => ({ addressLine: '', landmark: '', area: '', city: '', district: '', state: '', postalCode: '', country: 'India' });

export function addressFilled(a: PostalAddress | undefined): boolean {
  return Boolean(a && (a.addressLine || a.area || a.city || a.state || a.postalCode || a.landmark));
}

// ISO 3166-1 country codes; names come from the browser in the reader's language.
const COUNTRY_CODES =
  'AF AX AL DZ AS AD AO AI AQ AG AR AM AW AU AT AZ BS BH BD BB BY BE BZ BJ BM BT BO BQ BA BW BV BR IO BN BG BF BI CV KH CM CA KY CF TD CL CN CX CC CO KM CG CD CK CR CI HR CU CW CY CZ DK DJ DM DO EC EG SV GQ ER EE SZ ET FK FO FJ FI FR GF PF TF GA GM GE DE GH GI GR GL GD GP GU GT GG GN GW GY HT HM VA HN HK HU IS IN ID IR IQ IE IM IL IT JM JP JE JO KZ KE KI KP KR KW KG LA LV LB LS LR LY LI LT LU MO MG MW MY MV ML MT MH MQ MR MU YT MX FM MD MC MN ME MS MA MZ MM NA NR NP NL NC NZ NI NE NG NU NF MK MP NO OM PK PW PS PA PG PY PE PH PN PL PT PR QA RE RO RU RW BL SH KN LC MF PM VC WS SM ST SA SN RS SC SL SG SX SK SI SB SO ZA GS SS ES LK SD SR SJ SE CH SY TW TJ TZ TH TL TG TK TO TT TN TR TM TC TV UG UA AE GB US UM UY UZ VU VE VN VG VI WF EH YE ZM ZW'.split(' ');

function useCountries(): string[] {
  return useMemo(() => {
    let names: string[] = [];
    try {
      const dn = new Intl.DisplayNames(['en'], { type: 'region' });
      names = COUNTRY_CODES.map((c) => dn.of(c) ?? c);
    } catch {
      names = ['India'];
    }
    return ['India', ...names.filter((n) => n !== 'India').sort((a, b) => a.localeCompare(b))];
  }, []);
}

/**
 * A postal address picked rather than typed (D-155): house / building, landmark and area;
 * then the pincode (fills state, city and the areas it covers) or the device's location;
 * country and state from lists. Errors are keyed `${prefix}${field}` as the API sends them.
 */
export function AddressFields({
  value,
  onChange,
  errors = {},
  prefix = '',
  disabled
}: {
  value: PostalAddress;
  onChange: (next: PostalAddress) => void;
  errors?: Record<string, string>;
  prefix?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const countries = useCountries();
  const states = useQuery({ queryKey: ['geo', 'states'], queryFn: onboardingApi.states, staleTime: Infinity });
  const india = !value.country || value.country === 'India';
  const [areas, setAreas] = useState<string[]>([]);
  const [cities, setCities] = useState<string[]>([]);
  const [note, setNote] = useState<{ tone: 'ok' | 'err'; text: string } | null>(null);
  const [locating, setLocating] = useState(false);
  const latest = useRef(value);
  latest.current = value;
  const set = (patch: Partial<PostalAddress>) => onChange({ ...latest.current, ...patch });
  const err = (k: string) => errors[prefix + k];

  // A full Indian pincode fills state, city and district, and offers the areas it covers.
  const lookedUp = useRef('');
  useEffect(() => {
    const pin = value.postalCode.trim();
    if (!india || !/^[1-9][0-9]{5}$/.test(pin) || lookedUp.current === pin) return;
    lookedUp.current = pin;
    let live = true;
    onboardingApi
      .pincode(pin)
      .then((p) => {
        if (!live) return;
        setAreas(p.areas);
        setCities(Array.from(new Set([p.city, p.district].filter(Boolean))));
        const cur = latest.current;
        set({
          state: p.state,
          city: p.city,
          district: p.district,
          country: 'India',
          area: cur.area || (p.areas.length === 1 ? p.areas[0] : cur.area)
        });
        setNote({ tone: 'ok', text: `Filled from pincode: ${p.city}, ${p.state}. Pick your area below if it isn't right.` });
      })
      .catch((e) => live && setNote({ tone: 'err', text: isApiError(e) ? e.fieldErrors.postalCode ?? e.message : 'Couldn’t look up the pincode.' }));
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.postalCode, india]);

  const useLocation = () => {
    setNote(null);
    if (!('geolocation' in navigator)) {
      setNote({ tone: 'err', text: 'This device can’t share its location. Enter the pincode instead.' });
      return;
    }
    setLocating(true);
    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        try {
          const a = await onboardingApi.reverse(pos.coords.latitude, pos.coords.longitude);
          const cur = latest.current;
          lookedUp.current = a.postalCode ?? '';
          onChange({
            ...cur,
            addressLine: cur.addressLine || a.addressLine || '',
            area: a.area || cur.area,
            city: a.city || cur.city,
            district: a.district || cur.district,
            state: a.state || cur.state,
            postalCode: a.postalCode || cur.postalCode,
            country: a.country || cur.country || 'India',
            latitude: pos.coords.latitude,
            longitude: pos.coords.longitude
          });
          if (a.area) setAreas((x) => Array.from(new Set([a.area!, ...x])));
          setNote({ tone: 'ok', text: 'Filled from your current location. Check it, and add your house or building.' });
        } catch (e) {
          setNote({ tone: 'err', text: isApiError(e) ? e.message : 'Couldn’t find the address for your location.' });
        } finally {
          setLocating(false);
        }
      },
      (e) => {
        setLocating(false);
        setNote({ tone: 'err', text: e.code === e.PERMISSION_DENIED ? 'Location permission was refused. Enter the pincode instead.' : 'Couldn’t get your location. Enter the pincode instead.' });
      },
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 }
    );
  };

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label="House no. / building name" error={err('addressLine')} className="sm:col-span-2">
        <Input value={value.addressLine} onChange={(e) => set({ addressLine: e.target.value })} placeholder="12, Lakshmi Towers, 2nd floor" maxLength={200} disabled={disabled} autoComplete="address-line1" />
      </Field>
      <Field label="Landmark" error={err('landmark')}>
        <Input value={value.landmark} onChange={(e) => set({ landmark: e.target.value })} placeholder="Opposite bus stand" maxLength={200} disabled={disabled} />
      </Field>
      <div>
        <Field label="Area" error={err('area')} hint={areas.length > 1 ? `${areas.length} areas for this pincode — pick yours` : undefined}>
          <Input value={value.area} onChange={(e) => set({ area: e.target.value })} list={`${id}-areas`} placeholder="Gandhipuram" maxLength={200} disabled={disabled} autoComplete="address-line2" />
        </Field>
        <datalist id={`${id}-areas`}>
          {areas.map((a) => (
            <option key={a} value={a} />
          ))}
        </datalist>
      </div>
      <Field label="Pincode" error={err('postalCode')}>
        <Input
          value={value.postalCode}
          onChange={(e) => set({ postalCode: india ? e.target.value.replace(/\D/g, '').slice(0, 6) : e.target.value })}
          inputMode={india ? 'numeric' : 'text'}
          placeholder={india ? '641001' : 'Postal code'}
          maxLength={india ? 6 : 12}
          disabled={disabled}
          autoComplete="postal-code"
        />
      </Field>
      <div className="flex items-end">
        <Button type="button" variant="outline" className="w-full" onClick={useLocation} loading={locating} disabled={disabled}>
          <LocateFixed /> Use my current location
        </Button>
      </div>
      {note ? <p className={`-mt-2 text-xs sm:col-span-2 ${note.tone === 'ok' ? 'text-success' : 'text-danger'}`}>{note.text}</p> : null}
      <Field label="Country" error={err('country')}>
        <Select value={value.country || 'India'} onChange={(e) => set({ country: e.target.value, state: '', city: '' })} options={countries.map((c) => ({ value: c, label: c }))} disabled={disabled} />
      </Field>
      <Field label="State" error={err('state')}>
        {india ? (
          <Select
            value={value.state}
            onChange={(e) => set({ state: e.target.value })}
            placeholder="Choose…"
            options={(states.data?.data ?? []).map((s) => ({ value: s.name, label: s.name }))}
            disabled={disabled}
          />
        ) : (
          <Input value={value.state} onChange={(e) => set({ state: e.target.value })} placeholder="State / province" maxLength={200} disabled={disabled} autoComplete="address-level1" />
        )}
      </Field>
      <div className="sm:col-span-2">
        <Field label="City" error={err('city')}>
          <Input value={value.city} onChange={(e) => set({ city: e.target.value })} list={`${id}-cities`} placeholder="Coimbatore" maxLength={200} disabled={disabled} autoComplete="address-level2" />
        </Field>
        <datalist id={`${id}-cities`}>
          {cities.map((c) => (
            <option key={c} value={c} />
          ))}
        </datalist>
      </div>
    </div>
  );
}
