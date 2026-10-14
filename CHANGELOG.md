# Changelog

## 1.0.53

### Added

- `run_code` cards ride a subtle background slab — a shade darker than
  the window background in every theme (dark, light and the terminal's
  own) — so a stretch of executed code is visible at a glance.
- Tool cards read like the web client: the title shows the command, the
  path or the pattern instead of raw JSON; an expanded result paints a
  structured card — a read window with line numbers and syntax
  highlighting, a colored diff, search matches grouped by file, web
  citations — and plain output colors diff lines.
- Thinking renders as markdown (quietly), so lists and code inside a
  chain of thought read like the answer does.

### 新增

- `run_code` 卡片衬一层淡淡的背景色——深色、浅色及终端自绘主题下都
  比窗口背景略深——一眼即可认出这是一段执行的代码。
- 工具卡片对齐网页端：标题显示命令、路径或模式而非原始 JSON；展开
  结果渲染结构化卡片——带行号与语法高亮的文件读取窗口、彩色差异、
  按文件分组的搜索命中、网页引用——普通输出中的差异行也会着色。
- 思考内容改为 markdown 渲染（弱化色），思考中的列表与代码与正文
  同样易读。

### Changed

- Fork moved from `f` to `ctrl+f`, and dock tabs from bare `1`–`5` to
  `ctrl+1`–`ctrl+5`: typing a letter or a digit in the input no longer
  triggers them.

### 变更

- 分叉快捷键由 `f` 改为 `ctrl+f`，侧栏标签由 `1`–`5` 改为
  `ctrl+1`–`ctrl+5`：在输入框打字不再误触这两个功能。

### Fixed

- Works with the updated dsh host again: live streaming, tool results
  and turn endings all changed shape on the wire; abandoned model
  retries no longer leave half an answer on screen, and a turn ended by
  forking shows a quiet marker instead of an error.
- Two keys break the line in the input bar: `shift+enter` and `ctrl+j`.
  `shift+enter` is now reported correctly in kitty, WezTerm, Ghostty,
  foot and iTerm2; a terminal that cannot tell it from enter at all
  (Alacritty, GNOME Terminal) still sends, and there `ctrl+j` is the
  newline.
- `/new` no longer leaves the top bar reading "(no session)": a fresh,
  still-untitled session shows "new session".
- The session window and dialog boxes no longer cover the pinned input
  bar at the bottom of the window.
- The subagent dock (ctrl+t) lists the session's real children with
  their live running state; it used to query an endpoint the host does
  not serve and always looked empty.
- The spinner glyph before the thinking status in the top bar is gone.
- Fixed a crash at narrow window sizes: the top bar could compute a
  negative pad.
- The turn timer no longer resets to 0s on long turns: a mid-turn
  reconnect keeps the turn's known start instead of losing it together
  with the history page it fell off.
- Popups (the session window and all dialogs) now scale with the window
  (about 60% of its width and height) instead of staying a fixed size.
- Fixed popup borders losing their right edge or going crooked in CJK
  locales: the width model now counts ambiguous glyphs one column wide,
  the way terminals render them.
- Popups (the session window and all dialogs) now wear a raised panel
  color of their own instead of the terminal's background, so an open
  box no longer blends into the conversation behind it.
- The `run_code` background slab stays under its own card: on the card's
  rows it used to tint the whole subagent dock (ctrl+t), and it colored
  the row below the card too.
- The screen no longer stays garbled until the window is redrawn: the whole
  frame is drawn again once a second, in place, so a row the terminal
  mangles or loses repairs itself.

### 修复

- 适配新版 dsh 宿主：流式输出、工具结果、回合结束的线上格式均已
  变化；被放弃的重试不再在屏幕上留下半截回答，分叉结束的回合显示
  弱化标记而非报错。
- 输入框有两个换行键：`shift+enter` 与 `ctrl+j`。`shift+enter` 现在在
  kitty、WezTerm、Ghostty、foot、iTerm2 中都能被正确识别；终端本身无法
  区分它与回车的（Alacritty、GNOME 终端）仍会发送，此时用 `ctrl+j`
  换行。
- `/new` 后顶栏不再显示“（无会话）”：尚无标题的新会话显示“新会话”。
- 会话窗口与对话框不再遮挡窗口底部固定的输入栏。
- 子代理面板（ctrl+t）现在列出真实的子代理及其运行状态；此前查询
  宿主不存在的接口，始终显示为空。
