package main

import (
	"strconv"
	"strings"
	"testing"
)

// resetCfg 每个用例都从默认配置开始，避免互相污染。
func resetCfg(t *testing.T) {
	t.Helper()
	old := cfg
	cfg = defaultConfig()
	t.Cleanup(func() { cfg = old })
}

// ------------------------------------------------------------ 单元格换行清理

func TestCleanCellTextRemovesBreaks(t *testing.T) {
	resetCfg(t) // 默认 strip_cell_breaks = true，替换为空串

	cases := []struct{ in, want string }{
		{"非甲烷\n总烃", "非甲烷总烃"},     // 换行符
		{"非甲烷\r\n总烃", "非甲烷总烃"},   // Windows 换行
		{"非甲烷\r总烃", "非甲烷总烃"},     // 老式回车
		{"非甲烷\v总烃", "非甲烷总烃"},     // Word 手动换行符（Shift+Enter）
		{"非甲烷\u2028总烃", "非甲烷总烃"}, // 行分隔符
		{"非甲烷\u2029总烃", "非甲烷总烃"}, // 段分隔符
		{"排放浓度\n(mg/m³)", "排放浓度(mg/m³)"},
		{"一\v二\r\n三\n四", "一二三四"}, // 混在一起
		{"没有换行", "没有换行"},
		{"", ""},
		{"\v\v\r\n", ""},       // 全是换行 → 清空
		{"前后\t制表符", "前后\t制表符"}, // 制表符不该被动
	}
	for _, c := range cases {
		if got := cleanCellText(c.in); got != c.want {
			t.Errorf("cleanCellText(%q) = %q，应为 %q", c.in, got, c.want)
		}
	}
}

func TestCleanCellTextWithSpaceReplacement(t *testing.T) {
	resetCfg(t)
	cfg.CellBreakReplacement = " "

	if got := cleanCellText("非甲烷\v总烃"); got != "非甲烷 总烃" {
		t.Errorf("应替换成空格，实际 %q", got)
	}
	if got := cleanCellText("a\r\nb\nc"); got != "a b c" {
		t.Errorf("应替换成空格，实际 %q", got)
	}
}

func TestCleanCellTextKeepBreaksWhenDisabled(t *testing.T) {
	resetCfg(t)
	cfg.StripCellBreaks = false

	// 不清理时仍要折成 \n，好让 HTML 侧统一转 <br>
	if got := cleanCellText("a\r\nb"); got != "a\nb" {
		t.Errorf("应折成单个 \\n，实际 %q", got)
	}
	if got := cleanCellText("a\vb"); got != "a\nb" {
		t.Errorf("手动换行符应折成 \\n，实际 %q", got)
	}
}

func TestBuildTableStripsCellBreaks(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"间苯二甲酸\n装置", 0, 0, 1, 1},
		{"非甲烷\v总烃", 0, 1, 1, 2},
	}
	r := buildTable(cells)

	if strings.Contains(r.html, "<br>") {
		t.Errorf("单元格里不该再有 <br>：%s", r.html)
	}
	if !strings.Contains(r.html, "间苯二甲酸装置") {
		t.Errorf("换行未删除：%s", r.html)
	}
	if !strings.Contains(r.html, "非甲烷总烃") {
		t.Errorf("手动换行符未删除：%s", r.html)
	}
	// 纯文本版也不能被换行打断
	if strings.Contains(r.plain, "\n") || strings.Contains(r.plain, "\v") {
		t.Errorf("纯文本里还有换行：%q", r.plain)
	}
	if r.plain != "间苯二甲酸装置\t非甲烷总烃" {
		t.Errorf("纯文本不对：%q", r.plain)
	}
}

func TestBuildTableKeepsBreakAsBrWhenDisabled(t *testing.T) {
	resetCfg(t)
	cfg.StripCellBreaks = false

	r := buildTable([]tableCell{{"第一行\n第二行", 0, 0, 1, 1}})
	if !strings.Contains(r.html, "第一行<br>第二行") {
		t.Errorf("关掉清理后应转成 <br>：%s", r.html)
	}
	// 纯文本仍不能出现换行，否则一行一条记录的格式会散
	if strings.Contains(r.plain, "\n") {
		t.Errorf("纯文本不该有换行：%q", r.plain)
	}
}

