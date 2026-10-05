import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { View, StyleSheet } from 'react-native';
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { useCrm } from '../context/CrmContext';
import { colors } from '../theme';
import { Layout } from '../components/Layout';
import { TabBar } from '../components/TabBar';
import { ScreenLoader } from '../components/Loader';
import { fetchListing } from '../services/api';
import { parseRoute, paths } from './routes';

import { SearchScreen } from '../screens/user/SearchScreen';
import { BusinessDetailsScreen } from '../screens/user/BusinessDetailsScreen';
import { SavedCardsScreen } from '../screens/user/SavedCardsScreen';
import { SavedCardDetailScreen } from '../screens/user/SavedCardDetailScreen';
import { SharedCardScreen } from '../screens/user/SharedCardScreen';
import { ScanCardScreen } from '../screens/user/ScanCardScreen';
import { ProfileScreen } from '../screens/user/ProfileScreen';
import { SubscriptionScreen } from '../components/SubscriptionScreen';
import { MyBusinessHubScreen } from '../screens/user/MyBusinessHubScreen';
import { SupportHubScreen } from '../screens/user/SupportHubScreen';
import { SupportRequestScreen } from '../screens/user/SupportRequestScreen';
import { SupportTicketsScreen } from '../screens/user/SupportTicketsScreen';
import { SupportTicketDetailScreen } from '../screens/user/SupportTicketDetailScreen';
import { CrmHomeScreen } from '../screens/crm/CrmHomeScreen';
import { CrmModulesScreen } from '../screens/crm/CrmModulesScreen';
import { RecordListScreen } from '../screens/crm/RecordListScreen';
import { RecordDetailScreen } from '../screens/crm/RecordDetailScreen';
import { RecordFormScreen } from '../screens/crm/RecordFormScreen';
import { BusinessGate, BusinessSwitcher } from '../screens/crm/BusinessScreens';
import { WelcomeTour } from '../screens/crm/GettingStarted';

/**
 * The phone layout. The address bar is the navigation state (D-104): every screen has a
 * URL, the same URL the desktop CRM uses for the same thing, so links, reload and the
 * browser's Back button all work.
 *
 * Bottom tabs: Home · My CRM · Scan · My Cards · Browse.
 */
