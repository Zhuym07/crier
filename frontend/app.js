// 传令 · 界面逻辑。所有后端调用都经过 window.go.main.App（Wails 绑定）。
const api = window.go.main.App;

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

// 后端 History 的状态。
const STATES = {
  pending: { label: '待确认', tone: 'warn' },
  submitted: { label: '已提交', tone: 'ok' },
  printed: { label: '驱动报告已打印', tone: 'ok' },
  uncertain: { label: '结果未知', tone: 'bad' },
  acknowledged: { label: '已确认', tone: 'muted' },
};

const LAST_STEP = 4;

let cfg = emptyConfig();
let printers = [];
let entries = [];
let plugins = [];
let pluginEditor = null; // null，或 { id, executable }：编辑面板当前对应的插件
let step = 0;
let pollTimer = 0;
let currentVersion = '';
let toastTimer = 0;

const PLUGINS_EMPTY = '还没有外部插件。安装包自带的邮箱与 Telegram 通道已在上方。';

function emptyConfig() {
  return {
    printer: '',
    email: { address: '', username: '', secret: '', mailbox: '', enabled: false },
    ai: { url: '', key: '', model: '', enabled: false },
    telegram: { enabled: false, token: '', proxy_url: '', adminIDs: '', logChannel: '', todoChannel: '' },
    setupDone: false,
    autoStart: false,
  };
}

/* ---------- 配置 ↔ 表单 ---------- */

function getPath(obj, path) {
  return path.split('.').reduce((cur, key) => (cur == null ? undefined : cur[key]), obj);
}

function setPath(obj, path, value) {
  const keys = path.split('.');
  const last = keys.pop();
  let cur = obj;
  for (const key of keys) {
    if (cur[key] === null || typeof cur[key] !== 'object') cur[key] = {};
    cur = cur[key];
  }
  cur[last] = value;
}

// 两块表单（引导 / 设置）共用 data-field 路径，避免同一份字段写两遍。
function fillForm(root, data) {
  for (const el of $$('[data-field]', root)) {
    const value = getPath(data, el.dataset.field);
    if (el.dataset.kind === 'bool') {
      el.classList.toggle('on', !!value);
      el.setAttribute('aria-checked', value ? 'true' : 'false');
    } else {
      el.value = value == null ? '' : String(value);
    }
  }
}

function readForm(root, base) {
  const out = JSON.parse(JSON.stringify(base));
  for (const el of $$('[data-field]', root)) {
    if (el.dataset.kind === 'bool') {
      setPath(out, el.dataset.field, el.classList.contains('on'));
    } else {
      // 密码/密钥保留原样，其余去掉首尾空格。
      const raw = el.type === 'password' ? el.value : el.value.trim();
      setPath(out, el.dataset.field, raw);
    }
  }
  return out;
}

function syncForms() {
  fillForm($('#settings-form'), cfg);
  fillForm($('#plugins-form'), cfg);
  fillForm($('#oobe'), cfg);
  renderPrinterSelects();
}

/* ---------- 视图 ---------- */

function showView(name) {
  for (const item of $$('.nav-item')) item.classList.toggle('is-active', item.dataset.view === name);
  for (const view of $$('.view')) view.classList.toggle('is-active', view.id === `view-${name}`);
  if (name === 'dashboard') loadHistory();
  if (name === 'plugins') loadPlugins();
}

/* ---------- 打印机列表 ---------- */

async function loadPrinters() {
  try {
    printers = (await api.ListPrinters()) || [];
  } catch (err) {
    printers = [];
    toast(errText(err), true);
  }
  renderPrinterSelects();
}

function renderPrinterSelects() {
  const configured = cfg.printer || '';
  for (const select of $$('select[data-field="printer"]')) {
    const current = select.value || configured;
    const names = printers.slice();
    // 已保存但当前不在列表里的打印机（比如没开机）仍要保留，不能悄悄丢掉。
    if (current && !names.includes(current)) names.unshift(current);

    select.replaceChildren(new Option(names.length ? '请选择打印机' : '未检测到打印机', ''));
    for (const name of names) select.append(new Option(name, name));
    select.value = names.includes(current) ? current : '';
  }
}

