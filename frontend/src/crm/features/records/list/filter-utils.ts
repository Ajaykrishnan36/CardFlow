import type { FieldDef } from '@crm/api/types';
import { isGroup, type FilterCondition, type FilterGroup, type FilterNode, type FilterOp } from '@crm/api/types-features';

// What each kind of field can be compared with, and what value the comparison needs.

export type ValueKind = 'none' | 'text' | 'number' | 'numberPair' | 'date' | 'datePair' | 'days' | 'option' | 'options' | 'lookup' | 'bool';

const TEXT_OPS: FilterOp[] = ['contains', 'notContains', 'eq', 'neq', 'startsWith', 'empty', 'notEmpty'];
const NUMBER_OPS: FilterOp[] = ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between', 'empty', 'notEmpty'];
const DATE_OPS: FilterOp[] = ['on', 'before', 'after', 'between', 'today', 'yesterday', 'tomorrow', 'overdue', 'lastDays', 'nextDays',
  'thisWeek', 'lastWeek', 'thisMonth', 'lastMonth', 'thisQuarter', 'thisYear', 'empty', 'notEmpty'];

export function opsFor(f: FieldDef): FilterOp[] {
  switch (f.type) {
    case 'number':
    case 'currency':
    case 'percent':
    case 'rating':
      return NUMBER_OPS;
    case 'date':
    case 'datetime':
      return DATE_OPS;
    case 'boolean':
      return ['eq'];
    case 'select':
      return ['eq', 'neq', 'in', 'notIn', 'empty', 'notEmpty'];
    case 'multiselect':
      return ['contains', 'all', 'notIn', 'empty', 'notEmpty'];
    case 'lookup':
      return f.lookup === 'users' ? ['isMe', 'notMe', 'myTeam', 'eq', 'neq', 'empty', 'notEmpty'] : ['eq', 'neq', 'empty', 'notEmpty'];
    case 'relations':
      return ['contains', 'notIn', 'empty', 'notEmpty'];
    case 'files':
      return ['notEmpty', 'empty'];
    default:
      return TEXT_OPS;
  }
}

export function valueKindFor(f: FieldDef, op: FilterOp): ValueKind {
  if (['empty', 'notEmpty', 'today', 'yesterday', 'tomorrow', 'overdue', 'thisWeek', 'lastWeek', 'thisMonth', 'lastMonth', 'thisQuarter', 'thisYear', 'isMe', 'notMe', 'myTeam'].includes(op)) {
    return 'none';
  }
  if (op === 'lastDays' || op === 'nextDays') return 'days';
  switch (f.type) {
    case 'number':
    case 'currency':
    case 'percent':
    case 'rating':
      return op === 'between' ? 'numberPair' : 'number';
    case 'date':
    case 'datetime':
      return op === 'between' ? 'datePair' : 'date';
    case 'boolean':
      return 'bool';
    case 'select':
      return op === 'eq' || op === 'neq' ? 'option' : 'options';
    case 'multiselect':
      return 'options';
    case 'lookup':
      return 'lookup';
    case 'relations':
      return 'lookup';
  }
  return 'text';
}

export function defaultValue(kind: ValueKind): unknown {
  switch (kind) {
    case 'days':
      return 7;
    case 'bool':
      return true;
    case 'options':
      return [];
    case 'numberPair':
    case 'datePair':
      return ['', ''];
    case 'none':
      return undefined;
  }
  return '';
}

/** A fresh condition on a field (its first comparison). */
export function newCondition(f: FieldDef): FilterCondition {
  const op = opsFor(f)[0]!;
  return { field: f.key, op, value: defaultValue(valueKindFor(f, op)) };
}

export function emptyGroup(op: 'and' | 'or' = 'and'): FilterGroup {
  return { op, filters: [] };
}

/** Is the condition filled in enough to send? */
export function conditionReady(c: FilterCondition, f: FieldDef | undefined): boolean {
  if (!f) return false;
  const k = valueKindFor(f, c.op);
  const v = c.value;
  switch (k) {
    case 'none':
    case 'bool':
      return true;
    case 'options':
      return Array.isArray(v) && v.length > 0;
    case 'numberPair':
    case 'datePair':
      return Array.isArray(v) && v.length === 2 && v.every((x) => x !== '' && x !== null && x !== undefined);
    default:
      return v !== '' && v !== null && v !== undefined;
  }
}

/** Drops unfinished conditions and empty groups so the server only gets complete ones. */
export function cleanFilter(g: FilterGroup | undefined, byKey: Map<string, FieldDef>): FilterGroup | undefined {
  if (!g) return undefined;
  const walk = (n: FilterNode): FilterNode | null => {
    if (isGroup(n)) {
      const kids = n.filters.map(walk).filter((x): x is FilterNode => x !== null);
      return kids.length ? { op: n.op, filters: kids } : null;
    }
    const f = byKey.get(n.field);
    if (!conditionReady(n, f)) return null;
    // Select "in" with one value reads more naturally as "is"; the server treats both alike.
    return { ...n };
  };
  const out = walk(g);
  return out && isGroup(out) ? out : out ? { op: 'and', filters: [out] } : undefined;
}

export function countConditions(g: FilterGroup | undefined): number {
  if (!g) return 0;
  return g.filters.reduce((n, f) => n + (isGroup(f) ? countConditions(f) : 1), 0);
}

/** Adds a quick condition ("status is X") to a filter, e.g. from clicking a board column. */
export function withCondition(g: FilterGroup | undefined, c: FilterCondition): FilterGroup {
  const base = g ?? emptyGroup('and');
  if (base.op === 'and') return { op: 'and', filters: [...base.filters, c] };
  return { op: 'and', filters: [base, c] };
}
