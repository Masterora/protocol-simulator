// Original field names, defaults and order used by the supported forms.
globalThis.originalForms = {
  'client-modbus': {
    controls: {
      groupControl1: {
        id: 'groupControl1',
        type: 'GroupControl',
        location: [0, 0],
        text: 'Modbus主站基本配置',
      },
      cbYC_Fun: {
        id: 'cbYC_Fun',
        type: 'TextEdit',
        location: [218, 49],
        editvalue: '3',
        parent: 'groupControl1',
        label: '遥测功能码',
      },
      cbYX_Fun: {
        id: 'cbYX_Fun',
        type: 'TextEdit',
        location: [92, 49],
        editvalue: '2',
        parent: 'groupControl1',
        label: '遥信功能码',
      },
      txtYK_fun: {
        id: 'txtYK_fun',
        type: 'TextEdit',
        location: [218, 73],
        editvalue: '5',
        parent: 'groupControl1',
        label: '遥控功能码',
      },
      txtAdd: {
        id: 'txtAdd',
        type: 'TextEdit',
        location: [92, 25],
        editvalue: '1',
        parent: 'groupControl1',
        label: '子站点位',
      },
      groupControl6: {
        id: 'groupControl6',
        type: 'GroupControl',
        location: [0, 497],
        text: '保护装置事件记录读取',
      },
      cb_ReadEvent: {
        id: 'cb_ReadEvent',
        type: 'CheckEdit',
        location: [21, 26],
        text: '启用事件记录读取',
        parent: 'groupControl6',
        label: '读取点位',
      },
      txtEventAddr: {
        id: 'txtEventAddr',
        type: 'TextEdit',
        location: [206, 26],
        editvalue: '500H',
        parent: 'groupControl6',
        label: '读取点位',
      },
      cb_SynBroadcastAddr: {
        id: 'cb_SynBroadcastAddr',
        type: 'ComboBoxEdit',
        location: [73, 46],
        editvalue: '00',
        options: ['00', 'FF'],
        defaultindex: 0,
        parent: 'groupControl5',
        label: '广播点位',
      },
      groupControl2: {
        id: 'groupControl2',
        type: 'GroupControl',
        location: [0, 99],
        text: '信息体配置',
      },
      txtYK_Num: {
        id: 'txtYK_Num',
        type: 'TextEdit',
        location: [214, 92],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥控个数',
      },
      txtYC_Num: {
        id: 'txtYC_Num',
        type: 'TextEdit',
        location: [214, 48],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥测个数',
      },
      txtYX_Num: {
        id: 'txtYX_Num',
        type: 'TextEdit',
        location: [214, 26],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥信个数',
      },
      txtYK_Add: {
        id: 'txtYK_Add',
        type: 'TextEdit',
        location: [86, 92],
        editvalue: '2560',
        parent: 'groupControl2',
        label: '遥控首点位',
      },
      txtYC_Add: {
        id: 'txtYC_Add',
        type: 'TextEdit',
        location: [86, 48],
        editvalue: '1024',
        parent: 'groupControl2',
        label: '遥测首点位',
      },
      txtYX_Add: {
        id: 'txtYX_Add',
        type: 'TextEdit',
        location: [86, 26],
        editvalue: '1',
        parent: 'groupControl2',
        label: '遥信首点位',
      },
      TimeAddr: {
        id: 'TimeAddr',
        type: 'TextEdit',
        location: [83, 69],
        editvalue: '1000H',
        parent: 'groupControl5',
        label: '校时首点位',
      },
      cbTimeWay: {
        id: 'cbTimeWay',
        type: 'ComboBoxEdit',
        location: [173, 45],
        options: ['IEC标准格式'],
        defaultindex: 0,
        parent: 'groupControl5',
        label: '校时方式',
      },
      cb_yxTwoFun: {
        id: 'cb_yxTwoFun',
        type: 'ComboBoxEdit',
        location: [94, 23],
        options: [
          '0:BA(高字节在后)',
          '1:AB(高字节在前)',
          '2:BA(按位,高字节在后)',
          '3:AB(按位,高字节在前)',
        ],
        defaultindex: 0,
        parent: 'groupControl4',
        label: '遥信格式',
      },
      groupControl4: {
        id: 'groupControl4',
        type: 'GroupControl',
        location: [0, 240],
        text: '数据格式与类型',
      },
      cb_ycFormat: {
        id: 'cb_ycFormat',
        type: 'ComboBoxEdit',
        location: [94, 115],
        options: ['0:无符号短整型(Ushort)', '1:有符号短整型(Short)'],
        defaultindex: 0,
        parent: 'groupControl4',
        label: '遥测数据类型',
      },
      cb_check: {
        id: 'cb_check',
        type: 'ComboBoxEdit',
        location: [94, 92],
        options: ['0:AB(高字节在前)', '1:BA(高字节在后)'],
        defaultindex: 0,
        parent: 'groupControl4',
        label: '校验码格式',
      },
      cb_ycFunWay: {
        id: 'cb_ycFunWay',
        type: 'ComboBoxEdit',
        location: [94, 46],
        options: ['0:AB(高字节在前)', '1:BA(高字节在后)'],
        defaultindex: 0,
        parent: 'groupControl4',
        label: '遥测格式',
      },
      cbTime: {
        id: 'cbTime',
        type: 'CheckEdit',
        location: [21, 24],
        text: '启用校时',
        parent: 'groupControl5',
        label: '广播点位',
      },
      groupControl5: {
        id: 'groupControl5',
        type: 'GroupControl',
        location: [0, 403],
        text: '校时配置',
      },
      SynTime: {
        id: 'SynTime',
        type: 'TextEdit',
        location: [218, 69],
        editvalue: '10',
        parent: 'groupControl5',
        label: '校时间隔(s)',
      },
      groupControl3: {
        id: 'groupControl3',
        type: 'GroupControl',
        location: [0, 549],
        text: '其它配置',
      },
      txt_overTime: {
        id: 'txt_overTime',
        type: 'TextEdit',
        location: [222, 25],
        editvalue: '5',
        parent: 'groupControl3',
        label: '超时时间(s)',
      },
      txtSendInterval: {
        id: 'txtSendInterval',
        type: 'TextEdit',
        location: [89, 25],
        editvalue: '200',
        parent: 'groupControl3',
        label: '召唤间隔(ms)',
      },
    },
  },
  'server-modbus': {
    controls: {
      groupControl1: {
        id: 'groupControl1',
        type: 'GroupControl',
        location: [0, 0],
        text: 'Modbus子站基本配置',
      },
      txtYC_Fun: {
        id: 'txtYC_Fun',
        type: 'TextEdit',
        location: [229, 62],
        editvalue: '3',
        parent: 'groupControl1',
        label: '遥测功能码',
      },
      txtYK_fun: {
        id: 'txtYK_fun',
        type: 'TextEdit',
        location: [229, 91],
        editvalue: '5',
        parent: 'groupControl1',
        label: '遥控功能码',
      },
      txtYX_Fun: {
        id: 'txtYX_Fun',
        type: 'TextEdit',
        location: [88, 62],
        editvalue: '2',
        parent: 'groupControl1',
        label: '遥信功能码',
      },
      txtAdd: {
        id: 'txtAdd',
        type: 'TextEdit',
        location: [88, 32],
        editvalue: '1',
        parent: 'groupControl1',
        label: '子站点位',
      },
      groupControl2: {
        id: 'groupControl2',
        type: 'GroupControl',
        location: [0, 125],
        text: '信息体配置',
      },
      txtYK_Num: {
        id: 'txtYK_Num',
        type: 'TextEdit',
        location: [224, 120],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥控个数',
      },
      txtYK_Add: {
        id: 'txtYK_Add',
        type: 'TextEdit',
        location: [87, 120],
        editvalue: '2560',
        parent: 'groupControl2',
        label: '遥控首点位',
      },
      txtYX_Num: {
        id: 'txtYX_Num',
        type: 'TextEdit',
        location: [224, 31],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥信个数',
      },
      txtYC_Num: {
        id: 'txtYC_Num',
        type: 'TextEdit',
        location: [224, 60],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥测个数',
      },
      txtYX_Add: {
        id: 'txtYX_Add',
        type: 'TextEdit',
        location: [87, 31],
        editvalue: '1',
        parent: 'groupControl2',
        label: '遥信首点位',
      },
      txtYC_Add: {
        id: 'txtYC_Add',
        type: 'TextEdit',
        location: [87, 60],
        editvalue: '1024',
        parent: 'groupControl2',
        label: '遥测首点位',
      },
      groupControl3: {
        id: 'groupControl3',
        type: 'GroupControl',
        location: [0, 330],
        text: '数据格式与类型',
      },
      txtTimeOut: {
        id: 'txtTimeOut',
        type: 'TextEdit',
        location: [152, 215],
        editvalue: '15',
        parent: 'groupControl3',
        label: '报文超时时间（S）',
      },
      cb_yxtwoWay: {
        id: 'cb_yxtwoWay',
        type: 'ComboBoxEdit',
        location: [95, 32],
        options: [
          '0:BA(高字节在后)',
          '1:AB(高字节在前)',
          '2:BA(按位,高字节在后)',
          '3:AB(按位,高字节在前)',
        ],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '遥信格式',
      },
      cb_ycType: {
        id: 'cb_ycType',
        type: 'ComboBoxEdit',
        location: [95, 154],
        options: [
          '0:无符号短整型(Ushort)',
          '1:有符号短整型(Short)',
          '2:无符号整型(UInt)',
          '3:有符号整型(Int)',
          '4:单精度浮点(Float)',
        ],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '遥测类型',
      },
      cb_check: {
        id: 'cb_check',
        type: 'ComboBoxEdit',
        location: [95, 125],
        options: ['0:AB(高字节在前)', '1:BA(高字节在后)'],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '校验码格式',
      },
      cb_ycFunWay: {
        id: 'cb_ycFunWay',
        type: 'ComboBoxEdit',
        location: [95, 62],
        options: ['0:AB(高字节在前)', '1:BA(高字节在后)'],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '遥测格式',
      },
    },
  },
  'client-iec104': {
    controls: {
      groupControl3: {
        id: 'groupControl3',
        type: 'GroupControl',
        location: [0, 239],
        text: '其它配置',
      },
      text_replySICount: {
        id: 'text_replySICount',
        type: 'TextEdit',
        location: [140, 112],
        editvalue: '8',
        parent: 'groupControl3',
        label: '累计接收 I 帧数量',
      },
      retryNum: {
        id: 'retryNum',
        type: 'TextEdit',
        location: [140, 158],
        editvalue: '3',
        parent: 'groupControl3',
        label: '超时重发次数',
      },
      retryInterval: {
        id: 'retryInterval',
        type: 'TextEdit',
        location: [140, 135],
        editvalue: '2',
        parent: 'groupControl3',
        label: '超时重发间隔(S)',
      },
      cbxRetry: {
        id: 'cbxRetry',
        type: 'CheckEdit',
        location: [191, 136],
        text: '使能',
        parent: 'groupControl3',
        label: '超时重发间隔(S)',
      },
      txt_FrameOverTime: {
        id: 'txt_FrameOverTime',
        type: 'TextEdit',
        location: [140, 89],
        editvalue: '50',
        parent: 'groupControl3',
        label: '分帧延时判断时间(ms)',
      },
      txtSyn_Time: {
        id: 'txtSyn_Time',
        type: 'TextEdit',
        location: [140, 45],
        editvalue: '2',
        parent: 'groupControl3',
        label: '时间同步间隔(min)',
      },
      txtTotal_Time: {
        id: 'txtTotal_Time',
        type: 'TextEdit',
        location: [140, 23],
        editvalue: '60',
        parent: 'groupControl3',
        label: '总召间隔(S)',
      },
      timeEnable: {
        id: 'timeEnable',
        type: 'CheckEdit',
        location: [191, 45],
        editvalue: true,
        text: '使能',
        parent: 'groupControl3',
        label: '时间同步间隔(min)',
      },
      groupControl2: {
        id: 'groupControl2',
        type: 'GroupControl',
        location: [0, 96],
        text: '信息体配置',
      },
      txtYK_Num: {
        id: 'txtYK_Num',
        type: 'TextEdit',
        location: [198, 94],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥控个数',
      },
      txtYC_Num: {
        id: 'txtYC_Num',
        type: 'TextEdit',
        location: [198, 50],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥测个数',
      },
      txtYX_Num: {
        id: 'txtYX_Num',
        type: 'TextEdit',
        location: [198, 28],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥信个数',
      },
      txtYK_Add: {
        id: 'txtYK_Add',
        type: 'TextEdit',
        location: [76, 93],
        editvalue: '24577',
        parent: 'groupControl2',
        label: '遥控首点位',
      },
      txtYC_Add: {
        id: 'txtYC_Add',
        type: 'TextEdit',
        location: [76, 49],
        editvalue: '16385',
        parent: 'groupControl2',
        label: '遥测首点位',
      },
      txtYX_Add: {
        id: 'txtYX_Add',
        type: 'TextEdit',
        location: [76, 27],
        editvalue: '1',
        parent: 'groupControl2',
        label: '遥信首点位',
      },
      groupControl1: {
        id: 'groupControl1',
        type: 'GroupControl',
        location: [0, 0],
        text: '104主站基本配置',
      },
      txtUTest_Time: {
        id: 'txtUTest_Time',
        type: 'TextEdit',
        location: [209, 69],
        editvalue: '20',
        parent: 'groupControl1',
        label: 'U测试间隔(秒)',
      },
      txtASDU_Byte: {
        id: 'txtASDU_Byte',
        type: 'TextEdit',
        location: [209, 47],
        editvalue: '2',
        parent: 'groupControl1',
        label: 'ASDU点位字节',
      },
      txtMess_Byte: {
        id: 'txtMess_Byte',
        type: 'TextEdit',
        location: [88, 70],
        editvalue: '3',
        parent: 'groupControl1',
        label: '信息点位字节',
      },
      txtCOT: {
        id: 'txtCOT',
        type: 'TextEdit',
        location: [88, 48],
        editvalue: '2',
        parent: 'groupControl1',
        label: '传送原因字节',
      },
      txtAdd: {
        id: 'txtAdd',
        type: 'TextEdit',
        location: [88, 26],
        editvalue: '1',
        parent: 'groupControl1',
        label: '子站点位',
      },
    },
  },
  'server-iec104': {
    controls: {
      txtYX_Add: {
        id: 'txtYX_Add',
        type: 'TextEdit',
        location: [77, 25],
        editvalue: '1',
        parent: 'groupControl2',
        label: '遥信首点位',
      },
      groupControl2: {
        id: 'groupControl2',
        type: 'GroupControl',
        location: [2, 96],
        text: '信息体配置',
      },
      txtYX_Num: {
        id: 'txtYX_Num',
        type: 'TextEdit',
        location: [194, 24],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥信个数',
      },
      txtYC_Num: {
        id: 'txtYC_Num',
        type: 'TextEdit',
        location: [194, 47],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥测个数',
      },
      txtYK_Num: {
        id: 'txtYK_Num',
        type: 'TextEdit',
        location: [194, 93],
        editvalue: '50',
        parent: 'groupControl2',
        label: '遥控个数',
      },
      txtYC_Add: {
        id: 'txtYC_Add',
        type: 'TextEdit',
        location: [77, 48],
        editvalue: '16385',
        parent: 'groupControl2',
        label: '遥测首点位',
      },
      txtYK_Add: {
        id: 'txtYK_Add',
        type: 'TextEdit',
        location: [77, 94],
        editvalue: '24577',
        parent: 'groupControl2',
        label: '遥控首点位',
      },
      cbYX_Type: {
        id: 'cbYX_Type',
        type: 'ComboBoxEdit',
        location: [85, 46],
        options: ['单点', '双点'],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '遥信类型',
      },
      cbVSQ: {
        id: 'cbVSQ',
        type: 'ComboBoxEdit',
        location: [85, 24],
        options: ['顺序SQ1', '非顺序SQ0'],
        defaultindex: 0,
        parent: 'groupControl3',
        label: 'VSQ配置',
      },
      cbYC_Type: {
        id: 'cbYC_Type',
        type: 'ComboBoxEdit',
        location: [85, 90],
        options: [
          '带品质归一化(type:9)',
          '带品质标度化(type:11)',
          '带品质短浮点(type:13)',
          '归一化(type:21)',
          '带品质带绝对时标归一化(type:34)',
          '带品质带绝对时标标度化(type:35)',
          '带品质带绝对时标短浮点(type:36)',
        ],
        defaultindex: 0,
        parent: 'groupControl3',
        label: '遥测类型',
      },
      textEdit2: {
        id: 'textEdit2',
        type: 'TextEdit',
        location: [88, 48],
        editvalue: '2',
        parent: 'groupControl1',
        label: '传送原因字节',
      },
      txt_DeviceAddr: {
        id: 'txt_DeviceAddr',
        type: 'TextEdit',
        location: [65, 26],
        editvalue: '1',
        parent: 'groupControl1',
        label: '子站点位',
      },
      txtAdd: {
        id: 'txtAdd',
        type: 'TextEdit',
        location: [88, 70],
        editvalue: '3',
        parent: 'groupControl1',
        label: '信息点位字节',
      },
      txtUTest_Time: {
        id: 'txtUTest_Time',
        type: 'TextEdit',
        location: [220, 70],
        editvalue: '20',
        parent: 'groupControl1',
        label: 'U测试间隔(秒)',
      },
      textEdit3: {
        id: 'textEdit3',
        type: 'TextEdit',
        location: [220, 47],
        editvalue: '2',
        parent: 'groupControl1',
        label: 'ASDU点位字节',
      },
      groupControl3: {
        id: 'groupControl3',
        type: 'GroupControl',
        location: [2, 311],
        text: '其它配置',
      },
      isHasTime: {
        id: 'isHasTime',
        type: 'CheckEdit',
        location: [149, 156],
        text: '是否带时标',
        parent: 'groupControl3',
        label: '累计接收 I 帧数量',
      },
      txt_replyIFrame: {
        id: 'txt_replyIFrame',
        type: 'TextEdit',
        location: [131, 136],
        editvalue: '8',
        parent: 'groupControl3',
        label: '累计接收 I 帧数量',
      },
      ck_IsDataSentWaitS: {
        id: 'ck_IsDataSentWaitS',
        type: 'CheckEdit',
        location: [29, 156],
        text: '数据上送等待S帧',
        parent: 'groupControl3',
        label: '累计接收 I 帧数量',
      },
      groupControl1: {
        id: 'groupControl1',
        type: 'GroupControl',
        location: [2, 0],
        text: '104子站基本配置',
      },
    },
  },
  network: {
    controls: {
      rbUDP: {
        id: 'rbUDP',
        type: 'RadioButton',
        location: [168, 26],
        text: '网口UDP',
        parent: 'groupBoxEx1',
      },
      rbCom: {
        id: 'rbCom',
        type: 'RadioButton',
        location: [19, 26],
        text: '串口',
        parent: 'groupBoxEx1',
      },
      groupBoxEx1: {
        id: 'groupBoxEx1',
        type: 'GroupControl',
        location: [0, 0],
        text: '通信方式',
      },
      rbTcp: {
        id: 'rbTcp',
        type: 'RadioButton',
        location: [83, 26],
        text: '网口TCP',
        parent: 'groupBoxEx1',
      },
      groupBoxEx2: {
        id: 'groupBoxEx2',
        type: 'GroupControl',
        location: [0, 52],
        text: '串口配置',
      },
      cbStopBit: {
        id: 'cbStopBit',
        type: 'ComboBoxEdit',
        location: [177, 75],
        options: ['1', '1.5', '2'],
        defaultindex: 0,
        parent: 'groupBoxEx2',
        label: '停止位',
      },
      cbParity: {
        id: 'cbParity',
        type: 'ComboBoxEdit',
        location: [177, 49],
        options: ['无校验', '奇校验', '偶校验', 'Mark', 'Space'],
        defaultindex: 0,
        parent: 'groupBoxEx2',
        label: '校验位',
      },
      cbDataBit: {
        id: 'cbDataBit',
        type: 'ComboBoxEdit',
        location: [61, 75],
        options: ['8', '7', '6', '5'],
        defaultindex: 0,
        parent: 'groupBoxEx2',
        label: '数据位',
      },
      cbBaudRate: {
        id: 'cbBaudRate',
        type: 'ComboBoxEdit',
        location: [61, 49],
        options: [
          '300',
          '600',
          '1200',
          '2400',
          '4800',
          '9600',
          '19200',
          '28800',
          '38400',
          '43000',
          '56000',
          '57600',
          '115200',
        ],
        defaultindex: 5,
        parent: 'groupBoxEx2',
        label: '波特率',
      },
      cbComList: {
        id: 'cbComList',
        type: 'ComboBoxEdit',
        location: [61, 23],
        defaultindex: 0,
        parent: 'groupBoxEx2',
        label: '串口',
      },
      rbServer: {
        id: 'rbServer',
        type: 'RadioButton',
        location: [154, 25],
        text: 'Server',
        parent: 'groupBoxEx3',
        label: '端口',
      },
      rbClient: {
        id: 'rbClient',
        type: 'RadioButton',
        location: [64, 25],
        text: 'Client',
        parent: 'groupBoxEx3',
        label: '远方IP',
      },
      groupBoxEx3: {
        id: 'groupBoxEx3',
        type: 'GroupControl',
        location: [0, 152],
        text: '网口配置',
      },
      txtServerPort: {
        id: 'txtServerPort',
        type: 'TextEdit',
        location: [203, 75],
        parent: 'groupBoxEx3',
        label: '端口',
      },
      txtClientPort: {
        id: 'txtClientPort',
        type: 'TextEdit',
        location: [203, 48],
        parent: 'groupBoxEx3',
        label: '端口',
      },
      txtClientIP: {
        id: 'txtClientIP',
        type: 'TextEdit',
        location: [47, 48],
        parent: 'groupBoxEx3',
        label: '远方IP',
      },
      cbxServerIP: {
        id: 'cbxServerIP',
        type: 'ComboBoxEdit',
        location: [47, 75],
        parent: 'groupBoxEx3',
        label: '本地IP',
      },
      txt_TCPServerMaxConnect: {
        id: 'txt_TCPServerMaxConnect',
        type: 'TextEdit',
        location: [170, 92],
        editvalue: '1',
        parent: 'groupControl1',
        label: 'TCPServer允许最大连接数量',
      },
      groupControl1: {
        id: 'groupControl1',
        type: 'GroupControl',
        location: [0, 253],
        text: '其它配置',
      },
      txt_ComOverTime: {
        id: 'txt_ComOverTime',
        type: 'TextEdit',
        location: [187, 116],
        editvalue: '45',
        parent: 'groupControl1',
        label: '串口连接报文接收超时判断(ms)',
      },
      txt_ClientRelinkTime: {
        id: 'txt_ClientRelinkTime',
        type: 'TextEdit',
        location: [203, 24],
        editvalue: '10',
        parent: 'groupControl1',
        label: '间隔(s)',
      },
      ck_IsAutoReLink: {
        id: 'ck_IsAutoReLink',
        type: 'CheckEdit',
        location: [8, 24],
        text: 'TCPClient断开自动重连',
        parent: 'groupControl1',
        label: '间隔(s)',
      },
      cb_ClientAssignIp: {
        id: 'cb_ClientAssignIp',
        type: 'ComboBoxEdit',
        location: [46, 68],
        parent: 'groupControl1',
        label: '远方IP',
      },
      ck_AssignIpPort: {
        id: 'ck_AssignIpPort',
        type: 'CheckEdit',
        location: [8, 46],
        text: 'TCPClient连接时指定本地IP与端口',
        parent: 'groupControl1',
        label: '远方IP',
      },
      txt_ClientAssignPort: {
        id: 'txt_ClientAssignPort',
        type: 'TextEdit',
        location: [203, 68],
        parent: 'groupControl1',
        label: '端口',
      },
    },
  },
};