/* ---------- 渲染 ---------- */

function renderDashboard() {
  const email = cfg.email || {};
  const ai = cfg.ai || {};

  $('#dash-printer').textContent = cfg.printer || '未选择';
  $('#dash-email').textContent = email.enabled ? email.address || '已启用' : '未启用';
  $('#dash-mailbox').textContent = email.enabled ? `文件夹 ${email.mailbox || 'INBOX'}` : '';
  $('#dash-ai').textContent = ai.enabled ? ai.model || '已启用' : '未启用';
  $('#dash-model').textContent = ai.enabled ? ai.url || '' : '';

  const chip = $('#chip-status');
  chip.textContent = cfg.setupDone ? '运行中' : '待配置';
  chip.classList.toggle('on', !!cfg.setupDone);

  $('#dash-sub').textContent = cfg.setupDone
    ? '新邮件到达后会自动打印，并在这里留下凭条。'
    : '还没完成配置，右上角"设置"里可以继续填写。';

  $('#foot-status').textContent = cfg.printer || '未选择打印机';
  $('#foot-dot').classList.toggle('on', !!cfg.printer);
}

function formatTime(value) {
  if (value == null || value === '') return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const pad = (n) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function renderEntries() {
  const list = $('#entries');
  list.replaceChildren();
  $('#entries-empty').hidden = entries.length > 0;

  for (const entry of entries) {
    const row = document.createElement('li');
    row.className = 'entry';

    const main = document.createElement('div');
    main.className = 'entry-main';
    const title = document.createElement('div');
    title.className = 'entry-title';
    title.textContent = entry.title || '（无标题）';

    const meta = document.createElement('div');
    meta.className = 'entry-meta';
    const time = document.createElement('span');
    time.textContent = formatTime(entry.time);
    meta.append(time);
    if (entry.id) {
      const id = document.createElement('span');
      id.className = 'id';
      id.textContent = entry.id;
      meta.append(id);
    }
    main.append(title, meta);
    row.append(main);

    const state = STATES[entry.state] || { label: entry.state || '未知', tone: 'muted' };
    const badge = document.createElement('span');
    badge.className = `badge tone-${state.tone}`;
    badge.textContent = state.label;
    row.append(badge);

    // 已提交、驱动回执已打印或结果未知的凭条都需要人来确认。
    if (entry.state === 'submitted' || entry.state === 'printed' || entry.state === 'uncertain') {
      const ack = document.createElement('button');
      ack.type = 'button';
      ack.className = 'btn btn-quiet btn-sm';
      ack.textContent = '确认';
      ack.addEventListener('click', () =>
        withBusy(ack, async () => {
          await api.Acknowledge(entry.id);
          await loadHistory(false);
          toast('已确认');
        }),
      );
      row.append(ack);
    }

    list.append(row);
  }
}

/* ---------- 引导流程 ---------- */

function openOobe() {
  step = 0;
  syncForms();
  renderStep();
  $('#oobe').hidden = false;
}

function closeOobe() {
  $('#oobe').hidden = true;
}

function renderStep() {
  for (const item of $$('#steps li')) {
    const index = Number(item.dataset.step);
    item.classList.toggle('is-active', index === step);
    item.classList.toggle('is-done', index < step);
  }
  for (const pane of $$('.pane')) pane.classList.toggle('is-active', Number(pane.dataset.pane) === step);

  $('#o-prev').disabled = step === 0;
  $('#o-skip').hidden = step === LAST_STEP;
  $('#o-next').textContent = step === LAST_STEP ? '完成' : '下一步';
  if (step === LAST_STEP) renderSummary();
}

function renderSummary() {
  const draft = readForm($('#oobe'), cfg);
  const rows = [
    ['打印机', draft.printer || '未选择'],
    ['邮箱', draft.email.enabled ? draft.email.address || '已启用' : '未启用'],
    ['AI 整理', draft.ai.enabled ? draft.ai.model || '已启用' : '未启用'],
    ['开机自启', draft.autoStart ? '开启' : '关闭'],
  ];
  const list = $('#o-summary');
  list.replaceChildren();
  for (const [key, value] of rows) {
    const row = document.createElement('div');
    const dt = document.createElement('dt');
    dt.textContent = key;
    const dd = document.createElement('dd');
    dd.textContent = value;
    row.append(dt, dd);
    list.append(row);
  }
}

/* ---------- 动作 ---------- */

async function withBusy(button, work) {
  const wasDisabled = button.disabled;
  button.disabled = true;
  button.classList.add('is-busy');
  try {
    await work();
  } catch (err) {
    toast(errText(err), true);
  } finally {
    button.disabled = wasDisabled;
    button.classList.remove('is-busy');
  }
}

function errText(err) {
  const text = err && err.message ? err.message : String(err ?? '');
  return text.replace(/^Error:\s*/, '') || '操作失败';
}

async function loadHistory(loud = true) {
  try {
    const [history, error] = await Promise.all([api.History(), api.GetLastError()]);
    entries = history || [];
    renderDashboard();
    if (error) {
      const chip = $('#chip-status');
      chip.textContent = '检查失败';
      chip.classList.remove('on');
      $('#dash-sub').textContent = error;
    }
  } catch (err) {
    if (loud) toast(errText(err), true);
    return;
  }
  renderEntries();
}

// 设置页与插件页各承担一部分字段，保存/测试时合并读取，避免互相覆盖。
function configDraft() {
  if (!$('#oobe').hidden) return readForm($('#oobe'), cfg);
  const draft = readForm($('#settings-form'), cfg);
  return readForm($('#plugins-form'), draft);
}

async function saveConfig(button, extra) {
  await withBusy(button, async () => {
    const draft = Object.assign(configDraft(), extra || {});
    await api.SaveConfig(draft);
    cfg = await api.GetConfig();
    syncForms();
    renderDashboard();
    toast('已保存');
  });
}

async function testPrint(button) {
  const draft = configDraft();
  if (!draft.printer) {
    toast('先选一台打印机', true);
    return;
  }
  await withBusy(button, async () => {
    await api.TestPrint(draft.printer);
    toast('测试页已发送');
  });
}

async function preview(button) {
  await withBusy(button, async () => {
    const url = await api.PreviewDataURL();
    const box = $(`#${button.dataset.previewBox}`);
    $('img', box).src = url;
    box.hidden = false;
  });
}

async function testEmail(button) {
  const draft = configDraft();
  await withBusy(button, async () => {
    await api.TestEmail(draft.email);
    toast('邮箱连接正常');
  });
}

async function testAI(button) {
  const draft = configDraft();
  await withBusy(button, async () => {
    await api.TestAI(draft.ai);
    toast('AI 接口正常');
  });
}

async function checkNow(button) {
  await withBusy(button, async () => {
    await api.CheckNow();
    await loadHistory();
    toast('检查完成');
  });
}

async function oobeNext() {
  if (step < LAST_STEP) {
    step += 1;
    renderStep();
    return;
  }
  await saveConfig($('#o-next'), { setupDone: true });
  if (cfg.setupDone) {
    closeOobe();
    showView('dashboard');
  }
}

async function rerunSetup(button) {
  await saveConfig(button, { setupDone: false });
  if (!cfg.setupDone) openOobe();
}

/* ---------- 外部插件 ---------- */

function externalPlugins() {
  return plugins.filter((item) => !item.builtin);
}

async function loadPlugins() {
  try {
    plugins = (await api.ListPlugins()) || [];
    renderPlugins();
  } catch (err) {
    plugins = [];
    renderPlugins();
    $('#plugins-chip').textContent = '插件读取失败';
    $('#plugins-empty').hidden = false;
    $('#plugins-empty').textContent = errText(err);
  }
}

function renderPlugins() {
  const list = $('#plugin-list');
  const external = externalPlugins();
  list.replaceChildren();
  $('#plugins-empty').hidden = external.length > 0;
  $('#plugins-empty').textContent = PLUGINS_EMPTY;
  $('#plugins-chip').textContent = external.length ? `${external.length} 个外部插件` : '无外部插件';
  for (const item of external) list.append(pluginRow(item));
}

function badge(text, tone) {
  const el = document.createElement('span');
  el.className = `badge tone-${tone}`;
  el.textContent = text;
  return el;
}

function pluginRow(item) {
  const row = document.createElement('li');
  row.className = 'plugin';

  const main = document.createElement('div');
  main.className = 'plugin-main';

  const title = document.createElement('div');
  title.className = 'plugin-title';
  const id = document.createElement('span');
  id.className = 'plugin-id';
  id.textContent = item.id;
  title.append(id);
  title.append(badge(item.enabled ? '已启用' : '已停用', item.enabled ? 'ok' : 'muted'));
  if (item.allowAI) title.append(badge('AI', 'warn'));
  if (item.allowRaw) title.append(badge('原始打印', 'bad'));
  if (!item.exists) title.append(badge('文件缺失', 'bad'));

  const path = document.createElement('div');
  path.className = 'plugin-path';
  path.textContent = item.executable || '';
  path.title = item.executable || '';

  const meta = document.createElement('div');
  meta.className = 'plugin-meta';
  const count = settingsCount(item.settings);
  meta.append(document.createTextNode(count ? `设置 ${count} 项` : '无额外设置'));
  meta.append(document.createTextNode('· 本地程序'));
  main.append(title, path, meta);

  const actions = document.createElement('div');
  actions.className = 'plugin-actions';

  const config = document.createElement('button');
  config.type = 'button';
  config.className = 'btn btn-quiet btn-sm';
  config.textContent = '配置';
  config.addEventListener('click', () => openPluginEditor(item));

  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'btn btn-quiet btn-sm';
  toggle.textContent = item.enabled ? '停用' : '启用';
  toggle.addEventListener('click', (e) => togglePlugin(e.currentTarget, item, !item.enabled));

  const remove = document.createElement('button');
  remove.type = 'button';
  remove.className = 'btn btn-quiet btn-sm';
  remove.textContent = '移除';
  remove.addEventListener('click', (e) => removePlugin(e.currentTarget, item));

  actions.append(config, toggle, remove);
  row.append(main, actions);
  return row;
}

function settingsCount(settings) {
  return settings && typeof settings === 'object' ? Object.keys(settings).length : 0;
}

function defaultPluginId(path) {
  const base = String(path || '').split(/[\\/]/).pop().replace(/\.exe$/i, '');
  const id = base.toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^[^a-z0-9]+/, '').replace(/[-.]+$/, '');
  return (id.slice(0, 48).replace(/[-.]+$/, '') || 'plugin');
}

