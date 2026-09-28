import {
  Boxes,
  Briefcase,
  Building2,
  Circle,
  Contact,
  IdCard,
  Landmark,
  CreditCard,
  House,
  LayoutDashboard,
  LifeBuoy,
  Plug,
  ScrollText,
  Settings,
  Smartphone,
  Store,
  ShieldCheck,
  SquareCheck,
  Target,
  UserPlus,
  Users,
  UsersRound,
  type LucideIcon
} from 'lucide-react';

// Server-sent icon keys (access.NavItem.Icon) → components.
const icons: Record<string, LucideIcon> = {
  'layout-dashboard': LayoutDashboard,
  boxes: Boxes,
  'building-2': Building2,
  'users-round': UsersRound,
  'shield-check': ShieldCheck,
  'credit-card': CreditCard,
  'life-buoy': LifeBuoy,
  plug: Plug,
  'scroll-text': ScrollText,
  settings: Settings,
  house: House,
  'user-plus': UserPlus,
  contact: Contact,
  target: Target,
  'check-square': SquareCheck,
  briefcase: Briefcase,
  users: Users,
  landmark: Landmark,
  'id-card': IdCard,
  smartphone: Smartphone,
  store: Store
};

export function navIcon(key: string): LucideIcon {
  return icons[key] ?? Circle;
}
