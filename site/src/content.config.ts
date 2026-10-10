import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import { docsSchema } from '@casoon/pages-theme/content';
import { defineCollection } from 'astro:content';
import type { Loader } from 'astro/loaders';

// The chapters live in ../kapitel as plain Markdown, written to be read on
// GitHub. This loader serves the same files as the site's docs, so there is
// one source: title and order come from the "# Teil NN: Titel" heading, the
// sidebar group from the chapter number, and links are rewritten from
// repository paths to site paths.

const repoBlob = 'https://github.com/casoon/go-tutorial/blob/main/';
const kapitelDir = path.resolve('../kapitel');

/** Sidebar group per chapter; keys match docsGroups in astro.config.mjs. */
function groupOf(chapter: number): string {
  if (chapter <= 7) return 'grundlagen';
  if (chapter <= 12) return 'anwendung';
  return 'produktion';
}

const idOf = (file: string) => `${groupOf(Number(file.slice(0, 2)))}/${file.replace(/\.md$/, '')}`;

/** Rewrites one link target from the chapter's point of view in the repo. */
function rewriteLink(target: string, fromId: string): string {
  if (/^([a-z]+:|#)/i.test(target)) return target;

  const [file, hash] = target.split('#');
  const anchor = hash ? `#${hash}` : '';
  // A page at /docs/<group>/<slug>/ is this many levels below /docs/.
  const toDocs = '../'.repeat(fromId.split('/').length);

  const chapter = file.match(/^(?:\.\/)?(\d{2}-[a-z0-9-]+)\.md$/);
  if (chapter) return `${toDocs}${idOf(`${chapter[1]}.md`)}/${anchor}`;

  // The README is the overview, i.e. the start page.
  if (file === '../README.md') return `${toDocs}../${anchor}`;

  // Anything else in the repository (code, examples, Dockerfile) on GitHub.
  return `${repoBlob}${path.posix.normalize(path.posix.join('kapitel', file))}${anchor}`;
}

function kapitelLoader(): Loader {
  return {
    name: 'kapitel-loader',
    async load({ store, parseData, renderMarkdown, logger }) {
      store.clear();
      const files = (await readdir(kapitelDir)).filter((f) => /^\d{2}-.+\.md$/.test(f)).sort();

      for (const file of files) {
        const raw = await readFile(path.join(kapitelDir, file), 'utf8');
        const heading = raw.match(/^# Teil (\d+): (.+)$/m);
        if (!heading) {
          logger.warn(`${file}: no "# Teil NN: Titel" heading, skipped`);
          continue;
        }
        const id = idOf(file);

        let body = raw
          // The layout renders the title as h1 …
          .replace(/^# .+\n+/, '')
          // … and has its own previous/next pager instead of the footer line.
          .replace(/\n---\n+[^\n]*\[Übersicht\][^\n]*\n*$/, '\n')
          // Inline links, not images.
          .replace(/(?<!!)\]\(([^)\s]+)\)/g, (_, target: string) => `](${rewriteLink(target, id)})`);

        // The first paragraph becomes the lead under the title, so it is
        // taken out of the body instead of appearing twice.
        const lead = body.match(/^(?![#`|>-])(\S.*)$/m)?.[1];
        if (lead && body.startsWith(lead)) body = body.slice(lead.length).trimStart();
        const data = await parseData({
          id,
          data: {
            title: heading[2],
            sidebarLabel: `${Number(heading[1])}. ${heading[2]}`,
            description: lead?.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[*_`]/g, ''),
            order: Number(heading[1]),
          },
        });

        store.set({
          id,
          data,
          body,
          filePath: path.posix.join('..', 'kapitel', file),
          rendered: await renderMarkdown(body),
        });
      }
    },
  };
}

export const collections = {
  docs: defineCollection({ loader: kapitelLoader(), schema: docsSchema }),
};
