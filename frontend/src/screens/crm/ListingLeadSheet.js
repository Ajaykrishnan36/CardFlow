import React, { createElement, useEffect, useRef, useState } from 'react';
import { View, Text, TouchableOpacity, StyleSheet, TextInput, Modal, ScrollView } from 'react-native';
import { X, CheckCircle2 } from 'lucide-react';
import { colors, spacing, radii } from '../../theme';
import { Button } from '../../components/Button';
import { useCrm } from '../../context/CrmContext';
import { crmApi, errorText, newIdempotencyKey } from '../../services/crmApi';
import { Row, iconFor } from './ui';

const LABEL = { leads: 'Lead', contacts: 'Contact', accounts: 'Account' };

function clean(v) {
  return String(v || '').trim();
}

/** A directory listing → the fields of a lead in your CRM. */
export function listingToLead(biz) {
  const phones = [biz.phone, ...(Array.isArray(biz.phones) ? biz.phones : []), biz.whatsapp].map(clean).filter(Boolean);
  const unique = [...new Set(phones)];
  const address = clean(biz.address_line1 || biz.address);
  const category = clean(biz.primary_category || biz.category);
  const about = [category ? `Category: ${category}` : '', clean(biz.description), biz.gstin ? `GSTIN: ${clean(biz.gstin)}` : ''].filter(Boolean).join('\n');
  const values = {
    lastName: clean(biz.contact_name) || clean(biz.name || biz.business_name),
    organization: clean(biz.name || biz.business_name),
    phone: unique[0] || '',
    mobile: unique[1] || '',
    email: clean(biz.email),
    website: clean(biz.website),
    street: address && !/^address pending$/i.test(address) ? address : '',
    city: clean(biz.city),
    state: clean(biz.state),
    postalCode: /^0+$/.test(clean(biz.pincode)) ? '' : clean(biz.pincode),
    source: 'directory',
    description: about ? `From the business directory.\n${about}` : 'From the business directory.'
  };
  Object.keys(values).forEach((k) => {
    if (!values[k]) delete values[k];
  });
  return values;
}

/**
 * "New lead" on a directory listing: adds the business to the open business's CRM as a
 * lead. It first looks for the same phone, email or company among your own records, so
 * the same business isn't added twice by accident.
 */