function prettySettings(settings) {
  if (!settings || typeof settings !== 'object') return '{}';
  try {
    return JSON.stringify(settings, null, 2);
  } catch {
    return '{}';
  }
}

function pluginIdTaken(id, exceptId) {
  return externalPlugins().some((item) => item.id === id && item.id !== exceptId);
}

function renderEditorHint(existing) {
  const hint = $('#plugin-editor-hint');
  const id = $('#plugin-id').value.trim();
  if (existing) {
    hint.textContent = '可以修改设置与允许的能力；更换程序请点「选择程序」。保存后生效。';
  } else if (pluginIdTaken(id)) {
    hint.textContent = '已有同名插件，保存会替换它的程序路径、设置与允许项，并保留当前启用状态。';
  } else {
    hint.textContent = '导入后默认停用，确认程序无误后在列表里点「启用」才会开始收取。';
  }
}

function openPluginEditor(item, executable) {
  const existing = !!item;
  pluginEditor = {
    id: existing ? item.id : '',
    executable: executable || (existing ? item.executable : ''),
  };
  $('#plugin-editor-title').textContent = existing ? `配置 ${item.id}` : '导入本地插件';
  const idInput = $('#plugin-id');
  idInput.disabled = existing;
  idInput.value = existing ? item.id : defaultPluginId(pluginEditor.executable);
  $('#plugin-exe').value = pluginEditor.executable;
  $('#plugin-settings').value = prettySettings(existing ? item.settings : null);
  $('#plugin-allow-ai').checked = !!(existing && item.allowAI);
  $('#plugin-allow-raw').checked = !!(existing && item.allowRaw);
  $('#plugin-editor').hidden = false;
  renderEditorHint(existing);
  idInput.focus();
}

