import React, { useCallback, useEffect, useRef, useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TextInput, TouchableOpacity, ActivityIndicator } from 'react-native';
import { Clock, Send } from 'lucide-react';
import { colors, radii, spacing, typography } from '../../theme';
import { DetailScreenHeader } from '../../components/DetailScreenHeader';
import { useAuth } from '../../context/AuthContext';
import { apiClient } from '../../services/api';

// A support ticket is a conversation: the person's messages on the right ("You"),
// support replies on the left with who answered and their role. The person can keep
// writing; a resolved ticket reopens when they do.

function initials(name) {
  const parts = String(name || '').trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return '?';
  return ((parts[0][0] || '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase();
}

function timeLabel(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  const time = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return sameDay ? time : `${d.toLocaleDateString([], { day: 'numeric', month: 'short' })}, ${time}`;
}

function conversationOf(ticket) {
  if (Array.isArray(ticket?.messages) && ticket.messages.length) return ticket.messages;
  // Older server: the opening message and the single reply.
  const list = [{ id: `${ticket.id}-0`, sender: 'user', author_name: ticket.user_name, body: ticket.message || ticket.description || '', created_at: ticket.created_at }];
  if (ticket.admin_reply) {
    list.push({ id: `${ticket.id}-r`, sender: 'support', author_name: 'Support team', body: ticket.admin_reply, created_at: ticket.replied_at || ticket.updated_at });
  }
  return list;
}

function Avatar({ name, mine }) {
  return (
    <View style={[styles.avatar, mine ? styles.avatarMine : styles.avatarSupport]}>
      <Text style={[styles.avatarText, mine && { color: colors.textWhite }]}>{initials(name)}</Text>
    </View>
  );
}

function Bubble({ m, myName }) {
  const mine = m.sender === 'user';
  const who = mine ? 'You' : m.author_name || 'Support team';
  const role = mine ? '' : m.author_role || 'Support team';
  return (
    <View style={[styles.row, mine && styles.rowMine]}>
      <Avatar name={mine ? myName : who} mine={mine} />
      <View style={[styles.bubbleCol, mine && { alignItems: 'flex-end' }]}>
        <Text style={styles.author} numberOfLines={1}>
          <Text style={styles.authorName}>{who}</Text>
          {role ? ` · ${role}` : ''}
          {` · ${timeLabel(m.created_at)}`}
        </Text>
        <View style={[styles.bubble, mine ? styles.bubbleMine : styles.bubbleSupport]}>
          <Text style={[styles.bubbleText, mine && { color: colors.textWhite }]}>{m.body}</Text>
        </View>
      </View>
    </View>
  );
}

export function SupportTicketDetailScreen({ ticket: initial, onBack }) {
  const { token, user } = useAuth();
  const [ticket, setTicket] = useState(initial);
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const scrollRef = useRef(null);

  const refresh = useCallback(async () => {
    if (!initial?.id) return;
    const fresh = await apiClient.getMySupportTicket(initial.id, token);
    if (fresh) setTicket(fresh);
  }, [initial?.id, token]);

  useEffect(() => {
    refresh();
    // New replies from the support team show up while the ticket is open.
    const timer = setInterval(refresh, 15000);
    return () => clearInterval(timer);
  }, [refresh]);

  const messages = ticket ? conversationOf(ticket) : [];
  useEffect(() => {
    const t = setTimeout(() => scrollRef.current?.scrollToEnd?.({ animated: true }), 50);
    return () => clearTimeout(t);
  }, [messages.length]);

  if (!ticket) return null;

  const status = ticket.status || 'open';
  const statusLabel = status === 'resolved' ? 'Resolved' : status === 'in_progress' ? 'In progress' : 'Open';
  const waiting = !messages.some((m) => m.sender === 'support');
  const myName = user?.name || ticket.user_name || 'You';

  const send = async () => {
    const text = draft.trim();
    if (!text || sending) return;
    setSending(true);
    setError('');
    try {
      const updated = await apiClient.sendSupportMessage(ticket.id, text, token);
      if (updated) setTicket(updated);
      setDraft('');
    } catch (e) {
      setError(e.message || 'Could not send your message. Try again.');
    } finally {
      setSending(false);
    }
  };

  return (
    <View style={styles.root}>
      <DetailScreenHeader title="Ticket Detail" subtitle="My Tickets" onBack={onBack} />
      <View style={styles.summary}>
        <Text style={styles.id}>#{String(ticket.id).slice(0, 8)}</Text>
        <Text style={styles.subject}>{ticket.subject}</Text>
        <Text style={styles.meta}>
          {statusLabel}
          {ticket.category ? ` · ${String(ticket.category).replace(/_/g, ' ')}` : ''}
          {ticket.created_at ? ` · ${new Date(ticket.created_at).toLocaleString()}` : ''}
        </Text>
      </View>

      <ScrollView ref={scrollRef} style={styles.thread} contentContainerStyle={styles.threadContent} showsVerticalScrollIndicator={false}>
        {messages.map((m) => (
          <Bubble key={m.id} m={m} myName={myName} />
        ))}
        {waiting ? (
          <View style={styles.pendingBox}>
            <Clock size={14} color={colors.warning} style={{ marginRight: 6 }} />
            <Text style={styles.pendingText}>Our team is reviewing your request. You'll see the reply here.</Text>
          </View>
        ) : null}
      </ScrollView>

      <View style={styles.composer}>
        {status === 'resolved' ? <Text style={styles.note}>This ticket is resolved. Sending a message reopens it.</Text> : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
        <View style={styles.composerRow}>
          <TextInput
            value={draft}
            onChangeText={setDraft}
            placeholder="Write a message…"
            placeholderTextColor={colors.textMuted}
            multiline
            maxLength={4000}
            style={styles.input}
            accessibilityLabel="Message to support"
            onKeyPress={(e) => {
              // On the web, Enter sends and Shift+Enter adds a line.
              if (e?.nativeEvent?.key === 'Enter' && !e?.nativeEvent?.shiftKey && typeof window !== 'undefined') {
                e.preventDefault?.();
                send();
              }
            }}
          />
          <TouchableOpacity
            style={[styles.sendBtn, (!draft.trim() || sending) && styles.sendBtnOff]}
            onPress={send}
            disabled={!draft.trim() || sending}
            accessibilityRole="button"
            accessibilityLabel="Send message"
          >
            {sending ? <ActivityIndicator size="small" color={colors.textWhite} /> : <Send size={18} color={colors.textWhite} />}
          </TouchableOpacity>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.bgMuted },
  summary: { paddingHorizontal: spacing.lg, paddingTop: spacing.md, paddingBottom: spacing.sm, borderBottomWidth: 1, borderBottomColor: colors.border, backgroundColor: colors.bgSurface },
  id: { fontSize: 12, fontWeight: '700', color: colors.textMuted },
  subject: { ...typography.titleSmall, marginTop: 2 },
  meta: { fontSize: 12, color: colors.textSecondary, marginTop: 4 },
  thread: { flex: 1 },
  threadContent: { padding: spacing.lg, paddingBottom: spacing.xl },
  row: { flexDirection: 'row', alignItems: 'flex-end', marginBottom: spacing.md },
  rowMine: { flexDirection: 'row-reverse' },
  avatar: { width: 32, height: 32, borderRadius: 16, alignItems: 'center', justifyContent: 'center', marginHorizontal: 8 },
  avatarMine: { backgroundColor: colors.primary },
  avatarSupport: { backgroundColor: colors.goldLight },
  avatarText: { fontSize: 12, fontWeight: '700', color: colors.primary },
  bubbleCol: { maxWidth: '76%', alignItems: 'flex-start' },
  author: { fontSize: 11, color: colors.textMuted, marginBottom: 4 },
  authorName: { fontWeight: '700', color: colors.textSecondary },
  bubble: { paddingHorizontal: 14, paddingVertical: 9, borderRadius: 18 },
  bubbleMine: { backgroundColor: colors.primary, borderBottomRightRadius: 6 },
  bubbleSupport: { backgroundColor: colors.bgSurface, borderWidth: 1, borderColor: colors.border, borderBottomLeftRadius: 6 },
  bubbleText: { fontSize: 14, lineHeight: 20, color: colors.textPrimary },
  pendingBox: {
    marginTop: spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.warningLight,
    borderRadius: radii.md,
    padding: spacing.md
  },
  pendingText: { fontSize: 13, color: '#92400E', fontWeight: '500', flex: 1 },
  composer: { borderTopWidth: 1, borderTopColor: colors.border, backgroundColor: colors.bgSurface, paddingHorizontal: spacing.md, paddingTop: spacing.sm, paddingBottom: spacing.md },
  composerRow: { flexDirection: 'row', alignItems: 'flex-end' },
  input: {
    flex: 1,
    minHeight: 42,
    maxHeight: 120,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 21,
    paddingHorizontal: 16,
    paddingVertical: 10,
    fontSize: 14,
    color: colors.textPrimary,
    backgroundColor: colors.bgMuted
  },
  sendBtn: { width: 42, height: 42, borderRadius: 21, marginLeft: 8, backgroundColor: colors.primary, alignItems: 'center', justifyContent: 'center' },
  sendBtnOff: { opacity: 0.4 },
  note: { fontSize: 12, color: colors.textSecondary, marginBottom: 6 },
  error: { fontSize: 12, color: colors.danger, marginBottom: 6 }
});
