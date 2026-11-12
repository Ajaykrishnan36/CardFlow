import { useCountUp } from '../../hooks/useCountUp';
import { useInView } from '../../hooks/useInView';

/** A number that counts up when it comes into view. */
export function Counter({ to, prefix = '', suffix = '' }: { to: number; prefix?: string; suffix?: string }) {
  const [ref, inView] = useInView<HTMLSpanElement>(0.5);
  const v = useCountUp(to, inView);
  return (
    <span ref={ref} className="cw-num">
      {prefix}
      {v.toLocaleString('en-IN')}
      {suffix}
    </span>
  );
}
