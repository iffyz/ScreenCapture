# ScreenCapture（改造版）可编译源码

基于 [xland/ScreenCapture](https://github.com/xland/ScreenCapture) 2.5.15 与 [xland/Ling](https://github.com/xland/Ling) 的改造版本，
去掉录屏与二维码，改成「用完即退」，工具条换成表格识别 + 钉图。这里放的是**能直接编译出成品 exe** 的完整源码。

## 目录

```
build.ps1                一次性把 yoga -> Ling -> ScreenCapture 编完
src/                     ScreenCapture 源码（含 ScreenCapture.vcxproj）
ling/                    Ling 框架 + yoga 布局引擎
plugin/                  Go 写的 OCR 插件（腾讯云通用/表格识别），产出 ImageReader.exe
.github/workflows/build.yml   GitHub Actions：push 自动出 exe
```

## 定制改动清单

基线是上游 `xland/ScreenCapture` 的 2.5.15（`5e4713e`）。这一节回答「哪些地方是本仓库自己改的」——
日后 `git fetch upstream` 看上游更新、决定要不要 cherry-pick 时，先对照这张表。

| 提交 | 改动 | 为什么 / 备注 |
|---|---|---|
| `c1b2975` | 建立可编译源码仓库与 GitHub Actions 三段式云编译；主程序改成「用完即退」，删录屏与二维码，工具条换成表格识别 + 钉图；往图标字体追加 `U+E71F` 表格字形 | 初始改造基线。同时给 Ling 打了两处兼容性补丁，见「几个不能改的地方」 |
| `bf5d1ce` | 修 `.gitignore` 里裸的 `Debug/` 规则，补回 `ling/yoga/debug/` 四个源文件 | Windows 下大小写不敏感，误把 yoga 的 debug 源文件排除掉了 |
| `21c53af` | README 补仓库地址、留底分支说明与取产物步骤 | 文档 |
| `6833964` | 修「钉图后无法再截图」：改用 Ling 的 `refuseSecondInstance()` + `onSecondInstance`；新增 `Util::trimMemory()` | 原来的单实例互斥体直接 `quit(0)`；钉图常驻内存由 91 MB 降到约 12 MB |
| `d647259` | OCR 默认改 `GeneralAccurateOCR`；`config.go` 加编码归一化与布尔/数字容错，补 4 个用例 | 用户手写的 `config.json` 是 UTF-16 LE 且布尔写成字符串，原解析器直接报「没配置密钥」 |
| `856519e` | 整块删除长截图（8 个文件、约 627 行），取色面板只留 HEX 与 RGB | 长截图不好用，顺带去掉 CMYK 与坐标。exe 只小约 2%，因为 `/OPT:REF` 本来就会丢弃未引用代码 |
| `95b0738` | 取色面板高度提成常量 `pixTextH` | 面板改成两行后，翻面判断仍按旧高度算 |
| `b28c0ea` | 工具条数字键 `1`–`4` 与 `Ctrl+S`；钉图放行 `Ctrl+C` / `Ctrl+S` / `Enter` 与双击；缩放统一为 `Ctrl+滚轮`；6 份语言包补提示文案 | 只认主键区数字，不映射小键盘。`ToolCap` 抽出 `doAction(id)` 让鼠标与键盘共用一个出口 |

### 与上游的关系

- `origin` = 本仓库 `iffyz/ScreenCapture`；`upstream` **remote** = 原作者 `xland/ScreenCapture`。
- `origin/upstream` **分支**只是 fork 时原版代码的留底快照，不参与编译，也不是跟踪分支；名字容易与上面的 remote 混淆。
- **不要 merge 上游**：本版已删长截图、裁掉多个命令行参数、换 OCR 默认值、改图标字体，冲突面很大。
  正确做法是 `git fetch upstream` → 看 `git log` / `git diff` → 只挑需要的 `git cherry-pick`。

### 不在仓库里的改动（部署层）

| 项 | 现状 |
|---|---|
| `ImageReader.exe` / `TableOCR.exe` | 已换成 CI 产物（5,113,344 B），两者是同一个硬链接；CI 带 `-s -w` 去符号，所以比旧版本地编译的小 |
| `config.json` | UTF-16 LE + BOM、紧凑单行，是主程序 `Setting::save()` 写出来的。别手工改成 UTF-8，下次保存会被覆盖回去 |
| 本机编译环境 | VS 2022 生成工具与 SDK 已卸载，编译只走 GitHub Actions |
| 实际部署 | `D:\PortableAPP\ScreenCapture\`，主程序对应 `b28c0ea` |

### 版本锚点

`custom-2026-11-09` 标记本次收尾状态，用于精确回滚和与上游做 diff 基线。
CI 的 `tags: ["v*"]` 只匹配 `v` 开头，这个标签不会触发发版构建；需要带下载链接的 Release 时另打 `v*` 标签。

## 本地编译

前提：Windows 10/11 x64 + **Visual Studio 2022 Build Tools**（勾选「使用 C++ 的桌面开发」，
含 MSVC v143 与 Windows 10/11 SDK）。

```powershell
.\build.ps1
```

脚本自己找 MSBuild（`MSBUILD_EXE_PATH` → vswhere → 常见安装路径 → PATH），
从脚本所在目录推导所有路径，**整个文件夹随便挪位置都能编**。

产物：

| 文件 | 说明 |
|---|---|
| `src\Src\x64\Release\ScreenCapture.exe` | 主程序，`/MT` 静态链接，不需要 VC 运行库 |
| `plugin\ImageReader.exe` | 需另编，见下 |

## 编译插件

```powershell
cd plugin
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -H windowsgui" -o ImageReader.exe .
```

插件运行时按顺序找配置：`exe 同目录\config.json` → `%APPDATA%\ScreenCapture\plugin\config.json`。
把 `config.example.json` 复制成 `config.json` 填上腾讯云密钥即可。**密钥不要提交进仓库。**
`-H windowsgui` 不能省，否则识别时会闪一个黑框。

## 云编译

仓库：<https://github.com/iffyz/ScreenCapture>（fork 自 xland/ScreenCapture）

- `main` 分支 = 本改造版源码，push 后自动跑 `.github/workflows/build.yml`
- `origin/upstream` **分支** = fork 时原版代码的快照，仅作留底，不参与编译（不是跟踪分支；另有一个名为 `upstream` 的 remote 指向原作者仓库，见「定制改动清单」）

流程：

1. `microsoft/setup-msbuild@v2` 备好 MSBuild
2. `build.ps1` 编主程序
3. `setup-go` + `go test` + `go build` 编插件
4. 产物打包成 `ScreenCapture-win64-<sha>` artifact
5. 打 `v*` 标签时额外发 Release（Release 里的 exe 不需要登录就能下）

跑一次约 3~4 分钟，冷启动大头在编 yoga 和 Ling。

### 取产物

仓库页 → **Actions** → 点最新一次 `build` → 页面底部 **Artifacts** →
下载 `ScreenCapture-win64-<sha>.zip`，里面是三个文件：

| 文件 | 说明 |
|---|---|
| `ScreenCapture.exe` | 主程序 |
| `ImageReader.exe` | OCR 插件 |
| `config.example.json` | 密钥模板，复制成 `config.json` 再填 |

Artifact 默认只保留 90 天，过期就再 push 一个空提交重跑。

### 发一个带下载链接的版本

```bash
git tag v2.5.15-custom1
git push origin v2.5.15-custom1
```

标签触发的那次构建会顺带发 Release，里面的 exe 是直链，给别人或自己换机器都方便。

## 快捷键

### 框选完成、工具条出现时

| 键 | 动作 |
|---|---|
| `1` `2` `3` `4` | 标注 / 文字识别 / 表格识别 / 钉图，等于点工具条前四个按钮 |
| `Ctrl+S` | 另存为 PNG |
| `Enter` | 复制到剪切板并关闭 |
| `ESC` | 放弃并关闭 |

只认主键区的数字，小键盘的 `1`–`4` 不映射。

### 图像标记（标注模式）

| 键 | 动作 |
|---|---|
| `Ctrl+Z` / `Ctrl+Y` | 撤销 / 重做 |
| `Ctrl+C` / `Enter` / `双击` | 复制到剪切板并关闭 |
| `Ctrl+S` | 另存为 PNG |
| `Delete` | 删除光标悬停的元素（要先选中一个画笔） |
| `ESC` | 关闭，不保存，标注丢弃 |
| `Ctrl+滚轮` | 以光标为锚点缩放 |
| `滚轮` | 调当前元素的参数（马赛克块大小、线宽等） |
| `右键` | 收起 / 唤回工具条 |
| 未选画笔时左键拖动 | 移动窗口 |
| 文字编辑中 | `Enter` 换行、`Tab` 空格、`Ctrl+A/C/X/V`、`ESC` 结束编辑 |

### 钉图（工具条收着）

| 键 | 动作 |
|---|---|
| `空格` | 显示 / 隐藏工具条 |
| `ESC` | 先收工具条；本来就收着则关闭窗口 |
| `Ctrl+滚轮` | 缩放，与标注模式同一套手势 |
| `Ctrl+C` / `Ctrl+S` / `Enter` / `双击` | 与标注模式一致 |
| 左键拖动 | 移动贴图位置 |

## 几个不能改的地方

- **工具集必须是 v143**。v145 属于 VS2026，只支持 Win11；本仓库三个 `.vcxproj` 都是 v143。
- **`WindowsTargetPlatformVersion` 保持 `10.0`**，用机器上最新的 SDK，不要钉死小版本。
- **`src/Src/Res/iconfont.ttf` 是改过的**：往字体里追加了 U+E71F 这个表格识别图标字形。
  换字体文件会导致工具条少一个图标。
- **`build.ps1` 只能写 ASCII**。PowerShell 5.1 在文件没有 BOM 时按 ANSI 读，中文注释会把引号吃掉。
- **`build.ps1` 开头会清掉小写的 `http_proxy` 等变量**：父进程环境块里 `http_proxy` 和 `HTTP_PROXY`
  同时存在时，MSBuild 构造大小写不敏感字典会直接抛 MSB6001。
- **Ling 有两处兼容性改动**，不要用上游原版覆盖：
  `include/Util.h` 里 `COMPILE_TIME_RAND_STR` 的字符表从内层 lambda 局部变量提到了命名空间作用域
  （v143 会报 C3493）；两个 `.vcxproj` 的 `PlatformToolset` 是 v143。

## 许可

`src/` 与 `ling/` 分别遵循各自目录下的 LICENSE（MIT）。
