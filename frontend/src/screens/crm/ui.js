import React, { useEffect, useState } from 'react';
import { crmApi } from '../../services/crmApi';
import { View, Text, TouchableOpacity, StyleSheet, ActivityIndicator } from 'react-native';
import {
  UserPlus, Contact, Briefcase, Handshake, CheckSquare, Calendar, LifeBuoy, StickyNote, MessageSquare, Banknote, FileText, ShoppingCart,
  Package, Repeat, Book, Truck, Clipboard, Box, ScanLine, ChevronRight, AlertCircle, RefreshCw, CreditCard, FileSignature, HardDrive, Timer, ShieldCheck,
  CalendarClock, TrendingUp, Wrench, MapPin
} from 'lucide-react';
import { colors, spacing, radii, typography, shadows } from '../../theme';

// Small shared pieces for the CRM screens of the app.

export const objectIcons = {
  leads: UserPlus, contacts: Contact, accounts: Briefcase, opportunities: Handshake, tasks: CheckSquare, events: Calendar, cases: LifeBuoy,
  notes: StickyNote, communications: MessageSquare, income: Banknote, expenses: Banknote, quotes: FileText, invoices: FileText,
  sales_orders: ShoppingCart, purchase_orders: Truck, catalog_items: Package, subscriptions: Repeat, price_books: Book, solutions: Book,
  line_items: Clipboard, cards: ScanLine, payments: CreditCard, contracts: FileSignature, assets: HardDrive, sla_policies: Timer,
  entitlements: ShieldCheck, appointments: CalendarClock, forecasts: TrendingUp, vendors: Truck, services: Wrench, territories: MapPin, work_orders: Wrench, service_resources: Truck,
  refunds: Banknote, credit_notes: FileText, debit_notes: FileText, adjustments: Clipboard, resource_absences: Calendar, approvals: ShieldCheck
};

export function iconFor(object) {
  return objectIcons[object] || Box;
}

export function money(amount, currency = 'INR') {
  const n = Number(amount) || 0;
  try {
    return new Intl.NumberFormat('en-IN', { style: 'currency', currency, maximumFractionDigits: 0 }).format(n);
  } catch (e) {
    return `${currency} ${Math.round(n).toLocaleString('en-IN')}`;
  }
}

export function humanize(s) {
  return String(s || '').replace(/[_-]+/g, ' ').replace(/^\w/, (c) => c.toUpperCase());
}

const toneColors = () => ({
  success: [colors.successLight, colors.success],
  warning: [colors.warningLight, colors.warning],
  danger: [colors.dangerLight, colors.danger],
  primary: [colors.primaryLight, colors.primary],
  neutral: [colors.bgMutedDark, colors.textSecondary]
});

/** A status pill. `tone` comes from the object's status list when known. */
export function StatusPill({ label, tone = 'neutral' }) {
  const [bg, fg] = toneColors()[tone] || toneColors().neutral;
  if (!label) return null;
  return (
    <View style={[ui.pill, { backgroundColor: bg }]}>
      <Text style={[ui.pillText, { color: fg }]} numberOfLines={1}>{label}</Text>
    </View>
  );
}

export function guessTone(status) {
  const s = String(status || '').toLowerCase();
  if (/won|converted|completed|paid|received|closed|resolved|held|active|accepted/.test(s) && !/lost/.test(s)) return 'success';
  if (/lost|cancel|reject|overdue|failed|unqualified/.test(s)) return 'danger';
  if (/new|open|not_started|draft|planned/.test(s)) return 'primary';
  if (/progress|working|contacted|qualified|proposal|negotiat|waiting|sent|pending/.test(s)) return 'warning';
  return 'neutral';
}

export function Chip({ label, active, onPress }) {
  return (
    <TouchableOpacity onPress={onPress} activeOpacity={0.8} style={[ui.chip, active && ui.chipActive]}>
      <Text style={[ui.chipText, active && ui.chipTextActive]}>{label}</Text>
    </TouchableOpacity>
  );
}

