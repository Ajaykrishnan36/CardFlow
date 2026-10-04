import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, RefreshControl, TextInput } from 'react-native';
import { User, ChevronDown, Plus, ScanLine, TrendingUp, TrendingDown, Wallet } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { useAuth } from '../../context/AuthContext';
import { useCrm } from '../../context/CrmContext';
import { crmApi } from '../../services/crmApi';
import { Chip, SectionTitle, Row, Panel, Loading, ErrorBox, money, iconFor } from './ui';

const RANGES = [
  { key: 'today', label: 'Today' },
  { key: 'week', label: 'This week' },
  { key: 'month', label: 'This month' },
  { key: 'year', label: 'This year' },
  { key: 'all', label: 'All time' },
  { key: 'custom', label: 'Custom' }
];

const QUICK = [
  { object: 'leads', label: 'Lead' },
  { object: 'contacts', label: 'Contact' },
  { object: 'accounts', label: 'Account' },
  { object: 'opportunities', label: 'Deal' },
  { object: 'tasks', label: 'Task' },
  { object: 'events', label: 'Meeting' },
  { object: 'income', label: 'Income' },
  { object: 'expenses', label: 'Expense' }
];

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? 'Good morning' : h < 17 ? 'Good afternoon' : 'Good evening';
}

/**
 * Home: the numbers of the open business for a date range (real data from
 * /dashboard/summary — nothing here is sample data), quick create, and what needs
 * attention today. The header switches business.
 */
