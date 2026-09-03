let token = '';
let rules = [];
let lastPlan = null;
let lastCollectPlan = null;
let collectLoaded = false;
const expanded = new Set();
const collectExpanded = new Set();
const $ = id => document.getElementById(id);

async function api(path, options = {}) {
  options.headers = { ...(options.headers || {}), 'X-FolderMirror-Token': token };
  const response = await fetch(path, options);
  if (path === '/api/status') token = response.headers.get('X-FolderMirror-Token') || token;
  if (!response.ok) throw new Error((await response.text()).trim());
  return response.json();
}

function effective(path) {
  let value = false, best = -1;
  for (const rule of rules) {
    if (rule.path === '' || path === rule.path || path.startsWith(rule.path + '/')) {
      const depth = rule.path === '' ? 0 : rule.path.split('/').length;
      if (depth >= best) { best = depth; value = rule.include; }
    }
  }
  return value;
}

function explicit(path) { return rules.find(rule => rule.path === path); }

function setRule(path, value) {
  const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
  const inherited = effective(parent);
  rules = rules.filter(rule => rule.path !== path);
  if (value !== inherited || path === '') rules.push({ path, include: value });
  rules.sort((a, b) => a.path.localeCompare(b.path));
  $('selected').textContent = rules.length;
  lastPlan = null;
  $('apply').disabled = true;
}

function foldButton(node, openSet, rerender) {
  const hasChildren = Boolean(node.children?.length), isOpen = openSet.has(node.path);
  const button = document.createElement('button');
  button.className = 'fold'; button.type = 'button'; button.disabled = !hasChildren;
  button.setAttribute('aria-label', `${isOpen ? 'Collapse' : 'Expand'} ${node.name}`);
  button.setAttribute('aria-expanded', String(isOpen)); button.textContent = hasChildren ? '›' : '';
  button.onclick = () => { if (isOpen) openSet.delete(node.path); else openSet.add(node.path); rerender(); };
  return button;
}

function mirrorNodeView(node) {
  const toggle = foldButton(node, expanded, () => renderTree(window.treeData));
  const box = document.createElement('input');
  box.type = 'checkbox'; box.checked = effective(node.path); box.setAttribute('aria-label', `Mirror ${node.name}`);
  box.onchange = () => { setRule(node.path, box.checked); renderTree(window.treeData); };
  const label = document.createElement('label'); label.textContent = node.name;
  label.onclick = () => { if (node.children?.length) toggle.click(); };
  const row = document.createElement('div'); row.className = 'node-row'; row.append(toggle, box, label);
  if (explicit(node.path)) { const badge = document.createElement('span'); badge.className = 'override'; badge.textContent = 'override'; row.append(badge); }
  return wrapNode(node, row, expanded.has(node.path), mirrorNodeView);
}

function collectNodeView(node) {
  const toggle = foldButton(node, collectExpanded, () => renderCollectTree(window.collectTreeData));
  const radio = document.createElement('input');
  radio.type = 'radio'; radio.name = 'collect-source'; radio.checked = $('collect-folder').value === node.path;
  radio.setAttribute('aria-label', `Collect from ${node.name}`);
  radio.onchange = () => { $('collect-folder').value = node.path; lastCollectPlan = null; $('collect-apply').disabled = true; renderCollectTree(window.collectTreeData); };
  const label = document.createElement('label'); label.textContent = node.name; label.onclick = () => radio.click();
  const row = document.createElement('div'); row.className = 'node-row'; row.append(toggle, radio, label);
  return wrapNode(node, row, collectExpanded.has(node.path), collectNodeView);
}

function wrapNode(node, row, isOpen, renderer) {
  const wrap = document.createElement('div'); wrap.append(row);
  if (node.children?.length && isOpen) {
    const children = document.createElement('div'); children.className = 'children';
    for (const child of node.children) children.append(renderer(child));
    wrap.append(children);
  }
  return wrap;
}

function renderRoots(host, tree, renderer, emptyText) {
  const nodes = tree.children || [];
  host.replaceChildren(...nodes.map(renderer));
  if (!nodes.length) { const empty = document.createElement('div'); empty.className = 'empty'; empty.textContent = emptyText; host.append(empty); }
}

function renderTree(tree) { renderRoots($('tree'), tree, mirrorNodeView, 'The storage folder has no subfolders.'); }
function renderCollectTree(tree) { renderRoots($('collect-tree'), tree, collectNodeView, 'The imports folder has no subfolders.'); }
function normalizedPlan(plan) { return { create: plan.create || [], remove: plan.remove || [], skip: plan.skip || [] }; }

