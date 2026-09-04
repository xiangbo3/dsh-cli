# Changelog

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
