package ui

import (
	"net/http"
	"strings"
)

type Locale string

const (
	Chinese Locale = "zh-CN"
	English Locale = "en"
)

func requestLocale(r *http.Request) Locale {
	if c, err := r.Cookie("diskord_lang"); err == nil {
		if c.Value == string(English) {
			return English
		}
		if c.Value == string(Chinese) {
			return Chinese
		}
	}
	for _, item := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		language := strings.ToLower(strings.TrimSpace(strings.Split(item, ";")[0]))
		if strings.HasPrefix(language, "zh") {
			return Chinese
		}
		if strings.HasPrefix(language, "en") {
			return English
		}
	}
	return Chinese
}

func (l Locale) T(zh string) string {
	if l != English {
		return zh
	}
	if en, ok := english[zh]; ok {
		return en
	}
	return zh
}

var english = map[string]string{
	"本地控制台": "Local console", "本地消息观察站": "Local message archive",
	"在本机终端执行": "Run in a local terminal", "获取访问令牌。不要分享运行时目录或 CA 私钥。": "to get the access token. Do not share the runtime directory or CA private key.",
	"控制台访问令牌": "Console access token", "进入控制台 →": "Open console →", "仅用于本人设备、本人账号或已获授权的本地流量。": "Only for your own device, account, or authorized local traffic.",
	"访问令牌不正确。": "Incorrect access token.", "概览": "Overview", "消息": "Messages", "设置与证书": "Settings and certificates", "退出": "Log out",
	"你的本地消息档案。": "Your local message archive.", "只保存实体与关系。没有原始流量库，不采集客户端认证帧，不主动拉取历史消息。": "Only entities and relationships are saved. No raw traffic archive, client authentication frames, or active history fetching.",
	"常驻连接": "Running endpoints", "HTTP 代理": "HTTP proxy", "控制台": "Console", "运行时目录": "Runtime directory", "资源文件": "Resource files", "开启": "On", "关闭": "Off",
	"仅被动缓存已经过代理的安全图像类型": "Only safe image types observed through the proxy are cached", "关闭网页不会停止代理。应用不自动修改系统代理或根证书信任。非目标域名只进行 CONNECT 隧道转发。": "Closing this page does not stop the proxy. The app does not change system proxy or root trust. Other domains are only tunneled through CONNECT.",
	"每页 50 条，按消息 ID 倒序。只显示本机实际观察到的数据；未知关系会在后续事件中补全。": "50 per page, newest message ID first. Only locally observed data is shown; later events may fill in missing relationships.",
	"内容": "Content", "搜索已收集消息": "Search collected messages", "服务器 ID": "Server ID", "频道 ID": "Channel ID", "筛选": "Filter", "重置": "Reset",
	"修改采用 YAML 节点级补丁与原子替换。并发更改会被拒绝，不会静默覆盖。": "Changes use YAML node patches and atomic replacement. Concurrent edits are rejected.",
	"资源收集": "Resource collection", "允许保存 CDN 图像资源": "Save CDN images", "关闭时不拦截新的 CDN 连接，也不展示缓存。不会主动下载；旧文件不会自动删除。拒绝 SVG、HTML 与任意第三方域名。": "When off, new CDN connections are not intercepted and cached assets are hidden. Nothing is downloaded proactively; old files are retained. SVG, HTML, and third-party hosts are rejected.",
	"保存设置": "Save settings", "当前 CA": "Current CA", "证书": "Certificate", "私钥": "Private key", "SHA-256 指纹": "SHA-256 fingerprint", "有效期至": "Expires",
	"只在内存中签发站点叶证书。更换 CA 后，请手动调整信任并重新连接客户端。": "Site certificates are issued in memory. After changing the CA, adjust trust manually and reconnect the client.",
	"验证并选择已签发 CA": "Validate and select an issued CA", "证书路径": "Certificate path", "私钥路径": "Private key path", "校验并选择": "Validate and select",
	"签发新的根 CA": "Issue a new root CA", "调用已配置的系统 OpenSSL；没有工具链即报错。必须指定运行目录内的两个不同文件路径，绝不覆盖现有文件，也不会自动安装信任。": "Uses the configured system OpenSSL. Provide two distinct paths within the runtime directory. Existing files are never overwritten, and trust is never installed automatically.",
	"输出证书路径": "Output certificate path", "输出私钥路径": "Output private key path", "签发 CA": "Issue CA",
	"用户": "Users", "服务器": "Servers", "频道": "Channels", "采集健康状态": "Capture health", "WS 连接": "WS connections", "HTTP 已解析": "HTTP parsed", "Gateway 已解析": "Gateway parsed", "提交批次": "Committed batches", "缓存资源": "Cached assets",
	"队列丢弃": "Queue drops", "HTTP 跳过": "HTTP skipped", "WS 采集失步": "WS capture loss", "不支持的 WS 编码": "Unsupported WS encoding", "解析错误": "Decode errors", "存储错误": "Storage errors", "资源配额跳过": "Asset quota skips",
	"计数自本次启动开始。队列满或解码失败不会阻塞消息转发；非零错误意味着档案可能不完整。无 JavaScript 时可手动刷新。": "Counts start at launch. Queue or decode failures do not block forwarding; nonzero errors may mean an incomplete archive. Refresh manually without JavaScript.",
	"条消息": "messages", "刷新": "Refresh", "还没有匹配的消息": "No matching messages", "确认 CA 已信任、客户端已显式使用代理，然后在 Discord 中打开频道。代理不会补抓未经过本机的历史流量。": "Trust the CA, configure the client to use the proxy, then open a Discord channel. Historical traffic not observed locally is not fetched.",
	"已观察到删除事件 · 本地档案保留最后已知内容": "Deletion observed · last known content retained locally", "资源未缓存": "Asset not cached", "已编辑": "Edited", "查看更早的消息 →": "Older messages →",
	"本地授权观察 · HTTP + Gateway → SQLite · 原型，不保证无遗漏": "Authorized local observation · HTTP + Gateway → SQLite · prototype; capture may be incomplete",
	"数据库暂时不可读；请检查状态计数与磁盘空间。":                       "Database temporarily unavailable; check status counts and disk space.", "设置已保存。资源拦截策略对新连接生效；既有 CDN 连接需要重连。": "Settings saved. New connections use the updated asset policy; existing CDN connections must reconnect.",
	"CA 已签发但尚未选择。请核验指纹、显式选择，并手动安装公钥证书信任。": "CA issued but not selected. Verify its fingerprint, select it explicitly, and trust its public certificate manually.",
	"证书已校验并选择。新 TLS 握手使用新 CA；已有连接不被中断。":   "Certificate validated and selected. New TLS handshakes use the new CA; existing connections continue.",
	"未知频道": "Unknown channel", "私信 / 未知服务器": "DM / unknown server", "未知用户": "Unknown user",
	"所有服务器": "All servers", "私信与未知": "DMs and unknown", "尚未观察到服务器": "No servers observed", "尚未观察到频道": "No channels observed", "未分类频道": "Uncategorized channels", "已删除": "Deleted", "不可用": "Unavailable", "选择服务器或频道浏览本地档案": "Select a server or channel to browse the local archive",
	"全部频道": "All channels",
	"语言":   "Language", "中文": "中文", "English": "English", "分类": "Category", "文字频道": "Text channel", "其他频道": "Other channel",
}
