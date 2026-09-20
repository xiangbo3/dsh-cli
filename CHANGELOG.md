# Changelog

## 1.0.50

### Fixed

- Workspace switch (and new sessions) no longer fail on hosts whose
  default preset is stale — the id an upgrade renamed or the user
  deleted: the client now pins a valid preset (standard, or the first
  intact one) when the roster flags no default.
- PTC mode's preset id follows the upgraded host (ptc instead of the
  pre-rename code): the top-bar mode label and one-shot
  `--preset ptc` resolve again.

### 修复

- 修复宿主默认 preset 过期（升级改名或用户删除）时切换 workspace、
  新建会话报错的问题：名单中无默认标记时，客户端自动钉住可用 preset
  （standard 或第一个完好的）。
- PTC 模式的 preset id 跟随升级后的宿主（ptc，替代改名前的 code）：
  顶栏模式标签与 `--preset ptc` 一次性命令恢复可用。

## 1.0.49

### Fixed

- /mode switching against the upgraded dsh web build: the typed command
  and the picker work again, and the picker now marks the current mode.
- /new inherits the active session's mode on upgraded hosts instead of
  failing on a stale default preset.
- The upgraded build's internal dispatch events no longer clutter the
  transcript with raw "tool/ptc-dispatch" lines.
- Dropped a model-list route for an endpoint the upgraded host no
  longer has; old hosts are unaffected.

## 1.0.49

### 修复

- 修复升级后的 dsh web 下 /mode 切换报错；选择器现在会标出当前模式。
- 升级后的宿主上 /new 正确继承当前会话的模式，不再因宿主默认 preset
  过期而失败。
- 升级后构建的内部分发事件不再往对话窗口刷 "tool/ptc-dispatch" 原始行。
- 移除升级后宿主已不存在的模型列表路由；旧版宿主不受影响。

## 1.0.48

### Fixed

- Keybindings under tmux (extended keys re-encoded by tmux) no longer
  die silently; the dock toggle moved from ctrl+b to ctrl+t.
- Copy/paste failures fixed (clipboard outside a full desktop session,
  the terminal parking on copy, foot's default keybindings); text
  selection now auto-copies on release.

### Changed

- Popups render as solid cards in the main window's background color —
  no text shows through.

## 1.0.48

### 修复

- 修复 Tmux 下快捷键被静默吞掉的问题；侧栏切换从 ctrl+b 移到 ctrl+t。
- 修复复制粘贴失效（非完整桌面会话的剪贴板、复制时终端挂起、foot 默认
  键绑定冲突）；选中文字松开即自动复制。

### 改进

- 弹出窗口改为与主窗口同色的实体卡片，文字背后不再透出。

## 1.0.47

### Fixed

- Windows release builds: the windows/amd64 and windows/386
  cross-builds failed on an undefined syscall; the process kill now uses
  portable calls (unix platforms unchanged).

## 1.0.47

### 修复

- Windows 发布构建：windows/amd64 与 windows/386 交叉编译因未定义的
  系统调用失败；进程终止改用可移植调用（unix 平台不变）。

## 1.0.46

### Added

- Compatibility with the cookie-gated dsh web build: the launch token is
  exchanged for a session cookie; old builds keep working unchanged.
- `--token` flag on all commands.
- Cold-host auto-start: a stopped local dsh web is launched automatically
  (`dsh web --no-open`) and its launch token captured; opt out with
  `--no-autostart`.
- /status billing tab: set the input / output price per million tokens;
  token counts then carry the cost.
- The status bar shows the server's recent generation rate and keeps it
  after the session goes idle.

### Changed

- On the new build, all server push (session events, queue / job control,
  workspaces) arrives over a single multiplexed connection.
- Approvals and questions answer over the new build's result channel; the
  legacy channel is unchanged.
- The auto-started dsh web persists across dsh-cli runs; a live web that
  rejects the stored token is relaunched with a fresh one.
- Auth failures report their cause (no launch token vs. rejected token)
  and print the matching recovery line.

### Fixed

- ctrl+d on an empty input is now delete-forward, not a quit.
- Per-turn throughput counts fresh tokens only (cache re-reads no longer
  inflate the rate).
- /status shows the dsh host version when the build doesn't publish one
  on the wire.
- Fixed the input-bar caret on wrapped (CJK) text.
- Auto-start no longer waits the full token deadline for old builds that
  print no token.
- The subagent session window is scrollable.
- The status bar's token readout is live again (streamed estimate while a
  step runs).
- /status token statistics now track the host's cumulative totals in real
  time.

## 1.0.46

### 新增

- 兼容 cookie 鉴权的新版 dsh web：启动 token 换取会话 cookie；老版行为
  不变。
- 全部命令支持 `--token` 参数。
- 冷启动自举：本机 dsh web 未运行时自动启动（`dsh web --no-open`）并抓取
  启动 token；`--no-autostart` 可关闭。
- /status 新增计费区块：设置每百万 token 的输入/输出价格，token 统计行
  附带花费。
