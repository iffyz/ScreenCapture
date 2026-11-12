#include "pch.h"
#include <include/Ling.h>
#include "Setting.h"
#include "Util.h"
#include "App.h"

namespace {
    std::unique_ptr<Setting> setting;
    // 配置文件的默认内容。空文件、坏 JSON、缺键都拿它兜底，所以这里列出的每一项
    // 都是代码里会直接按名字取的（见 getLang / getToolObj）
    // 老的 autoStart / shortcutKey 两项已经删掉：前者随开机自启一起废了，后者在热键注册
    // 去掉之后没人读。用户那份 config.json 里残留的键不必清理，读的时候都是按名字取，
    // 多出来的键不影响任何逻辑，硬删反而会让用户丢掉"以后可能还想用"的值
    constexpr std::wstring_view defaultConfig{ LR"""({"common":{"language":"zh-CN"}})""" };
}


Setting::Setting() :dataPath{ initDataPath() }, configPath{ initConfigPath() }
{
    if (std::filesystem::exists(configPath)) {
        auto content = Ling::Util::readFileText(configPath);
        if (content.empty() || content.find_first_not_of(L" \t\r\n") == std::wstring::npos) {
            configObj = JsonObject::Parse(defaultConfig);
            save();
            return;
        }
        JsonObject obj{ nullptr };
        if (JsonObject::TryParse(content, obj)) {
            configObj = obj;
            return;
        }
        MessageBox(nullptr, L"config.json parse error，use default config", L"ScreenCapture", MB_OK | MB_ICONWARNING);
        // 解析失败还硬用默认配置继续跑，下一次任何 set（比如调一下标注工具的滑块）
        // 都会把这份坏文件整份覆盖掉 —— 里面可能只是少了个逗号，密钥还在。先留一份副本，
        // 出问题时用户还能手工把密钥捞回来。副本已存在就不覆盖，免得把最早那份好的冲掉
        CopyFileW(configPath.wstring().data(), (configPath.wstring() + L".bad").data(), TRUE);
    }
    configObj = JsonObject::Parse(defaultConfig); 
}



Setting::~Setting()
{

}

void Setting::init()
{
    auto ptr = new Setting();
    setting.reset(ptr);
}

void Setting::dispose()
{
    setting.reset();
}

Setting* Setting::get()
{
    return setting.get();
}

std::filesystem::path Setting::getDataPath()
{
    return dataPath; //复制一份路径对象，不允许就地修改
}

const JsonObject Setting::getConfigObj()
{
    return configObj;
}

std::filesystem::path Setting::initDataPath()
{
    PWSTR pathTmp;
    auto hr = SHGetKnownFolderPath(FOLDERID_RoamingAppData, 0, nullptr, &pathTmp);
    if (FAILED(hr)) {
        _ASSERT_EXPR(FALSE, L"get roaming path，error");
        return L"";
    }
    auto dataPath = std::filesystem::path{ pathTmp };
    CoTaskMemFree(pathTmp);
    // 只拼路径，不建目录。原来这里是"拼完就 create_directories"，结果绿色版
    // （exe 旁边已经有 config.json）一启动也会在 %appdata% 下留一个空的
    // ScreenCapture 文件夹 —— 用户看着就像"还想往 C 盘塞东西"。目录改由
    // initConfigPath 在真要落到 appdata 的那一刻才建
    return dataPath.append("ScreenCapture");
}

std::filesystem::path Setting::initConfigPath()
{
    // 与插件的查找顺序一致（见 Util.cpp 里的 findImageReader）：先看 exe 同目录。
    // 只有那份文件本来就存在时才认它 —— 不存在就不要在程序目录里新建，
    // 装在 Program Files 下时那儿通常没有写权限，况且默认位置该是 appdata
    wchar_t buffer[MAX_PATH]{};
    GetModuleFileName(nullptr, buffer, MAX_PATH);
    auto path = std::filesystem::path{ buffer }.parent_path().append(L"config.json");
    if (std::filesystem::exists(path)) return path;
    auto fallback = this->dataPath; //复制一份路径对象，append 会就地改
    if (fallback.empty()) return {};
    // 只有确定要走 appdata 这条路时才建目录（老版本的配置就在这里）。
    // 绿色版在上面就 return 了，从此不会为它凭空造文件夹
    std::error_code ec;
    std::filesystem::create_directories(fallback, ec);
    return fallback.append(L"config.json");
}

