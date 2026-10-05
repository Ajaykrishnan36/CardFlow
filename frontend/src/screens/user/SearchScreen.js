import React, { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, ActivityIndicator } from 'react-native';
import { Search, SlidersHorizontal, Building2, Phone, MessageSquare, Navigation, Check, BookmarkCheck, BookmarkPlus } from 'lucide-react';
import { colors, radii, spacing, typography, shadows } from '../../theme';
import { Card } from '../../components/Card';
import { Badge } from '../../components/Badge';
import { Button } from '../../components/Button';
import { EmptyState } from '../../components/EmptyState';
import { BrandSpinner, SkeletonCard } from '../../components/Loader';
import { Snackbar } from '../../components/Snackbar';
import { categories as fallbackCategories } from '../../data/mockData';
import { useAuth } from '../../context/AuthContext';
import { apiClient } from '../../services/api';

export function SearchScreen({ onSelectBusiness, initialCategoryId, onBack }) {
  const { isBusinessSaved, saveBusinessToVault, unsaveBusinessFromVault, user, myBusinesses } = useAuth();
  // Browse is for finding other businesses: your own are under Profile → My businesses.
  const mine = new Set((myBusinesses || []).map((b) => String(b.id)));
  const [savingBizId, setSavingBizId] = useState(null);
  const [snackbar, setSnackbar] = useState({ visible: false, message: '', type: 'success' });
  const showSnackbar = (message, type = 'success') => setSnackbar({ visible: true, message, type });

  const handleToggleSave = async (biz, isSaved) => {
    setSavingBizId(biz.id);
    try {
      if (isSaved) {
        await unsaveBusinessFromVault(biz);
        showSnackbar(`Removed ${biz.name || 'business'} from My Cards.`);
      } else {
        await saveBusinessToVault(biz);
        showSnackbar(`Saved ${biz.name || 'business'} to My Cards.`);
      }
    } catch (e) {
      showSnackbar(e?.message || "Couldn't update this business.", 'error');
    } finally {
      setSavingBizId(null);
    }
  };
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCat, setSelectedCat] = useState(initialCategoryId || 'all');
  const [selectedRadius, setSelectedRadius] = useState(10); // km
  const [gstOnly, setGstOnly] = useState(false);
  const [showFilters, setShowFilters] = useState(false);
  const [isSearching, setIsSearching] = useState(true);
  const [liveCategories, setLiveCategories] = useState(fallbackCategories);
  const [businesses, setBusinesses] = useState([]);

  const radiusOptions = [2, 5, 10, 25];

  // Fetch dynamic categories and businesses from API
  const loadDirectoryData = useCallback(async () => {
    setIsSearching(true);
    try {
      const [catsRes, bizRes] = await Promise.all([
        apiClient.getCategories(),
        apiClient.searchBusinesses({ q: searchQuery, category: selectedCat !== 'all' ? selectedCat : '', radius: selectedRadius })
      ]);

      if (catsRes && Array.isArray(catsRes) && catsRes.length > 0) {
        setLiveCategories(catsRes);
      }

      let fetchedList = (bizRes && Array.isArray(bizRes)) ? bizRes : [];

      setBusinesses(fetchedList);
    } catch (e) {
      console.warn('Error fetching directory:', e);
    } finally {
      setIsSearching(false);
    }
  }, [searchQuery, selectedCat, selectedRadius]);

  useEffect(() => {
    loadDirectoryData();
  }, [loadDirectoryData]);

  const filteredBusinesses = businesses.filter((biz) => {
    if (mine.has(String(biz.id)) || (user?.id && String(biz.owner_user_id) === String(user.id))) return false;
    if (selectedCat !== 'all' && biz.categoryId !== selectedCat && biz.category?.toLowerCase() !== selectedCat.toLowerCase()) return false;
    if (gstOnly && biz.verification !== 'gst') return false;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      const matchName = (biz.name || '').toLowerCase().includes(q);
      const matchDesc = (biz.description || '').toLowerCase().includes(q);
      const matchCat = (biz.category || '').toLowerCase().includes(q);
      const matchPincode = (biz.pincode || '').includes(q);
      const matchServices = Array.isArray(biz.services) && biz.services.some((s) => s.toLowerCase().includes(q));
      if (!matchName && !matchDesc && !matchCat && !matchPincode && !matchServices) {
        return false;
      }
    }
    return true;
  });

  return (
    <View style={styles.container}>
      <View style={styles.pageHead}>
        <Text style={styles.pageTitle}>Browse Businesses</Text>
        <Text style={styles.pageSub}>Discover businesses around you</Text>
      </View>
      {/* Search Input Bar */}
      <View style={styles.topBar}>
        <View style={styles.searchInputWrap}>
          <Search size={18} color={colors.textSecondary} style={{ marginRight: spacing.sm }} />
          <TextInput
            value={searchQuery}
            onChangeText={setSearchQuery}
            placeholder="Search businesses, services, pincodes..."
            placeholderTextColor={colors.textMuted}
            style={styles.input}
            autoFocus={false}
          />
        </View>
        <TouchableOpacity
          style={[styles.filterToggleBtn, showFilters && styles.filterToggleActive]}
          onPress={() => setShowFilters(!showFilters)}
          activeOpacity={0.8}
        >
          <SlidersHorizontal size={18} color={showFilters ? '#FFFFFF' : colors.textPrimary} />
        </TouchableOpacity>
      </View>

      {/* Expandable Filter Panel */}
      {showFilters && (
        <View style={styles.filterPanel}>
          <Text style={styles.filterSectionTitle}>Radius Distance</Text>
          <View style={styles.radiusRow}>
            {radiusOptions.map((r) => (
              <TouchableOpacity
                key={r}
                style={[styles.radiusBtn, selectedRadius === r && styles.radiusBtnActive]}
                onPress={() => setSelectedRadius(r)}
              >
                <Text style={[styles.radiusText, selectedRadius === r && styles.radiusTextActive]}>
                  {r} km
                </Text>
              </TouchableOpacity>
            ))}
          </View>

          <View style={styles.gstToggleRow}>
            <TouchableOpacity
              style={[styles.checkbox, gstOnly && styles.checkboxChecked]}
              onPress={() => setGstOnly(!gstOnly)}
            >
              {gstOnly && <Check size={14} color="#FFFFFF" />}
            </TouchableOpacity>
            <Text style={styles.gstToggleLabel}>GST Verified Businesses Only</Text>
          </View>
        </View>
      )}

      {/* Search Results List or Animated Skeletons */}
      <ScrollView contentContainerStyle={styles.resultsScroll} showsVerticalScrollIndicator={false}>
        <View style={styles.resultsMetaRow}>
          <Text style={styles.resultsCount}>
            {isSearching ? 'Searching…' : `${filteredBusinesses.length} businesses found`}
          </Text>
        </View>

        {isSearching ? (
          <View style={styles.loadingWrapper}>
            <BrandSpinner size={28} text="Loading directory…" />
            <SkeletonCard count={2} />
          </View>
        ) : filteredBusinesses.length === 0 ? (
          <EmptyState
            icon={Building2}
            title="No businesses found"
            description="Try another search, radius or category. Your own businesses are under Profile → My Businesses."
            actionTitle="Reset Filters"
            onAction={() => {
              setSearchQuery('');
              setSelectedCat('all');
              setGstOnly(false);
            }}
            compact
          />
        ) : (
          filteredBusinesses.map((biz) => {
            const isSaved = isBusinessSaved(biz);
            return (
              <Card key={biz.id} onPress={() => onSelectBusiness(biz)} style={styles.bizCard}>
                {/* Top Right SAVED Chip if already in user vault */}
                {isSaved && (
                  <View style={styles.savedChip}>
                    <BookmarkCheck size={13} color="#059669" style={{ marginRight: 4 }} />
                    <Text style={styles.savedChipText}>SAVED</Text>
                  </View>
                )}

                <View style={styles.bizHeader}>
                  <View style={styles.bizLogo}>
                    <Building2 size={24} color={colors.primary} />
                  </View>
                  <View style={{ flex: 1, paddingRight: isSaved ? 70 : 0 }}>
                    <Text style={styles.bizName}>{biz.name || biz.business_name}</Text>
                    <Text style={styles.bizCat}>
                      {[biz.category || biz.primary_category, biz.distanceKm ? `${biz.distanceKm} km` : null].filter(Boolean).join(' • ')}
                    </Text>
                    <View style={{ flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', marginTop: 2, gap: 6 }}>
                      {biz.verification === 'gst' ? <Badge type="gst" label="GST Verified" /> : null}
                      {biz.pincode ? <Text style={styles.pincode}>Pin: {biz.pincode}</Text> : null}
                    </View>
                  </View>
                </View>

                {biz.description ? <Text style={styles.bizDesc} numberOfLines={2}>{biz.description}</Text> : null}

                {(biz.services || []).length > 0 ? (
                <View style={styles.serviceChipsRow}>
                  {(biz.services || []).slice(0, 3).map((s, idx) => (
                    <View key={idx} style={styles.serviceChip}>
                      <Text style={styles.serviceText}>{s}</Text>
                    </View>
                  ))}
                </View>
                ) : null}

                {/* Action Buttons */}
                <View style={styles.bizActions}>
                  {biz.phone ? (
                  <TouchableOpacity style={styles.btnAction} onPress={() => window.open(`tel:${biz.phone}`)}>
                    <Phone size={14} color={colors.primary} />
                    <Text style={styles.btnActionText}>Call</Text>
                  </TouchableOpacity>
                  ) : null}

                  {(biz.whatsapp || biz.hasWhatsApp) && (biz.whatsapp || biz.phone) ? (
                  <TouchableOpacity
                    style={[styles.btnAction, { backgroundColor: '#ECFDF5' }]}
                    onPress={() => window.open(`https://wa.me/${(biz.whatsapp || biz.phone || '').replace(/[^0-9]/g, '')}`)}
                  >
                    <MessageSquare size={14} color="#059669" />
                    <Text style={[styles.btnActionText, { color: '#059669' }]}>WhatsApp</Text>
                  </TouchableOpacity>
                  ) : null}

                  <TouchableOpacity
                    style={styles.btnAction}
                    onPress={() => window.open(`https://maps.google.com/?q=${encodeURIComponent(biz.address || biz.name)}`)}
                  >
                    <Navigation size={14} color={colors.textSecondary} />
                    <Text style={[styles.btnActionText, { color: colors.textSecondary }]}>Directions</Text>
                  </TouchableOpacity>

                  {/* 1-Tap Save to Vault Button — tap again to unsave */}
                  <TouchableOpacity
                    style={[styles.btnAction, isSaved ? styles.btnActionSaved : styles.btnActionSave]}
                    disabled={savingBizId === biz.id}
                    onPress={(e) => {
                      e.stopPropagation();
                      handleToggleSave(biz, isSaved);
                    }}
                  >
                    {savingBizId === biz.id ? (
                      <ActivityIndicator size="small" color={isSaved ? '#059669' : colors.primary} />
                    ) : isSaved ? (
                      <>
                        <BookmarkCheck size={14} color="#059669" />
                        <Text style={[styles.btnActionText, { color: '#059669', fontWeight: '700' }]}>Saved</Text>
                      </>
                    ) : (
                      <>
                        <BookmarkPlus size={14} color={colors.primary} />
                        <Text style={[styles.btnActionText, { color: colors.primary, fontWeight: '700' }]}>Save</Text>
                      </>
                    )}
                  </TouchableOpacity>
                </View>
              </Card>
            );
          })
        )}
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
  container: {
    flex: 1,
    backgroundColor: colors.bgMuted
  },
  pageHead: {
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.sm,
    backgroundColor: colors.bgMuted
  },
  pageTitle: {
    fontSize: 22,
    fontWeight: '700',
    color: colors.textPrimary,
    fontFamily: typography.titleMedium.fontFamily
  },
  pageSub: {
    fontSize: 13,
    color: colors.textSecondary,
    marginTop: 4
  },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    backgroundColor: colors.bgCard,
    borderBottomWidth: 1,
    borderBottomColor: colors.border
  },
  searchInputWrap: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.bgMuted,
    borderRadius: radii.md,
    paddingHorizontal: spacing.md,
    height: 44,
    marginRight: spacing.sm
  },
  input: {
    flex: 1,
    fontSize: 14,
    color: colors.textPrimary,
    outlineStyle: 'none'
  },
  filterToggleBtn: {
    width: 44,
    height: 44,
    borderRadius: radii.md,
    backgroundColor: colors.bgMuted,
    alignItems: 'center',
    justifyContent: 'center'
  },
  filterToggleActive: {
    backgroundColor: colors.primary
  },
  filterPanel: {
    backgroundColor: colors.bgCard,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    borderBottomWidth: 1,
    borderBottomColor: colors.border
  },
  filterSectionTitle: {
    ...typography.caption,
    fontWeight: '700',
    color: colors.textSecondary,
    marginBottom: spacing.xs,
    textTransform: 'uppercase',
    letterSpacing: 0.5
  },
  radiusRow: {
    flexDirection: 'row',
    marginBottom: spacing.md
  },
  radiusBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: 6,
    borderRadius: radii.full,
    borderWidth: 1,
    borderColor: colors.border,
    marginRight: spacing.sm,
    backgroundColor: colors.bgCard
  },
  radiusBtnActive: {
    backgroundColor: colors.primaryLight,
    borderColor: colors.primary
  },
  radiusText: {
    fontSize: 12,
    fontWeight: '600',
    color: colors.textSecondary
  },
  radiusTextActive: {
    color: colors.primary,
    fontWeight: '700'
  },
  gstToggleRow: {
    flexDirection: 'row',
    alignItems: 'center'
  },
  checkbox: {
    width: 20,
    height: 20,
    borderRadius: 4,
    borderWidth: 1.5,
    borderColor: colors.border,
    marginRight: spacing.sm,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.bgCard
  },
  checkboxChecked: {
    backgroundColor: colors.primary,
    borderColor: colors.primary
  },
  gstToggleLabel: {
    fontSize: 13,
    color: colors.textPrimary,
    fontWeight: '500'
  },
  resultsScroll: {
    padding: spacing.lg,
    paddingBottom: 40
  },
  resultsMetaRow: {
    marginBottom: spacing.md
  },
  resultsCount: {
    ...typography.caption,
    fontWeight: '600',
    color: colors.textSecondary
  },
  loadingWrapper: {
    paddingVertical: spacing.md
  },
  bizCard: {
    marginBottom: spacing.md,
    padding: spacing.md,
    position: 'relative'
  },
  savedChip: {
    position: 'absolute',
    top: spacing.md,
    right: spacing.md,
    backgroundColor: '#D1FAE5',
    borderColor: '#A7F3D0',
    borderWidth: 1,
    borderRadius: radii.full,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 8,
    paddingVertical: 3,
    zIndex: 10
  },
  savedChipText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#065F46',
    letterSpacing: 0.5
  },
  bizHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    marginBottom: spacing.sm
  },
  bizLogo: {
    width: 44,
    height: 44,
    borderRadius: radii.md,
    backgroundColor: colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: spacing.md
  },
  bizName: {
    fontSize: 15,
    fontWeight: '700',
    color: colors.textPrimary,
    marginBottom: 2
  },
  bizCat: {
    ...typography.caption,
    color: colors.textSecondary
  },
  pincode: {
    fontSize: 11,
    color: colors.textMuted,
    marginLeft: spacing.sm
  },
  bizDesc: {
    fontSize: 12,
    color: colors.textSecondary,
    lineHeight: 18,
    marginBottom: spacing.sm
  },
  serviceChipsRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    marginBottom: spacing.md
  },
  serviceChip: {
    backgroundColor: colors.bgMutedDark,
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: radii.chip,
    marginRight: 6,
    marginBottom: 4
  },
  serviceText: {
    fontSize: 11,
    color: colors.textMuted,
    fontWeight: '500'
  },
  bizActions: {
    flexDirection: 'row',
    paddingTop: spacing.sm,
    justifyContent: 'space-between',
    alignItems: 'center'
  },
  btnAction: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 8,
    borderRadius: radii.button,
    backgroundColor: colors.bgMuted,
    marginHorizontal: 3
  },
  btnActionSave: {
    backgroundColor: colors.primaryLight
  },
  btnActionSaved: {
    backgroundColor: colors.successLight
  },
  btnActionText: {
    fontSize: 12,
    fontWeight: '600',
    color: colors.primary,
    marginLeft: 4
  }
});
