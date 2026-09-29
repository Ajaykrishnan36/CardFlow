import React, { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TextInput } from 'react-native';
import { colors, radii, spacing, typography } from '../../theme';
import { Button } from '../../components/Button';
import { useAuth } from '../../context/AuthContext';

export function OnboardingScreen() {
  const { completeOnboarding, user, loadMyBusinesses } = useAuth();
  // Pre-filled from the business card someone saved of this person (they can change it).
  const [name, setName] = useState(user?.suggestedName || '');
  const claimed = user?.claimedBusinesses || [];
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async () => {
    if (!name.trim()) {
      setError('Please enter your name');
      return;
    }
    setLoading(true);
    try {
      await completeOnboarding({ name: name.trim() });
      if (claimed.length && typeof loadMyBusinesses === 'function') {
        loadMyBusinesses();
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Text style={styles.title}>What's your name?</Text>
      <Text style={styles.subtitle}>This will be shown on your CardFlow profile.</Text>
      {claimed.length ? (
        <View style={styles.claimedBox}>
          <Text style={styles.claimedTitle}>We found your business</Text>
          <Text style={styles.claimedText}>
            {claimed.map((b) => b.name).join(', ')} {claimed.length === 1 ? 'is' : 'are'} already on CardFlow from your business card.
            You'll find it under My Business, ready for you to review and publish.
          </Text>
        </View>
      ) : null}

      <Text style={styles.label}>YOUR NAME</Text>
      <TextInput
        value={name}
        onChangeText={(v) => { setName(v); if (error) setError(''); }}
        placeholder="Enter name"
        placeholderTextColor={colors.textMuted}
        autoFocus
        style={styles.input}
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}

      <Button title="Continue" onPress={handleSubmit} loading={loading} size="lg" style={styles.cta} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  claimedBox: {
    backgroundColor: colors.primaryLight,
    borderRadius: radii.md,
    padding: spacing.md,
    marginBottom: spacing.lg
  },
  claimedTitle: { fontSize: 14, fontWeight: '700', color: colors.primary, marginBottom: 4 },
  claimedText: { fontSize: 13, color: colors.textSecondary, lineHeight: 19 },
  container: { flex: 1, backgroundColor: colors.bgMuted },
  content: { paddingHorizontal: spacing.xxl, paddingTop: 64, paddingBottom: spacing.xxxl },
  title: { ...typography.titleLarge, marginBottom: spacing.sm },
  subtitle: { ...typography.bodyMedium, marginBottom: spacing.xxxl },
  label: { ...typography.label, marginBottom: spacing.sm },
  input: {
    height: 52,
    borderRadius: radii.pill,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.primaryLight,
    paddingHorizontal: spacing.lg,
    fontSize: 16,
    color: colors.textPrimary,
    marginBottom: spacing.md,
    outlineStyle: 'none'
  },
  error: { color: colors.danger, fontSize: 12, marginBottom: spacing.sm },
  cta: { marginTop: spacing.lg }
});
