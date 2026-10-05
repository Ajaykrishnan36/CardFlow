import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity } from 'react-native';
import {
  User,
  ChevronRight,
  LogOut,
  Bell,
  Shield,
  FileText,
  Briefcase,
  Phone,
  Save,
  Headphones,
  Palette,
  Crown
} from 'lucide-react';
import { colors, spacing, radii, typography } from '../../theme';
import { Card } from '../../components/Card';
import { Input } from '../../components/Input';
import { Button } from '../../components/Button';
import { ThemeSettings } from '../../components/ThemeSettings';
import { NotificationSettings } from '../../components/NotificationSettings';
import { SubscriptionScreen } from '../../components/SubscriptionScreen';
import { ChangePhoneModal } from '../../components/ChangePhoneModal';
import { useAuth } from '../../context/AuthContext';
import { useCrm } from '../../context/CrmContext';
import { AccountSecurity, VerifiedBadge } from '../crm/AccountSecurity';
import { PlanScreen } from '../crm/PlanScreen';

function formatPhoneDisplay(phone) {
  if (!phone) return '';
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 10) return digits;
  if (digits.length === 12 && digits.startsWith('91')) return digits.slice(2);
  return digits;
}

export function ProfileScreen({ onNavigate, onBack, onSignOut }) {
  const { user, logout, updateProfile } = useAuth();
  const { businesses, active } = useCrm();
  const [showPlan, setShowPlan] = useState(false);
  const [name, setName] = useState(user?.name || '');
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState('');
  const [showTheme, setShowTheme] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [showSubscription, setShowSubscription] = useState(false);
  const [showChangePhone, setShowChangePhone] = useState(false);

  useEffect(() => {
    setName(user?.name || '');
  }, [user]);

  const phoneDisplay = formatPhoneDisplay(user?.phone);
  const nameChanged = name.trim() !== (user?.name || '').trim() && name.trim().length > 0;

  if (showTheme) {
    return <ThemeSettings onBack={() => setShowTheme(false)} />;
  }
  if (showNotifications) {
    return <NotificationSettings onBack={() => setShowNotifications(false)} />;
  }
  if (showSubscription) {
    return <SubscriptionScreen onBack={() => setShowSubscription(false)} />;
  }
  if (showPlan) {
    return <PlanScreen onBack={() => setShowPlan(false)} onUpgrade={() => { setShowPlan(false); setShowSubscription(true); }} />;
  }
  const flash = (msg) => {
    setToast(msg);
    setTimeout(() => setToast(''), 3000);
  };

  const handleSave = async () => {
    if (!name.trim()) {
      alert('Name is required.');
      return;
    }
    setSaving(true);
    try {
      await updateProfile({ name: name.trim() });
      setToast('Profile saved.');
      setTimeout(() => setToast(''), 3000);
    } catch (e) {
      alert(e.message || 'Could not save profile.');
    } finally {
      setSaving(false);
    }
  };

  const primaryItems = [
    { icon: Crown, label: 'Plan', sub: active ? `${active.name} — plan, usage and upgrades` : 'Plans for your business', action: () => setShowPlan(true) },
    { icon: Headphones, label: 'Support', action: () => onNavigate?.('user_support') }
  ];

  const settingsItems = [
    { icon: Palette, label: 'Theme', sub: 'Colors & appearance', action: () => setShowTheme(true) },
    { icon: Bell, label: 'Notifications', action: () => setShowNotifications(true) },
    { icon: Shield, label: 'Privacy', sub: 'Your records are private to each business', action: () => alert('Records, cards and files belong to the business they were saved in. Only its members see them, according to their role. Your public listing is the only thing other people can find.') },
    { icon: FileText, label: 'Terms & Conditions', action: () => alert('Terms & Conditions are available from support on request.') }
  ];

  const renderMenuCard = (items) => (
    <Card style={styles.menuCard}>
      {items.map((item, idx) => {
        const Icon = item.icon;
        return (
          <TouchableOpacity
            key={item.label}
            style={[styles.menuItem, idx === items.length - 1 && { borderBottomWidth: 0 }]}
            onPress={item.action}
            activeOpacity={0.7}
          >
            <Icon size={18} color={colors.textSecondary} style={{ marginRight: spacing.md }} />
            <View style={{ flex: 1 }}>
              <Text style={styles.menuText}>{item.label}</Text>
              {item.sub ? <Text style={styles.menuSub}>{item.sub}</Text> : null}
            </View>
            <ChevronRight size={18} color={colors.textMuted} />
          </TouchableOpacity>
        );
      })}
    </Card>
  );

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
      {onBack ? (
        <TouchableOpacity onPress={onBack} style={styles.backRow} accessibilityLabel="Back">
          <Text style={styles.backText}>← Back</Text>
        </TouchableOpacity>
      ) : null}
      <Text style={styles.pageTitle}>My Profile</Text>

      {toast ? (
        <View style={styles.toast}><Text style={styles.toastText}>{toast}</Text></View>
      ) : null}

      <Card style={styles.headerCard}>
        <View style={styles.profileRow}>
          <View style={styles.avatar}>
            <Text style={styles.avatarText}>{name?.[0]?.toUpperCase() || 'U'}</Text>
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.userName}>{name || 'Your name'}</Text>
            <Text style={styles.userPhone}>+91 {phoneDisplay}</Text>
            {businesses.length > 0 && (
              <View style={styles.bizBadge}>
                <Briefcase size={12} color={colors.primary} />
                <Text style={styles.bizBadgeText}>{businesses.length === 1 ? businesses[0].name : `${businesses.length} businesses`}</Text>
              </View>
            )}
          </View>
        </View>
      </Card>

      <Text style={styles.sectionTitle}>Account Details</Text>
      <Card style={styles.formCard}>
        <Input label="FULL NAME" value={name} onChangeText={setName} leftIcon={User} placeholder="Your name" />

        <View style={styles.labelRow}>
          <Text style={styles.fieldLabel}>MOBILE NUMBER</Text>
          <TouchableOpacity onPress={() => setShowChangePhone(true)} accessibilityLabel="Change mobile number">
            <Text style={styles.changeLink}>Change Number</Text>
          </TouchableOpacity>
        </View>
        <View style={styles.verifiedBox}>
          <Phone size={18} color={colors.textSecondary} />
          <Text style={styles.verifiedValue}>{phoneDisplay ? `+91 ${phoneDisplay}` : ''}</Text>
          <VerifiedBadge verified={user?.phoneVerified !== false} />
        </View>

        <AccountSecurity onToast={flash} />

        {nameChanged ? (
          <Button title="Save Changes" onPress={handleSave} loading={saving} icon={Save} size="lg" style={{ marginTop: spacing.sm }} />
        ) : null}
      </Card>

      <View style={styles.section}>
        {renderMenuCard(primaryItems)}
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>Settings</Text>
        {renderMenuCard(settingsItems)}
      </View>

      <TouchableOpacity style={styles.logoutBtn} onPress={onSignOut || logout} activeOpacity={0.7}>
        <LogOut size={18} color={colors.danger} style={{ marginRight: spacing.sm }} />
        <Text style={styles.logoutText}>Logout</Text>
      </TouchableOpacity>

      <ChangePhoneModal
        visible={showChangePhone}
        currentPhone={user?.phone}
        onClose={() => setShowChangePhone(false)}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  verifiedBox: { flexDirection: 'row', alignItems: 'center', gap: 10, borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 14, paddingVertical: 13, backgroundColor: colors.bgMuted },
  verifiedValue: { flex: 1, fontSize: 15, color: colors.textPrimary },
  container: { flex: 1, backgroundColor: colors.bgMuted },
  scrollContent: { padding: spacing.lg, paddingBottom: spacing.xxxl },
  pageTitle: { fontSize: 24, fontWeight: '700', color: colors.textPrimary, marginBottom: spacing.md },
  backRow: { marginBottom: spacing.sm, alignSelf: 'flex-start', paddingVertical: 4 },
  backText: { fontSize: 14, fontWeight: '600', color: colors.primary },
  toast: {
    backgroundColor: '#ECFDF5',
    borderRadius: radii.md,
    padding: spacing.sm,
    marginBottom: spacing.sm,
    borderWidth: 1,
    borderColor: '#A7F3D0'
  },
  toastText: { color: '#059669', fontWeight: '600', fontSize: 13 },
  headerCard: { padding: spacing.lg, marginBottom: spacing.md },
  profileRow: { flexDirection: 'row', alignItems: 'center' },
  avatar: {
    width: 64,
    height: 64,
    borderRadius: 32,
    backgroundColor: colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: spacing.md
  },
  avatarText: { fontSize: 26, fontWeight: '700', color: colors.primary },
  userName: { fontSize: 18, fontWeight: '700', color: colors.textPrimary },
  userPhone: { fontSize: 14, color: colors.textSecondary, marginTop: 4 },
  bizBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    marginTop: 6,
    backgroundColor: '#EFF6FF',
    alignSelf: 'flex-start',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 12
  },
  bizBadgeText: { fontSize: 11, color: colors.primary, fontWeight: '600', marginLeft: 4 },
  formCard: { padding: spacing.md, marginBottom: spacing.md },
  labelRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing.xs },
  fieldLabel: { ...typography.caption, color: colors.textPrimary, fontWeight: '600' },
  changeLink: { fontSize: 12, fontWeight: '700', color: colors.primary },
  section: { marginBottom: spacing.md },
  sectionTitle: {
    fontSize: 11,
    fontWeight: '700',
    color: colors.textMuted,
    letterSpacing: 0.8,
    marginBottom: spacing.xs,
    marginLeft: 4,
    textTransform: 'uppercase'
  },
  menuCard: { padding: 0, overflow: 'hidden' },
  menuItem: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: 1,
    borderBottomColor: colors.border
  },
  menuText: { fontSize: 14, fontWeight: '600', color: colors.textPrimary },
  menuSub: { fontSize: 12, color: colors.textMuted, marginTop: 2 },
  logoutBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.md,
    marginTop: spacing.sm,
    borderRadius: radii.lg,
    borderWidth: 1,
    borderColor: '#FECACA',
    backgroundColor: '#FEF2F2'
  },
  logoutText: { fontSize: 15, fontWeight: '700', color: colors.danger }
});
