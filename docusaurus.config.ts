import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'l8opensim',
  tagline: 'Layer 8 data center simulator — SNMP/SSH/HTTPS at 30,000-device scale',
  // Favicon deliberately omitted until a branded asset is added under static/img/.
  // Docusaurus warns cleanly when the field is absent rather than when it's
  // pointing at a missing file.

  future: {
    v4: true,
  },

  // Canonical published URL; baseUrl is the project-scoped path.
  url: 'https://labmonkeys-space.github.io',
  baseUrl: '/l8opensim/',

  // GitHub Pages deployment config.
  organizationName: 'labmonkeys-space',
  projectName: 'l8opensim',
  trailingSlash: false,

  // Strict-mode: matches the MkDocs `--strict` posture the project previously had.
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  themes: [
    '@docusaurus/theme-mermaid',
    [
      // Local full-text search. Algolia DocSearch is out of scope for phase 1
      // (application process is multi-day); swap later without docs churn.
      '@easyops-cn/docusaurus-search-local',
      {
        hashed: true,
        language: ['en'],
        indexDocs: true,
        indexBlog: false,
        docsRouteBasePath: '/',
      },
    ],
  ],

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: 'docs',
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/labmonkeys-space/l8opensim/edit/main/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'l8opensim',
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'gettingStarted',
          position: 'left',
          label: 'Getting Started',
        },
        {
          type: 'docSidebar',
          sidebarId: 'ops',
          position: 'left',
          label: 'Ops',
        },
        {
          type: 'docSidebar',
          sidebarId: 'reference',
          position: 'left',
          label: 'Reference',
        },
        {
          href: 'https://github.com/labmonkeys-space/l8opensim',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Quick Start', to: '/getting-started/quick-start'},
            {label: 'CLI Flags', to: '/reference/cli-flags'},
            {label: 'Web API', to: '/reference/web-api'},
          ],
        },
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/labmonkeys-space/l8opensim',
            },
            {
              label: 'Issues',
              href: 'https://github.com/labmonkeys-space/l8opensim/issues',
            },
            {
              label: 'Releases',
              href: 'https://github.com/labmonkeys-space/l8opensim/releases',
            },
          ],
        },
      ],
      copyright: `© ${new Date().getFullYear()} labmonkeys-space. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json', 'go', 'python', 'diff'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
