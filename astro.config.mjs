// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightThemeFlexoki from 'starlight-theme-flexoki';

// https://astro.build/config
export default defineConfig({
	integrations: [
		starlight({
			title: 'Object Lease Controller',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/ullbergm/object-lease-controller' }],
			plugins: [
				starlightThemeFlexoki({
				accentColor: "green",
				}),
			],
  			customCss: ["./src/styles/global.css"],
			sidebar: [
				{
					label: 'Guides',
					items: [
						// Each item here is one entry in the navigation menu.
						{ label: 'Getting Started', slug: 'guides/getting-started' },
					],
				},
				// {
				// 	label: 'Reference',
				// 	autogenerate: { directory: 'reference' },
				// },
			],
		}),
	],
});
