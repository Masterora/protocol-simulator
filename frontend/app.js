let nativeContext = null,
  nativeDesktop = false;
// Snapshots and channel configurations are JSON data from the Go bridge.
const cloneBridgeData = (value) =>
  typeof globalThis.structuredClone === 'function'
    ? globalThis.structuredClone(value)
    : JSON.parse(JSON.stringify(value));
// Older macOS WebKit supports validated button submission before requestSubmit.
function submitWithButton(form, button) {
  if (typeof form.requestSubmit === 'function') form.requestSubmit(button);
  else button?.click();
}
const $ = (id) => document.getElementById(id);
const escape = (value) =>
  String(value ?? '').replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
  );
const names = {
  iec104: 'IEC104',
  'modbus-tcp': 'Modbus TCP',
  'modbus-rtu': 'Modbus RTU',
  raw: 'RAW',
};
const kinds = {
  mc: '脉冲',
  position: '步位置',
  yk: '遥控',
  yt: '遥调',
  step: '步调节',
  yx: '遥信',
  yc: '遥测',
  coil: '线圈',
  discrete: '离散量',
  holding: '保持寄存器',
  input: '输入寄存器',
};
const statuses = {
  stopped: '已停止',
  connecting: '等待连接',
  connected: '已连接',
  listening: '监听中',
  disconnected: '已断开',
  error: '连接失败',
};
const active = (s) => ['connected', 'connecting', 'listening'].includes(s.status);
const transferReady = (s, protocol) =>
  s?.status === 'connected' && (protocol === 'iec104' ? (s.readyPeers ?? s.peers) : s.peers) > 0;
const POINT_PAGE_SIZE = 100;
const keyOf = (p) => `${p.kind}:${p.address}`;
let snapshot = { project: { name: '工程1', channels: [] }, states: [], messages: [] },
  selected = '',
  selectedDevice = '',
  busy = null,
  refreshing = null;
let sidebarKey = '',
  actionKey = '',
  pointKey = '',
  messageKey = '',
  pointPage = 0,
  paused = false,
  frozen = [],
  shownMessages = [],
  toastTimer;
let controlEditor = null,
  controlSelection = null,
  updateChecking = false;
const runtimeViews = new Map(),
  runtimeDetailContexts = new Map();
let runtimePollBusy = false,
  runtimeDetailRequest = 0;
let pointEditor = null,
  confirmResolver = null,
  projectIdentity = '';
let stationMode = 'client',
  deviceKey = 'initial',
  pointTabsKey = '';
const pausedViews = new Map();
const drafts = new Map();
const channel = () => snapshot.project.channels.find((c) => c.id === selected);
const device = () => channel()?.devices?.find((d) => d.id === selectedDevice);
const current = () => {
  const c = channel(),
    d = device();
  return c
    ? {
        ...c,
        points: d?.points || [],
        unit: d?.unit ?? c.unit,
        commonAddress: d?.commonAddress ?? c.commonAddress,
      }
    : null;
};
const draftKey = () =>
  selected +
  '/' +
  (selectedDevice || 'raw') +
  '/' +
  ($('protocol-actions').dataset.category || $('point-kind').value);
function normalizeSnapshot() {
  for (const c of snapshot.project.channels) {
    if (!Array.isArray(c.devices))
      c.devices =
        c.protocol === 'raw'
          ? []
          : [
              {
                id: 'device-1',
                name: '装置 1',
                unit: c.unit,
                commonAddress: c.commonAddress,
                points: c.points || [],
              },
            ];
  }
}
const state = () =>
  snapshot.states.find((s) => s.id === selected) || { status: 'stopped', peers: 0, tx: 0, rx: 0 };
const field = (form, name) => form.elements.namedItem(name);
function toast(text, error = false) {
  $('toast').textContent = text;
  $('toast').className = `visible${error ? ' error' : ''}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => ($('toast').className = ''), error ? 6000 : 2500);
}
function report(error, form) {
  revealField(error?.input);
  if (form?.id === 'device-form')
    globalThis.deviceEditor.showError($('original-device-panel'), error);
  const text = String(error?.message || error);
  const rowNumber = /第(\d+)[行组条]/.exec(text),
    row = rowNumber ? form?.querySelectorAll('tbody tr')[Number(rowNumber[1]) - 1] : null;
  if (!error?.input && row) {
    const key = /步长/.test(text)
      ? 'step'
      : /周期/.test(text)
        ? 'intervalMs'
        : /上限/.test(text)
          ? 'max'
          : /下限/.test(text)
            ? 'min'
            : /目标/.test(text)
              ? 'target'
              : 'points';
    const input =
      row.querySelector('[data-data-field="' + key + '"],[data-remote-field="' + key + '"]') ||
      row.querySelector('input:not([disabled]),select:not([disabled])');
    input?.focus();
  }
  const box = form?.querySelector('.form-error');
  if (box) {
    box.textContent = text;
    box.hidden = false;
    error?.input?.focus();
  } else toast(text, true);
}
function api(method, ...args) {
  const bridge = window.go?.main?.App;
  if (nativeContext?.kind) return bridge.CallNative(method, args);
  if (!bridge?.[method]) return Promise.reject(Error('桌面服务未就绪'));
  return bridge[method](...args);
}
// Each window holds cursors, never another copy of the whole engineering model.
let viewCursor = {},
  incrementalView = null,
  viewGeneration = null;
const pointContexts = new Map(),
  channelDevices = new Map();
function rememberPointContext() {
  if (selected && selectedDevice) channelDevices.set(selected, selectedDevice);
  if (selected && selectedDevice)
    pointContexts.set(selected + '/' + selectedDevice, {
      kind: $('point-kind').value,
      query: $('point-search').value,
      page: pointPage,
      scroll: $('point-scroll').scrollTop,
    });
}
function restorePointContext() {
  const context = pointContexts.get(selected + '/' + selectedDevice);
  $('point-kind').value = context?.kind || '';
  $('point-search').value = context?.query || '';
  pointPage = context?.page || 0;
  pointKey = '';
  pointTabsKey = '';
  requestAnimationFrame(() => {
    $('point-scroll').scrollTop = context?.scroll || 0;
  });
}
function viewCategory(p, protocol) {
  return p.tab && p.tab !== 'step'
    ? p.tab
    : protocol === 'iec104'
      ? p.kind === 'step'
        ? 'yt'
        : p.kind
      : { discrete: 'yx', coil: 'yk', holding: 'yc', input: 'yc' }[p.kind] || p.kind;
}
async function loadDesktopView() {
  if (incrementalView === false) {
    snapshot = await api('GetSnapshot');
    return;
  }
  const request = { ...viewCursor, role: stationMode, channel: selected, device: selectedDevice };
  const v = await api('GetView', request);
  if (!v || typeof v.metadata !== 'number') {
    incrementalView = false;
    snapshot = await api('GetSnapshot');
    return;
  }
  incrementalView = true;
  if (viewGeneration !== null && viewGeneration !== v.generation) {
    pointContexts.clear();
    channelDevices.clear();
    drafts.clear();
    runtimeViews.clear();
    runtimeDetailContexts.clear();
    pausedViews.clear();
    paused = false;
    frozen = [];
    pointPage = 0;
    actionKey = '';
    deviceKey = '';
    controlSelection = null;
    $('point-kind').value = '';
    $('point-search').value = '';
    $('point-scroll').scrollTop = 0;
    $('pause-messages').textContent = '暂停';
    $('pause-messages').classList.remove('is-paused');
    pointKey = '';
    pointTabsKey = '';
  }
  viewGeneration = v.generation;
  if (v.project) {
    snapshot.project = v.project;
    normalizeSnapshot();
    for (const c of snapshot.project.channels)
      for (const d of c.devices) {
        d.points = [];
        d._categoryVersions = {};
        d._metadata = v.metadata;
      }
  }
  snapshot.project.name = v.name;
  snapshot.path = v.path;
  snapshot.dirty = v.dirty;
  snapshot.states = v.states || [];
  const c = snapshot.project.channels.find((c) => c.id === v.channel),
    d = c?.devices.find((d) => d.id === v.device);
  if (d) {
    if (v.fullPoints) {
      d.points = v.points || [];
      d._pointIndex = new Map(d.points.map((p, i) => [keyOf(p), i]));
      for (const kind of ['yx', 'yc', 'yk', 'yt']) d._categoryVersions[kind] = v.pointVersion;
    } else
      for (const p of v.points || []) {
        const i = d._pointIndex?.get(keyOf(p));
        if (i === undefined) {
          d._pointIndex?.set(keyOf(p), d.points.length);
          d.points.push(p);
        } else d.points[i] = p;
        d._categoryVersions[viewCategory(p, c.protocol)] = v.pointVersion;
      }
    d._pointVersion = v.pointVersion;
  }
  snapshot.messages = (v.resetMessages ? [] : snapshot.messages || [])
    .concat(v.messages || [])
    .filter((m) => m.sequence >= v.firstMessage);
  snapshot.events = (v.resetEvents ? [] : snapshot.events || [])
    .concat(v.events || [])
    .filter((e) => e.sequence >= v.firstEvent);
  viewCursor = {
    metadata: v.metadata,
    pointIdentity: v.pointIdentity,
    points: v.pointVersion,
    messages: v.messageSequence,
    messageEpoch: v.messageEpoch,
    events: v.eventSequence,
    eventEpoch: v.eventEpoch,
  };
}
async function refresh(force = false) {
  let reloadNetwork = false;
  if (
    nativeContext?.kind === 'channel-dialog' ||
    (nativeContext?.kind === 'device-dialog' && !nativeContext.edit)
  ) {
    const c = await window.go.main.App.GetNativeContext();
    reloadNetwork = c.kind === 'channel-dialog' && c.activate !== nativeContext.activate;
    nativeContext = c;
    selected = c.channel;
  }
  if (refreshing) {
    await refreshing;
    if (!force) return;
  }
  refreshing = (async () => {
    try {
      await loadDesktopView();
      normalizeSnapshot();
      const identity = snapshot.project.channels
        .map((c) => c.id + ':' + c.devices.map((d) => d.id).join(','))
        .join('|');
      if (identity !== projectIdentity) {
        projectIdentity = identity;
        const valid = new Set(
          snapshot.project.channels.flatMap((c) => [
            c.id + '/raw',
            ...c.devices.map((d) => c.id + '/' + d.id),
          ]),
        );
        for (const id of drafts.keys())
          if (!valid.has(id.split('/').slice(0, 2).join('/'))) drafts.delete(id);
        for (const id of pointContexts.keys()) if (!valid.has(id)) pointContexts.delete(id);
        for (const id of channelDevices.keys())
          if (!snapshot.project.channels.some((c) => c.id === id)) channelDevices.delete(id);
      }
      if (!snapshot.project.channels.some((c) => c.id === selected && c.role === stationMode))
        selectChannel(
          snapshot.project.channels.find((c) => c.role === stationMode)?.id || '',
          false,
        );
      render();
    } catch (error) {
      $('footer-detail').textContent = String(error?.message || error);
      $('footer-detail').hidden = false;
    } finally {
      refreshing = null;
    }
  })();
  await refreshing;
  await pollRuntimeActions();
  if (reloadNetwork) channelDialog();
}
async function act(method, ...args) {
  const result = await api(method, ...args);
  await refresh(true);
  return result;
}
async function run(action, fn, form = null) {
  if (busy) return;
  let failure;
  busy = { action, channel: selected };
  if (form) {
    const box = form.querySelector('.form-error');
    if (box) box.hidden = true;
  }
  renderControls();
  try {
    return await fn();
  } catch (error) {
    failure = error;
    report(error, form);
  } finally {
    busy = null;
    renderControls();
    failure?.input?.focus();
  }
}
function on(id, event, fn, locked = true) {
  if (!$(id)) return;
  $(id).addEventListener(event, async (e) => {
    if (event === 'submit') e.preventDefault();
    if (locked) await run(id, () => fn(e), e.target.closest('form'));
    else {
      try {
        await fn(e);
      } catch (error) {
        report(error);
      }
    }
  });
}
function messageScope() {
  return $('all-messages').checked ? '*' : selected;
}
function loadPauseView() {
  const saved = pausedViews.get(messageScope());
  paused = !!saved;
  frozen = saved || [];
  $('pause-messages').textContent = paused ? '继续' : '暂停';
  $('pause-messages').classList.toggle('is-paused', paused);
  shownMessages = [];
  messageKey = '';
}
function selectChannel(id, draw = true) {
  if (id !== selected) {
    saveDraft();
    rememberPointContext();
    selected = id;
    selectedDevice = channelDevices.get(id) || channel()?.devices?.[0]?.id || '';
    const c = current();
    if (c) stationMode = c.role;
    pointPage = 0;
    pointKey = '';
    messageKey = '';
    $('point-search').value = '';
    $('point-kind').value = '';
    restorePointContext();
    loadPauseView();
  }
  if (nativeDesktop && !nativeContext?.kind)
    api('SetNativeSelection', {
      role: stationMode,
      channel: selected,
      device: selectedDevice,
    }).catch(report);
  if (draw) render();
}
function selectDevice(id, draw = true) {
  if (id !== selectedDevice) {
    saveDraft();
    rememberPointContext();
    selectedDevice = id;
    restorePointContext();
  }
  if (draw) render();
}
function setButtonLabel(id, text) {
  const b = $(id);
  (b.querySelector('span') || b).textContent = text;
}
function setHTML(id, html) {
  if ($(id).innerHTML !== html) $(id).innerHTML = html;
}
function formatBytes(n) {
  if (n < 1024) return `${n} <em>B</em>`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} <em>KB</em>`;
  return `${(n / 1048576).toFixed(1)} <em>MB</em>`;
}
function render() {
  if (!channel()?.devices?.some((d) => d.id === selectedDevice))
    selectDevice(channel()?.devices?.[0]?.id || '', false);
  const c = current(),
    s = state();
  const unsaved = !snapshot.path || snapshot.dirty;
  $('project-name').textContent = snapshot.project.name + (unsaved ? '（未保存）' : '');
  $('project-name').setAttribute(
    'aria-description',
    $('project-name').textContent + (snapshot.path ? '\n' + snapshot.path : ''),
  );
  const channels = snapshot.project.channels.filter((ch) => ch.role === stationMode);
  $('channel-count').textContent = channels.length;
  for (const button of document.querySelectorAll('[data-mode]')) {
    const chosen = button.dataset.mode === stationMode;
    button.classList.toggle('selected', chosen);
    button.setAttribute('aria-selected', String(chosen));
  }
  const sk = JSON.stringify([
    selected,
    stationMode,
    channels.map((ch) => [
      ch.id,
      ch.name,
      ch.protocol,
      ch.role,
      snapshot.states.find((st) => st.id === ch.id)?.status,
    ]),
  ]);
  if (sk !== sidebarKey) {
    sidebarKey = sk;
    const focused = document.activeElement?.dataset.select;
    const scroll = $('channel-list').scrollTop;
    $('channel-list').innerHTML =
      channels
        .map((ch) => {
          const st = snapshot.states.find((v) => v.id === ch.id) || { status: 'stopped' };
          return `<button class="channel-item ${ch.id === selected ? 'selected' : ''}" data-select="${escape(ch.id)}" aria-pressed="${ch.id === selected}" aria-description="${escape(ch.address)}"><span class="channel-symbol">${ch.role === 'server' ? '▣' : '⇄'}</span><span class="text"><b>${escape(ch.name)}</b><small>${escape(names[ch.protocol])} · ${ch.role === 'server' ? '子站' : '主站'}</small></span><i aria-description="${escape(statuses[st.status])}" class="status-dot ${active(st) ? 'online' : st.status === 'error' ? 'error' : ''}"></i></button>`;
        })
        .join('') || '<div class="channel-placeholder">暂无通道</div>';
    $('channel-list').scrollTop = scroll;
    if (focused) {
      const button = [...$('channel-list').querySelectorAll('button')].find(
        (b) => b.dataset.select === focused,
      );
      button?.focus({ preventScroll: true });
    }
  }
  const dk = JSON.stringify([
    c?.id,
    selectedDevice,
    c?.devices.map((d) => [d.id, d.name, d.unit, d.commonAddress]),
  ]);
  if (dk !== deviceKey) {
    deviceKey = dk;
    const list = $('device-list'),
      scroll = list.scrollTop,
      focused = document.activeElement?.dataset.device;
    list.innerHTML = c?.devices.length
      ? c.devices
          .map(
            (d) =>
              `<button class="device-item ${d.id === selectedDevice ? 'selected' : ''}" data-device="${escape(d.id)}" aria-pressed="${d.id === selectedDevice}" aria-description="${escape(d.name)} · ${c.protocol === 'iec104' ? '公共单元' : '站号'} ${c.protocol === 'iec104' ? d.commonAddress : d.unit} · 双击修改"><span>${escape(d.name)}</span><small>${c.protocol === 'iec104' ? '公共点位 ' + d.commonAddress : '设备点位 ' + d.unit}</small></button>`,
          )
          .join('')
      : '<div class="channel-placeholder">暂无装置</div>';
    list.scrollTop = scroll;
    if (focused)
      [...list.querySelectorAll('button')]
        .find((b) => b.dataset.device === focused)
        ?.focus({ preventScroll: true });
  }
  $('device-count').textContent = String(c?.devices.length || 0);
  $('selected-device-name').textContent = device()?.name || '';
  $('current-protocol').textContent = c ? names[c.protocol] : '';
  $('channel-name').textContent = c?.name || '未选择通道';
  setHTML(
    'channel-meta',
    c
      ? `<i class="status-dot ${active(s) ? 'online' : s.status === 'error' ? 'error' : ''}"></i><span class="connection-status">${escape(c.protocol === 'iec104' && s.status === 'connected' && s.readyPeers === 0 ? '等待激活' : statuses[s.status] || s.status)}</span><span>·</span><span class="endpoint" aria-description="${escape(s.address || c.address)}">${escape(s.address || c.address)}</span><span>·</span>${c.role === 'server' ? '子站' : '主站'}`
      : '',
  );
  setHTML('tx-count', formatBytes(s.tx || 0));
  setHTML('rx-count', formatBytes(s.rx || 0));
  $('peer-count').textContent = s.peers || 0;
  $('traffic-stats').hidden = s.status !== 'connected' || !s.peers;
  $('footer-detail').textContent = s.status === 'connecting' ? '自动重试中' : s.lastError || '';
  $('footer-detail').setAttribute('aria-description', s.lastError || '');
  $('footer-detail').hidden = s.status !== 'connecting' && !s.lastError;
  renderPointTabs(c);
  renderActions(c);
  renderPoints(c);
  renderMessages();
  renderEvents();
  renderDebug();
  renderManager();
  renderControls();
}
function renderUpdateButton() {
  const button = $('update-button');
  button.disabled = !!busy || updateChecking;
  button.setAttribute('aria-busy', String(updateChecking));
  setButtonLabel('update-button', updateChecking ? '\u68c0\u67e5\u4e2d\u2026' : '\u66f4\u65b0');
}
function renderControls() {
  renderUpdateButton();
  if ($('auto-data-dialog').open) updateDataControls();
  const c = current(),
    s = state(),
    locked = !!busy,
    online = !!c && transferReady(s, c.protocol);
  for (const id of ['new-project', 'open-project', 'save-project', 'save-as', 'add-channel'])
    $(id).disabled = locked;
  updateAllChannelsToggle();
  $('edit-channel').disabled = locked || !c;
  $('delete-channel').disabled = locked || !c || active(s);
  if ($('channel-dialog').open) updateChannelFields();
  $('device-config').disabled = locked || !device();
  $('add-device').disabled = locked;
  $('toggle-channel').disabled = locked || !c;
  setButtonLabel(
    'toggle-channel',
    busy?.action === 'toggle-channel' && busy.channel === selected
      ? active(s)
        ? '停止中…'
        : '连接中…'
      : active(s)
        ? '停止'
        : s.status === 'disconnected' || s.status === 'error'
          ? '重连'
          : '连接',
  );
  for (const id of [
    'read-clock',
    'counter-interrogate',
    'clock-sync',
    'interrogate',
    'publish',
    'read-modbus',
    'write-modbus',
  ])
    if ($(id)) $(id).disabled = locked || !online || !device();
  for (const id of [
    'open-yk',
    'open-yt',
    'open-step',
    'open-auto-data',
    'open-remote-config',
    'open-telemetry-values',
  ])
    if ($(id)) $(id).disabled = locked || !device();
  for (const id of [
    'read-clock',
    'counter-interrogate',
    'clock-sync',
    'interrogate',
    'publish',
    'read-modbus',
    'write-modbus',
  ])
    if ($(id)) $(id).setAttribute('aria-description', online ? '' : '请先连接通道；已连接后可发送');
  for (const button of document.querySelectorAll(
    '#channel-form button[type=submit],#point-form button[type=submit],#device-form button[type=submit],#remote-config-form button[type=submit],#telemetry-values-form button[type=submit]',
  ))
    button.disabled = locked;
  const raw = c?.protocol === 'raw';
  $('raw-form').hidden = !raw;
  $('raw-hex').disabled = locked || !raw;
  $('append-crc').disabled = locked || !raw;
  $('raw-form').querySelector('button').disabled = locked || !raw || !online;
  $('export-messages').disabled = locked || !snapshot.messages.length;
  $('clear-messages').disabled = locked || (!snapshot.messages.length && !frozen.length);
  if ($('telemetry-values-dialog').open) updateTelemetryFields();
  updateRuntimeActions();
  updateAutoButtons();
  for (const id of ['open-auto-yk', 'open-auto-yt'])
    if ($(id)) $(id).disabled = locked || !device();
  updateControlButtons();
  $('channel-form').setAttribute('aria-busy', String(locked));
  $('point-form').setAttribute('aria-busy', String(locked));
}
// Runtime actions use saved settings. Editors never start or pause tasks.
function runtimeEntries() {
  if (nativeContext?.kind) return [];
  const c = channel(),
    d = device(),
    category = $('point-kind').value.replace(/^tab:/, '');
  if (!c || !d) return [];
  const base = { channel: c.id, device: d.id, protocol: c.protocol };
  if (
    c.role === 'server' &&
    ['yx', 'yc'].includes(category) &&
    (c.protocol === 'iec104' || c.protocol.startsWith('modbus'))
  )
    return [
      {
        ...base,
        id: 'data',
        kind: 'data',
        scope: 'telemetry',
        label: c.protocol === 'iec104' ? '自动上报' : '自动数据',
      },
    ];
  if (
    c.role === 'client' &&
    ['yk', 'yt'].includes(category) &&
    (c.protocol === 'iec104' || c.protocol.startsWith('modbus'))
  )
    return [
      {
        ...base,
        id: 'auto',
        kind: 'auto',
        scope: category,
        label: category === 'yk' ? '遥控' : '遥调',
      },
    ];
  return [];
}
const runtimeKey = (e) => [e.channel, e.device, e.kind, e.scope].join('/');
async function getRuntimeView(e) {
  if (e.kind === 'auto') {
    const view = await api('GetAutoControlStatus', e.channel, e.device, e.scope);
    if (view) return view;
  }
  return api(e.kind === 'data' ? 'GetDataTest' : 'GetAutoControl', e.channel, e.device, e.scope);
}
function attachRuntimeActions(c) {
  const entries = runtimeEntries();
  for (const e of entries) {
    $('protocol-actions').insertAdjacentHTML(
      'beforeend',
      '<div class="runtime-actions"><button id="runtime-' +
        e.id +
        '-run">开启' +
        e.label +
        '</button>' +
        (e.kind === 'auto'
          ? '<button type="button" class="runtime-status row-button" id="runtime-' +
            e.id +
            '-status" aria-label="运行状态"></button>'
          : '') +
        '</div>',
    );
    on('runtime-' + e.id + '-run', 'click', async () => {
      const view = await getRuntimeView(e);
      if (!view) throw Error('请先保存配置');
      let result;
      if (e.kind === 'data') {
        if (['running', 'paused'].includes(view.status))
          result = await api('SetDataTestState', e.channel, e.device, e.scope, 'stop');
        else result = await api('StartDataTest', e.channel, e.device, e.scope);
      } else {
        if (!view.saved && !autoIsActive(view.status)) throw Error('请先保存自动操作配置');
        result = await api(
          autoIsActive(view.status) ? 'StopAutoControl' : 'StartAutoControl',
          e.channel,
          e.device,
          e.scope,
        );
      }
      if (result) runtimeViews.set(runtimeKey(e), result);
      await refresh(true);
    });
    if (e.kind === 'auto')
      on(
        'runtime-' + e.id + '-status',
        'click',
        () => {
          const v = runtimeViews.get(runtimeKey(e));
          $('all-messages').checked = false;
          $('direction-filter').value = '';
          $('message-search').value = v?.lastError || '';
          messageKey = '';
          renderMessages();
          $('message-search').focus();
        },
        false,
      );
    if (e.kind === 'auto') {
      const context = runtimeDetailContexts.get(runtimeKey(e)) || { open: false, offset: 0 };
      runtimeDetailContexts.set(runtimeKey(e), context);
      $('protocol-actions').insertAdjacentHTML(
        'beforeend',
        '<details id="runtime-auto-details" class="runtime-details"><summary>运行详情</summary><div id="runtime-auto-summary"></div><div class="runtime-result-scroll"><table><thead><tr>' +
          ['点位', '描述', ...(e.scope === 'yk' ? ['选择', '执行', '变位结果'] : ['执行结果'])]
            .map((label) => '<th>' + label + '</th>')
            .join('') +
          '</tr></thead><tbody id="runtime-auto-results"></tbody></table></div><div class="runtime-result-pager"><button type="button" id="runtime-results-prev" aria-label="上一页运行结果">‹</button><span id="runtime-results-page"></span><button type="button" id="runtime-results-next" aria-label="下一页运行结果">›</button></div><pre id="runtime-auto-errors" hidden></pre></details>',
      );
      const details = $('runtime-auto-details');
      details.open = context.open;
      details.addEventListener('toggle', () => {
        context.open = details.open;
        if (details.open) loadRuntimeDetails(e);
      });
      for (const [id, step] of [
        ['runtime-results-prev', -100],
        ['runtime-results-next', 100],
      ])
        on(
          id,
          'click',
          () => {
            context.offset = Math.max(0, context.offset + step);
            return loadRuntimeDetails(e);
          },
          false,
        );
      if (context.open) loadRuntimeDetails(e);
    }
  }
  updateRuntimeActions();
  if (entries.length) pollRuntimeActions();
}
function updateRuntimeActions() {
  for (const e of runtimeEntries()) {
    const run = $('runtime-' + e.id + '-run');
    if (!run) continue;
    const view = runtimeViews.get(runtimeKey(e)),
      status = view?.status,
      active = ['starting', 'running', 'paused', 'stopping'].includes(status),
      state = snapshot.states.find((s) => s.id === e.channel),
      ready =
        e.protocol === 'iec104' || e.kind === 'auto'
          ? transferReady(state, e.protocol)
          : !!state && ['connected', 'listening'].includes(state.status),
      saved =
        e.kind === 'data'
          ? view?.config?.rules?.some((r) => r.enabled)
          : view?.saved &&
            (view.enabledRows !== undefined
              ? view.enabledRows > 0
              : view?.rows?.some((r) => e.scope === 'yt' || r.enabled));
    run.textContent =
      (e.kind === 'data' ? (active ? '关闭' : '开启') : active ? '结束' : '开始') + e.label;
    run.setAttribute('aria-pressed', String(active));
    run.disabled =
      !!busy ||
      (!ready && !active) ||
      ['starting', 'stopping'].includes(status) ||
      (!active && !saved);
    const reason =
      !saved && !active
        ? '请先保存启用的配置'
        : state?.status === 'connected' && e.protocol === 'iec104' && state.readyPeers === 0
          ? '等待104链路激活'
          : !ready && !active
            ? '请手动连接通道'
            : '';
    run.setAttribute('aria-description', run.disabled && !busy ? reason : '');
    const info = $('runtime-' + e.id + '-status');
    if (info) {
      info.hidden = !active && !view?.lastError;
      info.textContent =
        view?.lastError ||
        ({ starting: '启动中', running: '运行中', paused: '已暂停', stopping: '停止中' }[status] ||
          '') + (view?.total > 0 ? ' · 确认 ' + (view.success || 0) + ' / ' + view.total : '');
      info.setAttribute('aria-description', view?.lastError ? '定位相关报文' : '查看当前通道报文');
    }
  }
}
async function loadRuntimeDetails(entry) {
  const details = $('runtime-auto-details');
  if (!details?.open) return;
  const key = runtimeKey(entry),
    context = runtimeDetailContexts.get(key);
  if (!context) return;
  const request = ++runtimeDetailRequest,
    generation = viewGeneration,
    view = runtimeViews.get(key);
  try {
    const result = await api(
      'GetAutoControlDetails',
      entry.channel,
      entry.device,
      entry.scope,
      context.offset,
    );
    if (
      request !== runtimeDetailRequest ||
      generation !== viewGeneration ||
      !details.isConnected ||
      !details.open
    )
      return;
    if (!result) {
      $('runtime-auto-summary').textContent = '暂无运行记录';
      return;
    }
    context.offset = result.offset || 0;
    $('runtime-auto-summary').textContent =
      '操作 ' +
      (view?.total || 0) +
      ' · 成功 ' +
      (view?.success || 0) +
      ' · 失败 ' +
      (view?.failure || 0);
    setHTML(
      'runtime-auto-results',
      (result.rows || [])
        .map(
          (row, i) =>
            '<tr' +
            (result.offset + i === result.current ? ' class="is-current"' : '') +
            '>' +
            [
              row.address,
              row.name,
              ...(entry.scope === 'yk'
                ? [row.selection, row.execution, row.feedbackResult]
                : [row.execution || (row.executed === '是' ? '执行成功' : '未执行')]),
            ]
              .map((value) => '<td>' + escape(value || value === 0 ? value : '—') + '</td>')
              .join('') +
            '</tr>',
        )
        .join('') || '<tr><td colspan="5">暂无运行记录</td></tr>',
    );
    const count = result.rowCount || 0;
    $('runtime-results-page').textContent = count
      ? Math.floor(context.offset / 100) + 1 + ' / ' + Math.ceil(count / 100)
      : '0';
    $('runtime-results-prev').disabled = context.offset === 0;
    $('runtime-results-next').disabled = context.offset + 100 >= count;
    details.querySelector('.runtime-result-pager').hidden = count <= 100;
    $('runtime-auto-errors').hidden = !result.errors?.length;
    $('runtime-auto-errors').textContent = (result.errors || []).join('\n');
  } catch (error) {
    if (request === runtimeDetailRequest && details.isConnected && details.open)
      $('runtime-auto-summary').textContent = String(error?.message || error);
  }
}
async function pollRuntimeActions() {
  if (nativeContext?.kind || runtimePollBusy || document.hidden) return;
  const entries = runtimeEntries();
  if (!entries.length) return;
  const generation = viewGeneration;
  runtimePollBusy = true;
  try {
    await Promise.allSettled(
      entries.map(async (e) => {
        try {
          const view = await getRuntimeView(e);
          if (generation !== viewGeneration) return;
          if (view) runtimeViews.set(runtimeKey(e), view);
          else runtimeViews.delete(runtimeKey(e));
          if (e.kind === 'auto') await loadRuntimeDetails(e);
        } catch {
          runtimeViews.delete(runtimeKey(e));
        }
      }),
    );
  } finally {
    runtimePollBusy = false;
    updateRuntimeActions();
  }
}
async function configurationSaved(channelID) {
  await refresh(true);
  const state = snapshot.states.find((s) => s.id === channelID);
  toast(state && active(state) ? '配置已保存' : '配置已保存，请手动连接后运行');
}

