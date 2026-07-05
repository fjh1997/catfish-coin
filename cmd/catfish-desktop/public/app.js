const state = {
  status: null,
  activeTab: 'wallet',
};

const $ = (id) => document.getElementById(id);

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

  setText('nodeState', daemon.online ? `在线 ${daemon.peers || 0}` : '离线');
  setText('height', daemon.online ? `${daemon.height}/${daemon.topoheight}` : '0');
  setText('minerState', miner.running ? (miner.mode === 'registration-bootstrap' ? '注册中' : '挖矿中') : '未运行');
  setText('balance', wallet.balance || '0.00000');
  setText('address', wallet.address || '...');
  setText('balanceFull', wallet.balance || '0.00000');
  setText('unlocked', wallet.unlocked || '0.00000');
  setText('registered', wallet.registered ? '已上链' : (data.registration?.running ? '上链中' : '未上链'));
  setText('walletHeight', `${wallet.height || 0}/${wallet.daemonHeight || 0}`);

  const minerBtn = $('minerBtn');
  minerBtn.textContent = miner.running ? '停止挖矿' : '开始挖矿';
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

async function refreshBlocks() {
  try {
    const data = await api('/api/blocks?limit=16');
    const rows = (data.blocks || []).map((block) => {
      const transactions = block.transactions || [];
      const txRows = transactions.map((tx) => `
        <div class="tx-row">
          <div>
            <strong>${escapeHtml(tx.type)}</strong>
            <span>${escapeHtml(tx.note || '')}</span>
          </div>
          <code>${escapeHtml(tx.hash || '')}</code>
          <span>大小 ${escapeHtml(tx.size || 0)} B</span>
          <span>费用 ${escapeHtml(tx.fees || '0.00000')}</span>
          <span>Payload ${escapeHtml(tx.payloads || 0)}</span>
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
          <div class="tx-list">${txRows || '<span class="muted">无交易</span>'}</div>
        </td>
      </tr>
    `;
    }).join('');
    $('blocks').innerHTML = rows || '<tr><td colspan="5">暂无区块</td></tr>';
  } catch (error) {
    toast(error.message);
  }
}

async function refreshWalletTransactions() {
  try {
    const data = await api('/api/wallet/transactions?limit=80');
    const rows = (data.transactions || []).map((tx) => `
      <tr>
        <td>${escapeHtml(tx.time)}</td>
        <td>${escapeHtml(tx.pending ? `${tx.kind}（待确认）` : tx.kind)}</td>
        <td>${escapeHtml(tx.amount)}</td>
        <td class="hash">${escapeHtml(tx.counterparty || '')}</td>
        <td>${escapeHtml(tx.message || '')}</td>
        <td class="hash">${escapeHtml(tx.txid)}</td>
      </tr>
    `).join('');
    $('walletTxs').innerHTML = rows || '<tr><td colspan="6">暂无可解密交易</td></tr>';
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
    toast(data.running ? '挖矿已启动' : '挖矿已停止');
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
    toast('交易已提交');
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
    toast('合约安装交易已提交');
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
    toast('合约调用交易已提交');
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
      toast('节点已重启');
      await refreshStatus();
    } catch (error) {
      toast(error.message);
    }
  });
  $('copyAddress').addEventListener('click', async () => {
    const address = state.status?.wallet?.address;
    if (!address) return;
    await navigator.clipboard.writeText(address);
    toast('地址已复制');
  });
}

function init() {
  $('contractSource').value = sampleContract;
  bind();
  refreshStatus();
  refreshLogs();
  setInterval(refreshStatus, 3000);
  setInterval(() => {
    if (state.activeTab === 'logs') refreshLogs();
    if (state.activeTab === 'explorer') refreshWalletTransactions();
  }, 3000);
}

init();
