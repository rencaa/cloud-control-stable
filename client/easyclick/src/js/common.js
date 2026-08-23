/*================================================================
 * 通用工具模块 - 配置、OCR、图色、手势、HID、通知等
 * 被 main.js 引用
 *================================================================*/

/*================================================================
 * 全局配置管理
 * 
 * 所有可配置的参数都集中在这里管理，方便调整和维护
 * 基准分辨率：1440 x 3200（可根据实际开发设备调整）
 *================================================================*/





const BASE_W = 1440;  // 基准屏幕宽度
const BASE_H = 3200;  // 基准屏幕高度

// 远程配置服务器地址（与云控同一服务器）
const REMOTE_CONFIG_URL = "https://sk.netmo99.cn/?json=1";

// 全局配置对象（从UI配置界面读取）
let CONFIG = loadConfig();

/**
 * 加载配置（从共享存储读取）
 */
function loadConfig() {
    try {
        let storage = storages.create("auto_config_store");
        let configStr = storage.getString("auto_config", "") + "";
        if (configStr && configStr !== "null" && configStr !== "undefined") {
            let cfg = JSON.parse(configStr);
            toast("加载配置: 刷直播间=" + cfg.enableLiveRoom + " 刷关键词=" + cfg.enableKeyword);
            return cfg;
        }
    } catch (e) {
        toast("加载配置失败: " + e);
    }
    toast("使用默认配置");
    return {
        mode: "all",            // "all" | "recommend" | "live" | "keyword" — 云端可指定单一模式
        enableFollow: true,     // 视频页是否点关注
        followProb: 30,         // 视频页点关注概率（%）
        enableRecommend: true,
        swipeIntervalMin: 15,   // 秒
        swipeIntervalMax: 30,   // 秒
        recEnableLike: true,
        recLikeProb: 60,
        recEnableComment: true,
        recCommentProb: 40,

        enableKeyword: true,
        kwStayMin: 8,           // 分钟
        kwStayMax: 15,          // 分钟
        kwEnableLike: true,
        kwLikeProb: 60,
        kwEnableComment: true,
        kwCommentProb: 40,
        kwDailyLimit: 5,

        enableLiveRoom: true,
        liveStayMin: 10,        // 分钟
        liveStayMax: 15,        // 分钟
        liveEnableLike: true,
        liveLikeProb: 60,
        liveEnableComment: true,
        liveCommentProb: 40,
        liveEnableFollow: true,
        liveFollowProb: 60,
        liveDailyLimit: 5,

        keywords: ["动漫", "海蓝之谜", "sk神仙水", "香奈儿", "黄金", "资生堂", "雅思兰黛", "传奇"],
        runTimeMin: 0
    };
}

/**
 * 从远程 PHP 服务器加载云端配置，覆盖本地默认值
 * 
 * GET REMOTE_CONFIG_URL → 解析JSON → 合并到 CONFIG
 * 服务器不可用或字段缺失时保持本地值不变
 */
function loadCloudConfig() {
    report("正在从远程服务器加载配置...");
    report("服务器: " + REMOTE_CONFIG_URL);
    
    let response;
    try {
        response = http.httpGetDefault(REMOTE_CONFIG_URL, 10 * 1000, null);
    } catch (e) {
        report("远程配置请求异常: " + e);
        return;
    }
    
    if (!response) {
        report("远程配置服务器无响应，使用本地配置");
        return;
    }
    
    try {
        let cloudCfg = JSON.parse(response);
        let updatedCount = 0;
        for (let key in cloudCfg) {
            CONFIG[key] = cloudCfg[key];
            updatedCount++;
        }
        report("远程配置加载成功，更新了 " + updatedCount + " 个配置项");
    } catch (e) {
        report("远程配置JSON解析失败: " + e);
    }
}

/*================================================================
 * 运行模式抽象层 - USB HID / 蓝牙 HID / OTG HID / 代理 / 无障碍
 * 
 * 从 storage 读取 UI 设置的模式：
 *   "USB HID"  → 使用 USB HID 主控（hidEvent, 网络/USB模式）
 *   "蓝牙 HID" → 使用蓝牙 HID 设备（bleEvent）
 *   "OTG HID"  → 使用 OTG HID 串口设备（otgEvent）
 *   "代理模式"  → 使用 EasyClick 代理服务控制
 *   "无障碍"   → 使用系统无障碍服务控制
 *================================================================*/

let RUN_MODE = "agent";

/**
 * 判断当前是否为 HID 类模式（USB HID / 蓝牙 HID / OTG HID）
 * @returns {boolean}
 */
function isHidMode() {
    return RUN_MODE === "hid_usb" || RUN_MODE === "hid_ble" || RUN_MODE === "hid_otg";
}

var gHidSettingsOpenGuard = false;

function resetHidSettingsOpenGuard() {
    gHidSettingsOpenGuard = false;
}

/**
 * HID 模式停止后统一打开 EasyClick 设置页，避免停在抖音前台导致下次 HID 失效。
 * 一次停止流程只打开一次；下一次重新启动业务时解除去重标记。
 */
function openHidSettingsAfterStop(reason) {
    if (!isHidMode() || gHidSettingsOpenGuard) {
        return gHidSettingsOpenGuard;
    }
    gHidSettingsOpenGuard = true;
    var label = reason || "HID本地脚本停止";
    report(label + "，正在打开EasyClick系统设置");
    try {
        if (typeof cloudLog === "function") {
            cloudLog(label + "，已打开EasyClick系统设置", "info");
        }
    } catch (ignoreStopCloudLog) {}

    var opened = false;
    try {
        if (typeof openECSystemSetting === "function") {
            opened = openECSystemSetting() !== false;
        }
    } catch (settingError) {
        report("打开EasyClick系统设置异常: " + settingError);
    }
    if (!opened) {
        try {
            if (typeof openECloudSetting === "function") {
                opened = openECloudSetting() !== false;
            }
        } catch (cloudSettingError) {
            report("打开EasyClick云控设置异常: " + cloudSettingError);
        }
    }
    if (!opened) {
        gHidSettingsOpenGuard = false;
        report("EasyClick设置页打开失败，请手动进入系统设置");
    }
    return opened;
}

/**
 * 初始化运行模式
 * @returns {boolean} true=初始化成功, false=初始化失败
 */
function initMode() {
    let storage = storages.create("auto_config_store");
    let modeStr = storage.getString("run_mode", "代理模式") + "";
    report("initMode读取: [" + modeStr + "]");

    if (modeStr.includes("USB HID") || modeStr.includes("HID模式")) {
        RUN_MODE = "hid_usb";
    } else if (modeStr.includes("蓝牙 HID") || modeStr.includes("BLE")) {
        RUN_MODE = "hid_ble";
    } else if (modeStr.includes("OTG HID") || modeStr.includes("OTG")) {
        RUN_MODE = "hid_otg";
    } else if (modeStr.includes("无障碍")) {
        RUN_MODE = "a11y";
    } else {
        RUN_MODE = "agent";
    }
    report("运行模式: " + RUN_MODE);

    if (RUN_MODE === "hid_usb") {
        // USB HID：直接初始化，使用 HID 主控现有配置，不调用 setHidCenter。
        report("正在初始化USB HID设备...");
        sleep(3000);
        let init = hidEvent.initUsbDevice();
        if (init === null) {
            report("USB HID设备初始化成功");
            return true;
        } else {
            report("USB HID设备初始化失败: " + init);
            return false;
        }
    } else if (RUN_MODE === "hid_ble") {
        // 蓝牙 HID：直连蓝牙设备
        report("正在连接蓝牙HID设备...");
        if (bleEvent.isConnected()) {
            report("蓝牙HID已连接");
            return true;
        }
        bleEvent.stopConnect();
        let cr = bleEvent.startConnect("", false, 15000);
        if (cr === null) {
            report("蓝牙HID连接成功");
            return true;
        } else {
            report("蓝牙HID连接失败: " + cr);
            return false;
        }
    } else if (RUN_MODE === "hid_otg") {
        // OTG HID：初始化串口并连接第一个设备
        report("正在初始化OTG HID串口...");
        let initErr = otgEvent.init();
        if (initErr === null) {
            sleep(2000);
            let connErr = otgEvent.connectFirst();
            if (connErr === null) {
                report("OTG HID连接成功");
                return true;
            } else {
                report("OTG HID连接失败: " + connErr);
                return false;
            }
        } else {
            report("OTG HID初始化失败: " + initErr);
            return false;
        }
    } else {
        // 代理 / 无障碍
        report("正在启动" + (RUN_MODE === "a11y" ? "无障碍" : "代理") + "服务...");
        startEnv();
        sleep(2000);
        if (RUN_MODE === "a11y") {
            if (!isAccMode() && !isServiceOk()) {
                report("无障碍服务启动失败！");
                return false;
            }
        } else {
            if (!isAgentMode()) {
                report("代理服务启动失败！请确认设备已通过电脑激活代理");
                return false;
            }
        }
        report((RUN_MODE === "a11y" ? "无障碍" : "代理") + "服务启动完成");
        return true;
    }
}

/* --- 模式无关包装函数 --- */

function doHome() {
    if (!isHidMode()) {
        return home();
    }
    return runHidActionWithRecovery("HID主页键", function () {
        if (RUN_MODE === "hid_usb") return hidEvent.home();
        if (RUN_MODE === "hid_ble") return bleEvent.home();
        return otgEvent.home();
    });
}

function doBack() {
    if (!isHidMode()) {
        return back();
    }
    return runHidActionWithRecovery("HID返回键", function () {
        if (RUN_MODE === "hid_usb") return hidEvent.back();
        if (RUN_MODE === "hid_ble") return bleEvent.back();
        return otgEvent.back();
    });
}

function checkHidClickResult(action, result) {
    if (!isHidMode() || result === null) {
        return result;
    }
    var detail;
    if (result === undefined) {
        detail = "HID接口未返回结果（成功必须为null）";
    } else if (result === "") {
        detail = "HID接口返回空错误消息（成功必须为null）";
    } else {
        detail = String(result);
    }
    var message = action + "失败: " + detail;
    report(message);
    throw new Error(message);
}

function hidResultDetail(result) {
    if (result === undefined) {
        return "HID接口未返回结果（成功必须为null）";
    }
    if (result === "") {
        return "HID接口返回空错误消息（成功必须为null）";
    }
    return String(result);
}

/**
 * 判断 EasyClick 自激活接口的返回值。
 * activeSelf 与 HID 点击接口不是同一套返回约定：点击成功必须为 null，
 * activeSelf 可能返回成功文字或 code=0 的 JSON，也兼容 null。
 */
function isHidActivationSuccess(result) {
    if (result === null || result === true) {
        return true;
    }
    if (result === false || result === undefined) {
        return false;
    }
    if (typeof result === "number") {
        return result === 0;
    }
    var text = String(result);
    var trimmed = text;
    try {
        trimmed = text.trim();
    } catch (ignoreTrim) {}
    if (!trimmed) {
        return false;
    }
    try {
        var parsed = JSON.parse(trimmed);
        if (parsed && (parsed.code === 0 || parsed.success === true || parsed.status === 0)) {
            return true;
        }
    } catch (ignoreJson) {}
    if (trimmed === "null" || trimmed === "成功" || trimmed === "激活成功") {
        return true;
    }
    return trimmed.indexOf("成功") >= 0 &&
        trimmed.indexOf("失败") < 0 &&
        trimmed.indexOf("错误") < 0;
}

/**
 * 重新激活/初始化当前 HID 模式。
 * 该函数只操作 HID 与 EasyClick 激活状态，不触碰 MQTT/WebSocket。
 */
