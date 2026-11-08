package main

// 表格识别模式：直接读剪贴板里的图，识别结果以 HTML 表格写回剪贴板。
//
// 与文字模式的区别只在入口：那边由 ScreenCapture 传 --image-path，
// 这边是用户双击（无参数），图从剪贴板拿。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type tableCell struct {
	Text  string `json:"Text"`
	RowTl int    `json:"RowTl"`
	ColTl int    `json:"ColTl"`
	RowBr int    `json:"RowBr"`
	ColBr int    `json:"ColBr"`
}

type tableDetection struct {
	Cells []tableCell `json:"Cells"`
}

type tableResponse struct {
	Response struct {
		Error           *apiError        `json:"Error"`
		TableDetections []tableDetection `json:"TableDetections"`
		RequestId       string           `json:"RequestId"`
	} `json:"Response"`
}

func runTableMode() int {
	return runTableModeFrom("", false)
}

// runTableModeFrom 是表格识别的实际实现。imgPath 为空时从剪贴板取图（用户双击运行）；
// 非空时用 ScreenCapture 框选出来的那张缓存图（工具条上的「表格识别」按钮），
// 这时不去碰剪贴板，免得把用户原来复制的东西冲掉。
func runTableModeFrom(imgPath string, delImage bool) int {
	if !hasCredentials() {
		logf("[表格] 未配置 secret_id / secret_key")
		failTable("缺少密钥", missingKeyMsg())
		return 3
	}

	var raw []byte
	if imgPath != "" {
		if delImage {
			// 读完就删：这是 ScreenCapture 的缓存图，留着只会在数据目录里越攒越多
			defer os.Remove(imgPath)
		}
		data, err := os.ReadFile(imgPath)
		if err != nil {
			logf("[表格] 读取图片失败: %v", err)
			failTable("没有取到图片", err.Error())
			return 2
		}
		raw = data
		logf("[表格] 框选图 %d 字节", len(data))
	} else {
		img, err := readClipboardImage()
		if err != nil {
			logf("[表格] 读取剪贴板失败: %v", err)
			failTable("没有取到图片", err.Error())
			return 2
		}
		raw = img.data
		logf("[表格] 剪贴板图片 %s %dx%d %d 字节", img.kind, img.w, img.h, len(img.data))
	}

	// 接口限制：base64 编码后不超过 7 MB
	if b64Len := len(raw)/3*4 + 4; b64Len > maxBase64Bytes {
		msg := fmt.Sprintf("图片太大（约 %.1f MB，接口上限 7 MB）。\n\n"+
			"请把表格区域截小一点再试。", float64(len(raw))/1048576)
		logf("[表格] 图片过大: %d 字节", len(raw))
		failTable("图片过大", msg)
		return 4
	}

	start := time.Now()
	tables, err := callTableOCR(raw)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		logf("[表格] 识别失败: %v", err)
		failTable("识别失败", "调用腾讯云表格识别出错：\n\n"+err.Error())
		return 1
	}

	res := buildAll(tables)
	if res.html == "" {
		logf("[表格] 未识别到表格 (%d ms)", cost)
		failTable("没有识别到表格",
			"腾讯云没有在图片里找到表格。\n\n"+
				"建议：\n"+
				"· 截图时把表格边框完整包进去，四周留一点余量\n"+
				"· 图片尽量正、清晰，避免倾斜和大面积空白\n"+
				"· 如果只是想识别文字（不是表格），请用 ScreenCapture 的文字识别")
		return 6
	}

	if !setClipboardTable(res.html, res.plain) {
		logf("[表格] 写入剪贴板失败")
		failTable("写入剪贴板失败", "可能有程序正占用剪贴板，请重试一次。")
		return 5
	}
	// 结果已经进剪贴板，提示窗还挂着也可以接受下一次运行
	releaseSingleInstance()

	if res.tables > 1 {
		logf("[表格] 识别成功 %d 个表格（首个 %d 行 %d 列，共 %d 字），%d ms",
			res.tables, res.rows, res.cols, utf8.RuneCountInString(res.plain), cost)
	} else {
		logf("[表格] 识别成功 %d 行 %d 列（共 %d 字），%d ms",
			res.rows, res.cols, utf8.RuneCountInString(res.plain), cost)
	}

	// 由 ScreenCapture 工具条调用时不给提示：那条路子本来就是按完按钮立刻回去贴，
	// 弹个窗横在屏幕上方反而挡事；只有双击运行时才需要这个反馈
	if imgPath == "" {
		if res.tables > 1 {
			toast(fmt.Sprintf("已识别 %d 个表格（首个 %d×%d），Ctrl+V 粘贴", res.tables, res.rows, res.cols))
		} else {
			toast(fmt.Sprintf("已识别 %d 行 × %d 列，Ctrl+V 粘贴", res.rows, res.cols))
		}
	}
	return 0
}

