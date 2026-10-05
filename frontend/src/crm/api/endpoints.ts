import { api, API_BASE, ApiError } from './client';
import type {
  ApiKey, ApiKeysResponse, AppNotification, BulkResult, Campaign, CampaignRecipient, Favorite, FileInfo, FilterNode, ImportResult, MailboxesResponse,
  EmailThread, InviteLinkSettings, RecordGroup, SavedView, SearchGroup, SSOSettings, Team, TimelineItem, Webhook, WebhookDelivery, Workflow, WorkflowDef, WorkflowRun
} from './types-features';
import type {
  DashboardDetail,
  TicketThread,
  DashboardSummary,
  DashboardWidget,
  ReportDefinition,
  ReportObject,
  ReportResult,
  ReportSummary,
  FieldCatalogObject,
  ObjectDefinition,
  ObjectDefinitionBody,
  ObjectsResponse,
  AppBusiness,
  BusinessSaver,
  AppUser,
  AppUserCounts,
  AppUserDetail,
  AppUserPatch,
  BusinessCounts,
  BusinessPatch,
  RecordPage,
  IntegrationInfo,
  SupportTicket,
  TicketCounts,
  TicketStatus,
  RoleBody,
  WorkspaceAdminMember,
  WorkspaceAdminOptions,
  WorkspaceInviteBody,
  WorkspaceRole,
  AccessCatalog,
  AccessWorkspaceOption,
  AddMembershipBody,
  CreateUserBody,
  CreateUserResult,
  GiveLoginBody,
  GiveLoginResult,
  MembershipAccessBody,
  PermissionSet,
  PermissionSetBody,
  WorkspaceContext,
  WorkspaceDashboard,
  AuditEntry,
  AuthStep,
  Capabilities,
  ConvertBody,
  ConvertResult,
  FieldCreateBody,
  FieldUpdateBody,
  Invitation,
  InvitationPreview,
  InviteBody,
  Layout,
  LookupTarget,
  LookupValue,
  ObjectKey,
  ObjectMeta,
  Page,
  ProductCreateBody,
  ProductDetail,
  ProductSummary,
  ProductUpdateBody,
  ProvisionBody,
  ProvisionResult,
  RecordDetail,
  RecordListParams,
  RecordRow,
  UserDetail,
  UserSummary,
  UserUpdateBody,
  WorkspaceDetail,
  WorkspaceSummary,
  WorkspaceUpdateBody,
  Me,
  MfaConfirmation,
  MfaEnrollment,
  NextStep,
  OwnerDashboard,
  SessionInfo
, InstalledApp } from './types';

export interface LoginBody {
  identifier: string;
  password: string;
  audience: 'owner' | 'workspace';
  workspaceCode?: string;
}

export const authApi = {
  login: (body: LoginBody) => api<AuthStep>('/auth/login', { method: 'POST', body }),
  /** Email a 6-digit sign-in code. Always "sent" (never reveals whether the address has an account). */
  requestOtp: (body: { identifier: string; audience: 'owner' | 'workspace' }) =>
    api<{ sent: boolean; expiresIn: number; channel: 'email'; devCode?: string }>('/auth/otp/request', { method: 'POST', body }),
  verifyOtp: (body: { identifier: string; code: string; audience: 'owner' | 'workspace' }) =>
    api<AuthStep>('/auth/otp/verify', { method: 'POST', body }),
  /** Self sign-up (only products whose setup allows it): email a code, then create the account. */
  requestSignup: (body: { product: string; name: string; email: string; invite?: string }) =>
    api<{ sent: boolean; expiresIn: number; devCode?: string }>('/auth/signup/request', { method: 'POST', body }),
  completeSignup: (body: { product: string; name: string; email: string; code: string; password?: string; invite?: string }) =>
    api<AuthStep>('/auth/signup/verify', { method: 'POST', body }),
  /** What a product's invite link joins (D-83). */
  inviteLink: (token: string) => api<{ product: string; name: string; domains: string[] }>(`/auth/join/${enc(token)}`),
  logout: () => api<void>('/auth/logout', { method: 'POST' }),
  logoutAll: () => api<void>('/auth/logout-all', { method: 'POST' }),
  verifyMfa: (code: string) => api<NextStep>('/auth/mfa/verify', { method: 'POST', body: { code } }),
  enrollMfa: () => api<MfaEnrollment>('/auth/mfa/enroll', { method: 'POST', body: {} }),
  confirmMfa: (code: string) => api<MfaConfirmation>('/auth/mfa/confirm', { method: 'POST', body: { code } }),
  forgotPassword: (identifier: string) => api<{ message: string }>('/auth/password/forgot', { method: 'POST', body: { identifier } }),
  resetPassword: (token: string, password: string) =>
    api<{ message: string }>('/auth/password/reset', { method: 'POST', body: { token, password } }),
  changePassword: (currentPassword: string, newPassword: string) =>
    api<NextStep>('/auth/password/change', { method: 'POST', body: { currentPassword, newPassword } })
};

export const meApi = {
  me: () => api<Me>('/me'),
  capabilities: () => api<Capabilities>('/capabilities'),
  sessions: () => api<{ data: SessionInfo[] }>('/me/sessions').then((r) => r.data),
  revokeSession: (id: string) => api<void>(`/me/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
};

function qs(params: object = {}): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : '';
}

const enc = encodeURIComponent;

export const platformApi = {
  dashboard: (params: { product?: string; app?: string } = {}) => api<OwnerDashboard>(`/platform/dashboard${qs(params)}`),
  /** Every app installed in every product (D-73). */
  apps: (params: { product?: string; app?: string } = {}) => api<{ data: InstalledApp[] }>(`/platform/apps${qs(params)}`).then((r) => r.data)
};

export const productsApi = {
  list: (params: { q?: string; status?: string } = {}) => api<Page<ProductSummary>>(`/platform/products${qs(params)}`),
  get: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}`),
  create: (body: ProductCreateBody) => api<ProductDetail>('/platform/products', { method: 'POST', body }),
  update: (id: string, body: ProductUpdateBody) => api<ProductDetail>(`/platform/products/${enc(id)}`, { method: 'PATCH', body }),
  publish: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/publish`, { method: 'POST', body: {} }),
  archive: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/archive`, { method: 'POST', body: {} }),
  restore: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/restore`, { method: 'POST', body: {} })
};

export const workspacesApi = {
  list: (params: { q?: string; status?: string; product?: string; app?: string } = {}) => api<Page<WorkspaceSummary>>(`/platform/workspaces${qs(params)}`),
  get: (id: string) => api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}`),
  /** Idempotent (PRD OWN-02): the same key never creates a second workspace. */
  provision: (body: ProvisionBody, idempotencyKey: string) =>
    api<ProvisionResult>('/platform/workspaces', { method: 'POST', body, headers: { 'Idempotency-Key': idempotencyKey } }),
  update: (id: string, body: WorkspaceUpdateBody) => api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}`, { method: 'PATCH', body }),
  assignProduct: (id: string, productId: string) =>
    api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}/products`, { method: 'POST', body: { productId } }),
  updateProduct: (id: string, productId: string, body: { status?: 'active' | 'suspended'; configVersion?: number }) =>
    api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}/products/${enc(productId)}`, { method: 'PATCH', body }),
  invite: (id: string, body: InviteBody) => api<Invitation>(`/platform/workspaces/${enc(id)}/invitations`, { method: 'POST', body }),
  resendInvite: (id: string, invitationId: string) =>
    api<Invitation>(`/platform/workspaces/${enc(id)}/invitations/${enc(invitationId)}/resend`, { method: 'POST', body: {} }),
  revokeInvite: (id: string, invitationId: string) =>
    api<void>(`/platform/workspaces/${enc(id)}/invitations/${enc(invitationId)}/revoke`, { method: 'POST', body: {} })
};

export const invitationsApi = {
  preview: (token: string) => api<InvitationPreview>(`/invitations/preview${qs({ token })}`),
  accept: (body: { token: string; password: string; displayName?: string }) =>
    api<AuthStep>('/invitations/accept', { method: 'POST', body })
};

export const usersApi = {
  list: (params: { q?: string; status?: string; limit?: number; offset?: number; product?: string; app?: string } = {}) => api<Page<UserSummary>>(`/platform/users${qs(params)}`),
  get: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}`),
  update: (id: string, body: UserUpdateBody) => api<UserDetail>(`/platform/users/${enc(id)}`, { method: 'PATCH', body }),
  sendPasswordReset: (id: string) =>
    api<{ message: string; devResetUrl?: string }>(`/platform/users/${enc(id)}/password-reset`, { method: 'POST', body: {} }),
  revokeSessions: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}/revoke-sessions`, { method: 'POST', body: {} }),
  resetMfa: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}/reset-mfa`, { method: 'POST', body: {} }),
  updateMembership: (id: string, membershipId: string, body: MembershipAccessBody) =>
    api<UserDetail>(`/platform/users/${enc(id)}/memberships/${enc(membershipId)}`, { method: 'PATCH', body }),
  addMembership: (id: string, body: AddMembershipBody) => api<UserDetail>(`/platform/users/${enc(id)}/memberships`, { method: 'POST', body }),
  /** Owner creates a user directly (temporary password or invitation). */
  create: (body: CreateUserBody) => api<CreateUserResult>('/platform/users', { method: 'POST', body })
};