function closePluginEditor() {
  pluginEditor = null;
  $('#plugin-editor').hidden = true;
}

async function pickPluginExecutable() {
  try {
    return (await api.PickPluginExecutable()) || '';
  } catch (err) {
    toast(errText(err), true);
    return '';
  }
}

async function importPlugin() {
  const path = await pickPluginExecutable();
  if (path) openPluginEditor(null, path);
}

async function repickPluginExecutable() {
  const path = await pickPluginExecutable();
  if (!path) return;
  pluginEditor.executable = path;
  $('#plugin-exe').value = path;
  if (!pluginEditor.id) {
    $('#plugin-id').value = defaultPluginId(path);
    renderEditorHint(false);
  }
}

async function savePluginDraft(button) {
  const existing = !!(pluginEditor && pluginEditor.id);
  const id = $('#plugin-id').value.trim();
  const executable = $('#plugin-exe').value.trim();
  if (!id) {
    toast('请填写插件 ID', true);
    return;
  }
  if (!executable) {
    toast('请先选择插件程序', true);
    return;
  }
  const replacing = !existing && pluginIdTaken(id);
  let settings = {};
  const raw = $('#plugin-settings').value.trim();
  if (raw) {
    try {
      settings = JSON.parse(raw);
    } catch {
      toast('设置必须是合法的 JSON 对象', true);
      return;
    }
    if (settings === null || typeof settings !== 'object' || Array.isArray(settings)) {
      toast('设置必须是 JSON 对象', true);
      return;
    }
  }
  await withBusy(button, async () => {
    await api.SavePlugin({
      id,
      executable,
      settings,
      allowAI: $('#plugin-allow-ai').checked,
      allowRaw: $('#plugin-allow-raw').checked,
    });
    closePluginEditor();
    await loadPlugins();
    toast(existing || replacing ? '插件已更新' : '已导入，默认停用。确认后点「启用」开始收取。');
  });
}

