import React, { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Modal } from 'react-native';
import {
  Phone,
  MessageSquare,
  Navigation,
  Share2,
  Mail,
  ShieldCheck,
  Send,
  X,
  CheckCircle2,
  QrCode,
  BookmarkCheck,
  Bookmark,
  Upload,
  Palette,
  Download,
  UserPlus
} from 'lucide-react';
import { colors, radii, spacing, typography } from '../../theme';
import { Card } from '../../components/Card';
import { Button } from '../../components/Button';
import { DetailScreenHeader } from '../../components/DetailScreenHeader';
import { CardViewToggle } from '../../components/CardViewToggle';
import { BusinessCardPreview } from '../../components/BusinessCardTemplates';
import { CardStyleModal } from '../../components/CardStyleModal';
import { Snackbar } from '../../components/Snackbar';
import { useCrm } from '../../context/CrmContext';
import { ListingLeadSheet } from '../crm/ListingLeadSheet';
import { useAuth } from '../../context/AuthContext';
import { fetchCardOriginalImageUrl } from '../../services/api';
import { getCardTemplate } from '../../utils/cardTemplateStorage';
import { downloadCardAs } from '../../utils/cardDownload';

export function BusinessDetailsScreen({ business, onBack, onHome, onShowQr, onBusinessUpdated, onOpenRecord }) {
  // Someone else's listing can become a lead in the business you have open.
  const crm = useCrm();
  const [leadSheet, setLeadSheet] = useState(false);
  const { user, token, myBusinesses, updateMyBusiness, isBusinessSaved, saveBusinessToVault, unsaveBusinessFromVault } = useAuth();
  const [snackbar, setSnackbar] = useState({ visible: false, message: '', type: 'success' });
  const [showEnquiryModal, setShowEnquiryModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [viewMode, setViewMode] = useState('digital');
  const [originalSide, setOriginalSide] = useState('front');
  const [enquiryMessage, setEnquiryMessage] = useState('');
  const [sharePhone, setSharePhone] = useState(true);
  const [enquirySent, setEnquirySent] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [savingEdit, setSavingEdit] = useState(false);
  const [frontUrl, setFrontUrl] = useState(null);
  const [backUrl, setBackUrl] = useState(null);
  const [editForm, setEditForm] = useState(null);
  const [cardTemplateId, setCardTemplateId] = useState(() => getCardTemplate(business?.id));
  const [showStyleModal, setShowStyleModal] = useState(false);
  const [showDownloadOptions, setShowDownloadOptions] = useState(false);
  const [downloadingFormat, setDownloadingFormat] = useState(null);
  const frontEditRef = React.useRef(null);
  const backEditRef = React.useRef(null);
  const cardCaptureRef = React.useRef(null);

  useEffect(() => {
    setCardTemplateId(getCardTemplate(business?.id));
  }, [business?.id]);

  const handleDownloadCard = async (format) => {
    setDownloadingFormat(format);
    try {
      await downloadCardAs(cardCaptureRef.current, format, (business.name || 'business-card').replace(/\s+/g, '-').toLowerCase());
    } catch (e) {
      alert(e.message || 'Could not download card.');
    } finally {
      setDownloadingFormat(null);
      setShowDownloadOptions(false);
    }
  };

  useEffect(() => {
    let blobs = [];
    const load = async () => {
      const frontPath = business?.card_image_url || business?.cardImageUrl;
      const backPath = business?.card_back_image_url || business?.cardBackImageUrl;
      if (frontPath?.startsWith('data:')) setFrontUrl(frontPath);
      else if (frontPath && token) {
        const u = await fetchCardOriginalImageUrl(frontPath, token);
        if (u) blobs.push(u);
        setFrontUrl(u);
      }
      if (backPath?.startsWith('data:')) setBackUrl(backPath);
      else if (backPath && token) {
        const u = await fetchCardOriginalImageUrl(backPath, token);
        if (u) blobs.push(u);
        setBackUrl(u);
      }
    };
    load();
    return () => blobs.forEach((u) => u?.startsWith?.('blob:') && URL.revokeObjectURL(u));
  }, [business?.id, business?.card_image_url, business?.card_back_image_url, token]);

  if (!business) return null;

  const isOwner = (myBusinesses || []).some(
    (b) => String(b.id) === String(business.id) || (b.slug && b.slug === business.slug)
  ) || business.owner_user_id === user?.id || business.ownerPhone === user?.phone;

  const hasOriginalPath = !!(
    business?.card_image_url ||
    business?.cardImageUrl ||
    business?.card_back_image_url ||
    business?.cardBackImageUrl
  );
  const hasOriginal = !!(frontUrl || backUrl || hasOriginalPath);
  const cardImageUrl = originalSide === 'back' ? backUrl : frontUrl;
  const whatsappNumber = business.whatsapp || business.phone;
  const showWhatsApp = !!(business.whatsapp || business.hasWhatsApp);
  const isSaved = isBusinessSaved(business);

  const handleSaveToVault = async () => {
    if (isSaving) return;
    setIsSaving(true);
    try {
      if (isSaved) {
        await unsaveBusinessFromVault(business);
        setSnackbar({ visible: true, message: `Removed ${business.name || 'business'} from My Cards.`, type: 'success' });
      } else {
        await saveBusinessToVault(business);
        setSnackbar({ visible: true, message: `Saved ${business.name || 'business'} to My Cards.`, type: 'success' });
      }
    } catch (e) {
      setSnackbar({ visible: true, message: e?.message || "Couldn't update this business.", type: 'error' });
    } finally {
      setIsSaving(false);
    }
  };

  const handleSendEnquiry = () => {
    if (!enquiryMessage.trim()) return;
    setEnquirySent(true);
    setTimeout(() => {
      setEnquirySent(false);
      setShowEnquiryModal(false);
      setEnquiryMessage('');
    }, 1500);
  };

  const handleShare = () => {
    const shareUrl = `https://cardflow.app/b/${business.slug}`;
    if (navigator.share) {
      navigator.share({
        title: business.name,
        text: `Check out ${business.name} on CardFlow:`,
        url: shareUrl
      });
    } else {
      navigator.clipboard.writeText(shareUrl);
      alert(`Public link copied to clipboard: ${shareUrl}`);
    }
  };

  const openEdit = () => {
    setEditForm({
      business_name: business.name || '',
      category: business.category || business.primary_category || '',
      phone: business.phone || business.phones?.[0] || '',
      whatsapp: business.whatsapp || '',
      email: business.email || '',
      website: business.website || '',
      gstin: business.gstin || '',
      address: business.address || business.address_line1 || '',
      city: business.city || '',
      state: business.state || '',
      pincode: business.pincode || '',
      description: business.description || '',
      services: Array.isArray(business.services) ? business.services.join(', ') : (business.services || ''),
      front_image_data: '',
      back_image_data: ''
    });
    setShowEditModal(true);
  };

  const readEditImage = (file, key) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => setEditForm((f) => ({ ...f, [key]: ev.target.result }));
    reader.readAsDataURL(file);
  };

  const handleSaveEdit = async () => {
    if (!editForm?.business_name?.trim()) {
      alert('Business name is required.');
      return;
    }
    setSavingEdit(true);
    try {
      const payload = {
        ...editForm,
        services: editForm.services
          ? editForm.services.split(',').map((s) => s.trim()).filter(Boolean)
          : []
      };
      const next = await updateMyBusiness(business.id, payload);
      onBusinessUpdated?.({ ...business, ...next, ...payload, name: payload.business_name });
      setShowEditModal(false);
      if (payload.front_image_data) setFrontUrl(payload.front_image_data);
      if (payload.back_image_data) setBackUrl(payload.back_image_data);
      setShowStyleModal(true);
    } catch (e) {
      alert(e.message || 'Could not update business');
    } finally {
      setSavingEdit(false);
    }
  };

  return (
    <View style={styles.container}>
      <DetailScreenHeader
        title={business.name}
        subtitle={business.category || business.primary_category}
        onBack={onBack}
        onHome={onHome}
        rightAction={
          isOwner ? (
            <TouchableOpacity
              onPress={openEdit}
              style={{ paddingHorizontal: 4, height: 44, justifyContent: 'center' }}
            >
              <Text style={{ color: colors.primary, fontWeight: '700', fontSize: 13 }}>Edit</Text>
            </TouchableOpacity>
          ) : null
        }
      />

      <ScrollView contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
        <CardViewToggle
          value={viewMode}
          onChange={setViewMode}
          disabledIds={!hasOriginal && !isOwner ? ['original'] : []}
        />

        {viewMode === 'original' ? (
          <View style={[styles.originalCardWrap, !hasOriginal && styles.originalEmptyWrap]}>
            {hasOriginal ? (
              <>
                <View style={{ flexDirection: 'row', gap: 8, marginBottom: 12 }}>
                  <TouchableOpacity onPress={() => setOriginalSide('front')} style={{ paddingHorizontal: 14, paddingVertical: 6, borderRadius: 999, backgroundColor: originalSide === 'front' ? '#FFFFFF' : 'rgba(255,255,255,0.15)' }}>
                    <Text style={{ fontSize: 12, fontWeight: '700', color: originalSide === 'front' ? colors.primary : '#FFFFFF' }}>Front</Text>
                  </TouchableOpacity>
                  <TouchableOpacity onPress={() => setOriginalSide('back')} style={{ paddingHorizontal: 14, paddingVertical: 6, borderRadius: 999, backgroundColor: originalSide === 'back' ? '#FFFFFF' : 'rgba(255,255,255,0.15)' }}>
                    <Text style={{ fontSize: 12, fontWeight: '700', color: originalSide === 'back' ? colors.primary : '#FFFFFF' }}>Back</Text>
                  </TouchableOpacity>
                </View>
                {cardImageUrl ? (
                  <img src={cardImageUrl} alt={`${business.name} business card`} style={styles.originalCardImg} />
                ) : (
                  <Text style={styles.originalEmptyText}>This side is not available.</Text>
                )}
              </>
            ) : (
              <View style={{ alignItems: 'center', padding: spacing.lg }}>
                <Text style={styles.originalEmptyTitle}>Original card unavailable</Text>
                <Text style={styles.originalEmptyText}>
                  {isOwner
                    ? 'Upload your physical business card so visitors can see it.'
                    : "This business hasn't uploaded an original card yet."}
                </Text>
                {isOwner ? (
                  <Button
                    title="+ Add Card Image"
                    size="sm"
                    onPress={openEdit}
                    style={{ marginTop: spacing.md }}
                  />
                ) : null}
              </View>
            )}
          </View>
        ) : null}

        {viewMode === 'digital' ? (
          <>
            <View style={{ position: 'relative' }}>
              <BusinessCardPreview ref={cardCaptureRef} business={business} templateId={cardTemplateId} />
            </View>

            {isOwner ? (
              <View style={styles.cardStyleActionsRow}>
                <TouchableOpacity style={styles.cardStyleAction} onPress={() => setShowStyleModal(true)}>
                  <Palette size={14} color={colors.primary} />
                  <Text style={styles.cardStyleActionText}>Change Style</Text>
                </TouchableOpacity>
                <TouchableOpacity style={styles.cardStyleAction} onPress={() => setShowDownloadOptions((v) => !v)}>
                  <Download size={14} color={colors.primary} />
                  <Text style={styles.cardStyleActionText}>Download Card</Text>
                </TouchableOpacity>
              </View>
            ) : null}

            {showDownloadOptions ? (
              <View style={styles.downloadRow}>
                {['png', 'jpeg', 'pdf'].map((fmt) => (
                  <Button
                    key={fmt}
                    title={downloadingFormat === fmt ? 'Downloading…' : fmt.toUpperCase()}
                    variant="outline"
                    size="sm"
                    loading={downloadingFormat === fmt}
                    disabled={!!downloadingFormat}
                    onPress={() => handleDownloadCard(fmt)}
                    style={{ flex: 1 }}
                  />
                ))}
              </View>
            ) : null}
          </>
        ) : null}

        <Card style={styles.profileHeaderCard}>
          {/* Action Row */}
          <View style={styles.actionRow}>
            <TouchableOpacity style={styles.actionCircleBtn} onPress={() => window.open(`tel:${business.phone}`)}>
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                <Phone size={18} color={colors.primary} />
              </View>
              <Text style={styles.actionLabel}>Call</Text>
            </TouchableOpacity>

            {showWhatsApp ? (
            <TouchableOpacity style={styles.actionCircleBtn} onPress={() => window.open(`https://wa.me/${whatsappNumber.replace(/[^0-9+]/g, '')}`)}>
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                <MessageSquare size={18} color={colors.primary} />
              </View>
              <Text style={styles.actionLabel}>WhatsApp</Text>
            </TouchableOpacity>
            ) : null}

            <TouchableOpacity
              style={styles.actionCircleBtn}
              onPress={handleSaveToVault}
              disabled={isSaving}
            >
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                {isSaved ? (
                  <BookmarkCheck size={18} color={colors.primary} />
                ) : (
                  <Bookmark size={18} color={colors.primary} />
                )}
              </View>
              <Text style={styles.actionLabel}>
                {isSaved ? 'Saved' : 'Save'}
              </Text>
            </TouchableOpacity>

            {!isOwner && crm.status === 'ready' && crm.active && crm.can('leads', 'create') ? (
            <TouchableOpacity style={styles.actionCircleBtn} onPress={() => setLeadSheet(true)} accessibilityLabel="Add this business to your CRM as a lead">
              <View style={[styles.actionCircle, { backgroundColor: colors.primary }]}>
                <UserPlus size={18} color="#FFFFFF" />
              </View>
              <Text style={styles.actionLabel}>New lead</Text>
            </TouchableOpacity>
            ) : null}

            <TouchableOpacity style={styles.actionCircleBtn} onPress={() => setShowEnquiryModal(true)}>
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                <Mail size={18} color={colors.primary} />
              </View>
              <Text style={styles.actionLabel}>Enquire</Text>
            </TouchableOpacity>

            <TouchableOpacity
              style={styles.actionCircleBtn}
              onPress={() => window.open(`https://maps.google.com/?q=${encodeURIComponent(business.address)}`)}
            >
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                <Navigation size={18} color={colors.primary} />
              </View>
              <Text style={styles.actionLabel}>Directions</Text>
            </TouchableOpacity>

            <TouchableOpacity style={styles.actionCircleBtn} onPress={handleShare}>
              <View style={[styles.actionCircle, { backgroundColor: colors.primaryLight }]}>
                <Share2 size={18} color={colors.primary} />
              </View>
              <Text style={styles.actionLabel}>Share</Text>
            </TouchableOpacity>
          </View>
        </Card>

        {/* Business owner — compact */}
        <View style={styles.ownerSection}>
          <Text style={styles.ownerLabel}>BUSINESS OWNER</Text>
          <View style={styles.ownerRow}>
            <View style={{ flex: 1 }}>
              <Text style={styles.ownerName}>{business.digitalCard?.ownerName || business.name}</Text>
              <Text style={styles.ownerRole}>{business.digitalCard?.title || 'Proprietor'}</Text>
              {(business.email || business.phone) ? (
                <Text style={styles.ownerMeta}>{[business.phone, business.email].filter(Boolean).join(' · ')}</Text>
              ) : null}
            </View>
            {onShowQr ? (
              <TouchableOpacity onPress={onShowQr} style={styles.qrSoft}>
                <QrCode size={22} color={colors.primary} />
              </TouchableOpacity>
            ) : null}
          </View>
          <Text style={styles.ownerLink}>cardflow.app/b/{business.slug}</Text>
        </View>

        {/* About Section */}
        <Card style={styles.sectionCard}>
          <Text style={styles.sectionTitle}>About Business</Text>
          <Text style={styles.aboutText}>{business.description}</Text>
        </Card>

        {/* Services / Products Section */}
        <Card style={styles.sectionCard}>
          <Text style={styles.sectionTitle}>Products & Services</Text>
          <View style={styles.serviceChipsWrap}>
            {(business.services || []).map((svc, idx) => (
              <View key={idx} style={styles.serviceChip}>
                <Text style={styles.serviceChipText}>{svc}</Text>
              </View>
            ))}
          </View>
        </Card>

        {/* Verification & Trust */}
        {business.verification === 'gst' ? (
        <Card style={styles.sectionCard}>
          <Text style={styles.sectionTitle}>Trust & Verification</Text>
          <View style={styles.trustRow}>
            <ShieldCheck size={20} color={colors.verifiedGst} style={{ marginRight: spacing.sm }} />
            <View>
              <Text style={styles.trustTitle}>GST Verified</Text>
              <Text style={styles.trustSub}>Confirmed by CardFlow verification</Text>
            </View>
          </View>
        </Card>
        ) : null}
      </ScrollView>

      {/* Direct Enquiry Modal Sheet */}
      {showEnquiryModal && (
        <View style={styles.modalOverlay}>
          <View style={styles.modalSheet}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Enquire with {business.name}</Text>
              <TouchableOpacity onPress={() => setShowEnquiryModal(false)}>
                <X size={20} color={colors.textSecondary} />
              </TouchableOpacity>
            </View>

            {enquirySent ? (
              <View style={styles.enquirySuccess}>
                <CheckCircle2 size={44} color={colors.success} style={{ marginBottom: spacing.sm }} />
                <Text style={styles.successTitle}>Enquiry Sent!</Text>
                <Text style={styles.successSub}>The business owner has been notified via WhatsApp and SMS.</Text>
              </View>
            ) : (
              <View style={styles.modalBody}>
                <Text style={styles.inputLabel}>What product or service are you looking for?</Text>
                <TextInput
                  style={styles.textArea}
                  placeholder="e.g. Need quotation for 500 units of custom CNC shafts by next week..."
                  placeholderTextColor={colors.textMuted}
                  multiline
                  numberOfLines={4}
                  value={enquiryMessage}
                  onChangeText={setEnquiryMessage}
                  autoFocus
                />

                <TouchableOpacity
                  style={styles.sharePhoneRow}
                  onPress={() => setSharePhone(!sharePhone)}
                  activeOpacity={0.8}
                >
                  <View style={[styles.checkbox, sharePhone && styles.checkboxChecked]}>
                    {sharePhone && <CheckCircle2 size={14} color="#FFFFFF" />}
                  </View>
                  <Text style={styles.sharePhoneLabel}>Share my verified phone number for faster reply</Text>
                </TouchableOpacity>

                <Button
                  title="Send Direct Business Enquiry"
                  onPress={handleSendEnquiry}
                  icon={Send}
                  size="lg"
                  style={{ marginTop: spacing.md }}
                />
              </View>
            )}
          </View>
        </View>
      )}

      {showEditModal && editForm ? (
        <Modal transparent animationType="slide" visible={showEditModal} onRequestClose={() => setShowEditModal(false)}>
          <View style={styles.editOverlay}>
            <View style={styles.editSheet}>
              <View style={styles.editHeader}>
                <Text style={styles.editTitle}>Edit Business</Text>
                <TouchableOpacity onPress={() => setShowEditModal(false)}>
                  <X size={20} color={colors.textSecondary} />
                </TouchableOpacity>
              </View>
              <ScrollView style={{ maxHeight: 460 }} showsVerticalScrollIndicator={false}>
                {[
                  { key: 'business_name', label: 'Business Name *' },
                  { key: 'category', label: 'Category' },
                  { key: 'phone', label: 'Phone' },
                  { key: 'whatsapp', label: 'WhatsApp' },
                  { key: 'email', label: 'Email' },
                  { key: 'website', label: 'Website' },
                  { key: 'gstin', label: 'GSTIN' },
                  { key: 'address', label: 'Address' },
                  { key: 'city', label: 'City' },
                  { key: 'state', label: 'State' },
                  { key: 'pincode', label: 'Pincode' },
                  { key: 'description', label: 'Description' },
                  { key: 'services', label: 'Products & Services (comma separated)' }
                ].map(({ key, label }) => (
                  <View key={key}>
                    <Text style={styles.inputLabel}>{label}</Text>
                    <TextInput
                      value={editForm[key]}
                      onChangeText={(v) => setEditForm((f) => ({ ...f, [key]: v }))}
                      style={styles.editInput}
                      placeholderTextColor={colors.textMuted}
                    />
                  </View>
                ))}
                <input type="file" accept="image/*" ref={frontEditRef} style={{ display: 'none' }} onChange={(e) => { if (e.target.files?.[0]) readEditImage(e.target.files[0], 'front_image_data'); e.target.value = ''; }} />
                <input type="file" accept="image/*" ref={backEditRef} style={{ display: 'none' }} onChange={(e) => { if (e.target.files?.[0]) readEditImage(e.target.files[0], 'back_image_data'); e.target.value = ''; }} />
                <TouchableOpacity style={styles.uploadRow} onPress={() => frontEditRef.current?.click()}>
                  <Upload size={16} color={colors.primary} />
                  <Text style={styles.uploadLabel}>
                    {editForm.front_image_data || frontUrl ? 'Replace Front Card' : 'Upload Front Card'}
                  </Text>
                </TouchableOpacity>
                <TouchableOpacity style={styles.uploadRow} onPress={() => backEditRef.current?.click()}>
                  <Upload size={16} color={colors.primary} />
                  <Text style={styles.uploadLabel}>
                    {editForm.back_image_data || backUrl ? 'Replace Back Card' : 'Upload Back Card'}
                  </Text>
                </TouchableOpacity>
              </ScrollView>
              <View style={{ flexDirection: 'row', gap: 8, marginTop: spacing.md }}>
                <Button title="Cancel" variant="outline" onPress={() => setShowEditModal(false)} style={{ flex: 1 }} />
                <Button title="Save" onPress={handleSaveEdit} loading={savingEdit} style={{ flex: 1.4 }} />
              </View>
            </View>
          </View>
        </Modal>
      ) : null}

      <CardStyleModal
        visible={showStyleModal}
        business={business}
        onClose={() => setShowStyleModal(false)}
        onSaved={(tpl) => {
          setCardTemplateId(tpl);
          setShowStyleModal(false);
        }}
      />

      <ListingLeadSheet visible={leadSheet} business={business} onClose={() => setLeadSheet(false)} onOpenRecord={onOpenRecord} />

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
  container: {
    flex: 1,
    backgroundColor: colors.bgMuted
  },
  scrollContent: {
    padding: spacing.md,
    paddingBottom: spacing.xxxl
  },
  originalCardWrap: {
    backgroundColor: '#0F172A',
    borderRadius: radii.card,
    padding: spacing.md,
    marginBottom: spacing.md,
    alignItems: 'center'
  },
  originalEmptyWrap: {
    backgroundColor: colors.bgMutedDark,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border
  },
  originalEmptyTitle: {
    color: colors.textPrimary,
    fontWeight: '700',
    fontSize: 14,
    textAlign: 'center'
  },
  originalEmptyText: {
    color: colors.textSecondary,
    fontSize: 12,
    textAlign: 'center',
    marginTop: 6,
    lineHeight: 18,
    maxWidth: 260
  },
  originalCardImg: { maxWidth: '100%', maxHeight: 280, objectFit: 'contain', borderRadius: radii.md },
  ownerSection: {
    backgroundColor: colors.bgCard,
    borderRadius: radii.card,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    padding: spacing.md,
    marginBottom: spacing.md
  },
  ownerLabel: {
    ...typography.caption,
    color: colors.textMuted,
    marginBottom: spacing.sm
  },
  ownerRow: { flexDirection: 'row', alignItems: 'center' },
  ownerName: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  ownerRole: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  ownerMeta: { fontSize: 11, color: colors.textMuted, marginTop: 4 },
  ownerLink: { fontSize: 11, color: colors.primary, fontWeight: '600', marginTop: spacing.sm },
  qrSoft: {
    width: 40,
    height: 40,
    borderRadius: radii.md,
    backgroundColor: colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center'
  },
  editOverlay: {
    flex: 1,
    backgroundColor: 'rgba(15,23,42,0.55)',
    justifyContent: 'center',
    padding: spacing.md
  },
  editSheet: {
    backgroundColor: colors.bgCard,
    borderRadius: radii.modal,
    padding: spacing.lg,
    maxWidth: 520,
    width: '100%',
    alignSelf: 'center'
  },
  editHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md
  },
  editTitle: { ...typography.titleSmall, color: colors.textPrimary },
  editInput: {
    backgroundColor: colors.bgMuted,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    borderRadius: radii.input,
    paddingHorizontal: spacing.md,
    paddingVertical: 8,
    fontSize: 13,
    color: colors.textPrimary,
    marginBottom: spacing.sm,
    outlineStyle: 'none'
  },
  uploadRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingVertical: spacing.sm,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border
  },
  uploadLabel: { fontSize: 13, fontWeight: '600', color: colors.primary },
  profileHeaderCard: {
    padding: spacing.lg,
    marginBottom: spacing.md,
    position: 'relative'
  },
  cardStyleActionsRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.sm,
    marginBottom: spacing.md
  },
  cardStyleAction: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    paddingVertical: 10,
    borderRadius: radii.md,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.bgCard
  },
  cardStyleActionText: { fontSize: 12, fontWeight: '700', color: colors.primary },
  downloadRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginBottom: spacing.md
  },
  actionRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'space-between',
    rowGap: 12
  },
  actionCircleBtn: {
    alignItems: 'center',
    width: 58
  },
  actionCircle: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 4
  },
  actionLabel: {
    fontSize: 11,
    color: colors.textSecondary,
    fontWeight: '500'
  },
  digitalCardPreview: {
    backgroundColor: '#1E293B',
    borderRadius: radii.lg,
    padding: spacing.lg,
    marginBottom: spacing.md,
    borderWidth: 0
  },
  digitalCardTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: spacing.lg
  },
  digitalOwnerName: {
    color: '#FFFFFF',
    fontSize: 18,
    fontWeight: '700'
  },
  digitalOwnerTitle: {
    color: '#94A3B8',
    fontSize: 13,
    marginTop: 2
  },
  digitalBizName: {
    color: colors.gold,
    fontSize: 14,
    fontWeight: '600',
    marginTop: 6
  },
  qrIconWrap: {
    padding: 6,
    backgroundColor: 'rgba(255, 255, 255, 0.1)',
    borderRadius: radii.md
  },
  digitalCardBottom: {
    borderTopWidth: 1,
    borderTopColor: '#334155',
    paddingTop: spacing.sm,
    flexDirection: 'column',
    gap: 4
  },
  digitalCardPhone: {
    color: '#CBD5E1',
    fontSize: 11,
    flex: 1,
    marginRight: spacing.sm
  },
  digitalCardUrl: {
    color: '#E2E8F0',
    fontSize: 11,
    fontWeight: '600'
  },
  sectionCard: {
    padding: spacing.lg,
    marginBottom: spacing.md
  },
  sectionTitle: {
    fontSize: 15,
    fontWeight: '700',
    color: colors.textPrimary,
    marginBottom: spacing.sm
  },
  aboutText: {
    fontSize: 13,
    color: colors.textSecondary,
    lineHeight: 20
  },
  serviceChipsWrap: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs
  },
  serviceChip: {
    backgroundColor: colors.bgMutedDark,
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: radii.chip,
    marginRight: 6,
    marginBottom: 4
  },
  serviceChipText: {
    fontSize: 11,
    color: colors.textMuted,
    fontWeight: '500'
  },
  trustRow: {
    flexDirection: 'row',
    alignItems: 'center'
  },
  trustTitle: {
    fontSize: 13,
    fontWeight: '600',
    color: colors.textPrimary
  },
  trustSub: {
    fontSize: 11,
    color: colors.textMuted,
    marginTop: 1
  },
  modalOverlay: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'flex-end',
    zIndex: 100
  },
  modalSheet: {
    backgroundColor: colors.bgCard,
    borderTopLeftRadius: radii.xl,
    borderTopRightRadius: radii.xl,
    padding: spacing.lg,
    maxHeight: '80%'
  },
  modalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: spacing.md
  },
  modalTitle: {
    fontSize: 16,
    fontWeight: '700',
    color: colors.textPrimary
  },
  modalBody: {},
  inputLabel: {
    fontSize: 13,
    fontWeight: '600',
    color: colors.textPrimary,
    marginBottom: spacing.xs
  },
  textArea: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radii.md,
    padding: spacing.md,
    fontSize: 14,
    color: colors.textPrimary,
    textAlignVertical: 'top',
    minHeight: 100,
    outlineStyle: 'none'
  },
  sharePhoneRow: {
    flexDirection: 'row',
    alignItems: 'center',
    marginTop: spacing.md
  },
  checkbox: {
    width: 20,
    height: 20,
    borderRadius: 4,
    borderWidth: 1.5,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: spacing.sm,
    backgroundColor: colors.bgCard
  },
  checkboxChecked: {
    backgroundColor: colors.primary,
    borderColor: colors.primary
  },
  sharePhoneLabel: {
    fontSize: 12,
    color: colors.textSecondary
  },
  enquirySuccess: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.xl
  },
  successTitle: {
    fontSize: 18,
    fontWeight: '700',
    color: colors.textPrimary,
    marginBottom: spacing.xs
  },
  successSub: {
    fontSize: 13,
    color: colors.textSecondary,
    textAlign: 'center'
  }
});
