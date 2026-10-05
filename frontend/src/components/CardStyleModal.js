import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView, Modal } from 'react-native';
import { Check, X, Lock } from 'lucide-react';
import { colors, radii, spacing, typography } from '../theme';
import { Button } from './Button';
import { BusinessCardPreview, CARD_TEMPLATES, PREMIUM_TEMPLATE_IDS } from './BusinessCardTemplates';
import { getCardTemplate, setCardTemplate } from '../utils/cardTemplateStorage';
import { UpgradeModal } from './UpgradeModal';
import { useAuth } from '../context/AuthContext';

export function CardStyleModal({ visible, business, onClose, onSaved }) {
  const { isPremiumActive } = useAuth();
  const [selected, setSelected] = useState(() => getCardTemplate(business?.id));
  const [showUpgrade, setShowUpgrade] = useState(false);

  useEffect(() => {
    if (visible) setSelected(getCardTemplate(business?.id));
  }, [visible, business?.id]);

  if (!visible) return null;

  const handlePick = (tpl) => {
    if (PREMIUM_TEMPLATE_IDS.includes(tpl.id) && !isPremiumActive) {
      setShowUpgrade(true);
      return;
    }
    setSelected(tpl.id);
  };

  const handleSave = () => {
    setCardTemplate(business?.id, selected);
    onSaved?.(selected);
  };

  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <View style={styles.sheet}>
          <View style={styles.header}>
            <View>
              <Text style={styles.title}>Choose Card Style</Text>
              <Text style={styles.subtitle}>Pick how your digital card looks to visitors.</Text>
            </View>
            <TouchableOpacity onPress={onClose} style={styles.closeHit}>
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>

          <ScrollView style={{ maxHeight: 520 }} showsVerticalScrollIndicator={false}>
            {CARD_TEMPLATES.map((tpl) => {
              const isSelected = selected === tpl.id;
              const isLocked = PREMIUM_TEMPLATE_IDS.includes(tpl.id) && !isPremiumActive;
              return (
                <TouchableOpacity
                  key={tpl.id}
                  onPress={() => handlePick(tpl)}
                  activeOpacity={0.85}
                  style={[styles.option, isSelected && styles.optionSelected]}
                >
                  <View style={styles.optionLabelRow}>
                    <Text style={styles.optionLabel}>
                      {tpl.name}{tpl.id === 'classic' ? ' · Default' : ''}
                    </Text>
                    {isLocked ? (
                      <View style={styles.premiumChip}>
                        <Lock size={11} color={colors.gold} strokeWidth={2.5} />
                        <Text style={styles.premiumChipText}>Premium</Text>
                      </View>
                    ) : isSelected ? (
                      <View style={styles.selectedChip}>
                        <Check size={12} color="#FFFFFF" strokeWidth={3} />
                      </View>
                    ) : null}
                  </View>
                  <View pointerEvents="none" style={isLocked && styles.previewLocked}>
                    <BusinessCardPreview business={business} templateId={tpl.id} />
                  </View>
                </TouchableOpacity>
              );
            })}
          </ScrollView>

          <Button title="Save Style" onPress={handleSave} size="lg" style={{ marginTop: spacing.md }} />
        </View>
      </View>

      <UpgradeModal
        visible={showUpgrade}
        onClose={() => setShowUpgrade(false)}
        title="Unlock premium card styles"
        message="Elegant Dark, Emboss Signet and Boutique Frame are Premium styles. Upgrade to Pro to use them."
      />
    </Modal>
  );
}

const styles = StyleSheet.create({
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.65)', justifyContent: 'center', padding: spacing.md },
  sheet: {
    backgroundColor: colors.bgCard,
    borderRadius: radii.modal,
    padding: spacing.lg,
    maxWidth: 520,
    width: '100%',
    alignSelf: 'center'
  },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: spacing.md },
  title: { ...typography.titleMedium, color: colors.textPrimary },
  subtitle: { fontSize: 12, color: colors.textSecondary, marginTop: 2, maxWidth: 320 },
  closeHit: { padding: 4 },
  option: {
    borderWidth: 2,
    borderColor: 'transparent',
    borderRadius: radii.lg,
    padding: spacing.sm,
    marginBottom: spacing.md
  },
  optionSelected: { borderColor: colors.primary, backgroundColor: colors.primaryLight },
  optionLabelRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing.sm, paddingHorizontal: 4 },
  optionLabel: { fontSize: 13, fontWeight: '700', color: colors.textPrimary },
  selectedChip: {
    width: 20, height: 20, borderRadius: radii.pill,
    backgroundColor: colors.primary, alignItems: 'center', justifyContent: 'center'
  },
  premiumChip: {
    flexDirection: 'row', alignItems: 'center', gap: 4,
    paddingHorizontal: 8, paddingVertical: 3, borderRadius: radii.pill,
    backgroundColor: colors.goldLight
  },
  premiumChipText: { fontSize: 10, fontWeight: '700', color: colors.gold },
  previewLocked: { opacity: 0.45 }
});