function saveDraft() {
  if (!selected) return;
  const data = { ...(drafts.get(draftKey()) || {}) };
  for (const el of $('protocol-actions').querySelectorAll('input,select')) data[el.id] = el.value;
  data.hex = $('raw-hex').value;
  data.crc = $('append-crc').checked;
  drafts.set(draftKey(), data);
}
function renderActions(c) {
  const key = c ? `${c.id}:${selectedDevice}:${c.protocol}:${c.role}:${$('point-kind').value}` : '';
  if (key === actionKey) return;
  actionKey = key;
  const box = $('protocol-actions');
  box.dataset.category = $('point-kind').value;
  box.hidden = !c || !device();
  if (!c) {
    box.innerHTML = '';
    return;
  }
  if (c.protocol === 'iec104')
    box.innerHTML =
      c.role === 'client'
        ? '<button id="interrogate" class="primary">总召唤</button><button id="counter-interrogate">脉冲召唤</button><button id="clock-sync">校时</button><button id="show-events">SOE / COS</button><button id="open-yk">遥控</button><button id="open-auto-yk">自动遥控配置</button><button id="open-step" aria-description="遥调（步调节）">步调节</button><button id="open-yt" aria-description="遥调（设值）">设值</button><button id="open-auto-yt">自动遥调配置</button>'
        : '<button id="publish" class="primary">上送</button><button id="open-auto-data">自动数据</button>';
  else if (c.protocol.startsWith('modbus') && c.role === 'client')
    box.innerHTML =
      '<button id="clock-sync">校时</button><button id="read-clock">读时</button><div class="operation-group"><select id="read-fc" aria-label="读功能码"><option value="3">03 保持寄存器</option><option value="4">04 输入寄存器</option><option value="1">01 线圈</option><option value="2">02 离散量</option></select><label>点位<input id="read-start" type="text" value="0-1" aria-label="读取点位" autocomplete="off"></label><button id="read-modbus" class="primary">读取</button></div><div class="operation-group"><select id="write-fc" aria-label="写功能码"><option value="6">06 单寄存器</option><option value="5">05 单线圈</option><option value="16">16 多寄存器</option><option value="15">15 多线圈</option></select><label>点位<input id="write-address" type="text" value="0" aria-label="写入点位" autocomplete="off"></label><label>值<input id="write-value" type="number" min="0" max="65535" value="1" aria-label="写值"></label><button id="write-modbus">写入</button></div>';
  else {
    box.innerHTML = '';
    box.hidden = true;
  }
  if (c.protocol.startsWith('modbus') && c.role === 'client') {
    const tab = $('point-kind').value.replace(/^tab:/, '');
    box.insertAdjacentHTML(
      'beforeend',
      tab === 'yx'
        ? '<button id="show-events">COS</button>'
        : tab === 'yk'
          ? '<button id="open-auto-yk">自动遥控配置</button>'
          : tab === 'yt'
            ? '<button id="open-auto-yt">自动遥调配置</button>'
            : '',
    );
  }
  if (c.role === 'server' && (c.protocol === 'iec104' || c.protocol.startsWith('modbus'))) {
    const tab = $('point-kind').value.replace(/^tab:/, '');
    if (['yx', 'yc'].includes(tab)) {
      box.hidden = false;
      box.innerHTML =
        (c.protocol === 'iec104' ? '<button id="publish" class="primary">上送</button>' : '') +
        '<button id="open-telemetry-values">批量设置</button><button id="open-auto-data">' +
        (c.protocol === 'iec104' ? '自动上报配置' : '自动数据配置') +
        '</button>';
    } else {
      box.hidden = false;
      box.innerHTML = '<button id="open-remote-config">' + tabNames[tab] + '配置</button>';
    }
  }
  if (c.protocol === 'iec104' && c.role === 'client') {
    const tab = $('point-kind').value.replace(/^tab:/, ''),
      ids =
        tab === 'yk'
          ? ['open-yk', 'open-auto-yk']
          : tab === 'yt'
            ? ['open-step', 'open-yt', 'open-auto-yt']
            : ['interrogate', 'clock-sync', ...(tab === 'yx' ? ['show-events'] : [])];
    for (const button of [...box.querySelectorAll('button')])
      if (!ids.includes(button.id)) button.remove();
    if (!ids.includes('open-auto-yt')) box.querySelector('.auto-yt-source')?.remove();
  }
  attachRuntimeActions(c);
  if ($('open-telemetry-values')) on('open-telemetry-values', 'click', () => openTelemetryValues());
  if ($('open-remote-config')) on('open-remote-config', 'click', () => openRemoteConfig());
  if ($('open-auto-data')) on('open-auto-data', 'click', () => openAutoDataWindow());
  const draft = drafts.get(draftKey()) || {};
  for (const el of box.querySelectorAll('input,select'))
    if (draft[el.id] !== undefined) el.value = draft[el.id];
  $('raw-hex').value = draft.hex || '';
  $('append-crc').checked = !!draft.crc;
  if ($('read-clock'))
    on('read-clock', 'click', () => act('ReadClockDevice', selected, selectedDevice));
  if ($('counter-interrogate'))
    on('counter-interrogate', 'click', () =>
      act('CounterInterrogateDevice', selected, selectedDevice),
    );
  if ($('clock-sync'))
    on('clock-sync', 'click', () => act('ClockSyncDevice', selected, selectedDevice));
  if ($('show-events'))
    on(
      'show-events',
      'click',
      () => {
        $('events-dialog').show();
        renderEvents();
      },
      false,
    );
  if ($('interrogate'))
    on('interrogate', 'click', () => act('InterrogateDevice', selected, selectedDevice));
  if ($('publish'))
    on('publish', 'click', () =>
      act(
        'PublishDeviceCategory',
        selected,
        selectedDevice,
        $('point-kind').value.replace(/^tab:/, ''),
      ),
    );
  for (const [id, mode] of [
    ['open-yk', 'yk'],
    ['open-step', 'step'],
    ['open-yt', 'setpoint'],
  ])
    if ($(id)) on(id, 'click', () => controlDialog(mode), false);
  for (const mode of ['yk', 'yt'])
    if ($('open-auto-' + mode)) on('open-auto-' + mode, 'click', () => openAutoWindow(mode));
  if ($('read-modbus')) {
    updateModbusLimits();
    on('read-fc', 'change', updateModbusLimits, false);
    on('write-fc', 'change', updateModbusLimits, false);
    on('read-modbus', 'click', () => {
      const selection = $('read-start').value;
      globalThis.pointSelection.parse(selection, 65535);
      return act(
        'ReadDeviceModbusPoints',
        selected,
        selectedDevice,
        Number($('read-fc').value),
        selection,
      );
    });
    on('write-modbus', 'click', async () => {
      const id = selected,
        deviceID = selectedDevice,
        fc = Number($('write-fc').value),
        selection = $('write-address').value,
        value = integer('write-value', 0, [5, 15].includes(fc) ? 1 : 65535);
      globalThis.pointSelection.parse(selection, 65535);
      if (
        globalThis.pointSelection.parse(selection, 65535).length === 1 ||
        (await confirmAction('写入确认', `${device().name} · 点位 ${selection} → ${value}`))
      ) {
        const result = await act('WriteDeviceModbusPoints', id, deviceID, fc, selection, value);
        toast(result?.broadcast ? '广播写入已发送' : '写入已确认');
      }
    });
  }

  if (c.protocol.startsWith('modbus') && c.role === 'client') {
    if (draft['read-start'] === undefined) applyRangeRequest();
    const tab = $('point-kind').value.replace(/^tab:/, ''),
      semantic = true,
      write = ['yk', 'yt'].includes(tab),
      groups = box.querySelectorAll('.operation-group');
    if (semantic) {
      groups[0].hidden = write;
      groups[1].hidden = !write;
      for (const option of $('write-fc').options)
        option.hidden =
          tab === 'yk'
            ? !['5', '6', '15'].includes(option.value)
            : tab === 'yt'
              ? !['6', '16'].includes(option.value)
              : false;
      $('read-modbus').textContent = '读取' + (tabNames[tab] || '数据');
      $('write-modbus').textContent = tab === 'yt' ? '遥调' : tab === 'yk' ? '遥控' : '写入';
    }
  }
}
function revealField(input) {
  for (let parent = input?.parentElement; parent; parent = parent.parentElement)
    if (parent.tagName === 'DETAILS') parent.open = true;
}
function integer(id, min, max) {
  const el = $(id),
    n = Number(el.value);
  if (el.value.trim() === '' || !Number.isInteger(n) || n < min || n > max) {
    revealField(el);
    el.focus();
    throw Object.assign(Error(`${el.getAttribute('aria-label') || '参数'}：${min}–${max} 整数`), {
      input: el,
    });
  }
  return n;
}
function updateModbusLimits() {
  if (!$('write-value')) return;
  $('write-value').max = ['5', '15'].includes($('write-fc').value) ? '1' : '65535';
  if (Number($('write-value').value) > Number($('write-value').max))
    $('write-value').value = $('write-value').max;
  saveDraft();
}
const tabNames = {
  yt: '遥调',
  step: '步调节',
  yx: '遥信',
  yc: '遥测',
  mc: '脉冲',
  position: '步位置',
  yk: '遥控',
  jdq: '继电器',
};
function activeRange() {
  const value = $('point-kind').value;
  return value.startsWith('tab:')
    ? device()?.configuration?.ranges?.find((r) => r.tab === value.slice(4))
    : null;
}
function applyRangeRequest() {
  if (!$('read-fc')) return;
  const category = $('point-kind').value.replace(/^tab:/, ''),
    points = device()?.points.filter((p) => pointCategory(p) === category) || [],
    first = [...points].sort((a, b) => a.address - b.address)[0],
    range = activeRange() || {
      tab: category,
      kind: first?.kind || { yx: 'discrete', yc: 'holding', yk: 'coil', yt: 'holding' }[category],
      address: first?.address || 0,
      count: points.length,
    };
  $('read-fc').value = String({ coil: 1, discrete: 2, holding: 3, input: 4 }[range.kind]);
  const selection = points.length
    ? globalThis.pointSelection.format(points.map((p) => p.address))
    : range.selection || globalThis.pointSelection.range(range.address, range.count);
  $('read-start').value = selection;
  $('write-address').value = selection;
  $('write-fc').value = range.kind === 'coil' ? '5' : '6';
  updateModbusLimits();
}
function pointCategory(p) {
  if (['yx', 'yc', 'yk', 'yt'].includes(p.tab)) return p.tab;
  if (p.tab && p.tab !== 'step') return '';
  if (channel()?.protocol === 'iec104')
    return p.kind === 'step' ? 'yt' : ['yx', 'yc', 'yk', 'yt'].includes(p.kind) ? p.kind : '';
  return { discrete: 'yx', holding: 'yc', input: 'yc', coil: 'yk' }[p.kind] || '';
}
function renderPointTabs(c) {
  const ranges = device()?.configuration?.ranges,
    types =
      !c || !device() || c.protocol === 'raw'
        ? []
        : ['yx', 'yc', 'yk', 'yt'].map((k) => (ranges?.some((r) => r.tab === k) ? 'tab:' + k : k));
  const previous = $('point-kind').value;
  for (const kind of types)
    if (![...$('point-kind').options].some((o) => o.value === kind)) {
      const o = document.createElement('option');
      o.value = kind;
      o.textContent = tabNames[kind.replace(/^tab:/, '')];
      $('point-kind').add(o);
    }
  $('point-kind').value =
    types.find((k) => k.replace(/^tab:/, '') === previous.replace(/^tab:/, '')) ||
    (ranges?.some((r) => r.tab === 'yx')
      ? types[0]
      : types.find((k) => (c?.points || []).some((p) => pointMatchesKind(p, k)))) ||
    types[0] ||
    '';
  const key = JSON.stringify([selectedDevice, types, $('point-kind').value]);
  if (key === pointTabsKey) return;
  pointTabsKey = key;
  $('point-tabs').hidden = !types.length;
  $('point-tabs').innerHTML = types
    .map(
      (kind) =>
        `<button role="tab" aria-selected="${kind === $('point-kind').value}" class="${kind === $('point-kind').value ? 'selected' : ''}" data-point-kind="${kind}">${tabNames[kind.replace(/^tab:/, '')]}</button>`,
    )
    .join('');
}
function pointMatchesKind(p, kind) {
  return !kind || pointCategory(p) === kind.replace(/^tab:/, '');
}
function pointMatchesSearch(p, query) {
  if (!query) return true;
  if (
    String(p.name || '')
      .toLowerCase()
      .includes(query)
  )
    return true;
  return (
    /^\d{1,5}$/.test(query) &&
    Number.isInteger(p.address) &&
    p.address >= 0 &&
    p.address <= 99999 &&
    String(p.address).includes(query)
  );
}
function pointWireType(p, configuration = device()?.configuration) {
  if (p.wireType) return Number(p.wireType);
  const values = configuration?.values || {};
  if (p.kind === 'yc')
    return Number(
      String(values.cbYC_Type || '13').match(/type:(\d+)\)/)?.[1] || values.cbYC_Type || 13,
    );
  if (p.kind === 'yx')
    return (
      (values.cbYX_Type === '双点' ? 3 : 1) + (['true', true].includes(values.isHasTime) ? 29 : 0)
    );
  return 0;
}
function pointLimits(p, configuration = device()?.configuration) {
  const type = pointWireType(p, configuration);
  let min = -3.4028234663852886e38,
    max = 3.4028234663852886e38,
    integer = false,
    normalized = false;
  if (['yx', 'yk', 'coil', 'discrete'].includes(p.kind)) {
    min = 0;
    max = [3, 31].includes(type) ? 3 : 1;
    integer = true;
  } else if (['holding', 'input'].includes(p.kind)) {
    [min, max] = {
      uint16: [0, 65535],
      int16: [-32768, 32767],
      uint32: [0, 4294967295],
      int32: [-2147483648, 2147483647],
      float32: [min, max],
    }[p.dataType || 'uint16'];
    integer = p.dataType !== 'float32';
  } else if ([9, 21, 34].includes(type)) {
    min = -1;
    max = 1;
    normalized = true;
  } else if (['step', 'position', 'mc'].includes(p.kind) || [11, 35].includes(type)) {
    [min, max] =
      p.kind === 'position'
        ? [-64, 63]
        : p.kind === 'mc'
          ? [-2147483648, 2147483647]
          : [-32768, 32767];
    integer = true;
  }
  return { min, max, integer, normalized };
}
function validatePointNumber(p, value, configuration) {
  const limits = pointLimits(p, configuration);
  if (
    !Number.isFinite(value) ||
    value < limits.min ||
    (limits.normalized ? value >= 1 : value > limits.max) ||
    (limits.integer && !Number.isInteger(value))
  ) {
    if (limits.normalized)
      throw Error(
        '点位 ' +
          p.address +
          ' 为归一化遥测，数值须满足 -1 ≤ 值 < 1；较大数值请选择标度化或短浮点格式',
      );
    throw Error(
      '点位 ' +
        p.address +
        ' 的数值应为 ' +
        limits.min +
        '–' +
        limits.max +
        (limits.integer ? ' 整数' : ''),
    );
  }
}
function pointEncoding(p, configuration = device()?.configuration) {
  const t = pointWireType(p, configuration),
    dt = p.dataType;
  let type = 'uint',
    bytes = 2;
  if (['yx', 'yk', 'coil', 'discrete'].includes(p.kind)) bytes = 1;
  else if (dt) {
    type = dt.startsWith('int') ? 'signed' : dt === 'float32' ? 'float' : 'uint';
    bytes = dt.endsWith('32') ? 4 : 2;
  } else if (p.kind === 'mc') {
    bytes = 4;
    type = 'signed';
  } else if (p.kind === 'position') {
    bytes = 1;
    type = 'signed';
  } else if (p.kind === 'step' || [11, 35].includes(t)) type = 'signed';
  else if ([9, 21, 34].includes(t)) type = 'normalized';
  else if (['yc', 'yt'].includes(p.kind)) {
    bytes = 4;
    type = 'float';
  }
  return { type, bytes, order: p.byteOrder || '' };
}
function fullPointHex(p) {
  const f = pointEncoding(p),
    b = new ArrayBuffer(f.bytes),
    v = new DataView(b);
  if (f.type === 'float') v.setFloat32(0, p.value);
  else if (f.bytes === 4) v.setUint32(0, p.value);
  else if (f.bytes === 2)
    v.setUint16(
      0,
      f.type === 'normalized' ? Math.min(32767, Math.round(p.value * 32768)) : p.value,
    );
  else v.setUint8(0, p.value);
  let raw = [...new Uint8Array(b)];
  if (f.order) {
    const chars = f.bytes === 4 ? 'ABCD' : 'AB';
    raw = [...f.order].map((c) => raw[chars.indexOf(c)]);
  }
  return raw
    .map((n) => n.toString(16).padStart(2, '0'))
    .join('')
    .toUpperCase();
}
function pointHex(p) {
  return fullPointHex(p).replace(/^0+(?=.)/, '');
}
function hexPointValue(p, hex, configuration = device()?.configuration) {
  const f = pointEncoding(p, configuration);
  if (!new RegExp('^[0-9a-fA-F]{1,' + f.bytes * 2 + '}$').test(hex))
    throw Error('请输入有效的十六进制码值');
  let raw = hex
    .padStart(f.bytes * 2, '0')
    .match(/../g)
    .map((n) => parseInt(n, 16));
  if (f.order) {
    const out = raw.slice(),
      chars = f.bytes === 4 ? 'ABCD' : 'AB';
    [...f.order].forEach((c, i) => (out[chars.indexOf(c)] = raw[i]));
    raw = out;
  }
  const v = new DataView(Uint8Array.from(raw).buffer);
  if (f.type === 'float') return v.getFloat32(0);
  if (f.type === 'normalized') return v.getInt16(0) / 32768;
  if (f.type === 'signed')
    return f.bytes === 4 ? v.getInt32(0) : f.bytes === 2 ? v.getInt16(0) : v.getInt8(0);
  return f.bytes === 4 ? v.getUint32(0) : f.bytes === 2 ? v.getUint16(0) : v.getUint8(0);
}

