import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import { cn } from '@crm/lib/utils';

// The CRM's charts (D-176): small SVG components that draw themselves in — numbers count up
// from zero (or from the value they showed before), lines draw left to right, bars and the
// funnel grow, gauges sweep, heatmap cells appear in a stagger. They show only the values
// they are given; with "reduce motion" on they appear at once.

export function useReducedMotion() {
  const [reduced, setReduced] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);
  useEffect(() => {
    const m = window.matchMedia?.('(prefers-reduced-motion: reduce)');
    if (!m) return;
    const on = () => setReduced(m.matches);
    m.addEventListener?.('change', on);
    return () => m.removeEventListener?.('change', on);
  }, []);
  return reduced;
}

/** True one frame after mount, so a CSS transition runs from the start state. */
export function useEntered(delay = 0) {
  const [on, setOn] = useState(false);
  useEffect(() => {
    let raf = 0;
    const t = window.setTimeout(() => {
      raf = requestAnimationFrame(() => setOn(true));
    }, delay);
    return () => {
      window.clearTimeout(t);
      cancelAnimationFrame(raf);
    };
  }, [delay]);
  return on;
}

const easeOut = (t: number) => 1 - Math.pow(1 - t, 3);

/** A number that moves to its target: from 0 the first time, from the last value after. */
export function useAnimatedNumber(target: number, duration = 1000) {
  const reduced = useReducedMotion();
  const [shown, setShown] = useState(reduced ? target : 0);
  const from = useRef(0);
  const cur = useRef(reduced ? target : 0);
  useEffect(() => {
    if (reduced || !Number.isFinite(target)) {
      cur.current = target;
      setShown(target);
      return;
    }
    from.current = cur.current;
    const start = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const p = Math.min(1, (now - start) / duration);
      const v = from.current + (target - from.current) * easeOut(p);
      cur.current = v;
      setShown(v);
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [target, duration, reduced]);
  return shown;
}

export function AnimatedNumber({ value, format = (n) => Math.round(n).toLocaleString('en-IN'), duration = 1000, className }: { value: number; format?: (n: number) => string; duration?: number; className?: string }) {
  const n = useAnimatedNumber(value, duration);
  return <span className={cn('tabular-nums', className)}>{format(n)}</span>;
}

/** ₹1.2Cr / ₹4.5L / ₹12.3K — short money for tiles and charts. */
export function shortMoney(v: number, symbol = '₹') {
  const a = Math.abs(v);
  const sign = v < 0 ? '−' : '';
  if (a >= 1e7) return `${sign}${symbol}${(a / 1e7).toFixed(a >= 1e8 ? 1 : 2)}Cr`;
  if (a >= 1e5) return `${sign}${symbol}${(a / 1e5).toFixed(a >= 1e6 ? 1 : 2)}L`;
  if (a >= 1e3) return `${sign}${symbol}${(a / 1e3).toFixed(1)}K`;
  return `${sign}${symbol}${Math.round(a)}`;
}

// ---------------------------------------------------------------- gauge

/** A ring (full circle) or a semicircle that sweeps from 0 to value % with a glowing end. */
export function Gauge({
  value,
  size = 180,
  thickness = 12,
  semi = false,
  children,
  className,
  track = 'hsl(var(--hero-foreground) / 0.12)',
  color = 'hsl(var(--chart-1))',
  marker,
}: {
  value: number;
  size?: number;
  thickness?: number;
  semi?: boolean;
  children?: ReactNode;
  className?: string;
  track?: string;
  color?: string;
  /** A second mark on the arc, in % (e.g. the quota line). */
  marker?: number;
}) {
  const pct = Math.max(0, Math.min(100, useAnimatedNumber(value, 1500)));
  const id = useId().replace(/:/g, '');
  const r = (size - thickness) / 2 - 4;
  const cx = size / 2;
  const cy = semi ? size / 2 + 6 : size / 2;
  const sweep = semi ? 240 : 360;
  const startDeg = semi ? 150 : -90;
  const pt = (deg: number) => [cx + r * Math.cos((deg * Math.PI) / 180), cy + r * Math.sin((deg * Math.PI) / 180)] as const;
  // Drawn in two halves, so a full ring (100%) still has a path to stroke.
  const arc = (from: number, to: number) => {
    const mid = from + (to - from) / 2;
    const [x1, y1] = pt(from);
    const [xm, ym] = pt(mid);
    const [x2, y2] = pt(Math.min(to, from + 359.99));
    return `M ${x1} ${y1} A ${r} ${r} 0 0 1 ${xm} ${ym} A ${r} ${r} 0 0 1 ${x2} ${y2}`;
  };
  const end = startDeg + (sweep * pct) / 100;
  const [ex, ey] = pt(end - 0.01);
  const full = semi ? arc(startDeg, startDeg + sweep) : null;
  const h = semi ? size * 0.82 : size;
  return (
    <div className={cn('relative inline-grid place-items-center', className)} style={{ width: size, height: h }}>
      <svg width={size} height={h} viewBox={`0 0 ${size} ${h}`} className="overflow-visible" aria-hidden>
        <defs>
          <linearGradient id={`g${id}`} x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor="hsl(var(--chart-3))" />
            <stop offset="100%" stopColor={color} />
          </linearGradient>
          <filter id={`f${id}`} x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="4" />
          </filter>
        </defs>
        {semi ? (
          <path d={full!} fill="none" stroke={track} strokeWidth={thickness} strokeLinecap="round" />
        ) : (
          <circle cx={cx} cy={cy} r={r} fill="none" stroke={track} strokeWidth={thickness} />
        )}
        {semi ? (
          // tick marks around the semicircle
          Array.from({ length: 41 }).map((_, i) => {
            const d = startDeg + (sweep * i) / 40;
            const [x1, y1] = [cx + (r + thickness) * Math.cos((d * Math.PI) / 180), cy + (r + thickness) * Math.sin((d * Math.PI) / 180)];
            const [x2, y2] = [cx + (r + thickness + (i % 5 ? 3 : 6)) * Math.cos((d * Math.PI) / 180), cy + (r + thickness + (i % 5 ? 3 : 6)) * Math.sin((d * Math.PI) / 180)];
            return <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} stroke={track} strokeWidth={1.2} />;
          })
        ) : null}
        {pct > 0.2 ? <path d={arc(startDeg, end)} fill="none" stroke={`url(#g${id})`} strokeWidth={thickness} strokeLinecap="round" /> : null}
        {marker !== undefined ? (() => {
          const d = startDeg + (sweep * Math.min(100, marker)) / 100;
          const [x1, y1] = [cx + (r - thickness) * Math.cos((d * Math.PI) / 180), cy + (r - thickness) * Math.sin((d * Math.PI) / 180)];
          const [x2, y2] = [cx + (r + thickness * 1.6) * Math.cos((d * Math.PI) / 180), cy + (r + thickness * 1.6) * Math.sin((d * Math.PI) / 180)];
          return <line x1={x1} y1={y1} x2={x2} y2={y2} stroke="hsl(var(--hero-foreground))" strokeWidth={2} strokeLinecap="round" />;
        })() : null}
        {pct > 0.2 ? (
          <>
            <circle cx={ex} cy={ey} r={thickness * 0.9} fill={color} filter={`url(#f${id})`} style={{ animation: 'crm-pulse-glow 2.4s ease-in-out infinite' }} />
            <circle cx={ex} cy={ey} r={thickness * 0.42} fill="white" />
          </>
        ) : null}
      </svg>
      <div className="pointer-events-none absolute inset-0 grid place-items-center text-center">{children}</div>
    </div>
  );
}

