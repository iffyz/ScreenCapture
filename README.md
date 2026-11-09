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
- `upstream` 分支 = fork 时原版代码的快照，仅作留底，不参与编译

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
