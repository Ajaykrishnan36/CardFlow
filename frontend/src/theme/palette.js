// Preset accent colors for the user-customizable theme.
// Kept to medium/dark tones only — buttons render white text on top of
// `colors.primary`, so anything lighter fails contrast.
export const DEFAULT_PRIMARY = '#32145F';

export const THEME_PALETTE = [
  { name: 'Royal Purple', value: '#32145F' },
  { name: 'Indigo', value: '#3730A3' },
  { name: 'Sapphire', value: '#1D4ED8' },
  { name: 'Teal', value: '#0F766E' },
  { name: 'Emerald', value: '#047857' },
  { name: 'Crimson', value: '#B91C1C' },
  { name: 'Rose', value: '#9D174D' },
  { name: 'Slate', value: '#334155' },
  { name: 'Charcoal', value: '#1F2937' },
  { name: 'Bronze Gold', value: '#B89445' }
];

function hexToRgb(hex) {
  const clean = hex.replace('#', '');
  const full = clean.length === 3 ? clean.split('').map((c) => c + c).join('') : clean;
  const int = parseInt(full, 16);
  return { r: (int >> 16) & 255, g: (int >> 8) & 255, b: int & 255 };
}

function clamp(n) {
  return Math.max(0, Math.min(255, Math.round(n)));
}

function mix(hex, targetHex, amount) {
  const a = hexToRgb(hex);
  const b = hexToRgb(targetHex);
  const r = clamp(a.r + (b.r - a.r) * amount);
  const g = clamp(a.g + (b.g - a.g) * amount);
  const bl = clamp(a.b + (b.b - a.b) * amount);
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${bl.toString(16).padStart(2, '0')}`;
}

export function darken(hex, amount) {
  return mix(hex, '#000000', amount);
}

export function lighten(hex, amount) {
  return mix(hex, '#ffffff', amount);
}

export function withAlpha(hex, alpha) {
  const { r, g, b } = hexToRgb(hex);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}
