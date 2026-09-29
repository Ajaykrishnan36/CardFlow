// Response shapes of /api/crm/v1 (mirrors the Go structs in backend/internal/crm).

export interface AuthStep {
  mfaRequired: boolean;
  mfaEnrollmentRequired: boolean;
  mustChangePassword: boolean;
  next: string;
}

export interface NextStep {
  next: string;
}

export interface Membership {
  id: string;
  workspaceId: string;
  workspaceCode: string;
  workspaceName: string;
  isPlatformWorkspace: boolean;
  status: string;
  authVersion: number;
  roleKey?: string;
  roleName?: string;
  roleRank: number;
}

export interface Me {
  identity: {
    id: string;
    displayName: string;
    email?: string;
    phone?: string;
    isPlatformOwner: boolean;
    locale: string;
    timezone: string;
    lastLoginAt?: string;
  };
  session: {
    audience: 'owner' | 'workspace';
    mfaRequired: boolean;
    mfaPassed: boolean;
    mfaEnrolled: boolean;
    mfaEnforced: boolean;
    mustChangePassword: boolean;
    privileged: boolean;
    idleExpiresAt: string;
  };
  memberships: Membership[];
  authVersion: number;
  next: string;
}

export interface NavItem {
  key: string;
  label: string;
  path: string;
  icon: string;
  group?: string;
  available: boolean;
}

export interface Capabilities {
  audience: 'owner' | 'workspace' | 'portal';
  actions: string[];
  navigation: NavItem[];
}

export interface MfaEnrollment {
  secret: string;
  otpauthUrl: string;
  qrDataUrl: string;
}

export interface MfaConfirmation {
  recoveryCodes: string[];
  next: string;
}

export interface SessionInfo {
  id: string;
  ip?: string;
  userAgent?: string;
  createdAt: string;
  lastSeenAt: string;
  current: boolean;
}

export interface Kpi {
  key: string;
  label: string;
  value: number;
  hint?: string;
  /** List page the card opens. */
  path?: string;
  /** Nav icon key for objects defined as data and connected apps. */
  icon?: string;
}

export interface ChecklistStep {
  key: string;
  title: string;
  description: string;
  status: 'done' | 'todo' | 'upcoming';
  path?: string;
}

export interface RecentWorkspace {
  id: string;
  code: string;
  name: string;
  status: string;
  products: number;
  members: number;
  createdAt: string;
}

export interface OwnerDashboard {
  kpis: Kpi[];
  checklist: ChecklistStep[];
  recentWorkspaces: RecentWorkspace[];
  refreshedAt: string;
}

// ---------------------------------------------------------------------------
// M2 Platform: products, workspaces, invitations, users, records, audit.
// ---------------------------------------------------------------------------

export interface Page<T> {
  data: T[];
  total: number;
}

export type SystemRoleKey = 'SUPER_ADMIN' | 'ADMIN' | 'STAFF' | 'END_USER';
/** A system role key or a custom role key (upper snake case, e.g. SALES_MANAGER). */
export type RoleKey = string;

export interface Option {
  value: string;
  label: string;
}

// ---- Products ----
export interface ProductUserType {
  key: string;
  label: string;
  description?: string;
  allowedRoles: SystemRoleKey[];
}

export interface ProductRole {
  key: SystemRoleKey;
  label: string;
  enabled: boolean;
}

export interface ProductStage {
  key: string;
  label: string;
  probability: number;
}

export interface ProductConfig {
  accentColor: string;
  modules: string[];
  userTypes: ProductUserType[];
  roles: ProductRole[];
  leadStatuses: Option[];
  pipelineStages: ProductStage[];
  conversion: { createContact: boolean; createOpportunity: boolean; requireQualified: boolean };
  loginMethods: { password: boolean; otp: boolean; google: boolean; linkedin: boolean };
  selfRegistration: boolean;
  integrations: { apiAccess: boolean; webhooks: boolean };
}

export type ProductStatus = 'draft' | 'active' | 'archived';

