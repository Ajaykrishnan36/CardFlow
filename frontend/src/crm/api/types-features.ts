// Types for the everyday CRM features: saved views, filters, timeline, files,
// favorites, notifications, workflows, API keys, webhooks, email, campaigns, teams, SSO.
import type { LookupValue, ObjectKey, RecordRow } from './types';

// ---- filters & views ----

export type FilterOp =
  | 'eq' | 'neq' | 'contains' | 'notContains' | 'startsWith' | 'endsWith' | 'in' | 'notIn' | 'all'
  | 'gt' | 'gte' | 'lt' | 'lte' | 'between'
  | 'empty' | 'notEmpty'
  | 'before' | 'after' | 'on' | 'today' | 'yesterday' | 'tomorrow' | 'overdue' | 'lastDays' | 'nextDays'
  | 'thisWeek' | 'lastWeek' | 'thisMonth' | 'lastMonth' | 'thisQuarter' | 'thisYear'
  | 'isMe' | 'notMe' | 'myTeam';

export interface FilterCondition {
  field: string;
  op: FilterOp;
  value?: unknown;
}

export interface FilterGroup {
  op: 'and' | 'or';
  filters: FilterNode[];
}

export type FilterNode = FilterCondition | FilterGroup;

export function isGroup(n: FilterNode): n is FilterGroup {
  return !('field' in n) || !n.field;
}

export interface SortSpec {
  field: string;
  dir: 'asc' | 'desc';
}

export type ViewKind = 'table' | 'kanban' | 'calendar';

export interface ViewDefinition {
  filter?: FilterGroup;
  sorts?: SortSpec[];
  columns?: string[];
  columnWidths?: Record<string, number>;
  groupBy?: string;
  calendarField?: string;
  calendarMode?: 'month' | 'week' | 'day';
  aggregates?: string[];
  q?: string;
  density?: '' | 'compact';
}

export interface SavedView {
  id: string;
  object: ObjectKey;
  name: string;
  kind: ViewKind;
  visibility: 'personal' | 'shared';
  ownerId?: string;
  ownerName?: string;
  definition: ViewDefinition;
  position: number;
  canEdit: boolean;
  updatedAt: string;
}

export interface RecordGroup {
  value: string;
  label: string;
  tone?: 'neutral' | 'primary' | 'success' | 'warning' | 'danger';
  count: number;
  rows: RecordRow[];
}

export interface BulkResult {
  processed: number;
  failed: number;
  failures: Array<{ id: string; title?: string; error: string }>;
}

export interface ImportResult {
  rows: number;
  created: number;
  updated: number;
  skipped: number;
  failed: number;
  errors: Array<{ row: number; errors: Record<string, string> }>;
  dryRun: boolean;
  durationMs: number;
}

export interface SearchGroup {
  object: ObjectKey;
  label: string;
  icon?: string;
  rows: Array<{ id: string; code?: string; title: string; subtitle?: string; status?: string }>;
}

// ---- timeline, files, favorites, notifications ----

export interface TimelineItem {
  id: string;
  kind: string; // record.created | record.updated | note | call | email | task | event | communication | file | app.* | ticket.*
  title: string;
  body?: string;
  detail?: Record<string, unknown> & {
    changes?: Array<{ field: string; label: string; from: unknown; to: unknown }>;
  };
  actor?: LookupValue;
  link?: LookupValue;
  status?: string;
  at: string;
  canDelete?: boolean;
}

export interface FileInfo {
  id: string;
  name: string;
  contentType: string;
  size: number;
  field?: string;
  createdBy?: string;
  createdAt: string;
}

export interface Favorite {
  id: string;
  kind: 'record' | 'view';
  object: ObjectKey;
  targetId: string;
  label: string;
  icon?: string;
  path: string;
}

export interface AppNotification {
  id: number;
  kind: string;
  title: string;
  body?: string;
  link?: string;
  actor?: string;
  readAt?: string;
  createdAt: string;
}

// ---- developer: API keys & webhooks ----

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  access: 'full' | 'permission_set';
  permissionSetId?: string;
  permissionSet?: string;
  createdBy: string;
  createdAt: string;
  expiresAt?: string;
  lastUsedAt?: string;
  revokedAt?: string;
  secret?: string;
}

export interface ApiKeysResponse {
  data: ApiKey[];
  apiAccess: boolean;
  webhooks: boolean;
  baseUrl: string;
  permissionSets: Array<{ id: string; name: string }>;
}

export interface Webhook {
  id: string;
  url: string;
  description: string;
  events: string[];
  objects: string[];
  status: 'active' | 'paused';
  lastDeliveryAt?: string;
  lastStatus?: number;
  createdAt: string;
  secret?: string;
  failing: number;
}

export interface WebhookDelivery {
  id: number;
  eventId: string;
  event: string;
  status: 'pending' | 'delivered' | 'failed';
  attempts: number;
  responseStatus?: number;
  responseBody?: string;
  nextAttemptAt?: string;
  createdAt: string;
  deliveredAt?: string;
  payload?: unknown;
}

// ---- workflows ----

export type TriggerType = 'record.created' | 'record.updated' | 'record.upserted' | 'record.deleted' | 'manual' | 'schedule' | 'webhook';

