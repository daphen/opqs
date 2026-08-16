pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

Singleton {
    id: backend

    property bool open: false
    property string nonce: ""
    property string target: ""
    property string status: "loading"
    property string message: ""
    property var suggestions: []
    signal resetRequested()

    function send(obj) {
        if (socket.connected)
            socket.write(JSON.stringify(obj) + "\n")
    }

    function search(query) {
        if (nonce)
            send({ type: "search", nonce: nonce, query: query })
    }

    function refresh() {
        if (nonce)
            send({ type: "refresh", nonce: nonce })
    }

    function select(item, kind, label) {
        if (!item || !nonce)
            return
        send({
            type: "select",
            nonce: nonce,
            item_id: item.item_id,
            field_kind: kind || item.field_kind,
            field_label: label || item.field_label
        })
    }

    function cancel() {
        if (nonce)
            send({ type: "cancel", nonce: nonce })
        open = false
        nonce = ""
    }

    function onEvent(line) {
        let event
        try { event = JSON.parse(line) } catch (error) { return }
        if (event.type === "show") {
            nonce = event.nonce || ""
            target = event.target || "original window"
            status = event.status || "ready"
            message = event.message || ""
            suggestions = event.suggestions || []
            resetRequested()
            open = true
        } else if (event.type === "results" && event.nonce === nonce) {
            suggestions = event.suggestions || []
        } else if (event.type === "status") {
            status = event.status || ""
            message = event.message || ""
        } else if (event.type === "hide" && event.nonce === nonce) {
            open = false
            hiddenAck.restart()
        } else if (event.type === "done") {
            nonce = ""
            open = false
        }
    }

    Timer {
        id: hiddenAck
        interval: 60
        onTriggered: backend.send({ type: "hidden", nonce: backend.nonce })
    }

    Socket {
        id: socket
        path: Quickshell.env("XDG_RUNTIME_DIR") + "/opqs.sock"
        connected: true
        parser: SplitParser { onRead: data => backend.onEvent(data) }
        onConnectionStateChanged: {
            if (connected) {
                backend.send({ type: "hello" })
                reconnect.stop()
            } else {
                reconnect.restart()
            }
        }
    }

    Timer {
        id: reconnect
        interval: 750
        repeat: true
        onTriggered: {
            socket.connected = false
            Qt.callLater(() => socket.connected = true)
        }
    }
}
