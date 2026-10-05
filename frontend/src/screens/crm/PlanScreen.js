import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet } from 'react-native';
import { ArrowLeft, Check, Crown } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { Button } from '../../components/Button';
import { useCrm } from '../../context/CrmContext';
import { crmApi } from '../../services/crmApi';
import { Loading, ErrorBox, Panel, SectionTitle, money } from './ui';

const SOURCE = {
  default: 'Every new business starts on this plan.',
  manual: 'Set for this business by the platform team.',
  app_premium: 'Included with your Pro subscription.',
  revenuecat: 'Paid through the app store.',
  grandfathered: 'Kept from your earlier subscription.',
  system: 'Managed by the platform team.'
};

function Meter({ label, used, limit }) {
  const unlimited = !limit;
  const pct = unlimited ? 0 : Math.min(100, Math.round((used / limit) * 100));
  const tight = !unlimited && pct >= 90;
  return (
    <View style={styles.meter}>
      <View style={styles.meterRow}>
        <Text style={styles.meterLabel}>{label}</Text>
        <Text style={[styles.meterValue, tight && { color: colors.danger }]}>{used.toLocaleString('en-IN')}{unlimited ? ' · no limit' : ` of ${limit.toLocaleString('en-IN')}`}</Text>
      </View>
      {!unlimited ? (
        <View style={styles.track}>
          <View style={[styles.fill, { width: `${pct}%`, backgroundColor: tight ? colors.danger : colors.primary }]} />
        </View>
      ) : null}
    </View>
  );
}

/** The plan of the open business: what it includes, what is used, and the plans on offer. */
export function PlanScreen({ onBack, onUpgrade }) {
  const { activeCode, active } = useCrm();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    if (!activeCode) return;
    crmApi.plan(activeCode).then((d) => {
      setData(d);
      setError('');
    }).catch((e) => setError(e.message || 'Could not load the plan.'));
  }, [activeCode]);
  useEffect(() => {
    load();
  }, [load]);

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <TouchableOpacity onPress={onBack} style={styles.back} accessibilityLabel="Back">
        <ArrowLeft size={20} color={colors.textPrimary} />
      </TouchableOpacity>
      <Text style={styles.title}>Plan</Text>
      <Text style={styles.sub}>{active ? `For ${active.name}. A plan belongs to a business, so your whole team shares it.` : 'Create a business to see its plan.'}</Text>

      {error ? <ErrorBox message={error} onRetry={load} /> : !activeCode ? null : !data ? <Loading /> : (
        <>
          <Panel style={[styles.current, shadows.sm]}>
            <View style={styles.currentTop}>
              <View style={styles.crown}>
                <Crown size={18} color={colors.primary} />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.planName}>{data.plan.name}</Text>
                <Text style={styles.planNote}>
                  {data.status === 'expired' ? 'Your paid plan ended; the business is back on this plan.' : SOURCE[data.source] || ''}
                  {data.currentPeriodEnd ? ` Until ${new Date(data.currentPeriodEnd).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })}.` : ''}
                </Text>
              </View>
            </View>
            <Meter label="Team members" used={data.usage.members} limit={data.plan.limits.members} />
            <Meter label="Records" used={data.usage.records} limit={data.plan.limits.records} />
            <Meter label="Card scans this month" used={data.usage.cardScansThisMonth} limit={data.plan.limits.cardScansPerMonth} />
          </Panel>

          <SectionTitle>Plans</SectionTitle>
          {data.plans.map((p) => {
            const current = p.key === data.plan.key;
            return (
              <Panel key={p.key} style={[styles.plan, current && styles.planCurrent]}>
                <View style={styles.planHead}>
                  <Text style={styles.planTitle}>{p.name}</Text>
                  <Text style={styles.price}>{p.priceMonthly > 0 ? `${money(p.priceMonthly, p.currency)} / month` : 'Free'}</Text>
                </View>
                {p.description ? <Text style={styles.planDesc}>{p.description}</Text> : null}
                {(p.features || []).map((f) => (
                  <View key={f} style={styles.feature}>
                    <Check size={14} color={colors.success} />
                    <Text style={styles.featureText}>{f}</Text>
                  </View>
                ))}
                {current ? <Text style={styles.currentTag}>Your current plan</Text> : null}
                {!current && p.key === 'pro' && onUpgrade ? <Button title="Upgrade to Pro" size="sm" onPress={onUpgrade} style={{ marginTop: spacing.md }} /> : null}
                {!current && p.key !== 'pro' && p.priceMonthly > 0 ? <Text style={styles.contact}>Ask support to move this business to {p.name}.</Text> : null}
              </Panel>
            );
          })}
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgMuted },
  content: { padding: spacing.lg, paddingBottom: 48 },
  back: { width: 32, height: 32, justifyContent: 'center', marginBottom: 4 },
  title: { fontSize: 24, fontWeight: '800', color: colors.textPrimary },
  sub: { fontSize: 13, color: colors.textSecondary, marginTop: 4, marginBottom: spacing.lg },
  current: { padding: spacing.lg },
  currentTop: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: spacing.md },
  crown: { width: 40, height: 40, borderRadius: 12, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center' },
  planName: { fontSize: 20, fontWeight: '800', color: colors.textPrimary },
  planNote: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  meter: { marginTop: spacing.md },
  meterRow: { flexDirection: 'row', justifyContent: 'space-between', marginBottom: 6 },
  meterLabel: { fontSize: 13, fontWeight: '600', color: colors.textPrimary },
  meterValue: { fontSize: 12, color: colors.textSecondary },
  track: { height: 6, borderRadius: 3, backgroundColor: colors.bgMutedDark, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 },
  plan: { padding: spacing.lg, marginBottom: spacing.md },
  planCurrent: { borderColor: colors.primary },
  planHead: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline' },
  planTitle: { fontSize: 16, fontWeight: '800', color: colors.textPrimary },
  price: { fontSize: 13, fontWeight: '700', color: colors.primary },
  planDesc: { fontSize: 13, color: colors.textSecondary, marginTop: 4, marginBottom: 6 },
  feature: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 6 },
  featureText: { flex: 1, fontSize: 13, color: colors.textPrimary },
  currentTag: { fontSize: 12, fontWeight: '700', color: colors.primary, marginTop: spacing.md },
  contact: { fontSize: 12, color: colors.textMuted, marginTop: spacing.md }
});
