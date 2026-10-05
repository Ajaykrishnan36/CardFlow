import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, TextInput, StyleSheet } from 'react-native';
import { colors, spacing, radii } from '../../theme';
import { Button } from '../../components/Button';
import { useCrm } from '../../context/CrmContext';
import { crmApi, errorText } from '../../services/crmApi';
import { Panel, SectionTitle, StatusPill, money } from './ui';

// The phone layout's side of the enterprise model (D-111…D-116): an invoice's payments
// with "Record payment", a case's SLA clocks and a contract's renewal. The same API as
// the desktop record page.

function minutesText(m) {
  const n = Math.abs(m);
  if (n < 60) return `${n} min`;
  if (n < 48 * 60) return `${Math.floor(n / 60)} h${n % 60 ? ` ${n % 60} min` : ''}`;
  return `${Math.round(n / 60 / 24)} days`;
}

const SLA = {
  running: ['Running', 'primary'],
  paused: ['Paused', 'neutral'],
  met: ['Met', 'success'],
  breached: ['Breached', 'danger'],
  missed: ['Met late', 'danger']
};

function Figure({ label, value, color }) {
  return (
    <View style={{ flex: 1 }}>
      <Text style={styles.figureLabel}>{label}</Text>
      <Text style={[styles.figureValue, color ? { color } : null]}>{value}</Text>
    </View>
  );
}

function InvoicePayments({ record, onChanged, onOpenRecord }) {
  const { activeCode, currency } = useCrm();
  const [ledger, setLedger] = useState(null);
  const [open, setOpen] = useState(false);
  const [amount, setAmount] = useState('');
  const [reference, setReference] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    crmApi.invoiceLedger(activeCode, record.id).then(setLedger).catch(() => setLedger(null));
  }, [activeCode, record.id]);
  useEffect(load, [load, record.version]);
  if (!ledger) return null;
  const cur = ledger.currency || currency;

  const save = async () => {
    const n = Number(amount);
    if (!(n > 0)) return setError('Enter the amount received.');
    setBusy(true);
    setError('');
    try {
      await crmApi.create(activeCode, 'payments', {
        name: `Payment for ${record.code || record.title}`,
        amount: n,
        paymentDate: new Date().toISOString().slice(0, 10),
        reference: reference.trim() || undefined,
        invoiceId: record.id,
        status: 'paid'
      });
      setOpen(false);
      setAmount('');
      setReference('');
      load();
      onChanged?.();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <View>
      <SectionTitle>Payments</SectionTitle>
      <Panel style={{ padding: spacing.md }}>
        <View style={styles.figures}>
          <Figure label="Total" value={money(ledger.total, cur)} />
          <Figure label="Paid" value={money(ledger.paid, cur)} color={colors.success} />
          <Figure label="Balance due" value={money(ledger.balance, cur)} color={ledger.balance > 0 ? colors.danger : undefined} />
        </View>
        {ledger.canSeePayments
          ? ledger.payments.map((p) => (
              <Text key={p.id} style={styles.line} onPress={() => onOpenRecord?.('payments', p.id)}>
                {p.code} · {p.date} · {money(p.applied, cur)}{p.counts ? '' : ` (${p.status})`}
              </Text>
            ))
          : null}
        {ledger.canRecordPayment && ledger.balance > 0 ? (
          open ? (
            <View style={{ marginTop: spacing.md }}>
              <TextInput style={styles.input} keyboardType="decimal-pad" placeholder={`Amount received (${money(ledger.balance, cur)} due)`} placeholderTextColor={colors.textMuted} value={amount} onChangeText={setAmount} />
              <TextInput style={styles.input} placeholder="Reference (optional)" placeholderTextColor={colors.textMuted} value={reference} onChangeText={setReference} />
              {error ? <Text style={styles.error}>{error}</Text> : null}
              <View style={styles.buttons}>
                <Button title="Cancel" variant="secondary" size="sm" onPress={() => setOpen(false)} />
                <Button title="Save payment" size="sm" loading={busy} onPress={save} />
              </View>
            </View>
          ) : (
            <View style={{ marginTop: spacing.md }}>
              <Button title="Record payment" size="sm" onPress={() => { setAmount(String(ledger.balance)); setOpen(true); }} />
            </View>
          )
        ) : null}
      </Panel>
    </View>
  );
}