- 状态栏显示服务器最近的生成速度，会话空闲后保留最后读数。

### 改进

- 新版构建的会话事件、队列/任务、工作区等推送全部走单条多路复用连接。
- 审批与用户提问改经新版结果通道应答（老版通道不变）。
- 自动启动的 dsh web 跨 dsh-cli 运行持续存活；拒绝已存 token 的运行中
  web 会被重启并换新 token。
- 鉴权失败会说明原因（未配置 token / token 被拒），一次性命令打印对应
  恢复提示。

### 修复

- 空输入框按 ctrl+d 不再退出（改为前删，退出仍是 ctrl+c / ctrl+q）。
- 每回合吞吐量只统计新 token，缓存重读不再推高数值。
- 新版宿主不发布版本号时，/status 回退显示本地 dsh 启动器的版本。
- 修复输入框换行（CJK 行）后的光标错位。
- 自动启动旧版（不输出 token）的 dsh web 时不再空等 90 秒超时。
- 子代理会话窗口支持滚动。
- 状态栏 token 读数恢复实时（流式期间按已流出内容估算）。
- /status 的 token 统计改为以宿主的累计用量为准，实时跟随。

## 1.0.45

### Added

- Subagent view: a SUBS dock tab listing direct children; enter opens a
  read-only child transcript (paged, x to interrupt); dock rows support
  @child mentions.
- @ completion lists children and files/directories.
- The terminal title follows the active session and is restored on exit.
- Unread indicator ("↓ N new") when scrolled up; Home/End/wheel keep the
  follow semantics.
- A max-tokens turn ends with an actionable hint (compact the context or
  switch to a larger model).
- Long session titles truncate in narrow windows.
- One-time warning toasts (DSH_INSECURE in effect, locale files lagging).
- Plain-http non-loopback remotes keep a standing warning in the status
  bar.
- .golangci.yml and a make lint target.

### Changed

- Host-provided display strings are sanitized of ANSI escapes at the
  store boundary.
- User-triggered roster refreshes are coalesced (at most one in flight).
- usage.json is 0600; config.json is written atomically.
- A TUI panic restores the terminal and prints the stack before exiting.
- Approvals use explicit a/d choices (bash needs a second a, d retracts);
  the parameter line shows the real command.
- Tool cards show elapsed time; edit/write cards expand inline to full
  text or a diff preview.
- alt+enter force-queues; esc clears the input line.
- An input estimate shows for the thinking block when the context window
  is unknown.
- The status bar hint steps through dock, then sidebar, controls.
- A non-active session's turn-end flashes its roster row.
- Boot baseline cache (DSH_CLI_NO_BOOT_CACHE disables it); versioned
  release artifacts and a source tarball.
- Locale and config files carry a _version stamp: stale files are
  regenerated or backfilled on upgrade, user edits are preserved within a
  version.

### Fixed

- Narrow (<30 col) top bar could overflow with long titles.
- Notification text kept stray ANSI parameter bytes.

## 1.0.45

### 新增

- 子代理视图：dock 新增 SUBS 标签（直接子代理列表），enter 打开只读
  子代理转录窗口（分页、x 中断）；dock 行支持 @child 内联提及。
- @ 补全：输入 @ 列出子代理与文件/目录，↑↓/enter 选用。
- 终端标题跟随活动会话（模式图标 + 标题 + (running)），退出时恢复。
- 未读指示：回滚离开底部时状态栏显示 "↓ N new"，Home/End/滚轮保持跟随
  语义。
- max-tokens 结束附可操作提示（/compact 压缩或换更大上下文窗口的模型）。
- 顶栏在窄窗下自动截断长会话标题。
- DSH_INSECURE 生效、语言文件落后内置表时各显示一次性警告。
- 明文 http 非回环远端在状态栏常驻警示标记。
- 新增 .golangci.yml 与 make lint 目标。

### 改进

- 主机驱动的显示串在 store 入口做 ANSI 消毒，防止终端转义混入。
- 用户动作触发的 roster 刷新合流（至多一个在途）。
- usage.json 权限降为 0600；config.json 原子写。
- TUI panic 时恢复终端并打印栈后退出。
- 审批卡改显式 a/d 选择（bash 需二次 a 确认，d 可反悔）；参数行显示
  真实命令。
- 工具卡显示耗时；edit/write 卡可就地展开全文或 diff 预览。
- alt+enter 强制入队；esc 清空输入行。
- 上下文窗口未知时 thinking 段显示输入估算。
- 非活动会话 turn-end 时 roster 行闪烁 1.5s。
- 启动基线缓存（DSH_CLI_NO_BOOT_CACHE 可关）；版本化发布产物与源码
  tarball。
- 语言文件与 config 带 _version 标记：升级时自动重新生成/补齐，同版本内
  保留用户编辑。

### 修复

- 修复窄窗（<30 列）顶栏在长标题下溢出。
- 修复通知文本残留 ANSI 参数字节。
