import React, { useState, useEffect, useRef } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity } from 'react-native';
import {
  Phone,
  MessageSquare,
  MapPin,
  Mail,
  Share2,
  UserPlus,
  Image as ImageIcon,
  Pencil,
  ShieldCheck,
  Building2,
  Trash2,
  Users
} from 'lucide-react';
import { colors, radii, spacing } from '../../theme';
import { Card } from '../../components/Card';
import { Button } from '../../components/Button';
import { Input } from '../../components/Input';
import { DetailScreenHeader } from '../../components/DetailScreenHeader';
import { CardCrmPanel } from '../crm/CardCrmPanel';
import { CardViewToggle } from '../../components/CardViewToggle';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { Snackbar } from '../../components/Snackbar';
import { useAuth } from '../../context/AuthContext';
import { fetchCardOriginalImageUrl, cardOriginalImagePath, apiClient } from '../../services/api';
import { buildVCard, downloadTextFile } from '../../utils/vcard';
import { isNativePlatform, saveContactToPhone } from '../../utils/contactsSync';

function hasWhatsApp(phones) {
  return (phones || []).some((p) => p && (p.is_whatsapp || p.isWhatsApp));
}

function whatsappNumber(card, phone) {
  const wa = (card.phones || []).find((p) => p.is_whatsapp || p.isWhatsApp);
  return (wa?.raw || wa?.e164 || phone || '').replace(/[^0-9]/g, '');
}

