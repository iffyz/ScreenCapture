# OCR 工具（Go 版）

一个 exe，两种用法：

| 场景 | 怎么触发 | 结果 |
|---|---|---|
| **图片文字识别** | ScreenCapture 框选后由主程序自动调用 | 文字写进剪贴板，无窗口 |
| **表格识别** | 双击运行（不带参数） | HTML 表格写进剪贴板，Word 里 `Ctrl+V` 粘成真表格 |

两种模式共用同一份 `config.json`，密钥只填一次。

## 模式是怎么判断的

不靠文件名，靠参数：

```
有 --image-path=<图>   → 文字识别（ScreenCapture 的调用方式）
没有任何参数            → 表格识别（用户双击）
```

也可以显式指定：`--mode=text` / `--mode=table`。

这样做的好处是 ScreenCapture 那边一行都不用改，而双击仍然是表格识别。

## 部署：两个名字，一份数据

ScreenCapture 硬编码去找同目录（或 `%APPDATA%\ScreenCapture\plugin`）下的 `ImageReader.exe`，
所以这个文件名必须存在。同时又希望有个叫 `TableOCR.exe` 的入口方便双击——

用 **NTFS 硬链接**解决，两个名字指向同一个物理文件，只占 6.6 MB：

```bat
mklink /H TableOCR.exe ImageReader.exe
```

PowerShell：

```powershell
New-Item -ItemType HardLink -Path TableOCR.exe -Target ImageReader.exe
```

如果硬链接不可用（比如放在 FAT32 或网络盘），直接把 exe 复制一份改名也行，只是各占 6.6 MB，功能完全一样。

> 注意：覆盖写 exe 内容时硬链接不会断（原地写），但**删除后重建**会断，需要重新 `mklink`。

## 配置

放在 exe 同目录的 `config.json`，查找顺序（先到先得，只填空缺不覆盖）：

1. `--config=<路径>` 指定的文件
2. exe 同目录的 `config.json` / `config.ini`
3. `%APPDATA%\ScreenCapture\plugin\config.json` / `config.ini`
4. 环境变量 `TENCENTCLOUD_SECRET_ID` / `SECRET_KEY`

```json
{
  "secret_id": "AKID...",
  "secret_key": "...",
  "region": "ap-guangzhou",

  "text_action": "GeneralBasicOCR",
  "table_action": "RecognizeTableAccurateOCR",

  "min_confidence": 0,
  "line_separator": "\r\n",
  "sort_by_position": false,

  "strip_cell_breaks": true,
  "cell_break_replacement": "",

  "show_error": true,
  "toast": true,
  "log": true
}
```

### 文字识别

| 项 | 默认 | 说明 |
|---|---|---|
| `text_action` | `GeneralBasicOCR` | 通用文字识别接口 |
| `min_confidence` | `0` | 置信度低于此值的行丢弃 |
| `line_separator` | `\r\n` | 行与行之间用什么连接；想连成一句就填 `" "` |
| `sort_by_position` | `false` | 按坐标重排（12px 内视为同一行）。接口默认顺序通常够用 |

### 表格识别

| 项 | 默认 | 说明 |
|---|---|---|
| `table_action` | `RecognizeTableAccurateOCR` | 高精度版。换成 `RecognizeTableOCR` 是快速版 |
| `strip_cell_breaks` | `true` | **删掉单元格内的换行**，见下 |
| `cell_break_replacement` | `""` | 换成什么。空串 = 直接删；填 `" "` = 换成空格 |

### 通用

| 项 | 默认 | 说明 |
|---|---|---|
| `show_error` | `true` | 出错弹框说明原因。改 `false` 则静默，只看日志 |
| `toast` | `true` | 表格识别成功后屏幕上方提示 2 秒（文字模式不提示） |
| `log` | `true` | 写 `ocr_plugin.log`，超 256 KB 自动滚成 `.log.1` |

## 单元格换行处理

