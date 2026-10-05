import React, { useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView } from 'react-native';
import { Check, Moon, Sun, Palette, RotateCcw, Lock } from 'lucide-react';
import { colors, spacing, radii, typography, activeTheme } from '../theme';
import { THEME_PALETTE, DEFAULT_PRIMARY } from '../theme/palette';
import { saveThemePrefs, clearThemePrefs } from '../theme/themeStorage';
import { Card } from './Card';
import { Button } from './Button';
import { UpgradeModal } from './UpgradeModal';
import { useAuth } from '../context/AuthContext';

function Swatch({ swatch, selected, isDefault, locked, onPress }) {
  return (
    <TouchableOpacity
      onPress={onPress}
      activeOpacity={0.8}
      style={styles.swatchWrap}
      accessibilityLabel={swatch.name}
    >
      <View style={[styles.swatch, { backgroundColor: swatch.value }, selected && styles.swatchSelected]}>
        {locked ? (
          <View style={styles.lockBadge}>
            <Lock size={11} color="#FFFFFF" strokeWidth={2.5} />
          </View>
        ) : selected ? (
          <Check size={16} color="#FFFFFF" strokeWidth={3} />
        ) : null}
      </View>
      <Text style={styles.swatchLabel} numberOfLines={1}>{swatch.name}{isDefault ? ' · Default' : ''}</Text>
    </TouchableOpacity>
  );
}

export function ThemeSettings({ onBack }) {
  const { isPremiumActive } = useAuth();
  const [primary, setPrimary] = useState(activeTheme.primary);
  const [darkMode, setDarkMode] = useState(activeTheme.darkMode);
  const [showUpgrade, setShowUpgrade] = useState(false);

  const isPending = primary !== activeTheme.primary || darkMode !== activeTheme.darkMode;

  const handlePickColor = (swatch) => {
    if (swatch.premium && !isPremiumActive) {
      setShowUpgrade(true);
      return;
    }
    setPrimary(swatch.value);
  };

  const handleApply = () => {
    saveThemePrefs({ primary, darkMode });
    window.location.reload();
  };

  const handleReset = () => {
    clearThemePrefs();
    window.location.reload();
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
      {onBack ? (
        <TouchableOpacity onPress={onBack} style={styles.backRow} accessibilityLabel="Back">
          <Text style={styles.backText}>← Back</Text>
        </TouchableOpacity>
      ) : null}
      <Text style={styles.pageTitle}>Theme</Text>
      <Text style={styles.pageSub}>Pick a color for CardFlow. Changes apply everywhere once you tap Update.</Text>

      <Text style={styles.sectionTitle}>Select Color</Text>
      <Card style={styles.paletteCard}>
        <View style={styles.swatchGrid}>
          {THEME_PALETTE.map((swatch) => (
            <Swatch
              key={`primary-${swatch.value}`}
              swatch={swatch}
              selected={primary === swatch.value}
              isDefault={swatch.value === DEFAULT_PRIMARY}
              locked={!!swatch.premium && !isPremiumActive}
              onPress={() => handlePickColor(swatch)}
            />
          ))}
        </View>
      </Card>

      <Text style={styles.sectionTitle}>Appearance</Text>
      <Card style={styles.modeCard}>
        <TouchableOpacity style={styles.modeRow} activeOpacity={0.8} onPress={() => setDarkMode(false)}>
          <View style={styles.modeIconWrap}>
            <Sun size={18} color={!darkMode ? colors.primary : colors.textMuted} />
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.modeText}>Light Mode</Text>
            <Text style={styles.modeSub}>Bright backgrounds, dark text</Text>
          </View>
          {!darkMode ? <Check size={18} color={colors.primary} strokeWidth={3} /> : null}
        </TouchableOpacity>
        <TouchableOpacity style={[styles.modeRow, { borderBottomWidth: 0 }]} activeOpacity={0.8} onPress={() => setDarkMode(true)}>
          <View style={styles.modeIconWrap}>
            <Moon size={18} color={darkMode ? colors.primary : colors.textMuted} />
          </View>
          <View style={{ flex: 1 }}>
            <Text style={styles.modeText}>Dark Mode</Text>
            <Text style={styles.modeSub}>Dark backgrounds, light text</Text>
          </View>
          {darkMode ? <Check size={18} color={colors.primary} strokeWidth={3} /> : null}
        </TouchableOpacity>
      </Card>

      <Button
        title="Update Theme"
        onPress={handleApply}
        icon={Palette}
        size="lg"
        disabled={!isPending}
        style={{ marginTop: spacing.md }}
      />
      <Button
        title="Reset to Default"
        onPress={handleReset}
        icon={RotateCcw}
        variant="outline"
        size="lg"
        style={{ marginTop: spacing.sm }}
      />

      <UpgradeModal
        visible={showUpgrade}
        onClose={() => setShowUpgrade(false)}
        title="Unlock every accent color"
        message="Royal Purple and Indigo are free. Upgrade to Pro to use all 10 accent colors."
      />
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
  sectionTitle: {
    fontSize: 11,
    fontWeight: '700',
    color: colors.textMuted,
    letterSpacing: 0.8,
    marginBottom: spacing.xs,
    marginLeft: 4,
    textTransform: 'uppercase'
  },
  paletteCard: { padding: spacing.md, marginBottom: spacing.md },
  swatchGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md },
  swatchWrap: { width: 68, alignItems: 'center' },
  swatch: {
    width: 44,
    height: 44,
    borderRadius: radii.pill,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 2,
    borderColor: 'transparent'
  },
  swatchSelected: { borderColor: colors.textPrimary },
  swatchLabel: { fontSize: 10, color: colors.textMuted, marginTop: 6, textAlign: 'center' },
  lockBadge: {
    width: 20,
    height: 20,
    borderRadius: radii.pill,
    backgroundColor: 'rgba(0,0,0,0.35)',
    alignItems: 'center',
    justifyContent: 'center'
  },
  modeCard: { padding: 0, overflow: 'hidden', marginBottom: spacing.md },
  modeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: 1,
    borderBottomColor: colors.border
  },
  modeIconWrap: {
    width: 32,
    height: 32,
    borderRadius: radii.pill,
    backgroundColor: colors.bgMuted,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: spacing.md
  },
  modeText: { fontSize: 14, fontWeight: '600', color: colors.textPrimary },
  modeSub: { fontSize: 12, color: colors.textMuted, marginTop: 2 }
});
