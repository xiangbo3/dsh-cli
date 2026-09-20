# Changelog

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

- Fixed keybinding failures under tmux (extended keys, csi-u): once
  dsh-cli requests modifyOtherKeys2, tmux re-encodes modified keys into kitty CSI-u,
  which the v1 key reader didn't know, so every chord (ctrl+t, ctrl+h,
  ctrl+c, alt+enter, copy/paste) died silently. A general CSI-u decoder now
  shares one (mods, sym) mapping table with the modifyOtherKeys2 decoder
  (one table, both protocols), folding in the old csiuChord / csiuShiftEnter
  cases. The dock moved off ctrl+b (the editor's cursor-left) to ctrl+t.
  `TestCsiuKey`, `TestTmuxCsiuFlow`, `TestDockToggleKey` pin it.
- Fixed copy/paste failures (three independent causes): (1) outside a full
  desktop session the native clipboard tools failed (missing XDG_RUNTIME_DIR)
  — `ensureClipEnv()`
  now supplies the standard runtime path; (2) every copy/paste parked the
  terminal and dropped mouse tracking, so the first copy handed selection to
  the terminal's native and the next ctrl+shift+c copied nothing — now crush's
  no-park pattern (dual-channel copy: OSC 52 + native tool; paste reads the
  native tool); (3) foot's default `[key-bindings]` bound the chords to its
  own clipboard — dsh-cli now requests XTerm modifyOtherKeys2 and decodes its
  reports (foot users: drop `Control+Shift+c` / `Control+Shift+v` from
  `[key-bindings]`). Text selection now auto-copies on release (crush's
  pattern); ctrl+shift+c stays the explicit repeat.
  `TestClipboardNoTerminalPark`, `TestCopyPasteChords`, `TestMok2Key`,
  `TestMousePickFlow` pin it.

### Changed

- Popups (help, model, search, question, approval, rename, language, session list) now sit as solid cards in the main window's own background color: the system-derived palette takes the terminal's reported background, the built-in profiles paint the terminal's default background (SGR 49) — no text shows through the box, and the box no longer shifts color against the main area; the hairline frame stays. The SGR scan treats an explicit default background as a painted surface (opaque), not a hole. `TestPopupOpaqueCard` pins it.

## 1.0.48

### 修复

- 修复 Tmux 下快捷键失效问题（扩展键、csi-u）：dsh-cli 请求 modifyOtherKeys2 后，
  tmux 把修饰键重编码为 kitty CSI-u，v1 键解析器不认识，所有和弦
  （ctrl+t、ctrl+h、ctrl+c、alt+enter、复制/粘贴）被静默吞掉。新增通用
  CSI-u 解码器，与 modifyOtherKeys2 解码器共享一张 (mods, sym) 映射表
  （一张表通吃两种协议），并入原 csiuChord / csiuShiftEnter 特例。侧栏
  切换从 ctrl+b（编辑器光标左移）移到 ctrl+t。
  `TestCsiuKey`、`TestTmuxCsiuFlow`、`TestDockToggleKey` 锁定。
- 修复复制粘贴功能失效（三层原因）：(1) 非完整桌面会话下原生剪贴板工具因缺 XDG_RUNTIME_DIR
  失败——`ensureClipEnv()` 现补上标准运行时路径；(2) 每次复制/粘贴都挂起
  终端并丢失鼠标跟踪，首次复制后选区交给终端原生、下次 ctrl+shift+c 便
  复制不到——现按 crush 模式不挂起（双通道复制：OSC 52 + 原生工具，粘贴
  读原生工具）；(3) foot 默认 `[key-bindings]` 把和弦绑给了它自己的剪贴板
  ——dsh-cli 现请求 XTerm modifyOtherKeys2 并解码其报告（foot 用户：从
  `[key-bindings]` 移除 `Control+Shift+c` / `Control+Shift+v`）。选中文本
  松开即自动复制（crush 模式）；ctrl+shift+c 保留为显式重复。
  `TestClipboardNoTerminalPark`、`TestCopyPasteChords`、`TestMok2Key`、
  `TestMousePickFlow` 锁定。

### 改进

- 弹出窗口（帮助、模型、搜索、问题、批准、重命名、语言、会话列表）背景现为与主窗口自身背景同色的实体卡片：系统派生配色取终端报告的背景色，内置配色绘终端默认背景（SGR 49）——文字背后不再透出，盒子不再与主区域色差，保留细线边框。SGR 解析把显式默认背景视为已绘制的表面（不透明）而非透明洞。`TestPopupOpaqueCard` 锁定。

## 1.0.47

### Fixed

- Windows release builds: the non-unix webhost file used `syscall.Kill`,
  which Go's Windows syscall package does not define, so the
  windows/amd64 and windows/386 cross-builds failed with
  `undefined: syscall.Kill`. The kill now goes through
  `os.FindProcess` + `Process.Kill` (a forced terminate on Windows);
  unix hosts keep their process-group kill unchanged.

## 1.0.47

### 修复

- Windows 发布构建：非 unix 的 webhost 文件使用了 Go 的 Windows syscall
  包未定义的 `syscall.Kill`，交叉编译 windows/amd64 与 windows/386
  时报 `undefined: syscall.Kill`。现改经 `os.FindProcess` +
  `Process.Kill`（Windows 上为强制终止）；unix 各平台保持原有进程组
  kill 不变。

## 1.0.46

### Added

- Compatibility with the cookie-gated dsh web build: the launch token the
  server prints at boot (`http://…/?token=…`) is exchanged for a session
  cookie on first contact; subsequent requests ride the cookie. Precedence:
  `--token` flag > `DSH_LAUNCH_TOKEN` env > the token stored in config.
  The exchanged cookie is stored with its minting host and re-applied on
  later boots (skipped when the base URL changed); a stale cookie clears
  with one re-exchange + retry.
- `--token` flag (all commands), wired to the one-shot and TUI paths.
- Legacy builds keep working unchanged: the dialect is probed once per
  host (a 401 index = cookie-gated, a 200 index = legacy).

- Cold-host auto-start: when the configured loopback dsh web is not
  running, dsh-cli now launches `dsh web --no-open` itself (same
  host/port as the configured URL), captures the `?token=…` line the
  server prints, stores the token in the config, and shuts the child
  down when dsh-cli exits — on every exit path, including the
  `os.Exit` ones. A dsh web that was already running is left alone
  (never killed). `--no-autostart` or `$DSH_NO_AUTOSTART=1` opts out;
  `$DSH_BIN` names a non-PATH dsh launcher. A non-loopback base URL is
  never auto-started, and a merely slow (timing out) host is not
  relaunched.

- Token price setting in /status: a new "billing" tab (the 4th section,
  reached with → / tab / 4) edits the input and output cost per one
  million tokens in two fields (↑↓ switches between them); a valid edit
  applies at once — no enter to confirm, a half-typed state ("2.") waits
  for the next digit, and a blank field clears. Once set, the cost
  follows every token count in the popup ("1.2M in / 400.4K out (133)").
- The status bar's generation rate ("… · 70t/s") is the server's recent
  generation average and stays displayed once a session has generated
  anything: an idle session keeps showing the last rate instead of the
  readout going off.

### Changed

- Single multiplexed downlink `/api/remote.mux` on the new build (the
  legacy dual-socket downlink is untouched): session events, queue/job
  control, and the workspace registry all arrive over one socket and are
  re-emitted into the existing internal frame stream, so the UI and store
  see no new protocol.
- Approvals and user questions now answer over the `$events` result
  channel (`/api/$events/result`) on the new build, keyed by the
  $events client id and the frame's eventId; the legacy `/api/respond`
  channel is unchanged.
- History pages on the new build (`session/page`): the page's tail seq is
  taken from the live session follow (a throwaway follow is opened when
  none is attached), since the new wire exposes no unary tail surface.

- Cookie-gated dsh web: the downlink now recovers a rejected or expired
  session cookie on its own — when the `/api/remote.mux` upgrade 401s and
  a launch token is held, the client re-exchanges before the next dial
  (the gate rejects before any HTTP retry path could run), so an expired
  cookie no longer strands the TUI on a silent backoff.
- Auth failures carry their cause: a 401 that a re-exchange could not
  clear now reports whether no launch token is configured or the held
  one was rejected (dsh web restarted), and one-shot commands print the
  matching recovery line (`dsh-cli --token <token>`).
- Dialect probe reads the 401 body: a marker-less 401 index is a legacy
  bearer-gated host (it keeps the legacy wire, whose bearer rides the
  DSH_TOKEN env), and a unary 404 retries once on the other wire as a
  misclassification safety net.

- Dsh web lifecycle rework: the auto-started dsh web now PERSISTS across
  dsh-cli runs — dsh-cli no longer kills it on exit (the child is
  re-parented, and its stdout/stderr ride
  `~/.dsh-cli/webhost-<port>.log` instead of dsh-cli's pipes, so it
  cannot die on a post-exit pipe write). When dsh-cli finds a dsh web
  already running it connects with the stored launch token; if that live
  web rejects the stored credentials (e.g. after a manual dsh web
  restart), dsh-cli kills the live web (via its port listeners),
  relaunches it, and stores the fresh token before connecting.

### Fixed

- ctrl+d on an empty input no longer quits the program: the released
  build treated it as the EOF quit, now it is the editor's delete-forward
  (a no-op on an empty line). The quit chords remain ctrl+c and ctrl+q.
- Per-turn throughput now counts a turn's fresh tokens: cache re-reads
  of already-processed context no longer inflate the t/s number (a
  multi-million-token cache hit no longer reads as millions of tokens of
  work).
- The /status connection line reports the dsh host version on builds that
  don't publish one on the wire: the local dsh launcher (the auto-start
  binary) answers for it.
