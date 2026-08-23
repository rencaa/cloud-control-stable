/*
 * 云控任务脚本：直接进入指定抖音直播间
 *
 * 在云控「任务参数」中填写：
 * {
 *   "live_room_id": "1234567890123456789"
 * }
 *
 * live_room_id 必须使用字符串，避免超长直播间 ID 被 JSON 数字精度截断。
 * 也可以填写包含 room_id 的抖音链接。
 */

var roomId = params && params.live_room_id;
if (roomId === undefined || roomId === null || String(roomId).trim() === "") {
	throw new Error("请填写任务参数 live_room_id");
}

var raw = String(roomId).trim();
var id = raw;
var queryMatch = raw.match(/(?:[?&]|^)room_id=([0-9]+)/i);
if (!queryMatch) {
	queryMatch = raw.match(/(?:[?&]|^)roomId=([0-9]+)/i);
}
if (queryMatch) {
	id = queryMatch[1];
} else {
	var pathMatch = raw.match(/(?:live\.douyin\.com\/|\/live\/|\/room\/)([0-9]+)/i);
	if (pathMatch) {
		id = pathMatch[1];
	}
}
var uri = "";
if (/^[0-9]+$/.test(id)) {
	uri = "snssdk1128://live?room_id=" + id;
} else {
	var urlMatch = raw.match(/https?:\/\/\S+/i);
	if (urlMatch) {
		uri = urlMatch[0].replace(/[)\]}，。]+$/g, "");
	} else {
		throw new Error("直播间ID或分享链接格式不正确");
	}
}
if (typeof report === "function") {
	report("========== 云控直接跳转抖音直播间 ==========");
	report("跳转意图: " + uri);
}

utils.openActivity({ uri: uri });
if (typeof report === "function") {
	report("抖音直播间跳转意图已发送");
}
sleep(3500);
return "已发起直播间跳转: " + id;
