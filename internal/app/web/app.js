let token = '';
let rules = [];
let lastPlan = null;
let lastCollectPlan = null;
let collectLoaded = false;
let wildcardHistory = [];
const expanded = new Set();
const collectExpanded = new Set();
const storageExpanded = new Set();
const $ = id => document.getElementById(id);

let busy = false;
async function withBusy(operation) {
  if (busy) return;
  busy = true;
  $('workspace-controls').disabled = true;
  $('workspace-controls').setAttribute('aria-busy', 'true');
  $('busy-status').hidden = false;
  try {
    return await operation();
  } finally {
    busy = false;
    $('workspace-controls').disabled = false;
    $('workspace-controls').setAttribute('aria-busy', 'false');
    $('busy-status').hidden = true;
  }
}

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

async function loadBranch(node, endpoint, openSet) {
  if (!node.loaded) Object.assign(node, await api(`${endpoint}?path=${encodeURIComponent(node.path)}`));
  await Promise.all((node.children || []).filter(child => child.directory && openSet.has(child.path)).map(child => loadBranch(child, endpoint, openSet)));
  return node;
}
async function loadTree(endpoint, openSet) {
  return loadBranch(await api(endpoint), endpoint, openSet);
}

function foldButton(node, openSet, rerender, endpoint, message) {
  const hasChildren = node.directory && (!node.loaded || Boolean(node.children?.length)), isOpen = openSet.has(node.path);
  const button = document.createElement('button');
  button.className = 'fold'; button.type = 'button'; button.disabled = !hasChildren;
  button.setAttribute('aria-label', `${isOpen ? 'Collapse' : 'Expand'} ${node.name}`);
  button.setAttribute('aria-expanded', String(isOpen)); button.textContent = hasChildren ? '›' : '';
  button.onclick = () => withBusy(async () => {
    try {
      if (isOpen) openSet.delete(node.path);
      else { await loadBranch(node, endpoint, openSet); openSet.add(node.path); }
      rerender();
    } catch (error) { showError($(message), error); }
  });
  return button;
}

function mirrorNodeView(node) {
  const toggle = foldButton(node, expanded, () => renderTree(window.treeData), '/api/tree', 'message');
  const box = document.createElement('input');
  box.type = 'checkbox'; box.checked = effective(node.path); box.setAttribute('aria-label', `Mirror ${node.name}`);
  box.onchange = () => { setRule(node.path, box.checked); renderTree(window.treeData); };
  const label = document.createElement('label'); label.textContent = node.name;
  label.onclick = () => { if (node.directory) toggle.click(); else box.click(); };
  const row = document.createElement('div'); row.className = `node-row ${node.directory ? 'folder-node' : 'file-node'}`; row.append(toggle, box, label);
  if (explicit(node.path)) { const badge = document.createElement('span'); badge.className = 'override'; badge.textContent = 'override'; row.append(badge); }
  return wrapNode(node, row, expanded.has(node.path), mirrorNodeView);
}

function collectNodeView(node) {
  const toggle = foldButton(node, collectExpanded, () => renderCollectTree(window.collectTreeData), '/api/collect/tree', 'collect-message');
  const radio = document.createElement('input');
  const selectedPath = node.path || '.';
  radio.type = 'radio'; radio.name = 'collect-source'; radio.checked = $('collect-folder').value === selectedPath;
  radio.setAttribute('aria-label', `Collect from ${node.name}`);
  radio.onchange = () => { $('collect-folder').value = selectedPath; lastCollectPlan = null; $('collect-apply').disabled = true; renderCollectTree(window.collectTreeData); };
  const label = document.createElement('label'); label.textContent = node.name; label.onclick = () => radio.click();
  const row = document.createElement('div'); row.className = 'node-row'; row.append(toggle, radio, label);
  return wrapNode(node, row, collectExpanded.has(node.path), collectNodeView);
}

