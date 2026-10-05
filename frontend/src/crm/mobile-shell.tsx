import { lazy, Suspense, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { IS_NATIVE } from '@crm/api/client';
import { useSignOut } from '@crm/auth/session';
import { FullPageLoader } from '@crm/components/states';

// The phone layout is the React Native Web part of the site (src/MobileRoot.js).
// @ts-expect-error — a JavaScript module without type declarations
const MobileRoot = lazy(() => import('../MobileRoot'));

const PHONE = '(max-width: 767px)';

/** Phone-sized screen, or the native app (always the phone layout). */
export function usePhoneLayout(): boolean {
  const [phone, setPhone] = useState(() => IS_NATIVE || (window.matchMedia?.(PHONE).matches ?? false));
  useEffect(() => {
    if (IS_NATIVE || !window.matchMedia) return;
    const mq = window.matchMedia(PHONE);
    const onChange = () => setPhone(mq.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  return phone;
}

/**
 * Mounts the phone layout over the page. It lives outside the desktop CRM's scroll
 * container (a portal on <body>) so neither layout's CSS reaches the other, and inside
 * the same router and session.
 */
export function PhoneLayout({ onUnavailable }: { onUnavailable: () => void }) {
  const signOut = useSignOut();
  useEffect(() => {
    document.documentElement.classList.add('cf-phone');
    return () => document.documentElement.classList.remove('cf-phone');
  }, []);
  return createPortal(
    <div className="cf-phone-root">
      <Suspense fallback={<FullPageLoader />}>
        <MobileRoot onUnavailable={onUnavailable} onSignOut={() => void signOut()} />
      </Suspense>
    </div>,
    document.body
  );
}