function CaseSla({ record }) {
  const { activeCode } = useCrm();
  const [sla, setSla] = useState(null);
  useEffect(() => {
    crmApi.caseSla(activeCode, record.id).then(setSla).catch(() => setSla(null));
  }, [activeCode, record.id, record.version]);
  if (!sla || !sla.timers?.length) return null;
  return (
    <View>
      <SectionTitle>Service level</SectionTitle>
      <Panel style={{ padding: spacing.md }}>
        {sla.policy ? <Text style={styles.muted}>Policy: {sla.policy.label}</Text> : null}
        {sla.timers.map((t) => {
          const [label, tone] = SLA[t.state] || SLA.running;
          const pct = Math.min(100, Math.round((t.elapsedMinutes / Math.max(1, t.targetMinutes)) * 100));
          const late = t.state === 'breached' || t.state === 'missed';
          return (
            <View key={t.milestone} style={{ marginTop: spacing.md }}>
              <View style={styles.rowBetween}>
                <Text style={styles.strong}>{t.milestone === 'first_response' ? 'First response' : 'Resolution'}</Text>
                <StatusPill label={label} tone={tone} />
              </View>
              <View style={styles.track}>
                <View style={[styles.fill, { width: `${pct}%`, backgroundColor: late ? colors.danger : pct >= 80 ? colors.warning : colors.primary }]} />
              </View>
              <Text style={styles.muted}>
                {t.state === 'running' || t.state === 'paused'
                  ? `${minutesText(t.remainingMinutes)} left of ${minutesText(t.targetMinutes)}`
                  : t.state === 'breached'
                    ? `Overdue by ${minutesText(t.remainingMinutes)}`
                    : `Target ${minutesText(t.targetMinutes)}, took ${minutesText(t.elapsedMinutes)}`}
              </Text>
            </View>
          );
        })}
      </Panel>
    </View>
  );
}