function pointValueError(input, text) {
  input.setAttribute('aria-invalid', 'true');
  input.setAttribute('aria-description', text);
  toast(text, true);
}
function parsePointCell(edit, raw) {
  const key = edit.target.key,
    field = edit.field;
  if (field === 'name') {
    if (raw.length > 200) throw Error('描述最多200字');
    return raw;
  }
  let value = Number(raw.trim());
  if (field === 'hex') {
    value = hexPointValue(
      edit.target.point || { kind: key.kind },
      raw.trim().replace(/^0x/i, ''),
      edit.target.configuration,
    );
  }
  if (!raw.trim() || !Number.isFinite(value)) throw Error('请输入有效数值');
  if (field === 'address') {
    const max = edit.target.protocol.startsWith('modbus') ? 65535 : 16777215;
    if (!Number.isInteger(value) || value < 0 || value > max)
      throw Error('点位应为 0–' + max + ' 整数');
  } else if (field === 'quality') {
    if (!Number.isInteger(value) || value < 0 || value > 255)
      throw Error('品质应为 0–255 整数，可用0x十六进制');
  } else
    validatePointNumber(
      edit.target.point || { kind: key.kind, address: key.address },
      value,
      edit.target.configuration,
    );
  return value;
}
async function commitPointValue(input) {
  const edit = input.pointEdit;
  if (!edit || edit.pending || !edit.dirty) return;
  let value;
  try {
    value = parsePointCell(edit, input.value);
  } catch (error) {
    pointValueError(input, error.message);
    return;
  }
  if (value === edit.base) {
    edit.dirty = false;
    input.removeAttribute('aria-invalid');
    return;
  }
  edit.pending = true;
  input.readOnly = true;
  input.setAttribute('aria-busy', 'true');
  const target = edit.target;
  target.tail = target.tail.then(async () => {
    try {
      const key = { ...target.key },
        field = edit.field === 'hex' ? 'value' : edit.field,
        telemetry =
          target.role === 'server' &&
          (target.protocol === 'iec104'
            ? ['yx', 'yc'].includes(key.kind)
            : target.protocol.startsWith('modbus'));
      const result =
        field === 'value' && telemetry
          ? await api('SetPointValue', target.channel, target.device, key, value)
          : await api('EditPointCell', target.channel, target.device, key, field, {
              [field]: value,
            });
      if (field === 'address') {
        target.key.address = value;
        target.row.dataset.pointKey = keyOf(target.key);
      }
      edit.base = value;
      edit.dirty = false;
      input.removeAttribute('aria-invalid');
      if (result?.mode === 'offline') toast('已保存，未连接或104传输未激活，未上送');
      else {
        input.classList.add('cell-saved');
        input.setAttribute(
          'aria-label',
          (input.getAttribute('aria-label') || '点值').replace(/，已保存$/, '') + '，已保存',
        );
        setTimeout(() => {
          input.classList.remove('cell-saved');
          input.setAttribute(
            'aria-label',
            (input.getAttribute('aria-label') || '点值').replace(/，已保存$/, ''),
          );
        }, 900);
      }
    } catch (error) {
      pointValueError(input, String(error?.message || error));
    } finally {
      edit.pending = false;
      input.readOnly = false;
      input.removeAttribute('aria-busy');
      pointKey = '';
      await refresh(true);
    }
  });
  return target.tail;
}
function commitPointRow(row) {
  for (const input of row.querySelectorAll('.point-cell-input')) commitPointValue(input);
}
function renderPointCell(cell, target, p, field) {
  let input = cell.querySelector('.point-cell-input');
  if (!input || input.pointEdit.target !== target || input.pointEdit.field !== field) {
    if (input) commitPointValue(input);
    input = document.createElement('input');
    input.type = 'text';
    input.className = 'point-cell-input' + (field === 'value' ? ' point-value-input' : '');
    input.dataset.pointField = field;
    input.inputMode =
      field === 'name'
        ? 'text'
        : field === 'hex'
          ? 'text'
          : field === 'value'
            ? 'decimal'
            : 'numeric';
    input.setAttribute(
      'aria-label',
      { address: '点位', hex: '码值', value: '数值', quality: '品质', name: '描述' }[field] +
        ' ' +
        p.address,
    );
    if (field === 'name') input.maxLength = 200;
    input.pointEdit = {
      target,
      field,
      base: field === 'hex' ? p.value : p[field],
      dirty: false,
      pending: false,
    };
    cell.replaceChildren(input);
    input.addEventListener('input', () => {
      input.pointEdit.dirty = true;
      input.removeAttribute('aria-invalid');
    });
    input.addEventListener('blur', () =>
      commitPointValue(input).then(() => {
        pointKey = '';
        renderPoints(current());
      }),
    );
    input.addEventListener('keydown', (e) => {
      if (e.isComposing) return;
      if (e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        commitPointValue(input);
      } else if (e.key === 'Escape' && !input.pointEdit.pending) {
        e.preventDefault();
        e.stopPropagation();
        input.value = input.pointEdit.displayBase;
        input.pointEdit.dirty = false;
        input.removeAttribute('aria-invalid');
      }
    });
  }
  if (!input.pointEdit.dirty && !input.pointEdit.pending && document.activeElement !== input) {
    input.value = field === 'hex' ? pointHex(p) : String(p[field] ?? '');
    input.pointEdit.displayBase = input.value;
    input.pointEdit.base = field === 'hex' ? p.value : p[field];
    input.setAttribute(
      'aria-description',
      field === 'hex'
        ? '完整码值：' + fullPointHex(p) + '；回车或离开提交，Esc取消'
        : field === 'quality'
          ? '0 正常；可输入十进制或0x品质码'
          : field === 'value' && ['yx', 'yk', 'coil', 'discrete'].includes(p.kind)
            ? '0 分 / 1 合；回车或离开提交'
            : '回车或离开提交，Esc取消',
    );
  }
}
function renderPoints(c) {
  const query = $('point-search').value.trim().toLowerCase(),
    kind = $('point-kind').value;
  const key = JSON.stringify([
    c?.id,
    c?.role,
    c?.protocol,
    selectedDevice,
    incrementalView
      ? [
          device()?._metadata,
          kind ? device()?._categoryVersions?.[kind.replace(/^tab:/, '')] : device()?._pointVersion,
        ]
      : [c?.points, device()?.configuration?.controls],
    query,
    kind,
    pointPage,
  ]);
  if (key === pointKey) return;
  pointKey = key;
  const all = (c?.points || [])
    .filter((p) => pointMatchesKind(p, kind) && pointMatchesSearch(p, query))
    .sort((a, b) => a.address - b.address || a.kind.localeCompare(b.kind));
  const pages = Math.max(1, Math.ceil(all.length / POINT_PAGE_SIZE));
  pointPage = Math.min(pointPage, pages - 1);
  const points = all.slice(pointPage * POINT_PAGE_SIZE, (pointPage + 1) * POINT_PAGE_SIZE);
  $('point-badge').textContent = String(all.length);
  $('point-badge').setAttribute(
    'aria-description',
    `当前列表 ${all.length} 点，装置共 ${c?.points.length || 0} 点`,
  );
  $('point-empty').textContent = query || kind ? '无匹配结果' : '暂无数据';
  $('point-empty').hidden = points.length > 0;
  $('point-pager').hidden = pages === 1;
  $('point-page').textContent = `${pointPage + 1} / ${pages}`;
  $('point-prev').disabled = pointPage === 0;
  $('point-next').disabled = pointPage === pages - 1;
  const command = ['yk', 'yt'].includes(kind.replace(/^tab:/, '')),
    binding = command && c?.role === 'server',
    fields = command
      ? binding
        ? ['address', 'binding', 'value', 'name']
        : ['address', 'value', 'name']
      : ['address', 'hex', 'value', 'quality', 'name'];
  const table = document.querySelector('.point-table'),
    header = table?.querySelector('thead tr'),
    labels = {
      address: '点位',
      hex: '码值',
      value: binding ? '下发值' : '值 / 状态',
      quality: '品质',
      name: '描述',
      binding: kind.replace(/^tab:/, '') === 'yk' ? '绑定遥信' : '绑定遥测',
    };
  if (table)
    table.dataset.columns = binding ? 'server-command' : command ? 'client-command' : 'telemetry';
  if (header) header.innerHTML = fields.map((field) => '<th>' + labels[field] + '</th>').join('');
  const scroll = $('point-scroll').scrollTop,
    tbody = $('point-rows');
  const existing = new Map([...tbody.children].map((row) => [row.dataset.pointKey, row]));
  points.forEach((p, index) => {
    const key = keyOf(p);
    let row = existing.get(key);
    if (!row) {
      row = document.createElement('tr');
      row.dataset.pointKey = key;
    }
    while (row.children.length < fields.length) row.append(document.createElement('td'));
    while (row.children.length > fields.length) row.lastElementChild.remove();
    existing.delete(key);
    if (
      !row.pointTarget ||
      row.pointTarget.channel !== c.id ||
      row.pointTarget.device !== selectedDevice
    ) {
      commitPointRow(row);
      row.pointTarget = {
        channel: c.id,
        device: selectedDevice,
        key: { kind: p.kind, address: p.address },
        role: c.role,
        protocol: c.protocol,
        tail: Promise.resolve(),
        row,
      };
    }
    row.pointTarget.point = p;
    row.pointTarget.configuration = device()?.configuration;
    row.setAttribute(
      'aria-description',
      p.timestamp ? `事件时间 ${p.timestamp}${p.timeInvalid ? '（时间无效）' : ''}` : '',
    );
    fields.forEach((field, n) => {
      const cell = row.children[n];
      cell.className = field === 'value' ? 'point-value' : '';
      if (field === 'binding') {
        const cfg = device()?.configuration?.controls?.[pointCategory(p)],
          mapped = (cfg?.mappings || []).filter(
            (m) => m.source.kind === p.kind && m.source.address === p.address,
          );
        const button = cell.querySelector('button') || document.createElement('button');
        button.className = 'row-button point-binding';
        button.textContent = mapped.length
          ? mapped.map((m) => m.target.address + (m.invert ? '（反向）' : '')).join('，')
          : p.feedback || '';
        button.setAttribute(
          'aria-label',
          '绑定' + (pointCategory(p) === 'yk' ? '遥信' : '遥测') + ' ' + p.address,
        );
        button.onclick = () => openRemoteConfig(row.pointTarget.point);
        if (button.parentNode !== cell) cell.replaceChildren(button);
      } else if (binding && field === 'value') cell.textContent = String(p.value);
      else renderPointCell(cell, row.pointTarget, p, field);
    });
    if (tbody.children[index] !== row) tbody.insertBefore(row, tbody.children[index] || null);
  });
  for (const row of existing.values()) {
    commitPointRow(row);
    row.remove();
  }
  $('point-scroll').scrollTop = scroll;
}
function messageSummary(m) {
  return ['SYS', 'ERR'].includes(m.direction) && m.peer && !String(m.summary || '').includes(m.peer)
    ? `${m.summary || ''} · ${m.peer}`
    : m.summary || '';
}
function filteredMessages() {
  const source = paused ? frozen : snapshot.messages,
    query = $('message-search').value.trim().toLowerCase().replace(/\s+/g, ''),
    direction = $('direction-filter').value;
  return source.filter(
    (m) =>
      ($('all-messages').checked || m.channelId === selected) &&
      (!direction || direction === m.direction) &&
      (!query || (m.summary + m.hex + m.peer).toLowerCase().replace(/\s+/g, '').includes(query)),
  );
}
function renderMessages() {
  const source = paused ? frozen : snapshot.messages,
    query = $('message-search').value.trim().toLowerCase().replace(/\s+/g, ''),
    direction = $('direction-filter').value;
  const all = filteredMessages();
  const list = all.slice(-300);
  const key = JSON.stringify([
    selected,
    paused,
    query,
    direction,
    $('all-messages').checked,
    list.map((m) => m.sequence),
  ]);
  if (key === messageKey) return;
  messageKey = key;
  $('message-badge').textContent = all.length > 300 ? `300 / ${all.length}` : String(all.length);
  $('message-badge').setAttribute(
    'aria-description',
    '显示最近 300 条匹配报文；导出包含当前缓存全部报文',
  );
  $('message-empty').hidden = !!list.length;
  $('message-empty').textContent = query || direction ? '无匹配报文' : '暂无报文';
  const scroller = $('message-scroll'),
    oldTop = scroller.scrollTop;
  const append =
    shownMessages.length &&
    list.length >= shownMessages.length &&
    shownMessages.every((m, i) => list[i]?.sequence === m.sequence);
  const row = (m) =>
    `<tr><td>${escape(m.time)}</td><td><span class="direction ${escape(m.direction)}">${escape({ TX: 'Send', RX: 'Rec' }[m.direction] || m.direction)}</span></td><td><div class="message-line"><span class="message-summary">${escape(messageSummary(m))}${$('all-messages').checked ? ' · ' + escape(snapshot.project.channels.find((c) => c.id === m.channelId)?.name || m.channelId) : ''}</span>${m.hex ? `<button class="row-button copy-message" data-copy-message="${m.sequence}">复制</button>` : ''}</div>${m.hex ? `<div class="message-hex">${escape(m.hex.match(/.{1,2}/g)?.join(' '))}</div>` : ''}</td></tr>`;
  if (append) {
    const rows = document.createElement('tbody');
    rows.innerHTML = list.slice(shownMessages.length).map(row).join('');
    $('message-rows').append(...rows.children);
  } else $('message-rows').innerHTML = list.map(row).join('');
  shownMessages = list;
  scroller.scrollTop = $('follow-messages').checked ? scroller.scrollHeight : oldTop;
}

function ask({
  title,
  text = '',
  input = null,
  choices = [
    ['cancel', '取消'],
    ['ok', '确认'],
  ],
  danger = false,
}) {
  if (nativeDesktop && nativeContext?.kind !== 'confirm-dialog')
    return api('AskNative', { title, text, input, choices, danger });
  if (confirmResolver) return Promise.resolve({ choice: 'cancel' });
  $('confirm-title').textContent = title;
  $('confirm-text').textContent = text;
  $('confirm-text').hidden = !text;
  $('confirm-input').hidden = input === null;
  $('confirm-input').required = input !== null;
  $('confirm-input').value = input ?? '';
  $('confirm-input').setAttribute('aria-label', text === '名称' ? '名称' : title);
  $('confirm-actions').replaceChildren(
    ...choices.map(([value, label], index) => {
      const button = document.createElement('button');
      button.type = 'submit';
      button.value = value;
      button.textContent = label;
      button.formNoValidate = value === 'cancel';
      if (index === choices.length - 1) button.className = danger ? 'danger-solid' : 'primary';
      return button;
    }),
  );
  $('confirm-dialog').showModal();
  if (input !== null) {
    $('confirm-input').focus();
    $('confirm-input').select();
  } else $('confirm-actions').querySelector('button')?.focus();
  return new Promise((resolve) => {
    confirmResolver = resolve;
  });
}
function finishAsk(choice) {
  if (nativeContext?.kind === 'confirm-dialog') {
    api('ResolveNativeAsk', { choice, value: $('confirm-input').value.trim() })
      .then(() => window.go.main.App.CloseNativeWindow())
      .catch(report);
    return;
  }
  const resolve = confirmResolver;
  confirmResolver = null;
  const value = $('confirm-input').value.trim();
  $('confirm-dialog').close();
  resolve?.({ choice, value });
}
$('confirm-form').addEventListener('submit', (e) => {
  e.preventDefault();
  finishAsk(e.submitter?.value || 'cancel');
});
$('confirm-dialog').addEventListener('cancel', (e) => {
  e.preventDefault();
  finishAsk('cancel');
});
$('confirm-input').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    submitWithButton($('confirm-form'), $('confirm-actions').lastElementChild);
  }
});
async function confirmAction(title, text, danger = false) {
  return (await ask({ title, text, danger })).choice === 'ok';
}
async function prepareReplace() {
  const dirty = snapshot.dirty,
    hasActive = snapshot.states.some(active);
  if (!dirty && !hasActive) return true;
  const answer = await ask({
    title: '更换工程',
    text: dirty ? '保存当前更改？更换后会停止现有通道。' : '将停止现有通道。',
    choices: dirty
      ? [
          ['cancel', '取消'],
          ['discard', '不保存'],
          ['save', '保存并继续'],
        ]
      : [
          ['cancel', '取消'],
          ['ok', '继续'],
        ],
  });
  if (answer.choice === 'cancel') return false;
  if (answer.choice === 'save') {
    if (!(await act('SaveProject', false))) return false;
  }
  return true;
}
function splitEndpoint(value) {
  const m = /^\[([^\]]+)\]:(\d+)$/.exec(value) || /^(.+):(\d+)$/.exec(value);
  return m ? [m[1], m[2]] : [value, '0'];
}
function endpoint(host, port, allowZero = false) {
  host = String(host ?? '')
    .trim()
    .replace(/^\[(.*)\]$/, '$1');
  if (allowZero && host.toLowerCase() === 'any') host = '0.0.0.0';
  port = String(port ?? '').trim();
  if (!host) throw Error('请填写 IP 或主机名');
  if (!/^\d+$/.test(port) || Number(port) > 65535 || Number(port) < (allowZero ? 0 : 1))
    throw Error('端口应为 ' + (allowZero ? '0' : '1') + '–65535 整数');
  return (host.includes(':') ? '[' + host + ']' : host) + ':' + Number(port);
}
function originalControl(host, id) {
  return host.querySelector('[data-original-id="' + id + '"]');
}
const networkEnabled = new Set([
  'rbCom',
  'rbTcp',
  'rbUDP',
  'rbClient',
  'rbServer',
  'cbComList',
  'cbBaudRate',
  'cbDataBit',
  'cbParity',
  'cbStopBit',
  'txtClientIP',
  'txtClientPort',
  'cbxServerIP',
  'txtServerPort',
  'ck_AssignIpPort',
  'cb_ClientAssignIp',
  'txt_ClientAssignPort',
]);
let networkChoicesGeneration = 0,
  configWindowFit = Promise.resolve();
function fitConfigWindow() {
  if (!nativeContext || !['channel-dialog', 'device-dialog'].includes(nativeContext.kind)) return;
  configWindowFit = new Promise((resolve) =>
    requestAnimationFrame(() => {
      const form = $(nativeContext.kind === 'channel-dialog' ? 'channel-form' : 'device-form');
      const width = nativeContext.kind === 'channel-dialog' ? 480 : 640;
      const height = Math.max(
        110,
        Math.min(
          Math.ceil(form.getBoundingClientRect().height) + 20,
          (screen.availHeight || 900) - 96,
        ),
      );
      window.go.main.App.FitConfigWindow(window.innerWidth, window.innerHeight, width, height)
        .catch(report)
        .finally(resolve);
    }),
  );
  return configWindowFit;
}
async function loadNetworkChoices() {
  const generation = ++networkChoicesGeneration;
  const results = await Promise.allSettled([api('SerialPorts'), api('LocalIPs')]);
  if (generation !== networkChoicesGeneration) return;
  const ports =
    results[0].status === 'fulfilled' && Array.isArray(results[0].value) ? results[0].value : [];
  const ips =
    results[1].status === 'fulfilled' && Array.isArray(results[1].value)
      ? results[1].value
      : ['Any'];
  for (const [id, options] of [
    ['cbComList', ports],
    ['cbxServerIP', ips],
    ['cb_ClientAssignIp', ips.filter((ip) => ip !== 'Any')],
  ]) {
    const list = $('choices-' + id);
    if (list)
      list.innerHTML = options.map((v) => `<option value="${escape(v)}"></option>`).join('');
  }
  const input = originalControl($('original-network-panel'), 'cbComList');
  if (input && !input.value && ports.length) input.value = ports[0];
  $('network-port-status').textContent = ports.length ? '串口 ' + ports.length + ' 个' : '无串口';
}
function channelDialog(edit = true) {
  const c = channel();
  if (!c) return;
  const f = $('channel-form');
  f.reset();
  f.dataset.section = 'transport';
  f.dataset.channel = c.id;
  f.querySelector('.form-error').hidden = true;
  for (const el of f.querySelectorAll('.form-grid input,.form-grid select')) el.disabled = true;
  for (const [k, v] of Object.entries(c)) if (field(f, k)) field(f, k).value = v;
  for (const name of ['unit', 'commonAddress']) field(f, name).value = 1;
  const [host, port] = splitEndpoint(c.address),
    [local, localPort] = splitEndpoint(c.localAddress || '127.0.0.1:7777');
  const v = {
    cbComList: '',
    cbBaudRate: String(c.baud),
    cbDataBit: String(c.dataBits),
    cbParity: { N: '无校验', O: '奇校验', E: '偶校验' }[c.parity],
    cbStopBit: String(c.stopBits),
    txtClientIP: '127.0.0.1',
    txtClientPort: '2404',
    cbxServerIP: 'Any',
    txtServerPort: '2404',
    ck_AssignIpPort: false,
    cb_ClientAssignIp: local,
    txt_ClientAssignPort: localPort,
    ...c.transportDraft,
    rbCom: c.transport === 'serial',
    rbTcp: c.transport.startsWith('tcp'),
    rbUDP: c.transport === 'udp',
    rbClient: c.transport !== 'tcp-server',
    rbServer: c.transport === 'tcp-server',
  };
  if (c.transport === 'serial') v.cbComList = c.address;
  else if (c.transport === 'tcp-server') {
    v.cbxServerIP = host === '0.0.0.0' ? 'Any' : host;
    v.txtServerPort = port;
  } else {
    v.txtClientIP = host;
    v.txtClientPort = port;
  }
  if (c.transport === 'udp') {
    v.cbxServerIP = c.localAddress ? (local === '0.0.0.0' ? 'Any' : local) : 'Any';
    v.txtServerPort = c.localAddress ? localPort : '0';
    v.rbServer = c.transportDraft?.rbServer === 'true';
    v.rbClient = !v.rbServer;
  } else if (c.transport === 'tcp-client' && c.localAddress) {
    v.ck_AssignIpPort = true;
    v.cb_ClientAssignIp = local;
    v.txt_ClientAssignPort = localPort;
  }
  globalThis.renderOriginalForm($('original-network-panel'), 'network', networkEnabled, v);
  const panel = $('original-network-panel'),
    net = panel.querySelector('[data-group-id="groupBoxEx3"]'),
    roles = document.createElement('div');
  roles.className = 'network-role-row';
  roles.append(
    originalControl(panel, 'rbClient').closest('label'),
    originalControl(panel, 'rbServer').closest('label'),
  );
  net.querySelector('.original-group-title').after(roles);
  const other = panel.querySelector('[data-group-id="groupControl1"] .original-group-title');
  if (other) other.textContent = '本地绑定';
  panel
    .querySelector('.original-submit')
    .insertAdjacentHTML(
      'afterbegin',
      '<button type="button" data-close="channel-dialog">取消</button>',
    );
  $('network-ports').textContent = '刷新';

  $('channel-dialog-title').textContent = '通信配置窗口';
  updateChannelFields();
  if (!$('channel-dialog').open) $('channel-dialog').show();
  originalControl($('original-network-panel'), 'rbTcp')?.focus();
  loadNetworkChoices().catch(report);
  fitConfigWindow();
}
function updateChannelFields() {
  const host = $('original-network-panel');
  if (!host.children.length) return;
  const get = (id) => originalControl(host, id),
    com = get('rbCom').checked,
    udp = get('rbUDP').checked,
    server = get('rbServer').checked;
  const bind = !com && !udp && !server && get('ck_AssignIpPort').checked;
  for (const id of ['cbComList', 'cbBaudRate', 'cbDataBit', 'cbParity', 'cbStopBit'])
    get(id).disabled = !com;
  get('rbClient').disabled = com;
  get('rbServer').disabled = com;
  get('ck_AssignIpPort').disabled = com || udp || server;
  for (const [id, enabled, port] of [
    ['txtClientIP', !com && (!server || udp), false],
    ['txtClientPort', !com && (!server || udp), true],
    ['cbxServerIP', !com && (server || udp), false],
    ['txtServerPort', !com && (server || udp), true],
    ['cb_ClientAssignIp', bind, false],
    ['txt_ClientAssignPort', bind, true],
  ]) {
    const input = get(id);
    input.disabled = !enabled;
    input.required = enabled;
    const type = port ? 'number' : 'text';
    if (input.type !== type) input.type = type;
    if (port) {
      input.min = id === 'txtClientPort' ? '1' : '0';
      input.max = '65535';
      input.step = '1';
    }
  }
  const c = channel(),
    st = snapshot.states.find((st) => st.id === c?.id) || {},
    linked = active(st);
  host.querySelector('[data-group-id="groupBoxEx2"]').hidden = !com;
  host.querySelector('[data-group-id="groupBoxEx3"]').hidden = com;
  host.querySelector('[data-group-id="groupControl1"]').hidden = com || udp || server;
  for (const id of ['txtClientIP', 'txtClientPort', 'cbxServerIP', 'txtServerPort'])
    get(id).closest('label').hidden = com || get(id).disabled;
  $('network-port-status').hidden = !com;
  setHTML(
    'network-summary',
    '<strong>' +
      escape(c?.name || '未选择通道') +
      '</strong><span class="network-state ' +
      (linked ? 'online' : '') +
      '">' +
      escape(statuses[st.status] || '已停止') +
      '</span>',
  );
  const submit = host.querySelector('button[type=submit]');
  if (submit.textContent !== '应用') submit.textContent = '应用';
  submit.setAttribute(
    'aria-description',
    linked ? '通信参数变化后停止通道，保存后请手动连接；自动任务结束' : '保存通信配置',
  );
  if (!c || busy) for (const input of host.querySelectorAll('input,select')) input.disabled = true;
}
function nextDefaultName(prefix, names, start = 0) {
  const used = new Set(names);
  let n = start;
  while (used.has(prefix + n)) n++;
  return prefix + n;
}
on('add-channel', 'click', async () => {
  const role = stationMode,
    name = nextDefaultName(
      '通道',
      snapshot.project.channels.filter((c) => c.role === role).map((c) => c.name),
      1,
    );
  const c = {
    name,
    protocol: 'raw',
    role,
    transport: role === 'server' ? 'tcp-server' : 'tcp-client',
    address: role === 'server' ? '0.0.0.0:2404' : '127.0.0.1:2404',
    localAddress: '',
    timeoutMs: 3000,
    unit: 1,
    commonAddress: 1,
    baud: 9600,
    dataBits: 8,
    parity: 'N',
    stopBits: 1,
    points: [],
  };
  const id = await act('UpsertChannel', c);
  selectChannel(id);
});
on('edit-channel', 'click', () => channelDialog(true, 'transport'), false);
on('device-config', 'click', () => deviceDialog(true), false);
on('add-device', 'click', () => deviceDialog(false), false);
on('channel-form', 'submit', async (e) => {
  e.preventDefault();
  if (nativeContext?.kind === 'channel-dialog') {
    const c = await window.go.main.App.GetNativeContext();
    selected = c.channel;
    await refresh(true);
  }
  const c = channel();
  if (!c) throw Error('通道已被移除');
  const v = globalThis.readOriginalForm($('original-network-panel')),
    com = v.rbCom,
    udp = v.rbUDP,
    server = !com && !udp && v.rbServer;
  const transport = com ? 'serial' : udp ? 'udp' : server ? 'tcp-server' : 'tcp-client';
  const data = {
    ...c,
    transport,
    address: com
      ? v.cbComList.trim()
      : endpoint(
          server ? v.cbxServerIP : v.txtClientIP,
          server ? v.txtServerPort : v.txtClientPort,
          server,
        ),
    localAddress: udp
      ? endpoint(v.cbxServerIP, v.txtServerPort, true)
      : !com && !server && v.ck_AssignIpPort
        ? endpoint(v.cb_ClientAssignIp, v.txt_ClientAssignPort, true)
        : '',
    baud: Number(v.cbBaudRate),
    dataBits: Number(v.cbDataBit),
    parity: { 无校验: 'N', 奇校验: 'O', 偶校验: 'E' }[v.cbParity],
    stopBits: Number(v.cbStopBit),
    transportDraft: Object.fromEntries(
      Object.entries(v).map(([key, value]) => [key, String(value)]),
    ),
  };
  try {
    await act('UpdateTransport', data);
  } catch (error) {
    await refresh(true);
    throw error;
  }
  $('channel-dialog').close();
  toast('通信配置已应用；参数变更后请手动连接');
});
on(
  'channel-form',
  'change',
  (e) => {
    if (!e.target.matches('[type=radio],[type=checkbox]')) return;
    updateChannelFields();
    fitConfigWindow();
  },
  false,
);
on('list-ports', 'click', loadNetworkChoices);
const deviceWindowCache = new Map(),
  devicePanelCache = new Map();
