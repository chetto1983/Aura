// Aura's own oxlint rules, loaded through jsPlugins in .oxlintrc.json (an alpha oxlint API, not
// semver: the lockfile pins oxlint, and scripts/lint-contract.mjs proves each rule still fires).

const REGISTERS = new Set(['test', 'it', 'describe']);
// Members a registration can carry: test.only(...), test.describe.serial(...),
// it.each([...])('name', fn), test.skip('name', fn). test.skip(condition, reason) is the
// sanctioned gate, since the runner reports the tests it skips.
const MODIFIERS = new Set([
  'describe',
  'only',
  'skip',
  'serial',
  'parallel',
  'fixme',
  'fail',
  'each',
  'concurrent',
  'sequential',
]);
/**
 * The slice of oxlint's ESTree a rule here reads: oxlint does not export its plugin types.
 * @typedef {{ type: string, name?: string, value?: unknown, parent?: Node | null, callee?: Node,
 *   object?: Node, property?: Node, arguments?: Node[] }} Node
 * @typedef {{ report(descriptor: { node: Node, messageId: string,
 *   data: Record<string, string> }): void }} Context
 */

const CONDITIONS = new Set(['IfStatement', 'ConditionalExpression', 'LogicalExpression']);
const FUNCTIONS = new Set(['FunctionExpression', 'ArrowFunctionExpression', 'FunctionDeclaration']);
// Playwright also overloads these as runtime annotations, which register nothing: test.skip(),
// test.skip(condition), test.skip(condition, reason), test.skip(callback, reason).
const ANNOTATIONS = new Set(['test.skip', 'test.fixme', 'test.fail']);

/**
 * registration names the call when it registers a test or a group: "test", "test.describe", …
 * @param {Node} callee
 * @returns {string | null}
 */
function registration(callee) {
  if (callee.type === 'Identifier') {
    return callee.name !== undefined && REGISTERS.has(callee.name) ? callee.name : null;
  }
  if (callee.type === 'CallExpression') return callee.callee ? registration(callee.callee) : null;
  const { object, property } = callee;
  if (callee.type !== 'MemberExpression' || !object || property?.type !== 'Identifier') return null;
  const base = registration(object);
  if (base === null || property.name === undefined || !MODIFIERS.has(property.name)) return null;
  return `${base}.${property.name}`;
}

/** @param {Node | undefined} node */
function isTitle(node) {
  if (node?.type === 'TemplateLiteral') return true;
  return node?.type === 'Literal' && typeof node.value === 'string';
}

/**
 * registers names the call when it declares a test or a group: a title then a body, inline or by
 * reference (test(title, fn), test(title, details, fn)), or a group body alone (test.describe(fn)).
 * A call that can also be an annotation declares only with a literal title or a function literal
 * after its first argument, so test.skip(title, body) with both held in variables reads as an
 * annotation: the one declaration this rule cannot see.
 * @param {Node} node
 * @returns {string | null}
 */
function registers(node) {
  if (node.type !== 'CallExpression' || !node.callee) return null;
  const name = registration(node.callee);
  if (name === null) return null;
  const args = node.arguments ?? [];
  const first = args.at(0);
  const rest = args.slice(1);
  const inlineBody = rest.some((argument) => FUNCTIONS.has(argument.type));
  const declares = ANNOTATIONS.has(name)
    ? inlineBody || (rest.length > 0 && isTitle(first))
    : rest.length > 0 || (first !== undefined && FUNCTIONS.has(first.type));
  return declares ? name : null;
}

/**
 * The condition a registration sits under, or null. A test's own body is not a registration
 * scope, so a condition inside it gates assertions, not the test; a group's body is one.
 * @param {Node} node
 * @returns {string | null}
 */
function condition(node) {
  for (let child = node, parent = node.parent; parent; child = parent, parent = parent.parent) {
    if (CONDITIONS.has(parent.type)) return parent.type;
    const group = FUNCTIONS.has(child.type) ? registers(parent) : null;
    if (group !== null && !group.includes('describe')) return null;
  }
  return null;
}

const noConditionalTestRegistration = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'A test registered only under a condition never reaches the run report when the condition is false, so the test ledger cannot see it; gate it with test.skip(condition, reason) instead.',
    },
    messages: {
      conditional:
        '{{name}} is registered under {{under}}: when it is false the test is missing from the report instead of skipped. Use test.skip(condition, reason).',
    },
    schema: [],
  },
  /** @param {Context} context */
  create(context) {
    return {
      /** @param {Node} node */
      CallExpression(node) {
        const name = registers(node);
        if (name === null) return;
        const under = condition(node);
        if (under !== null)
          context.report({ node, messageId: 'conditional', data: { name, under } });
      },
    };
  },
};

export default {
  meta: { name: 'aura' },
  rules: { 'no-conditional-test-registration': noConditionalTestRegistration },
};