// A shared point-set grammar for device creation and every point operation.
(() => {
  function parse(text, max = 16777215, allowEmpty = false) {
    const raw = String(text ?? '').trim();
    if (!raw && allowEmpty) return [];
    if (!raw || raw.length > 160000) throw Error('请填写点位');
    const spans = raw
      .replace(/，/g, ',')
      .split(',')
      .map((token) => {
        const match = token.trim().match(/^(\d+)\s*(?:-\s*(\d+))?$/);
        if (!match) throw Error('点位用，分隔，范围用-');
        const first = Number(match[1]),
          last = Number(match[2] ?? match[1]);
        if (!Number.isSafeInteger(first) || !Number.isSafeInteger(last) || first < 0 || last > max)
          throw Error('点位应为0–' + max + '整数');
        if (last < first) throw Error('范围末点位不能小于首点位');
        return [first, last];
      })
      .sort((a, b) => a[0] - b[0]);
    const merged = [];
    for (const span of spans) {
      const previous = merged[merged.length - 1];
      if (previous && span[0] <= previous[1] + 1) previous[1] = Math.max(previous[1], span[1]);
      else merged.push(span.slice());
    }
    if (merged.reduce((n, [a, b]) => n + b - a + 1, 0) > 10000) throw Error('一次最多10000个点位');
    return merged.flatMap(([a, b]) => Array.from({ length: b - a + 1 }, (_, i) => a + i));
  }
  function format(points) {
    const sorted = [...new Set(points)].sort((a, b) => a - b),
      parts = [];
    for (let i = 0; i < sorted.length;) {
      const first = sorted[i];
      let last = first;
      while (i + 1 < sorted.length && sorted[i + 1] === last + 1) last = sorted[++i];
      parts.push(first === last ? String(first) : first + '-' + last);
      i++;
    }
    return parts.join('，');
  }
  function range(first, count, width = 1) {
    return format(
      Array.from(
        { length: Math.min(10000, Math.max(0, count)) },
        (_, i) => Number(first) + i * width,
      ),
    );
  }
  globalThis.pointSelection = { parse, format, range };
})();