function activateAndReconnectHid() {
    var activeOk = false;
    try {
        if (typeof activeSelf === "function") {
            var activeResult = activeSelf(0, 10000);
            activeOk = isHidActivationSuccess(activeResult);
            report("EasyClick自激活结果: " + hidResultDetail(activeResult));
        } else {
            report("EasyClick自激活接口不可用，继续尝试重连HID");
        }
    } catch (e) {
        report("EasyClick自激活异常: " + e);
    }

    var modeOk = false;
    try {
        if (RUN_MODE === "hid_usb") {
            // USB HID：直接重新初始化，使用 HID 主控现有配置。
            sleep(1500);
            var usbResult = hidEvent.initUsbDevice();
            modeOk = (usbResult === null);
            report("USB HID重连结果: " + hidResultDetail(usbResult));
        } else if (RUN_MODE === "hid_ble") {
            try { bleEvent.stopConnect(); } catch (ignoreBleStop) {}
            var bleResult = bleEvent.startConnect("", false, 15000);
            modeOk = (bleResult === null);
            report("蓝牙HID重连结果: " + hidResultDetail(bleResult));
        } else if (RUN_MODE === "hid_otg") {
            var otgInitResult = otgEvent.init();
            if (otgInitResult === null) {
                sleep(1000);
                var otgConnectResult = otgEvent.connectFirst();
                modeOk = (otgConnectResult === null);
                report("OTG HID重连结果: " + hidResultDetail(otgConnectResult));
            } else {
                report("OTG HID初始化结果: " + hidResultDetail(otgInitResult));
            }
        }
    } catch (e2) {
        report("HID重连异常: " + e2);
    }

    // 有些设备只需要HID重连，有些设备需要EasyClick自激活；最终仍由点击重试确认。
    return activeOk || modeOk;
}

function stopHidAutomationAndOpenSettings(action, result) {
    if (typeof gHidActivationBlocked !== "undefined" && gHidActivationBlocked) {
        return;
    }
    if (typeof gHidActivationBlocked !== "undefined") {
        gHidActivationBlocked = true;
    }
    if (typeof gHidActivationFailureCount !== "undefined") {
        gHidActivationFailureCount = 3;
    }
    var message = action + "连续3次激活/重试失败，已停止本地脚本";
    report(message + "，正在打开应用设置，云控连接保持");
    try {
        if (typeof cloudLog === "function") {
            cloudLog(message + "；云控连接保持", "error");
        }
    } catch (ignoreCloudLog) {}

    // 先结束云端任务状态，避免业务线程随后被取消时留下旧 task_id。
    var interruptedTaskId = null;
    try {
        if (typeof gCurrentTaskId !== "undefined" && gCurrentTaskId) {
            interruptedTaskId = gCurrentTaskId;
            gCurrentTaskId = null;
        }
    } catch (taskStateError) {}
    try {
        if (interruptedTaskId && typeof sendTaskStatus === "function") {
            sendTaskStatus(interruptedTaskId, 3, message);
        }
    } catch (taskReportError) {}

    // 从独立线程停止业务，避免业务线程在这里取消自己，导致设置页面来不及打开。
    // 只停止业务线程，不调用 cleanup()，否则会把 MQTT/WebSocket 一起关闭。
    var finishStop = function () {
        try {
            if (typeof stopLocalBusiness === "function") {
                stopLocalBusiness();
            }
        } catch (stopError) {
            report("停止本地脚本异常: " + stopError);
        }
        try {
            if (typeof sendScriptState === "function") {
                sendScriptState(true);
            }
        } catch (stateError) {}

        openHidSettingsAfterStop(action);
    };
    try {
        if (typeof thread !== "undefined" && typeof thread.execAsync === "function") {
            thread.execAsync(function () {
                sleep(100);
                finishStop();
            });
        } else {
            finishStop();
        }
    } catch (deferError) {
        report("延迟停止本地脚本失败: " + deferError);
        finishStop();
    }
}

function runHidActionWithRecovery(action, invoke) {
    if (typeof gHidActivationBlocked !== "undefined" && gHidActivationBlocked) {
        throw new Error("HID激活失败，当前本地脚本已停止，请先处理应用设置");
    }
    var result;
    try {
        result = invoke();
    } catch (e) {
        result = "HID接口异常: " + e;
    }
    if (result === null) {
        return null;
    }

    var lastResult = result;
    for (var attempt = 1; attempt <= 3; attempt++) {
        report(action + "返回非null: " + hidResultDetail(lastResult) +
            "，第" + attempt + "次激活");
        var activated = activateAndReconnectHid();
        if (!activated) {
            report(action + "第" + attempt + "次激活失败");
            continue;
        }
        try {
            lastResult = invoke();
        } catch (retryError) {
            lastResult = "HID接口异常: " + retryError;
        }
        if (lastResult === null) {
            if (typeof gHidActivationFailureCount !== "undefined") {
                gHidActivationFailureCount = 0;
            }
            report(action + "激活后重试成功");
            return null;
        }
        report(action + "第" + attempt + "次激活后重试仍失败: " +
            hidResultDetail(lastResult));
    }

    stopHidAutomationAndOpenSettings(action, lastResult);
    var finalMessage = action + "失败: " + hidResultDetail(lastResult) +
        "；连续3次激活失败，本地脚本已停止，云控连接保持";
    throw new Error(finalMessage);
}

function doClick(x, y) {
    if (!isHidMode()) {
        return clickPoint(x, y);
    }
    return runHidActionWithRecovery("HID点击(" + x + "," + y + ")", function () {
        if (RUN_MODE === "hid_usb") {
            return hidEvent.clickPoint(x, y);
        } else if (RUN_MODE === "hid_ble") {
            return bleEvent.clickPoint(x, y);
        }
        return otgEvent.clickPoint(x, y);
    });
}

function doDoubleClick(x, y) {
    if (!isHidMode()) {
        // 代理模式没有 doubleClickPoint，用两次 clickPoint 模拟
        doClick(x, y);
        sleep(60);
        return doClick(x, y);
    }
    return runHidActionWithRecovery("HID双击(" + x + "," + y + ")", function () {
        if (RUN_MODE === "hid_usb") {
            return hidEvent.doubleClickPoint(x, y);
        } else if (RUN_MODE === "hid_ble") {
            return bleEvent.doubleClick(x, y);
        }
        return otgEvent.doubleClickPoint(x, y);
    });
}

function doSwipe(x1, y1, x2, y2, duration) {
    if (!isHidMode()) {
        return swipeToPoint(x1, y1, x2, y2, duration);
    }
    return runHidActionWithRecovery("HID滑动", function () {
        if (RUN_MODE === "hid_usb") return hidEvent.swipe(x1, y1, x2, y2, duration);
        if (RUN_MODE === "hid_ble") return bleEvent.swipe(x1, y1, x2, y2, duration);
        return otgEvent.swipe(x1, y1, x2, y2, duration);
    });
}

/**
 * 多指触摸（自动转换 HID 格式 → Agent 格式）
 * HID格式: [{action:0/2/1, x, y, pointer:1, delay}, ...]
 * 代理模式: 逐点使用 touchDown/Move/Up + sleep(delay) 保持贝塞尔时序
 */
function doMultiTouch(hidTouchArray, timeout) {
    if (isHidMode()) {
        return runHidActionWithRecovery("HID多指触摸", function () {
            if (RUN_MODE === "hid_usb") return hidEvent.multiTouch(hidTouchArray, timeout);
            if (RUN_MODE === "hid_ble") return bleEvent.multiTouch(hidTouchArray, timeout);
            return otgEvent.multiTouch(hidTouchArray, timeout);
        });
    } else {
        // 代理模式：按 action + delay 逐点执行，保留贝塞尔曲线时序
        for (let i = 0; i < hidTouchArray.length; i++) {
            let pt = hidTouchArray[i];
            if (pt.delay && pt.delay > 0) {
                sleep(pt.delay);
            }
            if (pt.action === 0) {
                touchDown(pt.x, pt.y);
            } else if (pt.action === 2) {
                touchMove(pt.x, pt.y);
            } else if (pt.action === 1) {
                touchUp(pt.x, pt.y);
            }
        }
        return null; // null=成功，与 hidEvent 保持一致
    }
}

/**
 * 长按（模式无关）
 */
function doLongPress(x, y, duration) {
    duration = duration || 800;
    if (isHidMode()) {
        return runHidActionWithRecovery("HID长按(" + x + "," + y + ")", function () {
            if (RUN_MODE === "hid_usb") return hidEvent.press(x, y, duration);
            if (RUN_MODE === "hid_ble") return bleEvent.press(x, y, duration);
            return otgEvent.press(x, y, duration);
        });
    } else {
        // 代理/无障碍：用 touchDown + sleep + touchUp 模拟
        touchDown(x, y);
        sleep(duration);
        touchUp(x, y);
        return null;
    }
}

/**
 * 输入文字（模式无关）
 * HID 模式：通过剪贴板粘贴
 * 代理/无障碍：IME 直接输入
 * @returns {boolean} true=成功
 */
function doInputText(text) {
    if (isHidMode()) {
        // HID 模式：设置剪贴板，由调用方触发粘贴
        try {
            importClass(android.content.ClipboardManager);
            importClass(android.content.ClipData);
            importClass(android.content.Context);
            var cm = context.getSystemService(Context.CLIPBOARD_SERVICE);
            var clip = ClipData.newPlainText("ec", text);
            cm.setPrimaryClip(clip);
            report("剪贴板已设置: " + text);
            return true;
        } catch (e) {
            report("剪贴板失败: " + e);
            return false;
        }
    } else {
        // 代理/无障碍：IME 输入
        try {
            imeInputText(null, text);
            return true;
        } catch (e) {
            report("IME输入失败: " + e);
            return false;
        }
    }
}

/*-------------------- 时间配置（毫秒） --------------------*/
const TIME = {
    APP_START: 3000,
    get VIDEO_VIEW_MIN() { return (CONFIG.swipeIntervalMin || 15) * 1000; },
    get VIDEO_VIEW_MAX() { return (CONFIG.swipeIntervalMax || 30) * 1000; },
    COMMENT_VIEW_MIN: 3000,
    COMMENT_VIEW_MAX: 5000,
    LIKE_DELAY: 300,
    BACK_DELAY: 1000,
    INPUT_DELAY_MIN: 1300,
    INPUT_DELAY_MAX: 2400,
    RANDOM_DELAY_MIN: 1000,
    RANDOM_DELAY_MAX: 3000
};