$('device-protocol').addEventListener('focus', rememberDevicePanel);
$('original-device-panel').addEventListener('input', rememberDevicePanel);
$('original-device-panel').addEventListener('change', rememberDevicePanel);
function rememberDevicePanel() {
  const f = $('device-form');
  if (!f.dataset.role || f.dataset.device) return;
  const role = f.dataset.role,
    group = $('device-protocol').value;
  if (group) {
    devicePanelCache.set(role + '/' + group, {
      ...globalThis.readOriginalForm($('original-device-panel')),
      ...Object.fromEntries(
        [...$('original-device-panel').querySelectorAll('[data-extra-setting]')].map((el) => [
          el.dataset.extraSetting,
          String(el.type === 'checkbox' ? el.checked : el.value),
        ]),
      ),
    });
    deviceWindowCache.set(role, { group, version: $('device-version').value });
    if (nativeContext?.kind === 'device-dialog' && !nativeContext.edit)
      api('SaveNativeDraft', {
        window: deviceWindowCache.get(role),
        panels: [...devicePanelCache].filter(([k]) => k.startsWith(role + '/')),
      }).catch(report);
  }
}
function supportedDeviceControls(group) {
  const key = $('device-form').dataset.role + '-' + group,
    controls = globalThis.originalForms[key]?.controls || {};
  if (group === 'modbus' && controls.cb_ycFormat)
    controls.cb_ycFormat.options = [
      '0:无符号短整型(Ushort)',
      '1:有符号短整型(Short)',
      '2:无符号整型(UInt)',
      '3:有符号整型(Int)',
      '4:单精度浮点(Float)',
    ];
  return new Set([
    'txtYT_Add',
    'txtYT_Num',
    ...Object.values(controls)
      .filter((c) => ['TextEdit', 'ComboBoxEdit', 'CheckEdit'].includes(c.type))
      .map((c) => c.id),
  ]);
}
function renderExtraSettings(values, group, role) {
  const host = $('original-device-panel'),
    submit = host.querySelector('.original-submit');
  const fields =
    group === 'iec104'
      ? [
          ['sendWindow', '未确认发送窗口', 12],
          ['ackDelayMs', 'S帧最长等待(ms)', 1000],
          ...(role === 'server'
            ? [
                ['txt_FrameOverTime', '分帧超时(ms)', 3000],
                ['responseMode', '应答策略', 'normal', ['normal', 'deny', 'silent']],
                ['responseDelayMs', '应答延迟(ms)', 0],
              ]
            : []),
        ]
      : role === 'server'
        ? [
            ['clockEnabled', '启用校时寄存器', false],
            ['TimeAddr', '校时首点位', '1000H'],
          ]
        : [
            ['pollEnabled', '周期采集', true],
            ['eventCount', '事件读取寄存器数', 16],
          ];
  const section = document.createElement('section');
  section.className = 'original-group';
  section.innerHTML =
    '<div class="original-group-title">' +
    (group === 'iec104' ? '链路与应答' : '采集与校时') +
    '</div>' +
    fields
      .map(([key, label, initial, options]) => {
        const v = values[key] ?? initial;
        return options
          ? `<label class="original-field"><span>${label}</span><select data-extra-setting="${key}">${options.map((o) => `<option value="${o}" ${o === v ? 'selected' : ''}>${{ normal: '正常', deny: '否认', silent: '不应答' }[o]}</option>`).join('')}</select></label>`
          : typeof initial === 'boolean'
            ? `<label class="original-check"><input type="checkbox" data-extra-setting="${key}" ${v === true || v === 'true' ? 'checked' : ''}>${label}</label>`
            : `<label class="original-field"><span>${label}</span><input data-extra-setting="${key}" type="${typeof initial === 'number' ? 'number' : 'text'}" value="${escape(v)}"></label>`;
      })
      .join('');
  host.insertBefore(section, submit);
  {
    const range = document.createElement('section');
    range.className = 'original-group';
    range.innerHTML =
      '<div class="original-group-title">遥调点表</div>' +
      [
        ['txtYT_Add', '遥调首点位', group === 'iec104' ? 25089 : 3072],
        ['txtYT_Num', '遥调个数', 50],
      ]
        .map(
          ([key, label, initial]) =>
            `<label class="original-field"><span>${label}</span><input data-original-id="${key}" type="text" value="${escape(values[key] ?? initial)}"></label>`,
        )
        .join('');
    host.insertBefore(range, submit);
  }
}
function organizeAdvancedDevice(role, group) {
  const host = $('original-device-panel'),
    advanced = document.createElement('details');
  advanced.className = 'advanced-fields original-advanced';
  advanced.innerHTML = '<summary>高级</summary><div class="advanced-grid"></div>';
  const ids = [
    'txtCOT',
    'textEdit2',
    'txtASDU_Byte',
    'textEdit3',
    'txtMess_Byte',
    'sendWindow',
    'ackDelayMs',
    'txt_FrameOverTime',
    'responseMode',
    'responseDelayMs',
    'cb_ycFunWay',
    'cb_mcFunWay',
    'cb_check',
    'txtUTest_Time',
    'txt_replyIFrame',
    'text_replySICount',
    'ck_IsDataSentWaitS',
    ...(role === 'server' && group === 'iec104' ? ['txtAdd'] : []),
  ];
  for (const id of ids) {
    const input = host.querySelector(
        '[data-original-id="' + id + '"],[data-extra-setting="' + id + '"]',
      ),
      label = input?.closest('label');
    if (label && !label.hidden) advanced.querySelector('div').append(label);
  }
  if (advanced.querySelector('input,select'))
    host.insertBefore(advanced, host.querySelector('.original-submit'));
  for (const section of [...host.querySelectorAll('.original-group')])
    if (!section.querySelector('input,select,button')) section.remove();
  advanced.addEventListener('toggle', () => fitConfigWindow());
}
function updateDeviceFields(restore = null) {
  const f = $('device-form'),
    group = $('device-protocol').value,
    role = f.dataset.role;
  if (!group) {
    $('original-device-panel').replaceChildren();
    fitConfigWindow();
    return;
  }
  const protocol =
    group === 'iec104'
      ? 'iec104'
      : $('device-version').value === 'TCP版'
        ? 'modbus-tcp'
        : 'modbus-rtu';
  field(f, 'protocol').value = protocol;
  const values = {
      ...(!f.dataset.device && group === 'iec104' ? { cbYC_Type: '带品质短浮点(type:13)' } : {}),
      ...(restore || devicePanelCache.get(role + '/' + group) || {}),
    },
    enabled = supportedDeviceControls(group);
  globalThis.renderOriginalForm(
    $('original-device-panel'),
    role + '-' + group,
    enabled,
    values,
    false,
  );
  if (group === 'iec104' && values.txt_FrameOverTime === undefined) {
    const timeout = originalControl($('original-device-panel'), 'txt_FrameOverTime');
    if (timeout) timeout.value = '3000';
  }
  renderExtraSettings(values, group, role);
  globalThis.deviceEditor.organizeFour($('original-device-panel'));
  updateByteOrders();
  globalThis.deviceEditor.enhance(
    $('original-device-panel'),
    role,
    group,
    $('device-version').value,
  );
  organizeAdvancedDevice(role, group);
  if (values.cbYC_Type !== undefined) {
    const telemetryType = originalControl($('original-device-panel'), 'cbYC_Type');
    if (telemetryType) telemetryType.value = values.cbYC_Type;
  }
  const addressID = role === 'server' && group === 'iec104' ? 'txt_DeviceAddr' : 'txtAdd',
    address = originalControl($('original-device-panel'), addressID)?.value || '1';
  field(f, protocol === 'iec104' ? 'commonAddress' : 'unit').value = address;
  fitConfigWindow();
}
function devicePointPreview(configuration, protocol) {
  const f = $('device-form'),
    old = snapshot.project.channels
      .find((c) => c.id === (f.dataset.channel || selected))
      ?.devices.find((d) => d.id === f.dataset.device),
    points = old?.points || [];
  const occupied = globalThis.deviceEditor.validate(
      $('original-device-panel'),
      configuration,
      protocol,
    ),
    before = new Set(points.map(keyOf));
  const unchanged =
    old &&
    (old.configuration
      ? JSON.stringify(old.configuration.ranges) === JSON.stringify(configuration.ranges)
      : configuration.ranges.every((r) => {
          const ps = points.filter((p) => p.kind === r.kind && (!p.tab || p.tab === r.tab));
          const selected = r.selection
            ? new Set(globalThis.pointSelection.parse(r.selection))
            : null;
          return (
            ps.length === r.count &&
            (!ps.length || Math.min(...ps.map((p) => p.address)) === r.address) &&
            (!selected || ps.every((p) => selected.has(p.address)))
          );
        }));
  const after = new Set(unchanged ? before : []);
  if (!unchanged) {
    for (const r of occupied)
      for (const point of r.points) after.add(keyOf({ kind: r.kind, address: point }));
    for (const p of points)
      if (!configuration.ranges.some((r) => r.kind === p.kind)) after.add(keyOf(p));
  }
  const kept = [...before].filter((k) => after.has(k)).length;
  return { kept, added: after.size - kept, removed: before.size - kept, ranges: occupied };
}
for (const event of ['input', 'change'])
  $('original-device-panel').addEventListener(event, () => {
    globalThis.deviceEditor.dependencies($('original-device-panel'));
    globalThis.deviceEditor.clear($('original-device-panel'));
  });
function fillDeviceVersions(group, value) {
  const versions =
    group === 'iec104' ? ['标准版'] : group === 'modbus' ? ['标准版(RTU)', 'TCP版'] : [];
  $('device-version').innerHTML = versions.map((v) => `<option >${v}</option>`).join('');
  $('device-version').value = versions.includes(value) ? value : versions[0] || '';
}
function deviceDialog(edit = false) {
  const c = channel(),
    d = edit ? device() : null;
  if (edit && !d) return;
  rememberDevicePanel();
  const f = $('device-form');
  f.reset();
  f.querySelector('.form-error').hidden = true;
  f.dataset.role = stationMode;
  f.dataset.device = d?.id || '';
  f.dataset.channel = edit ? c.id : '';
  field(f, 'name').value =
    d?.name ||
    nextDefaultName(
      '装置',
      (c?.devices || []).map((d) => d.name),
      1,
    );
  field(f, 'name').readOnly = false;
  f.devicePointSelections = {};
  f.deviceSourceValues = { ...(d?.configuration?.values || {}) };
  f.deviceSourceRanges = cloneBridgeData(d?.configuration?.ranges || []);
  f.deviceInitial = d ? JSON.stringify(d.configuration) : null;
  const suffix = stationMode === 'server' ? '子站' : '主站';
  $('device-dialog-title').textContent = edit ? '查看/修改装置配置' : suffix + '增加装置';
  $('device-protocol').innerHTML =
    '<option value="">请选择</option>' +
    [
      ['iec104', '104' + suffix],
      ['modbus', 'Modbus' + suffix],
    ]
      .map(([key, name]) => `<option value="${key}" >${name}</option>`)
      .join('');
  const saved = deviceWindowCache.get(stationMode),
    group = edit ? (c.protocol === 'iec104' ? 'iec104' : 'modbus') : saved?.group || 'iec104';
  const version = edit
    ? c.protocol === 'iec104'
      ? '标准版'
      : c.protocol === 'modbus-tcp'
        ? 'TCP版'
        : '标准版(RTU)'
    : saved?.version;
  $('device-protocol').value = group;
  $('device-protocol').disabled = edit;
  fillDeviceVersions(group, version);
  $('device-version').disabled = edit;
  let values = null;
  if (edit) {
    values = { ...(d.configuration?.values || {}) };
    values[stationMode === 'server' && group === 'iec104' ? 'txt_DeviceAddr' : 'txtAdd'] = String(
      group === 'iec104' ? d.commonAddress : d.unit,
    );
    if (!d.configuration)
      for (const [prefix, kind] of Object.entries(
        group === 'iec104'
          ? { YX: 'yx', YC: 'yc', MC: 'mc', YK: 'yk' }
          : { YX: 'discrete', YC: 'holding', MC: 'input', YK: 'coil' },
      )) {
        const points = d.points.filter((p) => p.kind === kind);
        values['txt' + prefix + '_Add'] = String(
          points.length ? Math.min(...points.map((p) => p.address)) : 0,
        );
        values['txt' + prefix + '_Num'] = String(points.length);
      }
  }
  if (edit) {
    for (const [prefix, tab, kind] of group === 'iec104'
      ? [
          ['YX', 'yx', 'yx'],
          ['YC', 'yc', 'yc'],
          ['MC', 'mc', 'mc'],
          ['YK', 'yk', 'yk'],
          ['YT', 'yt', 'yt'],
          ['ST', 'step', 'step'],
        ]
      : [
          ['YX', 'yx', 'discrete'],
          ['YC', 'yc', 'holding'],
          ['MC', 'mc', 'input'],
          ['YK', 'yk', 'coil'],
          ['JDQ', 'jdq', 'coil'],
          ['YT', 'yt', 'holding'],
        ]) {
      const range = d.configuration?.ranges?.find((r) => r.tab === tab),
        points = d.points.filter((p) =>
          range
            ? p.tab === tab || (!p.tab && p.kind === range.kind)
            : p.tab === tab ||
              (!p.tab && p.kind === kind && tab !== 'jdq' && (tab !== 'yt' || group === 'iec104')),
        );
      if (values['txt' + prefix + '_Add'] === undefined)
        values['txt' + prefix + '_Add'] = String(
          range?.address ??
            (points.length
              ? Math.min(...points.map((p) => p.address))
              : prefix === 'YT'
                ? group === 'iec104'
                  ? 25089
                  : 3072
                : prefix === 'ST'
                  ? 24833
                  : 0),
        );
      if (values['txt' + prefix + '_Num'] === undefined)
        values['txt' + prefix + '_Num'] = String(range?.count ?? points.length);
    }
    if (!d.configuration && group === 'modbus') {
      for (const [tab, kind] of [
        ['yc', 'holding'],
        ['mc', 'input'],
      ]) {
        const p = d.points.find((p) => p.kind === kind);
        if (!p?.dataType) continue;
        const typeIndex = (
          tab === 'mc'
            ? ['uint32', 'int32', 'float32']
            : ['uint16', 'int16', 'uint32', 'int32', 'float32']
        ).indexOf(p.dataType);
        if (typeIndex >= 0)
          values['cb_' + tab + (stationMode === 'server' ? 'Type' : 'Format')] = String(typeIndex);
        const orderIndex = (
          p.dataType.endsWith('32') ? ['ABCD', 'BADC', 'CDAB', 'DCBA'] : ['AB', 'BA']
        ).indexOf(p.byteOrder || '');
        if (orderIndex >= 0) values['cb_' + tab + 'FunWay'] = String(orderIndex);
      }
    }
  }
  if (edit)
    for (const category of ['yx', 'yc', 'yk', 'yt']) {
      const points = d.points.filter((p) => pointCategory(p) === category);
      values['txt' + category.toUpperCase() + '_Num'] = String(points.length);
      if (points.length)
        values['txt' + category.toUpperCase() + '_Add'] = String(
          Math.min(...points.map((p) => p.address)),
        );
    }
  updateDeviceFields(values);
  if (edit)
    for (const category of ['yx', 'yc', 'yk', 'yt']) {
      const points = d.points.filter((p) => pointCategory(p) === category),
        prefix = category.toUpperCase();
      f.devicePointSelections[category] = {
        address: Number(originalControl($('original-device-panel'), 'txt' + prefix + '_Add').value),
        count: points.length,
        width: globalThis.deviceEditor.pointWidth($('original-device-panel'), category),
        selection: globalThis.pointSelection.format(points.map((p) => p.address)),
      };
    }
  f.deviceInitial = edit
    ? JSON.stringify(configurationFromPanel(field(f, 'protocol').value))
    : null;
  f.deviceInitialName = d?.name;
  f.deviceInitialAddress = edit ? (c.protocol === 'iec104' ? d.commonAddress : d.unit) : null;
  if (!$('device-dialog').open) $('device-dialog').show();
  if (!edit) $('device-protocol').focus();
}
on(
  'device-protocol',
  'change',
  () => {
    fillDeviceVersions($('device-protocol').value);
    updateDeviceFields();
  },
  false,
);
on(
  'device-version',
  'change',
  () => {
    rememberDevicePanel();
    updateDeviceFields();
  },
  false,
);
function configurationFromPanel(protocol) {
  const values = globalThis.readOriginalForm($('original-device-panel')),
    ranges = [],
    modbus = protocol.startsWith('modbus');
  const definitions = [
    ['YX', 'yx', '遥信'],
    ['YC', 'yc', '遥测'],
    ['YK', 'yk', '遥控'],
    ['YT', 'yt', '遥调'],
  ];
  for (const [prefix, tab, name] of definitions) {
    const { address, count, selection } = globalThis.deviceEditor.pointRange(
      $('original-device-panel'),
      tab,
      modbus ? 65535 : 16777215,
    );
    values['txt' + prefix + '_Add'] = String(address);
    values['txt' + prefix + '_Num'] = String(count);
    let kind = tab;
    if (modbus) {
      const fc =
        prefix === 'YT'
          ? 6
          : Number(
              values[
                prefix === 'YX'
                  ? 'cbYX_Fun'
                  : prefix === 'YC'
                    ? 'cbYC_Fun'
                    : 'txt' + prefix + '_Fun'
              ] ??
                values['txt' + prefix + '_Fun'] ??
                values['txtYK_fun'],
            );
      const allowed =
        prefix === 'YX'
          ? [1, 2]
          : prefix === 'YC' || prefix === 'MC'
            ? [3, 4]
            : prefix === 'YK'
              ? [5, 6]
              : prefix === 'YT'
                ? [6]
                : [1];
      if (!allowed.includes(fc))
        globalThis.deviceEditor.error(
          prefix === 'YX'
            ? values.cbYX_Fun !== undefined
              ? 'cbYX_Fun'
              : 'txtYX_Fun'
            : prefix === 'YC'
              ? values.cbYC_Fun !== undefined
                ? 'cbYC_Fun'
                : 'txtYC_Fun'
              : prefix === 'YK'
                ? 'txtYK_fun'
                : 'txt' + prefix + '_Fun',
          name + '功能码仅支持 ' + allowed.join('/'),
        );
      kind = { 1: 'coil', 2: 'discrete', 3: 'holding', 4: 'input', 5: 'coil', 6: 'holding' }[fc];
    }
    ranges.push({ tab, kind, address, count, name, selection });
  }
  const activeValues = {
    ...($('device-form').deviceSourceValues || {}),
    ...Object.fromEntries(
      Object.entries(values)
        .filter(([key]) => supportedDeviceControls(modbus ? 'modbus' : 'iec104').has(key))
        .map(([key, value]) => [key, String(value)]),
    ),
  };
  for (const el of $('original-device-panel').querySelectorAll('[data-extra-setting]'))
    activeValues[el.dataset.extraSetting] = String(el.type === 'checkbox' ? el.checked : el.value);
  if ($('device-form').dataset.role === 'server') {
    delete activeValues.pollEnabled;
    delete activeValues.eventCount;
  }
  let savedRanges = ranges;
  if ($('device-form').dataset.device) {
    const source = $('device-form').deviceSourceRanges || [],
      updates = new Map(ranges.map((r) => [r.tab, r]));
    savedRanges = source.map((r) => {
      const change = updates.get(r.tab);
      updates.delete(r.tab);
      return change
        ? {
            ...r,
            address: change.address,
            count: change.count,
            kind: change.kind,
            selection: change.selection,
          }
        : cloneBridgeData(r);
    });
    for (const r of updates.values()) if (r.count > 0) savedRanges.push(r);
  }
  const config = { version: $('device-version').value, values: activeValues, ranges: savedRanges };
  globalThis.deviceEditor.validate($('original-device-panel'), config, protocol);
  return config;
}
on('device-form', 'submit', async (e) => {
  e.preventDefault();
  if (nativeContext?.kind === 'device-dialog' && !nativeContext.edit) {
    const context = await window.go.main.App.GetNativeContext();
    selected = context.channel;
    await refresh(true);
  }
  const f = $('device-form');
  const c = f.dataset.device
    ? snapshot.project.channels.find((c) => c.id === f.dataset.channel)
    : channel();
  if (!c || c.role !== f.dataset.role) throw Error('请先增加并选中通道');
  const protocol = field(f, 'protocol').value;
  if (!protocol) throw Error('请选择规约和版本');
  const name = field(f, 'name').value.trim();
  if (!name || [...name].length > 100)
    globalThis.deviceEditor.error('name', '装置名称应为 1–100 字');
  const configuration = configurationFromPanel(protocol),
    values = globalThis.readOriginalForm($('original-device-panel')),
    address = globalThis.deviceEditor.integer(
      values[f.dataset.role === 'server' && protocol === 'iec104' ? 'txt_DeviceAddr' : 'txtAdd'],
      f.dataset.role === 'server' && protocol === 'iec104' ? 'txt_DeviceAddr' : 'txtAdd',
      protocol === 'iec104' ? 1 : 0,
      protocol === 'iec104' ? 65534 : protocol === 'modbus-rtu' ? 247 : 255,
      true,
    );
  if (
    !String(
      values[f.dataset.role === 'server' && protocol === 'iec104' ? 'txt_DeviceAddr' : 'txtAdd'] ??
        '',
    ).trim() ||
    !Number.isInteger(address) ||
    address < 0 ||
    address > (protocol === 'iec104' ? 65534 : protocol === 'modbus-rtu' ? 247 : 255) ||
    (protocol === 'iec104' && address === 0) ||
    (protocol === 'modbus-rtu' && c.role === 'server' && address === 0)
  )
    throw Error('装置点位超出范围');
  if (
    f.dataset.device &&
    JSON.stringify(configuration) === f.deviceInitial &&
    name === f.deviceInitialName &&
    address === f.deviceInitialAddress
  ) {
    $('device-dialog').close();
    return;
  }
  let replace = false;
  const replace104 =
      protocol === 'iec104' && c.protocol === 'iec104' && !f.dataset.device && c.devices.length > 0,
    running = snapshot.states.some((st) => st.id === c.id && active(st));
  if (replace104 || (protocol !== c.protocol && (c.devices.length || running))) {
    let text = replace104
      ? '再次增加 IEC104 装置将替换原装置及其点表。'
      : c.devices.length
        ? `将移除当前通道的 ${c.devices.length} 个装置及其点表。`
        : '';
    if (running) text += '当前通道将停止。';
    if (
      !(await confirmAction(replace104 ? '替换 IEC104 装置' : '切换规约', text, !!c.devices.length))
    )
      return;
    replace = !!c.devices.length;
  }
  const preview = devicePointPreview(configuration, field(f, 'protocol').value);
  if (preview.removed > 0 && !replace) {
    if (
      !(await confirmAction(
        '确认点表变更',
        `新增 ${preview.added}，保留 ${preview.kept}，删除 ${preview.removed} 个点。删除的点值、品质和描述将移除。`,
        true,
      ))
    )
      return;
  }
  const existing = c.devices.find((d) => d.id === f.dataset.device),
    d = {
      id: replace ? '' : f.dataset.device,
      name,
      unit: protocol === 'iec104' ? 1 : address,
      commonAddress: protocol === 'iec104' ? address : 1,
      points: [],
      configuration,
    };
  await act('UpsertDevice', c.id, protocol, d, replace);
  if (f.dataset.device) {
    $('device-dialog').close();
  } else {
    if (protocol.startsWith('modbus'))
      originalControl($('original-device-panel'), 'txtAdd').value = String(
        Math.min(protocol === 'modbus-rtu' ? 247 : 255, address + 1),
      );
    rememberDevicePanel();
  }
  $('device-dialog').close();
  toast(existing ? '配置已保存，通道保持停止' : '装置已增加');
});
async function renameDevice() {
  await run('rename-device', async () => {
    const d = device(),
      id = selected;
    if (!d) return;
    const result = await ask({ title: '修改装置名称', input: d.name });
    if (result.choice === 'ok' && result.value) await act('RenameDevice', id, d.id, result.value);
  });
}
async function removeDevice() {
  await run('remove-device', async () => {
    const d = device(),
      id = selected;
    if (d && (await confirmAction('移除装置', `${d.name}及其点表将被移除。`, true))) {
      await act('DeleteDevice', id, d.id);
      selectDevice(channel()?.devices[0]?.id || '');
    }
  });
}
function pointDialog(key = null) {
  const c = current();
  if (!c) return;
  const original = key ? c.points.find((p) => keyOf(p) === key) : null;
  if (key && !original) {
    toast('此点已被删除', true);
    return;
  }
  const f = $('point-form');
  f.reset();
  f.querySelector('.form-error').hidden = true;
  const allowed =
    c.protocol === 'iec104'
      ? ['yx', 'yc', 'yk', 'yt', ...(original?.kind === 'step' ? ['step'] : [])]
      : c.protocol.startsWith('modbus')
        ? ['coil', 'discrete', 'holding', 'input']
        : ['yx', 'yc', 'holding'];
  field(f, 'kind').innerHTML = allowed
    .map((k) => `<option value="${k}">${kinds[k]}</option>`)
    .join('');
  const initialKind = allowed.includes($('point-kind').value)
    ? $('point-kind').value
    : activeRange()?.kind || allowed[0];
  let next = 0;
  const used = new Set(c.points.filter((p) => p.kind === initialKind).map((p) => p.address));
  while (used.has(next)) next++;
  const p = original || { kind: initialKind, address: next, name: '', value: 0, quality: 0 };
  for (const [k, v] of Object.entries(p)) if (field(f, k)) field(f, k).value = v;
  pointEditor = {
    channel: c.id,
    device: selectedDevice,
    key: original ? { kind: original.kind, address: original.address } : null,
    point: p,
    configuration: device()?.configuration,
    changed: new Set(),
  };
  $('point-dialog-title').textContent = original ? '编辑点' : '添加点';
  field(f, 'address').max = c.protocol.startsWith('modbus') ? 65535 : 16777215;
  updatePointLimits();
  $('point-dialog').showModal();
  field(f, original ? 'value' : 'address').focus();
}
function updatePointLimits() {
  if ($('point-feedback-label'))
    $('point-feedback-label').hidden = !(
      pointEditor?.point?.tab === 'yk' ||
      field($('point-form'), 'kind').value === 'yk' ||
      (field($('point-form'), 'kind').value === 'coil' && pointEditor?.point?.tab !== 'yx')
    );
  const f = $('point-form'),
    kind = field(f, 'kind').value,
    value = field(f, 'value'),
    p = pointEditor?.point?.kind === kind ? pointEditor.point : { kind },
    limits = pointLimits(p, pointEditor?.configuration);
  value.min = String(limits.min);
  value.max = String(limits.normalized ? 1 - Number.EPSILON / 2 : limits.max);
  value.step = limits.integer ? '1' : 'any';
}