export interface ProductSummary {
  id: string;
  key: string;
  name: string;
  description?: string;
  icon?: string;
  status: ProductStatus;
  currentVersion?: number;
  workspaces: number;
  hasUnpublishedChanges: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ModuleInfo {
  key: string;
  label: string;
  description: string;
  group: string;
  /** Works today (its objects exist). Planned modules can't be switched on yet. */
  available: boolean;
  /** Record objects this module switches on. */
  objects?: string[];
  /** One of the owner's custom objects. */
  custom?: boolean;
  /** Belongs to a connected app. */
  hidden?: boolean;
}

export interface ProductVersion {
  version: number;
  publishedAt: string;
  publishedBy?: string;
  workspaces: number;
}

export interface ProductDetail extends ProductSummary {
  draftConfig: ProductConfig;
  publishedConfig?: ProductConfig;
  versions: ProductVersion[];
  assignedWorkspaces: Array<{ id: string; code: string; name: string; status: string; configVersion: number; assignmentStatus: string }>;
  moduleCatalog: ModuleInfo[];
}

export interface ProductCreateBody {
  name: string;
  key: string;
  description?: string;
  icon?: string;
}

export interface ProductUpdateBody {
  name?: string;
  description?: string;
  icon?: string;
  draftConfig?: ProductConfig;
}

// ---- Workspaces & invitations ----
export type WorkspaceStatus = 'draft' | 'provisioning' | 'active' | 'failed' | 'suspended';

export interface WorkspaceSummary {
  id: string;
  code: string;
  name: string;
  status: WorkspaceStatus;
  timezone: string;
  locale: string;
  currency: string;
  products: number;
  members: number;
  pendingInvites: number;
  createdAt: string;
}

export interface WorkspaceProduct {
  productId: string;
  key: string;
  name: string;
  status: 'active' | 'suspended';
  productStatus: ProductStatus;
  configVersion: number;
  latestVersion?: number;
  assignedAt: string;
}

export interface WorkspaceMember {
  membershipId: string;
  identityId: string;
  displayName: string;
  email?: string;
  status: 'invited' | 'active' | 'suspended' | 'revoked';
  roleKey?: RoleKey;
  roleName?: string;
  lastLoginAt?: string;
  createdAt: string;
}

export interface Invitation {
  id: string;
  email?: string;
  displayName: string;
  roleKey: RoleKey;
  roleName: string;
  status: 'pending' | 'delivered' | 'accepted' | 'expired' | 'revoked' | 'delivery_failed';
  expiresAt: string;
  createdAt: string;
  acceptedAt?: string;
  /** Only returned in local/dev, where invitation emails go to the API log (like OTP previews). */
  devAcceptUrl?: string;
}

export interface WorkspaceDetail extends WorkspaceSummary {
  productList: WorkspaceProduct[];
  memberList: WorkspaceMember[];
  invitations: Invitation[];
  account?: { id: string; code: string; name: string };
}

export interface ProvisionBody {
  name: string;
  code: string;
  timezone: string;
  locale: string;
  currency: string;
  productIds: string[];
  superAdmin?: { name: string; email: string };
}

export interface ProvisionResult {
  workspace: WorkspaceDetail;
  invitation?: Invitation;
}

export interface WorkspaceUpdateBody {
  name?: string;
  timezone?: string;
  locale?: string;
  currency?: string;
  status?: 'active' | 'suspended';
}

export interface InviteBody {
  name: string;
  email: string;
  roleKey: RoleKey;
  productIds?: string[];
}

export interface InvitationPreview {
  workspaceName: string;
  workspaceCode: string;
  roleName: string;
  displayName: string;
  email: string;
  identityExists: boolean;
  expiresAt: string;
  status: Invitation['status'];
}

// ---- Users ----
export interface UserSummary {
  id: string;
  displayName: string;
  email?: string;
  phone?: string;
  status: 'active' | 'suspended' | 'deleted';
  isPlatformOwner: boolean;
  mfaEnrolled: boolean;
  hasPassword: boolean;
  lastLoginAt?: string;
  createdAt: string;
  workspaceNames: string[];
}

export interface UserMembership {
  id: string;
  workspaceId: string;
  workspaceCode: string;
  workspaceName: string;
  isPlatformWorkspace: boolean;
  status: WorkspaceMember['status'];
  roleKey?: RoleKey;
  roleName?: string;
  createdAt: string;
  productIds: string[];
  permissionSets: Array<{ id: string; name: string }>;
  /** Role + permission sets + products combined — what this user can actually see and do. */
  effective: EffectiveAccess;
}

export interface LinkedRecord {
  object: ObjectKey;
  id: string;
  code: string;
  name: string;
}

export interface AuditEntry {
  id: number;
  createdAt: string;
  actorName?: string;
  actorKind: string;
  action: string;
  entityType?: string;
  entityId?: string;
  workspaceName?: string;
  ip?: string;
}

export interface UserDetail extends UserSummary {
  locale: string;
  timezone: string;
  emailVerified: boolean;
  phoneVerified: boolean;
  activeSessions: number;
  memberships: UserMembership[];
  linkedRecords: LinkedRecord[];
  invitations: Array<Invitation & { workspaceName: string }>;
  recentActivity: AuditEntry[];
}

export interface UserUpdateBody {
  displayName?: string;
  email?: string;
  phone?: string;
  locale?: string;
  timezone?: string;
  status?: 'active' | 'suspended';
}

// ---- Records (metadata-driven leads / accounts / contacts) ----
export type BuiltinObjectKey = 'leads' | 'accounts' | 'contacts';
/** Built-in objects plus objects defined as data (opportunities, tasks, the owner's custom objects…). */
export type ObjectKey = BuiltinObjectKey | (string & {});

export type FieldType =
  | 'text'
  | 'textarea'
  | 'email'
  | 'phone'
  | 'url'
  | 'number'
  | 'currency'
  | 'percent'
  | 'date'
  | 'datetime'
  | 'select'
  | 'multiselect'
  | 'boolean'
  | 'lookup';

export type LookupTarget = ObjectKey | 'users' | 'products' | 'workspaces';

export interface FieldDef {
  key: string;
  label: string;
  type: FieldType;
  required: boolean;
  /** System-maintained (code, created/updated, converted…): shown but never editable. */
  readOnly: boolean;
  /** Standard fields ship with the CRM; custom ones were added by the owner. */
  standard: boolean;
  options?: Option[];
  lookup?: LookupTarget;
  helpText?: string;
}

export interface LayoutSection {
  id: string;
  title: string;
  columns: 1 | 2;
  fields: string[];
}

/** Fields not placed in any section are hidden from the record page. */
export interface Layout {
  highlights: string[];
  sections: LayoutSection[];
}

export interface StatusOption extends Option {
  tone: 'neutral' | 'primary' | 'success' | 'warning' | 'danger';
}

export interface ObjectMeta {
  object: ObjectKey;
  labelSingular: string;
  labelPlural: string;
  codePrefix: string;
  fields: FieldDef[];
  layout: Layout;
  defaultLayout: Layout;
  listColumns: string[];
  statusField?: string;
  statuses?: StatusOption[];
  /** Objects defined as data: their icon key and custom flag. */
  icon?: string;
  custom?: boolean;
}

export interface LookupValue {
  id: string;
  label: string;
  object?: LookupTarget;
}

export interface RecordWorkspaceRef {
  id: string;
  code: string;
  name: string;
  isPlatform: boolean;
}

export interface RecordRow {
  id: string;
  code: string;
  title: string;
  /** Set on owner lists across workspaces: where the record lives. */
  workspace?: RecordWorkspaceRef;
  values: Record<string, unknown>;
  /** Display labels for lookup fields, keyed by field key. */
  lookups: Record<string, LookupValue>;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface RelatedList {
  key: string;
  label: string;
  /** activities = timeline entries (no link); tickets = support tickets (link to the workspace Support page). */
  object: ObjectKey | 'workspaces' | 'users' | 'activities' | 'tickets' | 'app-users' | 'app-businesses' | 'app-cards';
  rows: Array<{ id: string; code?: string; title: string; subtitle?: string; status?: string }>;
}

export interface RecordPage extends Page<RecordRow> {
  /** Owner lists only: every workspace with how many records of this object it has (ignores q/status). */
  workspaces?: Array<RecordWorkspaceRef & { count: number }>;
}

export interface RecordDetail {
  record: RecordRow;
  related: RelatedList[];
  /** Lead only: what conversion produced. */
  conversion?: {
    convertedAt: string;
    account?: LookupValue;
    contact?: LookupValue;
    workspace?: LookupValue;
    invitation?: Invitation;
  };
}

export interface RecordListParams {
  /** Owner lists only: 'all' (default), 'platform', or a workspace code. */
  workspace?: string;
  q?: string;
  status?: string;
  sort?: string;
  dir?: 'asc' | 'desc';
  limit?: number;
  offset?: number;
}

export interface FieldCreateBody {
  label: string;
  key?: string;
  type: Exclude<FieldType, 'lookup'>;
  required?: boolean;
  options?: Option[];
  helpText?: string;
  /** Section to place the new field in; omitted → first section. */
  sectionId?: string;
}

export interface FieldUpdateBody {
  label?: string;
  required?: boolean;
  options?: Option[];
  helpText?: string;
}

export interface ConvertBody {
  account: { mode: 'new'; name: string; kind: 'business' | 'individual' } | { mode: 'existing'; accountId: string };
  createContact: boolean;
  /** Give the converted person a login (preferred over `provision`). Not allowed inside customer workspaces. */
  access?: GiveLoginBody;
  /** Legacy: create a customer workspace and invite them as its Super Admin. */
  provision?: {
    workspaceName: string;
    workspaceCode: string;
    productIds: string[];
    timezone?: string;
    currency?: string;
  };
}

export interface ConvertResult {
  accountId: string;
  contactId?: string;
  workspaceId?: string;
  identityId?: string;
  invitation?: Invitation;
}

// ---------------------------------------------------------------------------
// Access control: roles, permission sets, effective access, logins, workspace app.
// ---------------------------------------------------------------------------

export type ObjectAction = 'read' | 'create' | 'update' | 'delete' | 'convert' | 'export';
/** own = only records the user owns; workspace = every record in the workspace. */
export type RowScope = 'own' | 'workspace';

/** Shape of roles.base_rules and permission_sets.rules. Object keys are singular: lead, account, contact. */
export type FieldLevel = 'edit' | 'read' | 'hidden';

export interface AccessRules {
  objects: Record<string, ObjectAction[]>;
  rows: Record<string, { scope: RowScope }>;
  capabilities: string[];
  /** Field access per object (D-46): only restricted fields are listed; the rest are editable. */
  fields?: Record<string, Record<string, 'read' | 'hidden'>>;
}

/** GET /platform/workspaces/{id}/fields · /w/{code}/access/fields */
export interface FieldCatalogObject {
  key: string;
  object: string;
  label: string;
  fields: Array<{ key: string; label: string; type: string; standard: boolean; locked: boolean }>;
}

export interface AccessCatalog {
  objects: Array<{ key: string; label: string; module: string; actions: ObjectAction[] }>;
  actions: Array<{ key: ObjectAction; label: string }>;
  capabilities: Array<{ key: string; label: string; description: string }>;
  roles: Array<{ key: SystemRoleKey; name: string; rank: number; description: string; rules: AccessRules }>;
}

export interface PermissionSet {
  id: string;
  workspaceId: string;
  name: string;
  description?: string;
  rules: AccessRules;
  assignedCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface PermissionSetBody {
  name: string;
  description?: string;
  rules: AccessRules;
}

export interface EffectiveObjectAccess {
  actions: ObjectAction[];
  scope: RowScope;
  /** Where each grant comes from, e.g. "Role: Staff", "Permission set: Leads viewer". */
  sources: string[];
  /** False when none of the user's products enables this module (then nothing is allowed). */
  moduleEnabled: boolean;
  /** Restricted fields only (read-only or hidden). */
  fields?: Record<string, 'read' | 'hidden'>;
}

export interface EffectiveAccess {
  objects: Record<string, EffectiveObjectAccess>;
  capabilities: Array<{ key: string; sources: string[] }>;
  products: Array<{ id: string; key: string; name: string }>;
  modules: string[];
}

export interface AccessWorkspaceOption {
  id: string;
  code: string;
  name: string;
  /** The owner's own workspace (platform CRM leads/accounts); members work those records. */
  isPlatform: boolean;
  status: WorkspaceStatus;
  products: Array<{ id: string; key: string; name: string }>;
  permissionSets: Array<{ id: string; name: string }>;
  /** System + custom roles of this workspace, highest rank first. */
  roles: Array<{ key: RoleKey; name: string; isSystem: boolean }>;
}

export interface MembershipAccessBody {
  status?: 'active' | 'suspended';
  roleKey?: RoleKey;
  productIds?: string[];
  permissionSetIds?: string[];
}

/** Give someone a login: pick the workspace, their access, and how they get their password. */
export interface GiveLoginBody {
  workspace:
    | { mode: 'platform' }
    | { mode: 'existing'; workspaceId: string }
    | { mode: 'new'; name: string; code: string; productIds: string[]; timezone?: string; currency?: string };
  roleKey: RoleKey;
  /** Subset of the workspace's products; omitted → all of them. */
  productIds?: string[];
  permissionSetIds?: string[];
  /** invite = email a link (72 h); password = owner sets a temporary password the user must change at first sign-in. */
  method: 'invite' | 'password';
  password?: string;
}

export interface GiveLoginResult {
  identityId: string;
  workspaceId: string;
  membershipId: string;
  invitation?: Invitation;
  /** The person already had a login: they were added to the workspace and keep their current password. */
  existingLogin?: boolean;
}

export interface CreateUserBody extends GiveLoginBody {
  displayName: string;
  email: string;
  phone?: string;
}

export interface CreateUserResult extends GiveLoginResult {
  user: UserDetail;
}

export interface AddMembershipBody {
  workspaceId: string;
  roleKey: RoleKey;
  productIds?: string[];
  permissionSetIds?: string[];
}

// ---- Workspace app (what a signed-in member sees at /crm/w/:ws) ----
export interface WorkspaceContext {
  workspace: { id: string; code: string; name: string; isPlatform: boolean };
  role?: { key: RoleKey; name: string };
  effective: EffectiveAccess;
  navigation: NavItem[];
  /** metadata.manage: may edit page layouts and custom fields. */
  canCustomize: boolean;
  /** Other workspaces the user can switch to. */
  workspaces: Array<{ code: string; name: string }>;
  /** The platform owner opened this workspace from the owner console (full access, audited). */
  viewerIsOwner: boolean;
}

export interface WorkspaceDashboard {
  kpis: Kpi[];
  recent: Array<{ object: ObjectKey; label: string; rows: RelatedList['rows'] }>;
}

// ---- Roles (editable per workspace) & delegated administration ----

/** Capability keys added for delegation. A holder can only grant what they have themselves. */
export type DelegationCapability = 'access.manage' | 'members.manage';

export interface WorkspaceRole {
  id: string;
  workspaceId: string;
  key: RoleKey;
  name: string;
  description?: string;
  /** Built-in role (Super Admin, Admin, Staff, End user): can be edited or reset, not deleted or renamed. */
  isSystem: boolean;
  /** A built-in role whose permissions were changed in this workspace. */
  customized: boolean;
  rank: number;
  /** The role this one reports to; absent only for Super Admin. */
  parentRoleId?: string;
  rules: AccessRules;
  assignedCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface RoleBody {
  name?: string;
  description?: string;
  /** Roles form a hierarchy (D-48): the role this one reports to. */
  parentRoleId?: string;
  /** Ignored by the server: roles no longer grant permissions — permission sets do. */
  rules?: AccessRules;
}

/** What a delegated admin (a member with access.manage / members.manage) may do in their workspace. */
export interface WorkspaceAdminOptions {
  canManageAccess: boolean;
  canManageMembers: boolean;
  /** The most this person can grant — their own effective access as rules. Options beyond it must be disabled. */
  grantable: AccessRules;
  catalog: AccessCatalog;
  roles: WorkspaceRole[];
  permissionSets: PermissionSet[];
  /** Products they may hand out (their own products in this workspace). Empty for the platform team. */
  products: Array<{ id: string; key: string; name: string }>;
  isPlatform: boolean;
}

export interface WorkspaceAdminMember extends WorkspaceMember {
  productIds: string[];
  permissionSets: Array<{ id: string; name: string }>;
  effective: EffectiveAccess;
  isSelf: boolean;
  /** False when their access exceeds yours, it's you, or they're the platform owner. */
  editable: boolean;
  lockedReason?: string;
}

export interface WorkspaceInviteBody {
  displayName: string;
  email: string;
  phone?: string;
  roleKey: RoleKey;
  productIds?: string[];
  permissionSetIds?: string[];
  method: 'invite' | 'password';
  password?: string;
}

// ---- Support tickets (from connected apps, e.g. Business Card Snap / CardFlow) ----
export type TicketStatus = 'open' | 'in_progress' | 'resolved';

export interface SupportTicket {
  id: string;
  subject: string;
  message: string;
  /** billing | card_scan | business_listing | general */
  category: string;
  status: TicketStatus;
  reply?: string;
  repliedAt?: string;
  createdAt: string;
  updatedAt: string;
  user: { name: string; phone: string; role: string; externalId?: string };
  /** CRM records of the person who raised it, when known. */
  account?: LookupValue;
  contact?: LookupValue;
  source: string;
}

export interface TicketCounts {
  all: number;
  open: number;
  in_progress: number;
  resolved: number;
}

// ---- Reports & dashboards (/w/{code}/reports, /dashboards — D-50) ----
export type ReportChart = 'bar' | 'line' | 'donut' | 'number' | 'table';
export type FilterOp = 'eq' | 'neq' | 'contains' | 'gt' | 'gte' | 'lt' | 'lte' | 'empty' | 'notEmpty' | 'lastDays';

export interface ReportFilter {
  field: string;
  op: FilterOp;
  value?: string | number | boolean;
}

export interface ReportDefinition {
  filters: ReportFilter[];
  groupBy?: string;
  dateBucket?: 'day' | 'week' | 'month' | 'quarter' | 'year' | '';
  measure: { fn: 'count' | 'sum' | 'avg' | 'min' | 'max'; field?: string };
  chart: ReportChart;
  limit?: number;
}

export interface ReportSummary {
  id: string;
  name: string;
  description: string;
  object: string;
  objectLabel: string;
  definition: ReportDefinition;
  ownerId?: string;
  ownerName: string;
  canEdit: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ReportResult {
  object: string;
  objectLabel: string;
  groupLabel?: string;
  measureLabel: string;
  chart: ReportChart;
  rows: Array<{ key: string; label: string; value: number; count: number }>;
  total: number;
  count: number;
  currency?: boolean;
}

export interface ReportObject {
  key: string;
  label: string;
  icon?: string;
  fields: FieldDef[];
}

export interface DashboardWidget {
  reportId: string;
  chart?: ReportChart | '';
  size: 'sm' | 'md' | 'lg';
  report?: ReportSummary;
  result?: ReportResult;
  error?: string;
}

export interface DashboardSummary {
  id: string;
  name: string;
  description: string;
  widgets: number;
  ownerName: string;
  canEdit: boolean;
  updatedAt: string;
}

export interface DashboardDetail {
  id: string;
  name: string;
  description: string;
  widgets: DashboardWidget[];
  ownerName: string;
  canEdit: boolean;
  updatedAt: string;
}

// ---- Objects defined as data (/platform/objects, D-45) ----
export interface ObjectFieldDef {
  key: string;
  label: string;
  type: FieldType;
  required?: boolean;
  options?: Array<{ value: string; label: string }>;
  lookup?: string;
  helpText?: string;
}

export interface ObjectDefinition {
  key: string;
  module: string;
  singular: string;
  plural: string;
  description: string;
  icon: string;
  prefix: string;
  nameLabel: string;
  statusLabel?: string;
  statuses: StatusOption[];
  fields: ObjectFieldDef[];
  /** Shipped with the CRM (behind a module) rather than created by the owner. */
  standard: boolean;
  status: 'active' | 'archived';
  records: number;
  createdAt: string;
  updatedAt: string;
}

export type ObjectDefinitionBody = Partial<
  Pick<ObjectDefinition, 'key' | 'singular' | 'plural' | 'description' | 'icon' | 'prefix' | 'nameLabel' | 'statusLabel' | 'statuses' | 'fields'>
>;

export interface ObjectsResponse {
  data: ObjectDefinition[];
  icons: string[];
  lookupTargets: Array<{ key: string; label: string }>;
}

// ---- Connected app data (Business Card Snap): /w/{code}/app/* ----
export type AppPlanId = '3m' | '6m' | '12m' | 'lifetime';

export interface AppUser {
  id: string;
  name: string;
  phone: string;
  email: string;
  city: string;
  state: string;
  /** user | admin (the app's admin console) */
  role: 'user' | 'admin';
  /** pending_profile | active | suspended */
  status: string;
  /** Premium right now (subscribed and not expired). */
  premium: boolean;
  planId?: AppPlanId | string;
  planName?: string;
  /** Absent for lifetime or never granted. */
  expiresAt?: string;
  freeScansLeft: number;
  createdAt: string;
  lastLoginAt?: string;
  businesses: number;
  cards: number;
  openTickets: number;
  account?: LookupValue;
  contact?: LookupValue;
}

export interface AppUserCounts {
  all: number;
  premium: number;
  free: number;
  admin: number;
  suspended: number;
}

export interface SavedCard {
  id: string;
  personName: string;
  designation: string;
  company: string;
  website: string;
  type: 'business' | 'personal' | string;
  gstin: string;
  notes: string;
  phones: string[];
  emails: string[];
  address: string;
  source: string;
  createdAt: string;
  business?: { id: string; name: string };
}

export interface AppPayment {
  planId: string;
  planName: string;
  amountInr: number;
  /** Set for RevenueCat purchases (store currency); empty means INR. */
  currency?: string;
  status: string;
  createdAt: string;
  paidAt?: string;
}

export type BusinessStatus = 'draft' | 'pending_verification' | 'live' | 'under_review' | 'suspended' | 'removed';
export type BusinessVerification = 'pending' | 'gst' | 'pan' | 'tan' | 'manual' | 'failed';
export type BusinessListing = 'listed' | 'unlisted';

export interface AppBusiness {
  id: string;
  name: string;
  slug: string;
  description: string;
  categoryId: string;
  category: string;
  website: string;
  email: string;
  addressLine1: string;
  addressLine2: string;
  locality: string;
  city: string;
  district: string;
  state: string;
  pincode: string;
  gstin: string;
  status: BusinessStatus;
  verification: BusinessVerification;
  listing: BusinessListing;
  phoneVerified: boolean;
  completeness: number;
  services: string[];
  phones: string[];
  /** How many app users saved this business's card. */
  savedBy: number;
  createdAt: string;
  updatedAt: string;
  owner: { id: string; name: string; phone: string };
}

export interface BusinessCounts {
  all: number;
  listed: number;
  hidden: number;
  verified: number;
  review: number;
}

export interface AppUserDetail extends AppUser {
  businessList: AppBusiness[];
  cardList: SavedCard[];
  ticketList: SupportTicket[];
  payments: AppPayment[];
}

export interface AppUserPatch {
  name?: string;
  email?: string;
  city?: string;
  role?: 'user' | 'admin';
  status?: 'active' | 'suspended';
  access?: { action: 'grant'; planId: AppPlanId } | { action: 'revoke' };
}

export type BusinessPatch = Partial<
  Pick<
    AppBusiness,
    'name' | 'description' | 'categoryId' | 'website' | 'email' | 'addressLine1' | 'addressLine2' | 'locality' | 'city' | 'district' | 'state' | 'pincode' | 'gstin' | 'status' | 'verification' | 'listing' | 'services'
  >
> & { ownerPhone?: string };

// ---- Integrations (owner) ----
export interface IntegrationInfo {
  key: string;
  name: string;
  description: string;
  status: 'active' | 'error' | 'disabled';
  workspace?: { id: string; code: string; name: string };
  lastSyncAt?: string;
  lastError?: string;
  /** Seconds between syncs. */
  intervalSeconds: number;
  stats: {
    appUsers: number;
    accounts: number;
    contacts: number;
    leads: number;
    admins: number;
    ticketsOpen: number;
    ticketsTotal: number;
    signInsToday: number;
    newToday: number;
  };
}