- 移除顶栏 thinking 状态前的转圈图标。
- 修复窗口过窄时的崩溃：顶栏补齐宽度可能算出负数。
- 长回合的计时不再退回 0s：回合中途重连时，已知的回合起点不再随
  历史分页一起丢失。
- 弹窗（会话窗口与各类对话框）改为随窗口按比例缩放（约 60%），
  不再固定尺寸。
- 修复中文环境下弹窗右边框缺失、边框错位：宽度模型改为按终端
  实际渲染计宽（歧义宽度字符按 1 列计）。
- 弹窗（会话窗口与各类对话框）改用独立的浮起面板色，不再与终端
  背景同色，打开时不再与背后的会话内容混在一起。
- `run_code` 的背景色只衬在卡片自身：此前打开侧栏（ctrl+t）时，卡片
  所在行会把整个侧栏一并染色，卡片下面一行也会被多盖一行。
- 界面错乱不再需要重绘窗口才会恢复：整屏每秒原地重画一次，被终端
  弄乱或弄丢的行会自行恢复。

## 1.0.52

### Added

- The model picker can fetch a provider's live model list: `f` probes
  the provider's endpoint, models missing from the catalog open a
  selection dialog (all pre-checked); `space` toggles a model and
  `enter` adds the checked ones to the provider's settings.
- The input deck (input frame + status line) is pinned to the window
  bottom: toasts, the queue strip and the slash menu overlay the
  transcript instead of pushing the bar around.

### 新增

- 模型选择器可获取服务商端点的实时模型列表：`f` 查询端点，目录之外
  的模型弹出选择框（默认全部勾选），`space` 切换勾选，`enter` 把已
  勾选的模型加入服务商配置。
- 输入区（输入框 + 状态栏）固定钉在窗口底部：toast、排队条、斜杠
  菜单改为覆盖在会话记录上，不再挤动输入区。

### Changed

- Streaming performance: long streams batch their screen updates, and a
  long session no longer slows down as its history grows; per-frame
  rendering overhead is reduced (theme styles and language tables are
  pre-computed).
- Auto-start: a host that accepts connections but never answers is now
  detected as hung and killed and relaunched, instead of stalling the
  client; long one-off runs are no longer cut off by a 60s hard
  timeout.
- Tightened file permissions: the config file, the auto-start log and
  the baseline cache are now written 0600.

### 变更

- 流式性能优化：长流式输出改为批量刷新界面，长会话不再随历史增长而
  变慢；降低每帧渲染开销（主题样式与语言表预计算）。
- 自动启动：可连接但不响应的宿主现在会被识别为挂起并杀掉重启，客户
  端不再卡死；长时单次运行不再被 60 秒硬超时截断。
- 收紧文件权限：配置文件、自动启动日志与基线缓存改为 0600 写入。

### Fixed

- The top bar's model name updates the moment a model is chosen, even
  with no session current: the picker opens on the host's global
  catalog and stages the pick, which the first session to become
  current applies.
- 选择模型后顶栏模型名立即更新，无会话时亦然：选择器可基于全局目录
  打开，所选模型暂存，由第一个开启的会话应用。

- The transcript no longer serves stale or misplaced rows after loading
  older history (ctrl+u) or a mid-turn re-baseline: rebuilt and prepended
  items carried render IDs that collided with cached rows, so the board
  kept showing superseded content until a full refresh (ctrl+e).
- Data races in the multiplexed downlink: stream IDs are now allocated and
  stream endpoints read under the stream lock, so concurrent follows can
  no longer collide or be routed to the wrong stream.
- The workspace registry mirror is now copy-on-write: removing, updating,
  or adding a workspace no longer edits the slice handed to earlier
  readers.
- Turn token totals can no longer go negative when a streamed usage
  preview is not confirmed by the final message, and an empty queue
  snapshot no longer marks an idle session as running.
- A corrupt stored zh.json no longer degrades the whole face to English:
  the broken file is skipped and the built-in Chinese table is used, and
  the catalog files are written atomically so a crash cannot corrupt them.
- Auto-start's lsof-less kill fallback now targets the host on its own
  port, leaving dsh web on other ports alone.
