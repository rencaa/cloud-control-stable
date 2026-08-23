/*================================================================
 * 抖音业务函数 - 从 xs 项目迁移
 * 依赖 common.js 中的工具函数（OCR/HID/滑动/配置等）
 *================================================================*/

const DOUBAO_API_KEY = "e51a001c-3a65-42da-b20d-e8d72baabc27";
const DOUBAO_API_URL = "https://ark.cn-beijing.volces.com/api/v3/chat/completions";
const DOUBAO_MODEL = "doubao-seed-character-251128";

let _cachedKeywords = [];
let _cacheTheme = "";

/*-------------------- 业务入口（被云控代理调用）--------------------*/

function startDouyinMain() {
	commonLogToCenter(true);
	report("========================================");
	report("抖音自动化脚本启动，中控日志已开启");
	report("========================================");
	loadCloudConfig();
	report("========== 开始执行主流程 ==========");
	hidcs();
	report("========== 主流程执行完毕 ==========");
}

/*-------------------- 进入直播间 --------------------*/

function sjzbj2(keyword) {
	if (!keyword) { keyword = getRandomKeyword(); }
	report("========== 开始进入直播间流程 ==========");
	report("搜索关键词: " + keyword);
	report("先返回首页确保状态正确...");
	doHome();
	sleep(2000);
	var map2 = { "uri": "snssdk1128://search?keyword=" + keyword + "&search_tab=live" };
	var y = utils.openActivity(map2);
	report("搜索页面已打开（定位到直播标签），等待加载...");
	sleep(random(3000, 5000));
	sleep(2000);
	report("搜索结果加载完成");
	let enteredLiveRoom = false;
	report("直播标签搜索区域: (" + LIVE_CONFIG.VIDEO_TAB_X1 + "," + LIVE_CONFIG.VIDEO_TAB_Y1 + ") - (" + LIVE_CONFIG.VIDEO_TAB_X2 + "," + LIVE_CONFIG.VIDEO_TAB_Y2 + ")");
	for (let attempt = 1; attempt <= 3; attempt++) {
		report("第" + attempt + "次尝试进入直播间...");
		let foundLive = ocrFuncpddj("直播", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2);
		if (foundLive) {
			report("✓ 已找到并点击直播标签");
			let waitTime = random(5000, 8000);
			report("等待" + (waitTime / 1000) + "秒后点击视频区域...");
			sleep(waitTime);
			clickVideoArea();
			sleep(random(4000, 6000));
			if (checkLiveRoomStatus()) { report("✓ 已成功进入直播间"); enteredLiveRoom = true; }
			break;
		} else { report("未找到'直播'标签"); sleep(random(3000, 5000)); }
		sleep(2000);
	}
	report("检查是否成功进入直播间...");
	sleep(3000);
	let isLiveRoom = checkLiveRoomStatus();
	if (!isLiveRoom) {
		report("未检测到直播间特征，尝试从右往左滑动切换标签...");
		swipeRightToLeft();
		sleep(2000);
		let swipeFound = ocrFuncpddj("直播", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2);
		if (swipeFound) {
			report("✓ 滑动后找到并点击直播标签");
			let waitTime = random(5000, 8000);
			sleep(waitTime);
			clickVideoArea();
			sleep(random(4000, 6000));
			if (checkLiveRoomStatus()) { report("✓ 滑动后成功进入直播间"); enteredLiveRoom = true; }
		} else {
			report("滑动后仍未找到直播标签，切换关键词重新搜索");
			keyword = getRandomKeyword();
			doHome();
			sleep(2000);
			utils.openActivity({ uri: "snssdk1128://search?keyword=" + keyword + "&search_tab=live" });
			sleep(random(5000, 8000));
			let retryFound = ocrFuncpd("直播", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2);
			if (!retryFound) { report("仍未找到直播标签，返回首页刷推荐页"); goToDouyinHome(); return; }
			sleep(random(3000, 5000));
			clickVideoArea();
			sleep(random(4000, 6000));
			if (!checkLiveRoomStatus()) { report("仍未进入直播间，返回首页刷推荐页"); goToDouyinHome(); return; }
			enteredLiveRoom = true;
		}
	}
	report("✓ 已成功进入直播间");
	if (LIVE_CONFIG.ENABLE_FOLLOW) {
		let roll = random(1, 100);
		let followChance = LIVE_CONFIG.PROBABILITY_FOLLOW;
		if (roll <= followChance) {
			report("决定关注主播（概率" + roll + "/" + followChance + "）");
			sleep(random(5000, 10000));
			let followed = ocrFuncpddj("关注", LIVE_CONFIG.FOLLOW_X1, LIVE_CONFIG.FOLLOW_Y1, LIVE_CONFIG.FOLLOW_X2, LIVE_CONFIG.FOLLOW_Y2);
			if (followed) { report("✓ 关注操作成功"); sleep(2000); }
			else { report("未找到关注按钮"); }
		} else { report("跳过关注（概率" + roll + "/" + followChance + "）"); }
	}
	report("========== 进入直播间流程完成 ==========");
	return keyword;
}

/*-------------------- 直播间状态检测 --------------------*/

