// 'integrations' i18n namespace (merged into the crm bundle as integrations.*).
const strings = {
  providers: {
    title: 'Sign-in & mailbox providers',
    body: 'Connect Google, Microsoft and LinkedIn once for the whole platform. Each product then chooses which to allow in the app’s setup → Sign-in & integrations.',
    google: 'Google',
    microsoft: 'Microsoft 365',
    linkedin: 'LinkedIn',
    googleUse: '“Continue with Google” sign-in, plus Gmail and Google Calendar sync in Email & calendar.',
    microsoftUse: '“Continue with Microsoft” sign-in, plus Outlook mail and calendar sync.',
    linkedinUse: '“Continue with LinkedIn” sign-in.',
    connected: 'Connected',
    notSetUp: 'Not set up',
    step1: '1. Create an OAuth app in {{where}}.',
    step2: '2. Add this redirect URL:',
    step3: '3. Put its ID and secret in the server’s environment (Render → Environment), then redeploy:',
    sso: 'Single sign-on (SAML)',
    ssoUse: 'Set up per product by its admin in Settings → Single sign-on (Okta, Entra ID, Google Workspace…). Nothing to add here.'
  },
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
    workspace: 'Product',
    noWorkspace: 'No product linked',
    errorTitle: 'The last sync failed',
    disabledNote: 'This integration is turned off. Nothing is synced until it’s enabled again.',
    syncNow: 'Sync now',
    synced: '{{name}} synced.',
    syncFailed: 'Sync finished with an error: {{error}}',
    openCrm: 'Open product CRM',
    settings: 'Product settings',
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
    supportBody: 'App support tickets appear under Support in the product, and your replies show in the app.',
    adminsTitle: 'App admins get product access',
    adminsBody: 'App admins get Super Admin access to the product (by invitation).',
    logoutTitle: 'Logouts aren’t tracked yet',
    logoutBody: 'The app doesn’t record logouts, so they can’t be shown.'
  },
  connected: 'Connected: {{name}}'
};

export default strings;