async function togglePlugin(button, item, enabled) {
  await withBusy(button, async () => {
    await api.SetPluginEnabled(item.id, enabled);
    await loadPlugins();
    toast(enabled ? '插件已启用' : '插件已停用');
  });
}

async function removePlugin(button, item) {
  if (!window.confirm(`移除外部插件「${item.id}」？程序文件不会被删除。`)) return;
  await withBusy(button, async () => {
    await api.RemovePlugin(item.id);
    if (pluginEditor && pluginEditor.id === item.id) closePluginEditor();
    await loadPlugins();
    toast('插件已移除');
  });
}

async function openPluginReleasePage(button) {
  await withBusy(button, async () => {
    await api.OpenPluginReleasePage();
    toast('已在浏览器中打开发布页');
  });
}

/* ---------- toast ---------- */

function toast(message, isError = false) {
  const el = $('#toast');
  el.textContent = message;
  el.classList.toggle('is-error', !!isError);
  el.hidden = false;
  requestAnimationFrame(() => el.classList.add('show'));
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    el.classList.remove('show');
    setTimeout(() => {
      el.hidden = true;
    }, 220);
  }, 2600);
}

let updateInfo = null; // 最近一次检查更新的结果；null 表示还没检查过
let updatePath = ''; // 已下载并校验通过的安装包路径

