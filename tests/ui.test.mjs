import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const { Window } = await import(process.env.GYLAB_DOM_MODULE || 'happy-dom');
const html = (await readFile(new URL('../frontend/index.html', import.meta.url), 'utf8'))
  .replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '')
  .replace(/<link\b[^>]*>/g, '');
const code = await readFile(new URL('../frontend/app.js', import.meta.url), 'utf8');
const formCode = await readFile(new URL('../frontend/forms.js', import.meta.url), 'utf8');
const tick = () => new Promise((resolve) => setImmediate(resolve));
// happy-dom implements button activation through requestSubmit internally;
// retain that internal behavior while testing the API's absence in app code.
function legacySubmission(form) {
  const native = form.requestSubmit;
  form.requestSubmit = undefined;
  for (const button of form.querySelectorAll('button[type=submit]')) {
    const click = button.click.bind(button);
    button.click = () => {
      form.requestSubmit = native;
      try {
        return click();
      } finally {
        form.requestSubmit = undefined;
      }
    };
  }
}
test('older WebKit keeps project titles and message pause without Array.at or structuredClone', async () => {
  const h = await setup({
    messages: [
      {
        sequence: 1,
        channelId: 'a',
        direction: 'RX',
        hex: 'CAFE',
        summary: 'before',
        time: '12:00:00',
      },
    ],
    path: '/tmp/mac-project.json',
  });
  try {
    h.w.structuredClone = undefined;
    h.w.eval('Array.prototype.at=undefined');
    h.$('pause-messages').click();
    await tick();
    assert.equal(h.$('pause-messages').textContent, '继续');
    h.state.messages[0].summary = 'changed';
    h.state.messages.push({
      sequence: 2,
      channelId: 'a',
      direction: 'RX',
      summary: 'after',
      hex: 'BEEF',
      time: '12:00:01',
    });
    await h.refresh();
    assert.equal(h.$('project-path'), null);
    assert.equal(h.$('project-name').textContent, 'test');
    assert.match(h.$('project-name').getAttribute('aria-description'), /\/tmp\/mac-project\.json/);
    assert.match(h.$('message-rows').textContent, /before/);
    assert.doesNotMatch(h.$('message-rows').textContent, /changed/);
    h.$('pause-messages').click();
    await tick();
    assert.match(h.$('message-rows').textContent, /after/);
  } finally {
    await h.close();
  }
});
test('older WebKit Enter applies communication settings exactly once without requestSubmit', async () => {
  const h = await setup();
  try {
    h.$('edit-channel').click();
    await tick();
    legacySubmission(h.$('channel-form'));
    const port = originalField(h, 'txtClientPort', 'original-network-panel');
    port.value = '25045';
    port.dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );
    await tick();
    await tick();
    const edits = h.calls.filter((c) => c[0] === 'UpdateTransport');
    assert.equal(edits.length, 1);
    assert.equal(edits[0][0], 'UpdateTransport');
    assert.match(edits[0][1][0].address, /:25045$/);
  } finally {
    await h.close();
  }
});
test('older WebKit submission fallback retains form validation and selected confirmation choice', async () => {
  const h = await setup();
  try {
    const form = h.w.document.createElement('form');
    form.innerHTML = '<input required><button type="submit" value="save">Save</button>';
    h.w.document.body.append(form);
    legacySubmission(form);
    let submitted = 0,
      choice = '';
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      submitted++;
      choice = e.submitter?.value;
    });
    h.w.eval('submitWithButton')(form, form.querySelector('button'));
    assert.equal(submitted, 0);
    form.querySelector('input').value = 'valid';
    h.w.eval('submitWithButton')(form, form.querySelector('button'));
    assert.equal(submitted, 1);
    assert.equal(choice, 'save');
  } finally {
    await h.close();
  }
});
test('desktop automatic data opens with a fixed channel and device identity', async () => {
  const c = dataServer();
  const h = await setup({ project: { name: 'data', channels: [c] }, states: [] }, { kind: '' });
  try {
    h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
    for (const kind of ['yx', 'yc']) {
      h.w.document.querySelector('[data-point-kind="' + kind + '"]').click();
      h.$('open-auto-data').click();
      await tick();
      await tick();
      const context = h.calls.filter((c) => c[0] === 'OpenNativeWindow').at(-1)[1][0];
      assert.equal(context.kind, 'auto-data-dialog');
      assert.equal(context.channel, 'data');
      assert.equal(context.device, 'target');
      assert.equal(context.edit, true);
      assert.equal(context.pointKind, 'telemetry');
    }
  } finally {
    await h.close();
  }
});
function dataServer(protocol = 'iec104') {
  const points =
    protocol === 'iec104'
      ? [
          { kind: 'yc', address: 10, value: 0, quality: 0 },
          { kind: 'yc', address: 11, value: 0, quality: 0 },
          { kind: 'yx', address: 1, value: 0, quality: 0 },
        ]
      : [
          { kind: 'holding', address: 0, value: 0, quality: 0 },
          { kind: 'coil', address: 1, value: 0, quality: 0 },
        ];
  return {
    ...channel('data'),
    protocol,
    role: 'server',
    transport: 'tcp-server',
    devices: [{ id: 'target', name: 'data device', unit: 1, commonAddress: 1, points }],
  };
}
function dataState() {
  return {
    channel: 'data',
    device: 'target',
    status: 'stopped',
    config: { rules: [], stopOnDisconnect: true },
    cycles: [],
    updates: 0,
    lastError: '',
  };
}
async function setupData(protocol = 'iec104', native = false) {
  const c = dataServer(protocol),
    view = dataState();
  const h = await setup(
    {
      project: { name: 'data', channels: [c] },
      states: [
        {
          id: c.id,
          status: protocol === 'iec104' ? 'connected' : 'listening',
          peers: protocol === 'iec104' ? 1 : 0,
        },
      ],
    },
    native
      ? {
          kind: 'auto-data-dialog',
          role: 'server',
          channel: c.id,
          device: 'target',
          pointKind: 'yc',
        }
      : null,
    {
      GetDataTest: () => structuredClone(view),
      SaveDataTest: (channel, device, scope, config) => {
        view.config = structuredClone(config);
        view.cycles = config.rules.map(() => 0);
        return structuredClone(view);
      },
      StartDataTest: () => {
        view.status = 'running';
        return structuredClone(view);
      },
      SetDataTestState: (channel, device, scope, action) => {
        view.status = { pause: 'paused', resume: 'running', stop: 'stopped' }[action];
        return structuredClone(view);
      },
    },
  );
  if (!native) {
    h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
    await h.w.eval('initializeAutoDataWindow')({
      channel: c.id,
      device: 'target',
      pointKind: 'yc',
    });
    await tick();
    await tick();
  }
  return { ...h, view };
}
test('automatic data supports several data types and independent periods in one configuration', async () => {
  const h = await setupData();
  try {
    assert.equal(h.$('auto-data-rows').children.length, 2);
    const rows = h.$('auto-data-rows').children;
    rows[0].querySelector('[data-data-field="intervalMs"]').value = '500';
    rows[1].querySelector('[data-data-field="intervalMs"]').value = '1200';
    h.$('auto-data-save').click();
    await tick();
    await tick();
    const config = h.calls.find((c) => c[0] === 'SaveDataTest')[1][3];
    assert.deepEqual(
      Array.from(config.rules, (r) => r.kind),
      ['yc', 'yx'],
    );
    assert.deepEqual(
      Array.from(config.rules, (r) => r.intervalMs),
      [500, 1200],
    );
    assert.equal(
      h.calls.some((c) => c[0] === 'StartAutoData'),
      false,
    );
    assert.equal(h.$('auto-data-dialog').open, false);
  } finally {
    await h.close();
  }
});
test('automatic data configuration stays editable while running and only saves', async () => {
  const h = await setupData();
  try {
    h.view.status = 'running';
    h.w.eval('renderDataState')(structuredClone(h.view));
    assert.equal(h.$('auto-data-rows').querySelector('[data-data-field=min]').disabled, false);
    for (const id of ['auto-data-start', 'auto-data-pause', 'auto-data-stop'])
      assert.equal(h.$(id), null);
    h.$('auto-data-form').requestSubmit(h.$('auto-data-save'));
    await settleInline();
    assert.equal(h.$('auto-data-dialog').open, false);
    assert.ok(h.calls.some((c) => c[0] === 'SaveDataTest'));
    assert.equal(
      h.calls.some((c) => ['StartAutoData', 'SetAutoDataState'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});

test('automatic data can add remove and disable groups while rejecting blank numeric input', async () => {
  const h = await setupData();
  try {
    h.$('auto-data-add').click();
    assert.equal(h.$('auto-data-rows').children.length, 3);
    h.$('auto-data-rows').lastElementChild.querySelector('[data-data-remove]').click();
    assert.equal(h.$('auto-data-rows').children.length, 2);
    const input = h.$('auto-data-rows').querySelector('[data-data-field="min"]');
    input.value = '';
    h.$('auto-data-save').click();
    await tick();
    await tick();
    assert.equal(
      h.calls.some((c) => c[0] === 'SaveDataTest'),
      false,
    );
    assert.equal(h.$('auto-data-form').querySelector('.form-error').hidden, false);
    input.value = '12.25';
    const mode = h.$('auto-data-rows').querySelector('[data-data-field="mode"]');
    mode.value = 'fixed';
    mode.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.$('auto-data-rows').querySelector('[data-data-field="max"]').disabled, true);
    h.$('auto-data-save').click();
    await tick();
    await tick();
    const saved = h.calls.find((c) => c[0] === 'SaveDataTest')[1][3];
    assert.equal(saved.rules[0].min, 12.25);
    assert.equal(saved.rules[0].max, 12.25);
  } finally {
    await h.close();
  }
});
test('one device configuration and toggle operate mixed telemetry rules across both tabs', async () => {
  for (const protocol of ['iec104', 'modbus-tcp']) {
    const c = dataServer(protocol),
      view = dataState();
    if (protocol !== 'iec104')
      c.devices[0].points.push({ kind: 'discrete', tab: 'yx', address: 10, value: 0, quality: 0 });
    const overrides = {
      GetDataTest: (ch, d, scope) => {
        assert.equal(scope, 'telemetry');
        return structuredClone(view);
      },
      SaveDataTest: (ch, d, scope, cfg) => {
        view.config = structuredClone(cfg);
        return structuredClone(view);
      },
      StartDataTest: () => {
        view.status = 'running';
        return structuredClone(view);
      },
      SetDataTestState: (ch, d, scope, action) => {
        assert.equal(action, 'stop');
        view.status = 'stopped';
        return structuredClone(view);
      },
    };
    const h = await setup(
      {
        project: { name: 'unified', channels: [c] },
        states: [{ id: c.id, status: protocol === 'iec104' ? 'connected' : 'listening', peers: 1 }],
      },
      null,
      overrides,
    );
    try {
      h.w.document.querySelector('[data-mode=server]').click();
      h.$('open-auto-data').click();
      await settleInline();
      assert.equal(h.$('open-report-data'), null);
      assert.equal(h.$('runtime-data-stop'), null);
      assert.equal(h.$('auto-data-start'), null);
      const rows = [...h.$('auto-data-rows').children];
      assert.equal(rows.length, 2);
      rows.forEach(
        (row, i) =>
          (row.querySelector('[data-data-field=intervalMs]').value = String(400 + i * 400)),
      );
      h.$('auto-data-save').click();
      await settleInline();
      assert.equal(view.config.rules.length, 2);
      assert.equal(view.status, 'stopped');
      h.$('runtime-data-run').click();
      await settleInline();
      assert.equal(view.status, 'running');
      assert.equal(h.$('runtime-data-run').getAttribute('aria-pressed'), 'true');
      h.w.document.querySelector('[data-point-kind=yc]').click();
      await h.refresh();
      assert.match(h.$('runtime-data-run').textContent, /关闭/);
      h.$('open-auto-data').click();
      await settleInline();
      assert.equal(h.$('auto-data-rows').children.length, 2);
      h.$('auto-data-dialog').close();
      h.$('runtime-data-run').click();
      await settleInline();
      assert.equal(view.status, 'stopped');
    } finally {
      await h.close();
    }
  }
});
test('legacy native telemetry launch contexts open the same combined editor', async () => {
  for (const category of ['yx', 'yc']) {
    const c = dataServer();
    const h = await setup(
      { project: { name: 'native', channels: [c] }, states: [] },
      {
        kind: 'auto-data-dialog',
        role: 'server',
        channel: c.id,
        device: 'target',
        pointKind: 'tab:' + category,
        point: 'report',
      },
      {
        GetDataTest: (ch, d, scope) => {
          assert.equal(scope, 'telemetry');
          return dataState();
        },
      },
    );
    try {
      await settleInline();
      assert.equal(h.$('auto-data-dialog').open, true);
      assert.equal(h.$('auto-data-rows').children.length, 2);
      assert.match(h.$('auto-data-dialog').querySelector('h2').textContent, /自动上报配置/);
      assert.equal(h.$('auto-data-start'), null);
    } finally {
      await h.close();
    }
  }
});
test('newly added devices expose one automatic reporting editor for all telemetry points', async () => {
  const c = {
      ...channel('new'),
      protocol: 'raw',
      role: 'server',
      transport: 'tcp-server',
      devices: [],
    },
    view = dataState();
  const h = await setup({ project: { name: 'new-device', channels: [c] }, states: [] }, null, {
    GetDataTest: () => structuredClone(view),
    SaveDataTest: (ch, d, scope, cfg) => {
      assert.equal(d, 'added');
      assert.equal(scope, 'telemetry');
      view.config = cfg;
      return structuredClone(view);
    },
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    await settleInline();
    h.overrides.UpsertDevice = (cid, protocol, device) => {
      c.protocol = protocol;
      h.state.project.channels[0].protocol = protocol;
      h.state.project.channels[0].devices = [
        {
          ...device,
          id: 'added',
          points: device.configuration.ranges.flatMap((r) =>
            Array.from({ length: r.count }, (_, i) => ({
              kind: r.kind,
              tab: r.tab,
              address: r.address + i,
              value: 0,
              quality: 0,
            })),
          ),
        },
      ];
      return 'added';
    };
    h.$('add-device').click();
    assert.equal(originalField(h, 'cbYC_Type').value, '带品质短浮点(type:13)');
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await settleInline();
    assert.equal(
      h.calls.find((c) => c[0] === 'UpsertDevice')[1][2].configuration.values.cbYC_Type,
      '带品质短浮点(type:13)',
    );
    assert.equal(h.$('device-count').textContent, '1');
    assert.equal(h.$('open-report-data'), null);
    h.$('open-auto-data').click();
    await settleInline();
    const rows = [...h.$('auto-data-rows').children];
    assert.deepEqual(
      rows.map((r) => r.querySelector('[data-data-field=kind]').value),
      ['yx', 'yc'],
    );
    assert.deepEqual(
      rows.map((r) => r.querySelector('[data-data-field=points]').value),
      ['1-50', '16385-16434'],
    );
    const yc = rows.find((r) => r.querySelector('[data-data-field=kind]').value === 'yc');
    assert.equal(yc.querySelector('[data-data-field=max]').value, '100');
    yc.querySelector('[data-data-field=min]').value = '100';
    yc.querySelector('[data-data-field=max]').value = '220.5';
    yc.querySelector('[data-data-field=step]').value = '0.5';
    h.$('auto-data-save').click();
    await settleInline();
    assert.equal(view.config.rules.length, 2);
    assert.equal(view.config.rules.find((r) => r.kind === 'yc').max, 220.5);
  } finally {
    await h.close();
  }
});
test('combined editor retains report-only rules and disables irrelevant numeric inputs', async () => {
  const c = dataServer(),
    view = dataState();
  view.config.rules = [
    { enabled: true, kind: 'yc', points: '10-11', mode: 'report', cause: 2, intervalMs: 300 },
  ];
  const h = await setup(
    { project: { name: 'migrated', channels: [c] }, states: [] },
    { kind: 'auto-data-dialog', role: 'server', channel: c.id, device: 'target' },
    { GetDataTest: () => view },
  );
  try {
    await settleInline();
    const row = h.$('auto-data-rows').firstElementChild;
    assert.equal(row.querySelector('[data-data-field=mode]').value, 'report:2');
    assert.equal(row.querySelector('[data-data-field=min]').disabled, true);
    assert.equal(row.querySelector('[data-data-field=max]').disabled, true);
    assert.equal(row.querySelector('[data-data-field=step]').disabled, true);
    assert.equal(h.$('auto-data-save').disabled, false);
  } finally {
    await h.close();
  }
});
test('Modbus automatic data explains polling and offers registers and switches', async () => {
  const h = await setupData('modbus-tcp');
  try {
    assert.equal(h.$('auto-data-disconnect-label').hidden, true);
    assert.match(h.$('auto-data-note').textContent, /轮询/);
    assert.deepEqual(
      [...h.$('auto-data-rows').querySelector('[data-data-field="kind"]').options].map(
        (o) => o.value,
      ),
      ['discrete', 'holding', 'input'],
    );
  } finally {
    await h.close();
  }
});
test('automatic data opens a separate native window and closing it never stops its job', async () => {
  const h = await setupData('iec104', true);
  try {
    assert.equal(h.$('auto-data-dialog').classList.contains('native-content'), true);
    assert.equal(h.$('auto-data-dialog').open, true);
    h.$('auto-data-dialog').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    await tick();
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
    assert.equal(
      h.calls.some((c) => c[0] === 'SetAutoDataState'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('communication IP and port remain editable while connected and apply the edited endpoint', async () => {
  const h = await setup();
  try {
    assert.equal(h.$('edit-channel').disabled, false);
    h.$('edit-channel').click();
    const ip = originalField(h, 'txtClientIP', 'original-network-panel'),
      port = originalField(h, 'txtClientPort', 'original-network-panel');
    assert.equal(ip.disabled, false);
    assert.equal(port.disabled, false);
    ip.value = '192.0.2.55';
    port.value = '15020';
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'UpdateTransport')[1][0].address, '192.0.2.55:15020');
    assert.equal(h.$('channel-dialog').open, false);
  } finally {
    await h.close();
  }
});
test('communication groups follow transport and failed validation keeps edited fields for correction', async () => {
  const h = await setup();
  try {
    h.$('edit-channel').click();
    const panel = h.$('original-network-panel'),
      get = (id) => originalField(h, id, 'original-network-panel');
    assert.equal(panel.querySelector('[data-group-id="groupBoxEx2"]').hidden, true);
    assert.equal(panel.querySelector('[data-group-id="groupBoxEx3"]').hidden, false);
    get('txtClientIP').value = '192.0.2.66';
    get('txtClientPort').value = '16001';
    h.overrides.UpdateTransport = () => {
      throw Error('端口无效');
    };
    h.$('channel-form').requestSubmit(panel.querySelector('button[type=submit]'));
    await tick();
    await tick();
    await tick();
    assert.equal(h.$('channel-dialog').open, true);
    assert.equal(get('txtClientIP').value, '192.0.2.66');
    assert.equal(get('txtClientPort').value, '16001');
    assert.equal(get('txtClientIP').disabled, false);
    assert.match(h.$('channel-form').querySelector('.form-error').textContent, /端口无效/);
  } finally {
    await h.close();
  }
});
test('UDP allows editing its local IP as well as its local and remote ports', async () => {
  const c = { ...channel('a'), protocol: 'raw', devices: [] };
  const h = await setup({ project: { name: 'udp local', channels: [c] }, states: [] });
  try {
    h.$('edit-channel').click();
    const get = (id) => originalField(h, id, 'original-network-panel');
    get('rbUDP').checked = true;
    get('rbUDP').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(get('cbxServerIP').disabled, false);
    get('cbxServerIP').value = '127.0.0.2';
    get('txtServerPort').value = '19009';
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    assert.equal(
      h.calls.find((c) => c[0] === 'UpdateTransport')[1][0].localAddress,
      '127.0.0.2:19009',
    );
  } finally {
    await h.close();
  }
});
test('UDP exposes remote and local ports and disables TCP local-assignment options', async () => {
  const c = { ...channel('a'), protocol: 'raw', devices: [] };
  const h = await setup({ project: { name: 'udp', channels: [c] }, states: [] });
  try {
    h.$('edit-channel').click();
    const get = (id) => originalField(h, id, 'original-network-panel');
    get('rbUDP').checked = true;
    get('rbUDP').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(get('txtServerPort').disabled, false);
    assert.equal(get('txtClientPort').disabled, false);
    assert.equal(get('ck_AssignIpPort').disabled, true);
    get('txtClientIP').value = '127.0.0.1';
    get('txtClientPort').value = '19101';
    get('txtServerPort').value = '19102';
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    const config = h.calls.find((c) => c[0] === 'UpdateTransport')[1][0];
    assert.equal(config.transport, 'udp');
    assert.equal(config.address, '127.0.0.1:19101');
    assert.equal(config.localAddress, '0.0.0.0:19102');
  } finally {
    await h.close();
  }
});
test('communication keeps inactive endpoints and accepts Any when changing network roles', async () => {
  const c = channel('a');
  c.transportDraft = {
    txtClientIP: '192.0.2.10',
    txtClientPort: '1502',
    cbxServerIP: 'Any',
    txtServerPort: '1503',
    cbComList: 'COM18',
  };
  const h = await setup({ project: { name: 'drafts', channels: [c] }, states: [] });
  try {
    h.$('edit-channel').click();
    const get = (id) => originalField(h, id, 'original-network-panel');
    get('rbServer').checked = true;
    get('rbServer').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(get('txtServerPort').value, '1503');
    assert.equal(get('cbComList').value, 'COM18');
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    const config = h.calls.find((c) => c[0] === 'UpdateTransport')[1][0];
    assert.equal(config.address, '0.0.0.0:1503');
    assert.equal(config.transportDraft.txtClientIP, c.address.split(':')[0]);
    assert.equal(config.transportDraft.cbComList, 'COM18');
  } finally {
    await h.close();
  }
});
test('IP and serial port candidates remain editable and refresh leaves typed values alone', async () => {
  const h = await setup({ states: [] }, null, {
    LocalIPs: () => ['Any', '127.0.0.1', '192.0.2.1'],
    SerialPorts: () => ['COM4', 'COM5'],
  });
  try {
    h.$('edit-channel').click();
    await tick();
    await tick();
    const input = originalField(h, 'cbxServerIP', 'original-network-panel');
    assert.equal(input.tagName, 'INPUT');
    assert.equal(h.$('choices-cbxServerIP').children.length, 3);
    const port = originalField(h, 'cbComList', 'original-network-panel');
    port.value = 'COM99';
    h.$('network-ports').click();
    await tick();
    await tick();
    assert.equal(port.value, 'COM99');
  } finally {
    await h.close();
  }
});
test('device protocol and version switches preserve edited ranges', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    const input = originalField(h, 'txtYX_Add');
    input.value = '123';
    input.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    chooseDeviceProtocol(h, 'iec104');
    chooseDeviceProtocol(h, 'modbus', '标准版(RTU)');
    assert.equal(originalField(h, 'txtYX_Add').value, '123');
    h.$('device-version').value = 'TCP版';
    h.$('device-version').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(originalField(h, 'txtYX_Add').value, '123');
  } finally {
    await h.close();
  }
});
test('device Enter submits once and communication Enter applies once', async () => {
  const h = await setup({ states: [] });
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    originalField(h, 'txtAdd').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );
    await settleInline();
    assert.equal(h.$('device-dialog').open, false);
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertDevice').length, 1);
    h.$('edit-channel').click();
    originalField(h, 'txtClientPort', 'original-network-panel').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );
    await tick();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'UpdateTransport').length, 1);
  } finally {
    await h.close();
  }
});
test('communication Escape discards edits and applies to current selection as the original does', async () => {
  const h = await setup({ states: [] });
  try {
    h.$('edit-channel').click();
    originalField(h, 'txtClientPort', 'original-network-panel').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    await tick();
    assert.equal(
      h.calls.some((c) => c[0] === 'UpdateTransport'),
      false,
    );
    h.$('edit-channel').click();
    h.w.document.querySelector('[data-select="b"]').click();
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'UpdateTransport')[1][0].id, 'b');
  } finally {
    await h.close();
  }
});

