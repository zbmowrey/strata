// Strict TypeScript profile (docs/spec/06-gates.md §4.3).
import js from '@eslint/js'
import sonarjs from 'eslint-plugin-sonarjs'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['node_modules/**', 'vendor/**', '.claude-plugin/types/**', 'eslint.config.js'] },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  {
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    plugins: { sonarjs },
    rules: {
      'sonarjs/cognitive-complexity': ['error', 15],
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // The mod API spells its shapes as type aliases; the plugin follows it.
      '@typescript-eslint/consistent-type-definitions': ['error', 'type'],
      // checksVoidReturn walks every overload of the API's `on` (≈5 min a file);
      // hooks return promises by design. Conditionals and spreads stay checked.
      '@typescript-eslint/no-misused-promises': ['error', { checksVoidReturn: false }],
    },
  },
)
