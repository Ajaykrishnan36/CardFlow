import { useSyncExternalStore } from 'react';

// Which app (setup) of a product the viewer works in (D-73). Kept per product in
// this browser; the server falls back to the first app when it's missing or gone.

const key = (code: string) => `crm.app.${code}`;
const listeners = new Set<() => void>();

function read(code: string): string {
  try {
    return localStorage.getItem(key(code)) ?? '';
  } catch {
    return '';
  }
}

export function setSelectedApp(code: string, app: string) {
  try {
    localStorage.setItem(key(code), app);
  } catch {
    /* private mode: the choice lasts until reload */
  }
  memory.set(code, app);
  listeners.forEach((l) => l());
}

const memory = new Map<string, string>();

export function useSelectedApp(code: string | undefined): string {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => (code ? (memory.get(code) ?? read(code)) : '')
  );
}
