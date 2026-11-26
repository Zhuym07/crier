// 传令 · 界面逻辑。所有后端调用都经过 window.go.main.App（Wails 绑定）。
const api = window.go.main.App;

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

// 后端 History 的四种状态。
const STATES = {
  pending: { label: '待确认', tone: 'warn' },
  submitted: { label: '已提交', tone: 'ok' },
  uncertain: { label: '结果未知', tone: 'bad' },
  acknowledged: { label: '已确认', tone: 'muted' },
};

const LAST_STEP = 4;

let cfg = emptyConfig();
let printers = [];
let entries = [];
let step = 0;
let pollTimer = 0;
let toastTimer = 0;

function emptyConfig() {
  return {
    printer: '',
    email: { address: '', username: '', secret: '', mailbox: '', enabled: false },
    ai: { url: '', key: '', model: '', enabled: false },
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
  fillForm($('#oobe'), cfg);
  renderPrinterSelects();
}

/* ---------- 视图 ---------- */

function showView(name) {
  for (const item of $$('.nav-item')) item.classList.toggle('is-active', item.dataset.view === name);
  for (const view of $$('.view')) view.classList.toggle('is-active', view.id === `view-${name}`);
  if (name === 'dashboard') loadHistory();
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

    // 只有已提交或结果未知的凭条才谈得上"确认"；待确认的还没打印。
    if (entry.state === 'submitted' || entry.state === 'uncertain') {
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

function currentRoot() {
  return $('#oobe').hidden ? $('#settings-form') : $('#oobe');
}

async function saveConfig(button, extra) {
  await withBusy(button, async () => {
    const draft = Object.assign(readForm(currentRoot(), cfg), extra || {});
    await api.SaveConfig(draft);
    cfg = await api.GetConfig();
    syncForms();
    renderDashboard();
    toast('已保存');
  });
}

async function testPrint(button) {
  const draft = readForm(currentRoot(), cfg);
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
    const box = button.dataset.previewBox;
    $(`#${box}-img`).src = url;
    $(`#${box}`).hidden = false;
  });
}

async function testEmail(button) {
  const draft = readForm(currentRoot(), cfg);
  await withBusy(button, async () => {
    await api.TestEmail(draft.email);
    toast('邮箱连接正常');
  });
}

async function testAI(button) {
  const draft = readForm(currentRoot(), cfg);
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

/* ---------- 事件与启动 ---------- */

function bindEvents() {
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
  if (cfg.setupDone) {
    showView('dashboard');
  } else {
    showView('dashboard');
    openOobe();
  }
}

init();