- Input bar caret on wrapped text: a row's extent is now counted in
  runes, so a multi-byte (CJK) row no longer swallows later rows' caret
  positions; a wrapped row drops its break-point space so it never
  overruns the frame; and the end-of-line caret on a full row falls back
  to the last character instead of a clipped extra cell.
- Auto-launch when the started dsh web is an old build that prints no
  launch token: dsh-cli no longer waits the full 90s token deadline for a
  token that never comes — it settles into the token-less (bare)
  connection as soon as the port answers, in about 2s, and no longer
  kills the host it just started.
- The subagent session window is scrollable now: the child's transcript
  rarely fits the popup body, so the window walks with the arrows,
  pgup/pgdn, home/end, and the mouse wheel (the hint line lists them);
  while the window is open the wheel no longer scrolls the hidden
  transcript behind it.

- The status bar's token readout (tokens: ↓… ↑…) is live again: a step's
  usage chunk (the adapter's accounting, which lands ahead of the
  canonical message) now counts into the turn tokens and the in-flight
  item at once — and while a step is still streaming, the meter carries
  the streamed output as a live estimate until the usage lands. The
  canonical message folds only the remainder, so nothing is counted
  twice (the per-step t/s samples and the /status metered totals keep
  their exact values).

- /status token statistics now track the host in real time: the
  persistent totals (all-time / month / week / day, per workspace, per
  session) are driven by the host's own cumulative tokenUsage projection
  — each step's sample is counted as the host reports it (the session/
  projection push, the tail page's baseline, and the session list), the
  in-flight step's tokens included, so the aggregate no longer reads
  under the web client's numbers between steps. While the /status popup
  is open, the active session's baseline is re-confirmed every few
  seconds for a live readout. The first baseline of a session also
  repairs what per-event counting missed (pruned history, web-only
  steps) without double-counting what it already had; hosts without the
  projection keep the old per-event accounting.

