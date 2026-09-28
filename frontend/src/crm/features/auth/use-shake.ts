import { useCallback, useRef } from 'react';

/** Shakes an element on demand (wrong code, unknown email) without remounting it, so input values and focus survive. */
export function useShake<T extends HTMLElement = HTMLDivElement>() {
  const ref = useRef<T>(null);
  const shake = useCallback(() => {
    const el = ref.current;
    if (!el || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
    el.classList.remove('animate-shake');
    void el.offsetWidth; // restart the animation
    el.classList.add('animate-shake');
  }, []);
  return [ref, shake] as const;
}
