import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { Phone, Mail, MapPin, ShieldCheck, Building2 } from 'lucide-react';
import { colors, radii, spacing, fonts } from '../theme';

export const CARD_TEMPLATES = [
  { id: 'classic', name: 'Classic' },
  { id: 'banner', name: 'Bold Banner' },
  { id: 'minimal', name: 'Minimal Outline' },
  { id: 'split', name: 'Split Side' },
  { id: 'dark', name: 'Elegant Dark' },
  { id: 'emboss', name: 'Emboss Signet' },
  { id: 'ribbon', name: 'Ribbon Corner' },
  { id: 'boutique', name: 'Boutique Frame' }
];

// Locked behind Pro — everything else is free.
export const PREMIUM_TEMPLATE_IDS = ['dark', 'emboss', 'boutique'];

function initialOf(name) {
  return (name || '?').trim().charAt(0).toUpperCase() || '?';
}

// Two-letter monogram from the first two words of the name — e.g. "Ajay krish" -> "AK".
// Falls back to a single letter when the name is only one word.
function monogramOf(name) {
  const words = (name || '').trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return '?';
  if (words.length === 1) return words[0].charAt(0).toUpperCase();
  return (words[0].charAt(0) + words[1].charAt(0)).toUpperCase();
}

function composeAddress(business) {
  const line = business?.address || business?.address_line1 || '';
  const cityState = [business?.city, business?.state].filter(Boolean).join(', ');
  const withCityState = [line, cityState].filter(Boolean).join(', ');
  return business?.pincode ? [withCityState, business.pincode].filter(Boolean).join(' - ') : withCityState;
}

function ContactRow({ icon: Icon, text, tint, iconColor, textColor }) {
  if (!text) return null;
  return (
    <View style={s.row}>
      <View style={[s.rowIcon, { backgroundColor: tint }]}>
        <Icon size={13} color={iconColor || colors.primary} />
      </View>
      <Text style={[s.rowText, textColor ? { color: textColor } : null]} numberOfLines={2}>{text}</Text>
    </View>
  );
}

function Classic({ card }) {
  return (
    <View style={[s.card, { borderTopWidth: 4, borderTopColor: colors.primary }]}>
      <View style={s.accentCircle} />
      <View style={s.headerRow}>
        <View style={[s.avatar, { backgroundColor: colors.primary }]}>
          <Text style={s.avatarText}>{initialOf(card.name)}</Text>
        </View>
        <View style={{ flex: 1 }}>
          <Text style={s.name}>{card.name}</Text>
          {card.category ? <Text style={s.sub}>{card.category}</Text> : null}
        </View>
        {card.gstin ? (
          <View style={[s.badge, { backgroundColor: colors.goldLight }]}>
            <ShieldCheck size={11} color={colors.gold} />
            <Text style={[s.badgeText, { color: colors.gold }]}>GST</Text>
          </View>
        ) : null}
      </View>
      {card.gstin ? <Text style={s.gstinLine}>GSTIN: {card.gstin}</Text> : null}
      <View style={s.divider} />
      <ContactRow icon={Phone} text={card.phone} tint={colors.primaryLight} />
      <ContactRow icon={Mail} text={card.email} tint={colors.primaryLight} />
      <ContactRow icon={MapPin} text={card.address} tint={colors.primaryLight} />
    </View>
  );
}

function Banner({ card }) {
  return (
    <View style={s.card}>
      <View style={[s.bannerHead, { backgroundColor: colors.primary }]}>
        <View style={[s.avatar, { backgroundColor: 'rgba(255,255,255,0.2)' }]}>
          <Text style={[s.avatarText, { color: '#FFFFFF' }]}>{initialOf(card.name)}</Text>
        </View>
        <View style={{ flex: 1 }}>
          <Text style={[s.name, { color: '#FFFFFF' }]}>{card.name}</Text>
          {card.category ? <Text style={[s.sub, { color: 'rgba(255,255,255,0.8)' }]}>{card.category}</Text> : null}
        </View>
        {card.gstin ? (
          <View style={[s.badge, { backgroundColor: 'rgba(255,255,255,0.2)' }]}>
            <ShieldCheck size={11} color="#FFFFFF" />
            <Text style={[s.badgeText, { color: '#FFFFFF' }]}>GST</Text>
          </View>
        ) : null}
      </View>
      <View style={s.bannerBody}>
        {card.gstin ? <Text style={s.gstinLine}>GSTIN: {card.gstin}</Text> : null}
        <ContactRow icon={Phone} text={card.phone} tint={colors.primaryLight} />
        <ContactRow icon={Mail} text={card.email} tint={colors.primaryLight} />
        <ContactRow icon={MapPin} text={card.address} tint={colors.primaryLight} />
      </View>
    </View>
  );
}