export const accessApi = {
  catalog: () => api<AccessCatalog>('/platform/access/catalog'),
  /** Fields of every object switched on in a workspace (for field access). */
  fieldCatalog: (workspaceId: string) => api<{ data: FieldCatalogObject[] }>(`/platform/workspaces/${enc(workspaceId)}/fields`).then((r) => r.data),
  /** Every workspace (platform first) with its products and permission sets — feeds the pickers. */
  workspaces: () => api<{ data: AccessWorkspaceOption[] }>('/platform/access/workspaces').then((r) => r.data),
  permissionSets: (workspaceId: string) =>
    api<{ data: PermissionSet[] }>(`/platform/workspaces/${enc(workspaceId)}/permission-sets`).then((r) => r.data),
  createPermissionSet: (workspaceId: string, body: PermissionSetBody) =>
    api<PermissionSet>(`/platform/workspaces/${enc(workspaceId)}/permission-sets`, { method: 'POST', body }),
  updatePermissionSet: (id: string, body: Partial<PermissionSetBody>) =>
    api<PermissionSet>(`/platform/permission-sets/${enc(id)}`, { method: 'PATCH', body }),
  deletePermissionSet: (id: string) => api<void>(`/platform/permission-sets/${enc(id)}`, { method: 'DELETE' }),
  roles: (workspaceId: string) => api<{ data: WorkspaceRole[] }>(`/platform/workspaces/${enc(workspaceId)}/roles`).then((r) => r.data),
  createRole: (workspaceId: string, body: RoleBody) =>
    api<WorkspaceRole>(`/platform/workspaces/${enc(workspaceId)}/roles`, { method: 'POST', body }),
  updateRole: (id: string, body: Partial<RoleBody>) => api<WorkspaceRole>(`/platform/roles/${enc(id)}`, { method: 'PATCH', body }),
  /** Custom roles only; 409 role_in_use while assigned. */
  deleteRole: (id: string) => api<void>(`/platform/roles/${enc(id)}`, { method: 'DELETE' }),
  /** Built-in roles only: back to the default permissions. */
  resetRole: (id: string) => api<WorkspaceRole>(`/platform/roles/${enc(id)}/reset`, { method: 'POST', body: {} }),
  /** Give the person behind a lead / account / contact a login (uses the record's name and email). */
  giveLogin: (object: ObjectKey, id: string, body: GiveLoginBody) =>
    api<GiveLoginResult>(`/platform/crm/${object}/${enc(id)}/login`, { method: 'POST', body })
};

/** The member-facing workspace app at /crm/w/:ws. */
export const workspaceApi = {
  context: (code: string, app?: string) => api<WorkspaceContext>(`/w/${enc(code)}/context${qs({ app })}`),
  dashboard: (code: string) => api<WorkspaceDashboard>(`/w/${enc(code)}/dashboard`)
};

/**
 * Record engine client. `prefix` is '/platform' for the owner's Platform CRM or
 * `/w/<code>` for a workspace member (server enforces their permissions).
 */