/*-------------------- 直播间互动配置 --------------------*/
const LIVE_CONFIG = {
    get MODE() { return CONFIG.mode || "all"; },   // 云端运行模式: "all" | "recommend" | "live" | "keyword"
    get ENABLE_FOLLOW_VIDEO() { return CONFIG.enableFollow !== undefined ? CONFIG.enableFollow : true; },
    get FOLLOW_PROB() { return CONFIG.followProb || 30; },
    get PROBABILITY_FOLLOW() { return CONFIG.liveFollowProb || 60; },
    get PROBABILITY_LIKE() { return CONFIG.recLikeProb || 60; },
    get PROBABILITY_COMMENT() { return CONFIG.recCommentProb || 40; },
    get STAY_TIME_MIN() { return (CONFIG.liveStayMin || 10) * 60; },
    get STAY_TIME_MAX() { return (CONFIG.liveStayMax || 15) * 60; },
    get ENABLE_LIKE() { return CONFIG.recEnableLike !== undefined ? CONFIG.recEnableLike : true; },
    get ENABLE_COMMENT() { return CONFIG.recEnableComment !== undefined ? CONFIG.recEnableComment : true; },
    LIKE_VIDEO_CHANCE: 16,
    LIVE_ROOM_CHANCE: 30,
    LIKE_INTERVAL_MIN: 30,
    LIKE_INTERVAL_MAX: 120,
    COMMENT_INTERVAL_MIN: 60,
    COMMENT_INTERVAL_MAX: 180,
    WATCH_PAUSE_MIN: 5,
    WATCH_PAUSE_MAX: 15,
    get RUN_TIME_MIN() { return CONFIG.runTimeMin || 0; },
    
    // 关键词视频配置
    get KEYWORD_VIDEO_STAY_MIN() { return (CONFIG.kwStayMin || 8) * 60; },
    get KEYWORD_VIDEO_STAY_MAX() { return (CONFIG.kwStayMax || 15) * 60; },
    get KEYWORDS() { return CONFIG.keywords || ["动漫", "海蓝之谜", "sk神仙水", "香奈儿", "黄金", "资生堂", "雅思兰黛", "传奇"]; },
    get ENABLE_RECOMMEND() { return CONFIG.enableRecommend !== undefined ? CONFIG.enableRecommend : true; },
    get ENABLE_LIVE_ROOM() { return CONFIG.enableLiveRoom !== undefined ? CONFIG.enableLiveRoom : true; },
    get LIVE_DAILY_LIMIT() { return CONFIG.liveDailyLimit || 5; },
    get ENABLE_FOLLOW() { return CONFIG.liveEnableFollow !== undefined ? CONFIG.liveEnableFollow : true; },
    get ENABLE_LIVE_LIKE() { return CONFIG.liveEnableLike !== undefined ? CONFIG.liveEnableLike : true; },
    get ENABLE_LIVE_COMMENT() { return CONFIG.liveEnableComment !== undefined ? CONFIG.liveEnableComment : true; },
    get LIVE_LIKE_PROB() { return CONFIG.liveLikeProb || 60; },
    get LIVE_COMMENT_PROB() { return CONFIG.liveCommentProb || 40; },
    get ENABLE_KEYWORD() { return CONFIG.enableKeyword !== undefined ? CONFIG.enableKeyword : true; },
    get KW_DAILY_LIMIT() { return CONFIG.kwDailyLimit || 5; },
    get ENABLE_KW_LIKE() { return CONFIG.kwEnableLike !== undefined ? CONFIG.kwEnableLike : true; },
    get ENABLE_KW_COMMENT() { return CONFIG.kwEnableComment !== undefined ? CONFIG.kwEnableComment : true; },
    get KW_LIKE_PROB() { return CONFIG.kwLikeProb || 60; },
    get KW_COMMENT_PROB() { return CONFIG.kwCommentProb || 40; },

    // 发评论概率（比看评论低，更真实）
    get PROBABILITY_POST_COMMENT() { return 12; },       // 推荐页发评论概率 12%
    get KW_POST_COMMENT_PROB() { return 15; },            // 关键词视频发评论概率 15%
    get LIVE_POST_COMMENT_PROB() { return CONFIG.liveCommentProb ? Math.round(CONFIG.liveCommentProb / 2) : 20; },  // 直播间发评论概率
    
    // 视频标签页区域（搜索结果页）- 用于查找"视频"或"直播"标签
    // 基准分辨率1440x3200，由1080x2400等比放大
    VIDEO_TAB_X1: 27,             // 视频标签页X1 (20 * 1.333)
    VIDEO_TAB_Y1: 301,            // 视频标签页Y1 (226 * 1.333)
    VIDEO_TAB_X2: 1435,           // 视频标签页X2 (1076 * 1.333)
    VIDEO_TAB_Y2: 448,            // 视频标签页Y2 (336 * 1.333)
    
    // 视频点击区域（搜索结果页视频列表）- 点击进入视频刷视频模式
    // 基准分辨率1440x3200，由1080x2400等比放大 (44,504,512,1168)
    VIDEO_CLICK_X1: 59,           // 视频点击区域X1 (44 * 1.333)
    VIDEO_CLICK_Y1: 672,          // 视频点击区域Y1 (504 * 1.333)
    VIDEO_CLICK_X2: 683,          // 视频点击区域X2 (512 * 1.333)
    VIDEO_CLICK_Y2: 1557,         // 视频点击区域Y2 (1168 * 1.333)

    // 关注按钮区域（屏幕下方左侧）
    FOLLOW_X1: 13,                // 关注按钮左上角X
    FOLLOW_Y1: 27,              // 关注按钮左上角Y
    FOLLOW_X2: 1403,               // 关注按钮右下角X
    FOLLOW_Y2: 285,              // 关注按钮右下角Y
    
    // 直播间检测关键词
    LIVE_KEYWORDS: ["关注", "礼物", "评论", "分享", "直播间"]  // 直播间特征关键词
};

/*-------------------- OCR识别配置 --------------------*/
const OCR_CONFIG = {
    TIMEOUT: 20 * 1000,          // OCR识别超时时间（毫秒）
    SIMILARITY: 0.8              // 相似度阈值（0-1）
};

/*-------------------- 找图配置 --------------------*/
const IMAGE_CONFIG = {
    WEAK_THRESHOLD: 0.7,         // 弱阈值（模板匹配时）
    THRESHOLD: 0.7,              // 相似度阈值
    THRESHOLD_HIGH: 0.8,         // 高相似度阈值
    LIMIT: 1,                    // 返回结果数量限制
    SIMILARITY_FULL: 0.8         // 全屏找图相似度
};

/*-------------------- 滑动配置 --------------------*/
const SWIPE_CONFIG = {
    START_X_MIN: 210,            // 滑动起点X最小值 (用户提供)
    START_X_MAX: 1344,           // 滑动起点X最大值 (用户提供)
    START_Y_MIN: 2400,           // 滑动起点Y最小值（屏幕下方区域，优化：增加起点Y）
    START_Y_MAX: 2900,           // 滑动起点Y最大值（屏幕下方区域，优化：增加起点Y）
    END_X_MIN: 210,              // 滑动终点X最小值 (用户提供)
    END_X_MAX: 1344,             // 滑动终点X最大值 (用户提供)
    END_Y_MIN: 200,              // 滑动终点Y最小值（屏幕上方区域，优化：减小终点Y）
    END_Y_MAX: 600,              // 滑动终点Y最大值（屏幕上方区域，优化：减小终点Y）
    DURATION_MIN: 400,           // 滑动持续时间最小值（优化：增加持续时间）
    DURATION_MAX: 700,           // 滑动持续时间最大值（优化：增加持续时间）
    MAX_RETRY: 2                 // 滑动失败最大重试次数
};

/*-------------------- 评论滑动配置 --------------------*/
const COMMENT_SWIPE_CONFIG = {
    START_X_MIN: 115,            // 评论滑动起点X最小值 (用户提供)
    START_X_MAX: 1439,           // 评论滑动起点X最大值 (用户提供)
    START_Y_MIN: 2800,           // 评论滑动起点Y最小值 (屏幕下方，优化：增加起点Y)
    START_Y_MAX: 3000,           // 评论滑动起点Y最大值 (屏幕下方，优化：增加起点Y)
    END_X_MIN: 115,              // 评论滑动终点X最小值 (用户提供)
    END_X_MAX: 1439,             // 评论滑动终点X最大值 (用户提供)
    END_Y_MIN: 1500,             // 评论滑动终点Y最小值 (屏幕上方，优化：大幅减小终点Y)
    END_Y_MAX: 2000,             // 评论滑动终点Y最大值 (屏幕上方，优化：大幅减小终点Y)
    DURATION_MIN: 400,           // 滑动持续时间最小值（优化：增加持续时间）
    DURATION_MAX: 700            // 滑动持续时间最大值（优化：增加持续时间）
};

/*-------------------- 标签切换滑动配置 --------------------*/
// 用于在找不到直播或视频标签时，从右往左滑动切换标签
// 基于1440x3200基准分辨率坐标：50,310,1094,436
const TAB_SWIPE_CONFIG = {
    SWIPE_X1: 50,                // 滑动区域左上角X
    SWIPE_Y1: 310,               // 滑动区域左上角Y
    SWIPE_X2: 1094,              // 滑动区域右下角X
    SWIPE_Y2: 436,               // 滑动区域右下角Y
    DURATION: 300                // 滑动持续时间（毫秒）
};

/*-------------------- 视频点击区域配置 --------------------*/
// 找到视频或直播标签后，点击标签后点击这两个区域之一
// 基于1440x3200基准分辨率坐标
const VIDEO_CLICK_CONFIG = {
    // 区域1：左侧视频区域
    AREA1_X1: 116,
    AREA1_Y1: 838,
    AREA1_X2: 568,
    AREA1_Y2: 1390,
    // 区域2：右侧视频区域
    AREA2_X1: 806,
    AREA2_Y1: 980,
    AREA2_X2: 1134,
    AREA2_Y2: 1472
};

/*-------------------- 视频页点关注配置 --------------------*/
// 用于在视频页检测"关注"按钮并点击
// 基于1440x3200基准分辨率坐标：1107,652,1429,2499
const FOLLOW_CONFIG = {
    IMAGE_NAME: "点关注.png",       // 模板图片文件名
    CHECK_X1: 1107,                 // 检测区域左上角X
    CHECK_Y1: 652,                  // 检测区域左上角Y
    CHECK_X2: 1429,                 // 检测区域右下角X
    CHECK_Y2: 2499,                 // 检测区域右下角Y
    THRESHOLD: 0.7                  // 匹配阈值
};

/*-------------------- 直播间图片识别配置 --------------------*/
// 用于检测是否在直播间的图片识别区域
// 基于1440x3200基准分辨率坐标：1281,166,1423,299
const LIVE_IMAGE_CONFIG = {
    IMAGE_NAME: "1.png",         // 图片文件名
    CHECK_X1: 1281,              // 检测区域左上角X
    CHECK_Y1: 166,               // 检测区域左上角Y
    CHECK_X2: 1423,              // 检测区域右下角X
    CHECK_Y2: 299,               // 检测区域右下角Y
    THRESHOLD: 0.8               // 匹配阈值（0-1之间）
};

/**
 * 从右往左滑动切换标签
 * 
 * 在找不到直播或视频标签时使用
 * 滑动区域：50,310,1094,436（基于1440x3200基准分辨率）
 */
function swipeRightToLeft() {
    toast("从右往左滑动切换标签...");
    
    let { scaleX, scaleY } = getScreenScale();
    
    // 计算滑动起点（右侧）和终点（左侧）
    let startX = Math.round(TAB_SWIPE_CONFIG.SWIPE_X2 * scaleX);
    let startY = Math.round((TAB_SWIPE_CONFIG.SWIPE_Y1 + TAB_SWIPE_CONFIG.SWIPE_Y2) / 2 * scaleY);
    let endX = Math.round(TAB_SWIPE_CONFIG.SWIPE_X1 * scaleX);
    let endY = startY;
    
    toast("滑动: (" + startX + "," + startY + ") -> (" + endX + "," + endY + ")");
    
    // 执行滑动
    let result = doSwipe(startX, startY, endX, endY, TAB_SWIPE_CONFIG.DURATION);
    
    sleep(1000);  // 等待滑动完成
    
    return result;
}

/**
 * 点击视频区域（随机选择两个区域之一）
 * 
 * 找到视频或直播标签后，点击标签后点击这两个区域之一：
 * 区域1：116,838,568,1390（左侧）
 * 区域2：806,980,1134,1472（右侧）
 */
function clickVideoArea() {
    let { scaleX, scaleY } = getScreenScale();
    
    // 随机选择区域1或区域2
    let useArea1 = random(1, 100) <= 50;  // 50%概率选择区域1
    
    let clickX, clickY;
    
    if (useArea1) {
        // 区域1：左侧视频区域
        clickX = Math.round(random(VIDEO_CLICK_CONFIG.AREA1_X1, VIDEO_CLICK_CONFIG.AREA1_X2) * scaleX);
        clickY = Math.round(random(VIDEO_CLICK_CONFIG.AREA1_Y1, VIDEO_CLICK_CONFIG.AREA1_Y2) * scaleY);
        toast("点击左侧视频区域: (" + clickX + "," + clickY + ")");
    } else {
        // 区域2：右侧视频区域
        clickX = Math.round(random(VIDEO_CLICK_CONFIG.AREA2_X1, VIDEO_CLICK_CONFIG.AREA2_X2) * scaleX);
        clickY = Math.round(random(VIDEO_CLICK_CONFIG.AREA2_Y1, VIDEO_CLICK_CONFIG.AREA2_Y2) * scaleY);
        toast("点击右侧视频区域: (" + clickX + "," + clickY + ")");
    }
    
    doClick(clickX, clickY);
    
    return { x: clickX, y: clickY };
}