func TestHTMLEsc(t *testing.T) {
	cases := map[string]string{
		"a&b":       "a&amp;b",
		"<td>":      "&lt;td&gt;",
		"行1\n行2":    "行1<br>行2",
		"a&b<c\nd":  "a&amp;b&lt;c<br>d",
		"纯中文 12.5%": "纯中文 12.5%",
	}
	for in, want := range cases {
		if got := htmlEsc(in); got != want {
			t.Errorf("htmlEsc(%q) = %q，应为 %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------- CF_HTML

func offsetOf(t *testing.T, s, key string) int {
	t.Helper()
	marker := key + ":"
	i := strings.Index(s, marker)
	if i < 0 {
		t.Fatalf("找不到字段 %s", key)
	}
	start := i + len(marker)
	n, err := strconv.Atoi(s[start : start+10])
	if err != nil {
		t.Fatalf("字段 %s 不是 10 位定宽数字: %v", key, err)
	}
	return n
}

func TestBuildCFHTMLOffsets(t *testing.T) {
	frag := `<table><tr><td>序号</td><td>非甲烷总烃 12.5</td></tr></table>` // 含中文，验证按字节计
	s := string(buildCFHTML(frag))

	startHTML := offsetOf(t, s, "StartHTML")
	endHTML := offsetOf(t, s, "EndHTML")
	startFrag := offsetOf(t, s, "StartFragment")
	endFrag := offsetOf(t, s, "EndFragment")

	if !strings.HasPrefix(s, "Version:0.9\r\n") {
		t.Errorf("头部应以 Version:0.9 开头")
	}
	if startHTML != strings.Index(s, "<html>") {
		t.Errorf("StartHTML=%d，实际 <html> 位置 %d", startHTML, strings.Index(s, "<html>"))
	}
	if got := s[startHTML:startFrag]; got != cfHTMLPrefix {
		t.Errorf("StartHTML..StartFragment 应为前缀，实际 %q", got)
	}
	if got := s[startFrag:endFrag]; got != frag {
		t.Errorf("StartFragment..EndFragment 应为片段，实际 %q", got)
	}
	if got := s[endFrag:endHTML]; got != cfHTMLSuffix {
		t.Errorf("EndFragment..EndHTML 应为后缀，实际 %q", got)
	}
	if endHTML != len(s) {
		t.Errorf("EndHTML=%d，总长 %d", endHTML, len(s))
	}
	if endFrag-startFrag != len(frag) {
		t.Errorf("片段长度应等于字节数 %d，实际 %d", len(frag), endFrag-startFrag)
	}
}

// ---------------------------------------------------------------- 表格网格

func countTag(html, tag string) int { return strings.Count(html, "<"+tag) }

func TestBuildTableSimple(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"序号", 0, 0, 1, 1}, {"名称", 0, 1, 1, 2},
		{"1", 1, 0, 2, 1}, {"苯", 1, 1, 2, 2},
	}
	r := buildTable(cells)

	if r.rows != 2 || r.cols != 2 {
		t.Fatalf("应为 2 行 2 列，实际 %d 行 %d 列", r.rows, r.cols)
	}
	if n := countTag(r.html, "tr"); n != 2 {
		t.Errorf("应有 2 个 <tr>，实际 %d", n)
	}
	if n := countTag(r.html, "td"); n != 4 {
		t.Errorf("应有 4 个 <td>，实际 %d", n)
	}
	if strings.Contains(r.html, "rowspan") || strings.Contains(r.html, "colspan") {
		t.Errorf("普通表格不该有 span：%s", r.html)
	}
	if r.plain != "序号\t名称\r\n1\t苯" {
		t.Errorf("纯文本不对: %q", r.plain)
	}
}

func TestBuildTableColspan(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"污染物排放情况", 0, 0, 1, 4},
		{"序号", 1, 0, 2, 1}, {"排放口", 1, 1, 2, 2}, {"污染物", 1, 2, 2, 3}, {"浓度", 1, 3, 2, 4},
	}
	r := buildTable(cells)

	if r.rows != 2 || r.cols != 4 {
		t.Fatalf("应为 2 行 4 列，实际 %d 行 %d 列", r.rows, r.cols)
	}
	if !strings.Contains(r.html, `<td colspan="4">污染物排放情况</td>`) {
		t.Errorf("表头应 colspan=4：%s", r.html)
	}
	if n := countTag(r.html, "td"); n != 5 {
		t.Errorf("应有 5 个 <td>，实际 %d：%s", n, r.html)
	}
}

