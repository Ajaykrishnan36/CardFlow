import React, { useState, useEffect, useRef } from 'react';
import { View, Text, StyleSheet, ScrollView, TextInput, Animated } from 'react-native';
import { colors, radii, spacing, typography } from '../../theme';
import { Button } from '../../components/Button';
import { CardFlowLogo } from '../../components/CardFlowLogo';
import { useAuth } from '../../context/AuthContext';

export function LoginScreen({ onOtpRequested }) {
  const { sendOtp, isLoading } = useAuth();
  const [phone, setPhone] = useState('');
  const [error, setError] = useState('');
  const logoScale = useRef(new Animated.Value(0.94)).current;
  const contentOpacity = useRef(new Animated.Value(0)).current;
  const contentY = useRef(new Animated.Value(12)).current;

  useEffect(() => {
    Animated.parallel([
      Animated.timing(logoScale, { toValue: 1, duration: 280, useNativeDriver: true }),
      Animated.timing(contentOpacity, { toValue: 1, duration: 320, useNativeDriver: true }),
      Animated.timing(contentY, { toValue: 0, duration: 320, useNativeDriver: true })
    ]).start();
  }, [logoScale, contentOpacity, contentY]);

  const handleSendOtp = async (inputPhone) => {
    const targetPhone = (inputPhone || phone).trim();
    if (!targetPhone || targetPhone.length < 10) {
      setError('Enter a valid 10-digit mobile number');
      return;
    }
    if (isLoading) return;
    setError('');
    const res = await sendOtp(targetPhone);
    if (res.success) onOtpRequested(targetPhone);
    else setError(res.error || "Couldn't send OTP. Please try again.");
  };

  return (
    <ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
      <Animated.View style={[styles.logoWrap, { transform: [{ scale: logoScale }], opacity: contentOpacity }]}>
        <CardFlowLogo size={52} />
      </Animated.View>

      <Animated.View style={{ width: '100%', alignItems: 'center', opacity: contentOpacity, transform: [{ translateY: contentY }] }}>
      <Text style={styles.title}>Welcome to CardFlow</Text>
      <Text style={styles.subtitle}>Your business connections, all in one place.</Text>

      <Text style={styles.label}>PHONE NUMBER</Text>
      <View style={styles.phoneRow}>
        <View style={styles.codeBox}>
          <Text style={styles.codeText}>+91</Text>
        </View>
        <TextInput
          value={phone}
          onChangeText={(t) => { setPhone(t.replace(/[^0-9]/g, '').slice(0, 10)); if (error) setError(''); }}
          placeholder="00000 00000"
          placeholderTextColor={colors.textMuted}
          keyboardType="numeric"
          maxLength={10}
          style={styles.phoneInput}
        />
      </View>
      {error ? <Text style={styles.error}>{error}</Text> : null}

      <Button
        title={isLoading ? 'Sending OTP...' : 'Continue'}
        onPress={() => handleSendOtp()}
        loading={isLoading}
        disabled={isLoading}
        size="lg"
        style={styles.cta}
      />

      <Text style={styles.legal}>
        By continuing, you agree to{' '}
        <Text style={styles.legalGold}>Terms</Text> &{' '}
        <Text style={styles.legalGold}>Privacy Policy</Text>.
      </Text>
      </Animated.View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: {
    flexGrow: 1,
    paddingHorizontal: spacing.xxl,
    paddingTop: 56,
    paddingBottom: spacing.xxxl,
    backgroundColor: colors.bgMuted,
    alignItems: 'center'
  },
  logoWrap: { marginBottom: spacing.xl },
  title: { ...typography.titleLarge, textAlign: 'center', marginBottom: spacing.sm },
  subtitle: { ...typography.bodyMedium, textAlign: 'center', marginBottom: spacing.xxxl, maxWidth: 280 },
  label: { ...typography.label, alignSelf: 'flex-start', marginBottom: spacing.sm },
  phoneRow: { flexDirection: 'row', width: '100%', gap: spacing.sm, marginBottom: spacing.md },
  codeBox: {
    width: 72,
    height: 52,
    borderRadius: radii.pill,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.bgCard,
    alignItems: 'center',
    justifyContent: 'center'
  },
  codeText: { fontSize: 15, fontWeight: '600', color: colors.textPrimary },
  phoneInput: {
    flex: 1,
    height: 52,
    borderRadius: radii.pill,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.bgCard,
    paddingHorizontal: spacing.lg,
    fontSize: 16,
    color: colors.textPrimary,
    outlineStyle: 'none'
  },
  error: { color: colors.danger, fontSize: 12, alignSelf: 'flex-start', marginBottom: spacing.sm },
  cta: { marginTop: spacing.sm, width: '100%' },
  legal: { marginTop: spacing.lg, fontSize: 12, color: colors.textMuted, textAlign: 'center', lineHeight: 18 },
  legalGold: { color: colors.gold, fontWeight: '600' },
  devSection: { marginTop: spacing.xxxl, width: '100%', opacity: 0.7 },
  devLabel: { fontSize: 10, color: colors.textMuted, marginBottom: spacing.xs, textAlign: 'center' },
  devChip: {
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderRadius: radii.pill,
    borderWidth: 1,
    borderColor: colors.border,
    marginBottom: 4,
    backgroundColor: colors.bgCard
  },
  devChipText: { fontSize: 11, color: colors.textSecondary, textAlign: 'center' }
});