on(
  'point-form',
  'input',
  (e) => {
    if (e.target.name) pointEditor?.changed.add(e.target.name);
  },
  false,
);
on(
  'point-form',
  'change',
  (e) => {
    if (e.target.name) pointEditor?.changed.add(e.target.name);
    if (e.target.name === 'kind') updatePointLimits();
  },
  false,
);
on('point-form', 'submit', async (e) => {
  e.preventDefault();
  if (!pointEditor) return;
  const f = $('point-form'),
    editor = pointEditor;
  const p = {
    kind: field(f, 'kind').value,
    address: Number(field(f, 'address').value),
    name: field(f, 'name').value,
    value: Number(field(f, 'value').value),
    quality: Number(field(f, 'quality').value),
    feedback: field(f, 'feedback').value.trim(),
  };
  await act('PatchDevicePoint', editor.channel, editor.device, editor.key, p, [...editor.changed]);
  $('point-dialog').close();
  pointEditor = null;
  toast('已保存');
});

document.addEventListener('click', async (e) => {
  const target = e.target.closest('button');
  if (!target) return;
  if (target.dataset.mode && !busy) {
    rememberDevicePanel();
    for (const dialog of document.querySelectorAll('dialog[open]')) dialog.close();
    const ready = api('SetWorkspaceMode', target.dataset.mode);
    saveDraft();
    stationMode = target.dataset.mode;
    selectChannel(snapshot.project.channels.find((c) => c.role === stationMode)?.id || '');
    ready.then(() => refresh(true)).catch(report);
    return;
  }
  if (target.dataset.device !== undefined && !busy) {
    selectDevice(target.dataset.device);
    if (incrementalView) refresh().catch(report);
    return;
  }
  if (target.dataset.pointKind) {
    $('point-kind').value = target.dataset.pointKind;
    pointPage = 0;
    saveDraft();
    renderPointTabs(current());
    renderActions(current());
    renderPoints(current());
    renderControls();
    return;
  }
  if (target.dataset.select !== undefined && !busy) {
    selectChannel(target.dataset.select);
    if (incrementalView) refresh().catch(report);
    return;
  }
  if (target.dataset.close) {
    if (!busy) {
      if (target.dataset.close === 'device-dialog') rememberDevicePanel();
      $(target.dataset.close).close();
    }
    return;
  }
  if (target.dataset.editPoint !== undefined && !busy) {
    pointDialog(target.dataset.editPoint);
    return;
  }
  if (target.dataset.copyMessage) {
    const m = shownMessages.find((m) => String(m.sequence) === target.dataset.copyMessage);
    if (m)
      try {
        await navigator.clipboard.writeText(m.hex.match(/.{1,2}/g).join(' '));
        toast('已复制');
      } catch (error) {
        report(Error('无法访问剪贴板'));
      }
    return;
  }
  if (target.dataset.deletePoint !== undefined) {
    await run('delete-point', async () => {
      const c = current(),
        p = c?.points.find((p) => keyOf(p) === target.dataset.deletePoint);
      if (
        p &&
        (await confirmAction(
          '删除点',
          `${kinds[p.kind]} · ${p.address}${p.name ? ' · ' + p.name : ''}`,
          true,
        ))
      )
        await act('DeleteDevicePoint', c.id, selectedDevice, { kind: p.kind, address: p.address });
    });
  }
});
on('delete-channel', 'click', async () => {
  const c = current();
  if (c && (await confirmAction('删除通道', `${c.name}及其点表将被删除。`, true)))
    await act('DeleteChannel', c.id);
});
on('toggle-channel', 'click', () => {
  const id = selected;
  return act(active(state()) ? 'StopChannel' : 'StartChannel', id);
});
on('raw-form', 'submit', async (e) => {
  e.preventDefault();
  const id = selected,
    text = $('raw-hex').value;
  if (!text.trim()) {
    $('raw-hex').focus();
    throw Error('请输入十六进制报文');
  }
  saveDraft();
  await act('SendHex', id, text, $('append-crc').checked);
});
for (const id of ['point-search', 'point-kind'])
  on(
    id,
    id === 'point-search' ? 'input' : 'change',
    () => {
      pointPage = 0;
      if (id === 'point-kind') {
        saveDraft();
        renderPointTabs(current());
        renderActions(current());
        renderControls();
      }
      renderPoints(current());
    },
    false,
  );
on(
  'point-prev',
  'click',
  () => {
    pointPage = Math.max(0, pointPage - 1);
    renderPoints(current());
    $('point-scroll').scrollTop = 0;
  },
  false,
);
on(
  'point-next',
  'click',
  () => {
    pointPage++;
    renderPoints(current());
    $('point-scroll').scrollTop = 0;
  },
  false,
);
for (const id of ['direction-filter', 'all-messages', 'message-search'])
  on(
    id,
    id === 'message-search' ? 'input' : 'change',
    () => {
      if (id === 'all-messages') loadPauseView();
      shownMessages = [];
      messageKey = '';
      renderMessages();
    },
    false,
  );
on(
  'follow-messages',
  'change',
  () => {
    if ($('follow-messages').checked)
      $('message-scroll').scrollTop = $('message-scroll').scrollHeight;
  },
  false,
);
$('message-scroll').addEventListener(
  'scroll',
  () => {
    const el = $('message-scroll');
    if (el.scrollHeight - el.scrollTop - el.clientHeight > 70) $('follow-messages').checked = false;
  },
  { passive: true },
);
on(
  'pause-messages',
  'click',
  () => {
    paused = !paused;
    if (paused) {
      frozen = cloneBridgeData(snapshot.messages);
      pausedViews.set(messageScope(), frozen);
    } else pausedViews.delete(messageScope());
    $('pause-messages').textContent = paused ? '继续' : '暂停';
    $('pause-messages').classList.toggle('is-paused', paused);
    renderMessages();
  },
  false,
);
on('clear-messages', 'click', async () => {
  const all = $('all-messages').checked,
    id = selected;
  if (all) {
    await act('ClearMessages');
    pausedViews.clear();
  } else {
    if (!id) return;
    await act('ClearChannelMessages', id);
    pausedViews.delete(id);
  }
  if (paused) pausedViews.set(messageScope(), []);
  frozen = [];
  shownMessages = [];
  messageKey = '';
  renderMessages();
});
on('export-messages', 'click', async () => {
  const rows = filteredMessages();
  if (!rows.length) throw Error('没有匹配报文');
  if (await act('ExportMessageRows', rows)) toast('报文已导出');
});

function resetProjectView() {
  runtimeViews.clear();
  runtimeDetailContexts.clear();
  pointContexts.clear();
  channelDevices.clear();
  viewCursor = {};
  paused = false;
  frozen = [];
  pausedViews.clear();
  shownMessages = [];
  messageKey = '';
  pointPage = 0;
  actionKey = '';
  drafts.clear();
  $('pause-messages').textContent = '暂停';
  $('pause-messages').classList.remove('is-paused');
  render();
}
let nextProjectNumber = 1;
function numberedProject(name) {
  const match = /^工程([1-9]\d*)$/.exec(name || ''),
    n = match ? Number(match[1]) : 0;
  return Number.isSafeInteger(n) && n < Number.MAX_SAFE_INTEGER ? n : 0;
}
on('new-project', 'click', async () => {
  const number = Math.max(nextProjectNumber, numberedProject(snapshot.project.name) + 1);
  const result = await ask({ title: '新建', input: '工程' + number });
  if (result.choice !== 'ok' || !result.value) return;
  await api('NewProject', result.value, stationMode);
  nextProjectNumber = Math.max(number + 1, numberedProject(result.value) + 1);
});
on('open-project', 'click', async () => {
  if (await prepareReplace()) {
    const path = await act('OpenProject');
    if (path) resetProjectView();
  }
});
on('save-project', 'click', async () => {
  if (await act('SaveProject', false)) toast('工程已保存');
});
on('save-as', 'click', async () => {
  if (await act('SaveProject', true)) toast('工程已另存为');
});
on(
  'update-button',
  'click',
  async () => {
    if (updateChecking || busy) return;
    updateChecking = true;
    renderUpdateButton();
    try {
      const result = await api('CheckForUpdates');
      if (result?.status === 'available') {
        const choice = await ask({
          title: '\u53d1\u73b0\u65b0\u7248\u672c',
          text:
            '\u5f53\u524d ' +
            result.currentVersion +
            '\uff0c\u6700\u65b0 ' +
            result.latestVersion +
            '\u3002',
          choices: [
            ['cancel', '\u7a0d\u540e'],
            ['download', '\u4e0b\u8f7d\u66f4\u65b0'],
          ],
        });
        if (choice.choice === 'download') await api('OpenUpdatePage');
      } else if (result?.status === 'current')
        toast(
          '\u5f53\u524d\u5df2\u662f\u6700\u65b0\u7248\u672c\uff08' +
            result.currentVersion +
            '\uff09',
        );
      else if (result?.status === 'unpublished')
        toast('\u6682\u65e0\u5df2\u53d1\u5e03\u7684\u66f4\u65b0\u7248\u672c');
      else throw Error('\u66f4\u65b0\u4fe1\u606f\u65e0\u6548\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5');
    } finally {
      updateChecking = false;
      renderUpdateButton();
    }
  },
  false,
);

for (const id of ['channel-dialog', 'point-dialog', 'device-dialog'])
  $(id).addEventListener('cancel', (e) => {
    if (busy) e.preventDefault();
  });
