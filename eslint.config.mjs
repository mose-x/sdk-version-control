// Minimal ESLint flat config so the code-hooks `nodejs:lint` stage can run
// without erroring on a project that has no lint rules configured yet.
// ESLint v9 requires eslint.config.js | .mjs | .cjs at the repo root;
// without it `npx eslint <files>` exits non-zero and blocks every commit
// that touches .ts/.tsx/.js/.json. An empty config array ([{}]) makes ESLint
// parse files but apply zero rules, so it exits 0 (warnings only for files
// outside the configured file patterns). Upgrade this to a real rule set
// when the project adopts a linting policy.
export default [{}]
