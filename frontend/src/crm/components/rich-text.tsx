import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { EditorContent, useEditor, useEditorState, type Editor } from '@tiptap/react';
import { mergeAttributes, Node } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { TaskItem, TaskList } from '@tiptap/extension-list';
import { Placeholder } from '@tiptap/extensions';
import Image from '@tiptap/extension-image';
import DOMPurify from 'dompurify';
import {
  Bold,
  Code,
  Heading2,
  Heading3,
  ImagePlus,
  Italic,
  Link2,
  List,
  ListChecks,
  ListOrdered,
  Quote,
  Strikethrough
} from 'lucide-react';
import type { LookupValue } from '@crm/api/types';
import { cn } from '@crm/lib/utils';
import { Spinner } from './ui/spinner';

// Rich text (D-75): notes, task comments and descriptions. Stored as HTML that the
// server sanitizes; shown through DOMPurify again. Older plain text still shows as-is.

export const isRichHTML = (s: string | null | undefined) => Boolean(s && s.trim().startsWith('<'));

const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Plain text (older values) → paragraphs, so the editor can open it. */
function toEditorHtml(value: string): string {
  if (!value) return '';
  if (isRichHTML(value)) return value;
  return value
    .split(/\n{2,}/)
    .map((p) => `<p>${escapeHtml(p).replace(/\n/g, '<br>').replace(/@\[([^\]]{1,80})\]\(([0-9a-fA-F-]{36})\)/g, '<span data-type="mention" data-id="$2" data-label="$1">@$1</span>')}</p>`)
    .join('');
}

