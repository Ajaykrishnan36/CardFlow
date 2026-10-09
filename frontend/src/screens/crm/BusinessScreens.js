import React, { useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, Modal } from 'react-native';
import { Building2, Check, Plus, X } from 'lucide-react';
import { colors, spacing, radii, typography } from '../../theme';
import { Button } from '../../components/Button';
import { Input } from '../../components/Input';
import { useAuth } from '../../context/AuthContext';
import { useCrm } from '../../context/CrmContext';
import { errorText } from '../../services/crmApi';
import { Chip, Loading, ErrorBox } from './ui';

const INDUSTRIES = ['Retail', 'Wholesale & distribution', 'Automotive', 'Manufacturing', 'Agriculture', 'Transport & logistics', 'Services', 'Real estate', 'Construction', 'Education', 'Healthcare', 'Hospitality', 'Technology', 'Finance', 'Other'];

/** The form that creates a business (a CRM of its own). Used on first sign-in and from the switcher. */
export function BusinessForm({ onCreated, onCancel, first }) {
  const { createBusiness } = useCrm();
  const { user, updateProfile } = useAuth();
  const needsName = !user?.name || user.name === 'New user' || user.name === 'CardFlow User';
  const [yourName, setYourName] = useState('');
  const [name, setName] = useState('');
  const [industry, setIndustry] = useState('');
  const [city, setCity] = useState(user?.city && user.city !== 'Coimbatore' ? user.city : '');
  const [errors, setErrors] = useState({});
  const [alert, setAlert] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const fe = {};
    if (needsName && yourName.trim().length < 2) fe.yourName = 'Enter your name.';
    if (name.trim().length < 2) fe.name = 'Enter your business name.';
    setErrors(fe);
    setAlert('');
    if (Object.keys(fe).length) return;
    setBusy(true);
    try {
      if (needsName) await updateProfile({ name: yourName.trim() });
      const biz = await createBusiness({ name: name.trim(), industry, city: city.trim() });
      onCreated?.(biz);
    } catch (e) {
      setErrors(e.fieldErrors || {});
      setAlert(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <ScrollView contentContainerStyle={styles.form} keyboardShouldPersistTaps="handled">
      <View style={styles.hero}>
        <View style={styles.heroIcon}>
          <Building2 size={26} color={colors.primary} />
        </View>
        <Text style={styles.title}>{first ? 'Create your business' : 'New business'}</Text>
        <Text style={styles.subtitle}>
          You get a CRM for it — leads, contacts, deals, tasks, cases and finance. You’ll be its Super Admin and can invite your team later.
        </Text>
      </View>
      {alert ? <Text style={styles.alert}>{alert}</Text> : null}
      {needsName ? <Input label="Your name" value={yourName} onChangeText={setYourName} placeholder="Ajay Krishnan" error={errors.yourName} /> : null}
      <Input label="Business name" value={name} onChangeText={setName} placeholder="Sri Lakshmi Traders" error={errors.name} maxLength={120} />
      <Text style={styles.label}>Industry</Text>
      <View style={styles.chips}>
        {INDUSTRIES.map((i) => (
          <View key={i} style={{ marginBottom: 8 }}>
            <Chip label={i} active={industry === i} onPress={() => setIndustry(industry === i ? '' : i)} />
          </View>
        ))}
      </View>
      <Input label="City" value={city} onChangeText={setCity} placeholder="Coimbatore" error={errors.city} />
      <Button title={busy ? 'Creating…' : 'Create business'} onPress={submit} loading={busy} disabled={busy} style={{ marginTop: spacing.md }} />
      {onCancel ? <Button title="Cancel" variant="ghost" onPress={onCancel} style={{ marginTop: spacing.sm }} /> : null}
    </ScrollView>
  );
}

/**
 * Shown instead of the CRM tabs while there is nothing to open: still loading, an old
 * session that must sign in again, an error, or no business yet.
 */
export function BusinessGate({ children }) {
  const { status, error, businesses, canCreate, reload } = useCrm();
  if (status === 'loading') return <Loading text="Opening your business…" />;
  if (status === 'error') return <ErrorBox message={error} onRetry={reload} />;
  if (businesses.length === 0) {
    if (!canCreate) {
      return (
        <View style={styles.gate}>
          <Text style={styles.title}>No business yet</Text>
          <Text style={styles.subtitle}>Creating a business isn’t open right now. Ask your administrator for an invitation — it will appear here.</Text>
          <Button title="Check again" variant="outline" onPress={reload} style={{ marginTop: spacing.lg }} />
        </View>
      );
    }
    return <BusinessForm first />;
  }
  return children;
}

/** Bottom sheet: every business the person belongs to, with "new business". */
export function BusinessSwitcher({ visible, onClose, onSwitched }) {
  const { businesses, activeCode, switchBusiness, canCreate } = useCrm();
  const [creating, setCreating] = useState(false);
  const close = () => {
    setCreating(false);
    onClose();
  };
  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={close}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} onPress={close} activeOpacity={1} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{creating ? 'New business' : 'Your businesses'}</Text>
            <TouchableOpacity onPress={close} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          {creating ? (
            <BusinessForm
              onCreated={(biz) => {
                if (biz?.code) onSwitched?.(biz.code);
                close();
              }}
              onCancel={() => setCreating(false)}
            />
          ) : (
            <ScrollView style={{ maxHeight: 420 }}>
              {businesses.map((b) => (
                <TouchableOpacity
                  key={b.id}
                  style={styles.bizRow}
                  onPress={() => {
                    switchBusiness(b.code);
                    onSwitched?.(b.code);
                    close();
                  }}
                >
                  <View style={styles.bizAvatar}>
                    <Text style={styles.bizAvatarText}>{b.name.slice(0, 2).toUpperCase()}</Text>
                  </View>
                  <View style={{ flex: 1, minWidth: 0 }}>
                    <Text style={styles.bizName} numberOfLines={1}>{b.name}</Text>
                    <Text style={styles.bizMeta} numberOfLines={1}>{b.roleName || 'Member'} · {b.members} {b.members === 1 ? 'person' : 'people'}</Text>
                  </View>
                  {b.code === activeCode ? <Check size={18} color={colors.primary} /> : null}
                </TouchableOpacity>
              ))}
              {canCreate ? (
                <TouchableOpacity style={styles.bizRow} onPress={() => setCreating(true)}>
                  <View style={[styles.bizAvatar, { backgroundColor: colors.primaryLight }]}>
                    <Plus size={18} color={colors.primary} />
                  </View>
                  <Text style={[styles.bizName, { color: colors.primary }]}>New business</Text>
                </TouchableOpacity>
              ) : null}
              <Text style={styles.note}>Each business has its own leads, contacts and reports. Nothing is shared between them.</Text>
            </ScrollView>
          )}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  form: { padding: spacing.xl, paddingBottom: 48 },
  hero: { alignItems: 'center', marginBottom: spacing.lg },
  heroIcon: { width: 56, height: 56, borderRadius: 18, backgroundColor: colors.primaryLight, alignItems: 'center', justifyContent: 'center', marginBottom: spacing.md },
  title: { ...typography.titleMedium, textAlign: 'center' },
  subtitle: { ...typography.bodyMedium, color: colors.textSecondary, textAlign: 'center', marginTop: 6 },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  label: { ...typography.label, marginBottom: 8, marginTop: 4 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', marginBottom: spacing.sm },
  gate: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing.xxl },
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, paddingBottom: spacing.xl, maxHeight: '88%' },
  sheetHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  sheetTitle: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  bizRow: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 12, paddingHorizontal: spacing.lg },
  bizAvatar: { width: 40, height: 40, borderRadius: 12, backgroundColor: colors.primary, alignItems: 'center', justifyContent: 'center' },
  bizAvatarText: { color: '#FFFFFF', fontWeight: '800', fontSize: 13 },
  bizName: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  bizMeta: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  note: { fontSize: 12, color: colors.textMuted, paddingHorizontal: spacing.lg, paddingTop: spacing.md }
});