## 1.0.46

### 新增

- 兼容 cookie 鉴权的新版 dsh web：服务端启动时打印的 launch token
  （`http://…/?token=…`）首次接触时换取会话 cookie，后续请求带 cookie 访问。
  优先级：`--token` 参数 > `DSH_LAUNCH_TOKEN` 环境变量 > config 中存储的
  token。换取到的 cookie 连同其签发主机一并存入 config，后续启动自动
  复用（base URL 变化时不套用）；cookie 过期时自动重换并重试一次。
- `--token` 参数（全部命令），one-shot 与 TUI 路径均已接通。
- 老版 dsh web 行为不变：dialect 每主机探测一次（index 401 = 新版
  cookie 门，200 = 老版）。

- 冷启动自举：配置的本机 dsh web 未运行时，dsh-cli 自动以 `dsh web
  --no-open` 启动（host/端口与配置 URL 一致），抓取服务器打印的
  `?token=…` 行并存入配置，dsh-cli 退出时关闭子进程——覆盖所有退出
  路径（含 `os.Exit`）。已在运行的 dsh web 保持原样（绝不误杀）。
  `--no-autostart` 或 `$DSH_NO_AUTOSTART=1` 可关闭；`$DSH_BIN` 可指定
  不在 PATH 中的 dsh 启动器。非本机的 base URL 不自动启动；仅超时（未
  拒绝）的主机不视为停机。

- /status 新增 token 价格设置：新增“计费”标签页（第 4 个区块，→ / tab / 4
  进入），按每百万 token 的价格分设输入 / 输出两个价格字段（↑↓ 切换）；
  输入合法数字立即生效，无需回车确认（“2.” 这类半截状态等下一位数字，
  留空清除）。设置后弹窗内所有 token 统计行附带花费（如 “1.2M 输入 /
  400.4K 输出（133元）”）。
