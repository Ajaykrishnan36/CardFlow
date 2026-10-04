import React, { createElement, useCallback, useEffect, useRef, useState } from 'react';
import { View, Text, TouchableOpacity, StyleSheet, TextInput, Modal, ScrollView, Switch } from 'react-native';
import { X, UserPlus, Check, Link2 } from 'lucide-react';
import { colors, spacing, radii } from '../../theme';
import { Button } from '../../components/Button';
import { useCrm } from '../../context/CrmContext';
import { crmApi, errorText, newIdempotencyKey } from '../../services/crmApi';
import { Row, iconFor } from './ui';

const LABEL = { leads: 'Lead', contacts: 'Contact', accounts: 'Account' };

/** The app's card → the fields the CRM matches on. */
export function cardToFields(card) {
  if (!card) return {};
  return {
    personName: card.person_name || card.personName || '',
    designation: card.designation || '',
    company: card.company || card.company_name || '',
    website: card.website || '',
    address: card.raw_address || card.rawAddress || '',
    gstin: card.gstin || '',
    phones: (card.phones || []).map((p) => (typeof p === 'string' ? p : p.raw || p.e164 || '')).filter(Boolean),
    emails: (card.emails || []).map((e) => (typeof e === 'string' ? e : e.email || '')).filter(Boolean)
  };
}

/**
 * "Save to CRM" for one saved card: looks for the person in the open business only,
 * then saves the card as a new lead, a new contact, or onto an existing record.
 */
