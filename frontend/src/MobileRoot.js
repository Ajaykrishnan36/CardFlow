import React, { useEffect } from 'react';
import { AuthProvider } from './context/AuthContext';
import { CrmProvider } from './context/CrmContext';
import { AppNavigator } from './navigation/AppNavigator';
import { initPushNotifications } from './utils/pushNotifications';

// The phone layout of the site. It is mounted by the one app shell (src/crm/app.tsx)
// inside its router, after sign-in, when the screen is phone-sized or this is the
// native app. Sign-in itself is the shared sign-in page.
export default function MobileRoot({ onUnavailable, onSignOut }) {
  useEffect(() => {
    // Native shell only (no-op on web): asks for notification permission.
    initPushNotifications();
  }, []);
  return (
    <AuthProvider onUnavailable={onUnavailable} onSignOut={onSignOut}>
      <CrmProvider>
        <AppNavigator />
      </CrmProvider>
    </AuthProvider>
  );
}