document.addEventListener('keydown', (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
    e.preventDefault();
    if (!document.querySelector('dialog[open]') && !busy) $('save-project').click();
  }
});
document.addEventListener('keydown', (e) => {
  if (
    e.ctrlKey ||
    e.metaKey ||
    e.altKey ||
    e.repeat ||
    document.querySelector('dialog[open]') ||
    e.target.closest(
      'input,textarea,select,button,summary,[role=button],[contenteditable=true],#context-menu',
    )
  )
    return;
  if (e.key === ' ') {
    e.preventDefault();
    $('pause-messages').click();
  } else if (e.key.toLowerCase() === 'x') {
    e.preventDefault();
    $('clear-messages').click();
  } else if (e.key === 'Escape') hideContext();
});
document.addEventListener('dblclick', (e) => {
  if (busy || document.querySelector('dialog[open]')) return;
  const item = e.target.closest('[data-device]');
  if (item) {
    selectDevice(item.dataset.device);
    deviceDialog(true);
  }
});
$('point-rows').addEventListener('click', (e) => {
  const cell = e.target.closest('td');
  if (cell && e.target === cell) cell.querySelector('.point-cell-input')?.focus();
});
for (const [listID, selector, getID, select, getSelected] of [
  ['device-list', '[data-device]', (b) => b.dataset.device, selectDevice, () => selectedDevice],
  ['channel-list', '[data-select]', (b) => b.dataset.select, selectChannel, () => selected],
])
  $(listID).addEventListener('keydown', (e) => {
    if (busy || !['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    const buttons = [...$(listID).querySelectorAll(selector)];
    if (!buttons.length) return;
    const index = buttons.findIndex((b) => getID(b) === getSelected());
    const next =
      e.key === 'Home'
        ? 0
        : e.key === 'End'
          ? buttons.length - 1
          : Math.max(0, Math.min(buttons.length - 1, index + (e.key === 'ArrowDown' ? 1 : -1)));
    const id = getID(buttons[next]);
    select(id);
    [...$(listID).querySelectorAll(selector)]
      .find((b) => getID(b) === id)
      ?.focus({ preventScroll: true });
  });
let contextActions = [];
function hideContext() {
  $('context-menu').hidden = true;
  contextActions = [];
}
function showContext(e, items) {
  e.preventDefault();
  hideContext();
  const menu = $('context-menu');
  contextActions = items;
  menu.innerHTML = items
    .map(
      ([label, fn, disabled], i) =>
        `<button role="menuitem" data-context-action="${i}" ${disabled ? 'disabled' : ''}>${escape(label)}</button>`,
    )
    .join('');
  menu.hidden = false;
  menu.style.left =
    Math.max(4, Math.min(e.clientX, window.innerWidth - menu.offsetWidth - 8)) + 'px';
  menu.style.top =
    Math.max(4, Math.min(e.clientY, window.innerHeight - menu.offsetHeight - 8)) + 'px';
  menu.querySelector('button:not(:disabled)')?.focus();
}
document.addEventListener('contextmenu', (e) => {
  if (busy || document.querySelector('dialog[open]')) return;
  const pointRow = e.target.closest('#point-rows tr');
  if (pointRow) {
    const target = pointRow.pointTarget,
      key = { ...target.key };
    showContext(e, [
      ['详细配置', () => pointDialog(keyOf(key))],
      [
        '移除该点',
        async () => {
          if (await confirmAction('删除点', '此点将被移除。', true))
            await act('DeleteDevicePoint', target.channel, target.device, key);
        },
      ],
    ]);
    return;
  }
  const ch = e.target.closest('[data-select]');
  if (e.target.closest('[data-device]')) {
    const button = e.target.closest('[data-device]');
    selectDevice(button.dataset.device);
    showContext(e, [
      ['重命名', renameDevice],
      ['修改', () => deviceDialog(true)],
      ['删除', removeDevice],
    ]);
  } else if (ch) {
    selectChannel(ch.dataset.select);
    const id = selected,
      c = current(),
      running = active(state());
    showContext(e, [
      [
        '重命名',
        () =>
          run('rename', async () => {
            const result = await ask({ title: '重命名', input: c.name });
            if (result.choice === 'ok' && result.value)
              await act('RenameChannel', id, result.value);
          }),
      ],
      [
        '复制',
        () =>
          run('copy', async () => {
            const copy = cloneBridgeData(channel());
            copy.id = '';
            copy.name = c.name + ' 副本';
            const added = await act('UpsertChannel', copy);
            selectChannel(
              snapshot.project.channels.find((ch) => ch.role === stationMode)?.id || added,
            );
          }),
      ],
      ['删除', () => $('delete-channel').click(), running],
    ]);
  } else if (e.target.closest('#traffic-panel'))
    showContext(e, [
      [paused ? '开始显示报文　Space' : '停止显示报文　Space', () => $('pause-messages').click()],
      ['清除报文　X', () => $('clear-messages').click(), $('clear-messages').disabled],
      ['导出报文', () => $('export-messages').click(), $('export-messages').disabled],
    ]);
});
$('context-menu').addEventListener('click', (e) => {
  const button = e.target.closest('[data-context-action]');
  if (!button || button.disabled) return;
  const fn = contextActions[Number(button.dataset.contextAction)]?.[1];
  hideContext();
  fn?.();
});
$('context-menu').addEventListener('keydown', (e) => {
  if (!['ArrowDown', 'ArrowUp', 'Escape'].includes(e.key)) return;
  e.preventDefault();
  if (e.key === 'Escape') {
    hideContext();
    return;
  }
  const buttons = [...$('context-menu').querySelectorAll('button:not(:disabled)')],
    index = buttons.indexOf(document.activeElement);
  buttons[(index + (e.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length]?.focus();
});
document.addEventListener('pointerdown', (e) => {
  if (!e.target.closest('#context-menu')) hideContext();
});
window.addEventListener('resize', hideContext);
function resizeNavigationWidth(value) {
  const max = Math.max(144, Math.min(420, (window.innerWidth || 1120) - 700)),
    width = Math.max(144, Math.min(max, Number(value) || 180));
  $('sidebar-resizer').setAttribute('aria-valuemax', max);
  document
    .querySelector('.original-workspace')
    .style.setProperty('--navigation-width', width + 'px');
  $('sidebar-resizer').setAttribute('aria-valuenow', Math.round(width));
  try {
    localStorage.setItem('gylab-navigation-width', width);
  } catch {}
}
function resizeNavigationHeight(value) {
  const ratio = Math.max(0.2, Math.min(0.8, Number(value) || 0.5));
  document.querySelector('.navigation-dock').style.setProperty('--navigation-share', ratio);
  $('navigation-resizer').setAttribute('aria-valuenow', Math.round(ratio * 100));
  try {
    localStorage.setItem('gylab-navigation-share', ratio);
  } catch {}
}
let debugHeight = 130;
function toggleDebug(open) {
  $('debug-dock').hidden = !open;
  $('debug-resizer').hidden = !open;
  $('toggle-debug').setAttribute('aria-expanded', String(open));
  try {
    localStorage.setItem('gylab-debug-open', String(open));
  } catch {}
}
on('toggle-debug', 'click', () => toggleDebug($('debug-dock').hidden), false);
function resizeDebugHeight(value) {
  const dock = $('report-dock'),
    available =
      dock.clientHeight ||
      dock.getBoundingClientRect().height ||
      Math.max(235, window.innerHeight - 200);
  const max = Math.max(65, Math.floor(available - 170)),
    height = Math.max(65, Math.min(max, Number(value) || 130));
  debugHeight = height;
  dock.style.setProperty('--debug-height', height + 'px');
  $('debug-resizer').setAttribute('aria-valuemax', max);
  $('debug-resizer').setAttribute('aria-valuenow', Math.round(height));
  try {
    localStorage.setItem('gylab-debug-height', height);
  } catch {}
}
let navigationReady = false;
function initializeNavigationLayout() {
  navigationReady = true;
  try {
    toggleDebug(localStorage.getItem('gylab-debug-open') === 'true');
  } catch {
    toggleDebug(false);
  }
  try {
    resizeNavigationWidth(localStorage.getItem('gylab-navigation-width') || 180);
    resizeNavigationHeight(localStorage.getItem('gylab-navigation-share') || 0.5);
    resizeDebugHeight(localStorage.getItem('gylab-debug-height') || 130);
  } catch {
    resizeNavigationWidth(180);
    resizeNavigationHeight(0.5);
    resizeDebugHeight(130);
  }
}
for (const [id, vertical] of [
  ['sidebar-resizer', true],
  ['navigation-resizer', false],
]) {
  const handle = $(id);
  handle.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    handle.classList.add('dragging');
    const start = Number(handle.getAttribute('aria-valuenow')),
      startX = e.clientX,
      startY = e.clientY,
      height = Math.max(
        1,
        document.querySelector('.navigation-dock').getBoundingClientRect().height - 10,
      );
    const move = (event) =>
      vertical
        ? resizeNavigationWidth(start + event.clientX - startX)
        : resizeNavigationHeight(start / 100 + (event.clientY - startY) / height);
    const end = () => {
      handle.classList.remove('dragging');
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  });
  handle.addEventListener('dblclick', () =>
    vertical ? resizeNavigationWidth(180) : resizeNavigationHeight(0.5),
  );
  handle.addEventListener('keydown', (e) => {
    const previous = vertical ? 'ArrowLeft' : 'ArrowUp',
      next = vertical ? 'ArrowRight' : 'ArrowDown';
    if (![previous, next, 'Home'].includes(e.key)) return;
    e.preventDefault();
    const current = Number(handle.getAttribute('aria-valuenow'));
    vertical
      ? resizeNavigationWidth(e.key === 'Home' ? 180 : current + (e.key === previous ? -12 : 12))
      : resizeNavigationHeight(
          e.key === 'Home' ? 0.5 : current / 100 + (e.key === previous ? -0.05 : 0.05),
        );
  });
}
window.addEventListener('resize', () => {
  if (navigationReady) {
    resizeNavigationWidth(Number($('sidebar-resizer').getAttribute('aria-valuenow')));
    resizeDebugHeight(debugHeight);
  }
});
function resizePanels(value) {
  const ratio = Math.max(0.25, Math.min(0.75, value));
  document.querySelector('main').style.setProperty('--point-share', ratio);
  $('panel-resizer').setAttribute('aria-valuenow', Math.round(ratio * 100));
  try {
    localStorage.setItem('gylab-point-share', ratio);
  } catch {}
}
try {
  const saved = Number(localStorage.getItem('gylab-point-share'));
  resizePanels(saved >= 0.25 && saved <= 0.75 ? saved : 0.42);
} catch {
  resizePanels(0.42);
}
$('panel-resizer').addEventListener('pointerdown', (e) => {
  if (e.button !== 0) return;
  const handle = e.currentTarget;
  handle.setPointerCapture(e.pointerId);
  handle.classList.add('dragging');
  const left = document.querySelector('main').getBoundingClientRect().left;
  const total = document.querySelector('main').clientWidth;
  const move = (event) => resizePanels((event.clientX - left) / total);
  const end = () => {
    handle.classList.remove('dragging');
    handle.removeEventListener('pointermove', move);
    handle.removeEventListener('pointerup', end);
    handle.removeEventListener('pointercancel', end);
  };
  handle.addEventListener('pointermove', move);
  handle.addEventListener('pointerup', end);
  handle.addEventListener('pointercancel', end);
});
$('panel-resizer').addEventListener('keydown', (e) => {
  if (['ArrowLeft', 'ArrowRight', 'Home'].includes(e.key)) {
    e.preventDefault();
    const value = Number($('panel-resizer').getAttribute('aria-valuenow')) / 100;
    resizePanels(e.key === 'Home' ? 0.42 : value + (e.key === 'ArrowLeft' ? -0.05 : 0.05));
  }
});
$('debug-resizer').addEventListener('pointerdown', (e) => {
  if (e.button !== 0) return;
  e.preventDefault();
  const handle = e.currentTarget,
    startY = e.clientY,
    start = debugHeight;
  handle.setPointerCapture(e.pointerId);
  handle.classList.add('dragging');
  const move = (event) => resizeDebugHeight(start + startY - event.clientY);
  const end = () => {
    handle.classList.remove('dragging');
    handle.removeEventListener('pointermove', move);
    handle.removeEventListener('pointerup', end);
    handle.removeEventListener('pointercancel', end);
  };
  handle.addEventListener('pointermove', move);
  handle.addEventListener('pointerup', end);
  handle.addEventListener('pointercancel', end);
});
$('debug-resizer').addEventListener('dblclick', () => resizeDebugHeight(130));
$('debug-resizer').addEventListener('keydown', (e) => {
  if (!['ArrowUp', 'ArrowDown', 'Home'].includes(e.key)) return;
  e.preventDefault();
  resizeDebugHeight(e.key === 'Home' ? 130 : debugHeight + (e.key === 'ArrowUp' ? 12 : -12));
});
const debugAfterByChannel = new Map();
function renderDebug() {
  const rows = snapshot.messages
    .filter(
      (m) =>
        m.channelId === selected &&
        ['SYS', 'ERR'].includes(m.direction) &&
        m.sequence > (debugAfterByChannel.get(selected) || 0),
    )
    .slice(-200);
  $('debug-content').textContent = rows
    .map((m) => m.time + '  ' + m.direction + '  ' + messageSummary(m))
    .join('\n');
}
const managerTransportNames = {
  'tcp-client': 'TCP 客户端',
  'tcp-server': 'TCP 服务端',
  udp: 'UDP',
  serial: '串口',
};
function managerField(row, cell, c, key, label, options = null) {
  let input = cell.querySelector('[data-manager-field="' + key + '"]');
  if (!input) {
    const wrapper = document.createElement('label');
    wrapper.className = 'manager-field';
    const caption = document.createElement('span');
    caption.textContent = label;
    wrapper.append(caption);
    input = document.createElement(options ? 'select' : 'input');
    if (!options) input.type = 'text';
    if (options)
      for (const [value, text] of options) {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = text;
        input.append(option);
      }
    if (key === 'name') input.maxLength = 100;
    input.dataset.managerField = key;
    input.setAttribute('aria-label', label + ' · ' + c.name);
    input.setAttribute('aria-description', '回车或离开提交，Esc取消');
    input.managerEdit = { row, key, base: '', dirty: false, pending: false };
    input.addEventListener('input', () => {
      input.managerEdit.dirty = true;
      input.removeAttribute('aria-invalid');
    });
    input.addEventListener('change', () => {
      input.managerEdit.dirty = true;
      if (options) commitManagerField(input);
    });
    input.addEventListener('blur', () => commitManagerField(input));
    input.addEventListener('keydown', (e) => {
      if (e.isComposing) return;
      if (e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        commitManagerField(input);
      } else if (e.key === 'Escape' && !input.managerEdit.pending) {
        e.preventDefault();
        e.stopPropagation();
        input.value = input.managerEdit.base;
        input.managerEdit.dirty = false;
        input.removeAttribute('aria-invalid');
        updateAllChannelsToggle();
      }
    });
    wrapper.append(input);
    cell.append(wrapper);
  }
  const edit = input.managerEdit;
  if (!edit.dirty && !edit.pending && document.activeElement !== input) {
    input.value = String(c[key] ?? '');
    edit.base = input.value;
  }
  input.disabled = edit.pending || !!busy;
  return input;
}
async function commitManagerField(input) {
  const edit = input.managerEdit;
  if (!edit || !edit.dirty || edit.pending) return;
  const value = input.value.trim();
  if (value === edit.base) {
    edit.dirty = false;
    input.removeAttribute('aria-invalid');
    return;
  }
  if (!value && edit.key !== 'localAddress') {
    pointValueError(input, '请填写' + input.getAttribute('aria-label'));
    return;
  }
  edit.pending = true;
  input.disabled = true;
  input.setAttribute('aria-busy', 'true');
  const row = edit.row,
    key = edit.key,
    id = row.dataset.managerId;
  // Read fresh configuration inside the queue so edits to adjacent cells and
  // live device/point updates cannot overwrite one another.
  row.managerTail = row.managerTail.then(async () => {
    try {
      const live = await api('GetSnapshot'),
        c = live.project.channels.find((c) => c.id === id);
      if (!c) throw Error('通道已被移除');
      if (key === 'name') await api('RenameChannel', id, value);
      else {
        const config = {
          ...c,
          [key]: ['baud', 'dataBits', 'stopBits'].includes(key) ? Number(value) : value,
        };
        if (['baud', 'dataBits', 'stopBits'].includes(key) && !Number.isInteger(config[key]))
          throw Error('请输入有效整数');
        if (key === 'transport' && value === 'tcp-server') config.localAddress = '';
        // The full communication form also restores inactive-field drafts.
        // Keep edited serial settings aligned with those restored values.
        const draftKeys = {
          baud: 'cbBaudRate',
          dataBits: 'cbDataBit',
          parity: 'cbParity',
          stopBits: 'cbStopBit',
        };
        if (draftKeys[key])
          config.transportDraft = {
            ...config.transportDraft,
            [draftKeys[key]]:
              key === 'parity' ? { N: '无校验', E: '偶校验', O: '奇校验' }[value] : value,
          };
        await api('UpdateTransport', config);
      }
      edit.base = value;
      edit.dirty = false;
      input.removeAttribute('aria-invalid');
      toast(key === 'name' ? '已保存' : '已保存，通信参数变更后请手动连接');
    } catch (error) {
      pointValueError(input, String(error?.message || error));
    } finally {
      edit.pending = false;
      input.disabled = false;
      input.removeAttribute('aria-busy');
      await refresh(true);
    }
  });
  return row.managerTail;
}
function renderManager() {
  if (!$('manager-dialog').open) return;
  const tbody = $('manager-rows'),
    existing = new Map([...tbody.children].map((row) => [row.dataset.managerId, row]));
  snapshot.project.channels
    .filter((c) => c.role === stationMode)
    .forEach((c, index) => {
      let row = existing.get(c.id);
      if (!row) {
        row = document.createElement('tr');
        row.dataset.managerId = c.id;
        row.managerTail = Promise.resolve();
        for (let n = 0; n < 5; n++) row.append(document.createElement('td'));
        const button = document.createElement('button');
        button.dataset.managerChannel = c.id;
        row.children[4].append(button);
      }
      existing.delete(c.id);
      const st = snapshot.states.find((s) => s.id === c.id) || { status: 'stopped' };
      managerField(row, row.children[0], c, 'name', '通道名称');
      managerField(
        row,
        row.children[1],
        c,
        'transport',
        '通信方式',
        Object.entries(managerTransportNames),
      );
      managerField(
        row,
        row.children[2],
        c,
        'address',
        c.transport === 'serial' ? '串口路径' : 'IP / 主机名:端口',
      );
      const local = managerField(row, row.children[2], c, 'localAddress', '本地绑定（可留空）');
      local.closest('label').hidden = !['tcp-client', 'udp'].includes(c.transport);
      for (const [key, label, options] of [
        ['baud', '波特率', null],
        [
          'dataBits',
          '数据位',
          [
            ['8', '8'],
            ['7', '7'],
            ['6', '6'],
            ['5', '5'],
          ],
        ],
        [
          'parity',
          '校验',
          [
            ['N', '无'],
            ['E', '偶'],
            ['O', '奇'],
          ],
        ],
        [
          'stopBits',
          '停止位',
          [
            ['1', '1'],
            ['2', '2'],
          ],
        ],
      ])
        managerField(row, row.children[1], c, key, label, options).closest('label').hidden =
          c.transport !== 'serial';
      row.children[3].textContent = statuses[st.status] || st.status;
      const button = row.children[4].firstElementChild;
      button.textContent = active(st) ? '断开' : '连接';
      button.disabled =
        !!busy ||
        [...row.querySelectorAll('[data-manager-field]')].some(
          (input) => input.managerEdit.pending || input.managerEdit.dirty,
        );
      if (tbody.children[index] !== row) tbody.insertBefore(row, tbody.children[index] || null);
    });
  for (const row of existing.values()) row.remove();
}
on(
  'communication-manager',
  'click',
  () => {
    $('manager-dialog').show();
    renderManager();
  },
  false,
);
on(
  'clear-debug',
  'click',
  () => {
    debugAfterByChannel.set(
      selected,
      Math.max(
        0,
        ...snapshot.messages.filter((m) => m.channelId === selected).map((m) => m.sequence),
      ),
    );
    renderDebug();
  },
  false,
);
function managerEditsPending() {
  return [...$('manager-rows').querySelectorAll('[data-manager-field]')].some(
    (input) => input.managerEdit?.pending || input.managerEdit?.dirty,
  );
}
function allChannelsToggleState() {
  const channels = snapshot.project.channels.filter((c) => c.role === stationMode);
  return {
    channels,
    stopping: channels.some((c) =>
      active(snapshot.states.find((s) => s.id === c.id) || { status: 'stopped' }),
    ),
  };
}
function updateAllChannelsToggle() {
  const button = $('toggle-all-channels');
  if (!button) return;
  const { channels, stopping } = allChannelsToggleState(),
    pending = managerEditsPending();
  button.textContent = stopping ? '全部停止' : '全部连接';
  button.setAttribute('aria-pressed', String(stopping));
  button.disabled = !!busy || !channels.length || pending;
  button.setAttribute(
    'aria-description',
    pending
      ? '请先提交或取消通道参数修改'
      : !channels.length
        ? '当前列表暂无通道'
        : (stopping ? '停止' : '连接') + '当前列表的 ' + channels.length + ' 个通道',
  );
}
for (const event of ['input', 'change', 'focusout'])
  $('manager-rows').addEventListener(event, updateAllChannelsToggle);
on('toggle-all-channels', 'click', async () => {
  if (managerEditsPending()) return;
  const { channels, stopping } = allChannelsToggleState();
  try {
    for (const c of channels) await api(stopping ? 'StopChannel' : 'StartChannel', c.id);
  } finally {
    await refresh(true);
  }
});
$('manager-rows').addEventListener('click', (e) => {
  const b = e.target.closest('[data-manager-channel]');
  if (
    !b ||
    b.disabled ||
    busy ||
    [...b.closest('tr').querySelectorAll('[data-manager-field]')].some(
      (input) => input.managerEdit.pending || input.managerEdit.dirty,
    )
  )
    return;
  run('manager-connect', () => {
    const id = b.dataset.managerChannel,
      st = snapshot.states.find((s) => s.id === id) || { status: 'stopped' };
    return act(active(st) ? 'StopChannel' : 'StartChannel', id);
  });
});

on('network-ports', 'click', () => $('list-ports').click(), false);
setInterval(() => {
  if (!document.hidden) refresh();
}, 650);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) refresh();
});
initializeDesktop();

for (const [id, formID] of [
  ['device-dialog', 'device-form'],
  ['channel-dialog', 'channel-form'],
]) {
  $(id).addEventListener('keydown', (e) => {
    if (busy || !['Escape', 'Enter'].includes(e.key) || e.isComposing) return;
    if (e.key === 'Enter' && e.target.closest('button')) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.key === 'Escape') {
      if (id === 'device-dialog') rememberDevicePanel();
      $(id).close();
    } else submitWithButton($(formID), $(formID).querySelector('button[type=submit]'));
  });
}

// Native windows use normal document flow inside their own WebView. No DOM
// overlay is opened in the main workspace.
function installNativeDialogs() {
  for (const dialog of document.querySelectorAll('dialog')) {
    const show = () => {
      if (nativeContext?.kind) {
        dialog.setAttribute('open', '');
        return;
      }
      const f = $('device-form');
      const context = {
        kind: dialog.id,
        role: stationMode,
        channel: selected,
        device: selectedDevice,
        edit: [
          'control-dialog',
          'auto-data-dialog',
          'remote-config-dialog',
          'telemetry-values-dialog',
        ].includes(dialog.id)
          ? true
          : dialog.id === 'device-dialog'
            ? !!f.dataset.device
            : ['point-dialog', 'channel-dialog'].includes(dialog.id),
        pointKind: dialog.id === 'auto-data-dialog' ? 'telemetry' : $('point-kind').value,
        selection: dialog.id === 'telemetry-values-dialog' ? telemetrySelection : undefined,
        point:
          dialog.id === 'remote-config-dialog'
            ? remoteFocusPoint
            : dialog.id === 'auto-data-dialog'
              ? ''
              : dialog.id === 'control-dialog'
                ? controlEditor.mode
                : dialog.id === 'point-dialog' && pointEditor?.key
                  ? keyOf(pointEditor.key)
                  : '',
        control: dialog.id === 'control-dialog' ? controlEditor.request : null,
      };
      api('OpenNativeWindow', context).catch(report);
    };
    dialog.show = show;
    dialog.showModal = show;
    dialog.close = () => {
      if (nativeContext?.kind) window.go.main.App.CloseNativeWindow();
      else dialog.removeAttribute('open');
    };
  }
}
async function initializeDesktop() {
  try {
    const c = await window.go?.main?.App?.GetNativeContext?.();
    if (c && typeof c.kind === 'string') {
      nativeDesktop = true;
      nativeContext = c;
      if (!c.kind) stationMode = c.role || 'client';
      installNativeDialogs();
      if (c.kind) {
        stationMode = c.role || 'client';
        selected = c.channel;
        selectedDevice = c.device;
        if (c.draft) {
          deviceWindowCache.set(stationMode, c.draft.window);
          for (const [key, value] of c.draft.panels || []) devicePanelCache.set(key, value);
        }
        document.body.classList.add('native-window');
        $(c.kind).classList.add('native-content');
        await refresh();
        selected = c.channel;
        selectedDevice = c.device || channel()?.devices?.[0]?.id || '';
        if (c.kind === 'channel-dialog') channelDialog();
        else if (c.kind === 'device-dialog') deviceDialog(c.edit);
        else if (c.kind === 'point-dialog') {
          if (c.pointKind) $('point-kind').value = c.pointKind;
          pointDialog(c.point || null);
        } else if (c.kind === 'control-dialog') controlDialog(c.point, c.control);
        else if (c.kind === 'telemetry-values-dialog') initializeTelemetryValues(c);
        else if (c.kind === 'remote-config-dialog') await initializeRemoteConfig(c);
        else if (c.kind === 'auto-data-dialog') await initializeAutoDataWindow(c);
        else if (c.kind === 'auto-yk-dialog' || c.kind === 'auto-yt-dialog')
          await initializeAutoWindow(c.kind === 'auto-yk-dialog' ? 'yk' : 'yt');
        else if (c.kind === 'events-dialog') {
          $('events-dialog').show();
          renderEvents();
        } else if (c.kind === 'auto-bulk-dialog') $('auto-bulk-dialog').show();
        else if (c.kind === 'confirm-dialog') ask(c.ask);
        else {
          $(c.kind).show();
          if (c.kind === 'manager-dialog') renderManager();
        }
        if (!$(c.kind).open) throw Error('配置目标已不存在，请重新打开窗口');
        document.body.style.visibility = 'visible';
        // Signal readiness after WebKit has laid out the visible native editor.
        await configWindowFit;
        await new Promise((resolve) => requestAnimationFrame(resolve));
        const dialog = $(c.kind),
          controls = [...dialog.querySelectorAll('input,select,button,textarea')].filter(
            (el) => el.getClientRects().length && !el.hidden && !el.closest('details:not([open])'),
          );
        const labels = [
          ...dialog.querySelectorAll('#original-device-panel .original-field>span'),
        ].filter((el) => el.getClientRects().length && !el.closest('details:not([open])'));
        const wrappedLabels = labels.filter((el) => {
          const range = document.createRange();
          range.selectNodeContents(el);
          return (
            range.getBoundingClientRect().height > parseFloat(getComputedStyle(el).fontSize) * 1.8
          );
        }).length;
        const deviceInputs = [
          ...dialog.querySelectorAll(
            '#original-device-panel .original-field input,#original-device-panel .original-field select',
          ),
        ].filter((el) => el.getClientRects().length && !el.closest('details:not([open])'));
        const inputColumns = [[], []],
          middle = dialog.getBoundingClientRect().left + dialog.getBoundingClientRect().width / 2;
        for (const row of dialog.querySelectorAll(
          '#device-form .original-field,#device-form .protocol-selector>label,#device-form .device-name-field',
        )) {
          const input = row.querySelector('input,select');
          if (input?.getClientRects().length && !input.closest('details:not([open])'))
            inputColumns[row.getBoundingClientRect().left < middle ? 0 : 1].push(
              input.getBoundingClientRect().left,
            );
        }
        const inputAlignmentDelta = Math.ceil(
          Math.max(
            0,
            ...inputColumns.map((xs) => (xs.length ? Math.max(...xs) - Math.min(...xs) : 0)),
          ),
        );
        const devicePointFields = controls.filter((el) =>
            /^txt(YX|YC|YK|YT)_(Add|Num)$/.test(el.dataset.originalId || ''),
          ).length,
          qualityLabel = dialog.querySelector('.quality-caption span'),
          qualityCheckbox = dialog.querySelector('#telemetry-quality-enabled'),
          qualityCheckboxLeft =
            !!qualityLabel &&
            qualityCheckbox.getBoundingClientRect().right <=
              qualityLabel.getBoundingClientRect().left;
        const editorSaveButtons = controls.filter(
            (el) => el.tagName === 'BUTTON' && /^auto-(data|yk|yt)-save$/.test(el.id),
          ).length,
          editorRuntimeButtons = controls.filter(
            (el) => el.tagName === 'BUTTON' && /^auto-(data|yk|yt)-(start|pause|stop)$/.test(el.id),
          ).length;
        const contentBottom = Math.ceil(
            Math.max(0, ...controls.map((el) => el.getBoundingClientRect().bottom)),
          ),
          saveButtonBottom = Math.ceil(
            dialog
              .querySelector('#auto-data-save,#auto-yk-save,#auto-yt-save')
              ?.getBoundingClientRect().bottom || 0,
          );
        await api('NativeWindowReady', {
          contentBottom,
          saveButtonBottom,
          editorSaveButtons,
          editorRuntimeButtons,
          dataRuleRows: dialog.querySelectorAll('#auto-data-rows > tr').length,
          dataAddSymbol: !!dialog.querySelector('#auto-data-add[aria-label="添加规则"] svg'),
          viewportWidth: innerWidth,
          viewportHeight: innerHeight,
          documentWidth: document.documentElement.scrollWidth,
          dialogWidth: Math.round(dialog.getBoundingClientRect().width),
          controls: controls.length,
          wrappedLabels,
          minInputWidth: deviceInputs.length
            ? Math.floor(Math.min(...deviceInputs.map((el) => el.getBoundingClientRect().width)))
            : 0,
          inputAlignmentDelta,
          devicePointFields,
          qualityCheckboxLeft,
        });
      } else {
        initializeNavigationLayout();
        await api('SetWorkspaceMode', stationMode);
        await refresh();
        api('SetNativeSelection', {
          role: stationMode,
          channel: selected,
          device: selectedDevice,
        }).catch(report);
      }
    } else {
      initializeNavigationLayout();
      await api('SetWorkspaceMode', stationMode);
      await refresh();
    }
  } catch (error) {
    report(error);
  }
  document.body.style.visibility = 'visible';
  if (nativeContext?.kind === 'device-dialog')
    for (const event of ['input', 'change'])
      $('device-form').addEventListener(event, rememberDevicePanel);
}

