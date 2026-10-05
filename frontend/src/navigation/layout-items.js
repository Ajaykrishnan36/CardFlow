// Dashboard and menu arrangement (D-130), shared by the desktop CRM and the phone layout.
// A layout is { order: [keys], hidden: [keys] }. Items it doesn't mention keep their
// standard place after the ones it does, so something new in the product still shows.

/** Put items in the saved order and drop the hidden ones. */
export function arrange(items, keyOf, layout) {
  if (!layout || (!layout.order?.length && !layout.hidden?.length)) return items;
  const hidden = new Set(layout.hidden || []);
  const pos = new Map((layout.order || []).map((k, i) => [k, i]));
  return items
    .map((item, i) => ({ item, i, k: keyOf(item) }))
    .filter((x) => !hidden.has(x.k))
    .sort((a, b) => (pos.has(a.k) ? pos.get(a.k) : 10000 + a.i) - (pos.has(b.k) ? pos.get(b.k) : 10000 + b.i))
    .map((x) => x.item);
}

export const isHidden = (layout, key) => Boolean(layout?.hidden?.includes(key));

/** Menu entries that can't be hidden (the way back, and the settings). */
export const LOCKED_NAV = ['home', 'dashboard', 'overview', 'admin', 'access', 'settings'];

// A business's dashboard: its sections, and the tiles of the summary.
export const DASHBOARD_SECTIONS = {
  desktop: [
    { key: 'section:quick', label: 'Quick actions' },
    { key: 'section:start', label: 'Getting started' },
    { key: 'section:summary', label: 'Business summary' },
    { key: 'section:kpis', label: 'Record counts' },
    { key: 'section:recent', label: 'Recent records' }
  ],
  mobile: [
    { key: 'section:start', label: 'Getting started' },
    { key: 'section:summary', label: 'Business summary' },
    { key: 'section:quick', label: 'Quick add' },
    { key: 'section:recent', label: 'Lists (follow-ups, tasks, deals…)' }
  ]
};

export const SUMMARY_TILES = {
  desktop: [
    { key: 'summary:activeLeads', label: 'Active leads' },
    { key: 'summary:followUps', label: 'Follow-ups due' },
    { key: 'summary:contacts', label: 'Contacts' },
    { key: 'summary:accounts', label: 'Accounts' },
    { key: 'summary:openDeals', label: 'Open deals' },
    { key: 'summary:won', label: 'Won' },
    { key: 'summary:tasks', label: 'Tasks due today' },
    { key: 'summary:cases', label: 'Open cases' },
    { key: 'summary:income', label: 'Income' },
    { key: 'summary:expenses', label: 'Expenses' },
    { key: 'summary:net', label: 'Net income' }
  ],
  mobile: [
    { key: 'summary:finance', label: 'Income and expenses' },
    { key: 'summary:activeLeads', label: 'Active leads' },
    { key: 'summary:followUps', label: 'Follow-ups today' },
    { key: 'summary:contacts', label: 'Contacts' },
    { key: 'summary:accounts', label: 'Accounts' },
    { key: 'summary:openDeals', label: 'Open deals' },
    { key: 'summary:won', label: 'Won' },
    { key: 'summary:tasks', label: 'Tasks today' },
    { key: 'summary:meetings', label: 'Meetings ahead' },
    { key: 'summary:cases', label: 'Open cases' }
  ]
};

// The owner console's dashboard.
export const OWNER_SECTIONS = [
  { key: 'section:quick', label: 'Quick actions' },
  { key: 'section:kpis', label: 'Totals' },
  { key: 'section:checklist', label: 'Setup checklist' },
  { key: 'section:recent', label: 'Recent products' }
];
