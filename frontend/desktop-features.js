// Desktop-only workflows. The engine owns running jobs; editor polling never
// rewrites drafts, and closing an editor does not stop a project or channel.
globalThis.createDesktopFeatures = function (host) {
  const { $, api, ask, refresh } = host;
  const escape = (v) =>
    String(v ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  let tableSource = null,
    tablePreview = null,
    tableTicket = 0,
    tableBusy = false,
    packetView = null,
    packetDirty = false,
    packetBusy = false,
    statusBusy = false,
    tableTarget = [],
    packetChannel = '',
    packetTicket = 0;
  const showError = (id, error) => {
    const el = $(id).querySelector('.form-error');
    el.textContent = error?.message || String(error);
    el.hidden = false;
  };
  const clearError = (id) => {
    $(id).querySelector('.form-error').hidden = true;
  };
  const options = (rows, value) =>
    rows
      .map(
        ([key, text]) =>
          `<option value="${escape(key)}" ${String(value) === key ? 'selected' : ''}>${escape(text)}</option>`,
      )
      .join('');
  const pointKinds = [
    ['', '沿用类型列'],
    ['yx', '遥信'],
    ['yc', '遥测'],
    ['yk', '遥控'],
    ['yt', '遥调'],
    ['mc', '累计量'],
    ['position', '步位置'],
    ['step', '步调节'],
    ['coil', '线圈'],
    ['discrete', '离散输入'],
    ['holding', '保持寄存器'],
    ['input', '输入寄存器'],
  ];
  const target = () => [host.channel(), host.device()];
  async function initializeTable() {
    ++tableTicket;
    tableTarget = target();
    tableSource = tablePreview = null;
    $('point-table-file').value = '';
    $('point-table-preview').replaceChildren();
    $('point-table-summary').textContent = '';
    $('point-table-kind').innerHTML = options(pointKinds, host.kind());
    $('point-table-apply').disabled = true;
    clearError('point-table-dialog');
    $('point-table-dialog').show();
  }
  async function previewTable() {
    const ticket = ++tableTicket;
    tablePreview = null;
    $('point-table-apply').disabled = true;
    clearError('point-table-dialog');
    $('point-table-preview').replaceChildren();
    $('point-table-summary').textContent = '';
    if (!tableSource) return;
    try {
      const view = await api(
        'PreviewPointTable',
        ...tableTarget,
        tableSource.name,
        tableSource.data,
        $('point-table-kind').value,
        $('point-table-mode').value,
      );
      if (ticket !== tableTicket) return;
      tablePreview = view;
      $('point-table-summary').textContent =
        `导入 ${view.total} 点 · 新增 ${view.added} 点 · 同地址覆盖 ${view.updated} 点 · 移除 ${view.removed} 点${view.total > 100 ? ' · 预览前100点' : ''}`;
      $('point-table-preview').innerHTML = view.rows
        .map(
          (p, i) =>
            `<tr><td>${i + 1}</td><td>${escape(pointKinds.find(([k]) => k === p.kind)?.[1] || p.kind)}</td><td>${p.address}</td><td>${escape(p.value)}</td><td>${p.quality}</td><td>${escape(p.name)}</td></tr>`,
        )
        .join('');
      $('point-table-apply').disabled = false;
    } catch (error) {
      if (ticket === tableTicket) showError('point-table-dialog', error);
    }
  }
  $('open-point-table').addEventListener('click', () => {
    if (!host.device()) {
      host.error('请先选择装置');
      return;
    }
    initializeTable().catch(host.error);
  });
  $('point-table-file').addEventListener('change', async () => {
    const ticket = ++tableTicket,
      file = $('point-table-file').files?.[0];
    tablePreview = tableSource = null;
    $('point-table-apply').disabled = true;
    if (!file) return;
    try {
      if (file.size > 16 * 1024 * 1024) throw Error('点表不能超过16MB');
      const bytes = new Uint8Array(await file.arrayBuffer()),
        parts = [];
      for (let n = 0; n < bytes.length; n += 32768)
        parts.push(String.fromCharCode(...bytes.subarray(n, n + 32768)));
      if (ticket !== tableTicket) return;
      tableSource = { name: file.name, data: btoa(parts.join('')) };
      await previewTable();
    } catch (error) {
      showError('point-table-dialog', error);
    }
  });
  $('point-table-kind').addEventListener('change', previewTable);
  $('point-table-mode').addEventListener('change', previewTable);
  $('point-table-apply').addEventListener('click', async () => {
    if (!tablePreview || !tableSource || tableBusy) return;
    tableBusy = true;
    const controls = [
      ...$('point-table-dialog').querySelectorAll('input,select,button:not([data-close])'),
    ];
    const disabled = controls.map((el) => el.disabled);
    controls.forEach((el) => {
      el.disabled = true;
    });
    try {
      const mode = $('point-table-mode').value,
        view = tablePreview,
        source = tableSource,
        kind = $('point-table-kind').value,
        scope = [...tableTarget];
      const result = await ask({
        title: '导入点表',
        text: `${$('point-table-mode').selectedOptions[0].textContent}，导入${view.total}个点位。当前通道将停止，工程保存后可手动重连。`,
        danger: mode !== 'merge',
      });
      if (result.choice !== 'ok') return;
      $('point-table-apply').disabled = true;
      try {
        await api(
          'ApplyPointTable',
          ...scope,
          source.name,
          source.data,
          kind,
          mode,
          view.sourceSHA256,
          view.revision,
        );
        tablePreview = null;
        await refresh(true);
        $('point-table-summary').textContent = '已导入，请保存工程';
      } catch (error) {
        showError('point-table-dialog', error);
      }
    } finally {
      tableBusy = false;
      controls.forEach((el, i) => {
        el.disabled = disabled[i];
      });
      $('point-table-apply').disabled = !tablePreview;
    }
  });
  $('point-table-export').addEventListener('click', async () => {
    clearError('point-table-dialog');
    try {
      const path = await api(
        'ExportPointTable',
        ...tableTarget,
        $('point-table-export-scope').value === 'current' ? host.kind() : '',
        $('point-table-format').value,
      );
      if (path) $('point-table-summary').textContent = '已导出：' + path;
    } catch (error) {
      showError('point-table-dialog', error);
    }
  });
  const formats = [
    ['hex', 'HEX'],
    ['text', 'UTF-8文本'],
  ];
  const checksums = [
    ['none', '不追加'],
    ['crc16', 'Modbus CRC16低字节在前'],
    ['crc16-high', 'Modbus CRC16高字节在前'],
    ['sum', '累加和'],
    ['xor', '异或'],
  ];
  function rawRow(type, value = {}) {
    const tr = document.createElement('tr');
    tr.dataset.packetRow = type;
    const input = (key, v, numeric = false) =>
      `<input data-packet-field="${key}" type="${numeric ? 'number' : 'text'}" value="${escape(v)}" aria-label="${escape({ offset: '识别起始字节', match: '识别内容', response: '应答报文', data: '发送报文', delayMs: '应答延迟' }[key] || key)}" ${numeric ? 'min="0" max="60000"' : ''}>`;
    tr.innerHTML =
      `<td class="packet-row-number"></td><td><input type="checkbox" data-packet-field="enabled" aria-label="启用" ${value.enabled !== false ? 'checked' : ''}></td>` +
      (type === 'reply'
        ? `<td>${input('offset', value.offset ?? 0, true)}</td><td>${input('match', value.match ?? '')}</td>`
        : '') +
      `<td>${input(type === 'reply' ? 'response' : 'data', value.response ?? value.data ?? '')}</td><td><select data-packet-field="format" aria-label="报文格式">${options(formats, value.format || 'hex')}</select></td><td><select data-packet-field="checksum" aria-label="追加校验">${options(checksums, value.checksum || 'none')}</select></td>` +
      (type === 'reply' ? `<td>${input('delayMs', value.delayMs ?? 0, true)}</td>` : '') +
      '<td><button type="button" data-packet-move="-1" aria-label="上移">↑</button><button type="button" data-packet-move="1" aria-label="下移">↓</button><button type="button" data-packet-delete aria-label="删除报文">×</button></td>';
    return tr;
  }
  function numberRows() {
    for (const id of ['packet-replies', 'packet-sends'])
      [...$(id).children].forEach((r, i) => {
        r.querySelector('.packet-row-number').textContent = String(i + 1);
      });
  }
  function updateFraming() {
    const framing = $('packet-framing').value;
    for (const [id, mode] of [
      ['packet-idle', 'idle'],
      ['packet-length', 'fixed'],
      ['packet-delimiter', 'delimiter'],
    ])
      $(id).closest('label').hidden = framing !== mode;
  }
  $('packet-framing').addEventListener('change', updateFraming);
  function rawConfig() {
    const rows = (id) =>
      [...$(id).children].map((tr) => {
        const row = {};
        for (const el of tr.querySelectorAll('[data-packet-field]'))
          row[el.dataset.packetField] =
            el.type === 'checkbox'
              ? el.checked
              : el.type === 'number'
                ? Number(el.value)
                : el.value;
        return row;
      });
    return {
      framing: $('packet-framing').value,
      frameLength: Number($('packet-length').value),
      delimiter: $('packet-delimiter').value,
      idleMs: Number($('packet-idle').value),
      replies: rows('packet-replies'),
      rows: rows('packet-sends'),
      selection: $('packet-selection').value,
      intervalMs: Number($('packet-interval').value),
      cycleMs: Number($('packet-cycle').value),
      cycles: Number($('packet-cycles').value),
    };
  }
  function showPacketStatus(view) {
    packetView = view;
    for (const mode of ['reply', 'send']) {
      const status = view[mode],
        button = $('packet-' + mode + '-toggle');
      button.textContent = status.running ? '停止' : '启动';
      button.disabled = packetBusy;
      $('packet-' + mode + '-status').textContent =
        (status.running ? '运行中' : '未运行') +
        ` · 已发送 ${status.sent}` +
        (mode === 'send' ? ` · 已完成 ${status.cycles} 轮` : '') +
        (status.error ? ' · ' + status.error : '');
    }
    const running = view.reply.running || view.send.running;
    for (const el of $('packet-editor').querySelectorAll('input,select,button'))
      el.disabled = running || packetBusy;
    $('packet-save').disabled = running || packetBusy || !packetDirty;
  }
  async function initializePacket() {
    clearError('packet-tools-dialog');
    packetDirty = false;
    packetView = null;
    packetChannel = host.channel();
    const ticket = ++packetTicket;
    const view = await api('GetRawTools', packetChannel),
      c = view.config;
    if (ticket !== packetTicket) return;
    $('packet-framing').value = c.framing;
    $('packet-length').value = c.frameLength || 1;
    $('packet-delimiter').value = c.delimiter || '0D0A';
    $('packet-idle').value = c.idleMs || 30;
    $('packet-selection').value = c.selection || '';
    $('packet-interval').value = c.intervalMs;
    $('packet-cycle').value = c.cycleMs;
    $('packet-cycles').value = c.cycles;
    $('packet-replies').replaceChildren(...c.replies.map((v) => rawRow('reply', v)));
    $('packet-sends').replaceChildren(...c.rows.map((v) => rawRow('send', v)));
    numberRows();
    updateFraming();
    showPacketStatus(view);
    $('packet-tools-dialog').show();
  }
  $('open-packet-tools').addEventListener('click', () => {
    if (host.protocol() !== 'raw') {
      host.error('请先选择RAW调试通道，可从工具箱新建');
      return;
    }
    initializePacket().catch(host.error);
  });
  for (const [button, type, table] of [
    ['packet-add-reply', 'reply', 'packet-replies'],
    ['packet-add-send', 'send', 'packet-sends'],
  ])
    $(button).addEventListener('click', () => {
      if ($(table).children.length >= 256) {
        showError('packet-tools-dialog', Error('每个列表最多256条'));
        return;
      }
      $(table).append(rawRow(type));
      numberRows();
      packetDirty = true;
      showPacketStatus(packetView);
    });
  $('packet-editor').addEventListener('input', () => {
    packetDirty = true;
    $('packet-save').disabled = false;
  });
  $('packet-editor').addEventListener('change', () => {
    packetDirty = true;
    $('packet-save').disabled = false;
  });
  $('packet-editor').addEventListener('click', (event) => {
    const button = event.target.closest('[data-packet-delete],[data-packet-move]');
    if (!button) return;
    const row = button.closest('tr');
    if (button.hasAttribute('data-packet-delete')) row.remove();
    else if (button.dataset.packetMove === '-1' && row.previousElementSibling)
      row.parentElement.insertBefore(row, row.previousElementSibling);
    else if (button.dataset.packetMove === '1' && row.nextElementSibling)
      row.parentElement.insertBefore(row.nextElementSibling, row);
    numberRows();
    packetDirty = true;
    $('packet-save').disabled = false;
  });
  $('packet-save').addEventListener('click', async () => {
    if (!packetView || packetBusy) return;
    clearError('packet-tools-dialog');
    const config = rawConfig();
    ++packetTicket;
    packetBusy = true;
    showPacketStatus(packetView);
    try {
      await api('SaveRawTools', packetChannel, config);
      packetDirty = false;
      await refresh(true);
      showPacketStatus(await api('GetRawTools', packetChannel));
    } catch (error) {
      showError('packet-tools-dialog', error);
    } finally {
      packetBusy = false;
      showPacketStatus(packetView);
    }
  });
  for (const mode of ['reply', 'send'])
    $('packet-' + mode + '-toggle').addEventListener('click', async () => {
      if (!packetView || packetBusy) return;
      clearError('packet-tools-dialog');
      if (!packetView[mode].running && packetDirty) {
        showError('packet-tools-dialog', Error('请先保存报文配置'));
        return;
      }
      ++packetTicket;
      packetBusy = true;
      showPacketStatus(packetView);
      try {
        await api(packetView[mode].running ? 'StopRawTask' : 'StartRawTask', packetChannel, mode);
        showPacketStatus(await api('GetRawTools', packetChannel));
      } catch (error) {
        showError('packet-tools-dialog', error);
      } finally {
        packetBusy = false;
        showPacketStatus(packetView);
      }
    });
  const toolSpecs = {
    number: [
      ['integer', '整数进制转换'],
      ['bit-mask', '位掩码分析'],
      ['register16', '2 字节寄存器解读'],
      ['register32', '4 字节寄存器解读'],
      ['register64', '8 字节寄存器解读'],
      ['float32-tohex', 'Float32 → 字节'],
      ['hex-tofloat32', '字节 → Float32'],
      ['float64-tohex', 'Float64 → 字节'],
      ['hex-tofloat64', '字节 → Float64'],
    ],
    encoding: [
      ['encode', '文本 → 十六进制'],
      ['decode', '十六进制 → 文本'],
    ],
    string: [
      ['base64-encode', 'Base64编码'],
      ['base64-decode', 'Base64解码'],
      ['url-encode', 'URL编码'],
      ['url-decode', 'URL解码'],
      ['json-format', 'JSON格式化'],
      ['json-compact', 'JSON压缩'],
      ['hex-format', 'HEX格式化'],
      ['hexdump', 'HEX转储'],
      ['hex-to-base64', 'HEX → Base64'],
      ['base64-to-hex', 'Base64 → HEX'],
      ['escape', '字符串转义'],
      ['unescape', '字符串反转义'],
      ['reverse-bytes', '字节倒序'],
    ],
    time: [],
    iec104: [
      ['decode', '解析 IEC104 报文'],
      ['cp56', '解析 CP56Time2a'],
    ],
    modbus: [
      ['request', '解析请求'],
      ['response', '解析响应'],
      ['read', '生成读取报文'],
      ['build', '封装报文'],
    ],
    network: [['subnet', 'IPv4 网段计算']],
  };
  const toolDrafts = new Map();
  const isPacketTool = (kind) => kind === 'iec104' || kind === 'modbus';
  let packetTool = 'iec104';
  let modbusOperation = 'request';
  let modbusInactiveInput = '';
  let currentTool = 'number',
    toolTicket = 0;
  const names = {
    number: '数值转换',
    encoding: '编码转换',
    string: '文本处理',
    time: '时间戳',
    packet: '报文解析',
    network: 'IPv4 网段',
  };
  const swaps = {
    encode: 'decode',
    decode: 'encode',
    'base64-encode': 'base64-decode',
    'base64-decode': 'base64-encode',
    'url-encode': 'url-decode',
    'url-decode': 'url-encode',
    'hex-to-base64': 'base64-to-hex',
    'base64-to-hex': 'hex-to-base64',
    escape: 'unescape',
    unescape: 'escape',
  };
  for (const [kind, name] of Object.entries(names)) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = name;
    button.dataset.tool = kind;
    button.addEventListener('click', () => {
      $('tool-kind').value = kind === 'packet' ? packetTool : kind;
      changeTool();
    });
    $('tool-navigation').append(button);
  }
  const parameters = [
    'tool-input',
    'tool-operation',
    'tool-encoding',
    'tool-order',
    'tool-zone',
    'tool-transport',
    'tool-transaction',
    'tool-read-unit',
    'tool-read-function',
    'tool-read-address',
    'tool-read-quantity',
  ];
  const calendar = $('tool-time-date');
  const calendarMask = calendar.defaultValue;
  const calendarSlots = [...calendarMask].map((character) => /[a-z]/i.test(character));
  const fractionIndex = calendarMask.indexOf('.');
  const calendarPattern = /^(\d{4})(-?)(\d{2})\2(\d{2})[T ](\d{2}:\d{2}:\d{2})(?:\.(\d{1,3}))?/;
  let calendarMilliseconds = calendarMask.slice(fractionIndex + 1);
  let timeClock;
  let timeTicket = 0;
  const useMilliseconds = () => $('tool-time-unit').value === 'unix-ms';

  function syncTimeClock() {
    clearInterval(timeClock);
    if (currentTool !== 'time' || !$('toolbox-dialog').open || document.hidden) return;
    const update = () => {
      const now = Date.now();
      const stamp = String(useMilliseconds() ? now : Math.floor(now / 1000));
      if ($('tool-time-live').value !== stamp) $('tool-time-live').value = stamp;
    };
    update();
    timeClock = setInterval(update, useMilliseconds() ? 50 : 100);
  }

  function clearTimeFeedback() {
    ++timeTicket;
    $('tool-time-error').hidden = true;
  }
  function showTimeError(error) {
    $('tool-time-error').textContent = error?.message || String(error);
    $('tool-time-error').hidden = false;
  }

  function fillCalendar(match) {
    const [, year, , month, day, time, milliseconds = ''] = match;
    calendarMilliseconds = milliseconds.padEnd(3, '0');
    calendar.value =
      year + month + day + ' ' + time + (useMilliseconds() ? '.' + calendarMilliseconds : '');
  }

  function editCalendar(digits, deleteDirection = 0) {
    let start = calendar.selectionStart;
    let end = calendar.selectionEnd;
    if (deleteDirection < 0 && start === end) {
      do start--;
      while (start >= 0 && !calendarSlots[start]);
      start = Math.max(0, start);
    } else if (deleteDirection > 0 && start === end) {
      while (start < calendar.value.length && !calendarSlots[start]) start++;
      end = start + 1;
    }
    const value = [...calendar.value];
    for (let index = start; index < end && index < value.length; index++) {
      if (calendarSlots[index]) value[index] = calendarMask[index];
    }
    let cursor = start;
    for (const digit of digits) {
      while (cursor < value.length && !calendarSlots[cursor]) cursor++;
      if (cursor === value.length) break;
      value[cursor++] = digit;
    }
    if (digits) while (cursor < value.length && !calendarSlots[cursor]) cursor++;
    calendar.value = value.join('');
    calendar.setSelectionRange(cursor, cursor);
    clearTimeFeedback();
  }

  function readCalendar() {
    const match = calendarPattern.exec(calendar.value);
    if (!match) throw Error('请填写完整日期时间');
    const [, year, , month, day, time] = match;
    const fraction = calendar.value.slice(fractionIndex).replaceAll('S', '0');
    return year + '-' + month + '-' + day + ' ' + time + fraction;
  }

  async function convertTimestamp(fromDate) {
    clearTimeFeedback();
    const ticket = timeTicket;
    const unit = $('tool-time-unit').value;
    try {
      const input = fromDate ? readCalendar() : $('tool-time-stamp').value.trim();
      if (!input) throw Error('请输入时间戳');
      const result = await api('RunDesktopTool', {
        kind: 'time',
        operation: fromDate ? 'date' : unit,
        input,
        zone: $('tool-zone').value,
      });
      if (ticket !== timeTicket) return;
      if (fromDate) {
        const field = unit === 'unix-ms' ? 'Unix毫秒' : 'Unix秒';
        $('tool-time-stamp').value = result.fields.find(({ name }) => name === field).value;
      } else {
        const date = result.fields.find(({ name }) => name === '日期时间').value;
        fillCalendar(calendarPattern.exec(date));
      }
    } catch (error) {
      if (ticket === timeTicket) showTimeError(error);
    }
  }

  for (const [id, fromDate] of [
    ['tool-time-from-stamp', false],
    ['tool-time-from-date', true],
  ]) {
    $(id).addEventListener('submit', (event) => {
      event.preventDefault();
      void convertTimestamp(fromDate);
    });
    $(id).addEventListener('input', clearTimeFeedback);
  }
  calendar.addEventListener('beforeinput', (event) => {
    event.preventDefault();
    const { inputType, data } = event;
    if (inputType.startsWith('delete')) {
      editCalendar('', inputType.endsWith('Backward') ? -1 : inputType.endsWith('Forward') ? 1 : 0);
    } else if (data && /^\d+$/.test(data)) {
      editCalendar(data);
    }
  });
  calendar.addEventListener('paste', (event) => {
    event.preventDefault();
    const pasted = event.clipboardData.getData('text').trim();
    const match = calendarPattern.exec(pasted);
    if (match?.[0] === pasted) {
      fillCalendar(match);
      clearTimeFeedback();
    } else if (/^\d+$/.test(pasted)) {
      editCalendar(pasted);
    }
  });
  $('tool-time-unit').addEventListener('change', () => {
    if (calendar.value.length > fractionIndex)
      calendarMilliseconds = calendar.value.slice(fractionIndex + 1);
    calendar.value =
      calendar.value.slice(0, fractionIndex) +
      (useMilliseconds() ? '.' + calendarMilliseconds : '');
    clearTimeFeedback();
    syncTimeClock();
  });
  $('tool-time-copy-now').addEventListener('click', async () => {
    clearTimeFeedback();
    const ticket = timeTicket;
    try {
      await api('CopyDesktopText', $('tool-time-live').value);
    } catch (error) {
      if (ticket === timeTicket) showTimeError(error);
    }
  });
  $('toolbox-dialog').addEventListener('close', syncTimeClock);
  document.addEventListener('visibilitychange', syncTimeClock);

  const usesTextOutput = () => ['paired', 'generated'].includes($('tool-workspace').dataset.layout);
  function clearToolResult() {
    ++toolTicket;
    $('tool-output').value = '';
    $('tool-output').hidden = !usesTextOutput();
    $('tool-fields').replaceChildren();
    $('toolbox-run').disabled = false;
    $('toolbox-parse').disabled = true;
    $('toolbox-copy').disabled = true;
    $('toolbox-swap').disabled = true;
    clearError('toolbox-dialog');
  }
  function updateToolLayout() {
    const operation = $('tool-operation').value;
    const timestamp = currentTool === 'time',
      cp56 = currentTool === 'iec104' && operation === 'cp56',
      modbus = currentTool === 'modbus',
      read = modbus && operation === 'read',
      build = modbus && (operation === 'build' || read),
      paired = currentTool === 'encoding' || currentTool === 'string' || build,
      decoded = isPacketTool(currentTool) && !paired && !cp56;
    $('tool-workspace').dataset.layout = read
      ? 'generated'
      : paired
        ? 'paired'
        : decoded
          ? 'decoded'
          : cp56
            ? 'cp56'
            : currentTool;
    $('tool-workspace').hidden = timestamp;
    $('tool-time-workspace').hidden = !timestamp;
    $('tool-time-unit-wrap').hidden = !timestamp;
    $('toolbox-run').hidden = timestamp;
    $('toolbox-run').parentElement.hidden = timestamp;
    syncTimeClock();
    $('tool-protocol-wrap').hidden = !isPacketTool(currentTool);
    $('tool-read-fields').hidden = !read;
    $('tool-source').hidden = read;
    $('tool-operation-wrap').hidden = timestamp || currentTool === 'network';
    $('tool-transport-wrap').hidden = !modbus;
    $('tool-transaction-wrap').hidden = !build || $('tool-transport').value !== 'tcp';
    $('tool-encoding-wrap').hidden = currentTool !== 'encoding';
    $('tool-order-wrap').hidden =
      currentTool !== 'number' || ['integer', 'bit-mask'].includes(operation);
    const fourBytes = operation.includes('32');
    for (const option of $('tool-order').options) {
      if (!['word-swap', 'byte-swap'].includes(option.value)) continue;
      option.hidden = option.disabled = !fourBytes;
      if (option.disabled && option.selected && currentTool === 'number')
        $('tool-order').value = 'little';
    }
    $('tool-zone-wrap').hidden = !timestamp && !cp56;
    $('tool-output').hidden = !paired;
    $('toolbox-parse').hidden = !build;
    $('toolbox-swap').hidden = !paired || !swaps[operation];
    const runLabel = build
      ? read
        ? '生成报文'
        : '封装报文'
      : modbus || currentTool === 'iec104'
        ? '解析报文'
        : currentTool === 'network'
          ? '计算网段'
          : '转换';
    $('toolbox-run').setAttribute('aria-label', runLabel);
    $('toolbox-run').title = runLabel;
    let label = '原始文本',
      output = '结果',
      sample = '';
    if (currentTool === 'number') {
      const bytes = operation.includes('64') ? 8 : operation === 'register16' ? 2 : 4;
      label =
        operation === 'integer'
          ? '64位整数'
          : operation.startsWith('hex-')
            ? `Float${bytes * 8}字节（HEX）`
            : `Float${bytes * 8}数值`;
      if (operation.startsWith('register')) label = `寄存器字节（${bytes} 字节 HEX）`;
      if (operation.startsWith('register') || operation.startsWith('hex-')) {
        const order = $('tool-order').value;
        sample =
          bytes === 2
            ? 'FF FF'
            : bytes === 4
              ? {
                  big: '3F 80 00 00',
                  little: '00 00 80 3F',
                  'word-swap': '00 00 3F 80',
                  'byte-swap': '80 3F 00 00',
                }[order]
              : order === 'big'
                ? '3F F0 00 00 00 00 00 00'
                : '00 00 00 00 00 00 F0 3F';
      } else if (operation === 'integer' || operation === 'bit-mask') {
        label = '64 位整数';
        sample = operation === 'bit-mask' ? '0xA5' : '255';
      } else {
        sample = '1.5';
      }
    } else if (currentTool === 'encoding') {
      label = operation === 'encode' ? '原始文本' : '编码字节（HEX）';
      output = operation === 'encode' ? '编码字节（HEX）' : '解码文本';
      sample =
        operation === 'encode'
          ? '中文ABC'
          : {
              utf8: 'E4 B8 AD E6 96 87',
              gbk: 'D6 D0 CE C4',
              gb18030: 'D6 D0 CE C4',
              utf16le: '2D 4E 87 65',
              utf16be: '4E 2D 65 87',
            }[$('tool-encoding').value];
    } else if (currentTool === 'string') {
      const hint = {
        'base64-encode': ['待编码文本', 'Base64文本', '中文ABC'],
        'base64-decode': ['Base64文本', '解码文本', '5Lit5paH'],
        'url-encode': ['待编码文本', 'URL编码文本', '中文ABC'],
        'url-decode': ['URL编码文本', '解码文本', '%E4%B8%AD%E6%96%87'],
        'json-format': ['JSON文本', '格式化结果', '{"name":"示例","value":1}'],
        'json-compact': ['JSON文本', '压缩结果', '{"name": "示例", "value": 1}'],
        'hex-format': ['原始字节（HEX）', '规范 HEX', '01:03:00:00'],
        hexdump: ['原始字节（HEX）', '偏移 · 字节 · ASCII', '48 65 6C 6C 6F'],
        'hex-to-base64': ['原始字节（HEX）', 'Base64文本', 'FF 00 01'],
        'base64-to-hex': ['Base64文本', '原始字节（HEX）', '/wAB'],
        escape: ['待转义文本', '带引号的字符串', '第一行'],
        unescape: ['带引号的字符串', '还原文本', '"第一行\\n第二行"'],
        'reverse-bytes': ['原始字节（HEX）', '倒序字节（HEX）', '01 02 03 04'],
      }[operation];
      [label, output, sample] = hint;
    } else if (cp56) {
      label = 'CP56Time2a（7字节）';
      sample = '00 00 00 00 81 01 1A';
    } else if (currentTool === 'network') {
      label = 'IPv4 地址与前缀（CIDR）';
      sample = '192.168.1.25/24';
    } else if (modbus) {
      const format = $('tool-transport').value;
      if (build) {
        label = '站号 + 功能码 + 数据（HEX）';
        output = '完整报文';
        sample = '01 03 00 00 00 01';
      } else {
        output = '解析结果';
        label = format === 'ascii' ? '完整 ASCII 报文' : '完整报文（HEX）';
        const response = operation === 'response';
        sample = {
          rtu: response ? '01 03 02 00 2A 39 9B' : '01 03 00 00 00 01 84 0A',
          tcp: response
            ? '00 01 00 00 00 05 01 03 02 00 2A'
            : '00 01 00 00 00 06 01 03 00 00 00 01',
          ascii: response ? ':010302002AD0' : ':010300000001FB',
        }[format];
      }
      if (read) sample = '';
    } else if (currentTool === 'iec104') {
      output = '解析结果';
      label = 'APDU（HEX）';
      sample = '68 04 07 00 00 00';
    }
    $('tool-input-label').textContent = label;
    $('tool-output-label').textContent = output;
    $('tool-example').hidden = !sample;
    $('tool-example').dataset.sample = sample;
    const category = isPacketTool(currentTool) ? 'packet' : currentTool;
    for (const button of $('tool-navigation').children) {
      const selected = button.dataset.tool === category;
      button.classList.toggle('selected', selected);
      button.setAttribute('aria-pressed', String(selected));
    }
  }
  function changeTool() {
    toolDrafts.set(currentTool, Object.fromEntries(parameters.map((id) => [id, $(id).value])));
    currentTool = $('tool-kind').value;
    const draft = toolDrafts.get(currentTool);
    $('tool-operation').innerHTML = options(
      toolSpecs[currentTool],
      draft?.['tool-operation'] || toolSpecs[currentTool][0]?.[0],
    );
    for (const id of parameters.filter((id) => id !== 'tool-operation'))
      $(id).value =
        draft?.[id] ??
        ({
          'tool-encoding': 'utf8',
          'tool-order': 'little',
          'tool-zone': '+08:00',
          'tool-transport': 'rtu',
          'tool-transaction': '1',
          'tool-read-unit': '1',
          'tool-read-function': '3',
          'tool-read-address': '0',
          'tool-read-quantity': '1',
        }[id] ||
          '');
    if (currentTool === 'time' && !draft) {
      const offset = $('tool-zone').value === '+08:00' ? 8 * 60 * 60 * 1000 : 0;
      const today = new Date(Date.now() + offset).toISOString().slice(0, 10);
      fillCalendar(calendarPattern.exec(today + ' 00:00:00'));
    }
    if (isPacketTool(currentTool)) {
      packetTool = currentTool;
      $('tool-protocol').value = packetTool;
    }
    if (currentTool === 'modbus') modbusOperation = $('tool-operation').value;
    clearTimeFeedback();
    clearToolResult();
    updateToolLayout();
  }
  function changeToolOperation() {
    if (currentTool === 'modbus') {
      const operation = $('tool-operation').value;
      // The frame payload and the complete frame are separate drafts.
      if ((operation === 'build') !== (modbusOperation === 'build')) {
        const input = $('tool-input').value;
        $('tool-input').value = modbusInactiveInput;
        modbusInactiveInput = input;
      }
      modbusOperation = operation;
    }
    clearToolResult();
    updateToolLayout();
  }
  function renderToolResult(result) {
    $('tool-output').value = result.text;
    $('toolbox-parse').disabled = !result.text;
    $('toolbox-copy').disabled = !result.text;
    $('toolbox-swap').disabled = !result.text;
    const fields = result.fields ?? [];
    $('tool-output').hidden = !usesTextOutput() && fields.length > 0;
    $('tool-fields').replaceChildren();
    $('tool-fields').scrollTop = 0;
    if (!$('tool-output').hidden) return;
    const group = (name) => {
      const details = document.createElement('details');
      details.open = true;
      const heading = document.createElement('summary');
      heading.textContent = name;
      details.append(heading);
      const list = document.createElement('dl');
      details.append(list);
      $('tool-fields').append(details);
      return list;
    };
    let groups = null;
    if (currentTool === 'modbus' && fields[0]?.name === '协议')
      groups = { header: group('帧与校验'), points: group('功能与数据') };
    if (currentTool === 'iec104' && $('tool-operation').value === 'decode')
      groups = {
        header: group('帧头'),
        control: group('控制域'),
        asdu: group('ASDU'),
        points: group('信息体与操作'),
      };
    for (const field of fields) {
      let parent = $('tool-fields');
      if (groups && currentTool === 'modbus')
        parent =
          groups[/^(协议|站号|CRC|LRC|事务编号|MBAP长度)$/.test(field.name) ? 'header' : 'points'];
      else if (groups)
        parent =
          groups[
            /^(APDU)/.test(field.name)
              ? 'header'
              : /^(帧格式|发送序号|接收序号|命令)$/.test(field.name)
                ? 'control'
                : /^(信息体原始|信息体\d|控制|校时)/.test(field.name)
                  ? 'points'
                  : 'asdu'
          ];
      const row = document.createElement(groups ? 'div' : 'dl');
      row.className = 'tool-field';
      const label = document.createElement('dt'),
        value = document.createElement('dd');
      label.textContent = field.name;
      value.textContent = field.value;
      row.append(label, value);
      parent.append(row);
    }
    if (groups)
      for (const list of Object.values(groups))
        if (!list.children.length) list.parentElement.remove();
  }
  $('tool-input').addEventListener('input', clearToolResult);
  for (const id of [
    'tool-transaction',
    'tool-read-unit',
    'tool-read-address',
    'tool-read-quantity',
  ])
    $(id).addEventListener('input', clearToolResult);
  $('tool-read-function').addEventListener('change', clearToolResult);
  $('tool-example').addEventListener('click', () => {
    $('tool-input').value = $('tool-example').dataset.sample;
    clearToolResult();
    $('tool-input').focus();
  });
  const toolboxDialog = $('toolbox-dialog');
  toolboxDialog.addEventListener('pointerdown', () => toolboxDialog.classList.add('pointer-focus'));
  toolboxDialog.addEventListener('keydown', (event) => {
    if (event.key === 'Tab') toolboxDialog.classList.remove('pointer-focus');
    if (event.isComposing || event.key !== 'Enter') return;
    if (currentTool === 'time') {
      const form = event.target.matches('input') && event.target.closest('form');
      if (form) {
        event.preventDefault();
        form.requestSubmit();
      }
    } else if (event.ctrlKey || event.metaKey) {
      event.preventDefault();
      $('toolbox-run').click();
    }
  });
  $('tool-operation').addEventListener('change', changeToolOperation);
  for (const id of ['tool-encoding', 'tool-order', 'tool-zone', 'tool-transport'])
    $(id).addEventListener('change', () => {
      clearTimeFeedback();
      clearToolResult();
      updateToolLayout();
    });
  $('tool-kind').addEventListener('change', changeTool);
  $('tool-protocol').addEventListener('change', () => {
    $('tool-kind').value = $('tool-protocol').value;
    changeTool();
  });
  $('toolbox-swap').addEventListener('click', () => {
    const inverse = swaps[$('tool-operation').value],
      value = $('tool-output').value;
    if (!inverse || !value) return;
    $('tool-operation').value = inverse;
    $('tool-input').value = value;
    clearToolResult();
    updateToolLayout();
    $('tool-input').focus();
  });
  $('toolbox-parse').addEventListener('click', () => {
    const input = $('tool-output').value;
    const read = modbusOperation === 'read';
    if (!input) return;
    $('tool-operation').value = 'request';
    changeToolOperation();
    $('tool-input').value = input;
    if (read) {
      $('tool-input').focus();
      $('toolbox-run').click();
    } else {
      $('tool-operation').focus();
    }
  });
  $('toolbox-run').addEventListener('click', async () => {
    clearToolResult();
    const ticket = toolTicket;
    $('toolbox-run').disabled = true;
    try {
      const operation = $('tool-operation').value;
      const input = currentTool === 'modbus' && operation === 'read' ? '' : $('tool-input').value;
      const result = await api('RunDesktopTool', {
        kind: operation === 'cp56' ? 'time' : currentTool,
        operation,
        input,
        encoding: currentTool === 'modbus' ? $('tool-transport').value : $('tool-encoding').value,
        transaction: currentTool === 'modbus' ? $('tool-transaction').value : '',
        ...(currentTool === 'modbus' && operation === 'read'
          ? {
              read: {
                unit: $('tool-read-unit').value,
                function: $('tool-read-function').value,
                address: $('tool-read-address').value,
                quantity: $('tool-read-quantity').value,
              },
            }
          : {}),
        order: $('tool-order').value,
        zone: $('tool-zone').value,
      });
      if (ticket === toolTicket) renderToolResult(result);
    } catch (error) {
      if (ticket === toolTicket) {
        showError('toolbox-dialog', error);
      }
    } finally {
      if (ticket === toolTicket) $('toolbox-run').disabled = false;
    }
  });
  $('open-toolbox').addEventListener('click', () => {
    changeTool();
    $('toolbox-dialog').show();
    syncTimeClock();
  });
  $('toolbox-copy').addEventListener('click', async () => {
    const ticket = toolTicket;
    try {
      await api('CopyDesktopText', $('tool-output').value);
      if (ticket === toolTicket) {
        clearError('toolbox-dialog');
      }
    } catch (error) {
      if (ticket === toolTicket) showError('toolbox-dialog', error);
    }
  });
  async function poll() {
    if (!$('packet-tools-dialog').open || statusBusy || packetBusy || document.hidden) return;
    statusBusy = true;
    try {
      const ticket = packetTicket,
        channel = packetChannel;
      const view = await api('GetRawTools', channel);
      if (ticket === packetTicket && channel === packetChannel && !packetBusy)
        showPacketStatus(view);
    } catch (error) {
      if (!packetBusy) showError('packet-tools-dialog', error);
    } finally {
      statusBusy = false;
    }
  }
  setInterval(poll, 600);
  changeTool();
  return {
    initialize: async (kind) => {
      if (kind === 'point-table-dialog') await initializeTable();
      if (kind === 'packet-tools-dialog') await initializePacket();
      if (kind === 'toolbox-dialog') {
        changeTool();
        $('toolbox-dialog').show();
        syncTimeClock();
      }
    },
  };
};