- 状态栏生成速度（“… · 70t/s”）为服务器最近的平均生成速度，会话一旦
  产生过生成就一直显示：空闲时会话保留最后的速率，而不是读数消失。
- 顶部状态栏居中显示时钟（HH:MM:SS，本地时间）；窗口过窄、两侧腾不出
  位置时自动隐藏。
- 状态栏 tokens 行后新增每回合吞吐量（"tokens: ↓… ↑… · 70t/s"）：
  统计范围为单个回合的每秒平均 token 数——回合进行中实时计算，回合
  结束后显示最终值。

### 改进

- 新版构建使用单条多路复用下行 `/api/remote.mux`（老版双 socket 下行
  保持不变）：会话事件、队列/任务控制、工作区注册表全部走一条 socket，
  重新映射回既有内部帧流，UI 与 store 无需感知新协议。
- 新版构建的审批与用户提问改经 `$events` 结果通道（`/api/$events/result`）
  应答，以 $events clientId 与帧 eventId 为键；老版 `/api/respond` 通道
  不变。
- 新版构建的历史分页（`session/page`）：页面尾部 seq 取自实时的 session
  follow（无活动 follow 时临时开一条），因为新协议的 unary 面不暴露
  日志尾部。

- 新版（cookie 鉴权）dsh web：下行流自行恢复被拒或过期的会话 cookie ——
  `/api/remote.mux` 升级 401 且持有 launch token 时，下次拨号前重新换取
  （门控在 HTTP 重试路径之前就拒绝，靠它自己救不回来），cookie 过期不再
  让 TUI 卡在静默退避上。
- 鉴权失败携带原因：重换后仍 401 时报出是「未配置 launch token」还是
  「token 被拒（dsh web 已重启）」，一次性命令打印对应的恢复提示
  （`dsh-cli --token <token>`）。
- dialect 探测读取 401 响应体：无标记的 401 index 视为老版 bearer 门控
  主机（保持老线，bearer 走 DSH_TOKEN 环境变量）；unary 404 会在另一条
  线路上重试一次，作为探测误判的安全网。

- dsh web 生命周期调整：自动启动的 dsh web 现在**跨 dsh-cli 运行持续存活**
  ——dsh-cli 退出时不再关闭它（子进程被重新收养，stdout/stderr 写入
  `~/.dsh-cli/webhost-<port>.log` 而非 dsh-cli 的管道，父进程退出后不会
  因管道写入而死）。dsh-cli 发现 dsh web 已在运行时，使用已保存的 launch
  token 连接；若该运行中的 web 拒绝已保存的凭据（例如手动重启过 dsh
  web），dsh-cli 杀掉运行中的 web（按端口监听者）、重新启动并保存新
  token 后再连接。

### 修复

- 输入框为空时按 ctrl+d 不再退出程序：旧版把它当作 EOF 退出，现改为编
  辑器的前删（空行上无动作）；退出快捷键仍是 ctrl+c / ctrl+q。
- 每回合吞吐量改为统计新 token：缓存重读（已处理上下文的重复读取）不再
  推高 t/s 数值（百万级缓存命中不再被算作百万级的真实工作量）。
- /status 连接信息在新版 dsh 主机（线上不发布版本号）下显示 dsh 版本：
  回退读取本地 dsh 启动器（自动启动的二进制）的版本。
- 输入框换行后光标错位/消失：行范围改按 rune 计数，多字节（CJK）行不再
  吞掉后续行的光标位置；换行行丢弃换行点空格、不再超出边框；满行末尾
  的光标回退到最后一个字符（而不是被裁掉的额外单元）。
- 自动启动时，若被启动的 dsh web 是不输出启动 token 的旧版本：dsh-cli
  不再空等 90 秒的 token 超时——端口可达即按无 token（裸）连接就绪（约 2
  秒），不再杀掉刚启动的主机。
- subagent 会话窗口现在可滚动：子会话内容通常超出弹窗正文，窗口现支持
  ↑↓、翻页键、home/end 与鼠标滚轮（提示行有说明）；窗口打开时滚轮不再
  滚动其后隐藏的转录。

- 状态栏 token 读数（tokens: ↓… ↑…）恢复实时：每一步的 usage 块（适配
  器的用量报告，先于规范消息到达）现在即时计入回合统计与在途消息——步
  骤仍在流式生成时，读数先按已流出的内容估算、实时增长，直到用量数据
  落地时替换为精确值。规范消息只补差额，不重复计数（每步 t/s 采样与
  /status 的计量总额保持精确值不变）。