func callTableOCR(raw []byte) ([]tableDetection, error) {
	payload, err := json.Marshal(map[string]string{
		"ImageBase64": base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		return nil, err
	}

	// 表格识别比通用 OCR 慢，超时给足
	body, err := postOCR(payload, cfg.TableAction, 60*time.Second)
	if err != nil {
		return nil, err
	}

	var r tableResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w（返回内容：%s）", err, headOf(body))
	}
	if e := r.Response.Error; e != nil {
		return nil, e
	}
	return r.Response.TableDetections, nil
}

// failTable 表格模式的错误提示：优先弹框；关掉弹框就退回顶部提示，
// 至少让用户知道出事了，而不是双击后毫无反应。
func failTable(title, msg string) {
	releaseSingleInstance()
	if cfg.ShowError {
		showError(appTable+" - "+title, msg)
		return
	}
	toast(title + "，详见 " + logFileName)
}

func toast(msg string) {
	if !cfg.Toast {
		return
	}
	showToast(msg, 2000)
}

// ------------------------------------------------------------ 表格 → HTML

type tableResult struct {
	html  string // <tr>...</tr> 序列
	plain string
	rows  int
	cols  int
}

type buildResult struct {
	html   string // 完整 HTML 片段，可直接进剪贴板
	plain  string
	tables int
	rows   int
	cols   int
}

// 单个表格的 HTML 包装。字号字体跟原来的 AHK 版保持一致（宋体 10.5pt）。
const tableStyle = `border="1" cellspacing="0" cellpadding="3" ` +
	`style="border-collapse:collapse;border:1px solid #000000;` +
	`font-family:宋体,SimSun;font-size:10.5pt"`

func buildAll(tables []tableDetection) buildResult {
	var res buildResult
	var htmlParts, plainParts []string

	for _, t := range tables {
		r := buildTable(t.Cells)
		if r.html == "" {
			continue
		}
		res.tables++
		if res.tables == 1 {
			res.rows, res.cols = r.rows, r.cols
		}
		htmlParts = append(htmlParts, "<table "+tableStyle+">"+r.html+"</table>")
		plainParts = append(plainParts, r.plain)
	}

	res.html = strings.Join(htmlParts, "<p></p>")
	res.plain = strings.Join(plainParts, "\r\n\r\n")
	return res
}

