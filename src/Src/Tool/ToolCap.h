#pragma once
#include <include/Ling.h>

class WinCap;
class Tip;
// 框选完成后出现在选区右下方的工具条。往右走是图像标记 / 截长图 / 文字识别 /
// 表格识别 / 钉图，竖线之后是关闭 / 存文件 / 存剪切板。
class ToolCap : public Ling::WinBase
{
public:
	ToolCap(WinCap* win);
	~ToolCap();
private:
	void onCreated() override;
	void onMinMaxInfo(MINMAXINFO* mmi) override;
	void onClick(Ling::Button* btn);
	// 按当前 dpi 把窗口尺寸算出来并应用。构造时算一次，DPI 变了再算一次
	void refreshSize();
private:
	WinCap* win;
	// onDpiChanged 与 onSizeChanged 之间的接力标记，见构造函数里的注释
	bool dpiChanged{ false };

	// 悬停提示。要 hwnd，所以在 onCreated 里才建得起来
	std::unique_ptr<Tip> tip;
	// 三张表一一对应。spliter 是竖线分隔符，不是按钮
	std::vector<std::wstring> btnIds = { L"mark",L"long",L"ocr",L"table",L"pin",L"spliter",L"close",L"save",L"clipboard" };
	// \ue71f 是表格图标（后补进 iconfont.ttf 的 2×3 网格），\ue6a2 是长图工具条上那枚头图钉
	std::vector<std::wstring> btnCodes = { L"\ue97f",L"\ue73e",L"\ue67b",L"\ue71f",L"\ue6a2",L"",L"\ue62d",L"\ue608",L"\ue6ad" };
	// 翻译 key。空串是分隔符，没提示可挂
	std::vector<std::wstring> btnTips = { L"cap.mark",L"cap.long",L"cap.ocr",L"cap.table",L"cap.pin",L"",L"tool.close",L"tool.save",L"tool.clipboard" };
	// 以下都是逻辑像素，交给 Ling 的 setter 时由其内部乘 dpi
	static constexpr float btnSize{ 32.f };
	static constexpr float spliterW{ 1.f };
};