export function SaveCardSheet({ visible, card, onClose, onSaved }) {
  const { activeCode, active } = useCrm();
  const [matches, setMatches] = useState(null);
  const [error, setError] = useState('');
  const [action, setAction] = useState('lead');
  const [targetId, setTargetId] = useState('');
  const [createAccount, setCreateAccount] = useState(true);
  const [allowDuplicate, setAllowDuplicate] = useState(false);
  const [followUp, setFollowUp] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const idem = useRef(newIdempotencyKey());
  const fields = cardToFields(card);

  useEffect(() => {
    if (!visible || !card) return;
    idem.current = newIdempotencyKey();
    setMatches(null);
    setError('');
    setAllowDuplicate(false);
    setFollowUp('');
    setNote('');
    crmApi
      .matchCard(activeCode, cardToFields(card))
      .then((m) => {
        setMatches(m);
        setAction(m.suggested === 'attach' && m.matches.length ? 'attach' : m.suggested === 'contact' && m.can.contact ? 'contact' : m.can.lead ? 'lead' : m.can.contact ? 'contact' : 'card_only');
        setTargetId(m.matches[0]?.id || '');
      })
      .catch((e) => setError(errorText(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, card?.id, activeCode]);

  const target = matches?.matches.find((m) => m.id === targetId);
  const people = (matches?.matches || []).filter((m) => m.object !== 'accounts');

  const save = async () => {
    setBusy(true);
    setError('');
    try {
      const res = await crmApi.saveCard(
        activeCode,
        {
          cardId: card.id,
          action,
          target: action === 'attach' && target ? { object: target.object, id: target.id } : undefined,
          createAccount: action === 'contact' ? createAccount : undefined,
          allowDuplicate: allowDuplicate || undefined,
          followUpAt: followUp ? new Date(followUp).toISOString() : undefined,
          note: note.trim() || undefined
        },
        idem.current
      );
      onSaved?.(res);
    } catch (e) {
      idem.current = newIdempotencyKey();
      setError(e.code === 'possible_duplicate' ? 'Someone with this phone number or email is already in this business. Attach the card to them, or switch on “Create anyway”.' : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const options = matches
    ? [
        matches.matches.length > 0 && { key: 'attach', label: 'Attach to existing', hint: 'One record per person' },
        matches.can.lead && { key: 'lead', label: 'New lead', hint: 'Someone you may do business with' },
        matches.can.contact && { key: 'contact', label: 'New contact', hint: 'Someone you already work with' }
      ].filter(Boolean)
    : [];

  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <View style={{ flex: 1 }}>
              <Text style={styles.sheetTitle}>Save to CRM</Text>
              <Text style={styles.sheetSub} numberOfLines={1}>{fields.personName || fields.company || 'Business card'} → {active?.name}</Text>
            </View>
            <TouchableOpacity onPress={onClose}>
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          <ScrollView contentContainerStyle={{ padding: spacing.lg }} keyboardShouldPersistTaps="handled">
            {error ? <Text style={styles.alert}>{error}</Text> : null}
            {!matches && !error ? <Text style={styles.note}>Checking {active?.name} for this person…</Text> : null}
            {matches ? (
              <>
                {matches.matches.length ? (
                  <>
                    <Text style={styles.label}>Already in this business</Text>
                    {matches.matches.map((m) => {
                      const on = action === 'attach' && targetId === m.id;
                      return (
                        <TouchableOpacity
                          key={m.object + m.id}
                          style={[styles.choice, on && styles.choiceOn]}
                          onPress={() => {
                            setTargetId(m.id);
                            setAction('attach');
                          }}
                        >
                          <View style={{ flex: 1, minWidth: 0 }}>
                            <Text style={styles.choiceTitle} numberOfLines={1}>{m.title}</Text>
                            <Text style={styles.choiceHint} numberOfLines={1}>{LABEL[m.object]} · same {m.matchedOn.join(' and ')}</Text>
                          </View>
                          {on ? <Check size={18} color={colors.primary} /> : null}
                        </TouchableOpacity>
                      );
                    })}
                  </>
                ) : (
                  <Text style={styles.note}>Nobody in this business has this phone number or email yet.</Text>
                )}
                {matches.hidden > 0 ? (
                  <Text style={styles.warn}>A colleague’s record in this business already has this phone number or email. Check with them before adding another.</Text>
                ) : null}

                <Text style={[styles.label, { marginTop: spacing.md }]}>Save as</Text>
                {options.map((o) => (
                  <TouchableOpacity key={o.key} style={[styles.choice, action === o.key && styles.choiceOn]} onPress={() => setAction(o.key)}>
                    <View style={{ flex: 1 }}>
                      <Text style={styles.choiceTitle}>{o.label}</Text>
                      <Text style={styles.choiceHint}>{o.hint}</Text>
                    </View>
                    {action === o.key ? <Check size={18} color={colors.primary} /> : null}
                  </TouchableOpacity>
                ))}
                {options.length === 0 ? <Text style={styles.note}>Your role in this business can’t create leads or contacts. The card stays in My Cards.</Text> : null}

                {action === 'contact' && fields.company && matches.can.account ? (
                  <View style={styles.switchRow}>
                    <Text style={styles.switchLabel}>Put under the account “{fields.company}”</Text>
                    <Switch value={createAccount} onValueChange={setCreateAccount} trackColor={{ true: colors.primary, false: colors.borderDark }} />
                  </View>
                ) : null}
                {(action === 'lead' || action === 'contact') && (people.length > 0 || matches.hidden > 0) ? (
                  <View style={styles.switchRow}>
                    <Text style={styles.switchLabel}>Create anyway (a second record)</Text>
                    <Switch value={allowDuplicate} onValueChange={setAllowDuplicate} trackColor={{ true: colors.primary, false: colors.borderDark }} />
                  </View>
                ) : null}

                {options.length ? (
                  <>
                    <Text style={[styles.label, { marginTop: spacing.md }]}>Follow up on (optional)</Text>
                    {createElement('input', {
                      type: 'datetime-local',
                      value: followUp,
                      onChange: (e) => setFollowUp(e.target.value),
                      style: { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radii.input, padding: '11px 12px', fontSize: 14, color: colors.textPrimary, backgroundColor: colors.bgCard, fontFamily: 'inherit' }
                    })}
                    <Text style={[styles.label, { marginTop: spacing.md }]}>Note (optional)</Text>
                    <TextInput style={styles.input} value={note} onChangeText={setNote} placeholder="Where you met, what they need…" placeholderTextColor={colors.textMuted} multiline />
                    <Button
                      title={busy ? 'Saving…' : action === 'lead' ? 'Create lead' : action === 'contact' ? 'Create contact' : 'Attach card'}
                      onPress={save}
                      loading={busy}
                      disabled={busy || (action === 'attach' && !target)}
                      style={{ marginTop: spacing.lg }}
                    />
                  </>
                ) : null}
              </>
            ) : null}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

/** On a saved card: what it is linked to in the open business, and "Save to CRM". */
export function CardCrmPanel({ card, onOpenRecord }) {
  const { activeCode, active, status } = useCrm();
  const [links, setLinks] = useState(null);
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState('');

  const load = useCallback(() => {
    if (!activeCode || !card?.id) return;
    crmApi
      .card(activeCode, card.id)
      .then((c) => setLinks(c.links || []))
      .catch(() => setLinks([])); // not in this business yet
  }, [activeCode, card?.id]);
  useEffect(() => {
    setLinks(null);
    load();
  }, [load]);

  if (status !== 'ready' || !active || !card?.id) return null;
  return (
    <View style={styles.panel}>
      <View style={styles.panelHeader}>
        <Link2 size={16} color={colors.primary} />
        <Text style={styles.panelTitle}>In {active.name}</Text>
      </View>
      {done ? <Text style={styles.done}>{done}</Text> : null}
      {links && links.length === 0 ? <Text style={styles.note}>Not in your CRM yet.</Text> : null}
      {(links || []).map((l) => (
        <Row key={l.id} icon={iconFor(l.object)} title={l.title} subtitle={`${LABEL[l.object] || l.object} · ${l.code}`} onPress={onOpenRecord ? () => onOpenRecord(l.object, l.recordId) : undefined} />
      ))}
      <Button title={links && links.length ? 'Link to another record' : 'Save to CRM'} icon={UserPlus} variant={links && links.length ? 'outline' : 'primary'} onPress={() => setOpen(true)} style={{ marginTop: spacing.md }} />
      <SaveCardSheet
        visible={open}
        card={card}
        onClose={() => setOpen(false)}
        onSaved={(res) => {
          setOpen(false);
          setDone(res.record ? `${LABEL[res.record.object] || 'Record'} ${res.created ? 'created' : 'updated'}: ${res.record.title}` : 'Saved');
          load();
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, maxHeight: '90%', paddingBottom: spacing.lg },
  sheetHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  sheetTitle: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  sheetSub: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  warn: { backgroundColor: colors.warningLight, color: colors.textPrimary, padding: 10, borderRadius: radii.sm, marginTop: spacing.sm, fontSize: 13 },
  note: { fontSize: 13, color: colors.textSecondary, paddingVertical: 6 },
  done: { fontSize: 13, color: colors.success, fontWeight: '600', paddingVertical: 6 },
  label: { fontSize: 12, fontWeight: '800', color: colors.textMuted, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 8 },
  choice: { flexDirection: 'row', alignItems: 'center', gap: 10, borderWidth: 1, borderColor: colors.border, borderRadius: radii.md, padding: 12, marginBottom: 8 },
  choiceOn: { borderColor: colors.primary, backgroundColor: colors.primaryLight },
  choiceTitle: { fontSize: 14, fontWeight: '700', color: colors.textPrimary },
  choiceHint: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  switchRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10, paddingVertical: 8 },
  switchLabel: { flex: 1, fontSize: 13, fontWeight: '600', color: colors.textPrimary },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 11, fontSize: 14, color: colors.textPrimary, minHeight: 60, textAlignVertical: 'top', outlineStyle: 'none' },
  panel: { backgroundColor: colors.bgCard, borderRadius: radii.card, borderWidth: 1, borderColor: colors.border, padding: spacing.md, marginTop: spacing.md },
  panelHeader: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 4 },
  panelTitle: { fontSize: 14, fontWeight: '700', color: colors.textPrimary }
});
