// @ts-check
import casoonPages from '@casoon/pages-theme';
import { defineConfig } from 'astro/config';

// Project page: https://casoon.github.io/go-tutorial/
export default defineConfig({
  site: 'https://casoon.github.io/go-tutorial',
  base: '/go-tutorial/',
  integrations: [
    casoonPages({
      name: 'go-tutorial',
      description:
        'Go für echte Projekte: ein durchgehendes Tutorial, das Schritt für Schritt einen produktionsnahen URL-Monitor baut.',
      repo: 'casoon/go-tutorial',
      license: 'MIT',
      lang: 'de',
      docsDir: 'kapitel',
      showcase: false,
      changelog: false,
      docsGroups: {
        grundlagen: 'Grundlagen',
        anwendung: 'Die Anwendung',
        produktion: 'Produktion',
      },
    }),
  ],
});
