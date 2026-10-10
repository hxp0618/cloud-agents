import assert from "node:assert/strict";

// Selects a tenant and project through page-evaluated expressions. Admin Web shows a single
// available tenant or project as text carrying data-scope-id instead of a one-option selector.
export async function selectScope({
  evaluate,
  waitFor,
  tenantSelector,
  projectSelector,
  tenantId,
  projectId,
  label,
}) {
  const fallback = (scope) => `document.querySelector('[data-scope=${scope}]')`;
  const choose = (selector, scope, id) => `(() => {
    const select = document.querySelector(${JSON.stringify(selector)});
    if (select === null) return ${fallback(scope)}?.dataset.scopeId === ${JSON.stringify(id)};
    if (![...select.options].some(option => option.value === ${JSON.stringify(id)})) return false;
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(select, ${JSON.stringify(id)});
    select.dispatchEvent(new Event('change', { bubbles: true }));
    return true;
  })()`;
  const current = (selector, scope, id) =>
    `(document.querySelector(${JSON.stringify(selector)})?.value ?? ${fallback(scope)}?.dataset.scopeId) === ${JSON.stringify(id)}`;

  await waitFor(
    `(document.querySelector(${JSON.stringify(tenantSelector)}) ?? ${fallback("tenant")}) !== null`,
    `${label} tenant scope`,
  );
  assert.equal(
    await evaluate(choose(tenantSelector, "tenant", tenantId)),
    true,
    `${label} tenant selector`,
  );
  await waitFor(current(tenantSelector, "tenant", tenantId), `${label} tenant context`);
  await waitFor(
    `document.querySelector(${JSON.stringify(projectSelector)}) === null ? ${fallback("project")} !== null : [...document.querySelectorAll(${JSON.stringify(projectSelector)} + ' option')].some(option => option.value === ${JSON.stringify(projectId)})`,
    `${label} project option`,
  );
  assert.equal(
    await evaluate(choose(projectSelector, "project", projectId)),
    true,
    `${label} project selector`,
  );
  await waitFor(current(projectSelector, "project", projectId), `${label} project context`);
}