// Kept separate from presentation: a successful send is never reported as a
// successful operation. Results come from matching protocol confirmations.
function controlDialog(mode = 'yk', point = null) {
  const c = channel(),
    d = device();
  if (!c || !d || c.protocol !== 'iec104' || c.role !== 'client') return;
  const kind = mode === 'setpoint' ? 'yt' : mode,
    p =
      point ||
      d.points.find((p) => p.kind === kind) ||
      (mode === 'step' ? d.points.find((p) => p.kind === 'yt') : null);
  const request = {
    type: mode === 'yk' ? 45 : mode === 'step' ? 47 : 50,
    address: p?.address ?? null,
    value: mode === 'step' ? 2 : (p?.value ?? 0),
    ...(point?.type ? point : {}),
  };
  controlEditor = { channel: c.id, device: d.id, mode, request };
  controlSelection = null;
  $('control-device').value = `${c.name} / ${d.name} · 公共点位 ${d.commonAddress}`;
  $('control-address').value = request.address ?? '';
  if (
    point?.address === 0 &&
    !d.points.some((p) => p.kind === kind || (mode === 'step' && p.kind === 'yt'))
  )
    $('control-address').value = '';
  $('control-type').innerHTML = (
    mode === 'yk'
      ? [
          [45, '单点'],
          [46, '双点'],
        ]
      : mode === 'step'
        ? [[47, '步调节']]
        : [
            [48, '归一化（原码）'],
            [49, '标度化'],
            [50, '浮点数'],
          ]
  )
    .map(([value, label]) => `<option value="${value}">${label}</option>`)
    .join('');
  $('control-type').value = String(request.type);
  $('control-value').value = request.value;
  $('control-state').innerHTML =
    mode === 'step'
      ? '<option value="1">降</option><option value="2">升</option>'
      : '<option value="0">分闸</option><option value="1">合闸</option>';
  $('control-state').value =
    mode === 'step'
      ? request.value === 1
        ? '1'
        : '2'
      : String(request.type === 46 ? (request.value === 2 ? 1 : 0) : request.value ? 1 : 0);
  $('control-direct').checked = mode === 'setpoint';
  $('control-timestamp').checked = false;
  $('control-result').textContent = transferReady(state(), 'iec104')
    ? ''
    : '未连接或104链路未激活，可编辑参数；激活后才能发送';
  $('control-form').querySelector('.form-error').hidden = true;
  $('control-dialog').dataset.mode = mode;
  $('control-title').textContent =
    mode === 'yk' ? '遥控操作' : mode === 'step' ? '遥调（步调节）' : '遥调（设值）';
  updateControlFields();
  $('control-dialog').show();
}
function updateControlFields() {
  if (!controlEditor) return;
  const type = Number($('control-type').value),
    setpoint = type >= 48;
  $('control-value-label').hidden = !setpoint;
  $('control-value').disabled = !setpoint;
  $('control-state-label').hidden = setpoint;
  $('control-state').disabled = setpoint;
  const value = $('control-value');
  value.min = type === 50 ? '-3.4028234663852886e38' : '-32768';
  value.max = type === 50 ? '3.4028234663852886e38' : '32767';
  value.step = type === 50 ? 'any' : '1';
  updateControlButtons();
}
function controlParameters(operation) {
  const type = Number($('control-type').value),
    address = globalThis.pointSelection.parse($('control-address').value)[0],
    timeoutMs = integer('control-timeout', 100, 60000);
  let value = Number($('control-state').value);
  if (type === 46) value++;
  if (type >= 48) {
    const input = $('control-value');
    value = Number(input.value);
    if (
      !input.value.trim() ||
      !Number.isFinite(value) ||
      (type === 50 && Math.abs(value) > 3.4028234663852886e38) ||
      (type !== 50 && (!Number.isInteger(value) || value < -32768 || value > 32767))
    )
      throw Error(type === 50 ? '请输入有效短浮点设值' : '请输入 -32768–32767 整数');
  }
  return {
    type: $('control-timestamp').checked ? type + 13 : type,
    address,
    value,
    operation,
    direct: $('control-direct').checked,
    timeoutMs,
  };
}
function controlFingerprint() {
  return [
    $('control-type').value,
    $('control-address').value,
    $('control-value').value,
    $('control-state').value,
    $('control-timestamp').checked,
  ].join('/');
}
function updateControlButtons() {
  if (!controlEditor) return;
  const st = snapshot.states.find((s) => s.id === controlEditor.channel),
    online = transferReady(st, 'iec104');
  const selected =
      controlSelection?.signature === controlFingerprint() && Date.now() < controlSelection.until,
    locked = !!busy;
  let complete = false,
    hasPoints = false;
  try {
    const points = globalThis.pointSelection.parse($('control-address').value);
    hasPoints = points.length > 0;
    complete = selected && (controlSelection.addresses?.length ?? 1) === points.length;
  } catch {}
  for (const input of $('control-form').querySelectorAll('input:not([readonly]),select'))
    input.disabled = locked;
  $('control-value').disabled = locked || Number($('control-type').value) < 48;
  $('control-state').disabled = locked || Number($('control-type').value) >= 48;
  const reason = locked
    ? '操作处理中'
    : !online
      ? st?.status === 'connected'
        ? '等待104链路激活'
        : '请手动连接通道'
      : !hasPoints
        ? '请填写有效点位'
        : '';
  for (const id of ['control-select', 'control-execute', 'control-cancel'])
    $(id).setAttribute(
      'aria-description',
      reason ||
        (id === 'control-execute' && !$('control-direct').checked && !complete
          ? '请先选择成功'
          : id === 'control-cancel' && !selected
            ? '没有可撤销的选择'
            : ''),
    );
  $('control-select').disabled = locked || !online || !hasPoints;
  $('control-execute').disabled =
    locked || !online || !hasPoints || !($('control-direct').checked || complete);
  $('control-cancel').disabled = locked || !online || !selected;
}
for (const operation of ['select', 'execute', 'cancel'])
  on('control-' + operation, 'click', async () => {
    if (!controlEditor) return;
    const editor = controlEditor,
      request = controlParameters(operation),
      signature = controlFingerprint(),
      points =
        operation === 'cancel' && controlSelection?.addresses
          ? controlSelection.addresses
          : globalThis.pointSelection.parse($('control-address').value);
    let confirmed = [],
      firstConfirmed = 0,
      result = null;
    $('control-result').textContent = '等待子站应答…';
    try {
      for (const address of points) {
        result = await api('Command104Device', editor.channel, editor.device, {
          ...request,
          address,
        });
        confirmed.push(address);
        if (!firstConfirmed) firstConfirmed = Date.now();
      }
      controlSelection =
        operation === 'select'
          ? { signature, until: firstConfirmed + 30000, addresses: confirmed }
          : null;
      $('control-result').textContent =
        points.length === 1 ? result.message : `${confirmed.length}个点位 · ${result.message}`;
      await refresh(true);
    } catch (error) {
      controlSelection =
        operation === 'select' && confirmed.length
          ? { signature, until: firstConfirmed + 30000, addresses: confirmed }
          : null;
      $('control-result').textContent =
        (confirmed.length ? '已确认' + confirmed.length + '个点位；' : '') +
        String(error?.message || error);
      throw error;
    }
  });
for (const event of ['input', 'change'])
  $('control-form').addEventListener(event, () => {
    controlSelection = null;
    updateControlFields();
  });

// Automatic jobs live in the parent engine, so hiding/reopening a native window
// preserves its task, rows and counters, as in the original WinForms windows.
let autoEditor = null,
  autoView = null,
  autoPollBusy = false,
  autoEditQueue = Promise.resolve(),
  autoPending = new Map(),
  autoErrorSeen = '';
const autoIsActive = (status) => ['starting', 'running', 'paused', 'stopping'].includes(status);
async function openAutoWindow(mode) {
  const c = channel(),
    d = device();
  if (!c || !d) return;
  const seed = c.protocol.startsWith('modbus')
    ? { function: mode === 'yt' ? 6 : Number($('write-fc')?.value || 5) === 6 ? 6 : 5 }
    : {};
  const view = await api('PrepareAutoControl', c.id, d.id, mode, seed);
  if (nativeDesktop) {
    await api('OpenNativeWindow', {
      kind: 'auto-' + mode + '-dialog',
      channel: c.id,
      device: d.id,
      role: c.role,
      point: mode,
      edit: true,
    });
  } else await initializeAutoWindow(mode, view);
}
async function initializeAutoWindow(mode, initial = null) {
  autoEditor = {
    channel: nativeContext?.channel || selected,
    device: nativeContext?.device || selectedDevice,
    mode,
  };
  const v =
    initial || (await api('GetAutoControlDraft', autoEditor.channel, autoEditor.device, mode));
  if (!v) throw Error('自动操作数据未就绪');
  renderAutoState(v);
  $('auto-' + mode + '-dialog').show();
  if (nativeContext?.kind)
    await window.go.main.App.FitAutoWindow(window.innerWidth, window.innerHeight);
  setInterval(() => pollAutoControl(), 200);
}
async function pollAutoControl() {
  if (!autoEditor || autoPollBusy || document.hidden) return;
  autoPollBusy = true;
  try {
    renderAutoState(
      await api('GetAutoControlDraft', autoEditor.channel, autoEditor.device, autoEditor.mode),
    );
  } catch (error) {
    report(error);
  } finally {
    autoPollBusy = false;
  }
}
function autoWrite(key, fn) {
  const marker = Symbol();
  autoPending.set(key, marker);
  const operation = autoEditQueue.catch(() => {}).then(fn);
  autoEditQueue = operation
    .then((v) => {
      if (v) renderAutoState(v);
    })
    .catch((error) => {
      report(error, $('auto-' + autoEditor.mode + '-form'));
    })
    .finally(() => {
      if (autoPending.get(key) === marker) autoPending.delete(key);
    });
  return autoEditQueue;
}
function autoValue(id, value, pendingKey = id) {
  const input = $(id);
  if (input && document.activeElement !== input && !autoPending.has(pendingKey)) {
    if (input.type === 'checkbox') input.checked = !!value;
    else input.value = String(value ?? '');
  }
}
function renderAutoState(view) {
  if (!view || !autoEditor) return;
  for (const id of ['auto-yk-double', 'auto-yk-direct'])
    $(id).parentElement.hidden = !!view.protocol && view.protocol !== 'iec104';
  if (autoView && view.id === autoView.id && view.revision < autoView.revision) return;
  const previous = autoView;
  autoView = view;
  const mode = autoEditor.mode,
    o = view.options;
  autoValue('auto-' + mode + '-interval', o.intervalMs);
  if (mode === 'yk') {
    for (const [id, key] of [
      ['timeout', 'timeoutMs'],
      ['feedback', 'checkFeedback'],
      ['double', 'double'],
      ['cycle', 'cycle'],
      ['direct', 'direct'],
      ['autopause', 'autoPause'],
    ])
      autoValue('auto-yk-' + id, o[key]);
  }
  const tbody = $('auto-' + mode + '-rows');
  if (previous && (previous.id !== view.id || previous.generation !== view.generation)) {
    tbody.replaceChildren();
    autoPending.clear();
  }
  const existing = new Map([...tbody.children].map((row) => [Number(row.dataset.autoRow), row]));
  view.rows.forEach((r, index) => {
    let row = existing.get(index);
    if (!row) {
      row = document.createElement('tr');
      row.dataset.autoRow = index;
      row.innerHTML =
        mode === 'yk'
          ? '<td></td><td><input type="checkbox" data-auto-field="enabled" aria-label="启用"></td><td></td><td></td><td><select data-auto-field="action" aria-label="遥控类型"><option>合闸后分闸</option><option>分闸后合闸</option><option>合闸</option><option>分闸</option></select></td><td><input type="number" data-auto-field="count" min="1" max="1000000" aria-label="遥控次数"></td><td></td>'
          : '<td></td><td></td><td></td><td><input type="text" data-auto-field="value" aria-label="遥调数值"></td>';
      tbody.append(row);
    }
    existing.delete(index);
    const fingerprint = JSON.stringify(r);
    if (row.dataset.fingerprint === fingerprint) return;
    row.dataset.fingerprint = fingerprint;
    const cells = row.children;
    cells[0].textContent = index + 1;
    const put = (field, value) => {
      const input = row.querySelector('[data-auto-field="' + field + '"]');
      if (document.activeElement !== input && !autoPending.has(index + '/' + field)) {
        if (input.type === 'checkbox') input.checked = value;
        else input.value = value;
      }
    };
    if (mode === 'yk') {
      put('enabled', r.enabled);
      put('action', r.action);
      put('count', r.count);
      cells[2].textContent = r.address;
      cells[3].textContent = r.name;
      cells[6].textContent = r.feedbackName || r.feedback;
      cells[6].setAttribute('aria-description', r.feedback);
      cells[3].setAttribute('aria-description', r.name);
    } else {
      cells[1].textContent = r.address;
      cells[2].textContent = r.name;
      put('value', r.value);
    }
  });
  for (const row of existing.values()) row.remove();
  updateAutoButtons();
  if (mode === 'yt') {
    autoValue('auto-yt-type', o.valueType);
    $('auto-yt-type-label').hidden = !!view.protocol && view.protocol !== 'iec104';
    const identity = view.id + '/' + view.generation;
    if (autoEditor.selectionIdentity !== identity) {
      autoEditor.selectionIdentity = identity;
      $('auto-yt-addresses').value = globalThis.pointSelection.format(
        view.rows.map((r) => r.address),
      );
      $('auto-yt-addresses').dataset.base = $('auto-yt-addresses').value;
      $('auto-yt-value').value = '';
    }
  }
}
function updateAutoButtons() {
  if (!autoEditor) return;
  const button = $('auto-' + autoEditor.mode + '-save');
  if (button) button.disabled = !!busy;
}
function autoOptionsFromForm() {
  const mode = autoEditor.mode,
    o = { ...autoView.options, intervalMs: integer('auto-' + mode + '-interval', 0, 3600000) };
  if (mode === 'yk')
    Object.assign(o, {
      timeoutMs: integer('auto-yk-timeout', 100, 60000),
      checkFeedback: $('auto-yk-feedback').checked,
      double: $('auto-yk-double').checked,
      cycle: $('auto-yk-cycle').checked,
      direct: $('auto-yk-direct').checked,
      autoPause: $('auto-yk-autopause').checked,
    });
  else o.valueType = Number($('auto-yt-type').value);
  return o;
}
async function applyAutoYTSelection() {
  if (autoEditor?.mode !== 'yt') return;
  const input = $('auto-yt-addresses'),
    value = $('auto-yt-value');
  if (input.value === input.dataset.base && !value.value.trim()) return;
  globalThis.pointSelection.parse(input.value);
  const seed = {
    addresses: input.value,
    value: value.value.trim(),
    replaceValues: !!value.value.trim(),
    valueType: Number($('auto-yt-type').value),
  };
  const view = await api('PrepareAutoControl', autoEditor.channel, autoEditor.device, 'yt', seed);
  renderAutoState(view);
  input.dataset.base = input.value;
  value.value = '';
}
on('auto-yt-fill', 'click', async () => {
  await autoEditQueue;
  await applyAutoYTSelection();
});
function autoEditsFromForm() {
  return [...$('auto-' + autoEditor.mode + '-rows').children].map((row) => {
    const index = Number(row.dataset.autoRow);
    if (autoEditor.mode === 'yt')
      return {
        index,
        generation: autoView.generation,
        value: row.querySelector('[data-auto-field="value"]').value,
      };
    const input = row.querySelector('[data-auto-field="count"]'),
      count = Number(input.value);
    if (!input.value.trim() || !Number.isInteger(count) || count < 1 || count > 1000000) {
      input.focus();
      throw Error('遥控次数应为 1–1000000 整数');
    }
    return {
      index,
      generation: autoView.generation,
      count,
      enabled: row.querySelector('[data-auto-field="enabled"]').checked,
      action: row.querySelector('[data-auto-field="action"]').value,
    };
  });
}
for (const mode of ['yk', 'yt']) {
  on('auto-' + mode + '-form', 'submit', async (e) => {
    e.preventDefault();
    if (!autoEditor) return;
    await autoEditQueue;
    if (mode === 'yt') await applyAutoYTSelection();
    const args = [autoEditor.channel, autoEditor.device, mode],
      options = autoOptionsFromForm(),
      rows = autoEditsFromForm();
    renderAutoState(await api('UpdateAutoOptions', ...args, options));
    renderAutoState(await api('UpdateAutoRows', ...args, rows));
    renderAutoState(await api('SaveAutoControlDraft', ...args));
    await configurationSaved(autoEditor.channel);
    $('auto-' + mode + '-dialog').close();
  });
  $('auto-' + mode + '-rows').addEventListener('change', (e) => {
    const input = e.target.closest('[data-auto-field]'),
      row = input?.closest('[data-auto-row]');
    if (!row || !autoEditor) return;
    try {
      const index = Number(row.dataset.autoRow),
        generation = autoView.generation,
        field = input.dataset.autoField,
        value =
          field === 'enabled'
            ? input.checked
            : field === 'count'
              ? Number(input.value)
              : input.value;
      if (
        field === 'count' &&
        (!input.value.trim() || !Number.isInteger(value) || value < 1 || value > 1000000)
      )
        throw Error('遥控次数应为 1–1000000 整数');
      autoWrite(index + '/' + field, () =>
        api('UpdateAutoRows', autoEditor.channel, autoEditor.device, mode, [
          { index, generation, [field]: value },
        ]),
      );
    } catch (error) {
      report(error, $('auto-' + mode + '-form'));
    }
  });
  $('auto-' + mode + '-form').addEventListener('change', (e) => {
    if (
      !autoEditor ||
      !e.target.id.startsWith('auto-' + mode + '-') ||
      ['auto-yt-addresses', 'auto-yt-value', 'auto-yt-fill'].includes(e.target.id)
    )
      return;
    try {
      const options = autoOptionsFromForm();
      autoWrite(e.target.id, () =>
        api('UpdateAutoOptions', autoEditor.channel, autoEditor.device, mode, options),
      );
    } catch (error) {
      report(error, $('auto-' + mode + '-form'));
    }
  });
}
$('auto-yk-scroll').addEventListener('contextmenu', (e) => {
  if (!autoEditor) return;
  showContext(e, [
    [
      '批量修改',
      () => {
        const context = {
          kind: 'auto-bulk-dialog',
          channel: autoEditor.channel,
          device: autoEditor.device,
          role: 'client',
          point: 'yk',
          edit: true,
        };
        if (nativeDesktop) api('OpenAutoBulk', autoEditor.channel, autoEditor.device).catch(report);
        else $('auto-bulk-dialog').show();
      },
    ],
  ]);
});
on('auto-bulk-form', 'submit', async () => {
  const c = nativeContext?.kind === 'auto-bulk-dialog' ? nativeContext : autoEditor;
  if (!c) return;
  const bulk = {
    changeEnabled: $('auto-bulk-enable-check').checked,
    enabled: $('auto-bulk-enable').value === 'true',
    changeAction: $('auto-bulk-action-check').checked,
    action: $('auto-bulk-action').value,
    changeCount: $('auto-bulk-count-check').checked,
    count: $('auto-bulk-count-check').checked ? integer('auto-bulk-count', 1, 1000000) : 1,
  };
  const result = await api('ApplyAutoBulk', c.channel, c.device, bulk);
  if (autoEditor) renderAutoState(result);
  $('auto-bulk-dialog').close();
});

for (const mode of ['yk', 'yt'])
  $('auto-' + mode + '-rows').addEventListener('keydown', (e) => {
    const input = e.target.closest('[data-auto-field]');
    if (input && e.key === 'Enter') {
      e.preventDefault();
      input.dispatchEvent(new Event('change', { bubbles: true }));
      input.blur();
    }
  });
$('auto-yk-rows').addEventListener('click', (e) => {
  const cell = e.target.closest('td');
  if (cell?.cellIndex === 1 && e.target.tagName !== 'INPUT') {
    const input = cell.querySelector('input');
    input.checked = !input.checked;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && $('auto-bulk-dialog').open) {
    e.preventDefault();
    $('auto-bulk-dialog').close();
  }
});

// Multi-rule data generation runs in the Go engine; this window only edits and
// observes its fixed channel/device target.
let dataEditor = null,
  dataView = null;
function dataKinds() {
  return dataEditor?.protocol === 'iec104' ? ['yx', 'yc'] : ['discrete', 'holding', 'input'];
}

function dataIsBit(kind) {
  return ['yx', 'coil', 'discrete'].includes(kind);
}
function newDataRule(kind) {
  const selected = dataEditor.points.filter((p) => p.kind === kind),
    points = selected.map((p) => p.address),
    bit = dataIsBit(kind),
    limits = (selected.length ? selected : [{ kind }]).map((p) =>
      pointLimits(p, dataEditor.configuration),
    ),
    normalized = limits.some((v) => v.normalized),
    integer = limits.some((v) => v.integer);
  const low = Math.max(0, ...limits.map((v) => v.min)),
    high = Math.min(bit ? 1 : normalized ? 0.9 : 100, ...limits.map((v) => v.max)),
    max = integer ? Math.floor(high) : high;
  return {
    enabled: points.length > 0,
    kind,
    points: globalThis.pointSelection.format(points),
    start: points[0] ?? 0,
    end: points[points.length - 1] ?? 0,
    intervalMs: 1000,
    mode: bit ? 'toggle' : max > low ? 'increment' : 'fixed',
    min: low,
    max,
    step: normalized && !integer ? 0.1 : 1,
  };
}
function dataRuleTargets(r) {
  const selected = new Set(
    globalThis.pointSelection.parse(r.points, dataEditor.protocol === 'iec104' ? 16777215 : 65535),
  );
  return dataEditor.points.filter((p) => p.kind === r.kind && selected.has(p.address));
}
function dataRulePoints(r) {
  const points = dataEditor.points
    .filter((p) => p.kind === r.kind && p.address >= r.start && p.address <= r.end)
    .map((p) => p.address);
  return (
    globalThis.pointSelection.format(points) ||
    (r.start === r.end ? String(r.start) : r.start + '-' + r.end)
  );
}

function dataModeOptions(kind) {
  return [
    ...(dataIsBit(kind)
      ? [
          ['toggle', '循环变位'],
          ['sequence', '逐点变位'],
        ]
      : [
          ['fixed', '固定值'],
          ['increment', '递增循环'],
          ['random', '区间随机'],
        ]),
    ...(dataEditor.protocol === 'iec104'
      ? [
          ['report:3', '保持当前值'],
          ['report:2', '背景扫描'],
          ...(kind === 'yc' ? [['report:1', '周期扫描']] : []),
        ]
      : []),
  ];
}
function renderDataRules(rules) {
  $('auto-data-rows').innerHTML = rules
    .map(
      (r, index) => `<tr data-data-row="${index}" data-data-kind="${escape(r.kind)}">
<td><input type="checkbox" data-data-field="enabled" aria-label="启用第${index + 1}组" ${r.enabled ? 'checked' : ''}></td>
<td><select data-data-field="kind" aria-label="数据类型">${dataKinds()
        .map(
          (k) =>
            `<option value="${k}" ${k === r.kind ? 'selected' : ''}>${escape(kinds[k])}</option>`,
        )
        .join('')}</select></td>
<td><input data-data-field="points" aria-label="点位" type="text" autocomplete="off" value="${escape(r.points ?? dataRulePoints(r))}"></td>
<td><input data-data-field="intervalMs" aria-label="周期" type="number" step="1" min="200" max="3600000" value="${escape(r.intervalMs)}"></td>
<td><select data-data-field="mode" aria-label="变化方式">${dataModeOptions(r.kind)
        .map(
          ([v, label]) =>
            `<option value="${v}" ${(r.mode === 'report' ? 'report:' + String(r.cause || 3) : r.mode) === v ? 'selected' : ''}>${label}</option>`,
        )
        .join('')}</select></td>
${['min', 'max', 'step'].map((k) => `<td><input data-data-field="${k}" type="number" step="any" aria-label="${k === 'min' ? '下限' : k === 'max' ? '上限' : '步长'}" value="${escape(r[k])}"></td>`).join('')}
<td><button type="button" data-data-copy="${index}">复制</button><button type="button" data-data-remove="${index}" aria-label="删除第${index + 1}组">×</button></td></tr>`,
    )
    .join('');
  for (const [index, row] of [...$('auto-data-rows').children].entries()) {
    const r = rules[index];
    row.querySelector('[data-data-field="mode"]').value =
      r.mode === 'report' ? 'report:' + String(r.cause || 3) : r.mode;
  }
  updateDataControls();
}
function readDataRules(validate = true) {
  return [...$('auto-data-rows').children].map((row, index) => {
    const get = (k) => row.querySelector('[data-data-field="' + k + '"]'),
      r = { enabled: get('enabled').checked, kind: get('kind').value, mode: get('mode').value };
    if (r.mode.startsWith('report:')) {
      r.cause = Number(r.mode.split(':')[1]);
      r.mode = 'report';
    }
    r.points = get('points').value;
    let selected = [];
    try {
      selected = globalThis.pointSelection.parse(
        r.points,
        dataEditor.protocol === 'iec104' ? 16777215 : 65535,
      );
    } catch (error) {
      if (validate && r.enabled) {
        throw Object.assign(Error('第' + (index + 1) + '组点位：' + error.message), {
          input: get('points'),
        });
      }
    }
    r.start = selected[0] ?? 0;
    r.end = selected[selected.length - 1] ?? 0;
    for (const k of ['intervalMs', 'min', 'max', 'step']) {
      const input = get(k),
        raw =
          input.disabled && input.dataset.inapplicable === 'true'
            ? (input.dataset.draftValue ?? '')
            : input.value;
      r[k] = validate ? Number(raw) : raw;
      const needed =
        k === 'intervalMs' ||
        (r.mode !== 'report' &&
          (k === 'min' ||
            (k === 'max' && r.mode !== 'fixed') ||
            (k === 'step' && r.mode === 'increment')));
      if (validate && r.enabled && needed && (!raw.trim() || !Number.isFinite(r[k]))) {
        throw Object.assign(Error('第' + (index + 1) + '组：请填写有效数值'), { input });
      }
    }
    if (validate) {
      if (r.mode === 'fixed') r.max = r.min;
      if (r.mode === 'report') r.min = r.max = 0;
      if (r.mode !== 'increment') r.step = 1;
    }
    if (validate && r.enabled) {
      if (!Number.isInteger(r.start) || !Number.isInteger(r.end) || r.start < 0 || r.end < r.start)
        throw Error('第' + (index + 1) + '组：点位范围无效');
      if (!Number.isInteger(r.intervalMs) || r.intervalMs < 200 || r.intervalMs > 3600000)
        throw Error('第' + (index + 1) + '组：周期应为200–3600000毫秒');
      if (r.mode !== 'report' && r.min > r.max)
        throw Error('第' + (index + 1) + '组：下限不能大于上限');
      if (r.mode !== 'report')
        for (const p of dataRuleTargets(r)) {
          for (const name of r.mode === 'fixed' ? ['min'] : ['min', 'max'])
            try {
              validatePointNumber(p, r[name], dataEditor.configuration);
            } catch (error) {
              throw Object.assign(
                Error(
                  '第' +
                    (index + 1) +
                    '组' +
                    (name === 'min' ? '下限' : '上限') +
                    '：' +
                    error.message,
                ),
                { input: get(name) },
              );
            }
          if (
            r.mode === 'increment' &&
            pointLimits(p, dataEditor.configuration).integer &&
            !Number.isInteger(r.step)
          ) {
            throw Object.assign(Error('第' + (index + 1) + '组：整数点的步长应为整数'), {
              input: get('step'),
            });
          }
        }
    }
    return r;
  });
}