/**
 * 执行评论区滑动操作（单指贝塞尔曲线滑动）
 * 
 * 在评论区内从下往上滑动，浏览更多评论
 * 使用贝塞尔曲线生成自然的曲线轨迹
 */
function performCommentSectionSwipe() {
    let retryCount = 0;
    let maxRetry = 2;
    
    while (retryCount <= maxRetry) {
        let startX = random(COMMENT_SWIPE_CONFIG.START_X_MIN, COMMENT_SWIPE_CONFIG.START_X_MAX);
        let startY = random(COMMENT_SWIPE_CONFIG.START_Y_MIN, COMMENT_SWIPE_CONFIG.START_Y_MAX);
        
        let endX = startX + random(-10, 10); // 减少横向偏移
        endX = Math.max(COMMENT_SWIPE_CONFIG.END_X_MIN, Math.min(COMMENT_SWIPE_CONFIG.END_X_MAX, endX));
        
        let endY = random(COMMENT_SWIPE_CONFIG.END_Y_MIN, COMMENT_SWIPE_CONFIG.END_Y_MAX);
        
        let { scaleX, scaleY } = getScreenScale();
        let realStartX = Math.round(startX * scaleX);
        let realStartY = Math.round(startY * scaleY);
        let realEndX = Math.round(endX * scaleX);
        let realEndY = Math.round(endY * scaleY);
        
        // 计算滑动距离
        let distance = Math.abs(realStartY - realEndY);
        
        toast("评论滑动参数: 基准(" + startX + "," + startY + ") -> (" + endX + "," + endY + ")");
        toast("评论滑动参数: 实际(" + realStartX + "," + realStartY + ") -> (" + realEndX + "," + realEndY + ")");
        toast("评论滑动参数: 缩放比例(" + scaleX + "," + scaleY + ")，距离: " + distance + "px");
        
        let swipeDuration = random(COMMENT_SWIPE_CONFIG.DURATION_MIN, COMMENT_SWIPE_CONFIG.DURATION_MAX);
        // 根据距离动态调整持续时间
        swipeDuration = Math.round(swipeDuration * (distance / 1200));
        swipeDuration = Math.max(300, Math.min(800, swipeDuration));
        toast("评论滑动持续时间: " + swipeDuration + "ms");

        try {
            toast("执行评论区滑动（第" + (retryCount + 1) + "次尝试）");
            
            // 优先使用贝塞尔曲线滑动
            let result = rSwipe.rndSwipe(realStartX, realStartY, realEndX, realEndY);
            
            if (result == null) {
                toast("✓ 评论区贝塞尔曲线滑动成功");
                sleep(300);
                return true;
            } else {
                toast("评论区贝塞尔曲线滑动失败: " + result + "，尝试普通滑动");
                // 备用方案：使用普通滑动API
                doSwipe(realStartX, realStartY, realEndX, realEndY, swipeDuration);
                toast("✓ 评论区普通滑动执行成功");
                sleep(300);
                return true;
            }
        } catch (e) {
            toast("评论区滑动异常: " + e);
            if (retryCount < maxRetry) {
                toast("准备重试...");
                sleep(300);
            }
        }
        
        retryCount++;
    }
    
    toast("评论区滑动失败，已达到最大重试次数");
    return false;
}

/*-------------------- 点赞配置 --------------------*/
const LIKE_CONFIG = {
    X_MIN: 400,                 // 点赞区域X最小值
    X_MAX: 600,                 // 点赞区域X最大值
    Y_MIN: 600,                 // 点赞区域Y最小值
    Y_MAX: 990,                 // 点赞区域Y最大值
    CLICK_COUNT: 3              // 点赞点击次数
};

/*-------------------- 评论区配置 --------------------*/
const COMMENT_CONFIG = {
    // 搜索区域（屏幕上方）- 用于查找"直播"、"关注"等
    SEARCH_X1: 0,               // 搜索区域左上角X
    SEARCH_Y1: 10,              // 搜索区域左上角Y（屏幕顶部开始，根据用户提供值调整）
    SEARCH_X2: 1440,            // 搜索区域右下角X（根据用户提供值调整，适配BASE_W）
    SEARCH_Y2: 653,             // 搜索区域右下角Y（屏幕上方区域，根据用户提供值调整）
    
    // 评论页数配置
    PAGES_MIN: 1,               // 浏览评论页数最小值
    PAGES_MAX: 6,              // 浏览评论页数最大值
    
    // "说点什么"输入框区域（屏幕下方左侧）— 评论区页面
    INPUT_X1: 72,               // 输入框左上角X
    INPUT_Y1: 2998,             // 输入框左上角Y
    INPUT_X2: 568,              // 输入框右下角X
    INPUT_Y2: 3088,             // 输入框右下角Y
    
    // "发送"按钮区域（屏幕下方右侧）— 评论区页面
    SEND_X1: 886,               // 发送按钮左上角X
    SEND_Y1: 2738,              // 发送按钮左上角Y
    SEND_X2: 1436,              // 发送按钮右下角X
    SEND_Y2: 3078,              // 发送按钮右下角Y

    // 推荐页底部评论入口栏（"说点什么..."），点击打开评论区
    BOTTOM_BAR_X1: 72,
    BOTTOM_BAR_Y1: 2998,
    BOTTOM_BAR_X2: 568,
    BOTTOM_BAR_Y2: 3088,
    SEND_Y2: 2898,              // 发送按钮右下角Y
    
    // 首页检测区域（屏幕下方导航栏）
    HOME_CHECK_X1: 13,
    HOME_CHECK_Y1: 2951,
    HOME_CHECK_X2: 306,
    HOME_CHECK_Y2: 3162,
    
    // 评论区检测区域（用于OCR检测是否在评论区界面）
    COMMENT_CHECK_X1: 10,        // 评论检测左上角X
    COMMENT_CHECK_Y1: 1212,      // 评论检测左上角Y
    COMMENT_CHECK_X2: 1038,      // 评论检测右下角X
    COMMENT_CHECK_Y2: 1388,     // 评论检测右下角Y

    // 评论区点击区域（用于点击进入评论区）
    // 右侧栏第3个图标，位于点赞下方
    // 基准分辨率1440x3200
    COMMENT_CLICK_X1: 1150,      // 评论图标左侧X
    COMMENT_CLICK_Y1: 1750,      // 评论图标上边Y
    COMMENT_CLICK_X2: 1400,      // 评论图标右侧X
    COMMENT_CLICK_Y2: 2200,      // 评论图标下边Y
};

/**
 * 报告消息（同时输出到设备悬浮窗和中控日志）
 * 
 * toast() 仅显示在设备悬浮窗，不上报中控
 * logi() 会上报中控（默认同步），但不在悬浮窗显示
 * 本函数同时调用两者，兼顾本地调试和远程监控
 * 
 * @param {string} msg - 消息内容
 */
function report(msg) {
    toast(msg);
    logi(msg);
}

/**
 * 主入口函数
 * 
 * 负责初始化各模块、申请权限、启动主流程
 */
function SwipeRnd() {
    this.step = 0.08;
}

const rSwipe = new SwipeRnd();

/**
 * 贝塞尔曲线计算
 */
SwipeRnd.prototype._bezier_curves = function (cp, t) {
    let cx = 3.0 * (cp[1].x - cp[0].x),
        bx = 3.0 * (cp[2].x - cp[1].x) - cx,
        ax = cp[3].x - cp[0].x - cx - bx,
        cy = 3.0 * (cp[1].y - cp[0].y),
        by = 3.0 * (cp[2].y - cp[1].y) - cy,
        ay = cp[3].y - cp[0].y - cy - by,
        tSquared = t * t,
        tCubed = tSquared * t;
    return {
        "x": (ax * tCubed) + (bx * tSquared) + (cx * t) + cp[0].x,
        "y": (ay * tCubed) + (by * tSquared) + (cy * t) + cp[0].y
    };
};

/**
 * 生成单个手指的随机曲线滑动轨迹
 * @description 生成从(qx,qy)到(zx,zy)的贝塞尔曲线轨迹点
 * @param qx 起点X
 * @param qy 起点Y
 * @param zx 终点X
 * @param zy 终点Y
 * @return {Array} 轨迹点数组 [[x1,y1], [x2,y2], ...]
 */
SwipeRnd.prototype._rndSwipe = function (qx, qy, zx, zy) {
    let xxyy = [],
        xxy = [],
        point = [],
        // 计算滑动距离
        dy = qy - zy,
        // 控制点1：在起点下方
        cp1y = qy + Math.abs(dy) * 0.3,
        // 控制点2：在终点上方
        cp2y = zy - Math.abs(dy) * 0.3,
        // 计算X方向的中间值，用于控制点
        midX = (qx + zx) / 2,
        dx = [{
            "x": qx,  // 起点
            "y": qy
        }, {
            "x": midX + random(-40, 40),  // 控制点1：X在中间附近小幅偏移
            "y": cp1y  // 控制点1：向下偏移30%距离
        }, {
            "x": midX + random(-40, 40),  // 控制点2：X在中间附近小幅偏移
            "y": cp2y  // 控制点2：向上偏移30%距离
        }, {
            "x": zx,  // 终点
            "y": zy
        }];
    for (let i = 0; i < dx.length; i++) {
        point.push(dx[i]);
    }
    for (let i = 0; i < 1; i += this.step) {
        let calculated = this._bezier_curves(point, i);
        // 微颤：模拟真人手指微小抖动，±2px 随机偏移
        let wobbleX = random(-2, 2);
        let wobbleY = random(-2, 2);
        xxyy = [Math.round(calculated.x + wobbleX), Math.round(calculated.y + wobbleY)];
        xxy.push(xxyy);
    }
    return xxy;
};

/**
 * 执行单指贝塞尔曲线滑动
 */
SwipeRnd.prototype.rndSwipe = function (startX, startY, endX, endY, timeStart, timeEnd, timeOut, step) {
    timeStart = timeStart || 50;
    timeEnd = timeEnd || timeStart + 50;
    timeOut = timeOut || 2 * 1000;
    this.step = step || this.step;
    return this._gesture(this._rndSwipe(startX, startY, endX, endY), random(timeStart, timeEnd), timeOut);
};

/**
 * 真人变速滑动（Ease-In-Out + 微颤）
 * 
 * 模拟真人刷视频时的手指动作：
 * - 按下后先慢速启动（手指从静止到移动有个过程）
 * - 中间快速滑动
 * - 接近目标时减速停下
 * - 全程带有微小颤动
 * 
 * @param {number} startX - 起点X
 * @param {number} startY - 起点Y
 * @param {number} endX - 终点X
 * @param {number} endY - 终点Y
 * @param {number} [baseDelay=40] - 基础延迟（ms），越大越慢
 * @param {number} [timeout=3000] - 超时（ms）
 * @returns {string|null} null=成功
 */
SwipeRnd.prototype.rndSwipeHuman = function (startX, startY, endX, endY, baseDelay, timeout) {
    baseDelay = baseDelay || 40;
    timeout = timeout || 3000;
    return this._gestureEase(this._rndSwipe(startX, startY, endX, endY), baseDelay, timeout);
};

/**
 * 过冲回弹滑动（Overshoot）
 * 
 * 模拟真人用力过猛：滑过头 → 弹回来一点 → 抬起
 * 分三段轨迹：
 * 1. 正常滑动到目标位置
 * 2. 惯性继续滑过 5-10% 距离
 * 3. 回弹到目标位置
 * 
 * @param {number} startX - 起点X
 * @param {number} startY - 起点Y
 * @param {number} endX - 终点X
 * @param {number} endY - 终点Y
 * @param {number} [timeout=3000] - 超时（ms）
 * @returns {string|null} null=成功
 */
