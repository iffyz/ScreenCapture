#pragma once
#include <include/Ling.h>

class CutMask;
class ToolCap;
// 截图主窗口。铺满整个虚拟桌面，拖框结束后并不马上让位，而是留下来当宿主：
// 选区可以继续调整，选区右下方摆一个 ToolCap。
class WinCap:public Ling::WinBase
{
public:
	~WinCap();
	static void init();
	static WinCap* get();
	// 退出流程里调：窗口对象是文件级静态变量，交给静态析构就在 CoUninitialize 之后了
	static void dispose();
	Microsoft::WRL::ComPtr<ID2D1Bitmap1> getCutImg();
	// 工具条统一定位规则：右边与选区右边对齐，下方空间够就摆在选区右下方，
	// 不够就摆右上方，上下都不够就盖在选区右下角内部（留一点边距）
	void layoutTool(Ling::WinBase* tool);
	// 下面都是给工具条用的门面 ————————————————
	// ToolCap
	// 图像标记：钉到桌面上，带注释工具条
	void startPin();
	// 钉图：只把图贴在桌面上，不带工具条，单击或 ESC 关闭
	void startPinPure();
	// 文字识别 / 表格识别：选框交给外部插件 ImageReader.exe，结果直接进剪切板
	void startOcr();
	void startTableOcr();
	void saveToFile();
	void copyToClipboard();
public:
	// 选区就存在这里，工具条定位与各个动作都从它取，不再各自框选
	std::unique_ptr<CutMask> cutMask;
private:
	WinCap();
	void onCreated() override;
	void layout() override;
	BOOL setCursor() override;
	LRESULT onHitTest(const POINT pos) override;
	void setPixPos(POINT pos);
	void getPixImg(POINT pos);
	void paintPix(ID2D1DeviceContext* ctx);
	void onKey(UINT key);
	// Enter 用：把选区里的像素存进剪切板，效果与 Ctrl+C 一致。
	// 还在拖框取色（Select）时手上什么都没有，什么也不做
	void copyCurrentStage();
	void onDown(POINT pos, bool isRight);
	void onMove(POINT pos);
	void onUp(POINT pos, bool isRight);
	void onClosed();
	void makeToolCap();
	// 命令行给了 --enter=xxx（ocr / table / pin / pinonly）时，框完选区不出 ToolCap，
	// 直接走对应的那条路 —— 等于替用户点了工具条上的那个按钮。
	// 返回是否已经接手；值不认识（拼错了）就返回 false，照常出工具条
	bool enterByArg();
	// DPI 变了之后重走一遍工具条的摆放规则
	void relayoutTool();
	// 选区内的像素。BGRA、top-down、行紧凑，可以直接喂 Util 的存盘与剪切板
	bool getCutPixels(std::vector<BYTE>& pixels, int& cw, int& ch);
private:
	// Select 拖框取色 -> Adjust 调整选区并显示 ToolCap
	enum class CapStage { Select, Adjust };
	CapStage stage{ CapStage::Select };
	std::unique_ptr<ToolCap> toolCap;
	Microsoft::WRL::ComPtr<ID2D1Bitmap1> screenImg,pixImg;
	D2D1_RECT_F pixSrcRect{};
	// 铺满窗口的画布，走 swap chain 双缓冲：底图、蒙版、放大镜每帧都重画，
	// 单缓冲会让合成器采到"擦干净还没画完"的中间态
	Ling::Canvas* canvas{ nullptr };
	POINT pixPos;
	bool isPress{ false }, isClosed{ false };
	// onDpiChanged 与 onSizeChanged 之间的接力标记，见构造函数里的注释
	bool dpiChanged{ false };
	// 自己认双击用的上一次按下时间与位置。Ling 的窗口类没带 CS_DBLCLKS，
	// 收不到 WM_LBUTTONDBLCLK，只能按系统的双击间隔和双击判定框自己算
	ULONGLONG lastDownTime{ 0 };
	POINT lastDownPos{ 0, 0 };
	Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brushBg;
	Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brushText;
	Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> crossBrush;
};

