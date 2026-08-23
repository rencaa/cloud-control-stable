/**
 * 云控 MQTT 传输层
 *
 * 这是给真实业务脚本使用的最小 MQTT 3.1.1 客户端。
 * 它只依赖 666 项目已有的 JsSocket、thread、setInterval 和
 * EasyClick 的 Java 运行时，不依赖未解析的 mqtt.js。
 *
 * 上行事件：
 *   device/{device_id}/event
 *
 * 下行命令：
 *   cloud/device/{device_id}/command
 */

function CloudMqttTransport(options) {
    options = options || {};
    this.host = options.host || "192.168.20.88";
    this.port = options.port || 1883;
    this.clientId = options.clientId || ("ec-" + new Date().getTime());
    this.username = options.username || "";
    this.password = options.password || "";
    this.keepAlive = options.keepAlive || 60;
    this.commandTopic = options.commandTopic ||
        "cloud/device/" + this.clientId + "/command";
    // Keep the event topic aligned with the server-side device namespace.
    this.eventTopic = options.eventTopic ||
        "cloud/device/" + this.clientId + "/event";
    this.socket = null;
    this.input = null;
    this.output = null;
    this.writeLock = new java.util.concurrent.locks.ReentrantLock();
    this.running = false;
    // close() actively interrupts the blocking reader.  Mark that path so
    // expected socket-close errors are not reported as transport failures.
    this.closing = false;
    this.readerTask = null;
    this.heartbeatTimer = null;
    this.packetId = 1;
    this.onMessageCallback = null;
    this.onCloseCallback = null;
    this.onErrorCallback = null;
    this.onActivityCallback = null;
    this.lastInboundAt = 0;
    this.pingSentAt = 0;
}

CloudMqttTransport.prototype.onMessage = function (callback) {
    this.onMessageCallback = callback;
};

CloudMqttTransport.prototype.onClose = function (callback) {
    this.onCloseCallback = callback;
};

CloudMqttTransport.prototype.onError = function (callback) {
    this.onErrorCallback = callback;
};

CloudMqttTransport.prototype.onActivity = function (callback) {
    this.onActivityCallback = callback;
};

CloudMqttTransport.prototype.isConnected = function () {
    return this.running && this.socket != null && !this.socket.isClosed();
};

CloudMqttTransport.prototype._byteValue = function (number) {
    var value = Number(number) & 255;
    return value > 127 ? value - 256 : value;
};

CloudMqttTransport.prototype._javaBytes = function (numbers) {
    var bytes = java.lang.reflect.Array.newInstance(
        java.lang.Byte.TYPE, numbers.length
    );
    for (var i = 0; i < numbers.length; i++) {
        java.lang.reflect.Array.setByte(bytes, i, this._byteValue(numbers[i]));
    }
    return bytes;
};

CloudMqttTransport.prototype._utf8Bytes = function (text) {
    var value = new java.lang.String(String(text));
    var javaArray = value.getBytes("UTF-8");
    var result = [];
    for (var i = 0; i < javaArray.length; i++) {
        result.push(Number(javaArray[i]) & 255);
    }
    return result;
};

CloudMqttTransport.prototype._bytesToUtf8 = function (numbers) {
    return String(new java.lang.String(this._javaBytes(numbers), "UTF-8"));
};

CloudMqttTransport.prototype._pushU16 = function (target, value) {
    target.push((Number(value) >> 8) & 255);
    target.push(Number(value) & 255);
};

CloudMqttTransport.prototype._pushUtf8 = function (target, value) {
    var bytes = this._utf8Bytes(value);
    this._pushU16(target, bytes.length);
    for (var i = 0; i < bytes.length; i++) {
        target.push(bytes[i]);
    }
};

CloudMqttTransport.prototype._nextPacketId = function () {
    this.packetId++;
    if (this.packetId > 65535) {
        this.packetId = 1;
    }
    return this.packetId;
};

CloudMqttTransport.prototype._remainingLength = function (length) {
    var result = [];
    var value = Number(length);
    do {
        var digit = value % 128;
        value = Math.floor(value / 128);
        if (value > 0) {
            digit = digit | 128;
        }
        result.push(digit);
    } while (value > 0);
    return result;
};

CloudMqttTransport.prototype._packet = function (header, body) {
    var result = [header];
    var remaining = this._remainingLength(body.length);
    var i;
    for (i = 0; i < remaining.length; i++) {
        result.push(remaining[i]);
    }
    for (i = 0; i < body.length; i++) {
        result.push(body[i]);
    }
    return result;
};

