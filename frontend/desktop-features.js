// Desktop point-table and toolbox workflows.
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
    tableTarget = [];
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
  let currentTool = '',
    toolTicket = 0;
  const names = {
    time: '时间戳',
    packet: '报文解析',
    number: '数值转换',
    network: 'IPv4 网段',
    encoding: '编码转换',
    string: '文本处理',
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
    $('tool-result-pane').removeAttribute('aria-busy');
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
    $('tool-run-between').hidden = timestamp;
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
            ? `Float${bytes * 8}（${bytes} 字节 HEX）`
            : `Float${bytes * 8} 数值`;
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
      output = operation.endsWith('-tohex')
        ? '字节（HEX）'
        : operation.startsWith('hex-')
          ? '浮点数'
          : '结果';
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
      label = 'CP56Time2a（7 字节 HEX）';
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
    $('tool-result-pane').setAttribute('aria-busy', 'true');
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
      if (ticket === toolTicket) {
        renderToolResult(result);
        $('tool-output-label').scrollIntoView?.({ block: 'nearest' });
      }
    } catch (error) {
      if (ticket === toolTicket) {
        if (!$('tool-source').hidden) $('tool-input').setAttribute('aria-invalid', 'true');
        showError('toolbox-dialog', error);
        $('tool-error').scrollIntoView?.({ block: 'nearest' });
      }
    } finally {
      if (ticket === toolTicket) {
        $('toolbox-run').disabled = false;
        $('tool-result-pane').removeAttribute('aria-busy');
      }
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
  let frames = [],
    framePage = 0,
    selectedFrame = null,
    decodeTicket = 0;
  const framePageSize = 100;
  const frameDialog = 'field-tools-dialog';
  const fieldDialog = $(frameDialog);
  const framePanels = ['field-frames', 'field-check', 'field-ssh'];
  const fieldTabs = [...fieldDialog.querySelectorAll('[data-field-panel]')];
  function showFieldPanel(id) {
    for (const panel of framePanels) $(panel).hidden = panel !== id;
    for (const tab of fieldTabs) {
      const active = tab.dataset.fieldPanel === id;
      tab.classList.toggle('selected', active);
      tab.setAttribute('aria-pressed', String(active));
    }
    clearError(frameDialog);
  }
  for (const tab of fieldTabs) tab.onclick = () => showFieldPanel(tab.dataset.fieldPanel);
  $('open-field-tools').onclick = () => {
    showFieldPanel('field-ssh');
    renderFrames();
    fieldDialog.showModal();
  };
  function renderFrames() {
    const query = $('field-filter').value.trim().toLowerCase();
    const direction = $('field-direction').value;
    const matched = frames.filter((f) => {
      return (
        (!direction || f.direction === direction) &&
        [f.time, f.channelId, f.peer, f.summary, f.hex].some((text) =>
          String(text ?? '')
            .toLowerCase()
            .includes(query),
        )
      );
    });
    const pages = Math.max(1, Math.ceil(matched.length / framePageSize));
    framePage = Math.min(framePage, pages - 1);
    $('field-prev').disabled = framePage === 0;
    $('field-next').disabled = framePage >= pages - 1;
    $('field-page').textContent = matched.length
      ? `${framePage + 1} / ${pages} · ${matched.length} 条`
      : '无报文';
    const fragment = document.createDocumentFragment();
    for (const frame of matched.slice(framePage * framePageSize, (framePage + 1) * framePageSize)) {
      const tr = document.createElement('tr');
      tr.setAttribute('aria-selected', String(selectedFrame === frame));
      const time = document.createElement('td');
      tr.append(time);
      const select = document.createElement('button');
      select.type = 'button';
      select.textContent = frame.time || '查看';
      time.append(select);
      for (const value of [
        frame.channelId,
        frame.direction === 'RX' ? '接收' : '发送',
        frame.peer,
        frame.summary,
      ]) {
        const cell = document.createElement('td');
        cell.textContent = value || '—';
        tr.append(cell);
      }
      tr.onclick = () => {
        selectedFrame = frame;
        for (const row of $('field-rows').children)
          row.setAttribute('aria-selected', String(row === tr));
        decodeFrame();
      };
      fragment.append(tr);
    }
    $('field-rows').replaceChildren(fragment);
    if (selectedFrame && !matched.includes(selectedFrame)) {
      selectedFrame = null;
      decodeFrame();
    }
  }
  async function decodeFrame() {
    const ticket = ++decodeTicket;
    const protocol = $('field-protocol').value;
    $('field-transaction-wrap').hidden = protocol === 'iec104';
    $('field-frame-detail').hidden = !selectedFrame;
    $('field-hex').value = selectedFrame?.hex || '';
    $('field-decoded').textContent = selectedFrame ? '解析中…' : '';
    if (!selectedFrame) return;
    try {
      const decoded = await api('RunDesktopTool', {
        kind: protocol === 'iec104' ? 'iec104' : 'modbus',
        operation: protocol === 'iec104' ? 'decode' : $('field-transaction').value,
        encoding: protocol,
        input: selectedFrame.hex,
      });
      if (ticket === decodeTicket) {
        $('field-decoded').textContent = (decoded.fields || [])
          .map(({ name, value }) => `${name}  ${value}`)
          .join('\n');
      }
    } catch (error) {
      if (ticket === decodeTicket) $('field-decoded').textContent = error?.message || String(error);
    }
  }
  function fieldAction(id, action) {
    $(id).onclick = async () => {
      $(id).disabled = true;
      clearError(frameDialog);
      try {
        await action();
      } catch (error) {
        showError(frameDialog, error);
      } finally {
        $(id).disabled = false;
      }
    };
  }
  fieldAction('field-copy-hex', async () => {
    if (selectedFrame) await api('CopyDesktopText', selectedFrame.hex);
  });
  fieldAction('field-open-frames', async () => {
    const result = await api('OpenServerFrames');
    if (result === null) return;
    frames = result || [];
    framePage = 0;
    selectedFrame = null;
    $('field-filter').value = '';
    $('field-direction').value = '';
    renderFrames();
    decodeFrame();
  });
  $('field-filter').oninput = $('field-direction').onchange = () => {
    framePage = 0;
    renderFrames();
  };
  $('field-prev').onclick = () => {
    framePage--;
    renderFrames();
  };
  $('field-next').onclick = () => {
    framePage++;
    renderFrames();
  };
  $('field-protocol').onchange = $('field-transaction').onchange = decodeFrame;
  fieldAction('field-run-check', async () => {
    $('field-check-output').textContent = '';
    const lines = await api('CheckServerDeployment', $('field-ip').value, $('field-rules').checked);
    $('field-check-output').textContent = lines.join('\n');
  });
  const sshHost = $('field-host');
  const savedHostsKey = 'gylab-ssh-hosts';
  const lastHostKey = 'gylab-ssh-host';
  let savedHosts = [];
  try {
    const saved = JSON.parse(localStorage.getItem(savedHostsKey) || '[]');
    if (Array.isArray(saved)) savedHosts = saved.filter((host) => host?.target && host?.name);
  } catch {}
  const previousHost = localStorage.getItem(lastHostKey);
  if (
    previousHost &&
    !savedHosts.some((host) => host.target === previousHost) &&
    localStorage.getItem(savedHostsKey) === null
  ) {
    savedHosts.push({ name: previousHost, target: previousHost });
    localStorage.setItem(savedHostsKey, JSON.stringify(savedHosts));
  }
  let editingHost = '';
  function renderHostOptions(selected = sshHost.value || localStorage.getItem(lastHostKey)) {
    const entries = savedHosts.map((host) => [host.target, host.name]);
    const target = entries.some(([value]) => value === selected) ? selected : entries[0]?.[0] || '';
    sshHost.innerHTML = entries.length
      ? options(entries, target)
      : '<option value="">暂无已保存主机</option>';
    sshHost.value = target;
    $('field-run-ssh').disabled = !target || sshHost.disabled;
  }
  async function loadSSHHosts() {
    try {
      const systemHosts = (await api('ListSSHHosts')) || [];
      const entries = systemHosts.filter(
        (target) => !savedHosts.some((host) => host.target === target),
      );
      $('field-import-host').innerHTML = options(
        [['', '选择主机别名'], ...entries.map((target) => [target, target])],
        '',
      );
      $('field-import-host-wrap').hidden = !entries.length;
    } catch (error) {
      showError(frameDialog, error);
    }
  }
  function editHost(target) {
    const saved = savedHosts.find((host) => host.target === target);
    editingHost = saved?.target || '';
    $('field-host-name').value = saved?.name || target;
    $('field-host-target').value = target;
    $('field-host-remove').disabled = !saved;
  }
  $('field-manage-hosts').onclick = () => {
    const form = $('field-host-editor');
    form.hidden = !form.hidden;
    $('field-manage-hosts').setAttribute('aria-expanded', String(!form.hidden));
    if (!form.hidden) {
      editHost(sshHost.value);
      loadSSHHosts();
    }
  };
  $('field-import-host').onchange = (event) => {
    const target = event.target.value;
    if (!target) return;
    editHost('');
    $('field-host-target').value = target;
    $('field-host-name').value = target;
    $('field-host-name').focus();
  };
  $('field-host-new').onclick = () => {
    editHost('');
    $('field-host-target').focus();
  };
  function changeHostSelection() {
    $('field-ssh-output').replaceChildren();
    $('field-ssh-status').textContent = '';
    clearError(frameDialog);
    if (!$('field-host-editor').hidden) editHost(sshHost.value);
  }
  sshHost.onchange = changeHostSelection;
  $('field-host-editor').onsubmit = (event) => {
    event.preventDefault();
    const target = $('field-host-target').value.trim();
    const name = $('field-host-name').value.trim() || target;
    if (
      !/^(?:[A-Za-z0-9_][A-Za-z0-9_.-]*@)?(?:[A-Za-z0-9_][A-Za-z0-9_.-]*|\[[A-Fa-f0-9:]+\])$/.test(
        target,
      )
    ) {
      showError(frameDialog, '请输入 SSH 主机别名或 user@host');
      return;
    }
    if (savedHosts.some((host) => host.target === target && host.target !== editingHost)) {
      showError(frameDialog, '该连接目标已保存');
      return;
    }
    const index = savedHosts.findIndex((host) => host.target === editingHost);
    if (index < 0) savedHosts.push({ target, name });
    else savedHosts[index] = { target, name };
    localStorage.setItem(savedHostsKey, JSON.stringify(savedHosts));
    renderHostOptions(target);
    changeHostSelection();
    loadSSHHosts();
  };
  $('field-host-remove').onclick = () => {
    if (!editingHost) return;
    savedHosts = savedHosts.filter((host) => host.target !== editingHost);
    localStorage.setItem(savedHostsKey, JSON.stringify(savedHosts));
    if (localStorage.getItem(lastHostKey) === editingHost) localStorage.removeItem(lastHostKey);
    renderHostOptions('');
    changeHostSelection();
    loadSSHHosts();
  };
  renderHostOptions();
  const channelStatuses = {
    listening: '监听中',
    connected: '已连接',
    connecting: '连接中',
    reconnecting: '重连中',
    disconnected: '已断开',
    stopped: '已停止',
    error: '错误',
  };
  function renderSSHProjects(projects) {
    return (
      projects
        .map(({ info, devices, warnings, error }) => {
          if (error) return `<p class="field-project-warning">${escape(error)}</p>`;
          const view = info.view;
          const channels = (view.channels || []).map((c) => {
            const names = (devices?.[c.id] || []).map((d) => `${d.name}（${d.points} 点）`);
            const address = c.state.address || c.address || '—';
            const status = channelStatuses[c.state.status] || c.state.status || '未启动';
            return `<tr><th scope="row">${escape(c.name)}</th>
          <td>${escape(c.protocol)}</td><td>${escape(status)}</td><td>${escape(address)}</td>
          <td>${escape(c.state.peers ?? 0)}</td><td>${escape(names.join('、') || '—')}</td></tr>`;
          });
          const logs = (info.logs || []).map(
            (l) => `${l.time} · ${l.level} · ${l.message} ${l.fields || ''}`,
          );
          return `<section class="field-project">
        <div class="field-project-heading"><h3>${escape(view.name)}</h3><span>PID ${escape(info.pid)}</span></div>
        ${view.path ? `<div class="field-project-path">${escape(view.path)}</div>` : ''}
        ${
          channels.length
            ? `<div class="field-channel-table"><table>
          <thead><tr><th>通道</th><th>协议</th><th>状态</th><th>地址</th><th>连接</th><th>装置</th></tr></thead>
          <tbody>${channels.join('')}</tbody></table></div>`
            : '<p class="field-empty">无通道</p>'
        }
        ${(warnings || []).map((warning) => `<p class="field-project-warning">${escape(warning)}</p>`).join('')}
        ${logs.length ? `<details><summary>近期日志（${logs.length}）</summary><pre class="field-result">${escape(logs.join('\n'))}</pre></details>` : ''}
      </section>`;
        })
        .join('') || '<p class="field-empty">未发现运行工程</p>'
    );
  }
  $('field-ssh-form').onsubmit = async (event) => {
    event.preventDefault();
    const button = $('field-run-ssh');
    if (button.disabled) return;
    button.disabled = true;
    sshHost.disabled = true;
    $('field-manage-hosts').disabled = true;
    $('field-host-editor').hidden = true;
    $('field-manage-hosts').setAttribute('aria-expanded', 'false');
    clearError(frameDialog);
    const status = $('field-ssh-status');
    const output = $('field-ssh-output');
    status.textContent = '读取中…';
    output.replaceChildren();
    try {
      const target = sshHost.value.trim();
      const projects = await api('InspectServerSSH', target);
      output.innerHTML = renderSSHProjects(projects);
      localStorage.setItem(lastHostKey, target);
      status.textContent = `已读取 · ${new Date().toLocaleTimeString()}`;
    } catch (error) {
      status.textContent = '';
      showError(frameDialog, error);
    } finally {
      button.disabled = false;
      sshHost.disabled = false;
      $('field-manage-hosts').disabled = false;
    }
  };
  changeTool();
  return {
    initialize: async (kind) => {
      if (kind === 'point-table-dialog') await initializeTable();
      if (kind === 'toolbox-dialog') {
        changeTool();
        $('toolbox-dialog').show();
        syncTimeClock();
      }
    },
  };
};