// ---------------------------------------------------------------- funnel

export interface FunnelStage {
  key: string;
  label: string;
  count: number;
  value?: number;
}

/** Stages stacked as a glowing funnel: each band widens to its share as it appears. */
export function Funnel({ stages, height = 260, className }: { stages: FunnelStage[]; height?: number; className?: string }) {
  const on = useEntered(80);
  const reduced = useReducedMotion();
  const id = useId().replace(/:/g, '');
  const n = Math.max(1, stages.length);
  const w = 320;
  const band = height / n;
  const top = Math.max(1, ...stages.map((s) => s.count));
  // Each band's half-width: a funnel narrows down, and a bigger stage is wider.
  const half = (i: number) => {
    const shape = 1 - (i / n) * 0.78;
    const share = 0.35 + 0.65 * (stages[i] ? stages[i]!.count / top : 0);
    return (w / 2 - 6) * Math.max(0.12, shape * (0.55 + 0.45 * share));
  };
  return (
    <svg viewBox={`0 0 ${w} ${height}`} className={cn('w-full', className)} role="img" aria-label="Pipeline funnel">
      <defs>
        <linearGradient id={`fb${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="hsl(var(--chart-5))" stopOpacity="0.55" />
          <stop offset="100%" stopColor="hsl(var(--chart-1))" stopOpacity="0.9" />
        </linearGradient>
        <filter id={`fg${id}`} x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur stdDeviation="6" />
        </filter>
      </defs>
      {stages.map((s, i) => {
        const y = i * band;
        const a = half(i);
        const b = i + 1 < n ? half(i + 1) : a * 0.75;
        const cx = w / 2;
        const d = `M ${cx - a} ${y + 3} L ${cx + a} ${y + 3} L ${cx + b} ${y + band - 3} L ${cx - b} ${y + band - 3} Z`;
        return (
          <g
            key={s.key}
            style={{
              transformOrigin: `${cx}px ${y + band / 2}px`,
              transform: on || reduced ? 'scaleX(1)' : 'scaleX(0.05)',
              opacity: on || reduced ? 1 : 0,
              transition: `transform 900ms cubic-bezier(0.16,1,0.3,1) ${i * 110}ms, opacity 500ms ease ${i * 110}ms`,
            }}
          >
            <path d={d} fill={`url(#fb${id})`} filter={`url(#fg${id})`} opacity={0.5} />
            <path d={d} fill={`url(#fb${id})`} stroke="hsl(var(--chart-3) / 0.7)" strokeWidth={1} />
            <ellipse cx={cx} cy={y + 3} rx={a} ry={4} fill="none" stroke="hsl(var(--chart-3) / 0.9)" strokeWidth={1.2} />
          </g>
        );
      })}
    </svg>
  );
}