test('communication editor saves custom listener IP and port without hidden form validation', async () => {
  const h = await setup({ states: [{ id: 'a', status: 'stopped' }] });
  try {
    h.$('edit-channel').click();
    const get = (id) => originalField(h, id, 'original-network-panel');
    get('rbServer').checked = true;
    get('rbServer').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(get('txtClientIP').disabled, true);
    assert.equal(get('cbxServerIP').disabled, false);
    get('cbxServerIP').value = ' 0.0.0.0 ';
    get('txtServerPort').value = '15020';
    h.$('channel-form').elements.namedItem('address').value = '';
    assert.equal(h.$('channel-form').checkValidity(), true);
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    const config = h.calls.find((c) => c[0] === 'UpdateTransport')[1][0];
    assert.equal(config.address, '0.0.0.0:15020');
    assert.equal(config.transport, 'tcp-server');
    assert.equal(config.localAddress, '');
  } finally {
    await h.close();
  }
});

test('communication editor preserves custom IPv6 remote endpoint and local binding', async () => {
  const h = await setup({ states: [{ id: 'a', status: 'stopped' }] });
  try {
    h.$('edit-channel').click();
    const get = (id) => originalField(h, id, 'original-network-panel');
    get('txtClientIP').value = ' [::1] ';
    get('txtClientPort').value = '12404';
    get('ck_AssignIpPort').checked = true;
    get('ck_AssignIpPort').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    get('cb_ClientAssignIp').value = '::1';
    get('txt_ClientAssignPort').value = '0';
    assert.equal(get('cb_ClientAssignIp').disabled, false);
    assert.equal(h.$('channel-form').checkValidity(), true);
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    const config = h.calls.find((c) => c[0] === 'UpdateTransport')[1][0];
    assert.equal(config.address, '[::1]:12404');
    assert.equal(config.localAddress, '[::1]:0');
  } finally {
    await h.close();
  }
});

test('workspace displays and selects the default channel returned after mode initialization', async () => {
  const h = await setup({ project: { name: 'empty', channels: [] }, states: [] }, null, {
    SetWorkspaceMode: async () => null,
  });
  try {
    let ready;
    h.overrides.SetWorkspaceMode = () =>
      new Promise((resolve) => {
        ready = () => {
          h.state.project.channels.push({ ...channel('default'), role: 'server', devices: [] });
          resolve();
        };
      });
    h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
    await tick();
    ready();
    await tick();
    await tick();
    assert.ok(h.w.document.querySelector('[data-select="default"].selected'));
    assert.equal(h.$('channel-count').textContent, '1');
    assert.equal(h.calls.filter((c) => c[0] === 'StartChannel').length, 0);
  } finally {
    await h.close();
  }
});
function channel(id, points = []) {
  return {
    id,
    name: id,
    protocol: 'modbus-tcp',
    role: 'client',
    transport: 'tcp-client',
    address: '127.0.0.1:502',
    unit: 1,
    commonAddress: 1,
    timeoutMs: 3000,
    baud: 9600,
    dataBits: 8,
    parity: 'N',
    stopBits: 1,
    localAddress: '',
    points,
  };
}
async function setup(config = {}, nativeContext = null, initialOverrides = {}) {
  const w = new Window({
    url: 'http://unit.test',
    settings: { disableCSSFileLoading: true, disableJavaScriptFileLoading: true },
  });
  w.structuredClone = structuredClone;
  w.setInterval = () => 0;
  w.setTimeout = () => 0;
  w.fetch = () => {
    throw Error('Network forbidden in unit tests');
  };
  w.document.write(html);
  const state = {
    project: { name: 'test', channels: [channel('a'), channel('b')] },
    states: [
      { id: 'a', status: 'connected', peers: 1 },
      { id: 'b', status: 'connected', peers: 1 },
    ],
    messages: [],
    path: '',
    dirty: false,
    ...config,
  };
  const calls = [];
  const overrides = { ...initialOverrides };
  const invoke = async (name, args) => {
    calls.push([name, args]);
    if (overrides[name]) return overrides[name](...args);
    if (name === 'GetNativeContext') return structuredClone(nativeContext);
    if (name === 'CallNative') return invoke(args[0], args[1]);
    if (name === 'GetSnapshot') return structuredClone(state);
    if (name === 'OpenProject') return '';
    return null;
  };
  w.go = {
    main: {
      App: new Proxy(
        {},
        {
          get:
            (_, name) =>
            (...args) =>
              invoke(name, args),
        },
      ),
    },
  };
  w.eval(formCode);
  w.eval(code);
  await tick();
  await w.eval('refresh()');
  return {
    w,
    state,
    calls,
    overrides,
    $: (id) => w.document.getElementById(id),
    refresh: async () => {
      await w.eval('refresh(true)');
      await tick();
    },
    close: async () => {
      // Finish queued mock API responses before destroying their DOM.
      await tick();
      await w.happyDOM.close();
      assert.equal(w.closed, true);
    },
  };
}
test('polling preserves channel element focus and prevents a duplicate pending connection', async () => {
  const h = await setup();
  try {
    const button = h.w.document.querySelector('[data-select="a"]');
    button.focus();
    await h.refresh();
    assert.equal(h.w.document.activeElement, button);
    h.state.states[0].status = 'stopped';
    h.state.states[0].peers = 0;
    await h.refresh();
    let done;
    h.overrides.StartChannel = () =>
      new Promise((resolve) => {
        done = resolve;
      });
    h.$('toggle-channel').click();
    await tick();
    assert.equal(h.$('toggle-channel').disabled, true);
    await h.refresh();
    assert.equal(h.$('toggle-channel').disabled, true);
    h.$('toggle-channel').click();
    assert.equal(h.calls.filter((c) => c[0] === 'StartChannel').length, 1);
    done();
    await tick();
    await tick();
    assert.equal(h.$('toggle-channel').disabled, false);
  } finally {
    await h.close();
  }
});
test('channel-specific request drafts survive switching', async () => {
  const h = await setup();
  try {
    h.$('read-start').value = '123，125-127';
    h.w.document.querySelector('[data-select="b"]').click();
    await tick();
    h.$('read-start').value = '8';
    h.w.document.querySelector('[data-select="a"]').click();
    assert.equal(h.$('read-start').value, '123，125-127');
    assert.equal(h.$('read-count'), null);
  } finally {
    await h.close();
  }
});
test('point editor submits only modified fields against its original identity', async () => {
  const h = await setup({
    project: {
      name: 'test',
      channels: [
        channel('a', [{ address: 1, kind: 'holding', name: 'before', value: 5, quality: 0 }]),
      ],
    },
  });
  try {
    openPointDetails(h);
    h.$('point-form').elements.namedItem('name').value = 'after';
    h.$('point-form')
      .elements.namedItem('name')
      .dispatchEvent(new h.w.Event('input', { bubbles: true }));
    h.state.project.channels[0].points[0].value = 77;
    await h.refresh();
    h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));
    await tick();
    await tick();
    const call = h.calls.find((c) => c[0] === 'PatchDevicePoint');
    assert.ok(call);
    assert.deepEqual(JSON.parse(JSON.stringify(call[1][2])), { kind: 'holding', address: 1 });
    assert.deepEqual([...call[1][4]], ['name']);
    assert.equal(call[1][0], 'a');
    assert.equal(call[1][1], 'device-1');
  } finally {
    await h.close();
  }
});
test('point pages and empty search results are accurate', async () => {
  const points = Array.from({ length: 250 }, (_, i) => ({
    address: i,
    kind: 'holding',
    name: `p${i}`,
    value: i,
    quality: 0,
  }));
  const h = await setup({ project: { name: 'test', channels: [channel('a', points)] } });
  try {
    assert.equal(h.$('point-rows').children.length, 100);
    h.$('point-next').click();
    assert.equal(h.$('point-page').textContent, '2 / 3');
    h.$('point-search').value = 'no-match';
    h.$('point-search').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    assert.equal(h.$('point-empty').hidden, false);
    assert.equal(h.$('point-empty').textContent, '无匹配结果');
  } finally {
    await h.close();
  }
});
test('cancelled file selection never sends a stop request', async () => {
  const h = await setup();
  try {
    h.$('open-project').click();
    await tick();
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'ok'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'OpenProject').length, 1);
    assert.equal(h.calls.filter((c) => c[0] === 'StopAll').length, 0);
    assert.equal(h.state.states[0].status, 'connected');
  } finally {
    await h.close();
  }
});
test('new messages append without rebuilding existing rows; pause freezes the view', async () => {
  const message = (n) => ({
    sequence: n,
    time: '12:00:00.000',
    channelId: 'a',
    direction: 'RX',
    summary: 'test',
    hex: '0102',
  });
  const h = await setup({ messages: [message(1)] });
  try {
    const first = h.$('message-rows').firstElementChild;
    h.state.messages.push(message(2));
    await h.refresh();
    assert.equal(h.$('message-rows').firstElementChild, first);
    assert.equal(h.$('message-rows').children.length, 2);
    h.$('pause-messages').click();
    h.state.messages.push(message(3));
    await h.refresh();
    assert.equal(h.$('message-rows').children.length, 2);
    h.$('pause-messages').click();
    assert.equal(h.$('message-rows').children.length, 3);
  } finally {
    await h.close();
  }
});
test('connection addresses are visible in traffic and debug logs and searchable without opening filters', async () => {
  const h = await setup({
    messages: [
      {
        sequence: 1,
        time: '12:00',
        channelId: 'a',
        direction: 'SYS',
        summary: '传输连接已建立',
        peer: '10.20.12.59:2426',
        hex: '',
      },
      {
        sequence: 2,
        time: '12:01',
        channelId: 'a',
        direction: 'SYS',
        summary: '连接已断开',
        peer: '[::1]:2404',
        hex: '',
      },
      {
        sequence: 3,
        time: '12:02',
        channelId: 'a',
        direction: 'ERR',
        summary: '连接失败 127.0.0.1:2404',
        peer: '127.0.0.1:2404',
        hex: '',
      },
      {
        sequence: 4,
        time: '12:03',
        channelId: 'a',
        direction: 'SYS',
        summary: '通道已停止',
        peer: '',
        hex: '',
      },
    ],
  });
  try {
    assert.equal(h.$('message-options').hidden, false);
    h.$('toggle-debug').click();
    for (const id of ['message-rows', 'debug-content']) {
      const text = h.$(id).textContent;
      assert.match(text, /传输连接已建立 · 10\.20\.12\.59:2426/);
      assert.match(text, /连接已断开 · \[::1\]:2404/);
      assert.equal(text.split('127.0.0.1:2404').length - 1, 1);
      assert.doesNotMatch(text, /通道已停止 ·/);
    }
    h.$('message-search').value = '10.20.12.59:2426';
    h.$('message-search').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    assert.equal(h.$('message-rows').children.length, 1);
    assert.match(h.$('message-rows').textContent, /传输连接已建立/);
    await h.refresh();
    assert.equal(h.$('message-options').hidden, false);
  } finally {
    await h.close();
  }
});

test('live point updates preserve the focused edit action', async () => {
  const h = await setup({
    project: {
      name: 'test',
      channels: [channel('a', [{ address: 1, kind: 'holding', name: 'p', value: 1, quality: 0 }])],
    },
  });
  try {
    const button = h.w.document.querySelector('[data-point-field="name"]');
    button.focus();
    h.state.project.channels[0].points[0].value = 88;
    await h.refresh();
    assert.equal(h.w.document.activeElement, button);
    assert.equal(h.w.document.querySelector('.point-value-input').value, '88');
  } finally {
    await h.close();
  }
});
test('cancelled save-before-open keeps the current session and never opens another file', async () => {
  const h = await setup({ dirty: true });
  try {
    h.overrides.SaveProject = () => '';
    h.$('open-project').click();
    await tick();
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'save'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'SaveProject').length, 1);
    assert.equal(h.calls.filter((c) => c[0] === 'OpenProject' || c[0] === 'StopAll').length, 0);
  } finally {
    await h.close();
  }
});

test('station modes isolate channel lists and channel creation follows the selected station', async () => {
  const server = { ...channel('s'), role: 'server', transport: 'tcp-server' };
  const h = await setup({ project: { name: 'test', channels: [channel('a'), server] } });
  try {
    assert.ok(h.w.document.querySelector('[data-select="a"]'));
    assert.equal(h.w.document.querySelector('[data-select="s"]'), null);
    h.w.document.querySelector('[data-mode="server"]').click();
    assert.ok(h.w.document.querySelector('[data-select="s"]'));
    assert.equal(h.w.document.querySelector('[data-select="a"]'), null);
    h.$('add-channel').click();
    await settleInline();
    const config = h.calls.find((c) => c[0] === 'UpsertChannel')[1][0];
    assert.equal(config.role, 'server');
    assert.equal(config.protocol, 'raw');
    assert.equal(config.transport, 'tcp-server');
    assert.equal(h.calls.filter((c) => c[0] === 'StartChannel').length, 0);
  } finally {
    await h.close();
  }
});

test('point cells focus on one click and double-click never opens a popup; no point-add control remains', async () => {
  const points = [{ address: 1, kind: 'holding', name: 'p', value: 5, quality: 0 }];
  const h = await setup({ project: { name: 'test', channels: [channel('a', points)] } });
  try {
    const cell = h.$('point-rows').querySelector('td');
    cell.click();
    assert.equal(h.w.document.activeElement, cell.querySelector('input'));
    cell.dispatchEvent(new h.w.MouseEvent('dblclick', { bubbles: true }));
    assert.equal(h.$('point-dialog').open, false);
    assert.equal(h.$('add-point'), null);
  } finally {
    await h.close();
  }
});

test('Space pause is channel-scoped and X clears only current channel outside editors', async () => {
  const m = (n, id) => ({
    sequence: n,
    time: '12:00',
    channelId: id,
    direction: 'RX',
    summary: 'test',
    hex: '0102',
  });
  const h = await setup({ messages: [m(1, 'a'), m(2, 'b')] });
  try {
    h.w.document.body.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
    assert.equal(h.$('pause-messages').textContent, '继续');
    h.w.document.querySelector('[data-select="b"]').click();
    assert.equal(h.$('pause-messages').textContent, '暂停');
    h.w.document.querySelector('[data-select="a"]').click();
    assert.equal(h.$('pause-messages').textContent, '继续');
    h.$('message-search').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'x', bubbles: true }),
    );
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'ClearChannelMessages').length, 0);
    h.overrides.ClearChannelMessages = (id) => {
      h.state.messages = h.state.messages.filter((m) => m.channelId !== id);
    };
    h.w.document.body.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'x', bubbles: true }));
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'ClearChannelMessages')[1][0], 'a');
    assert.equal(h.state.messages[0].channelId, 'b');
    assert.equal(h.$('message-rows').children.length, 0);
  } finally {
    await h.close();
  }
});

test('message export uses the current channel and visible filters', async () => {
  const m = (n, id, direction) => ({
    sequence: n,
    time: '12:00',
    channelId: id,
    direction,
    summary: 'test',
    hex: '0102',
  });
  const h = await setup({ messages: [m(1, 'a', 'RX'), m(2, 'a', 'TX'), m(3, 'b', 'RX')] });
  try {
    h.$('direction-filter').value = 'RX';
    h.$('direction-filter').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    h.$('export-messages').click();
    await tick();
    assert.deepEqual(
      [...h.calls.find((c) => c[0] === 'ExportMessageRows')[1][0]].map((m) => m.sequence),
      [1],
    );
    h.w.document
      .querySelector('[data-select="a"]')
      .dispatchEvent(
        new h.w.MouseEvent('contextmenu', { bubbles: true, clientX: 20, clientY: 30 }),
      );
    assert.equal(h.$('context-menu').hidden, false);
    assert.deepEqual(
      [...h.$('context-menu').children].map((b) => b.textContent),
      ['重命名', '复制', '删除'],
    );
  } finally {
    await h.close();
  }
});

function multiChannel() {
  return {
    ...channel('a'),
    devices: [
      {
        id: 'one',
        name: '装置一',
        unit: 1,
        commonAddress: 1,
        points: [{ address: 0, kind: 'holding', value: 111, quality: 0, name: 'first' }],
      },
      {
        id: 'two',
        name: '装置二',
        unit: 2,
        commonAddress: 2,
        points: [{ address: 0, kind: 'holding', value: 222, quality: 0, name: 'second' }],
      },
    ],
  };
}
test('selecting devices switches independent point tables and request drafts without changing channel messages', async () => {
  const h = await setup({
    project: { name: 'multi', channels: [multiChannel(), channel('b')] },
    messages: [
      {
        sequence: 1,
        time: '12:00',
        channelId: 'a',
        direction: 'RX',
        summary: 'shared',
        hex: '0102',
      },
    ],
  });
  try {
    assert.equal(h.$('device-count').textContent, '2');
    assert.equal(h.$('selected-device-name').textContent, '装置一');
    assert.equal(h.w.document.querySelector('.point-value-input').value, '111');
    h.$('read-start').value = '17';
    h.w.document.querySelector('[data-device="two"]').click();
    assert.equal(h.$('selected-device-name').textContent, '装置二');
    assert.equal(h.w.document.querySelector('.point-value-input').value, '222');
    assert.equal(h.$('message-rows').children.length, 1);
    h.$('read-start').value = '28';
    h.$('read-modbus').click();
    await tick();
    assert.deepEqual([...h.calls.find((c) => c[0] === 'ReadDeviceModbusPoints')[1]].slice(0, 4), [
      'a',
      'two',
      3,
      '28',
    ]);
    h.w.document.querySelector('[data-device="one"]').click();
    assert.equal(h.$('read-start').value, '17');
    h.w.document.querySelector('[data-device="two"]').click();
    assert.equal(h.$('read-start').value, '28');
    h.w.document.querySelector('[data-select="b"]').click();
    h.w.document.querySelector('[data-select="a"]').click();
    assert.equal(h.$('selected-device-name').textContent, '装置二');
  } finally {
    await h.close();
  }
});
function chooseDeviceProtocol(h, group, version) {
  h.$('device-protocol').value = group;
  h.$('device-protocol').dispatchEvent(new h.w.Event('change', { bubbles: true }));
  if (version) {
    h.$('device-version').value = version;
    h.$('device-version').dispatchEvent(new h.w.Event('change', { bubbles: true }));
  }
}
function originalField(h, id, host = 'original-device-panel') {
  return h.$(host).querySelector('[data-original-id="' + id + '"]');
}
test('adding devices closes on success and retains the incremented address for reopening', async () => {
  const h = await setup({ project: { name: 'multi', channels: [multiChannel()] } });
  try {
    h.overrides.UpsertDevice = (id, protocol, d) => {
      h.state.project.channels[0].devices.push({ ...d, id: 'three' });
      return 'three';
    };
    h.$('add-device').click();
    assert.equal(h.$('device-protocol').value, 'iec104');
    assert.equal(h.$('device-version').value, '标准版');
    assert.ok(originalField(h, 'txtYC_Num'));
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    assert.equal(originalField(h, 'txtAdd').value, '1');
    originalField(h, 'txtAdd').value = '3';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    await tick();
    assert.equal(h.$('device-count').textContent, '3');
    assert.equal(h.$('selected-device-name').textContent, '装置一');
    assert.equal(h.$('device-dialog').open, false);
    assert.equal(originalField(h, 'txtAdd').value, '4');
    const payload = h.calls.find((c) => c[0] === 'UpsertDevice')[1][2];
    assert.equal(payload.configuration.ranges[0].address, 1);
    assert.equal(payload.configuration.ranges[0].count, 50);
    assert.equal(payload.configuration.ranges[1].address, 1024);
    assert.equal(
      h.calls.filter((c) => c[0] === 'StartChannel' || c[0] === 'UpsertChannel').length,
      0,
    );
    h.w.document.querySelector('[data-close="device-dialog"]').click();
    h.$('add-device').click();
    assert.equal(h.$('device-protocol').value, 'modbus');
    assert.equal(originalField(h, 'txtAdd').value, '4');
  } finally {
    await h.close();
  }
});

test('device context deletion targets the selected device and returns to first remaining device', async () => {
  const h = await setup({ project: { name: 'multi', channels: [multiChannel()] } });
  try {
    h.overrides.DeleteDevice = (id, did) => {
      h.state.project.channels[0].devices = h.state.project.channels[0].devices.filter(
        (d) => d.id !== did,
      );
    };
    h.w.document
      .querySelector('[data-device="two"]')
      .dispatchEvent(
        new h.w.MouseEvent('contextmenu', { bubbles: true, clientX: 20, clientY: 20 }),
      );
    const remove = [...h.$('context-menu').children].find((b) => b.textContent === '删除');
    remove.click();
    await tick();
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'ok'),
    );
    await tick();
    await tick();
    assert.deepEqual([...h.calls.find((c) => c[0] === 'DeleteDevice')[1]], ['a', 'two']);
    assert.equal(h.$('selected-device-name').textContent, '装置一');
    assert.equal(h.$('device-count').textContent, '1');
  } finally {
    await h.close();
  }
});
test('removing the last device clears its table and disables device operations', async () => {
  const h = await setup({ project: { name: 'multi', channels: [multiChannel()] } });
  try {
    h.state.project.channels[0].devices = [];
    await h.refresh();
    assert.equal(h.$('point-rows').children.length, 0);
    assert.equal(h.$('device-config').disabled, true);
    assert.equal(h.$('add-point'), null);
    assert.equal(h.$('read-modbus').disabled, true);
    assert.equal(h.$('add-device').disabled, false);
  } finally {
    await h.close();
  }
});

test('cancelling a protocol replacement leaves every existing device untouched', async () => {
  const h = await setup({
    project: { name: 'multi', channels: [multiChannel()] },
    states: [{ id: 'a', status: 'stopped' }],
  });
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'iec104', '标准版');
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    assert.equal(h.$('confirm-dialog').open, true);
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'cancel'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertDevice').length, 0);
    assert.equal(h.state.project.channels[0].devices.length, 2);
  } finally {
    await h.close();
  }
});

test('device list keyboard navigation selects the corresponding point table', async () => {
  const h = await setup({ project: { name: 'multi', channels: [multiChannel()] } });
  try {
    h.w.document.querySelector('[data-device="one"]').focus();
    h.$('device-list').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    );
    assert.equal(h.$('selected-device-name').textContent, '装置二');
    assert.equal(h.w.document.activeElement.dataset.device, 'two');
    h.$('device-list').dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Home', bubbles: true }),
    );
    assert.equal(h.$('selected-device-name').textContent, '装置一');
  } finally {
    await h.close();
  }
});

test('adding IEC104 requires replacement confirmation and replaces the original device', async () => {
  const c = {
    ...channel('a'),
    protocol: 'iec104',
    devices: [{ id: 'old', name: 'old 104', unit: 1, commonAddress: 1, points: [] }],
  };
  const h = await setup({ project: { name: '104', channels: [c] } });
  try {
    h.overrides.UpsertDevice = (cid, protocol, d, replace) => {
      assert.equal(replace, true);
      h.state.project.channels[0].devices = [{ ...d, id: 'new' }];
      h.state.states[0].status = 'stopped';
      return 'new';
    };
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'iec104', '标准版');
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    assert.equal(h.$('confirm-title').textContent, '替换 IEC104 装置');
    assert.ok(h.$('confirm-text').textContent.includes('当前通道将停止'));
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertDevice').length, 0);
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'ok'),
    );
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'UpsertDevice')[1][2].id, '');
    assert.equal(h.$('device-count').textContent, '1');
    assert.equal(h.w.document.querySelector('[data-device="old"]'), null);
    assert.equal(h.state.states[0].status, 'stopped');
  } finally {
    await h.close();
  }
});

test('unused device address fields cannot block form validation', async () => {
  const c = multiChannel();
  c.devices[0].commonAddress = 0;
  const h = await setup({ project: { name: 'fields', channels: [c] } });
  try {
    h.$('device-config').click();
    assert.equal(h.$('device-form').checkValidity(), true);
    h.$('device-dialog').close();
    h.state.states[0].status = 'stopped';
    await h.refresh();
    h.$('edit-channel').click();
    assert.equal(h.$('channel-form').checkValidity(), true);
  } finally {
    await h.close();
  }
});

