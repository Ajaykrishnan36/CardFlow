import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView } from 'react-native';
import { Receipt } from 'lucide-react';
import { colors, spacing, radii, typography } from '../theme';
import { Card } from './Card';
import { useAuth } from '../context/AuthContext';
import { apiClient } from '../services/api';

function formatDate(iso) {
  if (!iso) return '';
  try {
    return new Date(iso).toLocaleString('en-IN', {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  } catch (e) {
    return '';
  }
}

// Event types from the server (RevenueCat events + pre-migration payments).
const TYPE_STYLES = {
  INITIAL_PURCHASE: { bg: '#ECFDF5', color: '#059669', label: 'Purchased' },
  RENEWAL: { bg: '#ECFDF5', color: '#059669', label: 'Renewed' },
  NON_RENEWING_PURCHASE: { bg: '#ECFDF5', color: '#059669', label: 'Purchased' },
  PRODUCT_CHANGE: { bg: '#EFF6FF', color: '#2563EB', label: 'Plan changed' },
  UNCANCELLATION: { bg: '#EFF6FF', color: '#2563EB', label: 'Resumed' },
  CANCELLATION: { bg: '#FEF3C7', color: '#B45309', label: 'Cancelled' },
  BILLING_ISSUE: { bg: '#FEE2E2', color: '#DC2626', label: 'Payment issue' },
  EXPIRATION: { bg: '#F1F5F9', color: '#475569', label: 'Expired' },
  LEGACY_PAYMENT: { bg: '#ECFDF5', color: '#059669', label: 'Paid' },
  LEGACY_CREATED: { bg: '#FEF3C7', color: '#B45309', label: 'Pending' },
  LEGACY_FAILED: { bg: '#FEE2E2', color: '#DC2626', label: 'Failed' }
};

const STORE_LABELS = {
  APP_STORE: 'App Store',
  PLAY_STORE: 'Google Play',
  RC_BILLING: 'Web',
  STRIPE: 'Web',
  TEST_STORE: 'Test Store',
  PROMOTIONAL: 'Promotional',
  legacy: 'Earlier purchase'
};

const LEGACY_PLAN_NAMES = { '3m': '3 Months', '6m': '6 Months', '12m': '12 Months', lifetime: 'Lifetime' };

// cardflow_premium_annual → "Premium Annual" (store product ids are not user-facing).
function productLabel(id) {
  if (!id) return '';
  return id
    .replace(/^cardflow[_.-]?/i, '')
    .split(/[_.\-:]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}

function formatAmount(price, currency) {
  if (price == null || Number(price) === 0) return '';
  try {
    return new Intl.NumberFormat('en-IN', { style: 'currency', currency: currency || 'INR', maximumFractionDigits: 2 }).format(price);
  } catch (e) {
    return `${currency || ''} ${price}`.trim();
  }
}

export function TransactionHistoryScreen({ onBack }) {
  const { token } = useAuth();
  const [transactions, setTransactions] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    apiClient
      .getBillingTransactions(token)
      .then((data) => {
        if (alive) setTransactions(data?.transactions || []);
      })
      .catch(() => {})
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [token]);

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
      {onBack ? (
        <TouchableOpacity onPress={onBack} style={styles.backRow} accessibilityLabel="Back">
          <Text style={styles.backText}>← Back</Text>
        </TouchableOpacity>
      ) : null}
      <Text style={styles.pageTitle}>Transaction History</Text>
      <Text style={styles.pageSub}>Your Pro purchases, renewals and changes.</Text>

      {loading ? (
        <Text style={styles.emptyText}>Loading...</Text>
      ) : transactions.length === 0 ? (
        <Card style={styles.emptyCard}>
          <Receipt size={22} color={colors.textMuted} />
          <Text style={styles.emptyText}>No transactions yet.</Text>
        </Card>
      ) : (
        transactions.map((t, idx) => {
          const typeStyle = TYPE_STYLES[t.type] || { bg: '#F1F5F9', color: '#475569', label: t.type };
          const plan = t.source === 'legacy' ? LEGACY_PLAN_NAMES[t.product_id] || t.product_id : productLabel(t.product_id);
          const store = STORE_LABELS[t.store] || t.store;
          return (
            <Card key={t.id || idx} style={styles.txCard}>
              <View style={styles.txRow}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.txPlan}>{plan || 'Pro'}</Text>
                  <Text style={styles.txDate}>{formatDate(t.at)}</Text>
                </View>
                <Text style={styles.txAmount}>{formatAmount(t.price, t.currency)}</Text>
              </View>
              <View style={styles.txFooter}>
                <View style={[styles.statusBadge, { backgroundColor: typeStyle.bg }]}>
                  <Text style={[styles.statusText, { color: typeStyle.color }]}>{typeStyle.label}</Text>
                </View>
                <Text style={styles.txId} numberOfLines={1}>
                  {[store, t.environment === 'SANDBOX' ? 'Test' : ''].filter(Boolean).join(' · ')}
                </Text>
              </View>
            </Card>
          );
        })
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bgMuted },
  scrollContent: { padding: spacing.lg, paddingBottom: spacing.xxxl },
  backRow: { marginBottom: spacing.sm, alignSelf: 'flex-start', paddingVertical: 4 },
  backText: { fontSize: 14, fontWeight: '600', color: colors.primary },
  pageTitle: { fontSize: 24, fontWeight: '700', color: colors.textPrimary, marginBottom: 4 },
  pageSub: { ...typography.bodyMedium, marginBottom: spacing.lg },
  emptyCard: { padding: spacing.xl, alignItems: 'center', gap: spacing.sm },
  emptyText: { fontSize: 13, color: colors.textMuted, textAlign: 'center' },
  txCard: { padding: spacing.md, marginBottom: spacing.sm },
  txRow: { flexDirection: 'row', alignItems: 'center' },
  txPlan: { fontSize: 14, fontWeight: '700', color: colors.textPrimary },
  txDate: { fontSize: 12, color: colors.textMuted, marginTop: 2 },
  txAmount: { fontSize: 16, fontWeight: '800', color: colors.textPrimary },
  txFooter: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.sm },
  statusBadge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: radii.pill },
  statusText: { fontSize: 10, fontWeight: '700' },
  txId: { fontSize: 10, color: colors.textMuted, flexShrink: 1, marginLeft: spacing.sm, textAlign: 'right' }
});
