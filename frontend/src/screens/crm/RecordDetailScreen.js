import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, TextInput, Modal, Linking, RefreshControl, Switch } from 'react-native';
import { ArrowLeft, Phone, Mail, MessageCircle, Pencil, Trash2, CheckSquare, Calendar, StickyNote, ArrowRightLeft, X, Check, ChevronDown } from 'lucide-react';
import { colors, spacing, radii, shadows } from '../../theme';
import { Button } from '../../components/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useCrm } from '../../context/CrmContext';
import { crmApi, errorText, newIdempotencyKey } from '../../services/crmApi';
import { Row, Panel, SectionTitle, Loading, ErrorBox, StatusPill, iconFor, money, humanize, guessTone } from './ui';

const RELATED_OBJECTS = new Set(['leads', 'contacts', 'accounts']);
const LINK_FIELD = { leads: 'leadId', contacts: 'contactId', accounts: 'accountId', opportunities: 'opportunityId', cases: 'caseId' };

function stripHtml(s) {
  return String(s || '').replace(/<br\s*\/?>/gi, '\n').replace(/<\/p>/gi, '\n').replace(/<[^>]+>/g, '').replace(/&nbsp;/g, ' ').replace(/&amp;/g, '&').trim();
}

function display(f, record, currency) {
  const v = record.values?.[f.key];
  if (f.type === 'lookup') return record.lookups?.[f.key]?.label || '';
  if (f.type === 'relations') return (record.links?.[f.key] || []).map((l) => l.label).join(', ');
  if (v === null || v === undefined || v === '') return '';
  switch (f.type) {
    case 'currency': return money(v, currency);
    case 'percent': return `${v}%`;
    case 'boolean': return v ? 'Yes' : 'No';
    case 'date': return new Date(v).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });
    case 'datetime': return new Date(v).toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });
    case 'select': return f.options?.find((o) => o.value === v)?.label || humanize(v);
    case 'multiselect': return (Array.isArray(v) ? v : []).map((x) => f.options?.find((o) => o.value === x)?.label || x).join(', ');
    case 'richtext': return stripHtml(v);
    default: return typeof v === 'object' ? JSON.stringify(v) : String(v);
  }
}

