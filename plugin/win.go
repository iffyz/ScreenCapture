package main

// Windows 层：剪贴板（读图片 / 写文本 / 写 HTML 表格）、轻提示窗、消息框、单实例锁。
// 全部走 syscall，不引入任何第三方依赖。

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	cfBitmap      = 2
	cfDIB         = 8
	cfDIBV5       = 17
	cfUnicodeText = 13

	gmemMoveable = 0x0002

	mbIconError   = 0x00000010
	mbIconWarning = 0x00000030

	wsPopup        = 0x80000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsExLayered    = 0x00080000
	wsExNoActivate = 0x08000000

	ssCenter      = 0x00000001
	ssCenterImage = 0x00000200

	wmDestroy = 0x0002
	wmTimer   = 0x0113
	wmSetFont = 0x0030

	swShow = 5

	lwaAlpha = 0x00000002

	errorAlreadyExists = 183
	errorClassExists   = 1410
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	// instanceMutex 是单实例互斥体句柄，0 表示未持有
	instanceMutex uintptr

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procMessageBoxW                = user32.NewProc("MessageBoxW")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procCloseHandle  = kernel32.NewProc("CloseHandle")

	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procPostQuitMessage         = user32.NewProc("PostQuitMessage")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procTranslateMessage        = user32.NewProc("TranslateMessage")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procShowWindow              = user32.NewProc("ShowWindow")
	procSetTimer                = user32.NewProc("SetTimer")
	procGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	procSetLayeredWindowAttribs = user32.NewProc("SetLayeredWindowAttributes")
	procSendMessageW            = user32.NewProc("SendMessageW")

	// CreateFontW 属于 gdi32，不在 user32
	procCreateFontW = gdi32.NewProc("CreateFontW")
)

// ---------------------------------------------------------------- 剪贴板读取

// clipImage 是剪贴板里取到的一张图。
// data 已经是可直接上传的完整图片字节（PNG 原样，DIB 转成 BMP）。
type clipImage struct {
	data []byte
	kind string
	w, h int
}

func openClipboardRetry() bool {
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// readClipboardImage 优先取 PNG（无损、体积小），其次 CF_DIB / CF_DIBV5。
func readClipboardImage() (*clipImage, error) {
	if !openClipboardRetry() {
		return nil, errors.New("剪贴板正被其他程序占用，请稍后重试")
	}
	defer procCloseClipboard.Call()

	// 1) PNG：截图工具和多数程序会提供，拿到就是最终字节
	if id, _, _ := procRegisterClipboardFormatW.Call(strPtr("PNG")); id != 0 {
		if raw := readClipboardData(id); len(raw) > 8 && isPNG(raw) {
			return &clipImage{
				data: raw,
				kind: "png",
				w:    int(be32(raw, 16)),
				h:    int(be32(raw, 20)),
			}, nil
		}
	}

	// 2) CF_DIB / CF_DIBV5：补齐 BMP 文件头
	var lastErr error
	for _, format := range []uintptr{cfDIB, cfDIBV5} {
		raw := readClipboardData(format)
		if len(raw) < 40 {
			continue
		}
		bmp, w, h, err := dibToBMP(raw)
		if err != nil {
			lastErr = err
			continue
		}
		return &clipImage{data: bmp, kind: "bmp", w: w, h: h}, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("剪贴板图片格式不支持（%v）", lastErr)
	}

	// 诊断：只有需要 GDI 才能读出来的位图句柄时，别让用户以为剪贴板是空的
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfBitmap); r != 0 {
		return nil, errors.New("剪贴板里的图片是 GDI 位图（CF_BITMAP）格式，本程序暂不支持。\n\n" +
			"请用截图工具重新截一次（Win+Shift+S 或微信 Alt+A），那个格式可以直接识别")
	}

	return nil, errors.New("剪贴板里没有图片。\n\n请先截图或复制一张图片，再运行本程序")
}

func readClipboardData(format uintptr) []byte {
	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 || size > 64*1024*1024 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)

	out := make([]byte, int(size))
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(p)), int(size)))
	return out
}

func isPNG(b []byte) bool {
	return b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'
}

// ------------------------------------------------------- DIB → BMP 文件字节