test('modeless add-device window targets the currently selected channel', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    h.w.document.querySelector('[data-select="b"]').click();
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'UpsertDevice')[1][0], 'b');
    assert.equal(h.$('device-dialog').open, false);
  } finally {
    await h.close();
  }
});
test('original semantic tabs filter configured point ranges and set the read target', async () => {
  const c = multiChannel();
  c.devices[0].configuration = {
    version: 'TCP版',
    values: {},
    ranges: [
      { tab: 'yx', kind: 'discrete', address: 1, count: 18, name: '遥信' },
      { tab: 'yc', kind: 'holding', address: 1024, count: 18, name: '遥测' },
    ],
  };
  c.devices[0].points = [
    { kind: 'discrete', address: 1, name: '遥信1', value: 0, quality: 0 },
    { kind: 'holding', address: 1024, name: '遥测1', value: 23, quality: 0 },
  ];
  const h = await setup({ project: { name: 'tabs', channels: [c] } });
  try {
    assert.equal(h.$('point-rows').children.length, 1);
    assert.equal(h.$('read-start').value, '1');
    h.w.document.querySelector('[data-point-kind="tab:yc"]').click();
    assert.equal(
      h.$('point-rows').firstElementChild.children[0].querySelector('input').value,
      '1024',
    );
    assert.equal(h.$('read-fc').value, '3');
    assert.equal(h.$('read-start').value, '1024');
    assert.equal(h.$('read-count'), null);
  } finally {
    await h.close();
  }
});
test('network dialog saves separate client/server IP and port controls', async () => {
  const h = await setup({ states: [{ id: 'a', status: 'stopped' }] });
  try {
    h.$('edit-channel').click();
    originalField(h, 'txtClientIP', 'original-network-panel').value = '192.0.2.9';
    originalField(h, 'txtClientPort', 'original-network-panel').value = '1502';
    h.$('channel-form').requestSubmit(
      h.$('channel-form').querySelector('#original-network-panel button[type=submit]'),
    );
    await tick();
    await tick();
    const payload = h.calls.find((c) => c[0] === 'UpdateTransport')[1][0];
    assert.equal(payload.address, '192.0.2.9:1502');
    assert.equal(payload.devices.length, 1);
    assert.equal(payload.transport, 'tcp-client');
  } finally {
    await h.close();
  }
});

test('Modbus defaults to RTU and empty information-body fields are rejected', async () => {
  const h = await setup({
    project: { name: 'raw', channels: [{ ...channel('a'), protocol: 'raw', devices: [] }] },
    states: [{ id: 'a', status: 'stopped' }],
  });
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus');
    assert.equal(h.$('device-version').value, '标准版(RTU)');
    originalField(h, 'txtYX_Add').value = '1，';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertDevice').length, 0);
    assert.equal(h.$('device-form').querySelector('.form-error').hidden, false);
    originalField(h, 'txtYX_Add').value = '1';
    originalField(h, 'txtYX_Num').value = '5';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'UpsertDevice')[1][1], 'modbus-rtu');
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertChannel').length, 0);
  } finally {
    await h.close();
  }
});

test('desktop workspace opens native windows without opening a DOM overlay', async () => {
  const h = await setup({ states: [{ id: 'a', status: 'stopped' }] }, { kind: '' });
  try {
    for (const id of ['edit-channel', 'add-device', 'communication-manager']) {
      h.$(id).click();
      await tick();
      assert.equal(h.w.document.querySelector('dialog[open]'), null);
    }
    const contexts = h.calls.filter((c) => c[0] === 'OpenNativeWindow').map((c) => c[1][0]);
    assert.deepEqual(
      contexts.map((c) => c.kind),
      ['channel-dialog', 'device-dialog', 'manager-dialog'],
    );
    assert.equal(contexts[0].channel, 'a');
    assert.equal(contexts[0].edit, true);
    assert.equal(contexts[1].edit, false);
    h.overrides.AskNative = () => ({ choice: 'cancel' });
    h.$('new-project').click();
    await tick();
    assert.ok(h.calls.some((c) => c[0] === 'AskNative'));
    assert.equal(h.$('confirm-dialog').open, false);
  } finally {
    await h.close();
  }
});

test('native device editor follows the parent selection immediately on submit', async () => {
  const context = {
    kind: 'device-dialog',
    role: 'client',
    channel: 'a',
    device: '',
    edit: false,
    activate: 0,
  };
  const channels = ['a', 'b'].map((id) => ({ ...channel(id), protocol: 'raw', devices: [] }));
  const h = await setup({ project: { name: 'shared', channels }, states: [] }, context);
  try {
    assert.equal(h.w.document.body.classList.contains('native-window'), true);
    assert.equal(h.$('device-dialog').open, true);
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    originalField(h, 'txtAdd').value = '7';
    context.channel = 'b';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    for (let i = 0; i < 5; i++) await tick();
    const call = h.calls.find((c) => c[0] === 'UpsertDevice');
    assert.ok(call);
    assert.equal(call[1][0], 'b');
    assert.equal(call[1][2].unit, 7);
    assert.equal(originalField(h, 'txtAdd').value, '8');
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
    assert.ok(h.calls.some((c) => c[0] === 'SaveNativeDraft'));
  } finally {
    await h.close();
  }
});

