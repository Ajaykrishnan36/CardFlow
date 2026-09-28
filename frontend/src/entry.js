// Picks which app to boot for this URL. CardFlow (src/index.js) is unchanged; Ajay's CRM
// owns everything under /crm. Both are separate lazy chunks.
if (/^\/crm(\/|$)/.test(window.location.pathname)) {
  import('./crm/main');
} else {
  import('./index');
}