CloudMqttTransport.prototype._connectPacket = function () {
    var body = [];
    this._pushUtf8(body, "MQTT");
    body.push(4);
    // Clean Session=false。外部 EMQX 可保留订阅；嵌入 Broker 则由服务端
    // durable outbox + cmd_id ACK 完成等价的离线补发。
    var connectFlags = 0;
    if (this.username) connectFlags = connectFlags | 0x80;
    if (this.password) connectFlags = connectFlags | 0x40;
    body.push(connectFlags);
    this._pushU16(body, this.keepAlive);
    this._pushUtf8(body, this.clientId);
    if (this.username) this._pushUtf8(body, this.username);
    if (this.password) this._pushUtf8(body, this.password);
    return this._packet(0x10, body);
};

CloudMqttTransport.prototype._subscribePacket = function () {
    var body = [];
    this._pushU16(body, this._nextPacketId());
    this._pushUtf8(body, this.commandTopic);
    // 下行使用 QoS 1；业务层仍通过 cmd_id + ACK 确认“已处理”。
    body.push(1);
    return this._packet(0x82, body);
};

CloudMqttTransport.prototype._publishPacket = function (topic, text, qos) {
    var body = [];
    this._pushUtf8(body, topic);
    var header = 0x30;
    if (qos === 1) {
        this._pushU16(body, this._nextPacketId());
        header = 0x32;
    }
    var payload = this._utf8Bytes(text);
    for (var i = 0; i < payload.length; i++) {
        body.push(payload[i]);
    }
    return this._packet(header, body);
};

CloudMqttTransport.prototype._pingPacket = function () {
    return this._packet(0xC0, []);
};

CloudMqttTransport.prototype._pubAckPacket = function (packetId) {
    var body = [];
    this._pushU16(body, packetId);
    return this._packet(0x40, body);
};

CloudMqttTransport.prototype._write = function (packet) {
    if (this.output == null) {
        return false;
    }
    this.writeLock.lock();
    try {
        if (this.output == null) {
            return false;
        }
        var bytes = this._javaBytes(packet);
        this.output.write(bytes, 0, packet.length);
        this.output.flush();
        return true;
    } catch (error) {
        if (!this.closing) {
            this._reportError(error);
        }
        return false;
    } finally {
        this.writeLock.unlock();
    }
};

CloudMqttTransport.prototype._readByte = function () {
    var value = this.input.read();
    if (value == -1) {
        throw new Error("MQTT 连接已关闭");
    }
    return Number(value) & 255;
};

CloudMqttTransport.prototype._readExact = function (length) {
    var result = [];
    for (var i = 0; i < length; i++) {
        result.push(this._readByte());
    }
    return result;
};

CloudMqttTransport.prototype._readRemainingLength = function () {
    var multiplier = 1;
    var value = 0;
    for (var i = 0; i < 4; i++) {
        var digit = this._readByte();
        value += (digit & 127) * multiplier;
        if ((digit & 128) === 0) {
            return value;
        }
        multiplier *= 128;
    }
    throw new Error("MQTT 剩余长度非法");
};

CloudMqttTransport.prototype._readPacket = function () {
    var header = this._readByte();
    var length = this._readRemainingLength();
    if (length > 2 * 1024 * 1024) {
        throw new Error("MQTT 报文超过 2MB");
    }
    return {
        header: header,
        body: this._readExact(length)
    };
};

CloudMqttTransport.prototype._readU16 = function (body, offset) {
    return ((body[offset] & 255) << 8) | (body[offset + 1] & 255);
};

CloudMqttTransport.prototype._readUtf8 = function (body, offset) {
    var length = this._readU16(body, offset);
    var start = offset + 2;
    return {
        value: this._bytesToUtf8(body.slice(start, start + length)),
        next: start + length
    };
};

CloudMqttTransport.prototype._handlePacket = function (packet) {
    var packetType = packet.header >> 4;
    this.lastInboundAt = new Date().getTime();
    if (this.onActivityCallback != null) {
        try { this.onActivityCallback(packetType); } catch (ignoreActivity) {}
    }
    if (packetType === 3) {
        var topic = this._readUtf8(packet.body, 0);
        var offset = topic.next;
        var qos = (packet.header >> 1) & 3;
        var packetId = 0;
        if (qos > 0) {
            packetId = this._readU16(packet.body, offset);
            offset += 2;
            if (qos === 1) {
                this._write(this._pubAckPacket(packetId));
            }
        }
        var payload = this._bytesToUtf8(packet.body.slice(offset));
        if (this.onMessageCallback != null) {
            this.onMessageCallback(payload, topic.value);
        }
    }
    if (packetType === 13) {
        this.pingSentAt = 0;
    }
    // CONNACK、SUBACK、PINGRESP 和服务端心跳响应不需要上抛业务层。
};