export interface WorkflowInput {
  key: string;
  label: string;
  type: 'text' | 'number' | 'date' | 'boolean' | 'select';
  required?: boolean;
  options?: string[];
}

export interface WorkflowTrigger {
  type: TriggerType;
  object?: string;
  fields?: string[];
  filter?: FilterGroup;
  schedule?: { every?: number; unit?: 'minutes' | 'hours' | 'days' | 'weeks'; cron?: string };
  manual?: { mode?: 'single' | 'bulk' | 'global'; everyone?: boolean; form?: WorkflowInput[] };
}

export type StepType =
  | 'create_record' | 'update_record' | 'upsert_record' | 'delete_record' | 'find_records'
  | 'send_email' | 'http_request' | 'notify' | 'assign' | 'code' | 'delay' | 'if' | 'loop' | 'stop';

export interface WorkflowStep {
  id: string;
  type: StepType;
  name?: string;
  config: Record<string, unknown>;
  then?: WorkflowStep[];
  else?: WorkflowStep[];
}

export interface WorkflowDef {
  trigger: WorkflowTrigger;
  steps: WorkflowStep[];
}

export interface Workflow {
  id: string;
  name: string;
  description: string;
  status: 'draft' | 'active' | 'inactive';
  draft: WorkflowDef;
  published?: WorkflowDef;
  version: number;
  hasChanges: boolean;
  webhookUrl?: string;
  nextRunAt?: string;
  lastRunAt?: string;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  runs: { total: number; failed: number };
}

export interface StepLog {
  stepId: string;
  type: StepType;
  name?: string;
  status: 'ok' | 'failed' | 'skipped' | 'waiting';
  output?: unknown;
  error?: string;
  at: string;
  durationMs: number;
}

export interface WorkflowRun {
  id: string;
  workflowId: string;
  version: number;
  status: 'queued' | 'running' | 'waiting' | 'completed' | 'failed' | 'stopped';
  trigger: Record<string, unknown>;
  steps: StepLog[];
  error?: string;
  resumeAt?: string;
  startedBy?: string;
  startedAt: string;
  finishedAt?: string;
}

// ---- email & calendar ----

export interface Mailbox {
  id: string;
  provider: 'google' | 'microsoft' | 'imap';
  email: string;
  status: 'active' | 'error' | 'paused';
  error?: string;
  syncEmail: boolean;
  syncCalendar: boolean;
  visibility: 'share_everything' | 'subject' | 'metadata';
  autoCreateContacts: boolean;
  lastSyncedAt?: string;
  messages: number;
}

export interface MailboxesResponse {
  data: Mailbox[];
  blocklist: string[];
  providers: { google: boolean; microsoft: boolean; imap: boolean };
  crmSender: boolean;
  canSend: boolean;
}

export interface Campaign {
  id: string;
  name: string;
  subject: string;
  bodyHtml: string;
  fromName: string;
  replyTo: string;
  object: string;
  emailField: string;
  filter?: FilterGroup;
  status: 'draft' | 'scheduled' | 'sending' | 'sent' | 'failed' | 'cancelled';
  scheduledAt?: string;
  stats: Record<string, number>;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  sentAt?: string;
}

export interface CampaignRecipient {
  recordId: string;
  email: string;
  name: string;
  status: 'pending' | 'sent' | 'failed' | 'unsubscribed' | 'skipped';
  error?: string;
  sentAt?: string;
}

// ---- teams & SSO ----

export interface Team {
  id: string;
  name: string;
  description: string;
  members: Array<{ identityId: string; name: string; email: string }>;
  createdAt: string;
}

export interface SSOSettings {
  enabled: boolean;
  name: string;
  idpMetadataXml?: string;
  idpEntityId?: string;
  domains: string[];
  jitProvisioning: boolean;
  defaultRoleKey: string;
  spEntityId: string;
  spAcsUrl: string;
  spMetadataUrl: string;
  signInUrl: string;
  configured: boolean;
  updatedAt?: string;
  loginMethodOn: boolean;
  /** SAML 2.0 or OpenID Connect (D-82). */
  kind: 'saml' | 'oidc';
  oidcIssuer: string;
  oidcClientId: string;
  oidcSecretSet: boolean;
  oidcRedirectUrl: string;
}

/** Email conversations on a record (D-80). */
export interface ThreadMessage {
  id: string;
  direction: 'inbound' | 'outbound';
  from: string;
  fromName?: string;
  to: string[];
  cc: string[] | null;
  subject: string;
  body?: string;
  html?: string;
  status: string;
  error?: string;
  at: string;
  /** The mailbox owner shares less: "body" hides the text, "all" hides the subject too. */
  hidden?: 'body' | 'all';
  mailboxId?: string;
}

export interface EmailThread {
  id: string;
  subject: string;
  count: number;
  lastAt: string;
  snippet: string;
  participants: string[];
  messages: ThreadMessage[];
}

/** A product's shareable invite link (D-83). */
export interface InviteLinkSettings {
  configured: boolean;
  enabled: boolean;
  url?: string;
  domains: string[];
  roleKey: string;
  uses: number;
  updatedAt?: string;
}