export function AppNavigator() {
  const { isAuthenticated, authReady, savedCards, subscriptionOverlayOpen, closeSubscription, logout, user } = useAuth();
  const { status, businesses, activeCode, switchBusiness } = useCrm();
  const location = useLocation();
  const navigate = useNavigate();
  const [switcherOpen, setSwitcherOpen] = useState(false);
  // Things a URL can't carry: a listing or ticket picked from a list, a form's prefill.
  const memory = useRef({ listings: {}, tickets: {}, prefill: {}, steps: 0 });
  const [, bump] = useState(0);

  const route = useMemo(() => parseRoute(location.pathname, location.search), [location.pathname, location.search]);

  const go = useCallback((to, opts) => {
    memory.current.steps += opts?.replace ? 0 : 1;
    navigate(to, opts);
  }, [navigate]);

  /** Back: the previous screen when we came from one, otherwise the screen above this one. */
  const back = useCallback((fallback) => {
    if (memory.current.steps > 0) {
      memory.current.steps -= 1;
      navigate(-1);
    } else {
      navigate(fallback, { replace: true });
    }
  }, [navigate]);

  // The business in the URL is the open business.
  const urlCode = route?.code || '';
  const isMember = urlCode ? businesses.some((b) => b.code === urlCode) : true;
  useEffect(() => {
    if (status !== 'ready' || !urlCode) return;
    if (isMember && urlCode !== activeCode) switchBusiness(urlCode);
    if (!isMember) navigate(activeCode ? paths.home(activeCode) : paths.businesses, { replace: true });
  }, [status, urlCode, isMember, activeCode, switchBusiness, navigate]);

  // "/" (and the sign-in address, once signed in) is the open business's Home.
  useEffect(() => {
    if (!isAuthenticated || status !== 'ready' || !route) return;
    if (route.name === 'root' || (route.name === 'businesses' && businesses.length > 0 && !route.create)) {
      navigate(activeCode ? paths.home(activeCode) : paths.businesses, { replace: true });
    }
  }, [isAuthenticated, status, route, activeCode, businesses.length, navigate]);

  // A listing opened by link: load it once.
  const listingId = route?.name === 'listing' ? route.id : '';
  useEffect(() => {
    if (!listingId || memory.current.listings[listingId]) return;
    fetchListing(listingId)
      .then((biz) => {
        memory.current.listings[listingId] = biz;
        bump((n) => n + 1);
      })
      .catch(() => navigate(paths.browse, { replace: true }));
  }, [listingId, navigate]);

  // The tab title says where you are, as on the desktop.
  const activeName = businesses.find((b) => b.code === activeCode)?.name || '';
  useEffect(() => {
    const names = { home: 'Home', menu: 'My CRM', cards: 'My Cards', card: 'Business card', scan: 'Scan a card', browse: 'Browse', listing: 'Listing',
      mybusiness: 'My business', profile: 'Profile', support: 'Support', businesses: 'Your businesses', share: 'Shared card' };
    const what = route ? names[route.name] || (route.object ? route.object.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase()) : '') : '';
    document.title = [what, activeName, "Ajay's CRM"].filter(Boolean).join(' · ');
  }, [route, activeName]);

  const code = activeCode;
  const openRecord = useCallback((object, id) => go(paths.detail(code, object, id)), [go, code]);
  const openList = useCallback((object, opts) => go(paths.list(code, object, opts?.q)), [go, code]);
  const openCreate = useCallback((object, prefill) => {
    let key = '';
    if (prefill) {
      key = Math.random().toString(36).slice(2, 10);
      memory.current.prefill[key] = prefill;
    }
    go(paths.create(code, object, key));
  }, [go, code]);
  const openListing = useCallback((biz) => {
    memory.current.listings[biz.id] = biz;
    go(paths.listing(biz.id));
  }, [go]);
  const openCard = useCallback((card) => go(paths.card(code || '-', card.id)), [go, code]);

  // A shared-card link is public: no sign-in needed.
  if (route?.name === 'share') {
    return (
      <Layout>
        <SharedCardScreen cardId={route.id} onDone={() => navigate('/', { replace: true })} />
      </Layout>
    );
  }

  if (!authReady || !isAuthenticated || !route) {
    return (
      <Layout>
        <ScreenLoader message="Opening your workspace…" subMessage="" />
      </Layout>
    );
  }

  const selectTab = (tabId) => {
    if (tabId === 'user_dashboard') go(code ? paths.home(code) : paths.businesses);
    else if (tabId === 'user_crm') go(code ? paths.menu(code) : paths.businesses);
    else if (tabId === 'user_scan') go(paths.scan(code || '-'));
    else if (tabId === 'user_vault') go(paths.cards(code || '-'));
    else if (tabId === 'user_search') go(paths.browse);
    else if (tabId === 'user_my_business') go(paths.myBusiness(code || '-'));
    else if (tabId === 'user_support') go(paths.support('hub'));
    else if (tabId === 'user_profile') go(paths.profile);
  };

  const homePath = code ? paths.home(code) : paths.businesses;
  const menuPath = code ? paths.menu(code) : paths.businesses;
  let tab = 'user_dashboard';
  let hideTabs = false;
  let screen = null;

  switch (route.name) {
    case 'menu':
      tab = 'user_crm';
      screen = (
        <BusinessGate>
          <CrmModulesScreen
            onOpenSwitcher={() => setSwitcherOpen(true)}
            onOpenList={openList}
            onOpenPath={(path) => go(path)}
            onOpenRecord={openRecord}
            onOpenCards={() => go(paths.cards(code))}
            onOpenListing={() => go(paths.myBusiness(code))}
            onOpenTeam={() => go(paths.team(code))}
            onOpenBusinessProfile={() => go(paths.businessProfile(code))}
          />
        </BusinessGate>
      );
      break;
    case 'list':
      tab = 'user_crm';
      screen = (
        <BusinessGate>
          <RecordListScreen
            key={`${route.code}-${route.object}-${route.preset?.value || ''}`}
            object={route.object}
            initialQuery={route.q}
            preset={route.preset}
            onBack={() => back(menuPath)}
            onOpenRecord={openRecord}
            onCreate={(object) => openCreate(object)}
          />
        </BusinessGate>
      );
      break;
    case 'detail':
      tab = 'user_crm';
      screen = (
        <BusinessGate>
          <RecordDetailScreen
            key={`${route.code}-${route.object}-${route.id}`}
            object={route.object}
            id={route.id}
            onBack={() => back(paths.list(code, route.object))}
            onEdit={(object, id) => go(paths.edit(code, object, id))}
            onOpenRecord={openRecord}
            onCreateRelated={(object, prefill) => openCreate(object, prefill)}
            onOpenCard={(cardId) => go(paths.card(code, cardId))}
            onDeleted={() => navigate(paths.list(code, route.object), { replace: true })}
          />
        </BusinessGate>
      );
      break;
    case 'form': {
      tab = 'user_crm';
      hideTabs = true;
      const prefill = memory.current.prefill[route.prefill] || {};
      screen = (
        <BusinessGate>
          <RecordFormScreen
            key={`${route.code}-${route.object}-${route.id || 'new'}-${route.prefill || ''}`}
            object={route.object}
            id={route.id}
            initialValues={prefill.values}
            initialLookups={prefill.lookups}
            onBack={() => back(route.id ? paths.detail(code, route.object, route.id) : paths.list(code, route.object))}
            // The saved record replaces the form in history, so Back doesn't reopen the form.
            onSaved={(row) => navigate(paths.detail(code, route.object, route.id || row?.id), { replace: true })}
          />
        </BusinessGate>
      );
      break;
    }
    case 'cards':
      tab = 'user_vault';
      screen = <SavedCardsScreen onScanNewCard={() => go(paths.scan(code || '-'))} onSelectCard={openCard} />;
      break;
    case 'card': {
      tab = 'user_vault';
      const card = savedCards.find((c) => String(c.id) === String(route.id));
      screen = card ? (
        <SavedCardDetailScreen
          key={card.id}
          card={card}
          onBack={() => back(paths.cards(code || '-'))}
          onHome={() => go(homePath)}
          onUpdated={() => {}}
          onDeleted={() => navigate(paths.cards(code || '-'), { replace: true })}
          onOpenRecord={openRecord}
        />
      ) : (
        <SavedCardsScreen onScanNewCard={() => go(paths.scan(code || '-'))} onSelectCard={openCard} />
      );
      break;
    }
    case 'scan':
      tab = 'user_scan';
      hideTabs = true;
      screen = (
        <ScanCardScreen
          onCardSaved={(card) => navigate(card?.id ? paths.card(code || '-', card.id) : paths.cards(code || '-'), { replace: true })}
          onBack={() => back(homePath)}
        />
      );
      break;
    case 'mybusiness':
      tab = 'user_crm';
      screen = <MyBusinessHubScreen onSelectBusiness={openListing} onBack={() => back(menuPath)} onNewBusiness={() => setSwitcherOpen(true)} />;
      break;
    case 'browse':
      tab = 'user_search';
      screen = <SearchScreen onSelectBusiness={openListing} />;
      break;
    case 'listing': {
      tab = 'user_search';
      const biz = memory.current.listings[route.id];
      screen = biz ? (
        <BusinessDetailsScreen
          key={route.id}
          business={biz}
          onBack={() => back(paths.browse)}
          onHome={() => go(homePath)}
          onOpenRecord={openRecord}
          onBusinessUpdated={(next) => {
            memory.current.listings[route.id] = next;
            bump((n) => n + 1);
          }}
        />
      ) : (
        <ScreenLoader message="Opening the listing…" subMessage="" />
      );
      break;
    }
    case 'profile':
      hideTabs = true;
      screen = (
        <ProfileScreen
          onNavigate={selectTab}
          onBack={() => back(homePath)}
          onSignOut={logout}
          onOpenListing={openListing}
          onOpenBusiness={(bizCode) => go(paths.home(bizCode))}
          onNewBusiness={() => setSwitcherOpen(true)}
        />
      );
      break;
    case 'support': {
      hideTabs = true;
      const ticket = memory.current.tickets[route.id];
      if (route.view === 'request') {
        screen = <SupportRequestScreen onBack={() => back(paths.support('hub'))} onViewTickets={() => go(paths.support('tickets'))} />;
      } else if (route.view === 'tickets' || (route.view === 'detail' && !ticket)) {
        screen = (
          <SupportTicketsScreen
            onBack={() => back(paths.support('hub'))}
            onNewRequest={() => go(paths.support('request'))}
            onSelectTicket={(t) => {
              memory.current.tickets[t.id] = t;
              go(paths.support('detail', t.id));
            }}
          />
        );
      } else if (route.view === 'detail') {
        screen = <SupportTicketDetailScreen ticket={ticket} onBack={() => back(paths.support('tickets'))} />;
      } else {
        screen = <SupportHubScreen onBack={() => back(paths.profile)} onNewRequest={() => go(paths.support('request'))} onMyTickets={() => go(paths.support('tickets'))} />;
      }
      break;
    }
    case 'businesses':
    case 'root':
    case 'home':
    default:
      tab = 'user_dashboard';
      screen = (
        <BusinessGate>
          <CrmHomeScreen
            key={route.code || 'home'}
            onOpenProfile={() => go(paths.profile)}
            onOpenSwitcher={() => setSwitcherOpen(true)}
            onOpenList={(object) => openList(object)}
            onCreate={(object) => openCreate(object)}
            onOpenRecord={openRecord}
            onScan={() => go(paths.scan(code))}
            onStart={(where) => {
              if (where === 'lead') openCreate('leads');
              else if (where === 'scan') go(paths.scan(code));
              else if (where === 'task') openCreate('tasks');
              else if (where === 'business') go(paths.businessProfile(code));
              else if (where === 'team') go(paths.team(code));
              else if (where === 'account') go(paths.profile);
            }}
          />
        </BusinessGate>
      );
  }

  return (
    <>
      <Layout header={null} footer={hideTabs ? null : <TabBar currentTab={tab} onSelectTab={selectTab} />}>
        <View style={styles.stack}>{screen}</View>
      </Layout>

      {/* First sign-in on this device: a short tour (once the person has a business). */}
      {businesses.length > 0 ? <WelcomeTour userId={user?.id} /> : null}

      <BusinessSwitcher
        visible={switcherOpen}
        onClose={() => setSwitcherOpen(false)}
        onSwitched={(nextCode) => go(paths.home(nextCode))}
      />

      {subscriptionOverlayOpen ? (
        <View style={styles.paywallOverlay}>
          <SubscriptionScreen onBack={closeSubscription} />
        </View>
      ) : null}
    </>
  );
}

const styles = StyleSheet.create({
  stack: { flex: 1 },
  paywallOverlay: { ...StyleSheet.absoluteFillObject, backgroundColor: colors.bgMuted, zIndex: 50 }
});
