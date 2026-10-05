import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, TouchableOpacity, StyleSheet, Modal } from 'react-native';
import { CheckCircle2, Circle, ChevronRight, X, Home, Briefcase, ScanLine, Building2, Sparkles } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { Button } from '../../components/Button';
import { crmApi } from '../../services/crmApi';

function remembered(key) {
  try {
    return window.localStorage.getItem(key) === '1';
  } catch (e) {
    return false;
  }
}
function remember(key) {
  try {
    window.localStorage.setItem(key, '1');
  } catch (e) {}
}

/**
 * "Getting started" on Home: the first things to do in a new business. What is ticked
 * comes from the business's real data (the server works it out). It disappears when
 * everything is done or the person hides it.
 */
export function GettingStartedCard({ code, onGo }) {
  const hideKey = `cf_start_hidden_${code}`;
  const [data, setData] = useState(null);
  const [hidden, setHidden] = useState(() => remembered(hideKey));

  const load = useCallback(() => {
    if (!code) return;
    crmApi.gettingStarted(code).then(setData).catch(() => setData(null));
  }, [code]);
  useEffect(() => {
    setHidden(remembered(hideKey));
    setData(null);
    load();
  }, [load, hideKey]);

  if (hidden || !data || data.total === 0 || data.done >= data.total) return null;
  const pct = Math.round((data.done / data.total) * 100);
  return (
    <View style={[styles.card, shadows.sm]}>
      <View style={styles.head}>
        <View style={styles.spark}>
          <Sparkles size={16} color={colors.primary} />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={styles.title}>Getting started</Text>
          <Text style={styles.sub}>{data.done} of {data.total} done</Text>
        </View>
        <TouchableOpacity
          onPress={() => {
            remember(hideKey);
            setHidden(true);
          }}
          hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
          accessibilityLabel="Hide getting started"
        >
          <X size={18} color={colors.textMuted} />
        </TouchableOpacity>
      </View>
      <View style={styles.track}>
        <View style={[styles.fill, { width: `${pct}%` }]} />
      </View>
      {data.steps.map((s) => (
        <TouchableOpacity key={s.key} style={styles.step} onPress={() => !s.done && onGo(s.go)} activeOpacity={s.done ? 1 : 0.75} disabled={s.done}>
          {s.done ? <CheckCircle2 size={20} color={colors.success} /> : <Circle size={20} color={colors.borderDark} />}
          <View style={{ flex: 1 }}>
            <Text style={[styles.stepTitle, s.done && styles.stepDone]}>{s.title}</Text>
            {!s.done ? <Text style={styles.stepBody}>{s.body}</Text> : null}
          </View>
          {!s.done ? <ChevronRight size={16} color={colors.textMuted} /> : null}
        </TouchableOpacity>
      ))}
    </View>
  );
}

const SLIDES = [
  { icon: Building2, title: 'Welcome to your CRM', body: 'Everything here belongs to one business. Create more businesses any time — each keeps its own leads, customers and numbers. Switch from the name at the top of Home.' },
  { icon: Home, title: 'Home shows your day', body: 'Leads to call, tasks due, meetings ahead, and income and expenses. Change the date range at the top. Use Quick add to create anything in two taps.' },
  { icon: Briefcase, title: 'My CRM holds your records', body: 'Leads, contacts, accounts, deals, tasks and cases. Open one to call or WhatsApp, add a note, plan a follow-up, or convert a lead into a customer.' },
  { icon: ScanLine, title: 'Scan a card, get a lead', body: 'Tap SCAN and take a photo of a business card. The details are read for you; save it as a lead or contact. Your own digital card and public listing are under My CRM → My business card.' }
];

/** A short tour, shown once on this device the first time someone signs in. */
export function WelcomeTour({ userId }) {
  const key = `cf_tour_seen_${userId || 'me'}`;
  const [open, setOpen] = useState(() => !remembered(key));
  const [i, setI] = useState(0);
  if (!open || !userId) return null;
  const close = () => {
    remember(key);
    setOpen(false);
  };
  const slide = SLIDES[i];
  const Icon = slide.icon;
  const last = i === SLIDES.length - 1;
  return (
    <Modal transparent animationType="fade" visible onRequestClose={close}>
      <View style={styles.overlay}>
        <View style={styles.tour}>
          <TouchableOpacity onPress={close} style={styles.skip} accessibilityLabel="Skip the tour">
            <Text style={styles.skipText}>Skip</Text>
          </TouchableOpacity>
          <View style={styles.tourIcon}>
            <Icon size={28} color={colors.primary} />
          </View>
          <Text style={styles.tourTitle}>{slide.title}</Text>
          <Text style={styles.tourBody}>{slide.body}</Text>
          <View style={styles.dots}>
            {SLIDES.map((_, n) => (
              <View key={n} style={[styles.dot, n === i && styles.dotOn]} />
            ))}
          </View>
          <Button title={last ? 'Start using the CRM' : 'Next'} onPress={() => (last ? close() : setI(i + 1))} size="lg" style={{ alignSelf: 'stretch' }} />
          {i > 0 ? (
            <TouchableOpacity onPress={() => setI(i - 1)} style={{ marginTop: spacing.md }}>
              <Text style={styles.skipText}>Back</Text>
            </TouchableOpacity>
          ) : null}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  card: { backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.primary, padding: spacing.md, marginBottom: spacing.md },
  head: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  spark: { width: 32, height: 32, borderRadius: 10, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center' },
  title: { fontSize: 15, fontWeight: '800', color: colors.textPrimary },
  sub: { fontSize: 12, color: colors.textSecondary, marginTop: 1 },
  track: { height: 6, borderRadius: 3, backgroundColor: colors.bgMutedDark, overflow: 'hidden', marginTop: spacing.md, marginBottom: 4 },
  fill: { height: 6, borderRadius: 3, backgroundColor: colors.primary },
  step: { flexDirection: 'row', alignItems: 'flex-start', gap: 10, paddingVertical: 10, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.border },
  stepTitle: { fontSize: 14, fontWeight: '700', color: colors.textPrimary },
  stepDone: { color: colors.textMuted, textDecorationLine: 'line-through', fontWeight: '600' },
  stepBody: { fontSize: 12, color: colors.textSecondary, marginTop: 2, lineHeight: 17 },
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.65)', alignItems: 'center', justifyContent: 'center', padding: spacing.xl },
  tour: { width: '100%', maxWidth: 380, backgroundColor: colors.bgCard, borderRadius: radii.modal, padding: spacing.xxl, alignItems: 'center' },
  skip: { alignSelf: 'flex-end' },
  skipText: { fontSize: 13, fontWeight: '700', color: colors.textSecondary },
  tourIcon: { width: 64, height: 64, borderRadius: 20, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center', marginTop: spacing.sm, marginBottom: spacing.lg },
  tourTitle: { fontSize: 20, fontWeight: '800', color: colors.textPrimary, textAlign: 'center' },
  tourBody: { fontSize: 14, color: colors.textSecondary, textAlign: 'center', lineHeight: 21, marginTop: spacing.sm, minHeight: 105 },
  dots: { flexDirection: 'row', gap: 6, marginVertical: spacing.lg },
  dot: { width: 7, height: 7, borderRadius: 4, backgroundColor: colors.borderDark },
  dotOn: { width: 20, backgroundColor: colors.primary }
});
