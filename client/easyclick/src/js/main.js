/**
 * 通用云控系统 - 设备代理 (EasyClick)
 * 自动加载: common.js → douyin.js → main.js
 */

// ========== 全局变量 ==========
var gDeviceId = null;
var gDeviceToken = "";
var gWebSocket = null;
var gMqttTransport = null;
var gCloudMqttTransportClass = null;
var gCloudConnected = false;
var gMqttConnecting = false;
var CLOUD_TRANSPORT = "mqtt";
var CLOUD_AGENT_VERSION = "9.3.0";
var CLOUD_PROTOCOL_VERSION = 2;
var CLOUD_CAPABILITIES = ["qos1_ack", "durable_event_outbox_v2", "task_watchdog", "adaptive_heartbeat", "adaptive_stream", "signed_update_notice"];
var gHeartbeatTimer = null;
var gHeartbeatIdleMs = 30000;
var gHeartbeatBusyMs = 10000;
var gHeartbeatSequence = 0;
var gLogFlushTimer = null;
var gReconnectTimer = null;
var gReconnectAttempts = 0;
var CLOUD_RECONNECT_BASE_DELAY_MS = 5000;
var CLOUD_RECONNECT_MAX_DELAY_MS = 60000;
var gIsRunning = false;
var gIsPaused = false;
var gScriptStateSeq = 0;
var gCloudInstanceId = "ec-instance-" + new Date().getTime() + "-" +
    Math.floor(Math.random() * 1000000);
var gRuntimeLeaseTimer = null;
var CLOUD_RUNTIME_LEASE_STORE = "cloud_runtime_guard";
var CLOUD_RUNTIME_LEASE_KEY = "active_instance";
var CLOUD_RUNTIME_LEASE_TTL = 45000;
var gBusinessThreadId = 0;
var gCurrentTaskId = null;
var gCurrentTaskRunId = "";
var gTaskWatchdogTimer = null;
var gTaskTerminalSent = false;
var gShouldStop = false;
var gLocalInited = false;
var gHidActivationBlocked = false;
var gHidActivationFailureCount = 0;
var gLocation = null;
var gLogQueue = [];
var gStreamTimer = null;
var gStreamIntervalMs = 1000;
var gStreamQuality = 15;
var gStreamScalePercent = 40;
var gStreamFailureCount = 0;
var gStreamSessionId = 0;
var gSmsTimer = null;
var gLastSmsId = 0;
var gSmsCount = 0;
var gSmsLatest = "";
var gHidKeepAliveTimer = null;
var HID_CENTER_URL = "http://118.25.122.100:8988";
var MAX_CLOUD_LOG_QUEUE = 500;
var MAX_CLOUD_LOG_BATCH = 50;
var CLOUD_COMMAND_HISTORY_STORE = "cloud_command_history";
var CLOUD_COMMAND_HISTORY_KEY = "accepted_cmd_ids";
var MAX_CLOUD_COMMAND_HISTORY = 500;
var gHandledCloudCommandMap = null;
var gHandledCloudCommandOrder = null;
var CLOUD_EVENT_OUTBOX_STORE = "cloud_event_outbox";
var CLOUD_EVENT_OUTBOX_KEY = "pending_events";
var MAX_CLOUD_EVENT_OUTBOX = 500;
var MAX_CLOUD_EVENT_OUTBOX_BYTES = 2 * 1024 * 1024;
var CLOUD_EVENT_RETRY_MS = 10000;
var gCloudEventOutbox = null;
var CLOUD_TASK_JOURNAL_STORE = "cloud_task_journal";
var CLOUD_TASK_JOURNAL_KEY = "active_task";
var gUpdateCheckTimer = null;
var CLOUD_UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000;

// ========== 基础工具函数 ==========