export function CrmHomeScreen({ onOpenProfile, onOpenSwitcher, onOpenList, onCreate, onOpenRecord, onScan }) {
  const { user } = useAuth();
  const { active, activeCode, currency } = useCrm();
  const [range, setRange] = useState('month');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const customReady = range !== 'custom' || (/^\d{4}-\d{2}-\d{2}$/.test(from) && /^\d{4}-\d{2}-\d{2}$/.test(to));

  const load = useCallback(async () => {
    if (!activeCode || !customReady) return;
    try {
      const res = await crmApi.summary(activeCode, { range, from: range === 'custom' ? from : undefined, to: range === 'custom' ? to : undefined });
      setData(res);
      setError('');
    } catch (e) {
      setError(e.message || 'Could not load the dashboard.');
    }
  }, [activeCode, range, from, to, customReady]);

  useEffect(() => {
    setData(null);
    load();
  }, [load]);

  const refresh = async () => {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  };

  const m = (k) => (data?.metrics?.[k] ?? 0);
  const can = (k) => Boolean(data?.can?.[k]);
  const cur = data?.finance?.currency || data?.currency || currency;
  const firstName = (user?.name || '').split(' ')[0];

  const tiles = data
    ? [
        can('leads.read') && { label: 'Active leads', value: m('activeLeads'), hint: `${m('newLeads')} new`, object: 'leads' },
        can('leads.read') && { label: 'Follow-ups today', value: m('followUpsToday'), hint: 'leads to call', object: 'leads' },
        can('contacts.read') && { label: 'Contacts', value: m('activeContacts'), hint: `${m('newContacts')} new`, object: 'contacts' },
        can('accounts.read') && { label: 'Accounts', value: m('activeAccounts'), hint: `${m('newAccounts')} new`, object: 'accounts' },
        can('opportunities.read') && { label: 'Open deals', value: m('openOpportunities'), hint: money(m('pipelineValue'), cur), object: 'opportunities' },
        can('opportunities.read') && { label: 'Won', value: money(m('wonValue'), cur), hint: `${m('wonDeals')} deals`, object: 'opportunities' },
        can('tasks.read') && { label: 'Tasks today', value: m('tasksDueToday'), hint: `${m('overdueTasks')} overdue`, object: 'tasks', bad: m('overdueTasks') > 0 },
        can('events.read') && { label: 'Meetings ahead', value: m('upcomingMeetings'), hint: 'next 7 days', object: 'events' },
        can('cases.read') && { label: 'Open cases', value: m('openCases'), hint: `${m('newCases')} new`, object: 'cases' }
      ].filter(Boolean)
    : [];

  return (
    <ScrollView
      style={styles.screen}
      contentContainerStyle={styles.content}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} tintColor={colors.primary} />}
    >
      <View style={styles.header}>
        <TouchableOpacity style={styles.bizBtn} onPress={onOpenSwitcher} accessibilityLabel="Switch business">
          <View style={styles.bizAvatar}>
            <Text style={styles.bizAvatarText}>{(active?.name || '?').slice(0, 2).toUpperCase()}</Text>
          </View>
          <View style={{ flex: 1, minWidth: 0 }}>
            <Text style={styles.bizName} numberOfLines={1}>{active?.name || 'Your business'}</Text>
            <Text style={styles.bizRole} numberOfLines={1}>{active?.roleName || 'Member'}</Text>
          </View>
          <ChevronDown size={16} color={colors.textSecondary} />
        </TouchableOpacity>
        <TouchableOpacity style={styles.avatarBtn} onPress={onOpenProfile} accessibilityLabel="Open profile">
          <User size={18} color={colors.primary} />
        </TouchableOpacity>
      </View>

      <Text style={styles.greeting}>{greeting()}{firstName && firstName !== 'New' && firstName !== 'CardFlow' ? `, ${firstName}` : ''}</Text>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.ranges}>
        {RANGES.map((r) => (
          <Chip key={r.key} label={r.label} active={range === r.key} onPress={() => setRange(r.key)} />
        ))}
      </ScrollView>
      {range === 'custom' ? (
        <View style={styles.customRow}>
          <TextInput style={styles.dateInput} placeholder="From YYYY-MM-DD" placeholderTextColor={colors.textMuted} value={from} onChangeText={setFrom} type="date" />
          <TextInput style={styles.dateInput} placeholder="To YYYY-MM-DD" placeholderTextColor={colors.textMuted} value={to} onChangeText={setTo} type="date" />
        </View>
      ) : null}

      {error ? (
        <ErrorBox message={error} onRetry={load} />
      ) : !customReady ? (
        <Text style={styles.hint}>Choose a start and an end date.</Text>
      ) : !data ? (
        <Loading text="Loading your numbers…" />
      ) : (
        <>
          {data.finance ? (
            <Panel style={styles.financeCard}>
              <View style={styles.financeTop}>
                <View style={styles.financeIcon}>
                  <Wallet size={18} color={colors.primary} />
                </View>
                <View style={{ flex: 1 }}>
                  <Text style={styles.financeLabel}>Net income</Text>
                  <Text style={[styles.financeNet, { color: data.finance.net < 0 ? colors.danger : colors.textPrimary }]}>{money(data.finance.net, cur)}</Text>
                </View>
              </View>
              <View style={styles.financeRow}>
                <TouchableOpacity style={styles.financeCell} onPress={() => onOpenList('income')}>
                  <TrendingUp size={14} color={colors.success} />
                  <View>
                    <Text style={styles.financeSmall}>Income</Text>
                    <Text style={styles.financeValue}>{money(data.finance.income, cur)}</Text>
                  </View>
                </TouchableOpacity>
                <TouchableOpacity style={styles.financeCell} onPress={() => onOpenList('expenses')}>
                  <TrendingDown size={14} color={colors.danger} />
                  <View>
                    <Text style={styles.financeSmall}>Expenses</Text>
                    <Text style={styles.financeValue}>{money(data.finance.expenses, cur)}</Text>
                  </View>
                </TouchableOpacity>
              </View>
            </Panel>
          ) : null}

          <View style={styles.grid}>
            {tiles.map((t) => (
              <TouchableOpacity key={t.label} style={[styles.tile, shadows.sm]} onPress={() => onOpenList(t.object)} activeOpacity={0.8}>
                <Text style={styles.tileLabel} numberOfLines={1}>{t.label}</Text>
                <Text style={styles.tileValue} numberOfLines={1}>{t.value}</Text>
                <Text style={[styles.tileHint, t.bad && { color: colors.danger }]} numberOfLines={1}>{t.hint}</Text>
              </TouchableOpacity>
            ))}
          </View>

          <SectionTitle>Quick add</SectionTitle>
          <ScrollView horizontal showsHorizontalScrollIndicator={false}>
            <TouchableOpacity style={[styles.quick, styles.quickPrimary]} onPress={onScan}>
              <ScanLine size={16} color="#FFFFFF" />
              <Text style={[styles.quickText, { color: '#FFFFFF' }]}>Scan card</Text>
            </TouchableOpacity>
            {QUICK.filter((q) => can(`${q.object}.create`)).map((q) => (
              <TouchableOpacity key={q.object} style={styles.quick} onPress={() => onCreate(q.object)}>
                <Plus size={14} color={colors.primary} />
                <Text style={styles.quickText}>{q.label}</Text>
              </TouchableOpacity>
            ))}
          </ScrollView>

          {(data.lists || []).filter((l) => l.rows?.length).map((l) => {
            const Icon = iconFor(l.object);
            return (
              <View key={l.key || l.label}>
                <SectionTitle action="View all" onAction={() => onOpenList(l.object)}>{l.label}</SectionTitle>
                <Panel>
                  {l.rows.map((r) => (
                    <Row key={r.id} icon={Icon} title={r.title} subtitle={r.subtitle} status={r.status} onPress={() => onOpenRecord(l.object, r.id)} />
                  ))}
                </Panel>
              </View>
            );
          })}
          {tiles.length === 0 && !data.finance ? (
            <Text style={styles.hint}>Your role in this business doesn’t include the dashboard numbers. Open My CRM to see what you can work with.</Text>
          ) : null}
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgMuted },
  content: { padding: spacing.lg, paddingBottom: 40 },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  bizBtn: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 10, backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.border, padding: 10 },
  bizAvatar: { width: 36, height: 36, borderRadius: 12, backgroundColor: colors.primary, alignItems: 'center', justifyContent: 'center' },
  bizAvatarText: { color: '#FFFFFF', fontWeight: '800', fontSize: 13 },
  bizName: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  bizRole: { fontSize: 11, color: colors.textSecondary, marginTop: 1 },
  avatarBtn: { width: 44, height: 44, borderRadius: 22, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center' },
  greeting: { fontSize: 20, fontWeight: '700', color: colors.textPrimary, marginTop: spacing.lg },
  ranges: { marginTop: spacing.md, marginBottom: spacing.md },
  customRow: { flexDirection: 'row', gap: 8, marginBottom: spacing.md },
  dateInput: { flex: 1, borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 9, backgroundColor: colors.bgCard, color: colors.textPrimary, fontSize: 13 },
  hint: { fontSize: 13, color: colors.textSecondary, paddingVertical: spacing.lg, textAlign: 'center' },
  financeCard: { padding: spacing.lg, marginBottom: spacing.md },
  financeTop: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  financeIcon: { width: 40, height: 40, borderRadius: 12, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center' },
  financeLabel: { fontSize: 12, color: colors.textSecondary, fontWeight: '600' },
  financeNet: { fontSize: 26, fontWeight: '800', marginTop: 2 },
  financeRow: { flexDirection: 'row', gap: 12, marginTop: spacing.md, paddingTop: spacing.md, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.border },
  financeCell: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 8 },
  financeSmall: { fontSize: 11, color: colors.textSecondary },
  financeValue: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10 },
  tile: { width: '48%', flexGrow: 1, backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.border, padding: spacing.md },
  tileLabel: { fontSize: 12, color: colors.textSecondary, fontWeight: '600' },
  tileValue: { fontSize: 22, fontWeight: '800', color: colors.textPrimary, marginTop: 4 },
  tileHint: { fontSize: 11, color: colors.textMuted, marginTop: 4 },
  quick: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, paddingVertical: 10, borderRadius: radii.button, backgroundColor: colors.bgCard, borderWidth: 1, borderColor: colors.border, marginRight: 8 },
  quickPrimary: { backgroundColor: colors.primary, borderColor: colors.primary },
  quickText: { fontSize: 13, fontWeight: '700', color: colors.primary }
});
