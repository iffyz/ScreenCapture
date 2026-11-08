#pragma once
#include <include/Ling.h>
#include <fstream>
#include <array>

// 图像输出相关的工具函数。data 一律要求 BGRA、top-down、行紧凑（步长 = w*4），
// 这也是 WinPin::getImagePixels 交出来的格式。
class Util
{
public:
	// 同时写入 CF_DIBV5（Office / 微信 / WPS 这类原生程序认）和 "PNG" 注册格式
	//（浏览器 / Electron 程序认），两份都带 alpha
	static void saveToClipboard(const int w, const int h, BYTE* data);
	static bool saveToFile(const std::wstring& path, const int w, const int h, BYTE* data);
	// 弹系统另存为对话框，返回空串表示用户取消
	static std::wstring getSaveFilePath(HWND hwnd, const std::wstring& ext = L"png");
	// 以当前时间生成默认文件名，精确到毫秒，避免连续保存时重名
	static std::wstring createFileName(const std::wstring& ext);
	// GDI 抓屏。返回 BGRA、top-down、行紧凑（步长 = w*4），与本类其他函数的入参格式一致
	static std::vector<BYTE> captureScreen(const int x, const int y, const int w, const int h);
	// 把文件路径以 CF_HDROP 写进剪切板，粘贴到资源管理器/聊天窗口就是一个文件
	static void addFileToClipboard(const std::wstring& filePath);
	// 把图存成缓存文件，再交给外部插件 ImageReader.exe 做识别。插件先在本 exe
	// 同目录找，再找 %appdata%\ScreenCapture\plugin，都找不到就用默认浏览器打开它的
	// release 页面让用户自己下。缓存图由插件读完后自己删。
	// table = true 时多传一个 --mode=table，让插件按表格解析并把 HTML 表格写进剪切板
	static bool openWithImageReader(const int w, const int h, BYTE* data, bool table = false);
	// 往 %TEMP%\ScreenCapture.log 追加一行带时间戳的记录。
	// 改成"用完即退"后没有任何界面能反映程序在干什么，出问题只能靠这个文件定位：
	// 究竟是贴图窗口没建起来、还是建起来了又被退出流程顺手带走
	static void log(const std::wstring& msg);
	// 把已经没人用的图形内存真正还给系统。截图遮罩关掉时它会放掉一张整屏位图和两条
	// 与屏幕等大的交换链（1080p 约 25 MB），但 D2D / DXGI 只是把那些块记成"空闲"，
	// 页面仍算在进程工作集里。钉图要在桌面上摆一段时间，"只贴了一张图"却占着 90 MB
	// 太显眼，所以在这个交接点清一次
	static void trimMemory();
};
