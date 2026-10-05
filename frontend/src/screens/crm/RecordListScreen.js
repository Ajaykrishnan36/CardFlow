import React, { useCallback, useEffect, useRef, useState } from 'react';
import { View, Text, FlatList, TouchableOpacity, StyleSheet, TextInput, ScrollView, RefreshControl } from 'react-native';
import { ArrowLeft, Plus, Search, X } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { useCrm } from '../../context/CrmContext';
import { crmApi } from '../../services/crmApi';
import { Chip, Row, Loading, ErrorBox, Empty, iconFor, money, humanize } from './ui';

const PAGE = 30;

/** What to show under a record's name: the first list columns that have a value. */
function subtitleOf(row, meta, currency) {
  const parts = [];
  (meta?.listColumns || []).forEach((key) => {
    if (parts.length >= 2 || key === meta.statusField || key === 'ownerId' || key === 'createdAt') return;
    const f = meta.fields.find((x) => x.key === key);
    const v = row.values?.[key];
    if (!f || v === null || v === undefined || v === '') return;
    if (f.type === 'lookup') parts.push(row.lookups?.[key]?.label || '');
    else if (f.type === 'currency') parts.push(money(v, currency));
    else if (f.type === 'date' || f.type === 'datetime') parts.push(new Date(v).toLocaleDateString('en-IN', { day: 'numeric', month: 'short' }));
    else if (f.type === 'select') parts.push(f.options?.find((o) => o.value === v)?.label || humanize(v));
    else if (typeof v === 'string' || typeof v === 'number') parts.push(String(v));
  });
  return parts.filter(Boolean).join(' · ') || row.code;
}