function getServerUrl() {
    var raw = "";
    try {
        raw = storages.create("auto_config_store").getString("cloud_server_address", "") + "";
    } catch (e) {}
    raw = String(raw || "").trim();
    if (!raw || raw === "null" || raw === "undefined") return "";
    raw = raw.replace(/^http:\/\//i, "ws://").replace(/^https:\/\//i, "wss://");
    if (!/^wss?:\/\//i.test(raw)) raw = "ws://" + raw;
    raw = raw.replace(/\/+$/, "");
    var pathIndex = raw.indexOf("/ws/device");
    if (pathIndex >= 0) return raw.substring(0, pathIndex) + "/ws/device/";
    var authority = raw.replace(/^wss?:\/\//i, "").split("/")[0];
    if (authority.indexOf(":") < 0) raw += ":18080";
    return raw + "/ws/device/";
}

function getHTTPBaseURL() {
    var wsURL = getServerUrl();
    if (!wsURL) return "";
    return wsURL.replace(/^ws:/i, "http:").replace(/^wss:/i, "https:").replace(/\/ws\/device\/$/, "");
}

function getMqttEndpoint() {
    var raw = "";
    var port = 1883;
    try {
        var store = storages.create("auto_config_store");
        raw = store.getString("cloud_server_address", "") + "";
        var configuredPort = Number(store.getString("cloud_mqtt_port", "1883") + "");
        if (configuredPort > 0 && configuredPort <= 65535) port = configuredPort;
    } catch (e) {}
    raw = String(raw || "").trim().replace(/^wss?:\/\//i, "").replace(/^https?:\/\//i, "");
    raw = raw.split("/")[0];
    var host = raw;
    if (host.charAt(0) === "[") {
        var end = host.indexOf("]");
        if (end > 0) host = host.substring(1, end);
    } else if ((host.match(/:/g) || []).length === 1) {
        host = host.split(":")[0];
    }
    return { host: host, port: port };
}

function getDeviceName() {
    // 读取保存的设备名（用户在UI配置的）
    try {
        var n = storages.create("auto_config_store").getString("device_name", "") + "";
        if (n && n != "null" && n != "") return n;
    } catch (e) {}
    // 兜底：AndroidID
    try { var aid = device.getAndroidId(); if (aid) return aid; } catch (e) {}
    return "Unknown";
}

function initDeviceId() {
    if (gDeviceId && String(gDeviceId) !== "" && String(gDeviceId) !== "null") {
        return gDeviceId;
    }
    // 尝试从持久化存储读取
    try {
        var s = storages.create("cloud_agent");
        var v = s.getString("device_id", "");
        if (v && String(v) !== "" && String(v) !== "null") {
            gDeviceId = String(v);
            return gDeviceId;
        }
    } catch (e) {
        // 读取失败，继续
    }
    // 使用 AndroidId 作为设备标识
    try {
        var aid = device.getAndroidId();
        if (aid && String(aid) !== "" && String(aid) !== "null") {
            gDeviceId = "EC-" + String(aid);
        }
    } catch (e) {
        // 获取失败，继续
    }
    // 最终回退：使用时间戳
    if (!gDeviceId || String(gDeviceId) === "" || String(gDeviceId) === "null") {
        gDeviceId = "EC-" + new Date().getTime();
    }
    // 持久化保存
    try {
        var s = storages.create("cloud_agent");
        s.putString("device_id", gDeviceId);
    } catch (e) {
        // 保存失败，忽略
    }
    return gDeviceId;
}

function loadDeviceToken() {
    try {
        var token = storages.create("cloud_agent").getString("device_token", "") + "";
        gDeviceToken = token && token !== "null" ? token : "";
    } catch (e) {
        gDeviceToken = "";
    }
    return gDeviceToken;
}

function saveDeviceToken(token) {
    token = String(token || "");
    if (!token) return false;
    try {
        storages.create("cloud_agent").putString("device_token", token);
        gDeviceToken = token;
        return true;
    } catch (e) {
        return false;
    }
}

function getDeviceIP() {
    try {
        var v = device.getIpAddress();
        if (v && v !== "0.0.0.0") return v;
    } catch (e) {}
    try {
        var v = device.getWifiIp();
        if (v && v !== "0.0.0.0") return v;
    } catch (e) {}
    try {
        var v = device.getLocalIp();
        if (v && v !== "0.0.0.0") return v;
    } catch (e) {}
    try {
        var v = device.getIp();
        if (v && v !== "0.0.0.0") return v;
    } catch (e) {}
    // 地理位置 IP
    if (gLocation && gLocation.ip) {
        return gLocation.ip;
    }
    // 局域网直连不依赖公网 IP 服务，避免无互联网时阻塞首次上线。
    return "";
}

function tryGet(funcName) {
    try {
        var v = device[funcName]();
        return v || 0;
    } catch (e) {
        return 0;
    }
}

function fetchGeoLocation() {
    try {
        var resp = http.httpGetDefault("https://r.inews.qq.com/api/ip2city", 5000, null);
        if (resp) {
            var data = JSON.parse(resp);
            if (data.ret === 0) {
                gLocation = {
                    ip: data.ip || "",
                    province: data.province || "",
                    city: data.city || "",
                    country: data.country || ""
                };
            }
        }
    } catch (e) {
        // 获取地理位置失败
    }
}

function fetchPublicIP() {
    try {
        var resp = http.httpGetDefault("https://myip.ipip.net", 5000, null);
        if (resp) {
            // 返回格式 "当前 IP：x.x.x.x  来自于：..."
            var match = resp.match(/\d+\.\d+\.\d+\.\d+/);
            if (match) {
                logd("公网IP: " + match[0]);
                return match[0];
            }
        }
    } catch (e) {
        logd("公网IP获取失败: " + e);
    }
    return "";
}

// ========== WebSocket 通信 ==========

function sendWS(msg) {
    if (isMqttTransportConnected()) {
        try {
            if (gMqttTransport.sendObject(msg)) {
                return true;
            }
        } catch (e) {
            // MQTT 失败时继续尝试 WebSocket 备用通道
        }
    }
    if (isWebSocketConnected()) {
        try {
            gWebSocket.sendText(JSON.stringify(msg));
            return true;
        } catch (e) {
            // 发送失败
        }
    }
    return false;
}

function loadCloudEventOutbox() {
    if (gCloudEventOutbox !== null) return;
    gCloudEventOutbox = [];
    try {
        var raw = storages.create(CLOUD_EVENT_OUTBOX_STORE).getString(CLOUD_EVENT_OUTBOX_KEY, "") + "";
        var parsed = raw ? JSON.parse(raw) : [];
        if (parsed && parsed.length) gCloudEventOutbox = parsed;
    } catch (e) {
        gCloudEventOutbox = [];
    }
}

function cloudEventPolicy(message) {
    var type = message && message.type || "";
    var status = Number(message && message.data && message.data.status || 0);
    if (type === "task_status" && status >= 2) return { priority: 4, ttl_ms: 30 * 86400000, kind: "task_terminal" };
    if (type === "ack") return { priority: 3, ttl_ms: 7 * 86400000, kind: "command_ack" };
    if (type === "task_status") return { priority: 2, ttl_ms: 86400000, kind: "task_progress" };
    return { priority: 1, ttl_ms: 3600000, kind: type || "event" };
}

function compactCloudEventOutbox(now) {
    now = now || new Date().getTime();
    var kept = [];
    for (var i = 0; i < gCloudEventOutbox.length; i++) {
        var item = gCloudEventOutbox[i];
        if (item.expires_at && Number(item.expires_at) < now) continue;
        kept.push(item);
    }
    gCloudEventOutbox = kept;
    while (gCloudEventOutbox.length > MAX_CLOUD_EVENT_OUTBOX) {
        var removeIndex = -1;
        var lowest = 99;
        for (var j = 0; j < gCloudEventOutbox.length; j++) {
            var priority = Number(gCloudEventOutbox[j].priority || 1);
            if (priority < 4 && priority < lowest) {
                lowest = priority;
                removeIndex = j;
            }
        }
        // Completed/failed/timeout task states are never evicted by the soft
        // item count. Their result text is compacted when queued to bound disk.
        if (removeIndex < 0) break;
        gCloudEventOutbox.splice(removeIndex, 1);
    }
    var encoded = "";
    try { encoded = JSON.stringify(gCloudEventOutbox); } catch (e) {}
    while (encoded.length > MAX_CLOUD_EVENT_OUTBOX_BYTES) {
        var candidate = -1;
        var candidatePriority = 99;
        for (var k = 0; k < gCloudEventOutbox.length; k++) {
            var p = Number(gCloudEventOutbox[k].priority || 1);
            if (p < 4 && p < candidatePriority) {
                candidate = k;
                candidatePriority = p;
            }
        }
        if (candidate < 0) break;
        gCloudEventOutbox.splice(candidate, 1);
        try { encoded = JSON.stringify(gCloudEventOutbox); } catch (ignore) { break; }
    }
}

function saveCloudEventOutbox() {
    try {
        storages.create(CLOUD_EVENT_OUTBOX_STORE).putString(
            CLOUD_EVENT_OUTBOX_KEY, JSON.stringify(gCloudEventOutbox || [])
        );
    } catch (e) {}
}

function queueCloudEvent(message, eventId) {
    eventId = String(eventId || "");
    if (!eventId) return sendWS(message);
    loadCloudEventOutbox();
    for (var i = 0; i < gCloudEventOutbox.length; i++) {
        if (gCloudEventOutbox[i].event_id === eventId) {
            flushCloudEventOutbox(true);
            return true;
        }
    }
    var policy = cloudEventPolicy(message);
    var now = new Date().getTime();
    if (policy.kind === "task_progress") {
        var taskId = String(message.data && message.data.task_id || "");
        var runId = String(message.data && message.data.run_id || "");
        for (var j = gCloudEventOutbox.length - 1; j >= 0; j--) {
            var old = gCloudEventOutbox[j];
            var oldData = old.message && old.message.data || {};
            if (old.kind === "task_progress" && String(oldData.task_id || "") === taskId && String(oldData.run_id || "") === runId) {
                gCloudEventOutbox.splice(j, 1);
            }
        }
    }
    message.data = message.data || {};
    message.data.event_id = eventId;
    gCloudEventOutbox.push({
        event_id: eventId, message: message, last_sent_at: 0, attempts: 0,
        priority: policy.priority, kind: policy.kind, created_at: now, expires_at: now + policy.ttl_ms
    });
    compactCloudEventOutbox(now);
    saveCloudEventOutbox();
    flushCloudEventOutbox(true);
    return true;
}

function flushCloudEventOutbox(force) {
    loadCloudEventOutbox();
    if (!isCloudConnected() || gCloudEventOutbox.length === 0) return false;
    var now = new Date().getTime();
    var changed = false;
    var sent = 0;
    for (var i = 0; i < gCloudEventOutbox.length && sent < 20; i++) {
        var item = gCloudEventOutbox[i];
        if (!force && item.last_sent_at && now - Number(item.last_sent_at) < CLOUD_EVENT_RETRY_MS) continue;
        if (!sendWS(item.message)) break;
        item.last_sent_at = now;
        item.attempts = Number(item.attempts || 0) + 1;
        changed = true;
        sent++;
    }
    if (changed) saveCloudEventOutbox();
    return sent > 0;
}

function acknowledgeCloudEvent(eventId) {
    eventId = String(eventId || "");
    if (!eventId) return;
    loadCloudEventOutbox();
    var kept = [];
    for (var i = 0; i < gCloudEventOutbox.length; i++) {
        if (gCloudEventOutbox[i].event_id !== eventId) kept.push(gCloudEventOutbox[i]);
    }
    if (kept.length !== gCloudEventOutbox.length) {
        gCloudEventOutbox = kept;
        saveCloudEventOutbox();
    }
}

function cloudTransportLog(message) {
    try {
        logd("[云控传输] " + message);
    } catch (ignore) {
    }
    cloudLog(message);
}

function sendCloudRegister() {
    sendWS({
        type: "register",
        data: {
            device_id: gDeviceId,
            name: getDeviceName(),
            model: device.getModel(),
            os_version: device.getOSVersion(),
            agent_version: CLOUD_AGENT_VERSION,
            protocol_version: CLOUD_PROTOCOL_VERSION,
            capabilities: CLOUD_CAPABILITIES,
            ip: getDeviceIP(),
            battery: device.getBattery(),
            screen_width: device.getScreenWidth(),
            screen_height: device.getScreenHeight(),
            location: gLocation
        }
    });
}

function sendCloudAck(cmdId, kind, ok, message) {
    if (!cmdId) {
        return;
    }
    queueCloudEvent({
        type: "ack",
        data: {
            cmd_id: cmdId,
            kind: kind || "command",
            ok: ok !== false,
            message: message || "已接收"
        }
    }, "ack:" + cmdId);
}

function loadHandledCloudCommands() {
    if (gHandledCloudCommandMap !== null && gHandledCloudCommandOrder !== null) {
        return;
    }
    gHandledCloudCommandMap = {};
    gHandledCloudCommandOrder = [];
    try {
        var store = storages.create(CLOUD_COMMAND_HISTORY_STORE);
        var raw = store.getString(CLOUD_COMMAND_HISTORY_KEY, "") + "";
        var parsed = raw ? JSON.parse(raw) : [];
        if (parsed && parsed.length) {
            for (var i = 0; i < parsed.length; i++) {
                var cmdId = String(parsed[i] || "");
                if (!cmdId || gHandledCloudCommandMap[cmdId]) continue;
                gHandledCloudCommandMap[cmdId] = true;
                gHandledCloudCommandOrder.push(cmdId);
            }
        }
    } catch (e) {
        gHandledCloudCommandMap = {};
        gHandledCloudCommandOrder = [];
    }
}

// Returns false for a duplicate command. The id is persisted before ACK so a
// reconnect or an ACK retry cannot execute the same operation twice.
function claimCloudCommand(cmdId) {
    cmdId = String(cmdId || "");
    if (!cmdId) return true;
    loadHandledCloudCommands();
    if (gHandledCloudCommandMap[cmdId]) return false;
    gHandledCloudCommandMap[cmdId] = true;
    gHandledCloudCommandOrder.push(cmdId);
    while (gHandledCloudCommandOrder.length > MAX_CLOUD_COMMAND_HISTORY) {
        var expired = gHandledCloudCommandOrder.shift();
        delete gHandledCloudCommandMap[expired];
    }
    try {
        storages.create(CLOUD_COMMAND_HISTORY_STORE).putString(
            CLOUD_COMMAND_HISTORY_KEY,
            JSON.stringify(gHandledCloudCommandOrder)
        );
    } catch (e) {
        // The in-memory guard remains active for the current process.
    }
    return true;
}

function sendTaskStatus(taskId, status, result, runId) {
    var eventId = "task-status:" + taskId + ":" + status + ":" + new Date().getTime();
    queueCloudEvent({
        type: "task_status",
        data: {
            task_id: taskId,
			run_id: runId || "",
            status: status,
            result: safeSubStr(result || "", status >= 2 ? 2048 : 512)
        }
    }, eventId);
}

function saveActiveTaskJournal(taskData) {
    try {
        storages.create(CLOUD_TASK_JOURNAL_STORE).putString(CLOUD_TASK_JOURNAL_KEY, JSON.stringify({
            task_id: taskData.task_id,
            cmd_id: taskData.cmd_id || "",
			run_id: taskData.run_id || "",
            started_at: new Date().getTime()
        }));
    } catch (e) {}
}

function clearActiveTaskJournal(taskId) {
    try {
        var store = storages.create(CLOUD_TASK_JOURNAL_STORE);
        var raw = store.getString(CLOUD_TASK_JOURNAL_KEY, "") + "";
        var active = raw ? JSON.parse(raw) : null;
        if (!active || String(active.task_id) === String(taskId)) store.putString(CLOUD_TASK_JOURNAL_KEY, "");
    } catch (e) {}
}

function recoverInterruptedTask() {
    try {
        var store = storages.create(CLOUD_TASK_JOURNAL_STORE);
        var raw = store.getString(CLOUD_TASK_JOURNAL_KEY, "") + "";
        var active = raw ? JSON.parse(raw) : null;
        if (active && active.task_id) {
			sendTaskStatus(active.task_id, 4, "手机脚本重启，已恢复连接但上次任务被中断", active.run_id || "");
            store.putString(CLOUD_TASK_JOURNAL_KEY, "");
        }
    } catch (e) {}
}

function currentScriptStatus() {
    return gIsRunning
        ? (gIsPaused ? "paused" : "running")
        : "idle";
}

function sendScriptState(transition) {
    if (transition === true) {
        gScriptStateSeq++;
    }
    return sendWS({
        type: "heartbeat",
        data: {
            device_id: gDeviceId,
            script_status: currentScriptStatus(),
            instance_id: gCloudInstanceId,
            state_seq: gScriptStateSeq,
            state_transition: transition === true
        }
    });
}

function cloudLog(msg, level) {
    var message = (msg && msg.substring)
        ? safeSubStr(msg, 500)
        : safeSubStr(String(msg), 500);
    if (!message) {
        return;
    }
    while (gLogQueue.length >= MAX_CLOUD_LOG_QUEUE) {
        gLogQueue.shift();
    }
    gLogQueue.push({
        level: level || "info",
        message: message,
        client_time: new Date().getTime()
    });
}

function flushCloudLogs() {
    if (gLogQueue.length === 0 || !isCloudConnected()) {
        return false;
    }
    var batch = gLogQueue.slice(0, MAX_CLOUD_LOG_BATCH);
    var sent = sendWS({
        type: "log_batch",
        data: {
            entries: batch
        }
    });
    if (sent) {
        gLogQueue.splice(0, batch.length);
    }
    return sent;
}

function startLogFlush() {
    stopLogFlush();
    flushCloudLogs();
    gLogFlushTimer = setInterval(function () {
        flushCloudLogs();
    }, 3000);
}

function stopLogFlush() {
    if (gLogFlushTimer) {
        cancelInterval(gLogFlushTimer);
        gLogFlushTimer = null;
    }
}

function safeSubStr(s, max) {
    if (!s) return "";
    return s.length > max ? s.substring(0, max) : s;
}

// 同一台手机只允许一个云控脚本实例持有 MQTT 连接。
// EasyClick 同时开启“自动运行”和手动点击启动时，会产生两个相同 clientId 的连接，
// MQTT broker 会让它们相互顶掉，表现为“刚连接成功又立即关闭”。
function refreshRuntimeLease() {
    try {
        var store = storages.create(CLOUD_RUNTIME_LEASE_STORE);
        store.putString(CLOUD_RUNTIME_LEASE_KEY, JSON.stringify({
            instance_id: gCloudInstanceId,
            device_id: gDeviceId || "",
            updated_at: new Date().getTime()
        }));
        return true;
    } catch (e) {
        return false;
    }
}

function acquireRuntimeLease() {
    try {
        initDeviceId();
        var store = storages.create(CLOUD_RUNTIME_LEASE_STORE);
        var raw = store.getString(CLOUD_RUNTIME_LEASE_KEY, "") + "";
        var existing = null;
        if (raw) {
            try { existing = JSON.parse(raw); } catch (ignore) {}
        }
        var now = new Date().getTime();
        if (existing && existing.instance_id &&
            existing.instance_id !== gCloudInstanceId &&
            existing.updated_at && now - Number(existing.updated_at) >= 0 &&
            now - Number(existing.updated_at) < CLOUD_RUNTIME_LEASE_TTL) {
            logd("检测到同设备云控实例仍在运行，本次启动已退出，避免 MQTT 连接互相顶掉");
            toast("云控已在运行，请先停止旧脚本再启动");
            return false;
        }
        refreshRuntimeLease();
        var verified = null;
        try {
            verified = JSON.parse(store.getString(CLOUD_RUNTIME_LEASE_KEY, "") + "");
        } catch (ignore2) {}
        if (!verified || verified.instance_id !== gCloudInstanceId) {
            logd("云控单实例租约获取失败，本次启动已退出");
            return false;
        }
        gRuntimeLeaseTimer = setInterval(function () {
            refreshRuntimeLease();
        }, 15000);
        return true;
    } catch (e) {
        // 存储不可用时不能阻断主功能，仍继续启动。
        return true;
    }
}

function releaseRuntimeLease() {
    if (gRuntimeLeaseTimer) {
        cancelInterval(gRuntimeLeaseTimer);
        gRuntimeLeaseTimer = null;
    }
    try {
        var store = storages.create(CLOUD_RUNTIME_LEASE_STORE);
        var current = null;
        try {
            current = JSON.parse(store.getString(CLOUD_RUNTIME_LEASE_KEY, "") + "");
        } catch (ignore) {}
        if (current && current.instance_id === gCloudInstanceId) {
            store.putString(CLOUD_RUNTIME_LEASE_KEY, "");
        }
    } catch (e) {}
}

// 重写全局 report 函数，将日志同时上报到云端
var _origReport = this.report;
this.report = function (msg) {
    if (msg && typeof msg === "string") {
        cloudLog(msg);
    }
    try {
        if (typeof _origReport === "function") {
            _origReport(msg);
        }
    } catch (e) {
        // 原始 report 调用失败
    }
};

// ========== 任务执行 ==========

function cancelTaskWatchdog() {
    if (gTaskWatchdogTimer) {
        cancelTimeout(gTaskWatchdogTimer);
        gTaskWatchdogTimer = null;
    }
}

function finishCurrentTask(taskId, runId, status, result) {
    if (String(gCurrentTaskId || "") !== String(taskId || "") || String(gCurrentTaskRunId || "") !== String(runId || "") || gTaskTerminalSent) {
        return false;
    }
    gTaskTerminalSent = true;
    sendTaskStatus(taskId, status, result, runId);
    clearActiveTaskJournal(taskId);
    return true;
}

function cleanupCurrentTask(taskId, runId) {
    if (String(gCurrentTaskId || "") !== String(taskId || "") || String(gCurrentTaskRunId || "") !== String(runId || "")) return;
    cancelTaskWatchdog();
    gIsRunning = false;
    gIsPaused = false;
    gBusinessThreadId = 0;
    gCurrentTaskId = null;
    gCurrentTaskRunId = "";
    sendScriptState(true);
}

function startTaskWatchdog(taskId, runId, timeoutSeconds) {
    cancelTaskWatchdog();
    timeoutSeconds = Number(timeoutSeconds || 3600);
    if (timeoutSeconds < 30) timeoutSeconds = 30;
    if (timeoutSeconds > 86400) timeoutSeconds = 86400;
    gTaskWatchdogTimer = setTimeout(function () {
        if (!finishCurrentTask(taskId, runId, 4, "任务执行超时，已由手机端看门狗终止")) return;
        try {
            if (gBusinessThreadId) thread.cancelThread(gBusinessThreadId);
        } catch (e) {}
        cleanupCurrentTask(taskId, runId);
    }, timeoutSeconds * 1000);
}

function executeTask(taskData) {
    var taskId = taskData.task_id;
    var script = taskData.script;
    var params = taskData.params || {};
	var runId = taskData.run_id || "";

    if (gHidActivationBlocked) {
		sendTaskStatus(taskId, 3, "HID激活失败，本地脚本已停止，请先处理应用设置", runId);
        cloudLog("拒绝执行任务：HID连续激活失败，本地脚本已锁定", "error");
        return;
    }
    if (!script) {
		sendTaskStatus(taskId, 3, "脚本为空", runId);
        return;
    }
    if (gIsRunning) {
		sendTaskStatus(taskId, 3, "设备忙", runId);
        return;
    }

    resetHidSettingsOpenGuard();
    gIsRunning = true;
    gIsPaused = false;
    gCurrentTaskId = taskId;
	gCurrentTaskRunId = runId;
    gTaskTerminalSent = false;
    saveActiveTaskJournal(taskData);
	sendTaskStatus(taskId, 1, "开始执行", runId);
    startTaskWatchdog(taskId, runId, taskData.timeout_seconds);

    // 通知云端脚本状态变更
    sendScriptState(true);

    // 申请截图权限
    try {
        var ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + "";
        if (ms.includes("HID")) {
            image.requestScreenCapture(10000, 1);
        } else {
            image.requestScreenCapture(10000, 0);
        }
    } catch (e) {
        // 截图权限申请失败，继续执行
    }

    // 异步执行脚本
    gBusinessThreadId = thread.execAsync(function () {
        try {
            var fn = new Function("params", script);
            var r = fn(params);
            var resultStr = "OK";
            if (r !== undefined && r !== null) {
                resultStr = typeof r === "string" ? r : JSON.stringify(r);
            }
			finishCurrentTask(taskId, runId, 2, resultStr);
        } catch (e) {
			finishCurrentTask(taskId, runId, 3, e.message);
        } finally {
			cleanupCurrentTask(taskId, runId);
        }
    });
}

function stopCurrentTask(reason, expectedTaskId, expectedRunId) {
    if (expectedTaskId && String(expectedTaskId) !== String(gCurrentTaskId || "")) return false;
    if (expectedRunId && String(expectedRunId) !== String(gCurrentTaskRunId || "")) return false;
    var taskId = gCurrentTaskId;
    var runId = gCurrentTaskRunId;
    if (taskId) finishCurrentTask(taskId, runId, 3, reason || "已停止");
    if (gBusinessThreadId) {
        try {
            thread.cancelThread(gBusinessThreadId);
        } catch (e) {
            // 取消线程失败
        }
    }
    if (taskId) cleanupCurrentTask(taskId, runId);
    return true;
}

// ========== 消息处理 ==========

function handleMessage(text) {
    try {
        var m = JSON.parse(text);
        if (m.type === "event_ack") {
            acknowledgeCloudEvent((m.data || {}).event_id);
        } else if (m.type === "register") {
            var registerData = m.data || {};
            if (Number(registerData.heartbeat_idle_seconds) >= 15) gHeartbeatIdleMs = Number(registerData.heartbeat_idle_seconds) * 1000;
            if (Number(registerData.heartbeat_busy_seconds) >= 5) gHeartbeatBusyMs = Number(registerData.heartbeat_busy_seconds) * 1000;
            if (registerData.device_token && saveDeviceToken(registerData.device_token)) {
                sendWS({ type: "register_ack" });
                setTimeout(function () {
                    if (!gShouldStop) connectMQTT();
                }, 1000);
            }
        } else if (m.type === "stream_control") {
            var streamData = m.data || {};
            if (Number(streamData.interval_ms) >= 500) gStreamIntervalMs = Math.min(5000, Number(streamData.interval_ms));
            if (Number(streamData.quality) >= 5) gStreamQuality = Math.min(40, Number(streamData.quality));
            if (Number(streamData.scale_percent) >= 20) gStreamScalePercent = Math.min(60, Number(streamData.scale_percent));
        } else if (m.type === "task") {
            var taskData = m.data || {};
            if (!claimCloudCommand(taskData.cmd_id)) {
                sendCloudAck(taskData.cmd_id, "task", true, "重复任务已忽略");
                return;
            }
            sendCloudAck(taskData.cmd_id, "task", true, "任务已接收");
            executeTask(taskData);
        } else if (m.type === "command") {
            var commandData = m.data || {};
            if (!claimCloudCommand(commandData.cmd_id)) {
                sendCloudAck(commandData.cmd_id, "command", true, "重复命令已忽略");
                return;
            }
            sendCloudAck(commandData.cmd_id, "command", true, "命令已接收");
            executeCommand(commandData);
        }
    } catch (e) {
        // 消息解析失败
    }
}

// ========== 命令执行 ==========

function executeCommand(cmdData) {
    var c = cmdData.command;

    switch (c) {
        case "stop":
            stopCurrentTask("已停止");
            if (gLocalInited) {
                stopLocalBusiness();
            } else {
                openHidSettingsAfterStop("云端任务停止");
            }
            break;

        case "cancel_task":
            stopCurrentTask(cmdData.reason === "timeout" ? "服务端判定任务超时" : "任务已取消", cmdData.task_id, cmdData.run_id);
            break;

        case "pause":
            gIsPaused = true;
            sendScriptState(true);
            break;

        case "resume":
            gIsPaused = false;
            sendScriptState(true);
            break;

        case "run_local":
            startLocalBusiness();
            break;

        case "stop_local":
            stopLocalBusiness();
            break;

        case "screenshot":
            handleScreenshotCommand();
            break;

        case "home":
            home();
            break;

        case "back":
            back();
            break;

        case "screen_stream_start":
            startScreenStream();
            break;

        case "screen_stream_stop":
            stopScreenStream();
            break;

        case "download_file":
            downloadCloudFile(cmdData);
            break;

        case "install_apk":
            installApk(cmdData);
            break;

        case "uninstall_app":
            uninstallApp(cmdData);
            break;

        case "list_apps":
            listApps(cmdData);
            break;

        case "read_contacts":
            readContacts(cmdData);
            break;

        case "read_sms":
            readSMS(cmdData);
            break;

        case "sms_monitor":
            toggleSMSMonitor(cmdData);
            break;
    }
}

function handleScreenshotCommand() {
    try {
        // 申请截图权限
        var ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + "";
        image.requestScreenCapture(10000, ms.includes("HID") ? 1 : 0);

        // 截图
        var img = image.captureFullScreenEx();
        if (!img) {
            img = image.captureFullScreen();
        }
        if (!img) {
            return;
        }

        // 缩放图片（50%）
        var sw = device.getScreenWidth();
        var sh = device.getScreenHeight();
        var scaled = image.scaleImage(img, Math.round(sw * 0.5), Math.round(sh * 0.5));
        if (scaled) {
            image.recycle(img);
            img = scaled;
        }

        // 转 Base64
        var b64 = image.toBase64Format(img, "jpg", 20);
        if (!b64 || typeof b64 !== "string") {
            b64 = image.toBase64(img);
        }

        // 上传
        if (b64 && typeof b64 === "string" && b64.length > 0) {
            sendWS({
                type: "screenshot",
                data: {
                    device_id: gDeviceId,
                    image: b64
                }
            });
        }

        // 释放资源
        try {
            image.recycle(img);
        } catch (e) {
            // 释放失败
        }
    } catch (e) {
        // 截图异常
    }
}

// ========== MQTT / WebSocket 连接管理 ==========

function isCloudConnected() {
    return isMqttTransportConnected() || isWebSocketConnected();
}

function isWebSocketConnected() {
    try {
        return !!(gWebSocket && gWebSocket.isConnected());
    } catch (e) {
        return false;
    }
}

function isMqttTransportConnected() {
    try {
        return !!(gMqttTransport && gMqttTransport.isConnected());
    } catch (e) {
        return false;
    }
}

function connectMQTT() {
    if (isMqttTransportConnected()) {
        return true;
    }
    if (gMqttConnecting) {
        // 连接建立尚未完成时，不创建第二个相同 clientId 的 MQTT 客户端。
        return true;
    }
    if (!gDeviceId || String(gDeviceId) === "") {
        initDeviceId();
    }
    if (!gDeviceToken) {
        loadDeviceToken();
    }
    if (gMqttTransport) {
        var staleMqttTransport = gMqttTransport;
        gMqttTransport = null;
        try { staleMqttTransport.close(); } catch (ignoreStale) {}
    }
    var mqttTransport = null;
    gMqttConnecting = true;
    try {
        var mqttEndpoint = getMqttEndpoint();
        if (!mqttEndpoint.host) throw new Error("未配置 MQTT 主机");
        cloudTransportLog("MQTT开始连接 " + mqttEndpoint.host + ":" + mqttEndpoint.port);
        if (!gCloudMqttTransportClass) {
            var mqttModule = require("slib/mqtt_transport.js");
            gCloudMqttTransportClass = mqttModule.CloudMqttTransport ||
                mqttModule.default || mqttModule;
        }
        if (typeof gCloudMqttTransportClass !== "function") {
            throw new Error("slib/mqtt_transport.js 未导出 CloudMqttTransport");
        }
        mqttTransport = new gCloudMqttTransportClass({
            host: mqttEndpoint.host,
            port: mqttEndpoint.port,
            clientId: gDeviceId,
            username: gDeviceId,
            password: gDeviceToken,
            keepAlive: 30
        });
        gMqttTransport = mqttTransport;
        mqttTransport.onMessage(function (text) {
            handleMessage(text);
        });
        mqttTransport.onError(function (error) {
            cloudTransportLog("MQTT异常: " + error);
        });
        mqttTransport.onActivity(function () {
            gCloudConnected = true;
        });
        mqttTransport.onClose(function () {
            if (gMqttTransport !== mqttTransport) {
                return;
            }
            gMqttConnecting = false;
            gMqttTransport = null;
            // 如果 WS 备用通道已经在线，MQTT 断开时继续使用 WS，
            // 不要让设备短暂掉线，也不要重复建立第二条备用连接。
            if (isWebSocketConnected()) {
                gCloudConnected = true;
                startHeartbeat();
                cloudTransportLog("MQTT连接已断开，继续使用WS备用通道");
                return;
            }
            gCloudConnected = false;
            stopHeartbeat();
            cloudTransportLog("MQTT连接已断开");
            if (!gShouldStop) {
                scheduleReconnect();
            }
        });
        if (!mqttTransport.connect()) {
            throw new Error("MQTT connect 返回 false");
        }
        // MQTT 恢复后关闭旧 WS，保证同一设备只有一个主通道。
        if (gWebSocket) {
            var oldWebSocket = gWebSocket;
            gWebSocket = null;
            try {
                oldWebSocket.close();
            } catch (ignore) {}
        }
        gCloudConnected = true;
        gMqttConnecting = false;
        gReconnectAttempts = 0;
        sendCloudRegister();
        startHeartbeat();
		flushCloudEventOutbox(true);
        cloudTransportLog("MQTT连接成功，主通道已启用");
        toast("云控 MQTT 已连接");
        return true;
    } catch (e) {
        gMqttConnecting = false;
        gCloudConnected = false;
        cloudTransportLog("MQTT连接失败，切换WS备用: " + e);
        if (gMqttTransport) {
            try {
                gMqttTransport.close();
            } catch (ignore) {}
        }
        gMqttTransport = null;
        return false;
    }
}

function connectWS() {
    if (isMqttTransportConnected()) {
        return;
    }
    if (isWebSocketConnected()) {
        return;
    }
    if (!gDeviceId || String(gDeviceId) === "") {
        initDeviceId();
    }
    try {
        var serverURL = getServerUrl();
        if (!serverURL) {
            cloudTransportLog("未配置云控连接地址，请返回界面填写服务器局域网 IP");
            toast("请先填写云控连接地址");
            scheduleReconnect();
            return;
        }
        cloudTransportLog("WS直连开始连接 " + serverURL + gDeviceId);
        var wsURL = serverURL + gDeviceId;
        var webSocket = http.newWebsocket(wsURL, null, 1);
        gWebSocket = webSocket;
        webSocket.setCallTimeout(10);
        // 服务端约 54 秒发送一次 ping；90 秒读超时可容忍短时 Wi-Fi 抖动。
        webSocket.setReadTimeout(90);
        webSocket.setWriteTimeout(10);
        webSocket.setPingInterval(0);

        webSocket.onOpen(function () {
            if (gWebSocket !== webSocket) {
                return;
            }
            // 竞态保护：WS 建连期间 MQTT 可能已经恢复。
            if (isMqttTransportConnected()) {
                cloudTransportLog("WS备用通道已连接但MQTT已恢复，关闭WS备用");
                gWebSocket = null;
                try {
                    webSocket.close();
                } catch (ignore) {}
                return;
            }
            gReconnectAttempts = 0;
            gCloudConnected = true;
            sendCloudRegister();
            startHeartbeat();
			flushCloudEventOutbox(true);
            cloudTransportLog("WS局域网直连成功");
            toast("云控已连接");
        });

        webSocket.onText(function (ws1, text) {
            if (gWebSocket !== webSocket) {
                return;
            }
            handleMessage(text);
        });

        webSocket.onClose(function () {
            if (gWebSocket !== webSocket) {
                return;
            }
            gWebSocket = null;
            if (isMqttTransportConnected()) {
                gCloudConnected = true;
                return;
            }
            gCloudConnected = false;
            stopHeartbeat();
            if (!gShouldStop) {
                scheduleReconnect();
            }
        });

        webSocket.onError(function () {
            if (gWebSocket !== webSocket) {
                return;
            }
            cloudTransportLog("WS备用通道连接异常");
            gWebSocket = null;
            if (!isMqttTransportConnected() && !gShouldStop) {
                gCloudConnected = false;
                stopHeartbeat();
                scheduleReconnect();
            }
        });

        if (webSocket.connect(10000)) {
            return;
        }
        if (gWebSocket === webSocket) {
            gWebSocket = null;
        }
    } catch (e) {
        if (typeof webSocket !== "undefined" && gWebSocket === webSocket) {
            gWebSocket = null;
        }
        // 连接异常
    }
    scheduleReconnect();
}

function connectCloud() {
    if (isCloudConnected()) {
        return;
    }
	var configuredAddress = "";
	try { configuredAddress = storages.create("auto_config_store").getString("cloud_server_address", "") + ""; } catch (e) {}
	var secureWebOnly = /^(https|wss):\/\//i.test(String(configuredAddress || "").trim());
	if (CLOUD_TRANSPORT === "mqtt" && !secureWebOnly && connectMQTT()) {
        return;
    }
    // MQTT 暂时不可用时，继续保留原 WebSocket 兼容能力。
    connectWS();
}

// ========== 心跳管理 ==========

function startHeartbeat() {
    stopHeartbeat();
    sendHeartbeatNow();
    startLogFlush();
    scheduleNextHeartbeat();
}

function scheduleNextHeartbeat() {
    if (gShouldStop || !isCloudConnected()) return;
    var delay = gIsRunning ? gHeartbeatBusyMs : gHeartbeatIdleMs;
    gHeartbeatTimer = setTimeout(function () {
        gHeartbeatTimer = null;
        sendHeartbeatNow();
        scheduleNextHeartbeat();
    }, delay);
}

function sendHeartbeatNow() {
    flushCloudEventOutbox(false);
    gHeartbeatSequence++;
    var heartbeatData = {
        device_id: gDeviceId,
        script_status: currentScriptStatus(),
        instance_id: gCloudInstanceId,
        state_seq: gScriptStateSeq,
        state_transition: false,
        battery: device.getBattery(),
        ip: getDeviceIP(),
        province: (gLocation && gLocation.province) || "",
        city: (gLocation && gLocation.city) || "",
        sms_count: gSmsCount,
        sms_latest: safeSubStr(gSmsLatest, 200),
        outbox_depth: gCloudEventOutbox ? gCloudEventOutbox.length : 0
    };
    // Memory and disk probes are relatively expensive on older phones. Idle
    // agents report them every third heartbeat; transitions remain immediate.
    if (gHeartbeatSequence % 3 === 1 || gIsRunning) {
        heartbeatData.mem_total = tryGet("getTotalMem");
        heartbeatData.mem_avail = tryGet("getAvailMem");
        heartbeatData.disk_total = tryGet("getTotalDisk");
        heartbeatData.disk_avail = tryGet("getAvailDisk");
    }
    return sendWS({
        type: "heartbeat",
        data: heartbeatData
    });
}

function stopHeartbeat() {
    if (gHeartbeatTimer) {
        cancelTimeout(gHeartbeatTimer);
        gHeartbeatTimer = null;
    }
    stopLogFlush();
}

// Signed releases are announced only. Installation always requires an
// operator action; the agent never silently downloads or installs an APK.
function checkForAgentUpdate() {
    var baseURL = getHTTPBaseURL();
    if (!baseURL || !gDeviceId) return;
    thread.execAsync(function () {
        try {
            var url = baseURL + "/api/v1/client-updates/latest?channel=stable&device_id=" +
                encodeURIComponent(gDeviceId) + "&version=" + encodeURIComponent(CLOUD_AGENT_VERSION);
            var raw = http.httpGetDefault(url, 8000, null);
            var response = raw ? JSON.parse(raw) : null;
            var update = response && response.data;
            if (!update || update.available !== true || update.requires_confirmation !== true) return;
            var store = storages.create("cloud_agent_updates");
            store.putString("pending_manifest", JSON.stringify(update));
            var lastNotified = store.getString("last_notified_version", "") + "";
            if (lastNotified !== String(update.version || "")) {
                store.putString("last_notified_version", String(update.version || ""));
                cloudLog("发现已签名客户端版本 " + update.version + "，等待管理员确认升级", "info");
                toast("云控客户端有新版本，等待管理员确认");
            }
        } catch (e) {
            // 升级检查失败不影响任务和长连接。
        }
    });
}

function startAgentUpdateChecks() {
    if (gUpdateCheckTimer) return;
    checkForAgentUpdate();
    gUpdateCheckTimer = setInterval(function () { checkForAgentUpdate(); }, CLOUD_UPDATE_CHECK_INTERVAL_MS);
}

function stopAgentUpdateChecks() {
    if (gUpdateCheckTimer) {
        cancelInterval(gUpdateCheckTimer);
        gUpdateCheckTimer = null;
    }
}

// ========== 重连管理 ==========

function scheduleReconnect() {
    if (gReconnectTimer) {
        return;
    }
    // 网络恢复后必须持续尝试；仅限制退避间隔，不能因达到次数上限永久离线。
    gReconnectAttempts++;
    var exponent = Math.min(gReconnectAttempts - 1, 4);
    var backoff = CLOUD_RECONNECT_BASE_DELAY_MS * Math.pow(2, exponent);
    var jitter = Math.floor(Math.random() * CLOUD_RECONNECT_BASE_DELAY_MS);
    var delay = Math.min(backoff + jitter, CLOUD_RECONNECT_MAX_DELAY_MS);
    gReconnectTimer = setTimeout(function () {
        gReconnectTimer = null;
        connectCloud();
    }, delay);
}

// ========== 本地业务管理 ==========

function initLocalBusiness() {
    if (gLocalInited) {
        return true;
    }
    try {
        // 初始化 OCR
        if (typeof initOcrpaddle === "function") {
            initOcrpaddle();
        }
        // 初始化运行模式
        if (typeof initMode === "function" && !initMode()) {
            return false;
        }
        // 申请截图权限
        var ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + "";
        var isHid = ms.includes("HID");
        var reqRet = isHid
            ? image.requestScreenCapture(10000, 1)
            : image.requestScreenCapture(10000, 0);
        if (isHid) {
            if (reqRet == null || reqRet === true) {
                // HID 模式申请成功
            } else {
                return false;
            }
        } else {
            if (reqRet !== true) {
                // 代理模式重试一次
                startEnv();
                sleep(2000);
                reqRet = image.requestScreenCapture(10000, 0);
                if (reqRet !== true) {
                    return false;
                }
            }
        }
        // 初始化 OpenCV
        try {
            image.initOpenCV();
        } catch (e) {
            // OpenCV 初始化失败，不影响主流程
        }
        gLocalInited = true;
        return true;
    } catch (e) {
        return false;
    }
}

function startLocalBusiness() {
    if (gHidActivationBlocked) {
        cloudLog("拒绝启动本地脚本：HID连续激活失败，请先处理应用设置", "error");
        sendScriptState(true);
        return false;
    }
    if (gIsRunning) {
        return;
    }
    if (!initLocalBusiness()) {
        return;
    }
    resetHidSettingsOpenGuard();
    gIsRunning = true;
    gIsPaused = false;
    sendScriptState(true);
    toast("抖音启动中");

    gBusinessThreadId = thread.execAsync(function () {
        try {
            if (typeof startDouyinMain === "function") {
                startDouyinMain();
            } else if (typeof hidcs === "function") {
                hidcs();
            }
        } catch (e) {
            logd("业务异常: " + e.message);
        } finally {
            gIsRunning = false;
            gIsPaused = false;
            gBusinessThreadId = 0;
            sendScriptState(true);
        }
    });
}

function stopLocalBusiness() {
    gIsRunning = false;
    gIsPaused = false;
    try {
        if (gBusinessThreadId) {
            thread.cancelThread(gBusinessThreadId);
        }
    } catch (e) {
        // 取消线程失败
    }
    gBusinessThreadId = 0;
    gLocalInited = false;

    try {
        if (typeof stopDouyinAndReturnHome === "function") {
            stopDouyinAndReturnHome();
        }
    } catch (e) {
        // 停止失败
    }

    sendScriptState(true);
    openHidSettingsAfterStop("本地脚本停止");
}

// ========== 屏幕流 ==========

function startScreenStream() {
    if (gStreamTimer) {
        return;
    }
    // 申请截图权限
    var ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + "";
    image.requestScreenCapture(10000, ms.includes("HID") ? 1 : 0);

    gStreamIntervalMs = 1000;
    gStreamQuality = 15;
    gStreamScalePercent = 40;
    gStreamFailureCount = 0;
    gStreamSessionId++;
    var sessionId = gStreamSessionId;

    function captureAndSchedule() {
        if (sessionId !== gStreamSessionId || gShouldStop) return;
        var delay = gStreamIntervalMs;
        try {
            var battery = Number(device.getBattery() || 100);
            var charging = false;
            try { charging = device.isCharging() === true; } catch (ignoreCharging) {}
            if (battery > 0 && battery < 15 && !charging) {
                delay = 5000;
                gStreamTimer = setTimeout(captureAndSchedule, delay);
                return;
            }
            var img = image.captureFullScreenEx();
            if (!img) {
                img = image.captureFullScreen();
            }
            if (img) {
                // 根据服务端压力和本地发送结果动态缩放。
                var sw = device.getScreenWidth();
                var sh = device.getScreenHeight();
                var ratio = gStreamScalePercent / 100;
                var scaled = image.scaleImage(img, Math.round(sw * ratio), Math.round(sh * ratio));
                if (scaled) {
                    image.recycle(img);
                    img = scaled;
                }
                // 转 Base64
                var b64 = image.toBase64Format(img, "jpg", gStreamQuality);
                if (!b64 || typeof b64 !== "string") {
                    b64 = image.toBase64(img);
                }
                if (b64 && typeof b64 === "string") {
                    var sent = sendWS({
                        type: "screen_frame",
                        data: {
                            device_id: gDeviceId,
                            image: b64
                        }
                    });
                    if (sent) {
                        gStreamFailureCount = Math.max(0, gStreamFailureCount - 1);
                        if (gStreamFailureCount === 0 && gStreamIntervalMs > 1000) gStreamIntervalMs = Math.max(1000, gStreamIntervalMs - 250);
                    } else {
                        gStreamFailureCount++;
                        gStreamIntervalMs = Math.min(5000, gStreamIntervalMs + 500);
                        gStreamQuality = Math.max(8, gStreamQuality - 2);
                        gStreamScalePercent = Math.max(25, gStreamScalePercent - 5);
                    }
                }
                try {
                    image.recycle(img);
                } catch (e) {
                    // 释放失败
                }
            }
        } catch (e) {
            gStreamFailureCount++;
            delay = Math.min(5000, gStreamIntervalMs + 500);
        }
        if (sessionId === gStreamSessionId) gStreamTimer = setTimeout(captureAndSchedule, delay);
    }
    gStreamTimer = setTimeout(captureAndSchedule, 10);

    // 5 分钟后自动停止
    setTimeout(function () {
        if (gStreamTimer && sessionId === gStreamSessionId) {
            stopScreenStream();
        }
    }, 300000);
}

function stopScreenStream() {
    if (gStreamTimer) {
        cancelTimeout(gStreamTimer);
        gStreamTimer = null;
    }
    gStreamSessionId++;
}

// ========== Shell 相关 ==========

function hasShell() {
    var ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + "";
    return !ms.includes("HID") && !ms.includes("无障碍");
}

function downloadCloudFile(d) {
    var url = d.params && d.params.url;
    var name = d.params && d.params.name || url.substring(url.lastIndexOf("/") + 1).split("?")[0];
    var path = "/sdcard/Download/" + name;

    thread.execAsync(function () {
        var ok = http.downloadFile(url, path, 300 * 1000);
        sendWS({
            type: "response",
            data: {
                command: "download_file",
                result: ok ? "下载成功:" + path : "下载失败"
            }
        });
    });
}

function installApk(d) {
    if (!hasShell()) {
        sendWS({
            type: "response",
            data: {
                command: "install_apk",
                result: "不支持Shell"
            }
        });
        return;
    }
    var url = d.params && d.params.url;
    var name = d.params && d.params.name || "update.apk";
    var path = "/sdcard/Download/" + name;

    thread.execAsync(function () {
        var ok = http.downloadFile(url, path, 300 * 1000);
        if (ok) {
            shell.execCommand("pm install -r " + path);
            sendWS({
                type: "response",
                data: {
                    command: "install_apk",
                    result: "安装完成"
                }
            });
        } else {
            sendWS({
                type: "response",
                data: {
                    command: "install_apk",
                    result: "下载失败"
                }
            });
        }
    });
}

function uninstallApp(d) {
    if (!hasShell()) {
        return;
    }
    var pkg = d.params && d.params.package;
    if (!pkg) {
        return;
    }
    shell.execCommand("pm uninstall " + pkg);
    sendWS({
        type: "response",
        data: {
            command: "uninstall_app",
            result: "卸载完成:" + pkg
        }
    });
}

function listApps(d) {
    if (!hasShell()) {
        sendWS({
            type: "response",
            data: {
                command: "list_apps",
                result: "[]"
            }
        });
        return;
    }
    var r = shell.execCommand("pm list packages -3");
    var pkgs = r
        ? (r + "").split("\n")
            .filter(function (l) {
                return l.indexOf("package:") === 0;
            })
            .map(function (l) {
                return l.replace("package:", "").trim();
            })
        : [];
    sendWS({
        type: "response",
        data: {
            command: "list_apps",
            result: JSON.stringify(pkgs)
        }
    });
}

// ========== 通讯录 ==========

function readContacts(cmdData) {
    var list = [];
    try {
        importClass(android.provider.ContactsContract);
        var uri = android.provider.ContactsContract.CommonDataKinds.Phone.CONTENT_URI;
        var cursor = context.getContentResolver().query(
            uri,
            ["display_name", "data1"],
            null, null,
            "display_name ASC"
        );
        if (cursor && cursor.moveToFirst()) {
            var ni = cursor.getColumnIndex("display_name");
            var pi = cursor.getColumnIndex("data1");
            var n = 0;
            do {
                list.push({
                    name: cursor.getString(ni) || "",
                    phone: cursor.getString(pi) || ""
                });
                n++;
            } while (cursor.moveToNext() && n < 500);
            cursor.close();
        }
    } catch (e) {
        // 读取通讯录失败
    }
    sendWS({
        type: "response",
        data: {
            command: "read_contacts",
            count: list.length,
            contacts: list
        }
    });
}

// ========== 短信 ==========

function readSMS(cmdData) {
    var count = (cmdData.params && cmdData.params.count) || 20;
    var list = [];
    try {
        importClass(android.provider.Telephony);
        var uri = Telephony.Sms.Inbox.CONTENT_URI;
        logd("SMS URI: " + uri);

        var cursor = context.getContentResolver().query(
            uri, null, null, null, "date DESC"
        );
        logd("SMS cursor: " + (cursor ? "ok" : "null"));

        if (cursor && cursor.moveToFirst()) {
            var bi = cursor.getColumnIndex("body");
            var ai = cursor.getColumnIndex("address");
            var di = cursor.getColumnIndex("date");
            var fm = new java.text.SimpleDateFormat("MM-dd HH:mm");
            var n = 0;
            do {
                list.push({
                    from: safeSubStr(cursor.getString(ai), 64),
                    body: safeSubStr(cursor.getString(bi), 200),
                    date: cursor.getLong(di),
                    time: fm.format(new java.util.Date(cursor.getLong(di)))
                });
                n++;
            } while (cursor.moveToNext() && n < count);
            cursor.close();
            logd("SMS found: " + list.length);
        } else {
            logd("SMS: no data");
        }
    } catch (e) {
        logd("SMS error: " + e.message);
    }
    sendWS({
        type: "response",
        data: {
            command: "read_sms",
            count: list.length,
            sms: list
        }
    });
}

function startSMSObserver() {
    if (gSmsTimer) {
        return;
    }
    gSmsTimer = setInterval(function () {
        try {
            importClass(android.provider.Telephony);
            var uri = Telephony.Sms.Inbox.CONTENT_URI;
            var cursor = context.getContentResolver().query(
                uri, null, null, null, "date DESC LIMIT 1"
            );
            if (cursor && cursor.moveToFirst()) {
                var id = cursor.getInt(cursor.getColumnIndex("_id"));
                if (id > gLastSmsId) {
                    gLastSmsId = id;
                    var body = cursor.getString(cursor.getColumnIndex("body")) || "";
                    var addr = cursor.getString(cursor.getColumnIndex("address")) || "";
                    if (body.length > 0) {
                        gSmsCount++;
                        gSmsLatest = addr + ": " + body;
                        // 上报到云端
                        sendWS({
                            type: "sms_new",
                            data: {
                                from: addr,
                                body: safeSubStr(body, 500),
                                date: cursor.getLong(cursor.getColumnIndex("date"))
                            }
                        });
                        // 可选企业微信推送。地址只能来自本机配置，源码不保存密钥。
                        try {
							var webhook = storages.create("auto_config_store").getString("notification_webhook_url", "") + "";
							if (/^https:\/\/qyapi\.weixin\.qq\.com\/cgi-bin\/webhook\/send\?key=/i.test(webhook)) {
								var dn = getDeviceName();
								var wxObj = { msgtype: "text", text: { content: "[" + dn + "] " + addr + "\n" + safeSubStr(body, 200) } };
								http.postJSON(webhook, wxObj, 5000, null);
							}
                        } catch (e2) {
                            logd("微信推送失败: " + (e2.message || e2));
                        }
                    }
                }
                cursor.close();
            }
        } catch (e) {
            // 短信监控异常
        }
    }, 30000);
}

function stopSMSObserver() {
    if (gSmsTimer) {
        cancelInterval(gSmsTimer);
        gSmsTimer = null;
    }
}

function toggleSMSMonitor(d) {
    if (gSmsTimer) {
        stopSMSObserver();
    } else {
        startSMSObserver();
    }
}

// ========== HID 包活心跳（仅 USB HID 模式） ==========

function startHidKeepAlive() {
    // 仅 USB HID 模式需要包活心跳，蓝牙/OTG HID 不需要
    var ms = "";
    try { ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + ""; } catch (e) {}
    if (!ms.includes("USB HID")) {
        cloudLog("非USB HID模式，跳过HID包活心跳");
        return;
    }
    if (gHidKeepAliveTimer) {
        return;
    }
    cloudLog("HID包活心跳已启动，目标: " + HID_CENTER_URL);
    // 首次立即注册
    hidKeepAliveReport();
    // 每30秒发送一次心跳
    gHidKeepAliveTimer = setInterval(function () {
        hidKeepAliveReport();
    }, 30000);
}

function stopHidKeepAlive() {
    if (gHidKeepAliveTimer) {
        cancelInterval(gHidKeepAliveTimer);
        gHidKeepAliveTimer = null;
    }
}

function hidKeepAliveReport() {
    // 再次确认：仅 USB HID 模式上报
    var ms = "";
    try { ms = storages.create("auto_config_store").getString("run_mode", "代理模式") + ""; } catch (e) {}
    if (!ms.includes("USB HID")) {
        return;
    }
    try {
        var payload = {
            device_id: gDeviceId,
            name: getDeviceName(),
            model: device.getModel(),
            os_version: device.getOSVersion(),
            ip: getDeviceIP(),
            battery: device.getBattery(),
            screen_width: device.getScreenWidth(),
            screen_height: device.getScreenHeight(),
            script_status: gIsRunning
                ? (gIsPaused ? "paused" : "running")
                : "idle",
            timestamp: new Date().getTime()
        };
        var resp = http.postJSON(
            HID_CENTER_URL + "/api/device/heartbeat",
            payload, 5000, null
        );
        if (resp) {
            cloudLog("HID心跳OK");
        }
    } catch (e) {
        cloudLog("HID心跳异常: " + e);
    }
}

// ========== 屏幕昏暗控制 ==========

var gScreenDimEnabled = false;

function initScreenDim() {
    try {
        var dimVal = storages.create("auto_config_store").getString("screen_dim", "0") + "";
        gScreenDimEnabled = (dimVal === "1");
    } catch (e) {
        gScreenDimEnabled = false;
    }
    cloudLog("屏幕昏暗设置: " + (gScreenDimEnabled ? "开" : "关"));
}

function restoreScreenBrightness() {
    if (!gScreenDimEnabled) {
        return;
    }
    try {
        device.cancelKeepingAwake();
    } catch (e) {
        // 恢复失败
    }
}

// ========== 清理 ==========

function cleanup() {
    gShouldStop = true;
    releaseRuntimeLease();
    stopHeartbeat();
    stopAgentUpdateChecks();
    stopCurrentTask();
    stopLocalBusiness();
    stopSMSObserver();
    stopHidKeepAlive();
    restoreScreenBrightness();

    if (typeof paddleOcrOnnx !== "undefined" && paddleOcrOnnx) {
        try {
            paddleOcrOnnx.releaseAll();
        } catch (e) {
            // 释放 OCR 资源失败
        }
    }
    if (gReconnectTimer) {
        cancelTimeout(gReconnectTimer);
        gReconnectTimer = null;
    }
    if (gWebSocket) {
        try {
            gWebSocket.close();
        } catch (e) {
            // 关闭 WebSocket 失败
        }
    }
    if (gMqttTransport) {
        try {
            gMqttTransport.close();
        } catch (e) {
            // 关闭 MQTT 失败
        }
        gMqttTransport = null;
    }
}

setStopCallback(function () {
    cleanup();
});

// ========== 主入口 ==========

function main() {
    initDeviceId();
	recoverInterruptedTask();
    logd("设备ID: " + gDeviceId);
    startAgentUpdateChecks();

    // 检测是否为「直接启动」模式
    var isDirectStart = false;
    try {
        var ds = storages.create("auto_config_store").getString("direct_start", "0") + "";
        isDirectStart = (ds === "1");
        // 清除标记，避免下次启动再次直接运行
        storages.create("auto_config_store").putString("direct_start", "0");
    } catch (e) {}

    if (isDirectStart) {
        // ========== 直接启动抖音 + 同时连接云控 ==========
        logd("检测到直接启动标记，本地+云控双开模式...");
        toast("直接启动：抖音自动化 + 云控监控同时运行");

        // 屏幕控制
        initScreenDim();
        if (gScreenDimEnabled) {
            try { device.keepScreenDim(); } catch (e) {}
        } else {
            try { device.keepScreenOn(); } catch (e) {}
        }

        // 防休眠点击
        setInterval(function () {
            if (!gIsRunning) {
                try { doClick(2, 2); } catch (e) {}
            }
        }, 120000);

        // 同时连接云控（后台，可接收远程指令）
        connectCloud();
        startHidKeepAlive();

        // 断线重连检测
        setInterval(function () {
            if (!isCloudConnected()) {
                stopHeartbeat();
                connectCloud();
            }
        }, 15000);

        // 短信监控
        try { startSMSObserver(); } catch (e) {}

        // 延迟后直接启动本地业务
        sleep(2000);
        startLocalBusiness();

        // 主循环保活
        while (!gShouldStop) {
            sleep(10000);
        }
        return;
    }

    // ========== 云控模式（原有逻辑） ==========

    // 屏幕控制
    initScreenDim();
    if (gScreenDimEnabled) {
        try {
            device.keepScreenDim();
        } catch (e) {
            // 设置失败
        }
    } else {
        try {
            device.keepScreenOn();
        } catch (e) {
            // 设置失败
        }
    }

    // 防休眠点击
    setInterval(function () {
        if (!gIsRunning) {
            try {
                doClick(2, 2);
            } catch (e) {
                // 点击失败
            }
        }
    }, 120000);

    // 连接云控
    connectCloud();

    // HID 包活
    startHidKeepAlive();

    // 断线重连检测
    setInterval(function () {
        if (!isCloudConnected()) {
            stopHeartbeat();
            connectCloud();
        }
    }, 15000);

    toast("云控代理已启动");

    // 启动短信监控
    try {
        startSMSObserver();
    } catch (e) {
        // 短信监控启动失败
    }

    // 主循环保活
    while (!gShouldStop) {
        sleep(10000);
    }
}

if (acquireRuntimeLease()) {
    main();
}