function renderPlanInto(plan, host, applyButton, message, createTitle = 'Create links') {
  plan = normalizedPlan(plan); host.replaceChildren();
  for (const [key, title] of [['create', createTitle], ['remove', 'Remove stale links'], ['skip', 'Needs attention']]) {
    if (!plan[key].length) continue;
    const heading = document.createElement('h3'); heading.textContent = `${title} · ${plan[key].length}`; host.append(heading);
    for (const action of plan[key]) { const item = document.createElement('div'); item.className = `item ${key}`; item.textContent = `${action.path}${action.detail ? ' — ' + action.detail : ''}`; host.append(item); }
  }
  const changes = plan.create.length + plan.remove.length;
  applyButton.disabled = changes === 0;
  message.textContent = changes ? `${changes} safe change${changes === 1 ? '' : 's'} ready to apply.` : 'Everything is up to date.';
  return plan;
}

function collectRequest() { return { folder: $('collect-folder').value, pattern: $('collect-pattern').value, destination: $('collect-destination').value }; }
function showError(element, error) { element.textContent = error.message; element.className = 'message error'; }

async function showMode(mode) {
  const collect = mode === 'collect';
  $('mirror-mode').hidden = collect; $('collect-mode').hidden = !collect;
  $('mirror-tab').classList.toggle('active', !collect); $('collect-tab').classList.toggle('active', collect);
  $('mirror-tab').setAttribute('aria-selected', String(!collect)); $('collect-tab').setAttribute('aria-selected', String(collect));
  if (collect && !collectLoaded && !$('collect-tab').disabled) {
    try { window.collectTreeData = await api('/api/collect/tree'); renderCollectTree(window.collectTreeData); collectLoaded = true; }
    catch (error) { showError($('collect-message'), error); }
  }
}

async function init() {
  try {
    const status = await api('/api/status');
    $('storage').textContent = status.storage; $('collect-storage').textContent = status.storage;
    $('mirror').textContent = status.mirror; $('imports').textContent = status.imports || 'Not configured';
    $('platform').textContent = `${status.platform} · ${status.instance}`;
    $('managed').textContent = status.managedFiles; rules = status.rules || []; $('selected').textContent = rules.length;
    if (!status.imports) { $('collect-tab').disabled = true; $('collect-tab').title = 'Restart with -imports PATH to enable collection mode'; }
    window.treeData = await api('/api/tree'); renderTree(window.treeData);
  } catch (error) { showError($('message'), error); }
}

$('mirror-tab').onclick = () => showMode('mirror');
$('collect-tab').onclick = () => showMode('collect');
$('save').onclick = async () => { try { await api('/api/rules', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rules) }); $('message').textContent = 'Choices saved. Preview the resulting changes.'; } catch (error) { showError($('message'), error); } };
$('preview').onclick = async () => { try { await api('/api/rules', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rules) }); lastPlan = renderPlanInto(await api('/api/plan', { method: 'POST' }), $('plan'), $('apply'), $('message')); } catch (error) { showError($('message'), error); } };
$('apply').onclick = async () => { if (!lastPlan) return; try { lastPlan = renderPlanInto(await api('/api/apply', { method: 'POST' }), $('plan'), $('apply'), $('message')); const status = await api('/api/status'); $('managed').textContent = status.managedFiles; $('apply').disabled = true; $('message').textContent = 'Reconciliation complete.'; } catch (error) { showError($('message'), error); } };
$('collect-preview').onclick = async () => { try { lastCollectPlan = renderPlanInto(await api('/api/collect/plan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(collectRequest()) }), $('collect-plan'), $('collect-apply'), $('collect-message'), 'Create collected links'); } catch (error) { showError($('collect-message'), error); } };
$('collect-apply').onclick = async () => { if (!lastCollectPlan) return; try { lastCollectPlan = renderPlanInto(await api('/api/collect/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(collectRequest()) }), $('collect-plan'), $('collect-apply'), $('collect-message'), 'Created links'); $('collect-apply').disabled = true; $('collect-message').textContent = 'Collection complete.'; } catch (error) { showError($('collect-message'), error); } };
for (const id of ['collect-pattern', 'collect-destination']) $(id).oninput = () => { lastCollectPlan = null; $('collect-apply').disabled = true; };
init();
