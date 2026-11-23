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
  // The owner's leads by what was asked for on the website (D-181).
  leads: {
    param: 'requestType',
    values: {
      call: {
        label: 'Call requests', singular: 'call request', in: ['call'], create: { requestType: 'call', source: 'website_call', callStatus: 'requested' },
        columns: ['organization', 'mobile', 'industry', 'preferredDate', 'preferredTime', 'callStatus', 'status', 'ownerId', 'createdAt']
      },
      enquiry: {
        label: 'Website enquiries', singular: 'enquiry', in: ['enquiry'], create: { requestType: 'enquiry', source: 'website_enquiry' },
        columns: ['organization', 'mobile', 'subject', 'enquiryCategory', 'industry', 'status', 'ownerId', 'createdAt']
      },
      signup: { label: 'Sign-ups', singular: 'sign-up', in: ['signup'], create: {} }
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
  const q = typeof search === 'string' ? new URLSearchParams(search) : search;
  // A law firm's leads waiting for an owner, and one matter's documents or tasks (D-185).
  if (object === 'leads' && q.get('queue') === 'unassigned') {
    return { label: 'Unassigned leads', singular: 'lead', field: 'ownerId', value: 'unassigned', query: 'queue=unassigned', in: [], filter: { field: 'ownerId', op: 'empty' }, create: { sendToQueue: true } };
  }
  if ((object === 'legal_documents' || object === 'tasks') && q.get('matterId')) {
    const id = q.get('matterId');
    const category = q.get('category');
    return { label: object === 'tasks' ? 'Tasks of this matter' : 'Documents of this matter', singular: object === 'tasks' ? 'task' : 'document', field: 'matterId', value: id,
      query: `matterId=${encodeURIComponent(id)}`, in: [id], filter: { field: 'matterId', op: 'in', value: [id] }, create: { matterId: id, ...(category ? { category } : {}) }, createLabels: q.get('matterName') ? { matterId: q.get('matterName') } : undefined };
  }
  const def = PRESETS[object];
  if (!def) return null;
  const value = q.get(def.param);
  const preset = value ? def.values[value] : null;
  if (!preset) return null;
  return { ...preset, field: def.param, value, query: `${def.param}=${value}`, filter: { field: def.param, op: 'in', value: preset.in } };
}