func TestBuildTableRowspan(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"间苯二甲酸", 0, 0, 3, 1},
		{"DA001", 0, 1, 1, 2},
		{"DA002", 1, 1, 2, 2},
		{"DA003", 2, 1, 3, 2},
	}
	r := buildTable(cells)

	if r.rows != 3 || r.cols != 2 {
		t.Fatalf("应为 3 行 2 列，实际 %d 行 %d 列", r.rows, r.cols)
	}
	if !strings.Contains(r.html, `<td rowspan="3">间苯二甲酸</td>`) {
		t.Errorf("第一格应 rowspan=3：%s", r.html)
	}
	if n := countTag(r.html, "td"); n != 4 {
		t.Errorf("应有 4 个 <td>，实际 %d：%s", n, r.html)
	}
	if strings.Count(r.html, "间苯二甲酸") != 1 {
		t.Errorf("合并单元格文字重复输出：%s", r.html)
	}
}

func TestBuildTableUnorderedCells(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"C", 1, 1, 2, 2}, {"A", 0, 0, 1, 1},
		{"D", 1, 0, 2, 1}, {"B", 0, 1, 1, 2},
	}
	r := buildTable(cells)
	if r.html != "<tr><td>A</td><td>B</td></tr><tr><td>D</td><td>C</td></tr>" {
		t.Errorf("乱序铺网格结果不对：%s", r.html)
	}
}

func TestBuildTableHole(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{
		{"A", 0, 0, 1, 1}, {"C", 0, 2, 1, 3},
		{"D", 1, 0, 2, 1}, {"E", 1, 1, 2, 2}, {"F", 1, 2, 2, 3},
	}
	r := buildTable(cells)
	if r.cols != 3 || r.rows != 2 {
		t.Fatalf("应为 2 行 3 列，实际 %d 行 %d 列", r.rows, r.cols)
	}
	if !strings.HasPrefix(r.html, "<tr><td>A</td><td></td><td>C</td></tr>") {
		t.Errorf("缺格子未补空：%s", r.html)
	}
}

func TestBuildTableEmpty(t *testing.T) {
	resetCfg(t)
	if r := buildTable(nil); r.html != "" || r.rows != 0 {
		t.Errorf("空输入应返回空结果，实际 %+v", r)
	}
	r := buildTable([]tableCell{{"X", 0, 0, 1, 1}})
	if r.rows != 1 || r.cols != 1 || !strings.Contains(r.html, "X") {
		t.Errorf("1x1 表格不对：%+v", r)
	}
}

func TestBuildTableRejectsHugeGrid(t *testing.T) {
	resetCfg(t)
	cells := []tableCell{{"X", 0, 0, 100000, 1}}
	if r := buildTable(cells); r.html != "" {
		t.Errorf("超大网格应被拒绝，实际 rows=%d cols=%d", r.rows, r.cols)
	}
}

func TestBuildAllMultipleTables(t *testing.T) {
	resetCfg(t)
	res := buildAll([]tableDetection{
		{Cells: []tableCell{{"A", 0, 0, 1, 1}}},
		{Cells: []tableCell{{"B1", 0, 0, 1, 1}, {"B2", 0, 1, 1, 2}}},
	})
	if res.tables != 2 {
		t.Fatalf("应识别出 2 个表格，实际 %d", res.tables)
	}
	if n := countTag(res.html, "table"); n != 2 {
		t.Errorf("应有 2 个独立 <table>，实际 %d：%s", n, res.html)
	}
	if !strings.Contains(res.plain, "\r\n\r\n") {
		t.Errorf("多个表格之间应有空行：%q", res.plain)
	}
}

// ---------------------------------------------------------------- 配置

func TestParseINIConfig(t *testing.T) {
	ini := []byte("; 注释\r\n[tencent]\r\nSecretId=AKIDxxx\r\nSecretKey=key000\r\n" +
		"Region=ap-beijing\r\nTableAction=RecognizeTableOCR\r\nShowError=false\r\n" +
		"StripCellBreaks=true\r\n\r\n[other]\r\nSecretId=忽略我\r\n")
	fc := parseINIConfig(ini)

	if fc.SecretID != "AKIDxxx" || fc.SecretKey != "key000" {
		t.Errorf("密钥解析不对: %+v", fc)
	}
	if fc.Region != "ap-beijing" {
		t.Errorf("Region = %q", fc.Region)
	}
	if fc.TableAction != "RecognizeTableOCR" {
		t.Errorf("TableAction = %q", fc.TableAction)
	}
	if fc.ShowError == nil || *fc.ShowError {
		t.Errorf("ShowError 应解析为 false")
	}
	if fc.StripCellBreaks == nil || !*fc.StripCellBreaks {
		t.Errorf("StripCellBreaks 应解析为 true")
	}
	if fc.SecretID == "忽略我" {
		t.Errorf("不该读 [other] 段的键")
	}
}