export function SavedCardDetailScreen({ card, onBack, onHome, onUpdated, onDeleted, onOpenRecord }) {
  const { token, loadUserVault } = useAuth();
  const [viewMode, setViewMode] = useState('digital');
  const [originalSide, setOriginalSide] = useState('front');
  const [frontUrl, setFrontUrl] = useState(null);
  const [backUrl, setBackUrl] = useState(null);
  const [imageLoading, setImageLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [savingContact, setSavingContact] = useState(false);
  const [liveCard, setLiveCard] = useState(card);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [snackbar, setSnackbar] = useState({ visible: false, message: '', type: 'success' });
  const fileInputRef = useRef(null);
  const replaceSideRef = useRef('front');

  useEffect(() => {
    setLiveCard(card);
  }, [card]);

  useEffect(() => {
    let frontBlob = null;
    let backBlob = null;
    let cancelled = false;

    const load = async () => {
      if (!liveCard?.id || !token) {
        if (!cancelled) setImageLoading(false);
        return;
      }
      setImageLoading(true);
      const frontPath = liveCard.original_card_image_url || liveCard.originalCardImageUrl || cardOriginalImagePath(liveCard.id, 'front');
      const backPath = liveCard.original_back_image_url || liveCard.originalBackImageUrl || cardOriginalImagePath(liveCard.id, 'back');
      const [front, back] = await Promise.all([
        fetchCardOriginalImageUrl(frontPath, token),
        fetchCardOriginalImageUrl(backPath, token)
      ]);
      frontBlob = front;
      backBlob = back;
      if (!cancelled) {
        setFrontUrl(front);
        setBackUrl(back);
        setImageLoading(false);
        if (!front && back) setOriginalSide('back');
      }
    };

    load();
    return () => {
      cancelled = true;
      if (frontBlob?.startsWith?.('blob:')) URL.revokeObjectURL(frontBlob);
      if (backBlob?.startsWith?.('blob:')) URL.revokeObjectURL(backBlob);
    };
  }, [liveCard?.id, liveCard?.original_card_image_url, liveCard?.original_back_image_url, token]);

  const [form, setForm] = useState({});
  useEffect(() => {
    if (!liveCard) return;
    setForm({
      person_name: liveCard.person_name || liveCard.personName || '',
      designation: liveCard.designation || '',
      company: liveCard.company || '',
      phone: liveCard.phones?.[0]?.raw || liveCard.phone || '',
      whatsapp: (liveCard.phones || []).find((p) => p.is_whatsapp)?.raw || '',
      email: liveCard.emails?.[0] || liveCard.email || '',
      website: liveCard.website || '',
      gstin: liveCard.gstin || '',
      raw_address: liveCard.raw_address || liveCard.rawAddress || '',
      city: liveCard.city || '',
      state: liveCard.state || '',
      pincode: liveCard.pincode || ''
    });
  }, [liveCard]);

  const handleUploadOriginal = (file, side) => {
    if (!file || !liveCard?.id || !token) return;
    setUploading(true);
    const reader = new FileReader();
    reader.onload = async (ev) => {
      try {
        await apiClient.uploadCardOriginalImage(liveCard.id, ev.target.result, token, side);
        const url = await fetchCardOriginalImageUrl(cardOriginalImagePath(liveCard.id, side), token);
        if (side === 'back') setBackUrl(url);
        else setFrontUrl(url);
        await loadUserVault(token);
      } catch (err) {
        alert(err.message || 'Could not upload original card image.');
      } finally {
        setUploading(false);
      }
    };
    reader.readAsDataURL(file);
  };

  const handleSaveEdits = async () => {
    setSaving(true);
    try {
      const address = [form.raw_address, form.city, form.state, form.pincode].filter(Boolean).join(', ');
      const phones = [
        form.phone ? { raw: form.phone, e164: form.phone.replace(/[^0-9+]/g, ''), type: 'mobile', is_whatsapp: false } : null,
        form.whatsapp ? { raw: form.whatsapp, e164: form.whatsapp.replace(/[^0-9+]/g, ''), type: 'mobile', is_whatsapp: true } : null
      ].filter(Boolean);
      const updated = await apiClient.updateCard(liveCard.id, {
        person_name: form.person_name,
        designation: form.designation,
        company: form.company,
        website: form.website,
        gstin: form.gstin,
        raw_address: address,
        phones,
        emails: form.email ? [form.email] : []
      }, token);
      const next = {
        ...liveCard,
        ...updated,
        person_name: form.person_name,
        designation: form.designation,
        company: form.company,
        website: form.website,
        gstin: form.gstin,
        raw_address: address,
        phones,
        emails: form.email ? [form.email] : []
      };
      setLiveCard(next);
      await loadUserVault(token);
      setEditing(false);
      onUpdated && onUpdated(next);
    } catch (err) {
      alert(err.message || 'Could not save changes.');
    } finally {
      setSaving(false);
    }
  };

  if (!liveCard) return null;

  const phone = liveCard.phones?.[0]?.raw || liveCard.phone || '';
  const email = liveCard.emails?.[0] || liveCard.email || '';
  const gstin = liveCard.gstin || '';
  const address = liveCard.raw_address || liveCard.rawAddress || '';
  const originalUrl = originalSide === 'back' ? backUrl : frontUrl;
  const showWhatsApp = hasWhatsApp(liveCard.phones) && whatsappNumber(liveCard, phone);

  const openMaps = () => {
    if (liveCard.latitude && liveCard.longitude) {
      window.open(`https://www.google.com/maps?q=${liveCard.latitude},${liveCard.longitude}`, '_blank');
    } else if (address) {
      window.open(`https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(address)}`, '_blank');
    }
  };

  const saveContact = async () => {
    if (isNativePlatform()) {
      setSavingContact(true);
      try {
        await saveContactToPhone(liveCard);
        alert('Saved to your phone\'s contacts.');
      } catch (e) {
        alert(e?.message || "Couldn't save to your phone's contacts.");
      } finally {
        setSavingContact(false);
      }
      return;
    }
    downloadTextFile(
      `${(liveCard.person_name || liveCard.company || 'contact').replace(/\s+/g, '_')}.vcf`,
      buildVCard(liveCard)
    );
  };

  const handleRemove = async () => {
    setRemoving(true);
    try {
      await apiClient.deleteCard(liveCard.id, token);
      await loadUserVault(token);
      setConfirmRemove(false);
      onDeleted ? onDeleted(liveCard) : onBack();
    } catch (e) {
      setConfirmRemove(false);
      setSnackbar({ visible: true, message: e?.message || 'Could not remove this card.', type: 'error' });
    } finally {
      setRemoving(false);
    }
  };

  const handleShare = async () => {
    const shareUrl = `${window.location.origin}/share/${liveCard.id}`;
    const title = liveCard.person_name || liveCard.company || 'Business card';
    const text = `${title}${liveCard.company ? ' — ' + liveCard.company : ''} on CardFlow`;
    if (navigator.share) {
      try {
        await navigator.share({ title, text, url: shareUrl });
      } catch (e) {
        // user cancelled the share sheet — nothing to do
      }
      return;
    }
    try {
      await navigator.clipboard.writeText(shareUrl);
      setSnackbar({ visible: true, message: 'Share link copied to clipboard.', type: 'success' });
    } catch (e) {
      setSnackbar({ visible: true, message: shareUrl, type: 'success' });
    }
  };

  return (
    <View style={styles.wrapper}>
      <DetailScreenHeader
        title={liveCard.person_name || liveCard.personName || 'Contact'}
        subtitle={liveCard.company || liveCard.company_name || ''}
        onBack={onBack}
        onHome={onHome}
        rightAction={(
          <TouchableOpacity onPress={() => setEditing(!editing)} style={styles.editHit}>
            <Pencil size={18} color={colors.primary} />
          </TouchableOpacity>
        )}
      />
      <ScrollView style={styles.container} contentContainerStyle={styles.content}>
        {liveCard.saved_count > 1 ? (
          <View style={styles.savedCountRow}>
            <Users size={14} color={colors.primary} />
            <Text style={styles.savedCountText}>{liveCard.saved_count} people have saved this card</Text>
          </View>
        ) : null}
        <CardViewToggle
          value={viewMode}
          onChange={setViewMode}
          options={[
            { id: 'digital', label: 'Digital Card' },
            { id: 'original', label: 'Original' }
          ]}
        />

        {editing ? (
          <Card style={styles.formCard}>
            <Text style={styles.detailsTitle}>Edit Card</Text>
            <Text style={styles.editHint}>Card Images</Text>
            <View style={styles.replaceRow}>
              <View style={{ flex: 1 }}>
                <Text style={styles.detailLabel}>Front</Text>
                {frontUrl ? <img src={frontUrl} alt="Front" style={styles.replaceImg} /> : <Text style={styles.noImageText}>None yet</Text>}
                <Button
                  title="Replace"
                  variant="outline"
                  size="sm"
                  loading={uploading && replaceSideRef.current === 'front'}
                  onPress={() => { replaceSideRef.current = 'front'; fileInputRef.current && fileInputRef.current.click(); }}
                  style={{ marginTop: 8 }}
                />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.detailLabel}>Back</Text>
                {backUrl ? <img src={backUrl} alt="Back" style={styles.replaceImg} /> : <Text style={styles.noImageText}>None yet</Text>}
                <Button
                  title={backUrl ? 'Replace' : 'Add'}
                  variant="outline"
                  size="sm"
                  loading={uploading && replaceSideRef.current === 'back'}
                  onPress={() => { replaceSideRef.current = 'back'; fileInputRef.current && fileInputRef.current.click(); }}
                  style={{ marginTop: 8 }}
                />
              </View>
            </View>
            <Input label="NAME" value={form.person_name} onChangeText={(v) => setForm((f) => ({ ...f, person_name: v }))} />
            <Input label="DESIGNATION" value={form.designation} onChangeText={(v) => setForm((f) => ({ ...f, designation: v }))} />
            <Input label="COMPANY" value={form.company} onChangeText={(v) => setForm((f) => ({ ...f, company: v }))} />
            <Input label="PHONE" value={form.phone} onChangeText={(v) => setForm((f) => ({ ...f, phone: v }))} />
            <Input label="WHATSAPP" value={form.whatsapp} onChangeText={(v) => setForm((f) => ({ ...f, whatsapp: v }))} />
            <Input label="EMAIL" value={form.email} onChangeText={(v) => setForm((f) => ({ ...f, email: v }))} />
            <Input label="WEBSITE" value={form.website} onChangeText={(v) => setForm((f) => ({ ...f, website: v }))} />
            <Input label="GSTIN" value={form.gstin} onChangeText={(v) => setForm((f) => ({ ...f, gstin: v }))} />
            <Input label="ADDRESS" value={form.raw_address} onChangeText={(v) => setForm((f) => ({ ...f, raw_address: v }))} />
            <Input label="CITY" value={form.city} onChangeText={(v) => setForm((f) => ({ ...f, city: v }))} />
            <Input label="STATE" value={form.state} onChangeText={(v) => setForm((f) => ({ ...f, state: v }))} />
            <Input label="PINCODE" value={form.pincode} onChangeText={(v) => setForm((f) => ({ ...f, pincode: v }))} />
            <Button title="Save Changes" onPress={handleSaveEdits} loading={saving} size="lg" style={{ marginTop: spacing.sm }} />
            <Button title="Cancel" variant="outline" onPress={() => setEditing(false)} size="lg" style={{ marginTop: spacing.sm }} />
          </Card>
        ) : viewMode === 'digital' ? (
          <Card style={styles.digitalCard}>
            <View style={styles.digitalAccent} />
            <View style={styles.digitalHeader}>
              <View style={styles.digitalAvatar}>
                <Text style={styles.digitalAvatarText}>
                  {(liveCard.person_name || liveCard.personName || liveCard.company || '?').charAt(0).toUpperCase()}
                </Text>
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.digitalName}>{liveCard.person_name || liveCard.personName}</Text>
                {liveCard.designation ? <Text style={styles.digitalRole}>{liveCard.designation}</Text> : null}
              </View>
              {gstin ? (
                <View style={styles.digitalGstBadge}>
                  <ShieldCheck size={12} color={colors.gold} />
                  <Text style={styles.digitalGstBadgeText}>GST</Text>
                </View>
              ) : null}
            </View>

            <View style={styles.digitalCompanyRow}>
              <Building2 size={14} color={colors.primary} />
              <Text style={styles.digitalCompany}>{liveCard.company}</Text>
            </View>
            {gstin ? <Text style={styles.digitalGstLine}>GSTIN: {gstin}</Text> : null}

            <View style={styles.digitalDivider} />

            {phone ? (
              <View style={styles.digitalRow}>
                <View style={styles.digitalIconChip}><Phone size={14} color={colors.primary} /></View>
                <Text style={styles.digitalText}>{phone}</Text>
              </View>
            ) : null}
            {email ? (
              <View style={styles.digitalRow}>
                <View style={styles.digitalIconChip}><Mail size={14} color={colors.primary} /></View>
                <Text style={styles.digitalText}>{email}</Text>
              </View>
            ) : null}
            {address ? (
              <View style={styles.digitalRow}>
                <View style={styles.digitalIconChip}><MapPin size={14} color={colors.primary} /></View>
                <Text style={styles.digitalText}>{address}</Text>
              </View>
            ) : null}
          </Card>
        ) : (
          <View style={styles.originalWrap}>
            {(frontUrl || backUrl) ? (
              <View style={styles.sideToggle}>
                <TouchableOpacity
                  style={[styles.sideBtn, originalSide === 'front' && styles.sideBtnActive]}
                  onPress={() => setOriginalSide('front')}
                >
                  <Text style={[styles.sideBtnText, originalSide === 'front' && styles.sideBtnTextActive]}>Front</Text>
                </TouchableOpacity>
                <TouchableOpacity
                  style={[styles.sideBtn, originalSide === 'back' && styles.sideBtnActive]}
                  onPress={() => setOriginalSide('back')}
                >
                  <Text style={[styles.sideBtnText, originalSide === 'back' && styles.sideBtnTextActive]}>Back</Text>
                </TouchableOpacity>
              </View>
            ) : null}
            {imageLoading ? (
              <View style={styles.noImage}><Text style={styles.noImageText}>Loading original card…</Text></View>
            ) : originalUrl ? (
              <img src={originalUrl} alt="Original business card" style={styles.originalImg} />
            ) : (
              <View style={styles.noImage}>
                <ImageIcon size={32} color={colors.textMuted} />
                <Text style={styles.noImageTitle}>No original card yet</Text>
                <Text style={styles.noImageText}>Add the physical card image for this contact.</Text>
                <Button
                  title={uploading ? 'Uploading…' : `+ Add ${originalSide === 'back' ? 'Back' : 'Front'} Image`}
                  variant="outline"
                  size="sm"
                  loading={uploading}
                  onPress={() => { replaceSideRef.current = originalSide; fileInputRef.current && fileInputRef.current.click(); }}
                  style={{ marginTop: spacing.md }}
                />
              </View>
            )}
          </View>
        )}

        <input
          type="file"
          accept="image/*"
          ref={fileInputRef}
          style={{ display: 'none' }}
          onChange={(e) => {
            if (e.target.files?.[0]) handleUploadOriginal(e.target.files[0], replaceSideRef.current);
            e.target.value = '';
          }}
        />

        {!editing ? <CardCrmPanel card={liveCard} onOpenRecord={onOpenRecord} /> : null}

        {!editing ? (
          <>
            <View style={styles.actionsRow}>
              {phone ? (
                <TouchableOpacity style={styles.actionBtn} onPress={() => window.open(`tel:${phone}`)}>
                  <Phone size={18} color={colors.primary} />
                  <Text style={styles.actionLabel}>Call</Text>
                </TouchableOpacity>
              ) : null}
              {showWhatsApp ? (
                <TouchableOpacity
                  style={styles.actionBtn}
                  onPress={() => window.open(`https://wa.me/${whatsappNumber(liveCard, phone)}`)}
                >
                  <MessageSquare size={18} color="#059669" />
                  <Text style={styles.actionLabel}>WhatsApp</Text>
                </TouchableOpacity>
              ) : null}
              <TouchableOpacity style={styles.actionBtn} onPress={saveContact} disabled={savingContact}>
                <UserPlus size={18} color={colors.primary} />
                <Text style={styles.actionLabel}>
                  {isNativePlatform()
                    ? (savingContact ? 'Adding…' : 'Add to Phone Contacts')
                    : 'Download Contact Card'}
                </Text>
              </TouchableOpacity>
              {address ? (
                <TouchableOpacity style={styles.actionBtn} onPress={openMaps}>
                  <MapPin size={18} color={colors.primary} />
                  <Text style={styles.actionLabel}>Maps</Text>
                </TouchableOpacity>
              ) : null}
              <TouchableOpacity style={styles.actionBtn} onPress={handleShare}>
                <Share2 size={18} color={colors.primary} />
                <Text style={styles.actionLabel}>Share</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.actionBtn} onPress={() => setConfirmRemove(true)}>
                <Trash2 size={18} color={colors.danger} />
                <Text style={[styles.actionLabel, { color: colors.danger }]}>Remove</Text>
              </TouchableOpacity>
            </View>

            <Card style={styles.detailsCard}>
              <Text style={styles.detailsTitle}>Business Details</Text>
              {address ? (
                <View style={styles.detailBlock}>
                  <Text style={styles.detailLabel}>Address</Text>
                  <Text style={styles.detailValue}>{address}</Text>
                </View>
              ) : null}
              {email ? (
                <View style={styles.detailBlock}>
                  <Text style={styles.detailLabel}>Email</Text>
                  <TouchableOpacity onPress={() => window.open(`mailto:${email}`)}>
                    <Text style={[styles.detailValue, { color: colors.primary }]}>{email}</Text>
                  </TouchableOpacity>
                </View>
              ) : null}
              {liveCard.website ? (
                <View style={styles.detailBlock}>
                  <Text style={styles.detailLabel}>Website</Text>
                  <TouchableOpacity onPress={() => window.open(liveCard.website.startsWith('http') ? liveCard.website : `https://${liveCard.website}`)}>
                    <Text style={[styles.detailValue, { color: colors.primary }]}>{liveCard.website}</Text>
                  </TouchableOpacity>
                </View>
              ) : null}
            </Card>
          </>
        ) : null}
      </ScrollView>

      <ConfirmDialog
        visible={confirmRemove}
        title="Remove this card?"
        message={`"${liveCard.person_name || liveCard.company || 'This contact'}" will be removed from My Cards. This can't be undone.`}
        confirmLabel={removing ? 'Removing…' : 'Remove'}
        cancelLabel="Cancel"
        onCancel={() => setConfirmRemove(false)}
        onConfirm={handleRemove}
      />
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
  savedCountRow: {
    flexDirection: 'row',
    alignItems: 'center',
    alignSelf: 'flex-start',
    gap: 6,
    backgroundColor: colors.primaryLight,
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: radii.pill,
    marginBottom: spacing.md
  },
  savedCountText: { fontSize: 13, fontWeight: '600', color: colors.primary },
  wrapper: { flex: 1, backgroundColor: colors.bgMuted },
  container: { flex: 1, backgroundColor: colors.bgMuted },
  content: { padding: spacing.lg, paddingBottom: spacing.xxxl },
  editHit: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  noImageTitle: { color: '#FFFFFF', fontWeight: '700', fontSize: 14, marginTop: spacing.sm },
  digitalCard: { padding: spacing.lg, marginBottom: spacing.md, overflow: 'hidden', borderTopWidth: 4, borderTopColor: colors.primary },
  digitalAccent: { position: 'absolute', top: 0, right: 0, width: 96, height: 96, backgroundColor: colors.primaryLight, borderBottomLeftRadius: 96 },
  digitalHeader: { flexDirection: 'row', alignItems: 'center' },
  digitalAvatar: {
    width: 48,
    height: 48,
    borderRadius: radii.pill,
    backgroundColor: colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: spacing.md
  },
  digitalAvatarText: { fontSize: 18, fontWeight: '700', color: '#FFFFFF' },
  digitalGstBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: colors.goldLight,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: radii.pill
  },
  digitalGstBadgeText: { fontSize: 10, fontWeight: '700', color: colors.gold, letterSpacing: 0.4 },
  digitalName: { fontSize: 19, fontWeight: '800', color: colors.textPrimary },
  digitalRole: { fontSize: 13, color: colors.textSecondary, marginTop: 2 },
  digitalCompanyRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: spacing.md },
  digitalCompany: { fontSize: 15, fontWeight: '700', color: colors.primary },
  digitalGstLine: { fontSize: 11, fontWeight: '600', color: colors.textMuted, marginTop: 4 },
  digitalDivider: { height: 1, backgroundColor: colors.border, marginVertical: spacing.md },
  digitalRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: spacing.sm },
  digitalIconChip: {
    width: 30,
    height: 30,
    borderRadius: radii.pill,
    backgroundColor: colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center'
  },
  digitalText: { fontSize: 13, color: colors.textSecondary, flex: 1 },
  originalWrap: {
    backgroundColor: '#0F172A', borderRadius: radii.lg, padding: spacing.md,
    marginBottom: spacing.md, alignItems: 'center', minHeight: 220
  },
  originalImg: { maxWidth: '100%', maxHeight: 360, objectFit: 'contain', borderRadius: radii.md },
  noImage: { alignItems: 'center', justifyContent: 'center', padding: spacing.xl },
  noImageText: { color: '#A8A3B3', marginTop: 4, fontSize: 12, textAlign: 'center' },
  sideToggle: { flexDirection: 'row', gap: 8, marginBottom: spacing.md },
  sideBtn: { paddingHorizontal: 16, paddingVertical: 6, borderRadius: radii.pill, backgroundColor: 'rgba(255,255,255,0.12)' },
  sideBtnActive: { backgroundColor: colors.bgCard },
  sideBtnText: { fontSize: 12, fontWeight: '700', color: '#FFFFFF' },
  sideBtnTextActive: { color: colors.primary },
  actionsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm, marginBottom: spacing.md },
  actionBtn: {
    alignItems: 'center', justifyContent: 'center', backgroundColor: colors.bgCard,
    borderWidth: 1, borderColor: colors.border, borderRadius: radii.md,
    paddingVertical: spacing.sm, paddingHorizontal: spacing.md, minWidth: 72
  },
  actionLabel: { fontSize: 10, fontWeight: '700', color: colors.textSecondary, marginTop: 4 },
  detailsCard: { padding: spacing.lg },
  formCard: { padding: spacing.lg, marginBottom: spacing.md },
  detailsTitle: { fontSize: 14, fontWeight: '800', color: colors.textPrimary, marginBottom: spacing.md },
  editHint: { fontSize: 11, fontWeight: '700', color: colors.textMuted, letterSpacing: 0.6, marginBottom: spacing.sm, textTransform: 'uppercase' },
  replaceRow: { flexDirection: 'row', gap: spacing.md, marginBottom: spacing.md },
  replaceImg: { width: '100%', height: 80, objectFit: 'contain', backgroundColor: '#1A1228', borderRadius: 10 },
  detailBlock: { marginBottom: spacing.sm },
  detailLabel: { fontSize: 11, fontWeight: '700', color: colors.textMuted, textTransform: 'uppercase', marginBottom: 2 },
  detailValue: { fontSize: 14, color: colors.textPrimary, lineHeight: 20 }
});