func dibToBMP(dib []byte) ([]byte, int, int, error) {
	if len(dib) < 40 {
		return nil, 0, 0, errors.New("DIB 数据过短")
	}
	biSize := int(le32(dib, 0))
	if biSize < 40 || biSize > len(dib) {
		return nil, 0, 0, errors.New("DIB 头非法")
	}
	w := int(int32(le32(dib, 4)))
	hRaw := int(int32(le32(dib, 8)))
	bits := int(le16(dib, 14))
	compression := le32(dib, 16)
	clrUsed := int(le32(dib, 32))

	if w <= 0 || hRaw == 0 || w > 20000 || absInt(hRaw) > 20000 {
		return nil, 0, 0, fmt.Errorf("DIB 尺寸异常 (%dx%d)", w, hRaw)
	}
	h := absInt(hRaw)

	// 调色板 / 颜色掩码占用多少字节
	palBytes := 0
	switch {
	case bits <= 8:
		n := clrUsed
		if n == 0 {
			n = 1 << bits
		}
		palBytes = n * 4
	case compression == 3 && biSize == 40:
		palBytes = 12
	}
	pixelOff := biSize + palBytes
	if pixelOff > len(dib) {
		return nil, 0, 0, errors.New("DIB 像素偏移越界")
	}

	// 24 位及以下（含 RLE）像素布局与 BMP 完全一致，补 14 字节文件头即可
	if bits <= 24 {
		out := make([]byte, 14+len(dib))
		writeBMPHeader(out, len(out), 14+pixelOff)
		copy(out[14:], dib)
		return out, w, h, nil
	}

	// 32 位：直接丢掉 alpha 转 24 位。
	// 屏幕截图的 32bpp DIB 常常 alpha 全 0，原样当 BMP 用会被解码成整片透明/黑。
	if bits == 32 {
		return dib32To24(dib, w, hRaw, pixelOff)
	}

	return nil, 0, 0, fmt.Errorf("%d 位、压缩方式 %d", bits, compression)
}

func dib32To24(dib []byte, w, hRaw, pixelOff int) ([]byte, int, int, error) {
	topDown := hRaw < 0
	h := absInt(hRaw)
	srcStride := w * 4
	dstStride := ((w*3 + 3) / 4) * 4

	if pixelOff+srcStride*h > len(dib) {
		return nil, 0, 0, errors.New("DIB 像素数据不完整")
	}

	imgSize := dstStride * h
	out := make([]byte, 54+imgSize)
	writeBMPHeader(out, len(out), 54)
	put32(out, 14, 40)              // biSize
	put32(out, 18, uint32(w))       // biWidth
	put32(out, 22, uint32(h))       // biHeight > 0 表示 bottom-up
	put16(out, 26, 1)               // biPlanes
	put16(out, 28, 24)              // biBitCount
	put32(out, 30, 0)               // BI_RGB
	put32(out, 34, uint32(imgSize)) // biSizeImage
	put32(out, 38, 2835)            // 72 DPI
	put32(out, 42, 2835)

	dst := out[54:]
	for y := 0; y < h; y++ {
		srcY := y
		if topDown {
			// 源是 top-down，输出要求 bottom-up，行序翻转
			srcY = h - 1 - y
		}
		src := dib[pixelOff+srcY*srcStride:]
		row := dst[y*dstStride:]
		for x := 0; x < w; x++ {
			s, d := x*4, x*3
			row[d] = src[s+0] // B
			row[d+1] = src[s+1]
			row[d+2] = src[s+2]
		}
	}
	return out, w, h, nil
}

func writeBMPHeader(b []byte, total, offBits int) {
	b[0], b[1] = 'B', 'M'
	put32(b, 2, uint32(total))
	put32(b, 6, 0)
	put32(b, 10, uint32(offBits))
}

// ---------------------------------------------------------------- 剪贴板写入

// setClipboardText 只写纯文本，给图片文字识别用。
func setClipboardText(text string) bool {
	if !openClipboardRetry() {
		logf("剪贴板一直被占用，写入失败")
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	return setClipboardRaw(cfUnicodeText, utf16Bytes(text))
}

// cfHTMLHeader 里所有偏移都以「字节」计，且数字必须是 10 位定宽，
// 因此头部长度固定（105 字节），可以先把占位写出来算长度。
const cfHTMLHeader = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"

const (
	cfHTMLPrefix = "<html><body><!--StartFragment-->"
	cfHTMLSuffix = "<!--EndFragment--></body></html>"
)

// buildCFHTML 组装 CF_HTML 剪贴板数据（UTF-8 字节）。
func buildCFHTML(fragment string) []byte {
	hLen := len(fmt.Sprintf(cfHTMLHeader, 0, 0, 0, 0))
	startFrag := hLen + len(cfHTMLPrefix)
	endFrag := startFrag + len(fragment)
	endHTML := endFrag + len(cfHTMLSuffix)

	head := fmt.Sprintf(cfHTMLHeader, hLen, endHTML, startFrag, endFrag)
	return []byte(head + cfHTMLPrefix + fragment + cfHTMLSuffix)
}

// setClipboardTable 一次打开剪贴板，同时写纯文本和 HTML Format，
// Word / WPS 会优先按 HTML 识别成真表格。
func setClipboardTable(fragment, plainText string) bool {
	if !openClipboardRetry() {
		logf("剪贴板一直被占用，写入失败")
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	// 先纯文本，再 HTML：部分程序（含 Word）对顺序敏感
	okText := setClipboardRaw(cfUnicodeText, utf16Bytes(plainText))

	htmlFormat, _, _ := procRegisterClipboardFormatW.Call(strPtr("HTML Format"))
	if htmlFormat == 0 {
		return false
	}
	okHTML := setClipboardRaw(htmlFormat, append(buildCFHTML(fragment), 0))

	return okText && okHTML
}

func setClipboardRaw(format uintptr, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return false
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(data)), data)
	procGlobalUnlock.Call(h)

	// SetClipboardData 成功后这块内存归系统所有，不能再 GlobalFree
	if r, _, _ := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return false
	}
	return true
}