// Shared presentation and validation for the four device configuration forms.
// The engine repeats all range and protocol validation before changing a channel.
(() => {
  const labels = {
    cbxRetry: '超时重发',
    mcEnable: '周期脉冲召唤',
    timeEnable: '周期校时',
    txtMC_Time: '脉冲召唤间隔(min)',
    txtSyn_Time: '周期校时间隔(min)',
    retryNum: '超时重发次数',
    retryInterval: '超时重发间隔(s)',
    txtTotal_Time: '总召间隔(s)',
    txtUTest_Time: 'U测试间隔(s)',
    text_replySICount: '累计接收I帧数量',
    txt_replyIFrame: '累计接收I帧数量',
    sendWindow: '未确认发送窗口',
    ackDelayMs: 'S帧最长等待(ms)',
    responseDelayMs: '应答延迟(ms)',
    txtSendInterval: '采集间隔(ms)',
    txt_overTime: '应答超时(s)',
    txtTimeOut: '报文超时(s)',
    SynTime: '校时间隔(s)',
    eventCount: '事件读取寄存器数',
    TimeAddr: '校时首点位',
    txtEventAddr: '事件读取首点位',
    txt_FrameOverTime: '分帧超时(ms)',
    txtAdd: '装置点位',
    txt_DeviceAddr: '公共点位',
    pollEnabled: '周期采集',
    clockEnabled: '启用校时寄存器',
  };
  for (const [prefix, name] of Object.entries({
    YX: '遥信',
    YC: '遥测',
    MC: '脉冲',
    YK: '遥控',
    YT: '遥调',
    ST: '步调节',
    JDQ: '继电器',
  })) {
    labels['txt' + prefix + '_Add'] = name + '首点位';
    labels['txt' + prefix + '_Num'] = name + '数量';
  }
  Object.assign(labels, {
    txtASDU_Byte: 'ASDU点位字节数',
    textEdit3: 'ASDU点位字节数',
    txtMess_Byte: '信息点位字节数',
    txtCOT: '传送原因字节数',
    textEdit2: '传送原因字节数',
    name: '装置名称',
    cbYC_Fun: '遥测功能码',
    txtYC_Fun: '遥测功能码',
    cbYX_Fun: '遥信功能码',
    txtYX_Fun: '遥信功能码',
    txtMC_Fun: '脉冲功能码',
    txtYK_fun: '遥控功能码',
    txtJDQ_Fun: '继电器功能码',
  });
  const bounds = {
    txtASDU_Byte: [1, 2],
    textEdit3: [1, 2],
    txtMess_Byte: [1, 3],
    txtCOT: [1, 2],
    textEdit2: [1, 2],
    txtTotal_Time: [0, 86400],
    txtMC_Time: [0, 1440],
    txtSyn_Time: [0, 1440],
    txtUTest_Time: [1, 86400],
    text_replySICount: [1, 32767],
    txt_replyIFrame: [1, 32767],
    sendWindow: [1, 32767],
    ackDelayMs: [10, 60000],
    retryNum: [0, 10],
    retryInterval: [1, 60],
    responseDelayMs: [0, 60000],
    txtSendInterval: [20, 3600000],
    txt_overTime: [1, 60],
    txtTimeOut: [1, 60],
    SynTime: [0, 86400],
    eventCount: [1, 125],
    txt_FrameOverTime: [1, 60000],
  };
  const control = (host, id) =>
    [...host.querySelectorAll('[data-original-id],[data-extra-setting]')].find(
      (el) => (el.dataset.originalId || el.dataset.extraSetting) === id,
    ) || host.closest('form')?.elements.namedItem(id);
  function error(field, message) {
    const e = Error(message);
    e.field = field;
    throw e;
  }
  function integer(raw, id, min, max, address = false) {
    const s = String(raw ?? '').trim();
    let n;
    if (address && /^(?:0x[\da-f]+|[\da-f]+h)$/i.test(s))
      n = parseInt(s.replace(/^0x/i, '').replace(/h$/i, ''), 16);
    else if (/^\d+$/.test(s)) n = Number(s);
    if (!Number.isSafeInteger(n) || n < min || n > max)
      error(
        id,
        (labels[id] || id) +
          '应为 ' +
          min +
          '–' +
          max +
          ' 整数' +
          (address ? '，点位支持十进制、0x前缀或H后缀' : ''),
      );
    return n;
  }
  function dependencies(host) {
    const pairs = {
      cbxRetry: ['retryNum', 'retryInterval'],
      mcEnable: ['txtMC_Time'],
      timeEnable: ['txtSyn_Time'],
      cb_ReadEvent: ['txtEventAddr', 'eventCount'],
      cbTime: ['SynTime', 'cb_SynBroadcastAddr'],
      clockEnabled: ['TimeAddr'],
      pollEnabled: ['txtSendInterval'],
    };
    for (const [id, keys] of Object.entries(pairs)) {
      const source = control(host, id);
      if (!source) continue;
      for (const key of keys) {
        const el = control(host, key);
        if (el) {
          el.disabled = !source.checked;
          el.closest('label')?.classList.toggle('inactive-field', el.disabled);
        }
      }
    }
    const wait = control(host, 'ck_IsDataSentWaitS'),
      windowSize = control(host, 'sendWindow');
    if (wait && windowSize) windowSize.disabled = wait.checked;
    const mode = control(host, 'responseMode'),
      delay = control(host, 'responseDelayMs');
    if (mode && delay) delay.disabled = mode.value === 'silent';
  }
  function organizeFour(host) {
    let group = host.querySelector('[data-group-id="groupControl2"]');
    if (!group) {
      group = document.createElement('section');
      group.className = 'original-group';
      group.dataset.groupId = 'groupControl2';
      host.insertBefore(group, host.querySelector('.original-submit'));
    }
    group.classList.add('four-remote-config');
    group.setAttribute('role', 'group');
    group.setAttribute('aria-label', '四遥配置');
    const related = {
      yx: ['cbYX_Fun', 'cbYX_Type', 'cb_yxTwoFun', 'cb_yxtwoWay', 'isHasTime'],
      yc: ['cbYC_Fun', 'cbYC_Type', 'cb_ycType', 'cb_ycFormat', 'cb_ycFunWay'],
      yk: [],
      yt: [],
    };
    for (const category of ['yx', 'yc', 'yk', 'yt']) {
      for (const suffix of ['Add', 'Num']) {
        const el = control(host, 'txt' + category.toUpperCase() + '_' + suffix),
          label = el?.closest('label');
        if (label) {
          label.dataset.remoteField = category;
          group.append(label);
        }
      }
    }
    for (const category of ['yx', 'yc', 'yk', 'yt'])
      for (const el of [...host.querySelectorAll('[data-original-id]')]) {
        const id = el.dataset.originalId;
        if (
          (id.startsWith('txt' + category.toUpperCase()) && !/_(Add|Num)$/.test(id)) ||
          related[category].includes(id)
        ) {
          const label = el.closest('label');
          if (label) {
            label.dataset.remoteField = category;
            group.append(label);
          }
        }
      }
    for (const section of [...host.querySelectorAll('.original-group')]) {
      const title = section.querySelector(':scope>.original-group-title');
      if (title) {
        if (!section.hasAttribute('aria-label'))
          section.setAttribute('aria-label', title.textContent);
        section.setAttribute('role', 'group');
        title.remove();
      }
      if (!section.querySelector('input,select,button')) section.remove();
    }
  }
  function enhance(host, role, group, version) {
    labels.txtAdd =
      role === 'server' && group === 'iec104'
        ? '信息点位字节数'
        : group === 'iec104'
          ? '公共点位'
          : '装置点位';
    for (const el of host.querySelectorAll('[data-original-id],[data-extra-setting]')) {
      const id = el.dataset.originalId || el.dataset.extraSetting,
        label = el.closest('label'),
        caption = labels[id];
      if (caption && label) {
        if (el.type === 'checkbox') {
          for (const node of [...label.childNodes]) if (node.nodeType === 3) node.remove();
          label.append(document.createTextNode(caption));
        } else if (
          label.querySelector('span') &&
          !(id === 'txtAdd' && role === 'server' && group === 'iec104')
        )
          label.querySelector('span').textContent = caption;
      }
      if (bounds[id]) {
        el.min = bounds[id][0];
        el.max = bounds[id][1];
        el.setAttribute(
          'aria-description',
          (caption || id) +
            '：' +
            el.min +
            '–' +
            el.max +
            (String(el.min) === '0' ? '，0表示关闭' : ''),
        );
        el.inputMode = 'numeric';
      }
      if (
        /_(Add|Num)$/.test(id) ||
        ['TimeAddr', 'txtEventAddr', 'txtAdd', 'txt_DeviceAddr'].includes(id)
      )
        el.setAttribute(
          'aria-description',
          (caption || label?.textContent || id) +
            '；点位支持十进制、0x前缀或H后缀；数量为0时不生成该类点',
        );
    }
    if (group === 'modbus' && version === 'TCP版') {
      const crc = control(host, 'cb_check');
      if (crc) {
        crc.closest('label').hidden = true;
        crc.disabled = true;
      }
    }
    dependencies(host);
  }
  function validate(host, config, protocol) {
    for (const [id, range] of Object.entries(bounds)) {
      const el = control(host, id);
      if (el && !el.disabled) integer(el.value, id, ...range);
    }
    let max = protocol.startsWith('modbus') ? 65535 : 16777215;
    const occupied = [],
      occupiedKeys = new Map();
    let count = 0;
    if (protocol === 'iec104') {
      const server = host.dataset.form === 'server-iec104',
        ioaID = server ? 'txtAdd' : 'txtMess_Byte',
        caID = server ? 'textEdit3' : 'txtASDU_Byte',
        publicID = server ? 'txt_DeviceAddr' : 'txtAdd',
        ioa = integer(control(host, ioaID).value, ioaID, 1, 3),
        ca = integer(control(host, caID).value, caID, 1, 2);
      max = 2 ** (ioa * 8) - 1;
      integer(control(host, publicID).value, publicID, 1, Math.min(65534, 2 ** (ca * 8) - 1), true);
    }
    for (const r of config.ranges) {
      const prefix = { yx: 'YX', yc: 'YC', mc: 'MC', yk: 'YK', yt: 'YT', step: 'ST', jdq: 'JDQ' }[
        r.tab
      ];
      let width = 1;
      if (protocol.startsWith('modbus') && ['yc', 'mc'].includes(r.tab)) {
        const type =
          control(host, 'cb_' + r.tab + 'Type') || control(host, 'cb_' + r.tab + 'Format');
        width = r.tab === 'mc' || Number(type?.value.split(':')[0]) >= 2 ? 2 : 1;
      }
      const points = r.selection
        ? globalThis.pointSelection.parse(r.selection, max)
        : Array.from({ length: r.count }, (_, i) => r.address + i * width);
      for (const point of points) {
        if (point + width > max + 1) error('points_' + r.tab, r.name + '末点位超出 ' + max);
        for (let offset = 0; offset < width; offset++) {
          const key = r.kind + ':' + (point + offset);
          if (occupiedKeys.has(key))
            error('points_' + r.tab, r.name + '与' + occupiedKeys.get(key) + '点位范围重叠');
          occupiedKeys.set(key, r.name);
        }
      }
      occupied.push({ ...r, points, width });
      count += points.length;
    }

    if (count > 10000) error('txtYX_Num', '每个装置最多10000个点');
    if (protocol.startsWith('modbus')) {
      const enabled = control(host, 'clockEnabled')?.checked || control(host, 'cbTime')?.checked;
      if (enabled) {
        const el = control(host, 'TimeAddr'),
          a = integer(el.value, 'TimeAddr', 0, 65532, true);
        for (const r of occupied)
          if (
            r.count &&
            ['holding', 'input'].includes(r.kind) &&
            r.points.some((point) => point < a + 4 && point + r.width > a)
          )
            error('TimeAddr', '校时寄存器与' + r.name + '点位范围重叠');
      }
      const event = control(host, 'txtEventAddr');
      if (event && !event.disabled) integer(event.value, 'txtEventAddr', 0, 65535, true);
    }
    return occupied;
  }

  function pointWidth(host, category) {
    const input =
      control(host, 'cb_' + category + 'Type') || control(host, 'cb_' + category + 'Format');
    return category === 'yc' && Number(input?.value.split(':')[0]) >= 2 ? 2 : 1;
  }
  function pointRange(host, category, max) {
    const prefix = category.toUpperCase(),
      a = control(host, 'txt' + prefix + '_Add'),
      c = control(host, 'txt' + prefix + '_Num'),
      address = integer(a.value, 'txt' + prefix + '_Add', 0, max, true),
      count = integer(c.value, 'txt' + prefix + '_Num', 0, 10000),
      stored = host.closest('form')?.devicePointSelections?.[category];
    // Settings-only edits retain imported sparse tables until the range changes.
    const unchanged =
      stored &&
      stored.address === address &&
      stored.count === count &&
      stored.width === pointWidth(host, category);
    return { address, count, selection: unchanged ? stored.selection : '' };
  }
  function clear(host) {
    for (const el of host.querySelectorAll('[aria-invalid]')) el.removeAttribute('aria-invalid');
    for (const el of host.querySelectorAll('.device-field-error')) el.remove();
  }
  function showError(host, e) {
    clear(host);
    let el =
      e.field &&
      (e.field.startsWith('points_')
        ? control(host, 'txt' + e.field.slice(7).toUpperCase() + '_Add')
        : control(host, e.field));
    if (!el)
      for (const [id, label] of Object.entries(labels))
        if (String(e.message || e).includes(label.replace(/\([^)]*\)/g, ''))) {
          el = control(host, id);
          if (el) break;
        }
    if (el) {
      el.setAttribute('aria-invalid', 'true');
      for (let parent = el.parentElement; parent && parent !== host; parent = parent.parentElement)
        if (parent.tagName === 'DETAILS') parent.open = true;
      const msg = document.createElement('small');
      msg.className = 'device-field-error';
      msg.textContent = e.message || String(e);
      el.closest('label').append(msg);
      el.focus();
      el.scrollIntoView?.({ block: 'nearest' });
    }
  }
  globalThis.deviceEditor = {
    pointWidth,
    pointRange,
    labels,
    bounds,
    control,
    integer,
    error,
    dependencies,
    organizeFour,
    enhance,
    validate,
    clear,
    showError,
  };
})();