- Host-provided text that carries terminal escape sequences (e.g. pasted
  terminal output) no longer clobbers the UI: such text is stripped before
  it reaches the screen, so the top bar can no longer be wiped out while
  scrolling the transcript.

### 修复

- 加载更早历史（ctrl+u）或回合中途重新基线后，消息流不再残留旧行或显示
  错位内容：重建与前置的条目此前会撞上缓存行已有的渲染编号，旧内容一直
  保留到强制刷新（ctrl+e）为止。
- 修复多路复用下行链路的并发竞争：流编号分配与端点读取现在都在流锁保护
  下进行，并发关注不再撞号或路由到错误的流。
- 工作区注册表镜像改为写时复制：增删改工作区不再原地修改已交给早期
  读者的切片。
- 流式用量预览未获最终消息确认时，回合 token 统计不再出现负数；空队列
  快照不再把空闲会话误标为运行中。
- zh.json 损坏时不再整体退化为英文：跳过损坏文件、回退内置中文表；
  语言文件改为原子写入，崩溃不会留下半截文件。
- 自动启动在缺少 lsof 时的杀进程回退现在只针对本端口宿主，不影响其它
  端口的 dsh web。
- 宿主下发的文本若携带终端转义序列（如粘贴的终端输出），不再破坏界面：
  这类文本在入屏前统一去除，滚动消息流时顶栏不再被抹掉。

## 1.0.51

### Added

- Hermetic functional test suite (`make test-func`, included in
  `make check`): the built binary runs against a private fake dsh web,
  covering status/ls/new/history/models/workspaces, one-shot (arg and
  pipe), and the error paths.

### Fixed

- Cold auto-start no longer dials the freshly launched dsh web with the
  stored (stale) token: the launched host's own launch token is installed
  on the live connection as soon as it is captured, and the dead host's
  stored cookie is dropped.
- Auto-start no longer sticks in "connecting": the fresh dsh web answers
  404 briefly after its port opens (its routes are claimed later), and a
  probe inside that window locked the client onto the legacy wire for the
  whole session — the fresh token was captured but never used. The 404
  boot window now keeps the protocol undecided until the host settles,
  and a launch that meets the window waits for the late token instead of
  settling token-less.
- Question / approval confirmation: the "press ctrl+i to open" hint no
  longer stays lit after the answer — the pending frame settles as soon
  as the host accepts it.
- The top status bar no longer disappears when the multi-line input is
  taller than the window: the input keeps only what fits and the bar
  stays on line one.
- A mid-turn re-baseline no longer loses the turn's final summary when
  the history page's tail lags the live events.
- The question popup's answers reach the agent again on the upgraded
  host: the answer batch still rode the legacy envelope, so the host's
  ask_user_question read an empty answers slot.

### 新增

- 无外部依赖的功能测试套件（`make test-func`，已并入 `make check`）：
  用私有的假 dsh web 驱动构建出的二进制，覆盖
  status/ls/new/history/models/workspaces、一次性命令（参数与管道）
  及各错误路径。

### 修复

- 冷启动自动拉起 dsh web 后，不再拿已存的旧 token 去连接新进程：新 token
  捕获到的第一时间写入当前连接，并清掉上一代进程的旧 cookie。
- 自动拉起后不再卡在"连接中"：新宿主端口开放后有一小段时间回 404（路由
  稍后才注册），此前的探测会把协议代际永久钉在旧版线上——新 token 捕获
  了却从未用上。现在 404 启动窗口内保持协议待定、等宿主就绪后再判定；
  看到窗口的启动流程会等待迟到的 token，而不是空 token 收尾。
- 提问 / 审批确认窗口：回答后不再残留“按 ctrl+i 打开”的提示——宿主接受
  回答时待答帧立即清除。
- 多行输入高于窗口时，顶部状态栏不再消失：输入只显示放得下的行，状态栏
  保持在第一行。
- 回合中途重新基线化时，若历史页尾部落后于实时事件，不再丢失回合的
  最后总结。
- /permission 命令（及 ctrl+p 预设切换）在升级后的宿主上恢复工作：
  执行请求的附件参数名写错，网关拒绝了每次调用。
- 升级后的宿主上，提问弹窗的回答重新送达 agent：回答批次此前仍携带
  旧版信封，宿主的 ask_user_question 读不到答案。

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