// ---------------------------------------------------------------- line / area chart

export interface Series {
  key: string;
  label: string;
  color: string;
  points: Array<number | null>;
  /** Points from this index on are a projection (dashed). */
  dashedFrom?: number;
  area?: boolean;
}

export function LineChart({
  series,
  labels,
  height = 220,
  format = (n: number) => String(Math.round(n)),
  reference,
  className,
}: {
  series: Series[];
  labels: string[];
  height?: number;
  format?: (n: number) => string;
  reference?: { value: number; label: string };
  className?: string;
}) {
  const on = useEntered(60);
  const reduced = useReducedMotion();
  const id = useId().replace(/:/g, '');
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<SVGSVGElement>(null);
  const w = 640;
  const pad = { l: 44, r: 12, t: 12, b: 24 };
  const all = series.flatMap((s) => s.points.filter((p): p is number => p !== null));
  const max = Math.max(1, reference?.value ?? 0, ...all) * 1.08;
  const nPts = Math.max(2, labels.length);
  const x = (i: number) => pad.l + ((w - pad.l - pad.r) * i) / (nPts - 1);
  const y = (v: number) => pad.t + (height - pad.t - pad.b) * (1 - v / max);
  const line = (pts: Array<number | null>, from = 0, to = pts.length) => {
    let d = '';
    for (let i = from; i < to; i++) {
      const p = pts[i];
      if (p === null || p === undefined) continue;
      d += `${d ? 'L' : 'M'} ${x(i).toFixed(1)} ${y(p).toFixed(1)} `;
    }
    return d;
  };
  const ticks = 4;
  const onMove = (e: React.MouseEvent) => {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    const px = ((e.clientX - r.left) / r.width) * w;
    const i = Math.round(((px - pad.l) / (w - pad.l - pad.r)) * (nPts - 1));
    setHover(Math.max(0, Math.min(nPts - 1, i)));
  };
  const drawn = on || reduced;
  return (
    <div className={cn('relative', className)}>
      <svg ref={ref} viewBox={`0 0 ${w} ${height}`} className="w-full overflow-visible" onMouseMove={onMove} onMouseLeave={() => setHover(null)} role="img" aria-label="Trend chart">
        <defs>
          {series.map((s) => (
            <linearGradient key={s.key} id={`a${id}${s.key}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={s.color} stopOpacity="0.28" />
              <stop offset="100%" stopColor={s.color} stopOpacity="0" />
            </linearGradient>
          ))}
          <clipPath id={`c${id}`}>
            <rect x={0} y={0} height={height} width={drawn ? w : 0} style={{ transition: 'width 1400ms cubic-bezier(0.65,0,0.35,1)' }} />
          </clipPath>
        </defs>
        {Array.from({ length: ticks + 1 }).map((_, i) => {
          const v = (max / ticks) * i;
          return (
            <g key={i}>
              <line x1={pad.l} x2={w - pad.r} y1={y(v)} y2={y(v)} stroke="hsl(var(--border))" strokeWidth={1} />
              <text x={pad.l - 8} y={y(v) + 3} textAnchor="end" className="fill-muted-foreground" fontSize="10">{format(v)}</text>
            </g>
          );
        })}
        {labels.map((l, i) => (nPts <= 14 || i % Math.ceil(nPts / 8) === 0 ? (
          <text key={i} x={x(i)} y={height - 6} textAnchor="middle" className={cn('fill-muted-foreground', hover === i && 'fill-foreground font-semibold')} fontSize="10">{l}</text>
        ) : null))}
        {reference ? (
          <g>
            <line x1={pad.l} x2={w - pad.r} y1={y(reference.value)} y2={y(reference.value)} stroke="hsl(var(--foreground) / 0.55)" strokeDasharray="5 5" strokeWidth={1.2} />
            <text x={w - pad.r} y={y(reference.value) - 6} textAnchor="end" className="fill-foreground" fontSize="10" fontWeight="600">{reference.label}</text>
          </g>
        ) : null}
        <g clipPath={`url(#c${id})`}>
          {series.map((s) => {
            const solidTo = s.dashedFrom ?? s.points.length;
            const d = line(s.points, 0, solidTo);
            const last = [...s.points.slice(0, solidTo)].map((p, i) => (p === null ? -1 : i)).filter((i) => i >= 0).pop() ?? 0;
            const area = s.area && d ? `${d} L ${x(last)} ${y(0)} L ${x(s.points.findIndex((p) => p !== null))} ${y(0)} Z` : '';
            return (
              <g key={s.key}>
                {area ? <path d={area} fill={`url(#a${id}${s.key})`} /> : null}
                {d ? <path d={d} fill="none" stroke={s.color} strokeWidth={2.4} strokeLinejoin="round" strokeLinecap="round" /> : null}
                {s.dashedFrom !== undefined ? <path d={line(s.points, Math.max(0, s.dashedFrom - 1))} fill="none" stroke={s.color} strokeWidth={2.2} strokeDasharray="6 6" /> : null}
              </g>
            );
          })}
        </g>
        {hover !== null ? (
          <g>
            <line x1={x(hover)} x2={x(hover)} y1={pad.t} y2={height - pad.b} stroke="hsl(var(--muted-foreground) / 0.4)" />
            {series.map((s) => {
              const p = s.points[hover];
              return p === null || p === undefined ? null : <circle key={s.key} cx={x(hover)} cy={y(p)} r={4} fill="hsl(var(--card))" stroke={s.color} strokeWidth={2} />;
            })}
          </g>
        ) : null}
      </svg>
      {hover !== null ? (
        <div
          className="pointer-events-none absolute top-1 z-10 min-w-[140px] rounded-lg border bg-popover px-3 py-2 text-xs shadow-pop animate-fade-in"
          style={{ left: `calc(${(x(hover) / w) * 100}% ${x(hover) / w > 0.6 ? '- 150px' : '+ 10px'})` }}
        >
          <p className="mb-1 font-semibold text-foreground">{labels[hover]}</p>
          {series.map((s) => {
            const p = s.points[hover];
            return p === null || p === undefined ? null : (
              <p key={s.key} className="flex items-center justify-between gap-3">
                <span className="flex items-center gap-1.5 text-muted-foreground"><span className="size-2 rounded-full" style={{ background: s.color }} />{s.label}</span>
                <span className="font-medium tabular-nums text-foreground">{format(p)}</span>
              </p>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------- bars

export function BarChart({ bars, height = 180, format = (n: number) => String(Math.round(n)), color = 'hsl(var(--chart-1))', className }: { bars: Array<{ label: string; value: number }>; height?: number; format?: (n: number) => string; color?: string; className?: string }) {
  const on = useEntered(60);
  const reduced = useReducedMotion();
  const max = Math.max(1, ...bars.map((b) => b.value));
  return (
    <div className={cn('flex items-end gap-2', className)} style={{ height }}>
      {bars.map((b, i) => (
        <div key={b.label + i} className="group flex min-w-0 flex-1 flex-col items-center gap-1.5">
          <span className="text-[10px] font-medium tabular-nums text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100">{format(b.value)}</span>
          <div className="relative w-full flex-1">
            <div
              className="absolute inset-x-0 bottom-0 rounded-t-md"
              style={{
                height: `${(b.value / max) * 100}%`,
                minHeight: b.value > 0 ? 3 : 0,
                background: `linear-gradient(to top, ${color}, hsl(var(--chart-3)))`,
                transformOrigin: 'bottom',
                transform: on || reduced ? 'scaleY(1)' : 'scaleY(0)',
                transition: `transform 900ms cubic-bezier(0.16,1,0.3,1) ${i * 60}ms`,
              }}
            />
          </div>
          <span className="w-full truncate text-center text-[10px] text-muted-foreground">{b.label}</span>
        </div>
      ))}
    </div>
  );
}

/** Small bars for a tile (e.g. win rate by week); the last one stands out. */
export function MiniBars({ values, className }: { values: number[]; className?: string }) {
  const on = useEntered(120);
  const reduced = useReducedMotion();
  const max = Math.max(1, ...values);
  return (
    <div className={cn('flex h-9 items-end gap-[3px]', className)} aria-hidden>
      {values.map((v, i) => (
        <span
          key={i}
          className={cn('flex-1 rounded-sm', i === values.length - 1 ? 'bg-primary' : 'bg-muted-foreground/20')}
          style={{
            height: `${Math.max(8, (v / max) * 100)}%`,
            transformOrigin: 'bottom',
            transform: on || reduced ? 'scaleY(1)' : 'scaleY(0)',
            transition: `transform 700ms cubic-bezier(0.16,1,0.3,1) ${i * 40}ms`,
          }}
        />
      ))}
    </div>
  );
}

/** Floating bars that stack up to a total: closed, then commit, best case, pipeline. */
export function Waterfall({
  steps,
  height = 240,
  format,
  reference,
}: {
  steps: Array<{ key: string; label: string; sub?: string; from: number; to: number; color: string; striped?: boolean; ghost?: boolean }>;
  height?: number;
  format: (n: number) => string;
  reference?: { value: number; label: string };
}) {
  const on = useEntered(80);
  const reduced = useReducedMotion();
  const id = useId().replace(/:/g, '');
  const max = Math.max(1, reference?.value ?? 0, ...steps.map((s) => s.to)) * 1.15;
  const w = 560;
  const pad = { l: 48, r: 12, t: 26, b: 44 };
  const ih = height - pad.t - pad.b;
  const col = (w - pad.l - pad.r) / Math.max(1, steps.length);
  const y = (v: number) => pad.t + ih * (1 - v / max);
  return (
    <svg viewBox={`0 0 ${w} ${height}`} className="w-full overflow-visible" role="img" aria-label="Path to target">
      <defs>
        <pattern id={`s${id}`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
          <rect width="6" height="6" fill="hsl(var(--chart-1) / 0.25)" />
          <line x1="0" y1="0" x2="0" y2="6" stroke="hsl(var(--chart-1))" strokeWidth="2.5" />
        </pattern>
      </defs>
      {Array.from({ length: 5 }).map((_, i) => {
        const v = (max / 4) * i;
        return (
          <g key={i}>
            <line x1={pad.l} x2={w - pad.r} y1={y(v)} y2={y(v)} stroke="hsl(var(--border))" />
            <text x={pad.l - 8} y={y(v) + 3} textAnchor="end" fontSize="10" className="fill-muted-foreground">{format(v)}</text>
          </g>
        );
      })}
      {reference ? (
        <g>
          <line x1={pad.l} x2={w - pad.r} y1={y(reference.value)} y2={y(reference.value)} stroke="hsl(var(--foreground) / 0.5)" strokeDasharray="4 4" />
          <rect x={w - pad.r - 92} y={y(reference.value) - 10} width={92} height={20} rx={10} fill="hsl(var(--navy))" />
          <text x={w - pad.r - 46} y={y(reference.value) + 4} textAnchor="middle" fontSize="10" fontWeight="600" fill="white">{reference.label}</text>
        </g>
      ) : null}
      {steps.map((s, i) => {
        const x = pad.l + col * i + col * 0.25;
        const bw = col * 0.5;
        const top = y(s.to);
        const bottom = y(s.from);
        const hgt = Math.max(2, bottom - top);
        return (
          <g key={s.key}>
            <rect
              x={x}
              y={top}
              width={bw}
              height={hgt}
              rx={6}
              fill={s.striped ? `url(#s${id})` : s.ghost ? 'hsl(var(--muted))' : s.color}
              stroke={s.ghost ? 'hsl(var(--border))' : 'none'}
              style={{
                transformOrigin: `${x + bw / 2}px ${bottom}px`,
                transform: on || reduced ? 'scaleY(1)' : 'scaleY(0)',
                transition: `transform 900ms cubic-bezier(0.16,1,0.3,1) ${i * 180}ms`,
              }}
            />
            {i > 0 ? <line x1={x - col * 0.5} x2={x} y1={bottom} y2={bottom} stroke="hsl(var(--muted-foreground) / 0.4)" strokeDasharray="3 3" /> : null}
            <text x={x + bw / 2} y={top - 8} textAnchor="middle" fontSize="12" fontWeight="700" className="fill-foreground" style={{ opacity: on || reduced ? 1 : 0, transition: `opacity 400ms ease ${i * 180 + 600}ms` }}>
              {format(s.to)}
            </text>
            <text x={x + bw / 2} y={height - 24} textAnchor="middle" fontSize="11" fontWeight="600" className="fill-foreground">{s.label}</text>
            {s.sub ? <text x={x + bw / 2} y={height - 10} textAnchor="middle" fontSize="10" className="fill-muted-foreground">{s.sub}</text> : null}
          </g>
        );
      })}
    </svg>
  );
}

// ---------------------------------------------------------------- small pieces

export function Sparkline({ points, color = 'hsl(var(--chart-1))', width = 120, height = 32, className }: { points: number[]; color?: string; width?: number; height?: number; className?: string }) {
  const on = useEntered(100);
  const reduced = useReducedMotion();
  const id = useId().replace(/:/g, '');
  if (points.length < 2) return <svg width={width} height={height} className={className} aria-hidden />;
  const max = Math.max(...points);
  const min = Math.min(...points);
  const span = max - min || 1;
  const pts = points.map((p, i) => [(width - 4) * (i / (points.length - 1)) + 2, height - 4 - ((p - min) / span) * (height - 8)] as const);
  const d = pts.map(([x, y], i) => `${i ? 'L' : 'M'} ${x.toFixed(1)} ${y.toFixed(1)}`).join(' ');
  const [lx, ly] = pts[pts.length - 1]!;
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cn('overflow-visible', className)} aria-hidden>
      <defs>
        <linearGradient id={`sp${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.25" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${d} L ${lx} ${height} L 2 ${height} Z`} fill={`url(#sp${id})`} style={{ opacity: on || reduced ? 1 : 0, transition: 'opacity 900ms ease 500ms' }} />
      <path d={d} fill="none" stroke={color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" pathLength={1} strokeDasharray={1}
        style={{ strokeDashoffset: on || reduced ? 0 : 1, transition: 'stroke-dashoffset 1200ms cubic-bezier(0.65,0,0.35,1)' }} />
      <circle cx={lx} cy={ly} r={2.6} fill={color} style={{ opacity: on || reduced ? 1 : 0, transition: 'opacity 300ms ease 1100ms' }} />
    </svg>
  );
}

/** A small ring with a score in it: green when healthy, amber to watch, red at risk. */
export function HealthRing({ score, size = 38 }: { score: number; size?: number }) {
  const v = useAnimatedNumber(score, 1200);
  const tone = score >= 70 ? 'hsl(var(--success))' : score >= 50 ? 'hsl(var(--warning))' : 'hsl(var(--danger))';
  const r = size / 2 - 3;
  const c = 2 * Math.PI * r;
  return (
    <span className="relative inline-grid place-items-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="hsl(var(--muted))" strokeWidth={3} />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={tone} strokeWidth={3} strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - Math.max(0, Math.min(100, v)) / 100)} />
      </svg>
      <span className="absolute text-[11px] font-semibold tabular-nums text-foreground">{Math.round(v)}</span>
    </span>
  );
}

export function healthLabel(score: number) {
  return score >= 70 ? { label: 'Healthy', tone: 'text-success' } : score >= 50 ? { label: 'Watch', tone: 'text-warning' } : { label: 'At risk', tone: 'text-danger' };
}

export function ProgressBar({ value, max = 100, className, color = 'hsl(var(--chart-1))', marker }: { value: number; max?: number; className?: string; color?: string; marker?: number }) {
  const on = useEntered(80);
  const reduced = useReducedMotion();
  const pct = Math.max(0, Math.min(100, (value / (max || 1)) * 100));
  return (
    <div className={cn('relative h-2 overflow-hidden rounded-full bg-muted', className)}>
      <div className="h-full rounded-full" style={{ width: on || reduced ? `${pct}%` : '0%', background: color, transition: 'width 1100ms cubic-bezier(0.16,1,0.3,1)' }} />
      {marker !== undefined ? <span className="absolute inset-y-[-2px] w-0.5 rounded bg-foreground/70" style={{ left: `${Math.min(100, marker)}%` }} /> : null}
    </div>
  );
}

/** Weeks × days of activity, each cell coloured by how much happened. */
export function Heatmap({ cells, rows = 7, className }: { cells: number[]; rows?: number; className?: string }) {
  const on = useEntered(120);
  const reduced = useReducedMotion();
  const max = Math.max(1, ...cells);
  const cols = Math.ceil(cells.length / rows);
  const grid = useMemo(() => Array.from({ length: cols }, (_, c) => cells.slice(c * rows, c * rows + rows)), [cells, cols, rows]);
  return (
    <div className={cn('flex gap-[3px]', className)} aria-hidden>
      {grid.map((col, c) => (
        <div key={c} className="flex flex-1 flex-col gap-[3px]">
          {col.map((v, r) => (
            <span
              key={r}
              className="aspect-square rounded-[3px]"
              style={{
                background: v ? `hsl(var(--chart-1) / ${0.18 + 0.82 * (v / max)})` : 'hsl(var(--hero-foreground) / 0.07)',
                opacity: on || reduced ? 1 : 0,
                transform: on || reduced ? 'none' : 'scale(0.6)',
                transition: `opacity 320ms ease ${(c * rows + r) * 6}ms, transform 320ms ease ${(c * rows + r) * 6}ms`,
              }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

/** Percent change against the period before: "↑ 18%" in green, "↓ 6%" in red. */
export function Delta({ now, before, className, invert }: { now: number; before: number; className?: string; invert?: boolean }) {
  if (!before && !now) return <span className={cn('text-[11px] text-muted-foreground', className)}>—</span>;
  if (!before) return <span className={cn('text-[11px] font-semibold text-success', className)}>new</span>;
  const pct = ((now - before) / Math.abs(before)) * 100;
  const up = pct >= 0;
  const good = invert ? !up : up;
  return (
    <span className={cn('inline-flex items-center gap-0.5 text-[11px] font-semibold tabular-nums', good ? 'text-success' : 'text-danger', className)}>
      {up ? '↑' : '↓'} {Math.abs(pct) >= 100 ? Math.round(Math.abs(pct)) : Math.abs(pct).toFixed(Math.abs(pct) < 10 ? 1 : 0)}%
    </span>
  );
}
