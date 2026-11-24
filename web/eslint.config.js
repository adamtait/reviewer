// SPDX-License-Identifier: MIT

import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist/', 'node_modules/', 'test-results/', 'playwright-report/'] },
  ...tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    rules: {
      // Numbers in template strings are how every address here is built.
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // The description and every answer are untrusted text. Nothing in this
      // application may hand a string to the browser as HTML.
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: 'Never set HTML from a string: descriptions and answers are untrusted.',
        },
        {
          selector: "AssignmentExpression[left.property.name='innerHTML']",
          message: 'Never set HTML from a string: descriptions and answers are untrusted.',
        },
      ],
    },
  },
  { files: ['eslint.config.js'], ...tseslint.configs.disableTypeChecked },
);
