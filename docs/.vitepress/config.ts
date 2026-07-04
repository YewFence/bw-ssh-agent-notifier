import { defineConfig } from 'vitepress'

export default defineConfig({
  base: '/bw-ssh-agent-notifier/',
  lang: 'en-US',
  title: 'bwsshntfr',
  description: 'simple wrapper for notify who use bw ssh agent on linux desktop',

  themeConfig: {
    nav: [
      { text: 'Shell Completion', link: '/guide/completion' },
      { text: 'GitHub', link: 'https://github.com/YewFence/bw-ssh-agent-notifier' }
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Shell Completion', link: '/guide/completion' }
        ]
      }
    ],

    search: {
      provider: 'local'
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/YewFence/bw-ssh-agent-notifier' }
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © YewFence'
    },

    docFooter: {
      prev: 'Previous page',
      next: 'Next page'
    },

    outline: {
      label: 'On this page'
    },

    lastUpdated: {
      text: 'Last updated'
    }
  }
})
