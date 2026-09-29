import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, ActivityIndicator } from 'react-native';
import { Crown, Check, Receipt, ChevronRight, AlertTriangle, RefreshCw } from 'lucide-react';
import { colors, spacing, radii, typography } from '../theme';
import { Card } from './Card';
import { Button } from './Button';
import { TransactionHistoryScreen } from './TransactionHistoryScreen';
import { useAuth } from '../context/AuthContext';
import { billingPlatformName, isBillingConfigured } from '../services/subscription/subscriptionService';

const PERKS = [
  'Unlimited saved business cards',
  'Unlimited businesses on your account',
  'Premium card templates & themes',
  'Cloud backup of your phone contacts'
];

// ISO 8601 period (P1M, P1Y, P3M, P1W…) → labels.
const UNIT = { D: ['day', 'daily'], W: ['week', 'weekly'], M: ['month', 'monthly'], Y: ['year', 'yearly'] };
function describePeriod(iso) {
  const m = /^P(\d+)([DWMY])$/.exec(iso || '');
  if (!m) return null;
  const n = Number(m[1]);
  const [unit, adverb] = UNIT[m[2]];
  if (n === 1) return { per: unit, name: adverb.charAt(0).toUpperCase() + adverb.slice(1) };
  return { per: `${n} ${unit}s`, name: `${n} ${unit.charAt(0).toUpperCase() + unit.slice(1)}s` };
}

const PACKAGE_BADGES = { $rc_annual: 'Best Value', $rc_lifetime: 'One-time' };

function formatDate(v) {
  if (!v) return null;
  try {
    return new Date(v).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });
  } catch (e) {
    return null;
  }
}

const STORE_NOTE = {
  ios: 'Payment is charged to your Apple ID. The subscription renews automatically unless cancelled at least 24 hours before the end of the period. Manage or cancel it in your App Store settings.',
  android: 'Payment is charged to your Google Play account. The subscription renews automatically unless cancelled. Manage or cancel it in Google Play → Subscriptions.',
  web: 'Secure card payment. The subscription renews automatically unless cancelled. Manage or cancel it any time from "Manage subscription".'
};

function StatusCard({ subscription, onManage }) {
  if (!subscription || subscription.status === 'FREE') return null;
  const until = formatDate(subscription.expires_at);
  const map = {
    ACTIVE: {
      title: 'CardFlow Premium is active',
      sub: subscription.expires_at ? (subscription.will_renew ? `Renews on ${until}` : `Active until ${until}`) : 'Lifetime access',
      tone: 'gold'
    },
    CANCELLED: {
      title: 'Premium — cancelled',
      sub: `You keep Premium until ${until}. It won't renew.`,
      tone: 'gold'
    },
    BILLING_ISSUE: {
      title: 'There is a problem with your payment',
      sub: subscription.is_premium
        ? `Update your payment method to keep Premium (access until ${until}).`
        : 'Update your payment method to restore Premium.',
      tone: 'danger'
    },
    EXPIRED: {
      title: 'Your Premium has expired',
      sub: until ? `Expired on ${until}. Subscribe again to unlock Premium.` : 'Subscribe again to unlock Premium.',
      tone: 'muted'
    }
  };
  const m = map[subscription.status] || map.ACTIVE;
  return (
    <Card style={[styles.statusCard, m.tone === 'danger' && styles.statusDanger, m.tone === 'muted' && styles.statusMuted]}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        {m.tone === 'danger' ? <AlertTriangle size={16} color={colors.danger} /> : <Crown size={16} color={colors.gold} />}
        <Text style={styles.statusTitle}>{m.title}</Text>
      </View>
      <Text style={styles.statusSub}>{m.sub}</Text>
      {subscription.status === 'BILLING_ISSUE' ? (
        <Button title="Update payment method" variant="danger" size="sm" onPress={onManage} style={{ marginTop: spacing.sm, alignSelf: 'flex-start' }} />
      ) : null}
    </Card>
  );
}

