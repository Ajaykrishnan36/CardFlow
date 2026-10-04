import React, { createElement, useEffect, useMemo, useRef, useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, TextInput, Modal, Switch, KeyboardAvoidingView, Platform } from 'react-native';
import { ArrowLeft, ChevronDown, Search, X, Check } from 'lucide-react';
import { colors, spacing, radii, typography } from '../../theme';
import { Button } from '../../components/Button';
import { useCrm } from '../../context/CrmContext';
import { crmApi, errorText, newIdempotencyKey } from '../../services/crmApi';
import { Loading, ErrorBox, humanize } from './ui';

// One form for every object, built from the object's metadata (fields, types, options,
// lookups, required). The server validates again; its field messages are shown inline.

const EDITABLE = new Set(['text', 'textarea', 'richtext', 'email', 'phone', 'url', 'number', 'currency', 'percent', 'rating', 'date', 'datetime', 'select', 'multiselect', 'boolean', 'lookup']);
const SYSTEM = new Set(['code', 'createdBy', 'createdAt', 'updatedBy', 'updatedAt']);

function stripHtml(s) {
  return String(s || '').replace(/<br\s*\/?>/gi, '\n').replace(/<\/p>/gi, '\n').replace(/<[^>]+>/g, '').replace(/&nbsp;/g, ' ').replace(/&amp;/g, '&').trim();
}

