// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { docsBase } from './versions.mjs';

export default defineConfig({
	site: 'https://kevinjom.github.io',
	base: docsBase(),
	integrations: [
		starlight({
			title: 'co',
			description: 'A coding assistant for the directory you start it in.',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/kevinjom/co' }],
			components: {
				SiteTitle: './src/components/SiteTitle.astro',
			},
			sidebar: [
				{
					label: 'Start',
					items: [
						{ label: 'Overview', slug: 'index' },
						{ label: 'Install', slug: 'install' },
					],
				},
				{
					label: 'Setup',
					items: [
						{ label: 'Providers', slug: 'setup/providers' },
						{ label: 'API keys', slug: 'setup/api-keys' },
						{ label: 'Settings', slug: 'setup/settings' },
					],
				},
				{
					label: 'Use',
					items: [
						{ label: 'Sessions and commands', slug: 'use/sessions' },
						{ label: 'Tools', slug: 'use/tools' },
					],
				},
				{
					label: 'Tools',
					items: [
						{ label: 'Bash', slug: 'tools/bash' },
						{ label: 'Language server', slug: 'tools/lsp' },
						{ label: 'MCP', slug: 'tools/mcp' },
					],
				},
			],
		}),
	],
});
