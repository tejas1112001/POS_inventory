import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{js,jsx}'],
    extends: [
      js.configs.recommended,
      reactHooks.configs['recommended-latest'],
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        ecmaVersion: 'latest',
        ecmaFeatures: { jsx: true },
        sourceType: 'module',
      },
    },
    rules: {
      'no-unused-vars': ['error', { varsIgnorePattern: '^[A-Z_]' }],
    },
  },
  {
    // Super Admin console: shop names and other tenant text are shown as
    // plain text only (THREAT_MODEL §3.6), and the strict admin CSP allows
    // no inline styles. Each rule names what it stops.
    files: ['admin-console/**/*.{js,jsx}'],
    rules: {
      'no-restricted-syntax': [
        'error',
        { selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']", message: 'No HTML from strings in the console: render text.' },
        { selector: "MemberExpression[property.name=/^(innerHTML|outerHTML)$/]", message: 'No HTML from strings in the console.' },
        { selector: "CallExpression[callee.property.name='insertAdjacentHTML']", message: 'No HTML from strings in the console.' },
        { selector: "JSXAttribute[name.name='style']", message: 'Inline styles break the strict CSP: use a CSS class.' },
        { selector: "JSXAttribute[name.name=/^(href|src|action|formAction)$/][value.type='JSXExpressionContainer']", message: 'No computed URLs in href/src: tenant data must never become a link.' },
      ],
      'no-restricted-globals': ['error', { name: 'eval', message: 'No eval under the strict CSP.' }],
    },
  },
  {
    // Build and tool configs run in Node.
    files: ['*.config.js'],
    languageOptions: { globals: globals.node },
  },
])
