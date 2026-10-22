'use strict';
const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const el = (tag, cls = '', text = '') => { const n = document.createElement(tag); n.className = cls; n.textContent = text; return n; };
const paths = {
  dashboard: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="11" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="18" width="7" height="3" rx="1"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18Z"/>',
  apps: '<rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><path d="M14 17.5h7m-3.5-3.5v7"/>',
  box: '<path d="m12 3 9 5v9l-9 5-9-5V8Z"/><path d="m3 8 9 5 9-5M12 13v9M8 5l9 5"/>',
  layers: '<path d="m12 3 10 5-10 5L2 8Zm-10 9 10 5 10-5M2 16l10 5 10-5"/>',
  folder: '<path d="M3 7V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Zm0 2h18"/>',
  database: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 4 18 4 18 0V5M3 12c0 4 18 4 18 0"/>',
  terminal: '<rect x="2" y="4" width="20" height="16" rx="3"/><path d="m6 9 3 3-3 3m7 0h5"/>',
  activity: '<path d="M2 12h4l3-8 6 16 3-8h4"/>',
  shield: '<path d="m12 2 9 4v6c0 6-9 10-9 10S3 18 3 12V6Z"/><path d="m8 12 3 3 5-6"/>',
  lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4m-4 5v2"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 1v2m0 18v2M1 12h2m18 0h2M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2"/>',
  logout: '<path d="M10 4H4v16h6m4-12 4 4-4 4m-6-4h12"/>',
  menu: '<path d="M3 5h18M3 12h18M3 19h18"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10v.5"/>',
  plus: '<path d="M12 4v16M4 12h16"/>',
  refresh: '<path d="M21 4v6h-6M3 20v-6h6M4 8a8 8 0 0 1 13-4l4 6M3 14l4 6a8 8 0 0 0 13-4"/>',
  arrow: '<path d="M5 12h14m-6-6 6 6-6 6"/>',
  cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4"/>',
  memory: '<rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10v4m4-4v4m4-4v4m4-4v4M6 18v3m6-3v3m6-3v3"/>',
  bell: '<path d="M6 9a6 6 0 0 1 12 0c0 6 2.5 7.5 2.5 7.5h-17S6 15 6 9Z"/><path d="M10 20a2 2 0 0 0 4 0"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  disk: '<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="12" cy="10" r="4"/><path d="m12 10 6 6M6 18h1"/>',
  network: '<path d="M7 3v18m-4-4 4 4 4-4M17 21V3m-4 4 4-4 4 4"/>',
  file: '<path d="M5 2h9l5 5v15H5Zm9 0v6h5M8 12h8m-8 4h8"/>',
  trash: '<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  play: '<path d="m7 3 14 9-14 9Z"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 0 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 0 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 0 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 0 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
  monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8m-4-4v4"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z"/>',
  cert: '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M7 9h10M7 13h5"/><circle cx="16" cy="17" r="2.5"/><path d="m14.8 19.2-.8 2.8 2-1 2 1-.8-2.8"/>',
  image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/>',
};
function icon(name) { const span = el('span', 'icon'); span.innerHTML = `<svg class="icon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.info}</svg>`; return span; }
$$('[data-icon]').forEach(n => n.replaceChildren(icon(n.dataset.icon)));
const state = { csrf: '', user: '', demo: false, totp: false, route: 'overview', epoch: 0, filePath: '/', catalog: [], apps: [], sites: [], timers: [], socket: null };
// route: [title, description, icon, group]
const routes = {
  overview: ['概览', '', 'dashboard', 'overview'],
  apps: ['全部应用', '一键部署常用应用：端口只绑定本机，安装后可在“网站”中绑定域名。', 'apps', 'apps'],
  installed: ['已安装', '通过 JunPanel 安装的应用。卸载会保留数据卷与凭据，可以恢复部署。', 'apps', 'apps'],
  sites: ['网站', '静态网站与反向代理，交给系统 Caddy 并自动签发 HTTPS。', 'globe', 'sites'],
  certs: ['证书', 'Caddy 自动签发和续期的 HTTPS 证书。', 'cert', 'sites'],
  databases: ['数据库', '管理应用商店安装的数据库，执行查询、建库与备份。', 'database', 'databases'],
  containers: ['容器', '系统 Docker 中的全部容器，包括安装面板之前创建的容器。', 'box', 'containers'],
  projects: ['编排', '编写 docker-compose 文件并部署，也可以整组管理已有的 Compose 项目。', 'layers', 'containers'],
  images: ['镜像', '本机 Docker 镜像。仍被容器使用的镜像不能删除。', 'image', 'containers'],
  networks: ['网络', 'Docker 网络。仍有容器（包括已停止的）连接时不能删除。', 'network', 'containers'],
  volumes: ['卷', 'Docker 数据卷，保存容器的持久数据。删除后数据无法恢复。', 'disk', 'containers'],
  terminal: ['终端', '打开运行中容器的 /bin/sh，处理必要的维护。', 'terminal', 'containers'],
  files: ['文件', '管理 /srv/junpanel 内的文件，删除的文件先进入回收站。', 'folder', 'files'],
  schedules: ['计划任务', '按时间表自动备份或重启应用，只保留最近几份备份。', 'clock', 'schedules'],
  backups: ['备份', '为网站文件、应用数据和数据库留一份副本，需要时恢复。', 'disk', 'backups'],
  alerts: ['告警', '磁盘、证书、容器和服务出问题时提醒你，并推送到 Webhook。', 'bell', 'alerts'],
  jobs: ['任务', '每一次部署、备份和容器操作的执行记录。', 'activity', 'logs'],
  audit: ['操作日志', '谁在什么时间、从哪里做了什么修改，以及结果。', 'file', 'logs'],
  logins: ['登录日志', '登录、退出和再次验证密码失败的记录。', 'lock', 'logs'],
  security: ['安全', '管理员密码、两步验证与面板的权限边界。', 'shield', 'settings'],
  access: ['公网入口', '用域名或服务器 IP 通过 HTTPS 访问面板。', 'globe', 'settings'],
  upgrade: ['版本升级', '检查新版本，校验后在面板内一键升级。', 'refresh', 'settings'],
};
// Sidebar menu, in 1Panel order. [group, label, icon, badge id]
const menu = [
  ['overview', '概览', 'dashboard'],
  ['apps', '应用商店', 'apps'],
  ['sites', '网站', 'globe', 'site-badge'],
  ['databases', '数据库', 'database'],
  ['containers', '容器', 'box'],
  ['files', '文件', 'folder'],
  ['schedules', '计划任务', 'clock'],
  ['backups', '备份', 'disk'],
  ['alerts', '告警', 'bell', 'alert-badge'],
  ['logs', '日志', 'activity', 'job-badge'],
  ['settings', '面板设置', 'settings', 'upgrade-badge'],
];
const groupRoutes = g => Object.keys(routes).filter(r => routes[r][3] === g);
const groupLabel = g => (menu.find(m => m[0] === g) || [])[1] || '';
(function buildNav() {
  const nav = $('#nav');
  menu.forEach(([g, label, symbol, badge]) => {
    if (g === 'logs') nav.append(el('div', 'nav-divider'));
    const b = el('button', 'nav-link'); b.type = 'button'; b.dataset.group = g; b.dataset.route = groupRoutes(g)[0];
    b.append(icon(symbol), el('span', '', label));
    if (badge) { const n = el('span', `nav-badge${badge === 'site-badge' ? ' count' : ''}`); n.id = badge; n.hidden = true; b.append(n); }
    nav.append(b);
  });
})();
const view = $('#view');
const bytes = n => { if (n == null || !Number.isFinite(Number(n))) return '—'; let i = 0; n = Number(n); while (n >= 1024 && i < 4) { n /= 1024; i++; } return `${n.toFixed(i ? (n < 10 ? 1 : 0) : 0)} ${['B','KB','MB','GB','TB'][i]}`; };
const percent = n => n == null ? '采样中' : `${Number(n).toFixed(1)}%`;
const date = s => s ? new Date(s).toLocaleString('zh-CN', { hour12: false }) : '—';
const uptime = s => s ? `${Math.floor(s / 86400)} 天 ${Math.floor(s % 86400 / 3600)} 小时` : '—';
function toast(message, error = false) { const n = el('div', `toast${error ? ' error' : ''}`, message); $('#toasts').append(n); setTimeout(() => n.remove(), 5500); }
const destructive = /^(删除|卸载|移除|永久删除|清空回收站)$/;
function button(text, fn, kind = 'secondary', symbol) { const b = el('button', `button ${kind}`, text); b.type = 'button'; if (kind.includes('quiet') && destructive.test(text)) b.classList.add('danger-text'); if (symbol) b.prepend(icon(symbol)); b.addEventListener('click', async () => { try { await fn(b); } catch(e) { toast(e.message, true); } }); return b; }
function tag(text, cls = '') { return el('span', `tag ${cls}`, text); }
function empty(message, sub = '') { const n = el('div', 'empty'); n.append(icon('layers'), el('h3', '', message), el('p', 'muted', sub)); return n; }
function note(message, warning = false) { const n = el('div', warning ? 'info warning' : 'info'); n.append(icon('info'), el('span', '', message)); return n; }
function line(name, value) { const n = el('div', 'details-row'); n.append(el('span', 'muted', name), el('span', '', value)); return n; }
function panel(title, right) { const n = el('section', 'panel'), h = el('div', 'panel-header'); h.append(el('h2', '', title)); if (right) h.append(right); n.append(h); return n; }
function field(label, value = '', options = {}) { const n = el('div', 'field'), id = `f-${crypto.randomUUID()}`, input = el(options.area ? 'textarea' : options.select ? 'select' : 'input'); input.id = id; input.value = value; if (!options.area && !options.select) input.type = options.type || 'text'; if (options.required !== false) input.required = true; if (options.placeholder) input.placeholder = options.placeholder; if (options.max) input.maxLength = options.max; if (options.area) input.rows = options.rows || 5; if (options.select) options.select.forEach(([v,t]) => { const o = el('option','',t); o.value = v; input.append(o); }); input.value = value; const l = el('label','',label); l.htmlFor = id; n.append(l); if (options.type === 'password') { const wrap = el('div','pw-wrap'), eye = el('button','pw-toggle','显示'); eye.type = 'button'; eye.setAttribute('aria-label','显示或隐藏密码'); eye.setAttribute('aria-pressed','false'); eye.onclick = () => { const shown = input.type === 'password'; input.type = shown ? 'text' : 'password'; eye.textContent = shown ? '隐藏' : '显示'; eye.setAttribute('aria-pressed', String(shown)); input.focus(); }; wrap.append(input, eye); n.append(wrap); } else n.append(input); if (options.help) n.append(el('p','help',options.help)); return { n, input, val: () => input.value.trim() }; }
// The version this page was served with. After the panel is upgraded, every response carries
// the new version; reload once so an open tab does not keep running the old interface.
const pageVersion = $('meta[name=junpanel-version]')?.content || '';
function checkVersion(served) {
  if (!served || !pageVersion || served === pageVersion) return;
  let tried = null; try { tried = sessionStorage.getItem('junpanel-reloaded-for'); } catch {}
  if (tried === served) return;
  try { sessionStorage.setItem('junpanel-reloaded-for', served); } catch {}
  location.reload();
}
const rendering = new Set();
class Superseded extends Error {}
async function api(path, data, method) {
  const epoch = state.epoch;
  const opts = { method: method || (data === undefined ? 'GET' : 'POST'), headers: {}, credentials: 'same-origin', signal: AbortSignal.timeout(85000) };
  if (opts.method !== 'GET') opts.headers['X-CSRF-Token'] = state.csrf;
  if (data !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(data); }
  const response = await fetch(`/api${path}`, opts);
  checkVersion(response.headers.get('X-JunPanel-Version'));
  let value; try { value = await response.json(); } catch { throw new Error('服务响应异常，请检查本机代理状态'); }
  if (!response.ok) { if (response.status === 401 && path !== '/login') showLogin(); throw new Error(value.error || `操作失败 (${response.status})`); }
  if (rendering.has(epoch) && epoch !== state.epoch) throw new Superseded('页面已切换');
  $('#footer-status').replaceChildren(el('span','status-dot'), document.createTextNode(` 已连接 · ${new Date().toLocaleTimeString('zh-CN', { hour12: false })}`));
  return value;
}
const modal = $('#modal');
let modalTimer = null;
function closeModal() { modal.close(); delete modal.dataset.job; clearInterval(modalTimer); modalTimer = null; $('#modal-content').replaceChildren(); $('#modal-footer').replaceChildren(); }
function openModal(title, content, actions = [], large = false) { closeModal(); modal.classList.toggle('large', large); $('#modal-title').textContent = title; $('#modal-content').append(content); $('#modal-footer').append(...actions); modal.showModal(); }
$('#modal-close').onclick = closeModal;
modal.addEventListener('cancel', () => { clearInterval(modalTimer); modalTimer = null; });
function formModal(title, fields, submitText, submit, options = {}) {
  const form = el('form'); if (options.note) form.append(note(options.note, options.warning)); fields.forEach(f => form.append(f.n)); const error = el('div','form-error'); form.append(error);
  const save = button(submitText, () => form.requestSubmit(), options.danger ? 'danger' : 'primary');
  form.onsubmit = async e => { e.preventDefault(); save.disabled = true; error.textContent = ''; try { await submit(); } catch(e) { error.textContent = e.message; } finally { save.disabled = false; } };
  openModal(title, form, [button('取消', closeModal), save], options.large);
  return form;
}
function confirm(title, message, fn, danger = false) { const n = el('div'); n.append(el('p','',message)); const b = button('确认操作', async () => { b.disabled = true; try { await fn(); } finally { b.disabled = false; } }, danger ? 'danger' : 'primary'); openModal(title, n, [button('取消', closeModal), b]); }
async function mutation(path, data, method) { const result = await api(path, data, method); closeModal(); if (result && result.id && result.state) { toast('任务已开始'); showJob(result.id); } else { toast('操作已完成'); await render(); } return result; }
function clearPage() { state.epoch++; state.timers.forEach(clearInterval); state.timers = []; if (state.socket) { state.socket.close(); state.socket = null; } }
function interval(fn, ms) { state.timers.push(setInterval(() => { if (!document.hidden) fn().catch(e => { $('#connection-error').hidden = false; $('#connection-error').textContent = e.message; }); }, ms)); }
function showLogin() { clearPage(); closeModal(); state.csrf = ''; $('#application').hidden = true; $('#login').hidden = false; }
function setSiteBadge(count) { const b = $('#site-badge'); b.hidden = count == null; b.textContent = count ?? ''; }
async function enter(session) { Object.assign(state, session); $('#login').hidden = true; $('#application').hidden = false; $('#demo-banner').hidden = !state.demo; $('#current-user').textContent = state.user; $('#user-avatar').textContent = (state.user || 'J').slice(0, 1).toUpperCase(); $('#login-password').value = ''; await navigate(location.hash.slice(1).split('?')[0] || 'overview'); const currentCSRF=state.csrf; Promise.allSettled([api('/sites'),api('/apps'),api('/alerts'),api('/upgrade')]).then(([sites,apps,alerts,upgrade])=>{if(state.csrf!==currentCSRF)return;if(sites.status==='fulfilled'){state.sites=inventorySites(sites.value);setSiteBadge(sites.value.error?null:inventorySites(sites.value).length);}if(apps.status==='fulfilled')state.apps=apps.value;if(alerts.status==='fulfilled')setAlertBadge(alerts.value.active.length);if(upgrade.status==='fulfilled')setUpgradeBadge(upgrade.value.available?upgrade.value.latest.version:'');}); }
$('#login-form').onsubmit = async e => { e.preventDefault(); const b = $('#login-submit'); b.disabled = true; $('#login-error').textContent = ''; try { const r = await api('/login', { user: $('#login-user').value, password: $('#login-password').value, otp: $('#login-otp').value }); await enter(r); } catch(e) { $('#login-error').textContent = e.message; } finally { b.disabled = false; } };
$('#show-password').onclick = () => { const i = $('#login-password'); i.type = i.type === 'password' ? 'text' : 'password'; };
$('#logout').onclick = async () => { try { await api('/logout', {}); } finally { showLogin(); } };
$('#mobile-menu').onclick = () => $('#sidebar').classList.toggle('open');
$('#sidebar-mask').onclick = () => $('#sidebar').classList.remove('open');
// The theme setting is "auto" (follow the system, also while the page is open), "light" or "dark".
const themeModes = ['auto', 'light', 'dark'], themeNames = { auto: '跟随系统', light: '浅色', dark: '深色' }, themeIcons = { auto: 'monitor', light: 'sun', dark: 'moon' };
const systemDark = matchMedia('(prefers-color-scheme: dark)');
let themeMode = 'auto'; try { const saved = localStorage.getItem('junpanel-theme'); if (themeModes.includes(saved)) themeMode = saved; } catch {}
function applyTheme(mode = themeMode) {
  themeMode = mode;
  document.documentElement.dataset.theme = mode === 'auto' ? (systemDark.matches ? 'dark' : 'light') : mode;
  const toggle = $('#theme-toggle'), title = `主题：${themeNames[mode]}（点击切换）`;
  toggle.replaceChildren(icon(themeIcons[mode])); toggle.title = title; toggle.setAttribute('aria-label', title);
  $$('.chart').forEach(c => c._draw && c._draw());
}
applyTheme();
systemDark.addEventListener('change', () => { if (themeMode === 'auto') applyTheme(); });
$('#theme-toggle').onclick = () => { const next = themeModes[(themeModes.indexOf(themeMode) + 1) % themeModes.length]; applyTheme(next); try { localStorage.setItem('junpanel-theme', next); } catch {} toast(`主题：${themeNames[next]}`); };
$$('#nav [data-route]').forEach(b => b.onclick = () => navigate(b.dataset.route));
window.addEventListener('hashchange', () => { const r = location.hash.slice(1).split('?')[0]; if (state.csrf && r !== state.route) navigate(r); });
let lastActivity = 0;
['pointerdown','keydown'].forEach(type => document.addEventListener(type, () => { if (!state.csrf || Date.now() - lastActivity < 60000) return; lastActivity = Date.now(); api('/heartbeat', {}).catch(() => {}); }, { passive: true }));
async function navigate(route) { state.route = routes[route] ? route : 'overview'; history.replaceState(null, '', `#${state.route}`); $('#sidebar').classList.remove('open'); await render(); }
function drawChrome() {
  const r = routes[state.route], group = r[3], siblings = groupRoutes(group), label = groupLabel(group);
  const crumb = $('#breadcrumb'); crumb.replaceChildren();
  if (siblings.length > 1) crumb.append(el('span', '', label), el('span', 'sep', '/'));
  crumb.append(el('strong', '', siblings.length > 1 ? r[0] : label));
  document.title = `${siblings.length > 1 ? r[0] + ' · ' + label : label} · JunPanel`;
  document.body.dataset.route = state.route;
  $$('#nav .nav-link').forEach(n => { const active = n.dataset.group === group; n.classList.toggle('active', active); if (active) n.setAttribute('aria-current', 'page'); else n.removeAttribute('aria-current'); });
  const sub = $('#subnav'); sub.hidden = siblings.length < 2;
  sub.replaceChildren(...siblings.map(s => { const b = el('button', `subnav-item${s === state.route ? ' active' : ''}`, routes[s][0]); b.type = 'button'; b.onclick = () => navigate(s); return b; }));
  $('#page-title').textContent = siblings.length > 1 ? '' : label; $('#page-title').hidden = siblings.length > 1;
  $('#page-description').textContent = r[1]; $('#page-bar').hidden = state.route === 'overview';
}
const renderers = () => ({ overview: overviewPage, apps: appsPage, installed: appsPage, sites: sitesPage, certs: certsPage, databases: databasesPage, containers: containersPage, projects: projectsPage, images: imagesPage, networks: networksPage, volumes: volumesPage, terminal: terminalPage, files: filesPage, schedules: schedulesPage, backups: backupsPage, alerts: alertsPage, jobs: jobsPage, audit: auditPage, logins: loginsPage, security: securityPage, access: accessPage, upgrade: upgradePage });
async function render() {
  clearPage(); const epoch = state.epoch; drawChrome(); $('#connection-error').hidden = true;
  $('#page-actions').replaceChildren(); view.replaceChildren(el('div','skeleton','正在加载…'));
  rendering.add(epoch);
  try { const n = await renderers()[state.route](); if (epoch !== state.epoch) return; view.replaceChildren(n); }
  catch(e) { if (epoch !== state.epoch || e instanceof Superseded) return; view.replaceChildren(empty('暂时无法读取', e.message), button('重新连接', render, 'secondary','refresh')); }
  finally { rendering.delete(epoch); }
}
function action(...buttons) { $('#page-actions').append(...buttons); }
function table(headers) { const wrap = el('div','table-wrap'), t = el('table','data-table'), head = el('thead'), row = el('tr'), body = el('tbody'); headers.forEach(h => row.append(el('th','',h))); head.append(row); t.append(head,body); wrap.append(t); return { n: wrap, body }; }
function cells(body, ...values) { const r = el('tr'); values.forEach(v => { const c = el('td'); c.append(v instanceof Node ? v : document.createTextNode(String(v))); r.append(c); }); body.append(r); return r; }
function ops(...buttons) { const n = el('div','table-ops'); n.append(...buttons); return n; }
function titleCell(title, sub, symbol = 'box') { const n = el('div','table-name'), texts = el('div'); texts.append(el('strong','',title), el('small','muted',sub)); n.append(icon(symbol),texts); return n; }
function link(url, text) { const n = el('a','domain-link',text); n.href = url; n.target = '_blank'; n.rel = 'noopener noreferrer'; return n; }
function siteDomains(s) { const n = el('div','domain-links'); s.domains.forEach(d => n.append(link(`https://${d}`, `${d} ↗`))); return n; }
function appLogo(t) { const n = el('div', `app-logo ${t.key || ''}`, (t.name || t.key || 'A').slice(0, 1).toUpperCase()); if (t.color) n.style.background = t.color; return n; }
function meter(value) { const p = el('progress','progress'); p.max = 100; p.value = Math.max(0,Math.min(100,value || 0)); return p; }
const cssVar = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
// series: [[key, label, color variable]]; max = 100 for percentages, 0 to scale to the data (bytes/s).
function chart(history, series, max = 100) {
  const canvas = el('canvas','chart'); canvas.setAttribute('aria-label',series.map(s=>s[1]).join(' / ')); canvas.setAttribute('role','img');
  canvas._draw = () => {
    const rect = canvas.getBoundingClientRect(); if (!rect.width) return; const dpr = devicePixelRatio || 1, w = rect.width, h = 220, left = 58, right = 10, top = 28, bottom = 26;
    canvas.width = w * dpr; canvas.height = h * dpr; const ctx = canvas.getContext('2d'); ctx.scale(dpr,dpr);
    const muted = cssVar('--muted'), grid = cssVar('--line-soft'), font = getComputedStyle(document.body).fontFamily;
    const all = series.flatMap(([k]) => history.map(p => Number(p[k] || 0))), peak = max || Math.max(1024, ...all) * 1.2;
    ctx.font = `12px ${font}`; ctx.textBaseline = 'middle';
    for (let i = 0; i <= 4; i++) { const y = top + (h - top - bottom) * i / 4; ctx.strokeStyle = grid; ctx.beginPath(); ctx.moveTo(left, y); ctx.lineTo(w - right, y); ctx.stroke(); ctx.fillStyle = muted; ctx.textAlign = 'right'; ctx.fillText(max ? `${Math.round(peak * (1 - i / 4))}%` : `${bytes(peak * (1 - i / 4))}/s`, left - 8, y); }
    let lx = left; series.forEach(([, label, color]) => { ctx.fillStyle = cssVar(color); ctx.fillRect(lx, 8, 12, 3); ctx.fillStyle = muted; ctx.textAlign = 'left'; ctx.fillText(label, lx + 18, 10); lx += ctx.measureText(label).width + 40; });
    if (history.length < 2) { ctx.textAlign = 'center'; ctx.fillText('等待更多采样…', (left + w) / 2, h / 2); return; }
    const time = s => new Date(s).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' });
    ctx.fillStyle = muted; ctx.textAlign = 'left'; ctx.fillText(time(history[0].at), left, h - 10); ctx.textAlign = 'right'; ctx.fillText(time(history[history.length - 1].at), w - right, h - 10);
    series.forEach(([key, , colorVar]) => {
      const color = cssVar(colorVar), points = history.map((p, i) => [left + (w - left - right) * i / (history.length - 1), h - bottom - Math.min(peak, Number(p[key] || 0)) / peak * (h - top - bottom)]);
      ctx.beginPath(); points.forEach(([x, y], i) => i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)); ctx.strokeStyle = color; ctx.lineWidth = 1.8; ctx.lineJoin = 'round'; ctx.stroke();
      ctx.lineTo(w - right, h - bottom); ctx.lineTo(left, h - bottom); ctx.closePath(); const g = ctx.createLinearGradient(0, top, 0, h - bottom); g.addColorStop(0, color + '33'); g.addColorStop(1, color + '00'); ctx.fillStyle = g; ctx.fill();
    });
  };
  const observer = new ResizeObserver(() => { if (!canvas.isConnected) { observer.disconnect(); return; } canvas._draw(); }); observer.observe(canvas); return canvas;
}
function gauge(title, value, text, sub) {
  const pct = Math.max(0, Math.min(100, Number(value) || 0)), n = el('div','gauge'), ring = el('div', `gauge-ring${pct >= 90 ? ' bad' : pct >= 75 ? ' warn' : ''}`), r = 46, c = 2 * Math.PI * r;
  ring.innerHTML = `<svg viewBox="0 0 112 112" aria-hidden="true"><circle class="track" cx="56" cy="56" r="${r}"/><circle class="value" cx="56" cy="56" r="${r}" stroke-dasharray="${c.toFixed(1)}" stroke-dashoffset="${(c * (1 - pct / 100)).toFixed(1)}"/></svg>`;
  const t = el('div','gauge-text'); t.append(el('strong','',text)); ring.append(t); n.append(ring, el('span','',title), el('small','',sub)); return n;
}
function loadText(p) { return p < 50 ? '运行流畅' : p < 75 ? '运行正常' : p < 90 ? '运行缓慢' : '运行堵塞'; }
function inventorySites(data) {
  return [...(data.sites || []), ...(data.external_hosts || []).map(host=>({name:host,domains:[host],kind:'existing',readonly:true,sources:(data.caddy_sources || []).filter(source=>(source.hosts || []).includes(host)).map(source=>source.config)}))];
}
function accessLabel() { return location.protocol==='https:'?'HTTPS 加密访问':'本机 / SSH 访问'; }
async function overviewPage() {
  const data = await api('/overview'), n = el('div'); let choice = 0;
  function paint(d) {
    const visibleSites = inventorySites(d), h = d.host || {}, disk = h.disk || {}, containers = d.containers || [], running = containers.filter(c => c.state === 'running').length;
    state.sites = visibleSites; state.apps = d.apps || []; setSiteBadge(d.sites_error || d.caddy_error ? null : visibleSites.length); setAlertBadge(d.alerts_active); setUpgradeBadge(d.update_available);
    n.replaceChildren();
    for (const problem of [d.host_error, d.sites_error, d.apps_error, d.caddy_error, d.docker_error]) if (problem) n.append(note(problem, true));
    if (d.alerts_active > 0) { const b = note(`有 ${d.alerts_active} 条告警尚未解决。`, true); b.append(button('查看告警', () => navigate('alerts'), 'quiet small')); n.append(b); }
    if (d.update_available) { const b = note(`JunPanel v${d.update_available} 已发布，当前版本 v${d.version}。`); b.append(button('查看并升级', () => navigate('upgrade'), 'quiet small')); n.append(b); }
    const grid = el('div','home-grid'), main = el('div','home-col'), side = el('div','home-col');

    const counts = panel('概览'), cg = el('div','count-grid');
    [['网站', d.sites_error || d.caddy_error ? '—' : visibleSites.length, 'sites'], ['已安装应用', d.apps_error ? '—' : (d.apps || []).filter(a => !a.removed).length, 'installed'], ['容器', d.docker_error ? '—' : `${running} / ${containers.length}`, 'containers'], ['告警', d.alerts_active || 0, 'alerts']].forEach(([label, value, route]) => {
      const b = el('button', `count-item${route === 'alerts' && d.alerts_active ? ' alarm' : ''}`); b.type = 'button'; b.append(el('span','',label), el('strong','',String(value))); b.onclick = () => navigate(route); cg.append(b);
    });
    counts.append(cg);

    const status = panel('状态'), gg = el('div','gauge-grid'), cores = h.cores || 1, load = (h.load || [])[0] || 0, loadPct = load / cores * 100, mem = h.mem_total ? h.mem_used / h.mem_total * 100 : 0, used = disk.total ? disk.used / disk.total * 100 : 0;
    gg.append(gauge('负载', loadPct, `${Math.round(Math.min(loadPct, 100))}%`, loadText(loadPct)), gauge('CPU', h.cpu, h.cpu == null ? '—' : `${Number(h.cpu).toFixed(1)}%`, `${cores} 核`), gauge('内存', mem, `${mem.toFixed(1)}%`, `${bytes(h.mem_used)} / ${bytes(h.mem_total)}`), gauge('磁盘 /', used, `${used.toFixed(1)}%`, `${bytes(disk.used)} / ${bytes(disk.total)}`));
    status.append(gg);

    const choices = [['CPU', [['cpu','CPU','--primary']], 100], ['内存', [['memory','内存','--success']], 100], ['流量', [['rx','下行','--primary'], ['tx','上行','--warning']], 0]];
    const tabs = el('div','segmented'), now = el('div','monitor-now'), head = el('div','monitor-head'), box = el('div');
    now.append(...[['↓ 下行', `${bytes(h.rx)}/s`], ['↑ 上行', `${bytes(h.tx)}/s`], ['累计', `${bytes(h.rx_total)} / ${bytes(h.tx_total)}`]].map(([a, b]) => { const s = el('span','',a + ' '); s.append(el('b','',b)); return s; }));
    const choose = i => { choice = i; tabs.replaceChildren(...choices.map((c, j) => { const b = button(c[0], () => choose(j), 'quiet small'); b.classList.toggle('active', j === i); return b; })); box.replaceChildren(chart(h.history || [], choices[i][1], choices[i][2])); };
    choose(choice); head.append(tabs, now);
    const monitor = panel('监控', tag('最近 10 分钟')); monitor.append(head, box, el('p','chart-caption','每 5 秒采样一次，保存在内存中，面板重启后重新采样。流量已排除容器虚拟网卡。'));

    const dockerPanel = panel('容器', button('全部容器', () => navigate('containers'), 'quiet small'));
    if (d.docker_error) dockerPanel.append(note(d.docker_error, true));
    else if (!containers.length) dockerPanel.append(empty('没有容器', '系统 Docker 当前没有任何容器。'));
    else { const t = table(['名称', '镜像', '状态']); containers.slice(0, 6).forEach(c => cells(t.body, c.name, el('code','',c.image), containerStatus(c))); dockerPanel.append(t.n); }
    main.append(counts, status, monitor, dockerPanel);

    const info = panel('系统信息'); info.classList.add('sysinfo');
    [['主机名称', h.hostname || '—'], ['发行版本', h.os || 'Linux'], ['内核版本', h.kernel || '—'], ['处理器', `${cores} 核`], ['交换分区', `${bytes(h.swap_used)} / ${bytes(h.swap_total)}`], ['运行时间', uptime(h.uptime)], ['面板版本', d.version ? `v${d.version}` : '—'], ['访问方式', accessLabel()]].forEach(([a, b]) => info.append(line(a, b)));
    const services = panel('服务');
    const caddy = (d.caddy_state || '').trim(), caddyLabel = { active: '运行中', inactive: '已停止', failed: '失败', activating: '启动中', deactivating: '停止中', reloading: '重载中' }[caddy] || '未知';
    [['Docker', d.docker_error ? '连接失败' : '运行中', !d.docker_error, 'box', d.docker_error], ['Caddy', caddyLabel, caddy === 'active', 'globe', caddy]].forEach(([a, b, ok, sym, detail]) => { const r = el('div','service-item'), t = el('div','service-title'), state = tag(b, ok ? 'ok' : 'warn'); if (detail) state.title = detail; t.append(icon(sym), el('span','',a)); r.append(t, state); services.append(r); });
    const sites = panel('网站', button('全部网站', () => navigate('sites'), 'quiet small'));
    if (!visibleSites.length) sites.append(empty(d.caddy_error || d.sites_error ? '网站读取未完成' : '还没有网站', d.caddy_error || d.sites_error || '这里会显示面板创建的网站和原有 Caddy 域名。'));
    visibleSites.slice(0, 5).forEach(s => { const r = el('div','resource-row'), l = el('div','resource-left'), t = el('div'); t.append(el('strong','resource-name',s.name), el('small','',s.readonly ? '原有 Caddy 配置' : s.domains.join(' · '))); l.append(el('span','round-initial',s.readonly ? 'C' : s.kind === 'proxy' ? '↗' : 'S'), t); r.append(l, tag(s.readonly ? '原有' : s.enabled ? '已启用' : '已停用', s.readonly ? 'blue' : s.enabled ? 'ok' : '')); sites.append(r); });
    const activity = panel('最近任务', button('全部', () => navigate('jobs'), 'quiet small'));
    if (!d.jobs?.length) activity.append(empty('暂无任务', '部署、备份和容器操作会记录在这里。'));
    (d.jobs || []).slice(0, 4).forEach(j => { const b = el('button','resource-row activity-button'), l = el('div','resource-left'), t = el('div'); t.append(el('strong','resource-name',j.title), el('small','',date(j.started))); l.append(el('span',`job-icon ${j.state}`,j.state === 'success' ? '✓' : j.state === 'running' ? '↻' : '!'), t); b.append(l, jobTag(j)); b.onclick = () => showJob(j.id); activity.append(b); });
    side.append(info, services, sites, activity);
    grid.append(main, side); n.append(grid);
  }
  paint(data);
  const epoch = state.epoch; interval(async () => { const d = await api('/overview'); if (epoch === state.epoch) { paint(d); $('#connection-error').hidden = true; } }, 10000); return n;
}
async function sitesPage() {
  const data=await api('/sites'), rows=data.sites; state.sites=inventorySites(data); setSiteBadge(data.error?null:inventorySites(data).length); action(button('创建网站',()=>siteForm(),'primary','plus')); const n=el('div');n.append(note('网站使用系统 Caddy 自动签发 HTTPS。域名需先解析到服务器，80/443 需已可达。面板只管理自己创建的配置。'));
  if(data.error)n.append(note(data.error,true));
  if(data.caddy_sources?.length){const sources=panel('Caddy 配置来源',tag(`${data.caddy_sources.length} 份配置`,'blue'));sources.classList.add('existing-site-list');sources.append(el('p','muted','从系统主配置及运行中的 Caddy 进程发现。这里只读取配置文件；原有实例不由面板重载，磁盘配置可能与当前内存配置不同。'));for(const source of data.caddy_sources){const row=el('div','resource-row'),text=el('div');text.append(el('strong','resource-name',source.config),el('small','muted',`PID ${source.pids?.length?source.pids.join(', '):'未关联'} · ${source.error?'读取失败':`${source.hosts?.length || 0} 个主机匹配`} · ${source.primary?'面板连接的主配置':'原有实例，只读'}`));if(source.error)text.append(el('p','form-error',source.error));row.append(text,tag(source.error?'读取失败':'已读取',source.error?'bad':'ok'));sources.append(row);}n.append(sources);}
  if(data.external_hosts?.length){const existing=panel('原有 Caddy 网站',tag(`${data.external_hosts.length} 个域名`,'blue'));existing.classList.add('existing-site-list');existing.append(el('p','muted','已汇总系统主配置和其他运行中 Caddy 实例的配置。按域名去重；配置存在不代表网站已通过健康检查。'));for(const host of data.external_hosts){const r=el('div','resource-row'),t=el('div');t.append(el('strong','resource-name',host),el('small','muted','来源：'+((data.caddy_sources || []).filter(source=>(source.hosts || []).includes(host)).map(source=>source.config).join(' · ') || '系统 Caddy')));r.append(t,tag('只读','blue'));existing.append(r);}n.append(existing);}
  if(!rows.length){n.append(note(data.external_hosts?.length?'上方是已识别的原有网站。你还没有通过 JunPanel 新建网站；原有网站配置继续保留。':'尚未通过 JunPanel 创建网站，可创建静态网站或反向代理。'));return n;}
  const t=table(['网站 / 域名','类型','目录 / 上游','状态','管理']); rows.forEach(s=>{const name=el('div','site-title');name.append(el('strong','',s.name),siteDomains(s));const controls=[button('编辑',()=>siteForm(s),'quiet small'),button(s.enabled?'停用':'启用',()=>confirm(`${s.enabled?'停用':'启用'} ${s.name}`, '配置通过校验后，将重载现有 Caddy。',()=>mutation(`/sites/${s.id}/toggle`,{})),'quiet small')];if(s.kind==='static')controls.unshift(button('文件',()=>{state.filePath=`/sites/${s.id}`;return navigate('files');},'quiet small'));controls.push(button('移除',()=>confirm(`移除 ${s.name}`,'仅移除站点配置并重载 Caddy；静态文件会保留。',()=>mutation(`/sites/${s.id}`,undefined,'DELETE'),true),'quiet small'));const status=el('div','table-ops');status.append(tag(s.readonly?'原有配置':s.enabled?'已启用':'已停用',s.readonly?'blue':s.enabled?'ok':''));if(s.auth)status.append(tag('需要密码','blue'));if(s.allow_ips?.length)status.append(tag(`限 ${s.allow_ips.length} 个地址`,'blue'));cells(t.body,name,{static:'静态网站',proxy:'反向代理',redirect:'跳转网站'}[s.kind] || s.kind,el('code','',s.kind==='static'?s.root:s.kind==='redirect'?`→ ${s.target}${s.keep_path?'（保留路径）':''} · ${s.permanent?301:302}`:s.upstream),status,ops(...controls));});n.append(t.n);return n;
}
function siteForm(site, app) {
  const name=field('网站名称',site?.name || app?.name || '',{max:60}), domains=field('域名',site?.domains.join('\n') || '',{area:true,rows:2,placeholder:'example.com\nwww.example.com',help:'每行一个域名，中文域名请先转换为 Punycode。'}), kind=field('网站类型',site?.kind || 'proxy',{select:[['static','静态网站'],['proxy','反向代理'],['redirect','跳转网站']]}), upstream=field('上游地址',site?.upstream || (app?`http://127.0.0.1:${app.port}`:'http://127.0.0.1:8080'),{help:'完整的 http(s)://主机:端口；不包含路径。'});
  const target=field('跳转到',site?.target || '',{placeholder:'https://www.example.com',help:'完整的 http(s):// 网址。'}), code=field('跳转方式',site?.permanent?'301':'302',{select:[['302','临时跳转（302）'],['301','永久跳转（301，浏览器会记住）']]}), keep=field('访问路径',site && !site.keep_path?'0':'1',{select:[['1','保留（/a/b 跳到 目标/a/b）'],['0','不保留（都跳到目标地址）']]});
  const authUser=field('访问用户名',site?.auth_user || '',{required:false,max:32,placeholder:'留空表示不需要密码'}), authPass=field('访问密码','',{type:'password',required:false,max:72,placeholder:site?.auth?'留空则保持原密码':'至少 8 位',help:'打开网站时浏览器会要求输入这个用户名和密码。可点右侧“显示”核对输入。'}), authPass2=field('再次输入访问密码','',{type:'password',required:false,max:72,placeholder:'与上面的密码一致'});
  const allow=field('只允许这些 IP 访问',(site?.allow_ips || []).join('\n'),{area:true,rows:2,required:false,placeholder:'留空表示不限制，例如 203.0.113.5 或 10.0.0.0/8',help:'每行一个 IP 或网段。经过线路机等中转访问时，这里看到的是中转机的 IP。'}), compress=field('压缩传输',site?.no_compress?'0':'1',{select:[['1','开启（推荐）'],['0','关闭']]});
  // These are the new site's own access credentials, not a login: keep browsers and password managers from filling in the panel's saved account.
  authUser.input.autocomplete='off';authUser.input.name='site-access-user';authUser.input.setAttribute('data-lpignore','true');authUser.input.setAttribute('data-1p-ignore','true');authPass2.input.autocomplete='new-password';authPass2.input.name='site-access-secret2';authPass2.input.setAttribute('data-lpignore','true');authPass2.input.setAttribute('data-1p-ignore','true');authPass.input.autocomplete='new-password';authPass.input.name='site-access-secret';authPass.input.setAttribute('data-lpignore','true');authPass.input.setAttribute('data-1p-ignore','true');
  if(site)kind.input.disabled=true;
  const refresh=()=>{const k=kind.val();upstream.n.hidden=k!=='proxy';upstream.input.required=k==='proxy';[target,code,keep].forEach(f=>f.n.hidden=k!=='redirect');target.input.required=k==='redirect';[authUser,authPass,authPass2,allow,compress].forEach(f=>f.n.hidden=k==='redirect');};kind.input.onchange=refresh;refresh();
  formModal(site?'编辑网站':'创建网站',[name,domains,kind,upstream,target,code,keep,authUser,authPass,authPass2,allow,compress],'保存并应用',async()=>{const k=kind.val(),redirect=k==='redirect';if(!redirect&&authPass.input.value!==authPass2.input.value)throw new Error('两次输入的访问密码不一致，请点“显示”核对');await mutation('/sites',{id:site?.id || '',name:name.val(),domains:domains.val().split(/[\s,]+/).filter(Boolean),kind:k,upstream:k==='proxy'?upstream.val():'',target:redirect?target.val():'',permanent:code.input.value==='301',keep_path:keep.input.value==='1',auth_user:redirect?'':authUser.val(),auth_password:redirect?'':authPass.input.value,allow_ips:redirect?[]:allow.val().split(/[\s,]+/).filter(Boolean),no_compress:!redirect&&compress.input.value==='0'});},{note:'面板会检查域名冲突，校验 Caddy 配置，再重载服务。失败时尝试还原本次配置。'});
}
async function appsPage() {
  const [catalog,apps]=await Promise.all([api('/catalog'),api('/apps')]);state.catalog=catalog;state.apps=apps;const n=el('div');action(button('刷新',render,'secondary','refresh'));
  if(state.route==='apps') {
    n.append(note('数据库凭据随机生成，保存在管理代理的私有目录。标有“可备份”的应用可以在“备份”页备份和恢复，升级前也会自动备份。'));
    const categories=['全部',...new Set(catalog.map(t=>t.category))];
    const chips=el('div','chips'),grid=el('div','catalog-grid');let category='全部';const draw=()=>{chips.replaceChildren(...categories.map(c=>{const b=button(c,()=>{category=c;draw();},'quiet small');b.classList.add('chip');b.classList.toggle('active',category===c);return b;}));grid.replaceChildren();catalog.filter(t=>category==='全部'||t.category===category).forEach(t=>{const card=el('section','app-card'),head=el('div','app-card-head'),title=el('div');title.append(el('h3','',t.name),el('small','muted',t.category));head.append(appLogo(t),title);if(t.backup)head.append(tag('可备份','ok'));const bottom=el('div','app-card-bottom'),have=t.multi?null:apps.find(x=>x.template===t.key&&!x.removed),install=have?button('已安装',()=>{},'secondary small'):button('安装',()=>installApp(t),'primary small');if(have){install.disabled=true;install.title=`已安装为“${have.name}”，可在“已安装”页管理；卸载后才能再次安装`;card.classList.add('installed');}bottom.append(el('code','',t.image),install);card.append(head,el('p','muted',t.summary),bottom);grid.append(card);});};draw();n.append(chips,grid);
  } else {
    if(!apps.length){const e=empty('还没有安装应用','到“全部应用”中选择一个开始部署。');e.append(button('去安装',()=>navigate('apps'),'primary'));n.append(e);}
    const grid=el('div','installed-grid');apps.forEach(a=>{const t=catalog.find(t=>t.key===a.template),card=el('section','installed-card'),head=el('div','installed-header'),name=el('div');name.append(el('h3','',a.name),el('small','muted',t?.name || a.template));head.append(appLogo(t || {key:a.template}),name,tag(a.busy?'任务中':a.removed?'已卸载':a.docker_available===false?'状态未知':`${a.running || 0}/${a.containers || 0} 运行`,a.busy?'blue':a.removed?'':a.running?'ok':'warn'));card.append(head,el('div','installed-meta',`项目 ${a.project} · 本机端口 ${a.port}`));if(a.last_error)card.append(el('p','error-text',a.last_error));const controls=el('div','installed-actions');controls.append(button('连接信息',()=>credentialForm(a),'quiet small'));if(a.removed)controls.append(button('恢复部署',()=>appAction(a,'restore'),'quiet small'));else{if(!a.database)controls.append(button('绑定域名',()=>siteForm(null,a),'quiet small'));else controls.append(button('管理数据库',()=>navigate('databases'),'quiet small'));[['升级','upgrade'],['启动','start'],['停止','stop'],['重启','restart'],['重新部署','deploy'],['卸载','remove']].forEach(([text,v])=>controls.append(button(text,()=>appAction(a,v,t),'quiet small')));}$$('button',controls).forEach(b=>b.disabled=!!a.busy);card.append(controls);grid.append(card);});if(apps.length)n.append(grid,note('“重新部署”使用本地已有镜像；“升级”会拉取镜像的最新版本并重建容器。'));
  }return n;
}
function installApp(t) { const id=field('应用标识',t.key,{max:32,help:'2–32 位小写字母、数字与中划线，以字母开头。'}),name=field('显示名称',t.name,{max:60}),port=field('本机端口',String(t.port),{type:'number',help:'仅绑定 127.0.0.1。请为每个应用使用独立端口。'});port.input.min=1024;port.input.max=65535;formModal(`安装 ${t.name}`,[name,id,port],'开始部署',()=>mutation('/apps',{id:id.val(),name:name.val(),template:t.key,port:Number(port.val())}),{note:'将使用现有 Docker Compose 下载并启动应用；可在任务日志中查看结果。'+(t.note?' '+t.note:'')}); }
function appAction(a,action,t) { const label={start:'启动',stop:'停止',restart:'重启',deploy:'重新部署',remove:'卸载',restore:'恢复部署',upgrade:'升级'}[action];const text=action==='remove'?'将停止并移除该应用的容器和网络，保留数据卷及私有项目配置。':action==='upgrade'?(t?.backup===false?'将拉取镜像的最新版本并重建容器。此应用不保存数据，不需要备份。':'将先备份应用数据（应用会短暂停止），再拉取镜像的最新版本并重建容器。备份失败时不会升级；升级后有问题可以在“备份”页恢复。'):`将通过现有 Docker Compose ${label}该应用。`;confirm(`${label} ${a.name}`,text,()=>mutation(`/apps/${a.id}/action`,{action}),action==='remove'||action==='stop'); }
function containerStatus(c) { return tag(({running:'运行中',exited:'已停止',created:'已创建',paused:'已暂停',restarting:'重启中',dead:'异常'})[c.state] || c.state,c.state==='running'?'ok':c.state==='exited'?'':'warn'); }
async function containersPage() {
  const n=el('div');
  const rows=await api('/containers');action(button('刷新',render,'secondary','refresh'));const search=field('筛选容器','',{required:false,placeholder:'名称、镜像或项目'});search.n.classList.add('toolbar');n.append(search.n);const holder=el('div');n.append(holder);const paint=()=>{const q=search.val().toLowerCase(),filtered=rows.filter(c=>`${c.name} ${c.image} ${c.project}`.toLowerCase().includes(q)),t=table(['容器 / 镜像','状态','项目','端口','操作']);filtered.forEach(c=>{const buttons=[button('日志',()=>showLogs(c),'quiet small')];if(c.state==='running'&&!c.protected)buttons.push(button('终端',()=>{state.terminalId=c.id;return navigate('terminal');},'quiet small'));if(!c.protected&&['running','exited','created'].includes(c.state)){(c.state==='running'?[['停止','stop'],['重启','restart']]:[['启动','start']]).forEach(([a,b])=>buttons.push(button(a,()=>confirm(`${a} ${c.name}`,'该操作会改变真实容器状态。',()=>mutation(`/containers/${c.id}/action`,{action:b}),b==='stop'),'quiet small')));}cells(t.body,titleCell(c.name,c.image),containerStatus(c),c.project || '独立容器',el('code','',(c.ports || []).map(p=>`${p.ip || '*'}:${p.public} → ${p.private}/${p.protocol}`).join('\n')||'—'),ops(...buttons,c.protected?tag('受保护'):el('span')));});holder.replaceChildren(filtered.length?t.n:empty(rows.length?'没有匹配的容器':'系统 Docker 未列出容器',rows.length?'请调整筛选条件。':'直接运行的程序、系统服务或静态网站不属于 Docker 容器。'));};search.input.oninput=paint;paint();return n;
}
async function imagesPage() { const rows=await api('/images'),n=el('div'),t=table(['镜像','ID','大小','创建时间','操作']);action(button('刷新',render,'secondary','refresh'),button('拉取镜像',()=>{const f=field('镜像名','',{placeholder:'nginx:stable-alpine',max:250});formModal('拉取镜像',[f],'开始拉取',()=>mutation('/images/pull',{image:f.val()}));},'primary','plus'));rows.forEach(i=>cells(t.body,(i.tags || []).join('\n')||'<无标签>',el('code','',i.id.slice(0,12)),bytes(i.size),date(i.created*1000),ops(button('删除',()=>confirm('删除镜像','将使用非强制方式删除镜像；仍被容器使用的镜像会拒绝删除。',()=>mutation(`/images/${i.id}`,undefined,'DELETE'),true),'quiet small'))));n.append(rows.length?t.n:empty('没有镜像'));return n; }
async function showLogs(c) { const n=el('div'),tools=el('div','log-toolbar'),tail=field('显示行数','200',{select:[['50','最近 50 行'],['100','最近 100 行'],['200','最近 200 行'],['500','最近 500 行']]}),out=el('pre','log-view','正在读取…');const load=async()=>{const r=await api(`/containers/${c.id}/logs?tail=${tail.val()}`);out.textContent=r.logs || r.text || '暂无日志';out.scrollTop=out.scrollHeight;};tools.append(tail.n,button('刷新',load,'secondary small','refresh'));tail.input.onchange=()=>load().catch(e=>toast(e.message,true));n.append(tools,out);openModal(`${c.name} · 日志`,n,[button('关闭',closeModal)],true);try{await load();}catch(e){out.textContent=e.message;} }
const stackActionText = { deploy: '按当前文件创建或更新容器，未变化的容器保持运行。', pull: '拉取文件中镜像的最新版本，并重建有变化的容器。', start: '启动已停止的容器。', stop: '停止全部容器，容器本身保留。', restart: '依次重启全部容器。', down: '删除容器和网络，保留数据卷和编排文件，之后可以再次部署。' };
const stackSample = 'services:\n  web:\n    image: nginx:alpine\n    restart: unless-stopped\n    ports:\n      - "127.0.0.1:8088:80"\n    volumes:\n      - ./html:/usr/share/nginx/html:ro\n';
async function projectsPage() {
  const [data, groups] = await Promise.all([api('/stacks'), api('/projects')]), n = el('div');
  action(button('刷新', render, 'secondary', 'refresh'), button('新建编排', () => stackEditor(null), 'primary', 'plus'));
  n.append(note(`编排的相对路径指向 ${data.root}/<编排名>。为安全起见，不允许特权容器、宿主机网络和命名空间、额外权限、设备，以及挂载 /srv/junpanel 以外的目录；保存前会逐项检查。`));
  if (data.docker_error) n.append(note(data.docker_error, true));
  if (!data.stacks.length) n.append(empty('还没有编排', '点击“新建编排”，粘贴或编写 docker-compose 文件。'));
  const grid = el('div', 'project-grid');
  data.stacks.forEach(s => {
    const card = panel(s.name, tag(s.busy ? '任务中' : `${s.running} / ${s.containers.length} 运行`, s.busy ? 'blue' : s.running ? 'ok' : s.containers.length ? 'warn' : ''));
    card.append(el('p', 'muted', `服务：${s.services.join('、') || '—'} · 更新于 ${date(s.updated)}`));
    if (s.last_error) card.append(el('p', 'error-text', s.last_error));
    const members = el('div', 'project-members');
    s.containers.forEach(c => { const r = el('div', 'resource-row'); r.append(el('span', '', c.name), containerStatus(c)); members.append(r); });
    const controls = el('div', 'installed-actions');
    controls.append(button('编辑', () => stackEditor(s.name), 'quiet small'));
    [['部署', 'deploy'], ['更新镜像', 'pull'], ['启动', 'start'], ['停止', 'stop'], ['重启', 'restart'], ['停用', 'down']].forEach(([label, act]) => controls.append(button(label, () => confirm(`${label} ${s.name}`, stackActionText[act], () => mutation(`/stacks/${s.name}/action`, { action: act }), act === 'stop' || act === 'down'), 'quiet small')));
    controls.append(button('删除', () => confirm(`删除编排 ${s.name}`, `将删除它的容器、网络和编排定义。数据卷和 ${data.root}/${s.name} 中的文件保留。`, () => mutation(`/stacks/${s.name}`, undefined, 'DELETE'), true), 'quiet small'));
    $$('button', controls).forEach(b => b.disabled = !!s.busy);
    card.append(members, controls); grid.append(card);
  });
  if (data.stacks.length) n.append(grid);
  const others = groups.filter(p => !data.stacks.some(s => s.name === p.name));
  if (others.length) {
    const box = panel('其它 Compose 项目', tag(`${others.length} 个`, 'blue'));
    box.append(el('p', 'muted', '面板应用和在面板之外部署的项目。可以整组启动、停止或重启，不能在这里编辑。'));
    others.forEach(p => {
      const r = el('div', 'resource-row'), t = el('div'), c = el('div', 'table-ops');
      t.append(el('strong', 'resource-name', p.name), el('small', '', `${p.running} / ${p.containers.length} 运行${p.name.startsWith('junpanel-') ? ' · 面板应用' : ''}`));
      [['启动', 'start'], ['停止', 'stop'], ['重启', 'restart']].forEach(([label, act]) => c.append(button(label, () => confirm(`${label}项目 ${p.name}`, '仅对现有非一次性容器逐个操作；中途失败时会停止并报告，已完成的操作不会回退。', () => mutation(`/projects/${encodeURIComponent(p.name)}/action`, { action: act }), act === 'stop'), 'quiet small')));
      r.append(t, c); box.append(r);
    });
    n.append(box);
  }
  return n;
}
function stackProblems(list) { const n = note('未通过检查：', true), ul = el('ul', 'problem-list'); list.forEach(p => ul.append(el('li', '', p))); n.lastChild.append(ul); return n; }
async function stackEditor(name) {
  const current = name ? await api(`/stacks/${name}`) : null;
  const title = field('编排名称', name || '', { max: 32, help: '2–32 位小写字母、数字或中划线，以字母开头，同时也是 Compose 的项目名。' });
  if (name) title.input.disabled = true;
  const src = field('docker-compose 文件', current ? current.source : stackSample, { area: true, rows: 18, required: false });
  src.input.classList.add('code-editor'); src.input.spellcheck = false;
  const result = el('div');
  const save = deploy => mutation('/stacks', { name: title.val(), source: src.input.value, create: !name, deploy });
  const form = formModal(name ? `编辑编排 ${name}` : '新建编排', [title, src], '保存并部署', () => save(true), { large: true, note: `相对路径指向 /srv/junpanel/stacks/${name || '<编排名>'}。保存前会用无权限的用户解析文件，再检查安全限制，部署的正是检查过的内容。` });
  form.append(result);
  const check = button('检查配置', async () => { const r = await api('/stacks/check', { name: title.val(), source: src.input.value }); result.replaceChildren(r.ok ? note(`检查通过：${r.services.join('、')}`) : stackProblems(r.problems)); }, 'secondary');
  const onlySave = button('仅保存', async b => { b.disabled = true; try { await save(false); } finally { b.disabled = false; } }, 'secondary');
  $('#modal-footer').prepend(check, onlySave);
}
const joinPath=(p,name)=>(p==='/'?'':p)+'/'+name;
async function filesPage() {
  const path=state.filePath, data=await api(`/files?path=${encodeURIComponent(path)}`), rows=data.entries || data, n=el('div');
  action(button('回收站',showTrash,'secondary','trash'),button('新建',()=>{const type=field('类型','file',{select:[['file','文本文件'],['folder','文件夹']]}),name=field('名称','',{max:200});formModal('新建文件或文件夹',[type,name],'创建',async()=>{if(/[\/\\]/.test(name.val()))throw new Error('请输入单个文件名');const p=joinPath(path,name.val());await mutation(type.val()==='folder'?'/files/mkdir':'/files/save',type.val()==='folder'?{path:p}:{path:p,text:'',revision:''});});},'secondary','plus'),button('上传文件',()=>uploadInput.click(),'primary','plus'));
  const uploadInput=el('input');uploadInput.type='file';uploadInput.hidden=true;uploadInput.onchange=async()=>{const f=uploadInput.files[0];if(!f)return;if(f.size>512*1024*1024){toast('单文件上传上限 512 MB',true);uploadInput.value='';return;}try{await uploadFile(joinPath(path,f.name),f);toast('上传完成');await render();}catch(e){toast(e.message,true);}finally{uploadInput.value='';}};n.append(uploadInput);
  const crumb=el('div','file-breadcrumb');crumb.append(button('/srv/junpanel',()=>{state.filePath='/';return render();},'quiet small','folder'));let current='';path.split('/').filter(Boolean).forEach(part=>{current+='/'+part;const dest=current;crumb.append(el('span','muted','/'),button(part,()=>{state.filePath=dest;return render();},'quiet small'));});n.append(crumb);
  const t=table(['名称','大小','修改时间','权限','操作']); if(path!=='/')cells(t.body,button('.. 上一级',()=>{state.filePath=path.slice(0,path.lastIndexOf('/'))||'/';return render();},'quiet small'),'', '', '', '');
  rows.forEach(f=>{const p=f.path || joinPath(path,f.name),isDir=f.directory ?? f.dir ?? f.is_dir; const name=button(f.name,()=>{if(f.link)throw new Error('符号链接不在界面中展开');if(isDir){state.filePath=p;return render();}return editFile(p);},'quiet small',isDir?'folder':'file');name.classList.add('file-name');const controls=[];if(!isDir&&!f.link){const download=link(`/api/files/download?path=${encodeURIComponent(p)}`,'下载');download.removeAttribute('target');controls.push(download);}controls.push(button('重命名',()=>{const v=field('新路径',p,{help:'可移动到管理目录中的其它位置；不会覆盖已有目标。'});formModal('重命名 / 移动',[v],'保存',()=>mutation('/files/rename',{path:p,to:v.val()}));},'quiet small'),button('删除',()=>confirm(`移入回收站 · ${f.name}`,'文件将移动到本机回收站，可在回收站中恢复。站点根目录不可移动。',()=>mutation('/files/trash',{path:p}),true),'quiet small'));cells(t.body,name,isDir?'文件夹':bytes(f.size),date(f.modified || f.mod_time),el('code','',f.mode || '—'),ops(...controls));});n.append(t.n,note('文件操作限定在 /srv/junpanel。文本编辑上限 1 MB，单文件上传上限 512 MB，下载不限大小。'));return n;
}
async function editFile(path) { const data=await api(`/files/content?path=${encodeURIComponent(path)}`),text=field(path,data.text || '',{area:true,rows:20,required:false});text.input.classList.add('code-editor');text.input.spellcheck=false;formModal('编辑文本文件',[text],'保存文件',()=>mutation('/files/save',{path,text:text.input.value,revision:data.revision}),{large:true,note:'保存前会检查文件是否被其它操作修改，避免覆盖更新。'}); }
async function showTrash() { const rows=await api('/trash'),n=el('div');n.append(el('p','muted','回收站中的项目保留 30 天后自动永久删除。'));if(!rows.length)n.append(empty('回收站为空'));rows.forEach(r=>{const item=el('div','resource-row'),left=el('div','resource-left'),text=el('div','resource-name'),ops=el('div','table-ops'),name=r.path || r.original;text.append(el('strong','',name),el('small','muted',date(r.at)));left.append(text);ops.append(button('恢复',()=>mutation(`/trash/${r.id}/restore`,{}),'secondary small'),button('永久删除',()=>confirm('永久删除',`将永久删除 ${name}，无法恢复。`,async()=>{await api(`/trash/${r.id}`,undefined,'DELETE');toast('已永久删除');await showTrash();},true),'quiet small'));item.append(left,ops);n.append(item);});const actions=[button('关闭',closeModal)];if(rows.length)actions.unshift(button('清空回收站',()=>confirm('清空回收站',`将永久删除回收站中的全部 ${rows.length} 项，无法恢复。`,async()=>{const v=await api('/trash/empty',{});closeModal();toast(`已永久删除 ${v.removed} 项`);},true),'danger'));openModal('回收站',n,actions); }
function credentialForm(app) {
 const password=field('当前面板密码','',{type:'password',max:256}),otp=field('6 位动态码','',{required:state.totp,max:6});otp.n.hidden=!state.totp;
 formModal(`${app.name} · 连接信息`,[password,otp],'验证并查看',async()=>{const data=await api('/credentials',{id:app.id,password:password.input.value,otp:otp.val()});password.input.value='';const n=el('div');n.append(note(data.note));[['地址',data.host],['端口',String(data.port)],['用户',data.user],['数据库',data.database]].filter(v=>v[1]).forEach(([a,b])=>n.append(line(a,b)));if(data.password){const secret=field('数据库密码',data.password,{type:'password',required:false});secret.input.readOnly=true;n.append(secret.n);const toggle=button('显示 / 隐藏',()=>{secret.input.type=secret.input.type==='password'?'text':'password';},'secondary small');const copy=button('复制密码',async()=>{await navigator.clipboard.writeText(data.password);toast('密码已复制，请妥善保管剪贴板');},'secondary small');n.append(ops(toggle,copy));}openModal(`${app.name} · 连接信息`,n,[button('关闭',closeModal)]);},{note:'连接凭据需要再次验证管理员身份，仅在当前弹窗中展示。'});
}
async function databasesPage() {
  const rows=await api('/databases');action(button('刷新',render,'secondary','refresh'));if(!rows.length)return empty('还没有受管数据库','在应用商店安装 PostgreSQL、MySQL、Redis 或 WordPress 后，会出现在这里。');const n=el('div'),layout=el('div','database-layout'),left=el('div','database-selector'),main=el('div','panel');n.append(note('此处使用受管应用的管理员凭据。SQL 可更改或删除数据，请核对语句；可在“备份”页创建备份。',true),layout);layout.append(left,main);
  const choose=d=>{left.replaceChildren();rows.forEach(row=>{const b=el('button',`database-card${row.id===d.id?' selected':''}`);b.append(icon('database'),el('strong','',row.name),el('small','muted',`${row.kind} · ${row.port || '容器内部'}`));b.onclick=()=>choose(row);left.append(b);});main.replaceChildren();main.append(el('h2','',d.name));const db=field('数据库名称',d.database || 'junpanel',{max:63}),sql=field(d.kind==='redis'?'只读命令':'SQL 语句',d.kind==='redis'?'INFO':'SELECT 1;',{area:d.kind!=='redis',rows:8,select:d.kind==='redis'?[['INFO','INFO'],['DBSIZE','DBSIZE'],['PING','PING']]:undefined});if(d.kind!=='redis')sql.input.classList.add('code-editor');const result=el('pre','log-view','执行结果将显示在这里。'),tools=el('div','database-controls'),run=button('执行',async()=>{run.disabled=true;try{const value=await api(`/databases/${d.id}/query`,{database:db.val(),sql:d.kind==='redis'?'':sql.input.value,command:d.kind==='redis'?sql.val():''});result.textContent=value.text || '执行成功（无输出）';}finally{run.disabled=false;}},'primary','play');tools.append(run,button('连接信息',()=>credentialForm(d),'secondary','lock'));if(d.kind!=='redis'){tools.append(button('新建数据库',()=>{const name=field('数据库名称','',{max:63,help:'仅字母、数字及下划线，以字母或下划线开头。'});formModal('新建数据库',[name],'创建',()=>mutation(`/databases/${d.id}/create`,{name:name.val()}));},'secondary'));tools.append(button('备份此数据库',async()=>{if(!/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(db.val()))throw new Error('数据库名称格式无效');await mutation('/backups',{type:'database',target:d.id,database:db.val()});},'secondary'));}if(d.kind!=='redis')main.append(db.n);main.append(sql.n,tools,result,el('p','help','查询结果限制 8 MB。数据库备份是流式导出的压缩 SQL，不受此限制，需用数据库原生工具导入。'));};choose(rows.find(d=>d.id===state.dbId)||rows[0]);return n;
}
function decode64(s, decoder = new TextDecoder()) { const b=Uint8Array.from(atob(s),x=>x.charCodeAt(0));return decoder.decode(b,{stream:true}); }
function encode64(s) { const b=new TextEncoder().encode(s);let binary='';for(let i=0;i<b.length;i+=8192)binary+=String.fromCharCode(...b.subarray(i,i+8192));return btoa(binary); }
async function terminalPage() {
  const containers=await api('/containers'),rows=containers.filter(c=>c.state==='running'&&!c.protected),n=el('div');n.append(note('这是容器内的基础行终端，支持命令输入与输出；全屏编辑器请使用 SSH。终端拥有该容器的权限，输出仅保留在当前浏览器内存。',true));if(!rows.length){n.append(empty('没有可连接的容器'));return n;}
  const p=el('section','terminal-panel'),tools=el('div','terminal-toolbar'),select=field('容器',state.terminalId || rows[0].id,{select:rows.map(c=>[c.id,`${c.name} · ${c.image}`])}),status=tag('未连接'),output=el('pre','terminal-output','连接后会在容器中启动 /bin/sh。\n'),form=el('form','terminal-input'),input=el('input');input.placeholder='输入命令，按 Enter 执行';input.autocomplete='off';input.spellcheck=false;input.setAttribute('aria-label','终端命令');form.append(el('span','muted','$'),input);let ws=null;
  const append=t=>{output.textContent=(output.textContent+t.replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))/g,'').replace(/\r/g,'')).slice(-180000);output.scrollTop=output.scrollHeight;};
  const send=s=>{if(!ws||ws.readyState!==1)throw new Error('请先连接容器');ws.send(JSON.stringify({type:'input',data:encode64(s)}));};
  const connect=button('连接',async()=>{if(state.demo)throw new Error('演示模式不执行服务器变更');if(ws)ws.close();const ticket=await api('/terminal/ticket',{container:select.val()});const terminalDecoder=new TextDecoder();ws=new WebSocket(`ws://${location.host}/api/terminal?ticket=${encodeURIComponent(ticket.ticket)}`);state.socket=ws;status.textContent='连接中';ws.onopen=()=>{status.textContent='已连接';status.className='tag ok';connect.disabled=true;ws.send(JSON.stringify({type:'resize',rows:30,cols:100}));input.focus();};ws.onmessage=e=>{const d=JSON.parse(e.data);if(d.type==='output')append(decode64(d.data,terminalDecoder));if(d.type==='error')append(`\n${d.text}\n`);};ws.onclose=()=>{status.textContent='已断开';status.className='tag';connect.disabled=false;append('\n[连接已关闭]\n');};ws.onerror=()=>append('\n[无法建立连接，请检查容器与代理]\n');},'primary small');
  form.onsubmit=e=>{e.preventDefault();try{send(input.value+'\n');input.value='';}catch(e){toast(e.message,true);}};tools.append(select.n,status,connect,button('Ctrl+C',()=>send('\x03'),'secondary small'),button('断开',()=>ws?.close(),'quiet small'));p.append(tools,output,form);n.append(p,el('p','help','最多同时连接 2 个终端；无输入 5 分钟、连接 30 分钟后关闭。退出终端不保证结束容器内已启动的后台进程。'));interval(async()=>{if(ws?.readyState===1 && Date.now()-lastActivity<240000)ws.send(JSON.stringify({type:'ping'}));},60000);return n;
}
function jobTag(j) { return tag(({running:'进行中',success:'已完成',failed:'失败'})[j.state] || j.state,j.state==='success'?'ok':j.state==='failed'?'bad':'blue'); }
async function jobsPage() { const rows=await api('/jobs'),n=el('div','jobs-list');action(button('刷新',render,'secondary','refresh'));n.append(note('记录保存在代理内存中，最多保留 80 项，重启后清空。任务输出有长度限制。'));if(!rows.length)n.append(empty('还没有任务日志'));rows.forEach(j=>{const r=el('div','resource-row'),left=el('div','resource-left'),text=el('div');text.append(el('strong','resource-name',j.title),el('small','muted',date(j.started)));left.append(el('span',`job-icon ${j.state}`,j.state==='success'?'✓':j.state==='failed'?'!':'↻'),text);r.append(left,ops(jobTag(j),button('查看输出',()=>showJob(j.id),'quiet small')));n.append(r);});const running=rows.filter(j=>j.state==='running').length;$('#job-badge').hidden=!running;$('#job-badge').textContent=running;return n; }
async function networksPage() {
  const rows = await api('/networks'), n = el('div'), t = table(['网络', '驱动', '子网', '容器', '操作']);
  action(button('刷新', render, 'secondary', 'refresh'), button('创建网络', () => {
    const name = field('网络名称', '', { max: 63, placeholder: 'app-net', help: '字母或数字开头，只含字母、数字、下划线、点和中划线。' });
    const internal = field('外网访问', 'false', { select: [['false', '允许访问外网'], ['true', '仅内部通信（internal）']] });
    formModal('创建网络', [name, internal], '创建', () => mutation('/networks', { name: name.val(), internal: internal.val() === 'true' }));
  }, 'primary', 'plus'));
  rows.forEach(w => {
    const tags = el('div', 'tag-row');
    if (w.builtin) tags.append(tag('内置'));
    if (w.internal) tags.append(tag('仅内部', 'blue'));
    if (w.project) tags.append(tag(w.project.startsWith('junpanel-') ? '面板应用' : `项目 ${w.project}`, 'blue'));
    const name = titleCell(w.name, w.id.slice(0, 12), 'network'); name.lastChild.append(tags);
    const remove = button('删除', () => confirm(`删除网络 ${w.name}`, '仍有容器（包括已停止的容器）连接的网络会拒绝删除，避免这些容器无法再启动。', () => mutation(`/networks/${encodeURIComponent(w.name)}`, undefined, 'DELETE'), true), 'quiet small');
    remove.disabled = w.builtin || w.containers > 0;
    if (remove.disabled) remove.title = w.builtin ? 'Docker 内置网络不能删除' : '仍有容器连接此网络';
    cells(t.body, name, `${w.driver} · ${w.scope}`, (w.subnets || []).join('\n') || '—', w.containers, ops(remove));
  });
  n.append(rows.length ? t.n : empty('没有网络'), note('删除不会强制断开容器。编排中的网络随编排一起创建和删除。'));
  return n;
}
async function volumesPage() {
  const rows = await api('/volumes'), n = el('div'), t = table(['卷', '大小', '状态', '创建时间', '操作']);
  action(button('刷新', render, 'secondary', 'refresh'), button('创建卷', () => {
    const name = field('卷名称', '', { max: 64, placeholder: 'app-data', help: '字母或数字开头，只含字母、数字、下划线、点和中划线。' });
    formModal('创建卷', [name], '创建', () => mutation('/volumes', { name: name.val() }));
  }, 'primary', 'plus'));
  rows.forEach(v => {
    const tags = el('div', 'tag-row');
    if (v.panel_app) tags.append(tag('面板应用', 'blue')); else if (v.project) tags.append(tag(`项目 ${v.project}`, 'blue'));
    const name = titleCell(v.name, v.driver, 'database'); name.lastChild.append(tags);
    const status = v.in_use === undefined ? tag('未知') : v.in_use ? tag(`使用中 · ${v.containers} 个容器`, 'ok') : tag('未使用');
    const text = v.panel_app ? `${v.name} 属于面板应用，存放着应用数据。删除后数据无法恢复；如仍需要，请先在“备份”页备份该应用。` : `将删除卷 ${v.name} 中的全部数据，无法恢复。仍被容器（包括已停止的容器）使用的卷会拒绝删除。`;
    const remove = button('删除', () => confirm(`删除卷 ${v.name}`, text, () => mutation(`/volumes/${encodeURIComponent(v.name)}`, undefined, 'DELETE'), true), 'quiet small');
    if (v.in_use) { remove.disabled = true; remove.title = '仍有容器使用此卷'; }
    cells(t.body, name, v.size === undefined || v.size < 0 ? '—' : bytes(v.size), status, v.created ? date(v.created) : '—', ops(remove));
  });
  n.append(rows.length ? t.n : empty('没有数据卷'), note('删除卷不可恢复，面板不会强制删除仍被使用的卷。大小来自 Docker 的统计，可能有延迟。'));
  return n;
}
async function auditRows(kind, headers, row) {
  const rows = await api(`/audit?kind=${kind}`), n = el('div'), t = table(headers);
  action(button('刷新', render, 'secondary', 'refresh'));
  rows.forEach(e => cells(t.body, ...row(e)));
  n.append(rows.length ? t.n : empty('还没有记录'), note('日志保存在网页服务的数据目录，超过 2 MB 时轮换一次，最多显示最近 500 条。只记录操作对象，不记录密码、SQL、文件内容或编排内容。'));
  return n;
}
const auditResultTag = e => tag(e.result === 'ok' ? '成功' : '失败', e.result === 'ok' ? 'ok' : 'bad');
function auditPage() { return auditRows('operation', ['时间', '操作', '对象', '来源 IP', '结果'], e => [date(e.time), titleCell(e.action, `${e.method} ${e.path}`, 'activity'), e.detail || '—', el('code', '', e.ip), auditResultTag(e)]); }
function loginsPage() { return auditRows('login', ['时间', '事件', '来源 IP', '结果'], e => [date(e.time), e.action, el('code', '', e.ip), auditResultTag(e)]); }
async function accessPage() {
  const data = await api('/access'), n = el('div', 'security-grid'), here = location.hostname;
  action(button('刷新', render, 'secondary', 'refresh'));
  const current = panel('公网入口', data.running ? tag('调整中', 'blue') : tag(data.entries.length ? `${data.entries.length} 个入口` : '未开放', data.entries.length ? 'ok' : ''));
  if (!data.managed && !data.entries.length) current.append(el('p', 'muted', '还没有通过面板或安装脚本开放公网入口，目前只能通过 SSH 隧道访问。'));
  data.entries.forEach(e => {
    const r = el('div', 'resource-row'), left = el('div', 'resource-left'), text = el('div');
    text.append(el('strong', 'resource-name', e.address), el('small', 'muted', e.kind === 'domain' ? '域名 · 自动申请正式证书' : 'IP · 自签名证书，浏览器会提示不安全'));
    left.append(icon(e.kind === 'domain' ? 'globe' : 'network'), text);
    const right = el('div', 'table-ops'); if (e.address === here) right.append(tag('当前正在使用', 'blue'));
    right.append(link(`https://${e.address}`, '打开 ↗'));
    r.append(left, right); current.append(r);
  });
  const domain = data.entries.find(e => e.kind === 'domain'), ip = data.entries.find(e => e.kind === 'ip'), busy = data.running || !data.supported;
  const controls = el('div', 'security-actions');
  controls.append(button('设置入口', () => accessForm(data), 'primary', 'globe'));
  if (domain && ip) {
    controls.append(button('移除域名入口', () => accessChange({ action: 'remove-domain' }, `将只保留 IP 入口 ${ip.address}。`, domain.address === here), 'secondary'));
    controls.append(button('移除 IP 入口', () => accessChange({ action: 'remove-ip' }, `将只保留域名入口 ${domain.address}。`, ip.address === here), 'secondary'));
  }
  if (data.entries.length) controls.append(button('关闭公网入口', () => accessChange({ action: 'disable' }, '关闭后只能通过 SSH 隧道访问面板。', location.protocol === 'https:'), 'quiet danger-text'));
  $$('button', controls).forEach(b => b.disabled = busy);
  current.append(controls);
  if (!data.supported) current.append(note('此服务器没有 systemd-run，无法在面板中调整入口；请通过 SSH 运行 public-access.sh。', true));
  else if (!data.managed && data.entries.length === 0) current.append(note('设置入口会修改 Caddy 配置并检查后重载；失败时自动恢复原配置。'));
  const help = panel('说明');
  [['域名', '先把域名的 A 记录指向访问者实际连接的服务器 IP（使用中转线路时指向线路机），面板会在应用前检查解析，并由 Caddy 自动申请证书。'], ['IP', '只能是本机可直接到达的公网 IPv4，使用自签名证书。'], ['同时使用', '可以同时保留一个域名和一个 IP；“只保留新填写的入口”会移除其它入口。'], ['安全', '每次修改都需要再次输入密码，并记录在操作日志中。修改正在使用的入口后，需要用新地址重新打开面板。']].forEach(([title, text]) => { const r = el('div', 'security-list'); r.append(el('strong', '', title), el('p', 'muted', text)); help.append(r); });
  n.append(current, help);
  return n;
}
function accessForm(data) {
  const domain = data.entries.find(e => e.kind === 'domain'), ip = data.entries.find(e => e.kind === 'ip');
  const d = field('域名', domain ? domain.address : '', { required: false, max: 253, placeholder: 'panel.example.com', help: '不带 https:// 和端口；留空表示不修改域名入口。' });
  const i = field('公网 IPv4', ip ? ip.address : '', { required: false, max: 15, placeholder: '203.0.113.5', help: '留空表示不修改 IP 入口。' });
  const keep = field('其它入口', 'keep', { select: [['keep', '保留其它入口'], ['only', '只保留新填写的入口']] });
  const dns = field('域名解析检查', 'check', { select: [['check', '应用前检查解析到本机'], ['skip', '跳过检查（使用中转线路或 CDN 时）']] });
  formModal('设置公网入口', [d, i, keep, dns], '下一步', () => {
    const req = { action: 'set', domain: d.val().trim(), ip: i.val().trim(), only: keep.val() === 'only', skip_dns: dns.val() === 'skip' };
    if (!req.domain && !req.ip) throw new Error('请填写域名或 IP');
    const changed = data.entries.some(e => e.address === location.hostname && e.address !== req.domain && e.address !== req.ip && (req.only || (e.kind === 'domain' ? req.domain : req.ip)));
    accessChange(req, `将设置：${[req.domain, req.ip].filter(Boolean).join('、')}${req.only ? '，并移除其它入口' : ''}。`, changed);
  });
}
function accessChange(req, summary, affectsHere) {
  const password = field('当前面板密码', '', { type: 'password', max: 256 }), otp = field('6 位动态码', '', { required: state.totp, max: 6 });
  otp.n.hidden = !state.totp;
  const text = `${summary}${affectsHere ? '\n当前浏览器使用的入口会失效，完成后请用新的地址重新打开面板。' : ''}`;
  formModal('确认修改公网入口', [password, otp], '确认修改', async () => {
    if (state.demo) throw new Error('演示模式不执行服务器变更');
    const job = await api('/access', { ...req, password: password.input.value, otp: otp.val() });
    password.input.value = ''; closeModal(); toast('任务已开始'); showJob(job.id);
  }, { note: text, warning: affectsHere, danger: affectsHere });
}
// A finished job is announced three ways: a banner at the top of its dialog, a toast (also
// when the dialog was closed), and a refresh of the page behind it.
const trackedJobs = new Set();
function trackJob(id) {
  if (trackedJobs.has(id)) return; trackedJobs.add(id);
  let tries = 0;
  const timer = setInterval(async () => {
    if (!state.csrf || ++tries > 900) { clearInterval(timer); return; }
    let rows; try { rows = await api('/jobs'); } catch { return; }
    const j = rows.find(x => x.id === id); if (!j) { clearInterval(timer); return; }
    if (j.state === 'running') return;
    clearInterval(timer);
    toast(j.state === 'success' ? `✓ 已完成：${j.title}` : `✕ 失败：${j.title}`, j.state !== 'success');
    if (!modal.open || modal.dataset.job === id) render().catch(() => {});
  }, 3000);
}
function elapsed(from, to) { const s = Math.max(0, Math.round(((to ? new Date(to) : new Date()) - new Date(from)) / 1000)); return `${Math.floor(s / 60)} 分 ${String(s % 60).padStart(2, '0')} 秒`; }
function jobBanner(j) {
  const kind = j.state === 'success' ? 'success' : j.state === 'failed' ? 'failed' : 'running', n = el('div', `job-banner ${kind}`), mark = el('span', 'job-mark', kind === 'success' ? '✓' : kind === 'failed' ? '✕' : ''), text = el('div');
  const head = kind === 'success' ? '已完成' : kind === 'failed' ? '失败' : '正在进行', sub = kind === 'running' ? `已用时 ${elapsed(j.started)}。完成后这里会变成绿色的“已完成”，也可以先关闭此窗口，完成时会弹出提示。` : kind === 'success' ? `用时 ${elapsed(j.started, j.ended)}，可以关闭此窗口。` : (j.error || '请查看下面的输出了解原因。');
  if (kind === 'running') mark.classList.add('spin');
  text.append(el('strong', '', `${head}：${j.title}`), el('small', '', sub)); n.append(mark, text); n.setAttribute('role', 'status'); return n;
}
async function showJob(id) {
  const n = el('div'); let banner = el('div'); const status = el('div', 'log-toolbar'), out = el('pre', 'log-view', '正在读取任务…'), done = button('关闭', closeModal);
  n.append(banner, status, out); openModal('任务详情', n, [done], true); modal.dataset.job = id;
  const load = async () => {
    const rows = await api('/jobs'), j = rows.find(j => j.id === id);
    if (!j) { out.textContent = '任务已不在当前记录中，可能因服务重启或记录轮换被清除。'; clearInterval(modalTimer); return; }
    $('#modal-title').textContent = j.state === 'success' ? '任务已完成' : j.state === 'failed' ? '任务失败' : '任务进行中';
    if (j.state === 'running') trackJob(id);
    banner.replaceWith(banner = jobBanner(j));
    status.replaceChildren(jobTag(j), el('span', 'muted', `${date(j.started)}${j.ended ? ' → ' + date(j.ended) : ''}`));
    out.textContent = [j.output, j.error].filter(Boolean).join('\n\n') || (j.state === 'running' ? '任务正在执行…' : '操作已完成。');
    out.scrollTop = out.scrollHeight;
    if (j.state !== 'running') { clearInterval(modalTimer); modalTimer = null; done.textContent = j.state === 'success' ? '完成' : '关闭'; if (j.state === 'success') done.className = 'button primary'; }
  };
  try { await load(); if (!modalTimer && banner.classList.contains('running')) modalTimer = setInterval(() => load().catch(e => { out.textContent = e.message; }), 1500); } catch (e) { out.textContent = e.message; }
}
async function securityPage() {
  const session=await api('/session');state.totp=session.totp;const n=el('div','security-grid'),account=panel('管理员账号',tag('单管理员','blue'));account.append(line('账号',state.user),line('会话有效期','闲置 30 分钟 / 最长 8 小时'),line('两步验证',state.totp?'已开启':'未开启'));const actions=el('div','security-actions');actions.append(button('修改密码',passwordForm,'secondary','lock'),button(state.totp?'关闭两步验证':'启用两步验证',totpForm,'primary','shield'));account.append(actions);
  const boundaries=panel('访问与权限');[['访问入口',`网页服务监听 127.0.0.1；当前浏览器通过${accessLabel()}进入。`],['网页服务','独立低权限账号，不直接连接 Docker socket。'],['本机代理','通过私有 Unix socket 接收受限接口请求；代理拥有 root 与 Docker 管理能力。'],['文件范围','文件界面限定在 /srv/junpanel。Docker 容器终端仍有其容器内权限。'],['现有服务','继续使用系统 Caddy 与 Docker；不配置防火墙、自动备份或 SSH 登录方式。']].forEach(([title,text])=>{const r=el('div','security-list');r.append(el('strong','',title),el('p','muted',text));boundaries.append(r);});n.append(account,boundaries,note('管理员可执行数据库 SQL、部署容器并进入容器终端。请把面板账号视为服务器管理凭据；不提供公网开放开关。',true));return n;
}
function passwordForm() { const old=field('当前密码','',{type:'password',max:256}),password=field('新密码','',{type:'password',max:256,help:'至少 8 位。'}),again=field('再次输入新密码','',{type:'password',max:256}),otp=field('6 位动态码','',{required:state.totp,max:6});otp.n.hidden=!state.totp;formModal('修改管理员密码',[old,password,again,otp],'修改并重新登录',async()=>{if(password.input.value!==again.input.value)throw new Error('两次新密码不一致');if(Array.from(password.input.value).length<8)throw new Error('新密码至少 8 位');await api('/security',{action:'password',password:old.input.value,new_password:password.input.value,otp:otp.val()});showLogin();toast('密码已修改，请重新登录');}); }
function totpForm() { const password=field('当前密码','',{type:'password',max:256});if(state.totp){const otp=field('6 位动态码','',{max:6});formModal('关闭两步验证',[password,otp],'关闭并重新登录',async()=>{await api('/security',{action:'totp_disable',password:password.input.value,otp:otp.val()});showLogin();toast('两步验证已关闭，请重新登录');},{warning:true,note:'关闭后将仅通过密码登录。'});return;}
  formModal('启用两步验证',[password],'验证密码',async()=>{const r=await api('/security',{action:'totp_setup',password:password.input.value});const secret=field('在验证器中手动添加此密钥',r.secret,{required:false,help:'选择基于时间（TOTP），每 30 秒更新，6 位数字。此密钥只用于你的验证器。'});secret.input.readOnly=true;const code=field('验证器生成的 6 位动态码','',{max:6});formModal('确认两步验证',[secret,code],'开启并重新登录',async()=>{await api('/security',{action:'totp_enable',otp:code.val()});showLogin();toast('两步验证已开启，请重新登录');},{note:'请先安全保存验证器密钥。丢失时可在服务器本机重置面板账号，重置会同时关闭两步验证。'});});
}
function openSearch() { const d=$('#search-dialog');if(!d.open)d.showModal();$('#search-input').value='';drawSearch();$('#search-input').focus(); }
function drawSearch() { const q=$('#search-input').value.toLowerCase(),results=$('#search-results');results.replaceChildren();Object.entries(routes).filter(([r,v])=>`${r} ${v[0]} ${groupLabel(v[3])} ${v[1]}`.toLowerCase().includes(q)).forEach(([r,v])=>{const b=el('button','search-result'),g=groupLabel(v[3]);b.append(icon(v[2]),el('span','',g===v[0]?v[0]:`${g} / ${v[0]}`),el('small','muted',v[1]));b.onclick=()=>{$('#search-dialog').close();navigate(r);};results.append(b);});state.sites.filter(s=>`${s.name} ${s.domains}`.toLowerCase().includes(q)&&q).forEach(s=>{const b=el('button','search-result');b.append(icon('globe'),el('span','',s.name),el('small','muted',s.domains.join(', ')));b.onclick=()=>{$('#search-dialog').close();navigate('sites');};results.append(b);});state.apps.filter(a=>a.name.toLowerCase().includes(q)&&q).forEach(a=>{const b=el('button','search-result');b.append(icon('apps'),el('span','',a.name));b.onclick=()=>{$('#search-dialog').close();navigate('installed');};results.append(b);});if(!results.children.length)results.append(empty('没有匹配结果')); }
$('#open-search').onclick=openSearch;$('#search-close').onclick=()=>$('#search-dialog').close();$('#search-input').oninput=drawSearch;document.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'&&state.csrf){e.preventDefault();openSearch();}});
$('#access-label').textContent=accessLabel();
$('.login-boundary').lastChild.textContent=location.protocol==='https:'?'HTTPS 加密连接 · 管理员认证':'本机入口 · 远程访问请使用 SSH 隧道';
(async()=>{try{const meta=await api('/meta');state.demo=meta.demo;$('#sidebar-version').textContent=meta.version;$('#footer-version').textContent=`v${meta.version}`;$('#demo-hint').hidden=!meta.demo;if(meta.demo){$('#login-user').value='demo';$('#login-password').value='JunPanelPreview2026!';}try{await enter(await api('/session'));}catch{showLogin();}}catch(e){$('#login-error').textContent=e.message;}})();
function uploadFile(target, file) {
  return new Promise((resolve, reject) => {
    const progress = el('div', 'toast', `正在上传 ${file.name} 0%`), xhr = new XMLHttpRequest();
    $('#toasts').append(progress);
    const finish = () => progress.remove();
    xhr.open('POST', `/api/files/upload?path=${encodeURIComponent(target)}`);
    xhr.setRequestHeader('X-CSRF-Token', state.csrf);
    xhr.setRequestHeader('Content-Type', 'application/octet-stream');
    xhr.timeout = 31 * 60 * 1000;
    xhr.upload.onprogress = e => { if (e.lengthComputable) progress.textContent = `正在上传 ${file.name} ${Math.floor(e.loaded / e.total * 100)}%`; };
    xhr.onload = () => {
      finish(); let value = {}; try { value = JSON.parse(xhr.responseText); } catch {}
      if (xhr.status >= 200 && xhr.status < 300) return resolve(value);
      if (xhr.status === 401) showLogin();
      reject(new Error(value.error || `上传失败 (${xhr.status})`));
    };
    xhr.onerror = () => { finish(); reject(new Error('上传中断，请检查网络后重试')); };
    xhr.ontimeout = () => { finish(); reject(new Error('上传超时')); };
    xhr.send(file);
  });
}
const backupTypes = { site: '网站文件', app: '应用数据', database: '数据库' };
async function backupsPage() {
  const data = await api('/backups'), rows = data.backups, n = el('div');
  action(button('刷新', render, 'secondary', 'refresh'), button('新建备份', newBackup, 'primary', 'plus'));
  n.append(note(`备份保存在服务器的 ${data.directory}（仅 root 可读），与应用数据在同一块磁盘，不能替代异地备份，请及时下载保存。磁盘可用 ${bytes(data.free)}。`, data.free > 0 && data.free < 2 * 1024 ** 3));
  if (!rows.length) { n.append(empty('还没有备份', '点击“新建备份”，为网站文件、应用数据或数据库留一份副本。')); return n; }
  const t = table(['时间', '类型', '对象', '大小', '说明', '操作']);
  rows.forEach(b => {
    const what = el('div'), download = link(`/api/backups/${b.id}/download`, '下载'), buttons = [download];
    what.append(el('strong', '', b.name), el('small', 'muted', b.type === 'database' ? `${b.target} / ${b.database}` : b.target));
    download.removeAttribute('target');
    buttons.push(button('恢复', () => b.type === 'database' ? restoreDatabaseBackup(b) : restoreBackup(b), 'quiet small'));
    buttons.push(button('删除', () => confirm('删除备份', '将永久删除这份备份文件，无法恢复。', () => mutation(`/backups/${b.id}`, undefined, 'DELETE'), true), 'quiet small'));
    cells(t.body, date(b.created), tag(backupTypes[b.type] || b.type), what, bytes(b.size), b.note || '手动备份', ops(...buttons));
  });
  n.append(t.n);
  return n;
}
async function newBackup() {
  const [sites, apps, databases, catalog] = await Promise.all([api('/sites'), api('/apps'), api('/databases'), api('/catalog')]);
  const supported = catalog.filter(t => t.backup).map(t => t.key);
  const options = {
    site: sites.sites.filter(s => s.kind === 'static').map(s => [s.id, `${s.name}（${s.domains[0]}）`]),
    app: apps.filter(a => !a.removed && supported.includes(a.template)).map(a => [a.id, a.name]),
    database: databases.filter(d => d.kind !== 'redis').map(d => [d.id, d.name]),
  };
  const type = field('备份内容', 'site', { select: [['site', '网站文件'], ['app', '应用数据（备份时短暂停止应用）'], ['database', '数据库导出（SQL）']] });
  const target = field('备份对象', '', { select: [] });
  const database = field('数据库名称', 'junpanel', { max: 63, help: '仅字母、数字及下划线。' });
  const fill = () => {
    const kind = type.input.value, list = options[kind];
    target.input.replaceChildren(...list.map(([v, text]) => { const o = el('option', '', text); o.value = v; return o; }));
    database.n.hidden = kind !== 'database'; database.input.required = kind === 'database';
    pick();
  };
  const pick = () => { const d = databases.find(x => x.id === target.input.value); if (d && type.input.value === 'database') database.input.value = d.database || 'junpanel'; };
  type.input.onchange = fill; target.input.onchange = pick; fill();
  formModal('新建备份', [type, target, database], '开始备份', async () => {
    if (!target.input.value) throw new Error('没有可备份的对象');
    await mutation('/backups', { type: type.input.value, target: target.input.value, database: type.input.value === 'database' ? database.val() : '' });
  }, { note: '备份作为后台任务运行，可在任务日志查看进度。应用备份会短暂停止应用以保证数据一致，完成后自动启动。' });
}
function restoreBackup(b) {
  const what = b.type === 'site'
    ? '该网站的全部文件将被替换为备份时的内容，备份之后新增或修改的文件会丢失。'
    : '该应用会先停止，其数据卷将被清空并替换为备份时的数据，再重新启动；备份之后写入的数据会丢失。仅适用于同一台服务器上的同一个应用。';
  confirm('恢复备份', `${what}开始前会自动创建一份“恢复前备份”，恢复失败时可用它回滚。`, () => mutation(`/backups/${b.id}/restore`, { confirm: true }), true);
}
function restoreDatabaseBackup(b) {
  const target = field('恢复到数据库', b.database, { max: 63, help: '默认恢复到原数据库。填写一个新名称，可以先恢复成副本核对，原数据库不受影响。' });
  formModal('恢复数据库备份', [target], '开始恢复', () => mutation(`/backups/${b.id}/restore`, { confirm: true, database: target.val() }), { danger: true, warning: true, note: `来源：${b.name} / ${b.database}，${date(b.created)}。目标数据库会先删除再重建，备份之后写入的数据会丢失；目标已存在时，会先自动做一份“恢复前备份”，恢复失败可以用它回滚。` });
}
const weekdayNames = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
function frequencyText(s) {
  if (s.every === 'daily') return `每天 ${s.at}`;
  if (s.every === 'weekly') return `每${weekdayNames[s.weekday]} ${s.at}`;
  return `每隔 ${s.hours} 小时`;
}
function scheduleWhat(s, apps, sites, databases) {
  if (s.action === 'restart') return ['重启应用', (apps.find(a => a.id === s.target) || {}).name || s.target];
  const label = { site: '备份网站文件', app: '备份应用数据', database: '备份数据库' }[s.type] || '备份';
  const names = { site: (sites.find(x => x.id === s.target) || {}).name, app: (apps.find(a => a.id === s.target) || {}).name, database: (databases.find(d => d.id === s.target) || {}).name };
  return [label, (names[s.type] || s.target) + (s.type === 'database' ? ` / ${s.database}` : '') + `，保留 ${s.keep} 份`];
}
async function schedulesPage() {
  const [data, sites, apps, databases, catalog] = await Promise.all([api('/schedules'), api('/sites'), api('/apps'), api('/databases'), api('/catalog')]), rows = data.schedules, n = el('div');
  state.catalog = catalog;
  const siteRows = sites.sites;
  action(button('刷新', render, 'secondary', 'refresh'), button('新建计划任务', () => scheduleForm(null, data, siteRows, apps, databases), 'primary', 'plus'));
  n.append(note(`时间按服务器本地时区（${data.zone}）计算。到点时若面板在停机，开机后会补跑一次；应用正忙时会每 10 分钟重试，最多 3 次。定时备份会短暂停止应用。计划任务只支持备份和重启应用，不能执行任意命令。`));
  if (!rows.length) { n.append(empty('还没有计划任务', '点击“新建计划任务”，让面板按时间表备份网站、应用或数据库。')); return n; }
  const t = table(['名称', '内容', '频率', '下次运行', '最近一次', '操作']);
  rows.forEach(s => {
    const [kind, detail] = scheduleWhat(s, apps, siteRows, databases), what = el('div'), name = el('div'), last = el('div');
    name.append(el('strong', '', s.name), el('small', 'muted', s.enabled ? '已启用' : '已停用'));
    what.append(el('strong', '', kind), el('small', 'muted', detail));
    if (s.last) {
      const cls = { success: 'ok', failed: 'bad', running: 'blue' }[s.last.status] || '';
      last.append(tag({ success: '成功', failed: '失败', running: '运行中' }[s.last.status] || s.last.status, cls), el('small', 'muted', `${date(s.last.at)}${s.last.message ? ' · ' + s.last.message : ''}`));
    } else last.append(el('span', 'muted', '尚未运行'));
    cells(t.body, name, what, frequencyText(s), s.enabled ? date(s.next) : '—', last, ops(
      button('立即运行', () => mutation(`/schedules/${s.id}/run`, {}), 'quiet small'),
      button('编辑', () => scheduleForm(s, data, siteRows, apps, databases), 'quiet small'),
      button(s.enabled ? '停用' : '启用', () => mutation(`/schedules/${s.id}/toggle`, {}), 'quiet small'),
      button('删除', () => confirm('删除计划任务', `将删除“${s.name}”。它已创建的备份会保留，之后不再自动清理。`, () => mutation(`/schedules/${s.id}`, undefined, 'DELETE'), true), 'quiet small')));
  });
  n.append(t.n);
  return n;
}
function scheduleForm(current, data, sites, apps, databases) {
  const supported = state.catalog.filter(t => t.backup).map(t => t.key);
  const live = apps.filter(a => !a.removed);
  const targets = {
    site: sites.filter(s => s.kind === 'static').map(s => [s.id, `${s.name}（${s.domains[0]}）`]),
    app: live.filter(a => supported.includes(a.template)).map(a => [a.id, a.name]),
    database: databases.filter(d => d.kind !== 'redis').map(d => [d.id, d.name]),
    restart: live.map(a => [a.id, a.name]),
  };
  const c = current || { action: 'backup', type: 'site', keep: 7, every: 'daily', at: '03:00', weekday: 1, hours: 24 };
  const name = field('名称', c.name || '', { max: 60, placeholder: '例如：每晚备份博客' });
  const what = field('任务', c.action === 'restart' ? 'restart' : c.type, { select: [['site', '备份网站文件'], ['app', '备份应用数据（短暂停止应用）'], ['database', '备份数据库（SQL）'], ['restart', '重启应用']] });
  const target = field('对象', c.target || '', { select: [] });
  const database = field('数据库名称', c.database || 'junpanel', { max: 63, help: '仅字母、数字及下划线。' });
  const keep = field('保留份数', String(c.keep || 7), { type: 'number', help: '只清理这个计划任务创建的备份，超出的旧备份会被删除；手动备份不受影响。' });
  const every = field('频率', c.every, { select: [['daily', '每天'], ['weekly', '每周'], ['interval', '每隔几小时']] });
  const time = field('时间', c.at || '03:00', { type: 'time', help: `服务器本地时区：${data.zone}` });
  const weekday = field('星期', String(c.weekday ?? 1), { select: weekdayNames.map((n, i) => [String(i), n]) });
  const hours = field('间隔（小时）', String(c.hours || 24), { type: 'number', help: '1–168，从每次开始运行时计算。' });
  const refresh = () => {
    const kind = what.input.value;
    target.input.replaceChildren(...targets[kind].map(([v, text]) => { const o = el('option', '', text); o.value = v; return o; }));
    if (current && (current.action === 'restart' ? 'restart' : current.type) === kind) target.input.value = current.target;
    database.n.hidden = kind !== 'database'; database.input.required = kind === 'database';
    keep.n.hidden = kind === 'restart';
    const d = databases.find(x => x.id === target.input.value); if (kind === 'database' && d && !(current && current.target === d.id)) database.input.value = d.database || 'junpanel';
    const f = every.input.value;
    time.n.hidden = f === 'interval'; time.input.required = f !== 'interval';
    weekday.n.hidden = f !== 'weekly'; hours.n.hidden = f !== 'interval'; hours.input.required = f === 'interval';
  };
  what.input.onchange = refresh; target.input.onchange = refresh; every.input.onchange = refresh; refresh();
  formModal(current ? '编辑计划任务' : '新建计划任务', [name, what, target, database, keep, every, time, weekday, hours], current ? '保存' : '创建', async () => {
    if (!target.input.value) throw new Error('没有可选的对象');
    const kind = what.input.value, f = every.input.value;
    await mutation('/schedules', {
      id: current ? current.id : '', name: name.val(), action: kind === 'restart' ? 'restart' : 'backup', type: kind === 'restart' ? '' : kind,
      target: target.input.value, database: kind === 'database' ? database.val() : '', keep: kind === 'restart' ? 0 : Number(keep.val()),
      every: f, at: f === 'interval' ? '' : time.val(), weekday: f === 'weekly' ? Number(weekday.input.value) : 0, hours: f === 'interval' ? Number(hours.val()) : 0,
    });
  });
}
function setAlertBadge(count) { const b = $('#alert-badge'); if (!b) return; b.hidden = !count; b.textContent = count || ''; }
const alertKinds = { triggered: ['告警', 'bad'], reminder: ['仍未解决', 'warn'], resolved: ['已恢复', 'ok'] };
async function alertsPage() {
  const data = await api('/alerts'), cfg = data.config, n = el('div');
  setAlertBadge(data.active.length);
  action(button('立即检查', async () => { await api('/alerts/check', {}); toast('检查完成'); await render(); }, 'secondary', 'refresh'));

  const current = panel('当前告警', tag(data.active.length ? `${data.active.length} 条` : '一切正常', data.active.length ? 'bad' : 'ok'));
  if (!data.active.length) current.append(el('p', 'muted', '没有未解决的告警。面板每 30 秒检查一次。'));
  data.active.forEach(a => {
    const row = el('div', 'resource-row'), left = el('div', 'resource-left'), text = el('div', 'resource-name');
    text.append(el('strong', '', a.title), el('small', '', a.detail), el('small', '', `开始于 ${date(a.since)}`)); left.append(text);
    row.append(left, tag(a.severity === 'critical' ? '严重' : '警告', a.severity === 'critical' ? 'bad' : 'warn')); current.append(row);
  });
  n.append(current);

  const form = el('form'), delivery = data.delivery;
  const format = field('通知格式', cfg.format, { select: Object.entries(data.formats) });
  const url = field('Webhook 地址', '', { required: false, placeholder: cfg.url_set ? `已设置（${cfg.url_host}），留空则保持不变` : 'https://…', help: '地址常含密钥，保存后不再显示。企业微信、钉钉、飞书填机器人地址；钉钉的“加签”不支持，请用“自定义关键词”并包含“JunPanel”。' });
  const notify = field('发送通知', cfg.notify ? '1' : '0', { select: [['1', '开启'], ['0', '关闭（只在面板里显示）']] });
  const disk = field('磁盘空间', cfg.disk_on ? '1' : '0', { select: [['1', '监控'], ['0', '不监控']] }), diskPercent = field('磁盘告警阈值（已用 %）', String(cfg.disk_percent), { type: 'number' });
  const cert = field('证书到期', cfg.cert_on ? '1' : '0', { select: [['1', '监控'], ['0', '不监控']] }), certDays = field('证书提前提醒（天）', String(cfg.cert_days), { type: 'number', help: '有效期不足 30 天的短期证书（如 IP 证书）只在最后 1/5 有效期内提醒。' });
  const container = field('容器停止', cfg.container_on ? '1' : '0', { select: [['1', '监控'], ['0', '不监控']] });
  const service = field('Docker 与 Caddy 服务', cfg.service_on ? '1' : '0', { select: [['1', '监控'], ['0', '不监控']] });
  const repeat = field('未解决时的重复提醒间隔（小时）', String(cfg.repeat_hours), { type: 'number' });
  const error = el('div', 'form-error'), save = button('保存设置', () => form.requestSubmit(), 'primary');
  const test = button('发送测试通知', async () => { await api('/alerts/test', {}); toast('测试通知已发送，请到接收端确认'); }, 'secondary');
  form.className = 'form-grid'; url.n.classList.add('span-2'); error.classList.add('span-2');
  form.append(format.n, notify.n, url.n, disk.n, diskPercent.n, cert.n, certDays.n, container.n, service.n, repeat.n, error);
  form.onsubmit = async e => {
    e.preventDefault(); error.textContent = ''; save.disabled = true;
    try {
      await api('/alerts/config', { format: format.input.value, url: url.val(), clear_url: false, notify: notify.input.value === '1', disk_on: disk.input.value === '1', disk_percent: Number(diskPercent.val()), cert_on: cert.input.value === '1', cert_days: Number(certDays.val()), container_on: container.input.value === '1', service_on: service.input.value === '1', repeat_hours: Number(repeat.val()) });
      toast('设置已保存'); await render();
    } catch (err) { error.textContent = err.message; } finally { save.disabled = false; }
  };
  const settings = panel('通知与规则', el('div', 'table-ops'));
  settings.querySelector('.table-ops').append(test, save);
  const hint = el('p', 'muted', '提示：手动执行 docker stop 的容器会提醒一次；面板内的停止、重启、备份操作不会。退出码为 0 的正常退出不提醒。');
  hint.style.margin = '0 0 16px';
  if (delivery) { const last = note(`最近一次通知（${date(delivery.at)}）：${delivery.message}`, !delivery.ok); last.style.marginBottom = '12px'; settings.append(last); }
  settings.append(hint, form);
  n.append(settings);

  const historyPanel = panel('最近事件');
  if (!data.history.length) historyPanel.append(el('p', 'muted', '还没有告警事件。'));
  else {
    const t = table(['时间', '类型', '内容']);
    data.history.slice(0, 30).forEach(h => { const what = el('div'); what.append(el('strong', '', h.title), el('small', 'muted', h.detail || '')); const [label, cls] = alertKinds[h.kind] || [h.kind, '']; cells(t.body, date(h.time), tag(label, cls), what); });
    historyPanel.append(t.n);
  }
  n.append(historyPanel);
  return n;
}
async function certsPage() {
  const certs = await api('/certificates'), n = el('div');
  action(button('刷新', render, 'secondary', 'refresh'));
  n.append(note('证书由 Caddy 自动签发和续期，无需手动操作。到期提醒可在“告警”中设置。'));
  if (!certs.certificates.length) { n.append(empty(certs.found ? '没有证书' : '未找到 Caddy 证书目录', certs.found ? 'Caddy 当前没有为已配置的网站保存证书。' : '读取 /var/lib/caddy 下的证书失败，可能 Caddy 使用了自定义数据目录。')); return n; }
  const t = table(['域名', '签发者', '到期时间', '剩余', '状态']);
  certs.certificates.forEach(c => cells(t.body, c.name, c.issuer, date(c.not_after), c.days_left < 0 ? '已过期' : `${c.days_left} 天`, tag(c.days_left < 0 ? '已过期' : c.warn ? '即将到期' : '正常', c.warn || c.days_left < 0 ? 'bad' : 'ok')));
  n.append(t.n); return n;
}
function setUpgradeBadge(ver) { const b = $('#upgrade-badge'); if (!b) return; b.hidden = !ver; b.textContent = ver ? '新' : ''; }
async function upgradePage() {
  const d = await api('/upgrade'), n = el('div'), latest = d.latest;
  setUpgradeBadge(d.available ? latest.version : '');
  action(button('立即检查', async () => { const r = await api('/upgrade/check', {}); if (r.check_error) toast(`检查更新失败：${r.check_error}`, true); else if (r.available && r.latest) toast(`发现新版本 v${r.latest.version}（当前 v${r.current}），可以升级`); else toast(`已是最新版本 v${r.current}，没有可用更新`); await render(); }, 'secondary', 'refresh'));
  const versions = panel('版本', tag(d.available ? '有新版本' : '已是最新', d.available ? 'blue' : 'ok'));
  const rows = [['当前版本', `v${d.current}`], ['最新版本', latest ? `v${latest.version}` : '尚未检查'], ['上次检查', d.checked_at && d.checked_at !== '0001-01-01T00:00:00Z' ? date(d.checked_at) : '—']];
  const list = el('dl', 'details'); rows.forEach(([k, v]) => { list.append(el('dt', '', k), el('dd', '', v)); }); versions.append(list);
  if (d.check_error) versions.append(note(`检查更新失败：${d.check_error}`, true));
  const every = [[1, '每小时'], [3, '每 3 小时'], [6, '每 6 小时（默认）'], [12, '每 12 小时'], [24, '每天'], [72, '每 3 天'], [168, '每周']], hours = d.interval_hours || 6;
  const auto = field('自动检查更新', d.auto_check ? String(hours) : 'off', { select: [...every.map(([h, t]) => [String(h), t]), ['off', '关闭']], help: '检查只读取 GitHub 上公开的版本信息，不会发送你的服务器信息。关闭后仍可手动检查；手动检查的结果会立即提示。' });
  let chosen = hours;
  auto.input.onchange = async () => { try { const off = auto.input.value === 'off'; if (!off) chosen = Number(auto.input.value); await api('/upgrade/config', { auto_check: !off, interval_hours: chosen }); toast(off ? '已关闭自动检查更新' : `已设置：${every.find(([h]) => h === chosen)[1]}检查一次更新`); } catch (e) { toast(e.message, true); } };
  versions.append(auto.n); n.append(versions);

  if (d.run) {
    const cls = { running: 'blue', done: 'ok', failed: 'bad' }[d.run.state] || '', label = { running: '进行中', done: '已完成', failed: '失败' }[d.run.state] || d.run.state;
    const run = panel(`最近一次升级（v${d.run.target}）`, tag(label, cls));
    run.append(el('p', 'muted', `${d.run.message || ''}${d.run.started ? ' · 开始于 ' + date(d.run.started) : ''}`));
    if (d.log && d.run.state !== 'done') run.append(el('pre', 'log-view', d.log));
    n.append(run);
  }
  if (!d.available) { n.append(note(latest ? '当前已是最新版本。' : '还没有检查过更新，点击右上角“立即检查”。')); return n; }

  const up = panel(`新版本 v${latest.version}`, tag('可升级', 'blue'));
  if (latest.notes) { const notes = el('pre', 'log-view', latest.notes); up.append(notes); }
  const full = el('p', 'muted', '完整的发布说明：'); full.append(link(d.notes_url, d.notes_url)); up.append(full);
  up.append(note('升级会下载安装包并用发布时固定的 SHA-256 校验，通过后才安装；面板和管理代理会重启约 1 分钟，现有应用和 Caddy 不受影响，登录会话会清空，账号、数据、备份与计划任务保留。安装失败会自动回滚。'));
  if (!d.supported) up.append(note(d.unsupported_reason, true));
  d.blockers.forEach(b => up.append(note(b + '。请等它结束后再升级。', true)));
  const go = button(`升级到 v${latest.version}`, () => startUpgrade(latest.version), 'primary', 'refresh');
  go.disabled = !d.can_upgrade; up.append(go); n.append(up);
  return n;
}
function startUpgrade(target) {
  confirm(`升级到 v${target}`, '升级会下载并校验安装包，然后重启面板和管理代理，约 1 分钟后恢复；现有应用和 Caddy 不受影响，登录会话会清空，安装失败会自动回滚。升级期间请不要关闭此页面。', async () => {
    const result = await api('/upgrade', {});
    closeModal(); watchUpgrade(target, result.id);
  });
}
function watchUpgrade(target, jobId) {
  const steps = ['下载', '校验', '解包', '安装', '重启'], stageIndex = { download: 0, verify: 1, unpack: 2, install: 3 };
  const content = el('div', 'upgrade-progress'), bar = el('div', 'bar'), fill = el('div', 'bar-fill'), percent = el('strong', 'bar-percent', '0%');
  bar.setAttribute('role', 'progressbar'); bar.setAttribute('aria-valuemin', '0'); bar.setAttribute('aria-valuemax', '100'); bar.append(fill);
  const head = el('div', 'bar-head'), label = el('span', '', '正在下载并校验安装包…'); head.append(label, percent);
  const list = el('ol', 'steps'), items = steps.map(s => { const li = el('li', '', s); list.append(li); return li; });
  const out = el('pre', 'log-view', ''), details = el('details', 'upgrade-log'); details.append(el('summary', '', '查看详细输出'), out);
  const banner = el('div', 'job-banner running'), mark = el('span', 'job-mark spin'), bannerText = el('div'), bannerHead = el('strong'), bannerSub = el('small');
  bannerText.append(bannerHead, bannerSub); banner.append(mark, bannerText); banner.setAttribute('role', 'status');
  const setBanner = (kind, headText, sub) => { banner.className = `job-banner ${kind}`; mark.className = kind === 'running' ? 'job-mark spin' : 'job-mark'; mark.textContent = kind === 'success' ? '✓' : kind === 'failed' ? '✕' : ''; bannerHead.textContent = headText; bannerSub.textContent = sub; };
  setBanner('running', `正在升级到 v${target}`, '面板重启期间连接会短暂中断，属于正常现象，请不要关闭此页面。完成后这里会变成绿色的“升级完成”。');
  content.append(banner, head, bar, list, details);
  openModal(`升级到 v${target}`, content, [button('关闭', closeModal)]);
  let shown = 0, current = 0, restartedAt = 0;
  const draw = (value, text) => {
    shown = Math.max(shown, Math.min(100, Math.round(value))); fill.style.width = `${shown}%`; percent.textContent = `${shown}%`; bar.setAttribute('aria-valuenow', String(shown));
    if (text) label.textContent = text;
    items.forEach((li, i) => { li.className = i < current ? 'done' : i === current ? 'active' : ''; });
  };
  draw(1);
  const started = Date.now(); let phase = 1;
  const timer = setInterval(async () => {
    try {
      if (phase === 1) {
        let jobs, status; try { [jobs, status] = await Promise.all([api('/jobs'), api('/upgrade')]); } catch { phase = 2; restartedAt = Date.now(); current = 4; draw(shown, '管理代理正在重启…'); out.textContent += '\n管理代理正在重启…'; return; }
        const j = jobs.find(x => x.id === jobId); if (!j) { phase = 2; restartedAt = Date.now(); current = 4; return; }
        out.textContent = j.output || '正在下载并校验安装包…';
        const run = status.run;
        if (run && run.target === target && run.stage in stageIndex) { current = stageIndex[run.stage]; draw(run.percent, ['正在下载安装包…', '正在校验 SHA-256…', '正在解包并核对版本…', '正在运行安装器…'][current]); }
        if (j.state === 'failed') { clearInterval(timer); label.textContent = '升级失败'; fill.classList.add('failed'); setBanner('failed', '升级失败', j.error || '请查看详细输出，原版本不受影响。'); out.textContent += `\n升级失败：${j.error}`; details.open = true; return; }
        if (j.state === 'success') { phase = 2; restartedAt = Date.now(); current = 3; draw(Math.max(shown, 78), '安装器已启动，等待面板重启…'); }
      } else {
        // The installer reports nothing while the services restart, so the bar eases toward 97%.
        const waited = (Date.now() - restartedAt) / 1000; draw(78 + 19 * (1 - Math.exp(-waited / 25)), current === 4 ? '正在重启面板…' : '安装器运行中，等待面板重启…');
        const r = await fetch('/api/meta', { signal: AbortSignal.timeout(4000), cache: 'no-store' }).catch(() => null);
        const meta = r && r.ok ? await r.json().catch(() => null) : null;
        if (meta && meta.version === target) { clearInterval(timer); current = steps.length; draw(100, `已升级到 v${target}，正在重新加载…`); setBanner('success', `升级完成：已是 v${target}`, '页面即将自动刷新为新版本。'); out.textContent += `\n已升级到 v${target}，正在重新加载…`; setTimeout(() => location.reload(), 1500); return; }
        if (!meta && current < 4) current = 4;
        if (Date.now() - started > 8 * 60 * 1000) { clearInterval(timer); fill.classList.add('failed'); label.textContent = '等待超时'; setBanner('failed', '等待超时', '没有等到新版本启动。请刷新页面查看结果，或查看下面的说明。'); out.textContent += '\n等待超时。请刷新页面后到“版本升级”页查看结果和安装日志；如果面板打不开，请通过 SSH 查看 journalctl -u junpanel-agent -u junpanel-web。'; details.open = true; }
      }
    } catch (e) { out.textContent += `\n${e.message}`; }
  }, 1000);
  // The confirmation dialog closed just before this one opened, and its 'close' event is delivered
  // asynchronously, so only start listening after that has passed.
  setTimeout(() => modal.addEventListener('close', () => clearInterval(timer), { once: true }), 200);
}