SwipeRnd.prototype.rndSwipeOvershoot = function (startX, startY, endX, endY, timeout) {
    timeout = timeout || 3000;
    
    // 计算过冲距离（滑过头 5-10%）
    let dy = endY - startY;
    let overshootRatio = 0.05 + Math.random() * 0.05;  // 5%-10%
    let overshootY = Math.round(endY + dy * overshootRatio);
    let overshootX = Math.round(endX + (endX - startX) * overshootRatio);
    
    // 分段生成轨迹
    // 第一段：起点 → 过冲点（正常速度）
    let traj1 = this._rndSwipe(startX, startY, overshootX, overshootY);
    // 第二段：过冲点 → 目标点（回弹，比第一段略短）
    let traj2 = this._rndSwipe(overshootX, overshootY, endX, endY);
    
    // 合并两段轨迹（去掉第二段的起点避免重复）
    let combinedTraj = traj1.concat(traj2.slice(1));
    
    // 用 ease-out 方式执行：回弹阶段更慢
    return this._gestureEase(combinedTraj, 35, timeout);
};

/**
 * 执行双指贝塞尔曲线滑动
 */
SwipeRnd.prototype.rndSwipeTwo = function (startX, startY, endX, endY, startX1, startY1, endX2, endY2, timeStart, timeEnd, timeOut, step) {
    timeStart = timeStart || 50;
    timeEnd = timeEnd || timeStart + 50;
    timeOut = timeOut || 2 * 1000;
    this.step = step || this.step;
    return this._gestureTwo([
        this._rndSwipe(startX, startY, endX, endY),
        this._rndSwipe(startX1, startY1, endX2, endY2)
    ], random(timeStart, timeEnd), timeOut);
};

/**
 * 执行多指贝塞尔曲线滑动（支持1-5个手指）
 */
SwipeRnd.prototype.rndSwipeMulti = function (points, timeStart, timeEnd, timeOut, step) {
    timeStart = timeStart || 50;
    timeEnd = timeEnd || timeStart + 50;
    timeOut = timeOut || 2 * 1000;
    this.step = step || this.step;
    
    let swipeList = [];
    for (let i = 0; i < points.length; i++) {
        let p = points[i];
        swipeList.push(this._rndSwipe(p.startX, p.startY, p.endX, p.endY));
    }
    
    return this._gestureMulti(swipeList, random(timeStart, timeEnd), timeOut);
};

/**
 * 单指手势执行
 */
SwipeRnd.prototype._gesture = function (swipeList, time, time1) {
    // 调试：输出轨迹前5个点和后5个点
    toast("轨迹起点: (" + swipeList[0][0] + "," + swipeList[0][1] + ")");
    toast("轨迹终点: (" + swipeList[swipeList.length-1][0] + "," + swipeList[swipeList.length-1][1] + ")");
    
    let touch1 = [{"action": 0, "x": swipeList[0][0], "y": swipeList[0][1], "pointer": 1, "delay": time}];
    for (let i = 1; i < swipeList.length - 1; i++) {
        touch1.push({"action": 2, "x": swipeList[i][0], "y": swipeList[i][1], "pointer": 1, "delay": time});
    }
    touch1.push({
        "action": 1,
        "x": swipeList[swipeList.length - 1][0],
        "y": swipeList[swipeList.length - 1][1],
        "pointer": 1,
        "delay": time
    });
    return doMultiTouch(touch1, time1);
};

/**
 * 单指变速手势执行（Ease-In-Out）
 * 
 * 模拟真人滑动的速度曲线：起始慢 → 中间快 → 结束慢
 * 每个 touch 点的 delay 不再均匀，而是按正弦曲线分布
 * 
 * @param {Array} swipeList - 轨迹点数组 [[x,y], ...]
 * @param {number} baseDelay - 基础延迟（ms），会按曲线缩放
 * @param {number} timeout - multiTouch 超时时间（ms）
 * @private
 */
SwipeRnd.prototype._gestureEase = function (swipeList, baseDelay, timeout) {
    let total = swipeList.length;
    let touch1 = [];
    
    // 第一个点：按下，起始慢（1.5x delay）
    touch1.push({
        "action": 0,
        "x": swipeList[0][0],
        "y": swipeList[0][1],
        "pointer": 1,
        "delay": Math.round(baseDelay * 1.5)
    });
    
    // 中间移动点：ease-in-out 变速
    for (let i = 1; i < total - 1; i++) {
        let progress = i / (total - 1);  // 0~1
        // 正弦 ease-in-out：两端慢(1.4x)，中间快(0.6x)
        let factor = 1.0 + 0.4 * Math.cos(progress * Math.PI * 2);
        let delay = Math.round(baseDelay * factor);
        // EC USB模式要求 delay >= 40ms
        delay = Math.max(40, delay);
        
        touch1.push({
            "action": 2,
            "x": swipeList[i][0],
            "y": swipeList[i][1],
            "pointer": 1,
            "delay": delay
        });
    }
    
    // 最后一个点：抬起，结束慢（1.5x delay）
    touch1.push({
        "action": 1,
        "x": swipeList[total - 1][0],
        "y": swipeList[total - 1][1],
        "pointer": 1,
        "delay": Math.round(baseDelay * 1.5)
    });
    
    return doMultiTouch(touch1, timeout);
};

/**
 * 双指手势执行
 */
SwipeRnd.prototype._gestureTwo = function (swipeList, time, time1) {
    let swipe = swipeList[0],
        swipe1 = swipeList[1],
        touch1 = [{"action": 0, "x": swipe[0][0], "y": swipe[0][1], "pointer": 1, "delay": time}],
        touch2 = [{"action": 0, "x": swipe1[0][0], "y": swipe1[0][1], "pointer": 2, "delay": time}];

    for (let i = 1; i < swipe.length - 1; i++) {
        touch1.push({"action": 2, "x": swipe[i][0], "y": swipe[i][1], "pointer": 1, "delay": time});
        touch2.push({"action": 2, "x": swipe1[i][0], "y": swipe1[i][1], "pointer": 2, "delay": time});
    }
    touch1.push({
        "action": 1,
        "x": swipe[swipe.length - 1][0],
        "y": swipe[swipe.length - 1][1],
        "pointer": 1,
        "delay": time
    });
    touch2.push({
        "action": 1,
        "x": swipe1[swipe1.length - 1][0],
        "y": swipe1[swipe1.length - 1][1],
        "pointer": 2,
        "delay": time
    });
    
    // 合并两个手指的触摸数据
    let combined = [];
    for (let i = 0; i < touch1.length; i++) {
        combined.push(touch1[i]);
        combined.push(touch2[i]);
    }
    return doMultiTouch(combined, time1);
};

/**
 * 多指手势执行（支持1-5个手指）
 */
SwipeRnd.prototype._gestureMulti = function (swipeList, time, time1) {
    let fingers = swipeList.length;
    let touchData = [];
    
    // 生成所有手指的触摸数据
    for (let finger = 0; finger < fingers; finger++) {
        let swipe = swipeList[finger];
        let pointer = finger + 1;
        
        // 按下
        touchData.push({"action": 0, "x": swipe[0][0], "y": swipe[0][1], "pointer": pointer, "delay": time});
    }
    
    // 移动
    for (let i = 1; i < swipeList[0].length - 1; i++) {
        for (let finger = 0; finger < fingers; finger++) {
            let swipe = swipeList[finger];
            let pointer = finger + 1;
            touchData.push({"action": 2, "x": swipe[i][0], "y": swipe[i][1], "pointer": pointer, "delay": time});
        }
    }
    
    // 抬起
    for (let finger = 0; finger < fingers; finger++) {
        let swipe = swipeList[finger];
        let pointer = finger + 1;
        touchData.push({
            "action": 1,
            "x": swipe[swipe.length - 1][0],
            "y": swipe[swipe.length - 1][1],
            "pointer": pointer,
            "delay": time
        });
    }
    
    return doMultiTouch(touchData, time1);
};

/**
 * 执行滑动操作（单指贝塞尔曲线滑动）
 * 
 * 从屏幕下方向上滑动，浏览下一个视频
 * 使用贝塞尔曲线生成自然的曲线轨迹
 * 增加重试机制和滑动距离优化
 * 
 * @example
 * performSwipe();  // 向上滑动浏览下一个视频
 */
function performSwipe() {
    let retryCount = 0;
    let maxRetry = SWIPE_CONFIG.MAX_RETRY;
    
    while (retryCount <= maxRetry) {
        // 生成随机滑动参数（基于基准分辨率1440x3200）
        let startX = random(SWIPE_CONFIG.START_X_MIN, SWIPE_CONFIG.START_X_MAX);
        let startY = random(SWIPE_CONFIG.START_Y_MIN, SWIPE_CONFIG.START_Y_MAX);
        
        // 终点X尽量接近起点X，避免横向偏移过大
        let endX = startX + random(-10, 10); // 优化：减少横向偏移，使滑动更垂直
        endX = Math.max(SWIPE_CONFIG.END_X_MIN, Math.min(SWIPE_CONFIG.END_X_MAX, endX));
        
        let endY = random(SWIPE_CONFIG.END_Y_MIN, SWIPE_CONFIG.END_Y_MAX);
        
        // 获取屏幕缩放比例并转换坐标
        let { scaleX, scaleY } = getScreenScale();
        let realStartX = Math.round(startX * scaleX);
        let realStartY = Math.round(startY * scaleY);
        let realEndX = Math.round(endX * scaleX);
        let realEndY = Math.round(endY * scaleY);
        
        // 计算滑动距离
        let distance = Math.abs(realStartY - realEndY);
        
        toast("滑动参数: 基准(" + startX + "," + startY + ") -> (" + endX + "," + endY + ")");
        toast("滑动参数: 实际(" + realStartX + "," + realStartY + ") -> (" + realEndX + "," + realEndY + ")");
        toast("滑动参数: 缩放比例(" + scaleX + "," + scaleY + ")，距离: " + distance + "px");
        
        // 计算滑动持续时间（根据距离动态调整）
        let swipeDuration = random(SWIPE_CONFIG.DURATION_MIN, SWIPE_CONFIG.DURATION_MAX);
        // 距离越长，持续时间越长（保证滑动速度稳定）
        swipeDuration = Math.round(swipeDuration * (distance / 1500));
        swipeDuration = Math.max(300, Math.min(1000, swipeDuration)); // 限制在300-1000ms之间
        
        toast("滑动持续时间: " + swipeDuration + "ms");
        
        // 执行滑动操作
        try {
            toast("执行滑动（第" + (retryCount + 1) + "次尝试）");
            
            // 随机选择滑动风格（模拟真人行为多变）
            let styleRoll = random(1, 100);
            let result;
            
            if (styleRoll <= 55) {
                // 55% 贝塞尔曲线滑动（基础风格）
                toast("滑动风格: 贝塞尔曲线");
                result = rSwipe.rndSwipe(realStartX, realStartY, realEndX, realEndY);
            } else if (styleRoll <= 80) {
                // 25% 真人变速滑动（ease-in-out + 微颤）
                toast("滑动风格: 真人变速");
                result = rSwipe.rndSwipeHuman(realStartX, realStartY, realEndX, realEndY);
            } else if (styleRoll <= 93) {
                // 13% 过冲回弹滑动
                toast("滑动风格: 过冲回弹");
                result = rSwipe.rndSwipeOvershoot(realStartX, realStartY, realEndX, realEndY);
            } else {
                // 7% 快速连滑（两次快速滑动，模拟不感兴趣快速划过）
                toast("滑动风格: 快速连滑");
                result = rSwipe.rndSwipeHuman(realStartX, realStartY, realEndX, realEndY, 25);
                if (result == null) {
                    sleep(random(80, 150));
                    // 第二滑：续滑一小段
                    let midX = realEndX + random(-5, 5);
                    let midY = realEndY - random(50, 100);
                    result = rSwipe.rndSwipeHuman(realEndX, realEndY, midX, midY, 20);
                }
            }
            
            if (result == null) {
                toast("✓ 滑动成功");
                sleep(500);
                return true;
            } else {
                toast("滑动失败: " + result + "，尝试普通滑动");
                doSwipe(realStartX, realStartY, realEndX, realEndY, swipeDuration);
                toast("✓ 普通滑动执行成功");
                sleep(500);
                return true;
            }
        } catch (e) {
            toast("滑动异常: " + e);
            if (retryCount < maxRetry) {
                toast("准备重试...");
                sleep(300);
            }
        }
        
        retryCount++;
    }
    
    toast("滑动失败，已达到最大重试次数");
    return false;
}
function performLike() {
    toast("执行点赞操作");
    
    // 获取屏幕缩放比例
    let { scaleX, scaleY } = getScreenScale();
    
    // 在点赞区域双击（带屏幕缩放）
    let x = random(LIKE_CONFIG.X_MIN, LIKE_CONFIG.X_MAX);
    let y = random(LIKE_CONFIG.Y_MIN, LIKE_CONFIG.Y_MAX);
    let realX = Math.round(x * scaleX);
    let realY = Math.round(y * scaleY);
    
    doDoubleClick(realX, realY);
    toast("点赞完成: 基准(" + x + "," + y + ") -> 实际(" + realX + "," + realY + ")");
}
function cv3pd(targetText) {
    // 1. 从res目录读取模板图片
    let sms = readResAutoImage(targetText);
    if (!sms) {
        toast("读取模板图片失败: " + targetText);
        return false;
    }
    
    // 2. 截取全屏
    let aimage = image.captureFullScreen();
    if (typeof aimage === "string") aimage = new AutoImage(aimage);
    if (!aimage) {
        toast("截图失败");
        image.recycle(sms);
        return false;
    }
    
    // 3. 执行模板匹配
    // 在全屏范围内查找，相似度阈值0.8
    let points = image.findImage2(
        aimage,                              // 大图（屏幕截图）
        sms,                                 // 小图（模板）
        0, 0,                                // 起始坐标（全屏）
        device.getScreenWidth(),             // 结束X（屏幕宽度）
        device.getScreenHeight(),            // 结束Y（屏幕高度）
        IMAGE_CONFIG.SIMILARITY_FULL,        // 弱阈值
        IMAGE_CONFIG.SIMILARITY_FULL,        // 相似度阈值
        1,                                   // 最大结果数
        IMAGE_CONFIG.LIMIT                   // 限制
    );
    
    // 4. 释放截图资源
    image.recycle(aimage);
    
    // 5. 检查匹配结果
    if (points && points.length > 0) {
        let match = points[0];
        toast("模板匹配成功: " + targetText + "，相似度: " + match.similarity.toFixed(2));
        image.recycle(sms);
        return true;
    }
    
    toast("模板匹配失败: " + targetText);
    image.recycle(sms);
    return false;
}

