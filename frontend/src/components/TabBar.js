import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { ScanLine, FolderOpen, Store, Compass, Home } from 'lucide-react';
import { colors, spacing, shadows } from '../theme';
import { useAuth } from '../context/AuthContext';

export function TabBar({ currentTab, onSelectTab }) {
  const { role } = useAuth();

  const getTabs = () => {
    // Home is a first-class destination. Profile is reached from Home — not via Scan.
    return [
      { id: 'user_dashboard', label: 'Home', icon: Home },
      { id: 'user_vault', label: 'My Cards', icon: FolderOpen },
      { id: 'user_scan', label: 'SCAN', icon: ScanLine, isCenter: true },
      { id: 'user_my_business', label: 'My Business', icon: Store },
      { id: 'user_search', label: 'Browse', icon: Compass }
    ];
  };

  const tabs = getTabs();

  return (
    <View style={styles.tabBar}>
      {tabs.map((tab) => {
        const IconComponent = tab.icon;
        const isActive = currentTab === tab.id;

        if (tab.isCenter) {
          return (
            <TouchableOpacity key={tab.id} activeOpacity={0.9} onPress={() => onSelectTab(tab.id)} style={styles.centerWrap}>
              <View style={[styles.centerButton, shadows.scan]}>
                <IconComponent size={22} color="#FFFFFF" strokeWidth={2} />
              </View>
              <Text style={styles.centerLabel}>SCAN</Text>
            </TouchableOpacity>
          );
        }

        return (
          <TouchableOpacity key={tab.id} activeOpacity={0.7} onPress={() => onSelectTab(tab.id)} style={styles.tabItem}>
            <IconComponent size={20} color={isActive ? colors.primary : colors.textMuted} strokeWidth={isActive ? 2.2 : 1.75} />
            <Text style={[styles.tabLabel, isActive && styles.tabLabelActive]}>{tab.label}</Text>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  tabBar: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    justifyContent: 'space-around',
    backgroundColor: colors.bgCard,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    paddingTop: 8,
    paddingBottom: 14,
    paddingHorizontal: 4,
    minHeight: 72
  },
  tabItem: { flex: 1, alignItems: 'center', justifyContent: 'center', paddingBottom: 4 },
  tabLabel: { fontSize: 10, fontWeight: '500', color: colors.textMuted, marginTop: 4 },
  tabLabelActive: { color: colors.primary, fontWeight: '700' },
  centerWrap: { alignItems: 'center', justifyContent: 'flex-end', flex: 1.15, marginTop: -22 },
  centerButton: {
    width: 58,
    height: 58,
    borderRadius: 29,
    backgroundColor: colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 3,
    borderColor: colors.bgSurface
  },
  centerLabel: { fontSize: 9, fontWeight: '800', color: colors.primary, marginTop: 4, letterSpacing: 0.5 }
});