function toLocalInput(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** The platform's own date / date-time picker (the app runs in a web view). */
function DateInput({ type, value, onChange }) {
  return createElement('input', {
    type: type === 'datetime' ? 'datetime-local' : 'date',
    value: value || '',
    onChange: (e) => onChange(e.target.value),
    style: {
      width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radii.input, padding: '11px 12px',
      fontSize: 14, color: colors.textPrimary, backgroundColor: colors.bgCard, fontFamily: 'inherit', outline: 'none'
    }
  });
}

function OptionSheet({ visible, title, options, value, multiple, onChange, onClose }) {
  const selected = multiple ? new Set(value || []) : null;
  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <TouchableOpacity onPress={onClose}>
              <Text style={styles.sheetDone}>Done</Text>
            </TouchableOpacity>
          </View>
          <ScrollView style={{ maxHeight: 420 }}>
            {!multiple ? (
              <TouchableOpacity style={styles.option} onPress={() => { onChange(''); onClose(); }}>
                <Text style={[styles.optionText, { color: colors.textMuted }]}>None</Text>
              </TouchableOpacity>
            ) : null}
            {options.map((o) => {
              const on = multiple ? selected.has(o.value) : value === o.value;
              return (
                <TouchableOpacity
                  key={o.value}
                  style={styles.option}
                  onPress={() => {
                    if (multiple) {
                      const next = new Set(selected);
                      if (on) next.delete(o.value); else next.add(o.value);
                      onChange([...next]);
                    } else {
                      onChange(o.value);
                      onClose();
                    }
                  }}
                >
                  <Text style={[styles.optionText, on && { color: colors.primary, fontWeight: '700' }]}>{o.label}</Text>
                  {on ? <Check size={16} color={colors.primary} /> : null}
                </TouchableOpacity>
              );
            })}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function LookupSheet({ visible, title, target, onPick, onClose }) {
  const { activeCode } = useCrm();
  const [q, setQ] = useState('');
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const seq = useRef(0);
  useEffect(() => {
    if (!visible) return undefined;
    const mine = ++seq.current;
    const t = setTimeout(() => {
      crmApi
        .lookup(activeCode, target, q.trim())
        .then((r) => {
          if (mine === seq.current) {
            setRows(r);
            setError('');
          }
        })
        .catch((e) => mine === seq.current && setError(e.message));
    }, 200);
    return () => clearTimeout(t);
  }, [visible, q, target, activeCode]);
  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <TouchableOpacity onPress={onClose}>
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          <View style={styles.lookupSearch}>
            <Search size={16} color={colors.textMuted} />
            <TextInput style={styles.lookupInput} value={q} onChangeText={setQ} placeholder="Search" placeholderTextColor={colors.textMuted} autoFocus autoCapitalize="none" />
          </View>
          <ScrollView style={{ maxHeight: 360 }} keyboardShouldPersistTaps="handled">
            {error ? <Text style={styles.sheetNote}>{error}</Text> : null}
            {!rows && !error ? <Text style={styles.sheetNote}>Searching…</Text> : null}
            {rows && rows.length === 0 ? <Text style={styles.sheetNote}>Nothing found.</Text> : null}
            <TouchableOpacity style={styles.option} onPress={() => onPick(null)}>
              <Text style={[styles.optionText, { color: colors.textMuted }]}>None</Text>
            </TouchableOpacity>
            {(rows || []).map((r) => (
              <TouchableOpacity key={r.id} style={styles.option} onPress={() => onPick(r)}>
                <Text style={styles.optionText} numberOfLines={1}>{r.label}</Text>
              </TouchableOpacity>
            ))}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

/**
 * Create or edit a record.
 * - `id` set: edit (loads the record, sends only what changed, with its version).
 * - `initialValues` / `initialLookups`: prefill a new record (e.g. a task for this lead).
 */
export function RecordFormScreen({ object, id, initialValues, initialLookups, onBack, onSaved }) {
  const { activeCode } = useCrm();
  const [meta, setMeta] = useState(null);
  const [record, setRecord] = useState(null);
  const [values, setValues] = useState({});
  const [labels, setLabels] = useState({});
  const [errors, setErrors] = useState({});
  const [alert, setAlert] = useState('');
  const [loadError, setLoadError] = useState('');
  const [saving, setSaving] = useState(false);
  const [sheet, setSheet] = useState(null); // field key whose picker is open
  const idem = useRef(newIdempotencyKey());

  const load = async () => {
    setLoadError('');
    try {
      const [m, d] = await Promise.all([crmApi.meta(activeCode, object), id ? crmApi.get(activeCode, object, id) : Promise.resolve(null)]);
      setMeta(m);
      if (d) {
        setRecord(d.record);
        const v = {};
        const l = {};
        m.fields.forEach((f) => {
          let x = d.record.values?.[f.key];
          if (f.type === 'richtext') x = stripHtml(x);
          if (f.type === 'datetime') x = toLocalInput(x);
          if (f.type === 'date' && typeof x === 'string') x = x.slice(0, 10);
          v[f.key] = x ?? (f.type === 'multiselect' ? [] : f.type === 'boolean' ? false : '');
          if (f.type === 'lookup' && d.record.lookups?.[f.key]) l[f.key] = d.record.lookups[f.key].label;
        });
        setValues(v);
        setLabels(l);
      } else {
        setValues({ ...(initialValues || {}) });
        setLabels({ ...(initialLookups || {}) });
      }
    } catch (e) {
      setLoadError(e.message || 'Could not open the form.');
    }
  };
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeCode, object, id]);

  // Fields in page-layout order, required ones first within the form's first group.
  const groups = useMemo(() => {
    if (!meta) return [];
    const byKey = Object.fromEntries(meta.fields.map((f) => [f.key, f]));
    const usable = (f) => f && !f.readOnly && !SYSTEM.has(f.key) && EDITABLE.has(f.type) && f.key !== 'ownerId' && !(f.type === 'lookup' && (f.lookup === 'users' || f.lookup === 'products' || f.lookup === 'workspaces'));
    const seen = new Set();
    const out = [];
    (meta.layout?.sections || []).forEach((s) => {
      if (s.id === 'system' || s.id === 'conversion' || s.id === 'access') return;
      const fields = s.fields.map((k) => byKey[k]).filter((f) => usable(f) && !seen.has(f.key));
      fields.forEach((f) => seen.add(f.key));
      if (fields.length) out.push({ title: s.title, fields });
    });
    const rest = meta.fields.filter((f) => usable(f) && !seen.has(f.key) && f.required);
    if (rest.length) out.unshift({ title: 'Required', fields: rest });
    return out;
  }, [meta]);

  const set = (key, v) => {
    setValues((prev) => ({ ...prev, [key]: v }));
    if (errors[key]) setErrors((e) => ({ ...e, [key]: undefined }));
  };

  const outgoing = (f) => {
    const v = values[f.key];
    if (f.type === 'number' || f.type === 'currency' || f.type === 'percent' || f.type === 'rating') {
      if (v === '' || v === null || v === undefined) return null;
      const n = Number(String(v).replace(/,/g, ''));
      return Number.isNaN(n) ? v : n;
    }
    if (f.type === 'datetime') return v ? new Date(v).toISOString() : null;
    if (f.type === 'boolean') return Boolean(v);
    if (f.type === 'multiselect') return v || [];
    if (v === '' || v === undefined) return null;
    return v;
  };

  const save = async () => {
    if (saving) return;
    const fe = {};
    const body = {};
    groups.forEach((g) =>
      g.fields.forEach((f) => {
        const out = outgoing(f);
        if (f.required && (out === null || out === '' || (Array.isArray(out) && out.length === 0))) fe[f.key] = `Enter ${f.label.toLowerCase()}.`;
        if (id) {
          let before = record?.values?.[f.key] ?? null;
          if (f.type === 'richtext') before = stripHtml(before) || null;
          if (f.type === 'datetime' && before) before = new Date(before).toISOString();
          if (f.type === 'date' && typeof before === 'string') before = before.slice(0, 10);
          if (JSON.stringify(before ?? null) !== JSON.stringify(out ?? null) && !(before == null && (out === false || (Array.isArray(out) && out.length === 0)))) body[f.key] = out;
        } else if (out !== null && !(Array.isArray(out) && out.length === 0) && out !== false) {
          body[f.key] = out;
        }
      })
    );
    setErrors(fe);
    setAlert('');
    if (Object.keys(fe).length) {
      setAlert('Fill in the highlighted fields.');
      return;
    }
    if (id && Object.keys(body).length === 0) {
      onSaved?.(record);
      return;
    }
    setSaving(true);
    try {
      const row = id ? await crmApi.update(activeCode, object, id, body, record.version) : await crmApi.create(activeCode, object, body, idem.current);
      onSaved?.(row);
    } catch (e) {
      setErrors(e.fieldErrors || {});
      setAlert(e.status === 409 ? 'Someone else changed this record. Go back and open it again.' : errorText(e));
      setSaving(false);
    }
  };

  const renderField = (f) => {
    const v = values[f.key];
    const err = errors[f.key];
    let control;
    if (f.type === 'select' || f.type === 'multiselect') {
      const multi = f.type === 'multiselect';
      const shown = multi ? (v || []).map((x) => f.options?.find((o) => o.value === x)?.label || x).join(', ') : f.options?.find((o) => o.value === v)?.label || (v ? humanize(v) : '');
      control = (
        <TouchableOpacity style={[styles.input, styles.picker, err && styles.inputError]} onPress={() => setSheet(f.key)}>
          <Text style={[styles.pickerText, !shown && { color: colors.textMuted }]} numberOfLines={1}>{shown || 'Choose…'}</Text>
          <ChevronDown size={16} color={colors.textMuted} />
        </TouchableOpacity>
      );
    } else if (f.type === 'lookup') {
      control = (
        <TouchableOpacity style={[styles.input, styles.picker, err && styles.inputError]} onPress={() => setSheet(f.key)}>
          <Text style={[styles.pickerText, !labels[f.key] && { color: colors.textMuted }]} numberOfLines={1}>{labels[f.key] || 'Search…'}</Text>
          <Search size={16} color={colors.textMuted} />
        </TouchableOpacity>
      );
    } else if (f.type === 'boolean') {
      control = <Switch value={Boolean(v)} onValueChange={(x) => set(f.key, x)} trackColor={{ true: colors.primary, false: colors.borderDark }} />;
    } else if (f.type === 'date' || f.type === 'datetime') {
      control = <DateInput type={f.type} value={v} onChange={(x) => set(f.key, x)} />;
    } else {
      const multiline = f.type === 'textarea' || f.type === 'richtext';
      const keyboardType = f.type === 'email' ? 'email-address' : f.type === 'phone' ? 'phone-pad' : ['number', 'currency', 'percent', 'rating'].includes(f.type) ? 'decimal-pad' : f.type === 'url' ? 'url' : 'default';
      control = (
        <TextInput
          style={[styles.input, multiline && styles.multiline, err && styles.inputError]}
          value={v === null || v === undefined ? '' : String(v)}
          onChangeText={(x) => set(f.key, x)}
          keyboardType={keyboardType}
          multiline={multiline}
          autoCapitalize={f.type === 'email' || f.type === 'url' ? 'none' : 'sentences'}
          placeholderTextColor={colors.textMuted}
        />
      );
    }
    return (
      <View key={f.key} style={styles.field}>
        <Text style={styles.label}>
          {f.label}
          {f.required ? <Text style={{ color: colors.danger }}> *</Text> : null}
        </Text>
        {control}
        {err ? <Text style={styles.error}>{err}</Text> : f.helpText ? <Text style={styles.help}>{f.helpText}</Text> : null}
      </View>
    );
  };

  const sheetField = sheet && meta ? meta.fields.find((f) => f.key === sheet) : null;

  return (
    <KeyboardAvoidingView style={styles.screen} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack} style={styles.back} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
          <ArrowLeft size={20} color={colors.textPrimary} />
        </TouchableOpacity>
        <Text style={styles.title} numberOfLines={1}>{meta ? `${id ? 'Edit' : 'New'} ${meta.labelSingular.toLowerCase()}` : ''}</Text>
      </View>
      {loadError ? (
        <ErrorBox message={loadError} onRetry={load} />
      ) : !meta || (id && !record) ? (
        <Loading />
      ) : (
        <>
          <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
            {alert ? <Text style={styles.alert}>{alert}</Text> : null}
            {groups.map((g) => (
              <View key={g.title}>
                <Text style={styles.group}>{g.title}</Text>
                {g.fields.map(renderField)}
              </View>
            ))}
          </ScrollView>
          <View style={styles.footer}>
            <Button title={saving ? 'Saving…' : 'Save'} onPress={save} loading={saving} disabled={saving} />
          </View>
        </>
      )}
      {sheetField && sheetField.type !== 'lookup' ? (
        <OptionSheet
          visible
          title={sheetField.label}
          options={sheetField.options || []}
          value={values[sheetField.key]}
          multiple={sheetField.type === 'multiselect'}
          onChange={(x) => set(sheetField.key, x)}
          onClose={() => setSheet(null)}
        />
      ) : null}
      {sheetField && sheetField.type === 'lookup' ? (
        <LookupSheet
          visible
          title={sheetField.label}
          target={sheetField.lookup}
          onPick={(r) => {
            set(sheetField.key, r ? r.id : '');
            setLabels((l) => ({ ...l, [sheetField.key]: r ? r.label : '' }));
            setSheet(null);
          }}
          onClose={() => setSheet(null)}
        />
      ) : null}
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgCard },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: spacing.lg, paddingVertical: spacing.md, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  back: { width: 32, height: 32, justifyContent: 'center' },
  title: { fontSize: 17, fontWeight: '700', color: colors.textPrimary, flex: 1 },
  content: { padding: spacing.lg, paddingBottom: 32 },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  group: { fontSize: 12, fontWeight: '800', color: colors.textMuted, textTransform: 'uppercase', letterSpacing: 0.6, marginTop: spacing.md, marginBottom: spacing.sm },
  field: { marginBottom: spacing.md },
  label: { ...typography.label, marginBottom: 6 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 11, fontSize: 14, color: colors.textPrimary, backgroundColor: colors.bgCard, outlineStyle: 'none' },
  inputError: { borderColor: colors.danger },
  multiline: { minHeight: 84, textAlignVertical: 'top' },
  picker: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  pickerText: { flex: 1, fontSize: 14, color: colors.textPrimary },
  error: { fontSize: 12, color: colors.danger, marginTop: 4 },
  help: { fontSize: 12, color: colors.textMuted, marginTop: 4 },
  footer: { padding: spacing.lg, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.border, backgroundColor: colors.bgCard },
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, paddingBottom: spacing.xl },
  sheetHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  sheetTitle: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  sheetDone: { fontSize: 14, fontWeight: '700', color: colors.primary },
  sheetNote: { fontSize: 13, color: colors.textSecondary, padding: spacing.lg },
  option: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 13, paddingHorizontal: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  optionText: { fontSize: 14, color: colors.textPrimary, flex: 1 },
  lookupSearch: { flexDirection: 'row', alignItems: 'center', gap: 8, margin: spacing.lg, marginBottom: spacing.sm, borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12 },
  lookupInput: { flex: 1, paddingVertical: 10, fontSize: 14, color: colors.textPrimary, outlineStyle: 'none' }
});
