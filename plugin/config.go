package main

// 配置：默认值、json/ini 读取、日志。
//
// 两个模式共用同一份 config.json：填一次密钥，文字识别和表格识别都能用。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	apiHost    = "ocr.tencentcloudapi.com"
	apiService = "ocr"
	// 必须带横线。写成 20181119 会返回 InvalidAction
	apiVersion = "2018-11-19"

	// 高精度版：字小、排版密的界面文字明显比基础版准，代价是单价高、单图限制更严
	//（base64 后同样 7 MB）。想换回便宜的基础版，在 config.json 里写
	// "text_action": "GeneralBasicOCR" 即可，不用重新编译
	defaultTextAction  = "GeneralAccurateOCR"
	defaultTableAction = "RecognizeTableAccurateOCR"

	// 接口限制：图片 base64 编码后不超过 7 MB
	maxBase64Bytes = 7 * 1024 * 1024

	logFileName = "ocr_plugin.log"
	logMaxBytes = 256 * 1024
)

// Config 是运行时配置（已合并默认值）。
type Config struct {
	SecretID  string
	SecretKey string
	Region    string

	// 图片文字识别
	TextAction     string
	MinConfidence  float64
	LineSeparator  string
	SortByPosition bool

	// 表格识别
	TableAction string
	// StripCellBreaks 为 true 时清掉单元格内的换行，避免粘贴到 Word 后
	// 单元格被撑成多行或表格走形。
	StripCellBreaks      bool
	CellBreakReplacement string

	// 通用
	ShowError bool
	Toast     bool
	Log       bool
}

// fileConfig 对应 config.json / config.ini 的字段。
// 用指针区分「没写」和「写了 false / 空串」。
type fileConfig struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
	Region    string `json:"region"`

	TextAction     string     `json:"text_action"`
	MinConfidence  *flexFloat `json:"min_confidence"`
	LineSeparator  *string    `json:"line_separator"`
	SortByPosition *flexBool  `json:"sort_by_position"`

	TableAction          string    `json:"table_action"`
	Action               string    `json:"action"` // 旧版 TableOCR 的字段名，兼容
	StripCellBreaks      *flexBool `json:"strip_cell_breaks"`
	CellBreakReplacement *string   `json:"cell_break_replacement"`

	ShowError *flexBool `json:"show_error"`
	Toast     *flexBool `json:"toast"`
	Log       *flexBool `json:"log"`
}

// flexBool / flexFloat 是"容忍写法"的标量。config.json 大多是手写、或被 PowerShell、
// AHK 这类工具生成的，布尔值很容易写成 "False" / "1" 这样的字符串。严格按 bool 解析的话
// 整个文件都会报废，而界面上看到的只是"还没有配置密钥" —— 完全联想不到是类型写错。
// 这里两种都认，解析失败也只影响这一个字段。
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		*b = true
	case "false", "0", "no", "off":
		*b = false
	default:
		return fmt.Errorf("不是布尔值: %q", s)
	}
	return nil
}

type flexFloat float64

func (f *flexFloat) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("不是数字: %q", s)
	}
	*f = flexFloat(v)
	return nil
}

// normalizeConfigBytes 把配置文件统一成 UTF-8：UTF-8 BOM、UTF-16 LE/BE BOM 都能吃。
// 起因：PowerShell 5.1 的 Out-File / Set-Content 默认写 UTF-16 LE，生成出来的 config.json
// 在 Go 这边报 "invalid character '\xff' looking for beginning of value"，
// 用户看到的是"还没有配置腾讯云密钥"，谁也想不到问题在编码上。
func normalizeConfigBytes(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return utf16BytesToUTF8(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return utf16BytesToUTF8(data[2:], true)
	}
	return data
}

func utf16BytesToUTF8(b []byte, bigEndian bool) []byte {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return []byte(string(utf16.Decode(u)))
}

var (
	cfg    Config
	exeDir = "."
)

func defaultConfig() Config {
	return Config{
		Region:        "ap-guangzhou",
		TextAction:    defaultTextAction,
		LineSeparator: "\r\n",
		TableAction:   defaultTableAction,
		// 默认删掉单元格内换行（替换为空串）
		StripCellBreaks:      true,
		CellBreakReplacement: "",
		ShowError:            true,
		Toast:                true,
		Log:                  true,
	}
}

// sharedPluginDir 是 ScreenCapture 存放插件配置的目录。
func sharedPluginDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "ScreenCapture", "plugin")
	}
	return ""
}