// buildTable 把腾讯云的 Cells 铺成网格。
// RowTl/ColTl/RowBr/ColBr 是 0 基的格子坐标，右下角为开区间（RowBr=RowTl+行跨度）。
func buildTable(cells []tableCell) tableResult {
	if len(cells) == 0 {
		return tableResult{}
	}

	maxR, maxC := 0, 0
	for _, c := range cells {
		if c.RowBr > maxR {
			maxR = c.RowBr
		}
		if c.ColBr > maxC {
			maxC = c.ColBr
		}
	}
	// 兼容个别返回把 RowBr 当闭区间的情况
	for _, c := range cells {
		if c.RowTl >= maxR {
			maxR = c.RowTl + 1
		}
		if c.ColTl >= maxC {
			maxC = c.ColTl + 1
		}
	}
	if maxR <= 0 || maxC <= 0 {
		return tableResult{}
	}
	// 防御：异常返回时不要把内存撑爆
	if maxR > 500 || maxC > 200 {
		logf("[表格] 网格异常：%d 行 %d 列，已忽略", maxR, maxC)
		return tableResult{}
	}

	type pos struct{ r, c int }
	byPos := make(map[pos]tableCell, len(cells))
	for _, c := range cells {
		k := pos{c.RowTl, c.ColTl}
		if _, exists := byPos[k]; !exists {
			byPos[k] = c
		}
	}

	occupied := make([]bool, maxR*maxC)
	idx := func(r, c int) int { return r*maxC + c }

	var html strings.Builder
	var plain strings.Builder

	for r := 0; r < maxR; r++ {
		html.WriteString("<tr>")
		var line strings.Builder

		for c := 0; c < maxC; {
			if occupied[idx(r, c)] {
				c++
				continue
			}

			cell, ok := byPos[pos{r, c}]
			if !ok {
				html.WriteString("<td></td>")
				occupied[idx(r, c)] = true
				c++
				continue
			}

			rs := cell.RowBr - cell.RowTl
			if rs < 1 {
				rs = 1
			}
			cs := cell.ColBr - cell.ColTl
			if cs < 1 {
				cs = 1
			}
			// 不越界
			if r+rs > maxR {
				rs = maxR - r
			}
			if c+cs > maxC {
				cs = maxC - c
			}

			for rr := r; rr < r+rs; rr++ {
				for cc := c; cc < c+cs; cc++ {
					occupied[idx(rr, cc)] = true
				}
			}

			attrs := ""
			if rs > 1 {
				attrs += fmt.Sprintf(` rowspan="%d"`, rs)
			}
			if cs > 1 {
				attrs += fmt.Sprintf(` colspan="%d"`, cs)
			}

			// 单元格内的换行按配置清掉，HTML 和纯文本两版用同一份文本
			txt := cleanCellText(cell.Text)
			html.WriteString("<td" + attrs + ">" + htmlEsc(txt) + "</td>")

			if line.Len() > 0 {
				line.WriteString("\t")
			}
			line.WriteString(oneLine(txt))

			c += cs
		}

		html.WriteString("</tr>")
		plain.WriteString(line.String())
		plain.WriteString("\r\n")
	}

	return tableResult{
		html:  html.String(),
		plain: strings.TrimRight(plain.String(), "\r\n"),
		rows:  maxR,
		cols:  maxC,
	}
}

// cellBreakFolders 把三种「换行」统一折成 \n，便于一次性处理：
//
//	\r\n \n \r    —— Enter（换行符 / 段落标记）
//	\v            —— Shift+Enter（Word 的手动换行符，界面上显示成 ↓）
//	\u2028 \u2029 —— 行分隔符 / 段分隔符，部分 PDF、网页复制会带
var cellBreakFolders = strings.NewReplacer(
	"\r\n", "\n",
	"\r", "\n",
	"\v", "\n",
	"\u2028", "\n",
	"\u2029", "\n",
)

// cleanCellText 清掉单元格内的换行。
// 默认替换为空串（直接删除）；把 cell_break_replacement 配成 " " 就改成空格，
// 避免中英文混排时两段文字粘在一起。
func cleanCellText(s string) string {
	folded := cellBreakFolders.Replace(s)
	if !cfg.StripCellBreaks {
		return folded
	}
	if cfg.CellBreakReplacement == "" {
		return strings.ReplaceAll(folded, "\n", "")
	}
	return strings.ReplaceAll(folded, "\n", cfg.CellBreakReplacement)
}

// oneLine 纯文本版兜底：即使关掉了 strip_cell_breaks，
// 也不能让换行把「一行一个制表符分隔记录」的结构冲掉。
func oneLine(s string) string {
	return strings.ReplaceAll(cellBreakFolders.Replace(s), "\n", " ")
}

// htmlEsc 转义 HTML 特殊字符。& 必须最先处理。
// 换行转 <br> 只在关掉 strip_cell_breaks 时还有机会命中。
func htmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return s
}
