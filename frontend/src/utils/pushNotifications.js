// Local push notifications for the native Android/iOS shell (Capacitor).
// No-op everywhere else (regular web/browser use).
//
// Notifications are scheduled per auth state, and the whole queue for the
// *other* states is cancelled whenever that state changes, so a user never
// gets a "please log in" nudge after they've logged in, etc.
//   - logged_out: alternating reminders to log in (ids 1000-4999)
//   - signed_in: "back up your contacts" nudge (ids 6000-6999)
// Ids 5000-5999 held the old "go premium" nudge; they are still cancelled so a phone
// that scheduled them before paid plans were removed stops showing them.

const PREFS_KEY = 'cf_notification_prefs';

// How far apart the recurring test notifications fire. The product ask was
// "every 5 minutes" specifically for testing this feature end-to-end.
const TEST_INTERVAL_MS = 5 * 60 * 1000;
const TEST_OCCURRENCES = 24;

const LOGGED_OUT_MESSAGES = {
  exploreBusiness: {
    title: 'CardFlow',
    body: 'Login and connect with business people near you!'
  },
  addBusiness: {
    title: 'CardFlow',
    body: "Hurry up! Login and publish your business to everyone."
  }
};

const SIGNED_IN_MESSAGE = {
  title: 'CardFlow',
  body: 'Back up your contacts and keep them safe with CardFlow Cloud.'
};

// Reserved notification-id ranges so cancelling one bucket never touches another.
const ID_RANGES = {
  logged_out: { start: 1000, end: 4999 },
  retiredPremiumNudge: { start: 5000, end: 5999 },
  signed_in: { start: 6000, end: 6999 }
};

const DEFAULT_PREFS = { exploreBusiness: true, addBusiness: true };

export function isNativePlatform() {
  return typeof window !== 'undefined' && !!window.Capacitor?.isNativePlatform?.();
}

export function getNotificationPrefs() {
  try {
    const raw = window.localStorage.getItem(PREFS_KEY);
    if (!raw) return { ...DEFAULT_PREFS };
    return { ...DEFAULT_PREFS, ...JSON.parse(raw) };
  } catch (e) {
    return { ...DEFAULT_PREFS };
  }
}

function savePrefs(prefs) {
  try {
    window.localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
  } catch (e) {
    // ignore
  }
}

// IMPORTANT: never `return`/resolve a Promise with the Capacitor plugin proxy
// itself as the value. The proxy answers ANY property access (including
// `then`) with a callable, so promise machinery mistakes it for a nested
// thenable and calls `.then()` on it — which crashes with "X.then() is not
// implemented on android/ios". Cache the plugin in a plain module variable
// instead, and only ever return/await plain data.
let cachedPlugin = null;
async function ensurePlugin() {
  if (!isNativePlatform() || cachedPlugin) return;
  const mod = await import('@capacitor/local-notifications');
  cachedPlugin = mod.LocalNotifications;
}

export async function requestNotificationPermission() {
  await ensurePlugin();
  const plugin = cachedPlugin;
  if (!plugin) return false;
  const status = await plugin.requestPermissions();
  return status.display === 'granted';
}

// Cancels not-yet-fired notifications in this id range AND clears any that
// already fired and are still sitting in the tray — otherwise a stale
// "please log in" reminder can outlive the login that made it irrelevant.
async function cancelRange(plugin, range) {
  const pending = await plugin.getPending();
  const pendingIds = (pending?.notifications || [])
    .map((n) => n.id)
    .filter((id) => id >= range.start && id <= range.end);
  if (pendingIds.length) {
    await plugin.cancel({ notifications: pendingIds.map((id) => ({ id })) });
  }

  const delivered = await plugin.getDeliveredNotifications();
  const deliveredIds = (delivered?.notifications || [])
    .map((n) => n.id)
    .filter((id) => id >= range.start && id <= range.end);
  if (deliveredIds.length) {
    await plugin.removeDeliveredNotificationsById({ ids: deliveredIds });
  }
}

function buildSchedule(startId, messages) {
  const now = Date.now();
  const notifications = [];
  let id = startId;
  for (let i = 1; i <= TEST_OCCURRENCES; i++) {
    for (const msg of messages) {
      notifications.push({
        id: id++,
        title: msg.title,
        body: msg.body,
        schedule: { at: new Date(now + i * TEST_INTERVAL_MS) }
      });
    }
  }
  return notifications;
}

// (Re)builds the logged-out reminder queue from scratch based on current
// prefs — simplest way to keep it in sync whenever a toggle changes.
export async function rescheduleTestNotifications(prefs = getNotificationPrefs()) {
  await ensurePlugin();
  const plugin = cachedPlugin;
  if (!plugin) return;

  await cancelRange(plugin, ID_RANGES.logged_out);

  const categories = Object.keys(LOGGED_OUT_MESSAGES).filter((key) => prefs[key]);
  if (categories.length === 0) return;

  const messages = categories.map((key) => LOGGED_OUT_MESSAGES[key]);
  await plugin.schedule({ notifications: buildSchedule(ID_RANGES.logged_out.start, messages) });
}

export async function setNotificationPref(key, enabled) {
  const prefs = { ...getNotificationPrefs(), [key]: enabled };
  savePrefs(prefs);
  await rescheduleTestNotifications(prefs);
  return prefs;
}

// Schedules the reminder queue for the given auth state and clears whatever
// was scheduled for the other state. Call this on every login/logout so the phone only ever nudges for the current state.
export async function syncAuthNotifications(authState) {
  await ensurePlugin();
  const plugin = cachedPlugin;
  if (!plugin) return;
  const granted = await requestNotificationPermission();
  if (!granted) return;

  await cancelRange(plugin, ID_RANGES.retiredPremiumNudge);
  await cancelRange(plugin, ID_RANGES.signed_in);

  if (authState === 'logged_out') {
    await rescheduleTestNotifications();
    return;
  }

  // Logged in — the "please log in" reminders no longer apply.
  await cancelRange(plugin, ID_RANGES.logged_out);

  await plugin.schedule({ notifications: buildSchedule(ID_RANGES.signed_in.start, [SIGNED_IN_MESSAGE]) });
}

// Call once at app startup, before auth state is known — just arms the
// permission prompt early so it isn't tied to the first notification sync.
export async function initPushNotifications() {
  if (!isNativePlatform()) return;
  await requestNotificationPermission();
}
