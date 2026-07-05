const LANG_KEY = 'catfish.dero.language';

const state = {
  status: null,
  activeTab: 'wallet',
  lang: detectLanguage(),
};

const $ = (id) => document.getElementById(id);

const i18n = {
  zh: {
    'app.title': 'Catfish DERO Desktop',
    'app.subtitle': 'Catfish 公网 DERO 链',
    'actions.refresh': '刷新',
    'actions.startMining': '开始挖矿',
    'actions.stopMining': '停止挖矿',
    'actions.copy': '复制',
    'actions.showSeed': '显示助记词',
    'actions.restartNode': '重启节点',
    'actions.send': '发送',
    'actions.installContract': '安装合约',
    'actions.call': '调用',
    'actions.query': '查询',
    'actions.refreshBlocks': '刷新区块',
    'actions.refreshWalletTx': '刷新我的交易',
    'actions.refreshLogs': '刷新日志',
    'metrics.node': '节点',
    'metrics.height': '高度',
    'metrics.miner': '矿工',
    'metrics.balance': '余额',
    'tabs.wallet': '钱包',
    'tabs.transfer': '转账',
    'tabs.contracts': '合约',
    'tabs.explorer': '浏览器',
    'tabs.logs': '日志',
    'wallet.address': '钱包地址',
    'wallet.balanceFull': '总余额',
    'wallet.unlocked': '可用余额',
    'wallet.chainStatus': '链上状态',
    'wallet.height': '钱包高度',
    'transfer.toAddress': '接收地址',
    'transfer.amount': '金额',
    'transfer.memo': '留言',
    'transfer.memoPlaceholder': '可选，接收方钱包可解密看到',
    'contracts.source': 'DVM 合约源码',
    'contracts.scid': 'SCID',
    'contracts.scidPlaceholder': '安装后得到的 txid',
    'contracts.entrypoint': '入口函数',
    'contracts.args': '参数 JSON',
    'contracts.deposit': 'DERO 存入',
    'explorer.publicTitle': '公开区块交易',
    'explorer.walletTitle': '我的可解密交易',
    'logs.title': '运行日志',
    'table.topo': 'Topo',
    'table.height': '高度',
    'table.transactions': '交易',
    'table.reward': '奖励',
    'table.hash': '哈希',
    'table.time': '时间',
    'table.type': '类型',
    'table.amount': '金额',
    'table.counterparty': '对方',
    'table.memo': '留言',
    'table.txid': 'TXID',
    'status.onlinePeers': '在线 {peers}',
    'status.offline': '离线',
    'status.registering': '注册中',
    'status.mining': '挖矿中',
    'status.minerStopped': '未运行',
    'status.registered': '已上链',
    'status.registrationPending': '上链中',
    'status.registrationSubmitted': '注册已提交，等待进块',
    'status.registrationConfirming': '已注册，待确认 {blocks} 块',
    'status.registrationSpendable': '已注册可用',
    'status.unregistered': '未上链',
    'toast.miningStarted': '挖矿已启动',
    'toast.miningStopped': '挖矿已停止',
    'toast.txSubmitted': '交易已提交',
    'toast.contractInstallSubmitted': '合约安装交易已提交',
    'toast.contractCallSubmitted': '合约调用交易已提交',
    'toast.nodeRestarted': '节点已重启',
    'toast.addressCopied': '地址已复制',
    'blocks.size': '大小 {size} B',
    'blocks.fees': '费用 {fees}',
    'blocks.payload': 'Payload {payloads}',
    'blocks.noTransactions': '无交易',
    'blocks.empty': '暂无区块',
    'walletTx.pending': '{kind}（待确认）',
    'walletTx.empty': '暂无可解密交易',
    'walletTx.miningReward': '挖矿奖励',
    'walletTx.received': '收到',
    'walletTx.sent': '发出',
    'blockNote.miningReward': '矿工奖励 {amount}',
  },
  en: {
    'app.title': 'Catfish DERO Desktop',
    'app.subtitle': 'Catfish public DERO chain',
    'actions.refresh': 'Refresh',
    'actions.startMining': 'Start Mining',
    'actions.stopMining': 'Stop Mining',
    'actions.copy': 'Copy',
    'actions.showSeed': 'Show Seed',
    'actions.restartNode': 'Restart Node',
    'actions.send': 'Send',
    'actions.installContract': 'Install Contract',
    'actions.call': 'Call',
    'actions.query': 'Query',
    'actions.refreshBlocks': 'Refresh Blocks',
    'actions.refreshWalletTx': 'Refresh My Txs',
    'actions.refreshLogs': 'Refresh Logs',
    'metrics.node': 'Node',
    'metrics.height': 'Height',
    'metrics.miner': 'Miner',
    'metrics.balance': 'Balance',
    'tabs.wallet': 'Wallet',
    'tabs.transfer': 'Transfer',
    'tabs.contracts': 'Contracts',
    'tabs.explorer': 'Explorer',
    'tabs.logs': 'Logs',
    'wallet.address': 'Wallet Address',
    'wallet.balanceFull': 'Total Balance',
    'wallet.unlocked': 'Unlocked Balance',
    'wallet.chainStatus': 'Chain Status',
    'wallet.height': 'Wallet Height',
    'transfer.toAddress': 'Receiver Address',
    'transfer.amount': 'Amount',
    'transfer.memo': 'Memo',
    'transfer.memoPlaceholder': 'Optional, decryptable by the receiver wallet',
    'contracts.source': 'DVM Contract Source',
    'contracts.scid': 'SCID',
    'contracts.scidPlaceholder': 'txid returned after install',
    'contracts.entrypoint': 'Entrypoint',
    'contracts.args': 'Args JSON',
    'contracts.deposit': 'DERO Deposit',
    'explorer.publicTitle': 'Public Block Transactions',
    'explorer.walletTitle': 'My Decryptable Transactions',
    'logs.title': 'Runtime Logs',
    'table.topo': 'Topo',
    'table.height': 'Height',
    'table.transactions': 'Transactions',
    'table.reward': 'Reward',
    'table.hash': 'Hash',
    'table.time': 'Time',
    'table.type': 'Type',
    'table.amount': 'Amount',
    'table.counterparty': 'Counterparty',
    'table.memo': 'Memo',
    'table.txid': 'TXID',
    'status.onlinePeers': 'Online {peers}',
    'status.offline': 'Offline',
    'status.registering': 'Registering',
    'status.mining': 'Mining',
    'status.minerStopped': 'Stopped',
    'status.registered': 'Registered',
    'status.registrationPending': 'Registering',
    'status.registrationSubmitted': 'Submitted, waiting for block',
    'status.registrationConfirming': 'Registered, {blocks} blocks remaining',
    'status.registrationSpendable': 'Registered and ready',
    'status.unregistered': 'Unregistered',
    'toast.miningStarted': 'Mining started',
    'toast.miningStopped': 'Mining stopped',
    'toast.txSubmitted': 'Transaction submitted',
    'toast.contractInstallSubmitted': 'Contract install transaction submitted',
    'toast.contractCallSubmitted': 'Contract call transaction submitted',
    'toast.nodeRestarted': 'Node restarted',
    'toast.addressCopied': 'Address copied',
    'blocks.size': 'Size {size} B',
    'blocks.fees': 'Fees {fees}',
    'blocks.payload': 'Payload {payloads}',
    'blocks.noTransactions': 'No transactions',
    'blocks.empty': 'No blocks yet',
    'walletTx.pending': '{kind} (pending)',
    'walletTx.empty': 'No decryptable transactions',
    'walletTx.miningReward': 'Mining reward',
    'walletTx.received': 'Received',
    'walletTx.sent': 'Sent',
    'blockNote.miningReward': 'Mining reward {amount}',
  },
};

