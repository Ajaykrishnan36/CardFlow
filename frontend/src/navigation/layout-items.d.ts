export interface UILayout {
  order: string[];
  hidden: string[];
}
export interface LayoutItem {
  key: string;
  label: string;
}
export function arrange<T>(items: T[], keyOf: (item: T) => string, layout?: UILayout | null): T[];
export function isHidden(layout: UILayout | null | undefined, key: string): boolean;
export const LOCKED_NAV: string[];
export const DASHBOARD_SECTIONS: { desktop: LayoutItem[]; mobile: LayoutItem[] };
export const SUMMARY_TILES: { desktop: LayoutItem[]; mobile: LayoutItem[] };
export const OWNER_SECTIONS: LayoutItem[];