`strip_cell_breaks: true`（默认）会把这些字符清掉：

| 字符 | 来源 |
|---|---|
| `\r\n` `\n` `\r` | Enter（换行符 / 段落标记） |
| `\v` (U+000B) | **Shift+Enter（Word 的手动换行符，界面上显示成 ↓）** |
| `\u2028` `\u2029` | 行分隔符 / 段分隔符，部分 PDF、网页复制会带 |

比如单元格里是

```
非甲烷
总烃
```

粘贴到 Word 后会变成 `非甲烷总烃`，单元格不会被撑成两行、表格也不会走形。

如果觉得中文直接粘连不好看，把 `cell_break_replacement` 改成 `" "`，结果就是 `非甲烷 总烃`。
想完全保留换行（转成 `<br>`）就把 `strip_cell_breaks` 设成 `false`——但纯文本版本仍会用空格连接，否则一行一条记录的结构会散。

## 剪贴板支持

读图优先级：**PNG**（自定义格式）> **CF_DIB** > **CF_DIBV5**。

- 32 位 DIB 会自己转 24 位并丢掉 alpha。屏幕截图的 32bpp DIB 经常 alpha 全 0，
  原样当 BMP 上传会被解成全透明/全黑。
- CF_BITMAP（GDI 位图句柄）不支持，会明确提示改用截图工具，而不是含糊地说"没有图片"。

写表格时同时放 `HTML Format` 和 `CF_UNICODETEXT`：Word/WPS 认 HTML 粘成表格，
粘到记事本或 Excel 则用制表符分隔的纯文本。

## 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 调用接口失败 / 网络错误 |
| 2 | 图片不存在（文字模式）或剪贴板无图（表格模式） |
| 3 | 没配置密钥 |
| 4 | 图片太大（base64 后超 7 MB） |
| 5 | 写剪贴板失败 |
| 6 | 图里没有识别到表格 |

## 调试

```bat
:: 把剪贴板里的图导出成 BMP，判断问题在取图还是在接口
ImageReader.exe --dump-clipboard-image=D:\dump.bmp

:: 不需要密钥，验证剪贴板写入链路
ImageReader.exe --selftest-clipboard="测试文本"
```

日志长这样，两个模式用 `[文字]` / `[表格]` 区分：

```
2026-11-08 04:18:22 [文字] 识别成功 2 行 30 字，602 ms
2026-11-08 04:18:28 [表格] 剪贴板图片 png 748x318 18052 字节
2026-11-08 04:18:28 [表格] 识别成功 3 行 4 列（共 60 字），736 ms
```

## 编译

只用标准库，零依赖：

```bat
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H windowsgui" -o ImageReader.exe .
```

`-H windowsgui` 让 PE subsystem = 2，双击不闪黑框（等价于 PyInstaller 的 `--noconsole`）。
产物约 6.6 MB。测试：`go test ./...`

## 代码结构

| 文件 | 内容 |
|---|---|
| `main.go` | 入口、参数解析、模式分发 |
| `config.go` | 配置默认值、json/ini 读取、日志 |
| `auth.go` | TC3-HMAC-SHA256 签名、HTTP 调用 |
| `textmode.go` | 图片文字识别 |
| `tablemode.go` | 表格识别、网格铺排、单元格换行清理 |
| `win.go` | 剪贴板读写、DIB 转换、CF_HTML、提示窗 |
| `main_test.go` | 单元测试 |

## 排错

常见接口错误：

- `AuthFailure.SecretIdNotFound` —— 密钥错了，或没读到配置文件
- `FailedOperation.ImageDecodeFailed` —— 图片格式有问题，用 `--dump-clipboard-image` 导出看看
- `FailedOperation.OcrFailed` —— 图里没表格，或太小太糊
- `RequestLimitExceeded` —— 超过 QPS，等一秒重试
- `ResourceUnavailable.InArrears` —— 账号欠费
