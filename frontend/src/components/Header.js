import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet, useWindowDimensions } from 'react-native';
import {
  ArrowLeft,
  MapPin,
  Shield,
  Briefcase,
  UserCheck,
  LogOut,
  Sparkles,
  Home,
  Search,
  Camera,
  FolderOpen,
  User,
  LayoutDashboard,
  Building2,
  Inbox,
  BarChart3,
  Users,
  ShieldAlert,
  Settings
} from 'lucide-react';
import { colors, radii, spacing, typography } from '../theme';
import { useAuth } from '../context/AuthContext';

export function Header({
  title,
  subtitle,
  showBack = false,
  onBack,
  rightAction,
  showLocation = false,
  locationText = 'Coimbatore, TN',
  currentTab,
  onSelectTab
}) {
  const { user, role, logout } = useAuth();
  const { width } = useWindowDimensions();
  const isDesktop = width >= 768;

  const getRoleBadge = () => {
    if (role === 'owner') {
      return {
        label: 'BUSINESS OWNER',
        bg: '#EFF6FF',
        color: '#2563EB',
        Icon: Briefcase
      };
    }
    return {
      label: 'USER',
      bg: '#F1F5F9',
      color: '#475569',
      Icon: UserCheck
    };
  };

  const getDesktopNavLinks = () => {
    if (!user || !onSelectTab) return [];

    return [
      { id: 'user_dashboard', label: 'Dashboard', icon: Home },
      { id: 'user_vault', label: 'My Cards', icon: FolderOpen },
      { id: 'user_scan', label: 'Scan', icon: Camera, isPrimary: true },
      { id: 'user_my_business', label: 'My Business', icon: Building2 },
      { id: 'user_support', label: 'Support', icon: User }
    ];
  };

  const roleConfig = getRoleBadge();
  const RoleIcon = roleConfig.Icon;
  const desktopLinks = getDesktopNavLinks();

  return (
    <View style={[styles.header, isDesktop && styles.desktopHeader]}>
      <View style={[styles.innerContainer, isDesktop && styles.desktopInnerContainer]}>
        {/* Left: Branding or Title */}
        <View style={styles.leftContainer}>
          {showBack && (
            <TouchableOpacity onPress={onBack} style={styles.backButton} activeOpacity={0.7}>
              <ArrowLeft size={20} color="#FFFFFF" />
            </TouchableOpacity>
          )}

          {isDesktop ? (
            <TouchableOpacity
              onPress={() => onSelectTab && onSelectTab('user_dashboard')}
              style={styles.desktopLogoRow}
              activeOpacity={0.8}
            >
              <View style={styles.logoBadge}>
                <Sparkles size={16} color="#FFFFFF" />
              </View>
              <View>
                <Text style={styles.brandTitle}>CardFlow</Text>
                <Text style={styles.brandSubtitle}>Business Card & Discovery Platform</Text>
              </View>
            </TouchableOpacity>
          ) : showLocation ? (
            <View style={styles.locationChip}>
              <MapPin size={14} color={colors.primary} style={{ marginRight: 4 }} />
              <Text style={styles.locationText}>{locationText}</Text>
            </View>
          ) : (
            <View>
              <Text style={styles.title}>{title}</Text>
              {subtitle && <Text style={styles.subtitle}>{subtitle}</Text>}
            </View>
          )}
        </View>

        {/* Center: Desktop Navigation Bar Links */}
        {isDesktop && desktopLinks.length > 0 && (
          <View style={styles.desktopNavList}>
            {desktopLinks.map((link) => {
              const IconComp = link.icon;
              const isActive = currentTab === link.id;

              if (link.isPrimary) {
                return (
                  <TouchableOpacity
                    key={link.id}
                    onPress={() => onSelectTab(link.id)}
                    style={[styles.desktopPrimaryNavBtn, isActive && styles.desktopPrimaryNavBtnActive]}
                    activeOpacity={0.8}
                  >
                    <IconComp size={16} color="#FFFFFF" style={{ marginRight: 6 }} />
                    <Text style={styles.desktopPrimaryNavText}>{link.label}</Text>
                  </TouchableOpacity>
                );
              }

              return (
                <TouchableOpacity
                  key={link.id}
                  onPress={() => onSelectTab(link.id)}
                  style={[styles.desktopNavItem, isActive && styles.desktopNavItemActive]}
                  activeOpacity={0.7}
                >
                  <IconComp size={16} color={isActive ? colors.primary : '#94A3B8'} style={{ marginRight: 6 }} />
                  <Text style={[styles.desktopNavText, isActive && styles.desktopNavTextActive]}>
                    {link.label}
                  </Text>
                </TouchableOpacity>
              );
            })}
          </View>
        )}

        {/* Right: User Status & Actions */}
        <View style={styles.rightContainer}>
          {user && (
            <View style={[styles.roleBadge, { backgroundColor: roleConfig.bg }]}>
              <RoleIcon size={12} color={roleConfig.color} style={{ marginRight: 4 }} />
              <Text style={[styles.roleText, { color: roleConfig.color }]}>
                {isDesktop && user.name ? `${user.name} (${roleConfig.label})` : roleConfig.label}
              </Text>
            </View>
          )}
          {rightAction ? (
            rightAction
          ) : user ? (
            <TouchableOpacity onPress={logout} title="Sign Out" style={styles.logoutBtn} activeOpacity={0.7}>
              <LogOut size={16} color="#94A3B8" />
              {isDesktop && <Text style={styles.logoutBtnText}>Sign Out</Text>}
            </TouchableOpacity>
          ) : null}
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    backgroundColor: '#0F172A',
    borderBottomWidth: 1,
    borderBottomColor: '#1E293B',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    zIndex: 20
  },
  desktopHeader: {
    paddingVertical: spacing.sm,
    backgroundColor: '#0F172A',
    borderBottomColor: '#1E293B',
    boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)'
  },
  innerContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    width: '100%'
  },
  desktopInnerContainer: {
    maxWidth: 1280,
    alignSelf: 'center'
  },
  leftContainer: {
    flexDirection: 'row',
    alignItems: 'center'
  },
  desktopLogoRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm
  },
  logoBadge: {
    width: 32,
    height: 32,
    borderRadius: radii.md,
    backgroundColor: colors.primary,
    alignItems: 'center',
    justifyContent: 'center'
  },
  brandTitle: {
    color: '#FFFFFF',
    fontSize: 16,
    fontWeight: '800',
    letterSpacing: 0.5
  },
  brandSubtitle: {
    color: '#94A3B8',
    fontSize: 10,
    fontWeight: '500'
  },
  backButton: {
    padding: spacing.xs,
    marginRight: spacing.sm,
    borderRadius: radii.sm,
    backgroundColor: '#1E293B'
  },
  title: {
    ...typography.titleSmall,
    color: '#FFFFFF'
  },
  subtitle: {
    ...typography.caption,
    color: '#94A3B8'
  },
  locationChip: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: '#1E293B',
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
    borderRadius: radii.full
  },
  locationText: {
    ...typography.bodySmall,
    fontWeight: '600',
    color: '#E2E8F0'
  },
  desktopNavList: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs
  },
  desktopNavItem: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radii.md,
    backgroundColor: 'transparent'
  },
  desktopNavItemActive: {
    backgroundColor: '#1E293B'
  },
  desktopNavText: {
    color: '#94A3B8',
    fontSize: 13,
    fontWeight: '600'
  },
  desktopNavTextActive: {
    color: '#FFFFFF',
    fontWeight: '700'
  },
  desktopPrimaryNavBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.primary,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radii.md,
    marginLeft: spacing.xs
  },
  desktopPrimaryNavBtnActive: {
    backgroundColor: colors.primaryDark
  },
  desktopPrimaryNavText: {
    color: '#FFFFFF',
    fontSize: 13,
    fontWeight: '700'
  },
  rightContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm
  },
  roleBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
    borderRadius: radii.full
  },
  roleText: {
    fontSize: 11,
    fontWeight: '700',
    letterSpacing: 0.5
  },
  logoutBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.sm,
    paddingVertical: 6,
    borderRadius: radii.md,
    backgroundColor: '#1E293B',
    gap: 6
  },
  logoutBtnText: {
    color: '#CBD5E1',
    fontSize: 12,
    fontWeight: '600'
  }
});