async function openAutoDataWindow() {
  const c = channel(),
    d = device();
  if (!c || !d || c.role !== 'server') return;
  if (nativeDesktop && !nativeContext?.kind) {
    $('auto-data-dialog').show();
    return;
  }
  await initializeAutoDataWindow({
    channel: c.id,
    device: d.id,
  });
}
async function initializeAutoDataWindow(context) {
  const c = snapshot.project.channels.find((c) => c.id === context.channel),
    d = c?.devices.find((d) => d.id === context.device);
  if (!c || !d) throw Error('自动数据目标已不存在');
  dataEditor = {
    channel: c.id,
    device: d.id,
    protocol: c.protocol,
    points: d.points,
    configuration: d.configuration,
  };
  const title = c.protocol === 'iec104' ? '自动上报配置' : '自动数据配置';
  $('auto-data-dialog').querySelector('.dialog-heading h2').textContent = title;
  $('data-mode-caption').textContent = '方式';
  const view = await api('GetDataTest', c.id, d.id, 'telemetry');
  if (!view) throw Error('自动数据服务未就绪');
  $('auto-data-target').textContent = c.name + ' / ' + d.name;
  $('auto-data-disconnect-label').hidden = c.protocol !== 'iec104';
  $('auto-data-note').textContent = c.protocol === 'iec104' ? '' : '自动更新点值，由主站轮询读取';
  $('auto-data-disconnect').checked = view.config.stopOnDisconnect;
  renderDataRules(view.config.rules);
  renderDataState(view);
  dataEditor.initialDraft = JSON.stringify({
    rules: readDataRules(false),
    stopOnDisconnect: $('auto-data-disconnect').checked,
  });
  $('auto-data-dialog').show();
}
function renderDataState(view) {
  if (!view) return;
  dataView = view;
  if (dataEditor) {
    const target = snapshot.project.channels
      .find((c) => c.id === dataEditor.channel)
      ?.devices.find((d) => d.id === dataEditor.device);
    if (target) {
      dataEditor.points = target.points;
      dataEditor.configuration = target.configuration;
    }
  }
  updateDataControls();
}
function updateDataControls() {
  const locked = !!busy;
  for (const row of $('auto-data-rows').children) {
    const get = (k) => row.querySelector('[data-data-field="' + k + '"]'),
      enabled = get('enabled').checked,
      mode = get('mode').value.startsWith('report:') ? 'report' : get('mode').value;
    for (const el of row.querySelectorAll('input,select,button'))
      el.disabled =
        locked || (!enabled && el !== get('enabled') && !el.matches('[data-data-remove]'));
    for (const key of ['min', 'max', 'step']) {
      const input = get(key),
        applicable =
          key === 'min'
            ? mode !== 'report'
            : key === 'max'
              ? !['fixed', 'report'].includes(mode)
              : mode === 'increment';
      if (!applicable && input.dataset.inapplicable !== 'true') {
        input.dataset.draftValue = input.value;
        input.value = '';
        input.dataset.inapplicable = 'true';
      } else if (applicable && input.dataset.inapplicable === 'true') {
        input.value = input.dataset.draftValue ?? '';
        delete input.dataset.inapplicable;
      }
    }
    get('min').disabled = locked || !enabled || mode === 'report';
    get('max').disabled = locked || !enabled || mode === 'fixed' || mode === 'report';
    get('step').disabled = locked || !enabled || mode !== 'increment';
  }
  $('auto-data-add').disabled = locked;
  $('auto-data-save').disabled = locked;
  $('auto-data-disconnect').disabled = locked;
}
async function saveDataConfiguration() {
  if (!dataEditor) throw Error('请选择装置');
  if (
    dataEditor.initialDraft ===
    JSON.stringify({
      rules: readDataRules(false),
      stopOnDisconnect: $('auto-data-disconnect').checked,
    })
  ) {
    dataEditor.unchanged = true;
    return dataView;
  }
  dataEditor.unchanged = false;
  const config = { rules: readDataRules(), stopOnDisconnect: $('auto-data-disconnect').checked };
  const view = await api(
    'SaveDataTest',
    dataEditor.channel,
    dataEditor.device,
    'telemetry',
    config,
  );
  renderDataState(view);
  return view;
}
on(
  'auto-data-add',
  'click',
  () => {
    const rules = readDataRules(false);
    if (rules.length >= 256) throw Error('最多256组规则');
    rules.push({ ...newDataRule(dataKinds()[0]), enabled: true, points: '' });
    renderDataRules(rules);
  },
  false,
);
$('auto-data-rows').addEventListener('click', (e) => {
  const button = e.target.closest('[data-data-remove],[data-data-copy]');
  if (!button || button.disabled) return;
  const rules = readDataRules(false);
  if (button.dataset.dataCopy !== undefined) {
    if (rules.length >= 256) return;
    const i = Number(button.dataset.dataCopy);
    rules.splice(i + 1, 0, { ...rules[i], points: '' });
  } else rules.splice(Number(button.dataset.dataRemove), 1);
  renderDataRules(rules);
});
$('auto-data-rows').addEventListener('change', (e) => {
  if (e.target.dataset.dataField === 'kind') {
    const rules = readDataRules(false),
      index = Number(e.target.closest('tr').dataset.dataRow);
    const previous = rules[index],
      previousDefaults = newDataRule(e.target.closest('tr').dataset.dataKind),
      defaults = newDataRule(e.target.value);
    rules[index] = {
      ...previous,
      kind: e.target.value,
      mode: dataModeOptions(e.target.value).some(([mode]) => mode === previous.mode)
        ? previous.mode
        : defaults.mode,
    };
    for (const field of ['min', 'max', 'step']) {
      if (String(previous[field]) === String(previousDefaults[field]))
        rules[index][field] = defaults[field];
    }
    renderDataRules(rules);
  } else updateDataControls();
});
on('auto-data-form', 'submit', async (e) => {
  e.preventDefault();
  await saveDataConfiguration();
  if (!dataEditor.unchanged) await configurationSaved(dataEditor.channel);
  $('auto-data-dialog').close();
});
$('auto-data-dialog').addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !busy) {
    e.preventDefault();
    $('auto-data-dialog').close();
  }
});

// Keep numeric validation while disabling native increment/decrement shortcuts.
document.addEventListener(
  'keydown',
  (e) => {
    if (e.target.matches('input[type=number]') && ['ArrowUp', 'ArrowDown'].includes(e.key))
      e.preventDefault();
  },
  true,
);
document.addEventListener(
  'wheel',
  (e) => {
    if (e.target === document.activeElement && e.target.matches('input[type=number]'))
      e.preventDefault();
  },
  { passive: false, capture: true },
);

function renderEvents() {
  if (!$('events-dialog').open) return;
  const category = $('event-category').value,
    events = (snapshot.events || []).filter(
      (e) => e.channelId === selected && (!category || e.category === category),
    );
  $('event-count').textContent = events.length + ' 条';
  $('event-rows').innerHTML = events
    .slice(-500)
    .map(
      (e) =>
        `<tr><td>${escape(e.category)}</td><td>${escape(e.timestamp || e.received)}</td><td>${e.timeInvalid ? '无效' : '有效'}</td><td>${e.address}</td><td>${escape(kinds[e.kind] || e.kind)}</td><td>${escape(e.value)}</td><td>${e.quality}</td></tr>`,
    )
    .join('');
}
on('event-category', 'change', renderEvents, false);
on('clear-events', 'click', async () => {
  await act('ClearEvents', selected, $('event-category').value);
  renderEvents();
});

function updateByteOrders() {
  const host = $('original-device-panel');
  for (const prefix of ['yc', 'mc']) {
    const type =
        originalControl(host, 'cb_' + prefix + 'Type') ||
        originalControl(host, 'cb_' + prefix + 'Format'),
      order = originalControl(host, 'cb_' + prefix + 'FunWay');
    if (!type || !order) continue;
    const wide = prefix === 'mc' || Number(type.value.split(':')[0]) >= 2,
      items = wide ? ['ABCD', 'BADC', 'CDAB', 'DCBA'] : ['AB', 'BA'],
      index = Math.max(0, Number(order.value.split(':')[0]) || 0);
    order.innerHTML = items
      .map(
        (o, i) =>
          `<option value="${i}:${o}" ${i === Math.min(index, items.length - 1) ? 'selected' : ''}>${i}:${o}</option>`,
      )
      .join('');
  }
}
$('original-device-panel').addEventListener('change', (e) => {
  if (/cb_(yc|mc)(Type|Format)/.test(e.target.dataset.originalId || '')) {
    updateByteOrders();
  }
});

let remoteEditor = null,
  remoteFocusPoint = '';
async function openRemoteConfig(point = null) {
  const c = channel(),
    d = device(),
    category = $('point-kind').value.replace(/^tab:/, '');
  if (!c || !d || c.role !== 'server' || !['yk', 'yt'].includes(category)) return;
  remoteFocusPoint = point ? keyOf(point) : '';
  if (nativeDesktop && !nativeContext?.kind) {
    $('remote-config-dialog').show();
    return;
  }
  await initializeRemoteConfig({
    channel: c.id,
    device: d.id,
    pointKind: category,
    point: remoteFocusPoint,
  });
}
async function initializeRemoteConfig(context) {
  const c = snapshot.project.channels.find((c) => c.id === context.channel),
    d = c?.devices.find((d) => d.id === context.device),
    category = context.pointKind.replace(/^tab:/, '');
  if (!c || !d || c.role !== 'server' || !['yk', 'yt'].includes(category))
    throw Error('控制配置目标已不存在');
  remoteEditor = { channel: c.id, device: d.id, protocol: c.protocol, category, points: d.points };
  const cfg = await api('GetControlSimulation', c.id, d.id, category);
  if (!cfg) throw Error('控制配置服务未就绪');
  $('remote-config-title').textContent = tabNames[category] + '配置';
  $('remote-config-dialog').dataset.category = category;
  $('remote-target-caption').textContent = category === 'yk' ? '绑定遥信点位' : '绑定遥测点位';
  $('remote-response').value = cfg.mode;
  $('remote-delay').value = cfg.delayMs;
  const rows = cfg.mappings || [];
  if (context.point) {
    const [kind, address] = context.point.split(':');
    if (!rows.some((r) => r.source.kind === kind && r.source.address === Number(address)))
      rows.push({ source: { kind, address: Number(address) } });
  }
  renderRemoteMappings(rows);
  remoteEditor.initialDraft = JSON.stringify({
    rows: remoteMappingDrafts(),
    mode: $('remote-response').value,
    delay: $('remote-delay').value,
  });
  $('remote-config-dialog').show();
  if (context.point) {
    const address = context.point.split(':')[1],
      row = [...$('remote-mappings').children].find(
        (r) => r.querySelector('[data-remote-field=source]').value === address,
      );
    requestAnimationFrame(() => {
      row?.scrollIntoView?.({ block: 'center' });
      row?.querySelector('[data-remote-field=target]')?.focus();
    });
  }
}
function remotePointList(category) {
  return remoteEditor.points
    .filter((p) => {
      const current =
        p.tab ||
        (remoteEditor.protocol === 'iec104'
          ? p.kind === 'step'
            ? 'yt'
            : p.kind
          : { discrete: 'yx', coil: 'yk', holding: 'yc', input: 'yc' }[p.kind]);
      return current === category;
    })
    .sort((a, b) => a.address - b.address);
}
function renderRemoteMappings(rows) {
  const sources = remotePointList(remoteEditor.category),
    targets = remotePointList(remoteEditor.category === 'yk' ? 'yx' : 'yc');
  $('remote-mappings').innerHTML = rows
    .map(
      (r, i) =>
        `<tr><td><input type="text" data-remote-field="source" aria-label="控制点位" value="${escape(r.sourceText ?? r.source?.address ?? '')}" data-original-key="${escape(JSON.stringify(r.source || null))}"></td><td><input type="text" data-remote-field="target" aria-label="目标点位" value="${escape(r.targetText ?? r.target?.address ?? '')}" data-original-key="${escape(JSON.stringify(r.target || null))}"></td><td><input type="checkbox" data-remote-field="invert" aria-label="反向" ${r.invert ? 'checked' : ''}></td><td><button type="button" data-remote-copy="${i}">复制</button><button type="button" data-remote-remove="${i}" aria-label="删除映射">×</button></td></tr>`,
    )
    .join('');
  updateRemoteControls();
}
function remoteMappingDrafts() {
  return [...$('remote-mappings').children].map((row) => {
    const get = (k) => row.querySelector('[data-remote-field="' + k + '"]');
    return {
      sourceText: get('source').value,
      targetText: get('target').value,
      source: JSON.parse(get('source').dataset.originalKey || 'null'),
      target: JSON.parse(get('target').dataset.originalKey || 'null'),
      invert: get('invert').checked,
    };
  });
}
function readRemoteMappings() {
  const result = [],
    seen = new Set();
  for (const row of $('remote-mappings').children) {
    const get = (k) => row.querySelector('[data-remote-field="' + k + '"]'),
      resolve = (key, category) => {
        const input = get(key),
          previous = JSON.parse(input.dataset.originalKey || 'null');
        let addresses;
        try {
          addresses = globalThis.pointSelection.parse(
            input.value,
            remoteEditor.protocol === 'iec104' ? 16777215 : 65535,
          );
        } catch (error) {
          throw Object.assign(
            Error(
              '第' +
                ([...row.parentNode.children].indexOf(row) + 1) +
                '行' +
                (key === 'source' ? '控制' : '目标') +
                '点位：' +
                error.message,
            ),
            { input },
          );
        }
        return addresses.map((address) => {
          let matches = remotePointList(category).filter((p) => p.address === address);
          if (previous?.address === address && matches.some((p) => p.kind === previous.kind))
            matches = matches.filter((p) => p.kind === previous.kind);
          if (matches.length !== 1)
            throw Object.assign(
              Error(
                '第' +
                  (row.rowIndex || [...row.parentNode.children].indexOf(row) + 1) +
                  '行' +
                  (key === 'source' ? '控制' : '目标') +
                  '点位 ' +
                  address +
                  (matches.length ? ' 对应多个类型' : ' 未配置'),
              ),
              { input },
            );
          return { kind: matches[0].kind, address };
        });
      },
      sources = resolve('source', remoteEditor.category),
      targets = resolve('target', remoteEditor.category === 'yk' ? 'yx' : 'yc');
    if (result.length + sources.length * targets.length > 10000) throw Error('最多10000条映射');
    for (const source of sources)
      for (const target of targets) {
        const key = keyOf(source) + '>' + keyOf(target);
        if (seen.has(key))
          throw Object.assign(
            Error(
              '第' +
                ([...row.parentNode.children].indexOf(row) + 1) +
                '行：点位 ' +
                source.address +
                ' → ' +
                target.address +
                ' 绑定重复',
            ),
            { input: get('target') },
          );
        seen.add(key);
        result.push({
          source,
          target,
          invert: remoteEditor.category === 'yk' && get('invert').checked,
        });
      }
  }
  return result;
}

function updateRemoteControls() {
  let count = 0;
  for (const row of $('remote-mappings').children) {
    try {
      count +=
        globalThis.pointSelection.parse(row.querySelector('[data-remote-field=source]').value)
          .length *
        globalThis.pointSelection.parse(row.querySelector('[data-remote-field=target]').value)
          .length;
    } catch {}
  }
  $('remote-config-form').querySelector('[type=submit]').textContent =
    '保存（' + count + ' 条绑定）';
  const silent = $('remote-response').value === 'silent';
  $('remote-delay').disabled = silent || !!busy;
  for (const el of $('remote-config-form').querySelectorAll(
    'td:nth-child(3),#remote-invert-caption',
  ))
    el.hidden = remoteEditor?.category !== 'yk';
}
on('remote-response', 'change', updateRemoteControls, false);
$('remote-mappings').addEventListener('input', updateRemoteControls);
on(
  'remote-mapping-add',
  'click',
  () => {
    const rows = remoteMappingDrafts();
    rows.push(
      rows.length
        ? {}
        : {
            source: remotePointList(remoteEditor.category)[0],
            target: remotePointList(remoteEditor.category === 'yk' ? 'yx' : 'yc')[0],
          },
    );
    renderRemoteMappings(rows);
  },
  false,
);
$('remote-mappings').addEventListener('click', (e) => {
  const b = e.target.closest('[data-remote-remove]'),
    copy = e.target.closest('[data-remote-copy]');
  if (b) b.closest('tr').remove();
  if (copy) {
    const rows = remoteMappingDrafts(),
      i = [...copy.closest('tbody').children].indexOf(copy.closest('tr'));
    rows.splice(i + 1, 0, { ...rows[i], sourceText: '', source: null });
    renderRemoteMappings(rows);
  }
});
on('remote-config-form', 'submit', async (e) => {
  e.preventDefault();
  if (!remoteEditor) return;
  if (
    remoteEditor.initialDraft ===
    JSON.stringify({
      rows: remoteMappingDrafts(),
      mode: $('remote-response').value,
      delay: $('remote-delay').value,
    })
  ) {
    $('remote-config-dialog').close();
    return;
  }
  const cfg = {
    mode: $('remote-response').value,
    delayMs: integer('remote-delay', 0, 60000),
    mappings: readRemoteMappings(),
  };
  await api(
    'SaveControlSimulation',
    remoteEditor.channel,
    remoteEditor.device,
    remoteEditor.category,
    cfg,
  );
  $('remote-config-dialog').close();
  await configurationSaved(remoteEditor.channel);
});

let telemetryEditor = null,
  telemetrySelection = '';
function openTelemetryValues() {
  const c = channel(),
    d = device();
  if (!c || !d) return;
  telemetrySelection = globalThis.pointSelection.format(
    d.points
      .filter(
        (p) =>
          pointMatchesKind(p, $('point-kind').value) &&
          pointMatchesSearch(p, $('point-search').value.trim().toLowerCase()),
      )
      .map((p) => p.address),
  );
  if (!telemetrySelection) throw Error('当前筛选无匹配点位');
  if (nativeDesktop && !nativeContext?.kind) {
    $('telemetry-values-dialog').show();
    return;
  }
  initializeTelemetryValues({
    channel: c.id,
    device: d.id,
    pointKind: $('point-kind').value,
    selection: telemetrySelection,
  });
}
function initializeTelemetryValues(context) {
  const c = snapshot.project.channels.find((c) => c.id === context.channel),
    d = c?.devices.find((d) => d.id === context.device),
    category = context.pointKind.replace(/^tab:/, '');
  if (!d || c.role !== 'server' || !['yx', 'yc'].includes(category))
    throw Error('批量设置目标已不存在');
  telemetryEditor = {
    channel: c.id,
    device: d.id,
    category,
    configuration: d.configuration,
    points: d.points,
  };
  const points = d.points.filter((p) => pointCategory(p) === category);
  $('telemetry-values-title').textContent = tabNames[category] + '批量设置';
  $('telemetry-points').value =
    context.selection ?? globalThis.pointSelection.format(points.map((p) => p.address));
  for (const option of $('telemetry-mode').options)
    option.hidden =
      (['flip'].includes(option.value) && category !== 'yx') ||
      (option.value === 'raw16' && category !== 'yc') ||
      (option.value === 'quality' && c.protocol !== 'iec104');
  $('telemetry-mode').value = 'set';
  $('telemetry-value').value = '';
  $('telemetry-quality-enabled').checked = false;
  $('telemetry-quality-field').hidden = c.protocol !== 'iec104';
  telemetryEditor.protocol = c.protocol;
  $('telemetry-quality').value = 0;
  updateTelemetryFields();
  $('telemetry-values-dialog').show();
}
function updateTelemetryFields() {
  let count = 0;
  try {
    const selected = new Set(
      globalThis.pointSelection.parse(
        $('telemetry-points').value,
        telemetryEditor?.protocol === 'iec104' ? 16777215 : 65535,
      ),
    );
    count =
      telemetryEditor?.points.filter(
        (p) => pointCategory(p) === telemetryEditor.category && selected.has(p.address),
      ).length || 0;
  } catch {}
  const save = $('telemetry-values-form').querySelector('[type=submit]');
  save.textContent = '应用 ' + count + ' 点';
  save.disabled = !!busy || count === 0;
  save.setAttribute('aria-description', count ? '' : '无匹配点位');
  $('telemetry-value').step = $('telemetry-mode').value === 'raw16' ? '1' : 'any';
  $('telemetry-value').disabled = !['set', 'raw16'].includes($('telemetry-mode').value);
  $('telemetry-quality').disabled = !$('telemetry-quality-enabled').checked;
}
on('telemetry-points', 'input', updateTelemetryFields, false);
on(
  'telemetry-mode',
  'change',
  () => {
    if ($('telemetry-mode').value === 'quality') $('telemetry-quality-enabled').checked = true;
    updateTelemetryFields();
  },
  false,
);
on('telemetry-quality-enabled', 'change', updateTelemetryFields, false);
on('telemetry-values-form', 'submit', async (e) => {
  e.preventDefault();
  const r = {
      category: telemetryEditor.category,
      points: $('telemetry-points').value,
      mode: $('telemetry-mode').value,
      value: Number($('telemetry-value').value),
    },
    selected = new Set(
      globalThis.pointSelection.parse(
        r.points,
        telemetryEditor.protocol === 'iec104' ? 16777215 : 65535,
      ),
    );
  if (
    !telemetryEditor.points.some(
      (p) => pointCategory(p) === telemetryEditor.category && selected.has(p.address),
    )
  )
    throw Error('无匹配点位');
  if (r.mode === 'quality' && !$('telemetry-quality-enabled').checked) throw Error('请勾选品质');
  if (
    (['set', 'raw16'].includes(r.mode) && !$('telemetry-value').value.trim()) ||
    !Number.isFinite(r.value)
  )
    throw Error('请填写有效数值');
  if (r.mode === 'raw16') r.value = integer('telemetry-value', 0, 65535);
  if (r.mode === 'set')
    for (const p of telemetryEditor.points)
      if (pointCategory(p) === telemetryEditor.category && selected.has(p.address))
        validatePointNumber(p, r.value, telemetryEditor.configuration);
  if ($('telemetry-quality-enabled').checked) r.quality = integer('telemetry-quality', 0, 255);
  await api('SetTelemetryRange', telemetryEditor.channel, telemetryEditor.device, r);
  $('telemetry-values-dialog').close();
  await refresh();
  toast('设置已应用');
});

for (const id of ['remote-config-dialog', 'telemetry-values-dialog'])
  $(id).addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !busy) {
      e.preventDefault();
      $(id).close();
    }
  });

document.addEventListener('input', (e) => {
  const form = e.target.closest('form'),
    error = form?.querySelector('.form-error');
  if (error) error.hidden = true;
});