// loadConfig 按优先级合并，先到先得（只填空缺，不覆盖已填的值）：
//  1. --config 指定的文件
//  2. exe 同目录的 config.json / config.ini
//  3. %APPDATA%\ScreenCapture\plugin 下的 config.json / config.ini
//  4. 环境变量 TENCENTCLOUD_SECRET_ID / SECRET_KEY
func loadConfig(opts map[string]string) {
	cfg = defaultConfig()

	if explicit := opts["config"]; explicit != "" {
		if !applyConfigFile(explicit) {
			logf("指定的配置文件读不到: %s", explicit)
		}
	} else {
		applyConfigFile(filepath.Join(exeDir, "config.json"))
		applyConfigFile(filepath.Join(exeDir, "config.ini"))

		if dir := sharedPluginDir(); dir != "" && !samePath(dir, exeDir) {
			applyConfigFile(filepath.Join(dir, "config.json"))
			applyConfigFile(filepath.Join(dir, "config.ini"))
		}
	}

	if id, key := os.Getenv("TENCENTCLOUD_SECRET_ID"), os.Getenv("TENCENTCLOUD_SECRET_KEY"); id != "" && key != "" {
		fillIfEmpty(&cfg.SecretID, id)
		fillIfEmpty(&cfg.SecretKey, key)
	}
}

func applyConfigFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	data = normalizeConfigBytes(data) // BOM / UTF-16，见函数注释

	var fc fileConfig
	if strings.HasSuffix(strings.ToLower(path), ".ini") {
		fc = parseINIConfig(data)
	} else if err := json.Unmarshal(data, &fc); err != nil {
		logf("配置解析失败 %s: %v", path, err)
		return false
	}

	fillIfEmpty(&cfg.SecretID, fc.SecretID)
	fillIfEmpty(&cfg.SecretKey, fc.SecretKey)
	fillIfEmpty(&cfg.Region, fc.Region)
	fillIfEmpty(&cfg.TextAction, fc.TextAction)

	fillIfEmpty(&cfg.TableAction, fc.TableAction)
	fillIfEmpty(&cfg.TableAction, fc.Action) // 旧字段名

	if fc.MinConfidence != nil {
		cfg.MinConfidence = float64(*fc.MinConfidence)
	}
	if fc.LineSeparator != nil {
		cfg.LineSeparator = *fc.LineSeparator
	}
	if fc.SortByPosition != nil {
		cfg.SortByPosition = bool(*fc.SortByPosition)
	}
	if fc.StripCellBreaks != nil {
		cfg.StripCellBreaks = bool(*fc.StripCellBreaks)
	}
	if fc.CellBreakReplacement != nil {
		cfg.CellBreakReplacement = *fc.CellBreakReplacement
	}
	if fc.ShowError != nil {
		cfg.ShowError = bool(*fc.ShowError)
	}
	if fc.Toast != nil {
		cfg.Toast = bool(*fc.Toast)
	}
	if fc.Log != nil {
		cfg.Log = bool(*fc.Log)
	}
	return true
}

// parseINIConfig 只认 [tencent] 段下的基本 key=value，够读老的 config.ini。
func parseINIConfig(data []byte) fileConfig {
	var fc fileConfig
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || (section != "tencent" && section != "") {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		b := flexBool(strings.EqualFold(v, "true") || v == "1")
		switch k {
		case "secretid":
			fc.SecretID = v
		case "secretkey":
			fc.SecretKey = v
		case "region":
			fc.Region = v
		case "action":
			fc.Action = v
		case "tableaction":
			fc.TableAction = v
		case "line_separator", "lineseparator":
			fc.LineSeparator = &v
		case "stripcellbreaks", "strip_cell_breaks":
			fc.StripCellBreaks = &b
		case "cellbreakreplacement", "cell_break_replacement":
			fc.CellBreakReplacement = &v
		case "showerror":
			fc.ShowError = &b
		case "toast":
			fc.Toast = &b
		case "log":
			fc.Log = &b
		}
	}
	return fc
}

func fillIfEmpty(dst *string, v string) {
	if *dst == "" && v != "" {
		*dst = v
	}
}

func samePath(a, b string) bool {
	pa, err1 := filepath.Abs(a)
	pb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(pa, pb)
}

func hasCredentials() bool {
	return cfg.SecretID != "" && cfg.SecretKey != ""
}

func logf(format string, a ...any) {
	if !cfg.Log {
		return
	}
	path := filepath.Join(exeDir, logFileName)
	if st, err := os.Stat(path); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	// 多行消息压成一行，否则 tail 只能看到半句
	msg := strings.ReplaceAll(fmt.Sprintf(format, a...), "\n", "\\n")
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func cleanup(path string, del bool) {
	if !del || !fileExists(path) {
		return
	}
	if err := os.Remove(path); err != nil {
		logf("删除临时图失败: %v", err)
	}
}
