# Catfish Coin v0.1.5 / 猫鱼币 v0.1.5

## 中文

本版本改进区块链浏览器分页。

更新内容：

- 区块链浏览器不再只显示最近十几个区块。
- 新增 `最新`、`上一页`、`下一页` 按钮，可以一直翻到创世区块。
- 区块接口新增 `start` 参数，并返回当前页 topo 范围和翻页指针。
- 保留 v0.1.4 的挖矿地址成熟期引导出块逻辑。

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

This release improves block explorer pagination.

Changes:

- The block explorer no longer shows only the latest small window of blocks.
- Added `Latest`, `Newer`, and `Older` controls so users can page back to genesis.
- The block API now accepts `start` and returns the current topo range plus pagination cursors.
- Keeps the v0.1.4 bootstrap mining behavior for maturing miner addresses.

Usage:

1. Download `catfish-dero-public-windows.zip`
2. Extract it
3. Double-click `CatfishDero.exe`
4. Click `Start Mining` / `开始挖矿` manually when blocks are needed

Notice:

- This project is for learning, entertainment, technical research, and evaluation only.
- Do not use it for issuance financing, exchange matching, investment solicitation, money laundering, fraud, pyramid schemes, gambling, illegal fundraising, illegal mining, or regulatory evasion.
- This project is based on DERO HE. The upstream Research License does not grant commercial use or commercial distribution rights.