/** One list for every object: search, status filter, paging, pull to refresh, create. */
export function RecordListScreen({ object, initialQuery = '', preset = null, onBack, onOpenRecord, onCreate }) {
  const { activeCode, can, currency } = useCrm();
  const [meta, setMeta] = useState(null);
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [q, setQ] = useState(initialQuery);
  const [status, setStatus] = useState('');
  const [state, setState] = useState('loading'); // loading | ready | error
  const [error, setError] = useState('');
  const [more, setMore] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    crmApi.meta(activeCode, object).then(setMeta).catch((e) => {
      setError(e.message);
      setState('error');
    });
  }, [activeCode, object]);

  const load = useCallback(
    async (offset = 0) => {
      const mine = ++seq.current;
      if (offset === 0) setState((s) => (s === 'ready' ? s : 'loading'));
      try {
        const res = await crmApi.list(activeCode, object, {
          q: q.trim() || undefined, status: status || undefined, limit: PAGE, offset,
          filter: preset ? JSON.stringify({ op: 'and', filters: [preset.filter] }) : undefined
        });
        if (mine !== seq.current) return;
        setRows((prev) => (offset === 0 ? res.data || [] : [...prev, ...(res.data || [])]));
        setTotal(res.total || 0);
        setError('');
        setState('ready');
      } catch (e) {
        if (mine !== seq.current) return;
        setError(e.message || 'Could not load the list.');
        setState('error');
      }
    },
    [activeCode, object, q, status, preset]
  );

  useEffect(() => {
    const t = setTimeout(() => load(0), q ? 250 : 0);
    return () => clearTimeout(t);
  }, [load, q]);

  const loadMore = async () => {
    if (more || rows.length >= total) return;
    setMore(true);
    await load(rows.length);
    setMore(false);
  };

  const Icon = iconFor(object);
  const statusField = meta?.statusField;
  const toneOf = (value) => meta?.statuses?.find((s) => s.value === value)?.tone;
  const labelOf = (value) => meta?.statuses?.find((s) => s.value === value)?.label || humanize(value);

  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack} style={styles.back} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
          <ArrowLeft size={20} color={colors.textPrimary} />
        </TouchableOpacity>
        <View style={{ flex: 1 }}>
          <Text style={styles.title}>{preset?.label || meta?.labelPlural || humanize(object)}</Text>
          {state === 'ready' ? <Text style={styles.count}>{total} {total === 1 ? 'record' : 'records'}</Text> : null}
        </View>
        {can(object, 'create') ? (
          <TouchableOpacity style={[styles.add, shadows.sm]} onPress={() => onCreate(object)} accessibilityLabel="New record">
            <Plus size={18} color="#FFFFFF" />
          </TouchableOpacity>
        ) : null}
      </View>

      <View style={styles.search}>
        <Search size={16} color={colors.textMuted} />
        <TextInput style={styles.searchInput} placeholder={`Search ${(meta?.labelPlural || object).toLowerCase()}`} placeholderTextColor={colors.textMuted} value={q} onChangeText={setQ} autoCapitalize="none" />
        {q ? (
          <TouchableOpacity onPress={() => setQ('')}>
            <X size={16} color={colors.textMuted} />
          </TouchableOpacity>
        ) : null}
      </View>

      {meta?.statuses?.length ? (
        <View>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.filters}>
            <Chip label="All" active={!status} onPress={() => setStatus('')} />
            {meta.statuses.map((s) => (
              <Chip key={s.value} label={s.label} active={status === s.value} onPress={() => setStatus(status === s.value ? '' : s.value)} />
            ))}
          </ScrollView>
        </View>
      ) : null}

      {state === 'error' ? (
        <ErrorBox message={error} onRetry={() => load(0)} />
      ) : state === 'loading' || !meta ? (
        <Loading />
      ) : rows.length === 0 ? (
        <Empty
          title={q || status ? 'Nothing matches' : `No ${meta.labelPlural.toLowerCase()} yet`}
          body={q || status ? 'Try another search or clear the filter.' : `Add your first ${meta.labelSingular.toLowerCase()} to get started.`}
          action={!q && !status && can(object, 'create') ? `New ${meta.labelSingular.toLowerCase()}` : undefined}
          onAction={() => onCreate(object)}
        />
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(r) => r.id}
          style={styles.list}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              tintColor={colors.primary}
              onRefresh={async () => {
                setRefreshing(true);
                await load(0);
                setRefreshing(false);
              }}
            />
          }
          onEndReached={loadMore}
          onEndReachedThreshold={0.4}
          renderItem={({ item }) => (
            <Row
              icon={Icon}
              title={item.title}
              subtitle={subtitleOf(item, meta, currency)}
              status={statusField && item.values?.[statusField] ? labelOf(item.values[statusField]) : undefined}
              statusTone={statusField ? toneOf(item.values?.[statusField]) : undefined}
              onPress={() => onOpenRecord(object, item.id)}
            />
          )}
          ListFooterComponent={
            rows.length < total ? (
              <TouchableOpacity style={styles.more} onPress={loadMore}>
                <Text style={styles.moreText}>{more ? 'Loading…' : `Show more (${total - rows.length} left)`}</Text>
              </TouchableOpacity>
            ) : null
          }
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgCard },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: spacing.lg, paddingTop: spacing.md, paddingBottom: spacing.sm },
  back: { width: 32, height: 32, justifyContent: 'center' },
  title: { fontSize: 20, fontWeight: '800', color: colors.textPrimary },
  count: { fontSize: 12, color: colors.textSecondary, marginTop: 1 },
  add: { width: 40, height: 40, borderRadius: 20, backgroundColor: colors.primary, alignItems: 'center', justifyContent: 'center' },
  search: { flexDirection: 'row', alignItems: 'center', gap: 8, backgroundColor: colors.bgMuted, borderRadius: radii.input, borderWidth: 1, borderColor: colors.border, paddingHorizontal: 12, marginHorizontal: spacing.lg },
  searchInput: { flex: 1, paddingVertical: 10, fontSize: 14, color: colors.textPrimary, outlineStyle: 'none' },
  filters: { paddingHorizontal: spacing.lg, paddingVertical: spacing.md },
  list: { flex: 1 },
  more: { padding: spacing.lg, alignItems: 'center' },
  moreText: { fontSize: 13, fontWeight: '700', color: colors.primary }
});
