// Record page extras: timeline, files, email, merge (merged into the crm bundle at the top level).
const strings = {
  richText: {
    toolbar: 'Formatting',
    bold: 'Bold (⌘B)',
    italic: 'Italic (⌘I)',
    strike: 'Strikethrough',
    h2: 'Heading',
    h3: 'Subheading',
    bullets: 'Bulleted list',
    numbers: 'Numbered list',
    checklist: 'Checklist',
    quote: 'Quote',
    code: 'Code block',
    link: 'Link',
    linkUrl: 'Link address',
    apply: 'Apply',
    image: 'Add image'
  },
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
  shortcuts: {
    title: 'Keyboard shortcuts',
    subtitle: 'Press ? anywhere to see this list. Shortcuts don’t fire while you type in a field.',
    search: 'Search and jump to anything',
    help: 'Show keyboard shortcuts',
    goTo: 'Go to {{page}}',
    nextRecord: 'Next record (on a record page)',
    previousRecord: 'Previous record (on a record page)',
    editRecord: 'Edit the record',
    save: 'Save changes',
    cancel: 'Cancel editing'
  },
  emails: {
    loadError: 'Couldn’t load the emails',
    count_one: '{{count}} conversation',
    count_other: '{{count}} conversations',
    new: 'New email',
    noneTitle: 'No emails yet',
    noneBody: 'Emails you send from here, and synced emails with this person, show up as conversations.',
    private: 'Private email',
    back: 'Back to conversations',
    messages_one: '{{count}} message',
    messages_other: '{{count}} messages',
    reply: 'Reply',
    replyAll: 'Reply all',
    replyPlaceholder: 'Write your reply… (⌘/Ctrl + Enter to send)',
    send: 'Send reply',
    failed: 'Not sent',
    toLine: 'To {{to}}',
    ccLine: 'Cc {{cc}}',
    hiddenBody: 'The mailbox owner shares only the subject of this email.'
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