function storageNodeView(node) {
  const toggle = foldButton(node, storageExpanded, () => renderStorageTree(window.storageTreeData), '/api/storage/tree', 'collect-message');
  const radio = document.createElement('input');
  radio.type = 'radio'; radio.name = 'collect-destination'; radio.checked = $('collect-destination').value === node.path;
  radio.setAttribute('aria-label', `Collect into ${node.name}`);
  radio.onchange = () => { $('collect-destination').value = node.path; lastCollectPlan = null; $('collect-apply').disabled = true; renderStorageTree(window.storageTreeData); };
  const label = document.createElement('label'); label.textContent = node.name; label.onclick = () => radio.click();
  const add = document.createElement('button'); add.type = 'button'; add.className = 'new-child-folder'; add.textContent = '+'; add.setAttribute('aria-label', `Create folder inside ${node.name}`);
  add.onclick = () => beginFolderCreate(node.path);
  const count = document.createElement('span'); count.className = 'file-count'; count.textContent = node.loaded ? `${node.fileCount || 0} files` : '…'; count.title = 'Files directly in this folder (counted when opened)';
  const row = document.createElement('div'); row.className = 'node-row'; row.append(toggle, radio, label, count, add);
  return wrapNode(node, row, storageExpanded.has(node.path), storageNodeView);
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
function renderCollectTree(tree) { $('collect-tree').replaceChildren(collectNodeView(tree)); }
function renderStorageTree(tree) { renderRoots($('collect-storage-tree'), tree, storageNodeView, 'The storage folder has no subfolders.'); }
function normalizedPlan(plan) { return { create: plan.create || [], remove: plan.remove || [], skip: plan.skip || [] }; }
function planErrorCount(plan) { return plan.skip.filter(action => action.kind === 'error').length; }
function showMessage(element, text, error = false) { element.textContent = text; element.className = `message${error ? ' error' : ''}`; }

function renderPlanInto(plan, host, applyButton, message, createTitle = 'Create links') {
  plan = normalizedPlan(plan); host.replaceChildren();
  for (const [key, title] of [['create', createTitle], ['remove', 'Remove stale links'], ['skip', 'Needs attention']]) {
    if (!plan[key].length) continue;
    const heading = document.createElement('h3'); heading.textContent = `${title} · ${plan[key].length}`; host.append(heading);
    for (const action of plan[key]) { const item = document.createElement('div'); item.className = `item ${key}`; item.textContent = `${action.path}${action.detail ? ' — ' + action.detail : ''}`; host.append(item); }
  }
  const changes = plan.create.length + plan.remove.length;
  applyButton.disabled = changes === 0;
  const errors = planErrorCount(plan);
  if (errors) showMessage(message, `${errors} error${errors === 1 ? '' : 's'} need attention.`, true);
  else showMessage(message, changes ? `${changes} safe change${changes === 1 ? '' : 's'} ready to apply.` : 'Everything is up to date.');
  return plan;
}

function collectRequest() { const folder = $('collect-folder').value; return { folder: folder === '.' ? '' : folder, pattern: $('collect-pattern').value, destination: $('collect-destination').value }; }
function showError(element, error) { showMessage(element, error.message, true); }
function showApplyResult(element, plan, successText) {
  const errors = planErrorCount(plan);
  showMessage(element, errors ? `${errors} action${errors === 1 ? '' : 's'} failed. Successful changes were kept; see Needs attention below.` : successText, errors > 0);
}
async function reloadStorageViews() {
  const [mirror, storage] = await Promise.all([window.treeData ? loadTree('/api/tree', expanded) : null, loadTree('/api/storage/tree', storageExpanded)]);
  if (mirror) { window.treeData = mirror; renderTree(mirror); }
  window.storageTreeData = storage;
  renderStorageTree(window.storageTreeData);
}
function rememberWildcard(pattern) {
  pattern = pattern.trim();
  wildcardHistory = [pattern, ...wildcardHistory.filter(item => item !== pattern)].slice(0, 12);
  renderWildcardHistory();
}

function beginFolderCreate(parent) {
  const form = $('new-folder-form');
  form.dataset.parent = parent;
  $('new-folder-parent').textContent = parent || 'Storage root';
  $('new-folder-name').value = '';
  form.hidden = false;
  $('new-folder-name').focus();
}
function renderWildcardHistory() {
  const list = $('wildcard-history'); list.replaceChildren();
  for (const item of wildcardHistory) { const option = document.createElement('option'); option.value = item; list.append(option); }
}

async function showMode(mode) {
  const collect = mode === 'collect';
  $('mirror-mode').hidden = collect; $('collect-mode').hidden = !collect;
  $('mirror-tab').classList.toggle('active', !collect); $('collect-tab').classList.toggle('active', collect);
  $('mirror-tab').setAttribute('aria-selected', String(!collect)); $('collect-tab').setAttribute('aria-selected', String(collect));
  if (collect && !collectLoaded && !$('collect-tab').disabled) {
    try {
      [window.collectTreeData, window.storageTreeData] = await Promise.all([loadTree('/api/collect/tree', collectExpanded), loadTree('/api/storage/tree', storageExpanded)]);
      renderCollectTree(window.collectTreeData); renderStorageTree(window.storageTreeData); collectLoaded = true;
    }
    catch (error) { showError($('collect-message'), error); }
  }
  else if (!collect) {
    try { window.treeData = await loadTree('/api/tree', expanded); renderTree(window.treeData); }
    catch (error) { showError($('message'), error); }
  }
}

async function init() {
  try {
    const status = await api('/api/status');
    $('storage').textContent = status.storage; $('collect-storage').textContent = status.storage;
    $('mirror').textContent = status.mirror; $('imports').textContent = status.imports || 'Not configured';
    $('platform').textContent = `${status.platform} · ${status.instance}`;
    $('managed').textContent = status.managedFiles; rules = status.rules || []; $('selected').textContent = rules.length;
    wildcardHistory = status.wildcards || []; renderWildcardHistory();
    if (!status.imports) { $('collect-tab').disabled = true; $('collect-tab').title = 'Restart with -imports PATH to enable collection mode'; await showMode('mirror'); }
    else await showMode('collect');
  } catch (error) { showError($('message'), error); }
}

$('mirror-tab').onclick = () => showMode('mirror');
$('collect-tab').onclick = () => showMode('collect');
$('save').onclick = async () => { try { await api('/api/rules', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rules) }); showMessage($('message'), 'Choices saved. Preview the resulting changes.'); } catch (error) { showError($('message'), error); } };
$('preview').onclick = async () => { try { await api('/api/rules', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(rules) }); lastPlan = renderPlanInto(await api('/api/plan', { method: 'POST' }), $('plan'), $('apply'), $('message')); } catch (error) { showError($('message'), error); } };
$('apply').onclick = async () => { if (!lastPlan) return; try { lastPlan = renderPlanInto(await api('/api/apply', { method: 'POST' }), $('plan'), $('apply'), $('message')); const status = await api('/api/status'); $('managed').textContent = status.managedFiles; $('apply').disabled = true; showApplyResult($('message'), lastPlan, 'Reconciliation complete.'); } catch (error) { showError($('message'), error); } };
$('collect-preview').onclick = async () => { try { const request = collectRequest(); lastCollectPlan = renderPlanInto(await api('/api/collect/plan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) }), $('collect-plan'), $('collect-apply'), $('collect-message'), 'Create collected links'); rememberWildcard(request.pattern); } catch (error) { showError($('collect-message'), error); } };
$('collect-apply').onclick = async () => { if (!lastCollectPlan) return; try { const request = collectRequest(); lastCollectPlan = renderPlanInto(await api('/api/collect/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) }), $('collect-plan'), $('collect-apply'), $('collect-message'), 'Created links'); rememberWildcard(request.pattern); $('collect-apply').disabled = true; await reloadStorageViews(); showApplyResult($('collect-message'), lastCollectPlan, 'Collection complete.'); } catch (error) { showError($('collect-message'), error); } };
$('collect-pattern').oninput = () => { lastCollectPlan = null; $('collect-apply').disabled = true; };
$('new-root-folder').onclick = () => beginFolderCreate('');
$('cancel-new-folder').onclick = () => { $('new-folder-form').hidden = true; };
$('new-folder-form').onsubmit = async event => {
  event.preventDefault();
  const form = $('new-folder-form');
  try {
    const response = await api('/api/storage/folders', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ parent: form.dataset.parent || '', name: $('new-folder-name').value }) });
    if (form.dataset.parent) storageExpanded.add(form.dataset.parent);
    $('collect-destination').value = response.path;
    lastCollectPlan = null; $('collect-apply').disabled = true; form.hidden = true;
    await reloadStorageViews();
    showMessage($('collect-message'), `Created storage folder ${response.path}.`);
  } catch (error) { showError($('collect-message'), error); }
};
for (const id of ['mirror-tab', 'collect-tab', 'save', 'preview', 'apply', 'collect-preview', 'collect-apply']) {
  const button = $(id);
  const handler = button.onclick;
  button.onclick = event => withBusy(() => handler(event));
}
const createFolder = $('new-folder-form').onsubmit;
$('new-folder-form').onsubmit = event => {
  event.preventDefault();
  return withBusy(() => createFolder(event));
};
withBusy(init);
