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
	"strings"
	"time"
)

const (
	apiHost    = "ocr.tencentcloudapi.com"
	apiService = "ocr"
	// 必须带横线。写成 20181119 会返回 InvalidAction
	apiVersion = "2018-11-19"

	defaultTextAction  = "GeneralBasicOCR"
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

	TextAction     string   `json:"text_action"`
	MinConfidence  *float64 `json:"min_confidence"`
	LineSeparator  *string  `json:"line_separator"`
	SortByPosition *bool    `json:"sort_by_position"`

	TableAction          string  `json:"table_action"`
	Action               string  `json:"action"` // 旧版 TableOCR 的字段名，兼容
	StripCellBreaks      *bool   `json:"strip_cell_breaks"`
	CellBreakReplacement *string `json:"cell_break_replacement"`

	ShowError *bool `json:"show_error"`
	Toast     *bool `json:"toast"`
	Log       *bool `json:"log"`
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
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // 记事本会加 BOM

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
		cfg.MinConfidence = *fc.MinConfidence
	}
	if fc.LineSeparator != nil {
		cfg.LineSeparator = *fc.LineSeparator
	}
	if fc.SortByPosition != nil {
		cfg.SortByPosition = *fc.SortByPosition
	}
	if fc.StripCellBreaks != nil {
		cfg.StripCellBreaks = *fc.StripCellBreaks
	}
	if fc.CellBreakReplacement != nil {
		cfg.CellBreakReplacement = *fc.CellBreakReplacement
	}
	if fc.ShowError != nil {
		cfg.ShowError = *fc.ShowError
	}
	if fc.Toast != nil {
		cfg.Toast = *fc.Toast
	}
	if fc.Log != nil {
		cfg.Log = *fc.Log
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
		b := strings.EqualFold(v, "true") || v == "1"
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