function checkLiveRoomStatus() {
	report("开始检测直播间状态...");
	let { scaleX, scaleY } = getScreenScale();
	report("尝试图片识别判断直播间...");
	let imgCheckX1 = Math.round(LIVE_IMAGE_CONFIG.CHECK_X1 * scaleX);
	let imgCheckY1 = Math.round(LIVE_IMAGE_CONFIG.CHECK_Y1 * scaleY);
	let imgCheckX2 = Math.round(LIVE_IMAGE_CONFIG.CHECK_X2 * scaleX);
	let imgCheckY2 = Math.round(LIVE_IMAGE_CONFIG.CHECK_Y2 * scaleY);
	try {
		let templateImg = readResAutoImage(LIVE_IMAGE_CONFIG.IMAGE_NAME);
		if (templateImg) {
			let img;
			let _modeStr = (storages.create("auto_config_store").getString("run_mode", "代理模式") + "");
			if (_modeStr.includes("代理")) { img = image.captureFullScreenEx(); }
			else { img = image.captureFullScreen(); }
			if (img) {
				let rect = new Rect();
				rect.left = imgCheckX1; rect.top = imgCheckY1; rect.right = imgCheckX2; rect.bottom = imgCheckY2;
				let result = image.matchTemplate(img, templateImg, LIVE_IMAGE_CONFIG.THRESHOLD, LIVE_IMAGE_CONFIG.THRESHOLD, rect, -1, 1, 5);
				image.recycle(img);
				if (result && result.length > 0) { image.recycle(templateImg); return true; }
			}
			image.recycle(templateImg);
		}
	} catch (e) {}
	let checkY1 = Math.round(2700 * scaleY);
	let checkY2 = Math.round(3200 * scaleY);
	let checkX1 = Math.round(0 * scaleX);
	let checkX2 = Math.round(1440 * scaleX);
	let keywords = LIVE_CONFIG.LIVE_KEYWORDS;
	let foundCount = 0;
	for (let i = 0; i < keywords.length; i++) {
		let kw = keywords[i];
		if (ocrFuncpd(kw, checkX1, checkY1, checkX2, checkY2)) { foundCount++; if (foundCount >= 2) return true; }
	}
	if (foundCount === 1) return true;
	for (let i = 0; i < keywords.length; i++) {
		if (ocrFuncpd(keywords[i], 0, 0, checkX2, checkY2)) return true;
	}
	return false;
}

/*-------------------- 关键词/评论生成 --------------------*/

function getRandomKeyword() {
	try { let kw = generateKeywordFromCloud(); if (kw && kw.trim()) return kw; } catch (e) {}
	let keywords = CONFIG.keywords;
	if (!keywords || keywords.length === 0) { keywords = ["动漫", "海蓝之谜", "sk神仙水", "香奈儿", "黄金", "资生堂", "雅思兰黛", "传奇"]; }
	return keywords[random(0, keywords.length - 1)];
}

/*-------------------- AI 评论生成 --------------------*/

// 随机人格池
var AI_PERSONAS = [
    { name: "活泼大学生",  prompt: "你是一个活泼开朗的大学生，刷抖音看到好玩的就忍不住评论。说网络用语，偶尔用emoji，像日常聊天。" },
    { name: "摸鱼打工人",  prompt: "你是一个上班摸鱼的打工人，刷抖音打发时间。评论随意自然，有时吐槽有时真心夸。" },
    { name: "深夜冲浪人",  prompt: "你是一个熬夜刷手机的夜猫子，深夜情绪多，评论偶尔emo偶尔搞笑，话不多但有共鸣。" },
    { name: "热情阿姨",    prompt: "你是一个热情的中年阿姨，喜欢发点赞加油正能量评论，偶尔用🌸🌹💪等表情，语气亲切。" },
    { name: "高冷路人",    prompt: "你是一个高冷路人，评论极度简短，不超过10个字，从不废话，但偶尔一针见血。" },
    { name: "社牛话痨",    prompt: "你是一个社牛，看到什么都要说两句，爱用感叹号，语气夸张但不做作，15-25字。" },
    { name: "佛系潜水党",  prompt: "你是一个佛系潜水党，平时不评论，偶尔冒一句。评论很淡定，看破红尘的语气。" },
    { name: "追星少女",    prompt: "你是一个追星少女，看到喜欢的内容就疯狂打call，语气激动，爱用啊啊啊和感叹号。" }
];

// 随机语气标签
var AI_TONES = [
    "随意自然，像跟朋友聊天",
    "略微兴奋，好像看到了感兴趣的内容",
    "带点幽默调侃",
    "真诚地在夸",
    "简单直接不废话",
    "带一点小情绪"
];

/**
 * 纯 AI 生成评论（随机人格 + 随机语气）
 * @param {string} keyword - 可选的关键词上下文
 * @returns {string|null}
 */