/**
 * 全屏模板匹配（查找并显示结果）
 * 
 * 在全屏范围内查找指定模板图片，并输出匹配结果
 * 用于调试和测试
 * 
 * @param {string} targetText - 模板图片名称（从res目录读取）
 * @returns {boolean} true=找到, false=未找到
 * 
 * @example
 * cv3("搜索按钮");  // 会在日志中输出匹配结果
 */
function cv3(targetText) {
    // 1. 从res目录读取模板图片
    let sms = readResAutoImage(targetText);
    if (!sms) {
        toast("读取模板图片失败: " + targetText);
        return false;
    }
    
    // 2. 截取全屏
    let aimage = image.captureFullScreen();
    if (typeof aimage === "string") aimage = new AutoImage(aimage);
    if (!aimage) {
        toast("截图失败");
        image.recycle(sms);
        return false;
    }
    
    // 3. 执行模板匹配
    let points = image.findImage2(
        aimage,
        sms,
        0, 0,
        device.getScreenWidth(),
        device.getScreenHeight(),
        IMAGE_CONFIG.SIMILARITY_FULL,
        IMAGE_CONFIG.SIMILARITY_FULL,
        1,
        IMAGE_CONFIG.LIMIT
    );
    
    // 4. 输出详细日志
    toast("模板匹配结果: " + JSON.stringify(points));
    image.recycle(aimage);
    
    // 5. 解析并输出每个匹配结果
    if (points && points.length > 0) {
        for (let i = 0; i < points.length; i++) {
            let match = points[i];
            let centerX = parseInt((match.left + match.right) / 2);
            let centerY = parseInt((match.top + match.bottom) / 2);
            
            toast("匹配#" + (i + 1) + ": 相似度=" + match.similarity.toFixed(2) 
                + ", 坐标=(" + centerX + "," + centerY + ")");
            
            // Toast显示坐标
            toast("找到: (" + centerX + ", " + centerY + ")");
        }
        
        image.recycle(sms);
        return true;
    }
    
    toast("未找到模板: " + targetText);
    image.recycle(sms);
    return false;
}
/**
 * 区域模板匹配并点击（自适应缩放）
 * 
 * 在指定区域内查找模板图片，找到后点击匹配位置
 * 自动根据设备分辨率进行坐标缩放
 * 
 * @param {string} targetText - 模板图片名称（从res目录读取）
 * @param {number} x1 - 搜索区域左上角X（基准坐标）
 * @param {number} y1 - 搜索区域左上角Y（基准坐标）
 * @param {number} x2 - 搜索区域右下角X（基准坐标）
 * @param {number} y2 - 搜索区域右下角Y（基准坐标）
 * 
 * @example
 * // 在(0,0)到(500,500)的区域内查找"按钮"图片并点击
 * cv("按钮", 0, 0, 500, 500);
 */
function cv(targetText, x1, y1, x2, y2) {
    // 1. 获取当前设备的屏幕缩放比例
    // 这样可以用基准分辨率的坐标在不同设备上正常工作
    let { scaleX, scaleY } = getScreenScale();
    
    // 2. 将基准坐标转换为当前设备的实际坐标
    // 例如：基准1440x3200，设备1080x2400，scaleX=0.75, scaleY=0.75
    let realX1 = Math.round(x1 * scaleX);
    let realY1 = Math.round(y1 * scaleY);
    let realX2 = Math.round(x2 * scaleX);
    let realY2 = Math.round(y2 * scaleY);

    // 3. 从res目录读取模板图片
    let sms = readResAutoImage(targetText);
    if (!sms) {
        toast("读取模板失败: " + targetText);
        return;
    }
    
    // 4. 在指定区域内执行模板匹配
    // 使用归一化系数匹配法（method=5），适合大多数场景
    let points = image.findImageEx(
        sms,                    // 模板图片
        realX1, realY1,        // 搜索区域起点
        realX2, realY2,        // 搜索区域终点
        IMAGE_CONFIG.WEAK_THRESHOLD,  // 弱阈值
        IMAGE_CONFIG.THRESHOLD,       // 相似度阈值
        1,                    // 最大返回结果数
        5                     // TM_CCOEFF_NORMED 归一化系数匹配法
    );
    
    image.recycle(sms);  // 及时释放模板图片内存
    
    // 5. 处理匹配结果
    if (points && points.length > 0) {
        for (let i = 0; i < points.length; i++) {
            // 计算匹配区域的中心点坐标
            let centerX = parseInt((points[i].left + points[i].right) / 2);
            let centerY = parseInt((points[i].top + points[i].bottom) / 2);
            
            toast("找到模板 [" + targetText + "]，点击坐标: (" + centerX + ", " + centerY + ")");
            
            // 点击匹配位置（自动区分 HID/代理模式）
            doClick(centerX, centerY);
            sleep(30);  // 短暂延迟，避免点击过快
        }
    } else {
        toast("未找到模板: " + targetText);
    }
}

/**
 * 区域模板匹配检测（自适应缩放）
 * 
 * 在指定区域内查找模板图片，仅判断是否存在
 * 自动根据设备分辨率进行坐标缩放
 * 
 * @param {string} targetText - 模板图片名称（从res目录读取）
 * @param {number} x1 - 搜索区域左上角X（基准坐标）
 * @param {number} y1 - 搜索区域左上角Y（基准坐标）
 * @param {number} x2 - 搜索区域右下角X（基准坐标）
 * @param {number} y2 - 搜索区域右下角Y（基准坐标）
 * @returns {boolean} true=找到, false=未找到
 * 
 * @example
 * if (cvpd("确定", 0, 0, 500, 500)) {
 *     toast("找到确定按钮");
 * }
 */
function cvpd(targetText, x1, y1, x2, y2) {
    // 1. 获取屏幕缩放比例
    let { scaleX, scaleY } = getScreenScale();
    
    // 2. 坐标缩放
    let realX1 = Math.round(x1 * scaleX);
    let realY1 = Math.round(y1 * scaleY);
    let realX2 = Math.round(x2 * scaleX);
    let realY2 = Math.round(y2 * scaleY);

    // 3. 读取模板
    let sms = readResAutoImage(targetText);
    if (!sms) {
        toast("读取模板失败: " + targetText);
        return false;
    }
    
    // 4. 模板匹配（OpenCV可能未初始化，加保护）
    let points;
    try {
        points = image.findImageEx(
            sms, realX1, realY1, realX2, realY2,
            IMAGE_CONFIG.WEAK_THRESHOLD,
            IMAGE_CONFIG.THRESHOLD,
            1,
            5
        );
    } catch (e) {
        toast("模板匹配失败(OpenCV不可用): " + e);
        image.recycle(sms);
        return false;
    }
    
    image.recycle(sms);

    // 5. 判断结果
    if (points && points.length > 0) {
        let centerX = parseInt((points[0].left + points[0].right) / 2);
        if (centerX > 0) {
            toast("检测到模板: " + targetText);
            return true;
        }
    }
    
    toast("未检测到模板: " + targetText);
    return false;
}

/**
 * 区域模板匹配并点击指定位置（自适应缩放）
 * 
 * 在指定区域内查找模板图片，找到后点击另一个指定位置
 * 常用于：找到按钮A，点击按钮A旁边的按钮B
 * 
 * @param {string} targetText - 要查找的模板图片名称
 * @param {number} x1 - 搜索区域左上角X
 * @param {number} y1 - 搜索区域左上角Y
 * @param {number} x2 - 搜索区域右下角X
 * @param {number} y2 - 搜索区域右下角Y
 * @param {number} x3 - 点击位置X（找到模板后点击此处）
 * @param {number} y3 - 点击位置Y（找到模板后点击此处）
 * @returns {boolean} true=找到并点击, false=未找到
 */
function cvpddj(targetText, x1, y1, x2, y2, x3, y3) {
    // 1. 获取缩放比例
    let { scaleX, scaleY } = getScreenScale();
    
    // 2. 缩放所有坐标
    let realX1 = Math.round(x1 * scaleX);
    let realY1 = Math.round(y1 * scaleY);
    let realX2 = Math.round(x2 * scaleX);
    let realY2 = Math.round(y2 * scaleY);
    let realX3 = Math.round(x3 * scaleX);  // 点击位置也要缩放
    let realY3 = Math.round(y3 * scaleY);

    // 3. 读取模板
    let sms = readResAutoImage(targetText);
    if (!sms) {
        toast("读取模板失败: " + targetText);
        return false;
    }
    
    // 4. 模板匹配（使用稍高的相似度阈值）
    let points = image.findImageEx(
        sms, realX1, realY1, realX2, realY2,
        IMAGE_CONFIG.WEAK_THRESHOLD,
        IMAGE_CONFIG.THRESHOLD_HIGH,  // 高相似度阈值
        1,
        5
    );
    
    image.recycle(sms);
    
    // 5. 找到则点击指定位置
    if (points && points.length > 0) {
        let centerX = parseInt((points[0].left + points[0].right) / 2);
        let centerY = parseInt((points[0].top + points[0].bottom) / 2);
        
        if (centerX > 0) {
            toast("找到 [" + targetText + "]，点击位置: (" + realX3 + ", " + realY3 + ")");
            doClick(realX3, realY3);  // 自动区分 HID/代理模式
            return true;
        }
    }
    
    toast("未找到: " + targetText);
    return false;
}

