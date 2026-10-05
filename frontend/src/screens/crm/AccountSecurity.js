import React, { useEffect, useState } from 'react';
import { View, Text, TouchableOpacity, StyleSheet, Modal, TextInput } from 'react-native';
import { Mail, KeyRound, BadgeCheck, X } from 'lucide-react';
import { colors, spacing, radii, typography } from '../../theme';
import { Button } from '../../components/Button';
import { OtpBoxes } from '../../components/OtpBoxes';
import { useAuth } from '../../context/AuthContext';
import { crmApi, errorText } from '../../services/crmApi';

/** The green "Verified" mark next to a phone number or email the person has proved is theirs. */
export function VerifiedBadge({ verified = true }) {
  if (!verified) {
    return (
      <View style={[styles.badge, { backgroundColor: colors.warningLight }]}>
        <Text style={[styles.badgeText, { color: colors.warning }]}>Not verified</Text>
      </View>
    );
  }
  return (
    <View style={styles.badge}>
      <BadgeCheck size={13} color={colors.success} />
      <Text style={styles.badgeText}>Verified</Text>
    </View>
  );
}

function Sheet({ visible, title, onClose, children }) {
  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <TouchableOpacity onPress={onClose} accessibilityLabel="Close">
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          <View style={{ padding: spacing.lg }}>{children}</View>
        </View>
      </View>
    </Modal>
  );
}