function generateAIComment(keyword) {
    // 轮换人格，避免同一个"人"连发多条
    var persona = AI_PERSONAS[random(0, AI_PERSONAS.length - 1)];
    var tone = AI_TONES[random(0, AI_TONES.length - 1)];

    // 时间段上下文
    var now = new Date();
    var hour = now.getHours();
    var timeCtx = hour < 6 ? "凌晨" : hour < 9 ? "早上" : hour < 12 ? "上午"
                : hour < 14 ? "中午" : hour < 18 ? "下午" : hour < 22 ? "晚上" : "深夜";

    // 随机决定是否带 emoji
    var emojiHint = random(1, 100) <= 40 ? "可以适当加1个emoji。" : "不要加emoji。";

    var prompt = "现在是" + timeCtx + "。写一条抖音评论区留言。";
    if (keyword) { prompt += "内容围绕" + keyword + "相关话题。"; }
    prompt += "风格：" + tone + "。" + emojiHint + "像真人随手发的，不要AI味，不要引号。";

    var requestData = {
        model: DOUBAO_MODEL,
        messages: [
            { role: "system", content: persona.prompt },
            { role: "user", content: prompt }
        ]
    };
    var headers = { "Content-Type": "application/json", "Authorization": "Bearer " + DOUBAO_API_KEY };

    try {
        var response = http.postJSON(DOUBAO_API_URL, requestData, 15 * 1000, headers);
        if (response) {
            var result = JSON.parse(response);
            if (result && result.choices && result.choices.length > 0) {
                var comment = result.choices[0].message.content.trim();
                // 清理引号和换行
                comment = comment.replace(/["""'']/g, "").replace(/\n/g, " ").trim();
                if (comment.length > 60) comment = comment.substring(0, 60);
                if (comment.length > 0) {
                    report("AI评论[" + persona.name + "][" + tone + "]: " + comment);
                    return comment;
                }
            }
        }
    } catch (e) {
        report("AI评论生成异常: " + e);
    }
    return null;
}

// 纯 AI 兜底池（AI 挂了才用，保持最小化）
var AI_FALLBACK = [
    "说的太对了", "笑死我了哈哈", "学到了学到了", "这个真不错",
    "有被戳到", "一模一样", "这不就是我吗", "太真实了",
    "厉害了", "真的假的", "好家伙", "绝了", "牛的"
];

/**
 * 获取评论（纯 AI，失败则兜底）
 * @param {string} keyword
 * @returns {string}
 */
function getSmartComment(keyword) {
    var ai = generateAIComment(keyword);
    if (ai && ai.trim()) return ai;
    // AI 挂了，兜底
    var fb = AI_FALLBACK[random(0, AI_FALLBACK.length - 1)];
    report("AI失败，兜底评论: " + fb);
    return fb;
}

function generateKeywordFromCloud() {
	let themesRaw = CONFIG.keywordThemes;
	if (!themesRaw) return null;
	let themes = ("" + themesRaw).split(",").map(function(s) { return s.trim(); }).filter(function(s) { return s.length > 0; });
	if (themes.length === 0) return null;
	if (_cachedKeywords.length === 0) {
		let theme = themes[random(0, themes.length - 1)];
		_cacheTheme = theme;
		let prompt = "生成5个抖音上搜索'" + _cacheTheme + "'视频会用到的关键词，包括热门名称、角色名、相关话题。每行一个，不要编号、空行或任何解释，只输出关键词。";
		let requestData = { "model": DOUBAO_MODEL, "messages": [{"role": "user", "content": prompt}] };
		let headers = { "Content-Type": "application/json", "Authorization": "Bearer " + DOUBAO_API_KEY };
		let response = http.postJSON(DOUBAO_API_URL, requestData, 30 * 1000, headers);
		if (response) {
			try {
				let result = JSON.parse(response);
				if (result.choices && result.choices.length > 0) {
					let raw = result.choices[0].message.content.trim();
					_cachedKeywords = raw.split("\n").map(function(s) { return s.trim(); }).filter(function(s) { return s.length > 0; });
				}
			} catch (e) {}
		}
	}
	if (_cachedKeywords.length === 0) return null;
	let idx = random(0, _cachedKeywords.length - 1);
	let kw = _cachedKeywords[idx];
	_cachedKeywords.splice(idx, 1);
	return kw;
}

function callDoubaoAI(keyword) {
	let prompt = "帮我生成一条抖音直播间评论，";
	if (keyword) { prompt += "内容和" + keyword + "相关，"; }
	prompt += "要非常口语化，就像真实用户随手发的一样，可以带点语气词或表情，不要太正式，长度控制在10-20字左右。";
	let requestData = { "model": DOUBAO_MODEL, "messages": [{"role": "system", "content": "你是一个普通的抖音用户，性格活泼，喜欢在直播间和主播互动。说话方式很随意、很真实，就像日常聊天一样。"}, {"role": "user", "content": prompt}] };
	let headers = { "Content-Type": "application/json", "Authorization": "Bearer " + DOUBAO_API_KEY };
	let response = http.postJSON(DOUBAO_API_URL, requestData, 30 * 1000, headers);
	if (response) {
		try {
			let result = JSON.parse(response);
			if (result && result.choices && result.choices.length > 0) {
				let comment = result.choices[0].message.content.trim();
				if (comment.length > 50) comment = comment.substring(0, 50) + "...";
				return comment;
			}
		} catch (e) {}
	}
	return null;
}

	function sjzbj() { var url = "https://netmo99.cn/api/"; var x = http.httpGetDefault(url, 30 * 1000, {"User-Agent": "test"}); report(x); var map2 = { "uri": "snssdk1128://live?room_id=" + x }; utils.openActivity(map2); }

/*-------------------- 时间段控制 --------------------*/

/**
 * 确保当前在活跃时间段内（默认 7:00-23:00），否则休眠等待
 * @returns {boolean} true=可以继续
 */
function ensureActive() {
    if (isActiveHours()) {
        return true;
    }
    var sleepMin = Math.round(getSleepSeconds() / 60);
    report("当前不在活跃时段(7:00-23:00)，休眠约" + sleepMin + "分钟...");
    doHome();
    sleep(3000);
    // 分段休眠，每5分钟醒来检查一次
    while (!isActiveHours()) {
        sleep(5 * 60 * 1000);
    }
    report("进入活跃时段，准备启动抖音...");
    doHome();
    sleep(2000);
    utils.openAppByName("抖音");
    sleep(TIME.APP_START);
    return true;
}

/*-------------------- 主循环：支持云端 mode 模式选择 --------------------*/

function hidcs() {
	var mode = LIVE_CONFIG.MODE;
	report("当前运行模式: " + mode);

	report("正在返回主页...");
	doHome();
	sleep(TIME.APP_START);
	report("正在启动抖音...");
	utils.openAppByName("抖音");
	sleep(TIME.APP_START);

	var liveRoomCount = 0, keywordVideoCount = 0;
	try {
		var st0 = storages.create("auto_config_store");
		liveRoomCount = parseInt(st0.getString("daily_live_rooms", "0")) || 0;
		keywordVideoCount = parseInt(st0.getString("daily_keywords", "0")) || 0;
	} catch(e) {}

	var maxLiveRooms = LIVE_CONFIG.ENABLE_LIVE_ROOM ? LIVE_CONFIG.LIVE_DAILY_LIMIT : 0;
	report("刷视频: 点赞" + (LIVE_CONFIG.ENABLE_LIKE ? "开" : "关") + " 评论" + (LIVE_CONFIG.ENABLE_COMMENT ? "开" : "关") + " | 直播: " + (LIVE_CONFIG.ENABLE_LIVE_ROOM ? "开" : "关") + " 计划" + maxLiveRooms + "个(今日已" + liveRoomCount + ")");
	var maxKeywordVideos = LIVE_CONFIG.ENABLE_KEYWORD ? LIVE_CONFIG.KW_DAILY_LIMIT : 0;
	report("刷关键词: " + (LIVE_CONFIG.ENABLE_KEYWORD ? "开启" : "关闭") + " 计划" + maxKeywordVideos + "次(今日已" + keywordVideoCount + ")");

	var noHomeCount = 0;
	var maxNoHomeRetry = 3;
	var scriptStartTime = new Date().getTime();
	var runTimeMs = LIVE_CONFIG.RUN_TIME_MIN * 60 * 1000;

		// ========== recommend 模式：只刷推荐页 ==========
		if (mode === "recommend") {
			report("========== 推荐页模式启动 =========              =");
			while (true) {
				// 时间段检查：7:00-23:00 才活跃
				ensureActive();
				// 检查运行时长
			if (runTimeMs > 0 && (new Date().getTime() - scriptStartTime) >= runTimeMs) {
				report("已达运行时长(" + LIVE_CONFIG.RUN_TIME_MIN + "分钟)，退出");
				stopDouyinAndReturnHome();
				return;
			}
			// 确保在首页
			var isHome = false;
			for (var i = 0; i < 5; i++) {
				if (ocrFuncpddj("首页", COMMENT_CONFIG.HOME_CHECK_X1, COMMENT_CONFIG.HOME_CHECK_Y1, COMMENT_CONFIG.HOME_CHECK_X2, COMMENT_CONFIG.HOME_CHECK_Y2)) {
					isHome = true;
					noHomeCount = 0;
					break;
				}
				doBack();
				sleep(TIME.BACK_DELAY);
			}
			if (!isHome) {
				noHomeCount++;
				if (noHomeCount >= maxNoHomeRetry) {
					restartDouyin();
					noHomeCount = 0;
					sleep(5000);
					continue;
				}
			}
			// 刷视频
			if (isHome) {
				performSwipe();
				sleep(random(TIME.VIDEO_VIEW_MIN, TIME.VIDEO_VIEW_MAX));
			}
			// 随机点赞
			if (LIVE_CONFIG.ENABLE_LIKE && random(1, 100) <= LIVE_CONFIG.PROBABILITY_LIKE) {
				performLike();
			}
                // 随机看评论
                if (LIVE_CONFIG.ENABLE_COMMENT && random(1, 100) <= LIVE_CONFIG.PROBABILITY_COMMENT) {
                    performCommentAction();
                }
                // 随机发评论
                if (LIVE_CONFIG.ENABLE_COMMENT && random(1, 100) <= LIVE_CONFIG.PROBABILITY_POST_COMMENT) {
                    performPostComment();
                }
                // 随机点关注
                if (LIVE_CONFIG.ENABLE_FOLLOW_VIDEO && random(1, 100) <= LIVE_CONFIG.FOLLOW_PROB) {
                    performFollow();
                }
                // 随机休息（短）
                if (random(1, 100) <= 3) {
                    var restTime = random(30, 60);
                    report("休息" + restTime + "秒（模拟真人疲劳）");
                    sleep(restTime * 1000);
                }
                // 随机长休息（模拟放下手机：回桌面，可能打开别的app）
                if (random(1, 100) <= 2) {
                    var longRest = random(120, 900);
                    var longRestMin = Math.round(longRest / 60);
                    report("随机长休息" + longRestMin + "分钟（模拟放下手机）");
                    doHome();
                    sleep(1000);
                    // 随机打开一个别的app再回桌面（更真实）
                    if (random(1, 100) <= 50) {
                        var otherApps = ["微信", "QQ", "浏览器", "相册", "设置", "微博", "淘宝", "支付宝"];
                        var app = otherApps[random(0, otherApps.length - 1)];
                        report("打开" + app + "再退出");
                        try { utils.openAppByName(app); sleep(random(3000, 8000)); } catch (e) {}
                    }
                    doHome();
                    sleep(longRest * 1000);
                    // 休息完重启抖音
                    utils.openAppByName("抖音");
                    sleep(TIME.APP_START);
                    noHomeCount = 0;
                }
            }
        }

	        // ========== live 模式：只刷直播间 ==========
	        else if (mode === "live") {
			report("========== 直播间模式启动 ==========");
			while (true) {
				// 时间段检查：7:00-23:00 才活跃
				ensureActive();
				// 检查运行时长
			if (runTimeMs > 0 && (new Date().getTime() - scriptStartTime) >= runTimeMs) {
				report("已达运行时长(" + LIVE_CONFIG.RUN_TIME_MIN + "分钟)，退出");
				stopDouyinAndReturnHome();
				return;
			}
			// 检查每日上限
			if (liveRoomCount >= maxLiveRooms) {
				report("已达直播间每日上限(" + maxLiveRooms + ")，退出");
				stopDouyinAndReturnHome();
				return;
			}
			// 进入直播间
			performLiveRoomAction();
			liveRoomCount++;
			try {
				storages.create("auto_config_store").putString("daily_live_rooms", "" + liveRoomCount);
			} catch(e) {}
			report("直播间进度: " + liveRoomCount + "/" + maxLiveRooms);
			// 间隔休息
			var restTime = random(30, 120);
			report("休息" + restTime + "秒后进入下一个直播间...");
			sleep(restTime * 1000);
		}
	}

		// ========== keyword 模式：只搜关键词刷视频 ==========
		else if (mode === "keyword") {
			report("========== 关键词视频模式启动 ==========");
			while (true) {
				// 时间段检查：7:00-23:00 才活跃
				ensureActive();
				// 检查运行时长
			if (runTimeMs > 0 && (new Date().getTime() - scriptStartTime) >= runTimeMs) {
				report("已达运行时长(" + LIVE_CONFIG.RUN_TIME_MIN + "分钟)，退出");
				stopDouyinAndReturnHome();
				return;
			}
			// 检查每日上限
			if (keywordVideoCount >= maxKeywordVideos) {
				report("已达关键词每日上限(" + maxKeywordVideos + ")，退出");
				stopDouyinAndReturnHome();
				return;
			}
			// 搜关键词刷视频
			performKeywordVideoAction();
			keywordVideoCount++;
			try {
				storages.create("auto_config_store").putString("daily_keywords", "" + keywordVideoCount);
			} catch(e) {}
			report("关键词进度: " + keywordVideoCount + "/" + maxKeywordVideos);
			// 间隔休息
			var restTime = random(30, 120);
			report("休息" + restTime + "秒后搜索下一个关键词...");
			sleep(restTime * 1000);
		}
	}

		// ========== all 模式（默认）：保持原有逻辑 ==========
		else {
			report("========== 全功能模式启动 ==========");
			while (true) {
				// 时间段检查：7:00-23:00 才活跃
				ensureActive();
				if (runTimeMs > 0 && (new Date().getTime() - scriptStartTime) >= runTimeMs) {
				report("已达运行时长(" + LIVE_CONFIG.RUN_TIME_MIN + "分钟)，退出");
				stopDouyinAndReturnHome();
				return;
			}
			var isHome = false;
			for (var i = 0; i < 5; i++) {
				if (ocrFuncpddj("首页", COMMENT_CONFIG.HOME_CHECK_X1, COMMENT_CONFIG.HOME_CHECK_Y1, COMMENT_CONFIG.HOME_CHECK_X2, COMMENT_CONFIG.HOME_CHECK_Y2)) {
					isHome = true;
					noHomeCount = 0;
					break;
				}
				doBack();
				sleep(TIME.BACK_DELAY);
			}
			if (!isHome) {
				noHomeCount++;
				if (noHomeCount >= maxNoHomeRetry) {
					restartDouyin();
					noHomeCount = 0;
					sleep(5000);
					continue;
				}
			}
			if (isHome && LIVE_CONFIG.ENABLE_RECOMMEND) {
				performSwipe();
				sleep(random(TIME.VIDEO_VIEW_MIN, TIME.VIDEO_VIEW_MAX));
			}
			if (LIVE_CONFIG.ENABLE_RECOMMEND && LIVE_CONFIG.ENABLE_LIKE && random(1, 100) <= LIVE_CONFIG.PROBABILITY_LIKE) {
				performLike();
			}
            if (LIVE_CONFIG.ENABLE_RECOMMEND && LIVE_CONFIG.ENABLE_COMMENT && random(1, 100) <= LIVE_CONFIG.PROBABILITY_COMMENT) {
                performCommentAction();
            }
            // 随机发评论
            if (LIVE_CONFIG.ENABLE_RECOMMEND && LIVE_CONFIG.ENABLE_COMMENT && random(1, 100) <= LIVE_CONFIG.PROBABILITY_POST_COMMENT) {
                performPostComment();
            }
            if (LIVE_CONFIG.ENABLE_FOLLOW_VIDEO && random(1, 100) <= LIVE_CONFIG.FOLLOW_PROB) {
                performFollow();
            }
            if (LIVE_CONFIG.ENABLE_LIVE_ROOM && random(1, 100) <= 20 && liveRoomCount < maxLiveRooms) {
				performLiveRoomAction();
				liveRoomCount++;
				try {
					storages.create("auto_config_store").putString("daily_live_rooms", "" + liveRoomCount);
				} catch(e) {}
				if (liveRoomCount >= maxLiveRooms) {
					report("已达直播间上限");
				}
			}
			if (LIVE_CONFIG.ENABLE_KEYWORD && random(1, 100) <= 30 && keywordVideoCount < maxKeywordVideos) {
				performKeywordVideoAction();
				keywordVideoCount++;
				try {
					storages.create("auto_config_store").putString("daily_keywords", "" + keywordVideoCount);
				} catch(e) {}
				if (keywordVideoCount >= maxKeywordVideos) {
					report("已达关键词上限");
				}
			}
			if (!LIVE_CONFIG.ENABLE_RECOMMEND && !LIVE_CONFIG.ENABLE_LIVE_ROOM && !LIVE_CONFIG.ENABLE_KEYWORD) {
				report("所有功能已关闭，休眠30秒...");
				sleep(30000);
				continue;
			}
				if (LIVE_CONFIG.ENABLE_RECOMMEND && random(1, 100) <= 3) {
					var restTime = random(30, 60);
					report("休息" + restTime + "秒（模拟真人疲劳）");
					sleep(restTime * 1000);
				}
				// 随机长休息（模拟放下手机：回桌面，可能打开别的app）
				if (random(1, 100) <= 2) {
					var longRest = random(120, 900);
					var longRestMin = Math.round(longRest / 60);
					report("随机长休息" + longRestMin + "分钟（模拟放下手机）");
					doHome();
					sleep(1000);
					if (random(1, 100) <= 50) {
						var otherApps = ["微信", "QQ", "浏览器", "相册", "设置", "微博", "淘宝", "支付宝"];
						var app = otherApps[random(0, otherApps.length - 1)];
						report("打开" + app + "再退出");
						try { utils.openAppByName(app); sleep(random(3000, 8000)); } catch (e) {}
					}
					doHome();
					sleep(longRest * 1000);
					// 休息完重启抖音
					utils.openAppByName("抖音");
					sleep(TIME.APP_START);
				}
			}
		}
	}

/*-------------------- 直播间互动 --------------------*/

function performLiveRoomAction() {
	report("========== 进入直播间互动流程 ==========");
	let keyword = sjzbj2();
	let staySeconds = random(LIVE_CONFIG.STAY_TIME_MIN, LIVE_CONFIG.STAY_TIME_MAX);
	let stayMinutes = Math.round(staySeconds / 60);
	report("计划在直播间停留: " + stayMinutes + "分钟 (" + staySeconds + "秒)");
	let lastLikeTime = 0, lastCommentTime = 0, likeCount = 0, commentCount = 0;
	let startTime = new Date().getTime();
	let endTime = startTime + (staySeconds * 1000);
	let detectFailCount = 0;
	while (new Date().getTime() < endTime) {
		let currentTime = new Date().getTime();
		let elapsedSeconds = Math.round((currentTime - startTime) / 1000);
		if (elapsedSeconds % 30 === 0) {
			if (!checkLiveRoomStatus()) { detectFailCount++; if (detectFailCount >= 3) { doHome(); sleep(3000); return; } doBack(); sleep(2000); continue; }
			else { detectFailCount = 0; }
		}
		if (LIVE_CONFIG.ENABLE_LIVE_LIKE) {
			let likeInterval = random(LIVE_CONFIG.LIKE_INTERVAL_MIN, LIVE_CONFIG.LIKE_INTERVAL_MAX);
			if (elapsedSeconds - lastLikeTime >= likeInterval && random(1, 100) <= LIVE_CONFIG.LIVE_LIKE_PROB) {
				for (let j = 0; j < random(1, 3); j++) { doDoubleClick(random(LIKE_CONFIG.X_MIN, LIKE_CONFIG.X_MAX), random(LIKE_CONFIG.Y_MIN, LIKE_CONFIG.Y_MAX)); sleep(random(200, 500)); }
				lastLikeTime = elapsedSeconds; likeCount++;
			}
		}
		if (LIVE_CONFIG.ENABLE_LIVE_COMMENT) {
				let commentInterval = random(LIVE_CONFIG.COMMENT_INTERVAL_MIN, LIVE_CONFIG.COMMENT_INTERVAL_MAX);
				if (elapsedSeconds - lastCommentTime >= commentInterval && random(1, 100) <= LIVE_CONFIG.LIVE_COMMENT_PROB) {
					let { scaleX, scaleY } = getScreenScale();
					let realInputX = Math.round(random(COMMENT_CONFIG.INPUT_X1, COMMENT_CONFIG.INPUT_X2) * scaleX);
					let realInputY = Math.round(random(COMMENT_CONFIG.INPUT_Y1, COMMENT_CONFIG.INPUT_Y2) * scaleY);
					doClick(realInputX, realInputY);
						sleep(800);
						// 模式无关输入（HID 走剪贴板，代理走 IME）
						let msg = getSmartComment(keyword);
						doInputText(msg);
						sleep(random(1500, 3000));
					let realSendX = Math.round(random(COMMENT_CONFIG.SEND_X1, COMMENT_CONFIG.SEND_X2) * scaleX);
					let realSendY = Math.round(random(COMMENT_CONFIG.SEND_Y1, COMMENT_CONFIG.SEND_Y2) * scaleY);
					doClick(realSendX, realSendY);
					lastCommentTime = elapsedSeconds; commentCount++;
			}
		}
		if (random(1, 100) <= 5) { sleep(random(LIVE_CONFIG.WATCH_PAUSE_MIN, LIVE_CONFIG.WATCH_PAUSE_MAX) * 1000); }
		sleep(1000);
	}
	report("本次互动: 点赞" + likeCount + "次，评论" + commentCount + "次，停留" + stayMinutes + "分钟");
	doBack(); sleep(random(1000, 2000));
	doBack(); sleep(random(1000, 2000));
}

/*-------------------- 关键词视频 --------------------*/

function performKeywordVideoAction() {
	report("========== 开始刷关键词视频流程 ==========");
	let keyword = getRandomKeyword();
	doHome(); sleep(2000);
	utils.openActivity({ "uri": "snssdk1128://search?keyword=" + keyword + "&search_tab=aweme" });
	sleep(random(5000, 8000));
	sleep(random(2000, 3000));
	let { scaleX, scaleY } = getScreenScale();
	let videoTabFound = false;
	for (let attempt = 0; attempt < 3; attempt++) {
		if (ocrFuncpddj("视频", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2)) {
			videoTabFound = true;
			sleep(random(5000, 8000));
			clickVideoArea();
			sleep(random(4000, 6000));
			break;
		}
		sleep(2000);
	}
	if (!videoTabFound) {
		swipeRightToLeft(); sleep(2000);
		if (ocrFuncpddj("视频", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2)) {
			sleep(random(5000, 8000)); clickVideoArea(); sleep(random(4000, 6000)); videoTabFound = true;
		} else {
			keyword = getRandomKeyword();
			doHome(); sleep(2000);
			utils.openActivity({ uri: "snssdk1128://search?keyword=" + keyword + "&search_tab=aweme" });
			sleep(random(5000, 8000));
			if (!ocrFuncpddj("视频", LIVE_CONFIG.VIDEO_TAB_X1, LIVE_CONFIG.VIDEO_TAB_Y1, LIVE_CONFIG.VIDEO_TAB_X2, LIVE_CONFIG.VIDEO_TAB_Y2)) { goToDouyinHome(); return; }
			sleep(random(5000, 8000)); clickVideoArea(); sleep(random(4000, 6000)); videoTabFound = true;
		}
	}
	if (!videoTabFound) { clickVideoArea(); sleep(random(5000, 8000)); }
	let staySeconds = random(LIVE_CONFIG.KEYWORD_VIDEO_STAY_MIN, LIVE_CONFIG.KEYWORD_VIDEO_STAY_MAX);
	let stayMinutes = Math.round(staySeconds / 60);
	let videoCount = 0, likeCount = 0, commentViewCount = 0, detectFailCount = 0;
	let startTime = new Date().getTime();
	let endTime = startTime + (staySeconds * 1000);
	let nextSwipeTime = startTime;
	while (new Date().getTime() < endTime) {
		let currentTime = new Date().getTime();
		if (currentTime >= nextSwipeTime) {
			let { scaleX: sx, scaleY: sy } = getScreenScale();
			let has666 = cvpd("666.png", 1225, 1439, 1433, 1941);
			let hasHomeTab = ocrFuncpd("首页", Math.round(COMMENT_CONFIG.HOME_CHECK_X1 * sx), Math.round(COMMENT_CONFIG.HOME_CHECK_Y1 * sy), Math.round(COMMENT_CONFIG.HOME_CHECK_X2 * sx), Math.round(COMMENT_CONFIG.HOME_CHECK_Y2 * sy));
			if (!has666 || hasHomeTab) { detectFailCount++; if (detectFailCount >= 3) { goToDouyinHome(); return; } doBack(); sleep(2000); continue; }
			else { detectFailCount = 0; }
                performSwipe(); videoCount++;
                sleep(random(3000, 6000));
                if (LIVE_CONFIG.ENABLE_KW_LIKE && random(1, 100) <= LIVE_CONFIG.KW_LIKE_PROB) { performLike(); likeCount++; }
                if (LIVE_CONFIG.ENABLE_KW_COMMENT && random(1, 100) <= LIVE_CONFIG.KW_COMMENT_PROB) { performCommentAction(); commentViewCount++; }
                if (LIVE_CONFIG.ENABLE_KW_COMMENT && random(1, 100) <= LIVE_CONFIG.KW_POST_COMMENT_PROB) { performPostComment(); }
                if (LIVE_CONFIG.ENABLE_FOLLOW_VIDEO && random(1, 100) <= LIVE_CONFIG.FOLLOW_PROB) { performFollow(); }
                nextSwipeTime = currentTime + random((CONFIG.swipeIntervalMin || 15) * 1000, (CONFIG.swipeIntervalMax || 30) * 1000);
		}
		sleep(1000);
	}
	report("本次统计: 刷视频" + videoCount + "个，点赞" + likeCount + "次");
	goToDouyinHome();
	for (let i = 0; i < 3; i++) { if (ocrFuncpd("首页", COMMENT_CONFIG.HOME_CHECK_X1, COMMENT_CONFIG.HOME_CHECK_Y1, COMMENT_CONFIG.HOME_CHECK_X2, COMMENT_CONFIG.HOME_CHECK_Y2)) break; doBack(); sleep(1500); }
}

/*-------------------- 视频页点关注 --------------------*/

function performFollow() {
    report("执行视频页点关注");
    // 使用模板匹配查找"点关注"按钮并自动点击
    cv(
        FOLLOW_CONFIG.IMAGE_NAME,
        FOLLOW_CONFIG.CHECK_X1, FOLLOW_CONFIG.CHECK_Y1,
        FOLLOW_CONFIG.CHECK_X2, FOLLOW_CONFIG.CHECK_Y2
    );
}

/*-------------------- 评论浏览 --------------------*/

function performCommentAction() {
    report("执行查看评论操作");
    let { scaleX, scaleY } = getScreenScale();

    // 点右侧栏评论区图标进入评论区（重试2次）
    let inComment = false;
    for (let retry = 0; retry < 2; retry++) {
        let cx = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_X1, COMMENT_CONFIG.COMMENT_CLICK_X2) * scaleX);
        let cy = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_Y1, COMMENT_CONFIG.COMMENT_CLICK_Y2) * scaleY);
        report("点右侧评论区图标: (" + cx + "," + cy + ")");
        doClick(cx, cy);
        sleep(2500);
        if (ocrFuncpd("评论", COMMENT_CONFIG.COMMENT_CHECK_X1, COMMENT_CONFIG.COMMENT_CHECK_Y1, COMMENT_CONFIG.COMMENT_CHECK_X2, COMMENT_CONFIG.COMMENT_CHECK_Y2)) {
            inComment = true;
            break;
        }
        report("评论区未打开，重试第" + (retry + 1) + "次");
    }
    if (!inComment) {
        report("评论区始终未打开，放弃");
        return;
    }

    let pages = random(COMMENT_CONFIG.PAGES_MIN, COMMENT_CONFIG.PAGES_MAX);
    for (let i = 0; i < pages; i++) { sleep(random(TIME.COMMENT_VIEW_MIN, TIME.COMMENT_VIEW_MAX)); performCommentSectionSwipe(); }
    doBack();
    sleep(TIME.BACK_DELAY);
}

/*-------------------- 发表评论 --------------------*/

/**
 * 发表一条评论（AI 生成 + 输入 + 发送）
 * 适配所有模式：代理用 IME，HID 用剪贴板粘贴
 */
function performPostComment() {
    report("========== 开始发表评论 ==========");
    let { scaleX, scaleY } = getScreenScale();

    // 1. 生成评论内容
    let msg = getSmartComment(null);
    if (!msg || msg.length === 0) {
        report("评论生成为空，跳过");
        return;
    }
    report("评论内容: " + msg);

    // 2. 点右侧栏评论区图标 → 进入评论区
    let cx = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_X1, COMMENT_CONFIG.COMMENT_CLICK_X2) * scaleX);
    let cy = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_Y1, COMMENT_CONFIG.COMMENT_CLICK_Y2) * scaleY);
    toast("点右侧评论区图标: (" + cx + "," + cy + ")");
    doClick(cx, cy);
    sleep(2500);

    // 3. 确认进入评论区（重试2次）
    let inComment = false;
    for (let retry = 0; retry < 2; retry++) {
        if (ocrFuncpd("评论", COMMENT_CONFIG.COMMENT_CHECK_X1, COMMENT_CONFIG.COMMENT_CHECK_Y1, COMMENT_CONFIG.COMMENT_CHECK_X2, COMMENT_CONFIG.COMMENT_CHECK_Y2)) {
            inComment = true;
            break;
        }
        toast("评论区未打开，重试第" + (retry + 1) + "次点右侧图标...");
        let rx = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_X1, COMMENT_CONFIG.COMMENT_CLICK_X2) * scaleX);
        let ry = Math.round(random(COMMENT_CONFIG.COMMENT_CLICK_Y1, COMMENT_CONFIG.COMMENT_CLICK_Y2) * scaleY);
        doClick(rx, ry);
        sleep(2500);
    }
    if (!inComment) {
        toast("评论区始终未打开，放弃发评论");
        doBack();
        return;
    }
    toast("已进入评论区");

    // 4. 点击输入框
    let inputX = Math.round(random(COMMENT_CONFIG.INPUT_X1, COMMENT_CONFIG.INPUT_X2) * scaleX);
    let inputY = Math.round(random(COMMENT_CONFIG.INPUT_Y1, COMMENT_CONFIG.INPUT_Y2) * scaleY);
    report("点击输入框: (" + inputX + "," + inputY + ")");
    doClick(inputX, inputY);
    sleep(800);

    // 5. 输入文字（设置剪贴板）
    doInputText(msg);
    sleep(800);

    // 6. 点击发送
    let sendX = Math.round(random(COMMENT_CONFIG.SEND_X1, COMMENT_CONFIG.SEND_X2) * scaleX);
    let sendY = Math.round(random(COMMENT_CONFIG.SEND_Y1, COMMENT_CONFIG.SEND_Y2) * scaleY);
    report("点击发送: (" + sendX + "," + sendY + ")");
    doClick(sendX, sendY);
    sleep(1000);

    // 7. 返回
    doBack();
    sleep(TIME.BACK_DELAY);
    report("========== 评论发送完成 ==========");
}
