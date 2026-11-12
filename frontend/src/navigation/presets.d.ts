export interface ListPreset {
  label: string;
  singular: string;
  field: string;
  value: string;
  query: string;
  in: string[];
  filter: { field: string; op: 'in'; value: string[] };
  create: Record<string, unknown>;
  /** The list's columns while the preset is on (else the object's own). */
  columns?: string[];
}
export function listPreset(object: string, search: string | URLSearchParams): ListPreset | null;