export function recordsApiFor(prefix: string) {
  return {
  meta: (object: ObjectKey) => api<ObjectMeta>(`${prefix}/crm/meta/${object}`),
  /** Every field, whatever the viewer's field access (page-layout editing). */
  layoutMeta: (object: ObjectKey) => api<ObjectMeta>(`${prefix}/crm/meta/${object}?purpose=layout`),
  saveLayout: (object: ObjectKey, layout: Layout) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/layout`, { method: 'PUT', body: layout }),
  resetLayout: (object: ObjectKey) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/layout/reset`, { method: 'POST', body: {} }),
  createField: (object: ObjectKey, body: FieldCreateBody) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields`, { method: 'POST', body }),
  updateField: (object: ObjectKey, key: string, body: FieldUpdateBody) =>
    api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields/${enc(key)}`, { method: 'PATCH', body }),
  deleteField: (object: ObjectKey, key: string) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields/${enc(key)}`, { method: 'DELETE' }),

  list: (object: ObjectKey, params: RecordListParams = {}) => api<RecordPage>(`${prefix}/crm/${object}${qs(params)}`),
  get: (object: ObjectKey, id: string) => api<RecordDetail>(`${prefix}/crm/${object}/${enc(id)}`),
  create: (object: ObjectKey, values: Record<string, unknown>) => api<RecordRow>(`${prefix}/crm/${object}`, { method: 'POST', body: { values } }),
  update: (object: ObjectKey, id: string, values: Record<string, unknown>, expectedVersion: number) =>
    api<RecordRow>(`${prefix}/crm/${object}/${enc(id)}`, { method: 'PATCH', body: { values, expectedVersion } }),
  remove: (object: ObjectKey, id: string) => api<void>(`${prefix}/crm/${object}/${enc(id)}`, { method: 'DELETE' }),
  convertLead: (id: string, body: ConvertBody, idempotencyKey: string) =>
    api<ConvertResult>(`${prefix}/crm/leads/${enc(id)}/convert`, { method: 'POST', body, headers: { 'Idempotency-Key': idempotencyKey } }),
  lookup: (target: LookupTarget, q = '') => api<{ data: LookupValue[] }>(`${prefix}/lookup/${target}${qs({ q })}`).then((r) => r.data),

  // Lists: grouped boards, bulk actions, recycle bin, CSV, merge.
  groups: (object: ObjectKey, params: RecordListParams & { field: string }) =>
    api<{ field: string; groups: RecordGroup[] }>(`${prefix}/crm/${object}/groups${qs(params)}`),
  bulk: (object: ObjectKey, body: { action: 'update' | 'delete' | 'restore' | 'destroy'; values?: Record<string, unknown>; query: BulkQuery }) =>
    api<BulkResult>(`${prefix}/crm/${object}/bulk`, { method: 'POST', body }),
  restore: (object: ObjectKey, id: string) => api<RecordRow>(`${prefix}/crm/${object}/${enc(id)}/restore`, { method: 'POST', body: {} }),
  exportUrl: (object: ObjectKey, params: RecordListParams & { columns?: string }) => `${API_BASE}${prefix}/crm/${object}/export${qs(params)}`,
  importRows: (object: ObjectKey, body: { rows: string[][]; mapping: string[]; mode: 'create' | 'upsert' | 'update'; matchField?: string; dryRun: boolean }) =>
    api<ImportResult>(`${prefix}/crm/${object}/import`, { method: 'POST', body }),
  duplicates: (object: ObjectKey, id: string) =>
    api<{ data: Array<{ id: string; code?: string; title: string }> }>(`${prefix}/crm/${object}/${enc(id)}/duplicates`).then((r) => r.data),
  merge: (object: ObjectKey, body: { primaryId: string; duplicateIds: string[]; values?: Record<string, unknown> }) =>
    api<RecordRow>(`${prefix}/crm/${object}/merge`, { method: 'POST', body }),

  // Saved views.
  views: (object: ObjectKey) => api<{ data: SavedView[]; canShare: boolean }>(`${prefix}/crm/${object}/views`),
  createView: (object: ObjectKey, body: Partial<Pick<SavedView, 'name' | 'kind' | 'visibility' | 'definition'>>) =>
    api<SavedView>(`${prefix}/crm/${object}/views`, { method: 'POST', body }),
  updateView: (object: ObjectKey, id: string, body: Partial<Pick<SavedView, 'name' | 'kind' | 'visibility' | 'definition' | 'position'>>) =>
    api<SavedView>(`${prefix}/crm/${object}/views/${enc(id)}`, { method: 'PATCH', body }),
  deleteView: (object: ObjectKey, id: string) => api<void>(`${prefix}/crm/${object}/views/${enc(id)}`, { method: 'DELETE' }),

  // Record page: timeline, notes, files, email.
  timeline: (object: ObjectKey, id: string, params: { before?: string; kind?: string; limit?: number } = {}) =>
    api<{ data: TimelineItem[]; next?: string }>(`${prefix}/crm/${object}/${enc(id)}/timeline${qs(params)}`),
  addNote: (object: ObjectKey, id: string, body: { body: string; kind?: 'note' | 'call' }) =>
    api<{ ok: boolean }>(`${prefix}/crm/${object}/${enc(id)}/notes`, { method: 'POST', body }),
  deleteTimelineItem: (itemId: string) => api<void>(`${prefix}/timeline/${enc(itemId)}`, { method: 'DELETE' }),
  files: (object: ObjectKey, id: string) => api<{ data: FileInfo[] }>(`${prefix}/crm/${object}/${enc(id)}/files`).then((r) => r.data),
  uploadFile: (object: ObjectKey, id: string, file: File, field?: string) => uploadForm<FileInfo>(`${prefix}/crm/${object}/${enc(id)}/files`, file, field),
  fileUrl: (fileId: string, inline = false) => `${API_BASE}${prefix}/files/${enc(fileId)}${inline ? '?inline=1' : ''}`,
  deleteFile: (fileId: string) => api<void>(`${prefix}/files/${enc(fileId)}`, { method: 'DELETE' }),
  sendEmail: (object: ObjectKey, id: string, body: { to: string[]; cc?: string[]; subject?: string; body?: string; html?: string; mailboxId?: string; replyTo?: string }) =>
    api<{ id: string; status: string }>(`${prefix}/crm/${object}/${enc(id)}/email`, { method: 'POST', body }),
  emailThreads: (object: ObjectKey, id: string) => api<{ data: EmailThread[] }>(`${prefix}/crm/${object}/${enc(id)}/emails`).then((r) => r.data),
  sendCommunication: (id: string, body: { mailboxId?: string; to?: string[] } = {}) =>
    api<RecordRow>(`${prefix}/crm/communications/${enc(id)}/send`, { method: 'POST', body }),

  // Everywhere: search, favorites, notifications, live stream.
  search: (q: string, limit = 5) => api<{ data: SearchGroup[] }>(`${prefix}/search${qs({ q, limit })}`).then((r) => r.data),
  favorites: () => api<{ data: Favorite[] }>(`${prefix}/favorites`).then((r) => r.data),
  addFavorite: (body: { kind: 'record' | 'view'; object: ObjectKey; targetId: string }) =>
    api<{ data: Favorite[] }>(`${prefix}/favorites`, { method: 'POST', body }).then((r) => r.data),
  removeFavorite: (idOrTarget: string) => api<{ data: Favorite[] }>(`${prefix}/favorites/${enc(idOrTarget)}`, { method: 'DELETE' }).then((r) => r.data),
  notifications: (params: { unread?: '1'; before?: number } = {}) =>
    api<{ data: AppNotification[]; unread: number }>(`${prefix}/notifications${qs(params)}`),
  readNotifications: (body: { ids?: number[]; all?: boolean }) =>
    api<{ data: AppNotification[]; unread: number }>(`${prefix}/notifications/read`, { method: 'POST', body }),
  streamUrl: () => `${API_BASE}${prefix}/stream`
  };
}

export interface BulkQuery {
  ids?: string[];
  filter?: FilterNode;
  q?: string;
  status?: string;
  workspace?: string;
  app?: string;
}

/** multipart upload with the CSRF header (fetch; the JSON client can't send files). */
async function uploadForm<T>(path: string, file: File, field?: string): Promise<T> {
  const fd = new FormData();
  fd.append('file', file);
  if (field) fd.append('field', field);
  const token = document.cookie.split('; ').find((c) => c.startsWith('crm_csrf='))?.split('=')[1] ?? '';
  const res = await fetch(API_BASE + path, { method: 'POST', body: fd, credentials: 'same-origin', headers: { 'X-CSRF-Token': decodeURIComponent(token), Accept: 'application/json' } });
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, data?.code ?? `http_${res.status}`, data?.message ?? 'Upload failed.', data?.fieldErrors ?? {}, data?.requestId);
  return data as T;
}

export const recordsApi = recordsApiFor('/platform');
export type RecordsApi = ReturnType<typeof recordsApiFor>;

export const auditApi = {
  list: (params: { limit?: number; before?: number; entityId?: string } = {}) =>
    api<{ data: AuditEntry[]; nextBefore?: number }>(`/platform/audit${qs(params)}`)
};


/**
 * Delegated administration inside a workspace (/crm/w/:ws/settings/access). The server
 * rejects anything beyond the caller's own access with 403 code `exceeds_your_access`.
 */
export function workspaceAdminApi(code: string) {
  const base = `/w/${enc(code)}/admin`;
  return {
    options: () => api<WorkspaceAdminOptions>(`${base}/options`),
    fieldCatalog: () => api<{ data: FieldCatalogObject[] }>(`/w/${enc(code)}/access/fields`).then((r) => r.data),
    members: () => api<{ data: WorkspaceAdminMember[]; invitations: Invitation[] }>(`${base}/members`),
    invite: (body: WorkspaceInviteBody) =>
      api<{ identityId: string; membershipId: string; invitation?: Invitation; existingLogin?: boolean }>(`${base}/members`, { method: 'POST', body }),
    updateMember: (membershipId: string, body: MembershipAccessBody) =>
      api<WorkspaceAdminMember>(`${base}/members/${enc(membershipId)}`, { method: 'PATCH', body }),
    createRole: (body: RoleBody) => api<WorkspaceRole>(`${base}/roles`, { method: 'POST', body }),
    updateRole: (id: string, body: Partial<RoleBody>) => api<WorkspaceRole>(`${base}/roles/${enc(id)}`, { method: 'PATCH', body }),
    deleteRole: (id: string) => api<void>(`${base}/roles/${enc(id)}`, { method: 'DELETE' }),
    createPermissionSet: (body: PermissionSetBody) => api<PermissionSet>(`${base}/permission-sets`, { method: 'POST', body }),
    updatePermissionSet: (id: string, body: Partial<PermissionSetBody>) =>
      api<PermissionSet>(`${base}/permission-sets/${enc(id)}`, { method: 'PATCH', body }),
    deletePermissionSet: (id: string) => api<void>(`${base}/permission-sets/${enc(id)}`, { method: 'DELETE' })
  };
}
export type WorkspaceAdminApi = ReturnType<typeof workspaceAdminApi>;

/** Support tickets of a workspace connected to an app (members need ticket read / update). */
export function workspaceSupportApi(code: string) {
  const base = `/w/${enc(code)}/support/tickets`;
  return {
    list: (params: { q?: string; status?: TicketStatus | '' } = {}) =>
      api<{ data: SupportTicket[]; counts: TicketCounts }>(`${base}${qs(params)}`),
    get: (id: string) => api<SupportTicket>(`${base}/${enc(id)}`),
    /** Reply and/or change status; the app user sees the reply in the app right away. */
    update: (id: string, body: { status?: TicketStatus; reply?: string }) =>
      api<SupportTicket>(`${base}/${enc(id)}`, { method: 'PATCH', body })
  };
}

/** Reports & dashboards of a project; they always run as the signed-in viewer. */
export function reportsApi(code: string) {
  const base = `/w/${enc(code)}`;
  type ReportBody = { name?: string; description?: string; object?: string; definition?: ReportDefinition };
  type WidgetBody = Pick<DashboardWidget, 'reportId' | 'chart' | 'size'>;
  return {
    objects: () => api<{ data: ReportObject[] }>(`${base}/reports/objects`).then((r) => r.data),
    run: (object: string, definition: ReportDefinition) => api<ReportResult>(`${base}/reports/run`, { method: 'POST', body: { object, definition } }),
    list: () => api<{ data: ReportSummary[] }>(`${base}/reports`).then((r) => r.data),
    get: (id: string) => api<ReportSummary>(`${base}/reports/${enc(id)}`),
    result: (id: string) => api<ReportResult>(`${base}/reports/${enc(id)}/result`),
    create: (body: ReportBody) => api<ReportSummary>(`${base}/reports`, { method: 'POST', body }),
    update: (id: string, body: ReportBody) => api<ReportSummary>(`${base}/reports/${enc(id)}`, { method: 'PATCH', body }),
    remove: (id: string) => api<void>(`${base}/reports/${enc(id)}`, { method: 'DELETE' }),
    dashboards: () => api<{ data: DashboardSummary[] }>(`${base}/dashboards`).then((r) => r.data),
    dashboard: (id: string) => api<DashboardDetail>(`${base}/dashboards/${enc(id)}`),
    createDashboard: (body: { name: string; description?: string; widgets?: WidgetBody[] }) =>
      api<DashboardDetail>(`${base}/dashboards`, { method: 'POST', body }),
    updateDashboard: (id: string, body: { name?: string; description?: string; widgets?: WidgetBody[] }) =>
      api<DashboardDetail>(`${base}/dashboards/${enc(id)}`, { method: 'PATCH', body }),
    removeDashboard: (id: string) => api<void>(`${base}/dashboards/${enc(id)}`, { method: 'DELETE' })
  };
}

/** Objects defined as data: standard objects behind modules and the owner's custom objects. */
/** Object builder API: '/platform' (owner, every object) or '/w/{code}' (a product's own objects, D-79). */
export function objectsApiFor(prefix: string) {
  const base = `${prefix}/objects`;
  return {
    list: () => api<ObjectsResponse>(base),
    get: (key: string) => api<ObjectDefinition>(`${base}/${enc(key)}`),
    create: (body: ObjectDefinitionBody) => api<ObjectDefinition>(base, { method: 'POST', body }),
    update: (key: string, body: ObjectDefinitionBody) => api<ObjectDefinition>(`${base}/${enc(key)}`, { method: 'PATCH', body }),
    archive: (key: string) => api<ObjectDefinition>(`${base}/${enc(key)}/archive`, { method: 'POST', body: {} }),
    restore: (key: string) => api<ObjectDefinition>(`${base}/${enc(key)}/restore`, { method: 'POST', body: {} })
  };
}
export const objectsApi = objectsApiFor('/platform');

/** The connected app's own data (Business Card Snap), edited in place. */
export function workspaceAppApi(code: string) {
  const base = `/w/${enc(code)}/app`;
  return {
    users: (params: { q?: string; filter?: string } = {}) => api<{ data: AppUser[]; counts: AppUserCounts }>(`${base}/users${qs(params)}`),
    user: (id: string) => api<AppUserDetail>(`${base}/users/${enc(id)}`),
    updateUser: (id: string, body: AppUserPatch) => api<AppUserDetail>(`${base}/users/${enc(id)}`, { method: 'PATCH', body }),
    deleteUser: (id: string) => api<void>(`${base}/users/${enc(id)}`, { method: 'DELETE' }),
    businesses: (params: { q?: string; filter?: string } = {}) =>
      api<{ data: AppBusiness[]; counts: BusinessCounts }>(`${base}/businesses${qs(params)}`),
    business: (id: string) => api<AppBusiness>(`${base}/businesses/${enc(id)}`),
    updateBusiness: (id: string, body: BusinessPatch) => api<AppBusiness>(`${base}/businesses/${enc(id)}`, { method: 'PATCH', body }),
    deleteBusiness: (id: string) => api<void>(`${base}/businesses/${enc(id)}`, { method: 'DELETE' }),
    savers: (id: string) => api<{ data: BusinessSaver[] }>(`${base}/businesses/${enc(id)}/savers`).then((r) => r.data),
    /** URL for an <img>; `v` busts the cache after an upload. */
    cardImageUrl: (id: string, side: 'front' | 'back', v?: string) =>
      `${API_BASE}${base}/businesses/${enc(id)}/card-image?side=${side}${v ? `&v=${encodeURIComponent(v)}` : ''}`,
    uploadCardImage: (id: string, side: 'front' | 'back', imageData: string) =>
      api<AppBusiness>(`${base}/businesses/${enc(id)}/card-image`, { method: 'PUT', body: { side, imageData } }),
    categories: () => api<{ data: Array<{ id: string; name: string }> }>(`${base}/categories`).then((r) => r.data),
    ticket: (id: string) => api<TicketThread>(`${base}/tickets/${enc(id)}`),
    replyTicket: (id: string, body: string) => api<TicketThread>(`${base}/tickets/${enc(id)}/messages`, { method: 'POST', body: { body } })
  };
}

export const integrationsApi = {
  list: () => api<{ data: IntegrationInfo[] }>('/platform/integrations').then((r) => r.data),
  sync: (key: string) => api<IntegrationInfo>(`/platform/integrations/${enc(key)}/sync`, { method: 'POST', body: {} })
};
/** Product-level tools inside /w/<code>: developer, workflows, email, campaigns, teams, SSO. */
export function workspaceToolsApi(code: string) {
  const base = `/w/${enc(code)}`;
  return {
    apiKeys: () => api<ApiKeysResponse>(`${base}/developer/api-keys`),
    createApiKey: (body: { name: string; access: 'full' | 'permission_set'; permissionSetId?: string; expiresInDays?: number }) =>
      api<ApiKey>(`${base}/developer/api-keys`, { method: 'POST', body }),
    revokeApiKey: (id: string) => api<void>(`${base}/developer/api-keys/${enc(id)}`, { method: 'DELETE' }),
    webhooks: () => api<{ data: Webhook[]; enabled: boolean; events: string[] }>(`${base}/developer/webhooks`),
    createWebhook: (body: { url: string; description?: string; events: string[]; objects?: string[] }) =>
      api<Webhook>(`${base}/developer/webhooks`, { method: 'POST', body }),
    updateWebhook: (id: string, body: Partial<{ url: string; description: string; events: string[]; objects: string[]; status: 'active' | 'paused' }>) =>
      api<Webhook>(`${base}/developer/webhooks/${enc(id)}`, { method: 'PATCH', body }),
    deleteWebhook: (id: string) => api<void>(`${base}/developer/webhooks/${enc(id)}`, { method: 'DELETE' }),
    rotateWebhook: (id: string) => api<Webhook>(`${base}/developer/webhooks/${enc(id)}/rotate`, { method: 'POST', body: {} }),
    testWebhook: (id: string) => api<{ status: number; ok: boolean; response: string }>(`${base}/developer/webhooks/${enc(id)}/test`, { method: 'POST', body: {} }),
    deliveries: (id: string) => api<{ data: WebhookDelivery[] }>(`${base}/developer/webhooks/${enc(id)}/deliveries`).then((r) => r.data),
    retryDelivery: (id: string, deliveryId: number) =>
      api<{ ok: boolean }>(`${base}/developer/webhooks/${enc(id)}/deliveries/${deliveryId}/retry`, { method: 'POST', body: {} }),
    openapi: () => api<Record<string, unknown>>(`${base}/developer/openapi.json`),
    graphql: (query: string, variables?: Record<string, unknown>) => api<{ data?: unknown; errors?: Array<{ message: string }> }>(`${base}/graphql`, { method: 'POST', body: { query, variables } }),

    workflows: () => api<{ data: Workflow[]; canManage: boolean }>(`${base}/workflows`),
    workflow: (id: string) => api<Workflow>(`${base}/workflows/${enc(id)}`),
    createWorkflow: (body: { name: string; description?: string; draft?: WorkflowDef }) => api<Workflow>(`${base}/workflows`, { method: 'POST', body }),
    updateWorkflow: (id: string, body: { name?: string; description?: string; draft?: WorkflowDef }) =>
      api<Workflow>(`${base}/workflows/${enc(id)}`, { method: 'PATCH', body }),
    deleteWorkflow: (id: string) => api<void>(`${base}/workflows/${enc(id)}`, { method: 'DELETE' }),
    publishWorkflow: (id: string) => api<Workflow>(`${base}/workflows/${enc(id)}/publish`, { method: 'POST', body: {} }),
    setWorkflowStatus: (id: string, status: 'active' | 'inactive') => api<Workflow>(`${base}/workflows/${enc(id)}/status`, { method: 'POST', body: { status } }),
    runWorkflow: (id: string, body: { recordIds?: string[]; input?: Record<string, unknown>; test?: boolean; record?: Record<string, unknown> }) =>
      api<{ runs: string[] }>(`${base}/workflows/${enc(id)}/run`, { method: 'POST', body }),
    workflowVersions: (id: string) =>
      api<{ data: Array<{ version: number; definition: WorkflowDef; publishedBy: string; publishedAt: string }> }>(`${base}/workflows/${enc(id)}/versions`).then((r) => r.data),
    restoreWorkflowVersion: (id: string, version: number) => api<Workflow>(`${base}/workflows/${enc(id)}/versions/${version}/restore`, { method: 'POST', body: {} }),
    runs: (id: string, status?: string) => api<{ data: WorkflowRun[] }>(`${base}/workflows/${enc(id)}/runs${qs({ status })}`).then((r) => r.data),
    stopRun: (id: string, runId: string) => api<WorkflowRun>(`${base}/workflows/${enc(id)}/runs/${enc(runId)}/stop`, { method: 'POST', body: {} }),
    retryRun: (id: string, runId: string) => api<WorkflowRun>(`${base}/workflows/${enc(id)}/runs/${enc(runId)}/retry`, { method: 'POST', body: {} }),

    mailboxes: () => api<MailboxesResponse>(`${base}/mailboxes`),
    connectMailbox: (provider: 'google' | 'microsoft') => api<{ url: string }>(`${base}/mailboxes/connect/${provider}`),
    connectImap: (body: { email: string; name?: string; imapHost: string; imapPort?: number; smtpHost?: string; smtpPort?: number; username?: string; password: string }) =>
      api<MailboxesResponse>(`${base}/mailboxes/imap`, { method: 'POST', body }),
    updateMailbox: (id: string, body: Partial<{ syncEmail: boolean; syncCalendar: boolean; visibility: string; autoCreateContacts: boolean; paused: boolean }>) =>
      api<MailboxesResponse>(`${base}/mailboxes/${enc(id)}`, { method: 'PATCH', body }),
    deleteMailbox: (id: string) => api<void>(`${base}/mailboxes/${enc(id)}`, { method: 'DELETE' }),
    syncMailbox: (id: string) => api<{ emails: number; events: number; contactsCreated: number; error?: string }>(`${base}/mailboxes/${enc(id)}/sync`, { method: 'POST', body: {} }),
    blocklist: (body: { add?: string; remove?: string }) => api<MailboxesResponse>(`${base}/mailboxes/blocklist`, { method: 'POST', body }),

    campaigns: () => api<{ data: Campaign[]; sentToday: number; dailyLimit: number; senderReady: boolean }>(`${base}/campaigns`),
    campaign: (id: string) => api<Campaign>(`${base}/campaigns/${enc(id)}`),
    createCampaign: (body: Partial<Campaign>) => api<Campaign>(`${base}/campaigns`, { method: 'POST', body }),
    updateCampaign: (id: string, body: Partial<Campaign> & { clearFilter?: boolean }) => api<Campaign>(`${base}/campaigns/${enc(id)}`, { method: 'PATCH', body }),
    deleteCampaign: (id: string) => api<void>(`${base}/campaigns/${enc(id)}`, { method: 'DELETE' }),
    audience: (id: string) => api<{ total: number; sample: Array<{ id: string; title: string; email: string }>; unsubscribed: number }>(`${base}/campaigns/${enc(id)}/audience`),
    recipients: (id: string, status?: string) => api<{ data: CampaignRecipient[] }>(`${base}/campaigns/${enc(id)}/recipients${qs({ status })}`).then((r) => r.data),
    testCampaign: (id: string, to: string) => api<{ ok: boolean }>(`${base}/campaigns/${enc(id)}/test`, { method: 'POST', body: { to } }),
    sendCampaign: (id: string, at?: string) => api<Campaign>(`${base}/campaigns/${enc(id)}/send`, { method: 'POST', body: at ? { at } : {} }),
    cancelCampaign: (id: string) => api<Campaign>(`${base}/campaigns/${enc(id)}/cancel`, { method: 'POST', body: {} }),

    teams: () => api<{ data: Team[]; canManage: boolean }>(`${base}/teams`),
    createTeam: (body: { name: string; description?: string; members?: string[] }) => api<{ data: Team[] }>(`${base}/teams`, { method: 'POST', body }),
    updateTeam: (id: string, body: { name?: string; description?: string; members?: string[] }) =>
      api<{ data: Team[] }>(`${base}/teams/${enc(id)}`, { method: 'PATCH', body }),
    deleteTeam: (id: string) => api<void>(`${base}/teams/${enc(id)}`, { method: 'DELETE' }),

    sso: () => api<SSOSettings>(`${base}/sso`),
    saveSso: (body: { kind: 'saml' | 'oidc'; oidcIssuer?: string; oidcClientId?: string; oidcClientSecret?: string; enabled: boolean; name?: string; idpMetadataXml?: string; idpMetadataUrl?: string; domains: string[]; jitProvisioning: boolean; defaultRoleKey: string }) =>
      api<SSOSettings>(`${base}/sso`, { method: 'PUT', body }),
    deleteSso: () => api<void>(`${base}/sso`, { method: 'DELETE' }),
    inviteLink: () => api<InviteLinkSettings>(`${base}/invite-link`),
    /** Recycle bin retention (D-84): null keeps deleted records until someone removes them. */
    binSettings: () => api<{ retentionDays: number | null }>(`${base}/recycle-bin`),
    saveBinSettings: (body: { retentionDays: number | null }) => api<{ retentionDays: number | null }>(`${base}/recycle-bin`, { method: 'PUT', body }),
    saveInviteLink: (body: { enabled: boolean; domains: string[]; roleKey: string; regenerate?: boolean }) =>
      api<InviteLinkSettings>(`${base}/invite-link`, { method: 'PUT', body })
  };
}
export type WorkspaceToolsApi = ReturnType<typeof workspaceToolsApi>;

export const signInApi = {
  methods: (product?: string) => api<{ methods: string[]; product: string; signup?: boolean }>(`/auth/methods${qs({ product })}`),
  providers: () => api<Record<'google' | 'microsoft' | 'linkedin', boolean>>('/oauth/providers'),
  oauthStartUrl: (provider: string, params: { product?: string; audience?: 'owner' | 'workspace' }) => `${API_BASE}/auth/oauth/${provider}/start${qs(params)}`,
  ssoStartUrl: (code: string) => `${API_BASE}/auth/sso/${enc(code)}/start`
};

// ---- Unified sign-in, businesses, cards and the dashboard summary (D-93…D-98) ----

export interface BusinessSummary {
  id: string;
  code: string;
  name: string;
  roleKey: string;
  roleName: string;
  isPlatform: boolean;
  currency: string;
  timezone: string;
  origin: string;
  profile: Record<string, string>;
  members: number;
  createdAt: string;
}

export interface BusinessProfile {
  code: string;
  name: string;
  currency: string;
  timezone: string;
  locale: string;
  origin: string;
  profile: Record<string, string>;
  members: number;
  createdAt: string;
  canEdit: boolean;
}

export interface BusinessDashboardSummary {
  range: { key: string; from?: string; to?: string; timezone: string };
  metrics: Record<string, number>;
  finance?: { currency: string; income: number; expenses: number; net: number };
  lists: Array<{ key?: string; object: string; label: string; rows: Array<{ id: string; code?: string; title: string; subtitle?: string; status?: string }> }>;
  currency: string;
  can: Record<string, boolean>;
}

export interface CardFields {
  personName?: string;
  designation?: string;
  company?: string;
  website?: string;
  phones?: string[];
  emails?: string[];
  address?: string;
  gstin?: string;
  notes?: string;
  metContext?: string;
}

export interface CardLink {
  id: string;
  object: string;
  recordId: string;
  code: string;
  title: string;
}

export interface SavedCard extends Required<Omit<CardFields, 'phones' | 'emails'>> {
  id: string;
  phones: string[];
  emails: string[];
  eventTag: string;
  hasImage: boolean;
  hasBack: boolean;
  mine: boolean;
  savedBy?: string;
  createdAt: string;
  links: CardLink[];
}

export interface CardMatch {
  object: string;
  id: string;
  code: string;
  title: string;
  subtitle?: string;
  status?: string;
  matchedOn: string[];
}

export interface CardMatches {
  matches: CardMatch[];
  hidden: number;
  suggested: 'lead' | 'contact' | 'attach';
  can: Record<string, boolean>;
}

export interface SaveCardBody {
  cardId?: string;
  card?: CardFields;
  action: 'lead' | 'contact' | 'attach' | 'card_only';
  target?: { object: string; id: string };
  createAccount?: boolean;
  allowDuplicate?: boolean;
  followUpAt?: string;
  note?: string;
}

export const phoneAuthApi = {
  /** Text a 6-digit code. `devCode` is present only where the server runs without an SMS provider. */
  request: (phone: string) => api<{ sent: boolean; expiresIn: number; channel: string; devCode?: string }>('/auth/phone/request', { method: 'POST', body: { phone } }),
  verify: (body: { phone: string; code: string; name?: string }) => api<AuthStep & { hasBusiness: boolean; isNewUser?: boolean }>('/auth/phone/verify', { method: 'POST', body })
};

export const businessApi = {
  list: () => api<{ data: BusinessSummary[]; canCreate: boolean; limit: number; created: number }>('/businesses'),
  create: (body: { name: string; industry?: string; phone?: string; email?: string; website?: string; city?: string; state?: string; country?: string }) =>
    api<{ business: BusinessSummary; next: string }>('/businesses', { method: 'POST', body }),
  profile: (code: string) => api<BusinessProfile>(`/w/${enc(code)}/business`),
  update: (code: string, body: { name?: string; currency?: string; timezone?: string; profile?: Record<string, string | null> }) =>
    api<BusinessProfile>(`/w/${enc(code)}/business`, { method: 'PATCH', body }),
  summary: (code: string, params: { range: string; from?: string; to?: string }) => api<BusinessDashboardSummary>(`/w/${enc(code)}/dashboard/summary${qs(params)}`)
};

export const cardsApi = {
  list: (code: string, params: { q?: string; object?: string; recordId?: string } = {}) => api<{ items: SavedCard[]; total: number }>(`/w/${enc(code)}/cards${qs(params)}`),
  get: (code: string, id: string) => api<SavedCard>(`/w/${enc(code)}/cards/${enc(id)}`),
  match: (code: string, card: CardFields) => api<CardMatches>(`/w/${enc(code)}/cards/match`, { method: 'POST', body: card }),
  save: (code: string, body: SaveCardBody) =>
    api<{ card: SavedCard; record?: CardLink; account?: CardLink; created: boolean; task?: { id: string; code: string; title: string } }>(`/w/${enc(code)}/cards`, { method: 'POST', body }),
  unlink: (code: string, cardId: string, linkId: string) => api<void>(`/w/${enc(code)}/cards/${enc(cardId)}/links/${enc(linkId)}`, { method: 'DELETE' }),
  imageUrl: (code: string, id: string, side: 'front' | 'back' = 'front') => `${API_BASE}/w/${enc(code)}/cards/${enc(id)}/image${side === 'back' ? '?side=back' : ''}`
};

// ---- Enterprise model (D-111…D-117): relationships, payments, SLA, contracts, forecasts ----

export interface RelationshipType {
  key: string;
  label: string;
  inverseLabel: string;
  sourceObject?: string;
  targetObject?: string;
  cardinality: 'one_to_one' | 'one_to_many' | 'many_to_one' | 'many_to_many';
  system: boolean;
}

export interface RecordRelationship {
  id: string;
  type: string;
  label: string;
  direction: 'in' | 'out';
  object: string;
  recordId: string;
  code: string;
  title: string;
  note?: string;
  createdAt: string;
}

export interface PaymentAllocation {
  id: string;
  invoiceId: string;
  invoice: string;
  invoiceCode: string;
  amount: number;
  createdAt: string;
}

export interface InvoiceLedger {
  total: number;
  paid: number;
  balance: number;
  status: string;
  currency: string;
  invoice: string;
  canRecordPayment: boolean;
  canSeePayments: boolean;
  payments: Array<{ id: string; code: string; name: string; status: string; date: string; method: string; amount: number; applied: number; refunded: number; counts: boolean }>;
}

export interface SlaTimer {
  milestone: 'first_response' | 'resolution';
  targetMinutes: number;
  startedAt: string;
  dueAt: string;
  pausedAt?: string;
  completedAt?: string;
  breachedAt?: string;
  state: 'running' | 'paused' | 'met' | 'breached' | 'missed';
  elapsedMinutes: number;
  remainingMinutes: number;
  businessHours: boolean;
}

export interface ForecastPeriod {
  key: string;
  kind: 'month' | 'quarter' | 'year';
  label: string;
  from: string;
  to: string;
}

export interface ForecastNumbers {
  quota: number;
  closed: number;
  commit: number;
  bestCase: number;
  pipeline: number;
  omitted: number;
  lost: number;
  weighted: number;
  forecast: number;
  bestCaseTotal: number;
  openPipeline: number;
  gap: number;
  attainment: number;
  coverage: number;
  openDeals: number;
  wonDeals: number;
}

export interface ForecastSubmission {
  id: string;
  status: 'submitted' | 'approved' | 'rejected';
  forecastAmount: number;
  comment?: string;
  overrideAmount?: number;
  managerComment?: string;
  submittedAt: string;
  approvedAt?: string;
}

export interface ForecastRow extends ForecastNumbers {
  id: string;
  name: string;
  members?: number;
  submission?: ForecastSubmission;
}

export interface Forecast {
  period: ForecastPeriod;
  groupBy: 'owner' | 'team' | 'territory' | 'role';
  pipeline: string;
  currency: string;
  totals: ForecastNumbers;
  rows: ForecastRow[];
  canManage: boolean;
  me: string;
  scope: 'company' | 'team';
}

export function enterpriseApi(code: string) {
  const base = `/w/${enc(code)}`;
  return {
    relationshipTypes: () => api<{ data: RelationshipType[] }>(`${base}/relationship-types`).then((r) => r.data),
    relationships: (object: string, id: string) => api<{ data: RecordRelationship[]; canEdit: boolean }>(`${base}/crm/${enc(object)}/${enc(id)}/relationships`),
    addRelationship: (object: string, id: string, body: { type: string; targetObject: string; targetId: string; note?: string }) =>
      api<{ data: RecordRelationship[] }>(`${base}/crm/${enc(object)}/${enc(id)}/relationships`, { method: 'POST', body }),
    removeRelationship: (relationshipId: string) => api<void>(`${base}/relationships/${enc(relationshipId)}`, { method: 'DELETE' }),

    invoiceLedger: (invoiceId: string) => api<InvoiceLedger>(`${base}/invoices/${enc(invoiceId)}/payments`),
    allocations: (paymentId: string) => api<{ data: PaymentAllocation[] }>(`${base}/payments/${enc(paymentId)}/allocations`).then((r) => r.data),
    allocate: (paymentId: string, body: { invoiceId: string; amount: number }) =>
      api<{ data: PaymentAllocation[] }>(`${base}/payments/${enc(paymentId)}/allocations`, { method: 'POST', body }),
    unallocate: (paymentId: string, allocationId: string) => api<void>(`${base}/payments/${enc(paymentId)}/allocations/${enc(allocationId)}`, { method: 'DELETE' }),
    refund: (paymentId: string, body: { amount: number; reason?: string }) => api<unknown>(`${base}/payments/${enc(paymentId)}/refund`, { method: 'POST', body }),

    caseSla: (caseId: string) => api<{ policy?: { id: string; label: string }; timers: SlaTimer[] }>(`${base}/cases/${enc(caseId)}/sla`),
    renewContract: (contractId: string, body: { startDate?: string; endDate?: string; contractValue?: number }) =>
      api<{ id: string }>(`${base}/contracts/${enc(contractId)}/renew`, { method: 'POST', body }),

    forecastPeriods: () => api<{ data: ForecastPeriod[]; current: { month: string; quarter: string; year: string }; fiscalStartMonth: number }>(`${base}/forecast/periods`),
    forecast: (params: { period: string; groupBy?: string }) => api<Forecast>(`${base}/forecast${qs(params)}`),
    setQuota: (body: { periodKey: string; scope: 'company' | 'team' | 'user'; ownerId?: string; teamId?: string; amount: number }) =>
      api<{ ok: boolean }>(`${base}/forecast/quotas`, { method: 'PUT', body }),
    submitForecast: (body: { periodKey: string; forecastAmount?: number; comment?: string }) =>
      api<{ id: string; forecastAmount: number }>(`${base}/forecast/submit`, { method: 'POST', body }),
    reviewForecast: (submissionId: string, body: { status: 'approved' | 'rejected'; overrideAmount?: number; comment?: string }) =>
      api<{ ok: boolean }>(`${base}/forecast/submissions/${enc(submissionId)}/review`, { method: 'POST', body }),
    setFiscalStart: (fiscalStartMonth: number) => api<{ fiscalStartMonth: number }>(`${base}/forecast/settings`, { method: 'PUT', body: { fiscalStartMonth } })
  };
}

// ---- Commercial, sales-execution and service model (D-118…D-127) ----

export interface PriceBookEntry {
  id: string;
  priceBookId: string;
  priceBook?: string;
  itemId: string;
  item?: string;
  sku?: string;
  currency: string;
  listPrice: number;
  unitPrice: number;
  cost?: number;
  maxDiscountPercent?: string;
  minQuantity: string;
  maxQuantity?: string;
  validFrom: string;
  validTo?: string;
  isActive: boolean;
  version: number;
}

export interface BundleComponent {
  itemId: string;
  item?: string;
  required: boolean;
  quantity: number;
  minQuantity?: string;
  maxQuantity?: string;
  sequence?: number;
  priceMode: 'included' | 'additional';
}

export interface PricingRule {
  id: string;
  name: string;
  kind: 'price' | 'discount' | 'eligibility' | 'configuration';
  priority: number;
  condition: {
    itemIds?: string[];
    family?: string;
    itemType?: string;
    minQuantity?: number;
    maxQuantity?: number;
    accountIds?: string[];
    accountTypes?: string[];
    territoryIds?: string[];
    priceBookIds?: string[];
    withItemIds?: string[];
    currency?: string;
  };
  action: { type?: string; value?: number; requires?: string[]; excludes?: string[]; accountTypes?: string[]; territoryIds?: string[]; message?: string };
  effectiveFrom?: string;
  effectiveTo?: string;
  isActive: boolean;
}

export interface PriceLineInput {
  itemId?: string;
  name?: string;
  quantity: number;
  discountPercent?: number;
  unitPrice?: number;
  options?: Array<{ itemId: string; quantity?: number }>;
  startDate?: string;
  endDate?: string;
  billingFrequency?: string;
}

export interface PricedLine {
  itemId?: string;
  name: string;
  sku?: string;
  quantity: number;
  listPrice: number;
  unitPrice: number;
  discountPercent: number;
  discountAmount: number;
  taxRate: number;
  taxAmount: number;
  total: number;
  priceBookEntryId?: string;
  pricingNote: string;
  bundleOf?: number;
}

export interface PriceResult {
  priceBookId?: string;
  priceBook?: string;
  currency: string;
  lines: PricedLine[];
  subtotal: number;
  discount: number;
  tax: number;
  total: number;
  maxDiscountPercent: number;
  approval: { required: boolean; kind: string; value: number; approverRole?: string; label?: string };
}

export interface ApprovalRequest {
  id: string;
  kind: string;
  object: string;
  recordId: string;
  record: string;
  value: number;
  approverRole: string;
  reason: string;
  status: 'pending' | 'approved' | 'rejected' | 'cancelled';
  requestedBy: string;
  decidedBy?: string;
  note?: string;
  createdAt: string;
  decidedAt?: string;
  canDecide: boolean;
}

export interface DocumentLines {
  object: string;
  id: string;
  status: string;
  editable: boolean;
  pricing: PriceResult;
  approvalRequest?: ApprovalRequest;
}

export interface ApprovalRule {
  kind: string;
  minValue: number;
  approverRole: string;
  label: string;
}

export interface ExchangeRate {
  id: string;
  fromCurrency: string;
  toCurrency: string;
  rate: string;
  effectiveFrom: string;
  effectiveTo?: string;
  source: string;
}

export interface ContactRole {
  id: string;
  contactId: string;
  contact: string;
  email?: string;
  phone?: string;
  object: string;
  recordId: string;
  record: string;
  role: string;
  isPrimary: boolean;
  isActive: boolean;
  startDate?: string;
  endDate?: string;
  note?: string;
}

export interface RecordTeamMember {
  identityId: string;
  name: string;
  email?: string;
  teamRole: string;
  accessLevel: 'read' | 'write' | 'full';
  isPrimary: boolean;
}

export interface TerritoryNode {
  id: string;
  code: string;
  name: string;
  parentId?: string;
  type?: string;
  status?: string;
  manager?: string;
  depth: number;
  accounts: number;
  people: number;
  teams: number;
}

export interface TerritoryAssignment {
  id: string;
  territoryId: string;
  territory?: string;
  kind: 'account' | 'user' | 'team';
  targetId: string;
  target: string;
  isPrimary: boolean;
  effectiveFrom: string;
  effectiveTo?: string;
}

export interface CampaignMember {
  id: string;
  campaignId: string;
  campaign?: string;
  object: 'leads' | 'contacts';
  recordId: string;
  name: string;
  email?: string;
  status: string;
  source: string;
  responded: boolean;
  firstTouchAt: string;
  lastTouchAt: string;
}

export interface CampaignAttribution {
  id: string;
  name: string;
  type: string;
  status: string;
  members: number;
  responded: number;
  leads: number;
  contacts: number;
  convertedLeads: number;
  opportunities: number;
  wonDeals: number;
  pipeline: number;
  wonRevenue: number;
  actualCost?: number;
  budgetedCost?: number;
  expectedRevenue?: number;
  conversionRate: number;
  roiPercent?: number;
  costPerLead?: number;
}

export interface CreditAllocation {
  id: string;
  invoiceId: string;
  invoice: string;
  invoiceCode: string;
  amount: number;
}

export interface Slot {
  resourceId: string;
  resource: string;
  start: string;
  end: string;
}

export interface EntitlementUsage {
  data: Array<{ kind: 'case' | 'hours'; quantity: number; object: string; recordId: string; note: string; at: string }>;
  cases: { included: number; used: number; remaining?: number };
  hours: { included: number; used: number; remaining?: number };
  overagePolicy: string;
}

export interface AccountStatement {
  currency: string;
  invoiced: number;
  outstanding: number;
  unappliedPayments: number;
  unusedCredit: number;
  netReceivable: number;
  lines: Array<{ kind: 'invoice' | 'payment' | 'credit_note'; id: string; code: string; name: string; status: string; date: string; amount: number; open: number }>;
}

export function commerceApi(code: string) {
  const base = `/w/${enc(code)}`;
  const rec = (object: string, id: string) => `${base}/crm/${enc(object)}/${enc(id)}`;
  return {
    // pricing
    entries: (params: { priceBookId?: string; itemId?: string }) => api<{ data: PriceBookEntry[]; canManage: boolean; base: string }>(`${base}/price-book-entries${qs(params)}`),
    saveEntry: (body: Record<string, unknown>, id?: string) =>
      api<PriceBookEntry>(id ? `${base}/price-book-entries/${enc(id)}` : `${base}/price-book-entries`, { method: id ? 'PATCH' : 'POST', body }),
    deleteEntry: (id: string) => api<void>(`${base}/price-book-entries/${enc(id)}`, { method: 'DELETE' }),
    bundle: (itemId: string) => api<{ data: BundleComponent[]; canManage: boolean }>(`${base}/catalog_items/${enc(itemId)}/bundle`),
    saveBundle: (itemId: string, components: BundleComponent[]) =>
      api<{ data: BundleComponent[] }>(`${base}/catalog_items/${enc(itemId)}/bundle`, { method: 'PUT', body: { components } }),
    rules: () => api<{ data: PricingRule[]; canManage: boolean }>(`${base}/pricing/rules`),
    saveRule: (body: Partial<PricingRule>, id?: string) =>
      api<{ data: PricingRule[] }>(id ? `${base}/pricing/rules/${enc(id)}` : `${base}/pricing/rules`, { method: id ? 'PATCH' : 'POST', body }),
    deleteRule: (id: string) => api<void>(`${base}/pricing/rules/${enc(id)}`, { method: 'DELETE' }),
    preview: (body: { priceBookId?: string; currency?: string; accountId?: string; date?: string; lines: PriceLineInput[] }) =>
      api<PriceResult>(`${base}/pricing/preview`, { method: 'POST', body }),
    lines: (object: string, id: string) => api<DocumentLines>(`${base}/${enc(object)}/${enc(id)}/lines`),
    putLines: (object: string, id: string, body: { priceBookId?: string; currency?: string; lines: PriceLineInput[] }) =>
      api<DocumentLines>(`${base}/${enc(object)}/${enc(id)}/lines`, { method: 'PUT', body }),
    reviseQuote: (id: string) => api<{ object: string; id: string; code: string }>(`${base}/quotes/${enc(id)}/revise`, { method: 'POST' }),
    convertQuote: (id: string) => api<{ object: string; id: string; code: string }>(`${base}/quotes/${enc(id)}/convert`, { method: 'POST' }),
    invoiceOrder: (id: string) => api<{ object: string; id: string; code: string }>(`${base}/sales_orders/${enc(id)}/invoice`, { method: 'POST' }),

    // approvals
    approvalRules: () =>
      api<{ data: ApprovalRule[]; kinds: Record<string, string>; roles: Array<{ key: string; name: string }>; canManage: boolean }>(`${base}/approvals/rules`),
    saveApprovalRules: (kind: string, rules: ApprovalRule[]) => api<{ data: ApprovalRule[] }>(`${base}/approvals/rules`, { method: 'PUT', body: { kind, rules } }),
    approvals: (status = 'pending') => api<{ data: ApprovalRequest[]; kinds: Record<string, string> }>(`${base}/approvals${qs({ status })}`),
    decide: (id: string, status: 'approved' | 'rejected', note?: string) => api<{ ok: boolean }>(`${base}/approvals/${enc(id)}/decide`, { method: 'POST', body: { status, note } }),

    // currencies
    currencies: () => api<{ data: Array<{ code: string; name: string; symbol: string }>; base: string; canManage: boolean }>(`${base}/currencies`),
    rates: () => api<{ data: ExchangeRate[]; base: string; canManage: boolean }>(`${base}/exchange-rates`),
    saveRate: (body: { fromCurrency: string; rate: number; effectiveFrom?: string }) => api<{ data: ExchangeRate[] }>(`${base}/exchange-rates`, { method: 'PUT', body }),
    deleteRate: (id: string) => api<void>(`${base}/exchange-rates/${enc(id)}`, { method: 'DELETE' }),

    // finance
    creditAllocations: (id: string) => api<{ data: CreditAllocation[]; canEdit: boolean }>(`${base}/credit_notes/${enc(id)}/allocations`),
    applyCredit: (id: string, body: { invoiceId: string; amount: number }) => api<{ data: CreditAllocation[] }>(`${base}/credit_notes/${enc(id)}/apply`, { method: 'POST', body }),
    unapplyCredit: (id: string, allocationId: string) => api<void>(`${base}/credit_notes/${enc(id)}/allocations/${enc(allocationId)}`, { method: 'DELETE' }),
    statement: (accountId: string) => api<AccountStatement>(`${base}/accounts/${enc(accountId)}/statement`),

    // people on a record
    contactRoles: (object: string, id: string) => api<{ data: ContactRole[]; roles: string[]; canEdit: boolean }>(`${rec(object, id)}/contact-roles`),
    addContactRole: (object: string, id: string, body: { contactId: string; role: string; isPrimary?: boolean; note?: string }) =>
      api<{ data: ContactRole[] }>(`${rec(object, id)}/contact-roles`, { method: 'POST', body }),
    updateContactRole: (roleId: string, body: Partial<Pick<ContactRole, 'role' | 'isPrimary' | 'isActive' | 'endDate'>>) =>
      api<{ data: ContactRole[] }>(`${base}/contact-roles/${enc(roleId)}`, { method: 'PATCH', body }),
    deleteContactRole: (roleId: string) => api<void>(`${base}/contact-roles/${enc(roleId)}`, { method: 'DELETE' }),
    team: (object: string, id: string) => api<{ data: RecordTeamMember[]; roles: string[]; ownerId: string; canEdit: boolean }>(`${rec(object, id)}/team`),
    saveTeamMember: (object: string, id: string, body: { identityId: string; teamRole: string; accessLevel: string; isPrimary?: boolean }) =>
      api<{ data: RecordTeamMember[] }>(`${rec(object, id)}/team`, { method: 'PUT', body }),
    removeTeamMember: (object: string, id: string, identityId: string) => api<void>(`${rec(object, id)}/team/${enc(identityId)}`, { method: 'DELETE' }),

    // territories
    territoryTree: () => api<{ data: TerritoryNode[]; canManage: boolean }>(`${base}/territories/tree`),
    territoryAssignments: (id: string, history = false) =>
      api<{ data: TerritoryAssignment[]; canManage: boolean }>(`${base}/territories/${enc(id)}/assignments${qs({ history: history ? 1 : undefined })}`),
    assignTerritory: (id: string, body: { kind: string; accountId?: string; identityId?: string; teamId?: string; isPrimary?: boolean }) =>
      api<{ data: TerritoryAssignment[] }>(`${base}/territories/${enc(id)}/assignments`, { method: 'POST', body }),
    endAssignment: (id: string) => api<void>(`${base}/territory-assignments/${enc(id)}`, { method: 'DELETE' }),
    accountTerritories: (accountId: string) => api<{ data: TerritoryAssignment[]; canManage: boolean }>(`${rec('accounts', accountId)}/territories`),

    // campaigns
    attribution: (model: string) =>
      api<{ model: string; data: CampaignAttribution[]; currency: string; totals: { wonRevenue: number; pipeline: number; deals: number }; canManage: boolean; models: Array<{ key: string; label: string }> }>(
        `${base}/campaigns/attribution${qs({ model })}`
      ),
    campaignMembers: (campaignId: string) => api<{ data: CampaignMember[]; statuses: string[] }>(`${base}/campaigns/${enc(campaignId)}/members`),
    addCampaignMember: (campaignId: string, body: { leadId?: string; contactId?: string; status?: string; source?: string }) =>
      api<{ data: CampaignMember[] }>(`${base}/campaigns/${enc(campaignId)}/members`, { method: 'POST', body }),
    updateCampaignMember: (id: string, body: { status?: string; responded?: boolean }) => api<{ ok: boolean }>(`${base}/campaign-members/${enc(id)}`, { method: 'PATCH', body }),
    removeCampaignMember: (id: string) => api<void>(`${base}/campaign-members/${enc(id)}`, { method: 'DELETE' }),
    campaignCosts: (campaignId: string, body: { campaignType?: string; budgetedCost?: number; actualCost?: number; expectedRevenue?: number }) =>
      api<{ ok: boolean }>(`${base}/campaigns/${enc(campaignId)}/costs`, { method: 'PATCH', body }),
    recordCampaigns: (object: string, id: string) => api<{ data: CampaignMember[] }>(`${rec(object, id)}/campaigns`),

    // field service
    slots: (params: { serviceId?: string; date: string; days?: number; resourceId?: string; durationMinutes?: number }) =>
      api<{ data: Slot[]; durationMinutes: number }>(`${base}/scheduling/slots${qs(params)}`),
    book: (body: { serviceId?: string; resourceId?: string; start: string; accountId?: string; contactId?: string; workOrderId?: string; caseId?: string; name?: string; durationMinutes?: number }) =>
      api<RecordRow>(`${base}/scheduling/book`, { method: 'POST', body }),
    reschedule: (appointmentId: string, body: { start: string; resourceId?: string }) => api<RecordRow>(`${base}/appointments/${enc(appointmentId)}/reschedule`, { method: 'POST', body }),
    caseWorkOrder: (caseId: string) => api<RecordRow>(`${base}/cases/${enc(caseId)}/work-order`, { method: 'POST' }),
    invoiceWorkOrder: (id: string) => api<{ object: string; id: string; code: string }>(`${base}/work_orders/${enc(id)}/invoice`, { method: 'POST' }),
    entitlementUsage: (id: string) => api<EntitlementUsage>(`${base}/entitlements/${enc(id)}/usage`),
    forecastHistory: (period: string) =>
      api<{ events: Array<{ owner: string; event: string; forecastAmount: number; overrideAmount?: number; comment?: string; actor?: string; at: string }>; accuracy: Array<{ ownerId: string; submitted: number; closedWon: number; accuracyPercent?: number }> }>(
        `${base}/forecast/history${qs({ period })}`
      )
  };
}