/** Add or change the account's email: a code goes to the address and must be typed back. */
function EmailSheet({ visible, onClose, onDone }) {
  const [step, setStep] = useState('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [preview, setPreview] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (visible) {
      setStep('email');
      setEmail('');
      setCode('');
      setPreview('');
      setError('');
    }
  }, [visible]);

  const send = async () => {
    setBusy(true);
    setError('');
    try {
      const res = await crmApi.requestEmailCode(email.trim());
      setPreview(res.devCode || '');
      setCode('');
      setStep('code');
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value) => {
    const entered = (value || code).trim();
    if (entered.length !== 6 || busy) return;
    setBusy(true);
    setError('');
    try {
      await crmApi.verifyEmail(email.trim(), entered);
      onDone(email.trim().toLowerCase());
    } catch (e) {
      setError(errorText(e));
      setCode('');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet visible={visible} title={step === 'email' ? 'Add your email' : 'Enter the code'} onClose={onClose}>
      {error ? <Text style={styles.alert}>{error}</Text> : null}
      {step === 'email' ? (
        <>
          <Text style={styles.help}>We’ll email a 6-digit code to confirm the address is yours. You can then sign in with it too.</Text>
          <TextInput
            style={styles.input}
            value={email}
            onChangeText={setEmail}
            placeholder="you@example.com"
            placeholderTextColor={colors.textMuted}
            keyboardType="email-address"
            autoCapitalize="none"
            autoFocus
          />
          <Button title={busy ? 'Sending…' : 'Email me a code'} onPress={send} loading={busy} disabled={busy || !/^\S+@\S+\.\S+$/.test(email.trim())} style={{ marginTop: spacing.md }} />
        </>
      ) : (
        <>
          <Text style={styles.help}>We sent a code to {email.trim()}. It expires in 10 minutes.</Text>
          {preview ? <Text style={styles.preview}>No email service on this server — your code is {preview}</Text> : null}
          <OtpBoxes value={code} onChange={setCode} onComplete={verify} />
          <Button title={busy ? 'Verifying…' : 'Verify email'} onPress={() => verify()} loading={busy} disabled={busy || code.length !== 6} style={{ marginTop: spacing.md }} />
          <TouchableOpacity onPress={() => setStep('email')} style={{ marginTop: spacing.md, alignSelf: 'center' }}>
            <Text style={styles.link}>Use a different address</Text>
          </TouchableOpacity>
        </>
      )}
    </Sheet>
  );
}

/** Set a password (first time) or change it (needs the current one). */
function PasswordSheet({ visible, hasPassword, onClose, onDone }) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [again, setAgain] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (visible) {
      setCurrent('');
      setNext('');
      setAgain('');
      setError('');
    }
  }, [visible]);

  const save = async () => {
    if (next !== again) {
      setError('The two passwords don’t match.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await crmApi.setPassword(next, hasPassword ? current : undefined);
      onDone();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet visible={visible} title={hasPassword ? 'Change password' : 'Set a password'} onClose={onClose}>
      {error ? <Text style={styles.alert}>{error}</Text> : null}
      <Text style={styles.help}>
        {hasPassword ? 'Other devices are signed out when the password changes.' : 'With a password you can also sign in with your email or mobile number and this password.'}
      </Text>
      {hasPassword ? (
        <TextInput style={styles.input} value={current} onChangeText={setCurrent} placeholder="Current password" placeholderTextColor={colors.textMuted} secureTextEntry autoCapitalize="none" />
      ) : null}
      <TextInput style={[styles.input, { marginTop: 8 }]} value={next} onChangeText={setNext} placeholder="New password (at least 8 characters)" placeholderTextColor={colors.textMuted} secureTextEntry autoCapitalize="none" />
      <TextInput style={[styles.input, { marginTop: 8 }]} value={again} onChangeText={setAgain} placeholder="Type it again" placeholderTextColor={colors.textMuted} secureTextEntry autoCapitalize="none" />
      <Button title={busy ? 'Saving…' : 'Save password'} onPress={save} loading={busy} disabled={busy || next.length < 8 || !again || (hasPassword && !current)} style={{ marginTop: spacing.md }} />
    </Sheet>
  );
}

/** The email and password rows of the profile, with their sheets. */
export function AccountSecurity({ onToast }) {
  const { user, refreshAccount } = useAuth();
  const [emailOpen, setEmailOpen] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  return (
    <>
      <View style={styles.labelRow}>
        <Text style={styles.fieldLabel}>EMAIL</Text>
        <TouchableOpacity onPress={() => setEmailOpen(true)} accessibilityLabel={user?.email ? 'Change email' : 'Add email'}>
          <Text style={styles.changeLink}>{user?.email ? 'Change Email' : 'Add Email'}</Text>
        </TouchableOpacity>
      </View>
      <View style={styles.valueBox}>
        <Mail size={18} color={colors.textSecondary} />
        <Text style={[styles.value, !user?.email && { color: colors.textMuted }]} numberOfLines={1}>{user?.email || 'No email yet'}</Text>
        {user?.email ? <VerifiedBadge verified={user.emailVerified} /> : null}
      </View>

      <View style={styles.labelRow}>
        <Text style={styles.fieldLabel}>PASSWORD</Text>
        <TouchableOpacity onPress={() => setPasswordOpen(true)}>
          <Text style={styles.changeLink}>{user?.hasPassword ? 'Change Password' : 'Set Password'}</Text>
        </TouchableOpacity>
      </View>
      <View style={styles.valueBox}>
        <KeyRound size={18} color={colors.textSecondary} />
        <Text style={[styles.value, !user?.hasPassword && { color: colors.textMuted }]}>{user?.hasPassword ? '••••••••' : 'Not set — you sign in with a code'}</Text>
      </View>

      <EmailSheet
        visible={emailOpen}
        onClose={() => setEmailOpen(false)}
        onDone={async () => {
          setEmailOpen(false);
          await refreshAccount().catch(() => {});
          onToast?.('Email verified.');
        }}
      />
      <PasswordSheet
        visible={passwordOpen}
        hasPassword={Boolean(user?.hasPassword)}
        onClose={() => setPasswordOpen(false)}
        onDone={async () => {
          setPasswordOpen(false);
          await refreshAccount().catch(() => {});
          onToast?.('Password saved.');
        }}
      />
    </>
  );
}

const styles = StyleSheet.create({
  badge: { flexDirection: 'row', alignItems: 'center', gap: 4, backgroundColor: colors.successLight, borderRadius: radii.badge, paddingHorizontal: 8, paddingVertical: 3 },
  badgeText: { fontSize: 11, fontWeight: '700', color: colors.success },
  labelRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.md, marginBottom: 6 },
  fieldLabel: { ...typography.label },
  changeLink: { fontSize: 13, fontWeight: '700', color: colors.primary },
  valueBox: { flexDirection: 'row', alignItems: 'center', gap: 10, borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 14, paddingVertical: 13, backgroundColor: colors.bgMuted },
  value: { flex: 1, fontSize: 15, color: colors.textPrimary },
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, paddingBottom: spacing.lg },
  sheetHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  sheetTitle: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  help: { fontSize: 13, color: colors.textSecondary, marginBottom: spacing.md },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  preview: { fontSize: 13, fontWeight: '600', color: colors.textPrimary, backgroundColor: colors.warningLight, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 12, fontSize: 15, color: colors.textPrimary, backgroundColor: colors.bgCard, outlineStyle: 'none' },
  link: { fontSize: 13, fontWeight: '700', color: colors.primary }
});
