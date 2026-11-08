package main

// 图片文字识别模式：ScreenCapture 框选后调用
//
//	ImageReader.exe --image-path="C:\...\ocr_xxx.png" --del-image=true
//
// 识别结果整段写进剪贴板，不弹窗口。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type textDetection struct {
	DetectedText string  `json:"DetectedText"`
	Confidence   float64 `json:"Confidence"`
	ItemPolygon  struct {
		X      int `json:"X"`
		Y      int `json:"Y"`
		Width  int `json:"Width"`
		Height int `json:"Height"`
	} `json:"ItemPolygon"`
}

type generalOCRResponse struct {
	Response struct {
		Error          *apiError       `json:"Error"`
		TextDetections []textDetection `json:"TextDetections"`
		RequestId      string          `json:"RequestId"`
	} `json:"Response"`
}

// runTextMode 处理完图片后按约定删除临时图，再返回退出码。
func runTextMode(imgPath string, delImage bool) int {
	code := doTextOCR(imgPath)
	// os.Exit 不会执行 defer，清理必须显式做
	cleanup(imgPath, delImage)
	return code
}

func doTextOCR(imgPath string) int {
	if imgPath == "" || !fileExists(imgPath) {
		logf("[文字] 图片不存在: %q", imgPath)
		failText("找不到截图", "找不到截图文件：\n"+imgPath)
		return 2
	}
	if !hasCredentials() {
		logf("[文字] 未配置 secret_id / secret_key")
		failText("缺少密钥", missingKeyMsg())
		return 3
	}

	raw, err := os.ReadFile(imgPath)
	if err != nil {
		logf("[文字] 读取图片失败: %v", err)
		failText("读取失败", "读取截图失败：\n"+err.Error())
		return 1
	}

	if b64Len := len(raw)/3*4 + 4; b64Len > maxBase64Bytes {
		msg := fmt.Sprintf("选区太大（约 %d MB，上限 7 MB），请缩小框选范围。", b64Len/1024/1024)
		logf("[文字] %s", msg)
		failText("图片过大", msg)
		return 4
	}

	start := time.Now()
	lines, err := callGeneralOCR(raw)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		logf("[文字] 识别失败: %v", err)
		failText("识别失败", "OCR 失败：\n"+err.Error())
		return 1
	}

	if len(lines) == 0 {
		// 不动剪贴板：宁可失败也不要把用户原来复制的东西冲掉
		logf("[文字] 未识别到文字，剪贴板保持不变 (%d ms)", cost)
		return 0
	}

	text := strings.Join(lines, cfg.LineSeparator)
	if !setClipboardText(text) {
		logf("[文字] 识别到 %d 行，但剪贴板写入失败 (%d ms)", len(lines), cost)
		return 5
	}
	logf("[文字] 识别成功 %d 行 %d 字，%d ms", len(lines), utf8.RuneCountInString(text), cost)
	return 0
}

func callGeneralOCR(raw []byte) ([]string, error) {
	payload, err := json.Marshal(map[string]string{
		"ImageBase64": base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		return nil, err
	}

	body, err := postOCR(payload, cfg.TextAction, 20*time.Second)
	if err != nil {
		return nil, err
	}

	var r generalOCRResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w（返回内容：%s）", err, headOf(body))
	}
	if e := r.Response.Error; e != nil {
		return nil, e
	}

	type item struct {
		text string
		y, x int
	}
	var picked []item
	for _, d := range r.Response.TextDetections {
		if d.DetectedText == "" {
			continue
		}
		if d.Confidence < cfg.MinConfidence {
			continue
		}
		// 干净的换行，避免上游把 \r 一起带进来
		t := strings.TrimRight(d.DetectedText, "\r\n\v")
		if t == "" {
			continue
		}
		picked = append(picked, item{t, d.ItemPolygon.Y, d.ItemPolygon.X})
	}

	if cfg.SortByPosition {
		sort.SliceStable(picked, func(i, j int) bool {
			// 12px 当成同一行，行内按 x 排
			if absInt(picked[i].y-picked[j].y) > 12 {
				return picked[i].y < picked[j].y
			}
			return picked[i].x < picked[j].x
		})
	}

	out := make([]string, 0, len(picked))
	for _, p := range picked {
		out = append(out, p.text)
	}
	return out, nil
}

// failText 文字模式的错误提示：只弹框，不用顶部提示
// （ScreenCapture 调用时用户正等着粘贴，浮窗反而碍事）。
func failText(title, msg string) {
	if cfg.ShowError {
		showError(appText+" - "+title, msg)
	}
}

func missingKeyMsg() string {
	return "还没有配置腾讯云密钥。\n\n" +
		"请把 secret_id / secret_key 填进这个文件：\n" +
		filepath.Join(exeDir, "config.json")
}

func headOf(b []byte) string {
	if len(b) > 300 {
		return string(b[:300])
	}
	return string(b)
}