test('native point editor patches its captured identity and closes only the child window', async () => {
  const points = [{ kind: 'holding', address: 1, name: 'p', value: 5, quality: 0 }];
  const context = {
    kind: 'point-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    edit: true,
    point: 'holding:1',
  };
  const h = await setup({ project: { name: 'points', channels: [channel('a', points)] } }, context);
  try {
    const input = h.$('point-form').elements.namedItem('value');
    input.value = '9';
    input.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));
    for (let i = 0; i < 4; i++) await tick();
    const call = h.calls.find((c) => c[0] === 'PatchDevicePoint');
    assert.equal(call[1][0], 'a');
    assert.equal(call[1][1], 'device-1');
    assert.equal(call[1][2].address, 1);
    assert.deepEqual([...call[1][4]], ['value']);
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
    assert.equal(
      h.calls.some((c) => c[0] === 'StopAll'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('native confirmation returns its result over IPC before closing', async () => {
  const context = {
    kind: 'confirm-dialog',
    ask: {
      title: '名称',
      input: 'old',
      choices: [
        ['cancel', '取消'],
        ['ok', '确认'],
      ],
    },
  };
  const h = await setup({}, context);
  try {
    h.$('confirm-input').value = 'new';
    h.$('confirm-form').requestSubmit(h.$('confirm-actions').lastElementChild);
    for (let i = 0; i < 3; i++) await tick();
    const result = h.calls.find((c) => c[0] === 'ResolveNativeAsk');
    assert.equal(result[1][0].choice, 'ok');
    assert.equal(result[1][0].value, 'new');
    assert.ok(
      h.calls.findIndex((c) => c[0] === 'CloseNativeWindow') >
        h.calls.findIndex((c) => c[0] === 'ResolveNativeAsk'),
    );
  } finally {
    await h.close();
  }
});

function iecChannel(id = 'a', points = []) {
  return { ...channel(id, points), protocol: 'iec104' };
}

test('IEC104 remote control and adjustment open independent desktop windows', async () => {
  const h = await setup(
    {
      project: {
        name: 'controls',
        channels: [iecChannel('a', [{ kind: 'yk', address: 5000, value: 1, quality: 0 }])],
      },
    },
    { kind: '' },
  );
  try {
    for (const [button, mode, type, tab] of [
      ['open-yk', 'yk', 45, 'yk'],
      ['open-step', 'step', 47, 'yt'],
      ['open-yt', 'setpoint', 50, 'yt'],
    ]) {
      h.w.document.querySelector('[data-point-kind="' + tab + '"]').click();
      h.$(button).click();
      await tick();
      const call = h.calls.filter((c) => c[0] === 'OpenNativeWindow').at(-1);
      assert.equal(call[1][0].kind, 'control-dialog');
      assert.equal(call[1][0].point, mode);
      assert.equal(call[1][0].control.type, type);
      assert.equal(call[1][0].channel, 'a');
      assert.equal(h.$('control-dialog').open, false);
    }
  } finally {
    await h.close();
  }
});

test('native remote control waits for selection confirmation before execute', async () => {
  const context = {
    kind: 'control-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: 'yk',
    control: { type: 46, address: 5000, value: 2 },
  };
  const h = await setup({ project: { name: 'controls', channels: [iecChannel()] } }, context);
  try {
    assert.equal(h.$('control-dialog').open, true);
    assert.equal(h.$('control-execute').disabled, true);
    let reply;
    h.overrides.Command104Device = () =>
      new Promise((resolve) => {
        reply = resolve;
      });
    h.$('control-select').click();
    await tick();
    assert.equal(h.$('control-select').disabled, true);
    assert.equal(h.$('control-execute').disabled, true);
    let request = h.calls.filter((c) => c[0] === 'Command104Device').at(-1)[1];
    assert.equal(request[0], 'a');
    assert.equal(request[1], 'device-1');
    assert.equal(request[2].type, 46);
    assert.equal(request[2].value, 2);
    assert.equal(request[2].operation, 'select');
    reply({ stage: 'selected', message: '选择确认' });
    for (let i = 0; i < 4; i++) await tick();
    assert.equal(h.$('control-execute').disabled, false);
    assert.equal(h.$('control-cancel').disabled, false);
    h.overrides.Command104Device = () => ({ stage: 'completed', message: '执行结束' });
    h.$('control-execute').click();
    for (let i = 0; i < 4; i++) await tick();
    assert.equal(h.$('control-result').textContent, '执行结束');
    assert.equal(h.$('control-execute').disabled, true);
  } finally {
    await h.close();
  }
});

test('editing control parameters invalidates selection; negative replies never show success', async () => {
  const context = {
    kind: 'control-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: 'yk',
    control: { type: 45, address: 5000, value: 1 },
  };
  const h = await setup({ project: { name: 'controls', channels: [iecChannel()] } }, context);
  try {
    h.overrides.Command104Device = () => ({ stage: 'selected', message: '选择确认' });
    h.$('control-select').click();
    for (let i = 0; i < 4; i++) await tick();
    assert.equal(h.$('control-execute').disabled, false);
    h.$('control-address').value = '5001';
    h.$('control-address').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    assert.equal(h.$('control-execute').disabled, true);
    assert.equal(h.$('control-cancel').disabled, true);
    h.overrides.Command104Device = () => {
      throw Error('子站拒绝命令（原因 47）');
    };
    h.$('control-select').click();
    for (let i = 0; i < 4; i++) await tick();
    assert.ok(h.$('control-result').textContent.includes('拒绝'));
    assert.equal(h.$('control-execute').disabled, true);
    assert.equal(h.$('control-form').querySelector('.form-error').hidden, false);
  } finally {
    await h.close();
  }
});

test('setpoint and step controls validate their actual wire values', async () => {
  for (const mode of ['setpoint', 'step']) {
    const context = {
      kind: 'control-dialog',
      role: 'client',
      channel: 'a',
      device: 'device-1',
      point: mode,
      control: { type: mode === 'step' ? 47 : 50, address: 123, value: 0 },
    };
    const h = await setup({ project: { name: 'controls', channels: [iecChannel()] } }, context);
    try {
      h.overrides.Command104Device = () => ({ stage: 'completed', message: '执行结束' });
      if (mode === 'setpoint') {
        assert.equal(h.$('control-direct').checked, true);
        h.$('control-value').value = '1.5';
        h.$('control-execute').click();
        for (let i = 0; i < 4; i++) await tick();
        assert.equal(h.calls.filter((c) => c[0] === 'Command104Device').at(-1)[1][2].value, 1.5);
        h.$('control-type').value = '49';
        h.$('control-type').dispatchEvent(new h.w.Event('change', { bubbles: true }));
        h.$('control-value').value = '1.5';
        const count = h.calls.filter((c) => c[0] === 'Command104Device').length;
        h.$('control-execute').click();
        await tick();
        assert.equal(h.calls.filter((c) => c[0] === 'Command104Device').length, count);
      } else {
        h.$('control-direct').checked = true;
        h.$('control-direct').dispatchEvent(new h.w.Event('change', { bubbles: true }));
        h.$('control-state').value = '1';
        h.$('control-execute').click();
        for (let i = 0; i < 4; i++) await tick();
        const request = h.calls.filter((c) => c[0] === 'Command104Device').at(-1)[1][2];
        assert.equal(request.type, 47);
        assert.equal(request.value, 1);
      }
    } finally {
      await h.close();
    }
  }
});

test('read-only device confirmation does not regenerate imported control points', async () => {
  const c = iecChannel('a', [{ kind: 'yt', address: 123, value: 42, quality: 0 }]);
  const h = await setup({ project: { name: 'controls', channels: [c] } });
  try {
    h.$('device-config').click();
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    for (let i = 0; i < 3; i++) await tick();
    assert.equal(
      h.calls.some((c) => c[0] === 'UpsertDevice'),
      false,
    );
    assert.equal(h.state.project.channels[0].points[0].value, 42);
  } finally {
    await h.close();
  }
});

test('double-point OFF is encoded as 1 rather than ON when reopening a native command window', async () => {
  const context = {
    kind: 'control-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: 'yk',
    control: { type: 46, address: 5000, value: 1 },
  };
  const h = await setup({ project: { name: 'controls', channels: [iecChannel()] } }, context);
  try {
    assert.equal(h.$('control-state').value, '0');
    h.overrides.Command104Device = () => ({ stage: 'selected', message: '选择确认' });
    h.$('control-select').click();
    for (let i = 0; i < 4; i++) await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'Command104Device').at(-1)[1][2].value, 1);
  } finally {
    await h.close();
  }
});

function autoSnapshot(mode = 'yk') {
  return {
    id: 'job-1',
    channel: 'a',
    device: 'device-1',
    mode,
    status: 'idle',
    revision: 1,
    run: 0,
    current: -1,
    total: 0,
    success: 0,
    failure: 0,
    errors: [],
    lastError: '',
    options: {
      intervalMs: mode === 'yk' ? 2 : 200,
      timeoutMs: 10000,
      double: false,
      cycle: false,
      direct: false,
      checkFeedback: false,
      autoPause: true,
      valueType: 50,
    },
    rows: [0, 1].map((i) => ({
      address: 100 + i,
      name: 'point ' + i,
      enabled: true,
      action: '合闸后分闸',
      count: 1,
      value: '10',
      feedback: '',
      feedbackName: '未关联',
      selection: '',
      execution: '',
      feedbackResult: '',
      executed: '否',
    })),
  };
}
async function setupAuto(mode = 'yk') {
  const data = autoSnapshot(mode),
    copy = () => structuredClone(data),
    mutate = (fn) => {
      fn();
      data.revision++;
      return copy();
    };
  const context = {
    kind: 'auto-' + mode + '-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: mode,
    edit: true,
  };
  const handlers = {
    GetAutoControl: copy,
    GetAutoControlDraft: copy,
    SaveAutoControlDraft: () =>
      mutate(() => {
        data.saved = true;
      }),
    UpdateAutoOptions: (_c, _d, _m, o) =>
      mutate(() => {
        data.options = structuredClone(o);
      }),
    UpdateAutoRows: (_c, _d, _m, edits) =>
      mutate(() => {
        for (const edit of edits) Object.assign(data.rows[edit.index], edit);
      }),
    StartAutoControl: () =>
      mutate(() => {
        data.status = 'running';
        data.run++;
      }),
    PauseAutoControl: () =>
      mutate(() => {
        data.status = 'paused';
      }),
    ResumeAutoControl: () =>
      mutate(() => {
        data.status = 'running';
      }),
    StopAutoControl: () =>
      mutate(() => {
        data.status = 'stopped';
      }),
  };
  const h = await setup(
    { project: { name: 'auto', channels: [iecChannel('a')] } },
    context,
    handlers,
  );
  for (let i = 0; i < 3; i++) await tick();
  return {
    ...h,
    data,
    poll: async () => {
      await h.w.eval('pollAutoControl()');
      await tick();
    },
  };
}
async function settleAuto() {
  for (let i = 0; i < 6; i++) await tick();
}

test('automatic command launchers open configuration without seeding hidden main-page parameters', async () => {
  const h = await setup(
    {
      project: {
        name: 'auto',
        channels: [
          iecChannel('a', [
            { kind: 'yk', address: 100, value: 0 },
            { kind: 'yt', address: 200, value: 0 },
          ]),
        ],
      },
    },
    { kind: '' },
    { PrepareAutoControl: () => autoSnapshot() },
  );
  try {
    h.w.document.querySelector('[data-point-kind="yk"]').click();
    h.$('open-auto-yk').click();
    await settleAuto();
    let call = h.calls.filter((c) => c[0] === 'OpenNativeWindow').at(-1);
    assert.equal(call[1][0].kind, 'auto-yk-dialog');
    assert.equal(call[1][0].device, 'device-1');
    h.w.document.querySelector('[data-point-kind="yt"]').click();
    h.$('auto-yt-addresses').value = '5,1-3';
    h.$('auto-yt-value').value = '10H';
    h.$('auto-yt-type').value = '49';
    h.$('open-auto-yt').click();
    await settleAuto();
    call = h.calls.filter((c) => c[0] === 'PrepareAutoControl').at(-1);
    assert.equal(call[1][2], 'yt');
    assert.deepEqual(JSON.parse(JSON.stringify(call[1][3])), {});
    assert.equal(
      h.calls.filter((c) => c[0] === 'OpenNativeWindow').at(-1)[1][0].kind,
      'auto-yt-dialog',
    );
    assert.equal(h.w.document.querySelector('dialog[open]'), null);
  } finally {
    await h.close();
  }
});

test('automatic YK editor preserves configuration defaults and excludes runtime columns', async () => {
  const h = await setupAuto('yk');
  try {
    assert.deepEqual(
      [...h.$('auto-yk-dialog').querySelectorAll('th')].map((el) => el.textContent),
      ['序号', '启用', '点位', '点名称', '遥控类型', '遥控次数', '遥信关联'],
    );
    assert.equal(h.$('auto-yk-interval').value, '2');
    assert.equal(h.$('auto-yk-timeout').value, '10000');
    assert.equal(h.$('auto-yk-autopause').checked, true);
    assert.equal(h.$('auto-yk-start'), null);
    assert.equal(h.$('auto-yk-stop'), null);
    h.$('auto-yk-form').requestSubmit(h.$('auto-yk-save'));
    await settleAuto();
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
    assert.equal(h.calls.filter((c) => c[0] === 'SaveAutoControlDraft').length, 1);
    assert.equal(
      h.calls.some((c) =>
        ['StartAutoControl', 'PauseAutoControl', 'ResumeAutoControl', 'StopAutoControl'].includes(
          c[0],
        ),
      ),
      false,
    );
  } finally {
    await h.close();
  }
});

test('automatic configuration polling preserves drafts without mixing in runtime outcomes', async () => {
  const h = await setupAuto('yk');
  try {
    const row = h.$('auto-yk-rows').children[0],
      input = row.querySelector('[data-auto-field="count"]');
    input.focus();
    input.value = '3';
    h.data.current = 0;
    h.data.rows[0].selection = '第1次合闸选择成功';
    h.data.revision++;
    await h.poll();
    assert.equal(h.w.document.activeElement, input);
    assert.equal(input.value, '3');
    assert.equal(row.children.length, 7);
    assert.doesNotMatch(row.textContent, /选择成功/);
    input.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    await settleAuto();
    const call = h.calls.filter((c) => c[0] === 'UpdateAutoRows').at(-1);
    assert.equal(call[1][3][0].count, 3);
    assert.equal(call[1][0], 'a');
    assert.equal(call[1][1], 'device-1');
    h.data.rows[0].execution = '第1次合闸执行成功';
    h.data.rows[0].feedbackResult = '变位成功';
    h.data.revision++;
    await h.poll();
    assert.equal(row.children.length, 7);
    assert.doesNotMatch(row.textContent, /执行成功|变位成功/);
  } finally {
    await h.close();
  }
});

test('automatic YK right-click opens the independent original bulk editor', async () => {
  const h = await setupAuto('yk');
  try {
    h.$('auto-yk-rows').dispatchEvent(
      new h.w.MouseEvent('contextmenu', { bubbles: true, clientX: 10, clientY: 10 }),
    );
    const button = h.$('context-menu').querySelector('button');
    assert.equal(button.textContent, '批量修改');
    button.click();
    await settleAuto();
    const call = h.calls.find((c) => c[0] === 'OpenAutoBulk');
    assert.deepEqual([...call[1]], ['a', 'device-1']);
  } finally {
    await h.close();
  }
});

test('bulk changes apply only checked fields to the captured channel/device', async () => {
  const context = {
    kind: 'auto-bulk-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: 'yk',
  };
  const h = await setup({}, context, { ApplyAutoBulk: () => autoSnapshot() });
  try {
    h.$('auto-bulk-action-check').checked = true;
    h.$('auto-bulk-action').value = '分闸';
    h.$('auto-bulk-count-check').checked = true;
    h.$('auto-bulk-count').value = '4';
    h.$('auto-bulk-form').requestSubmit(h.$('auto-bulk-form').querySelector('button'));
    await settleAuto();
    const call = h.calls.find((c) => c[0] === 'ApplyAutoBulk');
    assert.equal(call[1][0], 'a');
    assert.equal(call[1][1], 'device-1');
    assert.equal(call[1][2].changeEnabled, false);
    assert.equal(call[1][2].action, '分闸');
    assert.equal(call[1][2].count, 4);
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
  } finally {
    await h.close();
  }
});

test('automatic YT editor keeps four configuration columns and saves without starting', async () => {
  const h = await setupAuto('yt');
  try {
    assert.deepEqual(
      [...h.$('auto-yt-dialog').querySelectorAll('th')].map((el) => el.textContent),
      ['序号', '点位', '点名称', '遥调数值'],
    );
    assert.equal(h.$('auto-yt-interval').value, '200');
    assert.equal(h.$('auto-yt-start'), null);
    assert.equal(h.$('auto-yt-stop'), null);
    const input = h.$('auto-yt-rows').children[1].querySelector('input');
    input.value = '1.5';
    input.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    await settleAuto();
    h.$('auto-yt-form').requestSubmit(h.$('auto-yt-save'));
    await settleAuto();
    assert.equal(h.data.rows[1].value, '1.5');
    assert.ok(h.calls.some((c) => c[0] === 'SaveAutoControlDraft'));
    assert.equal(
      h.calls.some((c) => c[0] === 'StartAutoControl'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('closing an automatic configuration window never stops its running task', async () => {
  const h = await setupAuto('yk');
  try {
    h.data.status = 'running';
    h.$('auto-yk-dialog').close();
    await settleAuto();
    assert.ok(h.calls.some((c) => c[0] === 'CloseNativeWindow'));
    assert.equal(
      h.calls.some((c) => ['SaveAutoControlDraft', 'StopAutoControl', 'StopAll'].includes(c[0])),
      false,
    );
    assert.equal(h.data.status, 'running');
  } finally {
    await h.close();
  }
});

test('invalid repetition input prevents saving stale automatic settings', async () => {
  const h = await setupAuto('yk');
  try {
    h.$('auto-yk-rows').children[0].querySelector('[data-auto-field=count]').value = '';
    h.$('auto-yk-form').dispatchEvent(new h.w.Event('submit', { bubbles: true, cancelable: true }));
    await settleAuto();
    assert.equal(
      h.calls.some((c) => c[0] === 'SaveAutoControlDraft'),
      false,
    );
    assert.equal(h.$('auto-yk-form').querySelector('.form-error').hidden, false);
  } finally {
    await h.close();
  }
});

test('point editor exposes and saves the original inverse YX association syntax', async () => {
  const h = await setup({
    project: {
      name: 'link',
      channels: [
        iecChannel('a', [{ kind: 'yk', address: 100, value: 0, quality: 0, feedback: '!1' }]),
      ],
    },
  });
  try {
    h.w.document.querySelector('[data-point-kind="yk"]').click();
    openPointDetails(h);
    const field = h.$('point-form').elements.namedItem('feedback');
    assert.equal(field.value, '!1');
    assert.equal(h.$('point-feedback-label').hidden, false);
    field.value = '2';
    field.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    h.$('point-form').requestSubmit(h.$('point-form').querySelector('button[type=submit]'));
    await settleAuto();
    const call = h.calls.find((c) => c[0] === 'PatchDevicePoint');
    assert.equal(call[1][3].feedback, '2');
    assert.deepEqual([...call[1][4]], ['feedback']);
  } finally {
    await h.close();
  }
});

test('failed device creation leaves the editor open for correction', async () => {
  const h = await setup();
  try {
    h.overrides.UpsertDevice = () => {
      throw Error('duplicate device');
    };
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    for (let i = 0; i < 5; i++) await tick();
    assert.equal(h.$('device-dialog').open, true);
    assert.equal(
      h.calls.some((c) => c[0] === 'CloseNativeWindow'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('waiting connection remains stoppable and communication settings stay editable', async () => {
  const h = await setup({
    states: [{ id: 'a', status: 'connecting', lastError: 'connection refused' }],
  });
  try {
    assert.ok(h.$('toggle-channel').textContent.includes('停止'));
    h.$('edit-channel').click();
    assert.equal(originalField(h, 'txtClientIP', 'original-network-panel').disabled, false);
    h.$('channel-dialog').close();
    h.$('toggle-channel').click();
    await tick();
    await tick();
    assert.ok(h.calls.some((c) => c[0] === 'StopChannel'));
    assert.equal(
      h.calls.some((c) => c[0] === 'StartChannel'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('startup enters the client workspace directly and switching modes has no launcher', async () => {
  for (const context of [null, { kind: '' }]) {
    const h = await setup({}, context);
    try {
      assert.equal(h.$('launch-screen'), null);
      assert.equal(h.$('main-menu'), null);
      assert.ok(h.calls.some((c) => c[0] === 'SetWorkspaceMode' && c[1][0] === 'client'));
      assert.equal(
        h.w.document
          .querySelector('.mode-switch [data-mode="client"]')
          .getAttribute('aria-selected'),
        'true',
      );
      assert.ok(h.w.document.querySelector('[data-select="a"].selected'));
      h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
      await tick();
      await tick();
      assert.ok(h.calls.some((c) => c[0] === 'SetWorkspaceMode' && c[1][0] === 'server'));
      assert.equal(
        h.w.document
          .querySelector('.mode-switch [data-mode="server"]')
          .getAttribute('aria-selected'),
        'true',
      );
      assert.equal(
        h.calls.some((c) => c[0] === 'SetWorkspaceMode' && c[1][0] === ''),
        false,
      );
      assert.equal(
        h.calls.some((c) => c[0] === 'StartChannel'),
        false,
      );
    } finally {
      await h.close();
    }
  }
});

async function setupInline() {
  const c = dataServer();
  c.devices.push({ ...structuredClone(c.devices[0]), id: 'other', name: 'other device' });
  const h = await setup({
    project: { name: 'inline', channels: [c] },
    states: [{ id: c.id, status: 'connected', peers: 1 }],
  });
  h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
  await tick();
  await tick();
  h.w.document.querySelector('[data-point-kind="yc"]').click();
  h.overrides.SetPointValue = (channel, device, key, value) => {
    h.state.project.channels
      .find((c) => c.id === channel)
      .devices.find((d) => d.id === device)
      .points.find((p) => p.kind === key.kind && p.address === key.address).value = value;
    return { mode: 'reported', receivers: 1 };
  };
  return h;
}
function editInline(h, input, value) {
  input.value = value;
  input.dispatchEvent(new h.w.Event('input', { bubbles: true }));
}
function inlineKey(h, input, key) {
  input.dispatchEvent(new h.w.KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));
}
async function settleInline() {
  for (let i = 0; i < 6; i++) await tick();
}

test('inline analog Enter and blur submit once while polling preserves the pending input', async () => {
  const h = await setupInline();
  try {
    const input = h.w.document.querySelector('[data-point-key="yc:10"] .point-value-input');
    assert.ok(input);
    input.focus();
    editInline(h, input, '12.5');
    let finish;
    h.overrides.SetPointValue = (channel, device, key, value) =>
      new Promise((resolve) => {
        finish = () => {
          h.state.project.channels[0].devices[0].points[0].value = value;
          resolve({ mode: 'reported', receivers: 1 });
        };
      });
    inlineKey(h, input, 'Enter');
    input.blur();
    inlineKey(h, input, 'Enter');
    h.state.project.channels[0].devices[0].points[1].value = 55;
    await h.refresh();
    assert.equal(h.w.document.querySelector('[data-point-key="yc:10"] .point-value-input'), input);
    assert.equal(input.value, '12.5');
    assert.equal(input.readOnly, true);
    assert.equal(h.calls.filter((c) => c[0] === 'SetPointValue').length, 1);
    finish();
    await settleInline();
    assert.equal(input.readOnly, false);
    assert.equal(input.value, '12.5');
    input.dispatchEvent(new h.w.Event('blur'));
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'SetPointValue').length, 1);
    assert.deepEqual(JSON.parse(JSON.stringify(h.calls.find((c) => c[0] === 'SetPointValue')[1])), [
      'data',
      'target',
      { kind: 'yc', address: 10 },
      12.5,
    ]);
    assert.equal(
      h.calls.some((c) => c[0] === 'PublishDevice'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('inline drafts survive polling and commit to their captured device on selection change', async () => {
  const h = await setupInline();
  try {
    const input = h.w.document.querySelector('.point-value-input');
    input.focus();
    editInline(h, input, '23.75');
    h.state.project.channels[0].devices[0].points[0].value = 99;
    await h.refresh();
    assert.equal(input.value, '23.75');
    h.w.document.querySelector('[data-device="other"]').click();
    await settleInline();
    const call = h.calls.find((c) => c[0] === 'SetPointValue');
    assert.equal(call[1][1], 'target');
    assert.equal(call[1][3], 23.75);
    assert.equal(h.state.project.channels[0].devices[1].points[0].value, 0);
    assert.equal(h.$('selected-device-name').textContent, 'other device');
  } finally {
    await h.close();
  }
});
test('inline values validate before submitting, Escape cancels, and errors preserve the draft', async () => {
  const h = await setupInline();
  try {
    const input = h.w.document.querySelector('.point-value-input');
    for (const value of ['', 'abc', 'Infinity', '1e50']) {
      editInline(h, input, value);
      inlineKey(h, input, 'Enter');
      await settleInline();
      assert.equal(input.getAttribute('aria-invalid'), 'true');
    }
    assert.equal(
      h.calls.some((c) => c[0] === 'SetPointValue'),
      false,
    );
    inlineKey(h, input, 'Escape');
    assert.equal(input.value, '0');
    assert.equal(input.hasAttribute('aria-invalid'), false);
    editInline(h, input, '20');
    h.overrides.SetPointValue = () => {
      throw Error('connection failed');
    };
    inlineKey(h, input, 'Enter');
    await settleInline();
    assert.equal(input.value, '20');
    assert.equal(input.getAttribute('aria-invalid'), 'true');
    h.w.document.querySelector('[data-point-kind="yx"]').click();
    await settleInline();
    const bit = h.w.document.querySelector('.point-value-input');
    editInline(h, bit, '2');
    inlineKey(h, bit, 'Enter');
    await settleInline();
    assert.equal(bit.getAttribute('aria-invalid'), 'true');
  } finally {
    await h.close();
  }
});
test('inline blur saves offline with an explicit unsent status and operation buttons stay removed', async () => {
  const h = await setupInline();
  try {
    h.overrides.SetPointValue = (channel, device, key, value) => {
      h.state.project.channels[0].devices[0].points[0].value = value;
      return { mode: 'offline', receivers: 0 };
    };
    const input = h.w.document.querySelector('.point-value-input');
    input.focus();
    editInline(h, input, '45');
    input.blur();
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'SetPointValue').length, 1);
    assert.ok(h.$('toast').textContent.includes('未上送'));
  } finally {
    await h.close();
  }
  const client = await setup();
  try {
    assert.ok(client.w.document.querySelector('#point-rows'));
    assert.equal(client.w.document.querySelectorAll('[data-edit-point]').length, 0);
  } finally {
    await client.close();
  }
});

function openPointDetails(h) {
  h.$('point-rows').firstElementChild.dispatchEvent(
    new h.w.MouseEvent('contextmenu', { bubbles: true, clientX: 10, clientY: 10 }),
  );
  [...h.$('context-menu').children].find((b) => b.textContent === '详细配置').click();
}

test('navigation separators resize by keyboard and pointer, restore half height and persist preferences', async () => {
  const h = await setup();
  try {
    const width = h.$('sidebar-resizer'),
      height = h.$('navigation-resizer');
    assert.ok(width && height);
    assert.equal(height.getAttribute('aria-valuenow'), '50');
    width.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    assert.equal(width.getAttribute('aria-valuenow'), '192');
    height.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    assert.equal(height.getAttribute('aria-valuenow'), '55');
    assert.equal(h.w.localStorage.getItem('gylab-navigation-share'), '0.55');
    height.dispatchEvent(new h.w.MouseEvent('dblclick', { bubbles: true }));
    assert.equal(height.getAttribute('aria-valuenow'), '50');
    width.setPointerCapture = () => {};
    width.dispatchEvent(
      new h.w.PointerEvent('pointerdown', { button: 0, pointerId: 1, clientX: 200, bubbles: true }),
    );
    width.dispatchEvent(
      new h.w.PointerEvent('pointermove', { pointerId: 1, clientX: 240, bubbles: true }),
    );
    assert.equal(width.getAttribute('aria-valuenow'), '232');
    width.dispatchEvent(new h.w.PointerEvent('pointercancel', { pointerId: 1, bubbles: true }));
    assert.equal(width.classList.contains('dragging'), false);
    width.dispatchEvent(new h.w.MouseEvent('dblclick', { bubbles: true }));
    assert.equal(width.getAttribute('aria-valuenow'), '180');
  } finally {
    await h.close();
  }
});
test('point grid contains five editable columns without row operation buttons and client commits locally', async () => {
  const h = await setup({
    project: {
      name: 'client',
      channels: [
        channel('a', [{ kind: 'holding', address: 1, value: 2, name: 'local', quality: 0 }]),
      ],
    },
  });
  try {
    assert.equal(h.w.document.querySelectorAll('.point-table th').length, 5);
    assert.equal(h.$('point-rows').querySelectorAll('button').length, 0);
    assert.equal(h.$('point-rows').querySelectorAll('.point-cell-input').length, 5);
    h.overrides.EditPointCell = (channel, device, key, field, patch) => {
      Object.assign(h.state.project.channels[0].devices[0].points[0], patch);
      return { mode: 'stored' };
    };
    const value = h.w.document.querySelector('[data-point-field="value"]');
    editInline(h, value, '123');
    inlineKey(h, value, 'Enter');
    await settleInline();
    const call = h.calls.find((c) => c[0] === 'EditPointCell');
    assert.equal(call[1][3], 'value');
    assert.equal(call[1][4].value, 123);
    assert.equal(
      h.calls.some((c) =>
        ['SetPointValue', 'PublishDevice', 'WriteDeviceModbus', 'Command104Device'].includes(c[0]),
      ),
      false,
    );
  } finally {
    await h.close();
  }
});
test('hexadecimal telemetry edits use IEEE float values and invalid quality stays uncommitted', async () => {
  const h = await setupInline();
  try {
    const hex = h.w.document.querySelector('[data-point-field="hex"]');
    editInline(h, hex, '41480000');
    inlineKey(h, hex, 'Enter');
    await settleInline();
    assert.equal(h.calls.find((c) => c[0] === 'SetPointValue')[1][3], 12.5);
    const quality = h.w.document.querySelector('[data-point-field="quality"]');
    editInline(h, quality, '256');
    inlineKey(h, quality, 'Enter');
    await settleInline();
    assert.equal(quality.getAttribute('aria-invalid'), 'true');
    assert.equal(
      h.calls.some((c) => c[0] === 'EditPointCell'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('address changes serialize following cell edits to the new key without replacing the focused editor', async () => {
  const h = await setupInline();
  try {
    let finish;
    h.overrides.EditPointCell = (channel, device, key, field, patch) =>
      new Promise((resolve) => {
        finish = () => {
          Object.assign(h.state.project.channels[0].devices[0].points[0], patch);
          resolve({ mode: 'stored' });
        };
      });
    const address = h.w.document.querySelector('[data-point-field="address"]'),
      name = h.w.document.querySelector('[data-point-field="name"]');
    editInline(h, address, '500');
    inlineKey(h, address, 'Enter');
    await tick();
    name.focus();
    editInline(h, name, 'renamed');
    inlineKey(h, name, 'Enter');
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'EditPointCell').length, 1);
    finish();
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'EditPointCell').length, 2);
    assert.equal(h.calls.filter((c) => c[0] === 'EditPointCell')[1][1][2].address, 500);
    assert.equal(
      h.w.document.querySelector('[data-point-key="yc:500"] [data-point-field="name"]'),
      name,
    );
    finish();
    await settleInline();
    assert.equal(name.value, 'renamed');
  } finally {
    await h.close();
  }
});
test('a configured point keeps its tab when edited beyond the original address range', async () => {
  const c = dataServer();
  c.devices[0].points[0].address = 1000;
  c.devices[0].points[0].tab = 'yc';
  c.devices[0].configuration = {
    ranges: [
      { tab: 'yc', kind: 'yc', address: 10, count: 2 },
      { tab: 'yx', kind: 'yx', address: 1, count: 1 },
    ],
  };
  const h = await setup({ project: { name: 'custom', channels: [c] } });
  try {
    h.w.document.querySelector('.mode-switch [data-mode="server"]').click();
    await settleInline();
    h.w.document.querySelector('[data-point-kind="tab:yc"]').click();
    assert.ok(h.w.document.querySelector('[data-point-key="yc:1000"]'));
  } finally {
    await h.close();
  }
});

test('native popup initialization does not overwrite main-window navigation preferences', async () => {
  const h = await setup({}, { kind: 'manager-dialog', role: 'client', channel: 'a' });
  try {
    assert.equal(h.w.localStorage.getItem('gylab-navigation-width'), null);
    assert.equal(h.w.localStorage.getItem('gylab-navigation-share'), null);
    h.w.dispatchEvent(new h.w.Event('resize'));
    assert.equal(h.w.localStorage.getItem('gylab-navigation-width'), null);
  } finally {
    await h.close();
  }
});

test('all numeric inputs hide spinners and ignore increment shortcuts', async () => {
  const css = await readFile(new URL('../frontend/app.css', import.meta.url), 'utf8');
  assert.match(css, /input\[type=[\'"]?number[\'"]?\]::-webkit-inner-spin-button/);
  assert.match(css, /-webkit-appearance:\s*none/);
  const h = await setup();
  try {
    h.$('edit-channel').click();
    const port = originalField(h, 'txtClientPort', 'original-network-panel');
    port.value = '2425';
    port.focus();
    for (const key of ['ArrowUp', 'ArrowDown']) {
      const event = new h.w.KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
      port.dispatchEvent(event);
      assert.equal(event.defaultPrevented, true);
      assert.equal(port.value, '2425');
    }
    const wheel = new h.w.WheelEvent('wheel', { deltaY: -1, bubbles: true, cancelable: true });
    port.dispatchEvent(wheel);
    assert.equal(wheel.defaultPrevented, true);
    assert.equal(port.type, 'number');
    assert.equal(port.max, '65535');
  } finally {
    await h.close();
  }
});

async function setupManager() {
  const h = await setup(
    {
      project: {
        name: 'two',
        channels: [channel('a'), { ...channel('b'), address: '127.0.0.1:2425' }],
      },
    },
    { kind: 'manager-dialog', role: 'client', channel: 'a' },
  );
  h.overrides.UpdateTransport = (config) => {
    const c = h.state.project.channels.find((c) => c.id === config.id);
    Object.assign(c, config);
    h.state.states = h.state.states.filter((s) => s.id !== c.id);
    h.state.states.push({ id: c.id, status: 'stopped' });
  };
  h.overrides.RenameChannel = (id, name) => {
    h.state.project.channels.find((c) => c.id === id).name = name;
  };
  return h;
}
const managerInput = (h, id, key) =>
  h
    .$('manager-rows')
    .querySelector('[data-manager-id="' + id + '"] [data-manager-field="' + key + '"]');
test('communication manager edits the clicked row directly without editor buttons or window calls', async () => {
  const h = await setupManager();
  try {
    assert.equal(h.$('manager-rows').querySelector('[data-manager-edit]'), null);
    assert.equal(
      [...h.$('manager-rows').querySelectorAll('button')].some((b) =>
        b.textContent.includes('编辑'),
      ),
      false,
    );
    const address = managerInput(h, 'b', 'address');
    editInline(h, address, '127.0.0.1:2500');
    inlineKey(h, address, 'Enter');
    address.dispatchEvent(new h.w.Event('blur'));
    await settleInline();
    const edits = h.calls.filter((c) => c[0] === 'UpdateTransport');
    assert.equal(edits.length, 1);
    assert.equal(edits[0][1][0].id, 'b');
    assert.equal(edits[0][1][0].address, '127.0.0.1:2500');
    assert.equal(h.state.project.channels[0].address, '127.0.0.1:502');
    assert.equal(
      h.calls.some((c) => ['OpenNativeWindow', 'StartChannel'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});
test('manager drafts survive polling, Escape cancels and failures preserve input', async () => {
  const h = await setupManager();
  try {
    const address = managerInput(h, 'b', 'address');
    address.focus();
    editInline(h, address, 'bad endpoint');
    await h.refresh();
    assert.equal(managerInput(h, 'b', 'address'), address);
    assert.equal(address.value, 'bad endpoint');
    inlineKey(h, address, 'Escape');
    assert.equal(address.value, '127.0.0.1:2425');
    address.blur();
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'UpdateTransport'),
      false,
    );
    h.overrides.UpdateTransport = () => {
      throw Error('点位应为 host:port');
    };
    editInline(h, address, 'invalid');
    inlineKey(h, address, 'Enter');
    await settleInline();
    assert.equal(address.value, 'invalid');
    assert.equal(address.getAttribute('aria-invalid'), 'true');
    assert.equal(h.state.project.channels[1].address, '127.0.0.1:2425');
  } finally {
    await h.close();
  }
});
test('queued manager edits merge the latest endpoint, binding and point data', async () => {
  const h = await setupManager();
  try {
    let finish;
    const save = h.overrides.UpdateTransport;
    let count = 0;
    h.overrides.UpdateTransport = (config) => {
      if (++count === 1)
        return new Promise((resolve) => {
          finish = () => {
            save(config);
            resolve();
          };
        });
      save(config);
    };
    const address = managerInput(h, 'b', 'address'),
      local = managerInput(h, 'b', 'localAddress');
    editInline(h, address, '127.0.0.1:2501');
    inlineKey(h, address, 'Enter');
    await tick();
    await tick();
    editInline(h, local, '127.0.0.1:0');
    inlineKey(h, local, 'Enter');
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'UpdateTransport').length, 1);
    finish();
    await settleInline();
    const edits = h.calls.filter((c) => c[0] === 'UpdateTransport');
    assert.equal(edits.length, 2);
    assert.equal(edits[1][1][0].address, '127.0.0.1:2501');
    assert.equal(edits[1][1][0].localAddress, '127.0.0.1:0');
    const name = managerInput(h, 'b', 'name');
    editInline(h, name, 'renamed');
    inlineKey(h, name, 'Enter');
    await settleInline();
    assert.equal(h.state.project.channels[1].name, 'renamed');
    assert.equal(
      h.calls.some((c) => c[0] === 'OpenNativeWindow'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('manager exposes serial settings inline and commits selection without duplicate blur', async () => {
  const h = await setupManager();
  try {
    h.state.project.channels[1].protocol = 'raw';
    h.state.project.channels[1].transport = 'serial';
    h.state.project.channels[1].address = '/dev/cu.test';
    await h.refresh();
    const baud = managerInput(h, 'b', 'baud');
    assert.equal(baud.closest('label').hidden, false);
    editInline(h, baud, '19200');
    baud.dispatchEvent(new h.w.Event('blur'));
    await settleInline();
    assert.equal(h.state.project.channels[1].baud, 19200);
    assert.equal(h.state.project.channels[1].transportDraft.cbBaudRate, '19200');
    const parity = managerInput(h, 'b', 'parity');
    parity.value = 'E';
    parity.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    parity.dispatchEvent(new h.w.Event('blur'));
    await settleInline();
    assert.equal(h.state.project.channels[1].parity, 'E');
    assert.equal(h.state.project.channels[1].transportDraft.cbParity, '偶校验');
    assert.equal(h.calls.filter((c) => c[0] === 'UpdateTransport').length, 2);
  } finally {
    await h.close();
  }
});
test('connected communication editor applies and closes without a start request', async () => {
  const h = await setup();
  try {
    h.$('edit-channel').click();
    assert.equal(
      h.$('original-network-panel').querySelector('button[type=submit]').textContent,
      '应用',
    );
    originalField(h, 'txtClientPort', 'original-network-panel').value = '2425';
    h.$('channel-form').requestSubmit(
      h.$('original-network-panel').querySelector('button[type=submit]'),
    );
    await tick();
    await tick();
    assert.equal(h.$('channel-dialog').open, false);
    assert.equal(h.calls.filter((c) => c[0] === 'UpdateTransport').length, 1);
    assert.equal(
      h.calls.some((c) => c[0] === 'StartChannel'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('communication blur and polling preserve the apply button during the first click', async () => {
  const h = await setup();
  try {
    h.$('edit-channel').click();
    const panel = h.$('original-network-panel'),
      button = panel.querySelector('button[type=submit]'),
      label = button.firstChild;
    const summary = h.$('network-summary').firstChild,
      port = originalField(h, 'txtClientPort', 'original-network-panel');
    port.focus();
    port.value = '25048';
    port.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    button.dispatchEvent(new h.w.MouseEvent('mousedown', { bubbles: true }));
    port.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    port.blur();
    assert.equal(button.firstChild, label);
    assert.equal(h.$('network-summary').firstChild, summary);
    await h.refresh();
    assert.equal(button.firstChild, label);
    assert.equal(h.$('network-summary').firstChild, summary);
    assert.equal(port.value, '25048');
    button.click();
    await tick();
    await tick();
    const edits = h.calls.filter((c) => c[0] === 'UpdateTransport');
    assert.equal(edits.length, 1);
    assert.equal(edits[0][1][0].address, '127.0.0.1:25048');
    assert.equal(h.$('channel-dialog').open, false);
  } finally {
    await h.close();
  }
});

test('new device has a visible unused default name and saves the typed name', async () => {
  const c = {
    ...channel('a'),
    protocol: 'raw',
    devices: [{ id: 'first', name: '装置1', points: [], unit: 1, commonAddress: 1 }],
  };
  const h = await setup({ project: { name: 'names', channels: [c] }, states: [] });
  try {
    h.$('add-device').click();
    const name = h.$('device-form').elements.namedItem('name');
    assert.equal(name.type, 'text');
    assert.equal(name.value, '装置2');
    assert.equal(name.readOnly, false);
    name.value = '自定义装置';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    h.$('confirm-actions').querySelector('button[value=ok]').click();
    await settleInline();
    assert.equal(h.calls.find((c) => c[0] === 'UpsertDevice')[1][2].name, '自定义装置');
  } finally {
    await h.close();
  }
});
test('repeated channel clicks while creation is pending create only one channel', async () => {
  const h = await setup();
  try {
    let release;
    h.overrides.UpsertChannel = () =>
      new Promise((resolve) => {
        release = resolve;
      });
    h.$('add-channel').click();
    h.$('add-channel').click();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertChannel').length, 1);
    release('new');
    await settleInline();
    assert.equal(h.$('add-channel').disabled, false);
  } finally {
    await h.close();
  }
});

test('new project opens another workspace without replacing or stopping this project', async () => {
  for (const role of ['client', 'server']) {
    const h = await setup(
      {
        project: {
          name: 'original',
          channels: [
            { ...channel('old'), role, transport: role === 'server' ? 'tcp-server' : 'tcp-client' },
          ],
        },
        states: [{ id: 'old', status: 'connected', peers: 1 }],
        dirty: true,
        path: '/tmp/original.json',
      },
      { kind: '' },
    );
    try {
      if (role === 'server') {
        h.w.document.querySelector('[data-mode=server]').click();
        await settleInline();
      }
      h.overrides.AskNative = () => ({ choice: 'ok', value: '工程2' });
      h.$('new-project').click();
      await settleInline();
      const call = h.calls.find((c) => c[0] === 'NewProject');
      assert.deepEqual([...call[1]], ['工程2', role]);
      assert.equal(h.calls.filter((c) => c[0] === 'AskNative').length, 1);
      assert.equal(h.state.project.name, 'original');
      assert.equal(h.state.path, '/tmp/original.json');
      assert.equal(h.state.states[0].status, 'connected');
      assert.ok(h.w.document.querySelector('[data-select=old]'));
      assert.equal(
        h.calls.some((c) =>
          ['StartChannel', 'StopAll', 'StopChannel', 'SaveProject'].includes(c[0]),
        ),
        false,
      );
    } finally {
      await h.close();
    }
  }
});
test('new project defaults advance from project one and cancelled dialogs do not consume numbers', async () => {
  const h = await setup(
    { project: { name: '工程1', channels: [] }, states: [], dirty: false },
    { kind: '' },
  );
  try {
    const prompts = [];
    let answer = 'cancel';
    h.overrides.AskNative = (options) => {
      prompts.push(options.input);
      return { choice: answer, value: options.input };
    };
    h.overrides.NewProject = () => null;
    h.$('new-project').click();
    await settleInline();
    assert.equal(prompts.at(-1), '工程2');
    assert.equal(h.state.project.name, '工程1');
    answer = 'ok';
    h.$('new-project').click();
    await settleInline();
    assert.equal(prompts.at(-1), '工程2');
    assert.equal(h.state.project.name, '工程1');
    h.$('new-project').click();
    await settleInline();
    assert.equal(prompts.at(-1), '工程3');
    assert.equal(h.state.project.name, '工程1');
    h.state.project.name = '工程8';
    await h.refresh();
    h.$('new-project').click();
    await settleInline();
    assert.equal(prompts.at(-1), '工程9');
  } finally {
    await h.close();
  }
});

test('project title marks unsaved paths and edits, then clears the marker only after a successful save', async () => {
  const h = await setup({
    project: { name: '工程1', channels: [channel('a')] },
    states: [],
    path: '',
    dirty: false,
  });
  try {
    assert.equal(h.$('project-name').textContent, '工程1（未保存）');
    h.overrides.SaveProject = () => {
      h.state.path = '/tmp/现场.json';
      h.state.project.name = '现场';
      h.state.dirty = false;
      return h.state.path;
    };
    h.$('save-project').click();
    await settleInline();
    assert.equal(h.$('project-name').textContent, '现场');
    h.state.states = [{ id: 'a', status: 'connected', peers: 1 }];
    h.state.messages.push({
      sequence: 1,
      channelId: 'a',
      direction: 'RX',
      summary: 'received',
      hex: '01',
    });
    await h.refresh();
    assert.equal(h.$('project-name').textContent, '现场');
    h.state.dirty = true;
    await h.refresh();
    assert.equal(h.$('project-name').textContent, '现场（未保存）');
    h.overrides.SaveProject = () => '';
    h.$('save-project').click();
    await settleInline();
    assert.equal(h.$('project-name').textContent, '现场（未保存）');
    h.overrides.SaveProject = () => {
      throw Error('save failed');
    };
    h.$('save-project').click();
    await settleInline();
    assert.equal(h.$('project-name').textContent, '现场（未保存）');
  } finally {
    await h.close();
  }
});

test('debug panel resizes from an external separator and remembers keyboard or pointer changes', async () => {
  const h = await setup();
  try {
    const handle = h.$('debug-resizer'),
      dock = h.$('report-dock');
    assert.equal(handle.parentElement, dock);
    assert.equal(handle.closest('.debug-dock'), null);
    assert.equal(handle.getAttribute('aria-valuenow'), '130');
    handle.setPointerCapture = () => {};
    handle.dispatchEvent(
      new h.w.PointerEvent('pointerdown', { button: 0, pointerId: 1, clientY: 400, bubbles: true }),
    );
    handle.dispatchEvent(
      new h.w.PointerEvent('pointermove', { pointerId: 1, clientY: 350, bubbles: true }),
    );
    assert.equal(handle.getAttribute('aria-valuenow'), '180');
    assert.equal(dock.style.getPropertyValue('--debug-height'), '180px');
    handle.dispatchEvent(new h.w.PointerEvent('pointercancel', { pointerId: 1, bubbles: true }));
    assert.equal(handle.classList.contains('dragging'), false);
    handle.dispatchEvent(
      new h.w.PointerEvent('pointermove', { pointerId: 1, clientY: 300, bubbles: true }),
    );
    assert.equal(handle.getAttribute('aria-valuenow'), '180');
    handle.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    assert.equal(handle.getAttribute('aria-valuenow'), '168');
    assert.equal(h.w.localStorage.getItem('gylab-debug-height'), '168');
    handle.dispatchEvent(new h.w.MouseEvent('dblclick', { bubbles: true }));
    assert.equal(handle.getAttribute('aria-valuenow'), '130');
    handle.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
    assert.equal(handle.getAttribute('aria-valuenow'), '142');
    handle.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'Home', bubbles: true }));
    assert.equal(handle.getAttribute('aria-valuenow'), '130');
  } finally {
    await h.close();
  }
  const popup = await setup({}, { kind: 'manager-dialog', role: 'client', channel: 'a' });
  try {
    assert.equal(popup.w.localStorage.getItem('gylab-debug-height'), null);
    popup.w.dispatchEvent(new popup.w.Event('resize'));
    assert.equal(popup.w.localStorage.getItem('gylab-debug-height'), null);
  } finally {
    await popup.close();
  }
});

test('editing an existing device saves real parameters once without connecting and preserves drafts during polling', async () => {
  const c = iecChannel('a');
  c.devices = [
    {
      id: 'dev',
      name: 'existing',
      unit: 1,
      commonAddress: 1,
      points: [{ kind: 'yx', address: 1, value: 1, quality: 0 }],
      configuration: {
        version: '标准版',
        values: {
          txtAdd: '1',
          txtYX_Add: '1',
          txtYX_Num: '1',
          txtYC_Add: '100',
          txtYC_Num: '0',
          txtMC_Add: '200',
          txtMC_Num: '0',
          txtYK_Add: '300',
          txtYK_Num: '0',
          txtTotal_Time: '60',
        },
        ranges: [{ tab: 'yx', kind: 'yx', address: 1, count: 1 }],
      },
    },
  ];
  const h = await setup({ project: { name: 'edit', channels: [c] } });
  try {
    h.$('device-config').click();
    const field = originalField(h, 'txtTotal_Time');
    assert.equal(field.disabled, false);
    field.value = '15';
    await h.refresh();
    assert.equal(field.value, '15');
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    await tick();
    const calls = h.calls.filter((c) => c[0] === 'UpsertDevice');
    assert.equal(calls.length, 1);
    assert.equal(calls[0][1][2].id, 'dev');
    assert.equal(calls[0][1][2].configuration.values.txtTotal_Time, '15');
    assert.equal(
      h.calls.some((c) => c[0] === 'StartChannel'),
      false,
    );
    assert.equal(h.$('device-dialog').open, false);
  } finally {
    await h.close();
  }
});
test('SOE and COS show device timestamps and invalid quality then clear only the selected category', async () => {
  const c = iecChannel();
  const h = await setup({
    project: { name: 'events', channels: [c] },
    events: [
      {
        channelId: 'a',
        category: 'SOE',
        timestamp: '2026-10-22T12:00:00.123+08:00',
        received: 'later',
        address: 1,
        kind: 'yx',
        value: 1,
        quality: 128,
        timeInvalid: true,
      },
      {
        channelId: 'a',
        category: 'COS',
        received: '2026-10-22T13:00:00Z',
        address: 2,
        kind: 'yx',
        value: 0,
        quality: 0,
      },
    ],
  });
  try {
    h.$('show-events').click();
    assert.match(h.$('event-rows').textContent, /12:00:00.123/);
    assert.match(h.$('event-rows').textContent, /无效/);
    h.$('event-category').value = 'SOE';
    h.$('event-category').dispatchEvent(new h.w.Event('change'));
    assert.equal(h.$('event-rows').children.length, 1);
    h.$('clear-events').click();
    await tick();
    assert.deepEqual([...h.calls.find((c) => c[0] === 'ClearEvents')[1]], ['a', 'SOE']);
  } finally {
    await h.close();
  }
});
test('typed registers edit signed and floating values with wire-order hexadecimal roundtrips', async () => {
  const c = channel('a', [
    { kind: 'holding', address: 0, value: -123, dataType: 'int16', byteOrder: 'BA', quality: 0 },
    {
      kind: 'holding',
      address: 2,
      value: 12.5,
      dataType: 'float32',
      byteOrder: 'CDAB',
      quality: 0,
    },
  ]);
  const h = await setup({ project: { name: 'typed', channels: [c] } });
  try {
    const rows = h.$('point-rows').children;
    assert.equal(rows[0].children[1].querySelector('input').value, '85FF');
    assert.equal(rows[1].children[1].querySelector('input').value, '4148');
    const input = rows[0].children[2].querySelector('input');
    input.value = '-32768';
    input.dispatchEvent(new h.w.Event('input'));
    input.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await settleInline();
    assert.equal(h.calls.find((c) => c[0] === 'EditPointCell')[1][4].value, -32768);
  } finally {
    await h.close();
  }
});
test('timed remote controls keep the existing selection workflow and send type 58', async () => {
  const h = await setup({
    project: { name: 'timed', channels: [iecChannel('a', [{ kind: 'yk', address: 1, value: 0 }])] },
  });
  try {
    h.w.document.querySelector('[data-point-kind="yk"]').click();
    h.$('open-yk').click();
    h.$('control-timestamp').checked = true;
    h.$('control-select').click();
    await tick();
    assert.equal(h.calls.find((c) => c[0] === 'Command104Device')[1][2].type, 58);
  } finally {
    await h.close();
  }
});
test('legacy configuration edits keep absent pulse counts at zero and typed point details accept signed values', async () => {
  const c = iecChannel('a', [{ kind: 'yx', address: 1, value: 1, quality: 0 }]);
  c.devices = [
    {
      id: 'd',
      name: 'legacy',
      commonAddress: 1,
      unit: 1,
      points: c.points,
      configuration: {
        version: '标准版',
        values: {
          txtYX_Add: '1',
          txtYX_Num: '1',
          txtYC_Add: '100',
          txtYC_Num: '0',
          txtYK_Add: '200',
          txtYK_Num: '0',
        },
        ranges: [{ tab: 'yx', kind: 'yx', address: 1, count: 1 }],
      },
    },
  ];
  const h = await setup({ project: { name: 'old', channels: [c] } });
  try {
    h.$('device-config').click();
    assert.equal(originalField(h, 'txtMC_Num'), null);
    assert.equal(
      h.w
        .eval('configurationFromPanel')('iec104')
        .ranges.some((r) => r.tab === 'mc'),
      false,
    );
  } finally {
    await h.close();
  }
  const typed = await setup({
    project: {
      name: 'typed',
      channels: [
        channel('a', [
          { kind: 'holding', address: 1, value: -12.5, dataType: 'float32', quality: 0 },
        ]),
      ],
    },
  });
  try {
    openPointDetails(typed);
    const value = typed.$('point-form').elements.namedItem('value');
    assert.equal(value.step, 'any');
    assert.ok(Number(value.min) < -12.5);
    assert.equal(typed.$('point-form').checkValidity(), true);
  } finally {
    await typed.close();
  }
});
test('32-bit configured ranges read complete register pairs', async () => {
  const c = channel('a');
  c.devices = [
    {
      id: 'd',
      name: 'float',
      unit: 1,
      commonAddress: 1,
      points: [
        { kind: 'holding', tab: 'yc', address: 100, value: 12.5, dataType: 'float32', quality: 0 },
      ],
      configuration: {
        version: 'TCP版',
        values: {},
        ranges: [{ tab: 'yc', kind: 'holding', address: 100, count: 80 }],
      },
    },
  ];
  const h = await setup({ project: { name: 'wide', channels: [c] } });
  try {
    assert.equal(h.$('read-start').value, '100');
    assert.equal(h.$('read-count'), null);
    h.$('read-modbus').click();
    await settleInline();
    assert.deepEqual(
      [...h.calls.find((c) => c[0] === 'ReadDeviceModbusPoints')[1]],
      ['a', 'd', 3, '100'],
    );
  } finally {
    await h.close();
  }
});

test('four-remote tabs select their own controls and offline launchers never send commands', async () => {
  const points = ['yx', 'yc', 'yk', 'yt'].map((kind, i) => ({
    kind,
    address: i * 100 + 1,
    name: kind,
    value: 0,
    quality: 0,
  }));
  const h = await setup({
    project: { name: 'four', channels: [iecChannel('a', points)] },
    states: [{ id: 'a', status: 'stopped', peers: 0 }],
  });
  try {
    for (const [kind, launcher] of [
      ['yk', 'open-yk'],
      ['yt', 'open-yt'],
    ]) {
      h.w.document.querySelector('[data-point-kind="' + kind + '"]').click();
      assert.equal(h.$('interrogate'), null);
      assert.equal(h.$(launcher).disabled, false);
      h.$(launcher).click();
      assert.equal(h.$('control-dialog').open, true);
      assert.equal(h.$('control-execute').disabled, true);
      assert.match(h.$('control-result').textContent, /未连接/);
      h.$('control-dialog').close();
    }
    h.w.document.querySelector('[data-point-kind="yx"]').click();
    assert.ok(h.$('interrogate'));
    assert.equal(h.$('interrogate').disabled, true);
    assert.match(h.$('interrogate').getAttribute('aria-description'), /连接/);
    assert.equal(h.$('open-yk'), null);
    assert.equal(
      h.calls.some((c) => c[0] === 'Command104Device'),
      false,
    );
  } finally {
    await h.close();
  }
});
test('new client devices expose configurable adjustment ranges for IEC104 and Modbus', async () => {
  for (const group of ['iec104', 'modbus']) {
    const h = await setup();
    try {
      h.$('add-device').click();
      if (group === 'modbus') chooseDeviceProtocol(h, 'modbus', 'TCP版');
      assert.equal(originalField(h, 'txtYT_Num').value, '50');
      originalField(h, 'txtYT_Add').value = '3200';
      originalField(h, 'txtYT_Num').value = '3';
      const config = h.w.eval('configurationFromPanel')(
        group === 'iec104' ? 'iec104' : 'modbus-tcp',
      );
      const range = config.ranges.find((r) => r.tab === 'yt');
      assert.equal(range.address, 3200);
      assert.equal(range.count, 3);
      assert.equal(range.kind, group === 'iec104' ? 'yt' : 'holding');
      assert.equal(config.values.txtYT_Num, '3');
    } finally {
      await h.close();
    }
  }
});

test('device invalid Enter keeps the window and focuses a readable field error; valid Enter saves once', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    const count = originalField(h, 'txtYX_Num');
    count.value = 'bad';
    count.dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );
    await settleInline();
    assert.equal(h.$('device-dialog').open, true);
    assert.equal(count.getAttribute('aria-invalid'), 'true');
    assert.equal(h.w.document.activeElement, count);
    assert.match(h.$('device-form').querySelector('.form-error').textContent, /数量/);
    assert.equal(
      h.calls.some((c) => c[0] === 'UpsertDevice'),
      false,
    );
    count.value = '5';
    count.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    count.dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'UpsertDevice').length, 1);
    assert.equal(h.$('device-dialog').open, false);
  } finally {
    await h.close();
  }
});
test('device enable labels are distinct and disabled settings retain drafts without blocking saves', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    const host = h.$('original-device-panel');
    assert.equal(
      [...host.querySelectorAll('.original-check')].filter((el) => el.textContent === '使能')
        .length,
      0,
    );
    const enabled = originalField(h, 'timeEnable'),
      period = originalField(h, 'txtSyn_Time');
    period.value = 'invalid';
    enabled.checked = false;
    enabled.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(period.disabled, true);
    assert.equal(period.value, 'invalid');
    const config = h.w.eval('configurationFromPanel')('iec104');
    assert.equal(config.values.timeEnable, 'false');
    assert.equal(config.values.txtSyn_Time, 'invalid');
    enabled.checked = true;
    enabled.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(period.disabled, false);
    assert.throws(() => h.w.eval('configurationFromPanel')('iec104'), /周期校时间隔/);
  } finally {
    await h.close();
  }
});
test('server panels expose four independent configurations while hiding client-only and TCP CRC settings', async () => {
  const c = { ...channel('s'), role: 'server', transport: 'tcp-server', devices: [] };
  const h = await setup({ project: { name: 'server', channels: [c] }, states: [] });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('add-device').click();
    assert.equal(originalField(h, 'txtYT_Num').value, '50');
    assert.equal(originalField(h, 'txtST_Num'), null);
    assert.deepEqual(
      [...h.w.eval('configurationFromPanel')('iec104').ranges].map((r) => r.tab),
      ['yx', 'yc', 'yk', 'yt'],
    );
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    assert.equal(h.w.document.querySelector('[data-extra-setting=pollEnabled]'), null);
    assert.equal(h.w.document.querySelector('[data-extra-setting=eventCount]'), null);
    assert.equal(originalField(h, 'cb_check').closest('label').hidden, true);
    h.$('device-version').value = '标准版(RTU)';
    h.$('device-version').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(originalField(h, 'cb_check').closest('label').hidden, false);
  } finally {
    await h.close();
  }
});
test('device first point and count validation respects physical register widths', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    originalField(h, 'txtYC_Add').value = '256';
    originalField(h, 'txtYC_Num').value = '2';
    const type = originalField(h, 'cb_ycFormat');
    type.value = '4:单精度浮点(Float)';
    type.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.$('device-point-summary'), null);
    assert.deepEqual(
      [
        ...h.w
          .eval('devicePointPreview')(
            h.w.eval('configurationFromPanel')('modbus-tcp'),
            'modbus-tcp',
          )
          .ranges.find((r) => r.tab === 'yc').points,
      ],
      [256, 258],
    );
    assert.equal(
      h.w
        .eval('configurationFromPanel')('modbus-tcp')
        .ranges.find((r) => r.tab === 'yc').address,
      256,
    );
    originalField(h, 'txtYT_Add').value = '257';
    assert.throws(() => h.w.eval('configurationFromPanel')('modbus-tcp'), /重叠/);
  } finally {
    await h.close();
  }
});
test('shrinking point ranges confirms deletion without a permanent preview and cancel preserves the device', async () => {
  const c = iecChannel('a');
  c.devices = [
    {
      id: 'd',
      name: 'keep',
      unit: 1,
      commonAddress: 1,
      configuration: {
        version: '标准版',
        values: {},
        ranges: [
          { tab: 'yx', kind: 'yx', address: 1, count: 2, name: '遥信' },
          { tab: 'yc', kind: 'yc', address: 100, count: 0, name: '遥测' },
          { tab: 'mc', kind: 'mc', address: 200, count: 0, name: '脉冲' },
          { tab: 'yk', kind: 'yk', address: 300, count: 0, name: '遥控' },
          { tab: 'yt', kind: 'yt', address: 400, count: 0, name: '遥调' },
          { tab: 'step', kind: 'step', address: 500, count: 0, name: '步调节' },
        ],
      },
      points: [
        { kind: 'yx', tab: 'yx', address: 1, value: 1, name: 'keep', quality: 0 },
        { kind: 'yx', tab: 'yx', address: 2, value: 0, name: 'remove', quality: 0 },
      ],
    },
  ];
  const h = await setup({ project: { name: 'preview', channels: [c] } });
  try {
    h.$('device-config').click();
    originalField(h, 'txtYX_Num').value = '1';
    originalField(h, 'txtYX_Num').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    assert.equal(h.$('device-point-summary'), null);
    assert.equal(h.w.document.querySelector('.device-support-note'), null);
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('button[type=submit]'));
    await tick();
    assert.equal(h.$('confirm-title').textContent, '确认点表变更');
    h.$('confirm-actions').querySelector('[value=cancel]').click();
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'UpsertDevice'),
      false,
    );
    assert.equal(h.state.project.channels[0].devices[0].points.length, 2);
    assert.equal(h.$('device-dialog').open, true);
  } finally {
    await h.close();
  }
});

test('disabled polling and stop-and-wait flow control disable their unused interval and window fields', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    const poll = h.w.document.querySelector('[data-extra-setting=pollEnabled]');
    poll.checked = false;
    poll.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(originalField(h, 'txtSendInterval').disabled, true);
    originalField(h, 'txtSendInterval').value = 'draft';
    assert.doesNotThrow(() => h.w.eval('configurationFromPanel')('modbus-tcp'));
    h.$('device-dialog').close();
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('add-device').click();
    const wait = originalField(h, 'ck_IsDataSentWaitS');
    wait.checked = true;
    wait.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.w.document.querySelector('[data-extra-setting=sendWindow]').disabled, true);
  } finally {
    await h.close();
  }
});

test('four tabs hide legacy categories while device saves retain imported ranges and settings', async () => {
  const c = iecChannel('a');
  c.devices = [
    {
      id: 'd',
      name: 'legacy',
      unit: 1,
      commonAddress: 1,
      configuration: {
        version: '标准版',
        values: { mcEnable: 'true', txtMC_Time: '3' },
        ranges: [
          { tab: 'yx', kind: 'yx', address: 1, count: 1, name: '遥信' },
          { tab: 'mc', kind: 'mc', address: 100, count: 1, name: '脉冲' },
        ],
      },
      points: [
        { kind: 'yx', tab: 'yx', address: 1, value: 1, name: 'switch', quality: 0 },
        { kind: 'mc', tab: 'mc', address: 100, value: 123, name: 'energy', quality: 0 },
      ],
    },
  ];
  const h = await setup({ project: { name: 'legacy', channels: [c] } });
  try {
    assert.equal(h.$('point-tabs').children.length, 4);
    assert.deepEqual(
      [...h.$('point-tabs').children].map((b) => b.textContent).sort(),
      ['遥信', '遥控', '遥测', '遥调'].sort(),
    );
    h.$('device-config').click();
    const config = h.w.eval('configurationFromPanel')('iec104');
    assert.equal(config.values.mcEnable, 'true');
    assert.equal(config.ranges.find((r) => r.tab === 'mc').count, 1);
    assert.equal(originalField(h, 'txtMC_Num'), null);
    assert.equal(h.$('device-point-summary'), null);
    assert.equal(h.w.document.querySelector('.device-support-note'), null);
  } finally {
    await h.close();
  }
});
test('search matches only zero-to-five-digit decimal addresses and descriptions', async () => {
  const points = [
    { kind: 'yc', address: 16385, name: '进线电压 Voltage', value: 220, quality: 0 },
    { kind: 'yc', address: 200, name: '温度', value: 25, quality: 0 },
    { kind: 'yc', address: 0, name: '零点位', value: 0, quality: 0 },
    { kind: 'yc', address: 99999, name: '五位点位', value: 0, quality: 0 },
    { kind: 'yc', address: 100000, name: '六位点位', value: 0, quality: 0 },
  ];
  const h = await setup({ project: { name: 'search', channels: [iecChannel('a', points)] } });
  try {
    for (const [query, addresses] of [
      ['16385', [16385]],
      [' 16385 ', [16385]],
      ['163', [16385]],
      ['0', [0, 200]],
      ['99999', [99999]],
      ['100000', []],
      ['10000', []],
      ['016385', []],
      ['0x4001', []],
      ['4001H', []],
      ['1e2', []],
      ['200.0', []],
      ['-1', []],
      ['电压', [16385]],
      ['VOLTAGE', [16385]],
      ['温度', [200]],
      ['六位点位', [100000]],
      ['遥测', []],
    ]) {
      h.$('point-search').value = query;
      h.$('point-search').dispatchEvent(new h.w.Event('input', { bubbles: true }));
      assert.deepEqual(
        [...h.$('point-rows').children].map((row) =>
          Number(row.querySelector('[data-point-field=address]').value),
        ),
        addresses,
        query,
      );
    }
  } finally {
    await h.close();
  }
});
test('Modbus four-remote request drafts remain isolated across category switches and polling', async () => {
  const c = multiChannel();
  const h = await setup({ project: { name: 'drafts', channels: [c] } });
  try {
    h.w.document.querySelector('[data-point-kind=yc]').click();
    h.$('read-start').value = '17';
    h.$('read-start').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    h.w.document.querySelector('[data-point-kind=yt]').click();
    h.$('write-address').value = '99';
    h.$('write-address').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    await h.refresh();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    assert.equal(h.$('read-start').value, '17');
    h.w.document.querySelector('[data-point-kind=yt]').click();
    assert.equal(h.$('write-address').value, '99');
  } finally {
    await h.close();
  }
});

test('editing a legacy device keeps range ordering so settings changes preserve sparse and manual points', async () => {
  const ranges = [
    { tab: 'yx', kind: 'yx', address: 1, count: 2, name: '遥信' },
    { tab: 'mc', kind: 'mc', address: 100, count: 1, name: '脉冲' },
    { tab: 'yc', kind: 'yc', address: 1000, count: 0, name: '遥测' },
  ];
  const c = iecChannel('a');
  c.devices = [
    {
      id: 'd',
      name: 'sparse',
      unit: 1,
      commonAddress: 1,
      configuration: { version: '标准版', values: {}, ranges: structuredClone(ranges) },
      points: [
        { kind: 'yx', tab: 'yx', address: 1, value: 1, quality: 0 },
        { kind: 'yx', tab: 'yx', address: 8, value: 0, quality: 0 },
        { kind: 'mc', tab: 'mc', address: 100, value: 12, quality: 0 },
      ],
    },
  ];
  const h = await setup({ project: { name: 'sparse', channels: [c] } });
  try {
    h.$('device-config').click();
    originalField(h, 'txtUTest_Time').value = '30';
    const config = h.w.eval('configurationFromPanel')('iec104');
    assert.deepEqual(
      [...config.ranges].map((r) => r.tab),
      ranges.map((r) => r.tab),
    );
    assert.equal(config.ranges[0].selection, '1，8');
    assert.equal(config.ranges[0].count, 2);
    assert.deepEqual(JSON.parse(JSON.stringify(config.ranges[1])), ranges[1]);
    const review = h.w.eval('devicePointPreview')(config, 'iec104');
    assert.equal(review.removed, 0);
    assert.equal(review.kept, 3);
  } finally {
    await h.close();
  }
});

test('four-remote server control pages save independent policies and mappings offline', async () => {
  for (const protocol of ['iec104', 'modbus-tcp']) {
    const c = dataServer(protocol),
      d = c.devices[0];
    d.points.push(
      ...(protocol === 'iec104'
        ? [
            { kind: 'yk', address: 100, name: 'control', value: 0 },
            { kind: 'yt', address: 200, name: 'adjust', value: 0 },
          ]
        : [
            { kind: 'coil', tab: 'yk', address: 100, name: 'control', value: 0 },
            { kind: 'holding', tab: 'yt', address: 200, name: 'adjust', value: 0 },
          ]),
    );
    if (protocol !== 'iec104')
      d.points.find((p) => p.kind === 'coil' && p.address === 1).tab = 'yx';
    const configs = {
      yk: { mode: 'normal', delayMs: 0, mappings: [] },
      yt: { mode: 'normal', delayMs: 0, mappings: [] },
    };
    const h = await setup({ project: { name: 'remote', channels: [c] }, states: [] }, null, {
      GetControlSimulation: (ch, dev, category) => structuredClone(configs[category]),
      SaveControlSimulation: (ch, dev, category, cfg) => {
        assert.equal(dev, 'target');
        configs[category] = structuredClone(cfg);
      },
    });
    try {
      h.w.document.querySelector('[data-mode=server]').click();
      for (const [category, delay] of [
        ['yk', '125'],
        ['yt', '250'],
      ]) {
        h.w.document.querySelector('[data-point-kind=' + category + ']').click();
        assert.equal(
          h.$('open-remote-config').textContent,
          category === 'yk' ? '遥控配置' : '遥调配置',
        );
        h.$('open-remote-config').click();
        await settleInline();
        h.$('remote-delay').value = delay;
        h.$('remote-mapping-add').click();
        const row = h.$('remote-mappings').firstElementChild;
        assert.equal(
          Number(row.querySelector('[data-remote-field=source]').value),
          category === 'yk' ? 100 : 200,
        );
        if (category === 'yk') row.querySelector('[data-remote-field=invert]').checked = true;
        h.$('remote-config-form').requestSubmit();
        await settleInline();
        assert.equal(configs[category].delayMs, Number(delay));
        assert.equal(configs[category].mappings.length, 1);
        assert.equal(configs[category].mappings[0].invert, category === 'yk');
      }
      assert.equal(configs.yk.delayMs, 125);
      assert.equal(configs.yt.delayMs, 250);
      assert.equal(
        h.calls.some((c) => c[0] === 'StartChannel'),
        false,
      );
    } finally {
      await h.close();
    }
  }
});
test('combined automatic reporting modes offer background and YC periodic causes', async () => {
  const c = dataServer();
  let saved;
  const h = await setup({ project: { name: 'scan', channels: [c] }, states: [] }, null, {
    GetDataTest: () => dataState(),
    SaveDataTest: (ch, d, scope, cfg) => {
      saved = cfg;
      return { ...dataState(), config: cfg };
    },
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('open-auto-data').click();
    await settleInline();
    const rows = [...h.$('auto-data-rows').children];
    for (const row of rows) {
      const kind = row.querySelector('[data-data-field=kind]').value,
        mode = row.querySelector('[data-data-field=mode]');
      assert.equal(
        [...mode.options].some((o) => o.value === 'report:1'),
        kind === 'yc',
      );
      mode.value = kind === 'yc' ? 'report:1' : 'report:2';
    }
    h.$('auto-data-save').click();
    await settleInline();
    assert.deepEqual(
      JSON.parse(JSON.stringify(saved.rules.map((r) => [r.kind, r.mode, r.cause]))),
      [
        ['yx', 'report', 2],
        ['yc', 'report', 1],
      ],
    );
  } finally {
    await h.close();
  }
});

test('batch telemetry values apply one range and optional quality without starting a repeating job', async () => {
  const c = dataServer();
  const h = await setup({ project: { name: 'batch', channels: [c] }, states: [] }, null, {
    SetTelemetryRange: () => ({ points: 1, receivers: 0 }),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('open-telemetry-values').click();
    await settleInline();
    h.$('telemetry-mode').value = 'flip';
    h.$('telemetry-mode').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.$('telemetry-value').disabled, true);
    h.$('telemetry-quality-enabled').checked = true;
    h.$('telemetry-quality-enabled').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    h.$('telemetry-quality').value = '128';
    h.$('telemetry-values-form').requestSubmit();
    await settleInline();
    const call = h.calls.find((c) => c[0] === 'SetTelemetryRange');
    assert.equal(call[1][2].category, 'yx');
    assert.equal(call[1][2].quality, 128);
    assert.equal(call[1][2].mode, 'flip');
    assert.equal(
      h.calls.some((c) => ['StartDataTest', 'StartChannel'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});
test('native control configuration launch fixes the category and device while keeping the main window clear', async () => {
  const c = dataServer();
  c.devices[0].points.push(
    { kind: 'yk', address: 100, value: 0 },
    { kind: 'yt', address: 200, value: 0 },
  );
  const h = await setup({ project: { name: 'native', channels: [c] }, states: [] }, { kind: '' });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    for (const category of ['yk', 'yt']) {
      h.w.document.querySelector('[data-point-kind=' + category + ']').click();
      h.$('open-remote-config').click();
      await settleInline();
      const ctx = h.calls.filter((c) => c[0] === 'OpenNativeWindow').at(-1)[1][0];
      assert.equal(ctx.kind, 'remote-config-dialog');
      assert.equal(ctx.edit, true);
      assert.equal(ctx.pointKind, category);
      assert.equal(ctx.device, 'target');
      assert.equal(h.$('remote-config-dialog').open, false);
    }
  } finally {
    await h.close();
  }
});
test('Modbus write actions expose batch register commands and automatic controls in the right tabs', async () => {
  const c = channel('a', [
    { kind: 'coil', tab: 'yk', address: 100, value: 0 },
    { kind: 'holding', tab: 'yt', address: 200, value: 0 },
  ]);
  const h = await setup({ project: { name: 'modbus', channels: [c] } });
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    assert.ok(h.$('open-auto-yk'));
    assert.equal(h.$('open-auto-yt'), null);
    h.w.document.querySelector('[data-point-kind=yt]').click();
    assert.ok(h.$('open-auto-yt'));
    h.$('write-fc').value = '16';
    h.$('write-fc').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.$('write-count'), null);
    h.$('write-address').value = '200-201';
    h.$('write-value').value = '42';
    h.$('write-modbus').click();
    await tick();
    h.$('confirm-form').requestSubmit(
      [...h.$('confirm-actions').children].find((b) => b.value === 'ok'),
    );
    await settleInline();
    const call = h.calls.find((c) => c[0] === 'WriteDeviceModbusPoints');
    assert.deepEqual([...call[1]], ['a', 'device-1', 16, '200-201', 42]);
  } finally {
    await h.close();
  }
});

test('Modbus remote control points can configure real YX feedback for automatic verification', async () => {
  for (const kind of ['coil', 'holding']) {
    const c = channel('a', [
      { kind, tab: 'yk', address: 100, value: 0, quality: 0, feedback: '!1' },
      { kind: 'discrete', tab: 'yx', address: 1, value: 0, quality: 0 },
    ]);
    const h = await setup({ project: { name: 'feedback', channels: [c] } });
    try {
      h.w.document.querySelector('[data-point-kind=yk]').click();
      openPointDetails(h);
      assert.equal(h.$('point-feedback-label').hidden, false);
      const input = h.$('point-form').elements.namedItem('feedback');
      input.value = '2';
      input.dispatchEvent(new h.w.Event('input', { bubbles: true }));
      h.$('point-form').requestSubmit();
      await settleInline();
      const call = h.calls.find((c) => c[0] === 'PatchDevicePoint');
      assert.equal(call[1][2].kind, kind);
      assert.equal(call[1][3].feedback, '2');
      assert.deepEqual([...call[1][4]], ['feedback']);
    } finally {
      await h.close();
    }
  }
});

test('point selections normalize mixed ranges and reject invalid or oversized input', async () => {
  const h = await setup();
  try {
    const selection = h.w.pointSelection;
    assert.deepEqual([...selection.parse('8，3 - 5,1，4')], [1, 3, 4, 5, 8]);
    assert.equal(selection.format(selection.parse('8，3-5，1')), '1，3-5，8');
    for (const input of ['1，', '-1', '5-3', '0x10', '10H', '1.5', '1e2', '65536', '0-10000'])
      assert.throws(() => selection.parse(input, 65535), undefined, input);
    h.$('read-start').value = '1，3-';
    h.$('read-modbus').click();
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'ReadDeviceModbusPoints'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('device first point and count drafts survive protocol switches', async () => {
  const h = await setup();
  try {
    h.$('add-device').click();
    originalField(h, 'txtYX_Add').value = '3';
    originalField(h, 'txtYX_Num').value = '5';
    originalField(h, 'txtYX_Num').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    originalField(h, 'txtYX_Add').value = '2';
    originalField(h, 'txtYX_Num').value = '6';
    originalField(h, 'txtYX_Num').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    chooseDeviceProtocol(h, 'iec104', '标准版');
    assert.equal(originalField(h, 'txtYX_Add').value, '3');
    assert.equal(originalField(h, 'txtYX_Num').value, '5');
    originalField(h, 'txtYC_Num').value = '0';
    const config = h.w.eval('configurationFromPanel')('iec104');
    assert.equal(config.ranges[0].count, 5);
    assert.equal(config.ranges[0].address, 3);
    assert.equal(config.values.txtYX_Num, '5');
    assert.equal(config.values.txtYX_Add, '3');
    assert.equal(config.ranges[1].count, 0);
  } finally {
    await h.close();
  }
});

test('sparse timed report defaults and legacy wide ranges retain high point numbers', async () => {
  for (const legacy of [false, true]) {
    const c = dataServer();
    c.devices[0].points = [
      { kind: 'yx', address: 1, value: 0 },
      { kind: 'yx', address: 3, value: 0 },
      { kind: 'yx', address: 4, value: 0 },
      { kind: 'yx', address: 100000, value: 0 },
    ];
    let saved;
    const view = {
      ...dataState(),
      config: {
        stopOnDisconnect: true,
        rules: legacy
          ? [
              {
                enabled: true,
                kind: 'yx',
                start: 0,
                end: 16777215,
                mode: 'report',
                intervalMs: 200,
              },
            ]
          : [],
      },
    };
    const h = await setup({ project: { name: 'sparse', channels: [c] }, states: [] }, null, {
      GetDataTest: () => structuredClone(view),
      SaveDataTest: (ch, d, scope, cfg) => {
        saved = structuredClone(cfg);
        return { ...view, config: cfg };
      },
    });
    try {
      h.w.document.querySelector('[data-mode=server]').click();
      h.w.document.querySelector('[data-point-kind=yx]').click();
      h.$('open-auto-data').click();
      await settleInline();
      const row = h.$('auto-data-rows').firstElementChild;
      assert.equal(row.querySelector('[data-data-field=points]').value, '1，3-4，100000');
      assert.equal(row.querySelector('[data-data-field=end]'), null);
      h.$('auto-data-save').click();
      await settleInline();
      if (legacy) {
        assert.equal(saved, undefined);
        assert.equal(view.config.rules[0].end, 16777215);
      } else {
        assert.equal(saved.rules[0].points, '1，3-4，100000');
        assert.equal(saved.rules[0].end, 100000);
      }
    } finally {
      await h.close();
    }
  }
});

test('batch quality checkbox precedes its label and applies to a mixed point selection', async () => {
  const c = dataServer();
  const h = await setup({ project: { name: 'quality', channels: [c] }, states: [] }, null, {
    SetTelemetryRange: () => ({ points: 5 }),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('open-telemetry-values').click();
    await settleInline();
    const label = h.$('telemetry-quality-enabled').closest('label');
    assert.equal(label.firstElementChild, h.$('telemetry-quality-enabled'));
    assert.equal(label.textContent, '品质');
    assert.equal(h.$('telemetry-values-form').textContent.includes('修改品质'), false);
    assert.equal(h.$('telemetry-start'), null);
    assert.equal(h.$('telemetry-end'), null);
    assert.equal(h.$('telemetry-quality').disabled, true);
    h.$('telemetry-points').value = '1，3-5，8';
    h.$('telemetry-value').value = '0';
    h.$('telemetry-quality-enabled').click();
    h.$('telemetry-quality').value = '128';
    h.$('telemetry-values-form').requestSubmit();
    await settleInline();
    const edit = h.calls.find((c) => c[0] === 'SetTelemetryRange')[1][2];
    assert.equal(edit.points, '1，3-5，8');
    assert.equal(edit.quality, 128);
  } finally {
    await h.close();
  }
});

test('control mappings accept mixed source and target point sets', async () => {
  const c = dataServer();
  c.devices[0].points = [
    ...[1, 3, 4].map((address) => ({ kind: 'yx', address, value: 0 })),
    ...[100, 103, 104].map((address) => ({ kind: 'yk', address, value: 0 })),
    ...[10, 13, 14].map((address) => ({ kind: 'yc', address, value: 0 })),
  ];
  let mappings;
  const h = await setup(
    { project: { name: 'mixed-associations', channels: [c] }, states: [] },
    null,
    {
      GetControlSimulation: () => ({ mode: 'normal', delayMs: 0, mappings: [] }),
      SaveControlSimulation: (ch, d, cat, cfg) => {
        mappings = cfg.mappings;
      },
    },
  );
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yk]').click();
    h.$('open-remote-config').click();
    await settleInline();
    h.$('remote-mapping-add').click();
    let row = h.$('remote-mappings').firstElementChild;
    row.querySelector('[data-remote-field=source]').value = '100，103-104';
    row.querySelector('[data-remote-field=target]').value = '1，3-4';
    h.$('remote-config-form').requestSubmit();
    await settleInline();
    assert.equal(mappings.length, 9);
    assert.deepEqual([...new Set(mappings.map((m) => m.source.address))], [100, 103, 104]);
    assert.deepEqual([...new Set(mappings.map((m) => m.target.address))], [1, 3, 4]);
  } finally {
    await h.close();
  }
});

test('multiple manual controls require every selection confirmation and allow cancelling confirmed points', async () => {
  const context = {
    kind: 'control-dialog',
    role: 'client',
    channel: 'a',
    device: 'device-1',
    point: 'yk',
    control: { type: 45, address: 1, value: 1 },
  };
  const h = await setup({ project: { name: 'mixed-controls', channels: [iecChannel()] } }, context);
  try {
    h.$('control-address').value = '1，3-4，8';
    h.$('control-address').dispatchEvent(new h.w.Event('input', { bubbles: true }));
    h.overrides.Command104Device = (ch, d, request) => {
      if (request.address === 4 && request.operation === 'select') throw Error('选择被拒绝');
      return { message: request.operation === 'cancel' ? '撤销确认' : '选择确认' };
    };
    h.$('control-select').click();
    await settleInline();
    assert.equal(h.$('control-execute').disabled, true);
    assert.equal(h.$('control-cancel').disabled, false);
    assert.match(h.$('control-result').textContent, /已确认2个点位/);
    h.$('control-cancel').click();
    await settleInline();
    assert.deepEqual(
      h.calls
        .filter((c) => c[0] === 'Command104Device' && c[1][2].operation === 'cancel')
        .map((c) => c[1][2].address),
      [1, 3],
    );
    assert.equal(h.$('control-cancel').disabled, true);
    h.overrides.Command104Device = () => ({ message: '选择确认' });
    h.$('control-select').click();
    await settleInline();
    assert.equal(h.$('control-execute').disabled, false);
    h.overrides.Command104Device = () => ({ message: '执行结束' });
    h.$('control-execute').click();
    await settleInline();
    assert.deepEqual(
      h.calls
        .filter((c) => c[0] === 'Command104Device' && c[1][2].operation === 'execute')
        .map((c) => c[1][2].address),
      [1, 3, 4, 8],
    );
  } finally {
    await h.close();
  }
});

test('automatic telemetry defaults fit all IEC104 formats including untagged legacy points', async () => {
  for (const type of [9, 11, 13, 21, 34, 35, 36]) {
    const c = dataServer();
    c.devices[0].configuration = {
      version: '标准版',
      values: { cbYC_Type: 'type:' + type + ')' },
      ranges: [],
    };
    let saved;
    const h = await setup(
      { project: { name: 'defaults', channels: [c] }, states: [] },
      {
        kind: 'auto-data-dialog',
        role: 'server',
        channel: c.id,
        device: 'target',
        pointKind: 'yc',
      },
      {
        GetDataTest: () => dataState(),
        SaveDataTest: (ch, d, scope, cfg) => {
          saved = cfg;
          return { ...dataState(), config: cfg };
        },
      },
    );
    try {
      await settleInline();
      const row = h.$('auto-data-rows').firstElementChild,
        get = (k) => row.querySelector('[data-data-field=' + k + ']'),
        normalized = [9, 21, 34].includes(type);
      assert.equal(get('min').value, '0');
      assert.equal(Number(get('max').value), normalized ? 0.9 : 100);
      assert.equal(Number(get('step').value), normalized ? 0.1 : 1);
      assert.equal(get('min').getAttribute('aria-label'), '下限');
      assert.equal(h.$('auto-data-dialog').querySelectorAll('th')[5].textContent, '下限');
      assert.equal(h.$('auto-data-start'), null);
      h.$('auto-data-save').click();
      await settleInline();
      assert.ok(saved, 'default format ' + type + ' did not save');
      assert.equal(
        h.calls.some((c) => c[0] === 'StartDataTest'),
        false,
      );
    } finally {
      await h.close();
    }
  }
});

test('normalized limit errors identify the field and point before sending configuration', async () => {
  const c = dataServer();
  c.devices[0].configuration = {
    version: '标准版',
    values: { cbYC_Type: '带品质归一化(type:9)' },
    ranges: [],
  };
  const h = await setup(
    { project: { name: 'normalized', channels: [c] }, states: [] },
    { kind: 'auto-data-dialog', role: 'server', channel: c.id, device: 'target', pointKind: 'yc' },
    {
      GetDataTest: () => dataState(),
      SaveDataTest: (ch, d, scope, cfg) => ({ ...dataState(), config: cfg }),
    },
  );
  try {
    await settleInline();
    const max = h.$('auto-data-rows').querySelector('[data-data-field=max]');
    max.value = '1';
    h.$('auto-data-save').click();
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'SaveDataTest'),
      false,
    );
    assert.match(
      h.$('auto-data-form').querySelector('.form-error').textContent,
      /上限.*点位 10.*归一化/,
    );
    assert.equal(h.w.document.activeElement, max);
    max.value = '0.8';
    h.$('auto-data-save').click();
    await settleInline();
    assert.ok(h.calls.some((c) => c[0] === 'SaveDataTest'));
  } finally {
    await h.close();
  }
});

test('inline and batch normalized values reject one while keeping online value operations available', async () => {
  const c = dataServer();
  c.devices[0].configuration = { version: '标准版', values: { cbYC_Type: '21' }, ranges: [] };
  const h = await setup(
    {
      project: { name: 'normalized-values', channels: [c] },
      states: [{ id: c.id, status: 'connected', peers: 1 }],
    },
    null,
    {
      SetTelemetryRange: () => ({ points: 2, receivers: 1 }),
      SetPointValue: () => ({ mode: 'reported' }),
    },
  );
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    const input = h.$('point-rows').querySelector('[data-point-field=value]');
    editInline(h, input, '1');
    inlineKey(h, input, 'Enter');
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'SetPointValue'),
      false,
    );
    assert.match(input.getAttribute('aria-description'), /归一化/);
    editInline(h, input, '0.5');
    inlineKey(h, input, 'Enter');
    await settleInline();
    assert.ok(h.calls.some((c) => c[0] === 'SetPointValue'));
    h.$('open-telemetry-values').click();
    await settleInline();
    assert.equal(h.$('telemetry-value').value, '');
    h.$('telemetry-value').value = '1';
    h.$('telemetry-values-form').requestSubmit();
    await settleInline();
    assert.equal(
      h.calls.some((c) => c[0] === 'SetTelemetryRange'),
      false,
    );
    assert.match(h.$('telemetry-values-form').querySelector('.form-error').textContent, /归一化/);
    h.$('telemetry-value').value = '0.5';
    h.$('telemetry-values-form').requestSubmit();
    await settleInline();
    assert.ok(h.calls.some((c) => c[0] === 'SetTelemetryRange'));
    assert.equal(
      h.calls.some((c) => c[0] === 'StopChannel'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('empty point tables do not prefill an unconfigured point zero', async () => {
  const c = dataServer();
  c.devices[0].points = [];
  c.devices[0].configuration = { version: '标准版', values: { cbYC_Type: '9' }, ranges: [] };
  const h = await setup({ project: { name: 'empty-points', channels: [c] }, states: [] }, null, {
    GetDataTest: () => dataState(),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    h.$('open-auto-data').click();
    await settleInline();
    const row = h.$('auto-data-rows').firstElementChild;
    assert.equal(row.querySelector('[data-data-field=points]').value, '');
    assert.equal(row.querySelector('[data-data-field=enabled]').checked, false);
    assert.equal(row.querySelector('[data-data-field=max]').value, '0.9');
    h.$('auto-data-dialog').close();
  } finally {
    await h.close();
  }
  const client = iecChannel('a');
  const h2 = await setup({ project: { name: 'empty-client', channels: [client] } });
  try {
    h2.w.document.querySelector('[data-point-kind=yt]').click();
    assert.equal(h2.$('auto-yt-addresses').value, '');
    h2.$('open-yt').click();
    assert.equal(h2.$('control-address').value, '');
    assert.equal(h2.$('control-execute').disabled, true);
  } finally {
    await h2.close();
  }
});

test('all Modbus formats keep valid first-point and count defaults', async () => {
  for (const role of ['client', 'server'])
    for (const version of ['TCP版', '标准版(RTU)']) {
      const c = {
          ...channel('a'),
          role,
          transport: role === 'client' ? 'tcp-client' : 'tcp-server',
          devices: [],
        },
        h = await setup({ project: { name: 'device-defaults', channels: [c] }, states: [] });
      try {
        h.w.document.querySelector('[data-mode=' + role + ']').click();
        h.$('add-device').click();
        chooseDeviceProtocol(h, 'modbus', version);
        const format = originalField(h, role === 'client' ? 'cb_ycFormat' : 'cb_ycType');
        for (const option of format.options) {
          format.value = option.value;
          format.dispatchEvent(new h.w.Event('change', { bubbles: true }));
          const cfg = h.w.eval('configurationFromPanel')(
            version === 'TCP版' ? 'modbus-tcp' : 'modbus-rtu',
          );
          assert.deepEqual(
            [...cfg.ranges].map((r) => r.count),
            [50, 50, 50, 50],
          );
          assert.equal(cfg.ranges[1].address, 1024);
          assert.equal(cfg.ranges[1].selection, '');
        }
      } finally {
        await h.close();
      }
    }
});

test('main four-remote pages toggle saved automatic controls with one start/end button', async () => {
  for (const protocol of ['iec104', 'modbus-tcp'])
    for (const category of ['yk', 'yt']) {
      const points =
          protocol === 'iec104'
            ? [
                { kind: 'yk', address: 100, value: 0 },
                { kind: 'yt', address: 200, value: 0 },
              ]
            : [
                { kind: 'coil', tab: 'yk', address: 100, value: 0 },
                { kind: 'holding', tab: 'yt', address: 200, value: 0 },
              ],
        c = { ...channel('a', points), protocol },
        view = { ...autoSnapshot(category), saved: true },
        change = (status) => {
          view.status = status;
          return structuredClone(view);
        };
      const h = await setup({ project: { name: 'runtime', channels: [c] } }, null, {
        GetAutoControl: () => structuredClone(view),
        StartAutoControl: () => change('running'),
        PauseAutoControl: () => change('paused'),
        ResumeAutoControl: () => change('running'),
        StopAutoControl: () => change('stopped'),
      });
      try {
        h.w.document.querySelector('[data-point-kind=' + category + ']').click();
        await h.refresh();
        assert.equal(h.$('runtime-auto-run').disabled, false);
        h.$('runtime-auto-run').click();
        await settleInline();
        assert.equal(view.status, 'running');
        assert.match(h.$('runtime-auto-run').textContent, /结束/);
        assert.equal(h.$('runtime-auto-stop'), null);
        h.$('runtime-auto-run').click();
        await settleInline();
        assert.equal(view.status, 'stopped');
        assert.match(h.$('runtime-auto-run').textContent, /开始/);
        view.status = 'paused';
        await h.refresh();
        assert.match(h.$('runtime-auto-run').textContent, /结束/);
        h.$('runtime-auto-run').click();
        await settleInline();
        assert.equal(view.status, 'stopped');
        assert.equal(
          h.calls.some((c) => ['PauseAutoControl', 'ResumeAutoControl'].includes(c[0])),
          false,
        );
        assert.equal(
          h.calls.some((c) =>
            [
              'UpdateAutoOptions',
              'UpdateAutoRows',
              'SaveAutoControlDraft',
              'PrepareAutoControl',
            ].includes(c[0]),
          ),
          false,
        );
      } finally {
        await h.close();
      }
    }
});

test('automatic control save disconnects and main start waits for manual connection', async () => {
  const c = iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }]),
    live = { ...autoSnapshot('yk'), saved: false },
    draft = structuredClone(live);
  let h;
  const copy = () => structuredClone(live),
    overrides = {
      GetAutoControl: copy,
      GetAutoControlDraft: () => structuredClone(draft),
      PrepareAutoControl: () => structuredClone(draft),
      UpdateAutoOptions: (ch, d, m, o) => {
        draft.options = o;
        return structuredClone(draft);
      },
      UpdateAutoRows: (ch, d, m, rows) => {
        for (const r of rows) Object.assign(draft.rows[r.index], r);
        return structuredClone(draft);
      },
      SaveAutoControlDraft: () => {
        Object.assign(live, structuredClone(draft), { saved: true, status: 'idle' });
        h.state.states[0].status = 'stopped';
        h.state.states[0].peers = 0;
        return copy();
      },
      StartChannel: () => {
        h.state.states[0].status = 'connected';
        h.state.states[0].peers = 1;
      },
      StartAutoControl: () => {
        live.status = 'running';
        return copy();
      },
    };
  h = await setup({ project: { name: 'save-connect-run', channels: [c] } }, null, overrides);
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    await h.refresh();
    assert.equal(h.$('runtime-auto-run').disabled, true);
    h.$('open-auto-yk').click();
    await settleInline();
    assert.equal(h.$('auto-yk-start'), null);
    h.$('auto-yk-form').requestSubmit(h.$('auto-yk-save'));
    await settleInline();
    assert.equal(h.state.states[0].status, 'stopped');
    assert.equal(h.$('runtime-auto-run').disabled, true);
    assert.equal(
      h.calls.some((c) => c[0] === 'StartAutoControl'),
      false,
    );
    h.$('toggle-channel').click();
    await settleInline();
    assert.equal(h.$('runtime-auto-run').disabled, false);
    h.$('runtime-auto-run').click();
    await settleInline();
    assert.equal(live.status, 'running');
    assert.equal(h.calls.filter((c) => c[0] === 'SaveAutoControlDraft').length, 1);
  } finally {
    await h.close();
  }
});

test('104 automatic and manual controls wait for STARTDT after TCP connects', async () => {
  const c = iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }]),
    v = { ...autoSnapshot('yk'), saved: true };
  const h = await setup(
    {
      project: { name: 'activation', channels: [c] },
      states: [{ id: 'a', status: 'connected', peers: 1, readyPeers: 0 }],
    },
    null,
    { GetAutoControl: () => structuredClone(v) },
  );
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    await h.refresh();
    assert.equal(h.$('runtime-auto-run').disabled, true);
    assert.match(h.$('runtime-auto-run').getAttribute('aria-description'), /激活/);
    assert.match(h.$('channel-meta').textContent, /等待激活/);
    h.$('open-yk').click();
    h.$('control-direct').checked = true;
    h.$('control-direct').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(h.$('control-execute').disabled, true);
    h.state.states[0].readyPeers = 1;
    await h.refresh();
    assert.equal(h.$('runtime-auto-run').disabled, false);
    assert.equal(h.$('control-execute').disabled, false);
    assert.equal(
      h.calls.some((c) => c[0] === 'StartAutoControl' || c[0] === 'Command104Device'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('104 data tasks and publish wait for active transfer while configuration stays editable', async () => {
  const c = dataServer(),
    view = {
      ...dataState(),
      config: {
        rules: [
          {
            enabled: true,
            kind: 'yc',
            points: '10-11',
            mode: 'increment',
            min: 0,
            max: 100,
            step: 1,
            intervalMs: 1000,
          },
        ],
        stopOnDisconnect: true,
      },
    };
  const h = await setup(
    {
      project: { name: 'activation-server', channels: [c] },
      states: [{ id: c.id, status: 'connected', peers: 1, readyPeers: 0 }],
    },
    null,
    { GetDataTest: () => structuredClone(view) },
  );
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    await h.refresh();
    assert.equal(h.$('publish').disabled, true);
    assert.equal(h.$('runtime-data-run').disabled, true);
    assert.equal(h.$('open-auto-data').disabled, false);
    h.state.states[0].readyPeers = 1;
    await h.refresh();
    assert.equal(h.$('publish').disabled, false);
    assert.equal(h.$('runtime-data-run').disabled, false);
  } finally {
    await h.close();
  }
});

test('server command pages show bound telemetry and keep received values read-only', async () => {
  const c = dataServer();
  c.devices[0].points.push(
    { kind: 'yk', address: 100, value: 1 },
    { kind: 'yt', address: 200, value: 42.5 },
  );
  c.devices[0].configuration = {
    controls: {
      yk: {
        mode: 'normal',
        mappings: [{ source: { kind: 'yk', address: 100 }, target: { kind: 'yx', address: 1 } }],
      },
      yt: {
        mode: 'normal',
        mappings: [{ source: { kind: 'yt', address: 200 }, target: { kind: 'yc', address: 10 } }],
      },
    },
  };
  const h = await setup({ project: { name: 'binding', channels: [c] }, states: [] }, null, {
    GetControlSimulation: (ch, d, category) =>
      structuredClone(c.devices[0].configuration.controls[category]),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    for (const [category, target] of [
      ['yk', '1'],
      ['yt', '10'],
    ]) {
      h.w.document.querySelector('[data-point-kind=' + category + ']').click();
      const row = h.$('point-rows').firstElementChild;
      assert.equal(row.querySelector('[data-point-field=value]'), null);
      assert.equal(row.querySelector('[data-point-field=hex]'), null);
      assert.equal(row.querySelector('[data-point-field=quality]'), null);
      assert.equal(row.children.length, 4);
      assert.deepEqual(
        [...h.w.document.querySelectorAll('.point-table th')].map((e) => e.textContent),
        ['点位', category === 'yk' ? '绑定遥信' : '绑定遥测', '下发值', '描述'],
      );
      assert.equal(row.children[1].textContent, target);
      row.children[1].querySelector('button').click();
      await settleInline();
      assert.equal(h.$('remote-config-dialog').open, true);
      assert.equal(
        h.$('remote-mappings').firstElementChild.querySelector('[data-remote-field=target]').value,
        target,
      );
      h.$('remote-config-dialog').close();
      c.devices[0].configuration.controls[category].mappings = [];
      await h.refresh();
      const blank = h.$('point-rows').firstElementChild.children[1];
      assert.equal(blank.textContent, '');
      assert.ok(blank.querySelector('button').getAttribute('aria-label'));
      blank.querySelector('button').click();
      await settleInline();
      assert.equal(h.$('remote-config-dialog').open, true);
      assert.equal(
        h.$('remote-mappings').firstElementChild.querySelector('[data-remote-field=target]').value,
        '',
      );
      h.$('remote-config-dialog').close();
    }
    assert.equal(
      h.calls.some((c) => ['SetPointValue', 'Command104Device', 'EditPointCell'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});

test('client control tables retain point value and description without telemetry-only fields', async () => {
  const c = iecChannel('a', [
    { kind: 'yk', address: 24577, value: 0, name: 'switch' },
    { kind: 'yt', address: 26113, value: 0, name: 'setpoint' },
  ]);
  const h = await setup({ project: { name: 'compact', channels: [c] } });
  try {
    for (const category of ['yk', 'yt']) {
      h.w.document.querySelector('[data-point-kind=' + category + ']').click();
      const row = h.$('point-rows').firstElementChild;
      assert.equal(row.children.length, 3);
      assert.equal(row.querySelector('[data-point-field=hex]'), null);
      assert.equal(row.querySelector('[data-point-field=quality]'), null);
      assert.ok(row.querySelector('[data-point-field=value]'));
      assert.deepEqual(
        [...h.w.document.querySelectorAll('.point-table th')].map((e) => e.textContent),
        ['点位', '值 / 状态', '描述'],
      );
    }
  } finally {
    await h.close();
  }
});

test('filtered batch values include all matching pages and display the affected count', async () => {
  const c = dataServer();
  c.devices[0].points = Array.from({ length: 160 }, (_, i) => ({
    kind: 'yc',
    address: i + 1,
    name: i < 125 ? '匹配' : '其他',
    value: 0,
    quality: 0,
  }));
  const h = await setup({ project: { name: 'batch', channels: [c] } }, null, {});
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    h.$('point-search').value = '匹配';
    h.$('point-search').dispatchEvent(new h.w.Event('input'));
    assert.equal(h.$('point-rows').children.length, 100);
    h.$('open-telemetry-values').click();
    await settleInline();
    assert.equal(h.$('telemetry-points').value, '1-125');
    assert.match(h.$('telemetry-values-form').querySelector('[type=submit]').textContent, /125/);
    h.$('telemetry-points').value = '999';
    h.$('telemetry-points').dispatchEvent(new h.w.Event('input'));
    assert.equal(h.$('telemetry-values-form').querySelector('[type=submit]').disabled, true);
  } finally {
    await h.close();
  }
});
test('automatic data add copy and type switch preserve incomplete compatible drafts', async () => {
  const h = await setupData();
  try {
    const first = h.$('auto-data-rows').children[0];
    first.querySelector('[data-data-field=intervalMs]').value = '';
    first.querySelector('[data-data-field=points]').value = '10，';
    h.$('auto-data-add').click();
    assert.equal(h.$('auto-data-rows').children.length, 3);
    assert.equal(
      h.$('auto-data-rows').children[0].querySelector('[data-data-field=intervalMs]').value,
      '',
    );
    assert.equal(
      h.$('auto-data-rows').children[0].querySelector('[data-data-field=points]').value,
      '10，',
    );
    h.$('auto-data-rows').querySelector('[data-data-copy]').click();
    assert.equal(h.$('auto-data-rows').children.length, 4);
    assert.equal(
      h.$('auto-data-rows').children[1].querySelector('[data-data-field=points]').value,
      '',
    );
    const row = h.$('auto-data-rows').children[0];
    row.querySelector('[data-data-field=intervalMs]').value = '1200';
    const type = row.querySelector('[data-data-field=kind]');
    type.value = 'yx';
    type.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    assert.equal(
      h.$('auto-data-rows').children[0].querySelector('[data-data-field=intervalMs]').value,
      '1200',
    );
    assert.equal(h.$('auto-data-dialog').textContent.includes('周期数'), false);
    assert.equal(h.$('auto-data-status'), null);
  } finally {
    await h.close();
  }
});
test('binding buttons retain focus across unrelated changes and target their own source point', async () => {
  const c = dataServer();
  c.devices[0].points.push(
    { kind: 'yk', address: 24577, value: 0 },
    { kind: 'yk', address: 24578, value: 0 },
  );
  const h = await setup({ project: { name: 'bindings', channels: [c] } }, null, {
    GetControlSimulation: () => ({ mode: 'normal', delayMs: 0, mappings: [] }),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yk]').click();
    const button = h.$('point-rows').children[1].querySelector('button');
    assert.equal(button.textContent, '');
    assert.equal(button.getAttribute('aria-label'), '绑定遥信 24578');
    button.focus();
    h.state.project.channels[0].devices[0].points[0].value = 3;
    await h.refresh();
    assert.equal(h.w.document.activeElement, button);
    button.click();
    await settleInline();
    assert.equal(h.$('remote-mappings').querySelector('[data-remote-field=source]').value, '24578');
    assert.equal(h.$('remote-mappings').querySelector('[data-remote-field=target]').value, '');
    h.$('remote-mapping-add').click();
    h.$('remote-mapping-add').click();
    assert.equal(h.$('remote-mappings').children.length, 3);
    assert.equal(h.$('remote-config-form').querySelector('.form-error').hidden, true);
  } finally {
    await h.close();
  }
});

test('per-device point category search pagination and scroll restore independently', async () => {
  const c = multiChannel();
  for (const d of c.devices)
    d.points = Array.from({ length: 140 }, (_, i) => ({
      kind: 'holding',
      tab: 'yc',
      address: i,
      name: 'value',
      value: 0,
      quality: 0,
    }));
  const h = await setup({ project: { name: 'contexts', channels: [c] } });
  try {
    h.w.document.querySelector('[data-point-kind=yc]').click();
    h.$('point-search').value = 'value';
    h.$('point-search').dispatchEvent(new h.w.Event('input'));
    h.$('point-next').click();
    h.$('point-scroll').scrollTop = 80;
    const [a, b] = c.devices;
    h.w.document.querySelector('[data-device="' + b.id + '"]').click();
    h.$('point-search').value = '1';
    h.w.document.querySelector('[data-device="' + a.id + '"]').click();
    await tick();
    assert.equal(h.$('point-search').value, 'value');
    assert.equal(h.$('point-page').textContent, '2 / 2');
    assert.equal(h.$('point-kind').value.replace('tab:', ''), 'yc');
  } finally {
    await h.close();
  }
});
test('incremental polling merges changed points and resets cleared logs without full snapshots', async () => {
  const c = dataServer();
  let phase = 0;
  const h = await setup({}, null, {
    GetView: (req) => {
      const base = {
        metadata: 1,
        generation: 1,
        name: 'incremental',
        path: '',
        dirty: false,
        states: [],
        channel: 'data',
        device: 'target',
        pointIdentity: 'data/target',
        pointVersion: phase ? 2 : 1,
        messageEpoch: phase === 2 ? 2 : 1,
        messageSequence: phase ? 2 : 1,
        eventEpoch: 1,
        eventSequence: 0,
        firstMessage: phase === 2 ? 0 : 1,
        firstEvent: 0,
      };
      return {
        ...base,
        ...(!req.metadata
          ? {
              project: {
                name: 'incremental',
                channels: [{ ...c, role: 'client', devices: [{ ...c.devices[0], points: null }] }],
              },
            }
          : {}),
        fullPoints: !req.points,
        points: !req.points
          ? c.devices[0].points
          : phase === 1 && req.points === 1
            ? [{ ...c.devices[0].points[0], value: 7 }]
            : [],
        resetMessages: req.messageEpoch !== base.messageEpoch,
        messages:
          phase === 2
            ? []
            : !req.messages
              ? [{ sequence: 1, channelId: 'data', direction: 'TX', hex: '01', summary: 'first' }]
              : phase === 1 && req.messages === 1
                ? [{ sequence: 2, channelId: 'data', direction: 'RX', hex: '02', summary: 'next' }]
                : [],
      };
    },
  });
  try {
    assert.equal(
      h.calls.some((c) => c[0] === 'GetSnapshot'),
      false,
    );
    h.w.document.querySelector('[data-point-kind=yc]').click();
    phase = 1;
    await h.refresh();
    assert.equal(
      h.$('point-rows').children[0].querySelector('[data-point-field=value]').value,
      '7',
    );
    assert.match(h.$('message-rows').textContent, /first/);
    assert.match(h.$('message-rows').textContent, /next/);
    phase = 2;
    await h.refresh();
    assert.equal(h.$('message-rows').children.length, 0);
  } finally {
    await h.close();
  }
});

test('same-ID project replacement clears search category page and old runtime context', async () => {
  const c = dataServer();
  let generation = 1;
  const h = await setup({}, null, {
    GetView: (req) => ({
      metadata: generation,
      generation,
      name: 'replacement',
      states: [],
      channel: 'data',
      device: 'target',
      pointIdentity: 'data/target',
      pointVersion: generation,
      messageEpoch: generation,
      eventEpoch: generation,
      messageSequence: 0,
      eventSequence: 0,
      resetMessages: true,
      resetEvents: true,
      firstMessage: 0,
      firstEvent: 0,
      fullPoints: true,
      points: c.devices[0].points,
      ...(req.metadata !== generation
        ? {
            project: {
              name: 'replacement',
              channels: [{ ...c, role: 'client', devices: [{ ...c.devices[0], points: null }] }],
            },
          }
        : {}),
    }),
  });
  try {
    h.w.document.querySelector('[data-point-kind=yc]').click();
    h.$('point-search').value = 'unmatched';
    h.$('point-search').dispatchEvent(new h.w.Event('input'));
    assert.equal(h.$('point-rows').children.length, 0);
    generation = 2;
    await h.refresh();
    assert.equal(h.$('point-search').value, '');
    assert.equal(h.$('point-kind').value, 'yx');
    assert.equal(h.$('point-rows').children.length, 1);
  } finally {
    await h.close();
  }
});
test('unchanged automatic configuration closes without save or reconnection side effects', async () => {
  const c = dataServer(),
    view = {
      ...dataState(),
      config: {
        rules: [
          {
            enabled: true,
            kind: 'yc',
            points: '10-11',
            intervalMs: 1000,
            mode: 'fixed',
            min: 1,
            max: 1,
            step: 1,
          },
        ],
        stopOnDisconnect: true,
      },
    };
  const h = await setup({ project: { name: 'noop', channels: [c] } }, null, {
    GetDataTest: () => structuredClone(view),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.$('open-auto-data').click();
    await settleInline();
    h.$('auto-data-save').click();
    await settleInline();
    assert.equal(h.$('auto-data-dialog').open, false);
    assert.equal(
      h.calls.some((c) => ['SaveDataTest', 'StopChannel', 'StartDataTest'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});

test('automatic data keeps one run toggle without status or point-update buttons in both telemetry tabs', async () => {
  for (const protocol of ['iec104', 'modbus-tcp', 'modbus-rtu']) {
    const h = await setupData(protocol);
    try {
      h.view.config.rules = [
        {
          enabled: true,
          kind: protocol === 'iec104' ? 'yc' : 'holding',
          points: protocol === 'iec104' ? '10' : '0',
          intervalMs: 1000,
          mode: 'fixed',
          min: 0,
          max: 0,
          step: 1,
        },
      ];
      for (const kind of ['yx', 'yc']) {
        h.w.document.querySelector('[data-point-kind=' + kind + ']').click();
        await h.refresh();
        assert.equal(h.$('runtime-data-status'), null);
        assert.equal(h.w.document.querySelectorAll('.runtime-actions button').length, 1);
        assert.ok(h.$('open-auto-data'));
      }
      h.$('runtime-data-run').click();
      await settleInline();
      assert.equal(h.calls.filter((c) => c[0] === 'StartDataTest').length, 1);
      assert.match(h.$('runtime-data-run').textContent, /关闭/);
      assert.equal(h.$('runtime-data-status'), null);
      h.view.updates = 8;
      await h.refresh();
      assert.equal(h.$('runtime-data-status'), null);
      assert.doesNotMatch(h.$('protocol-actions').textContent, /更新.*点次|运行中/);
      h.$('runtime-data-run').click();
      await settleInline();
      assert.equal(h.calls.filter((c) => c[0] === 'SetDataTestState').length, 1);
      assert.equal(h.calls.find((c) => c[0] === 'SetDataTestState')[1][3], 'stop');
      assert.match(h.$('runtime-data-run').textContent, /开启/);
      h.view.lastError = '链路发送失败';
      h.state.messages.push({
        sequence: 1,
        channelId: 'data',
        direction: 'SYS',
        summary: '自动数据发送失败：链路发送失败',
        hex: '',
        time: '12:00:00',
      });
      await h.refresh();
      assert.equal(h.$('runtime-data-status'), null);
      assert.match(h.$('message-rows').textContent, /链路发送失败/);
    } finally {
      await h.close();
    }
  }
});

test('channel manager has one batch toggle for the current list including mixed states', async () => {
  const clientA = channel('a'),
    clientB = channel('b'),
    server = { ...channel('s'), role: 'server' };
  let h;
  h = await setup(
    {
      project: { name: 'batch-switch', channels: [clientA, clientB, server] },
      states: [
        { id: 'a', status: 'stopped' },
        { id: 'b', status: 'stopped' },
        { id: 's', status: 'listening' },
      ],
    },
    null,
    {
      StartChannel: (id) => {
        h.state.states.find((s) => s.id === id).status = 'connected';
      },
      StopChannel: (id) => {
        h.state.states.find((s) => s.id === id).status = 'stopped';
      },
    },
  );
  try {
    h.$('communication-manager').click();
    await settleInline();
    const toggle = h.$('toggle-all-channels');
    assert.equal(h.$('connect-all'), null);
    assert.equal(h.$('stop-all'), null);
    assert.equal(toggle.textContent, '全部连接');
    assert.equal(toggle.getAttribute('aria-pressed'), 'false');
    toggle.click();
    await settleInline();
    assert.equal(toggle.textContent, '全部停止');
    assert.deepEqual(
      h.calls.filter((c) => c[0] === 'StartChannel').map((c) => c[1][0]),
      ['a', 'b'],
    );
    assert.equal(h.state.states[2].status, 'listening');
    h.state.states[1].status = 'stopped';
    await h.refresh();
    assert.equal(toggle.textContent, '全部停止');
    toggle.click();
    await settleInline();
    assert.equal(toggle.textContent, '全部连接');
    assert.deepEqual(
      h.calls.filter((c) => c[0] === 'StopChannel').map((c) => c[1][0]),
      ['a', 'b'],
    );
    assert.equal(h.state.states[2].status, 'listening');
    const input = h.$('manager-rows').querySelector('[data-manager-field=address]');
    input.value = '127.0.0.1:1502';
    input.dispatchEvent(new h.w.Event('input', { bubbles: true }));
    assert.equal(toggle.disabled, true);
    input.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    assert.equal(toggle.disabled, false);
  } finally {
    await h.close();
  }
});
test('automatic control single toggle blocks repeat clicks and can stop a paused offline task', async () => {
  const c = iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }]),
    view = { ...autoSnapshot('yk'), saved: true };
  let release;
  const h = await setup({ project: { name: 'one-switch', channels: [c] } }, null, {
    GetAutoControl: () => structuredClone(view),
    StartAutoControl: () =>
      new Promise((resolve) => {
        release = () => {
          view.status = 'running';
          resolve(structuredClone(view));
        };
      }),
    StopAutoControl: () => {
      view.status = 'stopped';
      return structuredClone(view);
    },
  });
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    await h.refresh();
    const toggle = h.$('runtime-auto-run');
    toggle.click();
    await tick();
    assert.equal(toggle.disabled, true);
    toggle.click();
    assert.equal(h.calls.filter((c) => c[0] === 'StartAutoControl').length, 1);
    release();
    await settleInline();
    assert.equal(toggle.getAttribute('aria-pressed'), 'true');
    assert.equal(toggle.textContent, '结束遥控');
    view.status = 'paused';
    h.state.states[0].status = 'disconnected';
    await h.refresh();
    assert.equal(toggle.disabled, false);
    toggle.click();
    await settleInline();
    assert.equal(view.status, 'stopped');
    assert.equal(toggle.textContent, '开始遥控');
    assert.equal(toggle.disabled, true);
  } finally {
    await h.close();
  }
});

test('runtime details load bounded pages only while open and retain real outcomes', async () => {
  const state = {
    ...autoSnapshot(),
    saved: true,
    status: 'running',
    total: 3,
    success: 2,
    failure: 1,
    lastError: 'rejected',
  };
  const h = await setup(
    {
      project: {
        name: 'results',
        channels: [iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }])],
      },
    },
    null,
    {
      GetAutoControlStatus: () => structuredClone(state),
      GetAutoControlDetails: (_c, _d, _m, offset) => ({
        offset,
        rowCount: 201,
        current: 0,
        rows: [
          {
            address: 100 + offset,
            name: 'target',
            selection: '选择确认',
            execution: '执行结束',
            feedbackResult: '变位成功',
          },
        ],
        errors: ['substation denied'],
      }),
    },
  );
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    await h.refresh();
    assert.equal(
      h.calls.some((c) => c[0] === 'GetAutoControlDetails'),
      false,
    );
    const details = h.$('runtime-auto-details');
    details.open = true;
    details.dispatchEvent(new h.w.Event('toggle'));
    await settleAuto();
    assert.match(h.$('runtime-auto-results').textContent, /选择确认.*执行结束.*变位成功/);
    assert.match(h.$('runtime-auto-summary').textContent, /成功 2.*失败 1/);
    assert.match(h.$('runtime-auto-errors').textContent, /denied/);
    h.$('runtime-auto-status').click();
    assert.equal(h.$('message-options').hidden, false);
    assert.equal(h.$('message-search').value, 'rejected');
    h.$('runtime-results-next').click();
    await settleAuto();
    assert.equal(h.calls.filter((c) => c[0] === 'GetAutoControlDetails').at(-1)[1][3], 100);
    assert.match(h.$('runtime-results-page').textContent, /2 \/ 3/);
    details.open = false;
    details.dispatchEvent(new h.w.Event('toggle'));
    const count = h.calls.filter((c) => c[0] === 'GetAutoControlDetails').length;
    await h.refresh();
    assert.equal(h.calls.filter((c) => c[0] === 'GetAutoControlDetails').length, count);
    assert.equal(
      h.calls.some((c) =>
        ['StartAutoControl', 'UpdateAutoRows', 'SaveAutoControlDraft'].includes(c[0]),
      ),
      false,
    );
  } finally {
    await h.close();
  }
});

test('late runtime responses cannot overwrite another device or a new page', async () => {
  let resolve;
  const c = iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }]);
  const h = await setup({ project: { name: 'late', channels: [c] } }, null, {
    GetAutoControlStatus: () => ({ saved: true, status: 'idle', enabledRows: 1 }),
    GetAutoControlDetails: () =>
      new Promise((r) => {
        resolve = r;
      }),
  });
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    const details = h.$('runtime-auto-details');
    details.open = true;
    details.dispatchEvent(new h.w.Event('toggle'));
    await tick();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    resolve?.({
      rows: [{ address: 999, name: 'stale' }],
      errors: [],
      rowCount: 1,
      offset: 0,
      current: -1,
    });
    await settleAuto();
    assert.equal(h.$('runtime-auto-results'), null);
    assert.doesNotMatch(h.$('protocol-actions').textContent, /stale/);
  } finally {
    await h.close();
  }
});

test('automatic adjustment parameters live in the editor and fill before Save without running', async () => {
  const h = await setupAuto('yt');
  try {
    assert.equal(h.$('protocol-actions').querySelector('#auto-yt-addresses'), null);
    assert.ok(h.$('auto-yt-dialog').contains(h.$('auto-yt-addresses')));
    assert.equal(h.$('auto-yt-addresses').value, '100-101');
    h.overrides.PrepareAutoControl = (_c, _d, _m, seed) => {
      assert.equal(seed.addresses, '101');
      assert.equal(seed.value, '22');
      assert.equal(seed.replaceValues, true);
      h.data.rows = [{ ...h.data.rows[1], value: seed.value }];
      h.data.options.valueType = seed.valueType;
      h.data.generation = 2;
      h.data.revision++;
      return structuredClone(h.data);
    };
    h.$('auto-yt-addresses').value = '101';
    h.$('auto-yt-value').value = '22';
    h.$('auto-yt-type').value = '49';
    h.$('auto-yt-save').click();
    await settleAuto();
    assert.equal(h.data.rows.length, 1);
    assert.equal(h.data.rows[0].value, '22');
    assert.equal(h.data.options.valueType, 49);
    assert.ok(h.calls.some((c) => c[0] === 'SaveAutoControlDraft'));
    assert.equal(
      h.calls.some((c) => c[0] === 'StartAutoControl'),
      false,
    );
  } finally {
    await h.close();
  }
});

test('invalid adjustment point selection stays in the editor and does not save', async () => {
  const h = await setupAuto('yt');
  try {
    h.$('auto-yt-addresses').value = '1-';
    h.$('auto-yt-save').click();
    await settleAuto();
    assert.equal(
      h.calls.some((c) => c[0] === 'SaveAutoControlDraft'),
      false,
    );
    assert.equal(h.$('auto-yt-form').querySelector('.form-error').hidden, false);
    assert.equal(h.data.status, 'idle');
  } finally {
    await h.close();
  }
});

test('advanced fields keep exact defaults and reveal validation failures without disconnecting', async () => {
  const h = await setup({
    project: {
      name: 'advanced',
      channels: [iecChannel('a', [{ kind: 'yc', address: 1, value: 0 }])],
    },
  });
  try {
    h.$('device-config').click();
    const panel = h.$('original-device-panel'),
      advanced = panel.querySelector('details');
    assert.ok(advanced);
    assert.equal(advanced.open, false);
    assert.equal(advanced.nextElementSibling, panel.querySelector('.original-submit'));
    const cot = originalField(h, 'txtCOT');
    assert.equal(cot.closest('details'), advanced);
    const previous = cot.value;
    cot.value = '9';
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('[type=submit]'));
    await settleInline();
    assert.equal(advanced.open, true);
    assert.equal(cot.getAttribute('aria-invalid'), 'true');
    assert.equal(
      h.calls.some((c) => ['UpsertDevice', 'StopChannel'].includes(c[0])),
      false,
    );
    cot.value = previous;
    assert.ok(panel.querySelector('.four-remote-config'));
    assert.equal(originalField(h, 'txtYX_Add').closest('details'), null);
  } finally {
    await h.close();
  }
});

test('advanced manual command fields preserve select execute cancel and time-tag options', async () => {
  const h = await setup(
    {
      project: {
        name: 'advanced',
        channels: [iecChannel('a', [{ kind: 'yk', address: 100, value: 0 }])],
      },
    },
    null,
    { Command104Device: () => ({ message: 'confirmed' }) },
  );
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    h.$('open-yk').click();
    const advanced = h.$('control-timestamp').closest('details');
    assert.equal(advanced.open, false);
    assert.equal(h.$('control-direct').closest('details'), null);
    advanced.open = true;
    h.$('control-timestamp').checked = true;
    h.$('control-timestamp').dispatchEvent(new h.w.Event('change', { bubbles: true }));
    h.$('control-select').click();
    await settleAuto();
    assert.equal(h.calls.filter((c) => c[0] === 'Command104Device').at(-1)[1][2].type, 58);
    h.$('control-timeout').value = '';
    h.$('control-select').click();
    await settleAuto();
    assert.equal(advanced.open, true);
    assert.equal(h.$('control-form').querySelector('.form-error').hidden, false);
  } finally {
    await h.close();
  }
});

test('normal online cell edits use local feedback but offline edits explicitly warn', async () => {
  const c = {
    ...iecChannel('a', [{ kind: 'yc', address: 1, value: 0, quality: 0 }]),
    role: 'server',
  };
  const h = await setup({ project: { name: 'feedback', channels: [c] } }, null, {
    SetPointValue: () => ({ mode: 'reported', receivers: 1 }),
  });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    h.w.document.querySelector('[data-point-kind=yc]').click();
    const input = h.$('point-rows').querySelector('[data-point-field=value]');
    input.value = '2';
    input.dispatchEvent(new h.w.Event('input'));
    input.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await settleInline();
    assert.equal(input.classList.contains('cell-saved'), true);
    assert.doesNotMatch(h.$('toast').textContent, /已提交上送/);
    h.overrides.SetPointValue = () => ({ mode: 'offline' });
    input.value = '3';
    input.dispatchEvent(new h.w.Event('input'));
    input.dispatchEvent(new h.w.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await settleInline();
    assert.match(h.$('toast').textContent, /未上送/);
  } finally {
    await h.close();
  }
});

test('single Modbus writes execute once while multi-point writes retain confirmation', async () => {
  const c = channel('a', [{ kind: 'holding', tab: 'yt', address: 10, value: 0 }]);
  const h = await setup({ project: { name: 'write', channels: [c] } }, null, {
    WriteDeviceModbusPoints: () => ({ requests: 1 }),
  });
  try {
    h.w.document.querySelector('[data-point-kind=yt]').click();
    h.$('write-address').value = '10';
    h.$('write-value').value = '42';
    h.$('write-fc').value = '6';
    h.$('write-modbus').click();
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'WriteDeviceModbusPoints').length, 1);
    assert.equal(h.$('confirm-dialog').open, false);
    h.$('write-address').value = '10-11';
    h.$('write-modbus').click();
    await settleInline();
    assert.equal(h.$('confirm-dialog').open, true);
    assert.equal(h.calls.filter((c) => c[0] === 'WriteDeviceModbusPoints').length, 1);
    h.$('confirm-actions').lastElementChild.click();
    await settleInline();
    assert.equal(h.calls.filter((c) => c[0] === 'WriteDeviceModbusPoints').length, 2);
  } finally {
    await h.close();
  }
});

test('collapsed advanced device values survive an unchanged save and cancel', async () => {
  const c = iecChannel('a', [{ kind: 'yx', address: 1, value: 0 }]);
  c.devices = [
    {
      id: 'd',
      name: 'd',
      commonAddress: 1,
      unit: 1,
      points: c.points,
      configuration: {
        version: '标准版',
        values: {
          txtCOT: '1',
          txtASDU_Byte: '1',
          txtMess_Byte: '2',
          txtUTest_Time: '17',
          ackDelayMs: '250',
        },
        ranges: [],
      },
    },
  ];
  const h = await setup({ project: { name: 'advanced', channels: [c] } });
  try {
    h.$('device-config').click();
    const advanced = h.$('original-device-panel').querySelector('details');
    assert.equal(advanced.open, false);
    for (const [id, value] of [
      ['txtCOT', '1'],
      ['txtASDU_Byte', '1'],
      ['txtMess_Byte', '2'],
      ['txtUTest_Time', '17'],
    ])
      assert.equal(originalField(h, id).value, value);
    h.$('device-form').requestSubmit(h.$('device-form').querySelector('[type=submit]'));
    await settleInline();
    assert.equal(
      h.calls.some((c) => ['UpsertDevice', 'StopChannel'].includes(c[0])),
      false,
    );
  } finally {
    await h.close();
  }
});

test('Space on disclosure or action controls does not also pause message display', async () => {
  const h = await setup({
    project: { name: 'keys', channels: [iecChannel('a', [{ kind: 'yk', address: 1, value: 0 }])] },
  });
  try {
    h.w.document.querySelector('[data-point-kind=yk]').click();
    for (const el of [
      h.$('runtime-auto-details').querySelector('summary'),
      h.$('runtime-auto-run'),
      h.$('toggle-debug'),
    ]) {
      el.dispatchEvent(
        new h.w.KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true }),
      );
      assert.equal(h.$('pause-messages').textContent, '暂停');
    }
    h.w.document.body.dispatchEvent(
      new h.w.KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true }),
    );
    await tick();
    assert.equal(h.$('pause-messages').textContent, '继续');
  } finally {
    await h.close();
  }
});

