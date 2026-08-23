/**
 * 云控设备代理 - UI 逻辑
 */
function main() {
    // ========== 模式定义 ==========
    var MODES = ["USB HID", "蓝牙 HID", "OTG HID", "代理模式", "无障碍"];

    // ========== 提前读取上次保存的设置 ==========
    var savedMode = "代理模式";
    var savedName = "";
    var savedServer = "";
    var savedDim = false;
    try { var s = storages.create("auto_config_store"); savedMode = s.getString("run_mode", "代理模式") + ""; savedName = s.getString("device_name", "") + ""; savedServer = s.getString("cloud_server_address", "") + ""; savedDim = s.getString("screen_dim", "0") === "1"; } catch (e) {}
    if (!savedMode || savedMode === "null" || savedMode === "undefined" || savedMode === "") savedMode = "代理模式";
    if (!savedName || savedName === "null" || savedName === "undefined") savedName = "";
    if (!savedServer || savedServer === "null" || savedServer === "undefined") savedServer = "";
    // 兼容旧数据：旧的 "HID模式" 映射到 "USB HID"
    if (savedMode === "HID模式") savedMode = "USB HID";
    // 确保在 MODES 中
    var modeIndex = MODES.indexOf(savedMode);
    if (modeIndex === -1) { modeIndex = 3; savedMode = "代理模式"; }

    // ========== 布局 ==========
    ui.layout("云控配置", "main.xml");
    ui.resetUIVar();

    // ========== 恢复设备名称 ==========
    if (savedName) try { ui.device_name.setText(savedName); } catch (e) {}
    if (savedServer) try { ui.server_address.setText(savedServer); } catch (e) {}
    // 没保存过 → 尝试读取手机号，预填到输入框
    if (!savedName) {
        try {
            importClass(android.telephony.TelephonyManager);
            var tm = context.getSystemService(android.content.Context.TELEPHONY_SERVICE);
            var num = tm.getLine1Number();
            if (num && String(num).length > 0) {
                ui.device_name.setText(String(num));
                savedName = String(num);
            }
        } catch (e) {}
    }

    // ========== 从存储同步模式（仅初始化/onResume时用） ==========
    function syncModeFromStorage() {
        try {
            var s = storages.create("auto_config_store");
            var m = s.getString("run_mode", "代理模式") + "";
            if (m === "HID模式") m = "USB HID";
            if (m && m !== "null" && m !== "undefined" && MODES.indexOf(m) !== -1) {
                savedMode = m;
                modeIndex = MODES.indexOf(m);
            }
        } catch (e) {}
    }

    // ========== 刷新按钮显示（不覆盖变量） ==========
    function refreshModeBtn() {
        ui.modeBtn.setText("当前: " + savedMode + " (点击切换)");
    }

    // 初始化：从存储同步 + 显示
    syncModeFromStorage();
    refreshModeBtn();

    // ========== 恢复昏暗按钮 ==========
    function refreshDimBtn() {
        ui.dimBtn.setText("屏幕昏暗: " + (savedDim ? "开" : "关") + " (点击切换)");
    }
    refreshDimBtn();

    // ========== Activity 生命周期 ==========
    ui.onActivityEvent("onResume", function () {
        // 从存储同步（纠正可能的缓存不一致）
        syncModeFromStorage();
        refreshModeBtn();
        refreshDimBtn();
        // 仅代理/无障碍模式需要启动自动化服务，HID 模式不需要
        if (!savedMode.includes("HID") && !savedMode.includes("无障碍") && !ui.isServiceOk()) {
            ui.startEnvAsync(function () {});
        }
        if (savedMode.includes("无障碍") && !ui.isServiceOk()) {
            ui.startEnvAsync(function () {});
        }
    });

    // ========== 保存参数 ==========
    function doSave() {
        var msg = [];
        var serverVal = "";
        try { serverVal = String(ui.server_address.getText() || "").trim(); } catch (e) {}
        if (!serverVal) {
            toast("请先填写云控服务器局域网 IP");
            return false;
        }
        try {
            storages.create("auto_config_store").putString("cloud_server_address", serverVal);
            savedServer = serverVal;
            msg.push("云控: " + serverVal);
        } catch (e) { logd("保存云控地址失败: " + e); return false; }
        try {
            storages.create("auto_config_store").putString("run_mode", savedMode);
            msg.push("模式: " + savedMode);
        } catch (e) { logd("保存模式失败: " + e); }
        try {
            storages.create("auto_config_store").putString("screen_dim", savedDim ? "1" : "0");
            msg.push("昏暗: " + (savedDim ? "开" : "关"));
        } catch (e) {}
        try {
            var nameVal = ui.device_name.getText();
            if (nameVal) { storages.create("auto_config_store").putString("device_name", nameVal + ""); savedName = nameVal + ""; msg.push("名称: " + nameVal); }
        } catch (e) {}
        toast("已保存 " + (msg.length > 0 ? msg.join(", ") : ""));
        logd("保存完成: " + JSON.stringify(msg));
        return true;
    }

    // ========== 模式切换：点一下切到下一个 ==========
    ui.setEvent(ui.modeBtn, "click", function () {
        modeIndex = (modeIndex + 1) % MODES.length;
        savedMode = MODES[modeIndex];
        refreshModeBtn();
        doSave();
    });

    // ========== 昏暗切换：点一下开/关 ==========
    ui.setEvent(ui.dimBtn, "click", function () {
        savedDim = !savedDim;
        refreshDimBtn();
        doSave();
    });

    ui.setEvent(ui.saveAllBtn, "click", doSave);
    ui.setEvent(ui.systemSetting, "click", function () { ui.openECSystemSetting(); });

    // ========== 启动脚本（云控模式） ==========
    function doStart() {
        if (!doSave()) return;
        // 清除直接启动标记，确保走云控模式
        try { storages.create("auto_config_store").putString("direct_start", "0"); } catch (e) {}
        toast("正在启动云控代理...");
        ui.start();
    }
    ui.setEvent(ui.startBtn, "click", doStart);

    // ========== 直接启动抖音（本地模式，跳过云控） ==========
    function doDirectStart() {
        if (!doSave()) return;
        // 设置直接启动标记
        try { storages.create("auto_config_store").putString("direct_start", "1"); } catch (e) {}
        toast("正在直接启动抖音自动化...");
        ui.start();
    }
    ui.setEvent(ui.directStartBtn, "click", doDirectStart);
}

main();