function when(iso) {
  const d = new Date(iso);
  const diff = (Date.now() - d.getTime()) / 1000;
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} h ago`;
  return d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short' });
}

/** Turn a lead into an account + contact (+ deal), with the server's own conversion rules. */
function ConvertSheet({ visible, record, onClose, onConverted }) {
  const { activeCode, context, can } = useCrm();
  const company = record?.values?.organization || '';
  const [accountName, setAccountName] = useState(company || record?.title || '');
  const [withAccount, setWithAccount] = useState(true);
  const [withDeal, setWithDeal] = useState(Boolean(context?.setup?.createOpportunity));
  const [dealName, setDealName] = useState(`${company || record?.title || 'New'} deal`);
  const [amount, setAmount] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const idem = useRef(newIdempotencyKey());
  const canDeal = can('opportunities', 'create');

  const submit = async () => {
    setBusy(true);
    setError('');
    try {
      const body = {
        account: withAccount ? { mode: 'new', name: accountName.trim(), kind: company ? 'business' : 'individual' } : { mode: 'none' },
        createContact: true
      };
      if (canDeal) body.opportunity = { create: withDeal, name: dealName.trim() || undefined, amount: amount ? Number(amount) : null };
      const res = await crmApi.convertLead(activeCode, record.id, body, idem.current);
      onConverted(res);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>Convert lead</Text>
            <TouchableOpacity onPress={onClose}>
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          <ScrollView contentContainerStyle={{ padding: spacing.lg }} keyboardShouldPersistTaps="handled">
            <Text style={styles.sheetNote}>{record?.title} becomes a contact. The lead stays in your history as converted.</Text>
            {error ? <Text style={styles.alert}>{error}</Text> : null}
            <View style={styles.switchRow}>
              <Text style={styles.switchLabel}>Create an account</Text>
              <Switch value={withAccount} onValueChange={setWithAccount} trackColor={{ true: colors.primary, false: colors.borderDark }} />
            </View>
            {withAccount ? <TextInput style={styles.input} value={accountName} onChangeText={setAccountName} placeholder="Account name" placeholderTextColor={colors.textMuted} /> : null}
            {canDeal ? (
              <>
                <View style={styles.switchRow}>
                  <Text style={styles.switchLabel}>Create a deal</Text>
                  <Switch value={withDeal} onValueChange={setWithDeal} trackColor={{ true: colors.primary, false: colors.borderDark }} />
                </View>
                {withDeal ? (
                  <>
                    <TextInput style={styles.input} value={dealName} onChangeText={setDealName} placeholder="Deal name" placeholderTextColor={colors.textMuted} />
                    <TextInput style={[styles.input, { marginTop: 8 }]} value={amount} onChangeText={setAmount} placeholder="Amount (optional)" keyboardType="decimal-pad" placeholderTextColor={colors.textMuted} />
                  </>
                ) : null}
              </>
            ) : null}
            <Button title={busy ? 'Converting…' : 'Convert'} onPress={submit} loading={busy} disabled={busy || (withAccount && accountName.trim().length < 2)} style={{ marginTop: spacing.lg }} />
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

/** One record: details by page layout, quick actions, related lists, timeline and notes. */
export function RecordDetailScreen({ object, id, onBack, onEdit, onOpenRecord, onCreateRelated, onOpenCard, onDeleted }) {
  const { activeCode, can, has, currency } = useCrm();
  const [meta, setMeta] = useState(null);
  const [detail, setDetail] = useState(null);
  const [timeline, setTimeline] = useState([]);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [note, setNote] = useState('');
  const [noteBusy, setNoteBusy] = useState(false);
  const [toast, setToast] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [converting, setConverting] = useState(false);

  const load = useCallback(async () => {
    try {
      const [m, d] = await Promise.all([crmApi.meta(activeCode, object), crmApi.get(activeCode, object, id)]);
      setMeta(m);
      setDetail(d);
      setError('');
      crmApi.timeline(activeCode, object, id).then(setTimeline).catch(() => setTimeline([]));
    } catch (e) {
      setError(e.status === 404 ? 'This record no longer exists, or you can’t open it.' : e.message || 'Could not open the record.');
    }
  }, [activeCode, object, id]);

  useEffect(() => {
    setDetail(null);
    load();
  }, [load]);

  const flash = (msg) => {
    setToast(msg);
    setTimeout(() => setToast(''), 2600);
  };

  const record = detail?.record;
  const byKey = useMemo(() => Object.fromEntries((meta?.fields || []).map((f) => [f.key, f])), [meta]);
  const statusField = meta?.statusField;
  const statusValue = statusField ? record?.values?.[statusField] : null;
  const statusDef = meta?.statuses?.find((s) => s.value === statusValue);
  const phone = record?.values?.mobile || record?.values?.phone;
  const email = record?.values?.email;
  const converted = object === 'leads' && statusValue === 'converted';

  const setStatus = async (value) => {
    setStatusOpen(false);
    if (value === statusValue) return;
    try {
      await crmApi.update(activeCode, object, id, { [statusField]: value }, record.version);
      flash('Status updated');
      load();
    } catch (e) {
      flash(errorText(e));
    }
  };

  const addNote = async (kind) => {
    const text = note.trim();
    if (!text || noteBusy) return;
    setNoteBusy(true);
    try {
      await crmApi.addNote(activeCode, object, id, text, kind);
      setNote('');
      flash(kind === 'call' ? 'Call logged' : 'Note added');
      crmApi.timeline(activeCode, object, id).then(setTimeline).catch(() => {});
    } catch (e) {
      flash(errorText(e));
    } finally {
      setNoteBusy(false);
    }
  };

  const remove = async () => {
    setConfirmDelete(false);
    try {
      await crmApi.remove(activeCode, object, id);
      onDeleted?.();
    } catch (e) {
      flash(errorText(e));
    }
  };

  const relatedPrefill = () => {
    const key = LINK_FIELD[object];
    return key ? { values: { [key]: id }, lookups: { [key]: record.title } } : { values: {}, lookups: {} };
  };

  if (error) {
    return (
      <View style={styles.screen}>
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack} style={styles.back}>
            <ArrowLeft size={20} color={colors.textPrimary} />
          </TouchableOpacity>
        </View>
        <ErrorBox message={error} onRetry={load} />
      </View>
    );
  }
  if (!record || !meta) {
    return (
      <View style={styles.screen}>
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack} style={styles.back}>
            <ArrowLeft size={20} color={colors.textPrimary} />
          </TouchableOpacity>
        </View>
        <Loading />
      </View>
    );
  }

  const canEdit = can(object, 'update');
  const sections = (meta.layout?.sections || [])
    .map((s) => ({ title: s.title, id: s.id, rows: s.fields.map((k) => byKey[k]).filter(Boolean).map((f) => ({ f, text: display(f, record, currency) })).filter((r) => r.text) }))
    .filter((s) => s.rows.length);
  const lists = (detail.related || []).filter((l) => l.object !== 'activities' && l.rows?.length);

  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        <TouchableOpacity onPress={onBack} style={styles.back} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
          <ArrowLeft size={20} color={colors.textPrimary} />
        </TouchableOpacity>
        <Text style={styles.headerLabel}>{meta.labelSingular} · {record.code}</Text>
        {canEdit ? (
          <TouchableOpacity onPress={() => onEdit(object, id)} style={styles.headerBtn} accessibilityLabel="Edit">
            <Pencil size={18} color={colors.primary} />
          </TouchableOpacity>
        ) : null}
        {can(object, 'delete') ? (
          <TouchableOpacity onPress={() => setConfirmDelete(true)} style={styles.headerBtn} accessibilityLabel="Delete">
            <Trash2 size={18} color={colors.danger} />
          </TouchableOpacity>
        ) : null}
      </View>

      <ScrollView
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            tintColor={colors.primary}
            onRefresh={async () => {
              setRefreshing(true);
              await load();
              setRefreshing(false);
            }}
          />
        }
      >
        <Text style={styles.title}>{record.title || 'Untitled'}</Text>
        {statusField && statusValue ? (
          <TouchableOpacity style={styles.statusRow} onPress={() => canEdit && !converted && setStatusOpen(true)} activeOpacity={canEdit && !converted ? 0.7 : 1}>
            <StatusPill label={statusDef?.label || humanize(statusValue)} tone={statusDef?.tone || guessTone(statusValue)} />
            {canEdit && !converted ? <ChevronDown size={14} color={colors.textMuted} /> : null}
          </TouchableOpacity>
        ) : null}

        <View style={styles.actions}>
          {phone ? (
            <>
              <TouchableOpacity style={styles.action} onPress={() => Linking.openURL(`tel:${String(phone).replace(/[^\d+]/g, '')}`)}>
                <Phone size={18} color={colors.primary} />
                <Text style={styles.actionText}>Call</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={styles.action}
                onPress={() => {
                  const d = String(phone).replace(/\D/g, '');
                  Linking.openURL(`https://wa.me/${d.length === 10 ? `91${d}` : d}`);
                }}
              >
                <MessageCircle size={18} color={colors.success} />
                <Text style={styles.actionText}>WhatsApp</Text>
              </TouchableOpacity>
            </>
          ) : null}
          {email ? (
            <TouchableOpacity style={styles.action} onPress={() => Linking.openURL(`mailto:${email}`)}>
              <Mail size={18} color={colors.info} />
              <Text style={styles.actionText}>Email</Text>
            </TouchableOpacity>
          ) : null}
          {LINK_FIELD[object] && can('tasks', 'create') ? (
            <TouchableOpacity style={styles.action} onPress={() => onCreateRelated('tasks', relatedPrefill())}>
              <CheckSquare size={18} color={colors.primary} />
              <Text style={styles.actionText}>Task</Text>
            </TouchableOpacity>
          ) : null}
          {RELATED_OBJECTS.has(object) && can('events', 'create') ? (
            <TouchableOpacity style={styles.action} onPress={() => onCreateRelated('events', relatedPrefill())}>
              <Calendar size={18} color={colors.primary} />
              <Text style={styles.actionText}>Meeting</Text>
            </TouchableOpacity>
          ) : null}
          {object === 'leads' && !converted && can('leads', 'convert') ? (
            <TouchableOpacity style={[styles.action, styles.actionPrimary]} onPress={() => setConverting(true)}>
              <ArrowRightLeft size={18} color="#FFFFFF" />
              <Text style={[styles.actionText, { color: '#FFFFFF' }]}>Convert</Text>
            </TouchableOpacity>
          ) : null}
        </View>

        {detail.conversion ? (
          <Panel style={{ marginTop: spacing.md }}>
            {detail.conversion.account ? <Row icon={iconFor('accounts')} title={detail.conversion.account.label} subtitle="Converted account" onPress={() => onOpenRecord('accounts', detail.conversion.account.id)} /> : null}
            {detail.conversion.contact ? <Row icon={iconFor('contacts')} title={detail.conversion.contact.label} subtitle="Converted contact" onPress={() => onOpenRecord('contacts', detail.conversion.contact.id)} /> : null}
          </Panel>
        ) : null}

        {sections.map((s) => (
          <View key={s.id}>
            <SectionTitle>{s.title}</SectionTitle>
            <Panel style={{ paddingHorizontal: spacing.md }}>
              {s.rows.map(({ f, text }) => {
                const link = f.type === 'lookup' && record.lookups?.[f.key] && f.lookup !== 'users' && f.lookup !== 'products' && f.lookup !== 'workspaces';
                return (
                  <TouchableOpacity
                    key={f.key}
                    style={styles.fieldRow}
                    disabled={!link}
                    onPress={() => onOpenRecord(f.lookup, record.lookups[f.key].id)}
                  >
                    <Text style={styles.fieldLabel}>{f.label}</Text>
                    <Text style={[styles.fieldValue, link && { color: colors.primary }]}>{text}</Text>
                  </TouchableOpacity>
                );
              })}
            </Panel>
          </View>
        ))}

        {lists.map((l) => (
          <View key={l.key}>
            <SectionTitle>{l.label}</SectionTitle>
            <Panel>
              {l.rows.slice(0, 8).map((r) => {
                const openable = l.object === 'cards' || (has(l.object) && !['workspaces', 'users', 'tickets'].includes(l.object) && !String(l.object).startsWith('app-'));
                return (
                  <Row
                    key={r.id}
                    icon={iconFor(l.object)}
                    title={r.title}
                    subtitle={r.subtitle || r.code}
                    status={r.status}
                    onPress={openable ? () => (l.object === 'cards' ? onOpenCard?.(r.id) : onOpenRecord(l.object, r.id)) : undefined}
                  />
                );
              })}
            </Panel>
          </View>
        ))}

        <SectionTitle>Notes & activity</SectionTitle>
        {canEdit ? (
          <Panel style={{ padding: spacing.md }}>
            <TextInput style={styles.noteInput} multiline placeholder="Write a note or what was said on a call…" placeholderTextColor={colors.textMuted} value={note} onChangeText={setNote} />
            <View style={styles.noteActions}>
              <Button title="Log call" variant="outline" size="sm" icon={Phone} onPress={() => addNote('call')} disabled={!note.trim() || noteBusy} />
              <Button title="Add note" size="sm" icon={StickyNote} onPress={() => addNote('note')} disabled={!note.trim() || noteBusy} loading={noteBusy} />
            </View>
          </Panel>
        ) : null}
        <Panel style={{ marginTop: spacing.sm }}>
          {timeline.length === 0 ? <Text style={styles.timelineEmpty}>No activity yet.</Text> : null}
          {timeline.map((t) => (
            <View key={t.id} style={styles.timelineRow}>
              <View style={styles.dot} />
              <View style={{ flex: 1 }}>
                <Text style={styles.timelineTitle}>{t.title}</Text>
                {t.body ? <Text style={styles.timelineBody}>{stripHtml(t.body)}</Text> : null}
                {t.detail?.changes?.length ? (
                  <Text style={styles.timelineBody} numberOfLines={3}>{t.detail.changes.map((c) => c.label).join(', ')}</Text>
                ) : null}
                <Text style={styles.timelineMeta}>{[t.actor?.label, when(t.at)].filter(Boolean).join(' · ')}</Text>
              </View>
            </View>
          ))}
        </Panel>
      </ScrollView>

      {toast ? (
        <View style={[styles.toast, shadows.md]}>
          <Text style={styles.toastText}>{toast}</Text>
        </View>
      ) : null}

      <ConfirmDialog
        visible={confirmDelete}
        title={`Delete this ${meta.labelSingular.toLowerCase()}?`}
        message="It moves to the recycle bin. An administrator can restore it from the web CRM."
        confirmLabel="Delete"
        onCancel={() => setConfirmDelete(false)}
        onConfirm={remove}
      />

      <Modal transparent animationType="slide" visible={statusOpen} onRequestClose={() => setStatusOpen(false)}>
        <View style={styles.overlay}>
          <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={() => setStatusOpen(false)} />
          <View style={styles.sheet}>
            <View style={styles.sheetHeader}>
              <Text style={styles.sheetTitle}>Change status</Text>
              <TouchableOpacity onPress={() => setStatusOpen(false)}>
                <X size={20} color={colors.textSecondary} />
              </TouchableOpacity>
            </View>
            <ScrollView style={{ maxHeight: 400 }}>
              {(meta.statuses || []).filter((s) => !(object === 'leads' && s.value === 'converted')).map((s) => (
                <TouchableOpacity key={s.value} style={styles.option} onPress={() => setStatus(s.value)}>
                  <Text style={[styles.optionText, s.value === statusValue && { color: colors.primary, fontWeight: '700' }]}>{s.label}</Text>
                  {s.value === statusValue ? <Check size={16} color={colors.primary} /> : null}
                </TouchableOpacity>
              ))}
            </ScrollView>
          </View>
        </View>
      </Modal>

      {converting ? (
        <ConvertSheet
          visible
          record={record}
          onClose={() => setConverting(false)}
          onConverted={(res) => {
            setConverting(false);
            flash('Lead converted');
            if (res?.contactId) onOpenRecord('contacts', res.contactId);
            else load();
          }}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bgMuted },
  header: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: spacing.lg, paddingVertical: spacing.md, backgroundColor: colors.bgCard, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  back: { width: 32, height: 32, justifyContent: 'center' },
  headerLabel: { flex: 1, fontSize: 13, fontWeight: '600', color: colors.textSecondary },
  headerBtn: { width: 36, height: 36, borderRadius: 18, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.bgMuted },
  content: { padding: spacing.lg, paddingBottom: 48 },
  title: { fontSize: 22, fontWeight: '800', color: colors.textPrimary },
  statusRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 8, alignSelf: 'flex-start' },
  actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: spacing.lg },
  action: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 12, paddingVertical: 9, borderRadius: radii.button, backgroundColor: colors.bgCard, borderWidth: 1, borderColor: colors.border },
  actionPrimary: { backgroundColor: colors.primary, borderColor: colors.primary },
  actionText: { fontSize: 13, fontWeight: '700', color: colors.textPrimary },
  fieldRow: { paddingVertical: 10, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  fieldLabel: { fontSize: 11, fontWeight: '600', color: colors.textMuted, textTransform: 'uppercase', letterSpacing: 0.4 },
  fieldValue: { fontSize: 14, color: colors.textPrimary, marginTop: 3 },
  noteInput: { minHeight: 64, fontSize: 14, color: colors.textPrimary, textAlignVertical: 'top', outlineStyle: 'none' },
  noteActions: { flexDirection: 'row', justifyContent: 'flex-end', gap: 8, marginTop: 8 },
  timelineEmpty: { fontSize: 13, color: colors.textSecondary, padding: spacing.lg, textAlign: 'center' },
  timelineRow: { flexDirection: 'row', gap: 10, paddingVertical: 10, paddingHorizontal: spacing.md, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.primary, marginTop: 6 },
  timelineTitle: { fontSize: 13, fontWeight: '700', color: colors.textPrimary },
  timelineBody: { fontSize: 13, color: colors.textSecondary, marginTop: 2 },
  timelineMeta: { fontSize: 11, color: colors.textMuted, marginTop: 3 },
  toast: { position: 'absolute', left: spacing.lg, right: spacing.lg, bottom: spacing.xl, backgroundColor: colors.bgDark, borderRadius: radii.md, padding: 12 },
  toastText: { color: '#FFFFFF', fontSize: 13, fontWeight: '600', textAlign: 'center' },
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, paddingBottom: spacing.xl, maxHeight: '85%' },
  sheetHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  sheetTitle: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  sheetNote: { fontSize: 13, color: colors.textSecondary, marginBottom: spacing.md },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  switchRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 10 },
  switchLabel: { fontSize: 14, fontWeight: '600', color: colors.textPrimary },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 11, fontSize: 14, color: colors.textPrimary, backgroundColor: colors.bgCard, outlineStyle: 'none' },
  option: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 13, paddingHorizontal: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  optionText: { fontSize: 14, color: colors.textPrimary, flex: 1 }
});
