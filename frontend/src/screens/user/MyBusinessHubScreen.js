import React, { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Modal, TextInput } from 'react-native';
import { Building2, Plus, Upload, X, ChevronRight, Users } from 'lucide-react';
import { colors, radii, spacing, typography } from '../../theme';
import { Badge } from '../../components/Badge';
import { Button } from '../../components/Button';
import { CardStyleModal } from '../../components/CardStyleModal';
import { UpgradeModal } from '../../components/UpgradeModal';
import { useAuth } from '../../context/AuthContext';
import { useCrm } from '../../context/CrmContext';

const FREE_BUSINESS_LIMIT = 2;

export function MyBusinessHubScreen({ onSelectBusiness, onNewBusiness }) {
  const { myBusinesses, addMyBusiness, isPremiumActive, loadMyBusinesses } = useAuth();
  // A business created in the CRM gets its listing on the server: show it as soon as it exists.
  const { businesses } = useCrm();
  React.useEffect(() => {
    loadMyBusinesses();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [businesses.length]);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showUpgrade, setShowUpgrade] = useState(false);
  const [styleBusiness, setStyleBusiness] = useState(null);
  const [form, setForm] = useState({
    business_name: '', category: 'Manufacturing', phone: '', whatsapp: '',
    email: '', website: '', gstin: '', address: '', area: '',
    city: 'Coimbatore', district: 'Coimbatore', state: 'Tamil Nadu',
    pincode: '', description: '', services: '',
    front_image_data: '', back_image_data: ''
  });
  const [toast, setToast] = useState('');
  const [creating, setCreating] = useState(false);
  const frontInputRef = React.useRef(null);
  const backInputRef = React.useRef(null);

  const emptyForm = {
    business_name: '', category: 'Manufacturing', phone: '', whatsapp: '',
    email: '', website: '', gstin: '', address: '', area: '',
    city: 'Coimbatore', district: 'Coimbatore', state: 'Tamil Nadu',
    pincode: '', description: '', services: '',
    front_image_data: '', back_image_data: ''
  };

  const openAddModal = () => {
    // A new business is created once, in the CRM; its listing and cards come with it.
    if (onNewBusiness) {
      onNewBusiness();
      return;
    }
    if (!isPremiumActive && myBusinesses.length >= FREE_BUSINESS_LIMIT) {
      setShowUpgrade(true);
      return;
    }
    setShowAddModal(true);
  };

  const readImage = (file, key) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => setForm((f) => ({ ...f, [key]: ev.target.result }));
    reader.readAsDataURL(file);
  };

  const handleCreate = async () => {
    if (!form.business_name.trim() || !form.phone.trim() || !form.gstin.trim() || !form.address.trim() || !form.pincode.trim()) {
      alert('Business Name, Phone, GSTIN, Address and Pincode are required.');
      return;
    }
    setCreating(true);
    try {
      const created = await addMyBusiness({
        ...form,
        services: form.services ? form.services.split(',').map((s) => s.trim()).filter(Boolean) : []
      });
      setToast(`"${form.business_name}" added successfully!`);
      setShowAddModal(false);
      setForm(emptyForm);
      setTimeout(() => setToast(''), 3500);
      setStyleBusiness(created);
    } catch (e) {
      alert(e.message || 'Could not create business. Check GSTIN is unique and try again.');
    } finally {
      setCreating(false);
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.scrollContent}>
      <View style={styles.header}>
        <View style={{ flex: 1 }}>
          <Text style={styles.title}>My Business</Text>
          <Text style={styles.subtitle}>
            {myBusinesses.length > 0
              ? `${myBusinesses.length} business${myBusinesses.length > 1 ? 'es' : ''}`
              : 'Your digital card, original card and directory listing'}

          </Text>
        </View>
        <Button title="+ Add" onPress={() => openAddModal()} size="sm" />
      </View>

      {toast ? (
        <View style={styles.toast}><Text style={styles.toastText}>{toast}</Text></View>
      ) : null}

      {myBusinesses.length === 0 ? (
        <View style={styles.emptyInline}>
          <Text style={styles.emptyTitle}>No business yet</Text>
          <Text style={styles.emptySub}>Create your business to get its digital card and a listing in Browse.</Text>
          <Button title="Add Business" onPress={() => openAddModal()} size="sm" style={{ marginTop: spacing.md, alignSelf: 'flex-start' }} />
        </View>
      ) : (
        myBusinesses.map((biz) => (
          <TouchableOpacity
            key={biz.id}
            style={styles.bizRow}
            onPress={() => onSelectBusiness && onSelectBusiness(biz)}
            activeOpacity={0.75}
          >
            <View style={styles.logoWrap}>
              <Building2 size={18} color={colors.primary} />
            </View>
            <View style={{ flex: 1 }}>
              <Text style={styles.bizName}>{biz.name || biz.business_name}</Text>
              <Text style={styles.bizMeta}>{[biz.city, biz.category || biz.primary_category].filter(Boolean).join(' · ')}</Text>
              {biz.verification === 'gst' ? (
                <Badge type="gst" label="GST Verified" style={{ marginTop: 4 }} />
              ) : biz.gstin ? (
                <Badge type="gstPending" label="GST Registered" style={{ marginTop: 4 }} />
              ) : null}
              {biz.saved_count > 0 ? (
                <View style={styles.savedRow}>
                  <Users size={12} color={colors.primary} />
                  <Text style={styles.savedText}>
                    Saved by {biz.saved_count} {biz.saved_count === 1 ? 'person' : 'people'}
                  </Text>
                </View>
              ) : null}
              {biz.source === 'card' && biz.status === 'draft' ? (
                <Text style={styles.claimedHint}>Created from your business card — review the details and publish it.</Text>
              ) : null}
            </View>
            <ChevronRight size={16} color={colors.textMuted} />
          </TouchableOpacity>
        ))
      )}

      {myBusinesses.length > 0 ? (
        <TouchableOpacity style={styles.addAnotherBtn} onPress={() => openAddModal()}>
          <Plus size={14} color={colors.primary} />
          <Text style={styles.addAnotherText}>Add another</Text>
        </TouchableOpacity>
      ) : null}

      {/* Add Business Modal */}
      {showAddModal && (
        <Modal transparent animationType="slide" visible={showAddModal} onRequestClose={() => setShowAddModal(false)}>
          <View style={styles.modalOverlay}>
            <View style={styles.modalCard}>
              <View style={styles.modalHeader}>
                <Text style={styles.modalTitle}>Create Business</Text>
                <TouchableOpacity onPress={() => setShowAddModal(false)}><X size={20} color={colors.textSecondary} /></TouchableOpacity>
              </View>
              <ScrollView style={{ maxHeight: 480 }} showsVerticalScrollIndicator={false}>
                {[
                  { key: 'business_name', label: 'Business Name *', placeholder: 'ABC Traders' },
                  { key: 'category', label: 'Business Category *', placeholder: 'Retail, Manufacturing, IT...' },
                  { key: 'phone', label: 'Phone *', placeholder: '+91 XXXXX XXXXX' },
                  { key: 'whatsapp', label: 'WhatsApp', placeholder: '+91 XXXXX XXXXX' },
                  { key: 'email', label: 'Email', placeholder: 'info@business.com' },
                  { key: 'website', label: 'Website', placeholder: 'www.business.com' },
                  { key: 'gstin', label: 'GSTIN *', placeholder: '33XXXXXXXXXXXXXX' },
                  { key: 'address', label: 'Address *', placeholder: 'Street, Area, Landmark' },
                  { key: 'area', label: 'Area', placeholder: 'Ganapathy, RS Puram...' },
                  { key: 'city', label: 'City *', placeholder: 'Coimbatore' },
                  { key: 'district', label: 'District', placeholder: 'Coimbatore' },
                  { key: 'state', label: 'State', placeholder: 'Tamil Nadu' },
                  { key: 'pincode', label: 'Pincode *', placeholder: '641001' },
                  { key: 'description', label: 'Business Description', placeholder: 'What you do' },
                  { key: 'services', label: 'Products / Services', placeholder: 'Comma separated' }
                ].map(({ key, label, placeholder }) => (
                  <View key={key}>
                    <Text style={styles.fieldLabel}>{label}</Text>
                    <TextInput
                      value={form[key]}
                      onChangeText={(v) => setForm((f) => ({ ...f, [key]: v }))}
                      placeholder={placeholder}
                      placeholderTextColor={colors.textMuted}
                      style={styles.input}
                    />
                  </View>
                ))}
                <input type="file" accept="image/*" ref={frontInputRef} style={{ display: 'none' }} onChange={(e) => { if (e.target.files?.[0]) readImage(e.target.files[0], 'front_image_data'); e.target.value = ''; }} />
                <input type="file" accept="image/*" ref={backInputRef} style={{ display: 'none' }} onChange={(e) => { if (e.target.files?.[0]) readImage(e.target.files[0], 'back_image_data'); e.target.value = ''; }} />
                <TouchableOpacity style={styles.uploadArea} onPress={() => frontInputRef.current && frontInputRef.current.click()}>
                  <Upload size={20} color={colors.primary} />
                  <Text style={styles.uploadText}>{form.front_image_data ? 'Front card selected ✓' : 'Upload Front Card'}</Text>
                </TouchableOpacity>
                <TouchableOpacity style={styles.uploadArea} onPress={() => backInputRef.current && backInputRef.current.click()}>
                  <Upload size={20} color={colors.primary} />
                  <Text style={styles.uploadText}>{form.back_image_data ? 'Back card selected ✓' : 'Upload Back Card'}</Text>
                </TouchableOpacity>
              </ScrollView>
              <View style={styles.modalActions}>
                <Button title="Cancel" variant="outline" onPress={() => setShowAddModal(false)} style={{ flex: 1 }} />
                <Button title="Create Business" onPress={handleCreate} loading={creating} style={{ flex: 1.5 }} />
              </View>
            </View>
          </View>
        </Modal>
      )}

      <CardStyleModal
        visible={!!styleBusiness}
        business={styleBusiness}
        onClose={() => setStyleBusiness(null)}
        onSaved={() => setStyleBusiness(null)}
      />

      <UpgradeModal
        visible={showUpgrade}
        onClose={() => setShowUpgrade(false)}
        title="Business limit reached"
        message="Free plan allows up to 2 businesses. Upgrade to Pro for unlimited business listings."
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  savedRow: { flexDirection: 'row', alignItems: 'center', gap: 4, marginTop: 4 },
  savedText: { fontSize: 12, fontWeight: '600', color: colors.primary },
  claimedHint: { fontSize: 11, color: colors.textSecondary, marginTop: 4 },
  container: { flex: 1, backgroundColor: colors.bgMuted },
  scrollContent: { padding: spacing.lg, paddingBottom: spacing.xxxl },
  header: { flexDirection: 'row', alignItems: 'flex-start', marginBottom: spacing.lg, gap: spacing.sm },
  title: { ...typography.titleMedium, color: colors.textPrimary },
  subtitle: { fontSize: 13, color: colors.textSecondary, marginTop: 2 },
  toast: { backgroundColor: '#D1FAE5', padding: spacing.sm, borderRadius: radii.md, marginBottom: spacing.md },
  toastText: { fontSize: 13, color: '#065F46', fontWeight: '600' },
  emptyInline: { paddingVertical: spacing.md },
  emptyTitle: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  emptySub: { fontSize: 13, color: colors.textSecondary, marginTop: 4, lineHeight: 18 },
  bizRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border
  },
  logoWrap: {
    width: 40, height: 40, borderRadius: radii.md, backgroundColor: colors.primaryLight,
    alignItems: 'center', justifyContent: 'center', marginRight: spacing.md
  },
  bizName: { fontSize: 15, fontWeight: '700', color: colors.textPrimary },
  bizMeta: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  addAnotherBtn: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    paddingVertical: spacing.md, marginTop: spacing.sm, gap: 6
  },
  addAnotherText: { fontSize: 13, fontWeight: '600', color: colors.primary },
  modalOverlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.65)', justifyContent: 'center', padding: spacing.md },
  modalCard: { backgroundColor: colors.bgCard, borderRadius: radii.modal, padding: spacing.lg, maxWidth: 520, width: '100%', alignSelf: 'center' },
  modalHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: spacing.md },
  modalTitle: { ...typography.titleMedium, color: colors.textPrimary },
  fieldLabel: { fontSize: 12, fontWeight: '600', color: colors.textPrimary, marginBottom: 4, marginTop: spacing.xs },
  input: {
    backgroundColor: colors.bgMuted, borderWidth: StyleSheet.hairlineWidth, borderColor: colors.border,
    borderRadius: radii.input, paddingHorizontal: spacing.md, paddingVertical: 8,
    fontSize: 13, color: colors.textPrimary, marginBottom: spacing.xs, outlineStyle: 'none'
  },
  uploadArea: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    borderWidth: StyleSheet.hairlineWidth, borderColor: colors.border, borderStyle: 'dashed',
    borderRadius: radii.md, padding: spacing.md, marginVertical: spacing.sm, gap: 8
  },
  uploadText: { fontSize: 13, fontWeight: '600', color: colors.primary },
  modalActions: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.md }
});
