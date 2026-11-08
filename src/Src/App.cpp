#include "pch.h"
#include "App.h"
#include "Setting.h"
#include "Lang.h"
#include "Util.h"
#include "./Win/WinCap.h"
#include "./Win/WinPin.h"

std::unique_ptr<App> app;


App::~App()
{
}

void App::init()
{
    auto ptr = new App();
    app.reset(ptr);
}

void App::dispose()
{
    // 窗口对象是文件级静态变量，交给静态析构就晚了（那时 CoUninitialize 已经跑完），
    // 所以趁这里把还开着的窗口先放掉
    WinPin::dispose();
    WinCap::dispose();
    Lang::dispose();
    Setting::dispose();
    app.reset();
}

App* App::get()
{
    return app.get();
}

void App::takeScreenShot(int x, int y, int w, int h, ID2D1Bitmap1** img)
{
    HDC hScreen = GetDC(NULL);
    HDC hDC = CreateCompatibleDC(hScreen);
    HBITMAP hBitmap = CreateCompatibleBitmap(hScreen, w, h);
    auto oldObj = SelectObject(hDC, hBitmap);
    BOOL bRet = BitBlt(hDC, 0, 0, w, h, hScreen, x, y, SRCCOPY);
    ReleaseDC(NULL, hScreen);
    std::vector<BYTE> data(w * 4 * h);
    BITMAPINFO bmi{};
    bmi.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    bmi.bmiHeader.biWidth = w;
    bmi.bmiHeader.biHeight = -h;
    bmi.bmiHeader.biPlanes = 1;
    bmi.bmiHeader.biBitCount = 32;
    bmi.bmiHeader.biCompression = BI_RGB;
    GetDIBits(hDC, hBitmap, 0, h, data.data(), &bmi, DIB_RGB_COLORS);
    SelectObject(hDC, oldObj);
    DeleteDC(hDC);
    DeleteObject(hBitmap);
    D2D1_BITMAP_PROPERTIES1 props = {
       .pixelFormat{D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM, D2D1_ALPHA_MODE_IGNORE)},
       .dpiX{96.0f}, .dpiY{96.0f}, .bitmapOptions{D2D1_BITMAP_OPTIONS_NONE}
    };
    auto d2d = Ling::D2D::get();
    auto hr = d2d->deviceContext->CreateBitmap(D2D1::SizeU(w, h), data.data(), w * 4, props, img);
}

std::tuple<int, int, int, int> App::getScreenArea()
{
	return std::make_tuple(GetSystemMetrics(SM_XVIRTUALSCREEN), 
        GetSystemMetrics(SM_YVIRTUALSCREEN), 
        GetSystemMetrics(SM_CXVIRTUALSCREEN), 
        GetSystemMetrics(SM_CYVIRTUALSCREEN));
}

void App::excludeFromCapture(HWND hwnd)
{
    if (!hwnd) return;
    // 老系统上这个调用不但不失败，还会把窗口变成捕获画面里的一整块黑（实测 build 18363：
    // 返回 TRUE，读回来的 affinity 就是 0x11 —— 内核照存，可那会儿的 DWM 只认"非零即
    // 受保护内容"，一律涂黑）。所以必须自己拦住，让老系统退回"照旧被录进去"。
    // GetVersionEx 会被兼容性清单骗，只有 RtlGetVersion 给的是真版本号
    static const bool supported = []() {
        OSVERSIONINFOW vi{ sizeof(vi) };
        auto rtlGetVersion = (LONG(WINAPI*)(OSVERSIONINFOW*))GetProcAddress(
            GetModuleHandleW(L"ntdll.dll"), "RtlGetVersion");
        return rtlGetVersion && rtlGetVersion(&vi) == 0 && vi.dwBuildNumber >= 19041;
    }();
    if (!supported) return;
    SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE);
}

App::App()
{
    Ling::init();
    auto app = Ling::App::get();
    app->initArgs();
    // 单实例保护：本程序没有驻留态，截图进行中再双击一次就会叠出第二层遮罩。
    // 两层看起来一模一样，用户只会觉得"拖了框没反应"（其实框画在下层）。发现已有
    // 实例就直接退场，让人接着用已经开着的那层。互斥体跟着进程一起释放，不用收尾
    HANDLE singleInstance = CreateMutexW(nullptr, FALSE, L"Local\\ScreenCapture_SingleInstance");
    if (singleInstance && GetLastError() == ERROR_ALREADY_EXISTS) {
        app->quit(0);
        return;
    }
    Ling::D2D::addFonts({ L"icon.ttf" });
    Setting::init();
    Lang::init();
    // 用完即走：本程序只为一次截图而生，动作做完自己退出，不驻留托盘、不注册全局热键。
    // --auto-start 是旧版本写进注册表的开机自启参数。现在没有驻留态了，真在开机时被叫起来
    // 就直接退场 —— 返回而不退出会让消息循环在一个窗口都没有的情况下空转成僵尸进程
    if (app->args[L"--auto-start"] == L"true") {
        Util::log(L"app: --auto-start，直接退场");
        app->quit(0);
        return;
    }
    Util::log(L"app: 启动，准备建遮罩");
    WinCap::init();
    Util::log(L"app: 遮罩已建");
}