export function SubscriptionScreen({ onBack }) {
  const { isPremiumActive, subscription, isPurchasing, loadOfferings, purchasePackage, restorePurchases, refreshSubscription, manageSubscription } = useAuth();
  const [packages, setPackages] = useState(null); // null = loading
  const [loadError, setLoadError] = useState('');
  const [selected, setSelected] = useState(null);
  const [showHistory, setShowHistory] = useState(false);
  const [notice, setNotice] = useState(null); // { tone: 'success' | 'error' | 'info', text }
  const configured = isBillingConfigured();

  const flash = (tone, text) => {
    setNotice({ tone, text });
    setTimeout(() => setNotice((n) => (n && n.text === text ? null : n)), 5000);
  };

  const fetchOfferings = useCallback(async () => {
    if (!configured) {
      setPackages([]);
      return;
    }
    setPackages(null);
    setLoadError('');
    // The SDK may still be logging in right after sign-in — retry briefly.
    for (let attempt = 0; attempt < 4; attempt += 1) {
      try {
        const list = await loadOfferings();
        setPackages(list);
        setSelected((cur) => cur || list.find((p) => p.packageType === '$rc_annual')?.id || list[0]?.id || null);
        return;
      } catch (e) {
        if (e?.code !== 'not_ready' || attempt === 3) {
          setLoadError(e?.message || 'Could not load plans.');
          setPackages([]);
          return;
        }
        await new Promise((r) => setTimeout(r, 1200));
      }
    }
  }, [configured, loadOfferings]);

  useEffect(() => {
    fetchOfferings();
    refreshSubscription().catch(() => {});
  }, [fetchOfferings, refreshSubscription]);

  if (showHistory) {
    return <TransactionHistoryScreen onBack={() => setShowHistory(false)} />;
  }

  const pkg = (packages || []).find((p) => p.id === selected) || null;
  const period = describePeriod(pkg?.period);

  const handlePurchase = async () => {
    if (!pkg || isPurchasing) return;
    try {
      const res = await purchasePackage(pkg);
      if (res.status === 'active') flash('success', 'Welcome to CardFlow Premium — everything is unlocked!');
      else flash('info', 'Purchase received. Premium will unlock as soon as the store confirms the payment.');
    } catch (e) {
      if (e?.code === 'cancelled' || e?.code === 'busy') return;
      flash('error', e?.message || 'The purchase could not be completed. You have not been charged.');
    }
  };

  const handleRestore = async () => {
    if (isPurchasing) return;
    try {
      const res = await restorePurchases();
      if (res.restored) flash('success', 'Purchases restored — Premium is active.');
      else flash('info', 'No active Premium subscription was found for this account.');
    } catch (e) {
      if (e?.code === 'busy') return;
      flash('error', e?.message || 'Could not restore purchases.');
    }
  };

  const handleManage = async () => {
    const opened = await manageSubscription();
    if (!opened) flash('info', 'Manage your subscription from the store where you bought it.');
  };

  const canBuy = !isPremiumActive || subscription?.status === 'EXPIRED';

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
      {onBack ? (
        <TouchableOpacity onPress={onBack} style={styles.backRow} accessibilityLabel="Back">
          <Text style={styles.backText}>← Back</Text>
        </TouchableOpacity>
      ) : null}

      <View style={styles.heroIcon}>
        <Crown size={26} color={colors.gold} />
      </View>
      <Text style={styles.pageTitle}>CardFlow Premium</Text>
      <Text style={styles.pageSub}>Unlock premium features and grow your business faster.</Text>

      <TouchableOpacity style={styles.historyRow} activeOpacity={0.75} onPress={() => setShowHistory(true)}>
        <Receipt size={16} color={colors.primary} style={{ marginRight: spacing.sm }} />
        <Text style={styles.historyText}>Transaction History</Text>
        <ChevronRight size={16} color={colors.textMuted} />
      </TouchableOpacity>

      {notice ? (
        <View style={[styles.notice, styles[`notice_${notice.tone}`]]} accessibilityLiveRegion="polite">
          <Text style={styles.noticeText}>{notice.text}</Text>
        </View>
      ) : null}

      <StatusCard subscription={subscription} onManage={handleManage} />

      <Card style={styles.perksCard}>
        {PERKS.map((perk) => (
          <View key={perk} style={styles.perkRow}>
            <Check size={16} color={colors.success} />
            <Text style={styles.perkText}>{perk}</Text>
          </View>
        ))}
      </Card>

      {canBuy ? (
        <>
          <Text style={styles.sectionTitle}>Choose a Plan</Text>
          {!configured ? (
            <Card style={styles.emptyCard}>
              <Text style={styles.emptyText}>
                {billingPlatformName === 'web'
                  ? 'CardFlow Premium is available in the CardFlow app for iPhone and Android. Buy it there and it unlocks here too when you sign in with the same account.'
                  : "Subscriptions aren't available on this build yet."}
              </Text>
            </Card>
          ) : packages === null ? (
            <Card style={styles.emptyCard}>
              <ActivityIndicator color={colors.primary} />
              <Text style={styles.emptyText}>Loading plans…</Text>
            </Card>
          ) : loadError || packages.length === 0 ? (
            <Card style={styles.emptyCard}>
              <Text style={styles.emptyText}>{loadError || 'No plans are available right now.'}</Text>
              <TouchableOpacity onPress={fetchOfferings} style={styles.retryRow}>
                <RefreshCw size={14} color={colors.primary} />
                <Text style={styles.retryText}>Try again</Text>
              </TouchableOpacity>
            </Card>
          ) : (
            packages.map((p) => {
              const isSelected = selected === p.id;
              const per = describePeriod(p.period);
              const badge = PACKAGE_BADGES[p.packageType];
              return (
                <TouchableOpacity key={p.id} activeOpacity={0.85} onPress={() => setSelected(p.id)} disabled={isPurchasing}>
                  <Card style={[styles.planCard, isSelected && styles.planCardActive]}>
                    <View style={[styles.radio, isSelected && styles.radioActive]}>{isSelected ? <View style={styles.radioDot} /> : null}</View>
                    <View style={{ flex: 1 }}>
                      <View style={styles.planHeaderRow}>
                        <Text style={styles.planLabel}>{per?.name || p.title}</Text>
                        {badge ? (
                          <View style={styles.badge}>
                            <Text style={styles.badgeText}>{badge}</Text>
                          </View>
                        ) : null}
                      </View>
                      {p.description ? <Text style={styles.planDesc} numberOfLines={2}>{p.description}</Text> : null}
                    </View>
                    <View style={{ alignItems: 'flex-end' }}>
                      <Text style={styles.planPrice}>{p.priceString}</Text>
                      {per ? <Text style={styles.planPer}>per {per.per}</Text> : null}
                    </View>
                  </Card>
                </TouchableOpacity>
              );
            })
          )}

          {pkg ? (
            <Button
              title={isPurchasing ? 'Processing…' : `Continue — ${pkg.priceString}${period ? ` / ${period.per}` : ''}`}
              onPress={handlePurchase}
              loading={isPurchasing}
              disabled={isPurchasing}
              size="lg"
              style={{ marginTop: spacing.md }}
            />
          ) : null}
        </>
      ) : null}

      {configured ? (
        <View style={styles.linksRow}>
          <TouchableOpacity onPress={handleRestore} disabled={isPurchasing} style={styles.linkHit}>
            <Text style={styles.link}>Restore purchases</Text>
          </TouchableOpacity>
          {subscription && subscription.status !== 'FREE' ? (
            <TouchableOpacity onPress={handleManage} disabled={isPurchasing} style={styles.linkHit}>
              <Text style={styles.link}>Manage subscription</Text>
            </TouchableOpacity>
          ) : null}
        </View>
      ) : null}

      {configured ? <Text style={styles.disclaimer}>{STORE_NOTE[billingPlatformName] || STORE_NOTE.web}</Text> : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bgMuted },
  scrollContent: { padding: spacing.lg, paddingBottom: spacing.xxxl },
  backRow: { marginBottom: spacing.sm, alignSelf: 'flex-start', paddingVertical: 4 },
  backText: { fontSize: 14, fontWeight: '600', color: colors.primary },
  heroIcon: {
    width: 52,
    height: 52,
    borderRadius: radii.pill,
    backgroundColor: colors.goldLight,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: spacing.md
  },
  pageTitle: { fontSize: 24, fontWeight: '700', color: colors.textPrimary, marginBottom: 4 },
  pageSub: { ...typography.bodyMedium, marginBottom: spacing.md },
  historyRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: spacing.sm, marginBottom: spacing.md },
  historyText: { flex: 1, fontSize: 13, fontWeight: '600', color: colors.textPrimary },
  notice: { padding: spacing.md, borderRadius: radii.md, marginBottom: spacing.md, borderWidth: 1 },
  notice_success: { backgroundColor: '#ECFDF5', borderColor: '#A7F3D0' },
  notice_error: { backgroundColor: '#FEF2F2', borderColor: '#FECACA' },
  notice_info: { backgroundColor: '#EFF6FF', borderColor: '#BFDBFE' },
  noticeText: { fontSize: 13, color: colors.textPrimary },
  statusCard: { padding: spacing.md, marginBottom: spacing.lg, backgroundColor: colors.goldLight, borderWidth: 1, borderColor: colors.gold },
  statusDanger: { backgroundColor: '#FEF2F2', borderColor: '#FECACA' },
  statusMuted: { backgroundColor: colors.bgMuted, borderColor: colors.border },
  statusTitle: { fontSize: 14, fontWeight: '700', color: colors.textPrimary, flexShrink: 1 },
  statusSub: { fontSize: 12, color: colors.textSecondary, marginTop: 4 },
  perksCard: { padding: spacing.lg, marginBottom: spacing.lg },
  perkRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: spacing.sm },
  perkText: { fontSize: 13, color: colors.textPrimary, flex: 1 },
  sectionTitle: {
    fontSize: 11,
    fontWeight: '700',
    color: colors.textMuted,
    letterSpacing: 0.8,
    marginBottom: spacing.sm,
    marginLeft: 4,
    textTransform: 'uppercase'
  },
  emptyCard: { padding: spacing.lg, alignItems: 'center', gap: spacing.sm },
  emptyText: { fontSize: 13, color: colors.textMuted, textAlign: 'center' },
  retryRow: { flexDirection: 'row', alignItems: 'center', gap: 6, padding: 4 },
  retryText: { fontSize: 13, fontWeight: '600', color: colors.primary },
  planCard: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: spacing.lg,
    marginBottom: spacing.sm,
    gap: spacing.md,
    borderWidth: 1.5,
    borderColor: 'transparent'
  },
  planCardActive: { borderColor: colors.primary, backgroundColor: colors.primaryLight },
  radio: { width: 20, height: 20, borderRadius: 10, borderWidth: 2, borderColor: colors.border, alignItems: 'center', justifyContent: 'center' },
  radioActive: { borderColor: colors.primary },
  radioDot: { width: 10, height: 10, borderRadius: 5, backgroundColor: colors.primary },
  planHeaderRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
  planLabel: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  planDesc: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  badge: { backgroundColor: colors.goldLight, paddingHorizontal: 8, paddingVertical: 2, borderRadius: radii.pill },
  badgeText: { fontSize: 10, fontWeight: '700', color: colors.gold },
  planPrice: { fontSize: 17, fontWeight: '800', color: colors.textPrimary },
  planPer: { fontSize: 11, color: colors.textMuted },
  linksRow: { flexDirection: 'row', justifyContent: 'center', gap: spacing.lg, marginTop: spacing.md, flexWrap: 'wrap' },
  linkHit: { padding: 4 },
  link: { fontSize: 13, fontWeight: '600', color: colors.primary },
  disclaimer: { fontSize: 11, color: colors.textMuted, textAlign: 'center', marginTop: spacing.md, lineHeight: 16 }
});