const sampleContract = `Function Initialize() Uint64
10 STORE("owner", SIGNER())
20 STORE("count", 0)
30 RETURN 0
End Function

Function Increment(step Uint64) Uint64
10 IF LOAD("owner") == SIGNER() THEN GOTO 30
20 RETURN 1
30 STORE("count", LOAD("count") + step)
40 RETURN 0
End Function`;

function detectLanguage() {
  const saved = localStorage.getItem(LANG_KEY);
  if (saved === 'zh' || saved === 'en') return saved;
  return navigator.language && navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

function t(key, params = {}) {
  const table = i18n[state.lang] || i18n.zh;
  const fallback = i18n.zh[key] || key;
  const template = table[key] || fallback;
  return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (_, name) => String(params[name] ?? ''));
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await response.json();
  if (!response.ok || data.ok === false) {
    throw new Error(data.error || `HTTP ${response.status}`);
  }
  return data;
}

function toast(message) {
  const box = $('toast');
  box.textContent = message;
  box.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => {
    box.hidden = true;
  }, 3600);
}

function setText(id, value) {
  $(id).textContent = value ?? '';
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[char]));
}

function applyLanguage() {
  document.documentElement.lang = state.lang === 'zh' ? 'zh-CN' : 'en';
  document.title = t('app.title');

  document.querySelectorAll('[data-i18n]').forEach((element) => {
    element.textContent = t(element.dataset.i18n);
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach((element) => {
    element.placeholder = t(element.dataset.i18nPlaceholder);
  });
  document.querySelectorAll('[data-lang]').forEach((button) => {
    const active = button.dataset.lang === state.lang;
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', active ? 'true' : 'false');
  });

  if (state.status) renderStatus(state.status);
}

function setLanguage(lang) {
  if (!i18n[lang] || state.lang === lang) return;
  state.lang = lang;
  localStorage.setItem(LANG_KEY, lang);
  applyLanguage();
  if (state.activeTab === 'explorer') {
    refreshBlocks();
    refreshWalletTransactions();
  }
}

function registrationStatusText(registration, wallet) {
  const remaining = Number(registration.confirmationsRemaining || 0);
  switch (registration.status) {
    case 'spendable':
      return t('status.registrationSpendable');
    case 'confirming':
      return t('status.registrationConfirming', { blocks: Math.max(1, remaining) });
    case 'submitted':
      return t('status.registrationSubmitted');
    case 'running':
      return t('status.registrationPending');
    default:
      if (wallet.registered) {
        return remaining > 0
          ? t('status.registrationConfirming', { blocks: Math.max(1, remaining) })
          : t('status.registered');
      }
      return t('status.unregistered');
  }
}

async function refreshStatus() {
  try {
    const data = await api('/api/status');
    state.status = data;
    renderStatus(data);
  } catch (error) {
    toast(error.message);
  }
}

function renderStatus(data) {
  const daemon = data.daemon || {};
  const wallet = data.wallet || {};
  const miner = data.miner || {};

  setText('nodeState', daemon.online ? t('status.onlinePeers', { peers: daemon.peers || 0 }) : t('status.offline'));
  setText('height', daemon.online ? `${daemon.height}/${daemon.topoheight}` : '0');
  setText('minerState', miner.running ? (miner.mode === 'registration-bootstrap' ? t('status.registering') : t('status.mining')) : t('status.minerStopped'));
  setText('balance', wallet.balance || '0.00000');
  setText('address', wallet.address || '...');
  setText('balanceFull', wallet.balance || '0.00000');
  setText('unlocked', wallet.unlocked || '0.00000');
  setText('registered', registrationStatusText(data.registration || {}, wallet));
  setText('walletHeight', `${wallet.height || 0}/${wallet.daemonHeight || 0}`);

  const minerBtn = $('minerBtn');
  minerBtn.textContent = miner.running ? t('actions.stopMining') : t('actions.startMining');
  minerBtn.className = miner.running ? 'warn' : 'primary';
}

async function refreshLogs() {
  try {
    const data = await api('/api/logs');
    $('logsBox').textContent = (data.lines || []).join('\n');
    $('logsBox').scrollTop = $('logsBox').scrollHeight;
  } catch (error) {
    toast(error.message);
  }
}

function displayBlockNote(note) {
  if (state.lang !== 'en') return note || '';
  const prefix = '矿工奖励 ';
  if (String(note || '').startsWith(prefix)) {
    return t('blockNote.miningReward', { amount: String(note).slice(prefix.length) });
  }
  return note || '';
}

function displayWalletKind(kind) {
  if (state.lang !== 'en') return kind || '';
  const map = {
    '挖矿奖励': t('walletTx.miningReward'),
    '收到': t('walletTx.received'),
    '发出': t('walletTx.sent'),
  };
  return map[kind] || kind || '';
}

async function refreshBlocks() {
  try {
    const data = await api('/api/blocks?limit=16');
    const rows = (data.blocks || []).map((block) => {
      const transactions = block.transactions || [];
      const txRows = transactions.map((tx) => `
        <div class="tx-row">
          <div>
            <strong>${escapeHtml(tx.type)}</strong>
            <span>${escapeHtml(displayBlockNote(tx.note))}</span>
          </div>
          <code>${escapeHtml(tx.hash || '')}</code>
          <span>${escapeHtml(t('blocks.size', { size: tx.size || 0 }))}</span>
          <span>${escapeHtml(t('blocks.fees', { fees: tx.fees || '0.00000' }))}</span>
          <span>${escapeHtml(t('blocks.payload', { payloads: tx.payloads || 0 }))}</span>
        </div>
      `).join('');
      return `
      <tr>
        <td>${escapeHtml(block.topoheight)}</td>
        <td>${escapeHtml(block.height)}</td>
        <td>${escapeHtml(transactions.length)} / ${escapeHtml(block.txcount)}</td>
        <td>${escapeHtml(block.reward)}</td>
        <td class="hash">
          <div>${escapeHtml(block.hash)}</div>
          <div class="tx-list">${txRows || `<span class="muted">${escapeHtml(t('blocks.noTransactions'))}</span>`}</div>
        </td>
      </tr>
    `;
    }).join('');
    $('blocks').innerHTML = rows || `<tr><td colspan="5">${escapeHtml(t('blocks.empty'))}</td></tr>`;
  } catch (error) {
    toast(error.message);
  }
}

async function refreshWalletTransactions() {
  try {
    const data = await api('/api/wallet/transactions?limit=80');
    const rows = (data.transactions || []).map((tx) => {
      const kind = displayWalletKind(tx.kind);
      return `
      <tr>
        <td>${escapeHtml(tx.time)}</td>
        <td>${escapeHtml(tx.pending ? t('walletTx.pending', { kind }) : kind)}</td>
        <td>${escapeHtml(tx.amount)}</td>
        <td class="hash">${escapeHtml(tx.counterparty || '')}</td>
        <td>${escapeHtml(tx.message || '')}</td>
        <td class="hash">${escapeHtml(tx.txid)}</td>
      </tr>
    `;
    }).join('');
    $('walletTxs').innerHTML = rows || `<tr><td colspan="6">${escapeHtml(t('walletTx.empty'))}</td></tr>`;
  } catch (error) {
    toast(error.message);
  }
}

function switchTab(tab) {
  state.activeTab = tab;
  document.querySelectorAll('.tab').forEach((button) => {
    button.classList.toggle('active', button.dataset.tab === tab);
  });
  document.querySelectorAll('.view').forEach((view) => {
    view.classList.toggle('active', view.id === tab);
  });
  refreshStatus();
  if (tab === 'logs') refreshLogs();
  if (tab === 'explorer') {
    refreshBlocks();
    refreshWalletTransactions();
  }
}

async function toggleMiner() {
  try {
    const threads = Math.max(1, Math.floor((navigator.hardwareConcurrency || 2) / 2));
    const data = await api('/api/miner/toggle', {
      method: 'POST',
      body: JSON.stringify({ threads }),
    });
    toast(data.running ? t('toast.miningStarted') : t('toast.miningStopped'));
    await refreshStatus();
    await refreshLogs();
  } catch (error) {
    toast(error.message);
  }
}

async function showSeed() {
  try {
    const data = await api('/api/wallet/seed');
    $('seedBox').hidden = false;
    $('seedBox').textContent = data.seed;
  } catch (error) {
    toast(error.message);
  }
}

async function sendTransfer() {
  $('transferResult').textContent = '';
  try {
    const data = await api('/api/transfer', {
      method: 'POST',
      body: JSON.stringify({
        destination: $('toAddress').value,
        amount: $('amount').value,
        memo: $('memo').value,
      }),
    });
    $('transferResult').textContent = JSON.stringify(data, null, 2);
    toast(t('toast.txSubmitted'));
    await refreshStatus();
    await refreshWalletTransactions();
  } catch (error) {
    $('transferResult').textContent = error.message;
    toast(error.message);
  }
}

async function installContract() {
  $('contractResult').textContent = '';
  try {
    const data = await api('/api/contracts/install', {
      method: 'POST',
      body: JSON.stringify({ source: $('contractSource').value }),
    });
    $('scid').value = data.scid;
    $('contractResult').textContent = JSON.stringify(data, null, 2);
    toast(t('toast.contractInstallSubmitted'));
  } catch (error) {
    $('contractResult').textContent = error.message;
    toast(error.message);
  }
}

async function callContract() {
  $('contractResult').textContent = '';
  try {
    const args = JSON.parse($('callArgs').value || '[]');
    const data = await api('/api/contracts/call', {
      method: 'POST',
      body: JSON.stringify({
        scid: $('scid').value,
        entrypoint: $('entrypoint').value,
        deroDeposit: $('deposit').value,
        args,
      }),
    });
    $('contractResult').textContent = JSON.stringify(data, null, 2);
    toast(t('toast.contractCallSubmitted'));
  } catch (error) {
    $('contractResult').textContent = error.message;
    toast(error.message);
  }
}

async function queryContract() {
  $('contractResult').textContent = '';
  try {
    const scid = encodeURIComponent($('scid').value.trim());
    const data = await api(`/api/contracts/query?scid=${scid}`);
    $('contractResult').textContent = JSON.stringify(data, null, 2);
  } catch (error) {
    $('contractResult').textContent = error.message;
    toast(error.message);
  }
}

function bind() {
  document.querySelectorAll('.tab').forEach((button) => {
    button.addEventListener('click', () => switchTab(button.dataset.tab));
  });
  document.querySelectorAll('[data-lang]').forEach((button) => {
    button.addEventListener('click', () => setLanguage(button.dataset.lang));
  });
  $('refreshBtn').addEventListener('click', refreshStatus);
  $('minerBtn').addEventListener('click', toggleMiner);
  $('logsBtn').addEventListener('click', refreshLogs);
  $('blocksBtn').addEventListener('click', refreshBlocks);
  $('walletTxBtn').addEventListener('click', refreshWalletTransactions);
  $('seedBtn').addEventListener('click', showSeed);
  $('sendBtn').addEventListener('click', sendTransfer);
  $('installContractBtn').addEventListener('click', installContract);
  $('callContractBtn').addEventListener('click', callContract);
  $('queryContractBtn').addEventListener('click', queryContract);
  $('restartNodeBtn').addEventListener('click', async () => {
    try {
      await api('/api/node/restart', { method: 'POST', body: '{}' });
      toast(t('toast.nodeRestarted'));
      await refreshStatus();
    } catch (error) {
      toast(error.message);
    }
  });
  $('copyAddress').addEventListener('click', async () => {
    const address = state.status?.wallet?.address;
    if (!address) return;
    await navigator.clipboard.writeText(address);
    toast(t('toast.addressCopied'));
  });
}

function init() {
  $('contractSource').value = sampleContract;
  bind();
  applyLanguage();
  refreshStatus();
  refreshLogs();
  setInterval(refreshStatus, 3000);
  setInterval(() => {
    if (state.activeTab === 'logs') refreshLogs();
    if (state.activeTab === 'explorer') refreshWalletTransactions();
  }, 3000);
}

init();