func TestFillIfEmptyDoesNotOverwrite(t *testing.T) {
	s := "keep"
	fillIfEmpty(&s, "override")
	if s != "keep" {
		t.Errorf("覆盖了已有值: %q", s)
	}
	fillIfEmpty(&s, "")
	if s != "keep" {
		t.Errorf("被空值覆盖: %q", s)
	}
	e := ""
	fillIfEmpty(&e, "fill")
	if e != "fill" {
		t.Errorf("未填充: %q", e)
	}
}

func TestDefaultConfigSane(t *testing.T) {
	c := defaultConfig()
	if !c.StripCellBreaks {
		t.Error("默认应清理单元格换行")
	}
	if c.CellBreakReplacement != "" {
		t.Errorf("默认替换为空串，实际 %q", c.CellBreakReplacement)
	}
	if c.TextAction != "GeneralBasicOCR" || c.TableAction != "RecognizeTableAccurateOCR" {
		t.Errorf("默认 action 不对: %q / %q", c.TextAction, c.TableAction)
	}
}

// ---------------------------------------------------------------- 参数

func TestParseArgs(t *testing.T) {
	// ScreenCapture 的真实调用方式
	o := parseArgs([]string{`--image-path=C:\Users\x\AppData\Local\Temp\ocr_1.png`, "--del-image=true"})
	if o["image-path"] != `C:\Users\x\AppData\Local\Temp\ocr_1.png` {
		t.Errorf("image-path 解析失败: %q", o["image-path"])
	}
	if o["del-image"] != "true" {
		t.Errorf("del-image 解析失败: %q", o["del-image"])
	}

	// 空格分开的写法
	o = parseArgs([]string{"--config", `/tmp/x.json`})
	if o["config"] != "/tmp/x.json" {
		t.Errorf("空格式解析失败: %+v", o)
	}

	// 裸参数：json 当配置
	o = parseArgs([]string{`D:\a\config.json`})
	if o["config"] != `D:\a\config.json` {
		t.Errorf("裸 json 未识别为配置: %+v", o)
	}
	if _, ok := o["image-path"]; ok {
		t.Errorf("裸 json 不该被当成图片: %+v", o)
	}

	// 裸参数：图片
	o = parseArgs([]string{`D:\a\shot.png`})
	if o["image-path"] != `D:\a\shot.png` {
		t.Errorf("裸图片未识别: %+v", o)
	}
	if _, ok := o["config"]; ok {
		t.Errorf("裸图片不该被当成配置: %+v", o)
	}

	// 双击 → 空参数
	if o = parseArgs(nil); len(o) != 0 {
		t.Errorf("无参数应得到空 map: %+v", o)
	}
}

// ---------------------------------------------------------------- 请求头

func TestBuildAuthHeader(t *testing.T) {
	resetCfg(t)
	cfg.SecretID = "AKIDtest"
	cfg.SecretKey = "secret"

	h, err := buildAuth([]byte(`{"ImageBase64":"abc"}`), "RecognizeTableAccurateOCR")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("X-TC-Version"); got != "2018-11-19" {
		t.Errorf("版本号必须带横线，实际 %q", got)
	}
	if got := h.Get("X-TC-Action"); got != "RecognizeTableAccurateOCR" {
		t.Errorf("Action = %q", got)
	}
	auth := h.Get("Authorization")
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDtest/") {
		t.Errorf("Authorization 格式不对: %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host") {
		t.Errorf("SignedHeaders 不对: %q", auth)
	}
	sig := auth[strings.LastIndex(auth, "Signature=")+len("Signature="):]
	if len(sig) != 64 || strings.ToLower(sig) != sig {
		t.Errorf("签名格式不对: %q", sig)
	}
}

func TestBuildAuthRejectsEmptyAction(t *testing.T) {
	if _, err := buildAuth([]byte("{}"), ""); err == nil {
		t.Error("空 action 应该报错")
	}
}