- /status 的 token 统计现在实时跟随宿主：持久化汇总（全部 / 本月 / 本周 /
  今日、按工作区、按会话）改为以宿主自己的累计 tokenUsage 投影为准——
  每一步的用量在宿主报告时即计入（session/projection 推送、尾页基线、
  会话列表），包含当前在途步骤的 token，汇总不再低于 Web 端的读数。/status
  弹窗打开期间，活跃会话的基线每数秒重新确认一次，读数保持实时。会话首次
  确认基线时，同时补上逐事件统计漏掉的部分（被裁剪的历史、仅 Web 端使用），
  且不重复计数已有的部分；没有该投影的旧版宿主仍走原来的逐事件统计。

## 1.0.45

### 新增

- 子代理视图：dock 新增第 5 个标签 SUBS（直接子代理列表：id、模式、标签、一次性标记、运行态），
  enter 打开只读子代理 transcript 窗口（分页、x 中断），dock 行支持 @child 内联提及。
- @ 补全：输入 @ 列出直接子代理与文件/目录（前 8 项），↑↓/enter 选用、esc 取消。
- 终端标题（OSC 0）：跟随活动会话（模式图标 + 标题 + (running)），退出时恢复原值。
- 未读指示：回滚离开底部时状态栏显示 "↓ N new"，Home/End/滚动保持跟随语义（end 恢复跟随）。
- max-tokens 结束附可操作 hint 行（/compact 压缩上下文，或换更大上下文窗口的模型）。
- 顶栏：长会话标题在窄窗下自动截断，客户端名+版本号始终在屏内。
- DSH_INSECURE 生效时显示一次性警告 toast。
- 语言文件落后内置表时显示一次性 toast（附 reseed 方式）。
- 明文 http 远端（http 非回环）：状态栏连接读常驻警示标记（原为启动时一次性 note）。
- .golangci.yml + make lint 目标。

### 改进

- 主机驱动的显示串（会话标题、工具名、工作区标题、任务标签、todo、goal、队列、
  问题卡片、通知）在 store 入口做 ANSI 转义消毒，防止主机把 SGR/OSC 混进终端。
- assistant 事件的 token 用量改为 transcript fold 单次解码，不再二次反序列化。
- 用户动作触发的 roster 刷新合流：至多一个在途、每 2s 一个，
  连续 创建/改名/fork 不再并发打 session.list。
- App 内部背景工作改挂应用生命周期 ctx，不再散落 context.Background()。
- usage.json 降为 0600；config.json 改为 temp+rename 原子写。
- TUI panic 时恢复终端（退出 alt-screen、显示光标）并打印栈后以 1 退出。
- ui 测试启动 hermetic 假主机，不再共享 127.0.0.1:3080 的真实会话状态。
- 审批卡改显式 a/d 选择（a 一次允许 / d 拒绝），bash 工具需二次 a 确认
  （再按 d 可反悔）；参数行提取真实命令（bash command / run_code code，
  截两行），缺省时回退其它一级参数。
- 工具卡显示耗时；edit/write 类工具卡可就地展开全文或前后 diff 预览。
- alt+enter 强制入队（即使输入非空）；esc 清空输入行并留在输入行。
- 上下文窗口未知时 thinking 段显示输入估算（输出 tokens × 15，显式标注估算）。
- 状态栏 hint 改四档：idle/running → dock（1-5 标签）→ 侧栏（space/enter/esc）。
- 非活动会话 turn-end：roster 行 accent 闪烁 1.5s（首见不闪，防历史加载误报）。
- turn-end 未知 kind 不再裸打 Kind，回退 turnend.unknown（仅展示 reason）。
- 重连 toast 国际化（中/英）。
- 启动基线缓存（DSH_CLI_NO_BOOT_CACHE 可关）。
- 版本化发布产物（.tar.gz / .exe）+ 源码 tarball。
- ~/.dsh-cli/locales 的 en.json/zh.json 带 _version 版本标记：与当前程序版本
  不一致（或文件缺失/损坏）时启动自动按内置表重新生成，版本一致则原样
  保留用户编辑，确保语言文件与程序匹配。
- config.json 带 _version 版本标记：高版本启动时自动为低版本配置补齐当前版本
  缺失的配置项（保留已有值）并重打版本，版本一致则不动文件。

### 修复

- 窄窗（<30 列）顶栏在长标题+模式+模型下可溢出窗口宽度。
- 通知文本残留 ANSI 参数字节（如 "[31m"）。
