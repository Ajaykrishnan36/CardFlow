// Menu entries that are one object seen through a fixed filter (D-117): Vendors are
// accounts of type vendor/supplier, Services are catalog items of type service.
// Used by both the desktop list page and the phone list screen.
const PRESETS = {
  accounts: {
    param: 'type',
    values: {
      vendor: { label: 'Vendors', singular: 'vendor', in: ['vendor', 'supplier'], create: { type: 'vendor' } },
      customer: { label: 'Customers', singular: 'customer', in: ['customer'], create: { type: 'customer' } },
      partner: { label: 'Partners', singular: 'partner', in: ['partner', 'reseller', 'distributor'], create: { type: 'partner' } }
    }
  },
  catalog_items: {
    param: 'itemType',
    values: {
      service: { label: 'Services', singular: 'service', in: ['service'], create: { itemType: 'service' } },
      product: { label: 'Products', singular: 'product', in: ['product'], create: { itemType: 'product' } }
    }
  }
};

/** The preset a list URL asks for, or null. `search` is the query string or URLSearchParams. */
export function listPreset(object, search) {
  const def = PRESETS[object];
  if (!def) return null;
  const q = typeof search === 'string' ? new URLSearchParams(search) : search;
  const value = q.get(def.param);
  const preset = value ? def.values[value] : null;
  if (!preset) return null;
  return { ...preset, field: def.param, value, query: `${def.param}=${value}`, filter: { field: def.param, op: 'in', value: preset.in } };
}