function renderVersion() {
  const chip = $('#version-current');
  const status = $('#version-status');
  const install = $('#s-download-update');
  chip.textContent = `v${updateInfo ? updateInfo.current : currentVersion || '—'}`;
  install.hidden = true;
  if (!updateInfo) {
    status.textContent = '检查更新只会访问 GitHub 的公开发布页，不会自动安装。';
    return;
  }
  if (updateInfo.error) {
    status.textContent = `检查失败：${updateInfo.error}`;
    return;
  }
  if (!updateInfo.available) {
    status.textContent = `已是最新版本（v${updateInfo.current}）。`;
    return;
  }
  status.textContent = `发现新版本 v${updateInfo.latest}，当前 v${updateInfo.current}。`;
  install.hidden = false;
  install.textContent = updatePath ? '安装并重启' : '下载并安装';
}

async function checkUpdate(button) {
  await withBusy(button, async () => {
    updatePath = '';
    updateInfo = await api.CheckUpdate();
    renderVersion();
    if (updateInfo.error) toast(`检查失败：${updateInfo.error}`, true);
    else if (updateInfo.available) toast(`发现新版本 v${updateInfo.latest}`);
    else toast('已是最新版本');
  });
}

// 下载后先校验 SHA-256，再运行安装程序覆盖安装；升级范围由用户点按决定。
async function installUpdate(button) {
  await withBusy(button, async () => {
    if (!updatePath) {
      updatePath = await api.DownloadUpdate();
      renderVersion();
      toast('安装包已下载并通过校验，再次点击即可安装');
      return;
    }
    await api.InstallUpdate(updatePath);
  });
}

async function openReleasesPage(button) {
  await withBusy(button, () => api.OpenReleasesPage());
}

/* ---------- 事件与启动 ---------- */