test('adjustment runtime uses one result column and preserves success and rejection', async () => {
  const h = await setup(
    {
      project: {
        name: 'yt-results',
        channels: [iecChannel('a', [{ kind: 'yt', address: 100, value: 0 }])],
      },
    },
    null,
    {
      GetAutoControlStatus: () => ({
        saved: true,
        status: 'completed',
        total: 2,
        success: 1,
        failure: 1,
      }),
      GetAutoControlDetails: () => ({
        offset: 0,
        rowCount: 2,
        current: -1,
        rows: [
          { address: 100, name: 'ok', executed: '是', execution: '' },
          { address: 101, name: 'bad', executed: '否', execution: 'denied' },
        ],
        errors: ['denied'],
      }),
    },
  );
  try {
    h.w.document.querySelector('[data-point-kind=yt]').click();
    await h.refresh();
    const d = h.$('runtime-auto-details');
    d.open = true;
    d.dispatchEvent(new h.w.Event('toggle'));
    await settleAuto();
    assert.equal(d.querySelectorAll('thead th').length, 3);
    assert.match(h.$('runtime-auto-results').textContent, /执行成功/);
    assert.match(h.$('runtime-auto-results').textContent, /denied/);
  } finally {
    await h.close();
  }
});

test('point pages are capped at 100 with exact boundaries, search reset and preserved drafts', async () => {
  for (const count of [0, 99, 100, 101, 200, 201]) {
    const points = Array.from({ length: count }, (_, i) => ({
      kind: 'yx',
      address: count - i,
      name: 'signal-' + (count - i),
      value: 0,
      quality: 0,
    }));
    const h = await setup({ project: { name: 'pages', channels: [iecChannel('a', points)] } });
    try {
      assert.equal(h.$('point-rows').children.length, Math.min(count, 100));
      assert.equal(h.$('point-pager').hidden, count <= 100);
      if (count) {
        assert.equal(h.$('point-rows').querySelector('[data-point-field=address]').value, '1');
      }
      if (count > 100) {
        h.$('point-next').click();
        assert.equal(h.$('point-rows').querySelector('[data-point-field=address]').value, '101');
        assert.equal(h.$('point-rows').children.length, Math.min(count - 100, 100));
        const input = h.$('point-rows').querySelector('[data-point-field=value]');
        editInline(h, input, '1');
        await h.refresh();
        assert.equal(h.$('point-rows').querySelector('[data-point-field=value]'), input);
        assert.equal(input.value, '1');
        inlineKey(h, input, 'Escape');
        assert.equal(input.value, '0');
        if (count > 200) {
          h.$('point-next').click();
          assert.equal(h.$('point-rows').children.length, 1);
          assert.equal(h.$('point-rows').querySelector('[data-point-field=address]').value, '201');
          assert.equal(h.$('point-next').disabled, true);
        }
        h.$('point-search').value = 'signal-101';
        h.$('point-search').dispatchEvent(new h.w.Event('input'));
        assert.equal(h.$('point-rows').children.length, 1);
        assert.equal(h.$('point-pager').hidden, true);
        assert.equal(h.$('point-page').textContent, '1 / 1');
        h.$('point-search').value = '';
        h.$('point-search').dispatchEvent(new h.w.Event('input'));
        assert.equal(h.$('point-rows').children.length, 100);
        assert.equal(h.$('point-page').textContent, '1 / ' + Math.ceil(count / 100));
      }
    } finally {
      await h.close();
    }
  }
});