export function ListingLeadSheet({ visible, business, onClose, onOpenRecord }) {
  const { activeCode, active } = useCrm();
  const [matches, setMatches] = useState(null);
  const [followUp, setFollowUp] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [created, setCreated] = useState(null);
  const idem = useRef(newIdempotencyKey());
  const values = business ? listingToLead(business) : {};

  useEffect(() => {
    if (!visible || !business) return;
    idem.current = newIdempotencyKey();
    setMatches(null);
    setError('');
    setCreated(null);
    setFollowUp('');
    setNote('');
    const v = listingToLead(business);
    crmApi
      .matchCard(activeCode, { company: v.organization, phones: [v.phone, v.mobile].filter(Boolean), emails: [v.email].filter(Boolean) })
      .then(setMatches)
      .catch((e) => setError(errorText(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, business?.id, activeCode]);

  const create = async () => {
    setBusy(true);
    setError('');
    try {
      const body = { ...values };
      if (followUp) body.nextFollowUpAt = new Date(followUp).toISOString();
      const row = await crmApi.create(activeCode, 'leads', body, idem.current);
      if (note.trim()) await crmApi.addNote(activeCode, 'leads', row.id, note.trim()).catch(() => {});
      setCreated(row);
    } catch (e) {
      idem.current = newIdempotencyKey();
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal transparent animationType="slide" visible={visible} onRequestClose={onClose}>
      <View style={styles.overlay}>
        <TouchableOpacity style={{ flex: 1 }} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.header}>
            <View style={{ flex: 1 }}>
              <Text style={styles.title}>New lead</Text>
              <Text style={styles.sub} numberOfLines={1}>{values.organization} → {active?.name}</Text>
            </View>
            <TouchableOpacity onPress={onClose} accessibilityLabel="Close">
              <X size={20} color={colors.textSecondary} />
            </TouchableOpacity>
          </View>
          <ScrollView contentContainerStyle={{ padding: spacing.lg }} keyboardShouldPersistTaps="handled">
            {created ? (
              <View style={styles.done}>
                <CheckCircle2 size={36} color={colors.success} />
                <Text style={styles.doneTitle}>Lead created</Text>
                <Text style={styles.note}>{created.title} ({created.code}) is in {active?.name}.</Text>
                <Button title="Open the lead" onPress={() => { onClose(); onOpenRecord?.('leads', created.id); }} style={{ alignSelf: 'stretch', marginTop: spacing.md }} />
                <Button title="Stay here" variant="ghost" onPress={onClose} style={{ alignSelf: 'stretch', marginTop: spacing.sm }} />
              </View>
            ) : (
              <>
                {error ? <Text style={styles.alert}>{error}</Text> : null}
                {!matches && !error ? <Text style={styles.note}>Checking {active?.name} for this business…</Text> : null}
                {matches && matches.matches.length > 0 ? (
                  <>
                    <Text style={styles.label}>Already in your CRM</Text>
                    <View style={styles.box}>
                      {matches.matches.map((m) => (
                        <Row
                          key={m.object + m.id}
                          icon={iconFor(m.object)}
                          title={m.title}
                          subtitle={`${LABEL[m.object]} · same ${m.matchedOn.join(' and ')}`}
                          onPress={() => { onClose(); onOpenRecord?.(m.object, m.id); }}
                        />
                      ))}
                    </View>
                    <Text style={styles.warn}>Open the existing record, or create another lead below.</Text>
                  </>
                ) : null}
                {matches && matches.hidden > 0 ? (
                  <Text style={styles.warn}>A colleague’s record in this business already has this phone number or email.</Text>
                ) : null}
                {matches ? (
                  <>
                    <Text style={[styles.label, { marginTop: spacing.md }]}>The lead</Text>
                    <View style={styles.box}>
                      {[['Company', values.organization], ['Phone', [values.phone, values.mobile].filter(Boolean).join(', ')], ['Email', values.email], ['Website', values.website],
                        ['Address', [values.street, values.city, values.state, values.postalCode].filter(Boolean).join(', ')], ['Source', 'Business directory']]
                        .filter(([, v]) => v)
                        .map(([k, v]) => (
                          <View key={k} style={styles.line}>
                            <Text style={styles.lineLabel}>{k}</Text>
                            <Text style={styles.lineValue}>{v}</Text>
                          </View>
                        ))}
                    </View>
                    {matches.can.lead ? (
                      <>
                        <Text style={[styles.label, { marginTop: spacing.md }]}>Follow up on (optional)</Text>
                        {createElement('input', {
                          type: 'datetime-local',
                          value: followUp,
                          onChange: (e) => setFollowUp(e.target.value),
                          style: { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radii.input, padding: '11px 12px', fontSize: 14, color: colors.textPrimary, backgroundColor: colors.bgCard, fontFamily: 'inherit' }
                        })}
                        <Text style={[styles.label, { marginTop: spacing.md }]}>Note (optional)</Text>
                        <TextInput style={styles.input} value={note} onChangeText={setNote} placeholder="Why you want to talk to them…" placeholderTextColor={colors.textMuted} multiline />
                        <Button title={busy ? 'Creating…' : matches.matches.length ? 'Create another lead' : 'Create lead'} onPress={create} loading={busy} disabled={busy} style={{ marginTop: spacing.lg }} />
                      </>
                    ) : (
                      <Text style={styles.warn}>Your role in {active?.name} can’t create leads.</Text>
                    )}
                  </>
                ) : null}
              </>
            )}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  overlay: { flex: 1, backgroundColor: 'rgba(15,23,42,0.55)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bgCard, borderTopLeftRadius: radii.modal, borderTopRightRadius: radii.modal, maxHeight: '90%', paddingBottom: spacing.lg },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: spacing.lg, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  title: { fontSize: 16, fontWeight: '700', color: colors.textPrimary },
  sub: { fontSize: 12, color: colors.textSecondary, marginTop: 2 },
  label: { fontSize: 12, fontWeight: '800', color: colors.textMuted, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 8 },
  box: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.md, overflow: 'hidden' },
  line: { paddingVertical: 9, paddingHorizontal: spacing.md, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  lineLabel: { fontSize: 11, fontWeight: '600', color: colors.textMuted, textTransform: 'uppercase', letterSpacing: 0.4 },
  lineValue: { fontSize: 14, color: colors.textPrimary, marginTop: 2 },
  note: { fontSize: 13, color: colors.textSecondary, paddingVertical: 6, textAlign: 'center' },
  warn: { backgroundColor: colors.warningLight, color: colors.textPrimary, padding: 10, borderRadius: radii.sm, marginTop: spacing.sm, fontSize: 13 },
  alert: { backgroundColor: colors.dangerLight, color: colors.danger, padding: 10, borderRadius: radii.sm, marginBottom: spacing.md, fontSize: 13 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.input, paddingHorizontal: 12, paddingVertical: 11, fontSize: 14, color: colors.textPrimary, minHeight: 60, textAlignVertical: 'top', outlineStyle: 'none' },
  done: { alignItems: 'center', paddingVertical: spacing.lg },
  doneTitle: { fontSize: 18, fontWeight: '800', color: colors.textPrimary, marginTop: spacing.sm }
});