void Setting::save()
{
    std::wstring str{ configObj.Stringify() };
    // 原子写：配置文件里存着 OCR 的密钥，而原来那条路是"直接覆盖目标文件"。写到一半掉电、
    // 被任务管理器结束、或者盘满了，留下的就是半个 JSON；下次启动解析失败会换成默认配置，
    // 再存一次就把密钥彻底冲掉。改成先写同目录的 .tmp，落盘之后用 ReplaceFileW 整份替换：
    // 要么是旧的完整内容，要么是新的完整内容，不存在中间态。
    // ReplaceFileW 顺带把被替换掉的那份留成 .bak，等于每次保存都自动备份"上一份可用的配置"
    auto target = configPath.wstring();
    // 取不到 roaming 路径时 configPath 是空的，这时 target 会变成相对路径 ".tmp"，
    // 落到"当前工作目录"里 —— 那可能是任何地方。什么都不写才是对的
    if (target.empty()) return;
    auto tmpPath = target + L".tmp";
    auto bakPath = target + L".bak";

    // 编码必须与 Ling::Util::saveFile 一致：UTF-16 LE + BOM。读侧（C++ 与 Go 插件）
    // 靠 BOM 判断编码，写成无 BOM 的会被当成 UTF-8 认，中文语言名就乱了
    HANDLE hFile = CreateFileW(tmpPath.data(), GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (hFile == INVALID_HANDLE_VALUE) return;
    DWORD written = 0;
    WORD bom = 0xFEFF;
    bool ok = WriteFile(hFile, &bom, sizeof(bom), &written, nullptr)
        && WriteFile(hFile, str.data(), static_cast<DWORD>(str.size() * sizeof(wchar_t)), &written, nullptr);
    // 先刷到盘再替换：不然 ReplaceFileW 换上去的可能只是一个还在系统缓存里的空壳
    if (ok) ok = FlushFileBuffers(hFile) != FALSE;
    CloseHandle(hFile);
    if (!ok) {
        DeleteFileW(tmpPath.data());
        return;
    }

    if (!ReplaceFileW(target.data(), tmpPath.data(), bakPath.data(), REPLACEFILE_IGNORE_MERGE_ERRORS, nullptr, nullptr)) {
        // 目标文件还不存在（第一次保存）时 ReplaceFileW 会失败，退回普通改名。
        // 此时没有旧文件可备份，也就没有 .bak，属于预期内的情况
        if (!MoveFileExW(tmpPath.data(), target.data(), MOVEFILE_REPLACE_EXISTING)) {
            DeleteFileW(tmpPath.data());
        }
    }
}

std::wstring Setting::getLang()
{
    auto common = configObj.GetNamedObject(L"common", nullptr);
    if (!common) return L"zh-CN";
    return std::wstring{ common.GetNamedString(L"language", L"zh-CN") };
}

JsonObject Setting::getToolObj(const std::wstring& tool)
{
    // 用带默认值的重载：这两层在旧配置文件里都不存在，直接 GetNamedObject 会抛异常，
    // 值被手工改成非对象时它也一样返回默认值，不会炸
    auto root = configObj.GetNamedObject(L"toolPin", nullptr);
    if (!root) {
        root = JsonObject();
        configObj.SetNamedValue(L"toolPin", root);
    }
    auto obj = root.GetNamedObject(tool, nullptr);
    if (!obj) {
        obj = JsonObject();
        root.SetNamedValue(tool, obj);
    }
    return obj;
}

bool Setting::getToolFlag(const std::wstring& tool, const std::wstring& key, bool def)
{
    return getToolObj(tool).GetNamedBoolean(key, def);
}

void Setting::setToolFlag(const std::wstring& tool, const std::wstring& key, bool val)
{
    getToolObj(tool).SetNamedValue(key, JsonValue::CreateBooleanValue(val));
    save();
}

float Setting::getToolNum(const std::wstring& tool, const std::wstring& key, float def)
{
    return static_cast<float>(getToolObj(tool).GetNamedNumber(key, def));
}

void Setting::setToolNum(const std::wstring& tool, const std::wstring& key, float val)
{
    getToolObj(tool).SetNamedValue(key, JsonValue::CreateNumberValue(val));
    save();
}