test('update is immediately right of save and preserves pending state through polling', async () => {
  const h = await setup({ dirty: true });
  try {
    const b = h.$('update-button');
    assert.equal(h.$('save-project').nextElementSibling, b);
    assert.equal(b.textContent.trim(), '更新');
    assert.equal(b.hasAttribute('title'), false);
    let complete;
    h.overrides.CheckForUpdates = () =>
      new Promise((resolve) => {
        complete = resolve;
      });
    const before = JSON.stringify(h.state);
    b.click();
    b.click();
    await tick();
    assert.equal(h.calls.filter((c) => c[0] === 'CheckForUpdates').length, 1);
    assert.equal(b.disabled, true);
    assert.equal(b.textContent.trim(), '检查中…');
    assert.equal(b.getAttribute('aria-busy'), 'true');
    assert.ok(b.querySelector('svg'));
    await h.refresh();
    assert.equal(b.disabled, true);
    assert.equal(h.$('save-project').disabled, false);
    assert.equal(h.$('toggle-channel').disabled, false);
    complete({ status: 'unpublished', currentVersion: '1.0.0' });
    await tick();
    await tick();
    assert.equal(b.disabled, false);
    assert.equal(b.textContent.trim(), '更新');
    assert.equal(b.getAttribute('aria-busy'), 'false');
    assert.match(h.$('toast').textContent, /暂无已发布/);
    assert.equal(JSON.stringify(h.state), before);
    assert.equal(
      h.calls.some((c) =>
        ['StopAll', 'StopChannel', 'StartChannel', 'SaveProject', 'OpenUpdatePage'].includes(c[0]),
      ),
      false,
    );
  } finally {
    await h.close();
  }
});
test('update shows current version or error and permits retry without changing drafts', async () => {
  const h = await setup();
  try {
    h.$('read-start').value = '123，125-127';
    h.overrides.CheckForUpdates = () => {
      throw Error('更新服务暂不可用或请求过于频繁，请稍后重试');
    };
    h.$('update-button').click();
    await tick();
    await tick();
    assert.match(h.$('toast').textContent, /请求过于频繁/);
    assert.equal(h.$('update-button').disabled, false);
    h.overrides.CheckForUpdates = () => ({ status: 'current', currentVersion: '1.0.0' });
    h.$('update-button').click();
    await tick();
    await tick();
    assert.equal(h.$('toast').textContent, '当前已是最新版本（1.0.0）');
    assert.equal(h.$('read-start').value, '123，125-127');
    h.overrides.CheckForUpdates = () => ({ status: 'unknown' });
    h.$('update-button').click();
    await tick();
    await tick();
    assert.match(h.$('toast').textContent, /更新信息无效/);
    assert.equal(h.$('update-button').disabled, false);
  } finally {
    await h.close();
  }
});
test('new version opens download page only after download choice in either workspace', async () => {
  for (const role of ['client', 'server']) {
    const h = await setup();
    try {
      if (role === 'server') {
        h.w.document.querySelector('[data-mode=server]').click();
        await tick();
        await tick();
      }
      h.overrides.CheckForUpdates = () => ({
        status: 'available',
        currentVersion: '1.0.0',
        latestVersion: 'v1.1.0',
      });
      h.$('update-button').click();
      await tick();
      assert.equal(h.$('confirm-dialog').open, true);
      assert.equal(h.$('confirm-title').textContent, '发现新版本');
      assert.match(h.$('confirm-text').textContent, /1.0.0.*v1.1.0/);
      h.$('confirm-actions').querySelector('[value=cancel]').click();
      await tick();
      await tick();
      assert.equal(
        h.calls.some((c) => c[0] === 'OpenUpdatePage'),
        false,
      );
      assert.equal(h.$('update-button').disabled, false);
      h.$('update-button').click();
      await tick();
      h.$('confirm-actions').querySelector('[value=download]').click();
      await tick();
      await tick();
      assert.equal(h.calls.filter((c) => c[0] === 'OpenUpdatePage').length, 1);
      assert.equal(h.$('update-button').disabled, false);
    } finally {
      await h.close();
    }
  }
});
test('failed opening of download page is visible and update can be checked again', async () => {
  const h = await setup();
  try {
    h.overrides.CheckForUpdates = () => ({
      status: 'available',
      currentVersion: '1.0.0',
      latestVersion: '1.1.0',
    });
    h.overrides.OpenUpdatePage = () => {
      throw Error('请从主窗口打开更新');
    };
    h.$('update-button').click();
    await tick();
    h.$('confirm-actions').querySelector('[value=download]').click();
    await tick();
    await tick();
    assert.match(h.$('toast').textContent, /请从主窗口/);
    assert.equal(h.$('update-button').disabled, false);
  } finally {
    await h.close();
  }
});

