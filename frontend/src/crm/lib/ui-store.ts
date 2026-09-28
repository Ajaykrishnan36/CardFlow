import { create } from 'zustand';

// Client-only UI state (PRD §11: Zustand for sidebar, command menu, drawer, theme).

export type ThemePref = 'light' | 'dark' | 'system';
export type Density = 'comfortable' | 'compact';

const STORAGE_KEY = 'crm_ui_prefs';

interface Persisted {
  theme: ThemePref;
  density: Density;
  sidebarCollapsed: boolean;
}

function load(): Persisted {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const p = JSON.parse(raw) as Partial<Persisted>;
      return {
        theme: p.theme === 'light' || p.theme === 'dark' || p.theme === 'system' ? p.theme : 'system',
        density: p.density === 'compact' ? 'compact' : 'comfortable',
        sidebarCollapsed: Boolean(p.sidebarCollapsed)
      };
    }
  } catch {
    /* storage unavailable — fall back to defaults */
  }
  return { theme: 'system', density: 'comfortable', sidebarCollapsed: false };
}

function save(p: Persisted) {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(p));
  } catch {
    /* ignore */
  }
}

interface UIState extends Persisted {
  mobileNavOpen: boolean;
  commandOpen: boolean;
  setTheme: (t: ThemePref) => void;
  setDensity: (d: Density) => void;
  toggleSidebar: () => void;
  setMobileNavOpen: (open: boolean) => void;
  setCommandOpen: (open: boolean) => void;
}

export const useUI = create<UIState>((set, get) => ({
  ...load(),
  mobileNavOpen: false,
  commandOpen: false,
  setTheme: (theme) => {
    set({ theme });
    const { density, sidebarCollapsed } = get();
    save({ theme, density, sidebarCollapsed });
  },
  setDensity: (density) => {
    set({ density });
    const { theme, sidebarCollapsed } = get();
    save({ theme, density, sidebarCollapsed });
  },
  toggleSidebar: () => {
    const sidebarCollapsed = !get().sidebarCollapsed;
    set({ sidebarCollapsed });
    const { theme, density } = get();
    save({ theme, density, sidebarCollapsed });
  },
  setMobileNavOpen: (mobileNavOpen) => set({ mobileNavOpen }),
  setCommandOpen: (commandOpen) => set({ commandOpen })
}));

export function resolveDark(pref: ThemePref): boolean {
  if (pref === 'dark') return true;
  if (pref === 'light') return false;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}