/**
 * OCR识别并点击（基于百度飞浆OCR）
 * 
 * 对指定区域进行OCR文字识别，如果识别到目标文字则点击
 * 
 * @param {string} targetText - 要识别的目标文字
 * @param {number} x1 - 识别区域左上角X
 * @param {number} y1 - 识别区域左上角Y
 * @param {number} x2 - 识别区域右下角X
 * @param {number} y2 - 识别区域右下角Y
 * @param {number} x3 - 点击位置X
 * @param {number} y3 - 点击位置Y
 * @returns {boolean} true=识别成功并点击, false=识别失败或文字不匹配
 */
function ocrdj(targetText, x1, y1, x2, y2, x3, y3) {
    // 1. 缩放坐标
    let { scaleX, scaleY } = getScreenScale();
    let realX1 = Math.round(x1 * scaleX);
    let realY1 = Math.round(y1 * scaleY);
    let realX2 = Math.round(x2 * scaleX);
    let realY2 = Math.round(y2 * scaleY);
    let realX3 = Math.round(x3 * scaleX);
    let realY3 = Math.round(y3 * scaleY);

    // 2. 截取屏幕
    let screenImg = image.captureFullScreen();
    if (!screenImg) {
        toast("截图失败");
        return false;
    }
    
    // 3. 剪裁识别区域
    let clipImg = image.clip(screenImg, realX1, realY1, realX2, realY2);
    image.recycle(screenImg);
    
    if (!clipImg) {
        toast("剪裁失败");
        return false;
    }
    
    // 4. 图片转Base64并发送给OCR服务
    let imageBase64 = image.toBase64Format(clipImg);
    image.recycle(clipImg);
    
    // 5. 调用OCR API
    let ocrUrl = uiurl;  // 百度飞浆OCR地址
    let result = http.postJSON(ocrUrl, imageBase64, 100 * 1000, null);
    
    // 6. 判断识别结果
    if (result === targetText) {
        toast(targetText);
        toast("OCR识别成功: " + targetText + "，点击位置: (" + realX3 + ", " + realY3 + ")");
        doClick(realX3, realY3);
        sleep(1500);
        return true;
    }
    
    toast("OCR识别结果: " + result + "，期望: " + targetText);
    return false;
}

/**
 * OCR文字检测（基于百度飞浆OCR）
 * 
 * 对指定区域进行OCR文字识别，仅判断是否识别到目标文字
 * 
 * @param {string} targetText - 要识别的目标文字
 * @param {number} x1 - 识别区域左上角X
 * @param {number} y1 - 识别区域左上角Y
 * @param {number} x2 - 识别区域右下角X
 * @param {number} y2 - 识别区域右下角Y
 * @returns {boolean} true=识别到目标文字, false=未识别到
 */
function ocrpd(targetText, x1, y1, x2, y2) {
    // 1. 缩放坐标
    let { scaleX, scaleY } = getScreenScale();
    let realX1 = Math.round(x1 * scaleX);
    let realY1 = Math.round(y1 * scaleY);
    let realX2 = Math.round(x2 * scaleX);
    let realY2 = Math.round(y2 * scaleY);

    // 2. 截取并剪裁
    let screenImg = image.captureFullScreen();
    if (!screenImg) {
        toast("截图失败");
        return false;
    }
    
    let clipImg = image.clip(screenImg, realX1, realY1, realX2, realY2);
    image.recycle(screenImg);
    
    if (!clipImg) {
        toast("剪裁失败");
        return false;
    }
    
    // 3. OCR识别
    let imageBase64 = image.toBase64Format(clipImg);
    image.recycleAllImage();  // 释放所有图片资源
    
    let ocrUrl = uiurl;
    let result = http.postJSON(ocrUrl, imageBase64, 100 * 1000, null);
    
    // 4. 判断结果
    if (result === targetText) {
        toast("OCR检测成功: " + targetText);
        return true;
    }
    
    toast("OCR检测失败: " + result);
    return false;
}

/**
 * 发送微信通知
 * 
 * 通过ShowDoc服务发送推送通知
 * 
 * @param {string} 标题 - 通知标题
 * @param {string} 内容 - 通知内容
 */
function 微信通知(标题, 内容) {
    // ShowDoc推送API地址
    let url = "https://push.showdoc.com.cn/server/api/push/b5c9b48c290a9d3fb1397f8ecaf91b76165229724";
    
    // 构建请求参数
    let params = {
        "title": 标题,
        "content": 内容
    };
    
    // 发送POST请求
    let response = http.postJSON(url, params, 100 * 1000, null);
    toast("微信通知发送结果: " + response);
}

/**
 * 初始化HID设备
 * 
 * HID用于模拟物理按键和触摸操作
 * 支持USB设备和网络HID
 * 
 * @returns {boolean} true=初始化成功, false=初始化失败
 */
function initHid() {
    // 直接初始化 HID，使用 HID 主控现有配置。
    sleep(3000);  // 等待设备连接稳定
    
    // 初始化USB HID设备
    let init = hidEvent.initUsbDevice();
    toast("HID初始化结果: " + init);
    
    if (init === null) {
        toast("HID设备初始化成功");
        return true;
    } else {
        toast("HID设备初始化失败: " + init);
        return false;
    }
}

/**
 * 自动启动自动化服务
 * 
 * 尝试多次启动自动化服务，直到成功或达到最大重试次数
 * 
 * @param {number} time - 最大重试次数
 * @returns {boolean} true=服务启动成功, false=服务启动失败
 */
function autoServiceStart(time) {
    for (var i = 0; i < time; i++) {
        // 检查服务是否已启动
        if (isServiceOk()) {
            toast("自动化服务已在运行");
            return true;
        }
        
        // 尝试启动服务
        var started = startEnv();
        toast("第" + (i + 1) + "次启动服务结果: " + started);
        
        // 检查启动结果
        if (isServiceOk()) {
            toast("自动化服务启动成功");
            return true;
        }
        
        // 启动失败则等待后重试
        sleep(1000);
    }
    
    toast("自动化服务启动失败，已重试" + time + "次");
    return isServiceOk();
}

/*================================================================
 * 脚本退出处理函数
 *================================================================*/

/**
 * 脚本停止时：打开抖音应用信息页面 -> 结束运行 -> 返回桌面
 * 
 * 使用 Intent 方式直接打开抖音的应用详情页面，
 * 用户可以在该页面点击"结束运行"或"强制停止"
 * 
 * 执行步骤：
 * 1. 使用 Intent 打开抖音应用信息页面
 * 2. 等待页面加载
 * 3. 点击"结束运行"按钮
 * 4. 确认结束
 * 5. 返回桌面
 */
function stopDouyinAndReturnHome() {
    try {
        doHome();
        toast("已返回桌面");
    } catch (e) {
        try {
            home();  // 全局home兜底
            toast("已返回桌面(fallback)");
        } catch (e2) {
            loge("返回桌面失败: " + e2);
        }
    }
}

/**
 * 重启抖音应用
 * 
 * 当连续多次找不到首页时调用，强制重启抖音应用
 * 
 * @example
 * restartDouyin();  // 重启抖音
 */


/**
 * 返回抖音首页（使用DeepLink方式）
 * 在刷视频或直播间找不到标志时使用此函数返回抖音首页
 */
function goToDouyinHome() {
    toast("正在返回抖音首页...");
    var map = {
        "uri": "snssdk1128://feed?refer=web&needlaunchlog=1"
    };
    var result = utils.openActivity(map);
    sleep(3000);
    return result;
}

function restartDouyin() {
    toast("========== 开始重启抖音 ==========");
    utils.openAppByName("抖音");

}

let paddleOcrOnnx = null;
function initOcrpaddle() {
    // PaddleOCR版本配置
    // 支持的版本：
    // - "paddleOcrOnnxV4": 轻量级版本，速度快
    // - "paddleOcrOnnxV5": 完整版本，精度更高
    let paddleOnnxMap = {
        "type": "paddleOcrOnnxV4",  // OCR引擎类型
        "modelsDir": "",            // 模型目录，空字符串使用内置模型
        "numThread": 2,             // 线程数，建议2-4
        "padding": 60,              // 图片边距填充
        "maxSideLen": 960           // 图片最大边长，超过会缩放
    };
    
    // 创建OCR识别器实例
    toast("正在创建PaddleOCR识别器...");
    paddleOcrOnnx = ocr.newOcr();
    
    // 初始化OCR引擎
    toast("正在初始化OCR引擎...");
    if (!paddleOcrOnnx.initOcr(paddleOnnxMap)) {
        // 初始化失败，获取错误信息并退出
        toast("OCR初始化失败: " + paddleOcrOnnx.getErrorMsg());
        toast("OCR初始化失败");
        exit();  // 退出脚本
        return;  // 防止 exit() 异步执行
    }
    
    toast("PaddleOCR初始化成功");
}

/*================================================================
 * 坐标自适应核心模块
 * 
 * 功能：
 * - 自动适配不同分辨率的设备
 * - 提供坐标和区域的缩放转换
 * - 防止NaN错误导致脚本崩溃
 *================================================================*/

/**
 * 获取当前设备的屏幕缩放比例
 * 
 * 将基准分辨率（1440x3200）的坐标转换为当前设备实际坐标
 * 
 * @returns {object} 缩放比例对象
 *   - scaleX: X轴缩放比例
 *   - scaleY: Y轴缩放比例
 * 
 * @example
 * // 基准坐标 (100, 200) 在1080x2400设备上会转换为 (75, 150)
 * let { scaleX, scaleY } = getScreenScale();
 * let realX = Math.round(100 * scaleX);
 * let realY = Math.round(200 * scaleY);
 */
function getScreenScale() {
    // 获取当前设备的屏幕分辨率
    let curW = device.getScreenWidth();
    let curH = device.getScreenHeight();
    
    // 防止设备信息获取失败导致NaN错误
    if (!curW || !curH || curW <= 0 || curH <= 0) {
        toast("屏幕尺寸获取失败（宽:" + curW + "，高:" + curH + "），使用默认缩放比例1.0");
        return { scaleX: 1, scaleY: 1 };
    }
    
    // 计算缩放比例
    // 例如：基准1440x3200，设备1080x2400
    // scaleX = 1080/1440 = 0.75
    // scaleY = 2400/3200 = 0.75
    let scaleX = curW / BASE_W;
    let scaleY = curH / BASE_H;
    
    toast("屏幕缩放比例: X=" + scaleX.toFixed(3) + ", Y=" + scaleY.toFixed(3) 
         + " (设备:" + curW + "x" + curH + ", 基准:" + BASE_W + "x" + BASE_H + ")");
    
    return {
        scaleX: scaleX,
        scaleY: scaleY
    };
}

/**
 * 将单个基准坐标点转换为实际设备坐标
 * 
 * @param {number} baseX - 基准X坐标
 * @param {number} baseY - 基准Y坐标
 * @returns {object} 实际坐标 { x, y }
 * 
 * @example
 * let point = toRealPoint(100, 200);
 * // 返回 { x: 75, y: 150 } (假设缩放比例0.75)
 */
function toRealPoint(baseX, baseY) {
    // 获取当前缩放比例
    let { scaleX, scaleY } = getScreenScale();
    
    // 转换为实际坐标
    return {
        x: Math.round(baseX * scaleX),
        y: Math.round(baseY * scaleY)
    };
}

/**
 * 将基准区域转换为实际设备区域
 * 
 * 用于OCR和找图函数的参数转换
 * 
 * @param {number} left   - 基准区域左边X
 * @param {number} top    - 基准区域上边Y
 * @param {number} right  - 基准区域右边X
 * @param {number} bottom - 基准区域下边Y
 * @returns {object} 实际区域 { left, top, right, bottom }
 * 
 * @example
 * let rect = toRealRect(0, 0, 500, 500);
 * // 返回 { left: 0, top: 0, right: 375, bottom: 375 } (假设缩放比例0.75)
 */