test('desktop update confirmation uses native dialog and opens page only on download', async () => {
  const h = await setup(
    {},
    { kind: '' },
    {
      CheckForUpdates: () => ({
        status: 'available',
        currentVersion: '1.0.0',
        latestVersion: 'v1.1.0',
      }),
      AskNative: () => ({ choice: 'download' }),
    },
  );
  try {
    h.$('update-button').click();
    await tick();
    await tick();
    const ask = h.calls.find((c) => c[0] === 'AskNative');
    assert.ok(ask);
    assert.equal(ask[1][0].title, '发现新版本');
    assert.deepEqual(JSON.parse(JSON.stringify(ask[1][0].choices)), [
      ['cancel', '稍后'],
      ['download', '下载更新'],
    ]);
    assert.equal(h.calls.filter((c) => c[0] === 'OpenUpdatePage').length, 1);
    assert.equal(h.$('confirm-dialog').open, false);
    assert.equal(h.$('update-button').disabled, false);
  } finally {
    await h.close();
  }
});

test('float default preserves explicitly selected telemetry formats and existing device settings', async () => {
  const c = dataServer();
  c.devices[0].configuration = {
    version: '标准版',
    values: { cbYC_Type: '带品质归一化(type:9)' },
    ranges: [],
  };
  const h = await setup({ project: { name: 'existing-format', channels: [c] }, states: [] });
  try {
    h.w.document.querySelector('[data-mode=server]').click();
    await settleInline();
    const before = JSON.stringify(h.state.project);
    h.$('device-config').click();
    assert.equal(originalField(h, 'cbYC_Type').value, '带品质归一化(type:9)');
    h.$('device-dialog').close();
    assert.equal(JSON.stringify(h.state.project), before);
    h.$('add-device').click();
    const type = originalField(h, 'cbYC_Type');
    assert.equal(type.value, '带品质短浮点(type:13)');
    type.value = '带品质标度化(type:11)';
    type.dispatchEvent(new h.w.Event('change', { bubbles: true }));
    chooseDeviceProtocol(h, 'modbus', 'TCP版');
    chooseDeviceProtocol(h, 'iec104', '标准版');
    assert.equal(originalField(h, 'cbYC_Type').value, '带品质标度化(type:11)');
    h.$('device-dialog').close();
    assert.equal(JSON.stringify(h.state.project), before);
  } finally {
    await h.close();
  }
});