function ContractRenewal({ record, onOpenRecord }) {
  const { activeCode, can } = useCrm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const status = String(record.values?.status || '');
  if (!can('contracts', 'create') || ['renewed', 'cancelled', 'draft'].includes(status)) return null;
  const renew = async () => {
    setBusy(true);
    setError('');
    try {
      const next = await crmApi.renewContract(activeCode, record.id, {});
      onOpenRecord?.('contracts', next.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <View>
      <SectionTitle>Renewal</SectionTitle>
      <Panel style={{ padding: spacing.md }}>
        <Text style={styles.muted}>Creates the next term as a new contract with the same customer, length and value, and marks this one as renewed.</Text>
        {error ? <Text style={styles.error}>{error}</Text> : null}
        <View style={{ marginTop: spacing.md }}>
          <Button title="Renew contract" size="sm" loading={busy} onPress={renew} />
        </View>
      </Panel>
    </View>
  );
}

const LINE_OBJECTS = ['quotes', 'sales_orders', 'invoices', 'work_orders', 'contracts', 'opportunities', 'credit_notes'];

// The priced items of a document, and the step to the next document (D-120).
function DocumentLines({ object, record, onOpenRecord }) {
  const { activeCode, can } = useCrm();
  const [doc, setDoc] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    crmApi.documentLines(activeCode, object, record.id).then(setDoc).catch(() => setDoc(null));
  }, [activeCode, object, record.id, record.version]);
  if (!doc || !doc.pricing?.lines?.length) return null;
  const p = doc.pricing;
  const status = String(record.values?.status || '');
  const approval = String(record.values?.approvalStatus || '');
  const next =
    object === 'quotes' && can('sales_orders', 'create') && !['declined', 'expired'].includes(status) && approval !== 'pending' && approval !== 'rejected'
      ? ['Create order', () => crmApi.convertQuote(activeCode, record.id)]
      : object === 'sales_orders' && can('invoices', 'create') && status !== 'cancelled'
        ? ['Create invoice', () => crmApi.invoiceOrder(activeCode, record.id)]
        : object === 'work_orders' && can('invoices', 'create') && status === 'completed' && !record.values?.invoiceId
          ? ['Create invoice', () => crmApi.invoiceWorkOrder(activeCode, record.id)]
          : null;
  const go = async () => {
    setBusy(true);
    setError('');
    try {
      const made = await next[1]();
      onOpenRecord?.(made.object, made.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <View>
      <SectionTitle>Items and pricing</SectionTitle>
      <Panel style={{ padding: spacing.md }}>
        {p.lines.map((l, i) => (
          <View key={i} style={[styles.rowBetween, { marginBottom: 6, paddingLeft: l.bundleOf != null ? spacing.md : 0 }]}>
            <Text style={[styles.muted, { flex: 1, marginTop: 0, color: colors.textPrimary }]} numberOfLines={2}>{l.quantity} × {l.name}</Text>
            <Text style={styles.strong}>{money(l.total, p.currency)}</Text>
          </View>
        ))}
        <View style={[styles.figures, { marginTop: spacing.sm, borderTopWidth: 1, borderTopColor: colors.border, paddingTop: spacing.sm }]}>
          <Figure label="Discount" value={money(p.discount, p.currency)} />
          <Figure label="Tax" value={money(p.tax, p.currency)} />
          <Figure label="Total" value={money(p.total, p.currency)} />
        </View>
        {approval === 'pending' ? <Text style={styles.muted}>The discount on this quote is waiting for approval.</Text> : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
        {next ? (
          <View style={{ marginTop: spacing.md }}>
            <Button title={next[0]} size="sm" loading={busy} onPress={go} />
          </View>
        ) : null}
      </Panel>
    </View>
  );
}

// Who is involved: contact roles and the record's team (D-122, D-123).
function PeopleOnRecord({ object, record, onOpenRecord }) {
  const { activeCode } = useCrm();
  const [roles, setRoles] = useState([]);
  const [team, setTeam] = useState([]);
  useEffect(() => {
    crmApi.contactRoles(activeCode, object, record.id).then((r) => setRoles(r.data || [])).catch(() => setRoles([]));
    crmApi.recordTeam(activeCode, object, record.id).then((r) => setTeam(r.data || [])).catch(() => setTeam([]));
  }, [activeCode, object, record.id]);
  if (!roles.length && !team.length) return null;
  return (
    <View>
      {roles.length ? (
        <>
          <SectionTitle>Contact roles</SectionTitle>
          <Panel style={{ padding: spacing.md }}>
            {roles.map((r) => (
              <Text key={r.id} style={styles.line} onPress={() => onOpenRecord?.('contacts', r.contactId)}>
                {r.contact} · {r.role}{r.isPrimary ? ' · primary' : ''}{r.isActive ? '' : ' · ended'}
              </Text>
            ))}
          </Panel>
        </>
      ) : null}
      {team.length ? (
        <>
          <SectionTitle>Team</SectionTitle>
          <Panel style={{ padding: spacing.md }}>
            {team.map((m) => (
              <Text key={m.identityId} style={[styles.muted, { color: colors.textPrimary }]}>
                {m.name} · {m.teamRole || 'Member'} · {m.accessLevel === 'read' ? 'can view' : m.accessLevel === 'write' ? 'can edit' : 'full access'}
              </Text>
            ))}
          </Panel>
        </>
      ) : null}
    </View>
  );
}

function CaseWorkOrder({ record, onOpenRecord }) {
  const { activeCode, can } = useCrm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  if (!can('work_orders', 'create')) return null;
  const make = async () => {
    setBusy(true);
    setError('');
    try {
      const wo = await crmApi.caseWorkOrder(activeCode, record.id);
      onOpenRecord?.('work_orders', wo.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <View style={{ marginTop: spacing.md }}>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Button title="Create work order" variant="secondary" size="sm" loading={busy} onPress={make} />
    </View>
  );
}

const PEOPLE_OBJECTS = ['accounts', 'opportunities', 'cases', 'contracts', 'work_orders'];

export function EnterprisePanels({ object, record, onChanged, onOpenRecord }) {
  return (
    <>
      {LINE_OBJECTS.includes(object) ? <DocumentLines object={object} record={record} onOpenRecord={onOpenRecord} /> : null}
      {object === 'invoices' ? <InvoicePayments record={record} onChanged={onChanged} onOpenRecord={onOpenRecord} /> : null}
      {object === 'cases' ? <CaseSla record={record} /> : null}
      {object === 'cases' ? <CaseWorkOrder record={record} onOpenRecord={onOpenRecord} /> : null}
      {object === 'contracts' ? <ContractRenewal record={record} onOpenRecord={onOpenRecord} /> : null}
      {PEOPLE_OBJECTS.includes(object) ? <PeopleOnRecord object={object} record={record} onOpenRecord={onOpenRecord} /> : null}
    </>
  );
}

const styles = StyleSheet.create({
  figures: { flexDirection: 'row', gap: spacing.md },
  figureLabel: { fontSize: 12, color: colors.textSecondary },
  figureValue: { fontSize: 16, fontWeight: '700', color: colors.textPrimary, marginTop: 2 },
  line: { fontSize: 13, color: colors.primary, marginTop: spacing.sm },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: radii.md, paddingHorizontal: spacing.md, paddingVertical: 10, fontSize: 15, color: colors.textPrimary, marginBottom: spacing.sm, backgroundColor: colors.bgCard },
  buttons: { flexDirection: 'row', justifyContent: 'flex-end', gap: spacing.sm },
  error: { color: colors.danger, fontSize: 13, marginBottom: spacing.sm },
  muted: { fontSize: 13, color: colors.textSecondary, marginTop: 4 },
  strong: { fontSize: 14, fontWeight: '600', color: colors.textPrimary },
  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  track: { height: 6, borderRadius: 3, backgroundColor: colors.bgMutedDark, marginTop: 6, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 }
});