function bindEvents() {
  const guide = $('#gmail-guide');
  for (const container of $$('[data-gmail-guide]')) {
    container.append(guide.content.cloneNode(true));
    $('[data-gmail-server]', container).addEventListener('click', () => {
      const address = $('[data-field="email.address"]', container.parentElement);
      address.value = 'imap.gmail.com:993';
      address.focus();
    });
    $('[data-gmail-help]', container).addEventListener('click', () => {
      window.runtime.BrowserOpenURL('https://support.google.com/accounts/answer/185833?hl=zh-Hans');
    });
  }

  // 托盘右键「设置」通过事件让界面直接跳到设置页。
  window.runtime.EventsOn('crier:open-settings', () => {
    closeOobe();
    showView('settings');
  });

  for (const item of $$('.nav-item')) {
    item.addEventListener('click', () => showView(item.dataset.view));
  }

  for (const toggle of $$('.switch')) {
    toggle.addEventListener('click', () => {
      const on = !toggle.classList.contains('on');
      toggle.classList.toggle('on', on);
      toggle.setAttribute('aria-checked', on ? 'true' : 'false');
      if (Number($('#oobe').hidden ? -1 : step) === LAST_STEP) renderSummary();
    });
  }

  $('#btn-check').addEventListener('click', (e) => checkNow(e.currentTarget));
  $('#btn-history').addEventListener('click', () => loadHistory());
  $('#btn-dash-print').addEventListener('click', (e) => testPrint(e.currentTarget));
  $('#btn-save').addEventListener('click', (e) => saveConfig(e.currentTarget));
  $('#btn-rerun').addEventListener('click', (e) => rerunSetup(e.currentTarget));

  $('#s-open-releases').addEventListener('click', (e) => openReleasesPage(e.currentTarget));
  $('#s-check-update').addEventListener('click', (e) => checkUpdate(e.currentTarget));
  $('#s-download-update').addEventListener('click', (e) => installUpdate(e.currentTarget));

  $('#btn-plugin-save').addEventListener('click', (e) => saveConfig(e.currentTarget));
  $('#btn-plugin-import').addEventListener('click', () => importPlugin());
  $('#btn-plugin-release').addEventListener('click', (e) => openPluginReleasePage(e.currentTarget));
  $('#plugin-exe-pick').addEventListener('click', () => repickPluginExecutable());
  $('#plugin-editor-cancel').addEventListener('click', () => closePluginEditor());
  $('#plugin-editor-save').addEventListener('click', (e) => savePluginDraft(e.currentTarget));
  $('#plugin-id').addEventListener('input', () => renderEditorHint(!!(pluginEditor && pluginEditor.id)));

  // 两个设置页共用 data-field 路径；回车不应触发表单导航。
  for (const form of $$('form.settings')) form.addEventListener('submit', (e) => e.preventDefault());

  for (const id of ['#s-printers-refresh', '#o-printers-refresh']) {
    $(id).addEventListener('click', (e) =>
      withBusy(e.currentTarget, async () => {
        await loadPrinters();
        syncForms();
        toast(printers.length ? `找到 ${printers.length} 台打印机` : '没有找到打印机');
      }),
    );
  }

  $('#s-test-print').addEventListener('click', (e) => testPrint(e.currentTarget));
  $('#o-test-print').addEventListener('click', (e) => testPrint(e.currentTarget));
  $('#s-test-email').addEventListener('click', (e) => testEmail(e.currentTarget));
  $('#o-test-email').addEventListener('click', (e) => testEmail(e.currentTarget));
  $('#s-test-ai').addEventListener('click', (e) => testAI(e.currentTarget));
  $('#o-test-ai').addEventListener('click', (e) => testAI(e.currentTarget));

  $('#s-preview').dataset.previewBox = 's-preview-box';
  $('#o-preview').dataset.previewBox = 'o-preview-box';
  $('#s-preview').addEventListener('click', (e) => preview(e.currentTarget));
  $('#o-preview').addEventListener('click', (e) => preview(e.currentTarget));

  $('#o-prev').addEventListener('click', () => {
    if (step > 0) {
      step -= 1;
      renderStep();
    }
  });
  $('#o-next').addEventListener('click', () => oobeNext());
  $('#o-skip').addEventListener('click', () => closeOobe());

  // 后台收信是异步的，首页停留时定时刷新凭条列表。
  pollTimer = setInterval(() => {
    if (document.visibilityState !== 'visible') return;
    if (!$('#view-dashboard').classList.contains('is-active')) return;
    loadHistory(false);
  }, 5000);
}

async function init() {
  try {
    cfg = await api.GetConfig();
  } catch (err) {
    toast(errText(err), true);
  }
  await loadPrinters();
  syncForms();
  renderDashboard();
  bindEvents();
  await loadHistory();
  await loadPlugins();
  try {
    currentVersion = await api.CurrentVersion();
  } catch {
    // 版本号只用于展示，读不到时显示占位符。
  }
  renderVersion();
  // 启动后在后台查一次更新，失败不影响主流程，也不弹窗打扰。
  api
    .CheckUpdate()
    .then((info) => {
      if (!info || !info.available) return;
      updateInfo = info;
      renderVersion();
      toast(`发现新版本 v${info.latest}，可在设置页更新`);
    })
    .catch(() => {});
  try {
    const url = await api.PluginReleaseURL();
    if (url) $('#plugin-release-url').textContent = url;
  } catch {
    // 地址只是展示信息，读不到就保留 HTML 里的默认地址。
  }
  if (cfg.setupDone) {
    showView('dashboard');
  } else {
    showView('dashboard');
    openOobe();
  }
}

init();
