import { defineConfig } from 'vitepress'

export default defineConfig({
  base: '/bw-ssh-agent-notifier/',
  lang: 'en-US',
  title: 'bw-ssh-agent-notifier',
  description: 'Show which local process is using the Bitwarden SSH agent on Linux desktop',

  themeConfig: {
    nav: [
      { text: 'Systemd', link: '/guide/systemd' },
      { text: 'Troubleshooting', link: '/guide/troubleshooting' },
      { text: 'Reference', link: '/reference/configuration' },
      { text: 'Shell Completion', link: '/guide/completion' },
      { text: 'GitHub', link: 'https://github.com/YewFence/bw-ssh-agent-notifier' }
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Systemd User Service', link: '/guide/systemd' },
          { text: 'Troubleshooting', link: '/guide/troubleshooting' },
          { text: 'Shell Completion', link: '/guide/completion' }
        ]
      },
      {
        text: 'Reference',
        items: [
          { text: 'Configuration', link: '/reference/configuration' },
          { text: 'Commands', link: '/reference/commands' },
          { text: 'Architecture', link: '/reference/architecture' }
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