func utf16Bytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, (len(u)+1)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(out[i*2:], v)
	}
	return out
}

// ---------------------------------------------------------------- 用户反馈

// showError 弹一个模态错误框。
func showError(title, msg string) {
	procMessageBoxW.Call(0, strPtr(msg), strPtr(title), mbIconError)
}

// showToast 在屏幕上方弹一个无边框、不可激活的小提示，到期自动消失。
// 阻塞到窗口销毁为止。
func showToast(msg string, ms uint32) {
	// 提示窗只是锦上添花。此时剪贴板已写好，任何 Win32 意外都不能把
	// 整个进程带崩，否则用户会以为识别失败。
	defer func() { _ = recover() }()

	hInst, _, _ := procGetModuleHandleW.Call(0)

	className := "OCRToolToast"
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(toastWndProc),
		hInstance:     hInst,
		hbrBackground: 6, // COLOR_WINDOW + 1，白底
		lpszClassName: strPtr(className),
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		if errno, ok := err.(syscall.Errno); !ok || errno != errorClassExists {
			return
		}
	}

	w, h := measureToast(msg)
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	x := (int(sw) - w) / 2
	y := int(sh) / 3

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExTopmost|wsExToolWindow|wsExLayered|wsExNoActivate,
		strPtr(className), 0, wsPopup,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, hInst, 0)
	if hwnd == 0 {
		return
	}
	procSetLayeredWindowAttribs.Call(hwnd, 0, 238, lwaAlpha)

	child, _, _ := procCreateWindowExW.Call(
		0, strPtr("STATIC"), strPtr(msg),
		wsChild|wsVisible|ssCenter|ssCenterImage,
		0, 0, uintptr(w), uintptr(h), hwnd, 0, hInst, 0)

	hFont, _, _ := procCreateFontW.Call(
		17, 0, 0, 0, 400,
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0,
		5, // CLEARTYPE_QUALITY
		0,
		strPtr("Microsoft YaHei UI"))
	if child != 0 && hFont != 0 {
		procSendMessageW.Call(child, wmSetFont, hFont, 1)
	}

	procShowWindow.Call(hwnd, swShow)
	procSetTimer.Call(hwnd, 1, uintptr(ms), 0)

	// 兜底：SetTimer 万一没生效，下面那个 GetMessage 会一直等下去 —— 结果明明已经进了
	// 剪贴板，进程却挂在这里不走，用户看到的就是"跑完卡住了"。等够时间自己补一个 WM_TIMER
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-time.After(time.Duration(ms)*time.Millisecond + 1500*time.Millisecond):
			procSendMessageW.Call(hwnd, wmTimer, 0, 0)
		case <-done:
		}
	}()

	var m msgW
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func toastWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTimer:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// measureToast 粗估提示框尺寸：中文按 17px、其余按 9px 累加。
func measureToast(msg string) (int, int) {
	w := 0
	for _, r := range msg {
		if r > 0x2E80 {
			w += 17
		} else {
			w += 9
		}
	}
	w += 48
	if w < 240 {
		w = 240
	}
	if w > 900 {
		w = 900
	}
	return w, 48
}

// acquireSingleInstance 防止连点出现多个进程同时抢剪贴板。
func acquireSingleInstance() bool {
	h, _, err := procCreateMutexW.Call(0, 0, strPtr("Local\\OCRTool_Go_SingleInstance"))
	if h == 0 {
		return true
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		return false
	}
	instanceMutex = h
	return true
}

// releaseSingleInstance 在结果已经写进剪贴板后立刻放锁，
// 这样提示窗还挂在屏幕上时，用户就能马上再识别下一张。
func releaseSingleInstance() {
	if instanceMutex != 0 {
		procCloseHandle.Call(instanceMutex)
		instanceMutex = 0
	}
}

// ------------------------------------------------------------------ 小工具

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
	hIconSm       uintptr
}

type point struct{ X, Y int32 }

type msgW struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// strCache 保证交给 Win32 的 UTF-16 字符串不会被 GC 回收。
// 进程存活时间很短，累积这点内存无所谓。
var strCache []*uint16

func strPtr(s string) uintptr {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return 0
	}
	strCache = append(strCache, p)
	return uintptr(unsafe.Pointer(p))
}

func le16(b []byte, off int) uint16 { return binary.LittleEndian.Uint16(b[off:]) }
func le32(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }
func be32(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }

func put16(b []byte, off int, v uint16) { binary.LittleEndian.PutUint16(b[off:], v) }
func put32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