function toRealRect(left, top, right, bottom) {
    // 获取当前缩放比例
    let { scaleX, scaleY } = getScreenScale();
    
    // 验证输入参数，防止NaN错误
    if (isNaN(left) || isNaN(top) || isNaN(right) || isNaN(bottom)) {
        toast("toRealRect: 输入参数包含NaN (left=" + left + ", top=" + top 
             + ", right=" + right + ", bottom=" + bottom + ")");
        // 返回全屏区域作为默认值
        return { 
            left: 0, 
            top: 0, 
            right: device.getScreenWidth(), 
            bottom: device.getScreenHeight() 
        };
    }
    
    // 执行坐标缩放
    let result = {
        left:   Math.round(left * scaleX),
        top:    Math.round(top * scaleY),
        right:  Math.round(right * scaleX),
        bottom: Math.round(bottom * scaleY)
    };
    
    // 验证输出结果
    if (isNaN(result.left) || isNaN(result.top) || isNaN(result.right) || isNaN(result.bottom)) {
        toast("toRealRect: 计算结果包含NaN");
        return { 
            left: 0, 
            top: 0, 
            right: device.getScreenWidth(), 
            bottom: device.getScreenHeight() 
        };
    }
    
    return result;
}




/*================================================================
 * OCR识别封装函数
 *================================================================*/

/**
 * OCR识别并双击（PaddleOCR版本）
 * 
 * 在指定区域内使用PaddleOCR进行文字识别
 * 找到目标文字后自动双击
 * 支持坐标自动缩放
 * 
 * @param {string} targetText - 要查找的目标文字（支持模糊匹配）
 * @param {number} [x1] - 搜索区域左边X（基准坐标，可选）
 * @param {number} [y1] - 搜索区域上边Y（基准坐标，可选）
 * @param {number} [x2] - 搜索区域右边X（基准坐标，可选）
 * @param {number} [y2] - 搜索区域下边Y（基准坐标，可选）
 * @returns {boolean} true=找到并点击, false=未找到
 * 
 * @example
 * // 全屏搜索
 * ocrFuncpddj("首页");
 * 
 * // 在指定区域搜索
 * ocrFuncpddj("直播", 0, 0, 500, 500);
 */
function ocrFuncpddj(targetText, x1, y1, x2, y2) {
    // 1. 开始性能计时
    console.time("ocr_pddj");
    
    // 2. 截取全屏（代理模式用Ex版本消除色差，HID模式用普通版本）
    let img;
    if (RUN_MODE === "agent") {
        img = image.captureFullScreenEx();
    }
    if (!img) {
        img = image.captureFullScreen();
    }
    if (!img) {
        toast("OCR截图失败");
        return false;
    }
    // EC 低版本 captureFullScreen 返回 UUID 字符串，OCR 需要 AutoImage
    if (typeof img === "string") img = new AutoImage(img);
    if (!img) {
        toast("OCR截图失败");
        return false;
    }
    
    // 3. 确定识别区域
    let clipImg;
    let offsetX = 0;  // 剪裁区域的偏移量（用于计算全局坐标）
    let offsetY = 0;
    
    // 判断是否传入了区域参数
    if (x1 !== undefined && y1 !== undefined && x2 !== undefined && y2 !== undefined) {
        // 有区域参数：在指定区域内识别
        // 将基准坐标转换为实际坐标
        let rect = toRealRect(x1, y1, x2, y2);
        
        // 记录偏移量（剪裁区域的左上角坐标）
        offsetX = rect.left;
        offsetY = rect.top;
        
        // 剪裁出识别区域
        clipImg = image.clip(img, rect.left, rect.top, rect.right, rect.bottom);
        image.recycle(img);  // 释放全屏截图
        
        if (!clipImg) {
            toast("OCR剪裁区域失败");
            return false;
        }
        
        toast("OCR区域模式: 区域[" + rect.left + "," + rect.top + "," 
             + rect.right + "," + rect.bottom + "], 偏移量(" + offsetX + "," + offsetY + ")");
    } else {
        // 无区域参数：全屏识别
        clipImg = img;
        toast("OCR全屏模式");
    }
    
    // 4. 执行OCR识别
    // 第二个参数是超时时间（毫秒）
    // 第三个参数是可选配置（这里使用空对象使用默认配置）
    let result = paddleOcrOnnx.ocrImage(clipImg, OCR_CONFIG.TIMEOUT, {});
    image.recycle(clipImg);  // 及时释放图片资源
    
    // 5. 检查识别结果
    if (!result || result.length === 0) {
        toast("OCR未识别到任何文字");
        console.timeEnd("ocr_pddj");
        return false;
    }
    
    toast("OCR识别到 " + result.length + " 个文字区域");
    
    // 6. 在识别结果中查找目标文字
    let findItem = null;
    for (let i = 0; i < result.length; i++) {
        let item = result[i];
        let text = item.label.trim();  // 去除首尾空格
        
        // 支持模糊匹配：只要包含目标文字即可
        if (text.includes(targetText)) {
            findItem = item;
            toast("OCR匹配成功: '" + text + "' 包含 '" + targetText + "'");
            break;
        }
    }
    
    // 7. 找到目标文字，计算坐标并点击
    if (findItem) {
        // OCR返回的坐标是相对于剪裁区域的
        // 需要加上偏移量得到全局坐标
        let globalX = offsetX + findItem.x + findItem.width / 2;
        let globalY = offsetY + findItem.y + findItem.height / 2;
        
        // 四舍五入取整
        globalX = Math.round(globalX);
        globalY = Math.round(globalY);
        
        toast("找到文字 [" + targetText + "]，双击坐标: (" + globalX + ", " + globalY + ")");
        
        // 执行双击
        doDoubleClick(globalX, globalY);
        
        console.timeEnd("ocr_pddj");
        return true;
    } else {
        // 未找到目标文字
        toast("OCR未找到指定文字: " + targetText);
        console.timeEnd("ocr_pddj");
        return false;
    }
}

/**
 * OCR文字检测（PaddleOCR版本）
 * 
 * 在指定区域内使用PaddleOCR进行文字识别
 * 仅判断目标文字是否存在，不执行点击
 * 支持坐标自动缩放
 * 
 * @param {string} targetText - 要查找的目标文字（支持模糊匹配）
 * @param {number} [x1] - 搜索区域左边X（基准坐标，可选）
 * @param {number} [y1] - 搜索区域上边Y（基准坐标，可选）
 * @param {number} [x2] - 搜索区域右边X（基准坐标，可选）
 * @param {number} [y2] - 搜索区域下边Y（基准坐标，可选）
 * @returns {boolean} true=文字存在, false=文字不存在
 * 
 * @example
 * // 全屏检测
 * if (ocrFuncpd("首页")) {
 *     toast("检测到首页文字");
 * }
 * 
 * // 在指定区域检测
 * if (ocrFuncpd("评论", 0, 0, 500, 500)) {
 *     toast("在指定区域检测到评论文字");
 * }
 */
function ocrFuncpd(targetText, x1, y1, x2, y2) {
    // 1. 截取全屏（代理模式用Ex版本消除色差，HID模式用普通版本）
    let img;
    if (RUN_MODE === "agent") {
        img = image.captureFullScreenEx();
    }
    if (!img) {
        img = image.captureFullScreen();
    }
    if (!img) {
        toast("OCR截图失败");
        return false;
    }
    // EC 低版本 captureFullScreen 返回 UUID 字符串，OCR 需要 AutoImage
    if (typeof img === "string") img = new AutoImage(img);
    if (!img) {
        toast("OCR截图失败");
        return false;
    }
    
    // 2. 确定识别区域
    let clipImg;
    if (x1 !== undefined && y1 !== undefined && x2 !== undefined && y2 !== undefined) {
        // 有区域参数
        let rect = toRealRect(x1, y1, x2, y2);
        clipImg = image.clip(img, rect.left, rect.top, rect.right, rect.bottom);
        image.recycle(img);
        
        if (!clipImg) {
            toast("OCR剪裁区域失败");
            return false;
        }
    } else {
        // 全屏模式
        clipImg = img;
    }
    
    // 3. 执行OCR识别
    let result = paddleOcrOnnx.ocrImage(clipImg, OCR_CONFIG.TIMEOUT, {});
    image.recycle(clipImg);
    
    if (!result || result.length === 0) {
        toast("OCR[" + targetText + "]: 未识别到任何文字");
        return false;
    }
    
    // 4. 查找目标文字
    for (let i = 0; i < result.length; i++) {
        let text = result[i].label.trim();
        if (text.includes(targetText)) {
            toast("OCR[" + targetText + "]: 检测到 → " + text);
            return true;
        }
    }
    
    // 5. 未匹配目标文字，输出已识别的文字供调试
    let allText = [];
    for (let i = 0; i < result.length; i++) {
        allText.push(result[i].label.trim());
    }
    toast("OCR[" + targetText + "]: 未匹配！识别到: " + allText.join(", "));
    
    return false;
}

/*================================================================
 * 辅助工具函数
 *================================================================*/

/**
 * 获取推荐的点击坐标
 * 
 * 根据当前屏幕尺寸计算一个推荐的点击位置
 * 主要用于点击视频区域中央
 * 
 * @returns {object} 推荐点击坐标 { x, y }
 */
function getClickPoint() {
    // 获取当前屏幕尺寸
    let sw = device.getScreenWidth();
    let sh = device.getScreenHeight();
    
    // 计算Y坐标（点击区域垂直居中）
    let targetY;
    if (sh <= 2400) {
        // 较小屏幕：Y范围微调
        targetY = Math.round(1500 + (sh - 2400) * 0.1);
    } else {
        // 较大屏幕：Y范围按比例缩放（使用基准值2135，即2100和2170的中间值）
        targetY = Math.round(yScale(2135, sh));
    }
    
    // 计算X坐标（点击区域水平居中）
    let targetX;
    if (sw <= 1080) {
        // 较窄屏幕：X范围微调
        targetX = Math.round(990 + (sw - 1080) * 0.5);
    } else {
        // 较宽屏幕：X范围按比例缩放
        targetX = Math.round(1335 + (sw - 1440) * 0.5);
    }
    
    toast("推荐点击坐标: (" + targetX + ", " + targetY + ")");
    return { x: targetX, y: targetY };
}

/**
 * Y坐标缩放计算
 * 
 * 根据屏幕高度对Y坐标进行缩放
 * 
 * @param {number} baseValue - 基准值（基于3200高度）
 * @param {number} currentHeight - 当前屏幕高度
 * @returns {number} 缩放后的Y坐标
 */
function yScale(baseValue, currentHeight) {
    // 计算缩放比例（基于3200高度）
    let scale = currentHeight / 3200;
    
    // 按比例缩放
    return baseValue * scale;
}

/*================================================================
 * 时间窗口控制
 *================================================================*/

/**
 * 判断当前是否在活跃时间段内（默认 7:00 - 23:00）
 * @returns {boolean} true=活跃时间, false=休眠时间
 */
function isActiveHours() {
    let now = new Date();
    let hour = now.getHours();
    // 7-23点活跃
    return hour >= 7 && hour < 23;
}

/**
 * 获取到下次活跃时间的等待秒数
 * @returns {number} 秒数
 */
function getSleepSeconds() {
    let now = new Date();
    let hour = now.getHours();
    let minute = now.getMinutes();
    let second = now.getSeconds();
    
    if (hour >= 23) {
        // 等到明早7点: (24-now.hour + 7) * 3600 - now.min*60 - now.sec
        return ((24 - hour + 7) * 3600) - (minute * 60) - second;
    } else {
        // 今天7点还没到: (7 - now.hour) * 3600 - now.min*60 - now.sec
        return ((7 - hour) * 3600) - (minute * 60) - second;
    }
}