test('maximum 10000-point four-remote table traverses every page with sorted points and editable last records', async () => {
  const points = [];
  for (let i = 2499; i >= 0; i--)
    for (const [n, kind] of ['yx', 'yc', 'yk', 'yt'].entries())
      points.push({
        kind,
        tab: kind,
        address: n * 20000 + i * 2,
        name: kind + '-record-' + i,
        value: 0,
        quality: 0,
        wireType: kind === 'yc' ? 13 : 0,
      });
  const h = await setup({
    project: { name: 'maximum-table', channels: [iecChannel('a', points)] },
  });
  try {
    for (const [n, kind] of ['yx', 'yc', 'yk', 'yt'].entries()) {
      h.w.document.querySelector('[data-point-kind=' + kind + ']').click();
      const seen = [];
      for (let page = 0; page < 25; page++) {
        assert.equal(h.$('point-page').textContent, page + 1 + ' / 25');
        assert.equal(h.$('point-rows').children.length, 100);
        seen.push(
          ...[...h.$('point-rows').querySelectorAll('[data-point-field=address]')].map((e) =>
            Number(e.value),
          ),
        );
        if (page < 24) {
          h.$('point-next').click();
          // Real page clicks occur in separate event-loop turns; release DOM WeakRefs too.
          await tick();
        }
      }
      assert.deepEqual(
        seen,
        Array.from({ length: 2500 }, (_, i) => n * 20000 + i * 2),
      );
      assert.equal(h.$('point-next').disabled, true);
      const input = h.$('point-rows').lastElementChild.querySelector('[data-point-field=value]');
      editInline(h, input, '1');
      await h.refresh();
      assert.equal(input.value, '1');
      assert.equal(
        h.$('point-rows').lastElementChild.querySelector('[data-point-field=value]'),
        input,
      );
      inlineKey(h, input, 'Escape');
      assert.equal(input.value, '0');
      h.$('point-search').value = kind + '-record-2499';
      h.$('point-search').dispatchEvent(new h.w.Event('input'));
      assert.equal(h.$('point-rows').children.length, 1);
      assert.equal(h.$('point-page').textContent, '1 / 1');
      h.$('point-search').value = '';
      h.$('point-search').dispatchEvent(new h.w.Event('input'));
      assert.equal(h.$('point-page').textContent, '1 / 25');
    }
    h.w.document.querySelector('[data-point-kind=yx]').click();
    h.$('point-search').value = '0';
    h.$('point-search').dispatchEvent(new h.w.Event('input'));
    assert.equal(h.$('point-rows').querySelector('[data-point-field=address]').value, '0');
    assert.equal(
      h.calls.some((c) =>
        ['StartChannel', 'Command104Device', 'SetPointValue', 'EditPointCell'].includes(c[0]),
      ),
      false,
    );
  } finally {
    await h.close();
  }
});