// Captions and defaults follow the original designer. Only implemented controls
// are rendered; grid flow replaces fragile absolute designer coordinates.
globalThis.renderOriginalForm = function (host, key, enabled, values = {}, edit = false) {
  const spec = globalThis.originalForms[key],
    escape = (s) =>
      String(s ?? '').replace(
        /[&<>"']/g,
        (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
      );
  if (!spec) {
    host.replaceChildren();
    return;
  }
  const controls = Object.values(spec.controls),
    order = (a, b) => a.location[1] - b.location[1] || a.location[0] - b.location[0];
  // Designer rows can differ by one pixel. Keep each address/count pair on
  // one row instead of sorting the two controls independently by that pixel.
  const rangeID = /^txt(YX|YC|MC|YK|JDQ)_(Add|Num)$/;
  const fieldOrder = (a, b) => {
    const ar = rangeID.exec(a.id),
      br = rangeID.exec(b.id);
    if (ar && br) {
      const row = (c) =>
        Math.min(
          ...controls
            .filter(
              (v) => v.parent === c.parent && rangeID.exec(v.id)?.[1] === rangeID.exec(c.id)?.[1],
            )
            .map((v) => v.location[1]),
        );
      return row(a) - row(b) || (ar[2] === 'Add' ? 0 : 1) - (br[2] === 'Add' ? 0 : 1);
    }
    return order(a, b);
  };
  const render = (c) => {
    const id = c.id,
      v = values[id] ?? c.editvalue ?? c.options?.[c.defaultindex || 0] ?? '',
      data = `data-original-id="${id}"`,
      name = `name="original-${id}"`,
      disabled = '';
    const prefixes = { YX: '遥信', YC: '遥测', MC: '脉冲', YK: '遥控', JDQ: '继电器' };
    const range = /^txt(YX|YC|MC|YK|JDQ)_(Add|Num)$/.exec(id);
    const networkLabels = {
      cb_ClientAssignIp: '本地IP',
      txt_ClientAssignPort: '本地端口',
      txtClientPort: '远方端口',
      txtServerPort: '监听端口',
      cbxServerIP: '监听IP',
    };
    const label = range
      ? prefixes[range[1]] + (range[2] === 'Add' ? '首点位' : '个数')
      : networkLabels[id] || c.label || c.text || id;
    if (c.type === 'CheckEdit' || c.type === 'RadioButton') {
      const radio = c.type === 'RadioButton',
        group = id === 'rbClient' || id === 'rbServer' ? 'network-role' : 'network-type';
      return `<label class="original-check"><input type="${radio ? 'radio' : 'checkbox'}" ${data} ${radio ? `name="${group}"` : name} ${v === true || v === 'true' ? 'checked' : ''} ${disabled}>${escape({ cbxRetry: '超时重发', mcEnable: '周期脉冲召唤', timeEnable: '周期校时' }[id] || c.text)}</label>`;
    }
    let input;
    if (key === 'network' && ['cbComList', 'cbxServerIP', 'cb_ClientAssignIp'].includes(id))
      input = `<input type="text" ${data} ${name} value="${escape(v)}" list="choices-${id}" ${disabled} autocomplete="off"><datalist id="choices-${id}"></datalist>`;
    else if (c.options?.length) {
      let options = c.options.filter((o) => !['1.5', 'Mark', 'Space'].includes(o));
      if (v !== '' && !options.includes(String(v))) options = [String(v), ...options];
      input = `<select ${data} ${name} ${disabled}>${options.map((o) => `<option ${String(o) === String(v) ? 'selected' : ''}>${escape(o)}</option>`).join('')}</select>`;
    } else
      input = `<input type="text" ${data} ${name} value="${escape(v)}" ${disabled} autocomplete="off">`;
    return `<label class="original-field" data-original-field="${id}"><span>${escape(label)}</span>${input}</label>`;
  };
  const groups = controls.filter((c) => c.type === 'GroupControl').sort(order);
  host.style.width = '';
  host.style.height = '';
  host.dataset.form = key;
  host.innerHTML =
    groups
      .map((g) => {
        const fields = controls
          .filter((c) => c.parent === g.id && enabled.has(c.id))
          .sort(fieldOrder);
        return fields.length
          ? `<section class="original-group" data-group-id="${g.id}"><div class="original-group-title">${escape(g.text)}</div>${fields.map(render).join('')}</section>`
          : '';
      })
      .join('') +
    `<div class="original-submit"><button type="submit" data-original-id="simpleButton1">确认</button></div>`;
};
globalThis.readOriginalForm = function (host) {
  return Object.fromEntries(
    [...host.querySelectorAll('[data-original-id]')]
      .filter((el) => el.tagName !== 'BUTTON')
      .map((el) => [
        el.dataset.originalId,
        el.type === 'checkbox' || el.type === 'radio' ? el.checked : el.value,
      ]),
  );
};
