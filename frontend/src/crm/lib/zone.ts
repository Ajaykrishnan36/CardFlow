// The time zone dates and times are shown in: the person's own setting, else the
// business's, else the browser's. Set once by the workspace layout; read by every formatter.
let zone: string | undefined;

export function setDisplayZone(tz?: string | null) {
  try {
    if (tz) new Intl.DateTimeFormat('en-IN', { timeZone: tz }); // throws on a name the browser doesn't know
    zone = tz || undefined;
  } catch {
    zone = undefined;
  }
}

/** Adds the display time zone to date-time format options. */
export function zoned<T extends Intl.DateTimeFormatOptions>(options: T): T {
  return zone ? { ...options, timeZone: zone } : options;
}