CloudMqttTransport.prototype._reportError = function (error) {
    if (this.onErrorCallback != null) {
        try {
            this.onErrorCallback(error);
        } catch (ignore) {
        }
    }
};

CloudMqttTransport.prototype._readerLoop = function () {
    try {
        while (this.running) {
            this._handlePacket(this._readPacket());
        }
    } catch (error) {
        if (this.running && !this.closing) {
            this._reportError(error);
        }
    }
    if (this.running) {
        this.running = false;
        this.closing = true;
        this._stopHeartbeat();
        if (this.socket != null) {
            this.socket.close();
        }
        if (this.onCloseCallback != null) {
            try {
                this.onCloseCallback();
            } catch (ignore) {
            }
        }
    }
};

CloudMqttTransport.prototype._startHeartbeat = function () {
    this._stopHeartbeat();
    var self = this;
    this.heartbeatTimer = setInterval(function () {
        if (self.isConnected()) {
            var now = new Date().getTime();
            var staleAfter = Math.max(45000, self.keepAlive * 2000);
            if (self.lastInboundAt > 0 && now - self.lastInboundAt > staleAfter) {
                self._reportError(new Error("MQTT 心跳响应超时，主动重连"));
                if (self.socket != null) self.socket.close();
                return;
            }
            self.pingSentAt = now;
            self._write(self._pingPacket());
        }
    }, Math.max(15000, Math.floor(this.keepAlive * 500)));
};

CloudMqttTransport.prototype._stopHeartbeat = function () {
    if (this.heartbeatTimer != null) {
        cancelInterval(this.heartbeatTimer);
        this.heartbeatTimer = null;
    }
};

CloudMqttTransport.prototype.connect = function () {
    try {
        this.closing = false;
        this.socket = new JsSocket();
        this.socket.setTcpNoDelay(true);
        this.socket.setKeepAlive(true);
        if (!this.socket.connect(this.host, this.port)) {
            throw new Error(this.socket.getErrorMsg() || "MQTT connect 返回 false");
        }
        this.input = this.socket.getInputStream();
        this.output = this.socket.getOutputStream();
        if (!this._write(this._connectPacket())) {
            throw new Error("MQTT CONNECT 写入失败");
        }
        var connack = this._readPacket();
        if ((connack.header >> 4) !== 2 ||
            connack.body.length < 2 ||
            connack.body[1] !== 0) {
            throw new Error("MQTT CONNACK 被服务器拒绝");
        }
        if (!this._write(this._subscribePacket())) {
            throw new Error("MQTT SUBSCRIBE 写入失败");
        }
        this.running = true;
        this.lastInboundAt = new Date().getTime();
        this._startHeartbeat();
        var self = this;
        this.readerTask = thread.execAsync(function () {
            self._readerLoop();
        });
        return true;
    } catch (error) {
        this.running = false;
        this.closing = true;
        this._stopHeartbeat();
        if (this.onErrorCallback != null) {
            this._reportError(error);
        }
        if (this.socket != null) {
            this.socket.close();
        }
        return false;
    }
};

CloudMqttTransport.prototype.sendObject = function (message) {
    if (!this.isConnected()) {
        return false;
    }
    try {
        var qos = message && (message.type === "ack" ||
            message.type === "task_status") ? 1 : 0;
        return this._write(this._publishPacket(
            this.eventTopic, JSON.stringify(message), qos
        ));
    } catch (error) {
        this._reportError(error);
        return false;
    }
};

CloudMqttTransport.prototype.close = function () {
    var wasRunning = this.running;
    this.closing = true;
    this.running = false;
    this._stopHeartbeat();
    if (wasRunning && this.output != null) {
        try {
            this._write(this._packet(0xE0, []));
        } catch (ignore) {
        }
    }
    if (this.socket != null) {
        this.socket.close();
    }
    this.socket = null;
    this.input = null;
    this.output = null;
};

module.exports = CloudMqttTransport;