export function SectionTitle({ children, action, onAction }) {
  return (
    <View style={ui.sectionRow}>
      <Text style={ui.sectionTitle}>{children}</Text>
      {action ? (
        <TouchableOpacity onPress={onAction} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
          <Text style={ui.sectionAction}>{action}</Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

/** One line of a list: title, subtitle, optional status, chevron. */
export function Row({ title, subtitle, status, statusTone, onPress, icon: Icon, right }) {
  return (
    <TouchableOpacity onPress={onPress} activeOpacity={onPress ? 0.75 : 1} style={ui.row} disabled={!onPress}>
      {Icon ? (
        <View style={ui.rowIcon}>
          <Icon size={16} color={colors.primary} />
        </View>
      ) : null}
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={ui.rowTitle} numberOfLines={1}>{title || 'Untitled'}</Text>
        {subtitle ? <Text style={ui.rowSub} numberOfLines={1}>{subtitle}</Text> : null}
      </View>
      {status ? <StatusPill label={humanize(status)} tone={statusTone || guessTone(status)} /> : null}
      {right}
      {onPress ? <ChevronRight size={16} color={colors.textMuted} /> : null}
    </TouchableOpacity>
  );
}

export function Panel({ children, style }) {
  return <View style={[ui.panel, shadows.sm, style]}>{children}</View>;
}

export function Loading({ text = 'Loading…' }) {
  return (
    <View style={ui.center}>
      <ActivityIndicator color={colors.primary} />
      <Text style={ui.muted}>{text}</Text>
    </View>
  );
}

/** An honest error state with a retry — never a blank screen or a silent failure. */
export function ErrorBox({ message, onRetry }) {
  return (
    <View style={ui.center}>
      <AlertCircle size={28} color={colors.danger} />
      <Text style={[ui.muted, { color: colors.textPrimary, textAlign: 'center' }]}>{message || 'Something went wrong.'}</Text>
      {onRetry ? (
        <TouchableOpacity onPress={onRetry} style={ui.retry}>
          <RefreshCw size={14} color={colors.primary} />
          <Text style={ui.retryText}>Try again</Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

export function Empty({ title, body, action, onAction }) {
  return (
    <View style={ui.center}>
      <Text style={ui.emptyTitle}>{title}</Text>
      {body ? <Text style={[ui.muted, { textAlign: 'center' }]}>{body}</Text> : null}
      {action ? (
        <TouchableOpacity onPress={onAction} style={ui.retry}>
          <Text style={ui.retryText}>{action}</Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

export const ui = StyleSheet.create({
  pill: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: radii.badge, maxWidth: 130 },
  pillText: { fontSize: 11, fontWeight: '700' },
  chip: { paddingHorizontal: 12, paddingVertical: 7, borderRadius: radii.chip, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.bgCard, marginRight: 8 },
  chipActive: { backgroundColor: colors.primary, borderColor: colors.primary },
  chipText: { fontSize: 12, fontWeight: '600', color: colors.textSecondary },
  chipTextActive: { color: '#FFFFFF' },
  sectionRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.xl, marginBottom: spacing.sm },
  sectionTitle: { fontSize: 14, fontWeight: '700', color: colors.textPrimary },
  sectionAction: { fontSize: 12, fontWeight: '700', color: colors.primary },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, paddingHorizontal: spacing.md, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  rowIcon: { width: 32, height: 32, borderRadius: 10, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center' },
  rowTitle: { fontSize: 14, fontWeight: '600', color: colors.textPrimary },
  rowSub: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  panel: { backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.border, overflow: 'hidden' },
  center: { alignItems: 'center', justifyContent: 'center', paddingVertical: 36, paddingHorizontal: spacing.xl, gap: 10 },
  muted: { ...typography.bodySmall, color: colors.textSecondary },
  emptyTitle: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  retry: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, paddingVertical: 8, borderRadius: radii.button, backgroundColor: colors.primaryLight, marginTop: 4 },
  retryText: { fontSize: 13, fontWeight: '700', color: colors.primary }
});

/** The business's saved arrangement for phones (D-130): { dashboard, nav }, each { order, hidden } or undefined. */
export function useMobileLayouts(code) {
  const [layouts, setLayouts] = useState({});
  useEffect(() => {
    let live = true;
    if (!code) return undefined;
    crmApi.uiLayout(code).then((r) => {
      if (live) setLayouts({ dashboard: r.layouts?.dashboard?.mobile, nav: r.layouts?.nav?.mobile });
    }).catch(() => {});
    return () => { live = false; };
  }, [code]);
  return layouts;
}
