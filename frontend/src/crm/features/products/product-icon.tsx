import {
  Boxes,
  Briefcase,
  Building2,
  GraduationCap,
  HeartPulse,
  Landmark,
  Plane,
  Rocket,
  ShoppingBag,
  Store,
  UsersRound,
  Wrench,
  type LucideIcon
} from 'lucide-react';
import { cn } from '@crm/lib/utils';

/** Icon keys stored in `product.icon` → lucide component. Unknown keys fall back to Boxes. */
export const PRODUCT_ICONS: Record<string, LucideIcon> = {
  boxes: Boxes,
  briefcase: Briefcase,
  'graduation-cap': GraduationCap,
  'heart-pulse': HeartPulse,
  store: Store,
  'building-2': Building2,
  rocket: Rocket,
  'users-round': UsersRound,
  'shopping-bag': ShoppingBag,
  landmark: Landmark,
  plane: Plane,
  wrench: Wrench
};

export const PRODUCT_ICON_KEYS = Object.keys(PRODUCT_ICONS);

/** Product accent swatches (the only hard-coded colours in the product screens). */
export const ACCENT_COLORS = ['#4F46E5', '#2563EB', '#0891B2', '#059669', '#65A30D', '#D97706', '#DC2626', '#DB2777'];

const sizes = {
  sm: 'size-8 rounded-md [&_svg]:size-4',
  md: 'size-10 rounded-lg [&_svg]:size-5',
  lg: 'size-12 rounded-xl [&_svg]:size-6'
};

/** Tinted icon tile. Without an accent it uses the theme's primary tint. */
export function ProductIcon({ icon, accent, size = 'md', className }: { icon?: string; accent?: string; size?: keyof typeof sizes; className?: string }) {
  const Icon = PRODUCT_ICONS[icon ?? ''] ?? Boxes;
  const valid = accent && /^#[0-9a-fA-F]{6}$/.test(accent) ? accent : undefined;
  return (
    <span
      className={cn('grid shrink-0 place-items-center', sizes[size], !valid && 'bg-primary-soft text-primary', className)}
      style={valid ? { backgroundColor: `${valid}1f`, color: valid } : undefined}
      aria-hidden
    >
      <Icon />
    </span>
  );
}