/** "@Priya" chips, stored as <span data-type="mention" data-id="…" data-label="…">. */
const Mention = Node.create({
  name: 'mention',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: false,
  addAttributes() {
    return {
      id: { default: null, parseHTML: (el) => el.getAttribute('data-id'), renderHTML: (a) => ({ 'data-id': a.id }) },
      label: { default: '', parseHTML: (el) => el.getAttribute('data-label') ?? '', renderHTML: (a) => ({ 'data-label': a.label }) }
    };
  },
  parseHTML() {
    return [{ tag: 'span[data-type="mention"]' }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ['span', mergeAttributes({ 'data-type': 'mention', class: 'crm-mention' }, HTMLAttributes), `@${node.attrs.label as string}`];
  },
  renderText({ node }) {
    return `@${node.attrs.label as string}`;
  }
});

interface MentionState {
  query: string;
  from: number;
  to: number;
}

function mentionAt(editor: Editor): MentionState | null {
  const { $from, empty } = editor.state.selection;
  if (!empty) return null;
  const before = $from.parent.textBetween(Math.max(0, $from.parentOffset - 40), $from.parentOffset, undefined, '￼');
  const m = /(?:^|\s)@([\w.]{0,30})$/.exec(before);
  if (!m) return null;
  const len = m[1]!.length + 1;
  return { query: m[1]!, from: $from.pos - len, to: $from.pos };
}

export function RichTextEditor({
  value,
  onChange,
  placeholder,
  minHeight = 96,
  onImage,
  peopleLookup,
  onSubmit,
  autoFocus,
  ariaLabel,
  disabled,
  invalid
}: {
  value: string;
  onChange: (html: string) => void;
  placeholder?: string;
  minHeight?: number;
  /** Upload an image; resolves to its URL. No button when missing. */
  onImage?: (file: File) => Promise<string>;
  /** People for @mentions. No mentions when missing. */
  peopleLookup?: (q: string) => Promise<LookupValue[]>;
  /** ⌘/Ctrl + Enter. */
  onSubmit?: () => void;
  autoFocus?: boolean;
  ariaLabel?: string;
  disabled?: boolean;
  invalid?: boolean;
}) {
  const { t } = useTranslation();
  const [mention, setMention] = useState<MentionState | null>(null);
  const [active, setActive] = useState(0);
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkUrl, setLinkUrl] = useState('');
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const lastEmitted = useRef<string>(value);
  const handlers = useRef({ onSubmit, pick: (_p: LookupValue) => {}, people: [] as LookupValue[], active: 0, mention: null as MentionState | null });

  const people = useQuery({
    queryKey: ['rich-mention', mention?.query ?? ''],
    queryFn: () => peopleLookup!(mention?.query ?? ''),
    enabled: Boolean(peopleLookup && mention),
    staleTime: 30_000
  });

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [2, 3] }, link: { openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' } } }),
      TaskList,
      TaskItem.configure({ nested: true }),
      Image.configure({ inline: false }),
      Placeholder.configure({ placeholder: placeholder ?? '' }),
      Mention
    ],
    content: toEditorHtml(value),
    editable: !disabled,
    autofocus: autoFocus ? 'end' : false,
    immediatelyRender: true,
    editorProps: {
      attributes: { class: 'crm-rich crm-rich-editor', 'aria-label': ariaLabel ?? placeholder ?? '', role: 'textbox', 'aria-multiline': 'true' },
      handleKeyDown: (_view, e) => {
        const h = handlers.current;
        if (h.mention && h.people.length) {
          if (e.key === 'ArrowDown') {
            setActive((a) => Math.min(a + 1, h.people.length - 1));
            return true;
          }
          if (e.key === 'ArrowUp') {
            setActive((a) => Math.max(a - 1, 0));
            return true;
          }
          if (e.key === 'Enter' || e.key === 'Tab') {
            h.pick(h.people[h.active] ?? h.people[0]!);
            return true;
          }
          if (e.key === 'Escape') {
            setMention(null);
            return true;
          }
        }
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && h.onSubmit) {
          h.onSubmit();
          return true;
        }
        return false;
      }
    },
    onUpdate: ({ editor: ed }) => {
      const html = ed.isEmpty ? '' : ed.getHTML();
      lastEmitted.current = html;
      onChange(html);
      setMention(peopleLookup ? mentionAt(ed) : null);
    },
    onSelectionUpdate: ({ editor: ed }) => setMention(peopleLookup ? mentionAt(ed) : null)
  });

  // Outside changes (e.g. cleared after saving) flow back into the editor.
  useEffect(() => {
    if (!editor || value === lastEmitted.current) return;
    lastEmitted.current = value;
    editor.commands.setContent(toEditorHtml(value), { emitUpdate: false });
  }, [value, editor]);

  useEffect(() => {
    editor?.setEditable(!disabled);
  }, [editor, disabled]);

  useEffect(() => setActive(0), [mention?.query]);

  const list = people.data ?? [];
  handlers.current = {
    onSubmit,
    people: list,
    active,
    mention,
    pick: (p: LookupValue) => {
      if (!editor || !mention) return;
      editor
        .chain()
        .focus()
        .insertContentAt({ from: mention.from, to: mention.to }, [
          { type: 'mention', attrs: { id: p.id, label: p.label.split(' · ')[0] } },
          { type: 'text', text: ' ' }
        ])
        .run();
      setMention(null);
    }
  };

  const state = useEditorState({
    editor,
    selector: ({ editor: ed }) =>
      ed
        ? {
            bold: ed.isActive('bold'),
            italic: ed.isActive('italic'),
            strike: ed.isActive('strike'),
            h2: ed.isActive('heading', { level: 2 }),
            h3: ed.isActive('heading', { level: 3 }),
            bullet: ed.isActive('bulletList'),
            ordered: ed.isActive('orderedList'),
            task: ed.isActive('taskList'),
            quote: ed.isActive('blockquote'),
            code: ed.isActive('codeBlock'),
            link: ed.isActive('link')
          }
        : null
  });

  if (!editor) return null;
  const tool = (label: string, on: boolean | undefined, run: () => void, icon: ReactNode) => (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={Boolean(on)}
      disabled={disabled}
      onMouseDown={(e) => e.preventDefault()}
      onClick={run}
      className={cn('grid size-7 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50', on && 'bg-primary-soft text-primary')}
    >
      {icon}
    </button>
  );
  const applyLink = () => {
    const url = linkUrl.trim();
    if (!url) editor.chain().focus().extendMarkRange('link').unsetLink().run();
    else editor.chain().focus().extendMarkRange('link').setLink({ href: /^(https?:|mailto:)/i.test(url) ? url : `https://${url}` }).run();
    setLinkOpen(false);
    setLinkUrl('');
  };

  return (
    <div className={cn('relative rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring/40', invalid && 'border-danger')}>
      <div className="flex flex-wrap items-center gap-0.5 border-b px-1.5 py-1" role="toolbar" aria-label={t('richText.toolbar')}>
        {tool(t('richText.bold'), state?.bold, () => editor.chain().focus().toggleBold().run(), <Bold className="size-3.5" />)}
        {tool(t('richText.italic'), state?.italic, () => editor.chain().focus().toggleItalic().run(), <Italic className="size-3.5" />)}
        {tool(t('richText.strike'), state?.strike, () => editor.chain().focus().toggleStrike().run(), <Strikethrough className="size-3.5" />)}
        <span className="mx-1 h-4 w-px bg-border" aria-hidden />
        {tool(t('richText.h2'), state?.h2, () => editor.chain().focus().toggleHeading({ level: 2 }).run(), <Heading2 className="size-3.5" />)}
        {tool(t('richText.h3'), state?.h3, () => editor.chain().focus().toggleHeading({ level: 3 }).run(), <Heading3 className="size-3.5" />)}
        <span className="mx-1 h-4 w-px bg-border" aria-hidden />
        {tool(t('richText.bullets'), state?.bullet, () => editor.chain().focus().toggleBulletList().run(), <List className="size-3.5" />)}
        {tool(t('richText.numbers'), state?.ordered, () => editor.chain().focus().toggleOrderedList().run(), <ListOrdered className="size-3.5" />)}
        {tool(t('richText.checklist'), state?.task, () => editor.chain().focus().toggleTaskList().run(), <ListChecks className="size-3.5" />)}
        {tool(t('richText.quote'), state?.quote, () => editor.chain().focus().toggleBlockquote().run(), <Quote className="size-3.5" />)}
        {tool(t('richText.code'), state?.code, () => editor.chain().focus().toggleCodeBlock().run(), <Code className="size-3.5" />)}
        <span className="mx-1 h-4 w-px bg-border" aria-hidden />
        {tool(t('richText.link'), state?.link, () => {
          setLinkUrl((editor.getAttributes('link').href as string | undefined) ?? '');
          setLinkOpen((o) => !o);
        }, <Link2 className="size-3.5" />)}
        {onImage
          ? tool(t('richText.image'), false, () => fileRef.current?.click(), uploading ? <Spinner className="size-3.5" /> : <ImagePlus className="size-3.5" />)
          : null}
        {onImage ? (
          <input
            ref={fileRef}
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            className="hidden"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              e.target.value = '';
              if (!file) return;
              setUploading(true);
              try {
                const src = await onImage(file);
                editor.chain().focus().setImage({ src, alt: file.name }).run();
              } finally {
                setUploading(false);
              }
            }}
          />
        ) : null}
      </div>
      {linkOpen ? (
        <div className="flex items-center gap-2 border-b px-2 py-1.5">
          <input
            autoFocus
            value={linkUrl}
            onChange={(e) => setLinkUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                applyLink();
              }
              if (e.key === 'Escape') setLinkOpen(false);
            }}
            placeholder="https://…"
            aria-label={t('richText.linkUrl')}
            className="h-7 min-w-0 flex-1 rounded border bg-background px-2 text-[13px] outline-none focus:ring-2 focus:ring-ring/40"
          />
          <button type="button" onClick={applyLink} className="rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground">
            {t('richText.apply')}
          </button>
        </div>
      ) : null}
      <EditorContent editor={editor} style={{ minHeight }} className="px-3 py-2 text-[13px]" />
      {mention && peopleLookup && list.length ? (
        <ul role="listbox" className="absolute left-2 right-2 top-full z-40 mt-1 max-h-56 overflow-auto rounded-md border bg-popover p-1 shadow-pop">
          {list.map((p, i) => (
            <li key={p.id}>
              <button
                type="button"
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => {
                  e.preventDefault();
                  handlers.current.pick(p);
                }}
                className={cn('w-full truncate rounded px-2 py-1.5 text-left text-[13px] hover:bg-muted', i === active && 'bg-muted')}
              >
                {p.label}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

const PURIFY = {
  ALLOWED_TAGS: ['p', 'br', 'h1', 'h2', 'h3', 'strong', 'b', 'em', 'i', 'u', 's', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'hr', 'a', 'img', 'span', 'label', 'input', 'div'],
  ALLOWED_ATTR: ['href', 'target', 'rel', 'src', 'alt', 'data-type', 'data-checked', 'data-id', 'data-label', 'type', 'checked', 'disabled', 'start'],
  ALLOW_DATA_ATTR: false
};

/** Shows rich text safely (or older plain text as it was). */
export function RichTextView({ value, className, clamp }: { value: string | null | undefined; className?: string; clamp?: boolean }) {
  const html = useMemo(() => {
    if (!value || !isRichHTML(value)) return null;
    const clean = DOMPurify.sanitize(value, PURIFY) as string;
    // Checklists are read-only here; links open in a new tab.
    return clean
      .replace(/<input /g, '<input disabled ')
      .replace(/<a /g, '<a target="_blank" rel="noopener noreferrer nofollow" ');
  }, [value]);
  if (!value) return null;
  if (html === null) {
    return <p className={cn('whitespace-pre-wrap break-words text-[13px]', clamp && 'line-clamp-6', className)}>{renderPlainMentions(value)}</p>;
  }
  return <div className={cn('crm-rich text-[13px]', clamp && 'line-clamp-[12]', className)} dangerouslySetInnerHTML={{ __html: html }} />;
}

/** Older notes wrote mentions as @[Name](id). */
function renderPlainMentions(body: string): ReactNode {
  const parts = body.split(/(@\[[^\]]+\]\([0-9a-fA-F-]{36}\))/g);
  return parts.map((p, i) => {
    const m = /^@\[([^\]]+)\]\(([0-9a-fA-F-]{36})\)$/.exec(p);
    return m ? (
      <span key={i} className="crm-mention">
        @{m[1]}
      </span>
    ) : (
      p
    );
  });
}
