import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Wayland
import QsLib as Lib

PanelWindow {
    id: root

    visible: Backend.open
    color: "transparent"
    anchors { top: true; bottom: true; left: true; right: true }
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "opqs"
    WlrLayershell.keyboardFocus: Backend.open ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    property int selected: 0
    property bool customMode: false
    property var customItem: null

    function reset() {
        selected = 0
        customMode = false
        customItem = null
        query.text = ""
        Qt.callLater(() => query.forceActiveFocus())
    }

    function currentItem() {
        if (Backend.suggestions.length === 0)
            return null
        return Backend.suggestions[Math.max(0, Math.min(selected, Backend.suggestions.length - 1))]
    }

    function activate() {
        const item = currentItem()
        if (!item)
            return
        if (!item.field_kind) {
            customMode = true
            customItem = item
            query.text = ""
            return
        }
        Backend.select(item, item.field_kind, item.field_label)
    }

    function openCustom() {
        const item = currentItem()
        if (!item)
            return
        customItem = item
        customMode = true
        query.text = ""
        Backend.message = "Type the exact 1Password field label"
    }

    function cycleField() {
        const item = currentItem()
        if (!item)
            return
        for (let offset = 1; offset <= Backend.suggestions.length; offset++) {
            const index = (selected + offset) % Backend.suggestions.length
            if (Backend.suggestions[index].item_id === item.item_id) {
                selected = index
                return
            }
        }
    }

    Connections {
        target: Backend
        function onResetRequested() { root.reset() }
    }

    Rectangle {
        anchors.fill: parent
        color: "#000000"
        opacity: Backend.open ? 0.38 : 0
    }

    Rectangle {
        id: panel
        width: 760
        height: Math.min(540, 132 + list.contentHeight)
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Backend.open ? 80 : -height
        radius: Lib.Theme.radiusCard
        color: Lib.Theme.bg
        border.color: Lib.Theme.hairline
        border.width: 1
        clip: true

        Behavior on anchors.bottomMargin {
            NumberAnimation { duration: 220; easing.type: Easing.OutCubic }
        }

        Column {
            anchors.fill: parent

            Item {
                width: parent.width
                height: 72

                Rectangle {
                    anchors.fill: parent
                    anchors.margins: 14
                    radius: Lib.Theme.insetCard
                    color: Lib.Theme.surface1
                    border.color: Lib.Theme.hairline
                    border.width: 1
                }

                TextField {
                    id: query
                    anchors.fill: parent
                    anchors.leftMargin: 30
                    anchors.rightMargin: 30
                    placeholderText: root.customMode ? "exact field label" : "item, username, email, password, name, or OTP"
                    color: Lib.Theme.fg
                    placeholderTextColor: Lib.Theme.fg_muted
                    font.family: Lib.Theme.fontFamily
                    font.pixelSize: 18
                    background: null
                    onTextChanged: {
                        if (!root.customMode) {
                            root.selected = 0
                            Backend.search(text)
                        }
                    }
                    Keys.onPressed: event => {
                        if (event.key === Qt.Key_Escape) {
                            if (root.customMode) root.reset()
                            else Backend.cancel()
                            event.accepted = true
                        } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                            if (root.customMode) {
                                const label = query.text.trim()
                                if (label && root.customItem)
                                    Backend.select(root.customItem, "custom", label)
                            } else root.activate()
                            event.accepted = true
                        } else if (event.key === Qt.Key_Down || (event.key === Qt.Key_J && (event.modifiers & Qt.ControlModifier))) {
                            root.selected = Math.min(Backend.suggestions.length - 1, root.selected + 1)
                            event.accepted = true
                        } else if (event.key === Qt.Key_Up || (event.key === Qt.Key_K && (event.modifiers & Qt.ControlModifier))) {
                            root.selected = Math.max(0, root.selected - 1)
                            event.accepted = true
                        } else if (event.key === Qt.Key_Tab && !root.customMode) {
                            root.cycleField()
                            event.accepted = true
                        } else if (event.key === Qt.Key_O && (event.modifiers & Qt.ControlModifier) && !root.customMode) {
                            root.openCustom()
                            event.accepted = true
                        } else if (event.key === Qt.Key_R && (event.modifiers & Qt.ControlModifier) && !root.customMode) {
                            Backend.refresh()
                            event.accepted = true
                        }
                    }
                }
            }

            Text {
                width: parent.width - 56
                height: 28
                anchors.horizontalCenter: parent.horizontalCenter
                text: Backend.message || (root.customMode ? "Only this field will be fetched" : "Target: " + Backend.target)
                color: Backend.message ? Lib.Theme.orange : Lib.Theme.fg_muted
                font.family: Lib.Theme.fontFamily
                font.pixelSize: 12
                elide: Text.ElideRight
            }

            ListView {
                id: list
                width: parent.width
                height: Math.min(388, contentHeight)
                clip: true
                model: root.customMode ? [] : Backend.suggestions
                currentIndex: root.selected

                delegate: Item {
                    required property var modelData
                    required property int index
                    width: list.width
                    height: 56

                    Rectangle {
                        anchors.fill: parent
                        anchors.leftMargin: 14
                        anchors.rightMargin: 14
                        radius: Lib.Theme.insetCard
                        color: parent.index === root.selected ? Lib.Theme.selection : "transparent"
                    }

                    Column {
                        anchors.left: parent.left
                        anchors.right: fieldKind.left
                        anchors.leftMargin: 28
                        anchors.rightMargin: 16
                        anchors.verticalCenter: parent.verticalCenter
                        spacing: 2
                        Text {
                            width: parent.width
                            text: modelData.label || modelData.title
                            color: Lib.Theme.fg
                            font.family: Lib.Theme.fontFamily
                            font.pixelSize: 15
                            font.weight: 500
                            elide: Text.ElideRight
                        }
                        Text {
                            width: parent.width
                            text: modelData.subtitle || modelData.vault || ""
                            color: Lib.Theme.fg_muted
                            font.family: Lib.Theme.fontFamily
                            font.pixelSize: 12
                            elide: Text.ElideRight
                        }
                    }

                    Text {
                        id: fieldKind
                        anchors.right: parent.right
                        anchors.rightMargin: 28
                        anchors.verticalCenter: parent.verticalCenter
                        text: modelData.category || ""
                        color: Lib.Theme.fg_muted
                        font.family: Lib.Theme.fontFamily
                        font.pixelSize: 11
                    }
                }

                onCurrentIndexChanged: positionViewAtIndex(currentIndex, ListView.Contain)
            }

            Item {
                width: parent.width
                height: 32
                Row {
                    anchors.left: parent.left
                    anchors.leftMargin: 22
                    spacing: 18
                    Text { text: "j/k move"; color: Lib.Theme.fg_muted; font.family: Lib.Theme.fontFamily; font.pixelSize: 11 }
                    Text { text: "↵ type"; color: Lib.Theme.fg_muted; font.family: Lib.Theme.fontFamily; font.pixelSize: 11 }
                    Text { text: "tab field"; color: Lib.Theme.fg_muted; font.family: Lib.Theme.fontFamily; font.pixelSize: 11 }
                    Text { text: "ctrl+o custom"; color: Lib.Theme.fg_muted; font.family: Lib.Theme.fontFamily; font.pixelSize: 11 }
                    Text { text: "esc cancel"; color: Lib.Theme.fg_muted; font.family: Lib.Theme.fontFamily; font.pixelSize: 11 }
                }
            }
        }
    }
}