function Minimal({ card }) {
  return (
    <View style={[s.card, { borderWidth: 1, borderColor: colors.border, alignItems: 'center' }]}>
      <View style={[s.avatar, { backgroundColor: colors.bgMuted, marginRight: 0, marginBottom: spacing.sm }]}>
        <Building2 size={20} color={colors.primary} />
      </View>
      <Text style={[s.name, { textAlign: 'center' }]}>{card.name}</Text>
      {card.category ? <Text style={[s.sub, { textAlign: 'center' }]}>{card.category}</Text> : null}
      {card.gstin ? <Text style={[s.gstinLine, { textAlign: 'center' }]}>GSTIN: {card.gstin}</Text> : null}
      <View style={[s.divider, { width: '60%' }]} />
      <View style={{ alignSelf: 'stretch' }}>
        <ContactRow icon={Phone} text={card.phone} tint={colors.bgMuted} />
        <ContactRow icon={Mail} text={card.email} tint={colors.bgMuted} />
        <ContactRow icon={MapPin} text={card.address} tint={colors.bgMuted} />
      </View>
    </View>
  );
}

function Split({ card }) {
  return (
    <View style={[s.card, { flexDirection: 'row', padding: 0, overflow: 'hidden' }]}>
      <View style={[s.splitLeft, { backgroundColor: colors.primary }]}>
        <View style={[s.avatar, { backgroundColor: 'rgba(255,255,255,0.2)', marginRight: 0 }]}>
          <Text style={[s.avatarText, { color: '#FFFFFF' }]}>{initialOf(card.name)}</Text>
        </View>
        {card.gstin ? (
          <View style={[s.badge, { backgroundColor: 'rgba(255,255,255,0.2)', marginTop: spacing.sm }]}>
            <ShieldCheck size={11} color="#FFFFFF" />
            <Text style={[s.badgeText, { color: '#FFFFFF' }]}>GST</Text>
          </View>
        ) : null}
      </View>
      <View style={{ flex: 1, padding: spacing.lg }}>
        <Text style={s.name}>{card.name}</Text>
        {card.category ? <Text style={s.sub}>{card.category}</Text> : null}
        {card.gstin ? <Text style={s.gstinLine}>GSTIN: {card.gstin}</Text> : null}
        <View style={s.divider} />
        <ContactRow icon={Phone} text={card.phone} tint={colors.primaryLight} />
        <ContactRow icon={Mail} text={card.email} tint={colors.primaryLight} />
        <ContactRow icon={MapPin} text={card.address} tint={colors.primaryLight} />
      </View>
    </View>
  );
}

function Dark({ card }) {
  return (
    <View style={[s.card, { backgroundColor: '#17151A', borderWidth: 0 }]}>
      <View style={s.headerRow}>
        <View style={[s.avatar, { backgroundColor: colors.gold }]}>
          <Text style={[s.avatarText, { color: '#17151A' }]}>{initialOf(card.name)}</Text>
        </View>
        <View style={{ flex: 1 }}>
          <Text style={[s.name, { color: '#FFFFFF' }]}>{card.name}</Text>
          {card.category ? <Text style={[s.sub, { color: 'rgba(255,255,255,0.6)' }]}>{card.category}</Text> : null}
        </View>
        {card.gstin ? (
          <View style={[s.badge, { backgroundColor: 'rgba(184,148,69,0.2)' }]}>
            <ShieldCheck size={11} color={colors.gold} />
            <Text style={[s.badgeText, { color: colors.gold }]}>GST</Text>
          </View>
        ) : null}
      </View>
      {card.gstin ? <Text style={[s.gstinLine, { color: colors.gold }]}>GSTIN: {card.gstin}</Text> : null}
      <View style={[s.divider, { backgroundColor: 'rgba(255,255,255,0.12)' }]} />
      <ContactRow icon={Phone} text={card.phone} tint="rgba(184,148,69,0.18)" iconColor={colors.gold} textColor="rgba(255,255,255,0.8)" />
      <ContactRow icon={Mail} text={card.email} tint="rgba(184,148,69,0.18)" iconColor={colors.gold} textColor="rgba(255,255,255,0.8)" />
      <ContactRow icon={MapPin} text={card.address} tint="rgba(184,148,69,0.18)" iconColor={colors.gold} textColor="rgba(255,255,255,0.8)" />
    </View>
  );
}

