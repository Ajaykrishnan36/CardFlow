// 'integrations' i18n namespace (merged into the crm bundle as integrations.*).
const strings = {
  page: {
    title: 'Integrations',
    description: 'Apps connected to your CRM',
    errorTitle: "Couldn't load integrations",
    emptyTitle: 'No integrations yet',
    emptyBody: 'Connected apps appear here once they are set up on the server.',
    autoRefresh: 'Updates every 10 seconds'
  },
  status: {
    active: 'Active',
    error: 'Error',
    disabled: 'Disabled'
  },
  card: {
    syncing: 'Syncing every {{seconds}}s',
    lastSynced: 'last synced {{time}}',
    neverSynced: 'not synced yet',
    workspace: 'Project',
    noWorkspace: 'No project linked',
    errorTitle: 'The last sync failed',
    disabledNote: 'This integration is turned off. Nothing is synced until it’s enabled again.',
    syncNow: 'Sync now',
    synced: '{{name}} synced.',
    syncFailed: 'Sync finished with an error: {{error}}',
    openCrm: 'Open project CRM',
    settings: 'Project settings',
    statsLabel: '{{name}} numbers'
  },
  stats: {
    appUsers: 'App users',
    newToday: 'Signed up today',
    signInsToday: 'Signed in today',
    accounts: 'Accounts',
    contacts: 'Contacts',
    tickets: 'Open tickets',
    ticketsOf: 'of {{total}}',
    admins: 'Admins'
  },
  how: {
    title: 'How it works',
    description: 'What {{name}} sends to the CRM, and where it shows up.',
    signUpTitle: 'New sign-ups become customers',
    signUpBody: 'A new sign-up in the app (phone + OTP) becomes a lead that’s converted straight away into an account + contact.',
    signInTitle: 'Sign-ins are logged',
    signInBody: 'Each sign-in is logged on the account’s activity.',
    supportTitle: 'Support tickets, answered from the CRM',
    supportBody: 'App support tickets appear under Support in the project, and your replies show in the app.',
    adminsTitle: 'App admins get project access',
    adminsBody: 'App admins get Super Admin access to the project (by invitation).',
    logoutTitle: 'Logouts aren’t tracked yet',
    logoutBody: 'The app doesn’t record logouts, so they can’t be shown.'
  },
  connected: 'Connected: {{name}}'
};

export default strings;
