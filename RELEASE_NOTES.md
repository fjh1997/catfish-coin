# Catfish Coin v0.1.6 / 猫鱼币 v0.1.6

## 中文

本版本修复 PoW 挖矿卡在 9 个 miniblock 后无法出完整块的问题。

更新内容：

- 修复 miner 提交 miniblock 后被误判为 `Account Unregistered` / `unregistered miner` 的问题。
- 修复原因：DERO miniblock 只序列化矿工地址哈希的前 16 字节，节点侧校验不能按完整 32 字节比较。
- 本地验证已从高度 28 挖到 30，`rejected=0`，钱包余额已增加。
- 保留 v0.1.5 的区块浏览器分页功能。

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

This release fixes PoW mining getting stuck after 9 miniblocks without producing a full block.

Changes:

- Fixed miner submissions being incorrectly rejected as `Account Unregistered` / `unregistered miner`.
- Root cause: DERO miniblocks serialize only the first 16 bytes of the miner address hash, so node-side validation must not compare all 32 bytes.
- Locally verified mining from height 28 to 30 with `rejected=0`; the wallet balance increased.
- Keeps the v0.1.5 block explorer pagination feature.

Usage:

1. Download `catfish-dero-public-windows.zip`
2. Extract it
3. Double-click `CatfishDero.exe`
4. Click `Start Mining` / `开始挖矿` manually when blocks are needed

Notice:

- This project is for learning, entertainment, technical research, and evaluation only.
- Do not use it for issuance financing, exchange matching, investment solicitation, money laundering, fraud, pyramid schemes, gambling, illegal fundraising, illegal mining, or regulatory evasion.
- This project is based on DERO HE. The upstream Research License does not grant commercial use or commercial distribution rights.