// Premium look #1 — a wax-seal style ring monogram with a thin gold frame.
function EmbossSignet({ card }) {
  return (
    <View style={[s.card, { borderWidth: 1.5, borderColor: colors.gold }]}>
      <View style={s.headerRow}>
        <View style={s.signetRingOuter}>
          <View style={s.signetRingInner}>
            <Text style={s.signetMonogram}>{monogramOf(card.name)}</Text>
          </View>
        </View>
        <View style={{ flex: 1 }}>
          <Text style={[s.name, s.serifFont]}>{card.name}</Text>
          {card.category ? <Text style={s.subCaps}>{card.category}</Text> : null}
        </View>
        {card.gstin ? (
          <View style={[s.badge, { backgroundColor: colors.goldLight }]}>
            <ShieldCheck size={11} color={colors.gold} />
            <Text style={[s.badgeText, { color: colors.gold }]}>GST</Text>
          </View>
        ) : null}
      </View>
      {card.gstin ? <Text style={s.gstinLine}>GSTIN: {card.gstin}</Text> : null}
      <View style={s.goldDivider} />
      <ContactRow icon={Phone} text={card.phone} tint={colors.goldLight} iconColor={colors.gold} />
      <ContactRow icon={Mail} text={card.email} tint={colors.goldLight} iconColor={colors.gold} />
      <ContactRow icon={MapPin} text={card.address} tint={colors.goldLight} iconColor={colors.gold} />
    </View>
  );
}

// Premium look #2 — a folded ribbon corner + a layered, slightly-tilted monogram badge.
function RibbonCorner({ card }) {
  return (
    <View style={[s.card, { paddingTop: spacing.xl }]}>
      <View style={s.ribbonCorner}>
        <Text style={s.ribbonText}>{card.gstin ? 'GST' : 'BIZ'}</Text>
      </View>
      <View style={s.headerRow}>
        <View style={s.monogramSquareBack}>
          <View style={s.monogramSquareFront}>
            <Text style={s.monogramSquareText}>{monogramOf(card.name)}</Text>
          </View>
        </View>
        <View style={{ flex: 1 }}>
          <Text style={s.name}>{card.name}</Text>
          {card.category ? <Text style={s.sub}>{card.category}</Text> : null}
        </View>
      </View>
      {card.gstin ? <Text style={s.gstinLine}>GSTIN: {card.gstin}</Text> : null}
      <View style={s.divider} />
      <ContactRow icon={Phone} text={card.phone} tint={colors.primaryLight} />
      <ContactRow icon={Mail} text={card.email} tint={colors.primaryLight} />
      <ContactRow icon={MapPin} text={card.address} tint={colors.primaryLight} />
    </View>
  );
}

// Premium look #3 — a boutique double-frame with a centered monogram seal.
function BoutiqueFrame({ card }) {
  return (
    <View style={s.card}>
      <View style={s.frameInner}>
        <View style={s.boutiqueRing}>
          <Text style={[s.signetMonogram, { color: colors.primary }]}>{monogramOf(card.name)}</Text>
        </View>
        <Text style={[s.name, s.serifFont, { textAlign: 'center', marginTop: spacing.sm }]}>{card.name}</Text>
        {card.category ? <Text style={[s.subCaps, { textAlign: 'center' }]}>{card.category}</Text> : null}
        <View style={s.boutiqueRule}>
          <View style={s.boutiqueRuleLine} />
          <View style={s.boutiqueDot} />
          <View style={s.boutiqueRuleLine} />
        </View>
        {card.gstin ? <Text style={[s.gstinLine, { textAlign: 'center', marginTop: 0 }]}>GSTIN: {card.gstin}</Text> : null}
        <View style={{ marginTop: spacing.md, alignSelf: 'stretch' }}>
          <ContactRow icon={Phone} text={card.phone} tint={colors.bgMuted} />
          <ContactRow icon={Mail} text={card.email} tint={colors.bgMuted} />
          <ContactRow icon={MapPin} text={card.address} tint={colors.bgMuted} />
        </View>
      </View>
    </View>
  );
}

const RENDERERS = {
  classic: Classic,
  banner: Banner,
  minimal: Minimal,
  split: Split,
  dark: Dark,
  emboss: EmbossSignet,
  ribbon: RibbonCorner,
  boutique: BoutiqueFrame
};

export const BusinessCardPreview = React.forwardRef(function BusinessCardPreview({ business, templateId }, ref) {
  const card = {
    name: business?.name || business?.business_name || '',
    category: [business?.category || business?.primary_category, business?.city].filter(Boolean).join(' · '),
    gstin: business?.gstin || '',
    phone: business?.phone || '',
    email: business?.email || '',
    address: composeAddress(business)
  };
  const Renderer = RENDERERS[templateId] || Classic;
  return (
    <View ref={ref} collapsable={false}>
      <Renderer card={card} />
    </View>
  );
});

