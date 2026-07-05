# Catfish Coin v0.1.2 / 猫鱼币 v0.1.2

## 中文

本版本修复注册状态提示不准确的问题。

更新内容：

- 钱包页链上状态现在区分“注册交易已提交”“已注册但待确认”“已注册可用”。
- 已注册但确认数不够时，会显示还差大约几个区块确认。
- 转账、安装合约、调用合约前的检查会给出更准确的注册状态错误。
- 如果注册交易已提交但还没进块，会提示等待出块并显示注册交易 TXID。
- 如果节点已确认注册但本地钱包还没同步，会提示等待同步，而不是误报未注册。

使用方式：

1. 下载 `catfish-dero-public-windows.zip`
2. 解压
3. 双击 `CatfishDero.exe`
4. 需要出块时手动点击 `开始挖矿`

注意：

- 本项目仅供学习、娱乐、技术研究和评估。
- 不得用于发行融资、交易撮合、投资理财、洗钱、诈骗、传销、赌博、非法集资、非法挖矿或规避监管。
- 本项目基于 DERO HE，上游 Research License 不授予商业使用或商业分发权利。

## English

This release fixes inaccurate wallet registration status messages.

Changes:

- The wallet page now distinguishes between "registration submitted", "registered but confirming", and "registered and ready".
- When the wallet is registered but not confirmed enough, the UI shows about how many blocks are still needed.
- Transfer, contract install, and contract call preflight checks now report more accurate registration-state errors.
- If the registration transaction is submitted but not mined yet, the client tells the user to wait for a block and shows the registration TXID.
- If the node sees the registration but the local wallet has not synced it yet, the client reports a wallet-sync delay instead of saying the account is unregistered.

Usage:

1. Download `catfish-dero-public-windows.zip`
2. Extract it
3. Double-click `CatfishDero.exe`
4. Click `Start Mining` / `开始挖矿` manually when blocks are needed

Notice:

- This project is for learning, entertainment, technical research, and evaluation only.
- Do not use it for issuance financing, exchange matching, investment solicitation, money laundering, fraud, pyramid schemes, gambling, illegal fundraising, illegal mining, or regulatory evasion.
- This project is based on DERO HE. The upstream Research License does not grant commercial use or commercial distribution rights.
