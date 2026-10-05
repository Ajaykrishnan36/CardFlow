import React, { useEffect, useRef, useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, TextInput } from 'react-native';
import { Search, ChevronDown, X, Store, Settings, Building2 } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { useCrm } from '../../context/CrmContext';
import { crmApi } from '../../services/crmApi';
import { iconFor, Panel, Row, Loading, SectionTitle } from './ui';

const SEARCHABLE = [
  { object: 'leads', label: 'Leads' },
  { object: 'contacts', label: 'Contacts' },
  { object: 'accounts', label: 'Accounts' },
  { object: 'opportunities', label: 'Deals' },
  { object: 'tasks', label: 'Tasks' },
  { object: 'cases', label: 'Cases' }
];
const GROUP_ORDER = ['CRM', 'Service', 'Sales', 'Finance'];

/**
 * My CRM: everything this person can work with in the open business, grouped like the
 * web sidebar, with one search across leads, contacts, accounts, deals, tasks and cases.
 */
export function CrmModulesScreen({ onOpenList, onOpenRecord, onOpenSwitcher, onOpenCards, onOpenListing, onOpenTeam, onOpenBusinessProfile }) {
  const { active, activeCode, navigation, context, has } = useCrm();
  const [q, setQ] = useState('');
  const [results, setResults] = useState(null);
  const [searching, setSearching] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) {
      setResults(null);
      return undefined;
    }
    const mine = ++seq.current;
    setSearching(true);
    const timer = setTimeout(async () => {
      const groups = await Promise.all(
        SEARCHABLE.filter((s) => has(s.object)).map((s) =>
          crmApi
            .list(activeCode, s.object, { q: term, limit: 5 })
            .then((r) => ({ ...s, rows: r.data || [], total: r.total || 0 }))
            .catch(() => ({ ...s, rows: [], total: 0 }))
        )
      );
      if (mine === seq.current) {
        setResults(groups.filter((g) => g.rows.length));
        setSearching(false);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [q, activeCode, has]);

  const modules = navigation.filter((n) => GROUP_ORDER.includes(n.group));
  const groups = GROUP_ORDER.map((g) => ({ name: g, items: modules.filter((n) => n.group === g) })).filter((g) => g.items.length);

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <TouchableOpacity style={styles.header} onPress={onOpenSwitcher}>
        <View style={{ flex: 1, minWidth: 0 }}>
          <Text style={styles.title}>My CRM</Text>
          <Text style={styles.subtitle} numberOfLines={1}>{active?.name || ''}</Text>
        </View>
        <ChevronDown size={18} color={colors.textSecondary} />
      </TouchableOpacity>

      <View style={styles.search}>
        <Search size={16} color={colors.textMuted} />
        <TextInput
          style={styles.searchInput}
          placeholder="Search leads, contacts, accounts, deals…"
          placeholderTextColor={colors.textMuted}
          value={q}
          onChangeText={setQ}
          autoCapitalize="none"
        />
        {q ? (
          <TouchableOpacity onPress={() => setQ('')}>
            <X size={16} color={colors.textMuted} />
          </TouchableOpacity>
        ) : null}
      </View>

      {q.trim().length >= 2 ? (
        searching && !results ? (
          <Loading text="Searching…" />
        ) : results && results.length === 0 ? (
          <Text style={styles.empty}>Nothing in {active?.name || 'this business'} matches “{q.trim()}”.</Text>
        ) : (
          (results || []).map((g) => (
            <View key={g.object}>
              <SectionTitle action={g.total > g.rows.length ? `All ${g.total}` : undefined} onAction={() => onOpenList(g.object, { q: q.trim() })}>{g.label}</SectionTitle>
              <Panel>
                {g.rows.map((r) => (
                  <Row key={r.id} icon={iconFor(g.object)} title={r.title} subtitle={r.code} status={r.values?.status || r.values?.lifecycle} onPress={() => onOpenRecord(g.object, r.id)} />
                ))}
              </Panel>
            </View>
          ))
        )
      ) : !context ? (
        <Loading text="Loading your CRM…" />
      ) : (
        <>
          {groups.map((g) => (
            <View key={g.name}>
              <SectionTitle>{g.name === 'CRM' ? 'Sales & relationships' : g.name}</SectionTitle>
              <View style={styles.grid}>
                {g.items.map((n) => {
                  const object = n.path.split('/').pop();
                  const Icon = iconFor(object);
                  return (
                    <TouchableOpacity
                      key={n.key}
                      style={[styles.module, shadows.sm]}
                      activeOpacity={0.8}
                      onPress={() => (object === 'cards' ? onOpenCards() : onOpenList(object))}
                    >
                      <View style={styles.moduleIcon}>
                        <Icon size={18} color={colors.primary} />
                      </View>
                      <Text style={styles.moduleLabel} numberOfLines={2}>{n.label}</Text>
                    </TouchableOpacity>
                  );
                })}
              </View>
            </View>
          ))}
          {groups.length === 0 ? <Text style={styles.empty}>Your role in this business has no CRM modules yet. Ask its administrator for access.</Text> : null}
          <SectionTitle>More</SectionTitle>
          <Panel>
            <Row icon={Store} title="My business card & listing" subtitle="Digital card, original card, QR and your directory listing" onPress={onOpenListing} />
            <Row icon={Building2} title="Business profile" subtitle="Name, contact and tax details" onPress={onOpenBusinessProfile} />
            <Row icon={Settings} title="Team, roles & access" subtitle="Add teammates and decide what each role can do" onPress={onOpenTeam} />
          </Panel>
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgMuted },
  content: { padding: spacing.lg, paddingBottom: 40 },
  header: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  title: { fontSize: 22, fontWeight: '800', color: colors.textPrimary },
  subtitle: { fontSize: 13, color: colors.textSecondary, marginTop: 2 },
  search: { flexDirection: 'row', alignItems: 'center', gap: 8, backgroundColor: colors.bgCard, borderRadius: radii.input, borderWidth: 1, borderColor: colors.border, paddingHorizontal: 12, marginTop: spacing.md },
  searchInput: { flex: 1, paddingVertical: 11, fontSize: 14, color: colors.textPrimary, outlineStyle: 'none' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10 },
  module: { width: '47%', flexGrow: 1, backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.border, padding: spacing.md, minHeight: 84, flexDirection: 'row', alignItems: 'center', gap: 10 },
  moduleIcon: { width: 36, height: 36, borderRadius: 12, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center', },
  moduleLabel: { flex: 1, fontSize: 13, fontWeight: '700', color: colors.textPrimary },
  empty: { fontSize: 13, color: colors.textSecondary, textAlign: 'center', paddingVertical: spacing.xxl }
});
