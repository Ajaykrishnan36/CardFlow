import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, ActivityIndicator } from 'react-native';
import { Phone, Mail, MapPin, Building2, ShieldCheck, X } from 'lucide-react';
import { colors, radii, spacing } from '../../theme';
import { Card } from '../../components/Card';
import { Button } from '../../components/Button';
import { Snackbar } from '../../components/Snackbar';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../../context/AuthContext';
import { apiClient, fetchPublicCardImageUrl } from '../../services/api';

// Shown when someone opens a "cardflow.app/share/{id}" link — a card another
// user scanned/saved and shared. Not signed in? The shared sign-in page, then this
// same screen re-renders straight into the card view (no detour to Home).
export function SharedCardScreen({ cardId, onDone }) {
  const { isAuthenticated, isSharedCardSaved, saveSharedCardToVault } = useAuth();
  const [card, setCard] = useState(null);
  const [imageUrl, setImageUrl] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const [snackbar, setSnackbar] = useState({ visible: false, message: '', type: 'success' });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await apiClient.getPublicCard(cardId);
        if (cancelled) return;
        setCard(data);
        if (data?.original_card_image_url) {
          const url = await fetchPublicCardImageUrl(cardId, 'front');
          if (!cancelled) setImageUrl(url);
        }
      } catch (e) {
        if (!cancelled) setError(e.message || 'This shared card link is no longer available.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [cardId]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await saveSharedCardToVault(card);
      setSnackbar({ visible: true, message: `Saved ${card.person_name || card.company || 'this contact'} to My Cards.`, type: 'success' });
    } catch (e) {
      setSnackbar({ visible: true, message: e?.message || 'Could not save this card.', type: 'error' });
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <View style={styles.center}>
        <ActivityIndicator size="large" color={colors.primary} />
      </View>
    );
  }

  if (error || !card) {
    return (
      <View style={styles.center}>
        <Text style={styles.errorText}>{error || 'This card could not be found.'}</Text>
        <Button title="Go to the app" onPress={onDone} style={{ marginTop: spacing.lg }} />
      </View>
    );
  }

  if (!isAuthenticated) {
    return (
      <View style={{ flex: 1 }}>
        <View style={styles.shareBanner}>
          <Text style={styles.shareBannerText}>
            {(card.person_name || card.company || 'Someone')} shared a business card with you.
            Sign in with your mobile number to view and save it.
          </Text>
        </View>
        <View style={{ padding: spacing.lg }}>
          <Button title="Sign in to view and save" size="lg" onPress={() => navigate(`/crm/login?returnTo=${encodeURIComponent(`/share/${cardId}`)}`)} />
        </View>
      </View>
    );
  }

  const alreadySaved = isSharedCardSaved(card);
  const phoneNum = card.phones?.[0]?.raw || '';
  const email = card.emails?.[0] || '';

  return (
    <View style={{ flex: 1, backgroundColor: colors.bgMuted }}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onDone} style={styles.closeBtn} accessibilityLabel="Close">
          <X size={20} color={colors.textSecondary} />
        </TouchableOpacity>
      </View>
      <ScrollView contentContainerStyle={{ padding: spacing.lg }}>
        <Card style={styles.card}>
          {imageUrl ? <img src={imageUrl} alt="Business card" style={styles.cardImg} /> : null}
          <Text style={styles.name}>{card.person_name || card.company || 'Contact'}</Text>
          {card.designation ? <Text style={styles.role}>{card.designation}</Text> : null}
          {card.company ? (
            <View style={styles.row}><Building2 size={14} color={colors.primary} /><Text style={styles.rowText}>{card.company}</Text></View>
          ) : null}
          {phoneNum ? (
            <View style={styles.row}><Phone size={14} color={colors.primary} /><Text style={styles.rowText}>{phoneNum}</Text></View>
          ) : null}
          {email ? (
            <View style={styles.row}><Mail size={14} color={colors.primary} /><Text style={styles.rowText}>{email}</Text></View>
          ) : null}
          {card.raw_address ? (
            <View style={styles.row}><MapPin size={14} color={colors.primary} /><Text style={styles.rowText}>{card.raw_address}</Text></View>
          ) : null}
          {card.gstin ? (
            <View style={styles.row}><ShieldCheck size={14} color={colors.gold} /><Text style={styles.rowText}>GSTIN: {card.gstin}</Text></View>
          ) : null}
        </Card>

        <Button
          title={alreadySaved ? 'Already in My Cards' : (saving ? 'Saving…' : 'Save to My Cards')}
          onPress={handleSave}
          disabled={alreadySaved || saving}
          loading={saving}
          size="lg"
          style={{ marginTop: spacing.md }}
        />
        <Button title="Continue to CardFlow" variant="outline" onPress={onDone} size="lg" style={{ marginTop: spacing.sm }} />
      </ScrollView>
      <Snackbar
        visible={snackbar.visible}
        message={snackbar.message}
        type={snackbar.type}
        onDismiss={() => setSnackbar((s) => ({ ...s, visible: false }))}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.bgMuted, padding: spacing.xl },
  errorText: { fontSize: 14, color: colors.textSecondary, textAlign: 'center' },
  shareBanner: { backgroundColor: colors.primaryLight, padding: spacing.md },
  shareBannerText: { fontSize: 13, color: colors.primary, fontWeight: '600', textAlign: 'center' },
  topBar: { flexDirection: 'row', justifyContent: 'flex-end', padding: spacing.md },
  closeBtn: { width: 36, height: 36, alignItems: 'center', justifyContent: 'center', borderRadius: radii.pill, backgroundColor: colors.bgCard },
  card: { padding: spacing.lg },
  cardImg: { width: '100%', maxHeight: 200, objectFit: 'contain', borderRadius: radii.md, marginBottom: spacing.md, backgroundColor: '#0F172A' },
  name: { fontSize: 19, fontWeight: '800', color: colors.textPrimary },
  role: { fontSize: 13, color: colors.textSecondary, marginTop: 2, marginBottom: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginTop: spacing.sm },
  rowText: { fontSize: 13, color: colors.textSecondary, flex: 1 }
});
