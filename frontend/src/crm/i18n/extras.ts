// Record page extras: timeline, files, email, merge (merged into the crm bundle at the top level).
const strings = {
  notifications: {
    title: 'Notifications',
    open_one: 'Notifications ({{count}} unread)',
    open_other: 'Notifications ({{count}} unread)',
    readAll: 'Mark all read',
    empty: 'You’re all caught up. Mentions, assignments and workflow messages appear here.'
  },
  timeline: {
    filter: { all: 'All', notes: 'Notes', emails: 'Emails', tasks: 'Tasks', events: 'Meetings', files: 'Files', history: 'Changes' },
    empty: 'Nothing here yet. Notes, emails, meetings, tasks and changes to this record show up here.',
    more: 'Show older activity',
    updated: 'Changed',
    deleteNote: 'Delete note',
    emailFromTo: 'From {{from}} to {{to}}',
    direction: { inbound: 'Received', outbound: 'Sent' },
    compose: { note: 'Note', call: 'Log a call' },
    notePlaceholder: 'Write a note… Type @ to mention someone.',
    callPlaceholder: 'What was the call about? Next steps?',
    mentionHint: 'Type @ to mention a teammate — they get notified. ⌘↵ to save.',
    addNote: 'Add note',
    logCall: 'Log call',
    noteAdded: 'Note added.',
    callLogged: 'Call logged.'
  },
  files: {
    drop: 'Drop files here or click to upload (up to 10 MB each)',
    uploading: 'Uploading…',
    empty: 'No files yet.',
    tooBig: '{{name}} is larger than 10 MB.',
    download: 'Download {{name}}',
    delete: 'Delete {{name}}'
  },
  email: {
    title: 'Email {{name}}',
    subtitle: 'Sent from your connected mailbox, or from the CRM’s address with replies going to you. It’s saved on this record’s timeline.',
    from: 'From',
    fromCrm: 'The CRM’s address (replies come to you)',
    to: 'To',
    cc: 'Cc',
    subject: 'Subject',
    message: 'Message',
    send: 'Send email',
    sent: 'Email sent.'
  },
  merge: {
    title: 'Merge duplicates of {{name}}',
    subtitle: 'Keep this record and fold a duplicate into it. Everything linked to the duplicate moves here.',
    none: 'No likely duplicates found (same email, phone or name).',
    compare: 'Compare',
    pickValues: 'Where the two differ, choose which value to keep.',
    field: 'Field',
    keep: 'This record',
    other: 'Duplicate',
    same: 'Their fields are the same — merging just moves the duplicate’s history, files and links here.',
    what: 'The duplicate ({{code}}) goes to the recycle bin after merging, so it can still be restored.',
    back: 'Back to the list',
    merge: 'Merge',
    done: 'Records merged.'
  }
};

export default strings;
