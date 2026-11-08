// OCR 工具（Go 版）：一个 exe，两种用法。
//
//	① 图片文字识别 —— ScreenCapture 框选后由主程序调用：
//	     ImageReader.exe --image-path="C:\...\ocr_xxx.png" --del-image=true
//	   → 文字写进剪贴板，全程无窗口。
//
//	② 表格识别 —— 用户双击运行（不带参数）：
//	   → 读剪贴板里的表格截图，识别成 HTML 表格写回剪贴板，
//	     到 Word / WPS 里 Ctrl+V 就是真表格。
//
// 两种模式共用同一份 config.json，密钥只填一次。
// 只用标准库，CGO_ENABLED=0 编译，产物是单个静态 exe。
package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	appText  = "图片文字识别"
	appTable = "表格识别"
)

func main() {
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	opts := parseArgs(os.Args[1:])
	loadConfig(opts)

	// 无密钥也能跑的自检：验证剪贴板写入链路
	if text, ok := opts["selftest-clipboard"]; ok {
		if setClipboardText(text) {
			os.Exit(0)
		}
		os.Exit(5)
	}

	// 调试：把剪贴板里的图导成 BMP，用来判断问题出在取图还是接口
	if out := opts["dump-clipboard-image"]; out != "" {
		os.Exit(dumpClipboardImage(out))
	}

	imgPath := imagePathFrom(opts)
	delImage := strings.EqualFold(opts["del-image"], "true")

	// --mode 可以强制指定，正常使用用不到
	switch strings.ToLower(opts["mode"]) {
	case "text", "ocr", "reader":
		os.Exit(runTextMode(imgPath, delImage))
	case "table":
		// 带 --image-path 是 ScreenCapture 工具条上的「表格识别」按钮：
		// 直接读那张框选缓存图，不碰剪贴板，也不加单实例锁
		if imgPath != "" {
			os.Exit(runTableModeFrom(imgPath, delImage))
		}
		os.Exit(runTableMode())
	}

	// 有图路径 = ScreenCapture 的插件调用；没有 = 用户双击，处理剪贴板
	if imgPath != "" {
		os.Exit(runTextMode(imgPath, delImage))
	}

	// 表格模式耗时较长又带提示窗，加单实例锁防止连点
	if !acquireSingleInstance() {
		os.Exit(0)
	}
	os.Exit(runTableMode())
}

// imagePathFrom 兼容 --image-path 和裸路径两种写法。
func imagePathFrom(opts map[string]string) string {
	if p := opts["image-path"]; p != "" {
		return p
	}
	return opts["image"]
}

func dumpClipboardImage(out string) int {
	img, err := readClipboardImage()
	if err != nil {
		logf("[调试] 取图失败: %v", err)
		return 2
	}
	if err := os.WriteFile(out, img.data, 0o644); err != nil {
		logf("[调试] 写文件失败: %v", err)
		return 1
	}
	logf("[调试] 已导出剪贴板图片 %s %dx%d %d 字节", img.kind, img.w, img.h, len(img.data))
	return 0
}

// parseArgs 解析 --key=value / --key value / -key 三种写法。
// 裸参数按扩展名区分：.json/.ini 当配置文件，其余当图片路径。
func parseArgs(args []string) map[string]string {
	out := map[string]string{}
	var positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		body := strings.TrimLeft(a, "-")
		if k, v, ok := strings.Cut(body, "="); ok {
			out[strings.ToLower(k)] = v
			continue
		}
		k := strings.ToLower(body)
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out[k] = args[i+1]
			i++
			continue
		}
		out[k] = "true"
	}

	for _, p := range positional {
		lower := strings.ToLower(p)
		switch {
		case strings.HasSuffix(lower, ".json"), strings.HasSuffix(lower, ".ini"):
			if _, ok := out["config"]; !ok {
				out["config"] = p
			}
		default:
			if _, ok := out["image-path"]; !ok {
				out["image-path"] = p
			}
		}
	}
	return out
}
