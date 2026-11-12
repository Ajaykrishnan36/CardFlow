import { useEffect } from 'react';

/**
 * Marks every [data-reveal] inside the root with data-shown once it scrolls into view. An
 * attribute, not a class: React rewrites className on re-render and would hide it again.
 * Elements that appear later (prices loaded from the server, a switched tab) are watched too.
 */
export function useReveal(root: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const calm = !('IntersectionObserver' in window) || window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const io = calm
      ? null
      : new IntersectionObserver(
          (entries) => {
            for (const e of entries) {
              if (e.isIntersecting) {
                e.target.setAttribute('data-shown', '');
                io!.unobserve(e.target);
              }
            }
          },
          { threshold: 0.12, rootMargin: '0px 0px -6% 0px' }
        );
    const watched = new WeakSet<Element>();
    const watch = () => {
      const items = Array.from(el.querySelectorAll<HTMLElement>('[data-reveal]:not([data-shown])')).filter((it) => !watched.has(it));
      // Siblings come in one after another.
      const seen = new Map<Element, number>();
      for (const it of items) {
        watched.add(it);
        const n = seen.get(it.parentElement!) ?? 0;
        it.style.setProperty('--rd', `${Math.min(n, 8) * 70}ms`);
        seen.set(it.parentElement!, n + 1);
        if (io) io.observe(it);
        else it.setAttribute('data-shown', '');
      }
    };
    watch();
    const mo = new MutationObserver(watch);
    mo.observe(el, { childList: true, subtree: true });
    return () => {
      mo.disconnect();
      io?.disconnect();
    };
  }, [root]);
}