const s = StyleSheet.create({
  card: {
    backgroundColor: colors.bgCard,
    borderRadius: radii.card,
    padding: spacing.lg,
    overflow: 'hidden'
  },
  accentCircle: {
    position: 'absolute', top: 0, right: 0, width: 96, height: 96,
    backgroundColor: colors.primaryLight, borderBottomLeftRadius: 96
  },
  headerRow: { flexDirection: 'row', alignItems: 'center' },
  avatar: {
    width: 44, height: 44, borderRadius: radii.pill,
    alignItems: 'center', justifyContent: 'center', marginRight: spacing.md
  },
  avatarText: { fontSize: 17, fontWeight: '700', color: '#FFFFFF' },
  name: { fontSize: 17, fontWeight: '800', color: colors.textPrimary },
  serifFont: { fontFamily: fonts.serif },
  sub: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  subCaps: { fontSize: 10, color: colors.textMuted, marginTop: 3, letterSpacing: 1, textTransform: 'uppercase' },
  badge: {
    flexDirection: 'row', alignItems: 'center', gap: 4,
    paddingHorizontal: 8, paddingVertical: 4, borderRadius: radii.pill
  },
  badgeText: { fontSize: 10, fontWeight: '700' },
  gstinLine: { fontSize: 11, fontWeight: '600', color: colors.textMuted, marginTop: spacing.sm },
  divider: { height: 1, backgroundColor: colors.border, marginVertical: spacing.md },
  goldDivider: { height: 1, backgroundColor: colors.gold, opacity: 0.35, marginVertical: spacing.md },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: spacing.sm },
  rowIcon: {
    width: 26, height: 26, borderRadius: radii.pill,
    alignItems: 'center', justifyContent: 'center'
  },
  rowText: { fontSize: 12, color: colors.textSecondary, flex: 1 },
  bannerHead: { flexDirection: 'row', alignItems: 'center', margin: -spacing.lg, marginBottom: spacing.md, padding: spacing.lg },
  bannerBody: {},
  splitLeft: { width: 64, alignItems: 'center', justifyContent: 'center', padding: spacing.sm },

  // Emboss Signet
  signetRingOuter: {
    width: 52, height: 52, borderRadius: radii.pill,
    borderWidth: 2, borderColor: colors.gold,
    alignItems: 'center', justifyContent: 'center',
    marginRight: spacing.md
  },
  signetRingInner: {
    width: 40, height: 40, borderRadius: radii.pill,
    backgroundColor: colors.primary,
    alignItems: 'center', justifyContent: 'center'
  },
  signetMonogram: { fontSize: 15, fontWeight: '700', color: '#FFFFFF', fontFamily: fonts.serif },

  // Ribbon Corner
  ribbonCorner: {
    position: 'absolute', top: 16, right: -36, width: 140,
    backgroundColor: colors.primary, paddingVertical: 4,
    alignItems: 'center', zIndex: 5,
    transform: [{ rotate: '45deg' }]
  },
  ribbonText: { fontSize: 10, fontWeight: '800', color: '#FFFFFF', letterSpacing: 1 },
  monogramSquareBack: {
    width: 46, height: 46, borderRadius: radii.sm,
    backgroundColor: colors.primaryLight,
    alignItems: 'center', justifyContent: 'center',
    marginRight: spacing.md,
    transform: [{ rotate: '8deg' }]
  },
  monogramSquareFront: {
    width: 40, height: 40, borderRadius: radii.sm,
    backgroundColor: colors.primary,
    alignItems: 'center', justifyContent: 'center',
    transform: [{ rotate: '-8deg' }]
  },
  monogramSquareText: { fontSize: 15, fontWeight: '800', color: '#FFFFFF' },

  // Boutique Frame
  frameInner: {
    borderWidth: 1, borderColor: colors.border, borderRadius: radii.md,
    padding: spacing.lg, alignItems: 'center'
  },
  boutiqueRing: {
    width: 56, height: 56, borderRadius: radii.pill,
    borderWidth: 2, borderColor: colors.gold,
    alignItems: 'center', justifyContent: 'center',
    backgroundColor: colors.bgMuted
  },
  boutiqueRule: { flexDirection: 'row', alignItems: 'center', marginVertical: spacing.md, alignSelf: 'stretch' },
  boutiqueRuleLine: { flex: 1, height: 1, backgroundColor: colors.gold, opacity: 0.5 },
  boutiqueDot: { width: 5, height: 5, borderRadius: 3, backgroundColor: colors.gold, marginHorizontal: 8 }
});
